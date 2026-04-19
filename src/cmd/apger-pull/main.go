// apger-pull — runs on the HOST machine (not in the pod).
// Watches for .ready marker files in the pod's output directory and
// copies the corresponding .apg packages to a local destination via kubectl cp.
//
// Usage:
//
//	apger-pull --dest /path/on/host [--namespace apger] [--pod apger] [--pod-dir /output/packages] [--watch]
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	dest := flag.String("dest", "", "Local destination directory (required)")
	ns := flag.String("namespace", "apger", "Kubernetes namespace")
	pod := flag.String("pod", "apger", "Pod name")
	podDir := flag.String("pod-dir", "/output/packages", "Directory inside pod containing packages")
	watch := flag.Bool("watch", false, "Keep watching for new packages (poll every 5s)")
	flag.Parse()

	if *dest == "" {
		fmt.Fprintln(os.Stderr, "error: --dest is required")
		flag.Usage()
		os.Exit(1)
	}

	if err := os.MkdirAll(*dest, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error: create dest dir: %v\n", err)
		os.Exit(1)
	}

	seen := map[string]bool{}

	for {
		pulled, err := pullReady(*ns, *pod, *podDir, *dest, seen)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		if pulled > 0 {
			fmt.Printf("Pulled %d package(s) to %s\n", pulled, *dest)
		}

		if !*watch {
			break
		}
		time.Sleep(5 * time.Second)
	}
}

// pullReady lists .ready markers in the pod, copies the corresponding .apg files,
// then removes the marker. Returns number of packages pulled.
func pullReady(ns, pod, podDir, dest string, seen map[string]bool) (int, error) {
	// List files in pod directory
	out, err := exec.Command("kubectl", "exec", pod, "-n", ns, "--",
		"find", podDir, "-name", "*.ready", "-type", "f").Output()
	if err != nil {
		return 0, fmt.Errorf("kubectl exec find: %w", err)
	}

	markers := strings.Fields(string(out))
	pulled := 0

	for _, marker := range markers {
		if seen[marker] {
			continue
		}

		// Derive .apg path from marker (strip .ready suffix)
		apgInPod := strings.TrimSuffix(marker, ".ready")
		base := filepath.Base(apgInPod)
		localDest := filepath.Join(dest, base)

		fmt.Printf("Pulling %s ...\n", base)

		// kubectl cp namespace/pod:src dest
		cpCmd := exec.Command("kubectl", "cp",
			fmt.Sprintf("%s/%s:%s", ns, pod, apgInPod),
			localDest,
		)
		cpCmd.Stdout = os.Stdout
		cpCmd.Stderr = os.Stderr
		if err := cpCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "  kubectl cp failed: %v\n", err)
			continue
		}

		// Also copy .sig if present
		sigInPod := apgInPod + ".sig"
		exec.Command("kubectl", "cp",
			fmt.Sprintf("%s/%s:%s", ns, pod, sigInPod),
			localDest+".sig",
		).Run() //nolint:errcheck — .sig is optional

		// Remove marker from pod
		exec.Command("kubectl", "exec", pod, "-n", ns, "--",
			"rm", "-f", marker).Run() //nolint:errcheck

		seen[marker] = true
		pulled++
		fmt.Printf("  → %s\n", localDest)
	}

	return pulled, nil
}
