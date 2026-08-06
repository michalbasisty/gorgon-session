package server

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
			Action      string   `json:"action"`
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

		// Bulk area import: auto-populate traders for all NPCs in a zone.
		if req.Action == "bulk_area" {
			rd := 5
			if req.ResetDays != nil {
				rd = *req.ResetDays
			}
			rh := 22
			if req.ResetHours != nil {
				rh = *req.ResetHours
			}
			var names []string
			for _, n := range s.Npcs {
				hasStore := false
				for _, svc := range n.Services {
					t := strings.ToLower(strings.TrimSpace(svc.Type))
					if t == "store" || t == "consignment" {
						hasStore = true
						break
					}
				}
				if !hasStore {
					continue
				}
				if strings.EqualFold(n.AreaFriendly, req.Area) {
					names = append(names, n.InternalName)
				}
			}
			if err := s.Trader.BulkEnsureByArea(req.Area, names, rd, rh); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := s.Trader.Save(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"ok": true, "count": len(names)})
			return
		}

		// Only call Ensure when we have setup data (limit/sold/reset/area).
		// Skipping for a plain log-sale (amount-only) preserves the existing remaining time.
		needsEnsure := req.WeeklyLimit != nil || req.Sold != nil || req.ResetDays != nil || req.ResetHours != nil || req.Area != ""
		if needsEnsure {
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

// handleTraderHistory returns refresh event history. Optional ?npc=Name filter.
func (s *Server) handleTraderHistory(w http.ResponseWriter, r *http.Request) {
	if s.Trader == nil {
		writeJSON(w, []any{})
		return
	}
	npcName := r.URL.Query().Get("npc")
	events := s.Trader.GetRefreshHistory(npcName)
	writeJSON(w, events)
}

// handleTraderHistoryExport: GET CSV dump of all refresh events.
func (s *Server) handleTraderHistoryExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="traders-history.csv"`)
	wr := csv.NewWriter(w)
	defer wr.Flush()

	wr.Write([]string{"time", "npc", "item", "qty", "price", "total", "action"})
	if s.Trader == nil {
		return
	}
	// ponytail: RefreshEvent has no per-item sale data; item/qty/price are
	// left empty and total is the sold amount at reset. Upgrade path: log
	// individual sales in LogSale and add those fields to RefreshEvent.
	for _, e := range s.Trader.GetRefreshHistory("") {
		wr.Write([]string{
			e.ResetAt.Format(time.RFC3339),
			e.NPCName,
			e.Area,
			"",
			"",
			strconv.FormatFloat(e.SoldAtReset, 'f', -1, 64),
			"reset",
		})
	}
}

// handleTraderHistoryDelete: POST {"id":"<event-id>"} removes one refresh event.
func (s *Server) handleTraderHistoryDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	if s.Trader == nil {
		http.Error(w, "traders not initialized", http.StatusInternalServerError)
		return
	}
	if err := s.Trader.DeleteHistoryEvent(req.ID); err != nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// TraderInfo is one trader row within an area group.
type TraderInfo struct {
	NPCName        string   `json:"npc_name"`
	HasBarter      bool     `json:"has_barter"`
	ItemTypes      []string `json:"item_types"`
	WeeklyLimit    float64  `json:"weekly_limit"`
	SoldThisWeek   float64  `json:"sold_this_week"`
	ResetDays      int      `json:"reset_days"`
	ResetHours     int      `json:"reset_hours"`
	TimeUntilReset string   `json:"time_until_reset"`
	UnusedWarning  bool     `json:"unused_warning"`
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

		// Look up full CDN NPC data for service item types.
		if cdnNpc, ok := s.Npcs[npc.InternalName]; ok {
			seen := make(map[string]bool)
			for _, svc := range cdnNpc.Services {
				t := strings.ToLower(strings.TrimSpace(svc.Type))
				if t == "store" || t == "consignment" || t == "barter" {
					for _, it := range svc.ItemTypes {
						if !seen[it] {
							seen[it] = true
							info.ItemTypes = append(info.ItemTypes, it)
						}
					}
				}
			}
		}

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

// handleTraderSchedule returns all traders sorted by closest refresh first.
func (s *Server) handleTraderSchedule(w http.ResponseWriter, r *http.Request) {
	if s.Trader == nil {
		writeJSON(w, []any{})
		return
	}
	schedule := s.Trader.GetRefreshSchedule()
	writeJSON(w, schedule)
}

// RouteInfo is one sell-route candidate for an item.
type RouteInfo struct {
	Trader             string   `json:"trader"`
	Area               string   `json:"area"`
	Keywords           []string `json:"keywords"`
	RemainingCapacityG float64  `json:"remaining_capacity_g"`
	RefreshInHours     float64  `json:"refresh_in_hours"`
	DistanceKm         *float64 `json:"distance_km"` // null when zone coords unavailable
}

// camelRE splits words at CamelCase boundaries (e.g. "LeatherArmor" ->
// "Leather", "Armor"). Only letters match, so digits are dropped. A run
// of all-caps (e.g. "LEATHERARMOR") stays one token; real keywords are
// MixedCase so this is fine.
var camelRE = regexp.MustCompile(`[A-Z]?[a-z]+|[A-Z]+`)

// tokenizeKeyword splits s into lowercase word tokens at non-alphanumeric
// and CamelCase boundaries, dropping tokens shorter than 3 chars.
func tokenizeKeyword(s string) []string {
	var toks []string
	for _, m := range camelRE.FindAllString(s, -1) {
		t := strings.ToLower(m)
		if len(t) >= 3 {
			toks = append(toks, t)
		}
	}
	return toks
}

// tokensOverlap reports whether any token in a and b are equal or one is a
// prefix/suffix of the other.
func tokensOverlap(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y || strings.HasPrefix(x, y) || strings.HasPrefix(y, x) ||
				strings.HasSuffix(x, y) || strings.HasSuffix(y, x) {
				return true
			}
		}
	}
	return false
}

// routePlannerMatch reports whether item matches a trader via NPC name
// (substring) or store/consignment keywords (token overlap). Empty
// keywords only match through the NPC name.
func routePlannerMatch(item, npcName string, keywords []string) bool {
	needle := strings.ToLower(item)
	if strings.Contains(strings.ToLower(npcName), needle) {
		return true
	}
	if len(keywords) == 0 {
		return false
	}
	itemTokens := tokenizeKeyword(item)
	for _, kw := range keywords {
		if tokensOverlap(itemTokens, tokenizeKeyword(kw)) {
			return true
		}
	}
	return false
}

// traderKeywords returns deduped store/consignment ItemTypes for an NPC.
func (s *Server) traderKeywords(npc cdn.Npc) []string {
	var keywords []string
	cdnNpc, ok := s.Npcs[npc.InternalName]
	if !ok {
		return keywords
	}
	seen := map[string]bool{}
	for _, svc := range cdnNpc.Services {
		t := strings.ToLower(strings.TrimSpace(svc.Type))
		if t == "store" || t == "consignment" {
			for _, kw := range svc.ItemTypes {
				if !seen[kw] {
					seen[kw] = true
					keywords = append(keywords, kw)
				}
			}
		}
	}
	return keywords
}

// areaCoords looks up an area's optional X/Y coordinates by friendly name.
// Prefers CDN coordinates; falls back to the built-in wiki-derived table when
// the CDN publishes none (the current state of areas.json).
func (s *Server) areaCoords(area string) (float64, float64, bool) {
	if s.Areas.ByFriendly != nil {
		if key, ok := s.Areas.ByFriendly[strings.ToLower(strings.TrimSpace(area))]; ok {
			if a, ok := s.Areas.ByInternal[key]; ok && a.X != nil && a.Y != nil {
				return *a.X, *a.Y, true
			}
		}
	}
	return fallbackAreaCoords(area)
}

// areaDistance returns the Euclidean distance between two areas (by friendly
// name), or ok=false when either zone has no coordinates in the area index.
func (s *Server) areaDistance(fromArea, toArea string) (float64, bool) {
	fx, fy, ok := s.areaCoords(fromArea)
	if !ok {
		return 0, false
	}
	tx, ty, ok := s.areaCoords(toArea)
	if !ok {
		return 0, false
	}
	dx, dy := fx-tx, fy-ty
	return round1(math.Sqrt(dx*dx + dy*dy)), true
}

// handleRoutePlanner: GET ?item=NAME lists traders whose name or store/consignment
// keywords match the item, sorted by remaining capacity (grams) descending, or by
// distance from the player's zone with ?sort=distance. GET ?trader=NAME returns
// the sell/favor items from the current session loot that match that trader.
func (s *Server) handleRoutePlanner(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	item := strings.TrimSpace(r.URL.Query().Get("item"))
	traderName := strings.TrimSpace(r.URL.Query().Get("trader"))
	if item != "" && traderName != "" {
		http.Error(w, "specify either item or trader, not both", http.StatusBadRequest)
		return
	}
	if traderName != "" {
		s.routePlannerByTrader(w, traderName)
		return
	}
	if item == "" {
		http.Error(w, "item query param required", http.StatusBadRequest)
		return
	}
	routes := []RouteInfo{}
	if s.Favor == nil {
		writeJSON(w, map[string]any{"item": item, "routes": routes})
		return
	}

	snap := s.Sess.Snapshot()
	for _, npc := range s.Favor.GetNPCs() {
		keywords := s.traderKeywords(npc)
		if !routePlannerMatch(item, npc.Name, keywords) {
			continue
		}

		ri := RouteInfo{Trader: npc.Name, Area: npc.AreaFriendly, Keywords: keywords}
		if s.Trader != nil {
			if t := s.Trader.Get(npc.Name); t != nil {
				ri.RemainingCapacityG = t.WeeklyLimit - t.SoldThisWeek
				ri.RefreshInHours = round1(s.Trader.TimeUntilReset(npc.Name).Hours())
			}
		}
		if d, ok := s.areaDistance(snap.Zone, npc.AreaFriendly); ok {
			ri.DistanceKm = &d
		}
		routes = append(routes, ri)
	}

	if r.URL.Query().Get("sort") == "distance" {
		sort.Slice(routes, func(i, j int) bool {
			di, dj := routes[i].DistanceKm, routes[j].DistanceKm
			if di == nil || dj == nil {
				return di != nil // routes without distance sort last
			}
			if *di == *dj {
				return routes[i].RemainingCapacityG > routes[j].RemainingCapacityG
			}
			return *di < *dj
		})
	} else {
		sort.Slice(routes, func(i, j int) bool {
			return routes[i].RemainingCapacityG > routes[j].RemainingCapacityG
		})
	}
	writeJSON(w, map[string]any{"item": item, "routes": routes})
}

// sellRouteItem is one session-loot item this trader's store/consignment buys.
type sellRouteItem struct {
	Name  string  `json:"name"`
	Count int     `json:"count"`
	Value float64 `json:"value"`
}

// favorRouteItem is one session-loot item this trader has a favor preference for.
type favorRouteItem struct {
	Name       string  `json:"name"`
	Count      int     `json:"count"`
	FavorScore float64 `json:"favor_score"`
}

// routePlannerByTrader: trader-centric view of what the current session loot
// can be sold to / gifted to a single trader.
func (s *Server) routePlannerByTrader(w http.ResponseWriter, traderName string) {
	empty := map[string]any{
		"trader": traderName, "area": "", "distance_km": nil,
		"sell_items": []sellRouteItem{}, "favor_items": []favorRouteItem{},
	}
	if s.Favor == nil {
		writeJSON(w, empty)
		return
	}
	var npc *cdn.Npc
	for _, n := range s.Favor.GetNPCs() {
		if strings.EqualFold(strings.TrimSpace(n.Name), traderName) ||
			strings.EqualFold(strings.TrimSpace(n.InternalName), traderName) {
			nn := n
			npc = &nn
			break
		}
	}
	if npc == nil {
		http.Error(w, "trader not found", http.StatusNotFound)
		return
	}

	snap := s.Sess.Snapshot()
	keywords := s.traderKeywords(*npc)
	sellItems := []sellRouteItem{}
	favorItems := []favorRouteItem{}
	for _, entry := range snap.Loot {
		if routePlannerMatch(entry.Name, npc.Name, keywords) {
			sellItems = append(sellItems, sellRouteItem{Name: entry.Name, Count: entry.Count, Value: entry.Valor})
		}
		dec := s.Favor.ResolveItem(s.ItemByName(entry.Name).Item)
		for _, tgt := range dec.FavorTargets {
			if tgt.Score > 0 && strings.EqualFold(strings.TrimSpace(tgt.NPC), npc.Name) {
				favorItems = append(favorItems, favorRouteItem{Name: entry.Name, Count: entry.Count, FavorScore: tgt.Score})
			}
		}
	}

	resp := map[string]any{
		"trader": npc.Name, "area": npc.AreaFriendly,
		"sell_items": sellItems, "favor_items": favorItems, "distance_km": nil,
	}
	if d, ok := s.areaDistance(snap.Zone, npc.AreaFriendly); ok {
		resp["distance_km"] = d
	}
	writeJSON(w, resp)
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
	s.cfgMu.RLock()
	reportDir := s.Cfg.ReportDir
	s.cfgMu.RUnlock()
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
	s.cfgMu.RLock()
	cfg := s.Cfg
	s.cfgMu.RUnlock()
	export := map[string]any{
		"config":      cfg,
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

	next := body.Config
	s.cfgMu.Lock()
	s.Cfg = next
	s.cfgMu.Unlock()
	if err := config.Save(next); err != nil {
		http.Error(w, "failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Update live components, mirroring handleConfig — an imported config with
	// different paths/regex must take effect without a restart.
	if s.Tailer != nil {
		s.Tailer.SetDir(next.ChatLogDir)
	}
	if s.PLTailer != nil {
		s.PLTailer.SetPath(next.PlayerLogPath)
	}
	if s.Parser != nil {
		if err := s.Parser.SetRegex(next.LootRegex); err != nil {
			http.Error(w, "failed to apply loot_regex: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if s.Favor != nil {
		s.Favor.SetPlayerPrices(next.PlayerPrices)
	}
	writeJSON(w, map[string]any{"ok": true, "message": "Settings imported successfully"})
}
