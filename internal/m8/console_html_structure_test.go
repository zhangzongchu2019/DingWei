package m8

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// This gate intentionally scans the complete page source, including HTML
// fragments constructed by JavaScript. Browser selectors can consistently
// select the first duplicate and thereby make an unusable second fragment look
// healthy; source-wide uniqueness catches that shared blind spot.
func TestConsolePageSourceHasNoDuplicateDOMIDs(t *testing.T) {
	tagPattern := regexp.MustCompile(`<[a-zA-Z][^<>]*>`)
	idPattern := regexp.MustCompile(`\bid\s*=\s*(?:"([^"]+)"|'([^']+)'|([^\s>]+))`)
	counts := map[string]int{}
	for _, tag := range tagPattern.FindAllString(consolePageHTML, -1) {
		match := idPattern.FindStringSubmatch(tag)
		if match == nil {
			continue
		}
		id := match[1]
		if id == "" {
			id = match[2]
		}
		if id == "" {
			id = strings.TrimRight(match[3], "'\";+")
		}
		counts[id]++
	}
	var duplicates []string
	for id, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, id+"="+strings.Repeat("+", count))
		}
	}
	sort.Strings(duplicates)
	if len(duplicates) != 0 {
		t.Fatalf("console page source contains duplicate DOM ids: %s", strings.Join(duplicates, ", "))
	}
}
