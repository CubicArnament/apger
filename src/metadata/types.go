// Package metadata provides types for APG package recipes and metadata.
package metadata

// PackageMeta represents the metadata.json structure for APGv2 packages.
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
	// URL is the download URL (tarball) or git remote URL.
	URL string `toml:"url" json:"url"`
	// TypeSrc is "tarball" or "git-repo".
	TypeSrc string `toml:"type_src" json:"type_src"`
	// IncludeSubmodules clones git submodules recursively (git-repo only).
	IncludeSubmodules bool `toml:"include_submodules" json:"include_submodules,omitempty"`
}

// RecipeSplit defines file glob patterns for splitting a package into sub-packages.
type RecipeSplit struct {
	Libs []string `toml:"libs" json:"libs,omitempty"` // → lib<name>
	Bins []string `toml:"bins" json:"bins,omitempty"` // → <name>
	Dev  []string `toml:"dev"  json:"dev,omitempty"`  // → <name>-dev
}

// Recipe represents a package recipe (.toml or .json).
type Recipe struct {
	Package struct {
		Name         string   `toml:"name"         json:"name"`
		Version      string   `toml:"version"      json:"version"`
		Type         string   `toml:"type"         json:"type"`
		Architecture string   `toml:"architecture" json:"architecture"`
		Description  string   `toml:"description"  json:"description"`
		Maintainer   string   `toml:"maintainer"   json:"maintainer"`
		License      string   `toml:"license"      json:"license"`
		Homepage     string   `toml:"homepage"     json:"homepage,omitempty"`
		Tags         []string `toml:"tags"         json:"tags,omitempty"`
		Dependencies []string `toml:"dependencies" json:"dependencies,omitempty"`
		Conflicts    []string `toml:"conflicts"    json:"conflicts,omitempty"`
		Provides     []string `toml:"provides"     json:"provides,omitempty"`
		Replaces     []string `toml:"replaces"     json:"replaces,omitempty"`
		Conf         []string `toml:"conf"         json:"conf,omitempty"`
		// Bootstrap marks packages that must be built before the toolchain is
		// available (libc, gcc, binutils). Bootstrap builds skip dependency
		// checks and use a pre-stage cross-compiler environment.
		Bootstrap bool `toml:"bootstrap" json:"bootstrap,omitempty"`
	} `toml:"package" json:"package"`

	// Source describes where to fetch the package source.
	Source RecipeSource `toml:"source" json:"source,omitempty"`

	Build struct {
		Template     string   `toml:"template"     json:"template,omitempty"`
		Dependencies []string `toml:"dependencies" json:"dependencies,omitempty"`
		Script       string   `toml:"script"       json:"script,omitempty"`
		Use          []string `toml:"use"          json:"use,omitempty"`
		// Override allows per-recipe compiler/flag overrides.
		// Empty fields fall back to apger.conf values.
		// Example in TOML:
		//   [build.override]
		//   cc = "clang"
		//   opt_level = "O3"
		Override *BuildOverride `toml:"override" json:"override,omitempty"`
	} `toml:"build" json:"build,omitempty"`

	Install struct {
		Script string `toml:"script" json:"script,omitempty"`
	} `toml:"install" json:"install,omitempty"`

	Split *RecipeSplit `toml:"split" json:"split,omitempty"`
}

// FileEntry describes a file in the recipe structure.
type FileEntry struct {
	Source      string `toml:"source"      json:"source"`
	Destination string `toml:"destination" json:"destination"`
	Permissions string `toml:"permissions" json:"permissions,omitempty"`
}

// SymlinkEntry describes a symlink in the recipe structure.
type SymlinkEntry struct {
	Source      string `toml:"source"      json:"source"`
	Destination string `toml:"destination" json:"destination"`
}

// BuildOverride allows a recipe to override compiler/flag settings from apger.conf.
// Only non-empty fields take effect; empty fields fall back to the config value.
//
// TOML example:
//
//	[build.override]
//	cc        = "clang"
//	cxx       = "clang++"
//	opt_level = "O3"
//	lto       = "full"
//	ld        = "lld"
type BuildOverride struct {
	CC       string `toml:"cc"        json:"cc,omitempty"`
	CXX      string `toml:"cxx"       json:"cxx,omitempty"`
	LD       string `toml:"ld"        json:"ld,omitempty"`
	OptLevel string `toml:"opt_level" json:"opt_level,omitempty"`
	LTO      string `toml:"lto"       json:"lto,omitempty"`
}
