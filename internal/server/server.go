// Package server exposes the embedded HTTP API + UI for gorgon-session.
package server

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yourname/gorgon-session/internal/cdn"
	"github.com/yourname/gorgon-session/internal/config"
	"github.com/yourname/gorgon-session/internal/favor"
	"github.com/yourname/gorgon-session/internal/logtail"
	"github.com/yourname/gorgon-session/internal/loot"
	"github.com/yourname/gorgon-session/internal/session"
	"github.com/yourname/gorgon-session/internal/trader"
)

// Server combines all the moving parts reachable over HTTP.
type Server struct {
	Cfg      config.Config
	Sess     *session.Manager
	Favor    *favor.Engine
	WebFS    fs.FS // embedded static content (web/ folder, serve at root)
	Tailer   *logtail.Tailer
	Parser   *loot.Parser
	Trader   *trader.Manager
	ItemByID map[int]cdn.Item // item code -> item data
}

// New wires a Server.
func New(cfg config.Config, sess *session.Manager, favor *favor.Engine, webFS fs.FS, tailer *logtail.Tailer, parser *loot.Parser, trader *trader.Manager, items cdn.ItemsFile, ver cdn.Version) *Server {
	// Build item-by-ID map
	itemByID := make(map[int]cdn.Item)
	for _, item := range items {
		itemByID[item.ItemID] = item
	}

	return &Server{
		Cfg:      cfg,
		Sess:     sess,
		Favor:    favor,
		WebFS:    webFS,
		Tailer:   tailer,
		Parser:   parser,
		Trader:   trader,
		ItemByID: itemByID,
	}
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
	mux.HandleFunc("/api/feed", s.handleFeed)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/npcs", s.handleNPCs)
	mux.HandleFunc("/api/traders", s.handleTraders)
	mux.HandleFunc("/", s.handleStatic)
	return mux
}

// handleStatic serves embedded dashboard assets by bare filename; "/" -> "index.html".
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" {
		p = "index.html"
	}
	b, err := fs.ReadFile(s.WebFS, p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType(p))
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}

func contentType(name string) string {
	switch filepath.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

// handleSession: GET current snapshot.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.Sess.Snapshot())
}

// handleStart: POST {dungeon, notes} starts a fresh session.
func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Dungeon string `json:"dungeon"`
		Notes   string `json:"notes"`
	}
	if r.Method == http.MethodPost {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if err := s.Sess.Start(body.Dungeon, body.Notes); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, s.Sess.Snapshot())
}

// handleStop: POST stops the active session and writes a report.
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if err := s.Sess.Stop(s.Cfg.ReportDir); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, s.Sess.Snapshot())
}

// handleLoot: GET current looted-items table.
func (s *Server) handleLoot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.Sess.Snapshot().Loot)
}

// handleFeed: GET Server-Sent Events stream of session events.
func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	events := s.Sess.Events()
	ping := make(chan struct{})
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-t.C:
				ping <- struct{}{}
			}
		}
	}()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping:
			_, _ = w.Write([]byte(":ping\n\n"))
			f.Flush()
		case ev, ok := <-events:
			if !ok {
				return
			}
			b, _ := json.Marshal(ev)
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n\n"))
			f.Flush()
		}
	}
}

// handleConfig: GET current config, or POST/PUT to update it live.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		var req struct {
			ChatLogDir          string             `json:"chat_log_dir"`
			LootRegex           string             `json:"loot_regex"`
			SellValueThreshold  float64            `json:"sell_value_threshold"`
			PlayerPrices        map[string]float64 `json:"player_prices"`
			NotificationThreshold float64          `json:"notification_threshold"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Update the server's config
		s.Cfg.ChatLogDir = req.ChatLogDir
		s.Cfg.LootRegex = req.LootRegex
		s.Cfg.SellValueThreshold = req.SellValueThreshold
		s.Cfg.NotificationThreshold = req.NotificationThreshold
		if req.PlayerPrices != nil {
			s.Cfg.PlayerPrices = req.PlayerPrices
		}

		// Save to disk
		if err := config.Save(s.Cfg); err != nil {
			http.Error(w, "failed to save config: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Update components live!
		if s.Tailer != nil {
			s.Tailer.SetDir(req.ChatLogDir)
		}
		if s.Parser != nil {
			_ = s.Parser.SetRegex(req.LootRegex)
		}
		if s.Favor != nil && req.PlayerPrices != nil {
			s.Favor.SetPlayerPrices(req.PlayerPrices)
		}

		writeJSON(w, map[string]any{"ok": true})
		return
	}

	c := s.Cfg
	writeJSON(w, map[string]any{
		"http_addr":              c.HTTPAddr,
		"chat_log_dir":           c.ChatLogDir,
		"loot_regex":             c.LootRegex,
		"cdn_root":               c.CDNRoot,
		"fallback_version":       c.FallbackVersion,
		"cache_dir":              c.CacheDir,
		"report_dir":             c.ReportDir,
		"sell_value_threshold":   c.SellValueThreshold,
		"player_prices":          c.PlayerPrices,
		"notification_threshold": c.NotificationThreshold,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// handleNPCs: GET list of all NPCs for settings.
func (s *Server) handleNPCs(w http.ResponseWriter, r *http.Request) {
	if s.Favor == nil {
		writeJSON(w, []any{})
		return
	}
	writeJSON(w, s.Favor.NPCList())
}

// SessionSummary represents a brief overview of a past session.
type SessionSummary struct {
	ID            string    `json:"id"`
	Dungeon       string    `json:"dungeon"`
	Notes         string    `json:"notes,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at"`
	DurationSecs  int       `json:"duration_secs"`
	TotalItems    int       `json:"total_items"`
	UniqueItems   int       `json:"unique_items"`
	TotalValue    float64   `json:"total_value"`
	FavorItems    int       `json:"favor_items"`
	SellItems     int       `json:"sell_items"`
	KeepItems     int       `json:"keep_items"`
}

// handleSessions: GET list of all past sessions.
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessions := []SessionSummary{}
	reportDir := s.Cfg.ReportDir

	if reportDir == "" {
		writeJSON(w, sessions)
		return
	}

	files, err := os.ReadDir(reportDir)
	if err != nil {
		writeJSON(w, sessions)
		return
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(reportDir, file.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var snapshot session.Snapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			continue
		}

		summary := SessionSummary{
			ID:           strings.TrimSuffix(file.Name(), ".json"),
			Dungeon:      snapshot.Dungeon,
			Notes:        snapshot.Notes,
			StartedAt:    snapshot.StartedAt,
			EndedAt:      snapshot.EndedAt,
			DurationSecs: int(snapshot.EndedAt.Sub(snapshot.StartedAt).Seconds()),
			UniqueItems:  len(snapshot.Loot),
		}

		totalItems := 0
		totalValue := 0.0
		favorItems := 0
		sellItems := 0
		keepItems := 0

		for _, loot := range snapshot.Loot {
			totalItems += loot.Count
			totalValue += loot.Valor * float64(loot.Count)

			switch loot.Decision.Verdict {
			case "favor":
				favorItems++
			case "sell_vendor", "sell_consignment":
				sellItems++
			case "keep":
				keepItems++
			}
		}

		summary.TotalItems = totalItems
		summary.TotalValue = totalValue
		summary.FavorItems = favorItems
		summary.SellItems = sellItems
		summary.KeepItems = keepItems

		sessions = append(sessions, summary)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})

	writeJSON(w, sessions)
}

// handleSessionByID: GET details of a specific past session, or GET /export for CSV.
func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/session/")
	if strings.HasSuffix(path, "/export") {
		s.handleSessionExport(w, r)
		return
	}

	sessionID := path
	if sessionID == "" {
		http.Error(w, "session ID required", http.StatusBadRequest)
		return
	}

	reportDir := s.Cfg.ReportDir
	if reportDir == "" {
		http.Error(w, "report directory not configured", http.StatusInternalServerError)
		return
	}

	filePath := filepath.Join(reportDir, sessionID+".json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	var snapshot session.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		http.Error(w, "failed to parse session", http.StatusInternalServerError)
		return
	}

	writeJSON(w, snapshot)
}

// handleSessionExport: GET CSV export of a specific past session.
func (s *Server) handleSessionExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/api/session/")
	sessionID = strings.TrimSuffix(sessionID, "/export")
	if sessionID == "" {
		http.Error(w, "session ID required", http.StatusBadRequest)
		return
	}

	reportDir := s.Cfg.ReportDir
	if reportDir == "" {
		http.Error(w, "report directory not configured", http.StatusInternalServerError)
		return
	}

	filePath := filepath.Join(reportDir, sessionID+".json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	var snapshot session.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		http.Error(w, "failed to parse session", http.StatusInternalServerError)
		return
	}

	// Generate CSV
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", sessionID))
	
	writer := csv.NewWriter(w)
	defer writer.Flush()
	
	// Write header
	writer.Write([]string{"name", "item_id", "value", "count", "bonus", "first_seen", "last_seen", "verdict", "sell_reason", "player_price"})
	
	// Write data rows
	for _, loot := range snapshot.Loot {
		writer.Write([]string{
			loot.Name,
			strconv.Itoa(loot.ItemID),
			strconv.FormatFloat(loot.Valor, 'f', -1, 64),
			strconv.Itoa(loot.Count),
			strconv.FormatBool(loot.Bonus),
			loot.FirstSeen.Format(time.RFC3339),
			loot.LastSeen.Format(time.RFC3339),
			string(loot.Decision.Verdict),
			loot.Decision.SellReason,
			strconv.FormatFloat(loot.Decision.PlayerPrice, 'f', -1, 64),
		})
	}
}

// handleTraders: GET/POST trader management.
func (s *Server) handleTraders(w http.ResponseWriter, r *http.Request) {
	if s.Trader == nil {
		writeJSON(w, []any{})
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Get all barter NPCs from CDN, grouped by area, with tracked data
		allTraders := s.getAllBarterTraders()
		writeJSON(w, allTraders)

	case http.MethodPost:
		// Update trader settings or log sale
		var req struct {
			NPCName     string   `json:"npc_name"`
			Area        string   `json:"area"`
			WeeklyLimit *float64 `json:"weekly_limit,omitempty"`
			Amount      *float64 `json:"amount,omitempty"`
			Sold        *float64 `json:"sold,omitempty"`
			ResetDays   *int     `json:"reset_days,omitempty"`
			ResetHours  *int     `json:"reset_hours,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Ensure trader exists (also updates reset settings)
		rd, rh := 5, 22
		if req.ResetDays != nil {
			rd = *req.ResetDays
		}
		if req.ResetHours != nil {
			rh = *req.ResetHours
		}
		if err := s.Trader.Ensure(req.NPCName, req.Area, rd, rh); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Update limit if provided
		if req.WeeklyLimit != nil {
			if err := s.Trader.UpdateLimit(req.NPCName, *req.WeeklyLimit); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		// Set sold amount if provided
		if req.Sold != nil {
			if err := s.Trader.SetSold(req.NPCName, *req.Sold); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else if req.Amount != nil && *req.Amount > 0 {
			// Log sale (additive)
			if err := s.Trader.LogSale(req.NPCName, *req.Amount); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if err := s.Trader.Save(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// getAllBarterTraders returns all NPCs with Store or Consignment service, grouped by area
func (s *Server) getAllBarterTraders() []map[string]interface{} {
	// Get all NPCs from favor engine
	npcs := s.Favor.GetNPCs()
	
	// Group by area
	areaMap := make(map[string][]map[string]interface{})
	
	for _, npc := range npcs {
		// Check if NPC has Store or Consignment service (buyers)
		hasService := false
		for _, svc := range npc.Services {
			if svc.Type == "Store" || svc.Type == "Consignment" {
				hasService = true
				break
			}
		}
		
		if !hasService {
			continue
		}
		
		// Get tracked data if exists
		tracked := s.Trader.Get(npc.Name)
		
		npcData := map[string]interface{}{
			"npc_name":   npc.Name,
			"area":       npc.AreaFriendly,
			"has_barter": true,
		}
		
		if tracked != nil {
			duration := s.Trader.TimeUntilReset(npc.Name)
			unusedCapacity := tracked.WeeklyLimit - tracked.SoldThisWeek
			unusedWarning := unusedCapacity > (tracked.WeeklyLimit * 0.5)
			
			npcData["weekly_limit"] = tracked.WeeklyLimit
			npcData["sold_this_week"] = tracked.SoldThisWeek
			npcData["reset_days"] = tracked.ResetDays
			npcData["reset_hours"] = tracked.ResetHours
			npcData["time_until_reset"] = trader.FormatDuration(duration)
			npcData["unused_warning"] = unusedWarning
		} else {
			npcData["weekly_limit"] = 0
			npcData["sold_this_week"] = 0
			npcData["reset_days"] = 5
			npcData["reset_hours"] = 22
			npcData["time_until_reset"] = "5d 22h"
			npcData["unused_warning"] = false
		}
		
		areaMap[npc.AreaFriendly] = append(areaMap[npc.AreaFriendly], npcData)
	}
	
	// Convert to sorted slice
	var result []map[string]interface{}
	for area, npcs := range areaMap {
		result = append(result, map[string]interface{}{
			"area":  area,
			"npcs":  npcs,
			"count": len(npcs),
		})
	}
	
	return result
}