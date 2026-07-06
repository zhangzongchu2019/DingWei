package router

import "strings"

// 字头路由匹配（M8，规范 §15.1）：
//   - 多字头列表用 ; 或 ； 分隔（OR）
//   - 通配符 ? = 0/1 任意字符；* = ≥0 任意字符
//   - prefix 语义：消息需以匹配该模式的串开头

// SplitPatterns 拆分多字头列表（; / ；）。
func SplitPatterns(expr string) []string {
	raw := strings.FieldsFunc(expr, func(r rune) bool { return r == ';' || r == '；' })
	var out []string
	for _, p := range raw {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// MatchPrefix 任一字头模式命中 input 的前缀即返回 true。
func MatchPrefix(expr, input string, caseSensitive bool) bool {
	ok, _ := MatchPrefixLen(expr, input, caseSensitive)
	return ok
}

// MatchPrefixLen 返回匹配到的前缀字节长度。
// 含 * 的模式取最短可行前缀，避免 trailing * 在 strip_prefix 时吞掉整句正文；
// 不含 * 的模式取最长前缀，使 ? 的可选后缀能被一起剥离。
func MatchPrefixLen(expr, input string, caseSensitive bool) (bool, int) {
	original := input
	if !caseSensitive {
		input = strings.ToLower(input)
	}
	for _, p := range SplitPatterns(expr) {
		if !caseSensitive {
			p = strings.ToLower(p)
		}
		ends := matchOnePrefixEnds(p, input)
		if len(ends) > 0 {
			end := ends[0]
			if strings.Contains(p, "*") {
				for _, n := range ends[1:] {
					if n < end {
						end = n
					}
				}
			} else {
				for _, n := range ends[1:] {
					if n > end {
						end = n
					}
				}
			}
			return true, runeEndByteOffset(original, end)
		}
	}
	return false, 0
}

// matchOnePrefix 判断 pat 是否匹配 s 的某个前缀（pat 完全消费）。
// DP：维护 s 上可达位置集合；? = 0/1，* = ≥0。
func matchOnePrefix(pat, s string) bool {
	return len(matchOnePrefixEnds(pat, s)) > 0
}

func matchOnePrefixEnds(pat, s string) []int {
	pr := []rune(pat)
	sr := []rune(s)
	cur := map[int]bool{0: true}
	for _, pc := range pr {
		next := map[int]bool{}
		switch pc {
		case '?':
			for p := range cur {
				next[p] = true // 0 个
				if p < len(sr) {
					next[p+1] = true // 1 个
				}
			}
		case '*':
			min := len(sr) + 1
			for p := range cur {
				if p < min {
					min = p
				}
			}
			for q := min; q <= len(sr); q++ {
				next[q] = true
			}
		default:
			for p := range cur {
				if p < len(sr) && sr[p] == pc {
					next[p+1] = true
				}
			}
		}
		if len(next) == 0 {
			return nil
		}
		cur = next
	}
	out := make([]int, 0, len(cur))
	for p := range cur {
		out = append(out, p)
	}
	return out // 任一可达位置 = 一个有效前缀终点
}

// Overlaps 判断两字头模式是否相交（同作用域唯一性/覆盖检测，§15.1）。
// 语义是 pattern + 任意后续文本，因此用两个小 NFA 做求交，而不是只看字面前缀。
func Overlaps(exprA, exprB string, caseSensitive bool) bool {
	for _, a := range SplitPatterns(exprA) {
		for _, b := range SplitPatterns(exprB) {
			if !caseSensitive {
				a, b = strings.ToLower(a), strings.ToLower(b)
			}
			if patternLanguagesOverlap(a, b) {
				return true
			}
		}
	}
	return false
}

func patternLanguagesOverlap(a, b string) bool {
	ar, br := []rune(a), []rune(b)
	alphabet := patternAlphabet(ar, br)
	type state struct{ i, j int }
	queue := []state{}
	seen := map[state]bool{}
	for _, i := range epsilonClosure(ar, 0) {
		for _, j := range epsilonClosure(br, 0) {
			st := state{i, j}
			queue = append(queue, st)
			seen[st] = true
		}
	}
	for len(queue) > 0 {
		st := queue[0]
		queue = queue[1:]
		if st.i == len(ar) && st.j == len(br) {
			return true
		}
		for _, ch := range alphabet {
			for _, ni := range transition(ar, st.i, ch) {
				for _, ci := range epsilonClosure(ar, ni) {
					for _, nj := range transition(br, st.j, ch) {
						for _, cj := range epsilonClosure(br, nj) {
							next := state{ci, cj}
							if !seen[next] {
								seen[next] = true
								queue = append(queue, next)
							}
						}
					}
				}
			}
		}
	}
	return false
}

const otherRune rune = -1

func patternAlphabet(patterns ...[]rune) []rune {
	seen := map[rune]bool{otherRune: true}
	out := []rune{otherRune}
	for _, pat := range patterns {
		for _, r := range pat {
			if r == '?' || r == '*' {
				continue
			}
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	return out
}

func epsilonClosure(pat []rune, pos int) []int {
	seen := map[int]bool{}
	var out []int
	var walk func(int)
	walk = func(p int) {
		if seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
		if p >= len(pat) {
			return
		}
		if pat[p] == '?' || pat[p] == '*' {
			walk(p + 1)
		}
	}
	walk(pos)
	return out
}

func transition(pat []rune, pos int, ch rune) []int {
	if pos >= len(pat) {
		return []int{pos}
	}
	switch pat[pos] {
	case '?':
		return []int{pos + 1}
	case '*':
		return []int{pos}
	default:
		if ch != otherRune && pat[pos] == ch {
			return []int{pos + 1}
		}
		return nil
	}
}

func runeEndByteOffset(s string, endRunes int) int {
	if endRunes <= 0 {
		return 0
	}
	i := 0
	for pos := range s {
		if i == endRunes {
			return pos
		}
		i++
	}
	if i == endRunes {
		return len(s)
	}
	return len(s)
}
