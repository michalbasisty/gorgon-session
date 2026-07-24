// Package loot parses Project Gorgon chat-log lines into structured events.
//
// PG chat-log lines are tagged with a channel prefix like `[Status]` (no
// `[HH:MM:SS]` timestamp on chat-log lines; that prefix is only on Player.log
// lines). The speakable loot format GorgonSurveyTracker relies on is:
//
//	[Status] <item name> x<count> added to inventory.
//	[Status] <item name> added to inventory.               (no count)
//
// Secondary events you may want later:
//
//	[Status] <item name> x<n>? collected!
//	[Status] You earned <n> XP in <skill>.
//	Also found <item name> x<n>? (speed bonus...            (bonus find after a collection)
//
// The parser is regex-driven and configurable via config.LootRegex so you can
// change it for non-English locales. The default regex is the same shape
// GorgonSurveyTracker uses (see survey_tracker.py _INV_ADD_RE).
package loot

import (
	"regexp"
	"strconv"
	"strings"
)

// DefaultRegex matches the canonical `[Status] <item> x<qty>? added to inventory.`
// line. Capture group 1 = item name, group 2 (optional) = integer count.
// If group 2 is absent, count defaults to 1.
const DefaultRegex = `\[Status\]\s+(.+?)\s+(?:x(\d+)\s+)?added to inventory\.`

// BonusRegex matches "Also found <item> x<n>? (speed bonus..." follow-up lines.
// Same capture groups as DefaultRegex.
const BonusRegex = `Also found (.+?)(?:\s+x(\d+))?\s+\(speed bonus`

// Event is a parsed loot chat-log line.
type Event struct {
	Raw      string // original chat-log line
	ItemName string // captured item name (trimmed)
	Count    int    // captured count, or 1 if absent
	Bonus    bool   // true if this came from a "Also found ... (speed bonus)" line
}

// Parser converts raw chat-log lines into loot events. It is safe for
// concurrent use after construction.
type Parser struct {
	itemRe  *regexp.Regexp
	bonusRe *regexp.Regexp
	tagRe   *regexp.Regexp
}

// New builds a parser. If lootRegex is empty, DefaultRegex is used. If the
// supplied regex fails to compile, DefaultRegex is used as a fallback. The
// bonus regex is always Default-derived (BonusRegex) and not user-tunable in
// this first phase; future phases can expose it.
func New(lootRegex string) (*Parser, error) {
	pat := lootRegex
	if pat == "" {
		pat = DefaultRegex
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		re = regexp.MustCompile(DefaultRegex)
	}
	return &Parser{itemRe: re, bonusRe: regexp.MustCompile(BonusRegex), tagRe: tagStripper()}, nil
}

// ParseLine returns nil if the line is not a loot event; otherwise an Event.
// It checks BOTH the main "added to inventory" pattern and the "Also found
// (speed bonus)" follow-up line, so a single loot drop that yields a bonus
// item generates two events (the main one first, the bonus one right after).
func (p *Parser) ParseLine(line string) *Event {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return nil
	}
	stripped := line // chat-log lines have no [<time>] prefix; keep as-is for the regexes (which all start with `[Status]` or `Also found`)
	_ = stripped

	// "Also found X (speed bonus)" follow-up line.
	if m := p.bonusRe.FindStringSubmatch(line); m != nil {
		return p.buildEvent(line, m, true)
	}
	// Main "X added to inventory." line.
	if m := p.itemRe.FindStringSubmatch(line); m != nil {
		return p.buildEvent(line, m, false)
	}
	return nil
}

func (p *Parser) buildEvent(raw string, m []string, bonus bool) *Event {
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
	return &Event{Raw: raw, ItemName: name, Count: count, Bonus: bonus}
}

// tagStripper is retained for forward-compatibility (future combat / ability
// chat-log lines may carry inline <color> tags the way the in-game chat
// window does); not applied to [Status] loot lines today, but harmless to
// keep around.
func tagStripper() *regexp.Regexp {
	return regexp.MustCompile(`</?color[^>]*>|<icon=\d+>|<[^>]+>`)
}