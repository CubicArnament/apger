// Package config provides build configuration parsing from apger.conf (TOML).
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// BuildProfile holds compiler flags for the [build.packages] profile.
// march, mtune, and levels_hwcaps are MArch objects — any casing/separator is accepted.
type BuildProfile struct {
	March              MArch   `toml:"march"`
	Mtune              MArch   `toml:"mtune"`
	OptLevel           string  `toml:"opt_level"`
	LTO                string  `toml:"lto"`
	CC                 string  `toml:"cc"`
	CXX                string  `toml:"cxx"`
	LD                 string  `toml:"ld"`
	LibraryGlibcHwcaps bool    `toml:"library_glibc_hwcaps"`
	LevelsHwcaps       []MArch `toml:"levels_hwcaps"`
}

// CFlags returns C compiler flags string.
func (p BuildProfile) CFlags() string {
	ltoFlag := ""
	switch p.LTO {
	case "full":
		ltoFlag = "-flto"
	case "thin":
		ltoFlag = "-flto=thin"
	case "none", "":
		ltoFlag = ""
	default:
		ltoFlag = "-flto=" + p.LTO
	}

	mtune := p.Mtune.String()
	if mtune == "" {
		mtune = p.March.String()
	}

	flags := fmt.Sprintf("-march=%s -mtune=%s -%s", p.March, mtune, p.OptLevel)
	if ltoFlag != "" {
		flags += " " + ltoFlag
	}
	return flags
}

// CXXFlags returns C++ compiler flags (same as CFlags).
func (p BuildProfile) CXXFlags() string { return p.CFlags() }

// LDFlags returns linker flags string.
func (p BuildProfile) LDFlags() string {
	if p.LD == "" {
		return ""
	}
	return fmt.Sprintf("-fuse-ld=%s", p.LD)
}

// EnvVars returns environment variables for this build profile.
func (p BuildProfile) EnvVars() map[string]string {
	env := map[string]string{
		"CC":       p.CC,
		"CXX":      p.CXX,
		"CFLAGS":   p.CFlags(),
		"CXXFLAGS": p.CXXFlags(),
		"LDFLAGS":  p.LDFlags(),
	}
	if p.LibraryGlibcHwcaps && len(p.LevelsHwcaps) > 0 {
		levels := ""
		for _, l := range p.LevelsHwcaps {
			if levels != "" {
				levels += ":"
			}
			levels += l.String()
		}
		env["LIBRARY_GLIBC_HWCAPS"] = "true"
		env["LEVELS_HWCAPS"] = levels
	}
	return env
}

// OOMKillLimits holds resource limits for Kubernetes pods.
type OOMKillLimits struct {
	CPU    string `toml:"cpu"`
	Memory string `toml:"memory"`
}

// KubernetesOptions holds K8s cluster settings.
type KubernetesOptions struct {
	Namespace     string        `toml:"namespace"`
	BaseImage     string        `toml:"base_image"`
	SearchLocal   bool          `toml:"search_local"`
	PullRemote    bool          `toml:"pull_remote"`
	KindLoad      bool          `toml:"kind_load"`
	OOMKillLimits OOMKillLimits `toml:"oomkill_limits"`
}

// ImagePullPolicy returns the Kubernetes imagePullPolicy string.
// pull_remote=true → Always; search_local=false → Never; default → IfNotPresent.
func (o KubernetesOptions) ImagePullPolicy() string {
	if o.PullRemote {
		return "Always"
	}
	if !o.SearchLocal {
		return "Never"
	}
	return "IfNotPresent"
}

// PodOptions holds Pod container settings.
type PodOptions struct {
	Stdin bool `toml:"stdin"`
	TTY   bool `toml:"tty"`
}

// DatabaseConfig holds database settings.
type DatabaseConfig struct {
	Type string `toml:"type"`
	Name string `toml:"name"`
}

// SaveOptions holds package save/publish options.
type SaveOptions struct {
	Remote        bool   `toml:"remote"`
	Type          string `toml:"type"`
	GithubOrgName string `toml:"github_org_name"`
	Method        string `toml:"method"`
	Repository    bool   `toml:"repository"`
}

// LoggingOptions holds logging/output settings.
type LoggingOptions struct {
	// Verbose reduces log filtering — more lines shown, still with highlighting.
	// Set verbose = true in [logging] section of apger.conf.
	Verbose bool `toml:"verbose"`
}

// Config holds the full apger.conf configuration.
// [build.self] is intentionally absent — self-build flags live in Meson.build.
type Config struct {
	Build struct {
		Packages BuildProfile `toml:"packages"`
	} `toml:"build"`
	Database struct {
		Pkgs DatabaseConfig `toml:"pkgs"`
	} `toml:"database"`
	Kubernetes struct {
		Options KubernetesOptions `toml:"options"`
	} `toml:"kubernetes"`
	Pod struct {
		Options PodOptions `toml:"options"`
	} `toml:"pod"`
	Save struct {
		Options SaveOptions `toml:"options"`
	} `toml:"save"`
	Logging LoggingOptions `toml:"logging"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	var cfg Config

	march, _ := ParseMArch("x86_64-v2")
	mtune, _ := ParseMArch("x86_64-v3")
	v3, _ := ParseMArch("x86_64-v3")
	v2, _ := ParseMArch("x86_64-v2")

	cfg.Build.Packages.March = march
	cfg.Build.Packages.Mtune = mtune
	cfg.Build.Packages.OptLevel = "O2"
	cfg.Build.Packages.LTO = "thin"
	cfg.Build.Packages.CC = "gcc"
	cfg.Build.Packages.CXX = "g++"
	cfg.Build.Packages.LD = "mold"
	cfg.Build.Packages.LibraryGlibcHwcaps = true
	cfg.Build.Packages.LevelsHwcaps = []MArch{v3, v2}

	cfg.Database.Pkgs.Type = "bbolt"
	cfg.Database.Pkgs.Name = "pkgs.db"

	cfg.Kubernetes.Options.Namespace = "apger"
	cfg.Kubernetes.Options.BaseImage = "fedora:43"
	cfg.Kubernetes.Options.SearchLocal = true
	cfg.Kubernetes.Options.PullRemote = false
	cfg.Kubernetes.Options.KindLoad = false
	cfg.Kubernetes.Options.OOMKillLimits = OOMKillLimits{CPU: "10", Memory: "16Gi"}

	cfg.Pod.Options.Stdin = true
	cfg.Pod.Options.TTY = true

	cfg.Save.Options.Remote = true
	cfg.Save.Options.Type = "forgejo:github"
	cfg.Save.Options.GithubOrgName = "NurOS-Packages"
	cfg.Save.Options.Method = "create_or_update"
	cfg.Save.Options.Repository = true

	return cfg
}

// LoadConfig reads apger.conf from the given path.
// Returns DefaultConfig if the file doesn't exist.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}

	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}

// FindConfig looks for apger.conf in: explicit path → ./apger.conf → ../apger.conf → ~/.config/apger/apger.conf.
func FindConfig(explicitPath string) Config {
	paths := []string{}
	if explicitPath != "" {
		paths = append(paths, explicitPath)
	}
	paths = append(paths, "apger.conf", "../apger.conf")
	if home, _ := os.UserHomeDir(); home != "" {
		paths = append(paths, filepath.Join(home, ".config", "apger", "apger.conf"))
	}
	for _, p := range paths {
		if cfg, err := LoadConfig(p); err == nil {
			return cfg
		}
	}
	return DefaultConfig()
}
