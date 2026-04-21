package tui

// Credentials screen — multi-user credential manager.
//
// Layout:
//   Left panel  — list of saved users (from credential manager)
//   Right panel — form for the selected / new user
//
// Auth modes (toggle with [r] in form panel):
//   PAT  — Personal Access Token  → go-github REST API
//   PEM  — GitHub App PEM key     → JWT → installation token
//
// Keybindings:
//   tab / shift+tab  — switch left ↔ right panel
//   ↑/↓  j/k        — navigate user list (left) or form fields (right)
//   n                — new user (right panel, blank form)
//   enter            — select user from list → load into form
//   ctrl+s           — save current form to credential manager
//   ctrl+k           — push credentials as Kubernetes Secret (kubectl apply)
//   ctrl+d           — self-destroy PGP key (revoke + delete)
//   delete / d       — delete selected user from list (left panel)
//   esc              — back to dashboard

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

// ── types ─────────────────────────────────────────────────────────────────────

type credPanel int

const (
	credPanelList credPanel = iota
	credPanelForm
)

type credAuthMode int

const (
	credAuthPAT credAuthMode = iota
	credAuthPEM
)

type credDestroyState int

const (
	credDestroyIdle     credDestroyState = iota
	credDestroyAskPass
	credDestroyWorking
	credDestroyDone
)

type credK8sState int

const (
	credK8sIdle    credK8sState = iota
	credK8sWorking
	credK8sDone
)

// formField indices
const (
	fName      = 0
	fEmail     = 1
	fGitHubOrg = 2
	fPAT       = 3 // PAT mode only
	fAppID     = 4 // PEM mode only
	fPEM       = 5 // PEM mode only
	fPGP       = 6
	fNamespace = 7
	fCount     = 8
)

var formLabels = [fCount]string{
	"Name",
	"Email",
	"GitHub Org",
	"PAT",
	"App ID",
	"App PEM (paste)",
	"PGP key (paste armored)",
	"K8s namespace",
}

// ── CredentialsScreen ─────────────────────────────────────────────────────────

type CredentialsScreen struct {
	ctx context.Context
	mgr *credentials.Manager
	org string

	// left panel — user list
	panel    credPanel
	users    []credentials.Credentials // all saved users (keyed by email)
	listIdx  int                        // cursor in user list

	// right panel — form
	fields   [fCount]textinput.Model
	fieldIdx int
	authMode credAuthMode
	pgpSet   bool
	pemSet   bool

	// self-destroy flow
	destroy     credDestroyState
	destroyPass textinput.Model
	destroyMsg  string

	// k8s secret flow
	k8sState credK8sState
	k8sMsg   string

	// status
	saved bool
	err   error
}

// NewCredentialsScreen creates the screen, loading all saved users.
func NewCredentialsScreen(org string) (*CredentialsScreen, error) {
	mgr, err := credentials.NewFromEnv()
	if err != nil {
		return nil, err
	}

	s := &CredentialsScreen{
		ctx:   context.Background(),
		mgr:   mgr,
		org:   org,
		panel: credPanelList,
	}

	s.initFields()
	s.loadUserList()

	// Pre-select first user if any
	if len(s.users) > 0 {
		s.loadUserIntoForm(s.users[0])
	}

	return s, nil
}

func (s *CredentialsScreen) initFields() {
	for i := range s.fields {
		f := textinput.New()
		f.Prompt = ""
		switch i {
		case fPAT:
			f.EchoMode = textinput.EchoPassword
			f.EchoCharacter = '•'
		case fAppID:
			f.Placeholder = "e.g. 123456"
		case fPEM, fPGP:
			f.CharLimit = 0
		case fNamespace:
			f.Placeholder = "apger"
		}
		s.fields[i] = f
	}
	s.fields[fNamespace].SetValue("apger")

	dp := textinput.New()
	dp.Placeholder = "(empty if no passphrase)"
	dp.EchoMode = textinput.EchoPassword
	dp.EchoCharacter = '•'
	s.destroyPass = dp
}

// loadUserList reads all users from the credential manager.
// The manager stores a list; if it stores a single entry we wrap it.
func (s *CredentialsScreen) loadUserList() {
	all, err := s.mgr.LoadAll()
	if err != nil {
		// fallback: try single-user load
		c, _ := s.mgr.Load()
		if c.Name != "" || c.Email != "" {
			s.users = []credentials.Credentials{c}
		}
		return
	}
	s.users = all
}

func (s *CredentialsScreen) loadUserIntoForm(c credentials.Credentials) {
	s.fields[fName].SetValue(c.Name)
	s.fields[fEmail].SetValue(c.Email)
	s.fields[fGitHubOrg].SetValue(c.GitHubOrg)
	s.fields[fPAT].SetValue(c.PAT)
	if c.GitHubAppID != 0 {
		s.fields[fAppID].SetValue(fmt.Sprintf("%d", c.GitHubAppID))
	} else {
		s.fields[fAppID].SetValue("")
	}
	s.pemSet = c.GitHubPEM != ""
	if s.pemSet {
		s.fields[fPEM].SetValue("[PEM loaded — paste to replace]")
	} else {
		s.fields[fPEM].SetValue("")
	}
	s.pgpSet = c.PGPPrivateKey != ""
	if s.pgpSet {
		s.fields[fPGP].SetValue("[PGP loaded — paste to replace]")
	} else {
		s.fields[fPGP].SetValue("")
	}

	if c.GitHubAppID != 0 && c.GitHubPEM != "" {
		s.authMode = credAuthPEM
	} else {
		s.authMode = credAuthPAT
	}
	s.saved = false
	s.err = nil
}

func (s *CredentialsScreen) clearForm() {
	for i := range s.fields {
		s.fields[i].SetValue("")
	}
	s.fields[fNamespace].SetValue("apger")
	s.authMode = credAuthPAT
	s.pgpSet = false
	s.pemSet = false
	s.saved = false
	s.err = nil
}

// WithContext sets the context.
func (s *CredentialsScreen) WithContext(ctx context.Context) *CredentialsScreen {
	s.ctx = ctx
	return s
}

// Init implements tea.Model.
func (s *CredentialsScreen) Init() tea.Cmd { return textinput.Blink }

// ── Update ────────────────────────────────────────────────────────────────────

func (s *CredentialsScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return s.handleKey(msg)
	case credSavedMsg:
		s.saved = true
		s.err = nil
		s.loadUserList()
		return s, nil
	case credErrMsg:
		s.err = msg.err
		s.saved = false
		return s, nil
	case destroyDoneMsg:
		s.destroy = credDestroyDone
		s.destroyMsg = msg.msg
		s.pgpSet = false
		s.loadUserList()
		return s, nil
	case k8sSecretDoneMsg:
		s.k8sState = credK8sDone
		s.k8sMsg = msg.msg
		return s, nil
	}

	var cmd tea.Cmd
	if s.destroy == credDestroyAskPass {
		s.destroyPass, cmd = s.destroyPass.Update(msg)
	} else if s.panel == credPanelForm {
		s.fields[s.fieldIdx], cmd = s.fields[s.fieldIdx].Update(msg)
	}
	return s, cmd
}

func (s *CredentialsScreen) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// ── destroy flow ──────────────────────────────────────────────────────────
	switch s.destroy {
	case credDestroyAskPass:
		switch key {
		case "enter":
			return s, s.runDestroy()
		case "esc":
			s.destroy = credDestroyIdle
		}
		return s, nil
	case credDestroyWorking:
		return s, nil
	case credDestroyDone:
		s.destroy = credDestroyIdle
		s.destroyMsg = ""
		return s, nil
	}

	// ── k8s done — dismiss ────────────────────────────────────────────────────
	if s.k8sState == credK8sDone {
		s.k8sState = credK8sIdle
		s.k8sMsg = ""
		return s, nil
	}

	// ── global ────────────────────────────────────────────────────────────────
	switch key {
	case "tab", "shift+tab":
		s.switchPanel()
		return s, nil
	case "ctrl+s":
		return s, s.save()
	case "ctrl+k":
		if s.k8sState != credK8sWorking {
			s.k8sState = credK8sWorking
			return s, s.pushK8sSecret()
		}
	case "ctrl+d":
		if s.pgpSet {
			s.destroy = credDestroyAskPass
			s.destroyPass.SetValue("")
			s.destroyPass.Focus()
		}
		return s, nil
	}

	// ── panel-specific ────────────────────────────────────────────────────────
	if s.panel == credPanelList {
		return s.handleListKey(key)
	}
	return s.handleFormKey(key)
}

func (s *CredentialsScreen) switchPanel() {
	if s.panel == credPanelList {
		s.panel = credPanelForm
		s.fields[s.fieldIdx].Focus()
	} else {
		s.panel = credPanelList
		s.fields[s.fieldIdx].Blur()
	}
}

func (s *CredentialsScreen) handleListKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if s.listIdx > 0 {
			s.listIdx--
			s.loadUserIntoForm(s.users[s.listIdx])
		}
	case "down", "j":
		if s.listIdx < len(s.users)-1 {
			s.listIdx++
			s.loadUserIntoForm(s.users[s.listIdx])
		}
	case "enter":
		if s.listIdx < len(s.users) {
			s.loadUserIntoForm(s.users[s.listIdx])
			s.panel = credPanelForm
			s.fields[s.fieldIdx].Focus()
		}
	case "n":
		s.clearForm()
		s.panel = credPanelForm
		s.fieldIdx = fName
		s.fields[fName].Focus()
	case "delete", "d":
		if s.listIdx < len(s.users) {
			email := s.users[s.listIdx].Email
			_ = s.mgr.DeleteUser(email)
			s.loadUserList()
			if s.listIdx >= len(s.users) && s.listIdx > 0 {
				s.listIdx--
			}
			if len(s.users) > 0 {
				s.loadUserIntoForm(s.users[s.listIdx])
			} else {
				s.clearForm()
			}
		}
	}
	return s, nil
}

func (s *CredentialsScreen) handleFormKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "down":
		s.fields[s.fieldIdx].Blur()
		s.fieldIdx = s.nextField(s.fieldIdx + 1)
		s.fields[s.fieldIdx].Focus()
	case "up":
		s.fields[s.fieldIdx].Blur()
		s.fieldIdx = s.nextField(s.fieldIdx - 1)
		s.fields[s.fieldIdx].Focus()
	case "r":
		// Toggle PAT ↔ PEM
		s.fields[s.fieldIdx].Blur()
		if s.authMode == credAuthPAT {
			s.authMode = credAuthPEM
		} else {
			s.authMode = credAuthPAT
		}
		s.fieldIdx = fName
		s.fields[fName].Focus()
	}
	return s, nil
}

func (s *CredentialsScreen) nextField(idx int) int {
	n := fCount
	for i := 0; i < n; i++ {
		c := ((idx + i) % n + n) % n
		if s.fieldVisible(c) {
			return c
		}
	}
	return fName
}

func (s *CredentialsScreen) fieldVisible(i int) bool {
	switch i {
	case fPAT:
		return s.authMode == credAuthPAT
	case fAppID, fPEM:
		return s.authMode == credAuthPEM
	default:
		return true
	}
}

// ── View ──────────────────────────────────────────────────────────────────────

func (s *CredentialsScreen) View() string {
	// overlays
	if s.destroy == credDestroyAskPass || s.destroy == credDestroyWorking || s.destroy == credDestroyDone {
		return s.viewDestroyOverlay()
	}
	if s.k8sState == credK8sWorking {
		return styleTitle.Render("  Credentials") + "\n\n" + styleDim.Render("  Pushing Kubernetes secret...")
	}
	if s.k8sState == credK8sDone {
		prefix := styleOK
		if strings.HasPrefix(s.k8sMsg, "✗") {
			prefix = styleError
		}
		return styleTitle.Render("  Credentials") + "\n\n" +
			prefix.Render("  "+s.k8sMsg) + "\n\n" +
			styleHelp.Render("  any key to continue")
	}

	var b strings.Builder
	b.WriteString(styleTitle.Render("  Credentials") + "\n\n")

	// two-panel layout
	leftW := 28
	rightW := 52

	left := s.viewList(leftW)
	right := s.viewForm(rightW)

	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
		styleBorder.Width(leftW).Render(left),
		styleBorder.Width(rightW).Render(right),
	))
	b.WriteString("\n")

	// status
	if s.saved {
		b.WriteString(styleOK.Render("  ✓ Saved") + "\n")
	}
	if s.err != nil {
		b.WriteString(styleError.Render("  ✗ "+s.err.Error()) + "\n")
	}

	// help
	help := "tab panels  ↑/↓ navigate  n new user  d delete  r auth mode  ctrl+s save  ctrl+k K8s secret"
	if s.pgpSet {
		help += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("ctrl+d destroy-pgp")
	}
	b.WriteString(styleHelp.Render("  "+help) + "\n")
	return b.String()
}

func (s *CredentialsScreen) viewList(width int) string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(" Users") + "\n")
	if len(s.users) == 0 {
		b.WriteString(styleDim.Render(" (none — press n to add)") + "\n")
		return b.String()
	}
	for i, u := range s.users {
		name := u.Name
		if name == "" {
			name = u.Email
		}
		if len(name) > width-4 {
			name = name[:width-4]
		}
		authTag := styleDim.Render(" PAT")
		if u.GitHubAppID != 0 && u.GitHubPEM != "" {
			authTag = styleOK.Render(" App")
		}
		line := fmt.Sprintf(" %-*s%s", width-6, name, authTag)
		if i == s.listIdx {
			if s.panel == credPanelList {
				b.WriteString(styleSelected.Render("▶"+line) + "\n")
			} else {
				b.WriteString(styleDim.Render("▶"+line) + "\n")
			}
		} else {
			b.WriteString(styleNormal.Render(" "+line) + "\n")
		}
	}
	return b.String()
}

func (s *CredentialsScreen) viewForm(width int) string {
	var b strings.Builder

	// auth mode radio
	patR, pemR := "( ) PAT", "( ) PEM (GitHub App)"
	if s.authMode == credAuthPAT {
		patR = styleSelected.Render("(●) PAT")
		pemR = styleDim.Render("( ) PEM (GitHub App)")
	} else {
		patR = styleDim.Render("( ) PAT")
		pemR = styleSelected.Render("(●) PEM (GitHub App)")
	}
	b.WriteString(fmt.Sprintf(" %s  %s  %s\n\n", patR, pemR, styleHelp.Render("[r]")))

	for i, label := range formLabels {
		if !s.fieldVisible(i) {
			continue
		}
		focused := s.panel == credPanelForm && i == s.fieldIdx
		ls := styleDim
		if focused {
			ls = styleSelected
		}
		b.WriteString(ls.Render(fmt.Sprintf(" %-22s", label+":")))

		switch {
		case i == fPEM && s.pemSet && strings.HasPrefix(s.fields[i].Value(), "[PEM"):
			b.WriteString(styleOK.Render("[PEM ✓]"))
		case i == fPGP && s.pgpSet && strings.HasPrefix(s.fields[i].Value(), "[PGP"):
			b.WriteString(styleOK.Render("[PGP ✓]"))
		default:
			b.WriteString(s.fields[i].View())
		}
		b.WriteString("\n")
	}

	// auth + pgp status
	b.WriteString("\n")
	authSt := styleDim.Render(" GitHub: not configured")
	c := s.currentFormCreds()
	if c.GitHubAppID != 0 && c.GitHubPEM != "" {
		authSt = styleOK.Render(fmt.Sprintf(" GitHub: App #%d", c.GitHubAppID))
	} else if c.PAT != "" {
		authSt = styleNormal.Render(" GitHub: PAT")
	}
	pgpSt := styleDim.Render("  PGP: —")
	if s.pgpSet {
		pgpSt = styleOK.Render("  PGP: ✓")
	}
	b.WriteString(authSt + pgpSt + "\n")

	return b.String()
}

func (s *CredentialsScreen) viewDestroyOverlay() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("  Credentials") + "\n\n")
	switch s.destroy {
	case credDestroyAskPass:
		b.WriteString(styleError.Render("  ⚠  SELF-DESTROY PGP KEY") + "\n\n")
		b.WriteString(styleDim.Render("  1. Generate revocation certificate\n"))
		b.WriteString(styleDim.Render("  2. Upload to NurOS-Packages/.pgp-revocations/\n"))
		b.WriteString(styleDim.Render("  3. Delete private key from local storage\n\n"))
		b.WriteString(styleNormal.Render("  PGP passphrase (empty if none):\n"))
		b.WriteString("  " + s.destroyPass.View() + "\n\n")
		b.WriteString(styleHelp.Render("  enter confirm  esc cancel"))
	case credDestroyWorking:
		b.WriteString(styleDim.Render("  Revoking..."))
	case credDestroyDone:
		st := styleOK
		if strings.HasPrefix(s.destroyMsg, "✗") {
			st = styleError
		}
		b.WriteString(st.Render("  "+s.destroyMsg) + "\n")
		b.WriteString(styleHelp.Render("  any key to continue"))
	}
	return b.String()
}

// ── helpers ───────────────────────────────────────────────────────────────────

// currentFormCreds builds a Credentials from the current form values (not saved yet).
func (s *CredentialsScreen) currentFormCreds() credentials.Credentials {
	c := credentials.Credentials{
		Name:      s.fields[fName].Value(),
		Email:     s.fields[fEmail].Value(),
		GitHubOrg: s.fields[fGitHubOrg].Value(),
	}
	switch s.authMode {
	case credAuthPAT:
		c.PAT = s.fields[fPAT].Value()
	case credAuthPEM:
		fmt.Sscanf(s.fields[fAppID].Value(), "%d", &c.GitHubAppID)
		pem := s.fields[fPEM].Value()
		if !strings.HasPrefix(pem, "[PEM") {
			c.GitHubPEM = pem
		} else if s.listIdx < len(s.users) {
			c.GitHubPEM = s.users[s.listIdx].GitHubPEM // keep existing
		}
	}
	return c
}

// ── tea.Cmd implementations ───────────────────────────────────────────────────

type credSavedMsg struct{}
type credErrMsg struct{ err error }
type destroyDoneMsg struct{ msg string }
type k8sSecretDoneMsg struct{ msg string }

func (s *CredentialsScreen) save() tea.Cmd {
	// snapshot form values before goroutine
	c := credentials.Credentials{
		Name:      s.fields[fName].Value(),
		Email:     s.fields[fEmail].Value(),
		GitHubOrg: s.fields[fGitHubOrg].Value(),
	}
	switch s.authMode {
	case credAuthPAT:
		c.PAT = s.fields[fPAT].Value()
	case credAuthPEM:
		fmt.Sscanf(s.fields[fAppID].Value(), "%d", &c.GitHubAppID)
		pem := s.fields[fPEM].Value()
		if pem != "" && !strings.HasPrefix(pem, "[PEM") {
			c.GitHubPEM = pem
		} else if s.listIdx < len(s.users) {
			c.GitHubPEM = s.users[s.listIdx].GitHubPEM
		}
	}
	pgpVal := s.fields[fPGP].Value()
	if pgpVal != "" && !strings.HasPrefix(pgpVal, "[PGP") {
		c.PGPPrivateKey = pgpVal
	} else if s.listIdx < len(s.users) {
		c.PGPPrivateKey = s.users[s.listIdx].PGPPrivateKey
	}

	ctx := s.ctx

	return func() tea.Msg {
		// Validate credentials before saving
		if err := credentials.ValidateCredentials(ctx, c); err != nil {
			return credErrMsg{err}
		}
		if err := s.mgr.SaveUser(c); err != nil {
			return credErrMsg{err}
		}
		return credSavedMsg{}
	}
}

func (s *CredentialsScreen) pushK8sSecret() tea.Cmd {
	namespace := strings.TrimSpace(s.fields[fNamespace].Value())
	if namespace == "" {
		namespace = "apger"
	}

	var c credentials.Credentials
	if s.listIdx < len(s.users) {
		c = s.users[s.listIdx]
	} else {
		c = s.currentFormCreds()
	}
	authMode := s.authMode

	return func() tea.Msg {
		args := []string{
			"create", "secret", "generic", "apger-credentials",
			"--namespace=" + namespace,
			"--save-config",
			"--dry-run=client",
			"-o", "yaml",
			"--from-literal=name=" + c.Name,
			"--from-literal=email=" + c.Email,
			"--from-literal=pgp_private_key=" + c.PGPPrivateKey,
		}
		switch authMode {
		case credAuthPAT:
			if c.PAT == "" {
				return k8sSecretDoneMsg{"✗ PAT is empty — save first"}
			}
			args = append(args,
				"--from-literal=github_app_id=0",
				"--from-literal=github_pem=",
				"--from-literal=pat="+c.PAT,
			)
		case credAuthPEM:
			if c.GitHubAppID == 0 || c.GitHubPEM == "" {
				return k8sSecretDoneMsg{"✗ App ID or PEM empty — save first"}
			}
			args = append(args,
				fmt.Sprintf("--from-literal=github_app_id=%d", c.GitHubAppID),
				"--from-literal=github_pem="+c.GitHubPEM,
				"--from-literal=pat=",
			)
		}

		yaml, err := exec.Command("kubectl", args...).Output()
		if err != nil {
			return k8sSecretDoneMsg{"✗ kubectl dry-run: " + err.Error()}
		}
		out, err := func() ([]byte, error) {
			cmd := exec.Command("kubectl", "apply", "-f", "-", "--namespace="+namespace)
			cmd.Stdin = strings.NewReader(string(yaml))
			return cmd.CombinedOutput()
		}()
		if err != nil {
			return k8sSecretDoneMsg{"✗ kubectl apply: " + strings.TrimSpace(string(out))}
		}
		return k8sSecretDoneMsg{fmt.Sprintf("✓ Secret apger-credentials → namespace %q", namespace)}
	}
}

func (s *CredentialsScreen) runDestroy() tea.Cmd {
	s.destroy = credDestroyWorking
	passphrase := s.destroyPass.Value()
	var c credentials.Credentials
	if s.listIdx < len(s.users) {
		c = s.users[s.listIdx]
	}
	org := s.org

	return func() tea.Msg {
		if c.PGPPrivateKey == "" {
			return destroyDoneMsg{"✗ No PGP key stored"}
		}
		keyHasPass := pgp.HasPassphrase(c.PGPPrivateKey)
		if keyHasPass && passphrase == "" {
			return destroyDoneMsg{"✗ Key is passphrase-protected — enter passphrase"}
		}
		if !keyHasPass && passphrase != "" {
			return destroyDoneMsg{"✗ Key has no passphrase — leave empty"}
		}
		revCert, err := pgp.GenerateRevocationCert(c.PGPPrivateKey, passphrase)
		if err != nil {
			return destroyDoneMsg{"✗ Revocation cert: " + err.Error()}
		}
		pub := publisher.New(c, org)
		id := c.Email
		if id == "" {
			id = c.Name
		}
		_ = pub.UploadRevocationCert(context.Background(), id, revCert)

		c.PGPPrivateKey = ""
		if err := s.mgr.SaveUser(c); err != nil {
			return destroyDoneMsg{"✗ Save after clear: " + err.Error()}
		}
		return destroyDoneMsg{"✓ PGP key revoked and destroyed"}
	}
}
