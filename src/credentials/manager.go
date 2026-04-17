// Package credentials manages apger credentials.
//
// Storage: ~/.credential-manager/apger.json (mode 0600)
// Format: {"users": [...]} — list of Credentials entries, keyed by email.
//
// Authentication priority per user:
//   1. GitHub App (AppID + PEM) → JWT → installation token
//   2. PAT
//   3. gh CLI  (`gh auth token`)  — fallback if neither is set
package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// store is the on-disk JSON structure.
type store struct {
	Users []Credentials `json:"users"`
}

// Manager handles loading and saving credentials.
type Manager struct {
	path string
}

// New creates a Manager using ~/.credential-manager/apger.json.
func New() (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}
	return &Manager{
		path: filepath.Join(home, ".credential-manager", "apger.json"),
	}, nil
}

// NewFromEnv creates a Manager that reads from APGER_CREDS_PATH env var,
// falling back to the default path.
func NewFromEnv() (*Manager, error) {
	if p := os.Getenv("APGER_CREDS_PATH"); p != "" {
		return &Manager{path: p}, nil
	}
	return New()
}

// ── multi-user API ────────────────────────────────────────────────────────────

// LoadAll returns all saved users.
func (m *Manager) LoadAll() ([]Credentials, error) {
	s, err := m.readStore()
	if err != nil {
		return nil, err
	}
	return s.Users, nil
}

// SaveUser creates or updates a user entry (matched by email, then name).
func (m *Manager) SaveUser(c Credentials) error {
	s, _ := m.readStore()
	updated := false
	for i, u := range s.Users {
		if u.Email == c.Email || (c.Email == "" && u.Name == c.Name) {
			s.Users[i] = c
			updated = true
			break
		}
	}
	if !updated {
		s.Users = append(s.Users, c)
	}
	return m.writeStore(s)
}

// DeleteUser removes a user by email.
func (m *Manager) DeleteUser(email string) error {
	s, err := m.readStore()
	if err != nil {
		return err
	}
	filtered := s.Users[:0]
	for _, u := range s.Users {
		if u.Email != email {
			filtered = append(filtered, u)
		}
	}
	s.Users = filtered
	return m.writeStore(s)
}

// ClearPGP removes the PGP key for the user with the given email.
func (m *Manager) ClearPGP(email string) error {
	s, err := m.readStore()
	if err != nil {
		return err
	}
	for i, u := range s.Users {
		if u.Email == email {
			s.Users[i].PGPPrivateKey = ""
			break
		}
	}
	return m.writeStore(s)
}

// ── single-user compat API (used by orchestrator / pods) ─────────────────────

// Load returns the first user, or empty Credentials if none saved.
// Also handles the legacy single-entry format {"name":...} for backwards compat.
func (m *Manager) Load() (Credentials, error) {
	s, err := m.readStore()
	if err != nil {
		return Credentials{}, err
	}
	if len(s.Users) == 0 {
		return Credentials{}, nil
	}
	return s.Users[0], nil
}

// Save writes a single Credentials, replacing the first entry (legacy compat).
func (m *Manager) Save(c Credentials) error {
	return m.SaveUser(c)
}

// ── internal ──────────────────────────────────────────────────────────────────

func (m *Manager) readStore() (store, error) {
	data, err := os.ReadFile(m.path)
	if os.IsNotExist(err) {
		return store{}, nil
	}
	if err != nil {
		return store{}, fmt.Errorf("read credentials: %w", err)
	}

	// Try new multi-user format first
	var s store
	if err := json.Unmarshal(data, &s); err == nil && s.Users != nil {
		return s, nil
	}

	// Legacy: single Credentials object
	var c Credentials
	if err := json.Unmarshal(data, &c); err == nil && (c.Name != "" || c.Email != "") {
		return store{Users: []Credentials{c}}, nil
	}

	return store{}, nil
}

func (m *Manager) writeStore(s store) error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return fmt.Errorf("create credential dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}
