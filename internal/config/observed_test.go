package config

import (
	"strings"
	"testing"
)

// T-066:本机制唯一的职责是「让生效值与来源可见」。
// 因此测试的判据全部围绕**可见性**,而不是「默认值是什么」——
// 后者会随渐进放量变化,钉死它反而会阻碍 warn→enforce 这类正当调整。

func TestObserve_EnvWins(t *testing.T) {
	t.Setenv("T066_X", "from-env")
	o := Observe("T066_X", "the-default", "默认值有风险")
	if o.Value != "from-env" || o.Source != SourceEnv {
		t.Fatalf("显式配置应优先且标 env: %+v", o)
	}
	if o.Warn != "" {
		t.Fatalf("已显式配置就不该再打扰,Warn 必须为空: %+v", o)
	}
}

func TestObserve_FallsBackAndCarriesWarn(t *testing.T) {
	t.Setenv("T066_Y", "")
	o := Observe("T066_Y", "the-default", "绑全接口")
	if o.Value != "the-default" || o.Source != SourceDefault {
		t.Fatalf("未配置应落默认值并标 default: %+v", o)
	}
	if o.Warn != "绑全接口" {
		t.Fatalf("落默认值时必须带上风险说明: %+v", o)
	}
}

// 🔴 空白串等同未配置 —— 实战里 supervisor/compose 很容易写出 `FCS_ADDR=`。
// 若把空串当成「已显式配置」,就会既不落默认值也不告警,是最坏的一种静默。
func TestObserve_BlankIsNotConfigured(t *testing.T) {
	t.Setenv("T066_Z", "   ")
	o := Observe("T066_Z", "dflt", "w")
	if o.Source != SourceDefault {
		t.Fatalf("纯空白应视为未配置: %+v", o)
	}
}

func TestObserveRequired_MissingIsVisible(t *testing.T) {
	t.Setenv("T066_REQ", "")
	o := ObserveRequired("T066_REQ")
	if o.Warn == "" {
		t.Fatalf("无默认值且未配置,必须可见: %+v", o)
	}
}

// Line 的判据:**有风险的默认值必须与普通默认值在视觉上可区分**。
// 若两者输出相同,运维扫一眼日志就分不出哪项需要处理 —— 等于没打印。
func TestLine_RiskyDefaultIsDistinguishable(t *testing.T) {
	t.Setenv("T066_A", "")
	t.Setenv("T066_B", "")
	risky := Observe("T066_A", "0.0.0.0:8081", "绑全接口")
	plain := Observe("T066_B", "/data/x.db", "")
	line := Line([]Observed{risky, plain})

	if !strings.Contains(line, "T066_A=0.0.0.0:8081(default!)") {
		t.Fatalf("有风险的默认值应标 default!:\n%s", line)
	}
	if !strings.Contains(line, "T066_B=/data/x.db(default)") {
		t.Fatalf("普通默认值应标 default:\n%s", line)
	}
	if strings.Contains(line, "T066_B=/data/x.db(default!)") {
		t.Fatalf("普通默认值不得被标成有风险:\n%s", line)
	}
}

// 阳性对照:若 Line 对两类默认值输出完全一致,上一条测试就成了摆设。
// 本条直接断言「两者输出不同」,与上一条互为补充——
// 上一条验格式正确,本条验**该机制确实在区分**。
func TestLine_PositiveControl_TwoKindsDiffer(t *testing.T) {
	t.Setenv("T066_C", "")
	risky := Observe("T066_C", "v", "有风险")
	plain := Observe("T066_C", "v", "")
	if Line([]Observed{risky}) == Line([]Observed{plain}) {
		t.Fatal("有风险与无风险的默认值输出相同 ⇒ 区分能力不存在,前一条测试不可信")
	}
}

func TestWarnings_OnlyRiskyDefaults(t *testing.T) {
	t.Setenv("T066_D", "set")
	t.Setenv("T066_E", "")
	t.Setenv("T066_F", "")
	list := []Observed{
		Observe("T066_D", "d", "有风险"), // 已显式配置 ⇒ 不该入列
		Observe("T066_E", "d", "有风险"), // 落默认且有风险 ⇒ 入列
		Observe("T066_F", "d", ""),    // 落默认但无风险 ⇒ 不该入列
	}
	w := Warnings(list)
	if len(w) != 1 || w[0].Key != "T066_E" {
		t.Fatalf("Warnings 只应含「落在有风险默认值上」的项,实得: %+v", w)
	}
}

// 固定排序:两台机器的启动日志要能直接 diff,顺序抖动会让比对失效。
func TestLine_StableOrder(t *testing.T) {
	t.Setenv("T066_M", "")
	t.Setenv("T066_N", "")
	a := Observe("T066_N", "1", "")
	b := Observe("T066_M", "2", "")
	if Line([]Observed{a, b}) != Line([]Observed{b, a}) {
		t.Fatal("输入顺序不同导致输出不同 ⇒ 跨机 diff 会产生假差异")
	}
}
