package worker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NurOS-Linux/apger/internal/pkgbuild"
)

type APGMetadata struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Type         string   `json:"type"`
	Architecture string   `json:"architecture"`
	Description  string   `json:"description"`
	Maintainer   string   `json:"maintainer"`
	License      string   `json:"license"`
	Tags         []string `json:"tags"`
	Homepage     string   `json:"homepage"`
	Dependencies []string `json:"dependencies"`
	Conflicts    []string `json:"conflicts"`
	Provides     []string `json:"provides"`
	Replaces     []string `json:"replaces"`
	Conf         []string `json:"conf"`
}

func writeMetadata(directory string, recipe pkgbuild.Recipe, packager string) error {
	license := "unknown"
	if len(recipe.Licenses) > 0 {
		license = strings.Join(recipe.Licenses, " AND ")
	}
	conf := make([]string, 0, len(recipe.Backup))
	for _, path := range recipe.Backup {
		path = strings.TrimSpace(path)
		if path != "" {
			conf = append(conf, "/"+strings.TrimPrefix(path, "/"))
		}
	}
	metadata := APGMetadata{
		Name: recipe.Name, Version: recipe.Version + "-" + recipe.Release, Type: "binary",
		Architecture: recipe.Architecture(), Description: recipe.Description, Maintainer: packager,
		License: license, Tags: []string{}, Homepage: recipe.URL,
		Dependencies: pkgbuild.CleanDependencies(recipe.Dependencies),
		Conflicts:    append([]string{}, recipe.Conflicts...),
		Provides:     append([]string{}, recipe.Provides...),
		Replaces:     append([]string{}, recipe.Replaces...),
		Conf:         conf,
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("encode APG metadata: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(directory, "metadata.json"), data, 0o644); err != nil {
		return fmt.Errorf("write APG metadata: %w", err)
	}
	return nil
}
