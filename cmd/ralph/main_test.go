package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Th0rgal/open-ralph-wiggum/internal/agent"
	"github.com/Th0rgal/open-ralph-wiggum/internal/history"
	"github.com/Th0rgal/open-ralph-wiggum/internal/state"
)

func TestParseCLIArgsSupportsFlagsAfterPrompt(t *testing.T) {
	cfg, err := parseCLIArgs([]string{"Build API", "--agent", "codex", "--max-iterations", "7", "--task-promise", "NEXT"})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if got := cfg.agent; got != "codex" {
		t.Fatalf("expected agent codex, got %q", got)
	}
	if got := cfg.maxIterations; got != 7 {
		t.Fatalf("expected max iterations 7, got %d", got)
	}
	if got := cfg.taskPromise; got != "NEXT" {
		t.Fatalf("expected task promise NEXT, got %q", got)
	}
	if len(cfg.promptParts) != 1 || cfg.promptParts[0] != "Build API" {
		t.Fatalf("unexpected prompt parts: %#v", cfg.promptParts)
	}
}

func TestParseCLIArgsUnknownFlag(t *testing.T) {
	_, err := parseCLIArgs([]string{"--nope"})
	if err == nil {
		t.Fatal("expected unknown flag parse error")
	}
}

func TestParseCLIArgsPassthroughFlags(t *testing.T) {
	cfg, err := parseCLIArgs([]string{"task", "--", "--json", "--trace"})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(cfg.extraAgentFlags) != 2 {
		t.Fatalf("expected 2 passthrough flags, got %d", len(cfg.extraAgentFlags))
	}
}

func TestParseCLIArgsRotationAndConfigFlags(t *testing.T) {
	cfg, err := parseCLIArgs([]string{"task", "--rotation", "opencode:model-a,codex:model-b", "--config", "/tmp/agents.json", "--prompt-template", "./template.txt"})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if cfg.rotationInput != "opencode:model-a,codex:model-b" {
		t.Fatalf("unexpected rotation value: %q", cfg.rotationInput)
	}
	if cfg.configPath != "/tmp/agents.json" {
		t.Fatalf("unexpected config path: %q", cfg.configPath)
	}
	if cfg.promptTemplate != "./template.txt" {
		t.Fatalf("unexpected prompt template path: %q", cfg.promptTemplate)
	}
}

func TestParseRotationInput(t *testing.T) {
	agents := map[string]agent.Definition{
		"opencode": {Type: "opencode"},
		"codex":    {Type: "codex"},
	}

	parsed, err := parseRotationInput("opencode:model-a,codex:model-b", agents)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 rotation entries, got %d", len(parsed))
	}

	if _, err := parseRotationInput("broken-entry", agents); err == nil {
		t.Fatal("expected malformed rotation parse error")
	}
	if _, err := parseRotationInput("unknown:model", agents); err == nil {
		t.Fatal("expected unknown-agent rotation parse error")
	}
}

func TestPrintStatusShowsRotationAndErrorPreview(t *testing.T) {
	tmp := t.TempDir()
	st := state.RalphState{
		Active:            true,
		Iteration:         5,
		MinIterations:     1,
		MaxIterations:     10,
		CompletionPromise: "COMPLETE",
		Prompt:            "demo",
		StartedAt:         time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339),
		Agent:             "opencode",
		Model:             "model-a",
		Rotation:          []string{"opencode:model-a", "codex:model-b"},
	}
	idx := 1
	st.RotationIndex = &idx
	if err := state.SaveLoopState(tmp, st); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}
	h := history.Empty()
	h.Iterations = append(h.Iterations, history.IterationRecord{Iteration: 1, DurationMs: 1000, Agent: "opencode", Model: "model-a"})
	h.StruggleIndicators.RepeatedErrors = map[string]int{"TypeError: boom in parser": 3}
	h.StruggleIndicators.NoProgressIterations = 3
	if err := history.Save(tmp, h); err != nil {
		t.Fatalf("failed to save history: %v", err)
	}
	tasks := "# Ralph Tasks\n- [x] Done\n- [/] Working\n"
	if err := os.WriteFile(state.TasksPath(tmp), []byte(tasks), 0o644); err != nil {
		t.Fatalf("failed to write tasks: %v", err)
	}

	output := withCapturedOutput(t, func() {
		withWorkingDir(t, tmp, func() {
			if code := printStatus(true); code != 0 {
				t.Fatalf("printStatus returned %d", code)
			}
		})
	})

	mustContain(t, output, "Rotation (position 2/2):")
	mustContain(t, output, "**ACTIVE**")
	mustContain(t, output, "Elapsed:")
	mustContain(t, output, "Progress: 1/2 complete, 1 in progress")
	mustContain(t, output, `error 3x: "TypeError: boom in parser"`)
}

func TestRunWarnsNoPluginsForClaudeCode(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "agents.json")
	config := `{"version":"1.0","agents":[{"type":"claude-code","command":"true","configName":"Claude Code","argsTemplate":"claude-code"}]}`
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	output := withCapturedOutput(t, func() {
		withWorkingDir(t, tmp, func() {
			code := run([]string{"task", "--agent", "claude-code", "--no-plugins", "--max-iterations", "1", "--config", configPath})
			if code != 0 {
				t.Fatalf("run returned %d", code)
			}
		})
	})

	mustContain(t, output, "Warning: --no-plugins has no effect with Claude Code agent")
}

func withCapturedOutput(t *testing.T, fn func()) string {
	t.Helper()
	origStdout := os.Stdout
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}
	os.Stdout = w
	os.Stderr = w

	outCh := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		outCh <- buf.String()
	}()

	fn()
	_ = w.Close()
	os.Stdout = origStdout
	os.Stderr = origStderr
	return <-outCh
}

func withWorkingDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(old) }()
	fn()
}

func mustContain(t *testing.T, output string, want string) {
	t.Helper()
	if !strings.Contains(output, want) {
		t.Fatalf("expected output to contain %q\nfull output:\n%s", want, output)
	}
}
