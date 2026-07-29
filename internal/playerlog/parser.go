// Package playerlog parses Project Gorgon's Player.log lines.
//
// Player.log lives at:
//
//	%USERPROFILE%\AppData\LocalLow\Elder Game\Project Gorgon\Player.log
//
// Lines have a `[HH:MM:SS]` timestamp prefix followed by the message body.
// Supported event kinds:
//
//	login:    [HH:MM:SS] Welcome to Project Gorgon!
//	zone:     [HH:MM:SS] You have entered <zone>.
//	skill:    [HH:MM:SS] [Status] [WW] Skill 'Name' gained 0.00
package playerlog

import (
	"regexp"
	"strconv"
	"strings"
)

// Kind enumerates parsed event types.
type Kind string

const (
	KindLogin         Kind = "login"
	KindZone          Kind = "zone"
	KindSkill         Kind = "skill"
	KindUseAbility    Kind = "use_ability"
	KindOnAttackHitMe Kind = "on_attack_hit_me"
)

// Event is one parsed line from Player.log.
type Event struct {
	Raw         string // original line
	Kind        Kind
	Zone        string // zone name
	Skill       string // skill name
	Value       int    // skill tick value (always 0 for [WW] but future-proof)
	AbilityName string // ability name (from UseAbility / OnAttackHitMe)
	AbilityID   int    // ability ID
}

var (
	loginRe = regexp.MustCompile(`Welcome to Project Gorgon!`)
	zoneRe  = regexp.MustCompile(`You have entered (.+?)\.$`)
	// [HH:MM:SS] [Status] [WW] Skill 'Name' gained 0.00
	skillRe = regexp.MustCompile(`\[Status\]\s+\[WW\]\s+Skill '(.+?)' gained (\d+\.?\d*)`)
	// UseAbility(Ability(Name,ID))
	useAbilityRe = regexp.MustCompile(`UseAbility\(Ability\(([^,]+),(\d+)\)\)`)
	// entity_XXXXX: OnAttackHitMe(Ability(Name,ID))
	onAttackHitMeRe = regexp.MustCompile(`entity_\d+: OnAttackHitMe\(Ability\(([^,]+),(\d+)\)\)`)
)

// Parser converts Player.log lines into typed events.
type Parser struct{}

// New creates a playerlog parser.
func New() *Parser {
	return &Parser{}
}

// ParseLine tries each known pattern and returns the first match, or nil.
func (p *Parser) ParseLine(line string) *Event {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return nil
	}

	// Strip leading [HH:MM:SS] prefix if present.
	body := line
	if len(body) > 9 && body[0] == '[' && body[3] == ':' && body[6] == ':' {
		body = strings.TrimSpace(body[9:])
	}

	// Login
	if loginRe.MatchString(body) {
		return &Event{Raw: line, Kind: KindLogin}
	}

	// Zone transition
	if m := zoneRe.FindStringSubmatch(body); m != nil {
		return &Event{Raw: line, Kind: KindZone, Zone: strings.TrimSpace(m[1])}
	}

	// Skill tick
	if m := skillRe.FindStringSubmatch(body); m != nil {
		s := strings.TrimSpace(m[1])
		v := 0
		if f, err := strconv.ParseFloat(m[2], 64); err == nil {
			v = int(f)
		}
		return &Event{Raw: line, Kind: KindSkill, Skill: s, Value: v}
	}

	// UseAbility
	if m := useAbilityRe.FindStringSubmatch(body); m != nil {
		id, _ := strconv.Atoi(m[2])
		return &Event{Raw: line, Kind: KindUseAbility, AbilityName: strings.TrimSpace(m[1]), AbilityID: id}
	}

	// OnAttackHitMe
	if m := onAttackHitMeRe.FindStringSubmatch(body); m != nil {
		id, _ := strconv.Atoi(m[2])
		return &Event{Raw: line, Kind: KindOnAttackHitMe, AbilityName: strings.TrimSpace(m[1]), AbilityID: id}
	}

	return nil
}
