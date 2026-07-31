package trader

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureAndGet(t *testing.T) {
	mgr := New(filepath.Join(t.TempDir(), "traders.json"))
	err := mgr.Ensure("TestNPC", "TestArea", 7, 0)
	if err != nil {
		t.Fatalf("Ensure failed: %v", err)
	}
	trader := mgr.Get("TestNPC")
	if trader == nil {
		t.Fatal("Expected trader to exist")
	}
	if trader.NPCName != "TestNPC" {
		t.Errorf("got NPCName %q", trader.NPCName)
	}
	if trader.Area != "TestArea" {
		t.Errorf("got Area %q", trader.Area)
	}
	if trader.ResetDays != 7 {
		t.Errorf("got ResetDays %d", trader.ResetDays)
	}
}

func TestLogSale(t *testing.T) {
	mgr := New(filepath.Join(t.TempDir(), "traders.json"))
	_ = mgr.Ensure("TestNPC", "TestArea", 7, 0)
	if err := mgr.LogSale("TestNPC", 250); err != nil {
		t.Fatalf("LogSale failed: %v", err)
	}
	trader := mgr.Get("TestNPC")
	if trader.SoldThisWeek != 250 {
		t.Errorf("got SoldThisWeek %f", trader.SoldThisWeek)
	}
	// Additive
	_ = mgr.LogSale("TestNPC", 300)
	trader = mgr.Get("TestNPC")
	if trader.SoldThisWeek != 550 {
		t.Errorf("got SoldThisWeek %f after second sale", trader.SoldThisWeek)
	}
}

func TestSetSold(t *testing.T) {
	mgr := New(filepath.Join(t.TempDir(), "traders.json"))
	_ = mgr.Ensure("TestNPC", "TestArea", 7, 0)
	_ = mgr.LogSale("TestNPC", 500)
	if err := mgr.SetSold("TestNPC", 100); err != nil {
		t.Fatalf("SetSold failed: %v", err)
	}
	trader := mgr.Get("TestNPC")
	if trader.SoldThisWeek != 100 {
		t.Errorf("got SoldThisWeek %f, want 100", trader.SoldThisWeek)
	}
}

func TestUpdateLimit(t *testing.T) {
	mgr := New(filepath.Join(t.TempDir(), "traders.json"))
	_ = mgr.Ensure("TestNPC", "TestArea", 7, 0)
	if err := mgr.UpdateLimit("TestNPC", 5000); err != nil {
		t.Fatalf("UpdateLimit failed: %v", err)
	}
	trader := mgr.Get("TestNPC")
	if trader.WeeklyLimit != 5000 {
		t.Errorf("got WeeklyLimit %f", trader.WeeklyLimit)
	}
}

func TestEnsureUpdatesExisting(t *testing.T) {
	mgr := New(filepath.Join(t.TempDir(), "traders.json"))
	_ = mgr.Ensure("TestNPC", "TestArea", 7, 0)
	_ = mgr.LogSale("TestNPC", 200)
	// Re-ensure with new reset settings
	_ = mgr.Ensure("TestNPC", "NewArea", 5, 22)
	trader := mgr.Get("TestNPC")
	if trader.Area != "NewArea" {
		t.Errorf("got Area %q", trader.Area)
	}
	if trader.ResetDays != 5 {
		t.Errorf("got ResetDays %d", trader.ResetDays)
	}
	if trader.ResetHours != 22 {
		t.Errorf("got ResetHours %d", trader.ResetHours)
	}
	// Sold data preserved
	if trader.SoldThisWeek != 200 {
		t.Errorf("got SoldThisWeek %f", trader.SoldThisWeek)
	}
}

func TestRemove(t *testing.T) {
	mgr := New(filepath.Join(t.TempDir(), "traders.json"))
	_ = mgr.Ensure("TestNPC", "TestArea", 7, 0)
	mgr.Remove("TestNPC")
	if mgr.Get("TestNPC") != nil {
		t.Error("trader should be nil after Remove")
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := New(filepath.Join(tmpDir, "traders.json"))
	_ = mgr.Ensure("NPC1", "Area1", 5, 22)
	_ = mgr.Ensure("NPC2", "Area2", 7, 0)
	_ = mgr.LogSale("NPC1", 500)
	if err := mgr.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	// Load into fresh manager
	mgr2 := New(filepath.Join(tmpDir, "traders.json"))
	if err := mgr2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	traders := mgr2.GetAll()
	if len(traders) != 2 {
		t.Fatalf("got %d traders", len(traders))
	}
	var npc1 *Trader
	for _, tr := range traders {
		if tr.NPCName == "NPC1" {
			npc1 = tr
			break
		}
	}
	if npc1 == nil {
		t.Fatal("NPC1 not found")
	}
	if npc1.SoldThisWeek != 500 {
		t.Errorf("got SoldThisWeek %f", npc1.SoldThisWeek)
	}
}

func TestLoadNonExistent(t *testing.T) {
	mgr := New(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err := mgr.Load(); err != nil {
		t.Errorf("Load should not fail for missing file: %v", err)
	}
	traders := mgr.GetAll()
	if len(traders) != 0 {
		t.Errorf("got %d traders", len(traders))
	}
}

func TestTimeUntilReset(t *testing.T) {
	mgr := New(filepath.Join(t.TempDir(), "traders.json"))
	_ = mgr.Ensure("TestNPC", "TestArea", 7, 0)
	// No sale yet: returns full duration
	d := mgr.TimeUntilReset("TestNPC")
	if d.Hours() < 7*24-1 {
		t.Errorf("expected ~7d, got %v", d)
	}
}

func TestDeleteHistoryEvent(t *testing.T) {
	mgr := New(filepath.Join(t.TempDir(), "traders.json"))
	// Ensure with zero remaining time anchors LastSale at exactly one cycle ago,
	// so the next LogSale triggers a refresh event.
	_ = mgr.Ensure("TestNPC", "TestArea", 0, 0)
	_ = mgr.LogSale("TestNPC", 100)

	events := mgr.GetRefreshHistory("")
	if len(events) != 1 {
		t.Fatalf("expected 1 history event, got %d", len(events))
	}
	if events[0].ID == "" {
		t.Fatal("expected event to have an ID")
	}

	if err := mgr.DeleteHistoryEvent(events[0].ID); err != nil {
		t.Fatalf("DeleteHistoryEvent failed: %v", err)
	}
	if got := mgr.GetRefreshHistory(""); len(got) != 0 {
		t.Errorf("expected empty history after delete, got %d events", len(got))
	}

	if err := mgr.DeleteHistoryEvent("ev-missing"); err == nil {
		t.Error("expected error for unknown event ID")
	}

	// Change is persisted: reload from disk.
	mgr2 := New(mgr.filePath)
	if err := mgr2.Load(); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if got := mgr2.GetRefreshHistory(""); len(got) != 0 {
		t.Errorf("expected empty history after reload, got %d", len(got))
	}
}

func TestLoadHistoryMigratesIDs(t *testing.T) {
	dir := t.TempDir()
	tradersPath := filepath.Join(dir, "traders.json")
	historyPath := filepath.Join(dir, "traders-history.json")

	// History file written before the ID field existed.
	legacy := []RefreshEvent{
		{NPCName: "NPC1", Area: "Area1", SoldAtReset: 500, WeeklyLimit: 1000, ResetAt: time.Now().Add(-48 * time.Hour)},
		{NPCName: "NPC2", Area: "Area2", SoldAtReset: 100, WeeklyLimit: 500, ResetAt: time.Now().Add(-24 * time.Hour)},
	}
	data, _ := json.MarshalIndent(legacy, "", "  ")
	if err := os.WriteFile(historyPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	mgr := New(tradersPath)
	if err := mgr.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	events := mgr.GetRefreshHistory("")
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	for i, e := range events {
		if e.ID == "" {
			t.Errorf("event %d missing migrated ID", i)
		}
	}

	// File rewritten with IDs on load.
	var reloaded []RefreshEvent
	raw, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &reloaded); err != nil {
		t.Fatal(err)
	}
	if len(reloaded) != 2 || reloaded[0].ID == "" || reloaded[1].ID == "" {
		t.Errorf("expected persisted events with IDs, got %+v", reloaded)
	}
}

func TestLoadKeepsPersistedLastSale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traders.json")

	// Write a trader whose LastSale is 8 days old (app closed mid-cycle).
	old := time.Now().Add(-8 * 24 * time.Hour)
	data, _ := json.MarshalIndent([]*Trader{{
		NPCName:      "Larsen",
		Area:         "Serbule",
		WeeklyLimit:  10000,
		SoldThisWeek: 7500,
		LastSale:     old,
		ResetDays:    7,
		ResetHours:   0,
	}}, "", "  ")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	mgr := New(path)
	if err := mgr.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	// The persisted anchor must survive load, not be recomputed from
	// ResetDays/ResetHours (which would zero elapsed time and skip catchup).
	tr := mgr.Get("Larsen")
	if tr == nil {
		t.Fatal("Larsen not loaded")
	}
	if !tr.LastSale.Equal(old) {
		t.Errorf("LastSale was recomputed: got %v, want %v", tr.LastSale, old)
	}

	// Start runs catchupMissed: 8 days elapsed >= 7-day cycle → one refresh
	// event backdated to the scheduled reset, counter reset.
	ctx, cancel := context.WithCancel(context.Background())
	mgr.Start(ctx)
	cancel()

	events := mgr.GetRefreshHistory("Larsen")
	if len(events) != 1 {
		t.Fatalf("expected 1 catchup event, got %d", len(events))
	}
	if events[0].SoldAtReset != 7500 || events[0].WeeklyLimit != 10000 {
		t.Errorf("unexpected event payload: %+v", events[0])
	}
	tr = mgr.Get("Larsen")
	if tr.SoldThisWeek != 0 {
		t.Errorf("expected SoldThisWeek reset to 0, got %f", tr.SoldThisWeek)
	}
	if got := mgr.TimeUntilReset("Larsen"); got.Hours() < 6*24-1 || got.Hours() > 6*24+1 {
		t.Errorf("expected ~6d remaining after catchup (refresh happened 1d ago), got %v", got)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
