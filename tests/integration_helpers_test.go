package tests

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	buildOnce   sync.Once
	binaryPath  string
	binaryError error
)

func builtRalphBinary(t *testing.T) string {
	t.Helper()
	root := projectRoot(t)
	buildOnce.Do(func() {
		tmpDir, err := os.MkdirTemp("", "ralph-bin-")
		if err != nil {
			binaryError = err
			return
		}
		binaryPath = filepath.Join(tmpDir, "ralph-test-bin")
		cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/ralph")
		cmd.Dir = root
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			binaryError = err
			binaryPath = out.String()
		}
	})
	if binaryError != nil {
		t.Fatalf("failed to build ralph binary: %v\n%s", binaryError, binaryPath)
	}
	return binaryPath
}

func runRalphBinary(t *testing.T, workingDir string, args ...string) (string, int) {
	t.Helper()
	bin := builtRalphBinary(t)
	finalArgs := append([]string{}, args...)
	hasNoCommit := false
	for _, arg := range finalArgs {
		if arg == "--no-commit" {
			hasNoCommit = true
			break
		}
	}
	if !hasNoCommit {
		finalArgs = append(finalArgs, "--no-commit")
	}
	cmd := exec.Command(bin, finalArgs...)
	cmd.Dir = workingDir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err == nil {
		return out.String(), 0
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("unexpected command error: %v", err)
	}
	return out.String(), exitErr.ExitCode()
}
