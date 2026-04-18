// Package builder provides package building functionality.
package builder

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	ar "github.com/CiscoSecurityServices/go-libarchive"
)

// Downloader handles downloading source code from various sources.
type Downloader struct {
	client *http.Client
}

// NewDownloader creates a new source downloader.
func NewDownloader() *Downloader {
	return &Downloader{
		client: &http.Client{
			Timeout: 30 * time.Minute,
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},
	}
}

// DownloadSource downloads source from URL to destination directory.
// Supports git, tar.gz, tar.xz, tar.bz2, and zip.
func (d *Downloader) DownloadSource(sourceURL, destDir string, progress func(downloaded, total int64)) error {
	if strings.HasSuffix(sourceURL, ".git") {
		return d.downloadGit(sourceURL, destDir)
	}
	return d.downloadArchive(sourceURL, destDir, progress)
}

func (d *Downloader) downloadGit(gitURL, destDir string) error {
	cmd := exec.Command("git", "clone", "--depth", "1", gitURL, destDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (d *Downloader) downloadArchive(url, destDir string, progress func(downloaded, total int64)) error {
	tmpFile, err := os.CreateTemp("", "apger-download-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %d", url, resp.StatusCode)
	}

	var writer io.Writer = tmpFile
	if progress != nil && resp.ContentLength > 0 {
		writer = &progressWriter{writer: tmpFile, progress: progress, total: resp.ContentLength}
	}

	if _, err := io.Copy(writer, resp.Body); err != nil {
		return fmt.Errorf("save download: %w", err)
	}

	if _, err := tmpFile.Seek(0, 0); err != nil {
		return fmt.Errorf("seek temp file: %w", err)
	}

	return extractArchive(tmpFile, destDir)
}

// progressWriter wraps an io.Writer to report progress.
type progressWriter struct {
	writer     io.Writer
	progress   func(downloaded, total int64)
	total      int64
	downloaded int64
}

func (w *progressWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.downloaded += int64(n)
	if w.progress != nil {
		w.progress(w.downloaded, w.total)
	}
	return n, err
}

func extractArchive(reader io.Reader, destDir string) error {
	archiveReader, err := ar.NewReader(reader)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer archiveReader.Close()

	for {
		entry, err := archiveReader.Next()
		if err == io.EOF {
			break
		}
		if errors.Is(err, ar.ErrArchiveWarn) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read archive entry: %w", err)
		}

		target := filepath.Join(destDir, filepath.Clean("/"+entry.PathName()))
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			continue
		}

		stat := entry.Stat()
		if stat.IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("mkdir %s: %w", target, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("mkdir parent %s: %w", target, err)
		}

		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, stat.Mode())
		if err != nil {
			return fmt.Errorf("create file %s: %w", target, err)
		}

		if _, err := io.Copy(f, io.LimitReader(archiveReader, 2<<30)); err != nil {
			f.Close()
			return fmt.Errorf("write file %s: %w", target, err)
		}
		f.Close()
	}

	return nil
}
