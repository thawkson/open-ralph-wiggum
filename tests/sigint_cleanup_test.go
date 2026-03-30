package tests

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Th0rgal/open-ralph-wiggum/internal/state"
)

// projectRoot returns the repository root directory for integration tests.
func projectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to get runtime caller")
	}
	return filepath.Dir(filepath.Dir(file))
}

// cleanupStateFiles removes persisted loop state files used by SIGINT tests.
func cleanupStateFiles(t *testing.T, root string) {
	t.Helper()
	_ = os.Remove(filepath.Join(root, ".ralph", "ralph-loop.state.json"))
	_ = os.Remove(filepath.Join(root, ".ralph", "ralph-questions.json"))
}

// TestSIGINTCleanup verifies SIGINT handling stops runs cleanly and clears persisted state.
func TestSIGINTCleanup(t *testing.T) {
	root := projectRoot(t)
	statePath := filepath.Join(root, ".ralph", "ralph-loop.state.json")

	t.Run("stops heartbeat timer on SIGINT", func(t *testing.T) {
		cleanupStateFiles(t, root)

		cmd := exec.Command("go", "run", "./cmd/ralph", "--max-iterations", "1", "--agent", "mock", "--no-commit", "sleep 5")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "NODE_ENV=test")
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out

		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start process: %v", err)
		}

		time.Sleep(1500 * time.Millisecond)
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()

		heartbeatCount := strings.Count(out.String(), "⏳ working...")
		if heartbeatCount == 0 {
			t.Fatalf("expected heartbeat output before SIGINT, got output: %s", out.String())
		}
	})

	t.Run("clears state on SIGINT", func(t *testing.T) {
		cleanupStateFiles(t, root)

		cmd := exec.Command("go", "run", "./cmd/ralph", "--max-iterations", "1", "--agent", "mock", "--no-commit", "sleep 5")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "NODE_ENV=test")
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out

		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start process: %v", err)
		}

		time.Sleep(500 * time.Millisecond)
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()

		if _, err := os.Stat(statePath); !os.IsNotExist(err) {
			t.Fatalf("expected state file to be removed, stat err=%v", err)
		}
	})

	t.Run("handles double SIGINT force stop", func(t *testing.T) {
		cleanupStateFiles(t, root)

		cmd := exec.Command("go", "run", "./cmd/ralph", "--max-iterations", "1", "--agent", "mock", "--no-commit", "sleep 10")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "NODE_ENV=test")
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out

		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start process: %v", err)
		}

		time.Sleep(500 * time.Millisecond)
		_ = cmd.Process.Signal(os.Interrupt)
		time.Sleep(100 * time.Millisecond)
		_ = cmd.Process.Signal(syscall.SIGINT)

		err := cmd.Wait()
		if err == nil {
			return
		}
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("unexpected error type: %T (%v)", err, err)
		}
		if code := exitErr.ExitCode(); code != 0 && code != 1 {
			t.Fatalf("expected exit code 0 or 1, got %d", code)
		}
	})

	t.Run("rotation state persists after SIGINT", func(t *testing.T) {
		cleanupStateFiles(t, root)

		idx := 1
		st := state.RalphState{
			Active:            true,
			Iteration:         2,
			MinIterations:     1,
			MaxIterations:     0,
			CompletionPromise: "COMPLETE",
			Prompt:            "sleep 60",
			StartedAt:         time.Now().UTC().Format(time.RFC3339),
			Agent:             "mock",
			Rotation:          []string{"mock:m1", "mock:m2"},
			RotationIndex:     &idx,
		}
		if err := state.SaveLoopState(root, st); err != nil {
			t.Fatalf("failed to seed state file: %v", err)
		}

		cmd := exec.Command(builtRalphBinary(t), "--no-commit")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "NODE_ENV=test")
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out

		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start process: %v", err)
		}

		time.Sleep(500 * time.Millisecond)
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()

		if _, err := os.Stat(filepath.Join(root, ".ralph", "ralph-loop.state.json")); !os.IsNotExist(err) {
			t.Fatalf("expected state file to be removed after SIGINT, stat err=%v", err)
		}
	})
}
