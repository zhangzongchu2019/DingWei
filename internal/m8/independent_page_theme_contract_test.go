package m8

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func darkIndependentPageHasThemeReceiver(source string) bool {
	if !strings.Contains(strings.ToLower(source), "<!doctype") || !strings.Contains(source, `html[data-theme="dark"]`) {
		return true
	}
	return strings.Contains(source, "DW_THEME_KEY") && strings.Contains(source, "BroadcastChannel") && strings.Contains(source, "addEventListener('storage'") && strings.Contains(source, "document.documentElement.dataset.theme")
}

func TestEveryIndependentDarkPageReceivesSharedTheme(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, name := range files {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		if strings.Contains(strings.ToLower(source), "<!doctype") && strings.Contains(source, `html[data-theme="dark"]`) {
			checked++
			if !darkIndependentPageHasThemeReceiver(source) {
				t.Errorf("independent dark page has no shared theme receiver: %s", name)
			}
		}
	}
	if checked < 3 {
		t.Fatalf("theme receiver scan matched %d pages, want at least shell/spec/task board", checked)
	}
	// Positive control for the mechanical scan: a future page that only adds a
	// dark CSS promise must fail until its receiver is wired. The literal lives
	// here, so the production-file scan above intentionally excludes this guard.
	darkOnly := `const page = "<!doctype html><style>html[data-theme="dark"]{color:white}</style>"`
	if darkIndependentPageHasThemeReceiver(darkOnly) {
		t.Fatal("dark-only independent page escaped the theme receiver guard")
	}
}
