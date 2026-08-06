package server

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michalbasisty/gorgon-session/internal/cdn"
	"github.com/michalbasisty/gorgon-session/internal/config"
	"github.com/michalbasisty/gorgon-session/internal/favor"
	"github.com/michalbasisty/gorgon-session/internal/session"
	"github.com/michalbasisty/gorgon-session/internal/trader"
)

func TestHandleSessionExport_NotFound(t *testing.T) {
	cfg := config.Config{ReportDir: t.TempDir()}
	srv := &Server{Cfg: cfg}

	req := httptest.NewRequest("GET", "/api/session/session-20990101-000000/export", nil)
	w := httptest.NewRecorder()

	srv.handleSessionByID(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleSessionExport_NoReportDir(t *testing.T) {
	cfg := config.Config{ReportDir: ""}
	srv := &Server{Cfg: cfg}

	req := httptest.NewRequest("GET", "/api/session/session-20240101-120000/export", nil)
	w := httptest.NewRecorder()

	srv.handleSessionByID(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleSessionByID_InvalidID(t *testing.T) {
	cfg := config.Config{ReportDir: t.TempDir()}
	srv := &Server{Cfg: cfg}

	req := httptest.NewRequest("GET", "/api/session/../../evil", nil)
	w := httptest.NewRecorder()

	srv.handleSessionByID(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSessionExport_InvalidID(t *testing.T) {
	cfg := config.Config{ReportDir: t.TempDir()}
	srv := &Server{Cfg: cfg}

	req := httptest.NewRequest("GET", "/api/session/../../evil/export", nil)
	w := httptest.NewRecorder()

	srv.handleSessionByID(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSessionExport_EmptySession(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Config{ReportDir: tmpDir}
	srv := &Server{Cfg: cfg}

	// Create an empty session report
	snapshot := session.Snapshot{
		State:     session.Stopped,
		Dungeon:   "Test Dungeon",
		StartedAt: time.Now().Add(-1 * time.Hour),
		EndedAt:   time.Now(),
		Loot:      []session.LootEntry{},
	}
	data, _ := json.Marshal(snapshot)
	os.WriteFile(filepath.Join(tmpDir, "session-20240101-120000.json"), data, 0644)

	req := httptest.NewRequest("GET", "/api/session/session-20240101-120000/export", nil)
	w := httptest.NewRecorder()

	srv.handleSessionByID(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Check CSV has header only
	reader := csv.NewReader(strings.NewReader(w.Body.String()))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to parse CSV: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 row (header only), got %d", len(records))
	}
}

func TestHandleSessionExport_WithLoot(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Config{ReportDir: tmpDir}
	srv := &Server{Cfg: cfg}

	// Create a session with loot
	snapshot := session.Snapshot{
		State:     session.Stopped,
		Dungeon:   "Test Dungeon",
		StartedAt: time.Now().Add(-1 * time.Hour),
		EndedAt:   time.Now(),
		Loot: []session.LootEntry{
			{
				Name:      "Magic Sword",
				ItemID:    123,
				Valor:     100,
				Count:     1,
				Bonus:     false,
				FirstSeen: time.Now(),
				LastSeen:  time.Now(),
				Decision: favor.Decision{
					Item:    "Magic Sword",
					Verdict: favor.VerdictFavor,
				},
			},
			{
				Name:      "Gold Coin",
				ItemID:    456,
				Valor:     50,
				Count:     10,
				Bonus:     true,
				FirstSeen: time.Now(),
				LastSeen:  time.Now(),
				Decision: favor.Decision{
					Item:       "Gold Coin",
					Verdict:    favor.VerdictSellVendor,
					SellReason: "Vendor (value 50)",
				},
			},
		},
	}
	data, _ := json.Marshal(snapshot)
	os.WriteFile(filepath.Join(tmpDir, "session-20240101-120000.json"), data, 0644)

	req := httptest.NewRequest("GET", "/api/session/session-20240101-120000/export", nil)
	w := httptest.NewRecorder()

	srv.handleSessionByID(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Check CSV content
	reader := csv.NewReader(strings.NewReader(w.Body.String()))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to parse CSV: %v", err)
	}
	if len(records) != 3 {
		t.Errorf("expected 3 rows (header + 2 items), got %d", len(records))
	}

	// Check header
	if records[0][0] != "name" {
		t.Errorf("expected header 'name', got %s", records[0][0])
	}

	// Check first item
	if records[1][0] != "Magic Sword" {
		t.Errorf("expected 'Magic Sword', got %s", records[1][0])
	}
	if records[1][7] != "favor" {
		t.Errorf("expected verdict 'favor', got %s", records[1][7])
	}

	// Check second item
	if records[2][0] != "Gold Coin" {
		t.Errorf("expected 'Gold Coin', got %s", records[2][0])
	}
	if records[2][3] != "10" {
		t.Errorf("expected count '10', got %s", records[2][3])
	}
}

func TestHandleSessionExport_SpecialCharacters(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Config{ReportDir: tmpDir}
	srv := &Server{Cfg: cfg}

	// Create a session with special characters in item names
	snapshot := session.Snapshot{
		State:     session.Stopped,
		Dungeon:   "Test Dungeon",
		StartedAt: time.Now().Add(-1 * time.Hour),
		EndedAt:   time.Now(),
		Loot: []session.LootEntry{
			{
				Name:      `Item with "quotes"`,
				ItemID:    123,
				Valor:     100,
				Count:     1,
				FirstSeen: time.Now(),
				LastSeen:  time.Now(),
				Decision: favor.Decision{
					Item:    `Item with "quotes"`,
					Verdict: favor.VerdictSellVendor,
				},
			},
			{
				Name:      "Item, with comma",
				ItemID:    456,
				Valor:     50,
				Count:     1,
				FirstSeen: time.Now(),
				LastSeen:  time.Now(),
				Decision: favor.Decision{
					Item:    "Item, with comma",
					Verdict: favor.VerdictSellVendor,
				},
			},
		},
	}
	data, _ := json.Marshal(snapshot)
	os.WriteFile(filepath.Join(tmpDir, "session-20240101-120000.json"), data, 0644)

	req := httptest.NewRequest("GET", "/api/session/session-20240101-120000/export", nil)
	w := httptest.NewRecorder()

	srv.handleSessionByID(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Check CSV can be parsed (special chars properly escaped)
	reader := csv.NewReader(strings.NewReader(w.Body.String()))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to parse CSV: %v", err)
	}
	if len(records) != 3 {
		t.Errorf("expected 3 rows, got %d", len(records))
	}

	// Check special characters are preserved
	if records[1][0] != `Item with "quotes"` {
		t.Errorf("expected quotes preserved, got %s", records[1][0])
	}
	if records[2][0] != "Item, with comma" {
		t.Errorf("expected comma preserved, got %s", records[2][0])
	}
}

func TestHandleSessions(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Config{ReportDir: tmpDir}
	srv := &Server{Cfg: cfg}

	// Create two session snapshot files
	now := time.Now()
	snapshots := []session.Snapshot{
		{
			State:     session.Stopped,
			Dungeon:   "Ancient Forest",
			StartedAt: now.Add(-2 * time.Hour),
			EndedAt:   now.Add(-1 * time.Hour),
			Loot: []session.LootEntry{
				{Name: "Sword", Valor: 100, Count: 1, Decision: favor.Decision{Verdict: favor.VerdictFavor}},
				{Name: "Gold", Valor: 50, Count: 5, Decision: favor.Decision{Verdict: favor.VerdictSellVendor}},
			},
		},
		{
			State:     session.Stopped,
			Dungeon:   "Crystal Cave",
			StartedAt: now.Add(-5 * time.Hour),
			EndedAt:   now.Add(-4 * time.Hour),
			Loot: []session.LootEntry{
				{Name: "Gem", Valor: 200, Count: 1, Decision: favor.Decision{Verdict: favor.VerdictKeep}},
			},
		},
	}

	for i, snap := range snapshots {
		data, _ := json.Marshal(snap)
		os.WriteFile(filepath.Join(tmpDir, fmt.Sprintf("session-%s-%s.json", snap.Dungeon, fmt.Sprintf("%03d", i))), data, 0644)
	}

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	w := httptest.NewRecorder()
	srv.handleSessions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var sessions []SessionSummary
	if err := json.Unmarshal(w.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}

	// Check fields on first session (recently started)
	if sessions[0].Dungeon != snapshots[0].Dungeon {
		t.Errorf("expected dungeon %q, got %q", snapshots[0].Dungeon, sessions[0].Dungeon)
	}
	if sessions[0].TotalItems != 6 {
		t.Errorf("expected 6 total items, got %d", sessions[0].TotalItems)
	}
	if sessions[0].UniqueItems != 2 {
		t.Errorf("expected 2 unique items, got %d", sessions[0].UniqueItems)
	}
	if sessions[0].TotalValue != 350 {
		t.Errorf("expected total value 350, got %f", sessions[0].TotalValue)
	}
	if sessions[0].FavorItems != 1 {
		t.Errorf("expected 1 favor item, got %d", sessions[0].FavorItems)
	}
	if sessions[0].SellItems != 1 {
		t.Errorf("expected 1 sell item, got %d", sessions[0].SellItems)
	}
}

func TestHandleSessions_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Config{ReportDir: tmpDir}
	srv := &Server{Cfg: cfg}

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	w := httptest.NewRecorder()
	srv.handleSessions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var sessions []SessionSummary
	if err := json.Unmarshal(w.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}

	// Verify it's an empty array, not null
	body := strings.TrimSpace(w.Body.String())
	if body != "[]" && body != "null" {
		t.Errorf("expected empty array, got %s", body)
	}
}

func TestHandleSessions_NoReportDir(t *testing.T) {
	cfg := config.Config{ReportDir: ""}
	srv := &Server{Cfg: cfg}

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	w := httptest.NewRecorder()
	srv.handleSessions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Should return empty array when no report dir configured
	var sessions []SessionSummary
	if err := json.Unmarshal(w.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestRoutePlannerMatch(t *testing.T) {
	cases := []struct {
		item     string
		npcName  string
		keywords []string
		want     bool
	}{
		{"Maple Wood", "The Wombat", []string{"Wooden", "CorpseTrophy"}, true},  // wood ⊂ wooden (prefix)
		{"Amazing Leather Roll", "Christina Fells", []string{"LeatherArmor"}, true}, // leather == leather, camel split
		{"Crushproof Knight's Helm", "Pegast", []string{"Armor", "Weapon"}, false},  // honest non-match
		{"Maple Wood", "Joeh", []string{"Food"}, false},
		{"wood", "The Wombat", []string{"Wooden"}, true}, // bare keyword query
		{"Fainor", "Fainor", []string{}, true},           // NPC name match, no keywords
		{"Maple Wood", "Joeh", []string{}, false},        // empty keywords don't match everything
		{"dagger", "Some NPC", []string{"Small Weapons"}, false}, // no equal/prefix/suffix overlap
	}
	for _, c := range cases {
		if got := routePlannerMatch(c.item, c.npcName, c.keywords); got != c.want {
			t.Errorf("routePlannerMatch(%q, %q, %v) = %v, want %v", c.item, c.npcName, c.keywords, got, c.want)
		}
	}
}

func TestHandleTraderPost_UpdateLimit(t *testing.T) {
	td := t.TempDir()
	traderMgr := trader.New(filepath.Join(td, "traders.json"))

	srv := &Server{
		Cfg:    config.Config{},
		Trader: traderMgr,
	}

	// Create a trader via POST
	body := `{"npc_name":"Test Merchant","area":"Test Area","weekly_limit":5000,"reset_days":7,"reset_hours":0}`
	req := httptest.NewRequest("POST", "/api/traders", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleTraders(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the trader was saved
	traders := traderMgr.GetAll()
	if len(traders) != 1 {
		t.Errorf("expected 1 trader, got %d", len(traders))
	}
	if traders[0].NPCName != "Test Merchant" {
		t.Errorf("expected 'Test Merchant', got %s", traders[0].NPCName)
	}
	if traders[0].WeeklyLimit != 5000 {
		t.Errorf("expected limit 5000, got %f", traders[0].WeeklyLimit)
	}
}

func TestHandleTraderPost_LogSale(t *testing.T) {
	td := t.TempDir()
	traderMgr := trader.New(filepath.Join(td, "traders.json"))

	// Create a trader first
	traderMgr.Ensure("Merchant", "Area", 5, 22)
	traderMgr.UpdateLimit("Merchant", 10000)

	srv := &Server{
		Cfg:    config.Config{},
		Trader: traderMgr,
	}

	// Log a sale
	body := `{"npc_name":"Merchant","amount":2500}`
	req := httptest.NewRequest("POST", "/api/traders", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleTraders(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify sale was logged
	traders := traderMgr.GetAll()
	if len(traders) != 1 {
		t.Errorf("expected 1 trader, got %d", len(traders))
	}
	if traders[0].SoldThisWeek != 2500 {
		t.Errorf("expected sold 2500, got %f", traders[0].SoldThisWeek)
	}
}

func TestHandleBulkExport_InvalidID(t *testing.T) {
	cfg := config.Config{ReportDir: t.TempDir()}
	srv := &Server{Cfg: cfg}

	body := `{"ids":["../evil"]}`
	req := httptest.NewRequest("POST", "/api/sessions/bulk-export", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleBulkExport(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSessionExport_AllVerdicts(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Config{ReportDir: tmpDir}
	srv := &Server{Cfg: cfg}

	// Create a session with all verdict types
	snapshot := session.Snapshot{
		State:     session.Stopped,
		Dungeon:   "Test Dungeon",
		StartedAt: time.Now().Add(-1 * time.Hour),
		EndedAt:   time.Now(),
		Loot: []session.LootEntry{
			{
				Name:     "Favor Item",
				Decision: favor.Decision{Verdict: favor.VerdictFavor},
			},
			{
				Name:     "Vendor Item",
				Decision: favor.Decision{Verdict: favor.VerdictSellVendor},
			},
			{
				Name:     "Consignment Item",
				Decision: favor.Decision{Verdict: favor.VerdictSellConsignment},
			},
			{
				Name:     "Keep Item",
				Decision: favor.Decision{Verdict: favor.VerdictKeep},
			},
		},
	}
	data, _ := json.Marshal(snapshot)
	os.WriteFile(filepath.Join(tmpDir, "session-20240101-120000.json"), data, 0644)

	req := httptest.NewRequest("GET", "/api/session/session-20240101-120000/export", nil)
	w := httptest.NewRecorder()

	srv.handleSessionByID(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Check all verdicts are present
	reader := csv.NewReader(strings.NewReader(w.Body.String()))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to parse CSV: %v", err)
	}
	if len(records) != 5 {
		t.Errorf("expected 5 rows (header + 4 items), got %d", len(records))
	}

	verdicts := map[string]bool{
		records[1][7]: true,
		records[2][7]: true,
		records[3][7]: true,
		records[4][7]: true,
	}
	if !verdicts["favor"] || !verdicts["sell_vendor"] || !verdicts["sell_consignment"] || !verdicts["keep"] {
		t.Errorf("not all verdicts present: %v", verdicts)
	}
}

func TestApplyConfigPatch_MergesNotReplaces(t *testing.T) {
	cfg := config.Config{
		HTTPAddr:              "127.0.0.1:9999",
		ChatLogDir:            `C:\logs`,
		LootRegex:             `custom`,
		SellValueThreshold:    50,
		NotificationThreshold: 500,
		BackupEnabled:         true,
		PlayerPrices:          map[string]float64{"Runestone": 900},
	}
	threshold := 99.5
	patched := applyConfigPatch(cfg, configPatch{SellValueThreshold: &threshold})

	if patched.SellValueThreshold != 99.5 {
		t.Errorf("expected merged threshold 99.5, got %v", patched.SellValueThreshold)
	}
	if patched.ChatLogDir != `C:\logs` {
		t.Errorf("expected chat_log_dir preserved, got %q", patched.ChatLogDir)
	}
	if patched.HTTPAddr != "127.0.0.1:9999" {
		t.Errorf("expected http_addr preserved, got %q", patched.HTTPAddr)
	}
	if !patched.BackupEnabled {
		t.Error("expected backup_enabled preserved")
	}
	if patched.LootRegex != "custom" {
		t.Errorf("expected loot_regex preserved, got %q", patched.LootRegex)
	}
	if patched.PlayerPrices["Runestone"] != 900 {
		t.Errorf("expected player_prices preserved, got %v", patched.PlayerPrices)
	}
	// nil patch pointer leaves everything untouched
	same := applyConfigPatch(cfg, configPatch{})
	if same.ChatLogDir != cfg.ChatLogDir || same.SellValueThreshold != cfg.SellValueThreshold {
		t.Error("empty patch must not change config")
	}
}

func TestApplyConfigPatch_Overlay(t *testing.T) {
	cfg := config.Config{Overlay: config.Default().Overlay}
	patched := applyConfigPatch(cfg, configPatch{Overlay: &config.OverlaySettings{
		Opacity:              40,
		ClickThroughOpacity:  35,
		ClickThroughByDefault: true,
		Position:             "top-left",
		Theme:                "light",
		AccentColor:          "#FF0000",
	}})
	if patched.Overlay.Opacity != 40 || patched.Overlay.Position != "top-left" || patched.Overlay.Theme != "light" {
		t.Errorf("overlay patch not applied: %+v", patched.Overlay)
	}
	// nil patch leaves overlay untouched
	if same := applyConfigPatch(cfg, configPatch{}); same.Overlay.Opacity != 98 {
		t.Errorf("empty patch must keep overlay defaults, got %+v", same.Overlay)
	}
}

func TestConfigPayload_IncludesOverlay(t *testing.T) {
	p := configPayload(config.Default())
	ov, ok := p["overlay"].(config.OverlaySettings)
	if !ok {
		t.Fatalf("expected overlay section in payload, got %T", p["overlay"])
	}
	if ov.Opacity != 98 || ov.ClickThroughOpacity != 78 || ov.Position != "bottom-right" {
		t.Errorf("unexpected overlay payload: %+v", ov)
	}
}

func TestHandleTraderHistoryExport(t *testing.T) {
	td := t.TempDir()
	traderMgr := trader.New(filepath.Join(td, "traders.json"))
	_ = traderMgr.Ensure("Merchant", "Serbule", 0, 0) // past refresh window
	_ = traderMgr.LogSale("Merchant", 100)            // triggers a refresh event

	srv := &Server{Cfg: config.Config{}, Trader: traderMgr}
	req := httptest.NewRequest("GET", "/api/traders/history/export", nil)
	w := httptest.NewRecorder()
	srv.handleTraderHistoryExport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/csv" {
		t.Errorf("expected text/csv, got %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "traders-history.csv") {
		t.Errorf("expected filename in Content-Disposition, got %q", cd)
	}
	records, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("failed to parse CSV: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected header + 1 row, got %d", len(records))
	}
	wantHeader := []string{"time", "npc", "item", "qty", "price", "total", "action"}
	for i, h := range wantHeader {
		if records[0][i] != h {
			t.Errorf("header col %d = %q, want %q", i, records[0][i], h)
		}
	}
	if records[1][1] != "Merchant" {
		t.Errorf("expected npc Merchant, got %q", records[1][1])
	}
	if records[1][6] != "reset" {
		t.Errorf("expected action reset, got %q", records[1][6])
	}
}

func TestHandleCraftingProfit(t *testing.T) {
	items := map[int]cdn.Item{
		1: {ItemID: 1, Name: "Iron Ore", Value: 5},
		2: {ItemID: 2, Name: "Coal", Value: 3},
		3: {ItemID: 3, Name: "Iron Bar", Value: 20},
	}
	recipes := cdn.RecipesFile{
		"recipe_1": {Name: "Smelt Iron Bar", Skill: "Blacksmithing", SkillLevelReq: 10,
			Ingredients: []cdn.RecipeIngredient{{ItemCode: 1, StackSize: 2}, {ItemCode: 2, StackSize: 1}},
			ResultItems: []cdn.RecipeResultItem{{ItemCode: 3, StackSize: 1}}},
		"recipe_2": {Name: "Mystery Craft", Skill: "Alchemy", SkillLevelReq: 5,
			Ingredients: []cdn.RecipeIngredient{{ItemCode: 999, StackSize: 1}},
			ResultItems: []cdn.RecipeResultItem{{ItemCode: 3, StackSize: 1}}},
		"recipe_3": {Name: "Expensive Craft", Skill: "Blacksmithing", SkillLevelReq: 50,
			Ingredients: []cdn.RecipeIngredient{{ItemCode: 1, StackSize: 1}},
			ResultItems: []cdn.RecipeResultItem{{ItemCode: 1, StackSize: 1}}},
	}
	srv := &Server{Recipes: recipes, ItemByID: items}

	req := httptest.NewRequest("GET", "/api/crafting/profit?max_level=20", nil)
	w := httptest.NewRecorder()
	srv.handleCraftingProfit(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Recipes []ProfitRecipe `json:"recipes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(resp.Recipes) != 2 {
		t.Fatalf("expected 2 recipes (max_level=20 filters recipe_3), got %d", len(resp.Recipes))
	}
	byName := map[string]ProfitRecipe{}
	for _, r := range resp.Recipes {
		byName[r.Name] = r
	}

	r1, ok := byName["Smelt Iron Bar"]
	if !ok {
		t.Fatal("expected Smelt Iron Bar in results")
	}
	if r1.IngredientsCost != 13 || r1.SellValue != 20 || r1.Profit != 7 {
		t.Errorf("expected cost 13 / sell 20 / profit 7, got %v / %v / %v", r1.IngredientsCost, r1.SellValue, r1.Profit)
	}
	if r1.CostUnknown {
		t.Error("expected cost_unknown false for full-data recipe")
	}
	if r1.MarginPct != 35 {
		t.Errorf("expected margin 35, got %v", r1.MarginPct)
	}

	r2, ok := byName["Mystery Craft"]
	if !ok {
		t.Fatal("expected Mystery Craft in results")
	}
	if !r2.CostUnknown {
		t.Error("expected cost_unknown true for missing ingredient item")
	}
	if r2.Profit != 0 {
		t.Errorf("expected profit 0 when cost unknown, got %v", r2.Profit)
	}

	// skill filter
	req2 := httptest.NewRequest("GET", "/api/crafting/profit?skill=Alchemy", nil)
	w2 := httptest.NewRecorder()
	srv.handleCraftingProfit(w2, req2)
	var resp2 struct {
		Recipes []ProfitRecipe `json:"recipes"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &resp2)
	if len(resp2.Recipes) != 1 || resp2.Recipes[0].Skill != "Alchemy" {
		t.Errorf("expected only Alchemy recipe, got %+v", resp2.Recipes)
	}

	// no matches -> empty array, 200
	req3 := httptest.NewRequest("GET", "/api/crafting/profit?skill=NoSuchSkill", nil)
	w3 := httptest.NewRecorder()
	srv.handleCraftingProfit(w3, req3)
	if w3.Code != http.StatusOK || strings.TrimSpace(w3.Body.String()) != `{"recipes":[]}` {
		t.Errorf("expected empty recipes, got %d %s", w3.Code, w3.Body.String())
	}
}

func TestHandleRecipesSearch(t *testing.T) {
	items := map[int]cdn.Item{
		1: {ItemID: 1, Name: "Iron Ore"},
		2: {ItemID: 2, Name: "Coal"},
	}
	recipes := cdn.RecipesFile{
		"recipe_1": {Name: "Smelt Iron Bar", Skill: "Blacksmithing", SkillLevelReq: 10,
			Ingredients: []cdn.RecipeIngredient{{ItemCode: 1, StackSize: 2}, {ItemCode: 2, StackSize: 1}},
			ResultItems: []cdn.RecipeResultItem{{ItemCode: 1, StackSize: 1}}},
		"recipe_2": {Name: "Cook Meat", Skill: "Cooking", SkillLevelReq: 5,
			Ingredients: []cdn.RecipeIngredient{{ItemCode: 2, StackSize: 3}},
			ResultItems: []cdn.RecipeResultItem{{ItemCode: 2, StackSize: 1}}},
	}
	srv := &Server{Recipes: recipes, ItemByID: items}

	req := httptest.NewRequest("GET", "/api/recipes/search?q=smelt", nil)
	w := httptest.NewRecorder()
	srv.handleRecipesSearch(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Recipes []RecipeHit `json:"recipes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(resp.Recipes) != 1 {
		t.Fatalf("expected 1 result for 'smelt', got %d", len(resp.Recipes))
	}
	hit := resp.Recipes[0]
	if hit.Name != "Smelt Iron Bar" || hit.Skill != "Blacksmithing" || hit.Level != 10 {
		t.Errorf("unexpected hit: %+v", hit)
	}
	if hit.ResultItem != "Iron Ore" {
		t.Errorf("expected result item Iron Ore, got %q", hit.ResultItem)
	}
	if len(hit.Ingredients) != 2 || hit.Ingredients[0] != "Iron Ore x2" || hit.Ingredients[1] != "Coal x1" {
		t.Errorf("unexpected ingredients: %v", hit.Ingredients)
	}

	// skill + level filters (case-insensitive skill)
	req2 := httptest.NewRequest("GET", "/api/recipes/search?skill=cooking&level=10", nil)
	w2 := httptest.NewRecorder()
	srv.handleRecipesSearch(w2, req2)
	var resp2 struct {
		Recipes []RecipeHit `json:"recipes"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &resp2)
	if len(resp2.Recipes) != 1 || resp2.Recipes[0].Name != "Cook Meat" {
		t.Errorf("expected Cook Meat, got %+v", resp2.Recipes)
	}

	// no match -> empty array
	req3 := httptest.NewRequest("GET", "/api/recipes/search?q=zzz", nil)
	w3 := httptest.NewRecorder()
	srv.handleRecipesSearch(w3, req3)
	if strings.TrimSpace(w3.Body.String()) != `{"recipes":[]}` {
		t.Errorf("expected empty recipes, got %s", w3.Body.String())
	}
}

// ---- Drop rates (zone/source breakdown) ----

type dropRateZone struct {
	Name         string  `json:"name"`
	Count        int     `json:"count"`
	SessionCount int     `json:"session_count"`
	Chance       float64 `json:"chance"`
}

type dropRateItem struct {
	Name          string         `json:"name"`
	TotalCount    int            `json:"total_count"`
	SessionCount  int            `json:"session_count"`
	AvgPerSession float64        `json:"avg_per_session"`
	AvgValue      float64        `json:"avg_value"`
	OverallChance float64        `json:"overall_chance"`
	NowChance     float64        `json:"now_chance"`
	PrimarySource string         `json:"primary_source"`
	Sources       []dropRateZone `json:"sources"`
	Zones         []dropRateZone `json:"zones"`
}

type dropRatesResponse struct {
	Items         []dropRateItem `json:"items"`
	Zones         []dropRateZone `json:"zones"`
	CurrentSource string         `json:"current_source"`
}

// writeDropRatesFixture writes one session report with a timeline of kills,
// zone changes and loot events, and returns a server reading that report dir.
func writeDropRatesFixture(t *testing.T) *Server {
	t.Helper()
	tmpDir := t.TempDir()
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	snap := session.Snapshot{
		State:   session.Stopped,
		Dungeon: "Test Dungeon",
		Kills: []session.KillEvent{
			{Mob: "Wolf", Time: base.Add(2 * time.Second)},
			{Mob: "Bear", Time: base.Add(12 * time.Second)},
			{Mob: "Wolf", Time: base.Add(22 * time.Second)},
		},
		ZoneHistory: []session.ZoneEntry{
			{Zone: "Forest", Time: base},
			{Zone: "Cave", Time: base.Add(10 * time.Second)},
			{Zone: "Ruins", Time: base.Add(20 * time.Second)},
		},
		LootEvents: []session.LootEvent{
			{Name: "Wolf Fang", Count: 1, Value: 10, Time: base.Add(3 * time.Second)},
			{Name: "Bear Claw", Count: 1, Value: 20, Time: base.Add(13 * time.Second)},
			{Name: "Wolf Fang", Count: 2, Value: 10, Time: base.Add(23 * time.Second)},
			{Name: "Old Coin", Count: 3, Value: 5, Time: base.Add(25 * time.Second)},
		},
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("failed to marshal snapshot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "session-20260101-100000.json"), data, 0644); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}
	return &Server{Cfg: config.Config{ReportDir: tmpDir}, Sess: session.New()}
}

func getDropRates(t *testing.T, srv *Server, query string) dropRatesResponse {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/drop-rates"+query, nil)
	w := httptest.NewRecorder()
	srv.handleDropRates(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp dropRatesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	return resp
}

func itemsByName(items []dropRateItem) map[string]dropRateItem {
	out := make(map[string]dropRateItem, len(items))
	for _, it := range items {
		out[it.Name] = it
	}
	return out
}

func TestHandleDropRates_ZonesBreakdown(t *testing.T) {
	resp := getDropRates(t, writeDropRatesFixture(t), "")

	items := itemsByName(resp.Items)
	wf, ok := items["Wolf Fang"]
	if !ok {
		t.Fatal("expected Wolf Fang in results")
	}
	if wf.TotalCount != 3 {
		t.Errorf("expected Wolf Fang total 3, got %d", wf.TotalCount)
	}
	if len(wf.Zones) != 2 || wf.Zones[0].Name != "Ruins" || wf.Zones[0].Count != 2 || wf.Zones[1].Name != "Forest" || wf.Zones[1].Count != 1 {
		t.Errorf("expected Wolf Fang zones [Ruins 2, Forest 1], got %+v", wf.Zones)
	}

	bc, ok := items["Bear Claw"]
	if !ok {
		t.Fatal("expected Bear Claw in results")
	}
	if len(bc.Zones) != 1 || bc.Zones[0].Name != "Cave" || bc.Zones[0].Count != 1 {
		t.Errorf("expected Bear Claw zones [Cave 1], got %+v", bc.Zones)
	}

	oc, ok := items["Old Coin"]
	if !ok {
		t.Fatal("expected Old Coin in results")
	}
	if len(oc.Zones) != 1 || oc.Zones[0].Name != "Ruins" || oc.Zones[0].Count != 3 {
		t.Errorf("expected Old Coin zones [Ruins 3], got %+v", oc.Zones)
	}

	// Envelope zones: global list sorted by count desc.
	if len(resp.Zones) != 3 ||
		resp.Zones[0].Name != "Ruins" || resp.Zones[0].Count != 5 ||
		resp.Zones[1].Name != "Cave" || resp.Zones[1].Count != 1 ||
		resp.Zones[2].Name != "Forest" || resp.Zones[2].Count != 1 {
		t.Errorf("expected envelope zones [Ruins 5, Cave 1, Forest 1], got %+v", resp.Zones)
	}
}

func TestHandleDropRates_ZoneFilter(t *testing.T) {
	resp := getDropRates(t, writeDropRatesFixture(t), "?zone=cave") // case-insensitive

	items := itemsByName(resp.Items)
	if len(items) != 1 {
		t.Fatalf("expected only 1 item after zone filter, got %+v", resp.Items)
	}
	bc, ok := items["Bear Claw"]
	if !ok {
		t.Fatalf("expected Bear Claw to survive zone=cave, got %+v", resp.Items)
	}
	if bc.TotalCount != 1 {
		t.Errorf("expected Bear Claw total 1, got %d", bc.TotalCount)
	}
	if len(bc.Zones) != 1 || bc.Zones[0].Name != "Cave" || bc.Zones[0].Count != 1 {
		t.Errorf("expected Bear Claw zones [Cave 1], got %+v", bc.Zones)
	}
	if len(resp.Zones) != 1 || resp.Zones[0].Name != "Cave" || resp.Zones[0].Count != 1 {
		t.Errorf("expected envelope zones [Cave 1], got %+v", resp.Zones)
	}
}

func TestHandleDropRates_SourceFilter(t *testing.T) {
	resp := getDropRates(t, writeDropRatesFixture(t), "?source=wolf") // case-insensitive

	items := itemsByName(resp.Items)
	if len(items) != 2 {
		t.Fatalf("expected 2 items after source filter, got %+v", resp.Items)
	}
	wf, ok := items["Wolf Fang"]
	if !ok {
		t.Fatal("expected Wolf Fang in results")
	}
	if wf.PrimarySource != "wolf" {
		t.Errorf("expected PrimarySource 'wolf', got %q", wf.PrimarySource)
	}
	if wf.TotalCount != 3 {
		t.Errorf("expected Wolf Fang total 3, got %d", wf.TotalCount)
	}
	oc, ok := items["Old Coin"]
	if !ok {
		t.Fatal("expected Old Coin in results")
	}
	if oc.PrimarySource != "wolf" {
		t.Errorf("expected Old Coin PrimarySource 'wolf', got %q", oc.PrimarySource)
	}
	if _, ok := items["Bear Claw"]; ok {
		t.Error("expected Bear Claw to be filtered out by source=wolf")
	}
}

func TestRequireLocal(t *testing.T) {
	cases := []struct {
		name    string
		origin  string
		host    string
		wantOK  bool
	}{
		{"no origin, 127.0.0.1 host", "", "127.0.0.1:7777", true},
		{"no origin, localhost host", "", "localhost:7777", true},
		{"same-origin fetch", "http://127.0.0.1:7777", "127.0.0.1:7777", true},
		{"localhost origin", "http://localhost:7777", "localhost:7777", true},
		{"scheme in host", "http://127.0.0.1:7777", "http://127.0.0.1:7777", true},
		{"cross-origin page", "http://evil.example", "127.0.0.1:7777", false},
		{"dns rebinding host", "", "evil.example", false},
		{"foreign origin + foreign host", "http://evil.example", "evil.example", false},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/api/feed", nil)
		req.Host = c.host
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		w := httptest.NewRecorder()
		allowed := []string{"127.0.0.1:7777", "localhost:7777"}
		if got := requireLocal(w, req, allowed); got != c.wantOK {
			t.Errorf("%s: requireLocal = %v, want %v (code %d)", c.name, got, c.wantOK, w.Code)
		}
	}
}

func TestMount_BlocksForeignOrigin(t *testing.T) {
	h := (&Server{}).Mount()
	req := httptest.NewRequest("POST", "/api/session/start", nil)
	req.Header.Set("Origin", "http://evil.example")
	req.Host = "127.0.0.1:7777"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for foreign origin, got %d", w.Code)
	}
}

func TestMount_BlocksForeignHost(t *testing.T) {
	h := (&Server{}).Mount()
	req := httptest.NewRequest("POST", "/api/overlay/spawn", nil)
	req.Host = "attacker.example"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for foreign host, got %d", w.Code)
	}
}

func TestMount_LocalHostPasses(t *testing.T) {
	h := (&Server{Cfg: config.Config{}}).Mount()
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	req.Host = "127.0.0.1:7777"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for local host, got %d", w.Code)
	}
}

// ---- Route planner (trader view + distance sorting) ----

func fptr(v float64) *float64 { return &v }

// routePlannerFixtures builds a server with one store trader (Mira, Serbule)
// that buys Wooden items and loves Wood, a session in Eltibule with loot, and
// area coordinates so Eltibule(0,0) -> Serbule(10,7.5) = 12.5km.
func routePlannerFixtures(t *testing.T) *Server {
	t.Helper()
	npcs := cdn.NpcsFile{
		"npc_mira": {
			InternalName: "npc_mira",
			Name:         "Mira",
			AreaFriendly: "Serbule",
			Services:     []cdn.Service{{Type: "Store", ItemTypes: []string{"Wooden"}}},
			Preferences:  []cdn.Preference{{Name: "Loves Wood", Keywords: []string{"Wood"}, Pref: 3.5}},
		},
	}
	mgr := session.New()
	if err := mgr.Start("Test Dungeon", ""); err != nil {
		t.Fatalf("session start: %v", err)
	}
	mgr.SetZone("Eltibule")
	mgr.AddLoot(session.LootEntry{Name: "Maple Wood", Valor: 100, Count: 3})
	mgr.AddLoot(session.LootEntry{Name: "Iron Ore", Valor: 5, Count: 1})

	return &Server{
		Favor: favor.FromNpcs(npcs),
		Npcs:  npcs,
		Sess:  mgr,
		Areas: cdn.AreaIndex{
			ByFriendly: map[string]string{"eltibule": "AreaEltibule", "serbule": "AreaSerbule"},
			ByInternal: map[string]cdn.Area{
				"AreaEltibule": {FriendlyName: "Eltibule", X: fptr(0), Y: fptr(0)},
				"AreaSerbule":  {FriendlyName: "Serbule", X: fptr(10), Y: fptr(7.5)},
			},
		},
		itemByName: map[string]cdn.Item{
			"maple wood": {Name: "Maple Wood", Keywords: []string{"Wood"}, Value: 100},
		},
	}
}

func TestHandleRoutePlanner_TraderView(t *testing.T) {
	srv := routePlannerFixtures(t)
	req := httptest.NewRequest("GET", "/api/route-planner?trader=Mira", nil)
	w := httptest.NewRecorder()
	srv.handleRoutePlanner(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Trader     string  `json:"trader"`
		Area       string  `json:"area"`
		DistanceKm *float64 `json:"distance_km"`
		SellItems  []struct {
			Name  string  `json:"name"`
			Count int     `json:"count"`
			Value float64 `json:"value"`
		} `json:"sell_items"`
		FavorItems []struct {
			Name       string  `json:"name"`
			Count      int     `json:"count"`
			FavorScore float64 `json:"favor_score"`
		} `json:"favor_items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if resp.Trader != "Mira" || resp.Area != "Serbule" {
		t.Errorf("expected trader Mira / area Serbule, got %q / %q", resp.Trader, resp.Area)
	}
	if resp.DistanceKm == nil || *resp.DistanceKm != 12.5 {
		t.Errorf("expected distance_km 12.5, got %v", resp.DistanceKm)
	}
	if len(resp.SellItems) != 1 || resp.SellItems[0].Name != "Maple Wood" ||
		resp.SellItems[0].Count != 3 || resp.SellItems[0].Value != 100 {
		t.Errorf("expected sell item Maple Wood x3 @100, got %+v", resp.SellItems)
	}
	if len(resp.FavorItems) != 1 || resp.FavorItems[0].Name != "Maple Wood" ||
		resp.FavorItems[0].Count != 3 || resp.FavorItems[0].FavorScore != 3.5 {
		t.Errorf("expected favor item Maple Wood x3 @3.5, got %+v", resp.FavorItems)
	}
}

func TestHandleRoutePlanner_TraderView_NotFound(t *testing.T) {
	srv := routePlannerFixtures(t)
	req := httptest.NewRequest("GET", "/api/route-planner?trader=NoSuchTrader", nil)
	w := httptest.NewRecorder()
	srv.handleRoutePlanner(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleRoutePlanner_DistanceSort(t *testing.T) {
	npcs := cdn.NpcsFile{
		"npc_near": {
			InternalName: "npc_near",
			Name:         "Near Trader",
			AreaFriendly: "Serbule",
			Services:     []cdn.Service{{Type: "Store", ItemTypes: []string{"Wooden"}}},
		},
		"npc_far": {
			InternalName: "npc_far",
			Name:         "Far Trader",
			AreaFriendly: "Eltibule",
			Services:     []cdn.Service{{Type: "Store", ItemTypes: []string{"Wooden"}}},
		},
	}
	mgr := session.New()
	_ = mgr.Start("Test Dungeon", "")
	mgr.SetZone("Serbule") // (0,0); Near Trader same zone, Far Trader at (3,4) = 5km

	traderMgr := trader.New(filepath.Join(t.TempDir(), "traders.json"))
	_ = traderMgr.Ensure("Near Trader", "Serbule", 7, 0)
	_ = traderMgr.UpdateLimit("Near Trader", 100)
	_ = traderMgr.Ensure("Far Trader", "Eltibule", 7, 0)
	_ = traderMgr.UpdateLimit("Far Trader", 900)

	srv := &Server{
		Favor: favor.FromNpcs(npcs),
		Npcs:  npcs,
		Sess:  mgr,
		Trader: traderMgr,
		Areas: cdn.AreaIndex{
			ByFriendly: map[string]string{"serbule": "AreaSerbule", "eltibule": "AreaEltibule"},
			ByInternal: map[string]cdn.Area{
				"AreaSerbule":  {FriendlyName: "Serbule", X: fptr(0), Y: fptr(0)},
				"AreaEltibule": {FriendlyName: "Eltibule", X: fptr(3), Y: fptr(4)},
			},
		},
	}

	getRoutes := func(query string) []RouteInfo {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/route-planner"+query, nil)
		w := httptest.NewRecorder()
		srv.handleRoutePlanner(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Routes []RouteInfo `json:"routes"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}
		return resp.Routes
	}

	// Default sort stays by remaining capacity (descending).
	routes := getRoutes("?item=Maple%20Wood")
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}
	if routes[0].Trader != "Far Trader" || routes[0].RemainingCapacityG != 900 {
		t.Errorf("default sort: expected Far Trader (900g) first, got %+v", routes[0])
	}

	// sort=distance: nearest first, capacity as tiebreak.
	routes = getRoutes("?item=Maple%20Wood&sort=distance")
	if routes[0].Trader != "Near Trader" {
		t.Errorf("distance sort: expected Near Trader first, got %+v", routes[0])
	}
	if routes[0].DistanceKm == nil || *routes[0].DistanceKm != 0 {
		t.Errorf("distance sort: expected Near Trader distance 0, got %v", routes[0].DistanceKm)
	}
	if routes[1].DistanceKm == nil || *routes[1].DistanceKm != 5 {
		t.Errorf("distance sort: expected Far Trader distance 5, got %v", routes[1].DistanceKm)
	}
}

func TestHandleRoutePlanner_DistanceUnavailable(t *testing.T) {
	npcs := cdn.NpcsFile{
		"npc_t": {
			InternalName: "npc_t",
			Name:         "Test Trader",
			AreaFriendly: "Somewhere Unknown", // no CDN coords, no fallback entry
			Services:     []cdn.Service{{Type: "Store", ItemTypes: []string{"Wooden"}}},
		},
	}
	mgr := session.New()
	_ = mgr.Start("Test Dungeon", "")
	mgr.SetZone("Eltibule")
	mgr.AddLoot(session.LootEntry{Name: "Maple Wood", Valor: 100, Count: 1})

	// Areas exist but carry no coordinates; trader zone not in fallback table.
	srv := &Server{
		Favor: favor.FromNpcs(npcs),
		Npcs:  npcs,
		Sess:  mgr,
		Areas: cdn.AreaIndex{
			ByFriendly: map[string]string{"eltibule": "AreaEltibule"},
			ByInternal: map[string]cdn.Area{
				"AreaEltibule": {FriendlyName: "Eltibule"},
			},
		},
	}

	// Item view: distance_km null.
	req := httptest.NewRequest("GET", "/api/route-planner?item=Maple%20Wood", nil)
	w := httptest.NewRecorder()
	srv.handleRoutePlanner(w, req)
	var itemResp struct {
		Routes []RouteInfo `json:"routes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &itemResp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(itemResp.Routes) != 1 || itemResp.Routes[0].DistanceKm != nil {
		t.Errorf("expected 1 route with null distance, got %+v", itemResp.Routes)
	}

	// Trader view: distance_km null, still 200 with items.
	req2 := httptest.NewRequest("GET", "/api/route-planner?trader=Test%20Trader", nil)
	w2 := httptest.NewRecorder()
	srv.handleRoutePlanner(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var traderResp struct {
		DistanceKm *float64        `json:"distance_km"`
		SellItems  []sellRouteItem `json:"sell_items"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &traderResp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if traderResp.DistanceKm != nil {
		t.Errorf("expected null distance, got %v", traderResp.DistanceKm)
	}
	if len(traderResp.SellItems) != 1 || traderResp.SellItems[0].Name != "Maple Wood" {
		t.Errorf("expected Maple Wood sell item, got %+v", traderResp.SellItems)
	}
}

func TestHandleRoutePlanner_DistanceFallback(t *testing.T) {
	// CDN publishes no coordinates; the built-in zone table + dungeon-name
	// matching must still produce distances so nearest-first sorting works.
	npcs := cdn.NpcsFile{
		"npc_serbule": {
			InternalName: "npc_serbule",
			Name:         "Serbule Trader",
			AreaFriendly: "Serbule", // fallback (200,300)
			Services:     []cdn.Service{{Type: "Store", ItemTypes: []string{"Wooden"}}},
		},
		"npc_eltibule": {
			InternalName: "npc_eltibule",
			Name:         "Eltibule Trader",
			AreaFriendly: "Eltibule", // fallback (200,450)
			Services:     []cdn.Service{{Type: "Store", ItemTypes: []string{"Wooden"}}},
		},
	}
	mgr := session.New()
	_ = mgr.Start("Test Dungeon", "")
	mgr.SetZone("Serbule Crypt") // dungeon name → parent zone Serbule (200,300)
	mgr.AddLoot(session.LootEntry{Name: "Maple Wood", Valor: 100, Count: 1})

	srv := &Server{
		Favor: favor.FromNpcs(npcs),
		Npcs:  npcs,
		Sess:  mgr,
		Areas: cdn.AreaIndex{}, // no CDN areas at all
	}

	req := httptest.NewRequest("GET", "/api/route-planner?item=Maple%20Wood&sort=distance", nil)
	w := httptest.NewRecorder()
	srv.handleRoutePlanner(w, req)
	var resp struct {
		Routes []RouteInfo `json:"routes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(resp.Routes) != 2 {
		t.Fatalf("expected 2 routes, got %+v", resp.Routes)
	}
	if resp.Routes[0].Trader != "Serbule Trader" || resp.Routes[0].DistanceKm == nil || *resp.Routes[0].DistanceKm != 0 {
		t.Errorf("expected Serbule Trader at 0km first, got %+v", resp.Routes[0])
	}
	if resp.Routes[1].Trader != "Eltibule Trader" || resp.Routes[1].DistanceKm == nil || *resp.Routes[1].DistanceKm != 150 {
		t.Errorf("expected Eltibule Trader at 150km, got %+v", resp.Routes[1])
	}
}
