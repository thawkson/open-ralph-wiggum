package tests

import (
	"os"
	"strings"
	"testing"

	"github.com/Th0rgal/open-ralph-wiggum/internal/state"
)

func TestAbortPromiseIntegrationNormalMode(t *testing.T) {
	wd := t.TempDir()
	output, code := runRalphBinary(t, wd, "echo '<promise>STOP</promise>'", "--agent", "mock", "--abort-promise", "STOP", "--max-iterations", "3")
	if code != 1 {
		t.Fatalf("expected abort exit code 1, got %d\n%s", code, output)
	}
	if !strings.Contains(output, "Abort promise detected") {
		t.Fatalf("missing abort detection output:\n%s", output)
	}
}

func TestAbortPromiseIntegrationTasksMode(t *testing.T) {
	wd := t.TempDir()
	if err := state.EnsureDir(wd); err != nil {
		t.Fatalf("ensure dir failed: %v", err)
	}
	incompleteTasks := "# Ralph Tasks\n- [ ] todo\n"
	if err := os.WriteFile(state.TasksPath(wd), []byte(incompleteTasks), 0o644); err != nil {
		t.Fatalf("write tasks failed: %v", err)
	}
	output, code := runRalphBinary(t, wd, "echo '<promise>STOP</promise>'", "--agent", "mock", "--tasks", "--abort-promise", "STOP", "--task-promise", "NEXT", "--max-iterations", "3")
	if code != 1 {
		t.Fatalf("expected abort exit code 1 in tasks mode, got %d\n%s", code, output)
	}
	if !strings.Contains(output, "Abort promise detected") {
		t.Fatalf("missing abort detection output:\n%s", output)
	}
}
