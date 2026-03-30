package loop

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Th0rgal/open-ralph-wiggum/internal/agent"
	"github.com/Th0rgal/open-ralph-wiggum/internal/state"
)

// TestParseRotationEntry verifies rotation entries parse into agent and model values.
func TestParseRotationEntry(t *testing.T) {
	agentName, model, err := parseRotationEntry("opencode:model-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentName != "opencode" || model != "model-a" {
		t.Fatalf("unexpected parse result: %s / %s", agentName, model)
	}

	if _, _, err := parseRotationEntry("invalid"); err == nil {
		t.Fatal("expected invalid entry error")
	}
}

// TestSelectedAgentModel verifies rotation index selection resolves to the expected agent and model.
func TestSelectedAgentModel(t *testing.T) {
	idx := 1
	st := &state.RalphState{
		Agent:         "opencode",
		Model:         "default",
		Rotation:      []string{"opencode:model-a", "codex:model-b"},
		RotationIndex: &idx,
	}
	agentName, model, gotIdx, err := selectedAgentModel(st)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotIdx != 1 || agentName != "codex" || model != "model-b" {
		t.Fatalf("unexpected selected model: idx=%d agent=%s model=%s", gotIdx, agentName, model)
	}
}

// TestRenderPromptTemplate verifies prompt template placeholders are substituted from state values.
func TestRenderPromptTemplate(t *testing.T) {
	tmp := t.TempDir()
	templatePath := filepath.Join(tmp, "template.txt")
	if err := os.MkdirAll(filepath.Join(tmp, ".ralph"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	template := "iter={{iteration}} max={{max_iterations}} prompt={{prompt}} done={{completion_promise}}"
	if err := os.WriteFile(templatePath, []byte(template), 0o644); err != nil {
		t.Fatalf("write template failed: %v", err)
	}

	st := &state.RalphState{
		Iteration:         2,
		MinIterations:     1,
		MaxIterations:     5,
		Prompt:            "Build API",
		CompletionPromise: "COMPLETE",
		PromptTemplate:    templatePath,
	}

	rendered, err := renderPromptTemplate(tmp, st)
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}
	if rendered == template {
		t.Fatal("expected substitutions in rendered output")
	}
}

// TestResolveAgentSpecAllowAllAndStreamArgs verifies allow-all and streaming flags are added per agent template.
func TestResolveAgentSpecAllowAllAndStreamArgs(t *testing.T) {
	claudeDef := agent.Definition{Type: "claude-code", Command: "claude", ArgsTemplate: "claude-code"}
	claude, err := resolveAgentSpec(claudeDef, "prompt", "m", []string{"--x"}, true, true)
	if err != nil {
		t.Fatalf("resolveAgentSpec claude failed: %v", err)
	}
	claudeJoined := strings.Join(claude.Args, " ")
	if !strings.Contains(claudeJoined, "--output-format stream-json") || !strings.Contains(claudeJoined, "--dangerously-skip-permissions") {
		t.Fatalf("expected claude stream/permission args, got %q", claudeJoined)
	}

	codexDef := agent.Definition{Type: "codex", Command: "codex", ArgsTemplate: "codex"}
	codex, err := resolveAgentSpec(codexDef, "prompt", "m", nil, true, false)
	if err != nil {
		t.Fatalf("resolveAgentSpec codex failed: %v", err)
	}
	if !contains(codex.Args, "--full-auto") {
		t.Fatalf("expected codex allow-all arg, got %#v", codex.Args)
	}

	copilotDef := agent.Definition{Type: "copilot", Command: "copilot", ArgsTemplate: "copilot"}
	copilot, err := resolveAgentSpec(copilotDef, "prompt", "m", nil, true, false)
	if err != nil {
		t.Fatalf("resolveAgentSpec copilot failed: %v", err)
	}
	if !contains(copilot.Args, "--allow-all") || !contains(copilot.Args, "--no-ask-user") {
		t.Fatalf("expected copilot allow-all args, got %#v", copilot.Args)
	}
}

// TestEnsureTasksFile verifies task file creation is correct and idempotent.
func TestEnsureTasksFile(t *testing.T) {
	tmp := t.TempDir()
	path, created, err := ensureTasksFile(tmp)
	if err != nil {
		t.Fatalf("ensureTasksFile failed: %v", err)
	}
	if !created {
		t.Fatal("expected tasks file to be created")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading tasks file failed: %v", err)
	}
	if !strings.Contains(string(payload), "# Ralph Tasks") {
		t.Fatalf("unexpected tasks file content: %q", string(payload))
	}

	_, createdAgain, err := ensureTasksFile(tmp)
	if err != nil {
		t.Fatalf("ensureTasksFile second call failed: %v", err)
	}
	if createdAgain {
		t.Fatal("did not expect tasks file to be recreated")
	}
}

// TestLoadAndClearContext verifies context can be loaded and then cleared from disk.
func TestLoadAndClearContext(t *testing.T) {
	tmp := t.TempDir()
	if err := state.EnsureDir(tmp); err != nil {
		t.Fatalf("state ensure dir failed: %v", err)
	}
	if err := os.WriteFile(state.ContextPath(tmp), []byte("# Ralph Loop Context\n\nhello"), 0o644); err != nil {
		t.Fatalf("write context failed: %v", err)
	}
	if got := loadContext(tmp); !strings.Contains(got, "hello") {
		t.Fatalf("expected loaded context to contain hello, got %q", got)
	}
	if err := clearContext(tmp); err != nil {
		t.Fatalf("clearContext failed: %v", err)
	}
	if got := loadContext(tmp); got != "" {
		t.Fatalf("expected empty context after clear, got %q", got)
	}
}

// TestPendingQuestionQueue verifies pending questions are queued and dequeued in FIFO order.
func TestPendingQuestionQueue(t *testing.T) {
	tmp := t.TempDir()
	if err := savePendingQuestion(tmp, "first"); err != nil {
		t.Fatalf("save first pending question failed: %v", err)
	}
	if err := savePendingQuestion(tmp, "second"); err != nil {
		t.Fatalf("save second pending question failed: %v", err)
	}

	q1, err := getAndClearPendingQuestion(tmp)
	if err != nil {
		t.Fatalf("getAndClearPendingQuestion failed: %v", err)
	}
	if q1 != "first" {
		t.Fatalf("expected first pending question, got %q", q1)
	}

	q2, err := getAndClearPendingQuestion(tmp)
	if err != nil {
		t.Fatalf("getAndClearPendingQuestion failed: %v", err)
	}
	if q2 != "second" {
		t.Fatalf("expected second pending question, got %q", q2)
	}

	q3, err := getAndClearPendingQuestion(tmp)
	if err != nil {
		t.Fatalf("getAndClearPendingQuestion failed: %v", err)
	}
	if q3 != "" {
		t.Fatalf("expected empty question after queue is drained, got %q", q3)
	}
}

// TestParseToolFromLine verifies default tool parsing across supported output formats.
func TestParseToolFromLine(t *testing.T) {
	cases := map[string]string{
		"Tool: question":                "question",
		"Using edit_file":               "edit_file",
		"{\"tool_name\": \"runTests\"}": "runtests",
		"|  grep_search":                "grep_search",
		"plain output line":             "",
	}
	for line, expected := range cases {
		if got := parseToolFromLine(line); got != expected {
			t.Fatalf("parseToolFromLine(%q) = %q, expected %q", line, got, expected)
		}
	}
}

// TestParseToolFromLineWithPatternClaudeCode verifies Claude-specific parsing only captures tool_use entries.
func TestParseToolFromLineWithPatternClaudeCode(t *testing.T) {
	line := `{"type":"tool_use","name":"ReadFile"}`
	got := parseToolFromLineWithPattern(line, "claude-code")
	if got != "readfile" {
		t.Fatalf("expected readfile for claude tool_use payload, got %q", got)
	}

	nonToolUse := `{"type":"message","name":"IgnoredName"}`
	got = parseToolFromLineWithPattern(nonToolUse, "claude-code")
	if got != "" {
		t.Fatalf("expected empty parse result for non-tool_use payload, got %q", got)
	}
}

// TestDetectPlaceholderAndModelErrors verifies placeholder plugin and model-not-found detection heuristics.
func TestDetectPlaceholderAndModelErrors(t *testing.T) {
	if !detectPlaceholderPluginError("ralph-wiggum is not yet ready for use. This is a placeholder package.") {
		t.Fatal("expected placeholder plugin detection")
	}
	if !detectModelNotFoundError("ProviderModelNotFoundError: missing model") {
		t.Fatal("expected model-not-found detection")
	}
	if detectModelNotFoundError("all good") {
		t.Fatal("did not expect model-not-found detection")
	}
}

// TestEnsureRalphConfig verifies generated OpenCode config filters plugins and applies allow-all permissions.
func TestEnsureRalphConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))

	userConfigDir := filepath.Join(tmp, "xdg", "opencode")
	if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	userConfig := `{"plugin":["auth-provider","not-auth","AUTH-helper"]}`
	if err := os.WriteFile(filepath.Join(userConfigDir, "opencode.json"), []byte(userConfig), 0o644); err != nil {
		t.Fatalf("write user config failed: %v", err)
	}

	path, err := ensureRalphConfig(tmp, true, true)
	if err != nil {
		t.Fatalf("ensureRalphConfig failed: %v", err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated config failed: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		t.Fatalf("unmarshal generated config failed: %v", err)
	}
	plugins, ok := parsed["plugin"].([]any)
	if !ok || len(plugins) != 3 {
		t.Fatalf("expected filtered auth plugins, got %#v", parsed["plugin"])
	}
	permission, ok := parsed["permission"].(map[string]any)
	if !ok {
		t.Fatalf("expected permission map, got %#v", parsed["permission"])
	}
	if permission["read"] != "allow" || permission["question"] != "allow" {
		t.Fatalf("expected allow-all permissions, got %#v", permission)
	}
}

// TestGetModifiedFilesSinceSnapshot verifies snapshot diffing reports changed, added, and removed files.
func TestGetModifiedFilesSinceSnapshot(t *testing.T) {
	before := fileSnapshot{files: map[string]string{
		"a.txt": "hash-a1",
		"b.txt": "hash-b1",
		"c.txt": "hash-c1",
	}}
	after := fileSnapshot{files: map[string]string{
		"a.txt": "hash-a2",
		"b.txt": "hash-b1",
		"d.txt": "hash-d1",
	}}

	got := getModifiedFilesSinceSnapshot(before, after)
	want := []string{"a.txt", "c.txt", "d.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected modified files: got %v want %v", got, want)
	}
}

// TestStreamPipeLongLineBeyondScannerLimit verifies streamPipe handles very long newline-terminated lines.
func TestStreamPipeLongLineBeyondScannerLimit(t *testing.T) {
	line := strings.Repeat("A", 128*1024)
	capture, output, lastActivity := runStreamPipeForTest(line+"\n", true, true, "default", nil)

	if capture != line+"\n" {
		t.Fatalf("unexpected capture length: got=%d want=%d", len(capture), len(line)+1)
	}
	if output != line+"\n" {
		t.Fatalf("unexpected output length: got=%d want=%d", len(output), len(line)+1)
	}
	if lastActivity == 0 {
		t.Fatal("expected stream activity timestamp to be updated")
	}
}

// TestStreamPipeFlushesPartialLineAtEOF verifies streamPipe flushes partial lines on EOF.
func TestStreamPipeFlushesPartialLineAtEOF(t *testing.T) {
	line := strings.Repeat("B", 96*1024)
	capture, output, _ := runStreamPipeForTest(line, true, true, "default", nil)

	if capture != line+"\n" {
		t.Fatalf("expected partial EOF line to be captured, got length %d", len(capture))
	}
	if output != line+"\n" {
		t.Fatalf("expected partial EOF line to be streamed, got length %d", len(output))
	}
}

// TestStreamPipeSuppressesToolLinesInCompactModeForLongLines verifies compact mode counts tool lines without streaming them.
func TestStreamPipeSuppressesToolLinesInCompactModeForLongLines(t *testing.T) {
	line := "Tool: edit_file " + strings.Repeat("X", 80*1024)
	tools := &toolSummaryState{
		counts:          map[string]int{},
		lastPrintedAt:   time.Now(),
		summaryInterval: time.Hour,
	}

	capture, output, _ := runStreamPipeForTest(line+"\n", true, false, "default", tools)

	if capture != line+"\n" {
		t.Fatalf("expected long tool line to be captured, got length %d", len(capture))
	}
	if output != "" {
		t.Fatalf("expected compact mode to suppress streamed tool line, got length %d", len(output))
	}

	snapshot := tools.snapshot()
	if snapshot["edit_file"] != 1 {
		t.Fatalf("expected tool count edit_file=1, got %#v", snapshot)
	}
}

// TestStreamPipeCapturesInNoStreamMode verifies no-stream mode captures output without writing it to the stream.
func TestStreamPipeCapturesInNoStreamMode(t *testing.T) {
	line := strings.Repeat("C", 72*1024)
	capture, output, _ := runStreamPipeForTest(line+"\n", false, false, "default", nil)

	if capture != line+"\n" {
		t.Fatalf("expected no-stream capture to include full line, got length %d", len(capture))
	}
	if output != "" {
		t.Fatalf("expected no streamed output when stream=false, got %q", output)
	}
}

// runStreamPipeForTest runs streamPipe with fixtures and returns captured output state.
func runStreamPipeForTest(input string, stream bool, verboseTools bool, parsePattern string, tools *toolSummaryState) (string, string, int64) {
	var capture bytes.Buffer
	var output bytes.Buffer
	activity := &atomic.Int64{}
	var wg sync.WaitGroup
	wg.Add(1)
	go streamPipe(strings.NewReader(input), &output, &capture, stream, verboseTools, parsePattern, activity, tools, &wg)
	wg.Wait()
	return capture.String(), output.String(), activity.Load()
}

// contains reports whether target appears in values.
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// TestFindCurrentTask verifies the first in-progress task is selected.
func TestFindCurrentTask(t *testing.T) {
	tasks := []loopTask{
		{text: "First", status: "complete"},
		{text: "Second", status: "in-progress"},
		{text: "Third", status: "todo"},
	}
	current := findCurrentTask(tasks)
	if current == nil || current.text != "Second" {
		t.Fatalf("expected in-progress task 'Second', got %v", current)
	}

	allTodo := []loopTask{{text: "A", status: "todo"}}
	if findCurrentTask(allTodo) != nil {
		t.Fatal("expected nil when no in-progress task")
	}

	if findCurrentTask(nil) != nil {
		t.Fatal("expected nil for empty slice")
	}
}

// TestFindNextTask verifies the first todo task is selected.
func TestFindNextTask(t *testing.T) {
	tasks := []loopTask{
		{text: "Done", status: "complete"},
		{text: "WIP", status: "in-progress"},
		{text: "Next", status: "todo"},
		{text: "Later", status: "todo"},
	}
	next := findNextTask(tasks)
	if next == nil || next.text != "Next" {
		t.Fatalf("expected first todo task 'Next', got %v", next)
	}

	allDone := []loopTask{
		{text: "A", status: "complete"},
		{text: "B", status: "in-progress"},
	}
	if findNextTask(allDone) != nil {
		t.Fatal("expected nil when no todo tasks")
	}
}

// TestAllTasksComplete verifies completion checks include both tasks and subtasks.
func TestAllTasksComplete(t *testing.T) {
	if allTasksComplete(nil) {
		t.Fatal("expected false for empty list")
	}
	if allTasksComplete([]loopTask{}) {
		t.Fatal("expected false for empty list")
	}

	incomplete := []loopTask{
		{text: "Done", status: "complete"},
		{text: "WIP", status: "in-progress"},
	}
	if allTasksComplete(incomplete) {
		t.Fatal("expected false with in-progress task")
	}

	withIncompleteSubtask := []loopTask{
		{text: "Done", status: "complete", subtasks: []loopTask{
			{text: "Sub", status: "todo"},
		}},
	}
	if allTasksComplete(withIncompleteSubtask) {
		t.Fatal("expected false with incomplete subtask")
	}

	complete := []loopTask{
		{text: "A", status: "complete", subtasks: []loopTask{
			{text: "A.1", status: "complete"},
		}},
		{text: "B", status: "complete"},
	}
	if !allTasksComplete(complete) {
		t.Fatal("expected true when all tasks and subtasks complete")
	}
}

// TestParseLoopTasks verifies markdown task parsing produces expected task and subtask statuses.
func TestParseLoopTasks(t *testing.T) {
	content := "# Tasks\n- [x] Done task\n- [/] In progress\n  - [ ] Sub todo\n  - [x] Sub done\n- [ ] Todo task\n"
	tasks := parseLoopTasks(content)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	if tasks[0].status != "complete" || tasks[0].text != "Done task" {
		t.Fatalf("task 0 mismatch: %+v", tasks[0])
	}
	if tasks[1].status != "in-progress" || len(tasks[1].subtasks) != 2 {
		t.Fatalf("task 1 mismatch: %+v", tasks[1])
	}
	if tasks[1].subtasks[0].status != "todo" || tasks[1].subtasks[1].status != "complete" {
		t.Fatalf("subtask mismatch: %+v", tasks[1].subtasks)
	}
	if tasks[2].status != "todo" {
		t.Fatalf("task 2 should be todo: %+v", tasks[2])
	}
}

// TestExtractClaudeStreamDisplayLines verifies Claude stream JSON is converted into displayable lines.
func TestExtractClaudeStreamDisplayLines(t *testing.T) {
	// Non-JSON line returns as-is
	plain := extractClaudeStreamDisplayLines("Hello world")
	if len(plain) != 1 || plain[0] != "Hello world" {
		t.Fatalf("expected passthrough for plain line, got %v", plain)
	}

	// Assistant message with content text
	assistantJSON := `{"type":"assistant","message":{"content":[{"type":"text","text":"Here is the answer"}]}}`
	lines := extractClaudeStreamDisplayLines(assistantJSON)
	if len(lines) == 0 || lines[0] != "Here is the answer" {
		t.Fatalf("expected extracted text, got %v", lines)
	}

	// Assistant delta with text
	deltaJSON := `{"type":"assistant","delta":{"text":"partial output\nsecond line"}}`
	lines = extractClaudeStreamDisplayLines(deltaJSON)
	if len(lines) != 2 || lines[0] != "partial output" || lines[1] != "second line" {
		t.Fatalf("expected delta text lines, got %v", lines)
	}

	// tool_use blocks should be skipped
	toolJSON := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"bash"},{"type":"text","text":"visible"}]}}`
	lines = extractClaudeStreamDisplayLines(toolJSON)
	if len(lines) != 1 || lines[0] != "visible" {
		t.Fatalf("expected tool_use skipped, got %v", lines)
	}

	// Result type
	resultJSON := `{"type":"result","result":"All done"}`
	lines = extractClaudeStreamDisplayLines(resultJSON)
	if len(lines) != 1 || lines[0] != "All done" {
		t.Fatalf("expected result text, got %v", lines)
	}

	// Error type with message object
	errorJSON := `{"type":"error","error":{"message":"something went wrong"}}`
	lines = extractClaudeStreamDisplayLines(errorJSON)
	if len(lines) != 1 || lines[0] != "something went wrong" {
		t.Fatalf("expected error message, got %v", lines)
	}

	// Malformed JSON returns original line
	malformed := extractClaudeStreamDisplayLines("{not json")
	if len(malformed) != 1 || malformed[0] != "{not json" {
		t.Fatalf("expected passthrough for malformed JSON, got %v", malformed)
	}

	// Unknown type returns empty
	unknown := extractClaudeStreamDisplayLines(`{"type":"unknown","data":"x"}`)
	if len(unknown) != 0 {
		t.Fatalf("expected empty for unknown type, got %v", unknown)
	}
}

// TestBuildLoopPromptDefaultMode verifies default-mode prompts include task instructions and iteration metadata.
func TestBuildLoopPromptDefaultMode(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".ralph"), 0o755); err != nil {
		t.Fatal(err)
	}
	st := &state.RalphState{
		Iteration:         3,
		MinIterations:     2,
		MaxIterations:     10,
		Prompt:            "Build a REST API",
		CompletionPromise: "DONE",
	}
	prompt := buildLoopPrompt(tmp, st)
	if !strings.Contains(prompt, "# Ralph Wiggum Loop - Iteration 3") {
		t.Fatal("missing header")
	}
	if !strings.Contains(prompt, "Build a REST API") {
		t.Fatal("missing task prompt")
	}
	if !strings.Contains(prompt, "<promise>DONE</promise>") {
		t.Fatal("missing completion promise")
	}
	if !strings.Contains(prompt, "3 / 10") {
		t.Fatal("missing iteration counter")
	}
	if !strings.Contains(prompt, "(min: 2)") {
		t.Fatal("missing min iterations display")
	}
	if !strings.Contains(prompt, "## Instructions") {
		t.Fatal("missing instructions section")
	}
	if !strings.Contains(prompt, "## Critical Rules") {
		t.Fatal("missing critical rules section")
	}
}

// TestBuildLoopPromptTasksMode verifies tasks-mode prompts include workflow guidance and promise rules.
func TestBuildLoopPromptTasksMode(t *testing.T) {
	tmp := t.TempDir()
	ralphDir := filepath.Join(tmp, ".ralph")
	if err := os.MkdirAll(ralphDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tasksContent := "# Tasks\n- [x] Setup project\n- [/] Build API\n- [ ] Write tests\n"
	if err := os.WriteFile(filepath.Join(ralphDir, "ralph-tasks.md"), []byte(tasksContent), 0o644); err != nil {
		t.Fatal(err)
	}

	st := &state.RalphState{
		Iteration:         2,
		MinIterations:     1,
		MaxIterations:     0,
		Prompt:            "Do everything",
		CompletionPromise: "ALL_DONE",
		TasksMode:         true,
		TaskPromise:       "NEXT_TASK",
	}
	prompt := buildLoopPrompt(tmp, st)
	if !strings.Contains(prompt, "TASKS MODE") {
		t.Fatal("missing tasks mode section")
	}
	if !strings.Contains(prompt, "CURRENT TASK") {
		t.Fatal("missing current task indicator")
	}
	if !strings.Contains(prompt, "Build API") {
		t.Fatal("missing current task text")
	}
	if !strings.Contains(prompt, "<promise>NEXT_TASK</promise>") {
		t.Fatal("missing task promise")
	}
	if !strings.Contains(prompt, "<promise>ALL_DONE</promise>") {
		t.Fatal("missing completion promise")
	}
	if !strings.Contains(prompt, "Task Workflow") {
		t.Fatal("missing task workflow section")
	}
}

// TestBuildLoopPromptWithContext verifies prompts include user-provided context when present.
func TestBuildLoopPromptWithContext(t *testing.T) {
	tmp := t.TempDir()
	ralphDir := filepath.Join(tmp, ".ralph")
	if err := os.MkdirAll(ralphDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ralphDir, "ralph-context.md"), []byte("Focus on auth module"), 0o644); err != nil {
		t.Fatal(err)
	}

	st := &state.RalphState{
		Iteration:         1,
		MinIterations:     1,
		Prompt:            "Fix bugs",
		CompletionPromise: "COMPLETE",
	}
	prompt := buildLoopPrompt(tmp, st)
	if !strings.Contains(prompt, "Additional Context") {
		t.Fatal("missing context section")
	}
	if !strings.Contains(prompt, "Focus on auth module") {
		t.Fatal("missing context content")
	}
}

// TestStreamPipeClaudeStreamParsing verifies Claude stream text is rendered while raw JSON remains captured.
func TestStreamPipeClaudeStreamParsing(t *testing.T) {
	input := `{"type":"assistant","delta":{"text":"Hello from Claude"}}` + "\n" +
		"plain line\n" +
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"bash"}]}}` + "\n"

	var output bytes.Buffer
	var capture bytes.Buffer
	activity := &atomic.Int64{}
	activity.Store(time.Now().UnixNano())
	tools := &toolSummaryState{counts: map[string]int{}, summaryInterval: 3 * time.Second}

	var wg sync.WaitGroup
	wg.Add(1)
	go streamPipe(strings.NewReader(input), &output, &capture, true, true, "claude-code", activity, tools, &wg)
	wg.Wait()

	outputStr := output.String()
	if !strings.Contains(outputStr, "Hello from Claude") {
		t.Fatalf("expected extracted Claude text in output, got: %s", outputStr)
	}
	if !strings.Contains(outputStr, "plain line") {
		t.Fatalf("expected plain line in output, got: %s", outputStr)
	}
	// The raw JSON should still be captured for promise detection
	if !strings.Contains(capture.String(), `"type":"assistant"`) {
		t.Fatal("expected raw JSON in capture buffer")
	}
}
