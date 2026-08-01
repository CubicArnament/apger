package pkgbuild

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
)

func TestSourceManagerDownloadsVerifiesAndCaches(t *testing.T) {
	payload := []byte("NurOS source\n")
	checksum := fmt.Sprintf("%x", sha256.Sum256(payload))
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = response.Write(payload)
	}))
	defer server.Close()

	recipe := Recipe{Sources: []string{"source.txt::" + server.URL + "/download"}, SHA256Sums: []string{checksum}}
	manager := SourceManager{Client: server.Client()}
	cache := filepath.Join(t.TempDir(), "cache")
	for range 2 {
		sources := filepath.Join(t.TempDir(), "src")
		if err := manager.Fetch(context.Background(), recipe, sources, cache); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(sources, "source.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(payload) {
			t.Fatalf("source content = %q", got)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestVerifySHA256RejectsMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifySHA256(path, fmt.Sprintf("%064d", 0)); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

func TestSourceCacheSeparatesSameNamedSources(t *testing.T) {
	payloads := [][]byte{[]byte("first"), []byte("second")}
	servers := make([]*httptest.Server, 0, len(payloads))
	for _, payload := range payloads {
		payload := payload
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			_, _ = response.Write(payload)
		}))
		defer server.Close()
		servers = append(servers, server)
	}

	cache := filepath.Join(t.TempDir(), "cache")
	for index, payload := range payloads {
		checksum := fmt.Sprintf("%x", sha256.Sum256(payload))
		recipe := Recipe{
			Sources:    []string{"source.txt::" + servers[index].URL + "/download"},
			SHA256Sums: []string{checksum},
		}
		sourceDir := filepath.Join(t.TempDir(), "src")
		if err := (SourceManager{Client: servers[index].Client()}).Fetch(context.Background(), recipe, sourceDir, cache); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(sourceDir, "source.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(payload) {
			t.Fatalf("source %d content = %q", index, got)
		}
	}
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(payloads) {
		t.Fatalf("cache entries = %d, want %d", len(entries), len(payloads))
	}
}

func TestSourceManagerRemovesChecksumMismatchFromCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("unexpected"))
	}))
	defer server.Close()
	cache := filepath.Join(t.TempDir(), "cache")
	recipe := Recipe{
		Sources:    []string{"source.txt::" + server.URL + "/download"},
		SHA256Sums: []string{fmt.Sprintf("%064d", 0)},
	}
	if err := (SourceManager{Client: server.Client()}).Fetch(context.Background(), recipe, t.TempDir(), cache); err == nil {
		t.Fatal("expected checksum mismatch")
	}
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid source remained in cache: %#v", entries)
	}
}

func TestSourceManagerRejectsOversizedSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Length", strconv.FormatInt(MaxSourceBytes+1, 10))
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	recipe := Recipe{
		Sources:    []string{"source.bin::" + server.URL + "/download"},
		SHA256Sums: []string{"SKIP"},
	}
	if err := (SourceManager{Client: server.Client()}).Fetch(context.Background(), recipe, t.TempDir(), ""); err == nil {
		t.Fatal("expected oversized source error")
	}
}
