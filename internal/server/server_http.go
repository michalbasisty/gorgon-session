package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/michalbasisty/gorgon-session/internal/config"
)

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

	events, unsubscribe := s.Sess.Subscribe()
	defer unsubscribe()

	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-pingTicker.C:
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
			ChatLogDir            *string             `json:"chat_log_dir"`
			PlayerLogPath         *string             `json:"player_log_path"`
			LootRegex             *string             `json:"loot_regex"`
			SellValueThreshold    *float64            `json:"sell_value_threshold"`
			PlayerPrices          *map[string]float64 `json:"player_prices"`
			NotificationThreshold *float64            `json:"notification_threshold"`
			BackupEnabled         *bool               `json:"backup_enabled"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.LootRegex != nil && *req.LootRegex != "" {
			if _, err := regexp.Compile(*req.LootRegex); err != nil {
				http.Error(w, "invalid loot_regex: "+err.Error(), http.StatusBadRequest)
				return
			}
		}

		next := s.Cfg
		if req.ChatLogDir != nil {
			next.ChatLogDir = *req.ChatLogDir
		}
		if req.PlayerLogPath != nil {
			next.PlayerLogPath = *req.PlayerLogPath
		}
		if req.LootRegex != nil {
			next.LootRegex = *req.LootRegex
		}
		if req.SellValueThreshold != nil {
			next.SellValueThreshold = *req.SellValueThreshold
		}
		if req.NotificationThreshold != nil {
			next.NotificationThreshold = *req.NotificationThreshold
		}
		if req.BackupEnabled != nil {
			next.BackupEnabled = *req.BackupEnabled
		}
		if req.PlayerPrices != nil {
			next.PlayerPrices = *req.PlayerPrices
		}

		if err := config.Save(next); err != nil {
			http.Error(w, "failed to save config: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.Cfg = next

		// Update components live.
		if req.ChatLogDir != nil && s.Tailer != nil {
			s.Tailer.SetDir(s.Cfg.ChatLogDir)
		}
		if req.PlayerLogPath != nil && s.PLTailer != nil {
			s.PLTailer.SetPath(s.Cfg.PlayerLogPath)
		}
		if req.LootRegex != nil && s.Parser != nil {
			if err := s.Parser.SetRegex(s.Cfg.LootRegex); err != nil {
				http.Error(w, "failed to apply loot_regex: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if req.PlayerPrices != nil && s.Favor != nil {
			s.Favor.SetPlayerPrices(s.Cfg.PlayerPrices)
		}

		writeJSON(w, map[string]any{"ok": true})
		return
	}

	c := s.Cfg
	writeJSON(w, map[string]any{
		"http_addr":              c.HTTPAddr,
		"chat_log_dir":           c.ChatLogDir,
		"player_log_path":        c.PlayerLogPath,
		"loot_regex":             c.LootRegex,
		"cdn_root":               c.CDNRoot,
		"fallback_version":       c.FallbackVersion,
		"cache_dir":              c.CacheDir,
		"report_dir":             c.ReportDir,
		"sell_value_threshold":   c.SellValueThreshold,
		"player_prices":          c.PlayerPrices,
		"notification_threshold": c.NotificationThreshold,
		"backup_enabled":         c.BackupEnabled,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
