// Package router 实现三级路由（规范 §4.1）与 M8 字头匹配/重叠检测（§15.1）。
//
//	① 显式结构化指令（确定性，不唤起 LLM）
//	② 唤起符号 → LLM 意图判定
//	③ 无符号 → 静默
package router

import "strings"

// Level 路由层级。
type Level int

const (
	LevelCommand Level = iota // ① 结构化指令
	LevelLLM                  // ② 符号唤起 LLM
	LevelSilent               // ③ 静默
)

// Command 结构化指令类型。
type Command string

const (
	CmdAdd      Command = "add"      // +
	CmdDelete   Command = "delete"   // -
	CmdModify   Command = "modify"   // 改
	CmdPostpone Command = "postpone" // 顺延
	CmdProgress Command = "progress" // 进度
	CmdDone     Command = "done"     // 完成
	CmdResult   Command = "result"   // 结果
	CmdRisk     Command = "risk"     // 风险
	CmdConfirm  Command = "confirm"  // 确认
	CmdCancel   Command = "cancel"   // 取消
	CmdReplace  Command = "replace"  // 全量
	CmdHelp     Command = "help"     // 帮助
	CmdClaim    Command = "claim"    // 认领
	CmdAppeal   Command = "appeal"   // 申诉
	CmdNone     Command = ""
)

// Decision 路由结果。
type Decision struct {
	Level   Level
	Command Command // LevelCommand 时有效
}

// ParseCommand 识别显式结构化指令（每行/首词）。
func ParseCommand(text string) Command {
	t := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(t, "+"):
		return CmdAdd
	case strings.HasPrefix(t, "-"):
		return CmdDelete
	}
	first := firstToken(t)
	switch first {
	case "改":
		return CmdModify
	case "顺延":
		return CmdPostpone
	case "进度":
		return CmdProgress
	case "完成", "done":
		return CmdDone
	case "结果":
		return CmdResult
	case "风险", "风险解除":
		return CmdRisk
	case "确认", "是", "ok":
		return CmdConfirm
	case "取消":
		return CmdCancel
	case "全量", "替换":
		return CmdReplace
	case "帮助", "格式", "?":
		return CmdHelp
	case "认领":
		return CmdClaim
	case "申诉":
		return CmdAppeal
	}
	return CmdNone
}

// Decide 三级路由判定。triggers 为配置的唤起符号集合（§4.4）。
func Decide(text string, triggers []string) Decision {
	if c := ParseCommand(text); c != CmdNone {
		return Decision{Level: LevelCommand, Command: c}
	}
	if hasTrigger(text, triggers) {
		return Decision{Level: LevelLLM}
	}
	return Decision{Level: LevelSilent}
}

func hasTrigger(text string, triggers []string) bool {
	t := strings.TrimSpace(text)
	for _, tr := range triggers {
		if tr != "" && strings.Contains(t, tr) {
			return true
		}
	}
	return false
}

func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t\n"); i >= 0 {
		return s[:i]
	}
	return s
}
