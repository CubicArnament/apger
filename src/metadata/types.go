// Package metadata provides types for APG package recipes and metadata.
package metadata

// PackageMeta represents the metadata.json structure inside APGv2 .apg archives.
// JSON tags are required — metadata.json is part of the APGv2 binary format.
type PackageMeta struct {
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Type          string   `json:"type"`
	Architecture  string   `json:"architecture"`
	Description   string   `json:"description"`
	Maintainer    string   `json:"maintainer"`
	License       string   `json:"license"`
	Homepage      string   `json:"homepage,omitempty"`
	SourceURL     string   `json:"source_url,omitempty"`
	InstalledSize int64    `json:"installed_size,omitempty"`
	SHA256        string   `json:"sha256,omitempty"`
	Dependencies  []string `json:"dependencies,omitempty"`
	Conflicts     []string `json:"conflicts,omitempty"`
	Provides      []string `json:"provides,omitempty"`
	Replaces      []string `json:"replaces,omitempty"`
	Conf          []string `json:"conf,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

// RecipeSource describes where to fetch the package source.
type RecipeSource struct {
	URL               string `toml:"url"`
	TypeSrc           string `toml:"type_src"`            // "tarball" | "git-repo"
	IncludeSubmodules bool   `toml:"include_submodules"`
}

// RecipeSplit defines file glob patterns for splitting a package into sub-packages.
type RecipeSplit struct {
	Libs []string `toml:"libs"` // → lib<name>
	Bins []string `toml:"bins"` // → <name>
	Dev  []string `toml:"dev"`  // → <name>-dev
}

// Recipe represents a package recipe (.toml only).
type Recipe struct {
	Package struct {
		Name         string   `toml:"name"`
		Version      string   `toml:"version"`
		Type         string   `toml:"type"`
		Architecture string   `toml:"architecture"`
		Description  string   `toml:"description"`
		Maintainer   string   `toml:"maintainer"`
		License      string   `toml:"license"`
		Homepage     string   `toml:"homepage"`
		Tags         []string `toml:"tags"`
		Dependencies []string `toml:"dependencies"`
		Conflicts    []string `toml:"conflicts"`
		Provides     []string `toml:"provides"`
		Replaces     []string `toml:"replaces"`
		Conf         []string `toml:"conf"`
		// Bootstrap marks packages built before the toolchain (libc, gcc, binutils).
		Bootstrap bool `toml:"bootstrap"`
		// Krnl marks Linux kernel packages — produces image + modules splits.
		Krnl bool `toml:"krnl"`
	} `toml:"package"`

	Source  RecipeSource `toml:"source"`
	Build   struct {
		Template     string         `toml:"template"`
		Dependencies []string       `toml:"dependencies"`
		Script       string         `toml:"script"`
		Use          []string       `toml:"use"`
		Override     *BuildOverride `toml:"override"`
	} `toml:"build"`
	Install struct {
		Script string `toml:"script"`
	} `toml:"install"`
	Split *RecipeSplit `toml:"split"`
}

// FileEntry describes a file in the recipe structure.
type FileEntry struct {
	Source      string `toml:"source"`
	Destination string `toml:"destination"`
	Permissions string `toml:"permissions"`
}

// SymlinkEntry describes a symlink in the recipe structure.
type SymlinkEntry struct {
	Source      string `toml:"source"`
	Destination string `toml:"destination"`
}

// BuildOverride allows per-recipe compiler/flag overrides from apger.conf.
// Empty fields fall back to the config value.
//
//	[build.override]
//	cc = "clang"
//	opt_level = "O3"
type BuildOverride struct {
	CC       string `toml:"cc"`
	CXX      string `toml:"cxx"`
	LD       string `toml:"ld"`
	OptLevel string `toml:"opt_level"`
	LTO      string `toml:"lto"`
}
