package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/michalbasisty/gorgon-session/internal/cdn"
	"github.com/michalbasisty/gorgon-session/internal/config"
	"github.com/michalbasisty/gorgon-session/internal/favor"
	"github.com/michalbasisty/gorgon-session/internal/logtail"
	"github.com/michalbasisty/gorgon-session/internal/loot"
	"github.com/michalbasisty/gorgon-session/internal/playerlog"
	"github.com/michalbasisty/gorgon-session/internal/session"
)

// ---- fixtures -----------------------------------------------------------

// testEngine builds a favor engine with one food-loving NPC.
func testEngine() *favor.Engine {
	return favor.FromNpcs(cdn.NpcsFile{
		"npc_foodie": {
			InternalName: "npc_foodie",
			Name:         "Foodie Joe",
			AreaName:     "Serbule",
			AreaFriendly: "Serbule",
			Preferences: []cdn.Preference{
				{Name: "Loves food", Desire: "Love", Keywords: []string{"Food"}, Pref: 10},
				{Name: "Likes fruit", Desire: "Like", Keywords: []string{"Fruit"}, Pref: 5},
			},
		},
	})
}

func testItemIndex() itemIndex {
	return indexItemsByName(cdn.ItemsFile{
		"item_1001": {ItemID: 1001, Name: "Iron Ore", Keywords: []string{"Ore"}, Value: 10, IconID: 555},
		"item_1002": {ItemID: 1002, Name: "Apple", Keywords: []string{"Food", "Fruit"}, Value: 5},
	})
}

func newLootParser(t *testing.T) *loot.Parser {
	t.Helper()
	p, err := loot.New("")
	if err != nil {
		t.Fatalf("loot parser: %v", err)
	}
	return p
}

// newManager starts a fresh session manager, cleaned up by the test.
func newManager(t *testing.T) *session.Manager {
	t.Helper()
	mgr := session.New()
	t.Cleanup(mgr.Close)
	if err := mgr.Start("Serbule Crypt", ""); err != nil {
		t.Fatalf("start session: %v", err)
	}
	return mgr
}

// pumpChatLines runs pipeline in a goroutine, pushes lines straight into the
// (never-started) tailer's Lines channel, then closes the channel so the
// goroutine drains deterministically and exits. The context cancel in cleanup
// is belt-and-suspenders against leaks.
func pumpChatLines(t *testing.T, p *loot.Parser, idx itemIndex, eng *favor.Engine, mgr *session.Manager, lines ...string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	tail := logtail.New("")
	done := make(chan struct{})
	go func() {
		defer close(done)
		pipeline(ctx, tail, p, idx, eng, mgr, &cdn.Client{Root: "http://cdn.example.com"}, cdn.Version("v480"))
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("pipeline goroutine leaked after cancel")
		}
	})
	for _, ln := range lines {
		select {
		case tail.Lines <- ln:
		case <-time.After(2 * time.Second):
			t.Fatalf("pipeline did not consume line: %q", ln)
		}
	}
	close(tail.Lines)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pipeline did not exit after channel close")
	}
}

// pumpPlayerLines is the playerPipeline equivalent of pumpChatLines.
func pumpPlayerLines(t *testing.T, mgr *session.Manager, lines ...string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	tail := logtail.NewFileTailer("")
	done := make(chan struct{})
	go func() {
		defer close(done)
		playerPipeline(ctx, tail, playerlog.New(), mgr)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("player pipeline goroutine leaked after cancel")
		}
	})
	for _, ln := range lines {
		select {
		case tail.Lines <- ln:
		case <-time.After(2 * time.Second):
			t.Fatalf("player pipeline did not consume line: %q", ln)
		}
	}
	close(tail.Lines)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("player pipeline did not exit after channel close")
	}
}

// ---- pipeline -----------------------------------------------------------

func TestPipelineChatLogRouting(t *testing.T) {
	mgr := newManager(t)
	pumpChatLines(t, newLootParser(t), testItemIndex(), testEngine(), mgr,
		"[Status] Iron Ore x3 added to inventory.",
		"[Status] Apple x2 added to inventory.",
		"[Status] You earned 250 XP in Fishing.",
		"[Status] You found 42 councils.",
		"[Status] You killed Giant Bat!",
		"[Status] You collected Pinecone x5.",
	)

	snap := mgr.Snapshot()

	if got := len(snap.Loot); got != 2 {
		t.Fatalf("expected 2 loot entries, got %d", got)
	}
	ore := snap.Loot[0]
	if ore.Name != "Iron Ore" || ore.Count != 3 || ore.ItemID != 1001 {
		t.Errorf("Iron Ore entry: got %+v", ore)
	}
	if ore.IconURL != "http://cdn.example.com/v480/icons/555.png" {
		t.Errorf("Iron Ore IconURL: got %q", ore.IconURL)
	}
	if ore.Decision.Verdict == "" {
		t.Error("Iron Ore decision verdict is empty")
	}

	apple := snap.Loot[1]
	if apple.Name != "Apple" || apple.Count != 2 {
		t.Errorf("Apple entry: got %+v", apple)
	}
	if apple.Decision.Verdict != favor.VerdictFavor {
		t.Errorf("Apple verdict: got %q, want %q", apple.Decision.Verdict, favor.VerdictFavor)
	}

	if len(snap.XPGains) != 1 || snap.XPGains[0].Skill != "Fishing" || snap.XPGains[0].Amount != 250 {
		t.Errorf("XPGains: got %+v", snap.XPGains)
	}
	if snap.TotalGold != 42 {
		t.Errorf("TotalGold: got %d, want 42", snap.TotalGold)
	}
	if len(snap.Kills) != 1 || snap.Kills[0].Mob != "Giant Bat" {
		t.Errorf("Kills: got %+v", snap.Kills)
	}
	if len(snap.Gathering) != 1 || snap.Gathering[0].Item != "Pinecone" || snap.Gathering[0].Count != 5 {
		t.Errorf("Gathering: got %+v", snap.Gathering)
	}
}

func TestPipelineUnknownItem(t *testing.T) {
	mgr := newManager(t)
	pumpChatLines(t, newLootParser(t), indexItemsByName(cdn.ItemsFile{}), favor.FromNpcs(nil), mgr,
		"[Status] Mystery Widget x2 added to inventory.",
	)

	snap := mgr.Snapshot()
	if len(snap.Loot) != 1 {
		t.Fatalf("expected 1 loot entry, got %d", len(snap.Loot))
	}
	e := snap.Loot[0]
	if e.Name != "Mystery Widget" {
		t.Errorf("Name: got %q, want raw name", e.Name)
	}
	if e.ItemID != 0 {
		t.Errorf("ItemID: got %d, want 0", e.ItemID)
	}
	if e.Count != 2 {
		t.Errorf("Count: got %d, want 2", e.Count)
	}
	if e.Decision.Verdict != favor.VerdictSellVendor {
		t.Errorf("Verdict: got %q, want %q (engine fallback)", e.Decision.Verdict, favor.VerdictSellVendor)
	}
}

// ---- playerPipeline -----------------------------------------------------

func TestPlayerPipelineZoneAndCorpseSearch(t *testing.T) {
	mgr := newManager(t)
	pumpPlayerLines(t, mgr,
		"Downloading Map [9e4c] GUID 9e4c for area Serbule Crypt runtime key 9e4c[Map_AreaSerbule2]",
		"[08:15:30] LocalPlayer: ProcessTalkScreen(870486, \"Search Corpse of Giant Bat\", \"...\", \"\", [], System.String[], 1, Corpse)",
		"[12:34:56] UseAbility(Ability(Arrow Volley,123))",
		"[12:34:56] entity_99999: OnAttackHitMe(Ability(Arrow Volley,123)). Evaded = False",
	)

	snap := mgr.Snapshot()
	if snap.Zone != "Serbule Crypt" {
		t.Errorf("Zone: got %q, want %q", snap.Zone, "Serbule Crypt")
	}
	if len(snap.Kills) != 1 || snap.Kills[0].Mob != "Giant Bat" {
		t.Errorf("Kills: got %+v", snap.Kills)
	}
}

// ---- indexItemsByName / Lookup -------------------------------------------

func TestItemIndexLookup(t *testing.T) {
	idx := testItemIndex()

	tests := []struct {
		name     string
		query    string
		wantID   int
		wantName string
	}{
		{"exact", "Iron Ore", 1001, "Iron Ore"},
		{"case-insensitive", "IRON ORE", 1001, "Iron Ore"},
		{"whitespace trimmed", "  Apple  ", 1002, "Apple"},
		{"unknown placeholder", "Mystery Widget", 0, "Mystery Widget"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := idx.Lookup(tt.query)
			if got.Item.ItemID != tt.wantID {
				t.Errorf("Lookup(%q).ItemID = %d, want %d", tt.query, got.Item.ItemID, tt.wantID)
			}
			if got.Item.Name != tt.wantName {
				t.Errorf("Lookup(%q).Name = %q, want %q", tt.query, got.Item.Name, tt.wantName)
			}
		})
	}
}

// ---- backups -------------------------------------------------------------

func TestBackupOnce(t *testing.T) {
	// Route config.Path() into a temp home so the real user config is untouched.
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".gorgon-session")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(`{"http_addr":"127.0.0.1:7777"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	reportDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(reportDir, "session-20260101-120000.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "notes.txt"), []byte("not a report"), 0o644); err != nil {
		t.Fatal(err)
	}

	backupDir := t.TempDir()
	backupOnce(config.Config{ReportDir: reportDir}, backupDir)

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("expected one timestamped backup dir, got %v", entries)
	}
	bDir := filepath.Join(backupDir, entries[0].Name())

	for _, want := range []string{"config.json", filepath.Join("reports", "session-20260101-120000.json")} {
		if _, err := os.Stat(filepath.Join(bDir, want)); err != nil {
			t.Errorf("backup missing %s: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(bDir, "reports", "notes.txt")); !os.IsNotExist(err) {
		t.Errorf("non-report file should not be backed up (err=%v)", err)
	}
}

func TestPruneOldBackups(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= 50; i++ {
		if err := os.Mkdir(filepath.Join(dir, fmt.Sprintf("202601%02d-000000", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stray := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(stray, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	pruneOldBackups(dir, 48)

	// The 48 newest survive; the 2 oldest (by timestamp name) are pruned.
	for i := 3; i <= 50; i++ {
		if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("202601%02d-000000", i))); err != nil {
			t.Errorf("backup %d should survive: %v", i, err)
		}
	}
	for i := 1; i <= 2; i++ {
		if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("202601%02d-000000", i))); !os.IsNotExist(err) {
			t.Errorf("backup %d should be pruned (err=%v)", i, err)
		}
	}
	if _, err := os.Stat(stray); err != nil {
		t.Errorf("non-backup file must be untouched: %v", err)
	}
}
