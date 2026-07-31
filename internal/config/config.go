// Package config loads/saves gorgon-session settings.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

// Config is the on-disk configuration for gorgon-session. It is intentionally
// small for the dungeon-session MVP; future phases (crafting, combat,
// inventory) will add fields without breaking this struct.
type Config struct {
	// HTTPAddr is the address the embedded web server binds to.
	HTTPAddr string `json:"http_addr"`
	// ChatLogDir is the absolute path to the Project Gorgon ChatLogs FOLDER
	// the game client writes *.log files into while the chat log is enabled
	// in-game (Game Settings -> GUI -> Save Chat Logs). The folder watcher
	// picks the newest *.log by mtime every poll and tails new bytes from it.
	ChatLogDir string `json:"chat_log_dir"`
	// LootRegex is the regex used to extract an item-name and optional count
	// from a chat-log line. Capture groups 1=item name, 2=count (optional).
	// If empty, parser.DefaultRegex is used. Change only if your locale /
	// client emits loot events with different prefix tags.
	LootRegex string `json:"loot_regex"`
	// ServerURL is the base CDN root (with no version) used to discover the
	// current version. Almost always leave default.
	CDNRoot string `json:"cdn_root"`
	// VersionFile is the URL that returns the current client version integer.
	VersionFile string `json:"version_file"`
	// FallbackVersion is used if VersionFile is unreachable.
	FallbackVersion string `json:"fallback_version"`
	// CacheDir is where CDN JSON is cached. Empty = next to config file.
	CacheDir string `json:"cache_dir"`
	// ReportDir is where session report JSON files are written when a
	// session is stopped. Empty = next to config file.
	ReportDir string `json:"report_dir"`
	// SellValueThreshold: items with Value >= this are suggested vendor-sell
	// rather than donate-if-no-loves-them (helps avoid wasting coin value).
	SellValueThreshold float64 `json:"sell_value_threshold"`
	// PlayerPrices: custom prices for items that sell for more to other
	// players than the vendor value (e.g. runestones). Key = item name,
	// value = player market price in gold.
	PlayerPrices map[string]float64 `json:"player_prices"`
	// NotificationThreshold: items with Value >= this trigger browser
	// notifications when looted (if notifications are enabled in the UI).
	NotificationThreshold float64 `json:"notification_threshold"`
	// BackupEnabled controls automatic session report backups.
	BackupEnabled bool `json:"backup_enabled"`
	// PlayerLogPath is the path to Player.log for skill ticks, zone
	// transitions, and login detection. Empty = auto-detect.
	PlayerLogPath string `json:"player_log_path"`
	// Overlay configures the native always-on-top HUD window (position,
	// opacity, click-through, theme). Applied by the overlay process at startup.
	Overlay OverlaySettings `json:"overlay"`
}

// OverlaySettings configures the native always-on-top HUD window. Values are
// read by the overlay process from /api/config when it starts.
type OverlaySettings struct {
	// Opacity is normal-mode window opacity, percent 30..100 (default 98).
	Opacity int `json:"opacity"`
	// ClickThroughOpacity is click-through-mode window opacity, percent
	// 30..100 (default 78).
	ClickThroughOpacity int `json:"click_through_opacity"`
	// ClickThroughByDefault starts the overlay in click-through mode
	// (default false).
	ClickThroughByDefault bool `json:"click_through_by_default"`
	// Position is the screen corner the window docks to:
	// "bottom-right"|"bottom-left"|"top-right"|"top-left" (default "bottom-right").
	Position string `json:"position"`
	// Theme is "dark"|"light" for the overlay web UI (default "dark").
	Theme string `json:"theme"`
	// AccentColor is a hex color like "#5b93ff" for the overlay web UI
	// (defaults to the app's CSS --accent so the overlay matches out of the box).
	// (default matches the app accent).
	AccentColor string `json:"accent_color"`
}

// Default returns the platform-appropriate default config.
func Default() Config {
	home, _ := os.UserHomeDir()
	appDir := filepath.Join(home, ".gorgon-session")
	return Config{
		HTTPAddr:              "127.0.0.1:7777",
		ChatLogDir:            defaultChatLogDir(),
		LootRegex:             "",
		CDNRoot:               "http://cdn.projectgorgon.com",
		VersionFile:           "http://client.projectgorgon.com/fileversion.txt",
		FallbackVersion:      "v481",
		CacheDir:              filepath.Join(appDir, "cache"),
		ReportDir:             filepath.Join(appDir, "reports"),
		SellValueThreshold:    50,
		PlayerPrices:          map[string]float64{},
		NotificationThreshold: 500,
		BackupEnabled:         true,
		PlayerLogPath:         defaultPlayerLogPath(),
		Overlay: OverlaySettings{
			Opacity:             98,
			ClickThroughOpacity: 78,
			Position:            "bottom-right",
			Theme:               "dark",
			AccentColor:         "#5b93ff",
		},
	}
}

// Path returns the default config file path.
// It checks next to the executable first (portable mode), then falls back to ~/.gorgon-session/config.json.
func Path() (string, error) {
	// 1. Check next to the executable first (portable mode)
	if execPath, err := os.Executable(); err == nil {
		localConfig := filepath.Join(filepath.Dir(execPath), "config.json")
		if _, err := os.Stat(localConfig); err == nil {
			return localConfig, nil
		}
	}

	// 2. Fallback to user home directory
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".gorgon-session")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads config from disk, writing defaults first if absent.
func Load() (Config, error) {
	p, err := Path()
	if err != nil {
		return Default(), err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		c := Default()
		_ = Save(c)
		return c, nil
	} else if err != nil {
		return Default(), err
	}
	c := Default()
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	return normalizeDefaults(c, p), nil
}

// normalizeDefaults fills in fields a saved config may have left empty:
//   - portable mode (config next to the exe) keeps cache/reports next to it
//   - an empty player_log_path re-enables auto-detection
//   - an empty http_addr would make ListenAndServe bind :80, so re-default it
func normalizeDefaults(c Config, p string) Config {
	homeDir := filepath.Dir(Default().CacheDir)
	if filepath.Dir(p) != homeDir {
		// portable mode: relocate data dirs next to the config file
		if c.CacheDir == "" || c.CacheDir == filepath.Join(homeDir, "cache") {
			c.CacheDir = filepath.Join(filepath.Dir(p), "cache")
		}
		if c.ReportDir == "" || c.ReportDir == filepath.Join(homeDir, "reports") {
			c.ReportDir = filepath.Join(filepath.Dir(p), "reports")
		}
	} else {
		if c.CacheDir == "" {
			c.CacheDir = filepath.Join(filepath.Dir(p), "cache")
		}
		if c.ReportDir == "" {
			c.ReportDir = filepath.Join(filepath.Dir(p), "reports")
		}
	}
	if c.PlayerLogPath == "" {
		c.PlayerLogPath = defaultPlayerLogPath()
	}
	if c.ChatLogDir == "" {
		c.ChatLogDir = defaultChatLogDir()
	}
	if c.HTTPAddr == "" {
		c.HTTPAddr = "127.0.0.1:7777"
	}
	if c.PlayerPrices == nil {
		c.PlayerPrices = map[string]float64{}
	}
	return c
}

// Save writes config to disk.
func Save(c Config) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// defaultChatLogDir mirrors GorgonSurveyTracker's directory discovery:
// Project Gorgon writes *.log files to AppData/LocalLow/Elder Game/Project Gorgon/ChatLogs/
// (note: "LocalLow" is a peer of "Local" and "Roaming" under AppData, not a
// sub-folder of Local). The game creates this folder once the player enables
// "Save Chat Logs" in Game Settings -> GUI -> Chat Logs section.
func defaultChatLogDir() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		la := os.Getenv("LOCALAPPDATA")
		if la == "" {
			la = filepath.Join(home, "AppData", "Local")
		}
		// %LOCALAPPDATA% = C:\Users\<u>\AppData\Local ; one level up is C:\Users\<u>\AppData
		appData := filepath.Dir(la)
		return filepath.Join(appData, "LocalLow", "Elder Game", "Project Gorgon", "ChatLogs")
	}
	return filepath.Join(home, ".local", "share", "Elder Game", "Project Gorgon", "ChatLogs")
}

// defaultPlayerLogPath returns the default Player.log path.
// On Windows: %USERPROFILE%\AppData\LocalLow\Elder Game\Project Gorgon\Player.log
func defaultPlayerLogPath() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		la := os.Getenv("LOCALAPPDATA")
		if la == "" {
			la = filepath.Join(home, "AppData", "Local")
		}
		appData := filepath.Dir(la)
		return filepath.Join(appData, "LocalLow", "Elder Game", "Project Gorgon", "Player.log")
	}
	return filepath.Join(home, ".local", "share", "Elder Game", "Project Gorgon", "Player.log")
}