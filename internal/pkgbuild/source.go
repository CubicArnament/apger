package pkgbuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const MaxSourceBytes int64 = 2 << 30

type SourceManager struct {
	Client *http.Client
	Stdout io.Writer
	Stderr io.Writer
}

func (m SourceManager) Fetch(ctx context.Context, recipe Recipe, sourceDir, cacheDir string) error {
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		return fmt.Errorf("create source directory: %w", err)
	}
	if cacheDir != "" {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return fmt.Errorf("create source cache: %w", err)
		}
	}
	for index, raw := range recipe.Sources {
		name, location := splitSource(raw)
		if isGitSource(location) {
			if recipe.SHA256Sums[index] != "SKIP" {
				return fmt.Errorf("git source %q must use SKIP checksum", raw)
			}
			if err := m.clone(ctx, name, location, sourceDir); err != nil {
				return err
			}
			continue
		}
		if !isRemote(location) {
			return fmt.Errorf("local source %q is unavailable; ACP must provide auxiliary source files", raw)
		}
		if name == "" {
			name = remoteFilename(location)
		}
		if name == "" || filepath.Base(name) != name {
			return fmt.Errorf("unsafe source filename %q", name)
		}
		cached := filepath.Join(cacheDir, cacheFilename(location, name, recipe.SHA256Sums[index]))
		if cacheDir == "" {
			cached = filepath.Join(sourceDir, name)
		}
		if err := m.download(ctx, location, cached); err != nil {
			return err
		}
		if err := verifySHA256(cached, recipe.SHA256Sums[index]); err != nil {
			_ = os.Remove(cached)
			return fmt.Errorf("verify %s: %w", name, err)
		}
		destination := filepath.Join(sourceDir, name)
		if cached != destination {
			if err := copyFile(cached, destination); err != nil {
				return fmt.Errorf("copy cached source: %w", err)
			}
		}
		if isArchive(name) {
			command := exec.CommandContext(ctx, "bsdtar", "-xf", destination, "-C", sourceDir)
			command.Stdout, command.Stderr = m.Stdout, m.Stderr
			if err := command.Run(); err != nil {
				return fmt.Errorf("extract %s: %w", name, err)
			}
		}
	}
	return nil
}

func (m SourceManager) download(ctx context.Context, location, destination string) error {
	if info, err := os.Lstat(destination); err == nil {
		if info.Mode().IsRegular() && info.Size() > 0 {
			return nil
		}
		if err := os.Remove(destination); err != nil {
			return fmt.Errorf("remove invalid cache entry: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	client := m.Client
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return fmt.Errorf("create source request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download %s: %w", location, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("download %s: unexpected HTTP status %s", location, response.Status)
	}
	if response.ContentLength > MaxSourceBytes {
		return fmt.Errorf("download %s: source exceeds %d bytes", location, MaxSourceBytes)
	}
	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".apger-download-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, MaxSourceBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > MaxSourceBytes {
		return fmt.Errorf("download %s: source exceeds %d bytes", location, MaxSourceBytes)
	}
	return os.Rename(temporary, destination)
}

func (m SourceManager) clone(ctx context.Context, name, location, sourceDir string) error {
	repository, refType, ref := parseGitSource(location)
	if name == "" {
		name = strings.TrimSuffix(remoteFilename(repository), ".git")
	}
	if name == "" || filepath.Base(name) != name {
		return fmt.Errorf("unsafe git source name %q", name)
	}
	destination := filepath.Join(sourceDir, name)
	args := []string{"clone", "--depth=1", repository, destination}
	if refType == "branch" || refType == "tag" {
		args = []string{"clone", "--depth=1", "--branch", ref, repository, destination}
	}
	command := exec.CommandContext(ctx, "git", args...)
	command.Stdout, command.Stderr = m.Stdout, m.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("clone %s: %w", repository, err)
	}
	if refType == "commit" {
		fetch := exec.CommandContext(ctx, "git", "-C", destination, "fetch", "--depth=1", "origin", ref)
		fetch.Stdout, fetch.Stderr = m.Stdout, m.Stderr
		if err := fetch.Run(); err != nil {
			return fmt.Errorf("fetch commit %s: %w", ref, err)
		}
		checkout := exec.CommandContext(ctx, "git", "-C", destination, "checkout", "--detach", ref)
		checkout.Stdout, checkout.Stderr = m.Stdout, m.Stderr
		if err := checkout.Run(); err != nil {
			return fmt.Errorf("checkout commit %s: %w", ref, err)
		}
	}
	return nil
}

func splitSource(raw string) (string, string) {
	if parts := strings.SplitN(raw, "::", 2); len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", raw
}

func isRemote(location string) bool {
	return strings.HasPrefix(location, "https://") || strings.HasPrefix(location, "http://")
}

func isGitSource(location string) bool {
	return strings.HasPrefix(location, "git+")
}

func parseGitSource(location string) (repository, refType, ref string) {
	repository = strings.TrimPrefix(location, "git+")
	if index := strings.Index(repository, "#"); index >= 0 {
		fragment := repository[index+1:]
		repository = repository[:index]
		if parts := strings.SplitN(fragment, "=", 2); len(parts) == 2 {
			refType, ref = parts[0], parts[1]
		}
	}
	return
}

func remoteFilename(location string) string {
	parsed, err := url.Parse(location)
	if err != nil {
		return ""
	}
	return filepath.Base(parsed.Path)
}

func cacheFilename(location, name, checksum string) string {
	digest := sha256.Sum256([]byte(location + "\x00" + name + "\x00" + strings.ToLower(checksum)))
	return hex.EncodeToString(digest[:])
}

func verifySHA256(path, expected string) error {
	if expected == "SKIP" {
		return nil
	}
	if len(expected) != sha256.Size*2 {
		return fmt.Errorf("invalid sha256 checksum")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

func isArchive(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range []string{".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz", ".tar.zst", ".zip"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
