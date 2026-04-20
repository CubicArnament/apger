package metadata

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// LoadRecipe loads a Recipe from a .toml file.
// JSON recipes are no longer supported — convert them to TOML.
func LoadRecipe(path string) (Recipe, error) {
	if strings.ToLower(filepath.Ext(path)) != ".toml" {
		return Recipe{}, fmt.Errorf("unsupported recipe format %q: only .toml is supported", filepath.Ext(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Recipe{}, fmt.Errorf("read recipe %s: %w", path, err)
	}
	var r Recipe
	if err := toml.Unmarshal(data, &r); err != nil {
		return Recipe{}, fmt.Errorf("parse toml recipe %s: %w", path, err)
	}
	return r, nil
}

// FindRecipes scans dir recursively for .toml recipe files.
func FindRecipes(dir string) (map[string][]string, error) {
	groups := make(map[string][]string)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if strings.ToLower(filepath.Ext(path)) != ".toml" {
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
	return toml.Unmarshal([]byte(content), r)
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
# template: meson | cmake | autotools | cargo | python-pep517 | gradle | makefile | kbuild | custom
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
