package server

import "net/http"

// Mount attaches routes to a mux and returns it.
func (s *Server) Mount() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/session", s.handleSession)
	mux.HandleFunc("/api/session/start", s.handleStart)
	mux.HandleFunc("/api/session/stop", s.handleStop)
	mux.HandleFunc("/api/session/", s.handleSessionByID)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/loot", s.handleLoot)
	mux.HandleFunc("/api/loot-note", s.handleLootNote)
	mux.HandleFunc("/api/feed", s.handleFeed)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/npcs", s.handleNPCs)
	mux.HandleFunc("/api/traders", s.handleTraders)
	mux.HandleFunc("/api/export", s.handleExport)
	mux.HandleFunc("/api/import", s.handleImport)
	mux.HandleFunc("/api/areas", s.handleAreas)
	mux.HandleFunc("/api/skills", s.handleSkills)
	mux.HandleFunc("/api/recipes", s.handleRecipes)
	mux.HandleFunc("/api/items", s.handleItems)
	mux.HandleFunc("/api/prices", s.handlePrices)
	mux.HandleFunc("/api/prices/", s.handlePriceByName)
	mux.HandleFunc("/api/combat", s.handleCombat)
	mux.HandleFunc("/api/zone-npcs", s.handleZoneNPCs)
	mux.HandleFunc("/api/drop-rates", s.handleDropRates)
	mux.HandleFunc("/api/sessions/bulk-export", s.handleBulkExport)
	mux.HandleFunc("/overlay", s.handleOverlay)
	mux.HandleFunc("/", s.handleStatic)
	return mux
}
