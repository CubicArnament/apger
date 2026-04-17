// Package downloader fetches package sources.
// Tarballs are downloaded via aria2c subprocess (parallel segments, resume support).
// Git repositories are cloned via go-git.
package downloader

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	config "github.com/NurOS-Linux/apger/src/core"
	"github.com/NurOS-Linux/apger/src/metadata"
)

// Progress carries download progress for the UI progress bar.
type Progress struct {
	Total      int64  // bytes, 0 if unknown
	Downloaded int64  // bytes received so far
	SpeedBps   int64  // bytes/sec
	ETA        string // human-readable ETA from aria2c
	Done       bool
	Err        error
}

// Download fetches the source described by src into destDir using aria2 settings from cfg.
// Progress updates are sent to the progress channel (non-blocking, buffered).
// The channel is closed when the download finishes or fails.
func Download(ctx context.Context, src metadata.RecipeSource, destDir string, cfg config.Aria2Config, progress chan<- Progress) error {
	defer close(progress)

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", destDir, err)
	}

	switch src.TypeSrc {
	case "git-repo":
		return downloadGit(ctx, src, destDir, progress)
	case "tarball", "":
		return downloadTarball(ctx, src.URL, destDir, cfg, progress)
	default:
		return fmt.Errorf("unknown type_src %q: use tarball or git-repo", src.TypeSrc)
	}
}

// ── tarball via aria2c ────────────────────────────────────────────────────────

// aria2ProgressRe matches aria2c summary lines:
//
//	[#abc123 16MiB/64MiB(25%) CN:4 DL:4.2MiB ETA:11s]
var aria2ProgressRe = regexp.MustCompile(
	`(\d+(?:\.\d+)?)([KMG]i?B)/(\d+(?:\.\d+)?)([KMG]i?B)\((\d+)%\).*DL:([\d.]+)([KMG]i?B)(?:.*ETA:(\S+))?`,
)

func downloadTarball(ctx context.Context, url, destDir string, cfg config.Aria2Config, progress chan<- Progress) error {
	connections := cfg.Connections
	if connections <= 0 {
		connections = 4
	}
	splits := cfg.Splits
	if splits <= 0 {
		splits = connections
	}
	minSplitSize := cfg.MinSplitSize
	if minSplitSize == "" {
		minSplitSize = "1M"
	}
	maxTries := cfg.MaxTries
	if maxTries <= 0 {
		maxTries = 5
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 60
	}

	args := []string{
		"--dir=" + destDir,
		"--out=" + filepath.Base(url),
		fmt.Sprintf("--max-connection-per-server=%d", connections),
		fmt.Sprintf("--split=%d", splits),
		"--min-split-size=" + minSplitSize,
		fmt.Sprintf("--max-tries=%d", maxTries),
		fmt.Sprintf("--connect-timeout=%d", timeout),
		"--console-readout-interval=500",
		"--summary-interval=0",
		"--show-console-readout=true",
		"--file-allocation=none",
		"--auto-file-renaming=false",
		"--allow-overwrite=true",
	}

	if cfg.UserAgent != "" {
		args = append(args, "--user-agent="+cfg.UserAgent)
	}
	if cfg.ProxyURL != "" {
		args = append(args, "--all-proxy="+cfg.ProxyURL)
	}
	if cfg.ContinueDownload {
		args = append(args, "--continue=true")
	}

	args = append(args, url)

	cmd := exec.CommandContext(ctx, "aria2c", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("aria2c stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("aria2c stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("aria2c not found — install aria2: %w", err)
	}

	// Parse progress from stdout
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			line := sc.Text()
			m := aria2ProgressRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			dl := parseSize(m[1], m[2])
			total := parseSize(m[3], m[4])
			speed := parseSize(m[6], m[7])
			eta := m[8]
			sendProgress(progress, Progress{
				Total:      total,
				Downloaded: dl,
				SpeedBps:   speed,
				ETA:        eta,
			})
		}
	}()
	// Drain stderr silently (aria2c writes verbose info there)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
		}
	}()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("aria2c download %s: %w", url, err)
	}

	sendProgress(progress, Progress{Done: true})
	return nil
}

// parseSize converts aria2c size string like "4.2", "MiB" → bytes as int64.
func parseSize(val, unit string) int64 {
	v, _ := strconv.ParseFloat(val, 64)
	mult := map[string]float64{
		"B": 1, "KB": 1e3, "MB": 1e6, "GB": 1e9,
		"KiB": 1024, "MiB": 1024 * 1024, "GiB": 1024 * 1024 * 1024,
	}[unit]
	if mult == 0 {
		mult = 1
	}
	return int64(v * mult)
}

// ── git via go-git ────────────────────────────────────────────────────────────

type gitProgress struct {
	ch chan<- Progress
}

func (g *gitProgress) Write(p []byte) (int, error) {
	line := strings.TrimSpace(string(p))
	if line != "" {
		sendProgress(g.ch, Progress{Downloaded: -1}) // pulse for UI
	}
	return len(p), nil
}

func downloadGit(ctx context.Context, src metadata.RecipeSource, destDir string, progress chan<- Progress) error {
	cloneOpts := &git.CloneOptions{
		URL:      src.URL,
		Depth:    1,
		Progress: &gitProgress{ch: progress},
	}

	if src.IncludeSubmodules {
		cloneOpts.RecurseSubmodules = git.DefaultSubmoduleRecursionDepth
	}

	// Support branch/tag via URL fragment: https://github.com/org/repo#v1.2.3
	if idx := strings.Index(src.URL, "#"); idx != -1 {
		cloneOpts.URL = src.URL[:idx]
		ref := src.URL[idx+1:]
		cloneOpts.ReferenceName = plumbing.NewTagReferenceName(ref)
		cloneOpts.Depth = 0 // full clone needed for tag checkout
	}

	_, err := git.PlainCloneContext(ctx, destDir, false, cloneOpts)
	if err != nil && err != git.ErrRepositoryAlreadyExists {
		return fmt.Errorf("git clone %s: %w", src.URL, err)
	}

	sendProgress(progress, Progress{Done: true})
	return nil
}

func sendProgress(ch chan<- Progress, p Progress) {
	select {
	case ch <- p:
	default:
	}
}
