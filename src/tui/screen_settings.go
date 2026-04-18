package tui

// Settings screen — publish target selection.
//
// Options:
//   [x] GitHub Packages  — push .apg as OCI artifact to ghcr.io/NurOS-Packages/<pkg>
//   [ ] NurOS-Packages   — create/update repo in NurOS-Packages org + upload .apg
//   [ ] Local only       — keep in PVC, no remote publish
//
// Navigation: ↑/↓, space to toggle, ctrl+s to save, esc to go back.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	core "github.com/NurOS-Linux/apger/src/core"
)

// PublishTarget is an alias for core.PublishTarget.
type PublishTarget = core.PublishTarget

const (
	PublishGitHubReleases = core.PublishGitHubReleases
	PublishNurOSOrg       = core.PublishNurOSOrg
	PublishLocal          = core.PublishLocal
)

// SettingsScreen manages publish target selection.
type SettingsScreen struct {
	cursor  int
	targets PublishTarget
	saved   bool
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
		detail: "Keep packages in PVC output only, skip all remote publishing",
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
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
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
				// Local is mutually exclusive with remote targets
				if bit == PublishLocal {
					s.targets = PublishLocal
				} else {
					s.targets &^= PublishLocal
				}
			}
		case "ctrl+s":
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
	}

	b.WriteString("\n")
	if s.saved {
		b.WriteString(styleOK.Render("  ✓ Settings saved") + "\n")
	}
	b.WriteString(styleHelp.Render("  ↑/↓ navigate  space toggle  ctrl+s save  esc back"))
	return b.String()
}
