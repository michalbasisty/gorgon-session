package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/michalbasisty/gorgon-session/internal/cdn"
	"github.com/michalbasisty/gorgon-session/internal/config"
	"github.com/michalbasisty/gorgon-session/internal/favor"
	"github.com/michalbasisty/gorgon-session/internal/logtail"
	"github.com/michalbasisty/gorgon-session/internal/loot"
	"github.com/michalbasisty/gorgon-session/internal/session"
	"github.com/michalbasisty/gorgon-session/internal/trader"
)

// configHome redirects config.Path() — used by config.Save/Load — into a temp
// dir so tests that persist config never touch the real ~/.gorgon-session.
// Returns the temp home dir (config lands at <home>/.gorgon-session/config.json).
func configHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// liveComponents builds a server whose live-updatable components (Tailer,
// PLTailer, Parser, Favor) can be asserted against after handleConfig /
// handleImport writes.
func liveComponents(t *testing.T) *Server {
	t.Helper()
	parser, err := loot.New("")
	if err != nil {
		t.Fatalf("loot.New: %v", err)
	}
	return &Server{
		Cfg: config.Config{
			ChatLogDir:         `C:\old\chatlogs`,
			PlayerLogPath:      `C:\old\player.log`,
			SellValueThreshold: 50,
			PlayerPrices:       map[string]float64{},
			Overlay:            config.Default().Overlay,
		},
		Tailer:   logtail.New(`C:\old\chatlogs`),
		PLTailer: logtail.NewFileTailer(`C:\old\player.log`),
		Parser:   parser,
		Favor:    favor.FromNpcs(cdn.NpcsFile{}),
	}
}

func postJSON(t *testing.T, h http.HandlerFunc, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

// ---- handleConfig ----

func TestHandleConfig_GetReturnsDefaults(t *testing.T) {
	srv := &Server{Cfg: config.Default()}
	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.handleConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if resp["http_addr"] != "127.0.0.1:7777" {
		t.Errorf("expected default http_addr, got %v", resp["http_addr"])
	}
	ov, ok := resp["overlay"].(map[string]any)
	if !ok {
		t.Fatalf("expected overlay object, got %T", resp["overlay"])
	}
	if ov["opacity"] != float64(98) || ov["click_through_opacity"] != float64(78) || ov["position"] != "bottom-right" {
		t.Errorf("unexpected overlay defaults: %+v", ov)
	}
}

func TestHandleConfig_PostPartialMergeAndLiveUpdate(t *testing.T) {
	home := configHome(t)
	srv := liveComponents(t)

	w := postJSON(t, srv.handleConfig, "/api/config",
		`{"chat_log_dir":"D:\\new\\chatlogs","player_prices":{"Runestone":1200}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// response reflects the merged config: patched fields updated, others preserved
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if resp["chat_log_dir"] != `D:\new\chatlogs` {
		t.Errorf("expected chat_log_dir merged into response, got %v", resp["chat_log_dir"])
	}
	if resp["sell_value_threshold"] != float64(50) {
		t.Errorf("expected sell_value_threshold preserved, got %v", resp["sell_value_threshold"])
	}

	// live components updated
	if srv.Tailer.Dir != `D:\new\chatlogs` {
		t.Errorf("expected Tailer.Dir live-updated, got %q", srv.Tailer.Dir)
	}
	dec := srv.Favor.ResolveItem(cdn.Item{Name: "Runestone", Value: 900})
	if dec.PlayerPrice != 1200 {
		t.Errorf("expected favor player price 1200, got %v", dec.PlayerPrice)
	}

	// config persisted to disk under the redirected home
	cfgPath := filepath.Join(home, ".gorgon-session", "config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("expected config persisted at %s: %v", cfgPath, err)
	}
	var saved config.Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("failed to parse saved config: %v", err)
	}
	if saved.ChatLogDir != `D:\new\chatlogs` {
		t.Errorf("expected persisted chat_log_dir, got %q", saved.ChatLogDir)
	}
	if saved.PlayerPrices["Runestone"] != 1200 {
		t.Errorf("expected persisted player price, got %v", saved.PlayerPrices)
	}
}

func TestHandleConfig_PostLootRegexLive(t *testing.T) {
	configHome(t)
	srv := liveComponents(t)

	if ev := srv.Parser.ParseLine("[Status] LOOTED Sword!"); ev != nil {
		t.Fatalf("expected no match under default regex, got %+v", ev)
	}

	w := postJSON(t, srv.handleConfig, "/api/config", `{"loot_regex":"\\[Status\\]\\s+LOOTED (.+?)!"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	ev := srv.Parser.ParseLine("[Status] LOOTED Sword!")
	if ev == nil || ev.Kind != loot.KindLoot || ev.ItemName != "Sword" {
		t.Errorf("expected parser live-updated to custom regex, got %+v", ev)
	}
}

func TestHandleConfig_PostInvalidLootRegex(t *testing.T) {
	home := configHome(t)
	srv := liveComponents(t)

	w := postJSON(t, srv.handleConfig, "/api/config", `{"loot_regex":"["}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid regex, got %d: %s", w.Code, w.Body.String())
	}
	if srv.Cfg.LootRegex != "" {
		t.Errorf("expected config unchanged on invalid regex, got %q", srv.Cfg.LootRegex)
	}
	if _, err := os.Stat(filepath.Join(home, ".gorgon-session", "config.json")); !os.IsNotExist(err) {
		t.Errorf("expected no config persisted after rejected patch")
	}
}

// Overlay patches replace the whole OverlaySettings struct (see
// applyConfigPatch); only the response-level opacity assertion matters here.
func TestHandleConfig_PostOverlayPatch(t *testing.T) {
	configHome(t)
	srv := liveComponents(t)

	w := postJSON(t, srv.handleConfig, "/api/config", `{"overlay":{"opacity":40}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	ov, ok := resp["overlay"].(map[string]any)
	if !ok {
		t.Fatalf("expected overlay object, got %T", resp["overlay"])
	}
	if ov["opacity"] != float64(40) {
		t.Errorf("expected overlay opacity 40, got %v", ov["opacity"])
	}
	if srv.Cfg.Overlay.Opacity != 40 {
		t.Errorf("expected live overlay opacity 40, got %d", srv.Cfg.Overlay.Opacity)
	}
}

func TestHandleConfig_PostSessionTemplates(t *testing.T) {
	configHome(t)
	srv := liveComponents(t)

	w := postJSON(t, srv.handleConfig, "/api/config",
		`{"session_templates":[{"name":"Serbule Farm","zone":"Serbule","notes":"kill wolves, loot hide"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	tmpls, ok := resp["session_templates"].([]any)
	if !ok || len(tmpls) != 1 {
		t.Fatalf("expected 1 session_template, got %#v", resp["session_templates"])
	}
	if len(srv.Cfg.SessionTemplates) != 1 || srv.Cfg.SessionTemplates[0].Name != "Serbule Farm" {
		t.Errorf("expected live template, got %#v", srv.Cfg.SessionTemplates)
	}
}

func TestHandleConfig_PostUnknownFieldRejected(t *testing.T) {
	configHome(t)
	srv := liveComponents(t)

	w := postJSON(t, srv.handleConfig, "/api/config", `{"bogus":1}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d: %s", w.Code, w.Body.String())
	}
}

// ---- handleImport ----

func TestHandleImport_AppliesLiveUpdates(t *testing.T) {
	home := configHome(t)
	srv := liveComponents(t)

	bundle := `{"config":{"chat_log_dir":"E:\\imported\\logs","player_log_path":"E:\\imported\\player.log","loot_regex":"\\[Status\\]\\s+LOOTED (.+?)!","player_prices":{"Runestone":2000}}}`
	w := postJSON(t, srv.handleImport, "/api/import", bundle)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if srv.Tailer.Dir != `E:\imported\logs` {
		t.Errorf("expected Tailer.Dir updated, got %q", srv.Tailer.Dir)
	}
	if srv.PLTailer.Path != `E:\imported\player.log` {
		t.Errorf("expected PLTailer.Path updated, got %q", srv.PLTailer.Path)
	}
	if ev := srv.Parser.ParseLine("[Status] LOOTED Sword!"); ev == nil || ev.ItemName != "Sword" {
		t.Errorf("expected parser regex updated via import, got %+v", ev)
	}
	if dec := srv.Favor.ResolveItem(cdn.Item{Name: "Runestone", Value: 900}); dec.PlayerPrice != 2000 {
		t.Errorf("expected favor player price 2000, got %v", dec.PlayerPrice)
	}

	data, err := os.ReadFile(filepath.Join(home, ".gorgon-session", "config.json"))
	if err != nil {
		t.Fatalf("expected config persisted after import: %v", err)
	}
	var saved config.Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("failed to parse saved config: %v", err)
	}
	if saved.PlayerLogPath != `E:\imported\player.log` {
		t.Errorf("expected persisted player_log_path, got %q", saved.PlayerLogPath)
	}
}

// ---- route planner / traders ----

func TestHandleRoutePlanner_ItemEchoAndParamValidation(t *testing.T) {
	srv := routePlannerFixtures(t)

	// item echoed back + routes always an array
	req := httptest.NewRequest("GET", "/api/route-planner?item=Iron%20Ore", nil)
	w := httptest.NewRecorder()
	srv.handleRoutePlanner(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Item   string      `json:"item"`
		Routes []RouteInfo `json:"routes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if resp.Item != "Iron Ore" {
		t.Errorf("expected item echoed, got %q", resp.Item)
	}
	if resp.Routes == nil {
		t.Error("expected routes array, got null")
	}

	// both item and trader -> 400
	w2 := httptest.NewRecorder()
	srv.handleRoutePlanner(w2, httptest.NewRequest("GET", "/api/route-planner?item=x&trader=Mira", nil))
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for item+trader, got %d", w2.Code)
	}

	// neither -> 400
	w3 := httptest.NewRecorder()
	srv.handleRoutePlanner(w3, httptest.NewRequest("GET", "/api/route-planner", nil))
	if w3.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for no params, got %d", w3.Code)
	}
}

func TestHandleTraders_Get(t *testing.T) {
	td := t.TempDir()
	traderMgr := trader.New(filepath.Join(td, "traders.json"))
	_ = traderMgr.Ensure("Mira", "Serbule", 7, 0)
	_ = traderMgr.UpdateLimit("Mira", 5000)

	srv := routePlannerFixtures(t)
	srv.Trader = traderMgr

	req := httptest.NewRequest("GET", "/api/traders", nil)
	w := httptest.NewRecorder()
	srv.handleTraders(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var areas []AreaTraders
	if err := json.Unmarshal(w.Body.Bytes(), &areas); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	found := false
	for _, a := range areas {
		for _, npc := range a.NPCs {
			if npc.NPCName == "Mira" {
				found = true
				if npc.WeeklyLimit != 5000 {
					t.Errorf("expected weekly limit 5000, got %v", npc.WeeklyLimit)
				}
				if !npc.UnusedWarning {
					t.Errorf("expected unused warning at 0/5000 sold (>50%% capacity unused), got %+v", npc)
				}
			}
		}
	}
	if !found {
		t.Errorf("expected Mira in trader list, got %+v", areas)
	}
}

func TestHandleTraders_GetNilTrader(t *testing.T) {
	srv := &Server{}
	req := httptest.NewRequest("GET", "/api/traders", nil)
	w := httptest.NewRecorder()
	srv.handleTraders(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("expected empty array, got %s", w.Body.String())
	}
}

// ---- data endpoints ----

func TestHandleItems(t *testing.T) {
	srv := &Server{ItemByID: map[int]cdn.Item{
		1: {ItemID: 1, Name: "Iron Ore"},
		2: {ItemID: 2, Name: "Coal"},
	}}
	req := httptest.NewRequest("GET", "/api/items", nil)
	w := httptest.NewRecorder()
	srv.handleItems(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var items []cdn.Item
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
}

func TestHandleDropRates_EmptyDir(t *testing.T) {
	srv := &Server{Cfg: config.Config{ReportDir: t.TempDir()}, Sess: session.New()}
	req := httptest.NewRequest("GET", "/api/drop-rates", nil)
	w := httptest.NewRecorder()
	srv.handleDropRates(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []any `json:"items"`
		Zones []any `json:"zones"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if resp.Items == nil || resp.Zones == nil {
		t.Errorf("expected empty arrays, got items=%v zones=%v", resp.Items, resp.Zones)
	}
}

func TestHandleZoneNPCs_NoZone(t *testing.T) {
	srv := &Server{Sess: session.New()}
	req := httptest.NewRequest("GET", "/api/zone-npcs", nil)
	w := httptest.NewRecorder()
	srv.handleZoneNPCs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("expected empty array, got %s", w.Body.String())
	}
}

func TestHandlePrices_Nil(t *testing.T) {
	srv := &Server{}
	req := httptest.NewRequest("GET", "/api/prices", nil)
	w := httptest.NewRecorder()
	srv.handlePrices(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if strings.TrimSpace(w.Body.String()) != "{}" {
		t.Errorf("expected empty object, got %s", w.Body.String())
	}

	req2 := httptest.NewRequest("GET", "/api/prices/Runestone", nil)
	w2 := httptest.NewRecorder()
	srv.handlePriceByName(w2, req2)
	if w2.Code != http.StatusOK || strings.TrimSpace(w2.Body.String()) != "[]" {
		t.Errorf("expected empty array, got %d %s", w2.Code, w2.Body.String())
	}

	req3 := httptest.NewRequest("GET", "/api/prices/trends", nil)
	w3 := httptest.NewRecorder()
	srv.handlePriceTrends(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Errorf("expected 400 without name, got %d", w3.Code)
	}
}

func TestHandleMetaEndpoints(t *testing.T) {
	srv := &Server{
		Favor: favor.FromNpcs(cdn.NpcsFile{}),
		Areas: cdn.AreaIndex{ByInternal: map[string]cdn.Area{
			"AreaSerbule": {FriendlyName: "Serbule"},
		}},
		Skills:  cdn.SkillsFile{},
		Recipes: cdn.RecipesFile{},
	}

	req := httptest.NewRequest("GET", "/api/areas", nil)
	w := httptest.NewRecorder()
	srv.handleAreas(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Serbule") {
		t.Errorf("expected areas with Serbule, got %d %s", w.Code, w.Body.String())
	}

	handlers := []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"/api/skills", srv.handleSkills},
		{"/api/recipes", srv.handleRecipes},
		{"/api/npcs", srv.handleNPCs},
	}
	for _, h := range handlers {
		w := httptest.NewRecorder()
		h.handler(w, httptest.NewRequest("GET", h.path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for %s, got %d", h.path, w.Code)
		}
	}
}

// ---- sessions lifecycle ----

func TestHandleStopAndSessionByID(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := session.New()
	if err := mgr.Start("Test Dungeon", "notes"); err != nil {
		t.Fatalf("session start: %v", err)
	}
	mgr.AddLoot(session.LootEntry{
		Name: "Iron Ore", Valor: 5, Count: 2,
		FirstSeen: mgr.Snapshot().StartedAt, LastSeen: mgr.Snapshot().StartedAt,
		Decision: favor.Decision{Item: "Iron Ore", Verdict: favor.VerdictSellVendor},
	})

	srv := &Server{Cfg: config.Config{ReportDir: tmpDir}, Sess: mgr}

	w := postJSON(t, srv.handleStop, "/api/session/stop", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from stop, got %d: %s", w.Code, w.Body.String())
	}

	// appears in /api/sessions
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	w2 := httptest.NewRecorder()
	srv.handleSessions(w2, req)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 from sessions, got %d", w2.Code)
	}
	var summaries []SessionSummary
	if err := json.Unmarshal(w2.Body.Bytes(), &summaries); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 session summary, got %d", len(summaries))
	}
	id := summaries[0].ID

	// report fetchable by id
	req3 := httptest.NewRequest("GET", "/api/session/"+id, nil)
	w3 := httptest.NewRecorder()
	srv.handleSessionByID(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200 from session by id, got %d", w3.Code)
	}
	var snap session.Snapshot
	if err := json.Unmarshal(w3.Body.Bytes(), &snap); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if snap.Dungeon != "Test Dungeon" || len(snap.Loot) != 1 {
		t.Errorf("unexpected snapshot: dungeon=%q loot=%d", snap.Dungeon, len(snap.Loot))
	}

	// unknown id -> 404
	req4 := httptest.NewRequest("GET", "/api/session/session-20990101-000000", nil)
	w4 := httptest.NewRecorder()
	srv.handleSessionByID(w4, req4)
	if w4.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown session, got %d", w4.Code)
	}
}

func TestHandleSession_PatchNotes(t *testing.T) {
	mgr := session.New()
	if err := mgr.Start("Test Dungeon", ""); err != nil {
		t.Fatalf("session start: %v", err)
	}
	srv := &Server{Sess: mgr}

	req := httptest.NewRequest("PATCH", "/api/session", strings.NewReader(`{"notes":"updated"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var snap session.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if snap.Notes != "updated" {
		t.Errorf("expected notes updated, got %q", snap.Notes)
	}

	w2 := httptest.NewRecorder()
	srv.handleSession(w2, httptest.NewRequest("GET", "/api/session", nil))
	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 from GET session, got %d", w2.Code)
	}
}

func TestHandleSession_FavorTargetDistances(t *testing.T) {
	mgr := session.New()
	if err := mgr.Start("Test", ""); err != nil {
		t.Fatalf("session start: %v", err)
	}
	mgr.SetZone("Eltibule")
	started := mgr.Snapshot().StartedAt
	mgr.AddLoot(session.LootEntry{
		Name: "Meat", Valor: 5, Count: 1,
		FirstSeen: started, LastSeen: started,
		Decision: favor.Decision{
			Item: "Meat", Verdict: favor.VerdictFavor,
			FavorTargets: []favor.Target{
				{NPC: "Same Zone", Area: "Eltibule", Score: 1},
				{NPC: "Far Away", Area: "Vidaria", Score: 1},
				{NPC: "Unknown Zone", Area: "Nowhere Land", Score: 1},
			},
		},
	})
	srv := &Server{Sess: mgr}

	w := httptest.NewRecorder()
	srv.handleSession(w, httptest.NewRequest("GET", "/api/session", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var snap session.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(snap.Loot) != 1 || snap.Zone != "Eltibule" {
		t.Fatalf("unexpected snapshot: zone=%q loot=%d", snap.Zone, len(snap.Loot))
	}
	byNPC := map[string]favor.Target{}
	for _, tg := range snap.Loot[0].Decision.FavorTargets {
		byNPC[tg.NPC] = tg
	}

	// Eltibule (200,450) -> Eltibule = 0 km
	if d := byNPC["Same Zone"].DistanceKm; d == nil || *d != 0 {
		t.Errorf("expected same-zone target at 0 km, got %v", d)
	}
	// Eltibule (200,450) -> Vidaria (300,920) ≈ 480.5 km
	if d := byNPC["Far Away"].DistanceKm; d == nil || *d <= 480 || *d >= 481 {
		t.Errorf("expected Vidaria target ~480.5 km, got %v", d)
	}
	// an area with no coords in the fallback table -> distance stays nil
	if d := byNPC["Unknown Zone"].DistanceKm; d != nil {
		t.Errorf("expected unknown-area target to have no distance, got %v", *d)
	}

	// live session state must not be mutated by response enrichment
	for _, tg := range mgr.Snapshot().Loot[0].Decision.FavorTargets {
		if tg.DistanceKm != nil {
			t.Errorf("live session target %q mutated with distance %v", tg.NPC, *tg.DistanceKm)
		}
	}
}

func TestHandleLoot_CRUD(t *testing.T) {
	srv := routePlannerFixtures(t)

	// POST manual entry (resolved through favor engine)
	w := postJSON(t, srv.handleLoot, "/api/loot", `{"name":"Maple Wood","value":100,"count":2}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// GET returns the session loot
	w2 := httptest.NewRecorder()
	srv.handleLoot(w2, httptest.NewRequest("GET", "/api/loot", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w2.Code)
	}
	var lootEntries []session.LootEntry
	if err := json.Unmarshal(w2.Body.Bytes(), &lootEntries); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(lootEntries) == 0 {
		t.Fatal("expected loot entries")
	}

	// DELETE by name
	w3 := httptest.NewRecorder()
	srv.handleLoot(w3, httptest.NewRequest("DELETE", "/api/loot?name=Maple%20Wood", nil))
	if w3.Code != http.StatusOK {
		t.Errorf("expected 200 from delete, got %d", w3.Code)
	}
}

func TestHandleSessionsCompare(t *testing.T) {
	tmpDir := t.TempDir()
	for _, id := range []string{"session-20240101-120000", "session-20240101-130000"} {
		data, _ := json.Marshal(session.Snapshot{State: session.Stopped, Dungeon: "D"})
		_ = os.WriteFile(filepath.Join(tmpDir, id+".json"), data, 0644)
	}
	srv := &Server{Cfg: config.Config{ReportDir: tmpDir}, Sess: session.New()}

	req := httptest.NewRequest("GET", "/api/sessions/compare?a=session-20240101-120000&b=session-20240101-130000", nil)
	w := httptest.NewRecorder()
	srv.handleSessionsCompare(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Diff map[string]float64 `json:"diff"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if resp.Diff["kills"] != 0 {
		t.Errorf("expected 0 kills diff, got %v", resp.Diff)
	}

	// missing params -> 400
	w2 := httptest.NewRecorder()
	srv.handleSessionsCompare(w2, httptest.NewRequest("GET", "/api/sessions/compare", nil))
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 without params, got %d", w2.Code)
	}
}

// ---- static / feed ----

func TestHandleStatic(t *testing.T) {
	webFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html><head><title>Gorgon Session</title></head></html>")},
		"app.js":     &fstest.MapFile{Data: []byte("console.log(1);")},
		"style.css":  &fstest.MapFile{Data: []byte("body {}")},
	}
	srv := &Server{WebFS: webFS}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.handleStatic(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<title>") {
		t.Errorf("expected index.html content, got %q", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("expected html content type, got %q", ct)
	}
	if ct := w.Header().Get("X-Content-Type-Options"); ct != "nosniff" {
		t.Errorf("expected nosniff header, got %q", ct)
	}

	// js / css content types
	wJS := httptest.NewRecorder()
	srv.handleStatic(wJS, httptest.NewRequest("GET", "/app.js", nil))
	if ct := wJS.Header().Get("Content-Type"); ct != "application/javascript; charset=utf-8" {
		t.Errorf("expected js content type, got %q", ct)
	}
	wCSS := httptest.NewRecorder()
	srv.handleStatic(wCSS, httptest.NewRequest("GET", "/style.css", nil))
	if ct := wCSS.Header().Get("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Errorf("expected css content type, got %q", ct)
	}

	// unknown path -> 404
	w2 := httptest.NewRecorder()
	srv.handleStatic(w2, httptest.NewRequest("GET", "/nope.txt", nil))
	if w2.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown asset, got %d", w2.Code)
	}
}

func TestHandleFeed_MethodNotAllowed(t *testing.T) {
	srv := &Server{}
	req := httptest.NewRequest("POST", "/api/feed", nil)
	w := httptest.NewRecorder()
	srv.handleFeed(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleExport(t *testing.T) {
	srv := &Server{Cfg: config.Config{HTTPAddr: "127.0.0.1:7777"}}
	req := httptest.NewRequest("GET", "/api/export", nil)
	w := httptest.NewRecorder()
	srv.handleExport(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "gorgon-session-export.json") {
		t.Errorf("expected export filename in Content-Disposition, got %q", cd)
	}
	var resp struct {
		Version string        `json:"version"`
		Config  config.Config `json:"config"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if resp.Version != "1" || resp.Config.HTTPAddr != "127.0.0.1:7777" {
		t.Errorf("unexpected export payload: version=%q config=%+v", resp.Version, resp.Config)
	}
}

func TestHandleStart(t *testing.T) {
	mgr := session.New()
	srv := &Server{Sess: mgr}

	w := postJSON(t, srv.handleStart, "/api/session/start", `{"dungeon":"Test Dungeon"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var snap session.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if snap.State != session.Running || snap.Dungeon != "Test Dungeon" {
		t.Errorf("expected running session, got %+v", snap)
	}

	// second start while running -> conflict
	w2 := postJSON(t, srv.handleStart, "/api/session/start", `{"dungeon":"Again"}`)
	if w2.Code != http.StatusConflict {
		t.Errorf("expected 409 for double start, got %d", w2.Code)
	}
}

func TestHandleOverlay(t *testing.T) {
	webFS := fstest.MapFS{"overlay.html": &fstest.MapFile{Data: []byte("<html>overlay</html>")}}
	srv := &Server{WebFS: webFS}
	req := httptest.NewRequest("GET", "/overlay", nil)
	w := httptest.NewRecorder()
	srv.handleOverlay(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "overlay") {
		t.Errorf("expected overlay.html content, got %q", w.Body.String())
	}

	// missing overlay.html -> 404
	srv2 := &Server{WebFS: fstest.MapFS{}}
	w2 := httptest.NewRecorder()
	srv2.handleOverlay(w2, httptest.NewRequest("GET", "/overlay", nil))
	if w2.Code != http.StatusNotFound {
		t.Errorf("expected 404 without overlay.html, got %d", w2.Code)
	}
}

func TestHandleTraderHistory_WithEvents(t *testing.T) {
	td := t.TempDir()
	traderMgr := trader.New(filepath.Join(td, "traders.json"))
	_ = traderMgr.Ensure("Merchant", "Serbule", 0, 0) // past refresh window
	_ = traderMgr.LogSale("Merchant", 100)            // triggers a refresh event

	srv := &Server{Cfg: config.Config{}, Trader: traderMgr}
	req := httptest.NewRequest("GET", "/api/traders/history?npc=Merchant", nil)
	w := httptest.NewRecorder()
	srv.handleTraderHistory(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var events []any
	if err := json.Unmarshal(w.Body.Bytes(), &events); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 refresh event, got %d", len(events))
	}
}

func TestHandleLootNote(t *testing.T) {
	mgr := session.New()
	_ = mgr.Start("Test Dungeon", "")
	mgr.AddLoot(session.LootEntry{Name: "Sword", Valor: 10, Count: 1})
	srv := &Server{Sess: mgr}

	req := httptest.NewRequest("PATCH", "/api/loot-note", strings.NewReader(`{"name":"Sword","note":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleLootNote(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// missing name -> 400
	w2 := httptest.NewRecorder()
	srv.handleLootNote(w2, httptest.NewRequest("PATCH", "/api/loot-note", strings.NewReader(`{"note":"hi"}`)))
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 without name, got %d", w2.Code)
	}
}

func TestHandleNPCs_WithFavor(t *testing.T) {
	srv := routePlannerFixtures(t)
	req := httptest.NewRequest("GET", "/api/npcs", nil)
	w := httptest.NewRecorder()
	srv.handleNPCs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var npcs []favor.NPCInfo
	if err := json.Unmarshal(w.Body.Bytes(), &npcs); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	found := false
	for _, n := range npcs {
		if n.Name == "Mira" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Mira in NPC list, got %+v", npcs)
	}
}

// ---- misc handlers ----

func TestHandleNotesExport_Empty(t *testing.T) {
	srv := &Server{Cfg: config.Config{ReportDir: t.TempDir()}, Sess: session.New()}
	req := httptest.NewRequest("GET", "/api/notes/export", nil)
	w := httptest.NewRecorder()
	srv.handleNotesExport(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("expected text/plain, got %q", ct)
	}
}

func TestHandleTraderMeta_Nil(t *testing.T) {
	srv := &Server{}

	w := httptest.NewRecorder()
	srv.handleTraderHistory(w, httptest.NewRequest("GET", "/api/traders/history", nil))
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("expected empty history, got %d %s", w.Code, w.Body.String())
	}

	w2 := httptest.NewRecorder()
	srv.handleTraderSchedule(w2, httptest.NewRequest("GET", "/api/traders/schedule", nil))
	if w2.Code != http.StatusOK || strings.TrimSpace(w2.Body.String()) != "[]" {
		t.Errorf("expected empty schedule, got %d %s", w2.Code, w2.Body.String())
	}

	// delete without id -> 400
	w3 := httptest.NewRecorder()
	srv.handleTraderHistoryDelete(w3, httptest.NewRequest("POST", "/api/traders/history/delete", strings.NewReader(`{"id":""}`)))
	if w3.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing id, got %d", w3.Code)
	}
}
