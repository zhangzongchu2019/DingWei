package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionMatchesRepositoryVersionFile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Version, strings.TrimSpace(string(data)); got != want {
		t.Fatalf("version constant=%q VERSION=%q", got, want)
	}
}
