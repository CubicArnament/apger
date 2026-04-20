package logger

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BuildLogEntry represents a single package build log entry.
type BuildLogEntry struct {
	PackageName string
	Version     string
	Timestamp   time.Time
	Duration    time.Duration
	Success     bool
	Log         string
	SHA256      string // SHA256 of built .apg file
	OutputFiles []string
}

// ExportBuildLog saves build log to output/build-logs/<arch>/<type>/ directory.
// Format: build-logs/<arch>/<type>/<package>-<timestamp>.log
func ExportBuildLog(outputDir string, entry BuildLogEntry, arch string, repoType string) error {
	// Default values
	if arch == "" {
		arch = "x86_64"
	}
	if repoType == "" {
		repoType = "main"
	}
	
	logDir := filepath.Join(outputDir, "build-logs", arch, repoType)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	// Filename: package-YYYYMMDD-HHMMSS.log
	timestamp := entry.Timestamp.Format("20060102-150405")
	filename := fmt.Sprintf("%s-%s.log", entry.PackageName, timestamp)
	logPath := filepath.Join(logDir, filename)

	f, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("create log file: %w", err)
	}
	defer f.Close()

	// Write structured log
	fmt.Fprintf(f, "=== APGer Build Log ===\n")
	fmt.Fprintf(f, "Package:    %s\n", entry.PackageName)
	fmt.Fprintf(f, "Version:    %s\n", entry.Version)
	fmt.Fprintf(f, "Arch:       %s\n", arch)
	fmt.Fprintf(f, "Repo:       %s\n", repoType)
	fmt.Fprintf(f, "Timestamp:  %s\n", entry.Timestamp.Format(time.RFC3339))
	fmt.Fprintf(f, "Duration:   %s\n", entry.Duration)
	fmt.Fprintf(f, "Status:     %s\n", statusString(entry.Success))
	if entry.SHA256 != "" {
		fmt.Fprintf(f, "SHA256:     %s\n", entry.SHA256)
	}
	if len(entry.OutputFiles) > 0 {
		fmt.Fprintf(f, "Output:     %d files\n", len(entry.OutputFiles))
		for _, file := range entry.OutputFiles {
			fmt.Fprintf(f, "  - %s\n", file)
		}
	}
	fmt.Fprintf(f, "\n=== Build Log ===\n\n")
	fmt.Fprintf(f, "%s\n", entry.Log)

	return nil
}

// ComputeSHA256 computes SHA256 hash of a file.
func ComputeSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func statusString(success bool) string {
	if success {
		return "SUCCESS"
	}
	return "FAILED"
}
