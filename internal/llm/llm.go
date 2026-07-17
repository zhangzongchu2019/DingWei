// Package llm 是 LLM Provider 抽象 + 双 LLM 故障转移（规范 §15.3）。
//
// 两套 provider（primary + backup），主失败自动切备；都失败 → 返回 ErrAllDown，
// 上层据此降级（路由：「LLM 失效，无法识别指令」；对账：暂停，不误判）。
package llm

import (
	"context"
	"errors"
)

// ErrAllDown 两套 LLM 都不可用。
var ErrAllDown = errors.New("llm: all providers unavailable")

// Provider LLM 提供方（抽取 / 意图判定 / 问答）。
type Provider interface {
	Name() string
	// Complete 通用补全；prompt 已含 system+user，要求结构化 JSON 时由调用方校验。
	Complete(ctx context.Context, system, user string) (string, error)
}

// Failover 双（多）provider 故障转移：按序尝试，全部失败返回 ErrAllDown。
type Failover struct {
	Providers []Provider // [primary, backup, ...]
}

func (f *Failover) Name() string { return "failover" }

func (f *Failover) Complete(ctx context.Context, system, user string) (string, error) {
	var last error
	for _, p := range f.Providers {
		out, err := p.Complete(ctx, system, user)
		if err == nil {
			return out, nil
		}
		last = err
	}
	if last == nil {
		last = ErrAllDown
	}
	return "", errors.Join(ErrAllDown, last)
}

// Healthy 是否至少一套可用（M9 监控用）。
func (f *Failover) Healthy(ctx context.Context) bool {
	for _, p := range f.Providers {
		if _, err := p.Complete(ctx, "ping", "ping"); err == nil {
			return true
		}
	}
	return false
}

// Stub 占位 provider（始终失败，便于测试降级路径）。
type Stub struct{ ID string }

func (s Stub) Name() string { return "stub:" + s.ID }
func (s Stub) Complete(ctx context.Context, system, user string) (string, error) {
	return "", errors.New("llm stub: not configured")
}
