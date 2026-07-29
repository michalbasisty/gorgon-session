package favor

import (
	"testing"

	"github.com/michalbasisty/gorgon-session/internal/cdn"
)

// minimal NPC data for tests
func testNpcs() cdn.NpcsFile {
	return cdn.NpcsFile{
		"npc_foodie": {
			InternalName: "npc_foodie",
			Name:         "Foodie Joe",
			AreaName:     "Serbule",
			AreaFriendly: "Serbule",
			Preferences: []cdn.Preference{
				{Name: "Loves food", Desire: "Love", Keywords: []string{"Food"}, Pref: 10},
				{Name: "Likes fruit", Desire: "Like", Keywords: []string{"Fruit"}, Pref: 5},
			},
		},
		"npc_blacksmith": {
			InternalName: "npc_blacksmith",
			Name:         "Smithy Sue",
			AreaName:     "Serbule",
			AreaFriendly: "Serbule",
			Preferences: []cdn.Preference{
				{Name: "Loves weapons", Desire: "Love", Keywords: []string{"Weapon"}, Pref: 12},
				{Name: "Loves head armor", Desire: "Love", Keywords: []string{"EquipmentSlot:Head", "Armor"}, Pref: 15},
			},
			Services: []cdn.Service{
				{Type: "Consignment", ItemTypes: []string{"Weapon"}},
			},
		},
		"npc_consignor": {
			InternalName: "npc_consignor",
			Name:         "Trader Tim",
			AreaName:     "Serbule",
			AreaFriendly: "Serbule",
			Services: []cdn.Service{
				{Type: "Consignment", ItemTypes: []string{"Gem"}},
			},
		},
		"npc_hater": {
			InternalName: "npc_hater",
			Name:         "Grumpy Gus",
			AreaName:     "Serbule",
			AreaFriendly: "Serbule",
			Preferences: []cdn.Preference{
				{Name: "Hates junk", Desire: "Hate", Keywords: []string{"Junk"}, Pref: -10},
			},
		},
	}
}

func TestFavorMatch(t *testing.T) {
	e := FromNpcs(testNpcs())
	d := e.ResolveItem(cdn.Item{Name: "Apple", Keywords: []string{"Food", "Fruit"}, Value: 5})
	if d.Verdict != VerdictFavor {
		t.Fatalf("expected favor, got %s", d.Verdict)
	}
	// Apple should match Foodie Joe for both food + fruit
	if len(d.FavorTargets) == 0 {
		t.Fatal("expected at least one favor target")
	}
	if d.FavorTargets[0].NPC != "Foodie Joe" {
		t.Fatalf("expected Foodie Joe, got %s", d.FavorTargets[0].NPC)
	}
	if d.FavorTargets[0].Score != 15 { // 10 (food) + 5 (fruit)
		t.Fatalf("expected score 15, got %.1f", d.FavorTargets[0].Score)
	}
}

func TestFavorSingleKeyword(t *testing.T) {
	e := FromNpcs(testNpcs())
	d := e.ResolveItem(cdn.Item{Name: "Bread", Keywords: []string{"Food"}, Value: 3})
	if d.Verdict != VerdictFavor {
		t.Fatalf("expected favor, got %s", d.Verdict)
	}
	if len(d.FavorTargets) != 1 || d.FavorTargets[0].NPC != "Foodie Joe" {
		t.Fatal("expected Foodie Joe as single target")
	}
}

func TestNoMatch(t *testing.T) {
	e := FromNpcs(testNpcs())
	d := e.ResolveItem(cdn.Item{Name: "Rock", Keywords: []string{"Stone", "Misc"}, Value: 1})
	if d.Verdict != VerdictSellVendor {
		t.Fatalf("expected sell_vendor for unmatched item, got %s", d.Verdict)
	}
}

func TestConsignmentMatch(t *testing.T) {
	e := FromNpcs(testNpcs())
	d := e.ResolveItem(cdn.Item{Name: "Ruby", Keywords: []string{"Gem"}, Value: 100})
	if d.Verdict != VerdictSellConsignment {
		t.Fatalf("expected sell_consignment for gem, got %s", d.Verdict)
	}
	if d.SellReason == "" {
		t.Fatal("expected sell reason")
	}
}

func TestCompositeKeyword(t *testing.T) {
	e := FromNpcs(testNpcs())
	d := e.ResolveItem(cdn.Item{Name: "Iron Helm", Keywords: []string{"Armor", "EquipmentSlot:Head"}, EquipmentSlot: "Head", Value: 20})
	if d.Verdict != VerdictFavor {
		t.Fatalf("expected favor from composite keyword match, got %s", d.Verdict)
	}
	if d.FavorTargets[0].NPC != "Smithy Sue" {
		t.Fatalf("expected Smithy Sue, got %s", d.FavorTargets[0].NPC)
	}
}

func TestPlayerPricePreferConsignment(t *testing.T) {
	e := FromNpcs(testNpcs())
	e.SetPlayerPrices(map[string]float64{"Ruby": 200})
	d := e.ResolveItem(cdn.Item{Name: "Ruby", Keywords: []string{"Gem", "Jewelry"}, Value: 100})
	if d.Verdict != VerdictSellConsignment {
		t.Fatalf("expected sell_consignment with player price, got %s", d.Verdict)
	}
	if d.PlayerPrice != 200 {
		t.Fatalf("expected player price 200, got %.0f", d.PlayerPrice)
	}
}

func TestPlayerPriceNoConsignor(t *testing.T) {
	e := FromNpcs(testNpcs())
	e.SetPlayerPrices(map[string]float64{"Rock": 500})
	d := e.ResolveItem(cdn.Item{Name: "Rock", Keywords: []string{"Stone"}, Value: 1})
	if d.Verdict != VerdictSellConsignment {
		t.Fatalf("expected sell_consignment for player-priced item with no consignor, got %s", d.Verdict)
	}
}

func TestResolveWithJustName(t *testing.T) {
	e := FromNpcs(testNpcs())
	d := e.Resolve("Apple", []string{"Food", "Fruit"}, 5)
	if d.Verdict != VerdictFavor {
		t.Fatalf("expected favor, got %s", d.Verdict)
	}
}

func TestSortTargets(t *testing.T) {
	ts := []Target{
		{NPC: "Low", Score: 5},
		{NPC: "High", Score: 15},
		{NPC: "Medium", Score: 10},
	}
	sortTargets(ts)
	if ts[0].NPC != "High" || ts[1].NPC != "Medium" || ts[2].NPC != "Low" {
		t.Fatal("targets not sorted descending by score")
	}
}

func TestNPCRowsAndKeywordKeys(t *testing.T) {
	e := FromNpcs(testNpcs())
	if e.NPCRows() != 4 {
		t.Fatalf("expected 4 NPC rows, got %d", e.NPCRows())
	}
	// Composite keywords excluded from byKeyword
	// 4 keywords: Food, Fruit, Weapon, Armor (EquipmentSlot:Head excluded), Junk
	// 5 simple keywords: Food, Fruit, Weapon, Armor, Junk (EquipmentSlot:Head excluded)
	if e.KeywordKeys() != 5 {
		t.Fatalf("expected 5 keyword keys (composites excluded), got %d", e.KeywordKeys())
	}
}

func TestSplitComposite(t *testing.T) {
	r := splitComposite("EquipmentSlot:Head")
	if len(r) != 2 || r[0] != "EquipmentSlot" || r[1] != "Head" {
		t.Fatal("splitComposite failed")
	}
	r = splitComposite("NoColon")
	if len(r) == 2 {
		t.Fatal("expected short result for string without colon")
	}
}

func TestIsComposite(t *testing.T) {
	if !isComposite("EquipmentSlot:Head") {
		t.Fatal("expected true for composite keyword")
	}
	if isComposite("Food") {
		t.Fatal("expected false for simple keyword")
	}
}
