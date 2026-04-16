//go:build bbolt

package storage

import (
	"encoding/json"
	"fmt"
	"time"

	"go.etcd.io/bbolt"
)

const packagesBucket = "packages"

type bboltStore struct {
	db *bbolt.DB
}

// NewDB opens or creates a bbolt database at path.
func NewDB(path string) (*DB, error) {
	db, err := bbolt.Open(path, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bbolt db: %w", err)
	}
	if err := db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(packagesBucket))
		return err
	}); err != nil {
		db.Close()
		return nil, fmt.Errorf("create bucket: %w", err)
	}
	return &DB{Store: &bboltStore{db: db}}, nil
}

func (s *bboltStore) Close() error { return s.db.Close() }

func (s *bboltStore) IsBuilt(name, hash string) (bool, error) {
	var exists bool
	err := s.db.View(func(tx *bbolt.Tx) error {
		v := tx.Bucket([]byte(packagesBucket)).Get([]byte(name))
		if v == nil {
			return nil
		}
		var info PackageInfo
		if err := json.Unmarshal(v, &info); err != nil {
			return err
		}
		exists = info.Built && info.Hash == hash
		return nil
	})
	return exists, err
}

func (s *bboltStore) MarkBuilt(name, version, hash, outputPath string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		info := PackageInfo{
			Name: name, Version: version, Hash: hash,
			Built: true, BuiltAt: time.Now(), OutputPath: outputPath,
		}
		data, err := json.Marshal(info)
		if err != nil {
			return err
		}
		return tx.Bucket([]byte(packagesBucket)).Put([]byte(name), data)
	})
}

func (s *bboltStore) GetPackage(name string) (*PackageInfo, error) {
	var info PackageInfo
	err := s.db.View(func(tx *bbolt.Tx) error {
		v := tx.Bucket([]byte(packagesBucket)).Get([]byte(name))
		if v == nil {
			return fmt.Errorf("package %q not found", name)
		}
		return json.Unmarshal(v, &info)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (s *bboltStore) ListPackages() (map[string]*PackageInfo, error) {
	packages := make(map[string]*PackageInfo)
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte(packagesBucket)).ForEach(func(k, v []byte) error {
			var info PackageInfo
			if err := json.Unmarshal(v, &info); err != nil {
				return fmt.Errorf("unmarshal %s: %w", k, err)
			}
			packages[string(k)] = &info
			return nil
		})
	})
	return packages, err
}

func (s *bboltStore) Clear() error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(packagesBucket))
		return b.ForEach(func(k, _ []byte) error { return b.Delete(k) })
	})
}

func (s *bboltStore) Delete(name string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte(packagesBucket)).Delete([]byte(name))
	})
}
