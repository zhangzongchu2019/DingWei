package taskdispatch

import (
	"errors"
	"testing"
)

func TestR10FixedTaskParser(t *testing.T) {
	good := "# Tasks\n- [ ] T-01 implement\n- [x] T-02 done\n```md\n- [ ] T-01 fake\n```\n    - [ ] T-01 fake\n- [ ] T-03.1 subtask\n"
	task, e := SelectTask(good, "T-01")
	if e != nil || task.Line != 2 {
		t.Fatal("R10_FIXED_TASK_NOT_FOUND", task, e)
	}
	if _, e = SelectTask(good, "T-02"); !errors.Is(e, ErrCompleted) {
		t.Fatal("R10_COMPLETED_TASK_GRANTED", e)
	}
	for _, text := range []string{"- [ ] T-01 a\n- [x] T-01 b", "- [ ] T-01 ", "```\n- [ ] T-01 fake", "```bad`info\n- [ ] T-01 hidden\n```", "<!--\n- [ ] T-01 fake\n-->", "- [ ] T-01 a\x00", "- [ ] T-01 a\xff"} {
		if _, e := ParseTasks(text); e == nil {
			t.Fatalf("R10_AMBIGUOUS_TASK_ACCEPTED %q", text)
		}
	}
	if _, e = SelectTask("```\n- [ ] T-01 fake\n```", "T-01"); !errors.Is(e, ErrNotFound) {
		t.Fatal("R10_CODE_TASK_ACCEPTED", e)
	}
}
