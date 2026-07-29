package loot

import (
	"testing"
)

func newTestParser(t *testing.T) *Parser {
	p, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func TestParseLoot(t *testing.T) {
	p := newTestParser(t)
	ev := p.ParseLine(`[Status] Silk Scarf x3 added to inventory.`)
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.Kind != KindLoot {
		t.Fatalf("expected loot, got %s", ev.Kind)
	}
	if ev.ItemName != "Silk Scarf" {
		t.Fatalf("expected 'Silk Scarf', got %q", ev.ItemName)
	}
	if ev.Count != 3 {
		t.Fatalf("expected count 3, got %d", ev.Count)
	}
	if ev.Bonus {
		t.Fatal("expected bonus=false")
	}
}

func TestParseLootNoCount(t *testing.T) {
	p := newTestParser(t)
	ev := p.ParseLine(`[Status] Rusty Sword added to inventory.`)
	if ev == nil || ev.Count != 1 {
		t.Fatalf("expected count 1, got %d", ev.Count)
	}
}

func TestParseLootMultiWord(t *testing.T) {
	p := newTestParser(t)
	ev := p.ParseLine(`[Status] Potent Curse Ward Potion x1 added to inventory.`)
	if ev == nil || ev.ItemName != "Potent Curse Ward Potion" {
		t.Fatalf("expected multi-word name, got %q", ev.ItemName)
	}
}

func TestParseBonusLoot(t *testing.T) {
	p := newTestParser(t)
	ev := p.ParseLine(`Also found Silk Scarf x2 (speed bonus from Lycanthropy)`)
	if ev == nil {
		t.Fatal("expected bonus event")
	}
	if ev.Kind != KindLoot {
		t.Fatalf("expected loot, got %s", ev.Kind)
	}
	if !ev.Bonus {
		t.Fatal("expected bonus=true")
	}
	if ev.ItemName != "Silk Scarf" {
		t.Fatalf("expected 'Silk Scarf', got %q", ev.ItemName)
	}
	if ev.Count != 2 {
		t.Fatalf("expected count 2, got %d", ev.Count)
	}
}

func TestParseXP(t *testing.T) {
	p := newTestParser(t)
	ev := p.ParseLine(`[Status] You earned 150 XP in Sword.`)
	if ev == nil || ev.Kind != KindXP {
		t.Fatalf("expected xp event, got %v", ev)
	}
	if ev.Amount != 150 || ev.Skill != "Sword" {
		t.Fatalf("expected 150 XP in Sword, got %d in %s", ev.Amount, ev.Skill)
	}
}

func TestParseGold(t *testing.T) {
	p := newTestParser(t)
	ev := p.ParseLine(`[Status] You found 42 councils.`)
	if ev == nil || ev.Kind != KindGold {
		t.Fatalf("expected gold event, got %v", ev)
	}
	if ev.Amount != 42 {
		t.Fatalf("expected 42 gold, got %d", ev.Amount)
	}
}

func TestParseGoldSingular(t *testing.T) {
	p := newTestParser(t)
	ev := p.ParseLine(`[Status] You found 1 council.`)
	if ev == nil || ev.Kind != KindGold || ev.Amount != 1 {
		t.Fatal("singular gold parse failed")
	}
}

func TestParseDeath(t *testing.T) {
	p := newTestParser(t)
	ev := p.ParseLine(`[Status] You have died.`)
	if ev == nil || ev.Kind != KindDeath {
		t.Fatalf("expected death event, got %v", ev)
	}
}

func TestParseKill(t *testing.T) {
	p := newTestParser(t)
	ev := p.ParseLine(`[Status] You killed Giant Bat!`)
	if ev == nil || ev.Kind != KindKill {
		t.Fatalf("expected kill event, got %v", ev)
	}
	if ev.Killer != "Giant Bat" {
		t.Fatalf("expected 'Giant Bat', got %q", ev.Killer)
	}
}

func TestParseGather(t *testing.T) {
	p := newTestParser(t)
	ev := p.ParseLine(`[Status] You collected Pinecone x5.`)
	if ev == nil || ev.Kind != KindGather {
		t.Fatalf("expected gather event, got %v", ev)
	}
	if ev.ItemName != "Pinecone" || ev.Count != 5 {
		t.Fatalf("gather name/count mismatch")
	}
}

func TestParseGatherNoCount(t *testing.T) {
	p := newTestParser(t)
	ev := p.ParseLine(`[Status] You collected Herb!`)
	if ev == nil || ev.Kind != KindGather || ev.Count != 1 || ev.ItemName != "Herb" {
		t.Fatal("gather without count failed")
	}
}

func TestParseLevel(t *testing.T) {
	p := newTestParser(t)
	ev := p.ParseLine(`[Status] You are now level 25 in Sword!`)
	if ev == nil || ev.Kind != KindLevel {
		t.Fatalf("expected level event, got %v", ev)
	}
	if ev.Amount != 25 || ev.Skill != "Sword" {
		t.Fatalf("expected level 25 in Sword, got %d in %s", ev.Amount, ev.Skill)
	}
}

func TestParseEmptyLine(t *testing.T) {
	p := newTestParser(t)
	if ev := p.ParseLine(""); ev != nil {
		t.Fatal("empty line should return nil")
	}
}

func TestParseUnmatched(t *testing.T) {
	p := newTestParser(t)
	if ev := p.ParseLine(`[Status] Someone said something irrelevant.`); ev != nil {
		t.Fatalf("unmatched line should return nil, got %v", ev)
	}
}

func TestParseCRLF(t *testing.T) {
	p := newTestParser(t)
	ev := p.ParseLine("[Status] Apple x2 added to inventory.\r\n")
	if ev == nil || ev.ItemName != "Apple" || ev.Count != 2 {
		t.Fatal("CRLF line not parsed correctly")
	}
}

func TestCustomRegex(t *testing.T) {
	const custom = `\[Status\]\s+Found:\s+(.+?)\s+x(\d+)`
	p, err := New(custom)
	if err != nil {
		t.Fatalf("New with custom regex: %v", err)
	}
	ev := p.ParseLine("[Status] Found: Rare Gem x5")
	if ev == nil || ev.Kind != KindLoot {
		t.Fatal("custom regex should match loot")
	}
	if ev.ItemName != "Rare Gem" || ev.Count != 5 {
		t.Fatal("custom regex parse result mismatch")
	}
}

func TestSetRegex(t *testing.T) {
	p := newTestParser(t)
	if err := p.SetRegex(`\[Status\]\s+Got:\s+(.+?)\s+x(\d+)`); err != nil {
		t.Fatalf("SetRegex: %v", err)
	}
	ev := p.ParseLine("[Status] Got: Cool Item x3")
	if ev == nil || ev.ItemName != "Cool Item" {
		t.Fatal("SetRegex should update live")
	}
}

func TestBonusTakesPriority(t *testing.T) {
	p := newTestParser(t)
	// A line that starts with "Also found" should match bonus, not regular loot
	ev := p.ParseLine("Also found TestItem (speed bonus)")
	if ev == nil || !ev.Bonus {
		t.Fatal("'Also found' should match bonus pattern first")
	}
}
