//go:build sqlite

package storage

import (
	"database/sql"
	"fmt"
	"time"

	// modernc.org/sqlite is a pure-Go SQLite3 implementation — no CGO required.
	_ "modernc.org/sqlite"
)

const createTable = `
CREATE TABLE IF NOT EXISTS packages (
	name        TEXT PRIMARY KEY,
	version     TEXT NOT NULL,
	hash        TEXT NOT NULL,
	built       INTEGER NOT NULL DEFAULT 0,
	built_at    TEXT NOT NULL,
	output_path TEXT
);`

type sqliteStore struct {
	db *sql.DB
}

// NewDB opens or creates a SQLite database at path.
// Uses modernc.org/sqlite (pure Go, no CGO).
func NewDB(path string) (*DB, error) {
	// modernc.org/sqlite registers as driver "sqlite" (not "sqlite3")
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}
	if _, err := db.Exec(createTable); err != nil {
		db.Close()
		return nil, fmt.Errorf("create table: %w", err)
	}
	return &DB{Store: &sqliteStore{db: db}}, nil
}

func (s *sqliteStore) Close() error { return s.db.Close() }

func (s *sqliteStore) IsBuilt(name, hash string) (bool, error) {
	var built int
	var storedHash string
	err := s.db.QueryRow(
		`SELECT built, hash FROM packages WHERE name = ?`, name,
	).Scan(&built, &storedHash)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return built == 1 && storedHash == hash, nil
}

func (s *sqliteStore) MarkBuilt(name, version, hash, outputPath string) error {
	_, err := s.db.Exec(
		`INSERT INTO packages (name, version, hash, built, built_at, output_path)
		 VALUES (?, ?, ?, 1, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		   version=excluded.version, hash=excluded.hash,
		   built=1, built_at=excluded.built_at, output_path=excluded.output_path`,
		name, version, hash, time.Now().UTC().Format(time.RFC3339), outputPath,
	)
	return err
}

func (s *sqliteStore) GetPackage(name string) (*PackageInfo, error) {
	var info PackageInfo
	var builtAt string
	var built int
	err := s.db.QueryRow(
		`SELECT name, version, hash, built, built_at, output_path FROM packages WHERE name = ?`, name,
	).Scan(&info.Name, &info.Version, &info.Hash, &built, &builtAt, &info.OutputPath)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("package %q not found", name)
	}
	if err != nil {
		return nil, err
	}
	info.Built = built == 1
	info.BuiltAt, _ = time.Parse(time.RFC3339, builtAt)
	return &info, nil
}

func (s *sqliteStore) ListPackages() (map[string]*PackageInfo, error) {
	rows, err := s.db.Query(
		`SELECT name, version, hash, built, built_at, output_path FROM packages`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	packages := make(map[string]*PackageInfo)
	for rows.Next() {
		var info PackageInfo
		var builtAt string
		var built int
		if err := rows.Scan(&info.Name, &info.Version, &info.Hash, &built, &builtAt, &info.OutputPath); err != nil {
			return nil, err
		}
		info.Built = built == 1
		info.BuiltAt, _ = time.Parse(time.RFC3339, builtAt)
		packages[info.Name] = &info
	}
	return packages, rows.Err()
}

func (s *sqliteStore) Clear() error {
	_, err := s.db.Exec(`DELETE FROM packages`)
	return err
}

func (s *sqliteStore) Delete(name string) error {
	_, err := s.db.Exec(`DELETE FROM packages WHERE name = ?`, name)
	return err
}
