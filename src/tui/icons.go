package tui

import "github.com/NurOS-Linux/apger/src/metadata"

// templateIcons maps build template names to Nerd Font icons.
// Codepoints are from Nerd Fonts v3 (https://www.nerdfonts.com/cheat-sheet).
// The terminal running apger must use a Nerd Font (e.g. MesloLGS NF, JetBrainsMono NF).
var templateIcons = map[string]string{
	"meson":         "\uf0ad ",  // nf-fa-wrench          U+F0AD
	"cmake":         "\ue615 ",  // nf-seti-cmake         U+E615
	"autotools":     "\uf085 ",  // nf-fa-cogs            U+F085
	"cargo":         "\ue7a8 ",  // nf-dev-rust           U+E7A8
	"python-pep517": "\ue73c ",  // nf-dev-python         U+E73C
	"gradle":        "\ue738 ",  // nf-dev-java           U+E738
	"go":            "\ue724 ",  // nf-dev-go             U+E724
	"npm":           "\ue71e ",  // nf-dev-nodejs_small   U+E71E
	"custom":        "\uf013 ",  // nf-fa-gear            U+F013
	"default":       "\uf1b2 ",  // nf-fa-cube            U+F1B2
}

// bootstrapIcon is shown for packages with bootstrap=true (libc, gcc, binutils).
// U+F0E7 = nf-fa-bolt — marks special pre-toolchain packages.
const bootstrapIcon = "\uf0e7 "

// IconForRecipe returns the Nerd Font icon for a recipe based on its build template.
// Bootstrap packages always get the bootstrap icon regardless of template.
func IconForRecipe(r metadata.Recipe) string {
	if r.Package.Bootstrap {
		return bootstrapIcon
	}
	if icon, ok := templateIcons[r.Build.Template]; ok {
		return icon
	}
	return templateIcons["default"]
}
