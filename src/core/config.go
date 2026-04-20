// Package core provides build configuration parsing from apger.conf (TOML).
package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/pelletier/go-toml/v2"
)

// defaultCC maps architecture family to default C compiler.
// "default" in apger.conf resolves to these values.
var defaultCC = map[ArchFamily]string{
	FamilyX86_64:  "gcc",
	FamilyAArch64: "clang",   // clang handles cross-compilation better
	FamilyRISCV64: "clang",
	FamilyOther:   "clang",
}

var defaultCXX = map[ArchFamily]string{
	FamilyX86_64:  "g++",
	FamilyAArch64: "clang++",
	FamilyRISCV64: "clang++",
	FamilyOther:   "clang++",
}

// mustParseMArch parses a march string and panics on error.
// Only used for hardcoded valid values in DefaultConfig.
func mustParseMArch(s string) MArch {
	m, err := ParseMArch(s)
	if err != nil {
		panic(fmt.Sprintf("mustParseMArch(%q): %v", s, err))
	}
	return m
}

// resolveCompiler returns the actual compiler binary for a given "cc" value and target arch.
// "default" → picks gcc/clang based on arch family.
func resolveCompiler(cc string, family ArchFamily, defaults map[ArchFamily]string) string {
	if cc == "" || cc == "default" || cc == "standard" {
		if v, ok := defaults[family]; ok {
			return v
		}
		return "gcc"
	}
	return cc
}

// BuildProfile holds compiler flags for a build target.
// CC/CXX = "default" resolves to gcc/g++ for x86_64, clang/clang++ for cross targets.
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

// ResolvedCC returns the actual CC binary (resolves "default").
func (p BuildProfile) ResolvedCC() string {
	return resolveCompiler(p.CC, p.March.Family(), defaultCC)
}

// ResolvedCXX returns the actual CXX binary (resolves "default").
func (p BuildProfile) ResolvedCXX() string {
	return resolveCompiler(p.CXX, p.March.Family(), defaultCXX)
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

	var flags string
	if march := p.March.String(); march != "" {
		flags += "-march=" + march
	}
	mtune := p.Mtune.String()
	if mtune == "" {
		mtune = p.March.String()
	}
	if mtune != "" {
		if flags != "" {
			flags += " "
		}
		flags += "-mtune=" + mtune
	}
	if p.OptLevel != "" {
		if flags != "" {
			flags += " "
		}
		flags += "-" + p.OptLevel
	}
	if ltoFlag != "" {
		if flags != "" {
			flags += " "
		}
		flags += ltoFlag
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
		"CC":       p.ResolvedCC(),
		"CXX":      p.ResolvedCXX(),
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

// CrossProfile defines a cross-compilation profile for a specific target architecture.
// Used in [build.cross.<arch>] sections of apger.conf.
type CrossProfile struct {
	BuildProfile
	// Target is the GNU target triple, e.g. "aarch64-linux-gnu".
	// Used to set CC/CXX cross-compiler prefix and --target flag.
	Target string `toml:"target"`
}

// CrossEnvVars returns environment variables for cross-compilation.
// Sets CROSS_COMPILE, CC, CXX with the target triple prefix,
// and adds --target=<triple> to CFLAGS/CXXFLAGS for clang.
func (c CrossProfile) CrossEnvVars() map[string]string {
	env := c.BuildProfile.EnvVars()

	if c.Target == "" {
		return env
	}

	cc := c.ResolvedCC()
	cxx := c.ResolvedCXX()

	// For clang: use --target flag (no prefix needed)
	if cc == "clang" || cc == "clang-cross" {
		env["CC"] = "clang"
		env["CXX"] = "clang++"
		env["CFLAGS"] = "--target=" + c.Target + " " + env["CFLAGS"]
		env["CXXFLAGS"] = "--target=" + c.Target + " " + env["CXXFLAGS"]
		env["LDFLAGS"] = "--target=" + c.Target + " " + env["LDFLAGS"]
	} else {
		// For gcc: use <triple>-gcc prefix
		env["CC"] = c.Target + "-" + cc
		env["CXX"] = c.Target + "-" + cxx
		env["CROSS_COMPILE"] = c.Target + "-"
	}

	env["ARCH"] = c.March.String()
	return env
}

// RecipeBuildOverride holds per-recipe compiler/flag overrides.
// Set in [build.override] section of a recipe file.
// Empty string means "use config default".
type RecipeBuildOverride struct {
	CC       string `toml:"cc"        json:"cc,omitempty"`
	CXX      string `toml:"cxx"       json:"cxx,omitempty"`
	LD       string `toml:"ld"        json:"ld,omitempty"`
	OptLevel string `toml:"opt_level" json:"opt_level,omitempty"`
	LTO      string `toml:"lto"       json:"lto,omitempty"`
}

// Resolve returns the effective BuildProfile for a given recipe override and target arch.
// Priority: recipe override > cross profile (if arch != host) > base packages profile.
func (cfg Config) Resolve(override RecipeBuildOverride, targetArch MArch) BuildProfile {
	// Start with base packages profile
	base := cfg.Build.Packages

	// If target arch differs from base arch family, use cross profile
	if targetArch.String() != "" && !targetArch.IsNative() &&
		targetArch.Family() != base.March.Family() {
		if cross, ok := cfg.Build.Cross[targetArch.String()]; ok {
			base = cross.BuildProfile
		} else if cross, ok := cfg.crossForFamily(targetArch.Family()); ok {
			base = cross.BuildProfile
		}
	}

	// Apply recipe overrides (non-empty fields win)
	if override.CC != "" {
		base.CC = override.CC
	}
	if override.CXX != "" {
		base.CXX = override.CXX
	}
	if override.LD != "" {
		base.LD = override.LD
	}
	if override.OptLevel != "" {
		base.OptLevel = override.OptLevel
	}
	if override.LTO != "" {
		base.LTO = override.LTO
	}

	return base
}

// crossForFamily finds the cross profile matching the given arch family.
// Keys are iterated in sorted order to ensure deterministic selection.
func (cfg Config) crossForFamily(family ArchFamily) (CrossProfile, bool) {
	keys := make([]string, 0, len(cfg.Build.Cross))
	for k := range cfg.Build.Cross {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		cp := cfg.Build.Cross[k]
		if cp.March.Family() == family {
			return cp, true
		}
	}
	return CrossProfile{}, false
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

// PackageSortMode controls subdirectory structure for local package output.
type PackageSortMode string

const (
	SortNone   PackageSortMode = "none"  // output-pkgs/pkg.apg
	SortByType PackageSortMode = "type"  // output-pkgs/extra/pkg.apg
	SortByArch PackageSortMode = "arch"  // output-pkgs/x86_64/pkg.apg
	SortByBoth PackageSortMode = "both"  // output-pkgs/x86_64/extra/pkg.apg
)

// SaveOptions holds package save/publish options.
type SaveOptions struct {
	Remote        bool            `toml:"remote"`
	Type          string          `toml:"type"`
	GithubOrgName string          `toml:"github_org_name"`
	Method        string          `toml:"method"`
	Repository    bool            `toml:"repository"`
	// LocalPath is a subdirectory inside the NFS-mounted PVC root.
	LocalPath     string          `toml:"local_path"`
	// SortMode controls subdirectory structure for local packages.
	SortMode      PackageSortMode `toml:"sort_mode"`
}

// LoggingOptions holds logging/output settings.
type LoggingOptions struct {
	Verbose bool `toml:"verbose"`
}

// Aria2Config holds aria2c download settings.
type Aria2Config struct {
	// Connections is --max-connection-per-server (default 4)
	Connections int `toml:"connections"`
	// Splits is --split — number of file segments (default = Connections)
	Splits int `toml:"splits"`
	// MinSplitSize is --min-split-size, e.g. "1M" (default "1M")
	MinSplitSize string `toml:"min_split_size"`
	// MaxTries is --max-tries (default 5)
	MaxTries int `toml:"max_tries"`
	// Timeout is --connect-timeout in seconds (default 60)
	Timeout int `toml:"timeout"`
	// ContinueDownload enables --continue=true for resuming partial downloads
	ContinueDownload bool `toml:"continue"`
	// UserAgent overrides the default aria2c User-Agent header
	UserAgent string `toml:"user_agent"`
	// ProxyURL sets --all-proxy, e.g. "http://proxy:3128"
	ProxyURL string `toml:"proxy"`
}

// CompressionConfig holds archive compression settings for .apg packages.
type CompressionConfig struct {
	// Type is the compression algorithm: zstd | xz | bz2 | gz | lz4 | lzma
	Type string `toml:"type"`
	// Level is the compression level (algorithm-specific).
	Level int `toml:"level"`
}

// Config holds the full apger.conf configuration.
type Config struct {
	Build struct {
		Packages BuildProfile            `toml:"packages"`
		Cross    map[string]CrossProfile `toml:"cross"`
	} `toml:"build"`
	Aria2       Aria2Config       `toml:"aria2"`
	Compression CompressionConfig `toml:"compression"`
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

	march := mustParseMArch("x86_64-v2")
	mtune := mustParseMArch("x86_64-v3")
	v3 := mustParseMArch("x86_64-v3")
	v2 := mustParseMArch("x86_64-v2")

	cfg.Build.Packages.March = march
	cfg.Build.Packages.Mtune = mtune
	cfg.Build.Packages.OptLevel = "O2"
	cfg.Build.Packages.LTO = "thin"
	cfg.Build.Packages.CC = "default"
	cfg.Build.Packages.CXX = "default"
	cfg.Build.Packages.LD = "mold"
	cfg.Build.Packages.LibraryGlibcHwcaps = true
	cfg.Build.Packages.LevelsHwcaps = []MArch{v3, v2}

	// Default cross profiles
	aarch64March := mustParseMArch("armv8-a")  // -march=armv8-a for AArch64 targets
	riscv64March := mustParseMArch("rv64gc")   // -march=rv64gc for RISC-V 64-bit

	cfg.Build.Cross = map[string]CrossProfile{
		"aarch64": {
			BuildProfile: BuildProfile{
				March:    aarch64March,
				OptLevel: "O2",
				LTO:      "thin",
				CC:       "default", // → clang
				CXX:      "default", // → clang++
				LD:       "mold",
			},
			Target: "aarch64-linux-gnu",
		},
		"riscv64": {
			BuildProfile: BuildProfile{
				March:    riscv64March,
				OptLevel: "O2",
				LTO:      "thin",
				CC:       "default", // → clang
				CXX:      "default", // → clang++
				LD:       "mold",
			},
			Target: "riscv64-linux-gnu",
		},
	}

	cfg.Database.Pkgs.Type = "bbolt"
	cfg.Database.Pkgs.Name = "pkgs.db"

	cfg.Compression.Type = "zstd"
	cfg.Compression.Level = 19

	cfg.Aria2.Connections = 4
	cfg.Aria2.Splits = 4
	cfg.Aria2.MinSplitSize = "1M"
	cfg.Aria2.MaxTries = 5
	cfg.Aria2.Timeout = 60
	cfg.Aria2.ContinueDownload = true

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
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// FindConfig looks for apger.conf in standard locations.
func FindConfig(explicitPath string) Config {
	paths := []string{}
	if explicitPath != "" {
		paths = append(paths, explicitPath)
	}
	// Check APGER_CONFIG env var (useful in pods)
	if envPath := os.Getenv("APGER_CONFIG"); envPath != "" {
		paths = append(paths, envPath)
	}
	paths = append(paths,
		"apger.conf",
		"../apger.conf",
		"/etc/apger/apger.conf", // Kubernetes ConfigMap mount
	)
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
