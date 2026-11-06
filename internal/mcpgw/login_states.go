package mcpgw

import (
	"context"
	"encoding/json"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
)

// LoginStates 给 C 路提供同一加密持久存储，不改动 C 的包或 state 语义。
// C 仍保存自己计算的 state 哈希、用途、AppID、回调地址和签发意图。
type LoginStates struct{ Store Store }

var _ feishulogin.StateStore = LoginStates{}

func (s LoginStates) Create(ctx context.Context, a feishulogin.LoginAttempt) error {
	if s.Store == nil || a.StateHash == "" || a.AppID == "" {
		return feishulogin.ErrConfig
	}
	b, e := json.Marshal(a)
	if e != nil {
		return e
	}
	return s.Store.Put(ctx, "cstate", a.StateHash, b, a.ExpiresAt, a.CreatedAt)
}
func (s LoginStates) Consume(ctx context.Context, key string, now time.Time) (feishulogin.LoginAttempt, error) {
	var a feishulogin.LoginAttempt
	if s.Store == nil {
		return a, feishulogin.ErrConfig
	}
	b, e := s.Store.Take(ctx, "cstate", key, now)
	if e != nil {
		return a, feishulogin.ErrInvalidState
	}
	if json.Unmarshal(b, &a) != nil {
		return a, feishulogin.ErrInvalidState
	}
	return a, nil
}
