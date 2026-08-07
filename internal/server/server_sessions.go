package server

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/michalbasisty/gorgon-session/internal/favor"
	"github.com/michalbasisty/gorgon-session/internal/session"
)

// handleSession: GET current snapshot.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		snap := s.Sess.Snapshot()
		s.enrichFavorDistances(&snap)
		writeJSON(w, snap)
	case http.MethodPatch:
		var body struct {
			Notes string `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.Sess.SetNotes(body.Notes)
		writeJSON(w, s.Sess.Snapshot())
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// enrichFavorDistances attaches the distance from the player's current zone to
// each favor target on the RESPONSE copy only. New slices are built so live
// session state is never mutated (Snapshot copies LootEntry structs but shares
// the FavorTargets backing arrays).
func (s *Server) enrichFavorDistances(snap *session.Snapshot) {
	for i := range snap.Loot {
		d := &snap.Loot[i].Decision
		if len(d.FavorTargets) == 0 {
			continue
		}
		targets := make([]favor.Target, len(d.FavorTargets))
		copy(targets, d.FavorTargets)
		for j := range targets {
			if dist, ok := s.areaDistance(snap.Zone, targets[j].Area); ok {
				targets[j].DistanceKm = &dist
			}
		}
		d.FavorTargets = targets
	}
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
	s.cfgMu.RLock()
	reportDir := s.Cfg.ReportDir
	s.cfgMu.RUnlock()
	if err := s.Sess.Stop(reportDir); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Record price history from the stopped session
	snap := s.Sess.Snapshot()
	if s.Prices != nil {
		batch := map[string]struct{ Price, Count float64 }{}
		for _, loot := range snap.Loot {
			batch[loot.Name] = struct{ Price, Count float64 }{loot.Valor, float64(loot.Count)}
		}
		s.Prices.AddBatch(batch)
		_ = s.Prices.Save()
	}
	// Invalidate session list cache
	s.sessionsMu.Lock()
	s.sessionsCached = false
	s.sessionsMu.Unlock()
	writeJSON(w, snap)
}

// handleLoot: GET current loot, POST manual entry, DELETE remove entry.
func (s *Server) handleLoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.Sess.Snapshot().Loot)
	case http.MethodPost:
		var body struct {
			Name  string  `json:"name"`
			Value float64 `json:"value"`
			Count int     `json:"count"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.Name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		if body.Count <= 0 {
			body.Count = 1
		}
		// resolve through favor engine if possible
		hit := s.ItemByName(body.Name)
		dec := s.Favor.ResolveItem(hit.Item)
		entry := session.LootEntry{
			Name:      hit.ItemName,
			ItemID:    hit.Item.ItemID,
			Valor:     body.Value,
			Count:     body.Count,
			FirstSeen: time.Now(),
			LastSeen:  time.Now(),
			Decision:  dec,
		}
		if body.Value > 0 {
			entry.Valor = body.Value
		}
		s.Sess.AddLoot(entry)
		writeJSON(w, entry)
	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name query param required", http.StatusBadRequest)
			return
		}
		if s.Sess.RemoveLoot(name) {
			writeJSON(w, map[string]any{"ok": true})
		} else {
			http.Error(w, "item not found", http.StatusNotFound)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleLootNote: PATCH to update a loot entry note.
func (s *Server) handleLootNote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "use PATCH", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Name string `json:"name"`
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if s.Sess.SetLootNote(body.Name, body.Note) {
		writeJSON(w, map[string]any{"ok": true})
	} else {
		http.Error(w, "item not found or session not running", http.StatusNotFound)
	}
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
	ID           string    `json:"id"`
	Dungeon      string    `json:"dungeon"`
	Notes        string    `json:"notes,omitempty"`
	Tags         []string  `json:"tags,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at"`
	DurationSecs int       `json:"duration_secs"`
	TotalItems   int       `json:"total_items"`
	UniqueItems  int       `json:"unique_items"`
	TotalValue   float64   `json:"total_value"`
	FavorItems   int       `json:"favor_items"`
	SellItems    int       `json:"sell_items"`
	KeepItems    int       `json:"keep_items"`
	TotalGold    int       `json:"total_gold,omitempty"`
	Deaths       int       `json:"deaths,omitempty"`
}

var sessionIDPattern = regexp.MustCompile(`^session-\d{8}-\d{6}$`)

func validSessionID(id string) bool {
	return sessionIDPattern.MatchString(id)
}

// handleSessions: GET list of all past sessions.
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.sessionsMu.RLock()
	cached := s.sessionsCached
	summary := s.sessionsCache
	s.sessionsMu.RUnlock()

	if cached {
		writeJSON(w, summary)
		return
	}

	summary = s.buildSessions()

	s.sessionsMu.Lock()
	s.sessionsCache = summary
	s.sessionsCached = true
	s.sessionsMu.Unlock()

	writeJSON(w, summary)
}

func (s *Server) buildSessions() []SessionSummary {
	sessions := []SessionSummary{}
	s.cfgMu.RLock()
	reportDir := s.Cfg.ReportDir
	s.cfgMu.RUnlock()

	if reportDir == "" {
		return sessions
	}

	files, err := os.ReadDir(reportDir)
	if err != nil {
		return sessions
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
			Tags:         snapshot.Tags,
			StartedAt:    snapshot.StartedAt,
			EndedAt:      snapshot.EndedAt,
			DurationSecs: int(snapshot.EndedAt.Sub(snapshot.StartedAt).Seconds()),
			UniqueItems:  len(snapshot.Loot),
			TotalGold:    snapshot.TotalGold,
			Deaths:       len(snapshot.Deaths),
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

	return sessions
}

// handleSessionByID: GET details of a specific past session, or GET /export for CSV.
func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/session/")
	if strings.HasSuffix(path, "/export") {
		s.handleSessionExport(w, r)
		return
	}
	if strings.HasSuffix(path, "/zones") {
		s.handleSessionZones(w, r)
		return
	}

	sessionID := path
	if sessionID == "" {
		http.Error(w, "session ID required", http.StatusBadRequest)
		return
	}
	if !validSessionID(sessionID) {
		http.Error(w, "invalid session ID", http.StatusBadRequest)
		return
	}

	s.cfgMu.RLock()
	reportDir := s.Cfg.ReportDir
	s.cfgMu.RUnlock()
	if reportDir == "" {
		http.Error(w, "report directory not configured", http.StatusInternalServerError)
		return
	}

	filePath := filepath.Join(reportDir, sessionID+".json")

	switch r.Method {
	case http.MethodGet:
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

	case http.MethodPatch:
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
		var body struct {
			Notes string   `json:"notes"`
			Tags  []string `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		snapshot.Notes = body.Notes
		snapshot.Tags = body.Tags
		out, _ := json.MarshalIndent(snapshot, "", "  ")
		if err := os.WriteFile(filePath, out, 0644); err != nil {
			http.Error(w, "failed to save", http.StatusInternalServerError)
			return
		}
		s.sessionsMu.Lock()
		s.sessionsCached = false
		s.sessionsMu.Unlock()
		writeJSON(w, snapshot)

	case http.MethodDelete:
		if err := os.Remove(filePath); err != nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		s.sessionsMu.Lock()
		s.sessionsCached = false
		s.sessionsMu.Unlock()
		writeJSON(w, map[string]any{"ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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
	if !validSessionID(sessionID) {
		http.Error(w, "invalid session ID", http.StatusBadRequest)
		return
	}

	s.cfgMu.RLock()
	reportDir := s.Cfg.ReportDir
	s.cfgMu.RUnlock()
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

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", sessionID))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	writer.Write([]string{"name", "item_id", "value", "count", "bonus", "first_seen", "last_seen", "verdict", "sell_reason", "player_price"})

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

// handleNotesExport: GET text/plain dump of every loot note across sessions,
// grouped under a session date/time header per report.
func (s *Server) handleNotesExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var b strings.Builder
	s.cfgMu.RLock()
	reportDir := s.Cfg.ReportDir
	s.cfgMu.RUnlock()
	if reportDir != "" {
		files, err := os.ReadDir(reportDir)
		if err == nil {
			for _, file := range files {
				if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
					continue
				}
				snap, ok := s.sessionSnapshot(strings.TrimSuffix(file.Name(), ".json"), w)
				if !ok {
					// skip unreadable reports rather than failing the whole dump
					continue
				}
				header := fmt.Sprintf("=== %s (%s) ===\n", strings.TrimSuffix(file.Name(), ".json"), snap.StartedAt.Format(time.RFC3339))
				wrote := false
				for _, loot := range snap.Loot {
					if strings.TrimSpace(loot.Note) == "" {
						continue
					}
					if !wrote {
						b.WriteString(header)
						wrote = true
					}
					b.WriteString(fmt.Sprintf("%s: %s\n", loot.Name, loot.Note))
				}
			}
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

// CompareSummary is one side of a session comparison.
type CompareSummary struct {
	ID              string    `json:"id"`
	StartedAt       time.Time `json:"started_at"`
	DurationSeconds float64   `json:"duration_seconds"`
	TotalLootValue  float64   `json:"total_loot_value"`
	Kills           int       `json:"kills"`
	XP              int       `json:"xp"`
}

func (s *Server) summarizeSnapshot(id string, snap session.Snapshot) CompareSummary {
	sum := CompareSummary{ID: id, StartedAt: snap.StartedAt}
	if !snap.StartedAt.IsZero() && !snap.EndedAt.IsZero() {
		sum.DurationSeconds = snap.EndedAt.Sub(snap.StartedAt).Seconds()
	}
	for _, loot := range snap.Loot {
		sum.TotalLootValue += loot.Valor * float64(loot.Count)
	}
	sum.Kills = len(snap.Kills)
	for _, xp := range snap.XPGains {
		sum.XP += xp.Amount
	}
	return sum
}

// handleSessionsCompare: GET ?a=ID&b=ID compares two past sessions.
func (s *Server) handleSessionsCompare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	aID, bID := r.URL.Query().Get("a"), r.URL.Query().Get("b")
	if aID == "" || bID == "" {
		http.Error(w, "a and b session IDs required", http.StatusBadRequest)
		return
	}
	snapA, ok := s.sessionSnapshot(aID, w)
	if !ok {
		return
	}
	snapB, ok := s.sessionSnapshot(bID, w)
	if !ok {
		return
	}
	a := s.summarizeSnapshot(aID, snapA)
	b := s.summarizeSnapshot(bID, snapB)
	writeJSON(w, map[string]any{
		"a": a, "b": b,
		"diff": map[string]float64{
			"loot_value": a.TotalLootValue - b.TotalLootValue,
			"kills":      float64(a.Kills - b.Kills),
			"xp":         float64(a.XP - b.XP),
		},
	})
}

// zoneStat is one row of the per-zone performance summary.
type zoneStat struct {
	Zone         string  `json:"zone"`
	Seconds      float64 `json:"seconds"`
	LootCount    int     `json:"loot_count"`
	LootValue    float64 `json:"loot_value"`
	Kills        int     `json:"kills"`
	Deaths       int     `json:"deaths"`
	XP           int     `json:"xp"`
	ValuePerHour float64 `json:"value_per_hour"`
	KillsPerHour float64 `json:"kills_per_hour"`
}

// handleSessionZones: GET per-zone performance summary for a past session.
// Route is /api/session/{id}/zones, dispatched from handleSessionByID.
func (s *Server) handleSessionZones(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID := strings.TrimPrefix(r.URL.Path, "/api/session/")
	sessionID = strings.TrimSuffix(sessionID, "/zones")
	if sessionID == "" {
		http.Error(w, "session ID required", http.StatusBadRequest)
		return
	}
	if !validSessionID(sessionID) {
		http.Error(w, "invalid session ID", http.StatusBadRequest)
		return
	}
	s.cfgMu.RLock()
	reportDir := s.Cfg.ReportDir
	s.cfgMu.RUnlock()
	if reportDir == "" {
		http.Error(w, "report directory not configured", http.StatusInternalServerError)
		return
	}
	data, err := os.ReadFile(filepath.Join(reportDir, sessionID+".json"))
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	var snap session.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		http.Error(w, "failed to parse session", http.StatusInternalServerError)
		return
	}
	writeJSON(w, zoneSummary(snap))
}

// zoneSummary aggregates loot/kills/deaths/XP per zone for one session report.
// Zones are ordered by first appearance in zone_history. Zone i covers
// [zone_history[i].Time, zone_history[i+1].Time), with the first zone starting
// at StartedAt and the last ending at EndedAt. Events before the first
// zone_history entry count toward the first zone. With no zone_history, a
// single "" zone spans the whole session.
func zoneSummary(snap session.Snapshot) []zoneStat {
	zh := snap.ZoneHistory
	endedAt := snap.EndedAt
	if endedAt.IsZero() {
		endedAt = snap.StartedAt.Add(time.Minute)
	}

	// zoneAt returns the index of the last zone_history entry at-or-before t.
	// Pre-first events (and the no-history case) land on index 0.
	zoneAt := func(t time.Time) int {
		if len(zh) == 0 {
			return 0
		}
		if t.IsZero() {
			return -1
		}
		idx := 0
		for idx+1 < len(zh) && !zh[idx+1].Time.After(t) {
			idx++
		}
		return idx
	}

	stats := make([]zoneStat, 0, len(zh)+1)
	for _, z := range zh {
		stats = append(stats, zoneStat{Zone: z.Zone})
	}
	if len(stats) == 0 {
		stats = append(stats, zoneStat{})
	}

	for i := range stats {
		start := snap.StartedAt
		if i > 0 {
			start = zh[i].Time
		}
		end := endedAt
		if i+1 < len(stats) {
			end = zh[i+1].Time
		}
		if secs := end.Sub(start).Seconds(); secs > 0 {
			stats[i].Seconds = secs
		}
	}

	for _, le := range snap.LootEvents {
		if le.Time.IsZero() {
			continue
		}
		if idx := zoneAt(le.Time); idx >= 0 {
			stats[idx].LootCount += le.Count
			stats[idx].LootValue += le.Value * float64(le.Count)
		}
	}
	for _, k := range snap.Kills {
		if idx := zoneAt(k.Time); idx >= 0 {
			stats[idx].Kills++
		}
	}
	for _, d := range snap.Deaths {
		if idx := zoneAt(d.Time); idx >= 0 {
			stats[idx].Deaths++
		}
	}
	for _, x := range snap.XPGains {
		if idx := zoneAt(x.Time); idx >= 0 {
			stats[idx].XP += x.Amount
		}
	}

	for i := range stats {
		if stats[i].Seconds > 0 {
			stats[i].ValuePerHour = round1(stats[i].LootValue / (stats[i].Seconds / 3600))
			stats[i].KillsPerHour = round1(float64(stats[i].Kills) / (stats[i].Seconds / 3600))
		}
	}
	return stats
}
