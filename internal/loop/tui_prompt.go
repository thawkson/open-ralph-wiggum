package loop

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const customResponseOption = "Type custom response"

var (
	explicitYesNoPattern      = regexp.MustCompile(`(?i)\b(?:yes\s*/\s*no|y\s*/\s*n|yes or no)\b`)
	inferredYesNoPattern      = regexp.MustCompile(`(?i)^\s*(?:do you|should i|can i|may i|is it ok)\b`)
	lineChoicePattern         = regexp.MustCompile(`(?m)^\s*(?:[-*]\s+|\d+[.)]\s+|[A-Za-z][.)]\s+)(.+?)\s*$`)
	inlineNumberMarkerPattern = regexp.MustCompile(`(?:^|[\s,;(])\d+[.)]\s*`)
	inlineLetterMarkerPattern = regexp.MustCompile(`(?:^|[\s,;(])[A-Za-z][.)]\s*`)
	markdownBoldPattern       = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	orSeparatorPattern        = regexp.MustCompile(`(?i)\s+or\s+`)
	multiSpacePattern         = regexp.MustCompile(`\s+`)
	leadQuestionPrefixPattern = regexp.MustCompile(`(?i)^.*?(?:do\s+you\s+want(?:\s+the\s+mvp\s+to)?|should\s+we|should\s+i|can\s+i|may\s+i|would\s+you|could\s+you)\s+`)
	leadChoiceFillerPattern   = regexp.MustCompile(`(?i)^(?:it\s+as\s+|it\s+to\s+|the\s+mvp\s+to\s+|assume\s+|treat\s+)`)
	listAnchorPattern         = regexp.MustCompile(`(?i)\b(?:standardize\s+on|choose|pick|select|between|among)\s+`)
	preambleChoicePattern     = regexp.MustCompile(`(?i)^for\s+(?:this|the)\b`)
)

type selectionPromptModel struct {
	title       string
	options     []string
	selected    int
	inCustom    bool
	customInput []rune
	submitValue string
	err         error
}

func newSelectionPromptModel(title string, options []string) selectionPromptModel {
	normalized := normalizeChoices(options)
	normalized = append(normalized, customResponseOption)
	startInCustom := len(normalized) == 1

	return selectionPromptModel{
		title:    title,
		options:  normalized,
		inCustom: startInCustom,
	}
}

func (m selectionPromptModel) Init() tea.Cmd {
	return nil
}

func (m selectionPromptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.inCustom {
		return m.updateCustomInput(keyMsg)
	}
	return m.updateSelection(keyMsg)
}

func (m selectionPromptModel) View() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(m.title)
	b.WriteString("\n")

	if m.inCustom {
		b.WriteString("Type a custom response and press Enter. Press Esc to return to choices.\n")
		b.WriteString("> ")
		b.WriteString(string(m.customInput))
		return b.String()
	}

	b.WriteString("Use up/down or j/k to select, then press Enter. Press c for custom input.\n")
	b.WriteString("Shortcuts: y/n work for yes-no or approve-deny choices.\n")
	for i, option := range m.options {
		prefix := "  "
		if i == m.selected {
			prefix = "> "
		}
		b.WriteString(prefix)
		b.WriteString(option)
		b.WriteString("\n")
	}
	return b.String()
}

func (m selectionPromptModel) updateSelection(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch strings.ToLower(msg.String()) {
	case "ctrl+c", "q":
		m.err = errors.New("prompt interrupted")
		return m, tea.Quit
	case "up", "k":
		if len(m.options) > 0 {
			m.selected = (m.selected - 1 + len(m.options)) % len(m.options)
		}
	case "down", "j":
		if len(m.options) > 0 {
			m.selected = (m.selected + 1) % len(m.options)
		}
	case "y", "n":
		if choice, ok := shortcutChoice(m.options, strings.ToLower(msg.String())); ok {
			m.submitValue = choice
			return m, tea.Quit
		}
	case "c":
		m.inCustom = true
	case "enter":
		if len(m.options) == 0 {
			m.inCustom = true
			return m, nil
		}
		selected := m.options[m.selected]
		if strings.EqualFold(selected, customResponseOption) {
			m.inCustom = true
			return m, nil
		}
		m.submitValue = selected
		return m, tea.Quit
	}

	return m, nil
}

func (m selectionPromptModel) updateCustomInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		m.err = errors.New("prompt interrupted")
		return m, tea.Quit
	case tea.KeyEsc:
		m.inCustom = false
		return m, nil
	case tea.KeyEnter:
		m.submitValue = strings.TrimSpace(string(m.customInput))
		return m, tea.Quit
	case tea.KeyBackspace, tea.KeyDelete:
		if len(m.customInput) > 0 {
			m.customInput = m.customInput[:len(m.customInput)-1]
		}
		return m, nil
	case tea.KeySpace:
		m.customInput = append(m.customInput, ' ')
		return m, nil
	case tea.KeyRunes:
		m.customInput = append(m.customInput, msg.Runes...)
		return m, nil
	default:
		return m, nil
	}
}

func runSelectionPrompt(title string, options []string) (string, error) {
	model := newSelectionPromptModel(title, options)
	program := tea.NewProgram(model, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))

	final, err := program.Run()
	if err != nil {
		return "", err
	}

	result, ok := final.(selectionPromptModel)
	if !ok {
		return "", errors.New("unable to parse selection prompt result")
	}
	if result.err != nil {
		return "", result.err
	}

	fmt.Println()
	return strings.TrimSpace(result.submitValue), nil
}

func detectQuestionOptions(question string) []string {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil
	}

	choices := []string{}
	if explicitYesNoPattern.MatchString(question) {
		choices = append(choices, "Yes", "No")
	}

	choices = append(choices, extractLineChoices(question)...)

	if inline := extractIndexedInlineChoices(question, inlineNumberMarkerPattern); len(inline) >= 2 {
		choices = append(choices, inline...)
	}
	if inline := extractIndexedInlineChoices(question, inlineLetterMarkerPattern); len(inline) >= 2 {
		choices = append(choices, inline...)
	}

	if natural := extractNaturalLanguageChoices(question); len(natural) >= 2 {
		choices = append(choices, natural...)
	}

	choices = normalizeChoices(choices)
	if len(choices) == 0 && inferredYesNoPattern.MatchString(question) {
		return []string{"Yes", "No"}
	}
	return choices
}

func extractLineChoices(question string) []string {
	matches := lineChoicePattern.FindAllStringSubmatch(question, -1)
	choices := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		choices = append(choices, cleanChoice(match[1]))
	}
	return normalizeChoices(choices)
}

func extractIndexedInlineChoices(question string, markerPattern *regexp.Regexp) []string {
	indices := markerPattern.FindAllStringIndex(question, -1)
	if len(indices) < 2 {
		return nil
	}

	choices := make([]string, 0, len(indices))
	for i, match := range indices {
		start := match[1]
		end := len(question)
		if i+1 < len(indices) {
			end = indices[i+1][0]
		}
		choices = append(choices, cleanChoice(question[start:end]))
	}
	return normalizeChoices(choices)
}

func extractNaturalLanguageChoices(question string) []string {
	boldMatches := markdownBoldPattern.FindAllStringSubmatch(question, -1)
	if len(boldMatches) >= 2 {
		choices := make([]string, 0, len(boldMatches))
		for _, match := range boldMatches {
			if len(match) != 2 {
				continue
			}
			choices = append(choices, cleanChoice(match[1]))
		}
		return normalizeChoices(choices)
	}

	flat := normalizeQuestionText(question)
	if !orSeparatorPattern.MatchString(flat) {
		return nil
	}

	candidate := cleanChoice(leadQuestionPrefixPattern.ReplaceAllString(flat, ""))
	candidate = cleanChoice(leadChoiceFillerPattern.ReplaceAllString(candidate, ""))
	candidate = trimDisjunctionLead(candidate)

	if !strings.Contains(candidate, ",") {
		parts := orSeparatorPattern.Split(candidate, -1)
		if len(parts) == 2 {
			left := cleanChoice(leadChoiceFillerPattern.ReplaceAllString(parts[0], ""))
			right := cleanChoice(parts[1])
			return cleanupNaturalChoices([]string{left, right})
		}
	}

	replaced := orSeparatorPattern.ReplaceAllString(candidate, ",")
	parts := splitChoiceParts(replaced)
	choices := make([]string, 0, len(parts))
	for idx, part := range parts {
		cleaned := cleanChoice(part)
		if idx == 0 {
			cleaned = cleanChoice(leadChoiceFillerPattern.ReplaceAllString(cleaned, ""))
		}
		if cleaned == "" {
			continue
		}
		choices = append(choices, cleaned)
	}

	return cleanupNaturalChoices(choices)
}

func cleanupNaturalChoices(choices []string) []string {
	normalized := normalizeChoices(choices)
	if len(normalized) >= 3 && preambleChoicePattern.MatchString(normalized[0]) {
		normalized = normalized[1:]
	}

	cleaned := make([]string, 0, len(normalized))
	for _, choice := range normalized {
		choice = cleanChoice(leadQuestionPrefixPattern.ReplaceAllString(choice, ""))
		choice = cleanChoice(leadChoiceFillerPattern.ReplaceAllString(choice, ""))
		if choice == "" {
			continue
		}
		cleaned = append(cleaned, choice)
	}
	return normalizeChoices(cleaned)
}

func normalizeQuestionText(question string) string {
	q := strings.TrimSpace(question)
	q = strings.ReplaceAll(q, "**", "")
	q = strings.TrimSuffix(q, "?")
	q = strings.TrimSuffix(q, ".")
	q = strings.TrimSpace(q)
	q = multiSpacePattern.ReplaceAllString(q, " ")
	return q
}

func trimDisjunctionLead(question string) string {
	lower := strings.ToLower(question)
	indices := listAnchorPattern.FindAllStringIndex(lower, -1)
	if len(indices) == 0 {
		return question
	}
	last := indices[len(indices)-1]
	if last[1] < len(question) {
		return strings.TrimSpace(question[last[1]:])
	}
	return question
}

func splitChoiceParts(value string) []string {
	parts := []string{}
	segmentStart := 0
	parenDepth := 0
	inSingleQuote := false
	inDoubleQuote := false
	inLeftCurlyQuote := false

	runes := []rune(value)
	for idx, r := range runes {
		switch r {
		case '(':
			if !inSingleQuote && !inDoubleQuote && !inLeftCurlyQuote {
				parenDepth++
			}
		case ')':
			if !inSingleQuote && !inDoubleQuote && !inLeftCurlyQuote && parenDepth > 0 {
				parenDepth--
			}
		case '\'':
			if !inDoubleQuote && !inLeftCurlyQuote {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if !inSingleQuote && !inLeftCurlyQuote {
				inDoubleQuote = !inDoubleQuote
			}
		case '\u201c':
			if !inSingleQuote && !inDoubleQuote {
				inLeftCurlyQuote = true
			}
		case '\u201d':
			if !inSingleQuote && !inDoubleQuote {
				inLeftCurlyQuote = false
			}
		case ',':
			if parenDepth == 0 && !inSingleQuote && !inDoubleQuote && !inLeftCurlyQuote {
				parts = append(parts, strings.TrimSpace(string(runes[segmentStart:idx])))
				segmentStart = idx + 1
			}
		}
	}

	parts = append(parts, strings.TrimSpace(string(runes[segmentStart:])))
	return parts
}

func cleanChoice(choice string) string {
	choice = strings.TrimSpace(choice)
	choice = strings.Trim(choice, `"' `)
	for {
		if strings.HasSuffix(choice, ".") || strings.HasSuffix(choice, ",") || strings.HasSuffix(choice, ";") || strings.HasSuffix(choice, ":") {
			choice = strings.TrimSpace(choice[:len(choice)-1])
			continue
		}
		break
	}
	return choice
}

func normalizeChoices(choices []string) []string {
	normalized := make([]string, 0, len(choices))
	seen := map[string]struct{}{}
	for _, choice := range choices {
		cleaned := cleanChoice(choice)
		if cleaned == "" {
			continue
		}
		if strings.EqualFold(cleaned, customResponseOption) {
			continue
		}
		key := strings.ToLower(cleaned)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, cleaned)
	}
	return normalized
}

func shortcutChoice(options []string, key string) (string, bool) {
	wantsApprove := key == "y"
	wantsDeny := key == "n"
	if !wantsApprove && !wantsDeny {
		return "", false
	}

	for _, option := range options {
		normalized := strings.ToLower(strings.TrimSpace(option))
		if wantsApprove {
			if normalized == "yes" || normalized == "approve" || normalized == "approved" || normalized == "allow" {
				return option, true
			}
			continue
		}
		if normalized == "no" || normalized == "deny" || normalized == "denied" {
			return option, true
		}
	}
	return "", false
}
