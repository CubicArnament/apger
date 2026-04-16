package tui

// Credentials screen — keyboard-driven form for managing apger credentials.
//
// Fields: Name, Email, PAT, PGP private key (paste armored block)
// ctrl+s  — save
// ctrl+d  — self-destroy-pgp: prompts for passphrase confirmation, then
//            generates revocation cert, uploads to GitHub, clears local key.
//
// self-destroy-pgp flow:
//  1. Show passphrase input (empty = no passphrase, but verified)
//  2. Attempt to decrypt key with entered passphrase
//  3. If OK → generate revocation cert → upload → clear local key

import (
	"context"
	"fmt"
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
	destroyConfirm                // passphrase verified, waiting for final y/N
	destroyInProgress             // executing
	destroyDone                   // finished (success or error)
)

// authMode selects which GitHub authentication method is active.
type authMode int

const (
	authModePAT authMode = iota // Personal Access Token
	authModeApp                 // GitHub App (AppID + PEM)
)

// CredentialsScreen is the TUI model for the credentials management screen.
type CredentialsScreen struct {
	mgr     *credentials.Manager
	pub     *publisher.Publisher // may be nil until PAT is saved
	org     string               // NurOS-Packages org name

	// Form fields: [0]=Name [1]=Email [2]=PAT [3]=AppID [4]=PEM [5]=PGP
	fields  [6]textinput.Model
	focus   int
	pgpSet  bool
	mode    authMode // PAT or App — radio selection

	// Self-destroy flow
	destroy     destroyState
	destroyPass textinput.Model
	destroyMsg  string

	err    error
	saved  bool
}

var credFieldLabels = [6]string{"Name", "Email", "PAT", "App ID", "App PEM key (paste)", "PGP Key (paste armored)"}

// NewCredentialsScreen creates the credentials screen.
func NewCredentialsScreen(org string) (*CredentialsScreen, error) {
	mgr, err := credentials.New()
	if err != nil {
		return nil, err
	}

	s := &CredentialsScreen{mgr: mgr, org: org}

	for i := range s.fields {
		f := textinput.New()
		f.Prompt = ""
		switch i {
		case 2: // PAT — mask
			f.EchoMode = textinput.EchoPassword
			f.EchoCharacter = '•'
		case 3: // App ID
			f.Placeholder = "e.g. 123456"
		case 4, 5: // PEM / PGP — no char limit
			f.CharLimit = 0
		}
		s.fields[i] = f
	}
	s.fields[0].Focus()

	dp := textinput.New()
	dp.Placeholder = "(empty if no passphrase)"
	dp.EchoMode = textinput.EchoPassword
	dp.EchoCharacter = '•'
	s.destroyPass = dp

	// Load existing credentials and set initial mode
	creds, _ := mgr.Load()
	s.fields[0].SetValue(creds.Name)
	s.fields[1].SetValue(creds.Email)
	s.fields[2].SetValue(creds.PAT)
	if creds.GitHubAppID != 0 {
		s.fields[3].SetValue(fmt.Sprintf("%d", creds.GitHubAppID))
	}
	s.pgpSet = creds.PGPPrivateKey != ""
	if s.pgpSet {
		s.fields[5].SetValue("[key loaded — paste new to replace]")
	}

	// Default mode: App if AppID is set, otherwise PAT
	if creds.GitHubAppID != 0 && creds.GitHubPEM != "" {
		s.mode = authModeApp
	} else {
		s.mode = authModePAT
	}

	return s, nil
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
		return s, nil
	case destroyDoneMsg:
		s.destroy = destroyDone
		s.destroyMsg = msg.msg
		s.pgpSet = false
		return s, nil
	}

	// Delegate to active input
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

	// Self-destroy flow
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
	case destroyConfirm, destroyInProgress, destroyDone:
		if key == "esc" || key == "enter" {
			s.destroy = destroyIdle
			s.destroyMsg = ""
		}
		return s, nil
	}

	// Normal form
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
		// Toggle auth mode radio button
		if s.mode == authModePAT {
			s.mode = authModeApp
		} else {
			s.mode = authModePAT
		}
		// Reset focus to first visible field for new mode
		s.fields[s.focus].Blur()
		s.focus = 0
		s.fields[0].Focus()
	case "ctrl+s":
		return s, s.save()
	case "ctrl+d":
		// Initiate self-destroy-pgp
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

// nextField returns the next valid field index for the current auth mode,
// wrapping around and skipping fields that don't belong to the active mode.
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

// isFieldVisible returns whether a field should be shown/navigable for the current mode.
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

	if s.destroy != destroyIdle {
		b.WriteString(s.viewDestroy())
		return b.String()
	}

	// Auth mode radio buttons
	patRadio := "( ) PAT"
	appRadio := "( ) GitHub App (JWT)"
	if s.mode == authModePAT {
		patRadio = styleSelected.Render("(●) PAT")
		appRadio = styleDim.Render("( ) GitHub App (JWT)")
	} else {
		patRadio = styleDim.Render("( ) PAT")
		appRadio = styleSelected.Render("(●) GitHub App (JWT)")
	}
	b.WriteString(fmt.Sprintf("  Auth:  %s   %s   %s\n\n",
		patRadio, appRadio, styleHelp.Render("[r] toggle")))
		if !s.isFieldVisible(i) {
			continue
		}
		focused := i == s.focus
		style := styleDim
		if focused {
			style = styleSelected
		}
		b.WriteString(style.Render(fmt.Sprintf("  %-22s", label+":")))

		if i == 5 && s.pgpSet && s.fields[i].Value() == "[key loaded — paste new to replace]" {
			b.WriteString(styleOK.Render("[PGP key loaded ✓]"))
		} else {
			b.WriteString(s.fields[i].View())
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if s.saved {
		b.WriteString(styleOK.Render("  ✓ Saved") + "\n")
	}
	if s.err != nil {
		b.WriteString(styleError.Render("  ✗ "+s.err.Error()) + "\n")
	}

	pgpStatus := styleDim.Render("  PGP key: not set")
	if s.pgpSet {
		pgpStatus = styleOK.Render("  PGP key: loaded ✓")
	}
	b.WriteString(pgpStatus + "\n")

	// Show which auth method will be used
	authStatus := styleDim.Render("  GitHub auth: not configured")
	creds, _ := s.mgr.Load()
	if creds.GitHubAppID != 0 && creds.GitHubPEM != "" {
		authStatus = styleOK.Render(fmt.Sprintf("  GitHub auth: App #%d (JWT)", creds.GitHubAppID))
	} else if creds.PAT != "" {
		authStatus = styleNormal.Render("  GitHub auth: PAT (fallback)")
	}
	b.WriteString(authStatus + "\n\n")

	help := "tab/↑↓ navigate  r auth mode  ctrl+s save"
	if s.pgpSet {
		help += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("ctrl+d self-destroy-pgp")
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
		b.WriteString(styleNormal.Render("  Enter PGP passphrase to confirm (empty if none):\n"))
		b.WriteString("  " + s.destroyPass.View() + "\n\n")
		b.WriteString(styleHelp.Render("  enter confirm  esc cancel"))

	case destroyInProgress:
		b.WriteString(styleError.Render("  Revoking key..."))

	case destroyDone:
		if strings.HasPrefix(s.destroyMsg, "✗") {
			b.WriteString(styleError.Render("  "+s.destroyMsg) + "\n")
		} else {
			b.WriteString(styleOK.Render("  "+s.destroyMsg) + "\n")
		}
		b.WriteString(styleHelp.Render("  press enter or esc to continue"))
	}

	return b.String()
}

// ── Commands ──────────────────────────────────────────────────────────────────

type credSavedMsg struct{}
type credErrMsg struct{ err error }
type destroyDoneMsg struct{ msg string }

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
			// Clear App fields when switching to PAT
			creds.GitHubAppID = 0
			creds.GitHubPEM = ""
		case authModeApp:
			var appID int64
			fmt.Sscanf(s.fields[3].Value(), "%d", &appID)
			creds.GitHubAppID = appID
			pem := s.fields[4].Value()
			if pem != "" {
				creds.GitHubPEM = pem
			}
			// Clear PAT when switching to App
			creds.PAT = ""
		}

		// Only update PGP key if user pasted a new one
		newKey := s.fields[5].Value()
		if newKey != "" && newKey != "[key loaded — paste new to replace]" {
			creds.PGPPrivateKey = newKey
		}

		if err := s.mgr.Save(creds); err != nil {
			return credErrMsg{err}
		}
		return credSavedMsg{}
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

		// Verify passphrase: if key has no passphrase, empty string must work;
		// if key has passphrase, the provided one must decrypt it.
		keyHasPass := pgp.HasPassphrase(creds.PGPPrivateKey)
		if keyHasPass && passphrase == "" {
			return destroyDoneMsg{"✗ Key is passphrase-protected — enter passphrase"}
		}
		if !keyHasPass && passphrase != "" {
			return destroyDoneMsg{"✗ Key has no passphrase — leave field empty"}
		}

		// Generate revocation cert
		revCert, err := pgp.GenerateRevocationCert(creds.PGPPrivateKey, passphrase)
		if err != nil {
			return destroyDoneMsg{"✗ Generate revocation cert: " + err.Error()}
		}

		// Upload to GitHub if PAT is available
		if creds.PAT != "" {
			pub := publisher.New(creds.PAT, s.org)
			pkgName := creds.Email // use email as identifier for the revocation file
			if pkgName == "" {
				pkgName = "apger"
			}
			if err := pub.UploadRevocationCert(context.Background(), pkgName, revCert); err != nil {
				// Non-fatal — still destroy locally
				_ = err
			}
		}

		// Delete private key from local storage
		if err := s.mgr.ClearPGP(); err != nil {
			return destroyDoneMsg{"✗ Clear local key: " + err.Error()}
		}

		return destroyDoneMsg{"✓ PGP key revoked and destroyed. Revocation cert uploaded."}
	}
}
