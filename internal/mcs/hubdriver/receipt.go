package hubdriver

// M1 增量2:回执桥——worker 会话经 hub 发回的回执信封,解析后按 guarded 记账入 board。
// 语义:M0 3.1(至少一次投递+执行点恰一次:rid 幂等由 board 谓词拦)/迟到回执由
// board Identity(owner,epoch) guard 拒(M0 v0.7 C-6 域;本层只透传不裁决)。
// 入口闭合(P3 集成件教训):未知 kind/phase 显式拒,不静默丢、不猜。

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/board"
)

// WireReceipt 是回执线格式(与 WireEnvelope 对偶;worker 侧按此回)。
type WireReceipt struct {
	Kind       string `json:"kind"` // 恒 "mcs_receipt_v1"
	NodeID     string `json:"node_id"`
	Generation int64  `json:"generation"`
	RID        string `json:"rid"`             // 必须回传派发信封里的 rid(幂等键)
	Phase      string `json:"phase"`           // processing | done
	OK         bool   `json:"ok"`              // phase=done 时有效
	ResultRef  string `json:"result_ref"`      // phase=done 时产物指针
	From       string `json:"from,omitempty"`  // worker 全名(对账)
}

// ReceiptSink 把回执落账。Identity=调度者身份(owner,epoch),guard 由 board 谓词执行。
type ReceiptSink struct {
	B  *board.Board
	ID board.Identity
}

// ParseReceipt 解析+校验回执线格式(kind/phase/必填字段)。入口闭合:未知值显式拒。
// 唯一解析权威——ReceiptSink 与 mcsd daemon hub 入口共用,防两处解析漂移。
func ParseReceipt(body []byte) (WireReceipt, error) {
	var r WireReceipt
	if err := json.Unmarshal(body, &r); err != nil {
		return r, fmt.Errorf("回执解析失败: %w", err)
	}
	if r.Kind != "mcs_receipt_v1" {
		return r, fmt.Errorf("未知回执 kind %q(入口闭合,拒)", r.Kind)
	}
	if r.NodeID == "" || r.RID == "" {
		return r, fmt.Errorf("回执缺 node_id/rid(拒)")
	}
	if r.Phase != "processing" && r.Phase != "done" {
		return r, fmt.Errorf("未知回执 phase %q(入口闭合,拒)", r.Phase)
	}
	return r, nil
}

// HandleBody 解析并入账一条回执信封。返回 board 判定结果;解析/校验失败=显式错。
func (s *ReceiptSink) HandleBody(ctx context.Context, body []byte) (board.Outcome, error) {
	r, err := ParseReceipt(body)
	if err != nil {
		return board.OutcomeRejected, err
	}
	return s.Handle(ctx, r)
}

// Handle 入账已解析回执。注意:本层=纯账语义(无池);带 worker 归还的常驻路径
// 走 mcsd Daemon.HandleHubReceipt。
func (s *ReceiptSink) Handle(ctx context.Context, r WireReceipt) (board.Outcome, error) {
	if r.Kind != "mcs_receipt_v1" {
		return board.OutcomeRejected, fmt.Errorf("未知回执 kind %q(入口闭合,拒)", r.Kind)
	}
	if r.NodeID == "" || r.RID == "" {
		return board.OutcomeRejected, fmt.Errorf("回执缺 node_id/rid(拒)")
	}
	switch r.Phase {
	case "processing":
		return s.B.Processing(ctx, r.NodeID, r.Generation, r.RID, s.ID)
	case "done":
		return s.B.Terminal(ctx, r.NodeID, r.Generation, r.RID, r.OK, r.ResultRef, s.ID)
	default:
		return board.OutcomeRejected, fmt.Errorf("未知回执 phase %q(入口闭合,拒)", r.Phase)
	}
}
