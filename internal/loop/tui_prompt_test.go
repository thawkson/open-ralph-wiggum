package loop

import (
	"reflect"
	"testing"
)

func TestDetectQuestionOptionsInlineNumbered(t *testing.T) {
	question := "Choose one: 1) Create 2) Update 3) Delete"
	got := detectQuestionOptions(question)
	want := []string{"Create", "Update", "Delete"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detectQuestionOptions() = %#v, want %#v", got, want)
	}
}

func TestDetectQuestionOptionsBulletList(t *testing.T) {
	question := "Select target:\n- Backend\n- Frontend\n- Docs"
	got := detectQuestionOptions(question)
	want := []string{"Backend", "Frontend", "Docs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detectQuestionOptions() = %#v, want %#v", got, want)
	}
}

func TestDetectQuestionOptionsInferredYesNo(t *testing.T) {
	question := "Should I run the migration now?"
	got := detectQuestionOptions(question)
	want := []string{"Yes", "No"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detectQuestionOptions() = %#v, want %#v", got, want)
	}
}

func TestDetectQuestionOptionsMarkdownAlternatives(t *testing.T) {
	question := "For this MVP, should we target **PostgreSQL** or **MySQL** as the primary production database?"
	got := detectQuestionOptions(question)
	want := []string{"PostgreSQL", "MySQL"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detectQuestionOptions() = %#v, want %#v", got, want)
	}
}

func TestDetectQuestionOptionsCommaOrList(t *testing.T) {
	question := "For the MVP production database target, should we standardize on PostgreSQL, MySQL, or require support for both from day one?"
	got := detectQuestionOptions(question)
	want := []string{"PostgreSQL", "MySQL", "require support for both from day one"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detectQuestionOptions() = %#v, want %#v", got, want)
	}
}

func TestDetectQuestionOptionsBinaryAlternatives(t *testing.T) {
	question := "Do you want the MVP to include user authentication/authorization, or should it be a single-user app with no login?"
	got := detectQuestionOptions(question)
	want := []string{"include user authentication/authorization", "should it be a single-user app with no login"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detectQuestionOptions() = %#v, want %#v", got, want)
	}
}

func TestDetectQuestionOptionsQuotedCommaDoesNotSplit(t *testing.T) {
	question := "For task \u201ccheck task,\u201d do you want it as a full update endpoint (toggle checked/unchecked) or one-way completion only (unchecked -> checked)?"
	got := detectQuestionOptions(question)
	want := []string{"a full update endpoint (toggle checked/unchecked)", "one-way completion only (unchecked -> checked)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detectQuestionOptions() = %#v, want %#v", got, want)
	}
}

func TestDetectQuestionOptionsServerRenderedVsSeparateClient(t *testing.T) {
	question := "For this MVP, should we assume server-rendered Flask templates with Bootstrap, or a separate frontend client that calls the Flask JSON API?"
	got := detectQuestionOptions(question)
	want := []string{"server-rendered Flask templates with Bootstrap", "a separate frontend client that calls the Flask JSON API"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detectQuestionOptions() = %#v, want %#v", got, want)
	}
}

func TestDetectQuestionOptionsParentheticalDoesNotBecomeOption(t *testing.T) {
	question := "For the MVP PRD, should we treat PostgreSQL as the primary production database target (while still noting MySQL compatibility), or require equal first-class support for both from day one?"
	got := detectQuestionOptions(question)
	want := []string{"PostgreSQL as the primary production database target (while still noting MySQL compatibility)", "require equal first-class support for both from day one"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detectQuestionOptions() = %#v, want %#v", got, want)
	}
}

func TestSplitChoicePartsIgnoresCommaInParens(t *testing.T) {
	input := "alpha (x, y), beta, gamma"
	got := splitChoiceParts(input)
	want := []string{"alpha (x, y)", "beta", "gamma"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitChoiceParts() = %#v, want %#v", got, want)
	}
}

func TestNormalizeChoicesDedupes(t *testing.T) {
	got := normalizeChoices([]string{"Yes", " yes ", "No", "", "Type custom response"})
	want := []string{"Yes", "No"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeChoices() = %#v, want %#v", got, want)
	}
}

func TestShortcutChoiceApproveDeny(t *testing.T) {
	options := []string{"Approve", "Deny", customResponseOption}

	approved, ok := shortcutChoice(options, "y")
	if !ok || approved != "Approve" {
		t.Fatalf("shortcutChoice(y) = %q, %t", approved, ok)
	}

	denied, ok := shortcutChoice(options, "n")
	if !ok || denied != "Deny" {
		t.Fatalf("shortcutChoice(n) = %q, %t", denied, ok)
	}
}
