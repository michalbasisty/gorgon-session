package config

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// TestOverlayDefaultsAfterEmptyLoad verifies the exact path Load() uses for an
// empty config file: start from Default(), unmarshal the (empty) file over it,
// and confirm the overlay defaults survive.
func TestOverlayDefaultsAfterEmptyLoad(t *testing.T) {
	c := Default()
	if err := json.Unmarshal([]byte("{}"), &c); err != nil {
		t.Fatalf("unmarshal empty config: %v", err)
	}
	ov := c.Overlay
	if ov.Opacity != 98 {
		t.Errorf("expected opacity 98, got %d", ov.Opacity)
	}
	if ov.ClickThroughOpacity != 78 {
		t.Errorf("expected click_through_opacity 78, got %d", ov.ClickThroughOpacity)
	}
	if ov.ClickThroughByDefault {
		t.Error("expected click_through_by_default false")
	}
	if ov.Position != "bottom-right" {
		t.Errorf("expected position bottom-right, got %q", ov.Position)
	}
	if ov.Theme != "dark" {
		t.Errorf("expected theme dark, got %q", ov.Theme)
	}
	if ov.AccentColor != "#5b93ff" {
		t.Errorf("expected accent_color #5b93ff, got %q", ov.AccentColor)
	}
}

func TestNormalizeDefaults_HomeMode(t *testing.T) {
	homeDir := filepath.Dir(Default().CacheDir)
	c := normalizeDefaults(Config{}, filepath.Join(homeDir, "config.json"))
	if c.CacheDir != filepath.Join(homeDir, "cache") {
		t.Errorf("expected cache in home dir, got %q", c.CacheDir)
	}
	if c.ReportDir != filepath.Join(homeDir, "reports") {
		t.Errorf("expected reports in home dir, got %q", c.ReportDir)
	}
}

func TestNormalizeDefaults_PortableModeRelocatesDirs(t *testing.T) {
	portable := filepath.Join(t.TempDir(), "config.json")
	homeDir := filepath.Dir(Default().CacheDir)

	// A config carrying the home-dir defaults (or empty fields) must move to
	// portable locations when the config file lives next to the exe.
	c := Default()
	c = normalizeDefaults(c, portable)
	if c.CacheDir != filepath.Join(filepath.Dir(portable), "cache") {
		t.Errorf("expected portable cache, got %q", c.CacheDir)
	}
	if c.ReportDir != filepath.Join(filepath.Dir(portable), "reports") {
		t.Errorf("expected portable reports, got %q", c.ReportDir)
	}

	// Explicitly configured custom dirs must survive.
	c2 := Default()
	c2.CacheDir = filepath.Join(homeDir, "custom-cache")
	c2.ReportDir = filepath.Join(homeDir, "custom-reports")
	c2 = normalizeDefaults(c2, portable)
	if c2.CacheDir != filepath.Join(homeDir, "custom-cache") || c2.ReportDir != filepath.Join(homeDir, "custom-reports") {
		t.Errorf("custom dirs clobbered: %q %q", c2.CacheDir, c2.ReportDir)
	}
}

func TestNormalizeDefaults_EmptyPlayerLogAndHTTPAddr(t *testing.T) {
	homeDir := filepath.Dir(Default().CacheDir)
	c := normalizeDefaults(Config{}, filepath.Join(homeDir, "config.json"))
	if c.PlayerLogPath == "" {
		t.Error("expected player_log_path re-defaulted")
	}
	if c.HTTPAddr != "127.0.0.1:7777" {
		t.Errorf("expected http_addr re-defaulted to 127.0.0.1:7777, got %q", c.HTTPAddr)
	}
	if c.PlayerPrices == nil {
		t.Error("expected player_prices non-nil")
	}
}
