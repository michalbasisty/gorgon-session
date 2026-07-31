// Package server exposes the embedded HTTP API + UI for gorgon-session.
package server

import (
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/michalbasisty/gorgon-session/internal/cdn"
	"github.com/michalbasisty/gorgon-session/internal/config"
	"github.com/michalbasisty/gorgon-session/internal/favor"
	"github.com/michalbasisty/gorgon-session/internal/logtail"
	"github.com/michalbasisty/gorgon-session/internal/loot"
	"github.com/michalbasisty/gorgon-session/internal/prices"
	"github.com/michalbasisty/gorgon-session/internal/session"
	"github.com/michalbasisty/gorgon-session/internal/trader"
)

// Server combines all the moving parts reachable over HTTP.
type Server struct {
	Cfg                config.Config
	Sess               *session.Manager
	Favor              *favor.Engine
	WebFS              fs.FS // embedded static content (web/ folder, serve at root)
	Tailer             *logtail.Tailer
	Parser             *loot.Parser
	Trader             *trader.Manager
	PLTailer           *logtail.FileTailer // Player.log tailer
	ItemByID           map[int]cdn.Item    // item code -> item data
	itemByName         map[string]cdn.Item // ponytail: lowercase-name index, beats O(n) scan
	Prices             *prices.Store       // item price history
	Npcs               cdn.NpcsFile        // full NPC data with services
	Areas              cdn.AreaIndex       // zone hierarchy lookup
	Skills             cdn.SkillsFile      // skill definitions
	Recipes            cdn.RecipesFile     // crafting recipes
	Abilities          cdn.AbilitiesFile   // combat abilities
	abilityByID        map[int]cdn.Ability
	abilityByNameKey   map[string]cdn.Ability
	abilityIDByNameKey map[string]int

	cfgMu          sync.RWMutex // guards Cfg (written by handleConfig/handleImport, read everywhere)
	sessionsMu     sync.RWMutex
	sessionsCache  []SessionSummary
	sessionsCached bool // false = invalidated
}

// New wires a Server.
func New(cfg config.Config, sess *session.Manager, favor *favor.Engine, webFS fs.FS, tailer *logtail.Tailer, plTailer *logtail.FileTailer, parser *loot.Parser, trader *trader.Manager, items cdn.ItemsFile, ver cdn.Version, npcs cdn.NpcsFile, areas cdn.AreaIndex, skills cdn.SkillsFile, recipes cdn.RecipesFile, abilities cdn.AbilitiesFile) *Server {
	// Build item-by-ID map
	itemByID := make(map[int]cdn.Item)
	itemByName := make(map[string]cdn.Item, len(items))
	for _, item := range items {
		itemByID[item.ItemID] = item
		if item.Name != "" {
			itemByName[strings.ToLower(strings.TrimSpace(item.Name))] = item
		}
	}

	// Price history store lives next to reports
	pricesPath := filepath.Join(cfg.ReportDir, "price-history.json")
	pricesStore := prices.New(pricesPath)

	abilityByID := make(map[int]cdn.Ability, len(abilities))
	abilityByNameKey := make(map[string]cdn.Ability, len(abilities)*2)
	abilityIDByNameKey := make(map[string]int, len(abilities)*2)
	for key, a := range abilities {
		id := 0
		if strings.HasPrefix(key, "ability_") {
			if parsed, err := strconv.Atoi(strings.TrimPrefix(key, "ability_")); err == nil {
				id = parsed
				abilityByID[id] = a
			}
		} else if parsed, err := strconv.Atoi(strings.TrimSpace(key)); err == nil {
			// Some ability dumps can be keyed by plain numeric strings.
			id = parsed
			abilityByID[id] = a
		}
		if a.Name != "" {
			k := strings.ToLower(strings.TrimSpace(a.Name))
			abilityByNameKey[k] = a
			if id > 0 {
				abilityIDByNameKey[k] = id
			}
		}
		if a.InternalName != "" {
			k := strings.ToLower(strings.TrimSpace(a.InternalName))
			abilityByNameKey[k] = a
			if id > 0 {
				abilityIDByNameKey[k] = id
			}
		}
	}

	return &Server{
		Cfg:                cfg,
		Sess:               sess,
		Favor:              favor,
		WebFS:              webFS,
		Tailer:             tailer,
		PLTailer:           plTailer,
		Parser:             parser,
		Trader:             trader,
		ItemByID:           itemByID,
		itemByName:         itemByName,
		Prices:             pricesStore,
		Npcs:               npcs,
		Areas:              areas,
		Skills:             skills,
		Recipes:            recipes,
		Abilities:          abilities,
		abilityByID:        abilityByID,
		abilityByNameKey:   abilityByNameKey,
		abilityIDByNameKey: abilityIDByNameKey,
	}
}
