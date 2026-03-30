package history

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/Th0rgal/open-ralph-wiggum/internal/state"
)

type IterationRecord struct {
	Iteration          int            `json:"iteration"`
	StartedAt          string         `json:"startedAt"`
	EndedAt            string         `json:"endedAt"`
	DurationMs         int64          `json:"durationMs"`
	Agent              string         `json:"agent"`
	Model              string         `json:"model"`
	ToolsUsed          map[string]int `json:"toolsUsed"`
	FilesModified      []string       `json:"filesModified"`
	ExitCode           int            `json:"exitCode"`
	CompletionDetected bool           `json:"completionDetected"`
	Errors             []string       `json:"errors"`
}

type StruggleIndicators struct {
	RepeatedErrors       map[string]int `json:"repeatedErrors"`
	NoProgressIterations int            `json:"noProgressIterations"`
	ShortIterations      int            `json:"shortIterations"`
}

type RalphHistory struct {
	Iterations         []IterationRecord  `json:"iterations"`
	TotalDurationMs    int64              `json:"totalDurationMs"`
	StruggleIndicators StruggleIndicators `json:"struggleIndicators"`
}

// Empty returns a new history value with initialized default fields.
func Empty() RalphHistory {
	return RalphHistory{
		Iterations:      []IterationRecord{},
		TotalDurationMs: 0,
		StruggleIndicators: StruggleIndicators{
			RepeatedErrors:       map[string]int{},
			NoProgressIterations: 0,
			ShortIterations:      0,
		},
	}
}

// Load reads history from disk and returns defaults when the file is missing.
func Load(cwd string) (RalphHistory, error) {
	path := state.HistoryPath(cwd)
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Empty(), nil
		}
		return RalphHistory{}, err
	}
	var h RalphHistory
	if err := json.Unmarshal(payload, &h); err != nil {
		return Empty(), nil
	}
	if h.StruggleIndicators.RepeatedErrors == nil {
		h.StruggleIndicators.RepeatedErrors = map[string]int{}
	}
	return h, nil
}

// Save persists history atomically in the workspace state directory.
func Save(cwd string, h RalphHistory) error {
	if err := state.EnsureDir(cwd); err != nil {
		return err
	}
	path := state.HistoryPath(cwd)
	tmp := path + ".tmp"
	payload, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Clear removes the history file and ignores missing-file errors.
func Clear(cwd string) error {
	err := os.Remove(state.HistoryPath(cwd))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// UpdateWithIteration appends r and refreshes struggle indicators.
func UpdateWithIteration(h *RalphHistory, r IterationRecord) {
	h.Iterations = append(h.Iterations, r)
	h.TotalDurationMs += r.DurationMs

	if len(r.FilesModified) == 0 {
		h.StruggleIndicators.NoProgressIterations++
	} else {
		h.StruggleIndicators.NoProgressIterations = 0
	}

	if r.DurationMs < 30000 {
		h.StruggleIndicators.ShortIterations++
	} else {
		h.StruggleIndicators.ShortIterations = 0
	}

	if len(r.Errors) == 0 {
		h.StruggleIndicators.RepeatedErrors = map[string]int{}
		return
	}

	for _, errLine := range r.Errors {
		key := errLine
		if len(key) > 100 {
			key = key[:100]
		}
		h.StruggleIndicators.RepeatedErrors[key] = h.StruggleIndicators.RepeatedErrors[key] + 1
	}
}

// TopTools formats the most-used tools as sorted name(count) entries.
func TopTools(tools map[string]int, max int) string {
	if len(tools) == 0 {
		return ""
	}
	type kv struct {
		k string
		v int
	}
	items := make([]kv, 0, len(tools))
	for k, v := range tools {
		items = append(items, kv{k: k, v: v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].v == items[j].v {
			return items[i].k < items[j].k
		}
		return items[i].v > items[j].v
	})
	if max > 0 && len(items) > max {
		items = items[:max]
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, item.k+"("+itoa(item.v)+")")
	}
	return strings.Join(parts, " ")
}

// itoa converts an integer to its base-10 string form.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return sign + string(buf[i:])
}
