// Package prices tracks item price history over time, stored as a JSON file
// alongside session data. Each entry records when an item was seen and at
// what value, so the UI can show average/median prices and price trends.
package prices

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Entry records one item occurrence in a session.
type Entry struct {
	Price float64   `json:"price"`
	Count int       `json:"count"`
	Date  time.Time `json:"date"`
}

// Store maps item name → list of historical price entries.
type Store struct {
	mu    sync.RWMutex
	data  map[string][]Entry
	path  string
	dirty bool
}

// New loads or creates a price history file at the given path.
func New(path string) *Store {
	s := &Store{data: map[string][]Entry{}, path: path}
	b, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(b, &s.data)
	}
	// Ensure map is never nil
	if s.data == nil {
		s.data = map[string][]Entry{}
	}
	return s
}

// Add records one or more price observations for an item.
func (s *Store) Add(name string, price float64, count int) {
	s.mu.Lock()
	s.data[name] = append(s.data[name], Entry{
		Price: price,
		Count: count,
		Date:  time.Now(),
	})
	s.dirty = true
	s.mu.Unlock()
}

// AddBatch records many items at once (e.g., from a session stop).
func (s *Store) AddBatch(entries map[string]struct{ Price, Count float64 }) {
	s.mu.Lock()
	now := time.Now()
	for name, e := range entries {
		s.data[name] = append(s.data[name], Entry{
			Price: e.Price,
			Count: int(e.Count),
			Date:  now,
		})
	}
	s.dirty = true
	s.mu.Unlock()
}

// All returns a copy of the full price history.
func (s *Store) All() map[string][]Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]Entry, len(s.data))
	for k, v := range s.data {
		entries := make([]Entry, len(v))
		copy(entries, v)
		out[k] = entries
	}
	return out
}

// Summary returns average, last, and occurrence count per item.
type ItemSummary struct {
	Average float64 `json:"average"`
	Last    float64 `json:"last"`
	Count   int     `json:"count"`
	Entries int     `json:"entries"` // total occurrences
}

// Summarize computes per-item stats from the full history.
func (s *Store) Summarize() map[string]ItemSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]ItemSummary, len(s.data))
	for name, entries := range s.data {
		if len(entries) == 0 {
			continue
		}
		var sum float64
		totalCount := 0
		for _, e := range entries {
			sum += e.Price * float64(e.Count)
			totalCount += e.Count
		}
		last := entries[len(entries)-1].Price
		out[name] = ItemSummary{
			Average: sum / float64(totalCount),
			Last:    last,
			Count:   totalCount,
			Entries: len(entries),
		}
	}
	return out
}

// Save writes the price history to disk if dirty.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.path, b, 0o644); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

// Get returns entries for a single item.
func (s *Store) Get(name string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := s.data[name]
	out := make([]Entry, len(entries))
	copy(out, entries)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Date.Before(out[j].Date)
	})
	return out
}
