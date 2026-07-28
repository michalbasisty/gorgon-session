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

	"github.com/michalbasisty/gorgon-session/internal/config"
	"github.com/michalbasisty/gorgon-session/internal/favor"
	"github.com/michalbasisty/gorgon-session/internal/session"
	"github.com/michalbasisty/gorgon-session/internal/trader"
)

func TestHandleSessionExport_NotFound(t *testing.T) {
	cfg := config.Config{ReportDir: t.TempDir()}
	srv := &Server{Cfg: cfg}

	req := httptest.NewRequest("GET", "/api/session/nonexistent/export", nil)
	w := httptest.NewRecorder()

	srv.handleSessionByID(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleSessionExport_NoReportDir(t *testing.T) {
	cfg := config.Config{ReportDir: ""}
	srv := &Server{Cfg: cfg}

	req := httptest.NewRequest("GET", "/api/session/test/export", nil)
	w := httptest.NewRecorder()

	srv.handleSessionByID(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
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

func TestHandleTraderPost_UpdateLimit(t *testing.T) {
	td := t.TempDir()
	traderMgr := trader.New(filepath.Join(td, "traders.json"))

	srv := &Server{
		Cfg:   config.Config{},
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
		Cfg:   config.Config{},
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
				Name:   "Favor Item",
				Decision: favor.Decision{Verdict: favor.VerdictFavor},
			},
			{
				Name:   "Vendor Item",
				Decision: favor.Decision{Verdict: favor.VerdictSellVendor},
			},
			{
				Name:   "Consignment Item",
				Decision: favor.Decision{Verdict: favor.VerdictSellConsignment},
			},
			{
				Name:   "Keep Item",
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
