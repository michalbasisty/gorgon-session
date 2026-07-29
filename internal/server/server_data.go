package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/michalbasisty/gorgon-session/internal/cdn"
	"github.com/michalbasisty/gorgon-session/internal/favor"
	"github.com/michalbasisty/gorgon-session/internal/session"
)

// handleAreas returns areas as internal key -> friendly name.
func (s *Server) handleAreas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	out := make(map[string]string, len(s.Areas.ByInternal))
	for k, a := range s.Areas.ByInternal {
		out[k] = a.FriendlyName
	}
	writeJSON(w, out)
}

func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.Skills)
}

func (s *Server) handleRecipes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.Recipes)
}

func (s *Server) handleItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var items []cdn.Item
	for _, item := range s.ItemByID {
		items = append(items, item)
	}
	writeJSON(w, items)
}

// handlePrices: GET price summary for all items.
func (s *Server) handlePrices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.Prices == nil {
		writeJSON(w, map[string]any{})
		return
	}
	writeJSON(w, s.Prices.Summarize())
}

// handlePriceByName: GET price entries for a specific item.
func (s *Server) handlePriceByName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/prices/")
	if name == "" {
		http.Error(w, "item name required", http.StatusBadRequest)
		return
	}
	if s.Prices == nil {
		writeJSON(w, []any{})
		return
	}
	writeJSON(w, s.Prices.Get(name))
}

// handleCombat: GET combat stats for the active session.
func (s *Server) handleCombat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snap := s.Sess.Snapshot()
	type abilityStat struct {
		Name       string  `json:"name"`
		Uses       int     `json:"uses"`
		Hits       int     `json:"hits,omitempty"`
		Skill      string  `json:"skill,omitempty"`
		DamageType string  `json:"damage_type,omitempty"`
		BaseDamage float64 `json:"base_damage,omitempty"`
		EstDPS     float64 `json:"est_dps,omitempty"`
	}
	stats := make([]abilityStat, 0)
	duration := snap.EndedAt.Sub(snap.StartedAt).Seconds()
	if duration <= 0 && snap.State == session.Running {
		duration = time.Since(snap.StartedAt).Seconds()
	}
	for name, uses := range snap.AbilityCounts {
		hits := snap.HitCounts[name]
		stat := abilityStat{Name: name, Uses: uses, Hits: hits}
		if a, ok := s.Abilities[name]; ok {
			stat.Skill = a.Skill
			stat.DamageType = a.DamageType
			stat.BaseDamage = a.BaseDamage
			if duration > 0 {
				stat.EstDPS = (a.BaseDamage * float64(hits)) / duration
			}
		} else {
			for _, a := range s.Abilities {
				if strings.EqualFold(a.Name, name) {
					stat.Skill = a.Skill
					stat.DamageType = a.DamageType
					stat.BaseDamage = a.BaseDamage
					if duration > 0 {
						stat.EstDPS = (a.BaseDamage * float64(hits)) / duration
					}
					break
				}
			}
		}
		stats = append(stats, stat)
	}
	writeJSON(w, stats)
}

// handleZoneNPCs: GET NPCs near the current zone (for sell/favor suggestions).
func (s *Server) handleZoneNPCs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snap := s.Sess.Snapshot()
	currentZone := snap.Zone
	if currentZone == "" {
		writeJSON(w, []any{})
		return
	}
	zoneLower := strings.ToLower(strings.TrimSpace(currentZone))
	npcs := s.Favor.NPCList()
	var nearby []favor.NPCInfo
	for _, n := range npcs {
		if strings.ToLower(strings.TrimSpace(n.Area)) == zoneLower {
			nearby = append(nearby, n)
		}
	}
	if nearby == nil {
		nearby = []favor.NPCInfo{}
	}
	writeJSON(w, nearby)
}

// handleDropRates: GET aggregated drop rates across all past sessions.
func (s *Server) handleDropRates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	reportDir := s.Cfg.ReportDir
	if reportDir == "" {
		writeJSON(w, []any{})
		return
	}
	files, err := os.ReadDir(reportDir)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	type dropStat struct {
		Name          string  `json:"name"`
		TotalCount    int     `json:"total_count"`
		SessionCount  int     `json:"session_count"`
		AvgPerSession float64 `json:"avg_per_session"`
		AvgValue      float64 `json:"avg_value"`
	}
	agg := map[string]*dropStat{}
	sessionCount := 0
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(reportDir, file.Name()))
		if err != nil {
			continue
		}
		var snap session.Snapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			continue
		}
		sessionCount++
		seen := map[string]bool{}
		for _, loot := range snap.Loot {
			seen[loot.Name] = true
			if d, ok := agg[loot.Name]; ok {
				d.TotalCount += loot.Count
				d.AvgValue += loot.Valor * float64(loot.Count)
			} else {
				agg[loot.Name] = &dropStat{
					Name:       loot.Name,
					TotalCount: loot.Count,
					AvgValue:   loot.Valor * float64(loot.Count),
				}
			}
		}
		for name := range seen {
			agg[name].SessionCount++
		}
	}
	result := make([]dropStat, 0, len(agg))
	for _, d := range agg {
		d.AvgPerSession = float64(d.TotalCount) / float64(sessionCount)
		if d.TotalCount > 0 {
			d.AvgValue = d.AvgValue / float64(d.TotalCount)
		}
		result = append(result, *d)
	}
	if result == nil {
		result = []dropStat{}
	}
	writeJSON(w, result)
}
