package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type RalphState struct {
	Active            bool     `json:"active"`
	Iteration         int      `json:"iteration"`
	MinIterations     int      `json:"minIterations"`
	MaxIterations     int      `json:"maxIterations"`
	CompletionPromise string   `json:"completionPromise"`
	AbortPromise      string   `json:"abortPromise,omitempty"`
	TasksMode         bool     `json:"tasksMode,omitempty"`
	TaskPromise       string   `json:"taskPromise,omitempty"`
	Prompt            string   `json:"prompt"`
	PromptTemplate    string   `json:"promptTemplate,omitempty"`
	StartedAt         string   `json:"startedAt"`
	Model             string   `json:"model"`
	Agent             string   `json:"agent"`
	Rotation          []string `json:"rotation,omitempty"`
	RotationIndex     *int     `json:"rotationIndex,omitempty"`
}

// RalphDir returns the workspace-local directory used for Ralph state files.
func RalphDir(cwd string) string {
	return filepath.Join(cwd, ".ralph")
}

// LoopStatePath returns the path to the persisted loop state file.
func LoopStatePath(cwd string) string {
	return filepath.Join(RalphDir(cwd), "ralph-loop.state.json")
}

// ContextPath returns the path to the pending context markdown file.
func ContextPath(cwd string) string {
	return filepath.Join(RalphDir(cwd), "ralph-context.md")
}

// TasksPath returns the path to the tasks markdown file.
func TasksPath(cwd string) string {
	return filepath.Join(RalphDir(cwd), "ralph-tasks.md")
}

// QuestionsPath returns the path to the queued question-answer file.
func QuestionsPath(cwd string) string {
	return filepath.Join(RalphDir(cwd), "ralph-questions.json")
}

// ApprovalsPath returns the path to the queued permission-decision file.
func ApprovalsPath(cwd string) string {
	return filepath.Join(RalphDir(cwd), "ralph-approvals.json")
}

// HistoryPath returns the path to the loop iteration history file.
func HistoryPath(cwd string) string {
	return filepath.Join(RalphDir(cwd), "ralph-history.json")
}

// EnsureDir creates the Ralph state directory when needed.
func EnsureDir(cwd string) error {
	return os.MkdirAll(RalphDir(cwd), 0o755)
}

// SaveLoopState writes the loop state atomically to disk.
func SaveLoopState(cwd string, st RalphState) error {
	if err := EnsureDir(cwd); err != nil {
		return err
	}
	path := LoopStatePath(cwd)
	tmp := path + ".tmp"

	payload, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadLoopState loads the current loop state or nil if no state exists.
func LoadLoopState(cwd string) (*RalphState, error) {
	path := LoopStatePath(cwd)
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var st RalphState
	if err := json.Unmarshal(payload, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// ClearLoopState removes the persisted loop state file if present.
func ClearLoopState(cwd string) error {
	path := LoopStatePath(cwd)
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// NewState builds an initial active loop state for a new run.
func NewState(prompt, completionPromise, abortPromise, model, agent string, minIterations, maxIterations int) RalphState {
	return RalphState{
		Active:            true,
		Iteration:         1,
		MinIterations:     minIterations,
		MaxIterations:     maxIterations,
		CompletionPromise: completionPromise,
		AbortPromise:      abortPromise,
		Prompt:            prompt,
		StartedAt:         time.Now().UTC().Format(time.RFC3339),
		Model:             model,
		Agent:             agent,
	}
}
