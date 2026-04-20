// Package credentials manages apger credentials.
//
// Storage: /srv/apger-nfs/.credentials/apger.db (encrypted bbolt database on NFS)
// Encryption: AES-256-GCM with key derived from machine ID + username
//
// Multi-node support: All nodes (master + workers) can access credentials via NFS
// and sign packages. Credentials are encrypted and shared across the cluster.
//
// Authentication priority per user:
//   1. GitHub App (AppID + PEM) → JWT → installation token
//   2. PAT
//   3. gh CLI  (`gh auth token`)  — fallback if neither is set
package credentials

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"go.etcd.io/bbolt"
)

// Credentials holds all credentials for one user / identity.
type Credentials struct {
	Name  string `json:"name"`
	Email string `json:"email"`

	// GitHub App (preferred)
	GitHubAppID int64  `json:"github_app_id,omitempty"`
	GitHubPEM   string `json:"github_pem,omitempty"`

	// Personal Access Token (fallback)
	PAT string `json:"pat,omitempty"`

	// PGP signing key (armored OpenPGP private key)
	PGPPrivateKey string `json:"pgp_private_key,omitempty"`
}

// AuthToken returns a GitHub access token using the best available method:
//  1. GitHub App JWT → installation token
//  2. PAT
//  3. gh CLI  (`gh auth token --hostname github.com`)
func (c Credentials) AuthToken(ctx context.Context, org string) (string, error) {
	// 1. GitHub App
	if c.GitHubAppID != 0 && c.GitHubPEM != "" {
		tok, err := InstallationToken(ctx, c.GitHubAppID, c.GitHubPEM, org)
		if err == nil {
			return tok, nil
		}
		// fall through to PAT / gh cli
	}

	// 2. PAT
	if c.PAT != "" {
		return c.PAT, nil
	}

	// 3. gh CLI
	if tok, err := ghCLIToken(); err == nil && tok != "" {
		return tok, nil
	}

	return "", fmt.Errorf("no GitHub credentials configured (set github_app_id+github_pem, pat, or run 'gh auth login')")
}

// ghCLIToken calls `gh auth token` and returns the token string.
func ghCLIToken() (string, error) {
	out, err := exec.Command("gh", "auth", "token", "--hostname", "github.com").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ── storage schema ────────────────────────────────────────────────────────────

const bucketName = "credentials"

// Manager handles loading and saving credentials in encrypted bbolt database.
type Manager struct {
	path string
	key  []byte // AES-256 key
}

// New creates a Manager using /srv/apger-nfs/.credentials/apger.db (NFS shared storage).
func New() (*Manager, error) {
	key, err := deriveKey()
	if err != nil {
		return nil, fmt.Errorf("derive encryption key: %w", err)
	}
	credsPath := os.Getenv("CREDENTIALS_PATH")
	if credsPath == "" {
		credsPath = "/srv/apger-nfs/.credentials"
	}
	return &Manager{
		path: filepath.Join(credsPath, "apger.db"),
		key:  key,
	}, nil
}

// NewFromEnv creates a Manager that reads from APGER_CREDS_PATH env var,
// falling back to the default NFS path.
func NewFromEnv() (*Manager, error) {
	key, err := deriveKey()
	if err != nil {
		return nil, fmt.Errorf("derive encryption key: %w", err)
	}
	if p := os.Getenv("APGER_CREDS_PATH"); p != "" {
		return &Manager{path: p, key: key}, nil
	}
	credsPath := os.Getenv("CREDENTIALS_PATH")
	if credsPath == "" {
		credsPath = "/srv/apger-nfs/.credentials"
	}
	return &Manager{
		path: filepath.Join(credsPath, "apger.db"),
		key:  key,
	}, nil
}

// deriveKey generates AES-256 key from machine ID + username.
func deriveKey() ([]byte, error) {
	u, err := user.Current()
	if err != nil {
		return nil, err
	}
	// Use username + hostname as seed
	hostname, _ := os.Hostname()
	seed := u.Username + "@" + hostname
	hash := sha256.Sum256([]byte(seed))
	return hash[:], nil
}

// ── multi-user API ────────────────────────────────────────────────────────────

// LoadAll returns all saved users.
func (m *Manager) LoadAll() ([]Credentials, error) {
	db, err := m.openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var users []Credentials
	err = db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			decrypted, err := m.decrypt(v)
			if err != nil {
				return fmt.Errorf("decrypt %s: %w", k, err)
			}
			var c Credentials
			if err := json.Unmarshal(decrypted, &c); err != nil {
				return fmt.Errorf("unmarshal %s: %w", k, err)
			}
			users = append(users, c)
			return nil
		})
	})
	return users, err
}

// SaveUser creates or updates a user entry (matched by email).
func (m *Manager) SaveUser(c Credentials) error {
	if c.Email == "" {
		return fmt.Errorf("email is required")
	}
	db, err := m.openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	encrypted, err := m.encrypt(data)
	if err != nil {
		return err
	}

	return db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		if err != nil {
			return err
		}
		return b.Put([]byte(c.Email), encrypted)
	})
}

// DeleteUser removes a user by email.
func (m *Manager) DeleteUser(email string) error {
	db, err := m.openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	return db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(email))
	})
}

// ClearPGP removes the PGP key for the user with the given email.
func (m *Manager) ClearPGP(email string) error {
	db, err := m.openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	return db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return fmt.Errorf("user not found")
		}
		v := b.Get([]byte(email))
		if v == nil {
			return fmt.Errorf("user not found")
		}
		decrypted, err := m.decrypt(v)
		if err != nil {
			return err
		}
		var c Credentials
		if err := json.Unmarshal(decrypted, &c); err != nil {
			return err
		}
		c.PGPPrivateKey = ""
		data, err := json.Marshal(c)
		if err != nil {
			return err
		}
		encrypted, err := m.encrypt(data)
		if err != nil {
			return err
		}
		return b.Put([]byte(email), encrypted)
	})
}

// ── single-user compat API (used by orchestrator / pods) ─────────────────────

// Load returns the first user, or empty Credentials if none saved.
func (m *Manager) Load() (Credentials, error) {
	users, err := m.LoadAll()
	if err != nil {
		return Credentials{}, err
	}
	if len(users) == 0 {
		return Credentials{}, nil
	}
	return users[0], nil
}

// Save writes a single Credentials (legacy compat).
func (m *Manager) Save(c Credentials) error {
	return m.SaveUser(c)
}

// ── internal ──────────────────────────────────────────────────────────────────

func (m *Manager) openDB() (*bbolt.DB, error) {
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return nil, fmt.Errorf("create credential dir: %w", err)
	}
	return bbolt.Open(m.path, 0600, nil)
}

func (m *Manager) encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(m.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (m *Manager) decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(m.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
