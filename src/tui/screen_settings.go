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
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	core "github.com/NurOS-Linux/apger/src/core"
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
	// path input state (shown when Local is selected)
	pathInput  string
	pathFocus  bool
	suggestion string // fish-style ghost completion
	LocalPath  string // confirmed local output path
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
		detail: "Keep packages in a local directory, skip all remote publishing",
	},
}

// NewSettingsScreen creates a settings screen with the given initial targets.
func NewSettingsScreen(targets PublishTarget) *SettingsScreen {
	if targets == 0 {
		targets = PublishGitHubReleases
	}
	return &SettingsScreen{targets: targets}
}

// Targets returns the currently selected publish targets.
func (s *SettingsScreen) Targets() PublishTarget { return s.targets }

func (s *SettingsScreen) Init() tea.Cmd { return nil }

func (s *SettingsScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
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
			if s.cursor > 0 {
				s.cursor--
			}
		case "down", "j":
			if s.cursor < len(settingsItems)-1 {
				s.cursor++
			}
		case " ", "enter":
			bit := settingsItems[s.cursor].bit
			if s.targets&bit != 0 {
				s.targets &^= bit
			} else {
				s.targets |= bit
				if bit == PublishLocal {
					s.targets = PublishLocal
					// Focus path input
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

		// Show path input below Local option when selected
		if item.bit == PublishLocal && s.targets&PublishLocal != 0 {
			b.WriteString("\n")
			b.WriteString(stylePathLabel.Render("  Output path: "))

			// Render user text (white) + suggestion suffix (grey)
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
