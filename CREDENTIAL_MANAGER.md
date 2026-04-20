# Credential Manager Improvements

## Overview

The apger credential manager has been significantly improved with encrypted storage, multi-maintainer support, and comprehensive validation.

**Storage Location:** `$HOME/.apger/credentials/apger.db`

**Kubernetes:** Credentials stored on master node via hostPath PVC. Worker nodes build packages but don't sign them — signing happens only on master node where credentials are stored.

## Key Changes

### 1. Encrypted Storage (bbolt + AES-256-GCM)

**Before:** Credentials stored in plaintext JSON at `~/.credential-manager/apger.json`

**After:** Encrypted bbolt database at `~/.apger/credentials/apger.db`

- **Encryption:** AES-256-GCM with key derived from `username@hostname` (SHA-256)
- **Format:** Each maintainer stored as encrypted JSON blob, keyed by email
- **Security:** Only the user on the same machine can decrypt the database
- **Kubernetes:** Mounted via hostPath PVC on master node only

### 2. Key Validation

#### OpenPGP Keys
- **ECC Required:** RSA keys are rejected with error: `"RSA Deprecated - введите ECC ключ"`
- **Supported:** ECDSA, EdDSA, ECDH (Curve25519, P-256, P-384, P-521)
- **Validation:** Parses armored key and checks `PubKeyAlgo` field

#### Libsodium Keys
- **Format:** Base64-encoded Ed25519 keys (32 or 64 bytes)
- **Validation:** Verifies key can generate valid Ed25519 keypair
- **Fallback:** If OpenPGP validation fails, tries libsodium format

#### GitHub Credentials
- **PAT:** Validates by calling `GET /user` API endpoint
- **PEM:** Validates by attempting JWT signature generation
- **Real-time:** Validation happens on save (ctrl+s in TUI)

### 3. Multi-Maintainer Support

**Storage:**
- Multiple maintainers stored in same encrypted database
- Each maintainer identified by unique email address
- Database grows automatically as maintainers are added

**TUI Workflow:**
1. Press `c` → Credentials screen
2. Press `n` → Add new maintainer
3. Fill form → ctrl+s to save
4. Repeat for additional maintainers

### 4. Build Flow with Maintainer Selection

**Single Maintainer:**
```
Press 'b' → Build Confirmation (Yes/No) → Build starts
```

**Multiple Maintainers:**
```
Press 'b' → Select Maintainer → Build Confirmation (Yes/No) → Build starts
```

**Screens:**

#### Maintainer Selection Screen
- Shows list of all configured maintainers (by email)
- Navigate: ↑/↓ or j/k
- Select: Enter
- Cancel: Esc

#### Build Confirmation Screen
- Shows selected maintainer
- Shows number of packages to build
- Two buttons: `[ Yes ]` and `[ No ]`
- Navigate: ←/→ or h/l
- Confirm: Enter
- Cancel: Esc

### 5. TUI Credentials Screen Updates

**Validation on Save:**
- All fields validated before saving to database
- Errors displayed immediately in red
- Validation includes:
  - Name and email required
  - GitHub credentials authenticity check
  - PGP key format and algorithm check

**Error Messages:**
- `"RSA Deprecated - введите ECC ключ"` — RSA key detected
- `"invalid PAT: HTTP 401"` — GitHub PAT authentication failed
- `"invalid PEM key: not RSA or ECDSA"` — Malformed GitHub App PEM
- `"invalid Ed25519 seed"` — Libsodium key validation failed

## Migration from JSON to bbolt

**Automatic:** No migration needed. Old JSON files are ignored.

**Manual Migration:**
1. Open old `~/.credential-manager/apger.json`
2. Copy credentials
3. Open TUI → press `c` → press `n`
4. Paste credentials → ctrl+s
5. Delete old JSON file

**New Location:** `~/.apger/credentials/apger.db`

## Kubernetes Deployment

### Complete Lifecycle Workflow

**1. Initial Start (no credentials):**
```bash
kubectl apply -f k8s-manifest.yml
# - Pod starts
# - initContainer: sync-credentials (no data, skips)
# - Pod runs, TUI available
# - PVC not mounted (optional)
```

**2. Add Credentials in TUI:**
```bash
kubectl attach -it apger -n apger
# In TUI:
# - Press 'c' → credentials screen
# - Press 'n' → new maintainer
# - Fill form → ctrl+s
# - Credentials saved to /home/.apger/credentials/apger.db
```

**3. Build Packages:**
```bash
# In TUI:
# - Press 'b' → select packages
# - Build completes
# - Returns to TUI (Pod stays alive)
```

**4. Quit (Pod terminates completely):**
```bash
# In TUI:
# - Press 'q' → quit confirmation
# - Select "Yes" → "Bye Bye! ^_^"
# - Pod terminates (restartPolicy: Never)
# - Pod status: Completed
```

**5. Manual Restart:**
```bash
# Pod does NOT auto-restart after quit
# Restart manually:
kubectl apply -f k8s-manifest.yml

# Or delete and recreate:
kubectl delete pod apger -n apger
kubectl apply -f k8s-manifest.yml

# - Pod restarts
# - initContainer: sync-credentials
#   → Reads /home/.apger/credentials/apger.db
#   → Encrypts and stores in Secret apger-credentials
# - PVC mounts (data exists)
# - apger reads encrypted DB from Secret
# - Decrypts and loads credentials
# - Ready to build with saved maintainers
```

### Quit Confirmation

**New Feature:** Press `q` in TUI shows confirmation dialog:

```
  Quit APGer

  Are you sure you want to quit?
  Pod will terminate completely.
  Restart manually: kubectl apply -f k8s-manifest.yml

  [ Yes ]  [ No ]

  ←/→ select  enter confirm  esc cancel
```

- **Yes:** Shows "Bye Bye! ^_^" and exits (Pod terminates, no auto-restart)
- **No:** Returns to dashboard
- **Esc:** Cancels and returns to dashboard

### Data Persistence

**What persists (survives Pod termination):**
- ✅ Credentials (encrypted in hostPath PVC at `~/.apger/credentials/`)
- ✅ Built packages (in NFS PVC)
- ✅ Package database (in NFS PVC)

**What is ephemeral (lost on Pod termination):**
- ⚠️ Build logs
- ⚠️ TUI state
- ⚠️ Unsaved recipe edits
- ⚠️ Pod itself (must restart manually)

### Setup

**1. Configure hostPath (one-time):**
```bash
# Edit k8s-manifest.yml
sed -i "s|YOUR_USERNAME|$(whoami)|g" k8s-manifest.yml

# Apply
kubectl apply -f k8s-manifest.yml
```

**2. Add credentials:**
```bash
# Attach to Pod
kubectl attach -it apger -n apger

# In TUI:
# - Press 'c' → credentials screen
# - Press 'n' → new maintainer
# - Fill form → ctrl+s to save
```

**3. Restart Pod (optional):**
```bash
kubectl delete pod apger -n apger
# Pod recreates automatically
# PVC mounts with saved credentials
```

### Verification

```bash
# Check if PVC is bound
kubectl get pvc apger-credentials -n apger
# STATUS: Bound (if credentials exist)
# STATUS: Pending (if no credentials yet)

# Check credentials on host
ls -la ~/.apger/credentials/
# Should show apger.db after saving in TUI
```

## Security Considerations

### Encryption Key Derivation
- Key = SHA256(username + "@" + hostname)
- **Pros:** No password prompt, seamless UX
- **Cons:** Anyone with access to the same user account can decrypt
- **Recommendation:** Use full-disk encryption (BitLocker/LUKS) for additional protection

### Key Storage
- Database file: `~/.apger/credentials/apger.db` (mode 0600)
- Directory: `~/.apger/credentials/` (mode 0700)
- No plaintext credentials on disk
- Kubernetes: hostPath PVC on master node only

### Validation
- GitHub credentials validated against live API
- PGP keys parsed and algorithm checked
- Libsodium keys verified by keypair generation

## API Changes

### credentials.Manager

**New Methods:**
```go
// LoadAll returns all maintainers
func (m *Manager) LoadAll() ([]Credentials, error)

// SaveUser creates or updates a maintainer (by email)
func (m *Manager) SaveUser(c Credentials) error

// DeleteUser removes a maintainer by email
func (m *Manager) DeleteUser(email string) error

// ClearPGP removes PGP key for a maintainer
func (m *Manager) ClearPGP(email string) error
```

**Changed:**
- Storage backend: JSON → bbolt
- File extension: `.json` → `.db`
- Encryption: none → AES-256-GCM

### credentials.Validation

**New Functions:**
```go
// ValidatePGPKey checks OpenPGP key is ECC (not RSA)
func ValidatePGPKey(armoredKey string) error

// ValidateLibsodiumKey checks Ed25519 key format
func ValidateLibsodiumKey(key string) error

// ValidateGitHubPAT validates PAT against GitHub API
func ValidateGitHubPAT(ctx context.Context, pat string) error

// ValidateGitHubAppPEM validates PEM key by JWT signing
func ValidateGitHubAppPEM(appID int64, pemKey string) error

// ValidateCredentials validates all fields
func ValidateCredentials(ctx context.Context, c Credentials) error

// HasPassphrase checks if OpenPGP key is encrypted
func HasPassphrase(armoredKey string) bool
```

## Dependencies Added

```go
require (
    github.com/GoKillers/libsodium-go v0.0.0-20171022220152-dd733721c3cb
    go.etcd.io/bbolt v1.4.3  // already present
)
```

## Testing

### Manual Testing Checklist

- [ ] Add maintainer with ECC OpenPGP key → should save
- [ ] Add maintainer with RSA OpenPGP key → should show "RSA Deprecated" error
- [ ] Add maintainer with libsodium Ed25519 key → should save
- [ ] Add maintainer with invalid GitHub PAT → should show HTTP error
- [ ] Add maintainer with valid GitHub PAT → should save
- [ ] Add maintainer with invalid PEM → should show parse error
- [ ] Build with 1 maintainer → should skip selection, show confirmation
- [ ] Build with 2+ maintainers → should show selection, then confirmation
- [ ] Confirmation "Yes" → should start build
- [ ] Confirmation "No" → should return to dashboard
- [ ] Database encryption → check `~/.credential-manager/apger.db` is binary

### Security Testing

```bash
# Verify database is encrypted
file ~/.apger/credentials/apger.db
# Output: "data" (not "ASCII text")

# Verify file permissions
ls -la ~/.apger/credentials/
# apger.db should be -rw------- (0600)

# Verify encryption key derivation
# Run on different machine → should not decrypt

# Kubernetes: verify PVC is bound to master node
kubectl get pv apger-credentials-pv -o yaml
# Check hostPath points to correct user home directory
```

## Known Limitations

1. **Key Derivation:** Uses username+hostname, not user-provided password
2. **No Key Rotation:** Changing username/hostname breaks decryption
3. **libsodium CGO:** Requires libsodium installed on system (Linux/macOS)
4. **Windows libsodium:** May require manual DLL installation

## Future Improvements

- [ ] Add password-based key derivation (PBKDF2/Argon2)
- [ ] Add key rotation support
- [ ] Add backup/restore functionality
- [ ] Add credential export (encrypted archive)
- [ ] Add OS keychain integration (macOS Keychain, Windows Credential Manager)
- [ ] Add hardware token support (YubiKey, Nitrokey)

## References

- [ProtonMail/go-crypto](https://github.com/ProtonMail/go-crypto) — OpenPGP implementation
- [GoKillers/libsodium-go](https://github.com/GoKillers/libsodium-go) — libsodium bindings
- [bbolt](https://github.com/etcd-io/bbolt) — Embedded key-value database
- [AES-GCM](https://en.wikipedia.org/wiki/Galois/Counter_Mode) — Authenticated encryption
