package pkgbuild

import (
	"fmt"
	"runtime"
	"strings"
)

type Recipe struct {
	Name          string
	Version       string
	Release       string
	Description   string
	URL           string
	Architectures []string
	Licenses      []string
	Dependencies  []string
	Conflicts     []string
	Provides      []string
	Replaces      []string
	Backup        []string
	Sources       []string
	SHA256Sums    []string
	Install       string
	Functions     map[string]bool
}

func (r Recipe) Validate() error {
	if r.Name == "" || r.Version == "" || r.Release == "" {
		return fmt.Errorf("PKGBUILD must define pkgname, pkgver, and pkgrel")
	}
	if len(r.Architectures) == 0 {
		return fmt.Errorf("PKGBUILD must define arch")
	}
	if !r.Functions["package"] {
		return fmt.Errorf("PKGBUILD must define package()")
	}
	if len(r.Sources) != len(r.SHA256Sums) {
		return fmt.Errorf("source and sha256sums must contain the same number of entries")
	}
	return nil
}

func (r Recipe) Architecture() string {
	if len(r.Architectures) == 0 || r.Architectures[0] == "any" {
		return "all"
	}
	arch := r.Architectures[0]
	if arch != "x86_64" && arch != "aarch64" {
		return arch
	}
	return arch
}

func NativeArchitecture() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return runtime.GOARCH
	}
}

func cleanDependency(value string) string {
	for _, operator := range []string{"<=", ">=", "=", "<", ">"} {
		if index := strings.Index(value, operator); index >= 0 {
			return value[:index]
		}
	}
	return value
}

func CleanDependencies(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = cleanDependency(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
