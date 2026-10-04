package m8

import (
	"strings"
	"testing"
)

func TestConsoleHostedFeatureTemplateIsServerInjected(t *testing.T) {
	if strings.Contains(consolePageHTML, "FEATURE_HOSTED=true") || strings.Contains(consolePageHTML, "FEATURE_HOSTED=false") {
		t.Fatal("console template must not hard-code the hosted feature")
	}
	if !strings.Contains(consolePageHTML, "{{FEATURE_HOSTED}}") {
		t.Fatal("console template is missing the server-side hosted placeholder")
	}
	for _, tc := range []struct {
		on   bool
		want string
	}{{true, "var FEATURE_HOSTED='true'==='true';"}, {false, "var FEATURE_HOSTED='false'==='true';"}} {
		page := renderConsolePage("test", "unknown", tc.on)
		if !strings.Contains(page, tc.want) || strings.Contains(page, "{{FEATURE_HOSTED}}") {
			t.Fatalf("hosted=%v was not fully server-rendered", tc.on)
		}
	}
}
