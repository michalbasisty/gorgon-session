// Package loot parses Project Gorgon chat-log lines into structured events.
//
// PG chat-log lines are tagged with a channel prefix like `[Status]` (no
// `[HH:MM:SS]` timestamp on chat-log lines; that prefix is only on Player.log
// lines).
//
// Supported event kinds:
//   loot:   [Status] <item> x<qty>? added to inventory.
//   loot:   Also found <item> x<qty>? (speed bonus...
//   xp:     [Status] You earned <N> XP in <Skill>.
//   death:  [Status] You have died.
//   gather: [Status] You collected <item> x<qty>? or <item> collected!
//   level:  [Status] You are now level <N> in <Skill>!
//   gold:   [Status] You found <N> councils?.
package loot

import (
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// DefaultRegex matches the canonical `[Status] <item> x<qty>? added to inventory.`
const DefaultRegex = `\[Status\]\s+(.+?)\s+(?:x(\d+)\s+)?added to inventory\.`

const BonusRegex = `Also found (.+?)(?:\s+x(\d+))?\s+\(speed bonus`
const XPRegex = `\[Status\]\s+You earned (\d+) XP in (.+?)\.`
const DeathRegex = `\[Status\]\s+You have died\.`
const KillRegex = `\[Status\]\s+You killed (.+?)!`
const GatherRegex = `\[Status\]\s+You collected (.+?)(?:\s+x(\d+))?(?:\.|!)`
const LevelRegex = `\[Status\]\s+You are now level (\d+) in (.+?)!`
const GoldRegex = `\[Status\]\s+You found (\d+) councils?\.?`

// Kind enumerates parsed event types.
type Kind string

const (
	KindLoot   Kind = "loot"
	KindXP     Kind = "xp"
	KindDeath  Kind = "death"
	KindKill   Kind = "kill"
	KindGather Kind = "gather"
	KindLevel  Kind = "level"
	KindGold   Kind = "gold"
)

// Event is one parsed line from a chat log.
type Event struct {
	Raw      string // original line
	Kind     Kind   // which pattern matched
	ItemName string // loot/gather item name
	Count    int    // loot/gather count (default 1)
	Bonus    bool   // loot: speed-bonus find
	Skill    string // xp/level skill name
	Amount   int    // xp/gold amount or new level
	Killer   string // kill: mob name
}

// Parser converts raw chat-log lines into typed events.
type Parser struct {
	mu       sync.RWMutex
	itemRe   *regexp.Regexp
	bonusRe  *regexp.Regexp
	xpRe     *regexp.Regexp
	deathRe  *regexp.Regexp
	killRe   *regexp.Regexp
	gatherRe *regexp.Regexp
	levelRe  *regexp.Regexp
	goldRe   *regexp.Regexp
}

// New builds a parser. If lootRegex is empty, DefaultRegex is used.
func New(lootRegex string) (*Parser, error) {
	pat := lootRegex
	if pat == "" {
		pat = DefaultRegex
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		log.Printf("loot regex %q failed to compile, using default: %v", pat, err)
		re = regexp.MustCompile(DefaultRegex)
	}
	return &Parser{
		itemRe:   re,
		bonusRe:  regexp.MustCompile(BonusRegex),
		xpRe:     regexp.MustCompile(XPRegex),
		deathRe:  regexp.MustCompile(DeathRegex),
		killRe:   regexp.MustCompile(KillRegex),
		gatherRe: regexp.MustCompile(GatherRegex),
		levelRe:  regexp.MustCompile(LevelRegex),
		goldRe:   regexp.MustCompile(GoldRegex),
	}, nil
}

// SetRegex compiles and updates the item loot regex live.
func (p *Parser) SetRegex(pat string) error {
	if pat == "" {
		pat = DefaultRegex
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.itemRe = re
	p.mu.Unlock()
	return nil
}

// ParseLine tries every known pattern and returns the first match, or nil.
func (p *Parser) ParseLine(line string) *Event {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return nil
	}

	// Speed-bonus follow-up (checked first so it wins over loot pattern)
	if m := p.bonusRe.FindStringSubmatch(line); m != nil {
		return p.buildItem(line, m, true, KindLoot)
	}

	// Loot: "X added to inventory."
	p.mu.RLock()
	lootRe := p.itemRe
	p.mu.RUnlock()
	if m := lootRe.FindStringSubmatch(line); m != nil {
		return p.buildItem(line, m, false, KindLoot)
	}

	// XP: "You earned N XP in Skill."
	if m := p.xpRe.FindStringSubmatch(line); m != nil {
		amt, _ := strconv.Atoi(m[1])
		return &Event{Raw: line, Kind: KindXP, Amount: amt, Skill: strings.TrimSpace(m[2])}
	}

	// Gold: "You found N councils."
	if m := p.goldRe.FindStringSubmatch(line); m != nil {
		amt, _ := strconv.Atoi(m[1])
		return &Event{Raw: line, Kind: KindGold, Amount: amt}
	}

	// Death: "You have died."
	if p.deathRe.MatchString(line) {
		return &Event{Raw: line, Kind: KindDeath}
	}

	// Kill: "You killed Mob!"
	if m := p.killRe.FindStringSubmatch(line); m != nil {
		return &Event{Raw: line, Kind: KindKill, Killer: strings.TrimSpace(m[1])}
	}

	// Gather: "You collected item xN" or "You collected item!"
	if m := p.gatherRe.FindStringSubmatch(line); m != nil {
		return p.buildItem(line, m, false, KindGather)
	}

	// Level: "You are now level N in Skill!"
	if m := p.levelRe.FindStringSubmatch(line); m != nil {
		lvl, _ := strconv.Atoi(m[1])
		return &Event{Raw: line, Kind: KindLevel, Amount: lvl, Skill: strings.TrimSpace(m[2])}
	}

	return nil
}

func (p *Parser) buildItem(raw string, m []string, bonus bool, kind Kind) *Event {
	name := strings.TrimSpace(m[1])
	if name == "" {
		return nil
	}
	count := 1
	if len(m) > 2 && m[2] != "" {
		if n, err := strconv.Atoi(m[2]); err == nil && n > 0 {
			count = n
		}
	}
	return &Event{Raw: raw, Kind: kind, ItemName: name, Count: count, Bonus: bonus}
}
