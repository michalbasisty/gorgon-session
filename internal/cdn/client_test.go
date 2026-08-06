package cdn

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestKeyID(t *testing.T) {
	cases := []struct {
		key  string
		want int
	}{
		{"item_1234", 1234},
		{"item_0", 0},
		{"npc_42", 42},
		{"item_12x", 0},     // non-numeric suffix
		{"item_", 0},        // empty suffix
		{"nounderscore", 0}, // no underscore at all
		{"", 0},
	}
	for _, c := range cases {
		if got := keyID(c.key); got != c.want {
			t.Errorf("keyID(%q) = %d, want %d", c.key, got, c.want)
		}
	}
}

func TestIconURL(t *testing.T) {
	cases := []struct {
		name   string
		root   string
		v      Version
		iconID int
		want   string
	}{
		{"basic", "http://cdn.projectgorgon.com", "v480", 5, "http://cdn.projectgorgon.com/v480/icons/5.png"},
		{"trailing slash root", "http://cdn.projectgorgon.com/", "v480", 5, "http://cdn.projectgorgon.com/v480/icons/5.png"},
		{"version without v", "http://cdn.projectgorgon.com", "480", 5, "http://cdn.projectgorgon.com/480/icons/5.png"},
		{"zero id is empty", "http://cdn.projectgorgon.com", "v480", 0, ""},
		{"negative id is empty", "http://cdn.projectgorgon.com", "v480", -1, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cl := &Client{Root: c.root}
			if got := cl.IconURL(c.v, c.iconID); got != c.want {
				t.Errorf("IconURL(%q, %d) = %q, want %q", c.v, c.iconID, got, c.want)
			}
		})
	}
}

func TestCurrentVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/480.txt":
			w.Write([]byte("480\n"))
		case "/481.txt":
			w.Write([]byte("  v481  "))
		case "/empty.txt":
			w.Write(nil)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if v, err := (&Client{VersionFile: srv.URL + "/480.txt"}).CurrentVersion(); err != nil || v != "v480" {
		t.Errorf("CurrentVersion('480') = %q, %v; want v480, nil", v, err)
	}
	if v, err := (&Client{VersionFile: srv.URL + "/481.txt"}).CurrentVersion(); err != nil || v != "v481" {
		t.Errorf("CurrentVersion('  v481  ') = %q, %v; want v481, nil", v, err)
	}

	// Empty body: fallback + error.
	fb := Version("v480")
	if v, err := (&Client{VersionFile: srv.URL + "/empty.txt", FallbackVersion: "v480"}).CurrentVersion(); v != fb || err == nil {
		t.Errorf("CurrentVersion(empty) = %q, %v; want fallback %q + error", v, err, fb)
	}

	// Unreachable server: fallback + error.
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	if v, err := (&Client{VersionFile: dead.URL + "/x.txt", FallbackVersion: "v480"}).CurrentVersion(); v != fb || err == nil {
		t.Errorf("CurrentVersion(dead) = %q, %v; want fallback %q + error", v, err, fb)
	}
}

func TestFetchAndLoadersWithCache(t *testing.T) {
	const itemsJSON = `{"item_1234":{"InternalName":"iron_ore","Name":"Iron Ore","IconId":7,"Value":3.5,"MaxStackSize":20,"Keywords":["Ore","Metal"]}}`
	const npcsJSON = `{"npc_blacksmith":{"Name":"Smithy Sue","AreaName":"Serbule","AreaFriendlyName":"Serbule","Preferences":[{"Name":"Loves ore","Desire":"Love","Keywords":["Ore"],"Pref":10}]}}`
	const areasJSON = `{"serbule_hills":{"FriendlyName":"Serbule Hills","ShortFriendlyName":"Serbule","X":10.5,"Y":20.25}}`
	const skillsJSON = `{"Sword":{"Combat":true,"Description":"Swords","MaxBonusLevels":50,"GuestLevelCap":40,"XpTable":"default"}}`
	const recipesJSON = `{"recipe_1":{"InternalName":"recipe_1","Name":"Iron Ingot","Skill":"Metallurgy","SkillLevelReq":10,"Ingredients":[{"ItemCode":100,"StackSize":2}],"ResultItems":[{"ItemCode":101,"StackSize":1}]}}`

	var hits int32
	files := map[string]string{
		"/v480/data/items.json":   itemsJSON,
		"/v480/data/npcs.json":    npcsJSON,
		"/v480/data/areas.json":   areasJSON,
		"/v480/data/skills.json":  skillsJSON,
		"/v480/data/recipes.json": recipesJSON,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body, ok := files[r.URL.Path]; ok {
			atomic.AddInt32(&hits, 1)
			w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))

	c := &Client{Root: srv.URL, CacheDir: t.TempDir()}
	v := Version("v480")

	items, err := c.LoadItems(v)
	if err != nil {
		t.Fatalf("LoadItems: %v", err)
	}
	it, ok := items["item_1234"]
	if !ok || it.ItemID != 1234 || it.Name != "Iron Ore" || it.Value != 3.5 || it.IconID != 7 {
		t.Fatalf("LoadItems parsed wrong: %+v (ok=%v)", it, ok)
	}

	npcs, err := c.LoadNpcs(v)
	if err != nil {
		t.Fatalf("LoadNpcs: %v", err)
	}
	n, ok := npcs["npc_blacksmith"]
	if !ok || n.InternalName != "npc_blacksmith" || n.Name != "Smithy Sue" || len(n.Preferences) != 1 {
		t.Fatalf("LoadNpcs parsed wrong: %+v (ok=%v)", n, ok)
	}

	areas, err := c.LoadAreas(v)
	if err != nil {
		t.Fatalf("LoadAreas: %v", err)
	}
	a, ok := areas["serbule_hills"]
	if !ok || a.FriendlyName != "Serbule Hills" || a.X == nil || *a.X != 10.5 || a.Y == nil || *a.Y != 20.25 {
		t.Fatalf("LoadAreas parsed wrong: %+v (ok=%v)", a, ok)
	}
	idx := IndexAreas(areas)
	if idx.ByInternal["serbule_hills"].FriendlyName != "Serbule Hills" {
		t.Errorf("IndexAreas ByInternal wrong")
	}
	if key, ok := idx.ByFriendly["serbule hills"]; !ok || key != "serbule_hills" {
		t.Errorf("IndexAreas ByFriendly = %q, %v; want serbule_hills", key, ok)
	}

	skills, err := c.LoadSkills(v)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	sk, ok := skills["Sword"]
	if !ok || !sk.Combat || sk.MaxBonusLevels != 50 || sk.XpTable != "default" {
		t.Fatalf("LoadSkills parsed wrong: %+v (ok=%v)", sk, ok)
	}

	recipes, err := c.LoadRecipes(v)
	if err != nil {
		t.Fatalf("LoadRecipes: %v", err)
	}
	r1, ok := recipes["recipe_1"]
	if !ok || r1.Name != "Iron Ingot" || r1.SkillLevelReq != 10 || len(r1.Ingredients) != 1 || r1.Ingredients[0].ItemCode != 100 {
		t.Fatalf("LoadRecipes parsed wrong: %+v (ok=%v)", r1, ok)
	}

	// Cache path: reloading must not hit the server.
	before := atomic.LoadInt32(&hits)
	if _, err := c.LoadItems(v); err != nil {
		t.Fatalf("reload LoadItems: %v", err)
	}
	if _, err := c.LoadNpcs(v); err != nil {
		t.Fatalf("reload LoadNpcs: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != before {
		t.Fatalf("cache miss: %d new server requests on reload", got-before)
	}

	// Cache survives server shutdown.
	srv.Close()
	items2, err := c.LoadItems(v)
	if err != nil || items2["item_1234"].Name != "Iron Ore" {
		t.Fatalf("expected cache to serve after server shutdown, got err=%v", err)
	}

	// No CacheDir: always downloads, so a dead server means an error.
	nc := &Client{Root: srv.URL}
	if _, err := nc.LoadItems(v); err == nil {
		t.Fatal("expected error downloading with no cache and dead server")
	}
}
