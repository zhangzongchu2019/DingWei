package store

import (
	"strings"
	"testing"
)

func TestConsoleSpecRevisionWriteKeepsCurrentEditorAtAtomicWritePoint(t *testing.T) {
	if !strings.Contains(appendConsoleSpecRevisionUpdateSQL, "current_editor_id=?") {
		t.Fatal("B4: revision UPDATE lost current-editor predicate; a transfer racing the earlier read could be overwritten")
	}
}
