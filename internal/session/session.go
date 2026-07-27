// Package session owns dungeon-session state: a time-bounded capture of
// looted items with rolling decisions. It is the central piece the HTTP
// server and the chat-log pipeline both touch.
package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/yourname/gorgon-session/internal/favor"
)

var (
	ErrAlreadyRunning = errors.New("a session is already running")
	ErrNotRunning     = errors.New("no session is currently running")
)

// State enumerates the lifecycle.
type State string

const (
	Idle       State = "idle"
	Running    State = "running"
	Stopped    State = "stopped"
)

// Manager holds one active session at a time (design simplification for the
// dungeon-session MVP; can be lifted trivially to multi-session later).
type Manager struct {
	mu     sync.RWMutex
	state  State
	dungeon string
	notes   string
	startedAt time.Time
	endedAt   time.Time

	loot   []LootEntry   // chronological
	byItem map[string]int // itemName -> index in loot
	counts map[string]int // itemName -> total count

	events     chan Event // events for SSE subscribers
	stopCh     chan struct{}
	subscribersMtx sync.Mutex
}

// Event is anything the UI may want to push to clients. Only Loot kinds
// are emitted today; reserved for combat/crafting later.
type Event struct {
	Kind    string      `json:"kind"`     // "loot", "session_start", "session_stop"
	Time    time.Time   `json:"time"`
	Payload interface{} `json:"payload,omitempty"`
}

// LootEntry is one item's aggregated record in a session: count + decisions.
type LootEntry struct {
	Name      string          `json:"name"`
	ItemID    int             `json:"item_id"`
	IconURL   string          `json:"icon_url,omitempty"`
	Valor     float64         `json:"value"`
	Count     int             `json:"count"`
	Bonus     bool            `json:"bonus"`
	FirstSeen time.Time       `json:"first_seen"`
	LastSeen  time.Time       `json:"last_seen"`
	Decision  favor.Decision  `json:"decision"`
}

// New constructs a Manager.
func New() *Manager {
	return &Manager{
		state:   Idle,
		loot:    []LootEntry{},
		byItem:  map[string]int{},
		counts:  map[string]int{},
		events:  make(chan Event, 256),
	}
}

// Start begins a session. Returns ErrAlreadyRunning if one is active.
func (m *Manager) Start(dungeon, notes string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == Running {
		return ErrAlreadyRunning
	}
	m.state = Running
	m.dungeon = dungeon
	m.notes = notes
	m.startedAt = time.Now()
	m.endedAt = time.Time{}
	m.loot = m.loot[:0]
	m.byItem = map[string]int{}
	m.counts = map[string]int{}
	m.publish(Event{Kind: "session_start", Time: m.startedAt, Payload: map[string]string{"dungeon": dungeon, "notes": notes}})
	return nil
}

// Stop finalizes the active session and writes a report JSON.
func (m *Manager) Stop(reportDir string) error {
	m.mu.Lock()
	if m.state != Running {
		m.mu.Unlock()
		return ErrNotRunning
	}
	m.state = Stopped
	m.endedAt = time.Now()
	snap := m.snapshotLocked()
	m.mu.Unlock()

	m.publish(Event{Kind: "session_stop", Time: m.endedAt, Payload: map[string]int{"items": len(snap.Loot)}})

	if reportDir != "" {
		_ = os.MkdirAll(reportDir, 0o755)
		fn := filepath.Join(reportDir, "session-"+snap.StartedAt.Format("20060102-150405")+".json")
		b, _ := json.MarshalIndent(snap, "", "  ")
		_ = os.WriteFile(fn, b, 0o644)
	}
	return nil
}

// State returns the current lifecycle snapshot (immutable copy).
func (m *Manager) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshotLocked()
}

func (m *Manager) snapshotLocked() Snapshot {
	out := Snapshot{
		State:     m.state,
		Dungeon:   m.dungeon,
		Notes:     m.notes,
		StartedAt: m.startedAt,
		EndedAt:   m.endedAt,
		Loot:      make([]LootEntry, len(m.loot)),
	}
	copy(out.Loot, m.loot)
	return out
}

// Snapshot is a JSON-serializable copy of the current session.
type Snapshot struct {
	State     State       `json:"state"`
	Dungeon   string      `json:"dungeon"`
	Notes     string      `json:"notes,omitempty"`
	StartedAt time.Time   `json:"started_at"`
	EndedAt   time.Time   `json:"ended_at"`
	Loot      []LootEntry `json:"loot"`
}

// AddLoot records one looted item (or increments an existing entry).
// `count` from the parsed chat-log line (e.g. "x12 added to inventory")
// is added to the running total for this item.
func (m *Manager) AddLoot(e LootEntry) {
	m.mu.Lock()
	if m.state != Running {
		m.mu.Unlock()
		return
	}
	if e.Count <= 0 {
		e.Count = 1
	}
	idx, ok := m.byItem[e.Name]
	if ok {
		m.loot[idx].Count += e.Count
		m.loot[idx].LastSeen = e.LastSeen
		// bonus items merge into the same row as the main item
	} else {
		m.byItem[e.Name] = len(m.loot)
		m.loot = append(m.loot, e)
	}
	sent := m.loot[m.byItem[e.Name]]
	sentCopy := sent // copy under lock so we can release before publish
	m.mu.Unlock()

	m.publish(Event{
		Kind:    "loot",
		Time:    sentCopy.LastSeen,
		Payload: sentCopy,
	})
}

// Events returns the broadcast channel SSE handlers consume.
func (m *Manager) Events() <-chan Event { return m.events }

// Close releases internal resources.
func (m *Manager) Close() {
	close(m.events)
}

func (m *Manager) publish(e Event) {
	select {
	case m.events <- e:
	default:
		// drop on backpressure rather than block; UI will refresh on next event
	}
}