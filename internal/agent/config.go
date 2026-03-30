package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type JSONAgentConfig struct {
	Type         string `json:"type"`
	Command      string `json:"command"`
	ConfigName   string `json:"configName"`
	ArgsTemplate string `json:"argsTemplate,omitempty"`
	EnvTemplate  string `json:"envTemplate,omitempty"`
	ParsePattern string `json:"parsePattern,omitempty"`
}

type FileConfig struct {
	Version string            `json:"version"`
	Agents  []JSONAgentConfig `json:"agents"`
}

type Definition struct {
	Type         string
	Command      string
	ConfigName   string
	ArgsTemplate string
	EnvTemplate  string
	ParsePattern string
}

// DefaultConfigPath returns the default path to the agent config file.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "open-ralph-wiggum", "agents.json")
}

// DefaultConfig returns the built-in agent configuration template.
func DefaultConfig() FileConfig {
	return FileConfig{
		Version: "1.0",
		Agents: []JSONAgentConfig{
			{Type: "opencode", Command: "opencode", ConfigName: "OpenCode", ArgsTemplate: "opencode"},
			{Type: "claude-code", Command: "claude", ConfigName: "Claude Code", ArgsTemplate: "claude-code"},
			{Type: "codex", Command: "codex", ConfigName: "Codex", ArgsTemplate: "codex"},
			{Type: "copilot", Command: "copilot", ConfigName: "Copilot CLI", ArgsTemplate: "copilot"},
		},
	}
}

// WriteDefaultConfig writes the default agent config to path.
func WriteDefaultConfig(path string) error {
	if strings.TrimSpace(path) == "" {
		path = DefaultConfigPath()
	}
	if path == "" {
		return errors.New("unable to determine default config path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(DefaultConfig(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}

// LoadMerged loads file-based config and overlays it on built-in definitions.
func LoadMerged(configPath string) (map[string]Definition, error) {
	defs := defaultDefinitions()
	path := strings.TrimSpace(configPath)
	if path == "" {
		path = DefaultConfigPath()
	}
	if path == "" {
		return defs, nil
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defs, nil
		}
		return nil, err
	}

	var cfg FileConfig
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return nil, fmt.Errorf("invalid agent config file: %w", err)
	}

	for _, entry := range cfg.Agents {
		typeKey := strings.TrimSpace(entry.Type)
		if typeKey == "" {
			continue
		}
		cmd := resolveCommand(entry.Command, envOverride(typeKey))
		if cmd == "" {
			continue
		}
		defs[typeKey] = Definition{
			Type:         typeKey,
			Command:      cmd,
			ConfigName:   firstNonEmpty(entry.ConfigName, typeKey),
			ArgsTemplate: firstNonEmpty(entry.ArgsTemplate, "default"),
			EnvTemplate:  firstNonEmpty(entry.EnvTemplate, "default"),
			ParsePattern: firstNonEmpty(entry.ParsePattern, "default"),
		}
	}
	return defs, nil
}

// envOverride returns the binary override environment value for agentType.
func envOverride(agentType string) string {
	key := "RALPH_" + strings.ToUpper(strings.ReplaceAll(agentType, "-", "_")) + "_BINARY"
	return os.Getenv(key)
}

// defaultDefinitions returns the built-in set of known agent definitions.
func defaultDefinitions() map[string]Definition {
	defs := map[string]Definition{
		"opencode": {
			Type:         "opencode",
			Command:      resolveCommand("opencode", os.Getenv("RALPH_OPENCODE_BINARY")),
			ConfigName:   "OpenCode",
			ArgsTemplate: "opencode",
			EnvTemplate:  "opencode",
			ParsePattern: "opencode",
		},
		"claude-code": {
			Type:         "claude-code",
			Command:      resolveCommand("claude", os.Getenv("RALPH_CLAUDE_BINARY")),
			ConfigName:   "Claude Code",
			ArgsTemplate: "claude-code",
			EnvTemplate:  "default",
			ParsePattern: "claude-code",
		},
		"codex": {
			Type:         "codex",
			Command:      resolveCommand("codex", os.Getenv("RALPH_CODEX_BINARY")),
			ConfigName:   "Codex",
			ArgsTemplate: "codex",
			EnvTemplate:  "default",
			ParsePattern: "codex",
		},
		"copilot": {
			Type:         "copilot",
			Command:      resolveCommand("copilot", os.Getenv("RALPH_COPILOT_BINARY")),
			ConfigName:   "Copilot CLI",
			ArgsTemplate: "copilot",
			EnvTemplate:  "default",
			ParsePattern: "copilot",
		},
		"mock": {
			Type:         "mock",
			Command:      resolveCommand("sh", os.Getenv("RALPH_MOCK_BINARY")),
			ConfigName:   "Mock",
			ArgsTemplate: "mock",
			EnvTemplate:  "default",
			ParsePattern: "default",
		},
	}
	return defs
}

// resolveCommand picks envOverride when set and normalizes Windows CLI lookup.
func resolveCommand(command string, envOverride string) string {
	if strings.TrimSpace(envOverride) != "" {
		return strings.TrimSpace(envOverride)
	}
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath(command); err == nil {
			return command
		}
		withExt := command + ".cmd"
		if _, err := exec.LookPath(withExt); err == nil {
			return withExt
		}
	}
	return command
}

// firstNonEmpty returns the first non-blank string in values.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
