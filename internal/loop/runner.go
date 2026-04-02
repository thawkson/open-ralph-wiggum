package loop

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Th0rgal/open-ralph-wiggum/internal/agent"
	"github.com/Th0rgal/open-ralph-wiggum/internal/completion"
	"github.com/Th0rgal/open-ralph-wiggum/internal/history"
	"github.com/Th0rgal/open-ralph-wiggum/internal/state"
)

type Options struct {
	Prompt              string
	MinIterations       int
	MaxIterations       int
	CompletionPromise   string
	AbortPromise        string
	TasksMode           bool
	TaskPromise         string
	Rotation            []string
	PromptTemplate      string
	Model               string
	Agent               string
	Agents              map[string]agent.Definition
	StreamOutput        bool
	VerboseTools        bool
	HandleQuestions     bool
	DisablePlugins      bool
	AutoCommit          bool
	AllowAllPermissions bool
	ExtraAgentFlags     []string
}

type AgentSpec struct {
	Command string
	Args    []string
}

// resolveAgentSpec builds the CLI command and arguments for an agent run.
func resolveAgentSpec(def agent.Definition, prompt, model string, extraFlags []string, allowAllPermissions bool, streamOutput bool) (*AgentSpec, error) {
	if def.Type == "mock" {
		return &AgentSpec{Command: "sh", Args: []string{"-c", prompt}}, nil
	}
	template := strings.TrimSpace(def.ArgsTemplate)
	if template == "" {
		template = def.Type
	}

	var args []string
	switch template {
	case "opencode":
		args = []string{"run"}
		if model != "" {
			args = append(args, "-m", model)
		}
		if len(extraFlags) > 0 {
			args = append(args, extraFlags...)
		}
		args = append(args, prompt)
	case "claude-code":
		args = []string{"-p", prompt}
		if streamOutput {
			args = append(args, "--output-format", "stream-json", "--include-partial-messages", "--verbose")
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		if allowAllPermissions {
			args = append(args, "--dangerously-skip-permissions")
		}
		if len(extraFlags) > 0 {
			args = append(args, extraFlags...)
		}
	case "codex":
		args = []string{"exec"}
		if model != "" {
			args = append(args, "--model", model)
		}
		if allowAllPermissions {
			args = append(args, "--full-auto")
		}
		if len(extraFlags) > 0 {
			args = append(args, extraFlags...)
		}
		args = append(args, prompt)
	case "copilot":
		args = []string{"-p", prompt}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, "--no-ask-user")
		if allowAllPermissions {
			args = append(args, "--allow-all")
		}
		if len(extraFlags) > 0 {
			args = append(args, extraFlags...)
		}
	default:
		args = []string{}
		if model != "" {
			args = append(args, "--model", model)
		}
		if allowAllPermissions {
			args = append(args, "--full-auto")
		}
		if len(extraFlags) > 0 {
			args = append(args, extraFlags...)
		}
		args = append(args, prompt)
	}

	return &AgentSpec{Command: def.Command, Args: args}, nil
}

// validateAgent checks that the resolved agent command exists in PATH.
func validateAgent(def agent.Definition) error {
	spec, err := resolveAgentSpec(def, "noop", "", nil, false, false)
	if err != nil {
		return err
	}
	_, err = exec.LookPath(spec.Command)
	if err != nil {
		name := def.ConfigName
		if strings.TrimSpace(name) == "" {
			name = def.Type
		}
		return fmt.Errorf("%s CLI not found in PATH (%s)", name, spec.Command)
	}
	return nil
}

// Run executes the iterative loop until completion, abort, or interruption.
func Run(opts Options) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error getting working directory:", err)
		return 1
	}

	loadedState, err := state.LoadLoopState(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error loading state:", err)
		return 1
	}

	if strings.TrimSpace(opts.Prompt) == "" && loadedState != nil && loadedState.Active {
		opts.Prompt = loadedState.Prompt
		opts.MinIterations = loadedState.MinIterations
		opts.MaxIterations = loadedState.MaxIterations
		opts.CompletionPromise = loadedState.CompletionPromise
		opts.AbortPromise = loadedState.AbortPromise
		opts.TasksMode = loadedState.TasksMode
		opts.TaskPromise = loadedState.TaskPromise
		opts.Rotation = loadedState.Rotation
		opts.PromptTemplate = loadedState.PromptTemplate
		opts.Model = loadedState.Model
		opts.Agent = loadedState.Agent
	}

	if opts.TasksMode && strings.TrimSpace(opts.CompletionPromise) == strings.TrimSpace(opts.TaskPromise) {
		fmt.Fprintln(os.Stderr, "Error: completion and task promises must be different in tasks mode")
		return 1
	}

	if opts.Agents == nil {
		defs, err := agent.LoadMerged("")
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error loading agent definitions:", err)
			return 1
		}
		opts.Agents = defs
	}

	if strings.TrimSpace(opts.Prompt) == "" {
		fmt.Fprintln(os.Stderr, "Error: No prompt provided")
		return 1
	}
	if opts.MinIterations < 1 {
		opts.MinIterations = 1
	}
	if opts.MaxIterations > 0 && opts.MinIterations > opts.MaxIterations {
		fmt.Fprintln(os.Stderr, "Error: --min-iterations cannot be greater than --max-iterations")
		return 1
	}
	runtimeRotation := opts.Rotation
	rotationActive := len(runtimeRotation) > 0
	if !rotationActive {
		def, ok := opts.Agents[opts.Agent]
		if !ok {
			fmt.Fprintln(os.Stderr, "Error: unsupported agent:", opts.Agent)
			return 1
		}
		if err := validateAgent(def); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			return 1
		}
	} else {
		for _, entry := range runtimeRotation {
			agentName, _, err := parseRotationEntry(entry)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				return 1
			}
			def, ok := opts.Agents[agentName]
			if !ok {
				fmt.Fprintln(os.Stderr, "Error: unsupported agent in rotation:", agentName)
				return 1
			}
			if err := validateAgent(def); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				return 1
			}
		}
	}

	if loadedState == nil || !loadedState.Active {
		_ = clearPendingApprovals(cwd)
		created := state.NewState(opts.Prompt, opts.CompletionPromise, opts.AbortPromise, opts.Model, opts.Agent, opts.MinIterations, opts.MaxIterations)
		created.TasksMode = opts.TasksMode
		created.TaskPromise = opts.TaskPromise
		created.Rotation = opts.Rotation
		created.PromptTemplate = opts.PromptTemplate
		if len(created.Rotation) > 0 {
			index := 0
			created.RotationIndex = &index
		}
		if err := state.SaveLoopState(cwd, created); err != nil {
			fmt.Fprintln(os.Stderr, "Error saving state:", err)
			return 1
		}
		loadedState = &created
	}

	permissionsEscalated := false

	if loadedState.TasksMode {
		createdPath, created, err := ensureTasksFile(cwd)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error preparing tasks file:", err)
			return 1
		}
		if created {
			fmt.Printf("Created tasks file: %s\n", createdPath)
		}
	}

	h, err := history.Load(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error loading history:", err)
		return 1
	}

	fmt.Println("Ralph Wiggum Loop (Go rewrite)")
	fmt.Printf("Task: %s\n", oneLinePreview(loadedState.Prompt, 80))
	fmt.Printf("Completion promise: %s\n", loadedState.CompletionPromise)
	fmt.Printf("Min iterations: %d\n", loadedState.MinIterations)
	if loadedState.MaxIterations > 0 {
		fmt.Printf("Max iterations: %d\n", loadedState.MaxIterations)
	} else {
		fmt.Println("Max iterations: unlimited")
	}
	printPluginWarnings(loadedState, opts)
	if opts.AllowAllPermissions {
		fmt.Println("Permissions: auto-approve all tools")
	}
	fmt.Println("Starting loop... (Ctrl+C to stop)")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	heartbeatInterval := 10 * time.Second
	if strings.EqualFold(os.Getenv("NODE_ENV"), "test") {
		heartbeatInterval = 1 * time.Second
	}

	var forced int32
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var currentProc atomic.Pointer[exec.Cmd]
	go func() {
		sigCount := 0
		for range sigCh {
			sigCount++
			if sigCount == 1 {
				fmt.Println("\nGracefully stopping Ralph loop...")
				cancel()
				if cmd := currentProc.Load(); cmd != nil && cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				_ = state.ClearLoopState(cwd)
				_ = clearPendingQuestions(cwd)
				_ = clearPendingApprovals(cwd)
			} else {
				fmt.Println("\nForce stopping...")
				atomic.StoreInt32(&forced, 1)
				if cmd := currentProc.Load(); cmd != nil && cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				_ = state.ClearLoopState(cwd)
				_ = clearPendingQuestions(cwd)
				_ = clearPendingApprovals(cwd)
				os.Exit(1)
			}
		}
	}()

	for {
		if loadedState.MaxIterations > 0 && loadedState.Iteration > loadedState.MaxIterations {
			fmt.Printf("Max iterations (%d) reached.\n", loadedState.MaxIterations)
			_ = state.ClearLoopState(cwd)
			_ = clearPendingQuestions(cwd)
			_ = clearPendingApprovals(cwd)
			return 0
		}
		if ctx.Err() != nil {
			if atomic.LoadInt32(&forced) == 1 {
				return 1
			}
			return 0
		}

		fmt.Printf("\nIteration %d\n", loadedState.Iteration)
		contextAtStart := loadContext(cwd)
		snapshotBefore := captureFileSnapshot(cwd)
		currentAgent, currentModel, rotationIndex, err := selectedAgentModel(loadedState)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			_ = state.ClearLoopState(cwd)
			return 1
		}
		def, ok := opts.Agents[currentAgent]
		if !ok {
			fmt.Fprintln(os.Stderr, "Error: unsupported agent:", currentAgent)
			_ = state.ClearLoopState(cwd)
			return 1
		}

		fullPrompt := buildLoopPrompt(cwd, loadedState)
		if def.Type == "mock" {
			// Mock agent runs prompt as a shell command; use raw prompt
			fullPrompt = loadedState.Prompt
		}
		if strings.TrimSpace(loadedState.PromptTemplate) != "" {
			rendered, err := renderPromptTemplate(cwd, loadedState)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error rendering prompt template:", err)
				_ = state.ClearLoopState(cwd)
				return 1
			}
			fullPrompt = rendered
		}

		effectiveAllowAll := opts.AllowAllPermissions
		if !effectiveAllowAll && hasApprovedMutatingPermission(cwd) {
			effectiveAllowAll = true
			if !permissionsEscalated {
				fmt.Println("Permissions: Ralph gate approved mutating tools for this run")
				permissionsEscalated = true
			}
		}

		spec, err := resolveAgentSpec(def, fullPrompt, currentModel, opts.ExtraAgentFlags, effectiveAllowAll, opts.StreamOutput)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			_ = state.ClearLoopState(cwd)
			return 1
		}

		iterCtx, iterCancel := context.WithCancel(ctx)
		cmd := exec.CommandContext(iterCtx, spec.Command, spec.Args...)
		runtimeOpts := opts
		runtimeOpts.AllowAllPermissions = effectiveAllowAll
		envVars, envErr := buildAgentEnv(cwd, def, runtimeOpts)
		if envErr != nil {
			iterCancel()
			fmt.Fprintln(os.Stderr, "Error preparing runtime environment:", envErr)
			_ = state.ClearLoopState(cwd)
			return 1
		}
		cmd.Env = envVars
		cmd.Dir = cwd
		currentProc.Store(cmd)

		stdoutPipe, err := cmd.StdoutPipe()
		if err != nil {
			iterCancel()
			fmt.Fprintln(os.Stderr, "Error opening stdout pipe:", err)
			_ = state.ClearLoopState(cwd)
			return 1
		}
		stderrPipe, err := cmd.StderrPipe()
		if err != nil {
			iterCancel()
			fmt.Fprintln(os.Stderr, "Error opening stderr pipe:", err)
			_ = state.ClearLoopState(cwd)
			return 1
		}
		cmd.Stdin = os.Stdin

		if err := cmd.Start(); err != nil {
			iterCancel()
			fmt.Fprintln(os.Stderr, "Error starting agent:", err)
			// Record failed iteration in history and continue
			iterationDuration := time.Since(time.Now()).Milliseconds()
			errorRecord := history.IterationRecord{
				Iteration:          loadedState.Iteration,
				StartedAt:          time.Now().UTC().Format(time.RFC3339),
				EndedAt:            time.Now().UTC().Format(time.RFC3339),
				DurationMs:         iterationDuration,
				Agent:              currentAgent,
				Model:              currentModel,
				ToolsUsed:          map[string]int{},
				FilesModified:      []string{},
				ExitCode:           -1,
				CompletionDetected: false,
				Errors:             []string{err.Error()},
			}
			history.UpdateWithIteration(&h, errorRecord)
			_ = history.Save(cwd, h)
			if len(loadedState.Rotation) > 0 {
				next := (rotationIndex + 1) % len(loadedState.Rotation)
				loadedState.RotationIndex = &next
			}
			loadedState.Iteration++
			_ = state.SaveLoopState(cwd, *loadedState)
			time.Sleep(2 * time.Second)
			continue
		}

		start := time.Now()
		startedAtISO := start.UTC().Format(time.RFC3339)
		var outBuf bytes.Buffer
		var errBuf bytes.Buffer
		activity := &atomic.Int64{}
		activity.Store(time.Now().UnixNano())
		liveTools := &toolSummaryState{
			counts:          map[string]int{},
			summaryInterval: 3 * time.Second,
		}

		var wg sync.WaitGroup
		wg.Add(2)
		go streamPipe(stdoutPipe, os.Stdout, &outBuf, opts.StreamOutput, opts.VerboseTools, def.ParsePattern, activity, liveTools, &wg)
		go streamPipe(stderrPipe, os.Stderr, &errBuf, opts.StreamOutput, opts.VerboseTools, def.ParsePattern, activity, liveTools, &wg)

		heartbeatDone := make(chan struct{})
		go heartbeatLoop(start, activity, heartbeatDone, heartbeatInterval)

		wg.Wait()
		waitErr := cmd.Wait()
		close(heartbeatDone)
		if opts.StreamOutput && !opts.VerboseTools {
			liveTools.flush(true)
		}
		iterCancel()
		currentProc.Store(nil)

		if ctx.Err() != nil {
			_ = state.ClearLoopState(cwd)
			if atomic.LoadInt32(&forced) == 1 {
				return 1
			}
			return 0
		}

		if !opts.StreamOutput {
			if stderrText := errBuf.String(); strings.TrimSpace(stderrText) != "" {
				fmt.Fprint(os.Stderr, stderrText)
			}
			if stdoutText := outBuf.String(); stdoutText != "" {
				fmt.Print(stdoutText)
			}
		}

		output := outBuf.String() + "\n" + errBuf.String()
		completionSignalDetected := completion.CheckTerminalPromise(outBuf.String(), loadedState.CompletionPromise)
		taskCompletionDetected := loadedState.TasksMode && completion.CheckTerminalPromise(outBuf.String(), loadedState.TaskPromise)
		completionDetected := completionSignalDetected
		if loadedState.TasksMode && completionSignalDetected {
			tasksGatePassed := false
			if tasksPayload, err := os.ReadFile(state.TasksPath(cwd)); err == nil {
				tasksGatePassed = completion.TasksMarkdownAllComplete(string(tasksPayload))
			}
			if !tasksGatePassed {
				completionDetected = false
				fmt.Fprintln(os.Stderr, "Completion promise ignored: tasks file still has incomplete items")
			}
		}
		abortDetected := loadedState.AbortPromise != "" && completion.CheckTerminalPromise(outBuf.String(), loadedState.AbortPromise)

		if waitErr != nil {
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				fmt.Fprintf(os.Stderr, "Agent exited with code %d, continuing...\n", exitErr.ExitCode())
			} else {
				fmt.Fprintln(os.Stderr, "Agent error:", waitErr)
			}
		}

		toolCounts := collectToolSummaryFromText(output, def.ParsePattern)
		if opts.StreamOutput {
			toolCounts = mergeToolCounts(toolCounts, liveTools.snapshot())
		}
		errorsDetected := extractErrors(output)
		iterDuration := time.Since(start).Milliseconds()
		snapshotAfter := captureFileSnapshot(cwd)
		filesModified := getModifiedFilesSinceSnapshot(snapshotBefore, snapshotAfter)
		record := history.IterationRecord{
			Iteration:          loadedState.Iteration,
			StartedAt:          startedAtISO,
			EndedAt:            time.Now().UTC().Format(time.RFC3339),
			DurationMs:         iterDuration,
			Agent:              currentAgent,
			Model:              currentModel,
			ToolsUsed:          toolCounts,
			FilesModified:      filesModified,
			ExitCode:           exitCodeFromError(waitErr),
			CompletionDetected: completionDetected,
			Errors:             errorsDetected,
		}
		history.UpdateWithIteration(&h, record)
		_ = history.Save(cwd, h)
		printIterationSummary(loadedState.Iteration, iterDuration, currentAgent, currentModel, toolCounts, exitCodeFromError(waitErr), completionDetected)
		if loadedState.Iteration > 2 && (h.StruggleIndicators.NoProgressIterations >= 3 || h.StruggleIndicators.ShortIterations >= 3) {
			fmt.Println("Potential struggle detected:")
			if h.StruggleIndicators.NoProgressIterations >= 3 {
				fmt.Printf("- No file changes in %d iterations\n", h.StruggleIndicators.NoProgressIterations)
			}
			if h.StruggleIndicators.ShortIterations >= 3 {
				fmt.Printf("- %d very short iterations (<30s)\n", h.StruggleIndicators.ShortIterations)
			}
			fmt.Println("Tip: run ralph --add-context \"hint\" from another terminal to steer the loop")
		}

		if currentAgent == "opencode" && detectPlaceholderPluginError(output) {
			fmt.Fprintln(os.Stderr, "\nOpenCode tried to load the legacy 'ralph-wiggum' plugin. This package is CLI-only.")
			fmt.Fprintln(os.Stderr, "Remove 'ralph-wiggum' from your opencode.json plugin list, or re-run with --no-plugins.")
			_ = state.ClearLoopState(cwd)
			_ = history.Clear(cwd)
			_ = clearPendingQuestions(cwd)
			_ = clearPendingApprovals(cwd)
			return 1
		}

		if detectModelNotFoundError(output) {
			fmt.Fprintln(os.Stderr, "\nModel configuration error detected.")
			fmt.Fprintln(os.Stderr, "The agent could not find a valid model to use.")
			if currentAgent == "opencode" {
				fmt.Fprintln(os.Stderr, "Set a default model in ~/.config/opencode/opencode.json or pass --model provider/model.")
			} else {
				fmt.Fprintf(os.Stderr, "Use --model with --agent %s, or configure a default model for that agent.\n", currentAgent)
			}
			_ = state.ClearLoopState(cwd)
			_ = history.Clear(cwd)
			_ = clearPendingQuestions(cwd)
			_ = clearPendingApprovals(cwd)
			return 1
		}

		if !opts.AllowAllPermissions {
			for _, toolName := range detectMutatingToolRequests(output, def.ParsePattern) {
				if isMutatingToolApproved(cwd, toolName) {
					continue
				}

				approved, promptErr := promptForToolApproval(toolName)
				if promptErr != nil {
					fmt.Fprintln(os.Stderr, "Permission gate error:", promptErr)
					fmt.Fprintln(os.Stderr, "Hint: rerun with --allow-all, or use an interactive terminal with --no-allow-all.")
					_ = state.ClearLoopState(cwd)
					_ = history.Clear(cwd)
					_ = clearPendingQuestions(cwd)
					_ = clearPendingApprovals(cwd)
					return 1
				}

				_ = savePendingApproval(cwd, toolName, approved)
				decision := "DENIED"
				if approved {
					decision = "APPROVED"
				}
				_ = appendContext(cwd, fmt.Sprintf("## Permission Decision\nTool: %s\nDecision: %s\n", toolName, decision))

				if !approved {
					fmt.Fprintf(os.Stderr, "Permission denied for mutating tool '%s'. Stopping current run.\n", toolName)
					_ = state.ClearLoopState(cwd)
					_ = history.Clear(cwd)
					_ = clearPendingQuestions(cwd)
					_ = clearPendingApprovals(cwd)
					return 1
				}
			}
		}

		if opts.HandleQuestions {
			if question := detectQuestionTool(output, def.ParsePattern); question != "" {
				answer, askErr := promptUser(question)
				if askErr == nil && strings.TrimSpace(answer) != "" {
					_ = savePendingQuestion(cwd, answer)
					_ = appendContext(cwd, "## Previous Answer\nYour previous answer was: "+answer+"\n")
				}
			} else if pendingAnswer, pendingErr := getAndClearPendingQuestion(cwd); pendingErr == nil && strings.TrimSpace(pendingAnswer) != "" {
				_ = appendContext(cwd, "## Previous Answer\nYour previous answer was: "+pendingAnswer+"\n")
			}
		}

		if abortDetected {
			fmt.Printf("Abort promise detected: <promise>%s</promise>\n", loadedState.AbortPromise)
			_ = state.ClearLoopState(cwd)
			_ = history.Clear(cwd)
			_ = clearContext(cwd)
			_ = clearPendingQuestions(cwd)
			_ = clearPendingApprovals(cwd)
			return 1
		}

		if taskCompletionDetected && !completionDetected {
			fmt.Printf("Task completion detected: <promise>%s</promise>\n", loadedState.TaskPromise)
		}

		if completionDetected && loadedState.Iteration >= loadedState.MinIterations {
			fmt.Printf("Completion promise detected: <promise>%s</promise>\n", loadedState.CompletionPromise)
			_ = state.ClearLoopState(cwd)
			_ = history.Clear(cwd)
			_ = clearContext(cwd)
			_ = clearPendingQuestions(cwd)
			_ = clearPendingApprovals(cwd)
			return 0
		}

		if strings.TrimSpace(output) == "" {
			fmt.Println("No output from agent in this iteration.")
		}

		if strings.TrimSpace(contextAtStart) != "" {
			fmt.Println("Context was consumed this iteration")
			_ = clearContext(cwd)
		}

		if len(loadedState.Rotation) > 0 {
			next := (rotationIndex + 1) % len(loadedState.Rotation)
			loadedState.RotationIndex = &next
			loadedState.Agent = currentAgent
			loadedState.Model = currentModel
		}

		if opts.AutoCommit {
			_ = tryAutoCommit(cwd, loadedState.Iteration)
		}

		loadedState.Iteration++
		if err := state.SaveLoopState(cwd, *loadedState); err != nil {
			fmt.Fprintln(os.Stderr, "Error saving state:", err)
			_ = state.ClearLoopState(cwd)
			return 1
		}

		// Brief pause between iterations
		iterDelay := 1 * time.Second
		if strings.EqualFold(os.Getenv("NODE_ENV"), "test") {
			iterDelay = 100 * time.Millisecond
		}
		time.Sleep(iterDelay)
	}
}

type pendingQuestion struct {
	Question  string `json:"question"`
	Timestamp string `json:"timestamp"`
}

type pendingApproval struct {
	Tool      string `json:"tool"`
	Approved  bool   `json:"approved"`
	Timestamp string `json:"timestamp"`
}

var mutatingToolNames = map[string]struct{}{
	"apply_patch":                     {},
	"bash":                            {},
	"create_and_run_task":             {},
	"create_file":                     {},
	"delete_file":                     {},
	"edit":                            {},
	"edit_file":                       {},
	"mcp_gitkraken_git_add_or_commit": {},
	"mcp_gitkraken_git_push":          {},
	"mcp_gitkraken_git_stash":         {},
	"rename":                          {},
	"rename_file":                     {},
	"run_in_terminal":                 {},
	"shell":                           {},
	"vscode_renamesymbol":             {},
	"write":                           {},
	"write_file":                      {},
}

var shellStyleActionPattern = regexp.MustCompile(`(?i)\((shell|bash)\)\s*$`)
var structuredTableToolPattern = regexp.MustCompile(`^\|\s{2}[A-Za-z0-9_-]+`)

func isMutatingTool(toolName string) bool {
	toolName = strings.ToLower(strings.TrimSpace(toolName))
	_, ok := mutatingToolNames[toolName]
	return ok
}

func detectMutatingToolRequests(output string, parsePattern string) []string {
	seen := map[string]struct{}{}
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if match := shellStyleActionPattern.FindStringSubmatch(trimmed); len(match) == 2 {
			candidate := strings.ToLower(strings.TrimSpace(match[1]))
			if isMutatingTool(candidate) {
				seen[candidate] = struct{}{}
			}
		}

		parsed := strings.ToLower(strings.TrimSpace(parseToolFromLineWithPattern(trimmed, parsePattern)))
		if parsed == "" || !isMutatingTool(parsed) {
			continue
		}

		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "tool:") || strings.HasPrefix(trimmed, "{") || structuredTableToolPattern.MatchString(trimmed) {
			seen[parsed] = struct{}{}
		}
	}

	tools := make([]string, 0, len(seen))
	for toolName := range seen {
		tools = append(tools, toolName)
	}
	sort.Strings(tools)
	return tools
}

func isInteractiveTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func promptForToolApproval(toolName string) (bool, error) {
	if !isInteractiveTerminal() {
		return false, errors.New("interactive permission prompts require a TTY")
	}

	prompt := fmt.Sprintf("Permission required for mutating tool '%s'.", toolName)
	answer, err := runSelectionPrompt(prompt, []string{"Approve", "Deny"})
	if err != nil {
		return false, err
	}

	normalized := strings.ToLower(strings.TrimSpace(answer))
	switch normalized {
	case "y", "yes", "approve", "approved", "allow":
		return true, nil
	case "n", "no", "deny", "denied", "":
		return false, nil
	default:
		fmt.Printf("Invalid response %q. Denying '%s'.\n", normalized, toolName)
		return false, nil
	}
}

func loadPendingApprovals(cwd string) ([]pendingApproval, error) {
	path := state.ApprovalsPath(cwd)
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []pendingApproval{}, nil
		}
		return nil, err
	}

	approvals := []pendingApproval{}
	if err := json.Unmarshal(payload, &approvals); err != nil {
		return []pendingApproval{}, nil
	}
	return approvals, nil
}

func savePendingApproval(cwd string, toolName string, approved bool) error {
	approvals, err := loadPendingApprovals(cwd)
	if err != nil {
		return err
	}
	approvals = append(approvals, pendingApproval{
		Tool:      strings.ToLower(strings.TrimSpace(toolName)),
		Approved:  approved,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	if err := state.EnsureDir(cwd); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(approvals, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(state.ApprovalsPath(cwd), payload, 0o644)
}

func isMutatingToolApproved(cwd string, toolName string) bool {
	approvals, err := loadPendingApprovals(cwd)
	if err != nil {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(toolName))
	for i := len(approvals) - 1; i >= 0; i-- {
		if approvals[i].Tool == normalized {
			return approvals[i].Approved
		}
	}
	return false
}

func hasApprovedMutatingPermission(cwd string) bool {
	approvals, err := loadPendingApprovals(cwd)
	if err != nil {
		return false
	}
	for _, approval := range approvals {
		if approval.Approved && isMutatingTool(approval.Tool) {
			return true
		}
	}
	return false
}

func clearPendingApprovals(cwd string) error {
	err := os.Remove(state.ApprovalsPath(cwd))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// loadPendingQuestions loads queued user answers from disk.
func loadPendingQuestions(cwd string) ([]pendingQuestion, error) {
	path := state.QuestionsPath(cwd)
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []pendingQuestion{}, nil
		}
		return nil, err
	}
	questions := []pendingQuestion{}
	if err := json.Unmarshal(payload, &questions); err != nil {
		return []pendingQuestion{}, nil
	}
	return questions, nil
}

// savePendingQuestion appends a user answer to the pending question queue.
func savePendingQuestion(cwd string, question string) error {
	questions, err := loadPendingQuestions(cwd)
	if err != nil {
		return err
	}
	questions = append(questions, pendingQuestion{Question: question, Timestamp: time.Now().UTC().Format(time.RFC3339)})
	if err := state.EnsureDir(cwd); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(questions, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(state.QuestionsPath(cwd), payload, 0o644)
}

// getAndClearPendingQuestion pops and returns the oldest queued answer.
func getAndClearPendingQuestion(cwd string) (string, error) {
	questions, err := loadPendingQuestions(cwd)
	if err != nil {
		return "", err
	}
	if len(questions) == 0 {
		return "", nil
	}
	first := strings.TrimSpace(questions[0].Question)
	remaining := questions[1:]
	if len(remaining) == 0 {
		if err := clearPendingQuestions(cwd); err != nil {
			return "", err
		}
		return first, nil
	}
	payload, err := json.MarshalIndent(remaining, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(state.QuestionsPath(cwd), payload, 0o644); err != nil {
		return "", err
	}
	return first, nil
}

// clearPendingQuestions removes the pending question queue file.
func clearPendingQuestions(cwd string) error {
	err := os.Remove(state.QuestionsPath(cwd))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// appendContext appends an entry to the loop context file, creating it if needed.
func appendContext(cwd string, entry string) error {
	if err := state.EnsureDir(cwd); err != nil {
		return err
	}
	path := state.ContextPath(cwd)
	payload, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		seed := "# Ralph Loop Context\n\n" + entry
		return os.WriteFile(path, []byte(seed), 0o644)
	}
	merged := string(payload) + "\n" + entry
	return os.WriteFile(path, []byte(merged), 0o644)
}

// loadContext returns trimmed context text, or an empty string when missing.
func loadContext(cwd string) string {
	payload, err := os.ReadFile(state.ContextPath(cwd))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(payload))
}

// clearContext removes the persisted context file.
func clearContext(cwd string) error {
	err := os.Remove(state.ContextPath(cwd))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// detectQuestionTool returns a detected question prompt from tool output.
func detectQuestionTool(output string, parsePattern string) string {
	lines := strings.Split(output, "\n")
	questionRegex := regexp.MustCompile(`(?i)(?:question|asking|please confirm|do you want|should i|can i)\s*[:\-]?\s*(.+)`)
	for _, line := range lines {
		if strings.EqualFold(parseToolFromLineWithPattern(line, parsePattern), "question") {
			qm := questionRegex.FindStringSubmatch(line)
			if len(qm) == 2 {
				return strings.TrimSpace(qm[1])
			}
			return "question detected"
		}
	}
	return ""
}

// promptUser asks for console input and returns the trimmed answer.
func promptUser(question string) (string, error) {
	if isInteractiveTerminal() {
		choices := detectQuestionOptions(question)
		return runSelectionPrompt("Question: "+question, choices)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("\nQuestion: %s\nYour answer: ", question)
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(answer), nil
}

// collectToolSummaryFromText counts detected tool names in text output.
func collectToolSummaryFromText(text string, parsePattern string) map[string]int {
	counts := map[string]int{}
	if strings.TrimSpace(text) == "" {
		return counts
	}
	for _, line := range strings.Split(text, "\n") {
		tool := parseToolFromLineWithPattern(line, parsePattern)
		if tool != "" {
			counts[tool] = counts[tool] + 1
		}
	}
	return counts
}

// parseToolFromLine parses a tool name with the default parser rules.
func parseToolFromLine(line string) string {
	return parseToolFromLineWithPattern(line, "default")
}

// parseToolFromLineWithPattern parses a tool name using parsePattern rules.
func parseToolFromLineWithPattern(line, parsePattern string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	pattern := strings.TrimSpace(parsePattern)
	if pattern == "" {
		pattern = "default"
	}
	patterns := []*regexp.Regexp{}
	if pattern == "opencode" {
		patterns = append(patterns, regexp.MustCompile(`^\|\s{2}([A-Za-z0-9_-]+)`))
	} else if pattern == "claude-code" {
		patterns = append(patterns,
			regexp.MustCompile(`(?i)(?:using|called|tool:)\s+([A-Za-z0-9_.-]+)`),
			regexp.MustCompile(`(?i)"name"\s*:\s*"([^\"]+)"`),
		)
	} else {
		patterns = append(patterns,
			regexp.MustCompile(`^\|\s{2}([A-Za-z0-9_-]+)`),
			regexp.MustCompile(`(?i)(?:tool:|using|calling|running)\s+([A-Za-z0-9_-]+)`),
		)
	}
	patterns = append(patterns,
		regexp.MustCompile(`(?i)"tool[_-]?name"\s*:\s*"([A-Za-z0-9_.-]+)"`),
		regexp.MustCompile(`(?i)"tool"\s*:\s*"([A-Za-z0-9_.-]+)"`),
	)
	for _, pattern := range patterns {
		match := pattern.FindStringSubmatch(trimmed)
		if len(match) == 2 {
			if strings.EqualFold(parsePattern, "claude-code") && strings.HasPrefix(trimmed, "{") {
				if strings.Contains(trimmed, `"type"`) && strings.Contains(trimmed, `"tool_use"`) {
					return strings.ToLower(strings.TrimSpace(match[1]))
				}
				continue
			}
			return strings.ToLower(strings.TrimSpace(match[1]))
		}
	}
	return ""
}

// detectPlaceholderPluginError checks for the legacy placeholder plugin error.
func detectPlaceholderPluginError(output string) bool {
	return strings.Contains(output, "ralph-wiggum is not yet ready for use. This is a placeholder package.")
}

// detectModelNotFoundError checks for model resolution failures in output.
func detectModelNotFoundError(output string) bool {
	return strings.Contains(output, "ProviderModelNotFoundError") ||
		strings.Contains(output, "Provider returned error") ||
		strings.Contains(strings.ToLower(output), "model not found") ||
		strings.Contains(output, "No model configured")
}

// extractErrors extracts unique error-like lines for history tracking.
func extractErrors(output string) []string {
	results := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error:") || strings.Contains(lower, "failed:") || strings.Contains(lower, "exception") || strings.Contains(lower, "typeerror") || strings.Contains(lower, "syntaxerror") || strings.Contains(lower, "referenceerror") || (strings.Contains(lower, "test") && strings.Contains(lower, "fail")) {
			clean := strings.TrimSpace(line)
			if clean == "" {
				continue
			}
			if len(clean) > 200 {
				clean = clean[:200]
			}
			if !seen[clean] {
				results = append(results, clean)
				seen[clean] = true
			}
		}
		if len(results) >= 10 {
			break
		}
	}
	return results
}

// exitCodeFromError returns an exit code when err wraps an ExitError.
func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

type fileSnapshot struct {
	files map[string]string
}

// captureFileSnapshot records hashes for tracked and changed files.
func captureFileSnapshot(cwd string) fileSnapshot {
	files := map[string]string{}
	tracked, trackedErr := gitLines(cwd, "ls-files")
	status, statusErr := gitLines(cwd, "status", "--porcelain")
	if trackedErr != nil && statusErr != nil {
		return fileSnapshot{files: files}
	}

	candidates := map[string]struct{}{}
	for _, file := range tracked {
		candidates[file] = struct{}{}
	}
	for _, line := range status {
		if file := parseStatusPath(line); file != "" {
			candidates[file] = struct{}{}
		}
	}

	for file := range candidates {
		hash, err := hashFile(filepath.Join(cwd, file))
		if err == nil {
			files[file] = hash
		}
	}
	return fileSnapshot{files: files}
}

// getModifiedFilesSinceSnapshot returns files changed between two snapshots.
func getModifiedFilesSinceSnapshot(before, after fileSnapshot) []string {
	changed := []string{}
	for file, hash := range after.files {
		if prev, ok := before.files[file]; !ok || prev != hash {
			changed = append(changed, file)
		}
	}
	for file := range before.files {
		if _, ok := after.files[file]; !ok {
			changed = append(changed, file)
		}
	}
	sort.Strings(changed)
	return changed
}

// gitLines executes git and returns trimmed non-empty output lines.
func gitLines(cwd string, args ...string) ([]string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	results := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			results = append(results, trimmed)
		}
	}
	return results, nil
}

// parseStatusPath extracts a path from a git porcelain status line.
func parseStatusPath(line string) string {
	if len(line) < 4 {
		return ""
	}
	path := strings.TrimSpace(line[3:])
	if path == "" {
		return ""
	}
	if strings.Contains(path, " -> ") {
		parts := strings.Split(path, " -> ")
		path = strings.TrimSpace(parts[len(parts)-1])
	}
	return path
}

// hashFile computes a SHA-1 digest for the file at path.
func hashFile(path string) (string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha1.Sum(payload)
	return fmt.Sprintf("%x", sum), nil
}

// buildAgentEnv builds the environment used to launch the agent process.
func buildAgentEnv(cwd string, def agent.Definition, opts Options) ([]string, error) {
	values := map[string]string{}
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}
	template := strings.TrimSpace(def.EnvTemplate)
	if template == "" {
		template = "default"
	}
	if template == "opencode" && (opts.DisablePlugins || opts.AllowAllPermissions) {
		path, err := ensureRalphConfig(cwd, opts.DisablePlugins, opts.AllowAllPermissions)
		if err != nil {
			return nil, err
		}
		values["OPENCODE_CONFIG"] = path
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result, nil
}

// ensureRalphConfig writes a run-scoped OpenCode config and returns its path.
func ensureRalphConfig(cwd string, filterPlugins bool, allowAllPermissions bool) (string, error) {
	if err := state.EnsureDir(cwd); err != nil {
		return "", err
	}
	configPath := filepath.Join(state.RalphDir(cwd), "ralph-opencode.config.json")

	xdgBase := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if xdgBase == "" {
		home, _ := os.UserHomeDir()
		xdgBase = filepath.Join(home, ".config")
	}
	userConfigPath := filepath.Join(xdgBase, "opencode", "opencode.json")
	projectConfigPath := filepath.Join(cwd, ".ralph", "opencode.json")
	legacyProjectConfigPath := filepath.Join(cwd, ".opencode", "opencode.json")

	config := map[string]any{
		"$schema": "https://opencode.ai/config.json",
	}

	if filterPlugins {
		plugins := append([]string{}, loadPluginsFromConfig(userConfigPath)...)
		plugins = append(plugins, loadPluginsFromConfig(projectConfigPath)...)
		plugins = append(plugins, loadPluginsFromConfig(legacyProjectConfigPath)...)
		unique := map[string]struct{}{}
		filtered := []string{}
		for _, plugin := range plugins {
			p := strings.TrimSpace(plugin)
			if p == "" || !regexp.MustCompile(`(?i)auth`).MatchString(p) {
				continue
			}
			if _, seen := unique[p]; seen {
				continue
			}
			unique[p] = struct{}{}
			filtered = append(filtered, p)
		}
		config["plugin"] = filtered
	}

	if allowAllPermissions {
		config["permission"] = map[string]string{
			"read":       "allow",
			"edit":       "allow",
			"glob":       "allow",
			"grep":       "allow",
			"list":       "allow",
			"bash":       "allow",
			"task":       "allow",
			"webfetch":   "allow",
			"websearch":  "allow",
			"codesearch": "allow",
			"todowrite":  "allow",
			"todoread":   "allow",
			"question":   "allow",
			"lsp":        "allow",
		}
	}

	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(configPath, payload, 0o644); err != nil {
		return "", err
	}
	return configPath, nil
}

// loadPluginsFromConfig reads plugin values from a config file.
func loadPluginsFromConfig(path string) []string {
	payload, err := os.ReadFile(path)
	if err != nil {
		return []string{}
	}
	text := string(payload)
	blockComment := regexp.MustCompile(`/\*[\s\S]*?\*/`)
	lineComment := regexp.MustCompile(`(?m)^\s*//.*$`)
	text = blockComment.ReplaceAllString(text, "")
	text = lineComment.ReplaceAllString(text, "")
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		return []string{}
	}
	raw, ok := data["plugin"]
	if !ok {
		return []string{}
	}
	list, ok := raw.([]any)
	if !ok {
		return []string{}
	}
	plugins := []string{}
	for _, item := range list {
		if s, ok := item.(string); ok {
			plugins = append(plugins, s)
		}
	}
	return plugins
}

// printPluginWarnings prints warnings related to plugin-filter behavior.
func printPluginWarnings(st *state.RalphState, opts Options) {
	if !opts.DisablePlugins {
		return
	}
	agents := []string{}
	if len(st.Rotation) > 0 {
		seen := map[string]struct{}{}
		for _, entry := range st.Rotation {
			agentName, _, err := parseRotationEntry(entry)
			if err != nil {
				continue
			}
			if _, ok := seen[agentName]; ok {
				continue
			}
			seen[agentName] = struct{}{}
			agents = append(agents, agentName)
		}
	} else {
		agents = append(agents, st.Agent)
	}
	for _, agentName := range agents {
		switch agentName {
		case "opencode":
			fmt.Println("OpenCode plugins: non-auth plugins disabled")
		case "claude-code":
			fmt.Println("Warning: --no-plugins has no effect with Claude Code agent")
		case "codex":
			fmt.Println("Warning: --no-plugins has no effect with Codex agent")
		case "copilot":
			fmt.Println("Warning: --no-plugins has no effect with Copilot CLI agent")
		}
	}
}

type toolSummaryState struct {
	mu              sync.Mutex
	counts          map[string]int
	lastPrintedAt   time.Time
	summaryInterval time.Duration
}

// record increments the count for tool and prints periodic summaries.
func (s *toolSummaryState) record(tool string) {
	if s == nil || tool == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[tool] = s.counts[tool] + 1
	now := time.Now()
	if s.lastPrintedAt.IsZero() || now.Sub(s.lastPrintedAt) >= s.summaryInterval {
		fmt.Printf("| Tools    %s\n", formatToolSummary(s.counts))
		s.lastPrintedAt = now
	}
}

// flush prints a tool summary when due or when force is true.
func (s *toolSummaryState) flush(force bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.counts) == 0 {
		return
	}
	now := time.Now()
	if force || s.lastPrintedAt.IsZero() || now.Sub(s.lastPrintedAt) >= s.summaryInterval {
		fmt.Printf("| Tools    %s\n", formatToolSummary(s.counts))
		s.lastPrintedAt = now
	}
}

// snapshot returns a copy of the current tool counters.
func (s *toolSummaryState) snapshot() map[string]int {
	if s == nil {
		return map[string]int{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copyMap := map[string]int{}
	for key, value := range s.counts {
		copyMap[key] = value
	}
	return copyMap
}

// mergeToolCounts merges two tool-count maps, keeping the higher count per tool.
func mergeToolCounts(a, b map[string]int) map[string]int {
	merged := map[string]int{}
	for key, value := range a {
		merged[key] = value
	}
	for key, value := range b {
		if value > merged[key] {
			merged[key] = value
		}
	}
	return merged
}

// formatToolSummary renders tool counts as sorted name(count) pairs.
func formatToolSummary(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	type pair struct {
		name  string
		count int
	}
	items := make([]pair, 0, len(counts))
	for name, count := range counts {
		items = append(items, pair{name: name, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].name < items[j].name
		}
		return items[i].count > items[j].count
	})
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("%s(%d)", item.name, item.count))
	}
	return strings.Join(parts, " ")
}

// printIterationSummary prints a human-readable summary of one iteration.
func printIterationSummary(iteration int, elapsedMs int64, agentName, model string, toolCounts map[string]int, exitCode int, completionDetected bool) {
	duration := formatDuration(time.Duration(elapsedMs) * time.Millisecond)
	fmt.Printf("Iteration %d completed in %s (%s / %s)\n", iteration, duration, agentName, model)
	fmt.Println("Iteration Summary")
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("Iteration: %d\n", iteration)
	fmt.Printf("Elapsed:   %s (%s / %s)\n", duration, agentName, model)
	if summary := formatToolSummary(toolCounts); summary != "" {
		fmt.Printf("Tools:     %s\n", summary)
	} else {
		fmt.Println("Tools:     none")
	}
	fmt.Printf("Exit code: %d\n", exitCode)
	if completionDetected {
		fmt.Println("Completion promise: detected")
	} else {
		fmt.Println("Completion promise: not detected")
	}
}

// tryAutoCommit stages all changes and creates an iteration commit when needed.
func tryAutoCommit(cwd string, iteration int) error {
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = cwd
	statusOut, err := statusCmd.Output()
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(statusOut)) == "" {
		return nil
	}
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = cwd
	if err := addCmd.Run(); err != nil {
		return err
	}
	commitCmd := exec.Command("git", "commit", "-m", fmt.Sprintf("Ralph iteration %d: work in progress", iteration))
	commitCmd.Dir = cwd
	return commitCmd.Run()
}

// ensureTasksFile ensures the tasks file exists and seeds it if missing.
func ensureTasksFile(cwd string) (string, bool, error) {
	if err := state.EnsureDir(cwd); err != nil {
		return "", false, err
	}
	path := state.TasksPath(cwd)
	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	seed := "# Ralph Tasks\n\nAdd your tasks below using: `ralph --add-task \"description\"`\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		return "", false, err
	}
	return path, true, nil
}

// loopTask mirrors the task type in main.go for prompt building.
type loopTask struct {
	text     string
	status   string
	subtasks []loopTask
}

// parseLoopTasks parses markdown tasks into loopTask structures.
func parseLoopTasks(content string) []loopTask {
	mainPat := regexp.MustCompile(`^- \[([ xX/])\]\s*(.+)$`)
	subPat := regexp.MustCompile(`^\s+- \[([ xX/])\]\s*(.+)$`)
	var tasks []loopTask
	var current *loopTask
	for _, line := range strings.Split(content, "\n") {
		if m := mainPat.FindStringSubmatch(line); len(m) == 3 {
			if current != nil {
				tasks = append(tasks, *current)
			}
			current = &loopTask{text: m[2], status: taskStatus(m[1])}
			continue
		}
		if m := subPat.FindStringSubmatch(line); len(m) == 3 && current != nil {
			current.subtasks = append(current.subtasks, loopTask{text: m[2], status: taskStatus(m[1])})
		}
	}
	if current != nil {
		tasks = append(tasks, *current)
	}
	return tasks
}

// taskStatus converts a markdown marker to an internal task status.
func taskStatus(marker string) string {
	switch strings.ToLower(marker) {
	case "x":
		return "complete"
	case "/":
		return "in-progress"
	default:
		return "todo"
	}
}

// findCurrentTask returns the first task marked in-progress.
func findCurrentTask(tasks []loopTask) *loopTask {
	for i := range tasks {
		if tasks[i].status == "in-progress" {
			return &tasks[i]
		}
	}
	return nil
}

// findNextTask returns the first task marked todo.
func findNextTask(tasks []loopTask) *loopTask {
	for i := range tasks {
		if tasks[i].status == "todo" {
			return &tasks[i]
		}
	}
	return nil
}

// allTasksComplete reports whether all tasks and subtasks are complete.
func allTasksComplete(tasks []loopTask) bool {
	if len(tasks) == 0 {
		return false
	}
	for _, t := range tasks {
		if t.status != "complete" {
			return false
		}
		for _, st := range t.subtasks {
			if st.status != "complete" {
				return false
			}
		}
	}
	return true
}

// getTasksModeSection builds the tasks-mode prompt section from tasks state.
func getTasksModeSection(cwd string, st *state.RalphState) string {
	tasksPayload, err := os.ReadFile(state.TasksPath(cwd))
	if err != nil {
		return fmt.Sprintf(`
## TASKS MODE: Enabled (no tasks file found)

Create .ralph/ralph-tasks.md with your task list, or use %sralph --add-task "description"%s to add tasks.
`, "`", "`")
	}

	tasksContent := string(tasksPayload)
	tasks := parseLoopTasks(tasksContent)
	currentTask := findCurrentTask(tasks)
	nextTask := findNextTask(tasks)

	var taskInstructions string
	if currentTask != nil {
		taskInstructions = fmt.Sprintf(`
CURRENT TASK: "%s"
   Focus on completing this specific task.
   When done: Mark as [x] in .ralph/ralph-tasks.md and output <promise>%s</promise>`, currentTask.text, st.TaskPromise)
	} else if nextTask != nil {
		taskInstructions = fmt.Sprintf(`
NEXT TASK: "%s"
   Mark as [/] in .ralph/ralph-tasks.md before starting.
   When done: Mark as [x] and output <promise>%s</promise>`, nextTask.text, st.TaskPromise)
	} else if allTasksComplete(tasks) {
		taskInstructions = fmt.Sprintf(`
ALL TASKS COMPLETE!
   Output <promise>%s</promise> to finish.`, st.CompletionPromise)
	} else {
		taskInstructions = "\nNo tasks found. Add tasks to .ralph/ralph-tasks.md or use `ralph --add-task`"
	}

	return fmt.Sprintf(`
## TASKS MODE: Working through task list

Current tasks from .ralph/ralph-tasks.md:
%s%s%s
%s

### Task Workflow
1. Find any task marked [/] (in progress). If none, pick the first [ ] task.
2. Mark the task as [/] in ralph-tasks.md before starting.
3. Complete the task.
4. Mark as [x] when verified complete.
5. Output <promise>%s</promise> to move to the next task.
6. Only output <promise>%s</promise> when ALL tasks are [x].

---
`, "```markdown\n", strings.TrimSpace(tasksContent), "\n```", taskInstructions, st.TaskPromise, st.CompletionPromise)
}

// buildLoopPrompt builds the per-iteration prompt for normal or tasks mode.
func buildLoopPrompt(cwd string, st *state.RalphState) string {
	context := ""
	if contextPayload, err := os.ReadFile(state.ContextPath(cwd)); err == nil {
		context = strings.TrimSpace(string(contextPayload))
	}
	contextSection := ""
	if context != "" {
		contextSection = fmt.Sprintf("\n## Additional Context (added by user mid-loop)\n\n%s\n\n---\n", context)
	}

	iterDisplay := fmt.Sprintf("%d", st.Iteration)
	if st.MaxIterations > 0 {
		iterDisplay += fmt.Sprintf(" / %d", st.MaxIterations)
	} else {
		iterDisplay += " (unlimited)"
	}
	minDisplay := ""
	if st.MinIterations > 1 {
		minDisplay = fmt.Sprintf(" (min: %d)", st.MinIterations)
	}

	if st.TasksMode {
		tasksSection := getTasksModeSection(cwd, st)
		return strings.TrimSpace(fmt.Sprintf(`# Ralph Wiggum Loop - Iteration %d

You are in an iterative development loop working through a task list.
%s%s
## Your Main Goal

%s

## Critical Rules

- Work on ONE task at a time from .ralph/ralph-tasks.md
- ONLY output <promise>%s</promise> when the current task is complete and marked in ralph-tasks.md
- ONLY output <promise>%s</promise> when ALL tasks are truly done
- Output promise tags DIRECTLY - do not quote them, explain them, or say you "will" output them
- Do NOT lie or output false promises to exit the loop
- If stuck, try a different approach
- Check your work before claiming completion

## Current Iteration: %s%s

Tasks Mode: ENABLED - Work on one task at a time from ralph-tasks.md

Now, work on the current task. Good luck!`,
			st.Iteration, contextSection, tasksSection, st.Prompt,
			st.TaskPromise, st.CompletionPromise, iterDisplay, minDisplay))
	}

	return strings.TrimSpace(fmt.Sprintf(`# Ralph Wiggum Loop - Iteration %d

You are in an iterative development loop. Work on the task below until you can genuinely complete it.
%s
## Your Task

%s

## Instructions

1. Read the current state of files to understand what's been done
2. Track your progress and plan remaining work
3. Make progress on the task
4. Run tests/verification if applicable
5. When the task is GENUINELY COMPLETE, output:
   <promise>%s</promise>

## Critical Rules

- ONLY output <promise>%s</promise> when the task is truly done
- Output the promise tag DIRECTLY - do not quote it, explain it, or say you "will" output it
- Do NOT lie or output false promises to exit the loop
- If stuck, try a different approach
- Check your work before claiming completion
- The loop will continue until you succeed

## Current Iteration: %s%s

Now, work on the task. Good luck!`,
		st.Iteration, contextSection, st.Prompt,
		st.CompletionPromise, st.CompletionPromise, iterDisplay, minDisplay))
}

// selectedAgentModel returns the active agent/model and rotation index.
func selectedAgentModel(st *state.RalphState) (string, string, int, error) {
	if len(st.Rotation) == 0 {
		return st.Agent, st.Model, 0, nil
	}
	idx := 0
	if st.RotationIndex != nil {
		idx = *st.RotationIndex
	}
	idx = ((idx % len(st.Rotation)) + len(st.Rotation)) % len(st.Rotation)
	agentName, model, err := parseRotationEntry(st.Rotation[idx])
	if err != nil {
		return "", "", idx, err
	}
	return agentName, model, idx, nil
}

// parseRotationEntry parses a single rotation entry in agent:model form.
func parseRotationEntry(entry string) (string, string, error) {
	parts := strings.Split(entry, ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid rotation entry: %s", entry)
	}
	agentName := strings.TrimSpace(parts[0])
	model := strings.TrimSpace(parts[1])
	if agentName == "" || model == "" {
		return "", "", fmt.Errorf("invalid rotation entry: %s", entry)
	}
	return agentName, model, nil
}

// renderPromptTemplate loads and renders a prompt template with runtime values.
func renderPromptTemplate(cwd string, st *state.RalphState) (string, error) {
	path := strings.TrimSpace(st.PromptTemplate)
	if path == "" {
		return st.Prompt, nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(payload)
	context := ""
	if contextPayload, err := os.ReadFile(state.ContextPath(cwd)); err == nil {
		context = strings.TrimSpace(string(contextPayload))
	}
	tasks := ""
	if tasksPayload, err := os.ReadFile(state.TasksPath(cwd)); err == nil {
		tasks = string(tasksPayload)
	}
	repl := map[string]string{
		"{{iteration}}":          fmt.Sprintf("%d", st.Iteration),
		"{{max_iterations}}":     maxIterationsDisplay(st.MaxIterations),
		"{{min_iterations}}":     fmt.Sprintf("%d", st.MinIterations),
		"{{prompt}}":             st.Prompt,
		"{{completion_promise}}": st.CompletionPromise,
		"{{abort_promise}}":      st.AbortPromise,
		"{{task_promise}}":       st.TaskPromise,
		"{{context}}":            context,
		"{{tasks}}":              tasks,
	}
	for key, value := range repl {
		content = strings.ReplaceAll(content, key, value)
	}
	return content, nil
}

// maxIterationsDisplay formats max iterations for template rendering.
func maxIterationsDisplay(max int) string {
	if max > 0 {
		return fmt.Sprintf("%d", max)
	}
	return "unlimited"
}

// extractClaudeStreamDisplayLines parses Claude Code's stream-json output
// and extracts human-readable display lines. Non-JSON lines are returned as-is.
func extractClaudeStreamDisplayLines(rawLine string) []string {
	cleanLine := strings.TrimSpace(completion.StripANSI(rawLine))
	if !strings.HasPrefix(cleanLine, "{") {
		return []string{rawLine}
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(cleanLine), &payload); err != nil {
		return []string{rawLine}
	}

	var lines []string
	addText := func(val any) {
		s, ok := val.(string)
		if !ok || s == "" {
			return
		}
		for _, part := range strings.Split(s, "\n") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				lines = append(lines, trimmed)
			}
		}
	}
	addContentText := func(content any) {
		if s, ok := content.(string); ok {
			addText(s)
			return
		}
		arr, ok := content.([]any)
		if !ok {
			return
		}
		for _, block := range arr {
			blockMap, ok := block.(map[string]any)
			if !ok {
				continue
			}
			if blockMap["type"] == "tool_use" {
				continue
			}
			addText(blockMap["text"])
			addText(blockMap["thinking"])
			if s, ok := blockMap["content"].(string); ok {
				addText(s)
			}
		}
	}

	payloadType, _ := payload["type"].(string)
	switch payloadType {
	case "assistant":
		if msg, ok := payload["message"].(map[string]any); ok {
			addContentText(msg["content"])
		}
		if delta, ok := payload["delta"].(map[string]any); ok {
			addText(delta["text"])
			addText(delta["thinking"])
			addText(delta["content"])
		}
	case "result":
		addText(payload["result"])
	case "error":
		if errMap, ok := payload["error"].(map[string]any); ok {
			addText(errMap["message"])
		} else {
			addText(payload["error"])
		}
	}

	return lines
}

// streamPipe forwards stream output, captures text, and tracks tool usage.
func streamPipe(reader io.Reader, writer io.Writer, capture *bytes.Buffer, stream bool, verboseTools bool, parsePattern string, activity *atomic.Int64, tools *toolSummaryState, wg *sync.WaitGroup) {
	defer wg.Done()
	isClaudeStream := strings.EqualFold(strings.TrimSpace(parsePattern), "claude-code")
	processLine := func(line string) {
		capture.WriteString(line)
		capture.WriteString("\n")
		activity.Store(time.Now().UnixNano())
		if !stream {
			return
		}
		tool := parseToolFromLineWithPattern(line, parsePattern)
		if tool != "" {
			tools.record(tool)
			if !verboseTools {
				return
			}
		}
		if isClaudeStream {
			for _, displayLine := range extractClaudeStreamDisplayLines(line) {
				fmt.Fprintln(writer, displayLine)
			}
		} else {
			fmt.Fprintln(writer, line)
		}
	}

	breader := bufio.NewReader(reader)
	var lineBuf bytes.Buffer
	for {
		chunk, err := breader.ReadSlice('\n')
		if len(chunk) > 0 {
			activity.Store(time.Now().UnixNano())
			lineBuf.Write(chunk)
		}

		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}

		if lineBuf.Len() > 0 {
			line := strings.TrimSuffix(lineBuf.String(), "\n")
			line = strings.TrimSuffix(line, "\r")
			processLine(line)
			lineBuf.Reset()
		}

		if err == nil {
			continue
		}

		if errors.Is(err, io.EOF) {
			return
		}

		if errors.Is(err, os.ErrClosed) {
			return
		}

		fmt.Fprintln(os.Stderr, "Error reading stream output:", err)
		return
	}
}

// heartbeatLoop prints elapsed progress until done is closed.
func heartbeatLoop(iterationStart time.Time, activity *atomic.Int64, done <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			now := time.Now()
			last := time.Unix(0, activity.Load())
			fmt.Printf("⏳ working... elapsed %s · last activity %s ago\n", formatDuration(now.Sub(iterationStart)), formatDuration(now.Sub(last)))
		}
	}
}

// formatDuration formats a duration as mm:ss or hh:mm:ss.
func formatDuration(d time.Duration) string {
	seconds := int(d.Seconds())
	if seconds < 0 {
		seconds = 0
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%d:%02d", minutes, secs)
}

// oneLinePreview collapses whitespace and truncates to max characters.
func oneLinePreview(s string, max int) string {
	trimmed := strings.Join(strings.Fields(s), " ")
	if len(trimmed) <= max {
		return trimmed
	}
	if max <= 3 {
		return trimmed[:max]
	}
	return trimmed[:max-3] + "..."
}
