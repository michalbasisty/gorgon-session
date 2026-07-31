// Package cdn fetches and caches JSON data from cdn.projectgorgon.com.
//
// Only items.json and npcs.json are loaded for the dungeon-session MVP.
// The package is structured so future phases (crafting, combat) can add
// more sources with one line per file.
package cdn

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Client knows how to talk to the Project Gorgon CDN.
type Client struct {
	Root            string // e.g. "http://cdn.projectgorgon.com"
	VersionFile     string // e.g. "http://client.projectgorgon.com/fileversion.txt"
	FallbackVersion string // e.g. "v480"
	CacheDir        string // local cache root
	HTTP            *http.Client
}

// Version is the discovered current game data version (e.g. "v480").
type Version string

// CurrentVersion fetches and parses fileversion.txt, falling back on error.
func (c *Client) CurrentVersion() (Version, error) {
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := hc.Get(c.VersionFile)
	if err != nil {
		return Version(c.FallbackVersion), err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return Version(c.FallbackVersion), err
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return Version(c.FallbackVersion), fmt.Errorf("empty version response")
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return Version(v), nil
}

// dataURL builds the CDN URL for a source file at a given version.
func (c *Client) dataURL(v Version, source string) string {
	return fmt.Sprintf("%s/%s/data/%s.json", strings.TrimRight(c.Root, "/"), v, source)
}

// cachePath returns the on-disk cache location for a version+source.
func (c *Client) cachePath(v Version, source string) string {
	return filepath.Join(c.CacheDir, string(v), source+".json")
}

// Fetch retrieves a source for the given version. It uses the on-disk cache
// if present and fresh; otherwise it downloads and caches the JSON.
// Returns the raw bytes. Caller parses into the desired shape.
func (c *Client) Fetch(v Version, source string) ([]byte, error) {
	if c.CacheDir == "" {
		return c.download(v, source)
	}
	p := c.cachePath(v, source)
	if b, err := os.ReadFile(p); err == nil {
		return b, nil
	}
	b, err := c.download(v, source)
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, b, 0o644)
	return b, nil
}

func (c *Client) download(v Version, source string) ([]byte, error) {
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 90 * time.Second}
	}
	resp, err := hc.Get(c.dataURL(v, source))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", source, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// Area represents one zone/area from areas.json. X/Y are optional zone
// coordinates used for route-planner distance sorting; nil when the CDN
// does not publish them for this area.
type Area struct {
	FriendlyName      string   `json:"FriendlyName"`
	ShortFriendlyName string   `json:"ShortFriendlyName,omitempty"`
	X                 *float64 `json:"X,omitempty"`
	Y                 *float64 `json:"Y,omitempty"`
}

// AreasFile is the JSON root of areas.json (map of internal key -> Area).
type AreasFile map[string]Area

// AreaIndex indexes areas by friendly name for lookups.
type AreaIndex struct {
	ByInternal map[string]Area   // internal key -> Area
	ByFriendly map[string]string // lowercase friendly name -> internal key
}

// IndexAreas builds AreaIndex from parsed areas.
func IndexAreas(areas AreasFile) AreaIndex {
	idx := AreaIndex{
		ByInternal: make(map[string]Area, len(areas)),
		ByFriendly: make(map[string]string, len(areas)),
	}
	for k, a := range areas {
		idx.ByInternal[k] = a
		if a.FriendlyName != "" {
			idx.ByFriendly[strings.ToLower(strings.TrimSpace(a.FriendlyName))] = k
		}
	}
	return idx
}

// RecipeIngredient is one material in a recipe.
type RecipeIngredient struct {
	ItemCode  int `json:"ItemCode"`
	StackSize int `json:"StackSize"`
}

// RecipeResultItem is one result item.
type RecipeResultItem struct {
	ItemCode  int `json:"ItemCode"`
	StackSize int `json:"StackSize"`
}

// Recipe from recipes.json.
type Recipe struct {
	InternalName           string             `json:"InternalName"`
	Name                   string             `json:"Name"`
	Description            string             `json:"Description,omitempty"`
	Skill                  string             `json:"Skill"`
	SkillLevelReq          int                `json:"SkillLevelReq"`
	Ingredients            []RecipeIngredient `json:"Ingredients,omitempty"`
	ResultItems            []RecipeResultItem `json:"ResultItems,omitempty"`
	RewardSkill            string             `json:"RewardSkill,omitempty"`
	RewardSkillXp          int                `json:"RewardSkillXp,omitempty"`
	RewardSkillXpFirstTime int                `json:"RewardSkillXpFirstTime,omitempty"`
}

// RecipesFile is the JSON root of recipes.json (map of "recipe_N" -> Recipe).
type RecipesFile map[string]Recipe

// Skill from skills.json. Each key in the map is the skill name.
// Uses CDN field names (PascalCase).
type Skill struct {
	Combat         bool   `json:"Combat"`
	Description    string `json:"Description"`
	MaxBonusLevels int    `json:"MaxBonusLevels"`
	GuestLevelCap  int    `json:"GuestLevelCap"`
	XpTable        string `json:"XpTable"`
}

// SkillsFile is the JSON root of skills.json (map of "SkillName" -> Skill).
type SkillsFile map[string]Skill

// ---- Typed source loaders ----------------------------------------------

// ItemsFile is the JSON root of items.json: a map of "item_<id>" -> Item.
type ItemsFile map[string]Item

// Item is the subset of fields the dungeon-session app needs. Other fields
// are kept in `Raw` for forward compatibility without re-parsing.
type Item struct {
	ItemID        int      `json:"ItemID"` // populated by LoadItems from key name
	InternalName  string   `json:"InternalName"`
	Name          string   `json:"Name"`
	Description   string   `json:"Description"`
	IconID        int      `json:"IconId"`
	Value         float64  `json:"Value"`
	MaxStackSize  int      `json:"MaxStackSize"`
	NumUses       int      `json:"NumUses"`
	Keywords      []string `json:"Keywords"`
	EquipmentSlot string   `json:"EquipmentSlot,omitempty"`
	SkillPrereq   string   `json:"SkillPrereq,omitempty"`
}

// NpcsFile is the JSON root of npcs.json: a map of internal name -> Npc.
type NpcsFile map[string]Npc

// Npc holds the gift-preference + service data needed for routing loot.
type Npc struct {
	InternalName string       `json:"-"` // populated by LoadNpcs from key
	Name         string       `json:"Name"`
	AreaName     string       `json:"AreaName"`
	AreaFriendly string       `json:"AreaFriendlyName"`
	Desc         string       `json:"Desc"`
	Pos          string       `json:"Pos"`
	ItemGifts    []string     `json:"ItemGifts"`
	Preferences  []Preference `json:"Preferences"`
	Services     []Service    `json:"Services"`
}

// Preference is one gift rule: an item matches if its keywords cover every
// keyword in Keywords[]. Pref is positive for things the NPC loves and
// negative for things they hate. Composite keywords (containing ':') require
// additional item-schema data and are skipped by the MVP matcher.
type Preference struct {
	Name     string   `json:"Name"`
	Desire   string   `json:"Desire"` // "Love", "Hate", ...
	Keywords []string `json:"Keywords"`
	Pref     float64  `json:"Pref"`
	Favor    string   `json:"Favor,omitempty"` // threshold for Hate entries
}

// Service is one NPC service (Store, Consignment, Storage, Training, ...).
type Service struct {
	Type         string   `json:"Type"` // "Store","Consignment","Storage","Training","InstallAugments"
	Favor        string   `json:"Favor,omitempty"`
	ItemTypes    []string `json:"ItemTypes,omitempty"`
	Skills       []string `json:"Skills,omitempty"`
	Unlocks      []string `json:"Unlocks,omitempty"`
	CapIncreases []string `json:"CapIncreases,omitempty"`
}

// LoadItems parses items.json into typed + indexed form.
func (c *Client) LoadItems(v Version) (ItemsFile, error) {
	b, err := c.Fetch(v, "items")
	if err != nil {
		return nil, err
	}
	var f ItemsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	for k, it := range f {
		it.ItemID = keyID(k)
		f[k] = it
	}
	return f, nil
}

// LoadNpcs parses npcs.json into typed + indexed form.
func (c *Client) LoadNpcs(v Version) (NpcsFile, error) {
	b, err := c.Fetch(v, "npcs")
	if err != nil {
		return nil, err
	}
	var f NpcsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	for k, n := range f {
		n.InternalName = k
		f[k] = n
	}
	return f, nil
}

// LoadAreas parses areas.json into typed form.
func (c *Client) LoadAreas(v Version) (AreasFile, error) {
	b, err := c.Fetch(v, "areas")
	if err != nil {
		return nil, err
	}
	var f AreasFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	return f, nil
}

// LoadSkills parses skills.json into typed form.
func (c *Client) LoadSkills(v Version) (SkillsFile, error) {
	b, err := c.Fetch(v, "skills")
	if err != nil {
		return nil, err
	}
	var f SkillsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	return f, nil
}

// LoadRecipes parses recipes.json into typed form.
func (c *Client) LoadRecipes(v Version) (RecipesFile, error) {
	b, err := c.Fetch(v, "recipes")
	if err != nil {
		return nil, err
	}
	var f RecipesFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	return f, nil
}

// ---- Ability types ------------------------------------------------

// Ability is one row from abilities.json.
type Ability struct {
	InternalName string `json:"InternalName"`
	Name         string `json:"Name"`
	Skill        string `json:"Skill"`
	DamageType   string `json:"DamageType"`
	PvE          struct {
		Damage float64 `json:"Damage"`
	} `json:"PvE"`
}

// BaseDamage returns PvE base damage value used for estimated DPS.
func (a Ability) BaseDamage() float64 {
	return a.PvE.Damage
}

// AbilitiesFile is the JSON root of abilities.json (map of internal key -> Ability).
type AbilitiesFile map[string]Ability

// LoadAbilities parses abilities.json into typed form.
func (c *Client) LoadAbilities(v Version) (AbilitiesFile, error) {
	b, err := c.Fetch(v, "abilities")
	if err != nil {
		return nil, err
	}
	var f AbilitiesFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	return f, nil
}

// keyID extracts the trailing integer from a key like "item_1234" -> 1234.
func keyID(k string) int {
	idx := strings.LastIndex(k, "_")
	if idx < 0 {
		return 0
	}
	n, _ := strconv.Atoi(k[idx+1:])
	return n
}

// IconURL returns the CDN URL for an item icon by its IconID.
// Icons are served at /vXXX/icons/ICONID.png
func (c *Client) IconURL(v Version, iconID int) string {
	if iconID <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/%s/icons/%d.png", strings.TrimRight(c.Root, "/"), v, iconID)
}
