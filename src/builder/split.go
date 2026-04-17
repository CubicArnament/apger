// Package builder — SplitAnalyzer splits a DESTDIR into lib/bin/dev groups.
package builder

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/NurOS-Linux/apger/src/metadata"
)

// SplitKind identifies which sub-package a file belongs to.
type SplitKind int

const (
	SplitLibs SplitKind = iota // lib<name>  — shared libraries
	SplitBins                  // <name>     — executables
	SplitDev                   // <name>-dev — headers, pkgconfig
)

// SplitResult maps sub-package name → list of absolute file paths.
type SplitResult map[string][]string

// default glob patterns relative to DESTDIR root (forward slashes, no leading /)
var (
	defaultLibsPatterns = []string{"usr/lib/*.so", "usr/lib/*.so.*", "usr/lib64/*.so", "usr/lib64/*.so.*"}
	defaultBinsPatterns = []string{"usr/bin/*", "usr/sbin/*", "bin/*", "sbin/*"}
	defaultDevPatterns  = []string{"usr/include/*", "usr/include/**/*", "usr/lib/pkgconfig/*.pc", "usr/lib64/pkgconfig/*.pc", "usr/share/pkgconfig/*.pc"}
)

// AnalyzeSplit walks destDir and groups files into lib/bin/dev sub-packages.
// pkgName is the base package name (e.g. "curl").
// recipe.Split overrides default patterns per-kind when non-nil.
func AnalyzeSplit(destDir, pkgName string, recipe *metadata.Recipe) SplitResult {
	libName := "lib" + pkgName
	binName := pkgName
	devName := pkgName + "-dev"

	result := SplitResult{
		libName: nil,
		binName: nil,
		devName: nil,
	}

	libsPatterns := defaultLibsPatterns
	binsPatterns := defaultBinsPatterns
	devPatterns := defaultDevPatterns

	if recipe != nil && recipe.Split != nil {
		if len(recipe.Split.Libs) > 0 {
			libsPatterns = recipe.Split.Libs
		}
		if len(recipe.Split.Bins) > 0 {
			binsPatterns = recipe.Split.Bins
		}
		if len(recipe.Split.Dev) > 0 {
			devPatterns = recipe.Split.Dev
		}
	}

	filepath.Walk(destDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(destDir, path)
		if err != nil {
			return nil
		}
		// normalise to forward slashes for pattern matching
		rel = filepath.ToSlash(rel)

		switch {
		case matchAny(rel, libsPatterns):
			result[libName] = append(result[libName], path)
		case matchAny(rel, binsPatterns):
			result[binName] = append(result[binName], path)
		case matchAny(rel, devPatterns):
			result[devName] = append(result[devName], path)
		}
		return nil
	})

	return result
}

// matchAny returns true if path matches any of the glob patterns.
// Supports ** as a recursive wildcard: the prefix before ** must match the
// start of path, and the suffix after **/ (if any) is matched with filepath.Match.
func matchAny(path string, patterns []string) bool {
	for _, pat := range patterns {
		if !strings.Contains(pat, "**") {
			if ok, _ := filepath.Match(pat, path); ok {
				return true
			}
			continue
		}
		// Split on the first occurrence of **
		parts := strings.SplitN(pat, "**", 2)
		prefix := parts[0] // e.g. "usr/include/"
		suffix := strings.TrimPrefix(parts[1], "/") // e.g. "*.h" or "*" or ""
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		if suffix == "" || suffix == "*" {
			return true
		}
		// Match only the filename portion against the suffix pattern
		base := filepath.Base(path)
		if ok, _ := filepath.Match(suffix, base); ok {
			return true
		}
	}
	return false
}
