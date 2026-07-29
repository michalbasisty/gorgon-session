package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/michalbasisty/gorgon-session/internal/cdn"
	"github.com/michalbasisty/gorgon-session/internal/config"
	"github.com/michalbasisty/gorgon-session/internal/session"
	"github.com/michalbasisty/gorgon-session/internal/trader"
)

// handleTraders: GET/POST trader management.
func (s *Server) handleTraders(w http.ResponseWriter, r *http.Request) {
	if s.Trader == nil {
		writeJSON(w, []any{})
		return
	}

	switch r.Method {
	case http.MethodGet:
		allTraders := s.getAllBarterTraders()
		writeJSON(w, allTraders)

	case http.MethodPost:
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

		if req.WeeklyLimit != nil {
			if err := s.Trader.UpdateLimit(req.NPCName, *req.WeeklyLimit); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if req.Sold != nil {
			if err := s.Trader.SetSold(req.NPCName, *req.Sold); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else if req.Amount != nil && *req.Amount > 0 {
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

// TraderInfo is one trader row within an area group.
type TraderInfo struct {
	NPCName        string  `json:"npc_name"`
	HasBarter      bool    `json:"has_barter"`
	WeeklyLimit    float64 `json:"weekly_limit"`
	SoldThisWeek   float64 `json:"sold_this_week"`
	ResetDays      int     `json:"reset_days"`
	ResetHours     int     `json:"reset_hours"`
	TimeUntilReset string  `json:"time_until_reset"`
	UnusedWarning  bool    `json:"unused_warning"`
}

// getAllBarterTraders returns all NPCs with Store or Consignment service, grouped by area
func (s *Server) getAllBarterTraders() []AreaTraders {
	npcs := s.Favor.GetNPCs()

	areaMap := make(map[string][]TraderInfo)

	for _, npc := range npcs {
		hasService := false
		for _, svc := range npc.Services {
			t := strings.ToLower(strings.TrimSpace(svc.Type))
			if t == "store" || t == "consignment" || t == "barter" {
				hasService = true
				break
			}
		}
		if !hasService {
			continue
		}

		tracked := s.Trader.Get(npc.Name)
		info := TraderInfo{NPCName: npc.Name, HasBarter: true}

		if tracked != nil {
			duration := s.Trader.TimeUntilReset(npc.Name)
			unusedCapacity := tracked.WeeklyLimit - tracked.SoldThisWeek
			info.WeeklyLimit = tracked.WeeklyLimit
			info.SoldThisWeek = tracked.SoldThisWeek
			info.ResetDays = tracked.ResetDays
			info.ResetHours = tracked.ResetHours
			info.TimeUntilReset = trader.FormatDuration(duration)
			info.UnusedWarning = unusedCapacity > (tracked.WeeklyLimit * 0.5)
		} else {
			info.TimeUntilReset = "5d 22h"
		}

		areaMap[npc.AreaFriendly] = append(areaMap[npc.AreaFriendly], info)
	}

	var result []AreaTraders
	for area, npcs := range areaMap {
		result = append(result, AreaTraders{Area: area, NPCs: npcs, Count: len(npcs)})
	}
	return result
}

// AreaTraders groups NPCs by their in-game area for the frontend.
type AreaTraders struct {
	Area  string       `json:"area"`
	NPCs  []TraderInfo `json:"npcs"`
	Count int          `json:"count"`
}

// ItemByName does a case-insensitive name lookup across all CDN items.
// Returns a synthetic item with just Name populated if not found.
func (s *Server) ItemByName(name string) struct {
	ItemName string
	Item     cdn.Item
} {
	key := strings.ToLower(strings.TrimSpace(name))
	if it, ok := s.itemByName[key]; ok {
		return struct {
			ItemName string
			Item     cdn.Item
		}{it.Name, it}
	}
	return struct {
		ItemName string
		Item     cdn.Item
	}{name, cdn.Item{Name: name}}
}

// handleBulkExport: POST a list of session IDs, returns a downloadable JSON bundle.
func (s *Server) handleBulkExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.IDs) == 0 {
		http.Error(w, "no session IDs provided", http.StatusBadRequest)
		return
	}
	type sessExport struct {
		ID      string           `json:"id"`
		Session session.Snapshot `json:"session"`
	}
	var sessions []sessExport
	reportDir := s.Cfg.ReportDir
	for _, id := range req.IDs {
		if !validSessionID(id) {
			http.Error(w, "invalid session ID in request", http.StatusBadRequest)
			return
		}
		filePath := filepath.Join(reportDir, id+".json")
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		var snap session.Snapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			continue
		}
		sessions = append(sessions, sessExport{ID: id, Session: snap})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=gorgon-sessions-export-%d.json", len(sessions)))
	json.NewEncoder(w).Encode(map[string]any{"sessions": sessions})
}

// handleExport: GET all config + settings as a downloadable JSON bundle.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	export := map[string]any{
		"config":      s.Cfg,
		"version":     "1",
		"exported_at": time.Now().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=gorgon-session-export.json")
	json.NewEncoder(w).Encode(export)
}

// handleImport: POST a JSON bundle to restore config + settings.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Config config.Config `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid import data: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.Cfg = body.Config
	if err := config.Save(s.Cfg); err != nil {
		http.Error(w, "failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if s.Favor != nil {
		s.Favor.SetPlayerPrices(s.Cfg.PlayerPrices)
	}
	writeJSON(w, map[string]any{"ok": true, "message": "Settings imported successfully"})
}
