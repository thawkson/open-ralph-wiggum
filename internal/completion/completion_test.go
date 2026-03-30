package completion

import "testing"

// TestCheckTerminalPromise verifies completion detection only when the final non-empty line matches the promise tag.
func TestCheckTerminalPromise(t *testing.T) {
	t.Run("detects completion when promise tag is the final non-empty line", func(t *testing.T) {
		output := "Implemented changes.\nAll tests pass.\n<promise>LEGION_EPIC_DONE_2026_02_17</promise>\n"
		if !CheckTerminalPromise(output, "LEGION_EPIC_DONE_2026_02_17") {
			t.Fatal("expected completion promise to be detected")
		}
	})

	t.Run("does not detect completion when promise appears earlier in output", func(t *testing.T) {
		output := "Do not output <promise>LEGION_EPIC_DONE_2026_02_17</promise> yet.\nStill working on pending items."
		if CheckTerminalPromise(output, "LEGION_EPIC_DONE_2026_02_17") {
			t.Fatal("expected completion promise not to be detected")
		}
	})

	t.Run("does not detect completion when a different final promise is emitted", func(t *testing.T) {
		output := "Task complete, moving to next task.\n<promise>READY_FOR_NEXT_TASK</promise>"
		if CheckTerminalPromise(output, "LEGION_EPIC_DONE_2026_02_17") {
			t.Fatal("expected mismatched promise not to be detected")
		}
	})

	t.Run("accepts flexible whitespace inside promise tags", func(t *testing.T) {
		output := "<promise>   COMPLETE   </promise>"
		if !CheckTerminalPromise(output, "COMPLETE") {
			t.Fatal("expected whitespace-normalized promise to be detected")
		}
	})
}

// TestGetLastNonEmptyLine verifies the last non-empty output line is returned.
func TestGetLastNonEmptyLine(t *testing.T) {
	output := "line 1\nline 2\n\n"
	got := GetLastNonEmptyLine(output)
	if got != "line 2" {
		t.Fatalf("expected line 2, got %q", got)
	}
}

// TestTasksMarkdownAllComplete verifies task markdown completion requires at least one task and all tasks checked.
func TestTasksMarkdownAllComplete(t *testing.T) {
	t.Run("requires at least one task", func(t *testing.T) {
		if TasksMarkdownAllComplete("# Ralph Tasks\n\nNo tasks yet.") {
			t.Fatal("expected false when there are no tasks")
		}
	})

	t.Run("returns false when any task is todo or in-progress", func(t *testing.T) {
		markdown := "# Ralph Tasks\n- [x] Completed task\n- [ ] Pending task\n  - [/] Subtask in progress"
		if TasksMarkdownAllComplete(markdown) {
			t.Fatal("expected false when incomplete tasks exist")
		}
	})

	t.Run("returns true only when all task checkboxes are complete", func(t *testing.T) {
		markdown := "# Ralph Tasks\n- [x] Task 1\n- [X] Task 2\n  - [x] Subtask 2.1"
		if !TasksMarkdownAllComplete(markdown) {
			t.Fatal("expected true when all tasks are complete")
		}
	})
}
