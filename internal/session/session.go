// Package session owns dungeon-session state: a time-bounded capture of
// looted items, XP gains, deaths, kills, gathering, and more. It is the
// central piece the HTTP server and both log pipelines touch.
package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/michalbasisty/gorgon-session/internal/favor"
)

// XPGain is one skill-XP tick during a session.
type XPGain struct {
	Skill  string    `json:"skill"`
	Amount int       `json:"amount"`
	Time   time.Time `json:"time"`
}

// DeathEvent records one death.
type DeathEvent struct {
	Time   time.Time `json:"time"`
	Killer string    `json:"killer,omitempty"`
}

// KillEvent records one mob kill.
type KillEvent struct {
	Mob  string    `json:"mob"`
	Time time.Time `json:"time"`
}

// GatherEvent records a gathered item.
type GatherEvent struct {
	Item  string    `json:"item"`
	Count int       `json:"count"`
	Time  time.Time `json:"time"`
}

// LevelUp records hitting a new level in a skill.
type LevelUp struct {
	Skill string    `json:"skill"`
	Level int       `json:"level"`
	Time  time.Time `json:"time"`
}

// ZoneEntry records a zone change.
type ZoneEntry struct {
	Zone string    `json:"zone"`
	Time time.Time `json:"time"`
}

// AbilityUseEvent and CombatHitEvent (ability use / hit / evade tracking)
// were removed: combat log data requires a VIP subscription.

var (
	ErrAlreadyRunning = errors.New("a session is already running")
	ErrNotRunning     = errors.New("no session is currently running")
)

// State enumerates the lifecycle.
type State string

const (
	Idle    State = "idle"
	Running State = "running"
	Stopped State = "stopped"
)

// Manager holds one active session at a time (design simplification for the
// dungeon-session MVP; can be lifted trivially to multi-session later).
type Manager struct {
	mu        sync.RWMutex
	state     State
	dungeon   string
	notes     string
	startedAt time.Time
	endedAt   time.Time

	loot       []LootEntry    // chronological (aggregated by item)
	lootEvents []LootEvent    // raw loot occurrences
	byItem     map[string]int // itemName -> index in loot
	counts     map[string]int // itemName -> total count

	// non-loot events collected during the session
	xpGains   []XPGain
	deaths    []DeathEvent
	kills     []KillEvent
	gathering []GatherEvent
	levelUps  []LevelUp
	totalGold int

	// zone tracking (from Player.log)
	zone        string
	zoneHistory []ZoneEntry

	subscribersMtx sync.RWMutex
	subscribers    map[chan Event]struct{}
	closed         bool
}

// Event is anything the UI may want to push to clients. Only Loot kinds
// are emitted today; reserved for crafting later.
type Event struct {
	Kind    string      `json:"kind"` // "loot", "session_start", "session_stop"
	Time    time.Time   `json:"time"`
	Payload interface{} `json:"payload,omitempty"`
}

// LootEntry is one item's aggregated record in a session: count + decisions.
type LootEntry struct {
	Name      string         `json:"name"`
	ItemID    int            `json:"item_id"`
	IconURL   string         `json:"icon_url,omitempty"`
	Valor     float64        `json:"value"`
	Count     int            `json:"count"`
	Bonus     bool           `json:"bonus"`
	FirstSeen time.Time      `json:"first_seen"`
	LastSeen  time.Time      `json:"last_seen"`
	Decision  favor.Decision `json:"decision"`
	Note      string         `json:"note,omitempty"`
}

// LootEvent is one raw loot occurrence, preserved for post-session analytics
// such as item->mob attribution.
type LootEvent struct {
	Name   string    `json:"name"`
	ItemID int       `json:"item_id,omitempty"`
	Count  int       `json:"count"`
	Bonus  bool      `json:"bonus,omitempty"`
	Value  float64   `json:"value,omitempty"`
	Time   time.Time `json:"time"`
}

// New constructs a Manager.
func New() *Manager {
	return &Manager{
		state:       Idle,
		loot:        []LootEntry{},
		byItem:      map[string]int{},
		counts:      map[string]int{},
		subscribers: map[chan Event]struct{}{},
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
	m.lootEvents = m.lootEvents[:0]
	m.byItem = map[string]int{}
	m.counts = map[string]int{}
	m.xpGains = m.xpGains[:0]
	m.deaths = m.deaths[:0]
	m.kills = m.kills[:0]
	m.gathering = m.gathering[:0]
	m.levelUps = m.levelUps[:0]
	m.totalGold = 0
	m.zone = ""
	m.zoneHistory = m.zoneHistory[:0]
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
		State:       m.state,
		Dungeon:     m.dungeon,
		Notes:       m.notes,
		StartedAt:   m.startedAt,
		EndedAt:     m.endedAt,
		Loot:        make([]LootEntry, len(m.loot)),
		LootEvents:  make([]LootEvent, len(m.lootEvents)),
		XPGains:     make([]XPGain, len(m.xpGains)),
		Deaths:      make([]DeathEvent, len(m.deaths)),
		Kills:       make([]KillEvent, len(m.kills)),
		Gathering:   make([]GatherEvent, len(m.gathering)),
		LevelUps:    make([]LevelUp, len(m.levelUps)),
		TotalGold:   m.totalGold,
		Zone:        m.zone,
		ZoneHistory: make([]ZoneEntry, len(m.zoneHistory)),
	}
	copy(out.Loot, m.loot)
	copy(out.LootEvents, m.lootEvents)
	copy(out.XPGains, m.xpGains)
	copy(out.Deaths, m.deaths)
	copy(out.Kills, m.kills)
	copy(out.Gathering, m.gathering)
	copy(out.LevelUps, m.levelUps)
	copy(out.ZoneHistory, m.zoneHistory)
	return out
}

// Snapshot is a JSON-serializable copy of the current session.
type Snapshot struct {
	State           State             `json:"state"`
	Dungeon         string            `json:"dungeon"`
	Notes           string            `json:"notes,omitempty"`
	StartedAt       time.Time         `json:"started_at"`
	EndedAt         time.Time         `json:"ended_at"`
	Loot            []LootEntry       `json:"loot"`
	LootEvents      []LootEvent       `json:"loot_events,omitempty"`
	XPGains         []XPGain          `json:"xp_gains,omitempty"`
	Deaths          []DeathEvent      `json:"deaths,omitempty"`
	Kills           []KillEvent       `json:"kills,omitempty"`
	Gathering       []GatherEvent     `json:"gathering,omitempty"`
	LevelUps        []LevelUp         `json:"level_ups,omitempty"`
	TotalGold       int               `json:"total_gold"`
	Zone            string       `json:"zone,omitempty"`
	ZoneHistory     []ZoneEntry  `json:"zone_history,omitempty"`
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
	now := time.Now()
	if e.FirstSeen.IsZero() {
		e.FirstSeen = now
	}
	if e.LastSeen.IsZero() {
		e.LastSeen = e.FirstSeen
	}

	idx, ok := m.byItem[e.Name]
	if ok {
		m.loot[idx].Count += e.Count
		if e.LastSeen.After(m.loot[idx].LastSeen) {
			m.loot[idx].LastSeen = e.LastSeen
		}
		// bonus items merge into the same row as the main item
	} else {
		m.byItem[e.Name] = len(m.loot)
		m.loot = append(m.loot, e)
	}

	// Preserve raw loot event for post-session analytics (e.g. drop source attribution).
	m.lootEvents = append(m.lootEvents, LootEvent{
		Name:   e.Name,
		ItemID: e.ItemID,
		Count:  e.Count,
		Bonus:  e.Bonus,
		Value:  e.Valor,
		Time:   e.LastSeen,
	})

	sent := m.loot[m.byItem[e.Name]]
	sentCopy := sent // copy under lock so we can release before publish
	m.mu.Unlock()

	m.publish(Event{
		Kind:    "loot",
		Time:    sentCopy.LastSeen,
		Payload: sentCopy,
	})
}

// AddXPGain records a skill XP tick.
func (m *Manager) AddXPGain(skill string, amount int) {
	m.mu.Lock()
	if m.state != Running {
		m.mu.Unlock()
		return
	}
	e := XPGain{Skill: skill, Amount: amount, Time: time.Now()}
	m.xpGains = append(m.xpGains, e)
	m.mu.Unlock()
	m.publish(Event{Kind: "xp", Time: e.Time, Payload: e})
}

// AddDeath records a death.
func (m *Manager) AddDeath(killer string) {
	m.mu.Lock()
	if m.state != Running {
		m.mu.Unlock()
		return
	}
	e := DeathEvent{Time: time.Now(), Killer: killer}
	m.deaths = append(m.deaths, e)
	m.mu.Unlock()
	m.publish(Event{Kind: "death", Time: e.Time, Payload: e})
}

// AddKill records a mob kill.
func (m *Manager) AddKill(mob string) {
	m.mu.Lock()
	if m.state != Running {
		m.mu.Unlock()
		return
	}
	e := KillEvent{Mob: mob, Time: time.Now()}
	m.kills = append(m.kills, e)
	m.mu.Unlock()
	m.publish(Event{Kind: "kill", Time: e.Time, Payload: e})
}

// AddGather records a gathered item.
func (m *Manager) AddGather(item string, count int) {
	m.mu.Lock()
	if m.state != Running {
		m.mu.Unlock()
		return
	}
	if count <= 0 {
		count = 1
	}
	e := GatherEvent{Item: item, Count: count, Time: time.Now()}
	m.gathering = append(m.gathering, e)
	m.mu.Unlock()
	m.publish(Event{Kind: "gather", Time: e.Time, Payload: e})
}

// AddLevelUp records reaching a new level in a skill.
func (m *Manager) AddLevelUp(skill string, level int) {
	m.mu.Lock()
	if m.state != Running {
		m.mu.Unlock()
		return
	}
	e := LevelUp{Skill: skill, Level: level, Time: time.Now()}
	m.levelUps = append(m.levelUps, e)
	m.mu.Unlock()
	m.publish(Event{Kind: "level", Time: e.Time, Payload: e})
}

// AddGold adds to the session gold total.
func (m *Manager) AddGold(amount int) {
	m.mu.Lock()
	if m.state != Running {
		m.mu.Unlock()
		return
	}
	m.totalGold += amount
	total := m.totalGold
	m.mu.Unlock()
	m.publish(Event{Kind: "gold", Time: time.Now(), Payload: map[string]int{"total": total}})
}

// SetZone updates the current zone (from Player.log).
func (m *Manager) SetZone(zone string) {
	m.mu.Lock()
	m.zone = zone
	m.zoneHistory = append(m.zoneHistory, ZoneEntry{Zone: zone, Time: time.Now()})
	m.mu.Unlock()
	m.publish(Event{Kind: "zone", Time: time.Now(), Payload: map[string]string{"zone": zone}})
}

// RemoveLoot removes entries matching name from the active session.
func (m *Manager) RemoveLoot(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != Running {
		return false
	}
	idx, ok := m.byItem[name]
	if !ok {
		return false
	}
	m.loot = append(m.loot[:idx], m.loot[idx+1:]...)
	delete(m.byItem, name)
	delete(m.counts, name)
	for i := range m.loot {
		m.byItem[m.loot[i].Name] = i
	}
	return true
}

// SetLootNote sets the note on a loot entry in the active session.
func (m *Manager) SetLootNote(name, note string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != Running {
		return false
	}
	idx, ok := m.byItem[name]
	if !ok {
		return false
	}
	m.loot[idx].Note = note
	return true
}

// SetNotes updates the active session's notes.
func (m *Manager) SetNotes(notes string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notes = notes
}

// Subscribe registers a new event subscriber channel and returns an
// unsubscribe function. Delivery is best-effort; slow subscribers may drop
// events under backpressure.
func (m *Manager) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 256)
	m.subscribersMtx.Lock()
	if m.closed {
		m.subscribersMtx.Unlock()
		close(ch)
		return ch, func() {}
	}
	m.subscribers[ch] = struct{}{}
	m.subscribersMtx.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			m.subscribersMtx.Lock()
			delete(m.subscribers, ch)
			m.subscribersMtx.Unlock()
		})
	}
	return ch, unsubscribe
}

// Events is a compatibility helper returning a subscribed channel.
// Prefer Subscribe when the caller can explicitly unsubscribe.
func (m *Manager) Events() <-chan Event {
	ch, _ := m.Subscribe()
	return ch
}

// Close releases internal resources.
func (m *Manager) Close() {
	m.subscribersMtx.Lock()
	m.closed = true
	m.subscribers = map[chan Event]struct{}{}
	m.subscribersMtx.Unlock()
}

func (m *Manager) publish(e Event) {
	m.subscribersMtx.RLock()
	if m.closed || len(m.subscribers) == 0 {
		m.subscribersMtx.RUnlock()
		return
	}
	subs := make([]chan Event, 0, len(m.subscribers))
	for ch := range m.subscribers {
		subs = append(subs, ch)
	}
	m.subscribersMtx.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- e:
		default:
			// drop on backpressure rather than block; UI will refresh on next event
		}
	}
}
