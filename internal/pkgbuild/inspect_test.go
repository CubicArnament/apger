package pkgbuild

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInspectorReadsPKGBUILD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PKGBUILD")
	content := `
pkgname=hello
pkgver=1.2.3
pkgrel=4
pkgdesc='test package'
url='https://nuros.org'
arch=('x86_64' 'aarch64')
license=('MIT')
depends=('glibc>=2.39' 'zlib')
source=('hello.tar.zst::https://example.invalid/source.tar.zst')
sha256sums=('SKIP')
prepare() { :; }
build() { :; }
package() { :; }
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	recipe, err := (Inspector{}).Inspect(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if recipe.Name != "hello" || recipe.Version != "1.2.3" || recipe.Release != "4" {
		t.Fatalf("unexpected identity: %#v", recipe)
	}
	if !recipe.Functions["prepare"] || !recipe.Functions["build"] || !recipe.Functions["package"] {
		t.Fatalf("functions not detected: %#v", recipe.Functions)
	}
	if got, want := CleanDependencies(recipe.Dependencies), []string{"glibc", "zlib"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dependencies = %#v, want %#v", got, want)
	}
}

func TestInspectorRejectsSplitPackages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PKGBUILD")
	content := "pkgname=(one two)\npkgver=1\npkgrel=1\narch=(any)\nsource=()\nsha256sums=()\npackage() { :; }\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Inspector{}).Inspect(context.Background(), path); err == nil {
		t.Fatal("expected split-package error")
	}
}
