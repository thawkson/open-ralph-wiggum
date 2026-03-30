package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptTemplatePrecedenceIntegration(t *testing.T) {
	wd := t.TempDir()
	templatePath := filepath.Join(wd, "template.txt")
	if err := os.WriteFile(templatePath, []byte("echo '<promise>TPL_DONE</promise>'"), 0o644); err != nil {
		t.Fatalf("write template failed: %v", err)
	}

	output, code := runRalphBinary(t, wd,
		"echo not-complete",
		"--agent", "mock",
		"--completion-promise", "TPL_DONE",
		"--prompt-template", templatePath,
		"--max-iterations", "3",
	)
	if code != 0 {
		t.Fatalf("command failed with %d\n%s", code, output)
	}
	if !strings.Contains(output, "Completion promise detected") {
		t.Fatalf("expected completion from template prompt override:\n%s", output)
	}
}
