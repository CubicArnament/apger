package tui

// Credentials screen — keyboard-driven form for managing apger credentials.
//
// Auth modes (toggle with [r]):
//   PAT  — Personal Access Token
//   App  — GitHub App (numeric AppID + PEM private key)
//
// Actions:
//   ctrl+s  — save credentials to ~/.credential-manager/apger.json
//   ctrl+k  — create / update Kubernetes Secret apger-credentials in the
//              configured namespace (requires kubectl in PATH or in-cluster)
//   ctrl+d  — self-destroy-pgp: generate revocation cert, upload, clear key

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/NurOS-Linux/apger/src/credentials"
	"github.com/NurOS-Linux/apger/src/pgp"
	"github.com/NurOS-Linux/apger/src/publisher"
)

// destroyState tracks the self-destroy-pgp confirmation flow.
type destroyState int

const (
	destroyIdle      destroyState = iota
	destroyAskPass                // waiting for passphrase input
	destroyInProgress             // executing
	destroyDone                   // finished (success or error)
)

// authMode selects which GitHub authentication method is active.
type authMode int

const (
	authModePAT authMode = iota // Personal Access Token
	authModeApp                 // GitHub App (AppID + PEM)
)

// k8sSecretState tracks the kubectl secret creation flow.
type k8sSecretState int

const (
	k8sIdle       k8sSecretState = iota
	k8sInProgress                // running kubectl
	k8sDone                      // finished
)

// CredentialsScreen is the TUI model for the credentials management screen.
type CredentialsScreen struct {
	ctx context.Context
	mgr *credentials.Manager
	org string // NurOS-Packages org name

	// Form fields:
	//  0 = Name
	//  1 = Email
	//  2 = PAT          (PAT mode only)
	//  3 = App ID       (App mode only)
	//  4 = App PEM key  (App mode only)
	//  5 = PGP key
	//  6 = K8s namespace
	fields [7]textinput.Model
	focus  int
	pgpSet bool
	mode   authMode

	// Self-destroy flow
	destroy     destroyState
	destroyPass textinput.Model
	destroyMsg  string

	// K8s secret creation
	k8sState k8sSecretState
	k8sMsg   string

	err   error
	saved bool
}

var credFieldLabels = [7]string{
	"Name",
	"Email",
	"PAT",
	"App ID",
	"App PEM key (paste)",
	"PGP Key (paste armored)",
	"K8s namespace",
}

// NewCredentialsScreen creates the credentials screen.
func NewCredentialsScreen(org string) (*CredentialsScreen, error) {
	mgr, err := credentials.NewFromEnv()
	if err != nil {
		return nil, err
	}

	s := &CredentialsScreen{ctx: context.Background(), mgr: mgr, org: org}

	for i := range s.fields {
		f := textinput.New()
		f.Prompt = ""
		switch i {
		case 2: // PAT — mask
			f.EchoMode = textinput.EchoPassword
			f.EchoCharacter = '•'
		case 3: // App ID
			f.Placeholder = "e.g. 123456"
		case 4, 5: // PEM / PGP — no char limit, multiline paste
			f.CharLimit = 0
		case 6: // namespace
			f.Placeholder = "apger"
		}
		s.fields[i] = f
	}
	s.fields[0].Focus()

	dp := textinput.New()
	dp.Placeholder = "(empty if no passphrase)"
	dp.EchoMode = textinput.EchoPassword
	dp.EchoCharacter = '•'
	s.destroyPass = dp

	// Load existing credentials
	creds, _ := mgr.Load()
	s.fields[0].SetValue(creds.Name)
	s.fields[1].SetValue(creds.Email)
	s.fields[2].SetValue(creds.PAT)
	if creds.GitHubAppID != 0 {
		s.fields[3].SetValue(fmt.Sprintf("%d", creds.GitHubAppID))
	}
	if creds.GitHubPEM != "" {
		s.fields[4].SetValue("[PEM loaded — paste new to replace]")
	}
	s.pgpSet = creds.PGPPrivateKey != ""
	if s.pgpSet {
		s.fields[5].SetValue("[key loaded — paste new to replace]")
	}
	s.fields[6].SetValue("apger")

	// Default mode: App if AppID+PEM are set, otherwise PAT
	if creds.GitHubAppID != 0 && creds.GitHubPEM != "" {
		s.mode = authModeApp
	} else {
		s.mode = authModePAT
	}

	return s, nil
}

// WithContext sets the context used for background operations.
func (s *CredentialsScreen) WithContext(ctx context.Context) *CredentialsScreen {
	s.ctx = ctx
	return s
}

// Init implements tea.Model.
func (s *CredentialsScreen) Init() tea.Cmd { return textinput.Blink }

// Update implements tea.Model.
func (s *CredentialsScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return s.handleKey(msg)
	case credSavedMsg:
		s.saved = true
		s.err = nil
		return s, nil
	case credErrMsg:
		s.err = msg.err
		s.saved = false
		return s, nil
	case destroyDoneMsg:
		s.destroy = destroyDone
		s.destroyMsg = msg.msg
		s.pgpSet = false
		return s, nil
	case k8sSecretDoneMsg:
		s.k8sState = k8sDone
		s.k8sMsg = msg.msg
		return s, nil
	}

	var cmd tea.Cmd
	if s.destroy == destroyAskPass {
		s.destroyPass, cmd = s.destroyPass.Update(msg)
	} else {
		s.fields[s.focus], cmd = s.fields[s.focus].Update(msg)
	}
	return s, cmd
}

func (s *CredentialsScreen) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Self-destroy flow intercepts all keys
	switch s.destroy {
	case destroyAskPass:
		switch key {
		case "enter":
			return s, s.verifyAndDestroy()
		case "esc":
			s.destroy = destroyIdle
			s.destroyMsg = ""
		}
		return s, nil
	case destroyInProgress:
		return s, nil
	case destroyDone:
		if key == "esc" || key == "enter" {
			s.destroy = destroyIdle
			s.destroyMsg = ""
		}
		return s, nil
	}

	// K8s done — dismiss with any key
	if s.k8sState == k8sDone {
		s.k8sState = k8sIdle
		s.k8sMsg = ""
		return s, nil
	}

	switch key {
	case "tab", "down":
		s.fields[s.focus].Blur()
		s.focus = s.nextField(s.focus + 1)
		s.fields[s.focus].Focus()

	case "shift+tab", "up":
		s.fields[s.focus].Blur()
		s.focus = s.nextField(s.focus - 1)
		s.fields[s.focus].Focus()

	case "r":
		// Toggle PAT ↔ App auth mode
		s.fields[s.focus].Blur()
		if s.mode == authModePAT {
			s.mode = authModeApp
		} else {
			s.mode = authModePAT
		}
		s.focus = 0
		s.fields[0].Focus()

	case "ctrl+s":
		return s, s.save()

	case "ctrl+k":
		// Create / update Kubernetes Secret
		if s.k8sState == k8sInProgress {
			return s, nil
		}
		s.k8sState = k8sInProgress
		s.k8sMsg = ""
		return s, s.createK8sSecret()

	case "ctrl+d":
		if !s.pgpSet {
			s.destroyMsg = "No PGP key stored."
			return s, nil
		}
		s.destroy = destroyAskPass
		s.destroyPass.SetValue("")
		s.destroyPass.Focus()
		s.destroyMsg = ""
	}
	return s, nil
}

// nextField returns the next navigable field index for the current auth mode.
func (s *CredentialsScreen) nextField(idx int) int {
	total := len(s.fields)
	for i := 0; i < total; i++ {
		candidate := ((idx + i) % total + total) % total
		if s.isFieldVisible(candidate) {
			return candidate
		}
	}
	return 0
}

// isFieldVisible returns whether a field is shown for the current auth mode.
func (s *CredentialsScreen) isFieldVisible(i int) bool {
	switch i {
	case 2: // PAT — only in PAT mode
		return s.mode == authModePAT
	case 3, 4: // AppID, PEM — only in App mode
		return s.mode == authModeApp
	default:
		return true
	}
}

// View implements tea.Model.
func (s *CredentialsScreen) View() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("  Credentials") + "\n\n")

	// Self-destroy overlay
	if s.destroy != destroyIdle {
		b.WriteString(s.viewDestroy())
		return b.String()
	}

	// K8s in-progress overlay
	if s.k8sState == k8sInProgress {
		b.WriteString(styleDim.Render("  Creating Kubernetes secret...") + "\n")
		return b.String()
	}
	if s.k8sState == k8sDone {
		if strings.HasPrefix(s.k8sMsg, "✗") {
			b.WriteString(styleError.Render("  "+s.k8sMsg) + "\n")
		} else {
			b.WriteString(styleOK.Render("  "+s.k8sMsg) + "\n")
		}
		b.WriteString(styleHelp.Render("  press any key to continue") + "\n")
		return b.String()
	}

	// Auth mode radio
	patLabel := "( ) PAT"
	appLabel := "( ) GitHub App (JWT)"
	if s.mode == authModePAT {
		patLabel = styleSelected.Render("(●) PAT")
		appLabel = styleDim.Render("( ) GitHub App (JWT)")
	} else {
		patLabel = styleDim.Render("( ) PAT")
		appLabel = styleSelected.Render("(●) GitHub App (JWT)")
	}
	b.WriteString(fmt.Sprintf("  Auth:  %s   %s   %s\n\n",
		patLabel, appLabel, styleHelp.Render("[r] toggle")))

	// Form fields
	for i, label := range credFieldLabels {
		if !s.isFieldVisible(i) {
			continue
		}
		focused := i == s.focus
		labelStyle := styleDim
		if focused {
			labelStyle = styleSelected
		}
		b.WriteString(labelStyle.Render(fmt.Sprintf("  %-24s", label+":")))

		switch {
		case i == 4 && strings.HasPrefix(s.fields[i].Value(), "[PEM"):
			b.WriteString(styleOK.Render("[PEM key loaded ✓]"))
		case i == 5 && s.pgpSet && strings.HasPrefix(s.fields[i].Value(), "[key"):
			b.WriteString(styleOK.Render("[PGP key loaded ✓]"))
		default:
			b.WriteString(s.fields[i].View())
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Status line
	if s.saved {
		b.WriteString(styleOK.Render("  ✓ Saved") + "\n")
	}
	if s.err != nil {
		b.WriteString(styleError.Render("  ✗ "+s.err.Error()) + "\n")
	}
	if s.destroyMsg != "" {
		b.WriteString(styleDim.Render("  "+s.destroyMsg) + "\n")
	}
	if s.k8sMsg != "" {
		b.WriteString(styleOK.Render("  "+s.k8sMsg) + "\n")
	}

	// Auth status
	creds, _ := s.mgr.Load()
	authStatus := styleDim.Render("  GitHub auth: not configured")
	if creds.GitHubAppID != 0 && creds.GitHubPEM != "" {
		authStatus = styleOK.Render(fmt.Sprintf("  GitHub auth: App #%d (JWT)", creds.GitHubAppID))
	} else if creds.PAT != "" {
		authStatus = styleNormal.Render("  GitHub auth: PAT")
	}
	pgpStatus := styleDim.Render("  PGP key: not set")
	if s.pgpSet {
		pgpStatus = styleOK.Render("  PGP key: loaded ✓")
	}
	b.WriteString(authStatus + "   " + pgpStatus + "\n\n")

	// Help bar
	help := "tab/↑↓ navigate  r auth mode  ctrl+s save  ctrl+k → K8s secret"
	if s.pgpSet {
		help += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("ctrl+d destroy-pgp")
	}
	b.WriteString(styleHelp.Render("  "+help) + "\n")

	return b.String()
}

func (s *CredentialsScreen) viewDestroy() string {
	var b strings.Builder
	switch s.destroy {
	case destroyAskPass:
		b.WriteString(styleError.Render("  ⚠  SELF-DESTROY PGP KEY") + "\n\n")
		b.WriteString(styleDim.Render("  This will:\n"))
		b.WriteString(styleDim.Render("    1. Generate a revocation certificate\n"))
		b.WriteString(styleDim.Render("    2. Upload it to NurOS-Packages/.pgp-revocations/\n"))
		b.WriteString(styleDim.Render("    3. Delete the private key from local storage\n\n"))
		b.WriteString(styleNormal.Render("  PGP passphrase (empty if none):\n"))
		b.WriteString("  " + s.destroyPass.View() + "\n\n")
		b.WriteString(styleHelp.Render("  enter confirm  esc cancel"))
	case destroyInProgress:
		b.WriteString(styleDim.Render("  Revoking key..."))
	case destroyDone:
		if strings.HasPrefix(s.destroyMsg, "✗") {
			b.WriteString(styleError.Render("  "+s.destroyMsg) + "\n")
		} else {
			b.WriteString(styleOK.Render("  "+s.destroyMsg) + "\n")
		}
		b.WriteString(styleHelp.Render("  enter or esc to continue"))
	}
	return b.String()
}

// ── tea.Cmd helpers ───────────────────────────────────────────────────────────

type credSavedMsg struct{}
type credErrMsg struct{ err error }
type destroyDoneMsg struct{ msg string }
type k8sSecretDoneMsg struct{ msg string }

func (s *CredentialsScreen) save() tea.Cmd {
	return func() tea.Msg {
		creds, err := s.mgr.Load()
		if err != nil {
			return credErrMsg{err}
		}

		creds.Name = s.fields[0].Value()
		creds.Email = s.fields[1].Value()

		switch s.mode {
		case authModePAT:
			creds.PAT = s.fields[2].Value()
			creds.GitHubAppID = 0
			creds.GitHubPEM = ""
		case authModeApp:
			var appID int64
			fmt.Sscanf(s.fields[3].Value(), "%d", &appID)
			creds.GitHubAppID = appID
			pem := s.fields[4].Value()
			if pem != "" && !strings.HasPrefix(pem, "[PEM") {
				creds.GitHubPEM = pem
			}
			creds.PAT = ""
		}

		newKey := s.fields[5].Value()
		if newKey != "" && !strings.HasPrefix(newKey, "[key") {
			creds.PGPPrivateKey = newKey
		}

		if err := s.mgr.Save(creds); err != nil {
			return credErrMsg{err}
		}
		return credSavedMsg{}
	}
}

// createK8sSecret runs kubectl to create/replace the apger-credentials Secret.
// It reads the current saved credentials and builds the kubectl command.
func (s *CredentialsScreen) createK8sSecret() tea.Cmd {
	namespace := strings.TrimSpace(s.fields[6].Value())
	if namespace == "" {
		namespace = "apger"
	}

	return func() tea.Msg {
		creds, err := s.mgr.Load()
		if err != nil {
			return k8sSecretDoneMsg{"✗ load credentials: " + err.Error()}
		}

		// Build kubectl args for --from-literal / --from-file style
		// We use --from-literal for all fields to avoid temp files.
		// PEM and PGP keys may contain newlines — kubectl handles them fine.
		args := []string{
			"create", "secret", "generic", "apger-credentials",
			"--namespace=" + namespace,
			"--save-config",
			"--dry-run=client",
			"-o", "yaml",
		}

		args = append(args, "--from-literal=name="+creds.Name)
		args = append(args, "--from-literal=email="+creds.Email)

		switch s.mode {
		case authModePAT:
			if creds.PAT == "" {
				return k8sSecretDoneMsg{"✗ PAT is empty — save credentials first"}
			}
			args = append(args, "--from-literal=github_app_id=0")
			args = append(args, "--from-literal=github_pem=")
			args = append(args, "--from-literal=pat="+creds.PAT)
		case authModeApp:
			if creds.GitHubAppID == 0 || creds.GitHubPEM == "" {
				return k8sSecretDoneMsg{"✗ App ID or PEM is empty — save credentials first"}
			}
			args = append(args, fmt.Sprintf("--from-literal=github_app_id=%d", creds.GitHubAppID))
			args = append(args, "--from-literal=github_pem="+creds.GitHubPEM)
			args = append(args, "--from-literal=pat=")
		}

		if creds.PGPPrivateKey != "" {
			args = append(args, "--from-literal=pgp_private_key="+creds.PGPPrivateKey)
		} else {
			args = append(args, "--from-literal=pgp_private_key=")
		}

		// First generate YAML via dry-run, then pipe to kubectl apply
		// This handles both create and update (idempotent).
		dryRun := exec.CommandContext(s.ctx, "kubectl", args...)
		yaml, err := dryRun.Output()
		if err != nil {
			return k8sSecretDoneMsg{"✗ kubectl dry-run: " + err.Error()}
		}

		apply := exec.CommandContext(s.ctx, "kubectl", "apply", "-f", "-", "--namespace="+namespace)
		apply.Stdin = strings.NewReader(string(yaml))
		out, err := apply.CombinedOutput()
		if err != nil {
			return k8sSecretDoneMsg{"✗ kubectl apply: " + strings.TrimSpace(string(out))}
		}

		return k8sSecretDoneMsg{fmt.Sprintf("✓ Secret apger-credentials applied in namespace %q", namespace)}
	}
}

func (s *CredentialsScreen) verifyAndDestroy() tea.Cmd {
	s.destroy = destroyInProgress
	passphrase := s.destroyPass.Value()

	return func() tea.Msg {
		creds, err := s.mgr.Load()
		if err != nil {
			return destroyDoneMsg{"✗ " + err.Error()}
		}
		if creds.PGPPrivateKey == "" {
			return destroyDoneMsg{"✗ No PGP key stored"}
		}

		keyHasPass := pgp.HasPassphrase(creds.PGPPrivateKey)
		if keyHasPass && passphrase == "" {
			return destroyDoneMsg{"✗ Key is passphrase-protected — enter passphrase"}
		}
		if !keyHasPass && passphrase != "" {
			return destroyDoneMsg{"✗ Key has no passphrase — leave field empty"}
		}

		revCert, err := pgp.GenerateRevocationCert(creds.PGPPrivateKey, passphrase)
		if err != nil {
			return destroyDoneMsg{"✗ Generate revocation cert: " + err.Error()}
		}

		// Upload to GitHub (best-effort)
		pub := publisher.New(creds, s.org)
		pkgName := creds.Email
		if pkgName == "" {
			pkgName = "apger"
		}
		_ = pub.UploadRevocationCert(s.ctx, pkgName, revCert)

		if err := s.mgr.ClearPGP(); err != nil {
			return destroyDoneMsg{"✗ Clear local key: " + err.Error()}
		}

		return destroyDoneMsg{"✓ PGP key revoked and destroyed. Revocation cert uploaded."}
	}
}
