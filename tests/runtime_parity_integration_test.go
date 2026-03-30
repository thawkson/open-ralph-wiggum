package tests

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Th0rgal/open-ralph-wiggum/internal/state"
)

func TestNoStreamPrintsBufferedOutput(t *testing.T) {
	wd := t.TempDir()
	output, code := runRalphBinary(t, wd, "echo HELLO_BUFFERED", "--agent", "mock", "--no-stream", "--max-iterations", "1", "--completion-promise", "DONE")
	if code != 0 {
		t.Fatalf("command failed with %d\n%s", code, output)
	}
	if !strings.Contains(output, "HELLO_BUFFERED") {
		t.Fatalf("expected buffered output to be printed in --no-stream mode:\n%s", output)
	}
}

func TestTasksModeCreatesTasksFile(t *testing.T) {
	wd := t.TempDir()
	output, code := runRalphBinary(t, wd,
		"echo '<promise>NEXT</promise>'",
		"--agent", "mock",
		"--tasks",
		"--task-promise", "NEXT",
		"--completion-promise", "COMPLETE",
		"--max-iterations", "1",
	)
	if code != 0 {
		t.Fatalf("command failed with %d\n%s", code, output)
	}
	payload, err := os.ReadFile(state.TasksPath(wd))
	if err != nil {
		t.Fatalf("expected tasks file to exist: %v", err)
	}
	if !strings.Contains(string(payload), "# Ralph Tasks") {
		t.Fatalf("unexpected tasks file content: %q", string(payload))
	}
	if !strings.Contains(output, "Created tasks file:") {
		t.Fatalf("expected creation message in output:\n%s", output)
	}
}

func TestContextClearedAfterConsumed(t *testing.T) {
	wd := t.TempDir()
	if err := state.EnsureDir(wd); err != nil {
		t.Fatalf("ensure dir failed: %v", err)
	}
	context := "# Ralph Loop Context\n\n## Context added at 2026-01-01T00:00:00Z\nUse this hint\n"
	if err := os.WriteFile(state.ContextPath(wd), []byte(context), 0o644); err != nil {
		t.Fatalf("write context failed: %v", err)
	}

	output, code := runRalphBinary(t, wd, "echo work", "--agent", "mock", "--max-iterations", "1", "--completion-promise", "DONE")
	if code != 0 {
		t.Fatalf("command failed with %d\n%s", code, output)
	}
	if _, err := os.Stat(state.ContextPath(wd)); !os.IsNotExist(err) {
		t.Fatalf("expected context file to be removed after consumption, stat err=%v", err)
	}
}

func TestMaxIterationsClearsPendingQuestions(t *testing.T) {
	wd := t.TempDir()
	if err := state.EnsureDir(wd); err != nil {
		t.Fatalf("ensure dir failed: %v", err)
	}
	questions := `[{
  "question": "pending answer",
  "timestamp": "` + time.Now().UTC().Format(time.RFC3339) + `"
}]`
	if err := os.WriteFile(state.QuestionsPath(wd), []byte(questions), 0o644); err != nil {
		t.Fatalf("write questions failed: %v", err)
	}

	output, code := runRalphBinary(t, wd, "echo work", "--agent", "mock", "--max-iterations", "1", "--completion-promise", "DONE")
	if code != 0 {
		t.Fatalf("command failed with %d\n%s", code, output)
	}
	if _, err := os.Stat(state.QuestionsPath(wd)); !os.IsNotExist(err) {
		t.Fatalf("expected pending questions file to be removed on max-iterations stop, stat err=%v", err)
	}
}

func TestStreamingHandlesVeryLongOutputLine(t *testing.T) {
	wd := t.TempDir()
	output, code := runRalphBinary(
		t,
		wd,
		"head -c 131072 /dev/zero | tr '\\0' 'A'; echo; echo '<promise>DONE</promise>'",
		"--agent", "mock",
		"--max-iterations", "1",
		"--completion-promise", "DONE",
	)
	if code != 0 {
		t.Fatalf("expected command to succeed with long streamed line, got %d\n%s", code, output)
	}
	if !strings.Contains(output, "Completion promise detected") {
		t.Fatalf("expected completion promise to be detected after long output line:\n%s", output)
	}
}
