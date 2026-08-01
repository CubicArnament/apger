package pkgbuild

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const inspectScript = `
set -eo pipefail
source "$1" >/dev/null
emit() { printf '%s\0%s\0' "$1" "$2"; }
emit_array() {
  local key="$1"
  shift
  local value
  for value in "$@"; do emit "$key" "$value"; done
}
emit_array pkgname "${pkgname[@]}"
emit pkgver "${pkgver:-}"
emit pkgrel "${pkgrel:-}"
emit pkgdesc "${pkgdesc:-}"
emit url "${url:-}"
emit install "${install:-}"
emit_array arch "${arch[@]}"
emit_array license "${license[@]}"
emit_array depends "${depends[@]}"
emit_array conflicts "${conflicts[@]}"
emit_array provides "${provides[@]}"
emit_array replaces "${replaces[@]}"
emit_array backup "${backup[@]}"
emit_array source "${source[@]}"
emit_array sha256sums "${sha256sums[@]}"
for function_name in prepare build check package; do
  if declare -F "$function_name" >/dev/null; then emit function "$function_name"; fi
done
`

type Inspector struct {
	Shell string
}

func (i Inspector) Inspect(ctx context.Context, path string) (Recipe, error) {
	shell := i.Shell
	if shell == "" {
		shell = "bash"
	}
	command := exec.CommandContext(ctx, shell, "--noprofile", "--norc", "-c", inspectScript, "apger-inspect", path)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return Recipe{}, fmt.Errorf("evaluate PKGBUILD metadata: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	fields, err := parseRecords(stdout.Bytes())
	if err != nil {
		return Recipe{}, err
	}
	names := fields["pkgname"]
	if len(names) != 1 {
		return Recipe{}, fmt.Errorf("exactly one pkgname is supported; split packages are not implemented")
	}
	recipe := Recipe{
		Name: names[0], Version: first(fields["pkgver"]), Release: first(fields["pkgrel"]),
		Description: first(fields["pkgdesc"]), URL: first(fields["url"]), Install: first(fields["install"]),
		Architectures: fields["arch"], Licenses: fields["license"], Dependencies: fields["depends"],
		Conflicts: fields["conflicts"], Provides: fields["provides"], Replaces: fields["replaces"],
		Backup: fields["backup"], Sources: fields["source"], SHA256Sums: fields["sha256sums"],
		Functions: make(map[string]bool),
	}
	for _, function := range fields["function"] {
		recipe.Functions[function] = true
	}
	if err := recipe.Validate(); err != nil {
		return Recipe{}, err
	}
	return recipe, nil
}

func parseRecords(data []byte) (map[string][]string, error) {
	parts := bytes.Split(data, []byte{0})
	if len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	}
	if len(parts)%2 != 0 {
		return nil, fmt.Errorf("invalid metadata stream from PKGBUILD")
	}
	fields := make(map[string][]string)
	for index := 0; index < len(parts); index += 2 {
		key, value := string(parts[index]), string(parts[index+1])
		if value != "" {
			fields[key] = append(fields[key], value)
		}
	}
	return fields, nil
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
