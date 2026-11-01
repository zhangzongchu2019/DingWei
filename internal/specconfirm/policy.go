package specconfirm

import (
	"bytes"
	"context"
	"errors"
	"path"
	"regexp"
	"strings"
	"time"
)

type Snapshot struct {
	Coordinate Coordinate
	Files      map[string][]byte
}

// SnapshotReader is a repository-hosting-provider interface. It returns the
// complete Markdown inventory at the requested immutable coordinate, retaining
// current per-principal ACLs. Reject symlinks/submodules, incomplete trees and
// truncated files. Nil Files is unknown. Credentials stay in the provider adapter.
type SnapshotReader interface {
	ReadSnapshot(context.Context, Coordinate) (Snapshot, error)
}
type Difference int

const (
	Indeterminate Difference = iota
	NoMaterial
	Material
)

type ChangePolicy interface {
	Version() string
	Compare(Snapshot, Snapshot) (Difference, error)
}

// ConservativePolicyA implements the approved PM policy; runtime activation is
// still separately gated by Service.PolicyApproved (false by default).
type ConservativePolicyA struct{}

func (ConservativePolicyA) Version() string { return "A-conservative-exemptions-v2" }

// comparisonPair permits only exact directory identity or active -> dated archive
// of the same change within the same provider/repository. It grants no access,
// does not rewrite stored coordinates, and does not alias history queries.
func comparisonPair(a, b Coordinate) bool {
	if a.Instance != b.Instance || a.Repository != b.Repository {
		return false
	}
	if a.ChangeDir == b.ChangeDir {
		return true
	}
	const active = "openspec/changes/"
	const archive = active + "archive/"
	if strings.HasPrefix(a.ChangeDir, archive) || !strings.HasPrefix(b.ChangeDir, archive) {
		return false
	}
	suffix := strings.TrimPrefix(b.ChangeDir, archive)
	if len(suffix) < 12 || suffix[10] != '-' {
		return false
	}
	if _, err := time.Parse("2006-01-02", suffix[:10]); err != nil {
		return false
	}
	return strings.TrimPrefix(a.ChangeDir, active) == suffix[11:]
}

func (ConservativePolicyA) Compare(a, b Snapshot) (Difference, error) {
	if a.Coordinate.Validate() != nil || b.Coordinate.Validate() != nil || !comparisonPair(a.Coordinate, b.Coordinate) || a.Files == nil || b.Files == nil {
		return Indeterminate, errors.New("incomplete or unrelated snapshot")
	}
	trees := make([]map[string][]byte, 2)
	for i, s := range []Snapshot{a, b} {
		if _, ok := s.Files[s.Coordinate.ChangeDir+"/proposal.md"]; !ok {
			return Indeterminate, errors.New("missing proposal")
		}
		trees[i] = make(map[string][]byte, len(s.Files))
		for name, body := range s.Files {
			if path.Clean(name) != name || !strings.HasPrefix(name, s.Coordinate.ChangeDir+"/") || !strings.HasSuffix(name, ".md") || strings.Contains(name, "\\") {
				return Indeterminate, errors.New("invalid snapshot path")
			}
			trees[i][strings.TrimPrefix(name, s.Coordinate.ChangeDir+"/")] = body
		}
	}
	if len(trees[0]) != len(trees[1]) {
		return Material, nil
	}
	moved := a.Coordinate.ChangeDir != b.Coordinate.ChangeDir
	for name, body := range trees[0] {
		other, ok := trees[1][name]
		if !ok {
			return Material, nil
		}
		// Archive exemption requires identical bytes, even in tasks.md. A move plus
		// a checkbox/handoff edit is not a content-unchanged archive move.
		if !moved && name == "tasks.md" {
			body, other = taskComparisonBytes(body), taskComparisonBytes(other)
		}
		if !bytes.Equal(body, other) {
			return Material, nil
		}
	}
	return NoMaterial, nil
}

var taskMark = regexp.MustCompile(`^ {0,3}[-*+] \[[ xX]\]([ \t]|$)`)
var atxHeading = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*)|[ \t]*)$`)
var setext = regexp.MustCompile(`^ {0,3}(=+|-+)[ \t]*$`)

// taskComparisonBytes recognizes a deliberately narrow Markdown subset. Fenced
// and indented code are never checkbox-normalized. Ambiguous HTML, setext or
// unterminated fences retain the original bytes rather than invent an exemption.
// Keep the handoff heading itself, and stop skipping at the next peer/parent
// heading, so task sections following a handoff remain material.
func taskComparisonBytes(body []byte) []byte {
	lines := strings.SplitAfter(string(body), "\n")
	var out strings.Builder
	fence := byte(0)
	width := 0
	handoff := 0
	seenHandoff := false
	for _, line := range lines {
		text := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		trimmed := strings.TrimLeft(text, " ")
		indent := len(text) - len(trimmed)
		if fence != 0 {
			if indent <= 3 && len(trimmed) >= width && strings.Trim(trimmed[:width], string(fence)) == "" && strings.TrimSpace(strings.TrimLeft(trimmed, string(fence))) == "" {
				fence = 0
			}
			if handoff == 0 {
				out.WriteString(line)
			}
			continue
		}
		if indent <= 3 && strings.HasPrefix(trimmed, "<") || setext.MatchString(text) {
			return body
		}
		if indent <= 3 && len(trimmed) >= 3 && (trimmed[0] == '`' || trimmed[0] == '~') {
			marker := trimmed[0]
			n := 0
			for n < len(trimmed) && trimmed[n] == marker {
				n++
			}
			if n >= 3 {
				// Backticks in an info string make this ambiguous; fail conservatively.
				if marker == '`' && strings.Contains(trimmed[n:], "`") {
					return body
				}
				fence, width = marker, n
				if handoff == 0 {
					out.WriteString(line)
				}
				continue
			}
		}
		h := atxHeading.FindStringSubmatch(text)
		if h != nil {
			level := len(h[1])
			if handoff != 0 && level <= handoff {
				handoff = 0
			}
			if strings.TrimSpace(h[2]) == "交接记录" {
				if seenHandoff {
					return body
				}
				seenHandoff = true
				// Only a real top-level section can introduce the exemption, never a
				// subheading inside some other ignored handoff.
				if handoff == 0 {
					out.WriteString(line)
					handoff = level
				}
				continue
			}
		}
		if handoff != 0 {
			continue
		}
		if loc := taskMark.FindStringIndex(text); loc != nil {
			// Normalize only the marker character; keep all task text/spacing/IDs.
			i := strings.Index(line, "[")
			line = line[:i+1] + " " + line[i+2:]
		}
		out.WriteString(line)
	}
	if fence != 0 {
		return body
	}
	return []byte(out.String())
}
