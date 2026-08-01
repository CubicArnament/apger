package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/NurOS-Linux/apger/internal/pkgbuild"
)

func TestWriteMetadataMapsPKGBUILD(t *testing.T) {
	directory := t.TempDir()
	recipe := pkgbuild.Recipe{
		Name: "hello", Version: "1.0", Release: "2", Description: "hello package", URL: "https://nuros.org",
		Architectures: []string{"any"}, Licenses: []string{"MIT"}, Dependencies: []string{"glibc>=2.39"},
		Backup: []string{"etc/hello.conf"},
	}
	if err := writeMetadata(directory, recipe, "NurOS Builder"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata APGMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Version != "1.0-2" || metadata.Architecture != "all" || metadata.Maintainer != "NurOS Builder" {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	if !reflect.DeepEqual(metadata.Dependencies, []string{"glibc"}) || !reflect.DeepEqual(metadata.Conf, []string{"/etc/hello.conf"}) {
		t.Fatalf("unexpected arrays: %#v", metadata)
	}
}

func TestWriteMetadataUsesArraysForEmptyRelations(t *testing.T) {
	directory := t.TempDir()
	if err := writeMetadata(directory, pkgbuild.Recipe{
		Name: "demo", Version: "1", Release: "1", Architectures: []string{"any"},
	}, "builder"); err != nil {
		t.Fatalf("writeMetadata: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"tags", "dependencies", "conflicts", "provides", "replaces", "conf"} {
		if _, ok := document[field].([]any); !ok {
			t.Fatalf("%s must be a JSON array, got %#v", field, document[field])
		}
	}
}

func TestCopyTreePreservesSymlink(t *testing.T) {
	source, destination := filepath.Join(t.TempDir(), "source"), filepath.Join(t.TempDir(), "destination")
	if err := os.MkdirAll(filepath.Join(source, "usr", "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "usr", "lib", "libhello.so.1"), []byte("lib"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("libhello.so.1", filepath.Join(source, "usr", "lib", "libhello.so")); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(source, destination); err != nil {
		t.Fatal(err)
	}
	link, err := os.Readlink(filepath.Join(destination, "usr", "lib", "libhello.so"))
	if err != nil {
		t.Fatal(err)
	}
	if link != "libhello.so.1" {
		t.Fatalf("symlink = %q", link)
	}
}
