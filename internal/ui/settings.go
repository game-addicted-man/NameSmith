package ui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/game-addicted-man/NameSmith/internal/engine"
)

// Settings are remembered between runs in %APPDATA%\NameSmith\settings.json.
type Settings struct {
	Version   int          `json:"version"`
	Rules     engine.Rules `json:"rules"`
	Recursive bool         `json:"recursive"`
	Bounds    []int        `json:"bounds,omitempty"` // x, y, width, height (96 DPI units)
	Maximized bool         `json:"maximized"`
	LastDir   string       `json:"lastDir"`
}

func settingsPath() string { return filepath.Join(configDir(), "settings.json") }

func loadSettings() Settings {
	s := Settings{Version: 1, Rules: engine.DefaultRules()}
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		return s
	}
	var loaded Settings
	if json.Unmarshal(data, &loaded) != nil {
		return s
	}
	// keep sane defaults for anything missing or broken
	if loaded.Rules.Date.Format == "" {
		loaded.Rules.Date.Format = s.Rules.Date.Format
	}
	if loaded.Rules.Number.Step == 0 {
		loaded.Rules.Number.Step = 1
	}
	if loaded.Rules.Number.Digits <= 0 {
		loaded.Rules.Number.Digits = s.Rules.Number.Digits
	}
	loaded.Version = 1
	return loaded
}

func saveSettings(s Settings) error {
	if err := os.MkdirAll(configDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := settingsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	_ = os.Remove(settingsPath())
	return os.Rename(tmp, settingsPath())
}
