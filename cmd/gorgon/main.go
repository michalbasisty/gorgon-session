// Command gorgon runs the gorgon-session dungeon-session app.
//
// It loads config, fetches/caches CDN data (items + npcs), tails Project
// Gorgon's ChatLogs folder (newest *.log file by mtime), parses loot events
// out of `[Status] <item> x<n>? added to inventory.` lines, suggests
// favor/sell routes based on npcs.json Preferences, and serves a local web UI
// on 127.0.0.1:7777 by default.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/michalbasisty/gorgon-session/internal/cdn"
	"github.com/michalbasisty/gorgon-session/internal/config"
	"github.com/michalbasisty/gorgon-session/internal/favor"
	"github.com/michalbasisty/gorgon-session/internal/logtail"
	"github.com/michalbasisty/gorgon-session/internal/loot"
	"github.com/michalbasisty/gorgon-session/internal/overlay"
	"github.com/michalbasisty/gorgon-session/internal/playerlog"
	"github.com/michalbasisty/gorgon-session/internal/server"
	"github.com/michalbasisty/gorgon-session/internal/session"
	"github.com/michalbasisty/gorgon-session/internal/trader"
	"github.com/michalbasisty/gorgon-session/web"
)

func main() {
	var (
		addr     = flag.String("addr", "", "override bind address (e.g. :8080)")
		chatLog  = flag.String("chatlog", "", "override ChatLogs FOLDER path (the game writes *.log files into it)")
		lootRe   = flag.String("loot-regex", "", "override loot-line regex (capture groups: 1=name, 2=count?)")
		version  = flag.String("version", "", "force a specific CDN version (e.g. v480) instead of auto-detect")
		testName = flag.String("test-loot", "", "test the loot regex against a pasted chat-log line, then exit")
		overlayF = flag.Bool("overlay", false, "run the native always-on-top HUD overlay (polls the local API) and exit")
	)
	flag.Parse()

	// The overlay is spawned by the dashboard's /api/overlay/spawn endpoint and
	// polls this app's HTTP API, so it must run BEFORE any config/CDN/HTTP init.
	if *overlayF {
		url := "http://127.0.0.1:7777"
		if a := *addr; a != "" {
			if strings.Contains(a, "://") {
				url = a
			} else {
				if strings.HasPrefix(a, ":") {
					a = "127.0.0.1" + a
				}
				url = "http://" + a
			}
		}
		if err := overlay.Run(url); err != nil {
			log.Printf("overlay: %v", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Printf("config load: %v", err)
	}
	if *addr != "" {
		cfg.HTTPAddr = *addr
	}
	if *chatLog != "" {
		cfg.ChatLogDir = *chatLog
	}
	if *lootRe != "" {
		cfg.LootRegex = *lootRe
	}
	_ = os.MkdirAll(cfg.CacheDir, 0o755)
	_ = os.MkdirAll(cfg.ReportDir, 0o755)

	client := &cdn.Client{
		Root:            cfg.CDNRoot,
		VersionFile:     cfg.VersionFile,
		FallbackVersion: cfg.FallbackVersion,
		CacheDir:        cfg.CacheDir,
	}

	ver := cdn.Version(*version)
	if ver == "" {
		v, err := client.CurrentVersion()
		if err != nil {
			log.Printf("version detect failed, using fallback: %v", err)
			ver = cdn.Version(cfg.FallbackVersion)
		} else {
			ver = v
		}
		log.Printf("CDN version: %s", ver)
	}

	items, err := client.LoadItems(ver)
	if err != nil {
		log.Fatalf("load items.json: %v", err)
	}
	npcs, err := client.LoadNpcs(ver)
	if err != nil {
		log.Fatalf("load npcs.json: %v", err)
	}
	log.Printf("loaded %d items, %d npcs", len(items), len(npcs))

	areas, err := client.LoadAreas(ver)
	if err != nil {
		log.Printf("load areas.json: %v (zone enrichment disabled)", err)
		areas = cdn.AreasFile{}
	}
	areaIdx := cdn.IndexAreas(areas)
	log.Printf("loaded %d areas", len(areas))

	skills, err := client.LoadSkills(ver)
	if err != nil {
		log.Printf("load skills.json: %v (skill enrichment disabled)", err)
		skills = cdn.SkillsFile{}
	}
	log.Printf("loaded %d skills", len(skills))

	recipes, err := client.LoadRecipes(ver)
	if err != nil {
		log.Printf("load recipes.json: %v (recipe browser disabled)", err)
		recipes = cdn.RecipesFile{}
	}
	log.Printf("loaded %d recipes", len(recipes))

	abilities, err := client.LoadAbilities(ver)
	if err != nil {
		log.Printf("load abilities.json: %v (combat stats disabled)", err)
		abilities = cdn.AbilitiesFile{}
	}
	log.Printf("loaded %d abilities", len(abilities))

	nameIdx := indexItemsByName(items)
	engine := favor.FromNpcs(npcs)
	engine.SetPlayerPrices(cfg.PlayerPrices)
	log.Printf("favor engine: %d npcs indexed, %d pref-keywords", engine.NPCRows(), engine.KeywordKeys())

	parser, err := loot.New(cfg.LootRegex)
	if err != nil {
		log.Fatalf("loot parser: %v", err)
	}

	if *testName != "" {
		ev := parser.ParseLine(*testName)
		if ev == nil {
			fmt.Println("no match")
			os.Exit(2)
		}
		match := nameIdx.Lookup(ev.ItemName)
		dec := engine.ResolveItem(match.Item)
		fmt.Printf("matched item:   %q\n", ev.ItemName)
		fmt.Printf("resolved ->     %q  (item_%d, keywords=%v)\n", match.Item.Name, match.Item.ItemID, match.Item.Keywords)
		b, _ := json.MarshalIndent(dec, "", "  ")
		fmt.Println(string(b))
		return
	}

	mgr := session.New()
	defer mgr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Always initialize and start the tailer. It handles empty directories gracefully.
	t := logtail.New(cfg.ChatLogDir)
	_ = t.Start(ctx)
	go pipeline(ctx, t, parser, nameIdx, engine, mgr, client, ver)

	if cfg.ChatLogDir == "" {
		log.Printf("warning: no chat_log_dir configured. set it in the settings dashboard")
	} else {
		log.Printf("tailing newest *.log in %s", cfg.ChatLogDir)
	}

	// Player.log tailer (skill ticks, zone transitions, login detection)
	plParser := playerlog.New()
	plTail := logtail.NewFileTailer(cfg.PlayerLogPath)
	_ = plTail.Start(ctx)

	// Seed the current zone: the tailer starts at EOF and PG only logs zone
	// lines on zone change, so scan Player.log for the LAST zone line. The
	// last change can be far from EOF (player idles in one zone for hours),
	// so read the whole file once — 10-20MB at startup is fine.
	if cfg.PlayerLogPath != "" {
		if f, err := os.Open(cfg.PlayerLogPath); err == nil {
			if data, err := io.ReadAll(f); err == nil {
				zone := ""
				for _, ln := range strings.Split(string(data), "\n") {
					if ev := plParser.ParseLine(ln); ev != nil && ev.Kind == playerlog.KindZone {
						zone = ev.Zone
					}
				}
				if zone != "" {
					mgr.SetZone(zone)
				}
			}
			f.Close()
		}
	}

	go playerPipeline(ctx, plTail, plParser, mgr)

	if cfg.PlayerLogPath != "" {
		log.Printf("watching Player.log: %s", cfg.PlayerLogPath)
	}

	// Initialize trader manager with auto-refresh
	traderFile := filepath.Join(filepath.Dir(cfg.ReportDir), "traders.json")
	traderMgr := trader.New(traderFile)
	if err := traderMgr.Load(); err != nil {
		log.Printf("warning: failed to load traders: %v", err)
	}
	traderMgr.Start(ctx)

	// Auto-populate traders from CDN: any NPC with Store/Consignment that isn't tracked yet.
	for _, n := range npcs {
		hasStore := false
		for _, svc := range n.Services {
			t := strings.ToLower(strings.TrimSpace(svc.Type))
			if t == "store" || t == "consignment" {
				hasStore = true
				break
			}
		}
		if !hasStore {
			continue
		}
		if traderMgr.Get(n.InternalName) == nil {
			_ = traderMgr.Ensure(n.InternalName, n.AreaFriendly, 7, 0)
		}
	}
	if err := traderMgr.Save(); err != nil {
		log.Printf("warning: save auto-populated traders: %v", err)
	}

	// Pass the embedded web FS directly; the embed directive in web/embed.go
	// uses bare filenames so paths inside the FS have no "web/" prefix.
	srv := server.New(cfg, mgr, engine, web.Files, t, plTail, parser, traderMgr, items, ver, npcs, areaIdx, skills, recipes, abilities)
	h := srv.Mount()
	hs := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("listening on http://%s", cfg.HTTPAddr)
	go func() {
		if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	// Auto-backup: every 60 minutes, copy config + reports to backup dir
	if cfg.BackupEnabled {
		backupDir := filepath.Join(filepath.Dir(cfg.ReportDir), "backups")
		_ = os.MkdirAll(backupDir, 0o755)
		go func() {
			ticker := time.NewTicker(60 * time.Minute)
			defer ticker.Stop()
			// Also do one initial backup 30s after startup
			time.Sleep(30 * time.Second)
			backupOnce(cfg, backupDir)
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					backupOnce(cfg, backupDir)
				}
			}
		}()
	}

	// Open browser after a short delay to let server start
	go func() {
		time.Sleep(500 * time.Millisecond)
		url := fmt.Sprintf("http://%s", cfg.HTTPAddr)
		log.Printf("opening browser at %s", url)

		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", url)
		case "darwin":
			cmd = exec.Command("open", url)
		default: // Linux, BSD, etc.
			cmd = exec.Command("xdg-open", url)
		}

		if err := cmd.Start(); err != nil {
			log.Printf("failed to open browser: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Printf("shutting down")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = hs.Shutdown(shutCtx)
	cancel()
}

// backupOnce copies config + session reports to the backup directory.
func backupOnce(cfg config.Config, backupDir string) {
	ts := time.Now().Format("20060102-150405")
	bDir := filepath.Join(backupDir, ts)
	if err := os.MkdirAll(bDir, 0o755); err != nil {
		log.Printf("backup: mkdir %s: %v", bDir, err)
		return
	}

	// Copy config file
	cfgPath, err := config.Path()
	if err == nil {
		copyFile(cfgPath, filepath.Join(bDir, "config.json"))
	}

	// Copy session reports
	entries, err := os.ReadDir(cfg.ReportDir)
	if err == nil {
		reportBackup := filepath.Join(bDir, "reports")
		_ = os.MkdirAll(reportBackup, 0o755)
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
				_ = copyFile(filepath.Join(cfg.ReportDir, e.Name()), filepath.Join(reportBackup, e.Name()))
			}
		}
	}
	log.Printf("backup saved to %s", bDir)

	// Prune old backups (keep last 48)
	pruneOldBackups(backupDir, 48)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func pruneOldBackups(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	// Filter directories with timestamp names (YYYYMMDD-HHMMSS)
	var dirs []os.DirEntry
	for _, e := range entries {
		if e.IsDir() && len(e.Name()) == 15 { // "20260728-120000" = 15 chars
			dirs = append(dirs, e)
		}
	}
	if len(dirs) <= keep {
		return
	}
	// Sort by name (which is timestamp-sorted)
	for i := 0; i < len(dirs)-keep; i++ {
		_ = os.RemoveAll(filepath.Join(dir, dirs[i].Name()))
	}
}

// itemIndex is a case-insensitive name lookup over items.
type itemIndex struct {
	byLower map[string]cdn.Item
}

func indexItemsByName(items cdn.ItemsFile) itemIndex {
	out := itemIndex{byLower: map[string]cdn.Item{}}
	for _, it := range items {
		// record exact-match by lowercase name; first one wins for duplicates
		key := strings.ToLower(strings.TrimSpace(it.Name))
		if key == "" {
			continue
		}
		if _, ok := out.byLower[key]; !ok {
			out.byLower[key] = it
		}
	}
	return out
}

// Lookup returns the best item for a name. If no exact match is found, the
// returned Item has only Name populated so the UI still shows the raw name with
// a sell_vendor verdict (encouraging the user to tune the loot regex).
func (i itemIndex) Lookup(rawName string) struct {
	ItemName string
	Item     cdn.Item
} {
	name := strings.TrimSpace(rawName)
	key := strings.ToLower(name)
	if it, ok := i.byLower[key]; ok {
		return struct {
			ItemName string
			Item     cdn.Item
		}{it.Name, it}
	}
	// unknown item, return a synthetic placeholder so UI still shows raw name.
	return struct {
		ItemName string
		Item     cdn.Item
	}{name, cdn.Item{Name: name}}
}

// pipeline converts chat lines into session events of all types.
func pipeline(ctx context.Context, t *logtail.Tailer, p *loot.Parser, idx itemIndex, eng *favor.Engine, mgr *session.Manager, cdnClient *cdn.Client, cdnVersion cdn.Version) {
	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-t.Lines:
			if !ok {
				return
			}
			ev := p.ParseLine(line)
			if ev == nil {
				continue
			}

			switch ev.Kind {
			case loot.KindLoot:
				hit := idx.Lookup(ev.ItemName)
				dec := eng.ResolveItem(hit.Item)
				mgr.AddLoot(session.LootEntry{
					Name:      hit.Item.Name,
					ItemID:    hit.Item.ItemID,
					IconURL:   cdnClient.IconURL(cdnVersion, hit.Item.IconID),
					Valor:     hit.Item.Value,
					Count:     ev.Count,
					Bonus:     ev.Bonus,
					FirstSeen: time.Now(),
					LastSeen:  time.Now(),
					Decision:  dec,
				})
			case loot.KindXP:
				mgr.AddXPGain(ev.Skill, ev.Amount)
			case loot.KindDeath:
				mgr.AddDeath("")
			case loot.KindKill:
				mgr.AddKill(ev.Killer)
			case loot.KindGather:
				mgr.AddGather(ev.ItemName, ev.Count)
			case loot.KindLevel:
				mgr.AddLevelUp(ev.Skill, ev.Amount)
			case loot.KindGold:
				mgr.AddGold(ev.Amount)
			}
		}
	}
}

// playerPipeline routes Player.log events into the session manager.
func playerPipeline(ctx context.Context, t *logtail.FileTailer, p *playerlog.Parser, mgr *session.Manager) {
	type recentUse struct {
		name     string
		nameNorm string
		id       int
		time     time.Time
	}
	recent := make([]recentUse, 0, 64)

	normAbility := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			return ""
		}
		var b strings.Builder
		b.Grow(len(s))
		for _, r := range s {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			}
		}
		return b.String()
	}

	pruneRecent := func(now time.Time) {
		cut := now.Add(-8 * time.Second)
		j := 0
		for i := 0; i < len(recent); i++ {
			if recent[i].time.After(cut) {
				recent[j] = recent[i]
				j++
			}
		}
		recent = recent[:j]
	}

	matchRecentUse := func(ev *playerlog.Event) (recentUse, bool) {
		now := time.Now()
		pruneRecent(now)
		norm := normAbility(ev.AbilityName)
		for i := len(recent) - 1; i >= 0; i-- {
			ru := recent[i]
			if now.Sub(ru.time) > 4*time.Second {
				continue
			}
			if ev.AbilityID > 0 && ru.id > 0 && ev.AbilityID == ru.id {
				return ru, true
			}
			if norm != "" && ru.nameNorm != "" && norm == ru.nameNorm {
				return ru, true
			}
		}
		return recentUse{}, false
	}

	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-t.Lines:
			if !ok {
				return
			}
			ev := p.ParseLine(line)
			if ev == nil {
				continue
			}
			switch ev.Kind {
			case playerlog.KindZone:
				mgr.SetZone(ev.Zone)
			case playerlog.KindLogin:
				// Login events can be used later for auto-session start
			case playerlog.KindSkill:
				// Skill ticks are granular; we already get "You earned N XP" from ChatLogs
			case playerlog.KindUseAbility:
				mgr.AddAbilityUseWithID(ev.AbilityName, ev.AbilityID)
				ru := recentUse{name: ev.AbilityName, nameNorm: normAbility(ev.AbilityName), id: ev.AbilityID, time: time.Now()}
				recent = append(recent, ru)
				pruneRecent(time.Now())
			case playerlog.KindOnAttackHitMe:
				// Count entity-hit lines only when they match a recent player use.
				if ru, ok := matchRecentUse(ev); ok {
					name := ru.name
					if strings.TrimSpace(name) == "" {
						name = ev.AbilityName
					}
					id := ru.id
					if id <= 0 {
						id = ev.AbilityID
					}
					if ev.Evaded {
						mgr.AddCombatEvadeWithID(name, id)
					} else {
						mgr.AddCombatHitWithID(name, id)
					}
				}
			case playerlog.KindCorpseSearch:
				if ev.Mob != "" {
					mgr.AddKill(ev.Mob)
				}
			}
		}
	}
}
