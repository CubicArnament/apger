// Package settings persists TUI preferences to a JSON file on NFS.
// Stored at $CREDENTIALS_PATH/settings.json (same dir as apger.db).
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// SortMode controls output-pkgs subdirectory structure.
type SortMode string

const (
	SortNone   SortMode = "none"   // output-pkgs/pkg.apg
	SortByType SortMode = "type"   // output-pkgs/extra/pkg.apg
	SortByArch SortMode = "arch"   // output-pkgs/x86_64/pkg.apg
	SortByBoth SortMode = "both"   // output-pkgs/x86_64/extra/pkg.apg
)

// Settings holds all TUI preferences (non-sensitive).
type Settings struct {
	PublishTargets int      `json:"publish_targets"` // bitmask
	LocalPath      string   `json:"local_path"`
	SortMode       SortMode `json:"sort_mode"`
}

func path() string {
	dir := os.Getenv("CREDENTIALS_PATH")
	if dir == "" {
		dir = "/srv/apger-nfs/.credentials"
	}
	return filepath.Join(dir, "settings.json")
}

// Load reads settings from NFS. Returns defaults if file doesn't exist.
func Load() Settings {
	s := Settings{SortMode: SortByBoth}
	data, err := os.ReadFile(path())
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s)
	return s
}

// Save writes settings to NFS.
func Save(s Settings) error {
	p := path()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}
