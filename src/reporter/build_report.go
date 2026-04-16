// Package reporter generates build reports from pkgs.db into .logs/.
package reporter

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NurOS-Linux/apger/src/storage"
)

// GenerateReport writes a build log for every package in db into logsDir.
// Each file: .logs/<pkgName>-<YYYY-MM-DD>.log
func GenerateReport(db *storage.DB, logsDir string) error {
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return fmt.Errorf("create logs dir: %w", err)
	}

	packages, err := db.ListPackages()
	if err != nil {
		return fmt.Errorf("list packages: %w", err)
	}

	date := time.Now().Format("2006-01-02")

	for name, info := range packages {
		logPath := filepath.Join(logsDir, fmt.Sprintf("%s-%s.log", name, date))
		if err := writeLog(logPath, info); err != nil {
			return fmt.Errorf("write log %s: %w", name, err)
		}
	}
	return nil
}

func writeLog(path string, info *storage.PackageInfo) error {
	status := "FAILED"
	if info.Built {
		status = "OK"
	}

	content := fmt.Sprintf(`APGer Build Report
==================
Package:    %s
Version:    %s
Status:     %s
Built at:   %s
Hash:       %s
Output:     %s
`,
		info.Name,
		info.Version,
		status,
		info.BuiltAt.Format(time.RFC3339),
		info.Hash,
		info.OutputPath,
	)

	return os.WriteFile(path, []byte(content), 0644)
}
