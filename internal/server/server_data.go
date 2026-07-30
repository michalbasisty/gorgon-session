package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
		AbilityID  int     `json:"ability_id,omitempty"`
		Uses       int     `json:"uses"`
		Hits       int     `json:"hits,omitempty"`
		Evades     int     `json:"evades,omitempty"`
		Skill      string  `json:"skill,omitempty"`
		DamageType string  `json:"damage_type,omitempty"`
		BaseDamage float64 `json:"base_damage,omitempty"`
		EstDamage  float64 `json:"est_damage,omitempty"`
		EstDPS     float64 `json:"est_dps,omitempty"`
	}

	duration := snap.EndedAt.Sub(snap.StartedAt).Seconds()
	if duration <= 0 && snap.State == session.Running {
		duration = time.Since(snap.StartedAt).Seconds()
	}

	stats := make(map[string]*abilityStat)

	resolve := func(name string, id int) (cdn.Ability, bool) {
		if id > 0 {
			if a, ok := s.abilityByID[id]; ok {
				return a, true
			}
		}
		k := strings.ToLower(strings.TrimSpace(name))
		if k == "" {
			return cdn.Ability{}, false
		}
		a, ok := s.abilityByNameKey[k]
		return a, ok
	}

	keyFor := func(name string, id int) string {
		if id > 0 {
			return "id:" + strconv.Itoa(id)
		}
		return "name:" + strings.ToLower(strings.TrimSpace(name))
	}

	for id, uses := range snap.AbilityIDCounts {
		k := keyFor("", id)
		st, ok := stats[k]
		if !ok {
			st = &abilityStat{AbilityID: id}
			stats[k] = st
		}
		st.Uses += uses
	}
	for id, hits := range snap.HitIDCounts {
		k := keyFor("", id)
		st, ok := stats[k]
		if !ok {
			st = &abilityStat{AbilityID: id}
			stats[k] = st
		}
		st.Hits += hits
	}
	for id, evades := range snap.EvadeIDCounts {
		k := keyFor("", id)
		st, ok := stats[k]
		if !ok {
			st = &abilityStat{AbilityID: id}
			stats[k] = st
		}
		st.Evades += evades
	}

	for name, uses := range snap.AbilityCounts {
		nameKey := strings.ToLower(strings.TrimSpace(name))
		if id, ok := s.abilityIDByNameKey[nameKey]; ok && id > 0 {
			// This event was already counted via AbilityIDCounts; only keep a readable name.
			k := keyFor("", id)
			st, ok := stats[k]
			if !ok {
				st = &abilityStat{AbilityID: id}
				stats[k] = st
			}
			if st.Name == "" && name != "" {
				st.Name = name
			}
			continue
		}
		k := keyFor(name, 0)
		if st, ok := stats[k]; ok {
			st.Uses += uses
			if st.Name == "" && name != "" {
				st.Name = name
			}
		} else {
			stats[k] = &abilityStat{Name: name, Uses: uses}
		}
	}
	for name, hits := range snap.HitCounts {
		nameKey := strings.ToLower(strings.TrimSpace(name))
		if id, ok := s.abilityIDByNameKey[nameKey]; ok && id > 0 {
			// Already counted via HitIDCounts; only keep human-readable naming.
			k := keyFor("", id)
			st, ok := stats[k]
			if !ok {
				st = &abilityStat{AbilityID: id}
				stats[k] = st
			}
			if st.Name == "" && name != "" {
				st.Name = name
			}
			continue
		}
		k := keyFor(name, 0)
		if st, ok := stats[k]; ok {
			st.Hits += hits
			if st.Name == "" && name != "" {
				st.Name = name
			}
		} else {
			stats[k] = &abilityStat{Name: name, Hits: hits}
		}
	}
	for name, evades := range snap.EvadeCounts {
		nameKey := strings.ToLower(strings.TrimSpace(name))
		if id, ok := s.abilityIDByNameKey[nameKey]; ok && id > 0 {
			// Already counted via EvadeIDCounts; only keep human-readable naming.
			k := keyFor("", id)
			st, ok := stats[k]
			if !ok {
				st = &abilityStat{AbilityID: id}
				stats[k] = st
			}
			if st.Name == "" && name != "" {
				st.Name = name
			}
			continue
		}
		k := keyFor(name, 0)
		if st, ok := stats[k]; ok {
			st.Evades += evades
			if st.Name == "" && name != "" {
				st.Name = name
			}
		} else {
			stats[k] = &abilityStat{Name: name, Evades: evades}
		}
	}

	out := make([]abilityStat, 0, len(stats))
	for _, st := range stats {
		a, ok := resolve(st.Name, st.AbilityID)
		if ok {
			// Prefer friendly CDN display name over internal/log token names.
			if strings.TrimSpace(a.Name) != "" {
				st.Name = a.Name
			} else if st.Name == "" {
				st.Name = a.InternalName
			}
			st.Skill = a.Skill
			st.DamageType = a.DamageType
			st.BaseDamage = a.BaseDamage()
		}

		// Player estimate is based on ability uses only.
		// entity_* hit lines are intentionally not used for outgoing player damage.
		eventsForDPS := st.Uses
		if eventsForDPS > 0 && st.BaseDamage > 0 {
			st.EstDamage = st.BaseDamage * float64(eventsForDPS)
			if duration > 0 {
				st.EstDPS = st.EstDamage / duration
			}
		}

		out = append(out, *st)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].EstDamage == out[j].EstDamage {
			if out[i].Uses == out[j].Uses {
				return out[i].Hits > out[j].Hits
			}
			return out[i].Uses > out[j].Uses
		}
		return out[i].EstDamage > out[j].EstDamage
	})

	writeJSON(w, out)
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
//
// Source attribution strategy:
//   - Preferred: link each raw loot event to the nearest kill event in a short
//     time window (heuristic item->mob attribution).
//   - Fallback: if old reports don't have loot_events, source is "Unknown".
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

	type dropSource struct {
		Name         string  `json:"name"`
		Count        int     `json:"count"`
		SessionCount int     `json:"session_count"`
		Chance       float64 `json:"chance"` // per-source chance [%]
	}
	type dropStat struct {
		Name          string       `json:"name"`
		TotalCount    int          `json:"total_count"`
		SessionCount  int          `json:"session_count"`
		AvgPerSession float64      `json:"avg_per_session"`
		AvgValue      float64      `json:"avg_value"`
		OverallChance float64      `json:"overall_chance"` // [%] sessions containing item
		NowChance     float64      `json:"now_chance"`     // [%] in current source context
		PrimarySource string       `json:"primary_source"`
		Sources       []dropSource `json:"sources,omitempty"`
	}
	type srcAccum struct {
		Count        int
		SessionCount int
	}
	type itemAccum struct {
		dropStat
		SourcesMap map[string]*srcAccum
	}

	inferLootSource := func(lootAt time.Time, kills []session.KillEvent, fallback string) string {
		if strings.TrimSpace(fallback) == "" {
			fallback = "Unknown"
		}
		if lootAt.IsZero() || len(kills) == 0 {
			return fallback
		}
		const beforeWindow = 20 * time.Second
		const afterWindow = 3 * time.Second
		bestMob := ""
		bestScore := time.Duration(1<<63 - 1)
		for _, k := range kills {
			if strings.TrimSpace(k.Mob) == "" || k.Time.IsZero() {
				continue
			}
			dt := lootAt.Sub(k.Time) // positive means kill happened before loot
			if dt >= 0 {
				if dt > beforeWindow {
					continue
				}
				if dt < bestScore {
					bestScore = dt
					bestMob = k.Mob
				}
				continue
			}
			adt := -dt
			if adt > afterWindow {
				continue
			}
			score := adt + 5*time.Second // prefer prior kills over future ones
			if score < bestScore {
				bestScore = score
				bestMob = k.Mob
			}
		}
		if bestMob == "" {
			return fallback
		}
		return bestMob
	}

	normalizeContext := func(v string) string {
		v = strings.TrimSpace(v)
		if v == "" {
			return ""
		}
		lv := strings.ToLower(v)
		if lv == "unnamed" || lv == "unknown" || lv == "test dungeon" {
			return ""
		}
		return v
	}

	agg := map[string]*itemAccum{}
	sessionCount := 0
	// denominator for per-source chance: sessions where that source was present
	sourceOpportunitySessions := map[string]int{}

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

		sessionFallbackSource := normalizeContext(snap.Dungeon)
		if sessionFallbackSource == "" && len(snap.ZoneHistory) > 0 {
			sessionFallbackSource = normalizeContext(snap.ZoneHistory[len(snap.ZoneHistory)-1].Zone)
		}
		if sessionFallbackSource == "" {
			sessionFallbackSource = "Unknown"
		}

		// Mark source opportunities in this session.
		sessionSources := map[string]bool{}
		for _, k := range snap.Kills {
			mob := strings.TrimSpace(k.Mob)
			if mob != "" {
				sessionSources[mob] = true
			}
		}
		if len(sessionSources) == 0 {
			sessionSources[sessionFallbackSource] = true
		}
		for src := range sessionSources {
			sourceOpportunitySessions[src]++
		}

		seenItem := map[string]bool{}
		seenItemSource := map[string]bool{}

		if len(snap.LootEvents) > 0 {
			for _, le := range snap.LootEvents {
				if strings.TrimSpace(le.Name) == "" {
					continue
				}
				count := le.Count
				if count <= 0 {
					count = 1
				}
				src := inferLootSource(le.Time, snap.Kills, sessionFallbackSource)
				name := le.Name

				seenItem[name] = true
				seenItemSource[name+"||"+src] = true

				item, ok := agg[name]
				if !ok {
					item = &itemAccum{
						dropStat:   dropStat{Name: name},
						SourcesMap: map[string]*srcAccum{},
					}
					agg[name] = item
				}
				item.TotalCount += count
				item.AvgValue += le.Value * float64(count)

				sa, ok := item.SourcesMap[src]
				if !ok {
					sa = &srcAccum{}
					item.SourcesMap[src] = sa
				}
				sa.Count += count
			}
		} else {
			// Legacy fallback (old reports): no event timeline to map item->mob.
			for _, loot := range snap.Loot {
				name := strings.TrimSpace(loot.Name)
				if name == "" {
					continue
				}
				count := loot.Count
				if count <= 0 {
					count = 1
				}
				src := sessionFallbackSource

				seenItem[name] = true
				seenItemSource[name+"||"+src] = true

				item, ok := agg[name]
				if !ok {
					item = &itemAccum{
						dropStat:   dropStat{Name: name},
						SourcesMap: map[string]*srcAccum{},
					}
					agg[name] = item
				}
				item.TotalCount += count
				item.AvgValue += loot.Valor * float64(count)
				sa, ok := item.SourcesMap[src]
				if !ok {
					sa = &srcAccum{}
					item.SourcesMap[src] = sa
				}
				sa.Count += count
			}
		}

		for name := range seenItem {
			if item, ok := agg[name]; ok {
				item.SessionCount++
			}
		}
		for k := range seenItemSource {
			parts := strings.SplitN(k, "||", 2)
			if len(parts) != 2 {
				continue
			}
			name, src := parts[0], parts[1]
			if item, ok := agg[name]; ok {
				if sa, ok := item.SourcesMap[src]; ok {
					sa.SessionCount++
				}
			}
		}
	}

	// Current context for "chance now": most recently killed mob in active session.
	currentSource := "Unknown"
	if cur := s.Sess.Snapshot(); len(cur.Kills) > 0 {
		mob := normalizeContext(cur.Kills[len(cur.Kills)-1].Mob)
		if mob != "" {
			currentSource = mob
		}
	}
	if currentSource == "Unknown" {
		cur := s.Sess.Snapshot()
		if z := normalizeContext(cur.Zone); z != "" {
			currentSource = z
		} else if d := normalizeContext(cur.Dungeon); d != "" {
			currentSource = d
		}
	}

	result := make([]dropStat, 0, len(agg))
	for _, item := range agg {
		d := item.dropStat
		if sessionCount > 0 {
			d.AvgPerSession = float64(d.TotalCount) / float64(sessionCount)
			d.OverallChance = (float64(d.SessionCount) / float64(sessionCount)) * 100.0
		}
		if d.TotalCount > 0 {
			d.AvgValue = d.AvgValue / float64(d.TotalCount)
		}

		sources := make([]dropSource, 0, len(item.SourcesMap))
		for srcName, sa := range item.SourcesMap {
			opp := sourceOpportunitySessions[srcName]
			chance := 0.0
			if opp > 0 {
				chance = (float64(sa.SessionCount) / float64(opp)) * 100.0
			}
			sources = append(sources, dropSource{
				Name:         srcName,
				Count:        sa.Count,
				SessionCount: sa.SessionCount,
				Chance:       chance,
			})
		}
		sort.Slice(sources, func(i, j int) bool {
			if sources[i].Chance == sources[j].Chance {
				if sources[i].SessionCount == sources[j].SessionCount {
					return sources[i].Count > sources[j].Count
				}
				return sources[i].SessionCount > sources[j].SessionCount
			}
			return sources[i].Chance > sources[j].Chance
		})
		d.Sources = sources
		if len(sources) > 0 {
			d.PrimarySource = sources[0].Name
		}

		d.NowChance = d.OverallChance
		if sa, ok := item.SourcesMap[currentSource]; ok {
			opp := sourceOpportunitySessions[currentSource]
			if opp > 0 {
				d.NowChance = (float64(sa.SessionCount) / float64(opp)) * 100.0
			}
		}
		result = append(result, d)
	}

	if result == nil {
		result = []dropStat{}
	}
	writeJSON(w, result)
}
