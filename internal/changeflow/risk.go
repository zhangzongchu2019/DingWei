package changeflow

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

//go:embed default_risk_paths.yaml
var defaultRules []byte

type Rule struct {
	ID       string   `yaml:"id"`
	Category string   `yaml:"category"`
	Paths    []string `yaml:"paths"`
	Keywords []string `yaml:"keywords"`
}
type Rules struct {
	Version        string
	DefaultVersion string
	RepositoryBlob string
	Entries        []Rule
}
type ruleFile struct {
	Version int    `yaml:"version"`
	Rules   []Rule `yaml:"rules"`
}

func GitBlob(b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
func ParseRules(blob string, repo []byte) (Rules, error) {
	var out Rules
	if len(repo) > 16<<10 || (len(repo) == 0 && blob != "") || (len(repo) > 0 && (!sha.MatchString(blob) || GitBlob(repo) != blob)) {
		return out, ErrInvalid
	}
	all := [][]byte{defaultRules}
	if len(repo) > 0 {
		all = append(all, repo)
	}
	seen := map[string]bool{}
	for _, raw := range all {
		var doc yaml.Node
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		if dec.Decode(&doc) != nil {
			return out, ErrInvalid
		}
		var safe func(*yaml.Node) bool
		safe = func(n *yaml.Node) bool {
			if n.Kind == yaml.AliasNode || n.Anchor != "" {
				return false
			}
			for _, v := range n.Content {
				if !safe(v) {
					return false
				}
			}
			return true
		}
		if !safe(&doc) {
			return out, ErrInvalid
		}
		dec = yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)
		var f ruleFile
		if dec.Decode(&f) != nil || f.Version != 1 || len(f.Rules) > 256 {
			return out, ErrInvalid
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			return out, ErrInvalid
		}
		for _, r := range f.Rules {
			if !token.MatchString(r.ID) || seen[r.ID] || !category(r.Category) || len(r.Paths)+len(r.Keywords) > 128 {
				return out, ErrInvalid
			}
			seen[r.ID] = true
			for _, p := range r.Paths {
				if _, e := glob(p); e != nil {
					return out, e
				}
			}
			for _, k := range r.Keywords {
				if strings.TrimSpace(k) == "" || len(k) > 200 || strings.ContainsAny(k, "\r\n\x00") {
					return out, ErrInvalid
				}
			}
			out.Entries = append(out.Entries, r)
		}
	}
	h := sha256.Sum256(defaultRules)
	out.DefaultVersion = hex.EncodeToString(h[:])
	out.RepositoryBlob = blob
	out.Version = ruleIdentity(out)

	return out, nil
}
func ruleIdentity(r Rules) string {
	return digest(struct {
		Schema, DefaultVersion, RepositoryBlob string
		Entries                                []Rule
	}{"literal-wordfold-glob-segments-v1", r.DefaultVersion, r.RepositoryBlob, r.Entries})
}
func category(s string) bool {
	for _, v := range []string{"risk:funds", "risk:permission", "risk:data-delete", "risk:api-contract", "risk:sla", "risk:migration", "risk:privacy"} {
		if s == v {
			return true
		}
	}
	return false
}
func cleanPath(p string) bool {
	return p != "" && len(p) <= 512 && path.Clean(p) == p && !strings.HasPrefix(p, "/") && p != "." && p != ".." && !strings.HasPrefix(p, "../") && !strings.ContainsAny(p, "\\\x00\r\n")
}

// Case-sensitive repository-relative globs; * and ? never cross '/', ** can.
func glob(p string) (*regexp.Regexp, error) {
	if !cleanPath(p) || strings.ContainsAny(p, "[]{}") {
		return nil, ErrInvalid
	}
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '*':
			if i+1 < len(p) && p[i+1] == '*' {
				i++
				if i+1 < len(p) && p[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(p[i])))
		}
	}
	b.WriteByte('$')
	return regexp.Compile(b.String())
}

type FileDiff struct {
	OldPath, NewPath, Patch string
	Binary, Complete        bool
}
type RiskInput struct {
	Coordinate Coordinate
	Base, Head string
	Complete   bool
	Labels     []string
	Files      []FileDiff
}
type addedLine struct {
	n    int
	text string
}

var hunk = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@`)

func additions(patch string) ([]addedLine, error) {
	if len(patch) > 2<<20 {
		return nil, ErrInvalid
	}
	var out []addedLine
	oldLeft, newLeft, next := 0, 0, 0
	for _, line := range strings.Split(strings.TrimSuffix(patch, "\n"), "\n") {
		if m := hunk.FindStringSubmatch(line); m != nil {
			if oldLeft != 0 || newLeft != 0 {
				return nil, ErrInvalid
			}
			oldLeft = 1
			newLeft = 1
			var e error
			if m[2] != "" {
				oldLeft, e = strconv.Atoi(m[2])
				if e != nil {
					return nil, ErrInvalid
				}
			}
			if m[4] != "" {
				newLeft, e = strconv.Atoi(m[4])
				if e != nil {
					return nil, ErrInvalid
				}
			}
			next, e = strconv.Atoi(m[3])
			if e != nil || next < 0 || oldLeft > 100000 || newLeft > 100000 {
				return nil, ErrInvalid
			}
			continue
		}
		if strings.HasPrefix(line, "\\ No newline at end") {
			continue
		}
		if oldLeft == 0 && newLeft == 0 {
			if line == "" || strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "new file mode ") || strings.HasPrefix(line, "deleted file mode ") || strings.HasPrefix(line, "similarity index ") || strings.HasPrefix(line, "rename from ") || strings.HasPrefix(line, "rename to ") {
				continue
			}
			return nil, ErrInvalid
		}
		if line == "" {
			return nil, ErrInvalid
		}
		switch line[0] {
		case '+':
			if next <= 0 || newLeft <= 0 {
				return nil, ErrInvalid
			}
			out = append(out, addedLine{next, line[1:]})
			newLeft--
			next++
		case '-':
			if oldLeft <= 0 {
				return nil, ErrInvalid
			}
			oldLeft--
		case ' ':
			if oldLeft <= 0 || newLeft <= 0 {
				return nil, ErrInvalid
			}
			oldLeft--
			newLeft--
			next++
		default:
			return nil, ErrInvalid
		}
	}
	if oldLeft != 0 || newLeft != 0 {
		return nil, ErrInvalid
	}
	return out, nil
}
func keyword(text, k string) bool {
	text = strings.ToLower(text)
	k = strings.ToLower(k)
	word := func(r rune) bool { return unicode.IsLetter(r) && r < 128 || unicode.IsDigit(r) || r == '_' }
	for offset := 0; offset <= len(text); {
		i := strings.Index(text[offset:], k)
		if i < 0 {
			return false
		}
		i += offset
		end := i + len(k)
		left, right := true, true
		if len(k) > 0 && word(rune(k[0])) && i > 0 {
			left = !word(rune(text[i-1]))
		}
		if len(k) > 0 && word(rune(k[len(k)-1])) && end < len(text) {
			right = !word(rune(text[end]))
		}
		if left && right {
			return true
		}
		offset = i + 1
	}
	return false
}

// Evaluate never fetches, writes, reads a clock or returns source text.
func Evaluate(in RiskInput, rules Rules) ([]Hit, error) {
	if in.Coordinate.Validate() != nil || !sha.MatchString(in.Base) || in.Head != in.Coordinate.Commit || !in.Complete || len(rules.DefaultVersion) != 64 || rules.Version != ruleIdentity(rules) || len(in.Files) > 256 || len(in.Labels) > 128 || len(rules.Entries) == 0 || len(rules.Entries) > 263 {
		return nil, ErrUnavailable
	}
	patterns := map[string]*regexp.Regexp{}
	terms := 0
	for _, r := range rules.Entries {
		terms += len(r.Paths) + len(r.Keywords)
		if terms > 1024 || !category(r.Category) || !token.MatchString(r.ID) {
			return nil, ErrUnavailable
		}
		for _, p := range r.Paths {
			g, e := glob(p)
			if e != nil {
				return nil, ErrUnavailable
			}
			patterns[p] = g
		}
		for _, k := range r.Keywords {
			if strings.TrimSpace(k) == "" || len(k) > 200 {
				return nil, ErrUnavailable
			}
		}
	}
	work := 0
	found := map[string]Hit{}
	add := func(h Hit) { found[digest(h)] = h }
	for _, label := range in.Labels {
		if category(label) {
			add(Hit{Category: label, Rule: "label/" + label, Source: "label"})
		}
	}
	total := 0
	for _, f := range in.Files {
		total += len(f.Patch)
		if total > 4<<20 || !f.Complete || (f.OldPath == "" && f.NewPath == "") || (f.OldPath != "" && !cleanPath(f.OldPath)) || (f.NewPath != "" && !cleanPath(f.NewPath)) {
			return nil, ErrUnavailable
		}
		var lines []addedLine
		var e error
		if f.Binary {
			if f.Patch != "" {
				return nil, ErrUnavailable
			}
		} else {
			lines, e = additions(f.Patch)
			if e != nil {
				return nil, ErrUnavailable
			}
			if len(lines) > 0 && f.NewPath == "" {
				return nil, ErrUnavailable
			}
		}
		for _, r := range rules.Entries {
			if !category(r.Category) || !token.MatchString(r.ID) {
				return nil, ErrUnavailable
			}
			for _, p := range r.Paths {
				g := patterns[p]
				for _, name := range []string{f.OldPath, f.NewPath} {
					if name != "" && g.MatchString(name) {
						add(Hit{Category: r.Category, Rule: r.ID, Source: "path", Path: name})
					}
				}
			}
			for _, line := range lines {
				for _, k := range r.Keywords {
					work += len(line.text) + len(k)
					if work > 64<<20 {
						return nil, ErrUnavailable
					}
					if keyword(line.text, k) {
						add(Hit{Category: r.Category, Rule: r.ID, Source: "added_line", Path: f.NewPath, Line: line.n})
					}
				}
			}
		}
	}
	out := make([]Hit, 0, len(found))
	for _, v := range found {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Line < b.Line
	})
	return out, nil
}
