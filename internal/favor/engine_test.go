package favor

import (
	"testing"

	"github.com/yourname/gorgon-session/internal/cdn"
)

func TestResolveItem_NoKeywords(t *testing.T) {
	npcs := cdn.NpcsFile{
		"test_npc": cdn.Npc{
			InternalName: "test_npc",
			Name:         "Test NPC",
			AreaFriendly: "Test Area",
			Preferences: []cdn.Preference{
				{Name: "Likes Swords", Desire: "Love", Keywords: []string{"Sword"}, Pref: 10},
			},
		},
	}
	engine := FromNpcs(npcs)

	item := cdn.Item{Name: "Unknown Item", Keywords: []string{}, Value: 100}
	dec := engine.ResolveItem(item)

	if dec.Verdict != VerdictSellVendor {
		t.Errorf("expected sell_vendor, got %s", dec.Verdict)
	}
}

func TestResolveItem_CompositeKeywordMatch(t *testing.T) {
	npcs := cdn.NpcsFile{
		"test_npc": cdn.Npc{
			InternalName: "test_npc",
			Name:         "Test NPC",
			AreaFriendly: "Test Area",
			Preferences: []cdn.Preference{
				{Name: "Likes Headgear", Desire: "Love", Keywords: []string{"EquipmentSlot:Head"}, Pref: 15},
			},
		},
	}
	engine := FromNpcs(npcs)

	item := cdn.Item{
		Name:          "Iron Helm",
		Keywords:      []string{"Armor", "Metal"},
		EquipmentSlot: "Head",
		Value:         50,
	}
	dec := engine.ResolveItem(item)

	if dec.Verdict != VerdictFavor {
		t.Errorf("expected favor, got %s", dec.Verdict)
	}
	if len(dec.FavorTargets) == 0 {
		t.Error("expected at least one favor target")
	}
	if dec.FavorTargets[0].NPC != "Test NPC" {
		t.Errorf("expected Test NPC, got %s", dec.FavorTargets[0].NPC)
	}
}

func TestResolveItem_CompositeKeywordNoMatch(t *testing.T) {
	npcs := cdn.NpcsFile{
		"test_npc": cdn.Npc{
			InternalName: "test_npc",
			Name:         "Test NPC",
			AreaFriendly: "Test Area",
			Preferences: []cdn.Preference{
				{Name: "Likes Headgear", Desire: "Love", Keywords: []string{"EquipmentSlot:Head"}, Pref: 15},
			},
		},
	}
	engine := FromNpcs(npcs)

	item := cdn.Item{
		Name:          "Iron Boots",
		Keywords:      []string{"Armor", "Metal"},
		EquipmentSlot: "Feet",
		Value:         50,
	}
	dec := engine.ResolveItem(item)

	if dec.Verdict != VerdictSellVendor {
		t.Errorf("expected sell_vendor, got %s", dec.Verdict)
	}
}

func TestResolveItem_PlayerPriceHigher(t *testing.T) {
	npcs := cdn.NpcsFile{
		"test_npc": cdn.Npc{
			InternalName: "test_npc",
			Name:         "Test NPC",
			AreaFriendly: "Test Area",
			Services: []cdn.Service{
				{Type: "Consignment", ItemTypes: []string{"Runestone"}},
			},
		},
	}
	engine := FromNpcs(npcs)
	engine.SetPlayerPrices(map[string]float64{"Runestone of Fire": 500})

	item := cdn.Item{
		Name:     "Runestone of Fire",
		Keywords: []string{"Runestone", "Magic"},
		Value:    100,
	}
	dec := engine.ResolveItem(item)

	if dec.Verdict != VerdictSellConsignment {
		t.Errorf("expected sell_consignment, got %s", dec.Verdict)
	}
	if dec.PlayerPrice != 500 {
		t.Errorf("expected player price 500, got %f", dec.PlayerPrice)
	}
}

func TestResolveItem_PlayerPriceLower(t *testing.T) {
	npcs := cdn.NpcsFile{
		"test_npc": cdn.Npc{
			InternalName: "test_npc",
			Name:         "Test NPC",
			AreaFriendly: "Test Area",
			Services: []cdn.Service{
				{Type: "Consignment", ItemTypes: []string{"Runestone"}},
			},
		},
	}
	engine := FromNpcs(npcs)
	engine.SetPlayerPrices(map[string]float64{"Common Item": 50})

	item := cdn.Item{
		Name:     "Common Item",
		Keywords: []string{"Junk"},
		Value:    100,
	}
	dec := engine.ResolveItem(item)

	if dec.Verdict != VerdictSellVendor {
		t.Errorf("expected sell_vendor (player price < vendor value), got %s", dec.Verdict)
	}
}

func TestResolveItem_MultipleNPCs_SortedByScore(t *testing.T) {
	npcs := cdn.NpcsFile{
		"npc1": cdn.Npc{
			InternalName: "npc1",
			Name:         "NPC One",
			AreaFriendly: "Area 1",
			Preferences: []cdn.Preference{
				{Name: "Likes Swords", Desire: "Love", Keywords: []string{"Sword"}, Pref: 5},
			},
		},
		"npc2": cdn.Npc{
			InternalName: "npc2",
			Name:         "NPC Two",
			AreaFriendly: "Area 2",
			Preferences: []cdn.Preference{
				{Name: "Loves Swords", Desire: "Love", Keywords: []string{"Sword"}, Pref: 20},
			},
		},
	}
	engine := FromNpcs(npcs)

	item := cdn.Item{
		Name:     "Magic Sword",
		Keywords: []string{"Sword", "Weapon"},
		Value:    100,
	}
	dec := engine.ResolveItem(item)

	if dec.Verdict != VerdictFavor {
		t.Errorf("expected favor, got %s", dec.Verdict)
	}
	if len(dec.FavorTargets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(dec.FavorTargets))
	}
	if dec.FavorTargets[0].NPC != "NPC Two" {
		t.Errorf("expected NPC Two (higher score) first, got %s", dec.FavorTargets[0].NPC)
	}
	if dec.FavorTargets[0].Score != 20 {
		t.Errorf("expected score 20, got %f", dec.FavorTargets[0].Score)
	}
}

func TestResolveItem_HatePreference(t *testing.T) {
	npcs := cdn.NpcsFile{
		"test_npc": cdn.Npc{
			InternalName: "test_npc",
			Name:         "Test NPC",
			AreaFriendly: "Test Area",
			Preferences: []cdn.Preference{
				{Name: "Hates Undead", Desire: "Hate", Keywords: []string{"Undead"}, Pref: -10},
			},
		},
	}
	engine := FromNpcs(npcs)

	item := cdn.Item{
		Name:     "Cursed Bone",
		Keywords: []string{"Undead", "Magic"},
		Value:    50,
	}
	dec := engine.ResolveItem(item)

	if dec.Verdict != VerdictSellVendor {
		t.Errorf("expected sell_vendor (hate = negative score), got %s", dec.Verdict)
	}
}

func TestResolveItem_MixedKeywords(t *testing.T) {
	npcs := cdn.NpcsFile{
		"test_npc": cdn.Npc{
			InternalName: "test_npc",
			Name:         "Test NPC",
			AreaFriendly: "Test Area",
			Preferences: []cdn.Preference{
				{Name: "Likes Metal Headgear", Desire: "Love", Keywords: []string{"Metal", "EquipmentSlot:Head"}, Pref: 25},
			},
		},
	}
	engine := FromNpcs(npcs)

	item := cdn.Item{
		Name:          "Steel Helm",
		Keywords:      []string{"Metal", "Armor"},
		EquipmentSlot: "Head",
		Value:         100,
	}
	dec := engine.ResolveItem(item)

	if dec.Verdict != VerdictFavor {
		t.Errorf("expected favor, got %s", dec.Verdict)
	}
	if len(dec.FavorTargets) == 0 {
		t.Error("expected at least one favor target")
	}
}

func TestResolveItem_EmptyItemName(t *testing.T) {
	npcs := cdn.NpcsFile{}
	engine := FromNpcs(npcs)

	item := cdn.Item{Name: "", Keywords: []string{}, Value: 0}
	dec := engine.ResolveItem(item)

	if dec.Verdict != VerdictSellVendor {
		t.Errorf("expected sell_vendor for empty item, got %s", dec.Verdict)
	}
}
