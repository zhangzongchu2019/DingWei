package router

import "testing"

func TestParseCommand_AllCommands(t *testing.T) {
	tests := []struct {
		text string
		cmd  Command
	}{
		{"+ 07/20 任务", CmdAdd},
		{"- 关键词", CmdDelete},
		{"改 任务 07/05", CmdModify},
		{"顺延 07/20 +3天", CmdPostpone},
		{"进度 任务 完成50%", CmdProgress},
		{"完成 任务", CmdDone},
		{"done 任务", CmdDone},
		{"结果 任务 内容", CmdResult},
		{"风险 环境不稳", CmdRisk},
		{"风险解除 环境", CmdRisk},
		{"确认", CmdConfirm},
		{"是", CmdConfirm},
		{"ok", CmdConfirm},
		{"取消", CmdCancel},
		{"全量", CmdReplace},
		{"替换", CmdReplace},
		{"帮助", CmdHelp},
		{"格式", CmdHelp},
		{"?", CmdHelp},
		{"认领 sess1", CmdClaim},
		{"申诉 时间不合适", CmdAppeal},
		{"随便聊聊", CmdNone},
		{"", CmdNone},
	}
	for _, tt := range tests {
		got := ParseCommand(tt.text)
		if got != tt.cmd {
			t.Errorf("ParseCommand(%q)=%s, want %s", tt.text, got, tt.cmd)
		}
	}
}

func TestDecide_LevelCommand(t *testing.T) {
	d := Decide("+ 07/20 任务", nil)
	if d.Level != LevelCommand || d.Command != CmdAdd {
		t.Fatalf("Decide(+)=%+v", d)
	}
}

func TestDecide_LevelLLM(t *testing.T) {
	d := Decide("帮我查排期", []string{"@bot", "/", "帮我"})
	if d.Level != LevelLLM {
		t.Fatalf("Decide with trigger: Level=%v want LevelLLM", d.Level)
	}
}

func TestDecide_LevelSilent(t *testing.T) {
	d := Decide("今天天气不错", nil)
	if d.Level != LevelSilent {
		t.Fatalf("Decide sans trigger: Level=%v want LevelSilent", d.Level)
	}
	d2 := Decide("普通聊天", []string{"@bot", "/"})
	if d2.Level != LevelSilent {
		t.Fatalf("Decide with unmatched triggers: Level=%v want LevelSilent", d2.Level)
	}
}

func TestHasTrigger(t *testing.T) {
	if !hasTrigger("hello @bot help", []string{"@bot", "/"}) {
		t.Fatal("hasTrigger with @bot should be true")
	}
	if hasTrigger("no match", []string{"@bot", "/"}) {
		t.Fatal("hasTrigger without match should be false")
	}
	if hasTrigger("text", nil) {
		t.Fatal("hasTrigger with empty triggers should be false")
	}
	if hasTrigger("text", []string{""}) {
		t.Fatal("hasTrigger with empty trigger string should be false")
	}
}
