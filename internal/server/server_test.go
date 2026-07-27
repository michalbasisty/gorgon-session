package server

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yourname/gorgon-session/internal/config"
	"github.com/yourname/gorgon-session/internal/favor"
	"github.com/yourname/gorgon-session/internal/session"
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
