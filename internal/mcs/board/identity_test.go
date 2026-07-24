package board

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
)

// P3 预制:身份 enforce(契约 C4)。默认 warn(引擎内部名放行+每名恰一次警事件);
// enforce 开=非法名拒登记+identity_rejected 事件(拒收本身可审计)。
func TestIdentity_WarnDefaultThenEnforce(t *testing.T) {
	ctx := context.Background()
	b, err := Open(":memory:", &clock.Fake{T: time.Unix(10_000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.Takeover(ctx, "board1", "orch1", 3600)
	// warn(默认):裸名放行+identity_warned 恰一次(去重)
	if err := b.RegisterAgent(ctx, "w0", "claude"); err != nil {
		t.Fatalf("warn 模式应放行:%v", err)
	}
	if err := b.RegisterAgent(ctx, "w0", "claude"); err != nil {
		t.Fatal(err)
	}
	var warned int
	b.db.QueryRow(`SELECT COUNT(*) FROM board_event WHERE kind='identity_warned'`).Scan(&warned)
	if warned != 1 {
		t.Fatalf("identity_warned=%d want 恰1(去重)", warned)
	}
	// 合法名:零事件
	if err := b.RegisterAgent(ctx, "zzc-worker1-2642", "claude"); err != nil {
		t.Fatal(err)
	}
	// enforce:裸名拒+identity_rejected 事件;合法名照常
	b.SetIdentityEnforce(true)
	err = b.RegisterAgent(ctx, "developer-out", "claude")
	var rej *ErrIdentityRejected
	if !errors.As(err, &rej) {
		t.Fatalf("enforce 应拒:%v", err)
	}
	var rejected int
	b.db.QueryRow(`SELECT COUNT(*) FROM board_event WHERE kind='identity_rejected'`).Scan(&rejected)
	if rejected != 1 {
		t.Fatalf("identity_rejected=%d want 1(拒收可审计)", rejected)
	}
	var agentCount int
	b.db.QueryRow(`SELECT COUNT(*) FROM agent WHERE agent_id='developer-out'`).Scan(&agentCount)
	if agentCount != 0 {
		t.Fatal("拒收必须零写入")
	}
	if err := b.RegisterAgent(ctx, "zzc-worker2-2642", "claude"); err != nil {
		t.Fatalf("enforce 下合法名应过:%v", err)
	}
}
