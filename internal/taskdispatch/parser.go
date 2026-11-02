package taskdispatch

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var checklist = regexp.MustCompile(`^ {0,3}[-*+] \[([ xX])\] +(T-[0-9]{2,30})(?:[ \t]+)(\S.*)$`)
var candidate = regexp.MustCompile(`^ {0,3}[-*+] \[[^\]]*\] +(T-[0-9]{2,30})(?:[ \t]|$)`)

type Task struct {
	ID        string `json:"id"`
	Line      int    `json:"line"`
	Completed bool   `json:"completed"`
}

// ParseTasks accepts an intentionally restricted Markdown checklist. Ambiguous
// HTML and unclosed fences fail the whole read instead of manufacturing tasks.
func ParseTasks(text string) (map[string]Task, error) {
	if len(text) > 2*1024*1024 || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return nil, ErrInvalid
	}
	tasks := map[string]Task{}
	fence := byte(0)
	width := 0
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		if fence != 0 {
			n := 0
			for n < len(trimmed) && trimmed[n] == fence {
				n++
			}
			if indent <= 3 && n >= width && strings.TrimSpace(trimmed[n:]) == "" {
				fence = 0
			}
			continue
		}
		if indent >= 4 || strings.HasPrefix(line, "\t") {
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[0]
			width = 0
			for width < len(trimmed) && trimmed[width] == fence {
				width++
			}
			if fence == '`' && strings.Contains(trimmed[width:], "`") {
				return nil, ErrInvalid
			}
			continue
		}
		if strings.Contains(line, "<") {
			return nil, ErrInvalid
		}
		matches := checklist.FindStringSubmatch(line)
		if matches == nil {
			if candidate.MatchString(line) {
				return nil, ErrInvalid
			}
			continue
		}
		id := matches[2]
		if _, ok := tasks[id]; ok {
			return nil, ErrInvalid
		}
		tasks[id] = Task{ID: id, Line: i + 1, Completed: matches[1] != " "}
	}
	if fence != 0 {
		return nil, ErrInvalid
	}
	return tasks, nil
}
func SelectTask(text, id string) (Task, error) {
	tasks, e := ParseTasks(text)
	if e != nil {
		return Task{}, e
	}
	t, ok := tasks[id]
	if !ok {
		return Task{}, ErrNotFound
	}
	if t.Completed {
		return Task{}, ErrCompleted
	}
	return t, nil
}
