// Package favor recommends where each looted item should go: gifted to a
// specific NPC (with a score), sold at a vendor, or consigned.
//
// The engine uses two pieces of CDN ground truth from npcs.json:
//
//  1. Preferences[] — keyword-based likes/hates with a `Pref` score.
//  2. Services[]   — `Store` (vendor), `Consignment` with `ItemTypes`.
//
// Matching rules (MVP):
//   - For a preference, ALL of its Keywords[] must be present in the item's
//     Keywords[]. Composite keywords containing ':' (e.g. "EquipmentSlot:Head")
//     are deferred and skipped, since matching them requires item-schema data
//     not yet loaded.
//   - Score = sum of matched preferences' Pref values. Love = positive.
//   - Hates contribute negative score (don't gift).
//   - If no NPC scores > 0 -> item is sellable. If any Consignment NPC's
//     ItemTypes intersect the item's Keywords -> prefer consignment over
//     vendor (higher expected return); else vendor.
package favor

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/michalbasisty/gorgon-session/internal/cdn"
)

// Engine holds indexed NPC data and answers routing decisions.
type Engine struct {
	npcs         []npcRow
	byKeyword    map[string][]int // npc pref keyword -> npc row indexes
	playerPrices map[string]float64

	mu sync.RWMutex // guards playerPrices (written by SetPlayerPrices, read by ResolveItem)
}

type npcRow struct {
	internal    string
	name        string
	areaName    string
	areaLabel   string
	preferences []prefRow
	services    []svcRow
	itemGifts   []string
}

type prefRow struct {
	name     string
	desire   string
	keywords []string
	pref     float64
}

type svcRow struct {
	kind      string
	favor     string
	itemTypes []string
}

// FromNpcs builds an Engine from a cdn-loaded NpcsFile.
func FromNpcs(npcs cdn.NpcsFile) *Engine {
	e := &Engine{byKeyword: map[string][]int{}, playerPrices: map[string]float64{}}
	for _, n := range npcs {
		row := npcRow{
			internal:  n.InternalName,
			name:      n.Name,
			areaName:  n.AreaName,
			areaLabel: n.AreaFriendly,
			itemGifts: n.ItemGifts,
		}
		for _, p := range n.Preferences {
			flat := prefRow{name: p.Name, desire: p.Desire, keywords: p.Keywords, pref: p.Pref}
			row.preferences = append(row.preferences, flat)
			for _, k := range p.Keywords {
				if isComposite(k) {
					continue
				}
				idx := len(e.npcs) // index of this npc row once appended below
				e.byKeyword[k] = append(e.byKeyword[k], idx)
			}
		}
		for _, s := range n.Services {
			row.services = append(row.services, svcRow{kind: s.Type, favor: s.Favor, itemTypes: s.ItemTypes})
		}
		e.npcs = append(e.npcs, row)
	}
	return e
}

// Decision is one routing suggestion for an item.
type Decision struct {
	Item         string   `json:"item"`
	Verdict      Verdict  `json:"verdict"`
	FavorTargets []Target `json:"favor_targets,omitempty"`
	SellReason   string   `json:"sell_reason,omitempty"`
	PlayerPrice  float64  `json:"player_price,omitempty"`
}

// Verdict classifies the routing decision.
type Verdict string

const (
	VerdictFavor           Verdict = "favor"
	VerdictSellVendor      Verdict = "sell_vendor"
	VerdictSellConsignment Verdict = "sell_consignment"
	VerdictKeep            Verdict = "keep"
)

// Target is a single NPC favor suggestion with score.
type Target struct {
	NPC        string   `json:"npc"`
	Area       string   `json:"area"`
	Score      float64  `json:"score"`
	DistanceKm *float64 `json:"distance_km,omitempty"`
	Matches    []string `json:"matches"`
}

// Resolve produces a decision for one item identified by its keywords + value.
func (e *Engine) Resolve(itemName string, itemKeywords []string, itemValue float64) Decision {
	return e.ResolveItem(cdn.Item{Name: itemName, Keywords: itemKeywords, Value: itemValue})
}

// ResolveItem produces a decision for a full item, including composite keyword matching.
func (e *Engine) ResolveItem(item cdn.Item) Decision {
	e.mu.RLock()
	defer e.mu.RUnlock()
	d := Decision{Item: item.Name}
	kwSet := toSet(item.Keywords)

	// Check for player price override
	if pp, ok := e.playerPrices[item.Name]; ok {
		d.PlayerPrice = pp
	}

	type npcScore struct {
		idx     int
		score   float64
		matches []string
	}
	candidates := map[int]*npcScore{}

	// Index NPCs by both regular and composite keywords
	for _, k := range item.Keywords {
		for _, idx := range e.byKeyword[k] {
			c, ok := candidates[idx]
			if !ok {
				c = &npcScore{idx: idx}
				candidates[idx] = c
			}
		}
	}

	// Also check composite keyword matches
	for idx, row := range e.npcs {
		for _, p := range row.preferences {
			hasComposite := false
			for _, k := range p.keywords {
				if isComposite(k) {
					hasComposite = true
					break
				}
			}
			if !hasComposite {
				continue
			}
			// Check if all composite keywords match
			allMatch := true
			for _, k := range p.keywords {
				if !isComposite(k) {
					if !kwSet[k] {
						allMatch = false
						break
					}
					continue
				}
				if !matchesComposite(k, item) {
					allMatch = false
					break
				}
			}
			if allMatch {
				c, ok := candidates[idx]
				if !ok {
					c = &npcScore{idx: idx}
					candidates[idx] = c
				}
			}
		}
	}

	for _, c := range candidates {
		row := &e.npcs[c.idx]
		for _, p := range row.preferences {
			// Check if all keywords (regular + composite) match
			allMatch := true
			for _, k := range p.keywords {
				if isComposite(k) {
					if !matchesComposite(k, item) {
						allMatch = false
						break
					}
				} else {
					if !kwSet[k] {
						allMatch = false
						break
					}
				}
			}
			if !allMatch {
				continue
			}
			c.score += p.pref
			c.matches = append(c.matches, p.name)
		}
	}

	for _, c := range candidates {
		if c.score > 0 {
			row := &e.npcs[c.idx]
			d.FavorTargets = append(d.FavorTargets, Target{
				NPC: row.name, Area: row.areaLabel, Score: c.score, Matches: c.matches,
			})
		}
	}
	sortTargets(d.FavorTargets)

	if len(d.FavorTargets) > 0 {
		d.Verdict = VerdictFavor
		return d
	}

	// If item has player price > vendor value, prefer consignment
	if d.PlayerPrice > item.Value {
		for _, row := range e.npcs {
			for _, s := range row.services {
				if s.kind != "Consignment" {
					continue
				}
				for _, t := range s.itemTypes {
					if kwSet[t] {
						d.Verdict = VerdictSellConsignment
						d.SellReason = fmt.Sprintf("Consign via %s (%s) - player price %.0fg", row.name, row.areaLabel, d.PlayerPrice)
						return d
					}
				}
			}
		}
		// No consignment NPC accepts it, but player price is higher
		d.Verdict = VerdictSellConsignment
		d.SellReason = fmt.Sprintf("Sell to players - player price %.0fg (vendor %.0fg)", d.PlayerPrice, item.Value)
		return d
	}

	// No NPC loves it -> sell. Prefer consignment if any NPC accepts the
	// item's keywords as one of its ItemTypes. Vendor is the fallback.
	for _, row := range e.npcs {
		for _, s := range row.services {
			if s.kind != "Consignment" {
				continue
			}
			for _, t := range s.itemTypes {
				if kwSet[t] {
					d.Verdict = VerdictSellConsignment
					d.SellReason = "Consign via " + row.name + " (" + row.areaLabel + ")"
					return d
				}
			}
		}
	}
	d.Verdict = VerdictSellVendor
	d.SellReason = fmt.Sprintf("Vendor (value %g)", item.Value)
	return d
}

// matchesComposite checks if a composite keyword like "EquipmentSlot:Head" matches the item.
func matchesComposite(keyword string, item cdn.Item) bool {
	parts := splitComposite(keyword)
	if len(parts) != 2 {
		return false
	}
	field, value := parts[0], parts[1]
	switch field {
	case "EquipmentSlot":
		return item.EquipmentSlot == value
	case "SkillPrereq":
		return item.SkillPrereq == value
	default:
		return false
	}
}

// splitComposite splits "Field:Value" into ["Field", "Value"].
func splitComposite(s string) []string {
	return strings.SplitN(s, ":", 2)
}

func isComposite(k string) bool {
	return strings.Contains(k, ":")
}
func toSet(ks []string) map[string]bool {
	m := make(map[string]bool, len(ks))
	for _, k := range ks {
		m[k] = true
	}
	return m
}
// NPCRows returns the count of indexed NPC rows (for startup diagnostics).
func (e *Engine) NPCRows() int { return len(e.npcs) }

// GetNPCs returns all NPCs with their services (for trader lookup)
func (e *Engine) GetNPCs() []cdn.Npc {
	var npcs []cdn.Npc
	for _, row := range e.npcs {
		npc := cdn.Npc{
			InternalName: row.internal,
			Name:         row.name,
			AreaName:     row.areaName,
			AreaFriendly: row.areaLabel,
		}
		// Convert services back to cdn.Service format
		for _, svc := range row.services {
			npc.Services = append(npc.Services, cdn.Service{
				Type:  svc.kind,
				Favor: svc.favor,
			})
		}
		npcs = append(npcs, npc)
	}
	return npcs
}

// KeywordKeys returns the count of distinct preference keywords indexed
// (composite keywords are excluded).
func (e *Engine) KeywordKeys() int { return len(e.byKeyword) }

func sortTargets(ts []Target) {
	sort.Slice(ts, func(i, j int) bool { return ts[i].Score > ts[j].Score })
}

type NPCInfo struct {
	Name string `json:"name"`
	Area string `json:"area"`
}

func (e *Engine) NPCList() []NPCInfo {
	out := make([]NPCInfo, 0, len(e.npcs))
	for _, r := range e.npcs {
		if r.name == "" {
			continue
		}
		out = append(out, NPCInfo{Name: r.name, Area: r.areaLabel})
	}
	return out
}

// SetPlayerPrices updates the player price overrides.
func (e *Engine) SetPlayerPrices(prices map[string]float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.playerPrices = prices
}