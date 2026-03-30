package completion

import (
	"regexp"
	"strings"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func StripANSI(input string) string {
	return ansiPattern.ReplaceAllString(input, "")
}

func escapeRegex(str string) string {
	return regexp.QuoteMeta(str)
}

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

func CheckTerminalPromise(output string, promise string) bool {
	lastLine := GetLastNonEmptyLine(output)
	if lastLine == "" {
		return false
	}

	escapedPromise := escapeRegex(promise)
	pattern := regexp.MustCompile(`(?i)^<promise>\s*` + escapedPromise + `\s*</promise>$`)
	return pattern.MatchString(lastLine)
}

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
