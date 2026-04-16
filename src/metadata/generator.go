package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// GenerateMetadata creates metadata.json from recipe and build info.
func GenerateMetadata(recipe Recipe, sourceURL string, installedSize int64) PackageMeta {
	return PackageMeta{
		Name:          recipe.Package.Name,
		Version:       recipe.Package.Version,
		Type:          recipe.Package.Type,
		Architecture:  recipe.Package.Architecture,
		Description:   recipe.Package.Description,
		Maintainer:    recipe.Package.Maintainer,
		License:       recipe.Package.License,
		Homepage:      recipe.Package.Homepage,
		SourceURL:     sourceURL,
		InstalledSize: installedSize,
		Dependencies:  recipe.Package.Dependencies,
		Conflicts:     recipe.Package.Conflicts,
		Provides:      recipe.Package.Provides,
		Replaces:      recipe.Package.Replaces,
		Conf:          recipe.Package.Conf,
		Tags:          recipe.Package.Tags,
	}
}

// WriteMetadata writes metadata to a JSON file.
func WriteMetadata(meta PackageMeta, outputPath string) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	return os.WriteFile(outputPath, data, 0644)
}

// GenerateSHA256Checksums generates sha256sums for all files in a directory.
func GenerateSHA256Checksums(dataDir, outputPath string) error {
	sums := make(map[string]string)

	err := filepath.Walk(dataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dataDir, path)
		if err != nil {
			return err
		}
		sum, err := sha256File(path)
		if err != nil {
			return fmt.Errorf("sha256 %s: %w", rel, err)
		}
		sums[rel] = sum
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk: %w", err)
	}

	keys := make([]string, 0, len(sums))
	for k := range sums {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, k := range keys {
		fmt.Fprintf(f, "%s  %s\n", sums[k], k)
	}
	return nil
}

// GenerateCRC32Checksums is a deprecated alias for GenerateSHA256Checksums.
// Deprecated: use GenerateSHA256Checksums.
func GenerateCRC32Checksums(dataDir, outputPath string) error {
	return GenerateSHA256Checksums(dataDir, outputPath)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CalculateSHA256 computes SHA-256 hash of a file.
func CalculateSHA256(path string) (string, error) {
	return sha256File(path)
}

// CalculateDirSize computes total size of files in a directory.
func CalculateDirSize(dir string) (int64, error) {
	var size int64
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// HashRecipe computes a SHA-256 hash of the recipe to detect changes.
func HashRecipe(recipe Recipe) (string, error) {
	data, err := json.Marshal(recipe)
	if err != nil {
		return "", fmt.Errorf("marshal recipe: %w", err)
	}
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil)), nil
}
