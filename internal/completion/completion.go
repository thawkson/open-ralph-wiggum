package completion

import (
	"regexp"
	"strings"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// StripANSI removes ANSI color and style escape codes from input.
func StripANSI(input string) string {
	return ansiPattern.ReplaceAllString(input, "")
}

// escapeRegex returns str escaped so it can be matched literally in a regexp.
func escapeRegex(str string) string {
	return regexp.QuoteMeta(str)
}

// GetLastNonEmptyLine returns the last non-empty line after ANSI stripping.
func GetLastNonEmptyLine(output string) string {
	normalized := strings.ReplaceAll(StripANSI(output), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// CheckTerminalPromise reports whether the last non-empty line matches promise.
func CheckTerminalPromise(output string, promise string) bool {
	lastLine := GetLastNonEmptyLine(output)
	if lastLine == "" {
		return false
	}

	escapedPromise := escapeRegex(promise)
	pattern := regexp.MustCompile(`(?i)^<promise>\s*` + escapedPromise + `\s*</promise>$`)
	return pattern.MatchString(lastLine)
}

// TasksMarkdownAllComplete returns true when all parsed markdown tasks are complete.
func TasksMarkdownAllComplete(tasksMarkdown string) bool {
	lines := strings.Split(tasksMarkdown, "\n")
	sawTask := false
	pattern := regexp.MustCompile(`^\s*-\s+\[([ xX/])\]\s+`)

	for _, line := range lines {
		match := pattern.FindStringSubmatch(line)
		if len(match) < 2 {
			continue
		}

		sawTask = true
		if strings.ToLower(match[1]) != "x" {
			return false
		}
	}

	return sawTask
}
