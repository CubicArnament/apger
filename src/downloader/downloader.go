// Package downloader fetches package sources.
// Tarballs are downloaded via aria2c subprocess (parallel, resume support).
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

	"github.com/NurOS-Linux/apger/src/metadata"
)

// Progress carries download progress for the UI progress bar.
type Progress struct {
	Total      int64  // bytes, 0 if unknown
	Downloaded int64  // bytes received so far
	SpeedBps   int64  // bytes/sec
	Done       bool
	Err        error
}

// Download fetches the source described by src into destDir.
// Progress updates are sent to the progress channel (non-blocking).
// The channel is closed when the download finishes or fails.
func Download(ctx context.Context, src metadata.RecipeSource, destDir string, progress chan<- Progress) error {
	defer close(progress)

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", destDir, err)
	}

	switch src.TypeSrc {
	case "git-repo":
		return downloadGit(ctx, src, destDir, progress)
	case "tarball", "":
		return downloadTarball(ctx, src.URL, destDir, progress)
	default:
		return fmt.Errorf("unknown type_src %q: use tarball or git-repo", src.TypeSrc)
	}
}

// ── tarball via aria2c ────────────────────────────────────────────────────────

// aria2ProgressRe matches aria2c console output lines like:
//
//	[#1 16MiB/64MiB(25%) CN:1 DL:4.2MiB ETA:11s]
var aria2ProgressRe = regexp.MustCompile(`(\d+)MiB/(\d+)MiB\((\d+)%\).*DL:([\d.]+)(\w+)`)

func downloadTarball(ctx context.Context, url, destDir string, progress chan<- Progress) error {
	outFile := filepath.Join(destDir, filepath.Base(url))

	cmd := exec.CommandContext(ctx, "aria2c",
		"--dir="+destDir,
		"--out="+filepath.Base(url),
		"--console-readout-interval=500",
		"--summary-interval=0",
		"--show-console-readout=true",
		"--file-allocation=none",
		"-x4", "-s4", // 4 connections
		url,
	)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("aria2c pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("aria2c stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("aria2c not found (install aria2): %w", err)
	}

	// Parse progress from stdout
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			line := sc.Text()
			if m := aria2ProgressRe.FindStringSubmatch(line); m != nil {
				dl, _ := strconv.ParseFloat(m[1], 64)
				total, _ := strconv.ParseFloat(m[2], 64)
				speed, _ := strconv.ParseFloat(m[4], 64)
				unit := strings.ToLower(m[5])
				mult := map[string]float64{"kib": 1024, "mib": 1024 * 1024, "gib": 1024 * 1024 * 1024}[unit]
				if mult == 0 {
					mult = 1
				}
				sendProgress(progress, Progress{
					Total:      int64(total * 1024 * 1024),
					Downloaded: int64(dl * 1024 * 1024),
					SpeedBps:   int64(speed * mult),
				})
			}
		}
	}()
	// Drain stderr silently
	go func() { bufio.NewScanner(stderr).Scan() }()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("aria2c download %s: %w", url, err)
	}

	sendProgress(progress, Progress{Done: true})
	_ = outFile
	return nil
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
