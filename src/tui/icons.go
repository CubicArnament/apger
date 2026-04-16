package tui

import "github.com/NurOS-Linux/apger/src/metadata"

// templateIcons maps build template names to Nerd Font icons.
// Codepoints are from Nerd Fonts v3 (https://www.nerdfonts.com/cheat-sheet).
// The terminal running apger must use a Nerd Font (e.g. MesloLGS NF, JetBrainsMono NF).
var templateIcons = map[string]string{
	"meson":         "\uf0ad ",  // nf-fa-wrench
	"cmake":         "\ue615 ",  // nf-seti-cmake
	"autotools":     "\uf085 ",  // nf-fa-cogs
	"cargo":         "\ue7a8 ",  // nf-dev-rust
	"python-pep517": "\ue73c ",  // nf-dev-python
	"gradle":        "\ue738 ",  // nf-dev-java
	"go":            "\ue724 ",  // nf-dev-go
	"npm":           "\ue71e ",  // nf-dev-nodejs_small
	"kbuild":        "\uf17c ",  // nf-fa-linux (kernel)
	"custom":        "\uf013 ",  // nf-fa-gear
}

// bootstrapIcon is shown for packages with bootstrap=true (libc, gcc, binutils).
// U+F0E7 = nf-fa-bolt — marks special pre-toolchain packages.
const bootstrapIcon = "\uf0e7 "

// IconForRecipe returns the Nerd Font icon for a recipe based on its build template.
// Bootstrap packages always get the bootstrap icon regardless of template.
// Returns empty string if the template has no icon.
func IconForRecipe(r metadata.Recipe) string {
	if r.Package.Bootstrap {
		return bootstrapIcon
	}
	return templateIcons[r.Build.Template]
}
