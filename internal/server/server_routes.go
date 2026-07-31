package server

import (
	"net"
	"net/http"
	"slices"
	"strings"
)

// localHosts returns the host:port authorities (Host header / Origin
// authority) accepted by the local-origin guard, derived from the configured
// bind address so a custom http_addr (or the overlay's -addr passthrough)
// keeps working.
func (s *Server) localHosts() []string {
	addr := s.Cfg.HTTPAddr
	if addr == "" {
		addr = "127.0.0.1:7777"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return []string{"127.0.0.1:7777", "localhost:7777"}
	}
	hosts := []string{"127.0.0.1:" + port, "localhost:" + port}
	if host != "" && host != "127.0.0.1" && host != "localhost" && host != "::1" {
		hosts = append(hosts, host+":"+port)
	}
	return hosts
}

// requireLocal rejects requests from foreign origins or hosts: it blocks
// cross-origin pages (CSRF) and DNS-rebinding attacks. Requests without an
// Origin header (curl, native overlay, same-origin fetch) pass through.
func requireLocal(w http.ResponseWriter, r *http.Request, allowed []string) bool {
	if o := r.Header.Get("Origin"); o != "" {
		authority := strings.TrimPrefix(strings.TrimPrefix(o, "http://"), "https://")
		if !slices.Contains(allowed, authority) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return false
		}
	}
	host := strings.TrimPrefix(strings.TrimPrefix(r.Host, "http://"), "https://")
	if !slices.Contains(allowed, host) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

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
	mux.HandleFunc("/api/traders/history", s.handleTraderHistory)
	mux.HandleFunc("/api/traders/history/export", s.handleTraderHistoryExport)
	mux.HandleFunc("/api/traders/history/delete", s.handleTraderHistoryDelete)
	mux.HandleFunc("/api/traders/schedule", s.handleTraderSchedule)
	mux.HandleFunc("/api/export", s.handleExport)
	mux.HandleFunc("/api/import", s.handleImport)
	mux.HandleFunc("/api/areas", s.handleAreas)
	mux.HandleFunc("/api/skills", s.handleSkills)
	mux.HandleFunc("/api/recipes", s.handleRecipes)
	mux.HandleFunc("/api/recipes/search", s.handleRecipesSearch)
	mux.HandleFunc("/api/items", s.handleItems)
	mux.HandleFunc("/api/prices", s.handlePrices)
	mux.HandleFunc("/api/prices/", s.handlePriceByName)
	mux.HandleFunc("/api/prices/trends", s.handlePriceTrends)
	mux.HandleFunc("/api/combat", s.handleCombat)
	mux.HandleFunc("/api/combat/breakdown", s.handleCombatBreakdown)
	mux.HandleFunc("/api/zone-npcs", s.handleZoneNPCs)
	mux.HandleFunc("/api/drop-rates", s.handleDropRates)
	mux.HandleFunc("/api/sessions/bulk-export", s.handleBulkExport)
	mux.HandleFunc("/api/sessions/compare", s.handleSessionsCompare)
	mux.HandleFunc("/api/crafting/profit", s.handleCraftingProfit)
	mux.HandleFunc("/api/route-planner", s.handleRoutePlanner)
	mux.HandleFunc("/api/notes/export", s.handleNotesExport)
	mux.HandleFunc("/api/overlay/spawn", s.handleOverlaySpawn)
	mux.HandleFunc("/overlay", s.handleOverlay)
	mux.HandleFunc("/", s.handleStatic)
	allowed := s.localHosts()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requireLocal(w, r, allowed) {
			return
		}
		mux.ServeHTTP(w, r)
	})
}
