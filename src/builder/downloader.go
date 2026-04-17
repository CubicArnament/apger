// Package builder provides package building functionality.
package builder

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Downloader handles downloading source code from various sources.
type Downloader struct {
	client *http.Client
}

// NewDownloader creates a new source downloader.
func NewDownloader() *Downloader {
	return &Downloader{
		client: &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},
	}
}

// DownloadSource downloads source from URL to destination directory.
// Supports git, tar.gz, tar.xz, tar.bz2, and zip.
func (d *Downloader) DownloadSource(sourceURL, destDir string, progress func(downloaded, total int64)) error {
	switch {
	case strings.HasSuffix(sourceURL, ".git"):
		return d.downloadGit(sourceURL, destDir)
	case strings.HasSuffix(sourceURL, ".tar.gz"), strings.HasSuffix(sourceURL, ".tgz"):
		return d.downloadTar(sourceURL, destDir, "gz", progress)
	case strings.HasSuffix(sourceURL, ".tar.xz"):
		return d.downloadTar(sourceURL, destDir, "xz", progress)
	case strings.HasSuffix(sourceURL, ".tar.bz2"):
		return d.downloadTar(sourceURL, destDir, "bz2", progress)
	case strings.HasSuffix(sourceURL, ".zip"):
		return d.downloadZip(sourceURL, destDir, progress)
	default:
		// Try as archive by default
		return d.downloadTar(sourceURL, destDir, "gz", progress)
	}
}

func (d *Downloader) downloadGit(gitURL, destDir string) error {
	cmd := exec.Command("git", "clone", "--depth", "1", gitURL, destDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (d *Downloader) downloadTar(url, destDir, compression string, progress func(downloaded, total int64)) error {
	// Download to temp file
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

	// Download with progress
	var writer io.Writer = tmpFile
	if progress != nil && resp.ContentLength > 0 {
		writer = &progressWriter{writer: tmpFile, progress: progress, total: resp.ContentLength}
	}

	if _, err := io.Copy(writer, resp.Body); err != nil {
		return fmt.Errorf("save download: %w", err)
	}

	// Reset file pointer
	if _, err := tmpFile.Seek(0, 0); err != nil {
		return fmt.Errorf("seek temp file: %w", err)
	}

	// Decompress
	var tarReader io.Reader
	switch compression {
	case "gz":
		gzReader, err := gzip.NewReader(tmpFile)
		if err != nil {
			return fmt.Errorf("gzip decompress: %w", err)
		}
		defer gzReader.Close()
		tarReader = gzReader
	case "bz2":
		tarReader = bzip2.NewReader(tmpFile)
	case "xz":
		// xz requires external tool on Windows
		return extractXZ(tmpFile.Name(), destDir)
	default:
		return fmt.Errorf("unsupported compression: %s", compression)
	}

	// Extract tar
	return extractTar(tarReader, destDir)
}

func (d *Downloader) downloadZip(url, destDir string, progress func(downloaded, total int64)) error {
	tmpFile, err := os.CreateTemp("", "apger-download-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	resp, err := d.client.Get(url)
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

	return extractZip(tmpFile.Name(), destDir)
}

// progressWriter wraps an io.Writer to report progress.
type progressWriter struct {
	writer   io.Writer
	progress func(downloaded, total int64)
	total    int64
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

func extractTar(reader io.Reader, destDir string) error {
	tr := tar.NewReader(reader)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}

		// Guard against path traversal (tar slip)
		target := filepath.Join(destDir, filepath.Clean("/"+header.Name))
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			continue // skip malicious entry
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("mkdir %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("mkdir parent %s: %w", target, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("create file %s: %w", target, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("write file %s: %w", target, err)
			}
			f.Close()
		}
	}

	return nil
}

func extractZip(src, destDir string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		target := filepath.Join(destDir, filepath.Clean("/"+f.Name))
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			continue // zip slip guard
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}

		outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return fmt.Errorf("create file %s: %w", target, err)
		}

		if _, err := io.Copy(outFile, rc); err != nil {
			rc.Close()
			outFile.Close()
			return fmt.Errorf("write file %s: %w", target, err)
		}

		rc.Close()
		outFile.Close()
	}

	return nil
}

func extractXZ(src, destDir string) error {
	// Use xz command for decompression
	cmd := exec.Command("xz", "-d", "-k", "-c", src)
	cmd.Stdout = os.Stdout // Will be replaced by tar extractor

	// Create pipe for tar streaming
	pipeReader, pipeWriter := io.Pipe()
	cmd.Stdout = pipeWriter

	// Start xz decompression
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start xz: %w", err)
	}

	// Extract tar in background
	go func() {
		if err := extractTar(pipeReader, destDir); err != nil {
			pipeReader.CloseWithError(fmt.Errorf("extract tar: %w", err))
		}
		pipeReader.Close()
	}()

	return cmd.Wait()
}
