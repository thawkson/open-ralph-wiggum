package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Th0rgal/open-ralph-wiggum/internal/state"
)

// TestRotationResumeProgression verifies resumed runs continue through configured rotation entries.
func TestRotationResumeProgression(t *testing.T) {
	wd := t.TempDir()
	configPath := filepath.Join(wd, "agents.json")
	config := `{"version":"1.0","agents":[{"type":"opencode","command":"true","configName":"OpenCode","argsTemplate":"opencode"},{"type":"codex","command":"true","configName":"Codex","argsTemplate":"codex"}]}`
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	idx := 0
	st := state.RalphState{
		Active:            true,
		Iteration:         1,
		MinIterations:     1,
		MaxIterations:     2,
		CompletionPromise: "COMPLETE",
		Prompt:            "echo work",
		StartedAt:         "2026-01-01T00:00:00Z",
		Agent:             "opencode",
		Rotation:          []string{"opencode:m1", "codex:m2"},
		RotationIndex:     &idx,
	}
	if err := state.SaveLoopState(wd, st); err != nil {
		t.Fatalf("save state failed: %v", err)
	}

	output, code := runRalphBinary(t, wd, "--config", configPath)
	if code != 0 {
		t.Fatalf("resume command failed with %d\n%s", code, output)
	}
	if !strings.Contains(output, "(opencode / m1)") || !strings.Contains(output, "(codex / m2)") {
		t.Fatalf("rotation progression not observed in output:\n%s", output)
	}
}
