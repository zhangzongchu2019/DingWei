package specconfirm

import (
	"bytes"
	"context"
	"errors"
	"path"
	"strings"
)

type Snapshot struct {
	Coordinate Coordinate
	Files      map[string][]byte
}

// Reader returns the complete change-directory Markdown inventory at exactly
// the supplied commit. Implementations must reject symlinks/submodules, partial
// trees and truncated files, use bounded resources, and retain current read ACL.
// Nil Files means incomplete/unknown, not an empty tree.
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

// ConservativePolicyA is the proposed policy, not an approved runtime rule.
// It compares all change-directory Markdown bytes, including tasks checkboxes
// and handoffs. Exemptions and archive normalization require PM approval and a
// separately versioned policy; none are silently activated in this implementation.
type ConservativePolicyA struct{}

func (ConservativePolicyA) Version() string { return "A-conservative-bytes-v1" }
func (ConservativePolicyA) Compare(a, b Snapshot) (Difference, error) {
	if a.Coordinate.Validate() != nil || b.Coordinate.Validate() != nil || !a.Coordinate.SameChange(b.Coordinate) || a.Files == nil || b.Files == nil {
		return Indeterminate, errors.New("incomplete snapshot")
	}
	for _, s := range []Snapshot{a, b} {
		if _, ok := s.Files[s.Coordinate.ChangeDir+"/proposal.md"]; !ok {
			return Indeterminate, errors.New("missing proposal")
		}
		for name := range s.Files {
			if path.Clean(name) != name || !strings.HasPrefix(name, s.Coordinate.ChangeDir+"/") || !strings.HasSuffix(name, ".md") || strings.Contains(name, "\\") {
				return Indeterminate, errors.New("invalid snapshot path")
			}
		}
	}
	if len(a.Files) != len(b.Files) {
		return Material, nil
	}
	for name, body := range a.Files {
		other, ok := b.Files[name]
		if !ok || !bytes.Equal(body, other) {
			return Material, nil
		}
	}
	return NoMaterial, nil
}
