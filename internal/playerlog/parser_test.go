package playerlog

import (
	"testing"
)

func newTestParser() *Parser {
	return New()
}

func TestParseLogin(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine(`[12:34:56] Welcome to Project Gorgon!`)
	if ev == nil || ev.Kind != KindLogin {
		t.Fatalf("expected login event, got %v", ev)
	}
}

func TestParseZone(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine(`[12:34:56] You have entered Serbule.`)
	if ev == nil || ev.Kind != KindZone {
		t.Fatalf("expected zone event, got %v", ev)
	}
	if ev.Zone != "Serbule" {
		t.Fatalf("expected 'Serbule', got %q", ev.Zone)
	}
}

func TestParseZoneMultiWord(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine(`[12:34:56] You have entered Serbule Hills.`)
	if ev.Zone != "Serbule Hills" {
		t.Fatalf("expected 'Serbule Hills', got %q", ev.Zone)
	}
}

func TestParseZoneFriendlyName(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine(`Downloading Map [9e4c] GUID 9e4c for area Serbule Hills runtime key 9e4c[Map_AreaSerbule2]`)
	if ev == nil || ev.Kind != KindZone {
		t.Fatalf("expected zone event, got %v", ev)
	}
	if ev.Zone != "Serbule Hills" {
		t.Fatalf("expected 'Serbule Hills', got %q", ev.Zone)
	}
}

func TestParseNoZoneFromCInit2(t *testing.T) {
	p := newTestParser()
	if ev := p.ParseLine(`[09:33:58] Sent C_INIT2 for AreaSerbule2`); ev != nil {
		t.Fatalf("expected nil for C_INIT2 line, got %v", ev)
	}
}

func TestParseNoZoneFromLoadingLevel(t *testing.T) {
	p := newTestParser()
	if ev := p.ParseLine(`[09:30:19] LOADING LEVEL ChooseCharacter`); ev != nil {
		t.Fatalf("expected nil for LOADING LEVEL line, got %v", ev)
	}
}

func TestParseSkill(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine(`[12:34:56] [Status] [WW] Skill 'Sword' gained 0.00`)
	if ev == nil || ev.Kind != KindSkill {
		t.Fatalf("expected skill event, got %v", ev)
	}
	if ev.Skill != "Sword" {
		t.Fatalf("expected 'Sword', got %q", ev.Skill)
	}
}

func TestParseSkillWithFractional(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine(`[12:34:56] [Status] [WW] Skill 'Fire Magic' gained 0.50`)
	if ev == nil || ev.Kind != KindSkill || ev.Skill != "Fire Magic" {
		t.Fatal("skill parse failed")
	}
}

func TestParseUseAbility(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine(`UseAbility(Ability(Arrow Volley,123))`)
	if ev == nil || ev.Kind != KindUseAbility {
		t.Fatalf("expected use_ability event, got %v", ev)
	}
	if ev.AbilityName != "Arrow Volley" {
		t.Fatalf("expected 'Arrow Volley', got %q", ev.AbilityName)
	}
	if ev.AbilityID != 123 {
		t.Fatalf("expected ability ID 123, got %d", ev.AbilityID)
	}
}

func TestParseUseAbilityWithTimestamp(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine(`[12:34:56] UseAbility(Ability(Fireball,456))`)
	if ev == nil || ev.Kind != KindUseAbility {
		t.Fatalf("expected use_ability event, got %v", ev)
	}
	if ev.AbilityName != "Fireball" || ev.AbilityID != 456 {
		t.Fatal("ability with timestamp parse failed")
	}
}

func TestParseOnAttackHitMe(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine(`entity_12345: OnAttackHitMe(Ability(Arrow Volley,123))`)
	if ev == nil || ev.Kind != KindOnAttackHitMe {
		t.Fatalf("expected on_attack_hit_me event, got %v", ev)
	}
	if ev.AbilityName != "Arrow Volley" {
		t.Fatalf("expected 'Arrow Volley', got %q", ev.AbilityName)
	}
	if ev.AbilityID != 123 {
		t.Fatalf("expected ability ID 123, got %d", ev.AbilityID)
	}
}

func TestParseOnAttackHitMeWithTimestamp(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine(`[12:34:56] entity_99999: OnAttackHitMe(Ability(Slice,789))`)
	if ev == nil || ev.Kind != KindOnAttackHitMe || ev.AbilityName != "Slice" || ev.AbilityID != 789 {
		t.Fatal("on_attack_hit_me with timestamp failed")
	}
}

func TestParseOnAttackHitMeAbilityEvaded(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine(`[12:34:56] entity_99999: OnAttackHitMe(Ability(Slice,789)). Evaded = True`)
	if ev == nil || ev.Kind != KindOnAttackHitMe {
		t.Fatalf("expected on_attack_hit_me event, got %v", ev)
	}
	if ev.AbilityName != "Slice" || ev.AbilityID != 789 {
		t.Fatalf("expected Slice/789, got %q/%d", ev.AbilityName, ev.AbilityID)
	}
	if !ev.Evaded {
		t.Fatal("expected Evaded=true")
	}
}

func TestParseOnAttackHitMeNamed(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine(`[08:15:31] entity_868259: OnAttackHitMe(Toxin Bomb 8). Evaded = False`)
	if ev == nil || ev.Kind != KindOnAttackHitMe {
		t.Fatalf("expected on_attack_hit_me event, got %v", ev)
	}
	if ev.AbilityName != "Toxin Bomb 8" {
		t.Fatalf("expected 'Toxin Bomb 8', got %q", ev.AbilityName)
	}
	if ev.AbilityID != 0 {
		t.Fatalf("expected ability ID 0 for name-only hit lines, got %d", ev.AbilityID)
	}
	if ev.Evaded {
		t.Fatal("expected Evaded=false")
	}
}

func TestParseOnAttackHitMeNamedEvaded(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine(`[08:15:31] entity_868259: OnAttackHitMe(Toxin Bomb 8). Evaded = True`)
	if ev == nil || ev.Kind != KindOnAttackHitMe {
		t.Fatalf("expected on_attack_hit_me event, got %v", ev)
	}
	if !ev.Evaded {
		t.Fatal("expected Evaded=true")
	}
}

func TestParseOnAttackHitMeIncomingLocalPlayerIgnoredNamed(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine(`[08:15:31] localPlayer - entity_966629: OnAttackHitMe(Arrow). Evaded = False`)
	if ev != nil {
		t.Fatalf("expected nil for incoming localPlayer hit line, got %v", ev)
	}
}

func TestParseOnAttackHitMeIncomingLocalPlayerIgnoredAbilityForm(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine(`[08:15:31] localPlayer - entity_966629: OnAttackHitMe(Ability(Arrow,1234))`)
	if ev != nil {
		t.Fatalf("expected nil for incoming localPlayer hit line, got %v", ev)
	}
}

func TestParseCorpseSearch(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine(`[08:15:30] LocalPlayer: ProcessTalkScreen(870486, "Search Corpse of Deceased Guardian", "...", "", [], System.String[], 1, Corpse)`)
	if ev == nil || ev.Kind != KindCorpseSearch {
		t.Fatalf("expected corpse_search event, got %v", ev)
	}
	if ev.Mob != "Deceased Guardian" {
		t.Fatalf("expected mob 'Deceased Guardian', got %q", ev.Mob)
	}
}

func TestParseEmptyLine(t *testing.T) {
	p := newTestParser()
	if ev := p.ParseLine(""); ev != nil {
		t.Fatal("empty line should return nil")
	}
}

func TestParseUnmatchedLine(t *testing.T) {
	p := newTestParser()
	if ev := p.ParseLine(`[12:34:56] Something completely different.`); ev != nil {
		t.Fatal("unmatched line should return nil")
	}
}

func TestParseCRLF(t *testing.T) {
	p := newTestParser()
	ev := p.ParseLine("[12:34:56] You have entered Serbule.\r\n")
	if ev == nil || ev.Kind != KindZone || ev.Zone != "Serbule" {
		t.Fatal("CRLF line not parsed correctly")
	}
}

func TestParseBodyWithoutTimestamp(t *testing.T) {
	p := newTestParser()
	// Some lines might not have timestamp prefix
	ev := p.ParseLine(`Welcome to Project Gorgon!`)
	if ev == nil || ev.Kind != KindLogin {
		t.Fatal("lines without timestamp should still match")
	}
}
