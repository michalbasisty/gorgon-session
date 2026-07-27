package trader

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManager_AddAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "traders.json")
	mgr := New(filePath)

	nextReset := time.Now().Add(7 * 24 * time.Hour)
	err := mgr.Add("TestNPC", "TestArea", 1000, nextReset)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	trader := mgr.Get("TestNPC")
	if trader == nil {
		t.Fatal("Expected trader to exist")
	}
	if trader.NPCName != "TestNPC" {
		t.Errorf("Expected NPCName 'TestNPC', got '%s'", trader.NPCName)
	}
	if trader.Area != "TestArea" {
		t.Errorf("Expected Area 'TestArea', got '%s'", trader.Area)
	}
	if trader.WeeklyLimit != 1000 {
		t.Errorf("Expected WeeklyLimit 1000, got %f", trader.WeeklyLimit)
	}
	if trader.SoldThisWeek != 0 {
		t.Errorf("Expected SoldThisWeek 0, got %f", trader.SoldThisWeek)
	}
}

func TestManager_LogSale(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "traders.json")
	mgr := New(filePath)

	nextReset := time.Now().Add(7 * 24 * time.Hour)
	_ = mgr.Add("TestNPC", "TestArea", 1000, nextReset)
	err := mgr.LogSale("TestNPC", 250)
	if err != nil {
		t.Fatalf("LogSale failed: %v", err)
	}

	trader := mgr.Get("TestNPC")
	if trader.SoldThisWeek != 250 {
		t.Errorf("Expected SoldThisWeek 250, got %f", trader.SoldThisWeek)
	}

	// Log another sale
	_ = mgr.LogSale("TestNPC", 300)
	trader = mgr.Get("TestNPC")
	if trader.SoldThisWeek != 550 {
		t.Errorf("Expected SoldThisWeek 550, got %f", trader.SoldThisWeek)
	}
}

func TestManager_RollingReset(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "traders.json")
	mgr := New(filePath)

	nextReset := time.Now().Add(7 * 24 * time.Hour)
	_ = mgr.Add("TestNPC", "TestArea", 1000, nextReset)
	_ = mgr.LogSale("TestNPC", 500)

	// Manually set NextReset to the past to simulate time passing
	mgr.mu.Lock()
	mgr.traders["TestNPC"].NextReset = time.Now().Add(-1 * time.Hour)
	mgr.mu.Unlock()

	// GetAll should trigger reset
	traders := mgr.GetAll()
	if len(traders) != 1 {
		t.Fatalf("Expected 1 trader, got %d", len(traders))
	}

	if traders[0].SoldThisWeek != 0 {
		t.Errorf("Expected SoldThisWeek to reset to 0, got %f", traders[0].SoldThisWeek)
	}

	// NextReset should be updated to future
	if traders[0].NextReset.Before(time.Now()) {
		t.Errorf("Expected NextReset to be in the future")
	}
}

func TestManager_Remove(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "traders.json")
	mgr := New(filePath)

	nextReset := time.Now().Add(7 * 24 * time.Hour)
	_ = mgr.Add("TestNPC", "TestArea", 1000, nextReset)
	mgr.Remove("TestNPC")

	trader := mgr.Get("TestNPC")
	if trader != nil {
		t.Error("Expected trader to be removed")
	}
}

func TestManager_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "traders.json")
	mgr := New(filePath)

	nextReset := time.Now().Add(7 * 24 * time.Hour)
	_ = mgr.Add("NPC1", "Area1", 1000, nextReset)
	_ = mgr.Add("NPC2", "Area2", 2000, nextReset)
	_ = mgr.LogSale("NPC1", 500)

	err := mgr.Save()
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Create new manager and load
	mgr2 := New(filePath)
	err = mgr2.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	traders := mgr2.GetAll()
	if len(traders) != 2 {
		t.Fatalf("Expected 2 traders, got %d", len(traders))
	}

	// Find NPC1
	var npc1 *Trader
	for _, tr := range traders {
		if tr.NPCName == "NPC1" {
			npc1 = tr
			break
		}
	}
	if npc1 == nil {
		t.Fatal("NPC1 not found after load")
	}
	if npc1.SoldThisWeek != 500 {
		t.Errorf("Expected SoldThisWeek 500, got %f", npc1.SoldThisWeek)
	}
}

func TestManager_LoadNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "nonexistent.json")
	mgr := New(filePath)

	err := mgr.Load()
	if err != nil {
		t.Errorf("Load should not fail for non-existent file: %v", err)
	}

	traders := mgr.GetAll()
	if len(traders) != 0 {
		t.Errorf("Expected 0 traders, got %d", len(traders))
	}
}

func TestManager_GetAll_AutoReset(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "traders.json")
	mgr := New(filePath)

	nextReset := time.Now().Add(7 * 24 * time.Hour)
	_ = mgr.Add("TestNPC", "TestArea", 1000, nextReset)
	_ = mgr.LogSale("TestNPC", 500)

	// Manually set NextReset to the past
	mgr.mu.Lock()
	mgr.traders["TestNPC"].NextReset = time.Now().Add(-1 * time.Hour)
	mgr.mu.Unlock()

	// LogSale should also trigger reset
	err := mgr.LogSale("TestNPC", 100)
	if err != nil {
		t.Fatalf("LogSale failed: %v", err)
	}

	trader := mgr.Get("TestNPC")
	// After reset, SoldThisWeek should be 100 (not 600)
	if trader.SoldThisWeek != 100 {
		t.Errorf("Expected SoldThisWeek 100 after reset, got %f", trader.SoldThisWeek)
	}
}

func TestManager_DuplicateAdd(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "traders.json")
	mgr := New(filePath)

	nextReset := time.Now().Add(7 * 24 * time.Hour)
	_ = mgr.Add("TestNPC", "Area1", 1000, nextReset)
	_ = mgr.LogSale("TestNPC", 500)

	// Try to add again - should not reset
	_ = mgr.Add("TestNPC", "Area2", 2000, nextReset)

	trader := mgr.Get("TestNPC")
	if trader.SoldThisWeek != 500 {
		t.Errorf("Expected SoldThisWeek to remain 500, got %f", trader.SoldThisWeek)
	}
	if trader.Area != "Area1" {
		t.Errorf("Expected Area to remain 'Area1', got '%s'", trader.Area)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
