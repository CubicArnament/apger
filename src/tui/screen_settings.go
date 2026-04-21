package tui

// Settings screen — publish target selection.
//
// Options:
//   [x] GitHub Releases  — upload .apg as release assets
//   [ ] NurOS-Packages   — commit .apg into org repo
//   [ ] Local only       — keep in a local path (shows path input with fish-style completion)
//
// Navigation: ↑/↓, space to toggle, ctrl+s to save, esc to go back.

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	core "github.com/NurOS-Linux/apger/src/core"
	"github.com/NurOS-Linux/apger/src/settings"
)

// PublishTarget is an alias for core.PublishTarget.
type PublishTarget = core.PublishTarget

const (
	PublishGitHubReleases = core.PublishGitHubReleases
	PublishNurOSOrg       = core.PublishNurOSOrg
	PublishLocal          = core.PublishLocal
)

var (
	stylePathInput      = lipgloss.NewStyle().Foreground(lipgloss.Color("255")) // white — user text
	stylePathSuggestion = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // grey — suggestion
	stylePathLabel      = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))
	stylePathError      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

// SettingsScreen manages publish target selection.
type SettingsScreen struct {
	cursor     int
	targets    PublishTarget
	saved      bool
	pathInput  string
	pathFocus  bool
	suggestion string
	LocalPath  string
	sortCursor int
	SortMode   settings.SortMode
	// NFS status
	nfsServer string
	nfsUp     *bool
}

type nfsCheckMsg bool

var sortModes = []struct {
	mode    settings.SortMode
	label   string
	example string
}{
	{settings.SortNone,   "No sorting",          "output-pkgs/pkg.apg"},
	{settings.SortByType, "Sort by type",         "output-pkgs/extra/pkg.apg"},
	{settings.SortByArch, "Sort by architecture", "output-pkgs/x86_64/pkg.apg"},
	{settings.SortByBoth, "Sort by arch + type",  "output-pkgs/x86_64/extra/pkg.apg"},
}

type settingsItem struct {
	label  string
	bit    PublishTarget
	detail string
}

var settingsItems = []settingsItem{
	{
		label:  "GitHub Releases",
		bit:    PublishGitHubReleases,
		detail: "Upload .apg + .sig as release assets to NurOS-Packages/<pkg> v<version>",
	},
	{
		label:  "NurOS-Packages org (file commit)",
		bit:    PublishNurOSOrg,
		detail: "Commit .apg into packages/<version>/ in the org repo",
	},
	{
		label:  "Local only (no publish)",
		bit:    PublishLocal,
		detail: "Copy packages to a local path on your machine after build (via kubectl cp)",
	},
}

// NewSettingsScreen creates a settings screen, loading saved settings from NFS.
func NewSettingsScreen(targets PublishTarget) *SettingsScreen {
	s := settings.Load()
	t := PublishTarget(s.PublishTargets)
	if t == 0 {
		t = PublishGitHubReleases
	}
	return &SettingsScreen{
		targets:   t,
		SortMode:  s.SortMode,
		LocalPath: s.LocalPath,
		pathInput: s.LocalPath,
	}
}

// Targets returns the currently selected publish targets.
func (s *SettingsScreen) Targets() PublishTarget { return s.targets }

func (s *SettingsScreen) Init() tea.Cmd {
	return s.checkNFS()
}

func (s *SettingsScreen) checkNFS() tea.Cmd {
	addr := s.nfsServer
	if addr == "" {
		addr = os.Getenv("NFS_SERVER")
	}
	if addr == "" {
		return nil
	}
	// Ensure addr has port — if no colon, default to 2049
	if !strings.Contains(addr, ":") {
		addr = addr + ":2049"
	}
	return func() tea.Msg {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			return nfsCheckMsg(false)
		}
		conn.Close()
		return nfsCheckMsg(true)
	}
}

func (s *SettingsScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case nfsCheckMsg:
		up := bool(msg)
		s.nfsUp = &up
		// Re-check every 10s
		return s, tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
			return s.checkNFS()()
		})
	case tea.KeyMsg:
		return s.handleKey(msg)
	}
	return s, nil
}

func (s *SettingsScreen) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
		// Path input is focused — handle typing
		if s.pathFocus {
			switch key.String() {
			case "esc":
				s.pathFocus = false
			case "enter":
				if s.suggestion != "" && s.pathInput == "" {
					s.pathInput = s.suggestion
				} else if s.suggestion != "" {
					s.pathInput = s.suggestion
				}
				s.LocalPath = s.pathInput
				s.pathFocus = false
			case "tab":
				// Accept suggestion
				if s.suggestion != "" {
					s.pathInput = s.suggestion
					s.suggestion = completePath(s.pathInput)
				}
			case "backspace":
				if len(s.pathInput) > 0 {
					s.pathInput = s.pathInput[:len(s.pathInput)-1]
					s.suggestion = completePath(s.pathInput)
				}
			case "ctrl+s":
				s.LocalPath = s.pathInput
				s.saved = true
				s.pathFocus = false
			default:
				ch := key.String()
				if len(ch) == 1 {
					s.pathInput += ch
					s.suggestion = completePath(s.pathInput)
				}
			}
			return s, nil
		}

		// Normal navigation
		switch key.String() {
		case "up", "k":
			if s.targets&PublishLocal != 0 && s.sortCursor > 0 {
				s.sortCursor--
			} else if s.cursor > 0 {
				s.cursor--
			}
		case "down", "j":
			if s.targets&PublishLocal != 0 && s.sortCursor < len(sortModes)-1 {
				s.sortCursor++
			} else if s.cursor < len(settingsItems)-1 {
				s.cursor++
			}
		case " ":
			if s.targets&PublishLocal != 0 {
				// Space on sort radio — select mode
				s.SortMode = sortModes[s.sortCursor].mode
			} else {
				bit := settingsItems[s.cursor].bit
				if s.targets&bit != 0 {
					s.targets &^= bit
				} else {
					s.targets |= bit
					if bit == PublishLocal {
						s.targets = PublishLocal
						s.pathFocus = true
						s.suggestion = completePath(s.pathInput)
					} else {
						s.targets &^= PublishLocal
					}
				}
			}
		case "enter":
			bit := settingsItems[s.cursor].bit
			if s.targets&bit != 0 {
				s.targets &^= bit
			} else {
				s.targets |= bit
				if bit == PublishLocal {
					s.targets = PublishLocal
					s.pathFocus = true
					s.suggestion = completePath(s.pathInput)
				} else {
					s.targets &^= PublishLocal
				}
			}
		case "ctrl+s":
			if s.targets&PublishLocal != 0 {
				s.LocalPath = s.pathInput
			}
			_ = settings.Save(settings.Settings{
				PublishTargets: int(s.targets),
				LocalPath:      s.LocalPath,
				SortMode:       s.SortMode,
			})
			s.saved = true
		}
	}
	return s, nil
}

func (s *SettingsScreen) View() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("  Publish Settings") + "\n\n")
	b.WriteString(styleDim.Render("  Select where to publish built packages:") + "\n\n")

	for i, item := range settingsItems {
		checked := "[ ]"
		if s.targets&item.bit != 0 {
			checked = styleOK.Render("[✓]")
		}
		line := fmt.Sprintf("  %s %s", checked, item.label)
		if i == s.cursor {
			b.WriteString(styleSelected.Render(line) + "\n")
			b.WriteString(styleDim.Render("       "+item.detail) + "\n")
		} else {
			b.WriteString(styleNormal.Render(line) + "\n")
		}

		// Show path input + sort radio below Local option when selected
		if item.bit == PublishLocal && s.targets&PublishLocal != 0 {
			b.WriteString("\n")

			// NFS status indicator
			nfsLine := ""
			server := s.nfsServer
			if server == "" {
				server = os.Getenv("NFS_SERVER")
			}
			if server == "" {
				nfsLine = styleDim.Render("  NFS: server not configured (set NFS_SERVER env or apger.conf)")
			} else if s.nfsUp == nil {
				nfsLine = styleDim.Render("  NFS: checking " + server + "...")
			} else if *s.nfsUp {
				nfsLine = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Render("  ● Server UP  " + server)
			} else {
				nfsLine = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("  ● Server DOWN  " + server)
			}
			b.WriteString(nfsLine + "\n\n")
			b.WriteString(stylePathLabel.Render("  Output path: "))

			userText := stylePathInput.Render(s.pathInput)
			ghost := ""
			if s.suggestion != "" && strings.HasPrefix(s.suggestion, s.pathInput) && s.suggestion != s.pathInput {
				ghost = stylePathSuggestion.Render(s.suggestion[len(s.pathInput):])
			}
			cursor := ""
			if s.pathFocus {
				cursor = stylePathInput.Render("█")
			}
			b.WriteString(userText + ghost + cursor + "\n")

			if s.pathFocus {
				b.WriteString(styleDim.Render("  tab accept  enter confirm  esc cancel") + "\n")
			} else if s.LocalPath != "" {
				b.WriteString(styleOK.Render("  ✓ "+s.LocalPath) + "\n")
			}

			// Sort mode radio buttons
			b.WriteString("\n")
			b.WriteString(stylePathLabel.Render("  Package sorting:") + "\n")
			for j, sm := range sortModes {
				radio := "( )"
				if s.SortMode == sm.mode {
					radio = styleOK.Render("(●)")
				}
				line := fmt.Sprintf("  %s %s", radio, sm.label)
				if j == s.sortCursor && !s.pathFocus {
					b.WriteString(styleSelected.Render(line))
				} else {
					b.WriteString(styleNormal.Render(line))
				}
				b.WriteString(styleDim.Render("  → "+sm.example) + "\n")
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if s.saved {
		b.WriteString(styleOK.Render("  ✓ Settings saved") + "\n")
	}
	b.WriteString(styleHelp.Render("  ↑/↓ navigate  space toggle  ctrl+s save  esc back"))
	return b.String()
}

// completePath returns the best filesystem completion for the given prefix.
// Returns the full completed path or empty string if no unique match.
func completePath(prefix string) string {
	if prefix == "" {
		home, _ := os.UserHomeDir()
		return home
	}

	// Expand ~ to home
	expanded := prefix
	if strings.HasPrefix(prefix, "~/") {
		home, _ := os.UserHomeDir()
		expanded = filepath.Join(home, prefix[2:])
	} else if prefix == "~" {
		home, _ := os.UserHomeDir()
		return home
	}

	// If prefix ends with separator, list inside that dir
	dir := expanded
	base := ""
	if !strings.HasSuffix(expanded, string(os.PathSeparator)) {
		dir = filepath.Dir(expanded)
		base = filepath.Base(expanded)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var matches []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), base) {
			matches = append(matches, filepath.Join(dir, e.Name()))
		}
	}

	if len(matches) == 1 {
		result := matches[0]
		// Restore ~ prefix if original had it
		if strings.HasPrefix(prefix, "~") {
			home, _ := os.UserHomeDir()
			result = "~" + strings.TrimPrefix(result, home)
		}
		return result
	}
	// Multiple matches — find longest common prefix
	if len(matches) > 1 {
		common := matches[0]
		for _, m := range matches[1:] {
			common = longestCommonPrefix(common, m)
		}
		if common != expanded {
			if strings.HasPrefix(prefix, "~") {
				home, _ := os.UserHomeDir()
				common = "~" + strings.TrimPrefix(common, home)
			}
			return common
		}
	}
	return ""
}

func longestCommonPrefix(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return a[:i]
}
