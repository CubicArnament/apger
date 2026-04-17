// Package storage provides persistent storage for built package state.
// The backend is selected at compile time via build tags:
//
//	go build -tags bbolt   → bbolt (default, embedded key-value store)
//	go build -tags sqlite  → SQLite3 via modernc.org/sqlite (pure Go, no CGO)
package storage

import "time"

// PackageInfo stores metadata about a built package.
type PackageInfo struct {
	Name       string    `json:"name"`
	Version    string    `json:"version"`
	Hash       string    `json:"hash"`
	Built      bool      `json:"built"`
	BuiltAt    time.Time `json:"built_at"`
	OutputPath string    `json:"output_path,omitempty"`
}

// Store is the common interface for package state backends.
// Both bbolt and sqlite3 implementations satisfy this interface.
type Store interface {
	// IsBuilt returns true if the package with the given name and recipe hash
	// has already been built successfully.
	IsBuilt(name, hash string) (bool, error)

	// MarkBuilt records a successful build for the given package.
	MarkBuilt(name, version, hash, outputPath string) error

	// GetPackage returns the stored info for a package, or an error if not found.
	GetPackage(name string) (*PackageInfo, error)

	// ListPackages returns all tracked packages.
	ListPackages() (map[string]*PackageInfo, error)

	// Clear removes all package records.
	Clear() error

	// Delete removes a single package record.
	Delete(name string) error

	// Close releases the underlying database resources.
	Close() error
}

// DB is the concrete storage type returned by NewDB.
// It embeds the backend selected at compile time.
type DB struct {
	Store
}
