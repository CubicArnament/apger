// Package storage provides persistent storage for built package state.
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
type Store interface {
	IsBuilt(name, hash string) (bool, error)
	MarkBuilt(name, version, hash, outputPath string) error
	GetPackage(name string) (*PackageInfo, error)
	ListPackages() (map[string]*PackageInfo, error)
	Clear() error
	Delete(name string) error
	Close() error
}

// DB is the concrete storage type returned by NewDB.
type DB struct {
	Store
}
