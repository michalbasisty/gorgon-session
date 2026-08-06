// Package server exposes the embedded HTTP API + UI for gorgon-session.
package server

import (
	"io/fs"
	"path/filepath"
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

	cfgMu          sync.RWMutex // guards Cfg (written by handleConfig/handleImport, read everywhere)
	sessionsMu     sync.RWMutex
	sessionsCache  []SessionSummary
	sessionsCached bool // false = invalidated
}

// New wires a Server.
func New(cfg config.Config, sess *session.Manager, favor *favor.Engine, webFS fs.FS, tailer *logtail.Tailer, plTailer *logtail.FileTailer, parser *loot.Parser, trader *trader.Manager, items cdn.ItemsFile, ver cdn.Version, npcs cdn.NpcsFile, areas cdn.AreaIndex, skills cdn.SkillsFile, recipes cdn.RecipesFile) *Server {
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

	return &Server{
		Cfg:        cfg,
		Sess:       sess,
		Favor:      favor,
		WebFS:      webFS,
		Tailer:     tailer,
		PLTailer:   plTailer,
		Parser:     parser,
		Trader:     trader,
		ItemByID:   itemByID,
		itemByName: itemByName,
		Prices:     pricesStore,
		Npcs:       npcs,
		Areas:      areas,
		Skills:     skills,
		Recipes:    recipes,
	}
}
