package mcsd

// M1 增量3:daemon 的 hub 回执入口——真 CLI 工人经 hub 发回的 mcs_receipt_v1
// 信封,解析(hubdriver.ParseReceipt 唯一解析权威)后走既有 Receipt* 路径
// (guarded 记账+Terminal 归还 worker)。additive:不改 P3 冻结的收件面语义。

import (
	"context"
	"fmt"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/hubdriver"
)

// HandleHubReceipt 处理一条 hub 回执信封。解析失败/未知 kind/phase=显式错(入口闭合)。
func (d *Daemon) HandleHubReceipt(ctx context.Context, body []byte) error {
	r, err := hubdriver.ParseReceipt(body)
	if err != nil {
		return err
	}
	switch r.Phase {
	case "processing":
		return d.ReceiptProcessing(ctx, r.NodeID, r.Generation, r.RID)
	case "done":
		return d.ReceiptTerminal(ctx, r.NodeID, r.Generation, r.RID, r.OK, r.ResultRef)
	default: // ParseReceipt 已闭合,此处防御性兜底
		return fmt.Errorf("未知回执 phase %q", r.Phase)
	}
}
