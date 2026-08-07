package server

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
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
	w.Header().Set("X-Content-Type-Options", "nosniff")
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
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

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

// configPayload renders the full config as the JSON object both GET and POST return.
func configPayload(c config.Config) map[string]any {
	return map[string]any{
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
		"overlay":                c.Overlay,
		"session_templates":      c.SessionTemplates,
	}
}

// configPatch is the subset of config fields a POST/PUT /api/config may update.
type configPatch struct {
	ChatLogDir            *string             `json:"chat_log_dir"`
	PlayerLogPath         *string             `json:"player_log_path"`
	LootRegex             *string             `json:"loot_regex"`
	SellValueThreshold    *float64            `json:"sell_value_threshold"`
	PlayerPrices          *map[string]float64 `json:"player_prices"`
	NotificationThreshold *float64            `json:"notification_threshold"`
	BackupEnabled         *bool                   `json:"backup_enabled"`
	Overlay               *config.OverlaySettings `json:"overlay"`
	SessionTemplates      *[]config.SessionTemplate `json:"session_templates"`
}

// applyConfigPatch overlays non-nil patch fields onto cfg (partial merge).
func applyConfigPatch(cfg config.Config, p configPatch) config.Config {
	if p.ChatLogDir != nil {
		cfg.ChatLogDir = *p.ChatLogDir
	}
	if p.PlayerLogPath != nil {
		cfg.PlayerLogPath = *p.PlayerLogPath
	}
	if p.LootRegex != nil {
		cfg.LootRegex = *p.LootRegex
	}
	if p.SellValueThreshold != nil {
		cfg.SellValueThreshold = *p.SellValueThreshold
	}
	if p.NotificationThreshold != nil {
		cfg.NotificationThreshold = *p.NotificationThreshold
	}
	if p.BackupEnabled != nil {
		cfg.BackupEnabled = *p.BackupEnabled
	}
	if p.PlayerPrices != nil {
		cfg.PlayerPrices = *p.PlayerPrices
	}
	if p.Overlay != nil {
		cfg.Overlay = *p.Overlay
	}
	if p.SessionTemplates != nil {
		cfg.SessionTemplates = *p.SessionTemplates
	}
	return cfg
}

// handleConfig: GET current config, or POST/PUT to update it live.
// POST/PUT merges the submitted fields over the existing config — unset fields
// keep their current values — and responds with the full merged config.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		var req configPatch
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

		s.cfgMu.Lock()
		next := applyConfigPatch(s.Cfg, req)
		if err := config.Save(next); err != nil {
			s.cfgMu.Unlock()
			http.Error(w, "failed to save config: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.Cfg = next
		s.cfgMu.Unlock()

		// Update components live. next is the freshly stored config, so use it
		// directly instead of re-reading s.Cfg under another lock.
		if req.ChatLogDir != nil && s.Tailer != nil {
			s.Tailer.SetDir(next.ChatLogDir)
		}
		if req.PlayerLogPath != nil && s.PLTailer != nil {
			s.PLTailer.SetPath(next.PlayerLogPath)
		}
		if req.LootRegex != nil && s.Parser != nil {
			if err := s.Parser.SetRegex(next.LootRegex); err != nil {
				http.Error(w, "failed to apply loot_regex: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if req.PlayerPrices != nil && s.Favor != nil {
			s.Favor.SetPlayerPrices(next.PlayerPrices)
		}

		writeJSON(w, configPayload(next))
		return
	}

	s.cfgMu.RLock()
	cfg := s.Cfg
	s.cfgMu.RUnlock()
	writeJSON(w, configPayload(cfg))
}

// handleOverlay serves the embedded overlay.html (added by the frontend lane).
func (s *Server) handleOverlay(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(s.WebFS, "overlay.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType("overlay.html"))
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}

// handleOverlaySpawn: POST starts the native HUD overlay as a detached process
// so it survives independently and doesn't inherit the server's console.
func (s *Server) handleOverlaySpawn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s.cfgMu.RLock()
	addr := s.Cfg.HTTPAddr
	s.cfgMu.RUnlock()
	cmd := exec.Command(exe, "--overlay", "-addr", addr)
	cmd.SysProcAttr = detachedProcAttr()
	if err := cmd.Start(); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// NaN/Inf or other encode failures previously produced a silent
		// 200 + empty body that crashed the client's .json() — surface them.
		log.Printf("writeJSON: encode failed: %v", err)
	}
}
