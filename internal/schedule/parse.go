package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// OpKind 排期操作类型。
type OpKind string

const (
	OpAdd      OpKind = "add"      // + MM/DD-MM/DD 任务
	OpDelete   OpKind = "delete"   // - 关键词
	OpModify   OpKind = "modify"   // 改 关键词 MM/DD-MM/DD
	OpPostpone OpKind = "postpone" // 顺延 MM/DD +N天
	OpReplace  OpKind = "replace"  // 全量 + 多行
)

// Op 一条解析后的排期操作。
type Op struct {
	Kind    OpKind
	Start   string // YYYY-MM-DD
	End     string
	Task    string
	Keyword string
	Anchor  string // 顺延锚点 YYYY-MM-DD
	Days    int
	Items   []Op // 全量替换的条目（均为 OpAdd）
}

// ParseLines 解析多行指令为操作序列。now/loc 用于日期推断（§15.4）。
func ParseLines(text string, now time.Time, loc *time.Location) ([]Op, error) {
	lines := splitNonEmpty(text)
	if len(lines) == 0 {
		return nil, nil
	}
	// 全量替换：首行 全量/替换，其余为条目
	if f := firstToken(lines[0]); f == "全量" || f == "替换" {
		var items []Op
		body := lines[1:]
		// 允许 "全量" 同行无内容；条目可带或不带前缀 +
		for _, ln := range body {
			ln = strings.TrimSpace(strings.TrimPrefix(ln, "+"))
			op, err := parseAdd(ln, now, loc)
			if err != nil {
				return nil, err
			}
			items = append(items, op)
		}
		return []Op{{Kind: OpReplace, Items: items}}, nil
	}

	var ops []Op
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(ln, "+"):
			op, err := parseAdd(strings.TrimSpace(ln[1:]), now, loc)
			if err != nil {
				return nil, err
			}
			ops = append(ops, op)
		case strings.HasPrefix(ln, "-"):
			kw := strings.TrimSpace(ln[1:])
			if kw == "" {
				return nil, fmt.Errorf("删除格式：- 关键词")
			}
			ops = append(ops, Op{Kind: OpDelete, Keyword: kw})
		case firstToken(ln) == "改":
			op, err := parseModify(ln, now, loc)
			if err != nil {
				return nil, err
			}
			ops = append(ops, op)
		case firstToken(ln) == "顺延":
			op, err := parsePostpone(ln, now, loc)
			if err != nil {
				return nil, err
			}
			ops = append(ops, op)
		default:
			return nil, fmt.Errorf("无法识别的排期指令：%q（用 + / - / 改 / 顺延 / 全量）", ln)
		}
	}
	return ops, nil
}

// parseAdd 解析 "MM/DD-MM/DD 任务"。
func parseAdd(s string, now time.Time, loc *time.Location) (Op, error) {
	dr, task := cut2(s)
	if dr == "" || task == "" {
		return Op{}, fmt.Errorf("新增格式：+ MM/DD-MM/DD 任务 — %q", s)
	}
	start, end, err := ParseDateRange(dr, now, loc)
	if err != nil {
		return Op{}, err
	}
	return Op{Kind: OpAdd, Start: start, End: end, Task: strings.TrimSpace(task)}, nil
}

// parseModify 解析 "改 关键词 MM/DD-MM/DD"。
func parseModify(s string, now time.Time, loc *time.Location) (Op, error) {
	fields := strings.Fields(s)
	if len(fields) < 3 {
		return Op{}, fmt.Errorf("改期格式：改 关键词 MM/DD-MM/DD — %q", s)
	}
	dr := fields[len(fields)-1]
	keyword := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, fields[0]), dr))
	keyword = strings.TrimSpace(keyword)
	start, end, err := ParseDateRange(dr, now, loc)
	if err != nil {
		return Op{}, err
	}
	if keyword == "" {
		return Op{}, fmt.Errorf("改期需关键词：改 关键词 MM/DD-MM/DD")
	}
	return Op{Kind: OpModify, Keyword: keyword, Start: start, End: end}, nil
}

// parsePostpone 解析 "顺延 MM/DD +N天"。
func parsePostpone(s string, now time.Time, loc *time.Location) (Op, error) {
	fields := strings.Fields(s)
	if len(fields) < 3 {
		return Op{}, fmt.Errorf("顺延格式：顺延 MM/DD +N天 — %q", s)
	}
	anchor, _, err := ParseDateRange(fields[1], now, loc)
	if err != nil {
		return Op{}, err
	}
	numStr := strings.NewReplacer("天", "", "+", "", "日", "").Replace(fields[2])
	n, err := strconv.Atoi(strings.TrimSpace(numStr))
	if err != nil {
		return Op{}, fmt.Errorf("顺延天数非法：%q", fields[2])
	}
	return Op{Kind: OpPostpone, Anchor: anchor, Days: n}, nil
}

// ---- helpers ----

func splitNonEmpty(text string) []string {
	var out []string
	for _, ln := range strings.Split(text, "\n") {
		if strings.TrimSpace(ln) != "" {
			out = append(out, strings.TrimSpace(ln))
		}
	}
	return out
}

func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

// cut2 取首个空白前为第一段，其余为第二段。
func cut2(s string) (first, rest string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], strings.TrimSpace(s[i+1:])
	}
	return s, ""
}
