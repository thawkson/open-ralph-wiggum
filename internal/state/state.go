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

func RalphDir(cwd string) string {
	return filepath.Join(cwd, ".ralph")
}

func LoopStatePath(cwd string) string {
	return filepath.Join(RalphDir(cwd), "ralph-loop.state.json")
}

func ContextPath(cwd string) string {
	return filepath.Join(RalphDir(cwd), "ralph-context.md")
}

func TasksPath(cwd string) string {
	return filepath.Join(RalphDir(cwd), "ralph-tasks.md")
}

func QuestionsPath(cwd string) string {
	return filepath.Join(RalphDir(cwd), "ralph-questions.json")
}

func HistoryPath(cwd string) string {
	return filepath.Join(RalphDir(cwd), "ralph-history.json")
}

func EnsureDir(cwd string) error {
	return os.MkdirAll(RalphDir(cwd), 0o755)
}

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

func ClearLoopState(cwd string) error {
	path := LoopStatePath(cwd)
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

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
