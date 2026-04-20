package metadata

import (
	"fmt"
	"path/filepath"
	"strings"
)

// APGv2 script names (standard for NurOS package format)
const (
	ScriptPreInstall  = "pre-install"
	ScriptPostInstall = "post-install"
	ScriptPreRemove   = "pre-remove"
	ScriptPostRemove  = "post-remove"
)

// ScriptTemplate represents a script generation template
type ScriptTemplate string

const (
	TemplateInit              ScriptTemplate = "init"              // neoinit service management
	TemplateUpdateAlternatives ScriptTemplate = "update-alternatives" // switch alternatives system
	TemplatePath              ScriptTemplate = "path"              // add to PATH via /etc/profile.d
	TemplateReload            ScriptTemplate = "reload"            // reload neoinit services
	TemplateLdconfig          ScriptTemplate = "ldconfig"          // update library cache
)

// GenerateScripts generates APGv2 package scripts based on templates
func GenerateScripts(recipe Recipe) map[string]string {
	scripts := make(map[string]string)
	
	// Generate pre-install
	scripts[ScriptPreInstall] = generateScript(recipe.Scripts.PreInstall, recipe.Package.Name, "pre-install")
	
	// Generate post-install
	scripts[ScriptPostInstall] = generateScript(recipe.Scripts.PostInstall, recipe.Package.Name, "post-install")
	
	// Generate pre-remove
	scripts[ScriptPreRemove] = generateScript(recipe.Scripts.PreRemove, recipe.Package.Name, "pre-remove")
	
	// Generate post-remove
	scripts[ScriptPostRemove] = generateScript(recipe.Scripts.PostRemove, recipe.Package.Name, "post-remove")
	
	// Add PATH script if requested
	if recipe.Scripts.AddToPath {
		scripts[ScriptPostInstall] += generatePathScript(recipe.Package.Name)
	}
	
	return scripts
}

func generateScript(templates []string, pkgName, phase string) string {
	if len(templates) == 0 {
		return generateEmptyScript(phase)
	}
	
	var parts []string
	parts = append(parts, "#!/usr/bin/env sh")
	parts = append(parts, fmt.Sprintf("# APGv2 %s script for %s", phase, pkgName))
	parts = append(parts, "set -e")
	parts = append(parts, "")
	
	for _, tmpl := range templates {
		parts = append(parts, expandTemplate(ScriptTemplate(tmpl), pkgName))
		parts = append(parts, "")
	}
	
	return strings.Join(parts, "\n")
}

func generateEmptyScript(phase string) string {
	return fmt.Sprintf("#!/usr/bin/env sh\n# APGv2 %s script (empty)\nexit 0\n", phase)
}

func expandTemplate(tmpl ScriptTemplate, pkgName string) string {
	switch tmpl {
	case TemplateInit:
		return fmt.Sprintf(`# Enable and start neoinit service
if [ -f /etc/neoinit/services/%s.yaml ]; then
    servctl start %s || true
fi`, pkgName, pkgName)
		
	case TemplateUpdateAlternatives:
		// NurOS uses 'switch' (similar to Gentoo's eselect)
		return fmt.Sprintf(`# Register alternatives with switch
if command -v switch >/dev/null 2>&1; then
    switch --install /usr/bin/%s %s /usr/bin/%s 50 || true
fi`, pkgName, pkgName, pkgName)
		
	case TemplatePath:
		return "" // Handled separately in generatePathScript
		
	case TemplateReload:
		return `# Reload neoinit services
if command -v servctl >/dev/null 2>&1; then
    servctl reload || true
fi`
		
	case TemplateLdconfig:
		return `# Update library cache
if command -v ldconfig >/dev/null 2>&1; then
    ldconfig || true
fi`
		
	default:
		return fmt.Sprintf("# Unknown template: %s", tmpl)
	}
}

func generatePathScript(pkgName string) string {
	// Generate /etc/profile.d script that adds package to PATH
	profileScript := fmt.Sprintf(`
# Generate /etc/profile.d script
cat > /etc/profile.d/%s.sh <<'EOF'
#!/usr/bin/env sh
# Add %s to PATH
export PATH="/usr/lib/%s/bin:$PATH"
EOF
chmod 644 /etc/profile.d/%s.sh
`, pkgName, pkgName, pkgName, pkgName)
	
	return profileScript
}

// ScriptPaths returns the standard APGv2 script paths
func ScriptPaths() []string {
	return []string{
		filepath.Join("scripts", ScriptPreInstall),
		filepath.Join("scripts", ScriptPostInstall),
		filepath.Join("scripts", ScriptPreRemove),
		filepath.Join("scripts", ScriptPostRemove),
	}
}

// GenerateNeoinitService generates neoinit YAML service configuration
func GenerateNeoinitService(recipe Recipe) string {
	if recipe.Service == nil {
		return ""
	}
	
	svc := recipe.Service
	pkgName := recipe.Package.Name
	
	// Default values
	exec := svc.Exec
	if exec == "" {
		exec = fmt.Sprintf("/usr/bin/%s", pkgName)
	}
	
	description := svc.Description
	if description == "" {
		description = recipe.Package.Description
	}
	
	workingDir := svc.WorkingDir
	if workingDir == "" {
		workingDir = "/"
	}
	
	svcType := svc.Type
	if svcType == "" {
		svcType = "simple"
	}
	
	var parts []string
	parts = append(parts, fmt.Sprintf("name: %s", pkgName))
	parts = append(parts, fmt.Sprintf("description: %s", description))
	parts = append(parts, fmt.Sprintf("exec: %s", exec))
	parts = append(parts, fmt.Sprintf("working_dir: %s", workingDir))
	parts = append(parts, fmt.Sprintf("restart: %t", svc.Restart))
	parts = append(parts, fmt.Sprintf("type: %s", svcType))
	
	// Add environment variables
	if len(svc.Env) > 0 {
		parts = append(parts, "env:")
		for k, v := range svc.Env {
			parts = append(parts, fmt.Sprintf("  - %s=%s", k, v))
		}
	}
	
	// Add user/group if specified
	if svc.User != "" {
		parts = append(parts, fmt.Sprintf("user: %s", svc.User))
	}
	if svc.Group != "" {
		parts = append(parts, fmt.Sprintf("group: %s", svc.Group))
	}
	
	return strings.Join(parts, "\n") + "\n"
}
