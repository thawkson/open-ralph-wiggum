Rewrite Ralph Wiggum Loop in Go as a standalone binary

Replace the Bun/TypeScript runtime (ralph.ts + completion.ts) with a
pure Go implementation that compiles to a single static binary with zero
runtime dependencies. The Go version is a drop-in replacement with full
feature parity.

Architecture:
- cmd/ralph/main.go: CLI entry point, argument parsing, status/task
  management commands
- internal/loop/runner.go: Core iteration loop, agent spawning, stream
  processing, prompt building, signal handling
- internal/agent/config.go: Agent definitions, config loading/merging,
  cross-platform command resolution
- internal/state/state.go: Loop state persistence (.ralph/ directory)
- internal/history/history.go: Iteration history and struggle detection
- internal/completion/completion.go: Promise detection, ANSI stripping,
  task completion checks

Features ported from TypeScript:
- All 4 agent types (opencode, claude-code, codex, copilot) plus
  built-in mock agent for testing
- Agent/model rotation system with per-iteration switching
- Tasks mode with structured task tracking (ralph-tasks.md)
- Rich structured prompt builder with iteration context, critical rules,
  and task-aware sections
- Claude Code stream-json parser for human-readable output
- Custom prompt templates with variable substitution
- Interactive question handling with context injection
- Completion/abort/task promise detection
- Per-iteration file change tracking via git content hashing
- Struggle detection (repeated errors, no-progress, short iterations)
- Mid-loop context injection (--add-context)
- Auto-commit after iterations
- OpenCode plugin filtering and permission management
- Compact tool summary with periodic flushing
- Heartbeat timer for long-running iterations
- Graceful SIGINT handling (first: stop loop, second: force exit)
- Auto-detect prompt from file when single positional arg is a path
- Error iteration recording on agent spawn failure
- Inter-iteration delay (1s normal, 100ms in test mode)

Build & distribution:
- Makefile with cross-compilation targets (linux, darwin, windows;
  amd64+arm64)
- install.sh / install.ps1 updated for standalone Go binary
- CGO_ENABLED=0 for fully static binaries

Test suite:
- Unit tests: completion parsing, prompt template rendering, stream
  pipe processing, tool parsing, rotation entry parsing, task helpers,
  Claude stream parser, prompt builder (default/tasks/context modes)
- Integration tests: SIGINT cleanup, abort promise, config precedence,
  rotation resume, status command, runtime parity with mock agents
- All tests run via standard `go test ./...`

Updated Agent Defaults to copilot vs opencode
and --no-allow-all is defaulted instead of auto allowing all tool