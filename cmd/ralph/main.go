package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Th0rgal/open-ralph-wiggum/internal/agent"
	"github.com/Th0rgal/open-ralph-wiggum/internal/history"
	"github.com/Th0rgal/open-ralph-wiggum/internal/loop"
	"github.com/Th0rgal/open-ralph-wiggum/internal/state"
)

const version = "1.2.2-go-pre"

// main exits with the status code returned by run.
func main() {
	os.Exit(run(os.Args[1:]))
}

type cliConfig struct {
	agent             string
	model             string
	minIterations     int
	maxIterations     int
	completionPromise string
	abortPromise      string
	taskPromise       string
	rotationInput     string
	rotation          []string
	promptTemplate    string
	configPath        string
	initConfigPath    string
	promptFile        string
	promptParts       []string
	extraAgentFlags   []string

	tasksMode           bool
	status              bool
	statusTasks         bool
	version             bool
	help                bool
	streamOutput        bool
	verboseTools        bool
	handleQuestions     bool
	disablePlugins      bool
	autoCommit          bool
	allowAllPermissions bool

	addContext      string
	clearContext    bool
	listTasks       bool
	addTask         string
	removeTaskIndex int
}

// run parses arguments, handles one-shot commands, and starts the loop.
func run(args []string) int {
	cfg, err := parseCLIArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		fmt.Fprintln(os.Stderr, "Run 'ralph --help' for available options")
		return 1
	}

	if cfg.help {
		printHelp()
		return 0
	}
	if cfg.version {
		fmt.Printf("ralph %s\n", version)
		return 0
	}
	if cfg.initConfigPath != "" {
		if err := agent.WriteDefaultConfig(cfg.initConfigPath); err != nil {
			fmt.Fprintln(os.Stderr, "Error writing default config:", err)
			return 1
		}
		target := cfg.initConfigPath
		if strings.TrimSpace(target) == "" {
			target = agent.DefaultConfigPath()
		}
		fmt.Println("Created default agent config at:", target)
		return 0
	}

	if cfg.addContext != "" {
		return addContext(cfg.addContext)
	}
	if cfg.clearContext {
		return clearContext()
	}
	if cfg.listTasks {
		return listTasks()
	}
	if cfg.addTask != "" {
		return addTask(cfg.addTask)
	}
	if cfg.removeTaskIndex > 0 {
		return removeTask(cfg.removeTaskIndex)
	}
	if cfg.status {
		return printStatus(cfg.statusTasks)
	}

	prompt := strings.TrimSpace(strings.Join(cfg.promptParts, " "))
	if cfg.promptFile != "" {
		payload, err := os.ReadFile(cfg.promptFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error reading prompt file:", err)
			return 1
		}
		prompt = strings.TrimSpace(string(payload))
	} else if len(cfg.promptParts) == 1 {
		// Auto-detect: if the single positional arg is an existing file, read it as prompt
		if info, err := os.Stat(cfg.promptParts[0]); err == nil && !info.IsDir() {
			payload, err := os.ReadFile(cfg.promptParts[0])
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error reading prompt file:", err)
				return 1
			}
			prompt = strings.TrimSpace(string(payload))
		}
	}

	agents, err := agent.LoadMerged(cfg.configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error loading agent config:", err)
		return 1
	}

	rotation, err := parseRotationInput(cfg.rotationInput, agents)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}

	if len(rotation) == 0 {
		if _, ok := agents[cfg.agent]; !ok {
			fmt.Fprintln(os.Stderr, "Error: --agent requires one of:", strings.Join(agentKeys(agents), ", "))
			return 1
		}
	} else {
		cfg.rotation = rotation
	}

	opts := loop.Options{
		Prompt:              prompt,
		MinIterations:       cfg.minIterations,
		MaxIterations:       cfg.maxIterations,
		CompletionPromise:   cfg.completionPromise,
		AbortPromise:        cfg.abortPromise,
		TasksMode:           cfg.tasksMode,
		TaskPromise:         cfg.taskPromise,
		Rotation:            cfg.rotation,
		PromptTemplate:      cfg.promptTemplate,
		Model:               strings.TrimSpace(cfg.model),
		Agent:               strings.TrimSpace(cfg.agent),
		Agents:              agents,
		StreamOutput:        cfg.streamOutput,
		VerboseTools:        cfg.verboseTools,
		HandleQuestions:     cfg.handleQuestions,
		DisablePlugins:      cfg.disablePlugins,
		AutoCommit:          cfg.autoCommit,
		AllowAllPermissions: cfg.allowAllPermissions,
		ExtraAgentFlags:     cfg.extraAgentFlags,
	}
	if opts.Agent == "" {
		opts.Agent = "opencode"
	}

	return loop.Run(opts)
}

// parseCLIArgs parses command-line flags and positional prompt arguments.
func parseCLIArgs(args []string) (cliConfig, error) {
	cfg := cliConfig{
		agent:               "copilot",
		minIterations:       1,
		maxIterations:       0,
		completionPromise:   "COMPLETE",
		taskPromise:         "READY_FOR_NEXT_TASK",
		streamOutput:        true,
		verboseTools:        false,
		handleQuestions:     true,
		disablePlugins:      false,
		autoCommit:          true,
		allowAllPermissions: false,
		configPath:          agent.DefaultConfigPath(),
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				cfg.extraAgentFlags = append(cfg.extraAgentFlags, args[i+1:]...)
			}
			break
		}

		nextValue := func(flagName string) (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", flagName)
			}
			i++
			if strings.TrimSpace(args[i]) == "" {
				return "", fmt.Errorf("%s requires a value", flagName)
			}
			return args[i], nil
		}

		switch arg {
		case "--help", "-h":
			cfg.help = true
		case "--version", "-v":
			cfg.version = true
		case "--status":
			cfg.status = true
		case "--tasks", "-t":
			cfg.tasksMode = true
			cfg.statusTasks = true
		case "--agent":
			v, err := nextValue("--agent")
			if err != nil {
				return cfg, err
			}
			cfg.agent = v
		case "--model":
			v, err := nextValue("--model")
			if err != nil {
				return cfg, err
			}
			cfg.model = v
		case "--min-iterations":
			v, err := nextValue("--min-iterations")
			if err != nil {
				return cfg, err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return cfg, errors.New("--min-iterations requires a number")
			}
			cfg.minIterations = n
		case "--max-iterations":
			v, err := nextValue("--max-iterations")
			if err != nil {
				return cfg, err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return cfg, errors.New("--max-iterations requires a number")
			}
			cfg.maxIterations = n
		case "--completion-promise":
			v, err := nextValue("--completion-promise")
			if err != nil {
				return cfg, err
			}
			cfg.completionPromise = v
		case "--abort-promise":
			v, err := nextValue("--abort-promise")
			if err != nil {
				return cfg, err
			}
			cfg.abortPromise = v
		case "--task-promise":
			v, err := nextValue("--task-promise")
			if err != nil {
				return cfg, err
			}
			cfg.taskPromise = v
		case "--rotation":
			v, err := nextValue("--rotation")
			if err != nil {
				return cfg, err
			}
			cfg.rotationInput = v
		case "--prompt-template":
			v, err := nextValue("--prompt-template")
			if err != nil {
				return cfg, err
			}
			cfg.promptTemplate = v
		case "--config":
			v, err := nextValue("--config")
			if err != nil {
				return cfg, err
			}
			cfg.configPath = v
		case "--init-config":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				cfg.initConfigPath = args[i]
			} else {
				cfg.initConfigPath = cfg.configPath
			}
		case "--prompt-file", "--file", "-f":
			v, err := nextValue(arg)
			if err != nil {
				return cfg, err
			}
			cfg.promptFile = v
		case "--no-stream":
			cfg.streamOutput = false
		case "--stream":
			cfg.streamOutput = true
		case "--verbose-tools":
			cfg.verboseTools = true
		case "--questions":
			cfg.handleQuestions = true
		case "--no-questions":
			cfg.handleQuestions = false
		case "--no-plugins":
			cfg.disablePlugins = true
		case "--no-commit":
			cfg.autoCommit = false
		case "--allow-all":
			cfg.allowAllPermissions = true
		case "--no-allow-all":
			cfg.allowAllPermissions = false
		case "--add-context":
			v, err := nextValue("--add-context")
			if err != nil {
				return cfg, err
			}
			cfg.addContext = v
		case "--clear-context":
			cfg.clearContext = true
		case "--list-tasks":
			cfg.listTasks = true
		case "--add-task":
			v, err := nextValue("--add-task")
			if err != nil {
				return cfg, err
			}
			cfg.addTask = v
		case "--remove-task":
			v, err := nextValue("--remove-task")
			if err != nil {
				return cfg, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return cfg, errors.New("--remove-task requires a positive number")
			}
			cfg.removeTaskIndex = n
		default:
			if strings.HasPrefix(arg, "-") {
				return cfg, fmt.Errorf("unknown option: %s", arg)
			}
			cfg.promptParts = append(cfg.promptParts, arg)
		}
	}

	return cfg, nil
}

// parseRotationInput parses and validates rotation entries in agent:model form.
func parseRotationInput(raw string, agents map[string]agent.Definition) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	parsed := make([]string, 0, len(parts))
	for _, entry := range parts {
		trimmed := strings.TrimSpace(entry)
		segments := strings.Split(trimmed, ":")
		if len(segments) != 2 {
			return nil, fmt.Errorf("invalid rotation entry %q (expected agent:model)", trimmed)
		}
		agentName := strings.TrimSpace(segments[0])
		model := strings.TrimSpace(segments[1])
		if agentName == "" || model == "" {
			return nil, fmt.Errorf("invalid rotation entry %q (both agent and model are required)", trimmed)
		}
		if _, ok := agents[agentName]; !ok {
			return nil, fmt.Errorf("invalid agent %q in rotation entry %q", agentName, trimmed)
		}
		parsed = append(parsed, agentName+":"+model)
	}
	return parsed, nil
}

// agentKeys returns the available agent keys from agents.
func agentKeys(agents map[string]agent.Definition) []string {
	keys := make([]string, 0, len(agents))
	for key := range agents {
		keys = append(keys, key)
	}
	return keys
}

// printHelp writes CLI usage and option help text.
func printHelp() {
	fmt.Print(`Ralph Wiggum Loop - Go rewrite

Usage:
  ralph "<prompt>" [options]
  ralph --prompt-file <path> [options]

Options:
  --agent AGENT                AI agent: opencode, claude-code, codex, copilot (default)
  --model MODEL                Model name for selected agent
  --min-iterations N           Minimum iterations before completion (default: 1)
  --max-iterations N           Maximum iterations (default: unlimited)
  --completion-promise TEXT    Promise tag content used for completion (default: COMPLETE)
  --abort-promise TEXT         Promise tag content used for abort
	--tasks, -t                  Enable tasks mode and show tasks in status
	--task-promise TEXT          Promise tag content used for task completion
	--rotation LIST              Agent/model rotation entries: "agent:model,agent:model"
	--prompt-template PATH       Use custom prompt template file
	--config PATH                Use custom agent config file
	--init-config [PATH]         Write default agent config and exit
	--prompt-file, --file, -f    Read prompt from file
	--status                     Show current loop status and optional tasks
	--verbose-tools              Print every tool line (disable compact summary)
	--questions                  Enable interactive question handling (default)
	--no-questions               Disable interactive question handling
	--no-plugins                 Disable non-auth OpenCode plugins for this run
	--no-commit                  Disable auto-commit after iterations
	--allow-all                  Auto-approve tool permissions
	--no-allow-all               Require Ralph interactive approval for mutating tools (default)
	--add-context TEXT           Add context for next iteration
	--clear-context              Clear pending context
	--list-tasks                 Show tasks with indices
	--add-task "desc"            Add a task to task list
	--remove-task N              Remove task by index
  --no-stream                  Buffer output until iteration ends
	--                           Pass remaining args to agent CLI
  --version, -v                Show version
  --help, -h                   Show this help
`)
}

type task struct {
	text         string
	status       string
	subtasks     []task
	originalLine string
}

// printStatus prints loop state, optional tasks, and recent history summary.
func printStatus(showTasks bool) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error getting working directory:", err)
		return 1
	}
	st, err := state.LoadLoopState(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error reading state:", err)
		return 1
	}
	if st == nil || !st.Active {
		fmt.Println("No active loop")
	} else {
		fmt.Println("Ralph Status")
		fmt.Printf("Active: true\n")
		fmt.Printf("Iteration: %d\n", st.Iteration)
		fmt.Printf("Agent: %s\n", st.Agent)
		if st.Model != "" {
			fmt.Printf("Model: %s\n", st.Model)
		}
		fmt.Printf("Started: %s\n", st.StartedAt)
		if startedAt, parseErr := time.Parse(time.RFC3339, st.StartedAt); parseErr == nil {
			fmt.Printf("Elapsed: %s\n", formatDurationLong(time.Since(startedAt).Milliseconds()))
		}
		if len(st.Rotation) > 0 {
			idx := 0
			if st.RotationIndex != nil {
				idx = *st.RotationIndex
			}
			idx = ((idx % len(st.Rotation)) + len(st.Rotation)) % len(st.Rotation)
			fmt.Printf("Rotation (position %d/%d):\n", idx+1, len(st.Rotation))
			for i, entry := range st.Rotation {
				label := ""
				if i == idx {
					label = " **ACTIVE**"
				}
				fmt.Printf("- %s%s\n", entry, label)
			}
		}
		fmt.Printf("Completion promise: %s\n", st.CompletionPromise)
		if st.TasksMode {
			showTasks = true
			fmt.Println("Tasks mode: enabled")
		}
	}

	contextPath := state.ContextPath(cwd)
	if payload, err := os.ReadFile(contextPath); err == nil {
		trimmed := strings.TrimSpace(string(payload))
		if trimmed != "" {
			fmt.Println("\nPending context:")
			fmt.Println(trimmed)
		}
	}

	if showTasks {
		if code := printTasksFromFile(cwd); code != 0 {
			return code
		}
	}

	h, err := history.Load(cwd)
	if err == nil && len(h.Iterations) > 0 {
		fmt.Printf("\nHistory (%d iterations)\n", len(h.Iterations))
		fmt.Printf("Total time: %s\n", formatDurationLong(h.TotalDurationMs))
		recent := h.Iterations
		if len(recent) > 5 {
			recent = recent[len(recent)-5:]
		}
		for _, iter := range recent {
			fmt.Printf("#%d %s %s / %s %s\n", iter.Iteration, formatDurationLong(iter.DurationMs), iter.Agent, iter.Model, history.TopTools(iter.ToolsUsed, 3))
		}
		struggle := h.StruggleIndicators
		hasRepeatedErrors := false
		for _, count := range struggle.RepeatedErrors {
			if count >= 2 {
				hasRepeatedErrors = true
				break
			}
		}
		if struggle.NoProgressIterations >= 3 || struggle.ShortIterations >= 3 || hasRepeatedErrors {
			fmt.Println("\nStruggle indicators:")
			if struggle.NoProgressIterations >= 3 {
				fmt.Printf("- No file changes in %d iterations\n", struggle.NoProgressIterations)
			}
			if struggle.ShortIterations >= 3 {
				fmt.Printf("- %d very short iterations (<30s)\n", struggle.ShortIterations)
			}
			if hasRepeatedErrors {
				fmt.Println("- Repeated error patterns detected")
				for _, preview := range topRepeatedErrorPreview(struggle.RepeatedErrors, 3) {
					fmt.Printf("  %s\n", preview)
				}
			}
		}
	}

	return 0
}

// formatDurationLong formats a millisecond duration into a readable string.
func formatDurationLong(ms int64) string {
	totalSeconds := ms / 1000
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

// addContext appends context text for the next loop iteration.
func addContext(text string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error getting working directory:", err)
		return 1
	}
	if err := state.EnsureDir(cwd); err != nil {
		fmt.Fprintln(os.Stderr, "Error creating state directory:", err)
		return 1
	}
	contextPath := state.ContextPath(cwd)
	timestamp := time.Now().UTC().Format(time.RFC3339)
	newEntry := fmt.Sprintf("\n## Context added at %s\n%s\n", timestamp, text)

	if payload, err := os.ReadFile(contextPath); err == nil {
		if err := os.WriteFile(contextPath, append(payload, []byte(newEntry)...), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "Error writing context:", err)
			return 1
		}
	} else {
		initial := "# Ralph Loop Context\n" + newEntry
		if err := os.WriteFile(contextPath, []byte(initial), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "Error writing context:", err)
			return 1
		}
	}

	fmt.Println("Context added for next iteration")
	return 0
}

// clearContext removes any pending context file.
func clearContext() int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error getting working directory:", err)
		return 1
	}
	contextPath := state.ContextPath(cwd)
	err = os.Remove(contextPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "Error clearing context:", err)
		return 1
	}
	fmt.Println("Context cleared")
	return 0
}

// addTask appends a new top-level task to the tasks file.
func addTask(description string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error getting working directory:", err)
		return 1
	}
	if err := state.EnsureDir(cwd); err != nil {
		fmt.Fprintln(os.Stderr, "Error creating state directory:", err)
		return 1
	}
	path := state.TasksPath(cwd)
	base := "# Ralph Tasks\n\n"
	payload, err := os.ReadFile(path)
	if err == nil {
		base = string(payload)
	} else if !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "Error reading tasks:", err)
		return 1
	}
	next := strings.TrimRight(base, "\n") + "\n- [ ] " + description + "\n"
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "Error writing tasks:", err)
		return 1
	}
	fmt.Printf("Task added: %q\n", description)
	return 0
}

// listTasks prints the current task list from disk.
func listTasks() int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error getting working directory:", err)
		return 1
	}
	return printTasksFromFile(cwd)
}

// removeTask removes a top-level task and its indented subtasks by index.
func removeTask(taskIndex int) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error getting working directory:", err)
		return 1
	}
	path := state.TasksPath(cwd)
	payload, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error reading tasks:", err)
		return 1
	}

	tasks := parseTasks(string(payload))
	if len(tasks) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no tasks found")
		return 1
	}
	if taskIndex < 1 || taskIndex > len(tasks) {
		fmt.Fprintf(os.Stderr, "Error: Task index %d is out of range (1-%d)\n", taskIndex, len(tasks))
		return 1
	}

	lines := strings.Split(string(payload), "\n")
	out := make([]string, 0, len(lines))
	inRemoved := false
	currentTop := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "- [") {
			currentTop++
			if currentTop == taskIndex {
				inRemoved = true
				continue
			}
			inRemoved = false
		}
		if inRemoved {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
				continue
			}
		}
		out = append(out, line)
	}

	if err := os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "Error writing tasks:", err)
		return 1
	}

	fmt.Printf("Removed task %d and its subtasks\n", taskIndex)
	return 0
}

// parseTasks parses markdown checklist content into top-level tasks and subtasks.
func parseTasks(content string) []task {
	tasks := []task{}
	lines := strings.Split(content, "\n")
	var current *task
	mainPattern := regexp.MustCompile(`^- \[([ xX/])\]\s*(.+)$`)
	subPattern := regexp.MustCompile(`^\s+- \[([ xX/])\]\s*(.+)$`)

	for _, line := range lines {
		if match := mainPattern.FindStringSubmatch(line); len(match) == 3 {
			if current != nil {
				tasks = append(tasks, *current)
			}
			current = &task{text: match[2], status: statusFromMarker(match[1]), originalLine: line}
			continue
		}
		if match := subPattern.FindStringSubmatch(line); len(match) == 3 && current != nil {
			current.subtasks = append(current.subtasks, task{text: match[2], status: statusFromMarker(match[1]), originalLine: line})
		}
	}
	if current != nil {
		tasks = append(tasks, *current)
	}
	return tasks
}

// statusFromMarker maps a markdown task marker to an internal status label.
func statusFromMarker(marker string) string {
	switch strings.ToLower(marker) {
	case "x":
		return "complete"
	case "/":
		return "in-progress"
	default:
		return "todo"
	}
}

// printTasksFromFile prints progress and task details from the tasks file.
func printTasksFromFile(cwd string) int {
	path := state.TasksPath(cwd)
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Println("No tasks file found. Use --add-task to create your first task.")
			return 0
		}
		fmt.Fprintln(os.Stderr, "Error reading tasks:", err)
		return 1
	}
	tasks := parseTasks(string(payload))
	if len(tasks) == 0 {
		fmt.Println("No tasks found.")
		return 0
	}

	complete := 0
	inProgress := 0
	for _, item := range tasks {
		if item.status == "complete" {
			complete++
		}
		if item.status == "in-progress" {
			inProgress++
		}
	}
	fmt.Printf("Progress: %d/%d complete, %d in progress\n", complete, len(tasks), inProgress)

	fmt.Println("Current tasks:")
	for i := range tasks {
		icon := "⏸️"
		if tasks[i].status == "complete" {
			icon = "✅"
		} else if tasks[i].status == "in-progress" {
			icon = "🔄"
		}
		fmt.Printf("%d. %s %s\n", i+1, icon, tasks[i].text)
		for _, sub := range tasks[i].subtasks {
			subIcon := "⏸️"
			if sub.status == "complete" {
				subIcon = "✅"
			} else if sub.status == "in-progress" {
				subIcon = "🔄"
			}
			fmt.Printf("   %s %s\n", subIcon, sub.text)
		}
	}

	return 0
}

// topRepeatedErrorPreview returns the top repeated errors as printable previews.
func topRepeatedErrorPreview(repeated map[string]int, max int) []string {
	if len(repeated) == 0 || max <= 0 {
		return []string{}
	}
	type item struct {
		err   string
		count int
	}
	items := make([]item, 0, len(repeated))
	for key, count := range repeated {
		if count >= 2 {
			items = append(items, item{err: key, count: count})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].err < items[j].err
		}
		return items[i].count > items[j].count
	})
	if len(items) > max {
		items = items[:max]
	}
	results := make([]string, 0, len(items))
	for _, entry := range items {
		preview := strings.TrimSpace(entry.err)
		if len(preview) > 50 {
			preview = preview[:50] + "..."
		}
		results = append(results, fmt.Sprintf("- Same error %dx: \"%s\"", entry.count, preview))
	}
	return results
}
