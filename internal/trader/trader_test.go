package trader

import (
	"os"
	"path/filepath"
	"testing"
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

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
