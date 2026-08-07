package server

import (
	"encoding/json"
	"fmt"
	"math"
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

// round1 rounds to one decimal place for display-friendly float fields.
func round1(v float64) float64 { return math.Round(v*10) / 10 }

// npcDisplayName maps a game-internal NPC key (e.g. "NPC_Rugen") to its
// friendly name ("Rugen") via the CDN data. Unknown/blank names pass through.
func (s *Server) npcDisplayName(raw string) string {
	n := strings.TrimSpace(raw)
	if n == "" {
		return n
	}
	if npc, ok := s.Npcs[n]; ok && strings.TrimSpace(npc.Name) != "" {
		return npc.Name
	}
	return n
}

// wilsonLower returns the 95% Wilson score lower bound (as a fraction) for
// `drops` out of `kills`, clamped to [0, observed rate]. Returns 0 when
// kills == 0.
func wilsonLower(drops, kills float64) float64 {
	if kills <= 0 {
		return 0
	}
	const z = 1.96
	z2 := z * z
	phat := (drops + z2/2) / (kills + z2)
	if phat > 1 {
		phat = 1 // drops can exceed kills (multi-loot); clamp so sqrt arg stays >= 0
	}
	halfwidth := z * math.Sqrt((phat*(1-phat)+z2/(4*kills))/kills) / (1 + z2/kills)
	lower := phat - halfwidth
	observed := drops / kills
	if lower < 0 {
		lower = 0
	}
	if lower > observed {
		lower = observed
	}
	return lower
}

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

// handleRecipesSearch: GET ?q=&skill=&level= case-insensitive substring search
// over recipe names, limited to 50 results.
func (s *Server) handleRecipesSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query().Get("q")
	skill := r.URL.Query().Get("skill")
	level, _ := strconv.Atoi(r.URL.Query().Get("level"))
	writeJSON(w, map[string]any{"recipes": searchRecipes(s.Recipes, s.ItemByID, q, skill, level)})
}

// RecipeHit is one recipe row in search results.
type RecipeHit struct {
	Name        string   `json:"name"`
	Skill       string   `json:"skill"`
	Level       int      `json:"level"`
	ResultItem  string   `json:"result_item"`
	Ingredients []string `json:"ingredients"`
}

func searchRecipes(recipes cdn.RecipesFile, items map[int]cdn.Item, q, skill string, level int) []RecipeHit {
	const limit = 50
	q = strings.ToLower(strings.TrimSpace(q))
	out := []RecipeHit{}
	for _, rec := range recipes {
		if q != "" && !strings.Contains(strings.ToLower(rec.Name), q) {
			continue
		}
		if skill != "" && !strings.EqualFold(rec.Skill, skill) {
			continue
		}
		if level > 0 && rec.SkillLevelReq > level {
			continue
		}
		hit := RecipeHit{Name: rec.Name, Skill: rec.Skill, Level: rec.SkillLevelReq}
		if len(rec.ResultItems) > 0 {
			code := rec.ResultItems[0].ItemCode
			if it, ok := items[code]; ok {
				hit.ResultItem = it.Name
			} else {
				hit.ResultItem = fmt.Sprintf("item_%d", code)
			}
		}
		for _, ing := range rec.Ingredients {
			name, ok := items[ing.ItemCode]
			ingName := fmt.Sprintf("item_%d", ing.ItemCode)
			if ok {
				ingName = name.Name
			}
			hit.Ingredients = append(hit.Ingredients, fmt.Sprintf("%s x%d", ingName, ing.StackSize))
		}
		out = append(out, hit)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// ProfitIngredient is one ingredient row in the profit calculator.
type ProfitIngredient struct {
	Name string  `json:"name"`
	Qty  int     `json:"qty"`
	Cost float64 `json:"cost"`
}

// ProfitRecipe is one recipe row in the profit calculator.
type ProfitRecipe struct {
	Name            string             `json:"name"`
	Skill           string             `json:"skill"`
	Level           int                `json:"level"`
	Ingredients     []ProfitIngredient `json:"ingredients"`
	IngredientsCost float64            `json:"ingredients_cost"`
	SellValue       float64            `json:"sell_value"`
	Profit          float64            `json:"profit"`
	MarginPct       float64            `json:"margin_pct"`
	CostUnknown     bool               `json:"cost_unknown"`
}

// calcProfit computes per-recipe profit from CDN item values. Recipes whose
// result or ingredient items are missing from the item index get cost_unknown.
func calcProfit(recipes cdn.RecipesFile, items map[int]cdn.Item, skill string, maxLevel int) []ProfitRecipe {
	out := []ProfitRecipe{}
	for _, rec := range recipes {
		if skill != "" && !strings.EqualFold(rec.Skill, skill) {
			continue
		}
		if maxLevel > 0 && rec.SkillLevelReq > maxLevel {
			continue
		}

		pr := ProfitRecipe{Name: rec.Name, Skill: rec.Skill, Level: rec.SkillLevelReq, Ingredients: []ProfitIngredient{}}
		sell := 0.0
		if len(rec.ResultItems) == 0 {
			pr.CostUnknown = true
		}
		for _, res := range rec.ResultItems {
			it, ok := items[res.ItemCode]
			if !ok {
				pr.CostUnknown = true
				continue
			}
			sell += it.Value * float64(res.StackSize)
		}
		cost := 0.0
		for _, ing := range rec.Ingredients {
			it, ok := items[ing.ItemCode]
			if !ok {
				pr.CostUnknown = true
				pr.Ingredients = append(pr.Ingredients, ProfitIngredient{Name: fmt.Sprintf("item_%d", ing.ItemCode), Qty: ing.StackSize})
				continue
			}
			c := it.Value * float64(ing.StackSize)
			cost += c
			pr.Ingredients = append(pr.Ingredients, ProfitIngredient{Name: it.Name, Qty: ing.StackSize, Cost: c})
		}
		pr.SellValue = sell
		pr.IngredientsCost = cost
		if pr.CostUnknown {
			pr.Profit = 0
		} else if sell > 0 {
			pr.Profit = sell - cost
			pr.MarginPct = round1(pr.Profit / sell * 100)
		} else {
			pr.Profit = -cost
		}
		out = append(out, pr)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Profit == out[j].Profit {
			return out[i].Name < out[j].Name
		}
		return out[i].Profit > out[j].Profit
	})
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

// handleCraftingProfit: GET ?skill=NAME&max_level=N top-20 recipes by profit.
func (s *Server) handleCraftingProfit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	skill := r.URL.Query().Get("skill")
	maxLevel, _ := strconv.Atoi(r.URL.Query().Get("max_level"))
	writeJSON(w, map[string]any{"recipes": calcProfit(s.Recipes, s.ItemByID, skill, maxLevel)})
}

// PriceTrendEntry is one price observation in the trends response.
type PriceTrendEntry struct {
	T     string  `json:"t"`
	Price float64 `json:"price"`
	Qty   int     `json:"qty"`
}

// handlePriceTrends: GET ?name=ITEM last 50 chronological price observations.
func (s *Server) handlePriceTrends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "name query param required", http.StatusBadRequest)
		return
	}
	entries := []PriceTrendEntry{}
	if s.Prices != nil {
		all := s.Prices.Get(name) // chronological ascending
		if len(all) > 50 {
			all = all[len(all)-50:]
		}
		for _, e := range all {
			entries = append(entries, PriceTrendEntry{T: e.Date.Format(time.RFC3339), Price: e.Price, Qty: e.Count})
		}
	}
	writeJSON(w, map[string]any{"item": name, "entries": entries})
}

// sessionSnapshot loads a past session report by ID, or the current snapshot
// when id is empty. On failure it writes the error response and returns ok=false.
func (s *Server) sessionSnapshot(id string, w http.ResponseWriter) (session.Snapshot, bool) {
	if id == "" {
		return s.Sess.Snapshot(), true
	}
	if !validSessionID(id) {
		http.Error(w, "invalid session ID", http.StatusBadRequest)
		return session.Snapshot{}, false
	}
	s.cfgMu.RLock()
	reportDir := s.Cfg.ReportDir
	s.cfgMu.RUnlock()
	if reportDir == "" {
		http.Error(w, "report directory not configured", http.StatusInternalServerError)
		return session.Snapshot{}, false
	}
	data, err := os.ReadFile(filepath.Join(reportDir, id+".json"))
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return session.Snapshot{}, false
	}
	var snap session.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		http.Error(w, "failed to parse session", http.StatusInternalServerError)
		return session.Snapshot{}, false
	}
	return snap, true
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
	requestedSource := strings.TrimSpace(r.URL.Query().Get("source"))
	sourceFilter := strings.ToLower(requestedSource)
	zoneFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("zone")))
	s.cfgMu.RLock()
	reportDir := s.Cfg.ReportDir
	s.cfgMu.RUnlock()
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
		Kills        int     `json:"kills"`
		ConfLower    float64 `json:"conf_lower"` // 95% Wilson lower bound on per-kill rate [%]
		LowSample    bool    `json:"low_sample"` // true when kills < 30
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
		Zones         []dropSource `json:"zones,omitempty"`
	}
	type srcAccum struct {
		Count        int
		SessionCount int
	}
	type itemAccum struct {
		dropStat
		SourcesMap map[string]*srcAccum
		ZonesMap   map[string]*srcAccum
	}
	type zoneCount struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
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
		return s.npcDisplayName(bestMob)
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
	// denominator for per-zone chance: sessions where that zone was present
	zoneOpportunitySessions := map[string]int{}
	// global per-zone item totals across the filtered event stream
	zoneTotals := map[string]int{}
	// kill-event totals per source mob and per zone (for Wilson confidence)
	sourceKills := map[string]int{}
	zoneKills := map[string]int{}

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
		mob := s.npcDisplayName(k.Mob)
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

		// Zone in effect at each loot event: the last zone_history entry at or
		// before the event time (both lists are chronological; walk forward).
		zh := snap.ZoneHistory
		zoneFallback := "Unknown"
		if len(zh) > 0 {
			if z := strings.TrimSpace(zh[len(zh)-1].Zone); z != "" {
				zoneFallback = z
			}
		}
		zi := 0
		zoneAt := func(t time.Time) string {
			if len(zh) == 0 || t.IsZero() {
				return zoneFallback
			}
			for zi+1 < len(zh) && !zh[zi+1].Time.After(t) {
				zi++
			}
			if zh[zi].Time.After(t) {
				return zoneFallback // event predates the first recorded zone
			}
			if z := strings.TrimSpace(zh[zi].Zone); z != "" {
				return z
			}
			return zoneFallback
		}

		sessionZones := map[string]bool{}
		for _, z := range zh {
			if zz := strings.TrimSpace(z.Zone); zz != "" {
				sessionZones[zz] = true
			}
		}
		if len(sessionZones) == 0 {
			sessionZones[zoneFallback] = true
		}
		for z := range sessionZones {
			zoneOpportunitySessions[z]++
		}

		// Kill attribution for confidence bounds. Kill events are
		// chronological, so a fresh walker is safe here (the zoneAt closure
		// above has already advanced over the loot timeline).
		zi2 := 0
		zoneAtKill := func(t time.Time) string {
			if len(zh) == 0 || t.IsZero() {
				return zoneFallback
			}
			for zi2+1 < len(zh) && !zh[zi2+1].Time.After(t) {
				zi2++
			}
			if zh[zi2].Time.After(t) {
				return zoneFallback
			}
			if z := strings.TrimSpace(zh[zi2].Zone); z != "" {
				return z
			}
			return zoneFallback
		}
		for _, k := range snap.Kills {
			if mob := s.npcDisplayName(k.Mob); mob != "" {
				sourceKills[mob]++
			}
			zoneKills[zoneAtKill(k.Time)]++
		}

		seenItem := map[string]bool{}
		seenItemSource := map[string]bool{}
		seenItemZone := map[string]bool{}

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
				if sourceFilter != "" && strings.ToLower(src) != sourceFilter {
					continue
				}
				zone := zoneAt(le.Time)
				if zoneFilter != "" && strings.ToLower(zone) != zoneFilter {
					continue
				}
				name := le.Name

				seenItem[name] = true
				seenItemSource[name+"||"+src] = true
				seenItemZone[name+"||"+zone] = true
				zoneTotals[zone] += count

				item, ok := agg[name]
				if !ok {
					item = &itemAccum{
						dropStat:   dropStat{Name: name},
						SourcesMap: map[string]*srcAccum{},
						ZonesMap:   map[string]*srcAccum{},
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

				za, ok := item.ZonesMap[zone]
				if !ok {
					za = &srcAccum{}
					item.ZonesMap[zone] = za
				}
				za.Count += count
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
				if sourceFilter != "" && strings.ToLower(src) != sourceFilter {
					continue
				}
				zone := zoneFallback
				if zoneFilter != "" && strings.ToLower(zone) != zoneFilter {
					continue
				}

				seenItem[name] = true
				seenItemSource[name+"||"+src] = true
				seenItemZone[name+"||"+zone] = true
				zoneTotals[zone] += count

				item, ok := agg[name]
				if !ok {
					item = &itemAccum{
						dropStat:   dropStat{Name: name},
						SourcesMap: map[string]*srcAccum{},
						ZonesMap:   map[string]*srcAccum{},
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

				za, ok := item.ZonesMap[zone]
				if !ok {
					za = &srcAccum{}
					item.ZonesMap[zone] = za
				}
				za.Count += count
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
		for k := range seenItemZone {
			parts := strings.SplitN(k, "||", 2)
			if len(parts) != 2 {
				continue
			}
			name, zone := parts[0], parts[1]
			if item, ok := agg[name]; ok {
				if za, ok := item.ZonesMap[zone]; ok {
					za.SessionCount++
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
			kills := sourceKills[srcName]
			confLower := 0.0
			if kills > 0 {
				confLower = round1(wilsonLower(float64(sa.Count), float64(kills)) * 100)
			}
			sources = append(sources, dropSource{
				Name:         srcName,
				Count:        sa.Count,
				SessionCount: sa.SessionCount,
				Chance:       chance,
				Kills:        kills,
				ConfLower:    confLower,
				LowSample:    kills < 30,
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
		if requestedSource != "" {
			d.PrimarySource = requestedSource
		} else if len(sources) > 0 {
			d.PrimarySource = sources[0].Name
		}

		zones := make([]dropSource, 0, len(item.ZonesMap))
		for zn, za := range item.ZonesMap {
			opp := zoneOpportunitySessions[zn]
			chance := 0.0
			if opp > 0 {
				chance = (float64(za.SessionCount) / float64(opp)) * 100.0
			}
			kills := zoneKills[zn]
			confLower := 0.0
			if kills > 0 {
				confLower = round1(wilsonLower(float64(za.Count), float64(kills)) * 100)
			}
			zones = append(zones, dropSource{
				Name:         zn,
				Count:        za.Count,
				SessionCount: za.SessionCount,
				Chance:       chance,
				Kills:        kills,
				ConfLower:    confLower,
				LowSample:    kills < 30,
			})
		}
		sort.Slice(zones, func(i, j int) bool {
			if zones[i].Count == zones[j].Count {
				if zones[i].SessionCount == zones[j].SessionCount {
					return zones[i].Name < zones[j].Name
				}
				return zones[i].SessionCount > zones[j].SessionCount
			}
			return zones[i].Count > zones[j].Count
		})
		d.Zones = zones

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
	zoneList := make([]zoneCount, 0, len(zoneTotals))
	for name, c := range zoneTotals {
		zoneList = append(zoneList, zoneCount{Name: name, Count: c})
	}
	sort.Slice(zoneList, func(i, j int) bool {
		if zoneList[i].Count == zoneList[j].Count {
			return zoneList[i].Name < zoneList[j].Name
		}
		return zoneList[i].Count > zoneList[j].Count
	})
	if zoneList == nil {
		zoneList = []zoneCount{}
	}
	writeJSON(w, map[string]any{
		"items":          result,
		"zones":          zoneList,
		"current_source": currentSource,
	})
}
