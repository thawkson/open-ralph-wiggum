package tests

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Th0rgal/open-ralph-wiggum/internal/history"
	"github.com/Th0rgal/open-ralph-wiggum/internal/state"
)

func TestStatusIntegrationShowsRotationAndStrugglePreview(t *testing.T) {
	wd := t.TempDir()
	st := state.RalphState{
		Active:            true,
		Iteration:         4,
		MinIterations:     1,
		MaxIterations:     0,
		CompletionPromise: "COMPLETE",
		Prompt:            "demo",
		StartedAt:         time.Now().Add(-90 * time.Second).UTC().Format(time.RFC3339),
		Agent:             "opencode",
		Model:             "model-a",
		Rotation:          []string{"opencode:model-a", "codex:model-b"},
	}
	idx := 1
	st.RotationIndex = &idx
	if err := state.SaveLoopState(wd, st); err != nil {
		t.Fatalf("save state failed: %v", err)
	}
	h := history.Empty()
	h.StruggleIndicators.NoProgressIterations = 3
	h.StruggleIndicators.RepeatedErrors = map[string]int{"TypeError: failed parse": 2}
	h.Iterations = append(h.Iterations, history.IterationRecord{Iteration: 1, DurationMs: 1500, Agent: "opencode", Model: "model-a"})
	if err := history.Save(wd, h); err != nil {
		t.Fatalf("save history failed: %v", err)
	}
	if err := os.WriteFile(state.TasksPath(wd), []byte("# Ralph Tasks\n- [x] done\n- [/] working\n"), 0o644); err != nil {
		t.Fatalf("write tasks failed: %v", err)
	}

	output, code := runRalphBinary(t, wd, "--status", "--tasks")
	if code != 0 {
		t.Fatalf("status command failed with %d\n%s", code, output)
	}
	if !strings.Contains(output, "Rotation (position 2/2):") {
		t.Fatalf("missing rotation status output:\n%s", output)
	}
	if !strings.Contains(output, "Progress: 1/2 complete, 1 in progress") {
		t.Fatalf("missing task progress output:\n%s", output)
	}
	if !strings.Contains(output, "error 2x:") {
		t.Fatalf("missing repeated error preview:\n%s", output)
	}
}
