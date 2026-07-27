// Package trader manages NPC trader limits and weekly sales tracking.
package trader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Manager tracks NPC trader sales with rolling 7-day reset.
type Manager struct {
	mu       sync.RWMutex
	filePath string
	traders  map[string]*Trader // key: NPC name
}

// Trader represents an NPC with weekly sell limits.
type Trader struct {
	NPCName      string        `json:"npc_name"`
	Area         string        `json:"area"`
	WeeklyLimit  float64       `json:"weekly_limit"`
	SoldThisWeek float64       `json:"sold_this_week"`
	LastSale     time.Time     `json:"last_sale"` // when last sale was made
	ResetDays    int           `json:"reset_days"` // days until reset
	ResetHours   int           `json:"reset_hours"` // additional hours
}

// New creates a Manager that persists to the given file path.
func New(filePath string) *Manager {
	return &Manager{
		filePath: filePath,
		traders:  make(map[string]*Trader),
	}
}

// Load reads traders from disk.
func (m *Manager) Load() error {
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

// Add adds a new trader with the given limit and reset duration.
func (m *Manager) Add(npcName, area string, weeklyLimit float64, resetDays, resetHours int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.traders[npcName]; exists {
		return nil // already exists
	}

	m.traders[npcName] = &Trader{
		NPCName:      npcName,
		Area:         area,
		WeeklyLimit:  weeklyLimit,
		SoldThisWeek: 0,
		LastSale:     time.Time{}, // no sale yet
		ResetDays:    resetDays,
		ResetHours:   resetHours,
	}
	return nil
}

// Ensure creates a trader entry if it doesn't exist (with zero limit)
func (m *Manager) Ensure(npcName, area string, resetDays, resetHours int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.traders[npcName]; exists {
		return nil
	}

	m.traders[npcName] = &Trader{
		NPCName:      npcName,
		Area:         area,
		WeeklyLimit:  0,
		SoldThisWeek: 0,
		LastSale:     time.Time{},
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

// Update modifies an existing trader's area, weekly limit, and reset duration.
func (m *Manager) Update(npcName, area string, weeklyLimit float64, resetDays, resetHours int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, exists := m.traders[npcName]
	if !exists {
		return fmt.Errorf("trader %s not found", npcName)
	}

	t.Area = area
	t.WeeklyLimit = weeklyLimit
	if resetDays > 0 || resetHours > 0 {
		t.ResetDays = resetDays
		t.ResetHours = resetHours
	}
	return nil
}

// LogSale adds a sale amount to a trader, auto-resetting if needed.
func (m *Manager) LogSale(npcName string, amount float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, exists := m.traders[npcName]
	if !exists {
		return nil
	}

	// Check if we need to reset based on duration since last sale
	resetDuration := time.Duration(t.ResetDays)*24*time.Hour + time.Duration(t.ResetHours)*time.Hour
	if !t.LastSale.IsZero() && time.Since(t.LastSale) > resetDuration {
		t.SoldThisWeek = 0
	}

	t.SoldThisWeek += amount
	t.LastSale = time.Now()
	return nil
}

// GetAll returns all traders, auto-resetting any that are past their reset time.
func (m *Manager) GetAll() []*Trader {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*Trader, 0, len(m.traders))
	now := time.Now()

	for _, t := range m.traders {
		// Auto-reset if past reset duration since last sale
		if !t.LastSale.IsZero() {
			resetDuration := time.Duration(t.ResetDays)*24*time.Hour + time.Duration(t.ResetHours)*time.Hour
			if now.Sub(t.LastSale) > resetDuration {
				t.SoldThisWeek = 0
			}
		}
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

	// If no sale yet, return full reset duration
	if t.LastSale.IsZero() {
		return time.Duration(t.ResetDays)*24*time.Hour + time.Duration(t.ResetHours)*time.Hour
	}

	// Calculate remaining time
	resetDuration := time.Duration(t.ResetDays)*24*time.Hour + time.Duration(t.ResetHours)*time.Hour
	remaining := resetDuration - time.Since(t.LastSale)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// FormatDuration formats a duration into a human-readable string.
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
