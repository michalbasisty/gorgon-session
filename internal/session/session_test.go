package session

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/michalbasisty/gorgon-session/internal/favor"
)

func state(m *Manager) State { return m.Snapshot().State }

func TestStartStopIdle(t *testing.T) {
	m := New()
	if state(m) != Idle {
		t.Fatalf("expected Idle, got %s", state(m))
	}
	if err := m.Start("test-dungeon", "test-notes"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if state(m) != Running {
		t.Fatalf("expected Running, got %s", state(m))
	}
	if err := m.Start("", ""); err != ErrAlreadyRunning {
		t.Fatalf("expected ErrAlreadyRunning, got %v", err)
	}
	// Stop
	dir := t.TempDir()
	if err := m.Stop(dir); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if state(m) != Stopped {
		t.Fatalf("expected Stopped, got %s", state(m))
	}
	if err := m.Stop(dir); err != ErrNotRunning {
		t.Fatalf("expected ErrNotRunning, got %v", err)
	}
	// Verify report file was written
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 report file, got %d", len(entries))
	}
}

func TestStopWithoutReportDir(t *testing.T) {
	m := New()
	m.Start("test", "")
	if err := m.Stop(""); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestAddLoot(t *testing.T) {
	m := New()
	m.Start("test", "")
	m.AddLoot(LootEntry{Name: "Silk Scarf", Valor: 10, Count: 1, LastSeen: time.Now()})
	snap := m.Snapshot()
	if len(snap.Loot) != 1 {
		t.Fatalf("expected 1 loot entry, got %d", len(snap.Loot))
	}
	if snap.Loot[0].Name != "Silk Scarf" {
		t.Fatalf("expected Silk Scarf, got %s", snap.Loot[0].Name)
	}
	// Add same item again — should merge
	m.AddLoot(LootEntry{Name: "Silk Scarf", Valor: 10, Count: 2, LastSeen: time.Now()})
	snap = m.Snapshot()
	if len(snap.Loot) != 1 {
		t.Fatalf("expected 1 merged entry, got %d", len(snap.Loot))
	}
	if snap.Loot[0].Count != 3 {
		t.Fatalf("expected merged count 3, got %d", snap.Loot[0].Count)
	}
}

func TestAddLootWhenIdle(t *testing.T) {
	m := New()
	m.AddLoot(LootEntry{Name: "Should Not Appear", Valor: 1})
	if len(m.Snapshot().Loot) != 0 {
		t.Fatal("loot added when idle should be dropped")
	}
}

func TestAddLootZeroCountDefaultsToOne(t *testing.T) {
	m := New()
	m.Start("test", "")
	m.AddLoot(LootEntry{Name: "Item", Valor: 5, Count: 0, LastSeen: time.Now()})
	snap := m.Snapshot()
	if snap.Loot[0].Count != 1 {
		t.Fatalf("expected count 1 (defaulted from 0), got %d", snap.Loot[0].Count)
	}
}

func TestRemoveLoot(t *testing.T) {
	m := New()
	m.Start("test", "")
	m.AddLoot(LootEntry{Name: "A", Valor: 1, LastSeen: time.Now()})
	m.AddLoot(LootEntry{Name: "B", Valor: 2, LastSeen: time.Now()})
	if !m.RemoveLoot("A") {
		t.Fatal("RemoveLoot returned false")
	}
	snap := m.Snapshot()
	if len(snap.Loot) != 1 || snap.Loot[0].Name != "B" {
		t.Fatal("expected only B remaining after removing A")
	}
	if m.RemoveLoot("Nonexistent") {
		t.Fatal("RemoveLoot should return false for missing item")
	}
}

func TestSetLootNote(t *testing.T) {
	m := New()
	m.Start("test", "")
	m.AddLoot(LootEntry{Name: "Test Item", Valor: 1, LastSeen: time.Now()})
	if !m.SetLootNote("Test Item", "crafting material") {
		t.Fatal("SetLootNote returned false")
	}
	snap := m.Snapshot()
	if snap.Loot[0].Note != "crafting material" {
		t.Fatalf("expected note 'crafting material', got %q", snap.Loot[0].Note)
	}
	if m.SetLootNote("Nonexistent", "x") {
		t.Fatal("SetLootNote should return false for missing item")
	}
}

func TestXPGains(t *testing.T) {
	m := New()
	m.Start("test", "")
	m.AddXPGain("Sword", 100)
	m.AddXPGain("Shield", 50)
	m.AddXPGain("Sword", 75)
	snap := m.Snapshot()
	if len(snap.XPGains) != 3 {
		t.Fatalf("expected 3 xp gains, got %d", len(snap.XPGains))
	}
	if snap.XPGains[0].Skill != "Sword" || snap.XPGains[0].Amount != 100 {
		t.Fatal("first xp gain mismatch")
	}
}

func TestSetZone(t *testing.T) {
	m := New()
	m.Start("test", "")
	m.SetZone("Serbule")
	m.SetZone("Serbule Hills")
	snap := m.Snapshot()
	if snap.Zone != "Serbule Hills" {
		t.Fatalf("current zone should be 'Serbule Hills', got %q", snap.Zone)
	}
	if len(snap.ZoneHistory) != 2 {
		t.Fatalf("expected 2 zone entries, got %d", len(snap.ZoneHistory))
	}
}

func TestCombatTracking(t *testing.T) {
	m := New()
	m.Start("test", "")
	m.AddAbilityUse("Arrow Volley")
	m.AddAbilityUse("Arrow Volley")
	m.AddAbilityUse("Flurry")
	m.AddCombatHit("Arrow Volley")
	snap := m.Snapshot()
	if snap.AbilityCounts["Arrow Volley"] != 2 {
		t.Fatalf("Arrow Volley use count: expected 2, got %d", snap.AbilityCounts["Arrow Volley"])
	}
	if snap.AbilityCounts["Flurry"] != 1 {
		t.Fatalf("Flurry use count: expected 1, got %d", snap.AbilityCounts["Flurry"])
	}
	if snap.HitCounts["Arrow Volley"] != 1 {
		t.Fatalf("Arrow Volley hit count: expected 1, got %d", snap.HitCounts["Arrow Volley"])
	}
}

func TestGold(t *testing.T) {
	m := New()
	m.Start("test", "")
	m.AddGold(100)
	m.AddGold(50)
	if m.Snapshot().TotalGold != 150 {
		t.Fatalf("expected total gold 150, got %d", m.Snapshot().TotalGold)
	}
}

func TestDeathsAndKills(t *testing.T) {
	m := New()
	m.Start("test", "")
	m.AddDeath("Goblin")
	m.AddKill("Wolf")
	snap := m.Snapshot()
	if len(snap.Deaths) != 1 || snap.Deaths[0].Killer != "Goblin" {
		t.Fatal("death not recorded correctly")
	}
	if len(snap.Kills) != 1 || snap.Kills[0].Mob != "Wolf" {
		t.Fatal("kill not recorded correctly")
	}
}

func TestEventsChannel(t *testing.T) {
	m := New()
	ch := m.Events()
	m.Start("test", "")
	ev := <-ch
	if ev.Kind != "session_start" {
		t.Fatalf("expected session_start event, got %s", ev.Kind)
	}
	m.AddLoot(LootEntry{Name: "Loot Event Test", Valor: 5, Count: 1, LastSeen: time.Now()})
	ev = <-ch
	if ev.Kind != "loot" {
		t.Fatalf("expected loot event, got %s", ev.Kind)
	}
}

func TestSnapshotImmutability(t *testing.T) {
	m := New()
	m.Start("test", "")
	m.AddLoot(LootEntry{Name: "Original", Valor: 10, Count: 1, LastSeen: time.Now()})
	snap := m.Snapshot()
	snap.Loot[0].Name = "Mutated"
	snap.TotalGold = 999
	// Verify the manager's state wasn't affected
	snap2 := m.Snapshot()
	if snap2.Loot[0].Name != "Original" {
		t.Fatal("snapshot should be a copy, not a reference")
	}
	if snap2.TotalGold != 0 {
		t.Fatal("snapshot mutation should not affect manager")
	}
}

func TestStartResetsState(t *testing.T) {
	m := New()
	m.Start("first", "")
	m.AddLoot(LootEntry{Name: "Old Item", Valor: 1, Count: 1, LastSeen: time.Now()})
	m.AddAbilityUse("Fireball")
	m.AddGold(999)
	m.Stop("")
	// Start new session
	m.Start("second", "")
	snap := m.Snapshot()
	if len(snap.Loot) != 0 {
		t.Fatal("new session should have empty loot")
	}
	if snap.TotalGold != 0 {
		t.Fatal("new session should have 0 gold")
	}
	if len(snap.AbilityCounts) != 0 {
		t.Fatal("new session should have empty ability counts")
	}
}

func TestJSONRoundtrip(t *testing.T) {
	m := New()
	m.Start("json-test", "some notes")
	m.AddLoot(LootEntry{Name: "Roundtrip Item", Valor: 42, Count: 3, ItemID: 123, LastSeen: time.Now(), Note: "test-note"})
	m.AddGold(100)
	m.SetZone("Serbule")
	m.AddXPGain("Sword", 50)
	m.Stop("")
	snap := m.Snapshot()
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var restored Snapshot
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if restored.Dungeon != "json-test" {
		t.Fatalf("dungeon roundtrip failed: %q", restored.Dungeon)
	}
	if restored.Notes != "some notes" {
		t.Fatalf("notes roundtrip failed: %q", restored.Notes)
	}
	if len(restored.Loot) != 1 || restored.Loot[0].Name != "Roundtrip Item" || restored.Loot[0].Note != "test-note" {
		t.Fatal("loot roundtrip failed")
	}
}

func TestEventBackpressure(t *testing.T) {
	m := New()
	// Don't drain events — fill the buffer to test backpressure doesn't block
	m.Start("bp-test", "")
	for i := 0; i < 5000; i++ {
		m.AddLoot(LootEntry{Name: "Spam", Valor: 1, Count: 1, LastSeen: time.Now()})
	}
	// If we get here without deadlock, backpressure works
}

func BenchmarkSnapshot(b *testing.B) {
	m := New()
	m.Start("bench", "")
	for i := 0; i < 1000; i++ {
		m.AddLoot(LootEntry{Name: "BenchItem", Valor: float64(i), Count: i, LastSeen: time.Now(), Decision: favor.Decision{Verdict: "favor"}})
		m.AddXPGain("Skill", i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Snapshot()
	}
}
