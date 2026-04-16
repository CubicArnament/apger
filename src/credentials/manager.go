// Package credentials manages apger credentials stored in
// $HOME/.credential-manager/apger.json (mode 0600).
//
// Authentication uses GitHub App (AppID + PEM private key) instead of PAT.
// JWT is generated on-the-fly from the PEM key and exchanged for a
// short-lived installation token via the GitHub API.
package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Credentials holds all apger credentials.
type Credentials struct {
	Name  string `json:"name"`
	Email string `json:"email"`

	// GitHub authentication — use one of:
	//   GitHub App (preferred): set GitHubAppID + GitHubPEM
	//   PAT (fallback):         set PAT only
	GitHubAppID int64  `json:"github_app_id,omitempty"` // GitHub App ID (numeric)
	GitHubPEM   string `json:"github_pem,omitempty"`    // PEM-encoded RSA private key
	PAT         string `json:"pat,omitempty"`           // Personal Access Token (fallback)

	// PGP signing key
	PGPPrivateKey string `json:"pgp_private_key,omitempty"` // armored OpenPGP private key
}

// AuthToken returns a GitHub access token using the best available method:
// GitHub App (JWT → installation token) if AppID+PEM are set, otherwise PAT.
func (c Credentials) AuthToken(ctx context.Context, org string) (string, error) {
	if c.GitHubAppID != 0 && c.GitHubPEM != "" {
		return InstallationToken(ctx, c.GitHubAppID, c.GitHubPEM, org)
	}
	if c.PAT != "" {
		return c.PAT, nil
	}
	return "", fmt.Errorf("no GitHub credentials configured (set github_app_id+github_pem or pat)")
}

// Manager handles loading and saving credentials.
type Manager struct {
	path string
}

// New creates a Manager using $HOME/.credential-manager/apger.json.
func New() (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}
	return &Manager{
		path: filepath.Join(home, ".credential-manager", "apger.json"),
	}, nil
}

// NewFromEnv creates a Manager that reads from the APGER_CREDS_PATH env var,
// falling back to the default path. Used inside Kubernetes pods where credentials
// are mounted from a Secret.
func NewFromEnv() (*Manager, error) {
	if p := os.Getenv("APGER_CREDS_PATH"); p != "" {
		return &Manager{path: p}, nil
	}
	return New()
}

// Load reads credentials from disk. Returns empty Credentials if file doesn't exist.
func (m *Manager) Load() (Credentials, error) {
	data, err := os.ReadFile(m.path)
	if os.IsNotExist(err) {
		return Credentials{}, nil
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("read credentials: %w", err)
	}
	var c Credentials
	return c, json.Unmarshal(data, &c)
}

// Save writes credentials to disk atomically with mode 0600.
func (m *Manager) Save(c Credentials) error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return fmt.Errorf("create credential dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}

// ClearPGP removes only the PGP private key.
func (m *Manager) ClearPGP() error {
	c, err := m.Load()
	if err != nil {
		return err
	}
	c.PGPPrivateKey = ""
	return m.Save(c)
}
