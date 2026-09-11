package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// 🔴 T-066 的修法不是逐个改默认值,而是让「生效值 + 来源」出现在启动日志。
//
// 四例实证(WP_HUB_ID 回退主机名 / WP_DB_PATH 静默共库 / FCS_ADDR 绑全接口 /
// WP_NAME_ENFORCE 默认 warn)的共同点**不是「默认值选错了」** ——
// warn→enforce 的渐进放量本身是正当做法,不能一概判为缺陷。
// 真正的问题是:**默认值没有出现在任何可见处,于是无人复核,也无人知道现在处在哪一档。**
//
// 故本机制只做一件事:把「取的是谁给的值」摆到启动日志里,不改变任何行为。
// 改默认值会破坏渐进放量;打印生效值不会,且比逐个改默认值更根本。

// Observed 描述一项「带默认值、且与暴露面或多实例语义相关」的配置在本次启动时的生效情况。
type Observed struct {
	Key    string
	Value  string
	Source string // "env" = 由环境变量显式给出;"default" = 代码内默认值
	Warn   string // 非空 = 当前取的正是默认值,且该默认值有已知风险,需被看见
}

const (
	SourceEnv     = "env"
	SourceDefault = "default"
)

// Observe 读取一项配置并记录其来源。
// defaultWarn 只在**实际落到默认值**时才会出现在输出里 —— 显式配置过的不打扰。
func Observe(key, def, defaultWarn string) Observed {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return Observed{Key: key, Value: v, Source: SourceEnv}
	}
	return Observed{Key: key, Value: def, Source: SourceDefault, Warn: defaultWarn}
}

// ObserveRequired 用于**不给默认值**的配置(如 WP_HUB_ID):未配置即空值,
// 由调用方决定是否拒绝启动。此处只负责让「没配」这件事同样可见。
func ObserveRequired(key string) Observed {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return Observed{Key: key, Value: v, Source: SourceEnv}
	}
	return Observed{Key: key, Value: "", Source: SourceDefault, Warn: "未配置且无默认值"}
}

// Line 产出一行可 grep 的摘要。格式刻意固定,便于运维直接 grep 单个 key。
//
//	effective_config WP_NAME_ENFORCE=warn(default!) FCS_ADDR=:8081(default!) WP_DB_PATH=/data/workpulse.db(default)
//
// `default!` 表示「取的是默认值**且**该默认值带已知风险」—— 一眼可辨。
func Line(list []Observed) string {
	sorted := append([]Observed(nil), list...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })
	parts := make([]string, 0, len(sorted))
	for _, o := range sorted {
		tag := o.Source
		if o.Source == SourceDefault && o.Warn != "" {
			tag = "default!"
		}
		v := o.Value
		if v == "" {
			v = "<空>"
		}
		parts = append(parts, fmt.Sprintf("%s=%s(%s)", o.Key, v, tag))
	}
	return "effective_config " + strings.Join(parts, " ")
}

// Warnings 单独列出「落在有风险的默认值上」的项,供启动时逐条打 WARN。
// 与 Line 分开:Line 是全量快照(便于比对两台机器),Warnings 是需要人看见的子集。
func Warnings(list []Observed) []Observed {
	var out []Observed
	for _, o := range list {
		if o.Source == SourceDefault && o.Warn != "" {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
