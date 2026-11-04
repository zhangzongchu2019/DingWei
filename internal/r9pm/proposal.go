// Package r9pm parses the bounded PM decision section from a fixed proposal read.
package r9pm

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	ErrIncomplete = errors.New("PM decision section incomplete")
	headingRE     = regexp.MustCompile(`^(#{1,6})\s+待 PM 决策(?:\s*[（(].*[）)])?\s*$`)
	pointRE       = regexp.MustCompile(`^\s*[-*]\s+([A-Za-z0-9][A-Za-z0-9_-]*-PM[0-9]{2,})[：:]\s*(.*)$`)
)

type Point struct {
	ID, Question   string
	Options        []string
	Recommendation string
}

// ParseDecisionPoints refuses an absent or malformed section. Zero points is known
// only when the section contains an explicit empty marker or all listed entries
// explicitly say they are decided.
func ParseDecisionPoints(markdown string) ([]Point, error) {
	text := strings.ReplaceAll(markdown, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	start, end, level := -1, len(lines), 0
	inFence := false
	fence := ""
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			mark := trim[:3]
			if !inFence {
				inFence = true
				fence = mark
			} else if mark == fence {
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		if m := headingRE.FindStringSubmatch(line); m != nil {
			if start >= 0 {
				return nil, fmt.Errorf("%w: duplicate section", ErrIncomplete)
			}
			start = i
			level = len(m[1])
			continue
		}
		if start >= 0 && strings.HasPrefix(trim, "#") {
			n := 0
			for n < len(trim) && trim[n] == '#' {
				n++
			}
			if n <= level {
				end = i
				break
			}
		}
	}
	if start < 0 {
		return nil, fmt.Errorf("%w: section absent", ErrIncomplete)
	}
	var pts []Point
	seen := map[string]bool{}
	empty := false
	current := -1
	var block []string
	flush := func() error {
		if current < 0 {
			return nil
		}
		p, decided, err := parsePoint(pts[current].ID, block)
		if err != nil {
			return err
		}
		if decided {
			pts = pts[:current]
			current = -1
			return nil
		}
		pts[current] = p
		current = -1
		return nil
	}
	bodyFence := false
	for _, line := range lines[start+1 : end] {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			bodyFence = !bodyFence
			continue
		}
		if bodyFence {
			continue
		}
		if strings.Contains(trim, "无待决项") || trim == "无" || trim == "（无）" {
			empty = true
			continue
		}
		m := pointRE.FindStringSubmatch(line)
		if m != nil {
			if err := flush(); err != nil {
				return nil, err
			}
			id := strings.ToUpper(m[1])
			if seen[id] {
				return nil, fmt.Errorf("%w: duplicate point %s", ErrIncomplete, id)
			}
			seen[id] = true
			pts = append(pts, Point{ID: id})
			current = len(pts) - 1
			block = []string{m[2]}
			continue
		}
		if current >= 0 {
			block = append(block, line)
		} else if trim != "" && !strings.HasPrefix(trim, "<!--") {
			return nil, fmt.Errorf("%w: unbound content", ErrIncomplete)
		}
	}
	if bodyFence {
		return nil, fmt.Errorf("%w: unclosed code fence", ErrIncomplete)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if len(pts) == 0 && !empty && len(seen) == 0 {
		return nil, fmt.Errorf("%w: no points without explicit empty marker", ErrIncomplete)
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].ID < pts[j].ID })
	return pts, nil
}
func parsePoint(id string, lines []string) (Point, bool, error) {
	p := Point{ID: id}
	var opts []string
	question, rec := "", ""
	status := ""
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if line == "" || strings.HasPrefix(line, ">") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "问题：") || strings.HasPrefix(line, "问题:"):
			question = strings.TrimSpace(line[len("问题："):])
			if strings.HasPrefix(line, "问题:") {
				question = strings.TrimSpace(line[len("问题:"):])
			}
		case strings.HasPrefix(line, "选项：") || strings.HasPrefix(line, "选项:"):
			v := strings.TrimSpace(line[len("选项："):])
			if strings.HasPrefix(line, "选项:") {
				v = strings.TrimSpace(line[len("选项:"):])
			}
			if v != "" {
				opts = append(opts, v)
			}
		case strings.HasPrefix(line, "建议：") || strings.HasPrefix(line, "建议:"):
			rec = strings.TrimSpace(line[len("建议："):])
			if strings.HasPrefix(line, "建议:") {
				rec = strings.TrimSpace(line[len("建议:"):])
			}
		case strings.HasPrefix(line, "状态：") || strings.HasPrefix(line, "状态:"):
			status = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "状态："), "状态:"))
		case regexp.MustCompile(`^[A-Z][.、:：)]\s*`).MatchString(line):
			opts = append(opts, line)
		default:
			if question == "" {
				question = line
			} else if rec == "" && strings.Contains(line, "建议") {
				rec = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "建议："), "建议:"))
			}
		}
	}
	if question == "" || rec == "" || len(opts) < 2 {
		return Point{}, false, fmt.Errorf("%w: %s requires question, at least two options and recommendation", ErrIncomplete, id)
	}
	if status == "已决" || status == "关闭" {
		return Point{}, true, nil
	}
	p.Question = question
	p.Options = opts
	p.Recommendation = rec
	return p, false, nil
}
func (p Point) ContentSHA256(pmUserIDLC string) string {
	var b strings.Builder
	b.WriteString(p.ID)
	b.WriteByte(0)
	b.WriteString(p.Question)
	for _, o := range p.Options {
		b.WriteByte(0)
		b.WriteString(o)
	}
	b.WriteByte(0)
	b.WriteString(p.Recommendation)
	b.WriteByte(0)
	b.WriteString(pmUserIDLC)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
