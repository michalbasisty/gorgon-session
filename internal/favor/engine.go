// Package favor recommends where each looted item should go: gifted to a
// specific NPC (with a score), sold at a vendor, or consigned.
//
// The engine uses two pieces of CDN ground truth from npcs.json:
//
//  1. Preferences[] — keyword-based likes/hates with a `Pref` score.
//  2. Services[]   — `Store` (vendor), `Consignment` with `ItemTypes`.
//
// Matching rules (MVP):
//   - For a preference, ALL of its Keywords[] must be present in the item's
//     Keywords[]. Composite keywords containing ':' (e.g. "EquipmentSlot:Head")
//     are deferred and skipped, since matching them requires item-schema data
//     not yet loaded.
//   - Score = sum of matched preferences' Pref values. Love = positive.
//   - Hates contribute negative score (don't gift).
//   - If no NPC scores > 0 -> item is sellable. If any Consignment NPC's
//     ItemTypes intersect the item's Keywords -> prefer consignment over
//     vendor (higher expected return); else vendor.
package favor

import (
	"fmt"

	"github.com/yourname/gorgon-session/internal/cdn"
)

// Engine holds indexed NPC data and answers routing decisions.
type Engine struct {
	npcs      []npcRow
	byKeyword map[string][]int // npc pref keyword -> npc row indexes
}

type npcRow struct {
	internal    string
	name        string
	areaName    string
	areaLabel   string
	preferences []prefRow
	services    []svcRow
	itemGifts   []string
}

type prefRow struct {
	name     string
	desire   string
	keywords []string
	pref     float64
}

type svcRow struct {
	kind      string
	favor     string
	itemTypes []string
}

// FromNpcs builds an Engine from a cdn-loaded NpcsFile.
func FromNpcs(npcs cdn.NpcsFile) *Engine {
	e := &Engine{byKeyword: map[string][]int{}}
	for _, n := range npcs {
		row := npcRow{
			internal:  n.InternalName,
			name:      n.Name,
			areaName:  n.AreaName,
			areaLabel: n.AreaFriendly,
			itemGifts: n.ItemGifts,
		}
		for _, p := range n.Preferences {
			flat := prefRow{name: p.Name, desire: p.Desire, keywords: p.Keywords, pref: p.Pref}
			row.preferences = append(row.preferences, flat)
			for _, k := range p.Keywords {
				if isComposite(k) {
					continue
				}
				idx := len(e.npcs) // index of this npc row once appended below
				e.byKeyword[k] = append(e.byKeyword[k], idx)
			}
		}
		for _, s := range n.Services {
			row.services = append(row.services, svcRow{kind: s.Type, favor: s.Favor, itemTypes: s.ItemTypes})
		}
		e.npcs = append(e.npcs, row)
	}
	return e
}

// Decision is one routing suggestion for an item.
type Decision struct {
	Item         string   `json:"item"`
	Verdict      Verdict  `json:"verdict"`
	FavorTargets []Target `json:"favor_targets,omitempty"`
	SellReason   string   `json:"sell_reason,omitempty"`
}

// Verdict classifies the routing decision.
type Verdict string

const (
	VerdictFavor           Verdict = "favor"
	VerdictSellVendor      Verdict = "sell_vendor"
	VerdictSellConsignment Verdict = "sell_consignment"
	VerdictKeep            Verdict = "keep"
)

// Target is a single NPC favor suggestion with score.
type Target struct {
	NPC     string   `json:"npc"`
	Area    string   `json:"area"`
	Score   float64  `json:"score"`
	Matches []string `json:"matches"`
}

// Resolve produces a decision for one item identified by its keywords + value.
func (e *Engine) Resolve(itemName string, itemKeywords []string, itemValue float64) Decision {
	d := Decision{Item: itemName}
	kwSet := toSet(itemKeywords)

	type npcScore struct {
		idx     int
		score   float64
		matches []string
	}
	candidates := map[int]*npcScore{}

	for _, k := range itemKeywords {
		if isComposite(k) {
			continue
		}
		for _, idx := range e.byKeyword[k] {
			c, ok := candidates[idx]
			if !ok {
				c = &npcScore{idx: idx}
				candidates[idx] = c
			}
		}
	}

	for _, c := range candidates {
		row := &e.npcs[c.idx]
		for _, p := range row.preferences {
			if isAnyComposite(p.keywords) {
				continue
			}
			if !allIn(p.keywords, kwSet) {
				continue
			}
			c.score += p.pref
			c.matches = append(c.matches, p.name)
		}
	}

	for _, c := range candidates {
		if c.score > 0 {
			row := &e.npcs[c.idx]
			d.FavorTargets = append(d.FavorTargets, Target{
				NPC: row.name, Area: row.areaLabel, Score: c.score, Matches: c.matches,
			})
		}
	}
	sortTargets(d.FavorTargets)

	if len(d.FavorTargets) > 0 {
		d.Verdict = VerdictFavor
		return d
	}

	// No NPC loves it -> sell. Prefer consignment if any NPC accepts the
	// item's keywords as one of its ItemTypes. Vendor is the fallback.
	for _, row := range e.npcs {
		for _, s := range row.services {
			if s.kind != "Consignment" {
				continue
			}
			for _, t := range s.itemTypes {
				if kwSet[t] {
					d.Verdict = VerdictSellConsignment
					d.SellReason = "Consign via " + row.name + " (" + row.areaLabel + ")"
					return d
				}
			}
		}
	}
	d.Verdict = VerdictSellVendor
	d.SellReason = fmt.Sprintf("Vendor (value %g)", itemValue)
	return d
}

func isComposite(k string) bool {
	for i := 0; i < len(k); i++ {
		if k[i] == ':' {
			return true
		}
	}
	return false
}
func isAnyComposite(ks []string) bool {
	for _, k := range ks {
		if isComposite(k) {
			return true
		}
	}
	return false
}
func allIn(needles []string, hay map[string]bool) bool {
	for _, n := range needles {
		if isComposite(n) {
			return false
		}
		if !hay[n] {
			return false
		}
	}
	return true
}
func toSet(ks []string) map[string]bool {
	m := make(map[string]bool, len(ks))
	for _, k := range ks {
		m[k] = true
	}
	return m
}
// NPCRows returns the count of indexed NPC rows (for startup diagnostics).
func (e *Engine) NPCRows() int { return len(e.npcs) }

// KeywordKeys returns the count of distinct preference keywords indexed
// (composite keywords are excluded).
func (e *Engine) KeywordKeys() int { return len(e.byKeyword) }

// Summary is a debug payload enumerating NPCs that have at least one
// preference, used by `gorgon --dump-npcs`.
type Summary struct {
	NPCs         int          `json:"npcs"`
	WithFavors   int          `json:"with_favors"`
	Indexable    int          `json:"indexable_keywords"`
	NPCCatalog   []SummaryNPC `json:"npc_catalog"`
}

// SummaryNPC is one entry per NPC for the dump.
type SummaryNPC struct {
	Internal    string         `json:"internal"`
	Name        string         `json:"name"`
	Area        string         `json:"area"`
	Preferences []SummaryPref  `json:"preferences"`
	ItemGifts   []string       `json:"item_gifts,omitempty"`
	Services    []SummarySvc   `json:"services,omitempty"`
}

type SummaryPref struct {
	Name     string   `json:"name"`
	Desire   string   `json:"desire"`
	Keywords []string `json:"keywords"`
	Pref     float64  `json:"pref"`
	Favor    string   `json:"favor,omitempty"`
}

type SummarySvc struct {
	Type      string   `json:"type"`
	Favor     string   `json:"favor,omitempty"`
	ItemTypes []string `json:"item_types,omitempty"`
}

// Summary produces a JSON-serializable dump of every NPC with gift prefs.
func (e *Engine) Summary() Summary {
	out := Summary{NPCs: len(e.npcs), Indexable: len(e.byKeyword)}
	for _, r := range e.npcs {
		if len(r.preferences) == 0 && len(r.services) == 0 {
			continue
		}
		out.WithFavors++
		s := SummaryNPC{
			Internal: r.internal, Name: r.name, Area: r.areaLabel,
		}
		for _, p := range r.preferences {
			s.Preferences = append(s.Preferences, SummaryPref{
				Name: p.name, Desire: p.desire, Keywords: p.keywords, Pref: p.pref,
			})
		}
		s.ItemGifts = append(s.ItemGifts, r.itemGifts...)
		for _, sv := range r.services {
			s.Services = append(s.Services, SummarySvc{
				Type: sv.kind, Favor: sv.favor, ItemTypes: sv.itemTypes,
			})
		}
		out.NPCCatalog = append(out.NPCCatalog, s)
	}
	return out
}

func sortTargets(ts []Target) {
	for i := 1; i < len(ts); i++ {
		j := i
		for j > 0 && ts[j].Score > ts[j-1].Score {
			ts[j], ts[j-1] = ts[j-1], ts[j]
			j--
		}
	}
}

type NPCInfo struct {
	Name string `json:"name"`
	Area string `json:"area"`
}

func (e *Engine) NPCList() []NPCInfo {
	out := make([]NPCInfo, 0, len(e.npcs))
	for _, r := range e.npcs {
		if r.name == "" {
			continue
		}
		out = append(out, NPCInfo{Name: r.name, Area: r.areaLabel})
	}
	return out
}