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

	"github.com/yourname/gorgon-session/internal/cdn"
	"github.com/yourname/gorgon-session/internal/config"
	"github.com/yourname/gorgon-session/internal/favor"
	"github.com/yourname/gorgon-session/internal/logtail"
	"github.com/yourname/gorgon-session/internal/loot"
	"github.com/yourname/gorgon-session/internal/server"
	"github.com/yourname/gorgon-session/internal/session"
	"github.com/yourname/gorgon-session/internal/trader"
	"github.com/yourname/gorgon-session/web"
)

func main() {
	var (
		addr     = flag.String("addr", "", "override bind address (e.g. :8080)")
		chatLog  = flag.String("chatlog", "", "override ChatLogs FOLDER path (the game writes *.log files into it)")
		lootRe   = flag.String("loot-regex", "", "override loot-line regex (capture groups: 1=name, 2=count?)")
		version  = flag.String("version", "", "force a specific CDN version (e.g. v480) instead of auto-detect")
		testName = flag.String("test-loot", "", "test the loot regex against a pasted chat-log line, then exit")
	)
	flag.Parse()

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

	// Initialize trader manager
	traderFile := filepath.Join(filepath.Dir(cfg.ReportDir), "traders.json")
	traderMgr := trader.New(traderFile)
	if err := traderMgr.Load(); err != nil {
		log.Printf("warning: failed to load traders: %v", err)
	}

	// Pass the embedded web FS directly; the embed directive in web/embed.go
	// uses bare filenames so paths inside the FS have no "web/" prefix.
	srv := server.New(cfg, mgr, engine, web.Files, t, parser, traderMgr, items, ver)
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

// pipeline converts chat lines into session loot events.
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
		}
	}
}