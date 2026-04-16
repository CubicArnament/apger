package metadata

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// LoadRecipe loads a Recipe from a .toml or .json file.
// The format is determined by the file extension.
func LoadRecipe(path string) (Recipe, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Recipe{}, fmt.Errorf("read recipe %s: %w", path, err)
	}

	var r Recipe
	switch strings.ToLower(filepath.Ext(path)) {
	case ".toml":
		if _, err := toml.Decode(string(data), &r); err != nil {
			return Recipe{}, fmt.Errorf("parse toml recipe %s: %w", path, err)
		}
	case ".json":
		if err := json.Unmarshal(data, &r); err != nil {
			return Recipe{}, fmt.Errorf("parse json recipe %s: %w", path, err)
		}
	default:
		return Recipe{}, fmt.Errorf("unsupported recipe format: %s (use .toml or .json)", filepath.Ext(path))
	}

	return r, nil
}

// FindRecipes scans dir recursively for .toml and .json recipe files.
// Returns paths grouped by subdirectory relative to dir.
func FindRecipes(dir string) (map[string][]string, error) {
	groups := make(map[string][]string)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".toml" && ext != ".json" {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		subdir := filepath.Dir(rel)
		groups[subdir] = append(groups[subdir], path)
		return nil
	})

	return groups, err
}

// DecodeRecipeTOML decodes a TOML string into a Recipe.
// Used by the TUI editor to parse in-memory content without a file.
func DecodeRecipeTOML(content string, r *Recipe) error {
	_, err := toml.Decode(content, r)
	return err
}

// RecipeTemplate returns a TOML template string for a new recipe.
func RecipeTemplate() string {
	return `[package]
name = ""
version = "1.0.0"
type = "binary"          # binary | library | meta
architecture = "x86_64"  # x86_64 | aarch64 | any
description = ""
maintainer = ""
license = ""
homepage = ""
tags = []
dependencies = []
conflicts = []
provides = []
replaces = []
conf = []
bootstrap = false        # true for libc, gcc, binutils — pre-toolchain packages

[source]
url = ""
type_src = "tarball"     # tarball | git-repo
include_submodules = false

[build]
# template: meson | cmake | autotools | cargo | python-pep517 | gradle | custom
# Use "custom" for conan, bazel, scons, waf, qmake, or any other build system.
# With "custom", fill in script and [install].script manually.
template = "meson"
dependencies = []
script = ""   # used only with template = "custom"
use = []

[install]
script = ""   # used only with template = "custom", DESTDIR is set automatically
`
}
