// Package tui provides a keyboard-driven terminal UI for apger.
// No mouse support. Navigation: ↑/↓/j/k, enter, space, esc, q.
package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/NurOS-Linux/apger/src/builder"
	config "github.com/NurOS-Linux/apger/src/core"
	"github.com/NurOS-Linux/apger/src/metadata"
	"github.com/NurOS-Linux/apger/src/storage"
)

// ── Screens ───────────────────────────────────────────────────────────────────

type screen int

const (
	screenDashboard screen = iota
	screenFM
	screenEditor
	screenBuild
	screenCredentials
	screenSettings
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	styleBorder  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62"))
	styleTitle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).Padding(0, 1)
	styleSelected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	styleNormal  = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleError   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	styleHelp    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	styleOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	styleBar     = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))

	// TOML syntax highlight styles
	tomlKey    = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	tomlStr    = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	tomlNum    = lipgloss.NewStyle().Foreground(lipgloss.Color("221"))
	tomlHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
)

// ── ModelConfig ───────────────────────────────────────────────────────────────

// ModelConfig holds runtime configuration for the TUI.
type ModelConfig struct {
	RepodataDir string
	RecipeDir   string
	OutputDir   string
	Kubeconfig  string
	PVCName     string
	Image       string
	DBPath      string
}

// ── Package item ──────────────────────────────────────────────────────────────

type pkgItem struct {
	path    string
	recipe  metadata.Recipe
	built   bool
	selected bool
}

func (p pkgItem) display(cursor bool) string {
	icon := IconForRecipe(p.recipe)
	name := p.recipe.Package.Name
	ver := p.recipe.Package.Version
	check := " "
	if p.selected {
		check = "✓"
	}
	status := styleDim.Render("○")
	if p.built {
		status = styleOK.Render("●")
	}
	line := fmt.Sprintf(" %s %s %s %s  %s", check, icon, name, styleDim.Render(ver), status)
	if cursor {
		return styleSelected.Render(line)
	}
	return styleNormal.Render(line)
}

// ── FM state ──────────────────────────────────────────────────────────────────

type fmState struct {
	groups  map[string][]pkgItem // subdir → items
	dirs    []string             // sorted dir names
	dirIdx  int                  // current dir
	itemIdx int                  // current item in dir
	panel   int                  // 0=dirs, 1=items
}

func (f *fmState) currentItems() []pkgItem {
	if len(f.dirs) == 0 {
		return nil
	}
	return f.groups[f.dirs[f.dirIdx]]
}

func (f *fmState) toggleCurrent() {
	items := f.currentItems()
	if f.itemIdx < len(items) {
		items[f.itemIdx].selected = !items[f.itemIdx].selected
		f.groups[f.dirs[f.dirIdx]] = items
	}
}

func (f *fmState) selectAllInDir() {
	items := f.currentItems()
	allSelected := true
	for _, it := range items {
		if !it.selected {
			allSelected = false
			break
		}
	}
	for i := range items {
		items[i].selected = !allSelected
	}
	f.groups[f.dirs[f.dirIdx]] = items
}

func (f *fmState) selectedPaths() []string {
	var paths []string
	for _, dir := range f.dirs {
		for _, it := range f.groups[dir] {
			if it.selected {
				paths = append(paths, it.path)
			}
		}
	}
	return paths
}

// ── Build log message ─────────────────────────────────────────────────────────

type buildLogMsg string
type buildDoneMsg struct{ err error }
type downloadProgressMsg struct {
	pct   int
	speed string
}

// ── Model ─────────────────────────────────────────────────────────────────────

// Model is the root bubbletea model.
type Model struct {
	cfg    ModelConfig
	screen screen
	width  int
	height int

	// dashboard
	dashItems []pkgItem
	dashIdx   int

	// file manager
	fm fmState

	// editor
	editor      textarea.Model
	editorFile  string // path being edited ("" = new)
	editorSaved bool
	editorFM    fmState // mini FM on the left panel
	editorPanel int     // 0=fm, 1=editor

	// build
	buildLog  viewport.Model
	buildBuf  strings.Builder
	buildDone bool
	buildErr  error
	dlPct     int
	dlSpeed   string

	// shared
	db         *storage.DB
	orch       *builder.Orchestrator
	ctx        context.Context
	err        error
	credScreen *CredentialsScreen
	settings   *SettingsScreen
}

// ── Constructor ───────────────────────────────────────────────────────────────

// NewModel creates a new Model. Call Initialize() before Run().
func NewModel(cfg ModelConfig) *Model {
	ta := textarea.New()
	ta.Placeholder = "TOML recipe..."
	ta.ShowLineNumbers = true
	ta.SetWidth(80)
	ta.SetHeight(30)

	return &Model{
		cfg:    cfg,
		screen: screenDashboard,
		editor: ta,
	}
}

// Initialize opens the DB, creates the orchestrator, and loads packages.
func (m *Model) Initialize() error {
	var err error
	m.db, err = storage.NewDB(m.cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	m.orch, err = builder.NewOrchestrator(builder.OrchestratorConfig{
		Kubeconfig:  m.cfg.Kubeconfig,
		RepodataDir: m.cfg.RepodataDir,
		RecipeDir:   m.cfg.RecipeDir,
		OutputDir:   m.cfg.OutputDir,
		PVCName:     m.cfg.PVCName,
		Image:       m.cfg.Image,
		DBPath:      m.cfg.DBPath,
		ApgerConfig: config.FindConfig(""),
	})
	if err != nil {
		return fmt.Errorf("create orchestrator: %w", err)
	}

	return m.reloadPackages()
}

// Run starts the bubbletea event loop.
func (m *Model) Run(ctx context.Context) error {
	m.ctx = ctx
	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithoutMouseAllEvents(), // keyboard only
	)
	_, err := p.Run()
	return err
}

// ── Package loading ───────────────────────────────────────────────────────────

func (m *Model) reloadPackages() error {
	groups, err := metadata.FindRecipes(m.cfg.RepodataDir)
	if err != nil {
		return err
	}

	// Flatten for dashboard
	m.dashItems = nil
	fmGroups := make(map[string][]pkgItem)
	var dirs []string

	for subdir, paths := range groups {
		dirs = append(dirs, subdir)
		for _, path := range paths {
			r, err := metadata.LoadRecipe(path)
			if err != nil {
				continue
			}
			hash, _ := metadata.HashRecipe(r)
			built, _ := m.db.IsBuilt(r.Package.Name, hash)
			item := pkgItem{path: path, recipe: r, built: built}
			m.dashItems = append(m.dashItems, item)
			fmGroups[subdir] = append(fmGroups[subdir], item)
		}
	}

	// Sort dirs: "." first
	sortedDirs := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if d == "." {
			sortedDirs = append([]string{d}, sortedDirs...)
		} else {
			sortedDirs = append(sortedDirs, d)
		}
	}

	m.fm = fmState{groups: fmGroups, dirs: sortedDirs}
	m.editorFM = fmState{groups: fmGroups, dirs: sortedDirs}
	return nil
}

// ── bubbletea interface ───────────────────────────────────────────────────────

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.buildLog.Width = m.width - 4
		m.buildLog.Height = m.height - 8
		m.editor.SetWidth(m.width/2 - 4)
		m.editor.SetHeight(m.height - 8)
		return m, nil

	case buildLogMsg:
		m.buildBuf.WriteString(string(msg))
		m.buildLog.SetContent(m.buildBuf.String())
		m.buildLog.GotoBottom()
		return m, nil

	case buildDoneMsg:
		m.buildDone = true
		m.buildErr = msg.err
		return m, nil

	case downloadProgressMsg:
		m.dlPct = msg.pct
		m.dlSpeed = msg.speed
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global
	switch key {
	case "ctrl+c", "q":
		if m.screen != screenEditor {
			return m, tea.Quit
		}
	case "esc":
		switch m.screen {
		case screenFM, screenEditor, screenBuild, screenCredentials, screenSettings:
			m.screen = screenDashboard
			return m, nil
		}
	}

	switch m.screen {
	case screenDashboard:
		return m.updateDashboard(key)
	case screenFM:
		return m.updateFM(key)
	case screenEditor:
		return m.updateEditor(key, msg)
	case screenBuild:
		return m.updateBuild(key)
	case screenCredentials:
		if m.credScreen != nil {
			newModel, cmd := m.credScreen.Update(msg)
			if cs, ok := newModel.(*CredentialsScreen); ok {
				m.credScreen = cs
			}
			return m, cmd
		}
	case screenSettings:
		if m.settings != nil {
			newModel, cmd := m.settings.Update(msg)
			if ss, ok := newModel.(*SettingsScreen); ok {
				m.settings = ss
			}
			return m, cmd
		}
	}
	return m, nil
}

// ── Dashboard ─────────────────────────────────────────────────────────────────

func (m *Model) updateDashboard(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.dashIdx > 0 {
			m.dashIdx--
		}
	case "down", "j":
		if m.dashIdx < len(m.dashItems)-1 {
			m.dashIdx++
		}
	case "b":
		// Build selected or current
		m.screen = screenFM
	case "a":
		m.openEditor("")
	case "c":
		// Open credentials screen
		cs, err := NewCredentialsScreen("NurOS-Packages")
		if err != nil {
			m.err = err
		} else {
			m.credScreen = cs.WithContext(m.ctx)
			m.screen = screenCredentials
		}
	case "s":
		if m.settings == nil {
			m.settings = NewSettingsScreen(0)
		}
		m.screen = screenSettings
	case "enter":
		if m.dashIdx < len(m.dashItems) {
			m.openEditor(m.dashItems[m.dashIdx].path)
		}
	}
	return m, nil
}

// ── File Manager ──────────────────────────────────────────────────────────────

func (m *Model) updateFM(key string) (tea.Model, tea.Cmd) {
	f := &m.fm
	switch key {
	case "tab":
		f.panel = 1 - f.panel
	case "up", "k":
		if f.panel == 0 {
			if f.dirIdx > 0 {
				f.dirIdx--
				f.itemIdx = 0
			}
		} else {
			if f.itemIdx > 0 {
				f.itemIdx--
			}
		}
	case "down", "j":
		if f.panel == 0 {
			if f.dirIdx < len(f.dirs)-1 {
				f.dirIdx++
				f.itemIdx = 0
			}
		} else {
			items := f.currentItems()
			if f.itemIdx < len(items)-1 {
				f.itemIdx++
			}
		}
	case " ":
		if f.panel == 1 {
			f.toggleCurrent()
		}
	case "a":
		f.selectAllInDir()
	case "enter", "b":
		paths := f.selectedPaths()
		if len(paths) == 0 && f.panel == 1 {
			items := f.currentItems()
			if f.itemIdx < len(items) {
				paths = []string{items[f.itemIdx].path}
			}
		}
		if len(paths) > 0 {
			return m.startBuild(paths)
		}
	}
	return m, nil
}

func (m *Model) startBuild(paths []string) (tea.Model, tea.Cmd) {
	m.screen = screenBuild
	m.buildBuf.Reset()
	m.buildDone = false
	m.buildErr = nil
	m.buildLog = viewport.New(m.width-4, m.height-8)

	cmd := func() tea.Msg {
		for _, path := range paths {
			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			if err := m.orch.BuildPackage(m.ctx, name); err != nil {
				return buildDoneMsg{err: err}
			}
		}
		return buildDoneMsg{}
	}
	return m, cmd
}

// ── Editor ────────────────────────────────────────────────────────────────────

func (m *Model) openEditor(path string) {
	m.screen = screenEditor
	m.editorFile = path
	m.editorSaved = false
	m.editorPanel = 1

	content := metadata.RecipeTemplate()
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			content = string(data)
		}
	}
	m.editor.SetValue(content)
	m.editor.Focus()
}

func (m *Model) updateEditor(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key {
	case "tab":
		m.editorPanel = 1 - m.editorPanel
		if m.editorPanel == 1 {
			m.editor.Focus()
		} else {
			m.editor.Blur()
		}
		return m, nil
	case "ctrl+s":
		return m, m.saveEditor()
	case "ctrl+n":
		m.openEditor("")
		return m, nil
	}

	// FM panel navigation
	if m.editorPanel == 0 {
		f := &m.editorFM
		switch key {
		case "up", "k":
			if f.itemIdx > 0 {
				f.itemIdx--
			}
		case "down", "j":
			items := f.currentItems()
			if f.itemIdx < len(items)-1 {
				f.itemIdx++
			}
		case "enter":
			items := f.currentItems()
			if f.itemIdx < len(items) {
				m.openEditor(items[f.itemIdx].path)
			}
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return m, cmd
}

func (m *Model) saveEditor() tea.Cmd {
	return func() tea.Msg {
		content := m.editor.Value()
		path := m.editorFile
		if path == "" {
			// Parse name from content to generate filename
			var r metadata.Recipe
			if parseTomlString(content, &r) == nil && r.Package.Name != "" {
				path = filepath.Join(m.cfg.RepodataDir, r.Package.Name+".toml")
			} else {
				path = filepath.Join(m.cfg.RepodataDir, "new-package.toml")
			}
			m.editorFile = path
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return buildLogMsg(fmt.Sprintf("save error: %v\n", err))
		}
		m.editorSaved = true
		m.reloadPackages() //nolint:errcheck
		return buildLogMsg("")
	}
}

// ── Build screen ──────────────────────────────────────────────────────────────

func (m *Model) updateBuild(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		m.buildLog.LineUp(1)
	case "down", "j":
		m.buildLog.LineDown(1)
	}
	return m, nil
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m *Model) View() string {
	switch m.screen {
	case screenDashboard:
		return m.viewDashboard()
	case screenFM:
		return m.viewFM()
	case screenEditor:
		return m.viewEditor()
	case screenBuild:
		return m.viewBuild()
	case screenCredentials:
		if m.credScreen != nil {
			return m.credScreen.View()
		}
	case screenSettings:
		if m.settings != nil {
			return m.settings.View()
		}
	}
	return ""
}

func (m *Model) viewDashboard() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("  APGer — NurOS Package Builder") + "\n\n")

	for i, item := range m.dashItems {
		b.WriteString(item.display(i == m.dashIdx) + "\n")
	}

	if m.err != nil {
		b.WriteString("\n" + styleError.Render(m.err.Error()) + "\n")
	}

	b.WriteString("\n" + styleHelp.Render("↑/↓ navigate  b build  a add new  enter edit  c credentials  s settings  q quit"))
	return b.String()
}

func (m *Model) viewFM() string {
	f := &m.fm
	w := m.width
	if w == 0 {
		w = 80
	}
	half := w/2 - 2

	// Left: directories
	var left strings.Builder
	left.WriteString(styleTitle.Render(" Directories") + "\n")
	for i, d := range f.dirs {
		line := " " + d
		if i == f.dirIdx {
			if f.panel == 0 {
				line = styleSelected.Render("▶ " + d)
			} else {
				line = styleDim.Render("▶ " + d)
			}
		}
		left.WriteString(line + "\n")
	}

	// Right: items in current dir
	var right strings.Builder
	right.WriteString(styleTitle.Render(" Packages") + "\n")
	for i, item := range f.currentItems() {
		cursor := f.panel == 1 && i == f.itemIdx
		right.WriteString(item.display(cursor) + "\n")
	}

	leftBox := styleBorder.Width(half).Render(left.String())
	rightBox := styleBorder.Width(half).Render(right.String())

	help := styleHelp.Render("tab switch panels  ↑/↓ navigate  space select  a select all  enter/b build  esc back")
	return lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox) + "\n" + help
}

func (m *Model) viewEditor() string {
	w := m.width
	if w == 0 {
		w = 80
	}
	half := w/2 - 2

	// Left: mini FM
	var left strings.Builder
	left.WriteString(styleTitle.Render(" Recipes") + "\n")
	f := &m.editorFM
	for i, item := range f.currentItems() {
		cursor := m.editorPanel == 0 && i == f.itemIdx
		name := filepath.Base(item.path)
		if cursor {
			left.WriteString(styleSelected.Render("▶ "+name) + "\n")
		} else {
			left.WriteString(styleDim.Render("  "+name) + "\n")
		}
	}

	// Right: editor with TOML syntax highlight
	var right strings.Builder
	title := "New Recipe"
	if m.editorFile != "" {
		title = filepath.Base(m.editorFile)
	}
	if m.editorSaved {
		title += " " + styleOK.Render("✓ saved")
	}
	right.WriteString(styleTitle.Render(" "+title) + "\n")
	right.WriteString(highlightTOML(m.editor.Value(), m.height-6))

	leftBox := styleBorder.Width(half).Render(left.String())
	rightBox := styleBorder.Width(half).Render(right.String())

	help := styleHelp.Render("tab switch  ctrl+s save  ctrl+n new  esc back")
	return lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox) + "\n" + help
}

func (m *Model) viewBuild() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("  Build Log") + "\n")

	// Download progress bar
	if m.dlPct > 0 && !m.buildDone {
		b.WriteString(renderProgressBar(m.dlPct, m.dlSpeed, m.width-4) + "\n")
	}

	b.WriteString(m.buildLog.View() + "\n")

	if m.buildDone {
		if m.buildErr != nil {
			b.WriteString(styleError.Render("✗ Build failed: "+m.buildErr.Error()) + "\n")
		} else {
			b.WriteString(styleOK.Render("✓ Build completed") + "\n")
		}
	}

	b.WriteString(styleHelp.Render("↑/↓ scroll  esc back"))
	return b.String()
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// renderProgressBar renders a download progress bar.
// pct=0..100, speed is a human-readable string.
func renderProgressBar(pct int, speed string, width int) string {
	if width < 20 {
		width = 20
	}
	barWidth := width - 20
	filled := barWidth * pct / 100
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	return styleBar.Render(fmt.Sprintf("[%s] %3d%%  %s", bar, pct, speed))
}

// highlightTOML applies basic TOML syntax highlighting using lipgloss.
// Returns at most maxLines lines.
func highlightTOML(content string, maxLines int) string {
	lines := strings.Split(content, "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	var out strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "#"):
			out.WriteString(styleDim.Render(line) + "\n")
		case strings.HasPrefix(trimmed, "["):
			out.WriteString(tomlHeader.Render(line) + "\n")
		case strings.Contains(line, "="):
			parts := strings.SplitN(line, "=", 2)
			key := tomlKey.Render(parts[0])
			val := parts[1]
			// Color value by type
			trimVal := strings.TrimSpace(val)
			switch {
			case strings.HasPrefix(trimVal, `"`):
				val = tomlStr.Render(val)
			case trimVal == "true" || trimVal == "false":
				val = tomlNum.Render(val)
			case len(trimVal) > 0 && (trimVal[0] >= '0' && trimVal[0] <= '9'):
				val = tomlNum.Render(val)
			}
			out.WriteString(key + "=" + val + "\n")
		default:
			out.WriteString(styleNormal.Render(line) + "\n")
		}
	}
	return out.String()
}

// parseTomlString decodes TOML content into a Recipe.
func parseTomlString(content string, r *metadata.Recipe) error {
	return metadata.DecodeRecipeTOML(content, r)
}
