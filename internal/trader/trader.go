// Package trader manages NPC trader limits and weekly sales tracking.
package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// RefreshCycle is the fixed 7-day refresh window for all NPC traders.
// The user sets ResetDays/ResetHours as "remaining time" since the app
// cannot read game data — LastSale is computed backward from those values.
const RefreshCycle = 7 * 24 * time.Hour

// RefreshEvent records a trader's automatic reset event.
type RefreshEvent struct {
	ID          string    `json:"id,omitempty"`
	NPCName     string    `json:"npc_name"`
	Area        string    `json:"area"`
	SoldAtReset float64   `json:"sold_at_reset"`
	WeeklyLimit float64   `json:"weekly_limit"`
	ResetAt     time.Time `json:"reset_at"`
}

// eventIDSeq guarantees unique IDs even for events created within the same nanosecond.
var eventIDSeq atomic.Int64

// newEventID returns a time-based unique event ID.
func newEventID(t time.Time) string {
	return fmt.Sprintf("ev-%d-%d", t.UnixNano(), eventIDSeq.Add(1))
}

// Manager tracks NPC trader sales with rolling 7-day reset.
type Manager struct {
	mu             sync.RWMutex
	filePath       string
	historyPath    string
	traders        map[string]*Trader // key: NPC name
	refreshHistory []RefreshEvent
}

// Trader represents an NPC with weekly sell limits.
// RefreshCycle is always 7 days. ResetDays/ResetHours are the
// user-input "time remaining until next refresh" (since the app
// cannot read game data) and are used to compute LastSale.
type Trader struct {
	NPCName      string    `json:"npc_name"`
	Area         string    `json:"area"`
	WeeklyLimit  float64   `json:"weekly_limit"`
	SoldThisWeek float64   `json:"sold_this_week"`
	LastSale     time.Time `json:"last_sale"`   // computed from remaining time: now - (7d - resetRemaining)
	ResetDays    int       `json:"reset_days"`  // remaining days until next refresh (user input)
	ResetHours   int       `json:"reset_hours"` // remaining hours within that day (user input)
}

// New creates a Manager that persists to the given file path.
func New(filePath string) *Manager {
	historyPath := filePath
	if ext := filepath.Ext(historyPath); ext == ".json" {
		historyPath = historyPath[:len(historyPath)-len(".json")] + "-history.json"
	} else {
		historyPath = historyPath + "-history"
	}
	return &Manager{
		filePath:    filePath,
		historyPath: historyPath,
		traders:     make(map[string]*Trader),
	}
}

// Load reads traders and refresh history from disk.
func (m *Manager) Load() error {
	if err := m.loadTraders(); err != nil {
		return err
	}
	return m.loadHistory()
}

// remainingTime returns the user-stored remaining time as a duration.
func remainingTime(resetDays, resetHours int) time.Duration {
	d := time.Duration(resetDays)*24*time.Hour + time.Duration(resetHours)*time.Hour
	if d > RefreshCycle {
		d = RefreshCycle
	}
	if d < 0 {
		d = 0
	}
	return d
}

// lastSaleFromRemaining computes the LastSale timestamp from the user's
// stored remaining time so that TimeUntilReset returns that remaining time.
func lastSaleFromRemaining(rem time.Duration) time.Time {
	return time.Now().Add(-(RefreshCycle - rem))
}

// loadTraders reads traders from disk and migrates LastSale for the fixed 7-day cycle.
func (m *Manager) loadTraders() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.filePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var traders []*Trader
	if err := json.Unmarshal(data, &traders); err != nil {
		return err
	}

	m.traders = make(map[string]*Trader)
	for _, t := range traders {
		// Keep the persisted LastSale as the true cycle anchor so catchupMissed
		// can detect refreshes that elapsed while the app was closed. Only
		// legacy records (no LastSale) get it computed from remaining time.
		if t.LastSale.IsZero() {
			rem := remainingTime(t.ResetDays, t.ResetHours)
			t.LastSale = lastSaleFromRemaining(rem)
		}
		m.traders[t.NPCName] = t
	}
	return nil
}

// Save writes traders to disk.
func (m *Manager) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	traders := make([]*Trader, 0, len(m.traders))
	for _, t := range m.traders {
		traders = append(traders, t)
	}

	data, err := json.MarshalIndent(traders, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(m.filePath), 0755); err != nil {
		return err
	}
	return os.WriteFile(m.filePath, data, 0644)
}

// Ensure creates a trader entry if it doesn't exist (with zero limit).
// ResetDays/ResetHours are the user-stated remaining time — LastSale
// is computed from them to anchor the 7-day cycle.
func (m *Manager) Ensure(npcName, area string, resetDays, resetHours int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rem := remainingTime(resetDays, resetHours)
	ls := lastSaleFromRemaining(rem)

	if t, exists := m.traders[npcName]; exists {
		if area != "" {
			t.Area = area
		}
		t.ResetDays = resetDays
		t.ResetHours = resetHours
		t.LastSale = ls
		return nil
	}

	m.traders[npcName] = &Trader{
		NPCName:      npcName,
		Area:         area,
		WeeklyLimit:  0,
		SoldThisWeek: 0,
		LastSale:     ls,
		ResetDays:    resetDays,
		ResetHours:   resetHours,
	}
	return nil
}

// UpdateLimit updates just the weekly limit for a trader
func (m *Manager) UpdateLimit(npcName string, weeklyLimit float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, exists := m.traders[npcName]
	if !exists {
		return fmt.Errorf("trader %s not found", npcName)
	}

	t.WeeklyLimit = weeklyLimit
	return nil
}

// Remove removes a trader.
func (m *Manager) Remove(npcName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.traders, npcName)
}

// LogSale adds a sale amount to a trader, auto-resetting if needed.
// LastSale is recomputed from the trader's stored remaining time.
func (m *Manager) LogSale(npcName string, amount float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, exists := m.traders[npcName]
	if !exists {
		return nil
	}

	// Refresh immediately if past the 7-day window
	if !t.LastSale.IsZero() && time.Since(t.LastSale) >= RefreshCycle {
		m.refreshHistory = append(m.refreshHistory, RefreshEvent{
			ID:          newEventID(time.Now()),
			NPCName:     t.NPCName,
			Area:        t.Area,
			SoldAtReset: t.SoldThisWeek,
			WeeklyLimit: t.WeeklyLimit,
			ResetAt:     time.Now(),
		})
		t.SoldThisWeek = 0
		if err := m.saveHistoryLocked(); err != nil {
			log.Printf("trader: save refresh history: %v", err)
		}
	}

	t.SoldThisWeek += amount
	rem := remainingTime(t.ResetDays, t.ResetHours)
	t.LastSale = lastSaleFromRemaining(rem)
	return nil
}

// GetAll returns all traders. Does NOT auto-reset — use TimeUntilReset to check.
func (m *Manager) GetAll() []*Trader {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Trader, 0, len(m.traders))
	for _, t := range m.traders {
		result = append(result, t)
	}
	return result
}

// Get returns a specific trader by name.
func (m *Manager) Get(npcName string) *Trader {
	m.mu.RLock()
	defer m.mu.RUnlock()

	t, exists := m.traders[npcName]
	if !exists {
		return nil
	}

	// Return a copy to avoid race conditions
	copy := *t
	return &copy
}

// TimeUntilReset calculates duration until next reset for a trader.
func (m *Manager) TimeUntilReset(npcName string) time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	t, exists := m.traders[npcName]
	if !exists {
		return 0
	}

	// LastSale is always set (computed from remaining time), but handle zero guard
	if t.LastSale.IsZero() {
		return remainingTime(t.ResetDays, t.ResetHours)
	}

	remaining := RefreshCycle - time.Since(t.LastSale)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// SetSold directly sets the sold amount for a trader.
// Refreshes immediately if past the 7-day window, then applies the edit.
// LastSale is recomputed from the trader's stored remaining time.
func (m *Manager) SetSold(npcName string, amount float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, exists := m.traders[npcName]
	if !exists {
		return fmt.Errorf("trader %s not found", npcName)
	}

	// Refresh immediately if past the 7-day window before applying the edit
	if !t.LastSale.IsZero() && time.Since(t.LastSale) >= RefreshCycle {
		m.refreshHistory = append(m.refreshHistory, RefreshEvent{
			ID:          newEventID(time.Now()),
			NPCName:     t.NPCName,
			Area:        t.Area,
			SoldAtReset: t.SoldThisWeek,
			WeeklyLimit: t.WeeklyLimit,
			ResetAt:     time.Now(),
		})
		if err := m.saveHistoryLocked(); err != nil {
			log.Printf("trader: save refresh history: %v", err)
		}
	}

	t.SoldThisWeek = amount
	rem := remainingTime(t.ResetDays, t.ResetHours)
	t.LastSale = lastSaleFromRemaining(rem)
	return nil
}

// ScheduleEntry is one row in the refresh schedule.
type ScheduleEntry struct {
	NPCName      string        `json:"npc_name"`
	Area         string        `json:"area"`
	WeeklyLimit  float64       `json:"weekly_limit"`
	SoldThisWeek float64       `json:"sold_this_week"`
	Remaining    time.Duration `json:"-"` // internal, formatted below
	TimeUntil    string        `json:"time_until"`
}

// BulkEnsureByArea calls Ensure for every NPC name in the list.
// Returns a single error aggregating all failures, or nil if all succeeded.
func (m *Manager) BulkEnsureByArea(area string, npcs []string, resetDays, resetHours int) error {
	var errs []error
	for _, name := range npcs {
		if err := m.Ensure(name, area, resetDays, resetHours); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d errors: %v", len(errs), errs)
	}
	return nil
}

// GetRefreshSchedule returns all traders sorted by TimeUntilReset ascending
// (closest to refresh first).
func (m *Manager) GetRefreshSchedule() []ScheduleEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := make([]ScheduleEntry, 0, len(m.traders))
	for _, t := range m.traders {
		remaining := time.Duration(0)
		if !t.LastSale.IsZero() {
			remaining = RefreshCycle - time.Since(t.LastSale)
			if remaining < 0 {
				remaining = 0
			}
		} else {
			remaining = remainingTime(t.ResetDays, t.ResetHours)
		}
		entries = append(entries, ScheduleEntry{
			NPCName:      t.NPCName,
			Area:         t.Area,
			WeeklyLimit:  t.WeeklyLimit,
			SoldThisWeek: t.SoldThisWeek,
			Remaining:    remaining,
			TimeUntil:    FormatDuration(remaining),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Remaining < entries[j].Remaining
	})
	return entries
}

// Start launches a background goroutine that auto-refreshes expired traders.
// First catches up any refreshes missed while the app was offline, then ticks every 30s.
// Stops when ctx is cancelled. Saves history after each batch that changes anything.
func (m *Manager) Start(ctx context.Context) {
	// Catch up refreshes missed while the app was down (backdates events so the timer is accurate)
	m.catchupMissed()

	ticker := time.NewTicker(30 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				m.mu.Lock()
				m.saveHistoryLocked()
				m.mu.Unlock()
				return
			case <-ticker.C:
				m.autoRefresh()
			}
		}
	}()
}

// catchupMissed catches up refresh periods that elapsed while the app was offline.
// Backdates events to the scheduled reset time and advances LastSale accordingly,
// so TimeUntilReset returns the correct remaining time from the actual reset moment.
func (m *Manager) catchupMissed() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	dirty := false
	for _, t := range m.traders {
		if t.LastSale.IsZero() {
			continue
		}
		elapsed := now.Sub(t.LastSale)

		for elapsed >= RefreshCycle {
			scheduledReset := t.LastSale.Add(RefreshCycle)
			m.refreshHistory = append(m.refreshHistory, RefreshEvent{
				ID:          newEventID(scheduledReset),
				NPCName:     t.NPCName,
				Area:        t.Area,
				SoldAtReset: t.SoldThisWeek,
				WeeklyLimit: t.WeeklyLimit,
				ResetAt:     scheduledReset,
			})
			t.SoldThisWeek = 0
			t.LastSale = scheduledReset
			elapsed -= RefreshCycle
			dirty = true
		}
	}
	if dirty {
		if err := m.saveHistoryLocked(); err != nil {
			log.Printf("trader: save catch-up history: %v", err)
		}
	}
}

// autoRefresh checks all traders and resets any past their 7-day refresh window (real-time).
func (m *Manager) autoRefresh() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	dirty := false
	for _, t := range m.traders {
		if t.LastSale.IsZero() {
			continue
		}
		if !now.Before(t.LastSale.Add(RefreshCycle)) {
			m.refreshHistory = append(m.refreshHistory, RefreshEvent{
				ID:          newEventID(now),
				NPCName:     t.NPCName,
				Area:        t.Area,
				SoldAtReset: t.SoldThisWeek,
				WeeklyLimit: t.WeeklyLimit,
				ResetAt:     now,
			})
			t.SoldThisWeek = 0
			t.LastSale = now
			dirty = true
		}
	}
	if dirty {
		if err := m.saveHistoryLocked(); err != nil {
			log.Printf("trader: save refresh history: %v", err)
		}
	}
}

// loadHistory reads refresh history from disk. Events saved before the ID
// field existed get IDs assigned here; the file is rewritten immediately.
func (m *Manager) loadHistory() error {
	data, err := os.ReadFile(m.historyPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var events []RefreshEvent
	if err := json.Unmarshal(data, &events); err != nil {
		return err
	}
	migrated := false
	for i := range events {
		if events[i].ID == "" {
			events[i].ID = newEventID(events[i].ResetAt)
			migrated = true
		}
	}
	m.refreshHistory = events
	if migrated {
		// No lock held here: Load() calls loadHistory outside the mutex.
		if err := m.saveHistoryLocked(); err != nil {
			return err
		}
	}
	return nil
}

// DeleteHistoryEvent removes a refresh event by ID and persists the change.
func (m *Manager) DeleteHistoryEvent(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, e := range m.refreshHistory {
		if e.ID == id {
			m.refreshHistory = append(m.refreshHistory[:i], m.refreshHistory[i+1:]...)
			return m.saveHistoryLocked()
		}
	}
	return fmt.Errorf("history event %s not found", id)
}

// saveHistoryLocked persists refresh history to disk. Caller must hold mu write lock.
func (m *Manager) saveHistoryLocked() error {
	// Trim history to last 1000 events to keep the file bounded
	const maxEvents = 1000
	if len(m.refreshHistory) > maxEvents {
		m.refreshHistory = m.refreshHistory[len(m.refreshHistory)-maxEvents:]
	}
	data, err := json.MarshalIndent(m.refreshHistory, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.historyPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(m.historyPath, data, 0644)
}

// GetRefreshHistory returns all refresh events. If npcName is non-empty, filters by NPC.
func (m *Manager) GetRefreshHistory(npcName string) []RefreshEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if npcName == "" {
		result := make([]RefreshEvent, len(m.refreshHistory))
		copy(result, m.refreshHistory)
		return result
	}

	var result []RefreshEvent
	for _, e := range m.refreshHistory {
		if e.NPCName == npcName {
			result = append(result, e)
		}
	}
	return result
}

// ponytail: history is kept in-memory and saved on auto-refresh cycles.
// If the app crashes between refreshes, up to 30s of events may be lost.
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return "now"
	}

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return "less than a minute"
}
