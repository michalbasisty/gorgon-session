package prices

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddAndSummarize(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "prices.json"))
	s.Add("Iron Ore", 100, 2)
	s.Add("Iron Ore", 200, 2)
	s.Add("Coal", 50, 1)

	sum := s.Summarize()
	if len(sum) != 2 {
		t.Fatalf("Summarize() has %d items, want 2: %v", len(sum), sum)
	}

	iron, ok := sum["Iron Ore"]
	if !ok {
		t.Fatalf("Summarize() missing Iron Ore: %v", sum)
	}
	// weighted average = (100*2 + 200*2) / 4 = 150
	if iron.Average != 150 || iron.Last != 200 || iron.Count != 4 || iron.Entries != 2 {
		t.Errorf("Iron Ore summary = %+v, want avg 150, last 200, count 4, entries 2", iron)
	}

	coal := sum["Coal"]
	if coal.Average != 50 || coal.Last != 50 || coal.Count != 1 || coal.Entries != 1 {
		t.Errorf("Coal summary = %+v, want avg 50, last 50, count 1, entries 1", coal)
	}
}

func TestAddBatch(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "prices.json"))
	s.AddBatch(map[string]struct{ Price, Count float64}{
		"Gem": {Price: 500, Count: 3},
	})
	got := s.Get("Gem")
	if len(got) != 1 || got[0].Price != 500 || got[0].Count != 3 {
		t.Fatalf("Get after AddBatch = %+v, want one 500x3 entry", got)
	}
}

func TestGetSortsChronologically(t *testing.T) {
	// Load a store whose on-disk entries are out of order; Get must sort by date.
	path := filepath.Join(t.TempDir(), "prices.json")
	raw := `{"Iron Ore":[
		{"price":200,"count":1,"date":"2026-01-02T00:00:00Z"},
		{"price":100,"count":1,"date":"2026-01-01T00:00:00Z"}
	]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	s := New(path)
	got := s.Get("Iron Ore")
	if len(got) != 2 {
		t.Fatalf("Get = %+v, want 2 entries", got)
	}
	if got[0].Price != 100 || got[1].Price != 200 {
		t.Errorf("Get order = [%v %v], want [100 200] (sorted by date)", got[0].Price, got[1].Price)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "prices.json")
	s := New(path)
	s.Add("Iron Ore", 100, 2)
	s.Add("Iron Ore", 200, 1)
	s.Add("Coal", 50, 1)

	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Saving again with nothing new is a no-op.
	if err := s.Save(); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	// Round-trip equality. Note: time.Time is compared with .Equal() because
	// the in-memory copy carries a monotonic clock reading that JSON drops.
	fresh := New(path)
	got := fresh.All()
	want := s.All()
	if len(got) != len(want) {
		t.Fatalf("round-trip mismatch: %d items, want %d", len(got), len(want))
	}
	for k, v := range want {
		g, ok := got[k]
		if !ok || len(g) != len(v) {
			t.Errorf("round-trip mismatch for %q: %+v, want %+v", k, g, v)
			continue
		}
		for i := range v {
			if g[i].Price != v[i].Price || g[i].Count != v[i].Count || !g[i].Date.Equal(v[i].Date) {
				t.Errorf("round-trip entry mismatch for %q[%d]: %+v, want %+v", k, i, g[i], v[i])
			}
		}
	}
}

func TestEmptyStore(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "missing.json"))

	if sum := s.Summarize(); len(sum) != 0 {
		t.Errorf("Summarize on empty store = %v, want empty", sum)
	}
	if all := s.All(); len(all) != 0 {
		t.Errorf("All on empty store = %v, want empty", all)
	}
	if got := s.Get("nothing"); len(got) != 0 {
		t.Errorf("Get on empty store = %v, want empty", got)
	}
	if err := s.Save(); err != nil {
		t.Errorf("Save on empty store: %v", err)
	}

	// Adding to an empty store still works (map is never nil).
	s.Add("X", 1, 1)
	if sum := s.Summarize(); len(sum) != 1 {
		t.Errorf("Summarize after Add = %v, want 1 item", sum)
	}
}
