// Package credentials manages apger credentials.
//
// Storage: /srv/apger-nfs/.credentials/apger.db (encrypted SQLite database on NFS)
// Encryption: AES-256-GCM with key derived from machine ID + username
//
// Multi-node support: All nodes (master + workers) can access credentials via NFS
// and sign packages. Credentials are encrypted and shared across the cluster.
//
// Authentication priority per user:
//  1. GitHub App (AppID + PEM) → JWT → installation token
//  2. PAT
//  3. gh CLI  (`gh auth token`)  — fallback if neither is set
package credentials

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
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
	if c.GitHubAppID != 0 && c.GitHubPEM != "" {
		tok, err := InstallationToken(ctx, c.GitHubAppID, c.GitHubPEM, org)
		if err == nil {
			return tok, nil
		}
	}
	if c.PAT != "" {
		return c.PAT, nil
	}
	if tok, err := ghCLIToken(); err == nil && tok != "" {
		return tok, nil
	}
	return "", fmt.Errorf("no GitHub credentials configured (set github_app_id+github_pem, pat, or run 'gh auth login')")
}

func ghCLIToken() (string, error) {
	out, err := exec.Command("gh", "auth", "token", "--hostname", "github.com").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Manager handles loading and saving credentials in an encrypted SQLite database.
type Manager struct {
	path string
	key  []byte // AES-256 key
}

// New creates a Manager using CREDENTIALS_PATH env var + /apger.db,
// falling back to /srv/apger-nfs/.credentials/apger.db.
func New() (*Manager, error) {
	key, err := deriveKey()
	if err != nil {
		return nil, fmt.Errorf("derive encryption key: %w", err)
	}
	credsPath := os.Getenv("CREDENTIALS_PATH")
	if credsPath == "" {
		credsPath = "/srv/apger-nfs/.credentials"
	}
	return &Manager{path: filepath.Join(credsPath, "apger.db"), key: key}, nil
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
	return &Manager{path: filepath.Join(credsPath, "apger.db"), key: key}, nil
}

func deriveKey() ([]byte, error) {
	u, err := user.Current()
	if err != nil {
		return nil, err
	}
	hostname, _ := os.Hostname()
	hash := sha256.Sum256([]byte(u.Username + "@" + hostname))
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

	rows, err := db.Query(`SELECT data FROM credentials`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []Credentials
	for rows.Next() {
		var blob []byte
		if err := rows.Scan(&blob); err != nil {
			return nil, err
		}
		decrypted, err := m.decrypt(blob)
		if err != nil {
			return nil, fmt.Errorf("decrypt: %w", err)
		}
		var c Credentials
		if err := json.Unmarshal(decrypted, &c); err != nil {
			return nil, fmt.Errorf("unmarshal: %w", err)
		}
		users = append(users, c)
	}
	return users, rows.Err()
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
	_, err = db.Exec(`INSERT INTO credentials(email, data) VALUES(?,?) ON CONFLICT(email) DO UPDATE SET data=excluded.data`, c.Email, encrypted)
	return err
}

// DeleteUser removes a user by email.
func (m *Manager) DeleteUser(email string) error {
	db, err := m.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`DELETE FROM credentials WHERE email=?`, email)
	return err
}

// GetUser returns the credentials for a specific email, or an error if not found.
func (m *Manager) GetUser(email string) (Credentials, error) {
	db, err := m.openDB()
	if err != nil {
		return Credentials{}, err
	}
	defer db.Close()

	var blob []byte
	err = db.QueryRow(`SELECT data FROM credentials WHERE email=?`, email).Scan(&blob)
	if err == sql.ErrNoRows {
		return Credentials{}, fmt.Errorf("user not found: %s", email)
	}
	if err != nil {
		return Credentials{}, err
	}
	decrypted, err := m.decrypt(blob)
	if err != nil {
		return Credentials{}, fmt.Errorf("decrypt: %w", err)
	}
	var c Credentials
	if err := json.Unmarshal(decrypted, &c); err != nil {
		return Credentials{}, err
	}
	return c, nil
}

// ClearPGP removes the PGP key for the user with the given email.
func (m *Manager) ClearPGP(email string) error {
	c, err := m.GetUser(email)
	if err != nil {
		return err
	}
	c.PGPPrivateKey = ""
	return m.SaveUser(c)
}

// ── single-user compat API ────────────────────────────────────────────────────

// Load returns the first user, or empty Credentials if none saved.
func (m *Manager) Load() (Credentials, error) {
	users, err := m.LoadAll()
	if err != nil || len(users) == 0 {
		return Credentials{}, err
	}
	return users[0], nil
}

// Save writes a single Credentials (legacy compat).
func (m *Manager) Save(c Credentials) error { return m.SaveUser(c) }

// ── internal ──────────────────────────────────────────────────────────────────

func (m *Manager) openDB() (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return nil, fmt.Errorf("create credential dir: %w", err)
	}
	dsn := "file:" + m.path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS credentials (email TEXT PRIMARY KEY, data BLOB NOT NULL)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
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
