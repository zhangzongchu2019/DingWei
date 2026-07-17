package schedule

import (
	"fmt"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Change 一条具体待应用变更（confirm 时直接执行，不再重算，规范 §M1 两步式写）。
type Change struct {
	Label  string         `json:"label"`  // 🆕/❌/✏️/⏩
	Action string         `json:"action"` // insert|delete|update
	New    model.Schedule `json:"new,omitempty"`
	Old    model.Schedule `json:"old,omitempty"`
}

// ComputeDiff 对照当前排期计算具体变更集 + 预览文本。
func ComputeDiff(current []model.Schedule, ops []Op, ownerKey string, loc *time.Location) ([]Change, string, error) {
	var changes []Change
	for _, op := range ops {
		switch op.Kind {
		case OpAdd:
			changes = append(changes, Change{Label: "🆕", Action: "insert",
				New: mkSched(ownerKey, op.Start, op.End, op.Task)})
		case OpDelete:
			for _, s := range matchKeyword(current, op.Keyword) {
				changes = append(changes, Change{Label: "❌", Action: "delete", Old: s})
			}
		case OpModify:
			for _, s := range matchKeyword(current, op.Keyword) {
				changes = append(changes, Change{Label: "✏️", Action: "update",
					Old: s, New: mkSched(ownerKey, op.Start, op.End, s.Task)})
			}
		case OpPostpone:
			for _, s := range current {
				if s.StartDate >= op.Anchor { // YYYY-MM-DD 字典序=日期序
					ns, err := shiftDate(s.StartDate, op.Days, loc)
					if err != nil {
						return nil, "", err
					}
					ne, err := shiftDate(s.EndDate, op.Days, loc)
					if err != nil {
						return nil, "", err
					}
					changes = append(changes, Change{Label: "⏩", Action: "update",
						Old: s, New: mkSched(ownerKey, ns, ne, s.Task)})
				}
			}
		case OpReplace:
			for _, s := range current {
				changes = append(changes, Change{Label: "❌", Action: "delete", Old: s})
			}
			for _, it := range op.Items {
				changes = append(changes, Change{Label: "🆕", Action: "insert",
					New: mkSched(ownerKey, it.Start, it.End, it.Task)})
			}
		}
	}
	return changes, renderPreview(changes), nil
}

func renderPreview(changes []Change) string {
	if len(changes) == 0 {
		return "无变化。"
	}
	var b strings.Builder
	b.WriteString("将变更如下：\n")
	for _, c := range changes {
		switch c.Action {
		case "insert":
			fmt.Fprintf(&b, "%s 新增 %s~%s %s\n", c.Label, c.New.StartDate, c.New.EndDate, c.New.Task)
		case "delete":
			fmt.Fprintf(&b, "%s 删除 %s~%s %s\n", c.Label, c.Old.StartDate, c.Old.EndDate, c.Old.Task)
		case "update":
			if c.Label == "⏩" {
				fmt.Fprintf(&b, "%s 顺延 %s：%s → %s\n", c.Label, c.Old.Task, c.Old.StartDate, c.New.StartDate)
			} else {
				fmt.Fprintf(&b, "%s 改期 %s：%s~%s → %s~%s\n", c.Label, c.Old.Task, c.Old.StartDate, c.Old.EndDate, c.New.StartDate, c.New.EndDate)
			}
		}
	}
	b.WriteString("回『确认』生效 / 『取消』放弃。")
	return b.String()
}

func matchKeyword(current []model.Schedule, kw string) []model.Schedule {
	var out []model.Schedule
	for _, s := range current {
		if strings.Contains(s.Task, kw) {
			out = append(out, s)
		}
	}
	return out
}

func mkSched(owner, start, end, task string) model.Schedule {
	return model.Schedule{OwnerKey: owner, StartDate: start, EndDate: end, Task: task, Status: "planned", Priority: 100}
}
