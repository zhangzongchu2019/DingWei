package nodegate

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
)

// 并入自复验件 REVIEW-G4-T50-REWORK-tosonnet55tc2k-20261001.md 附录 B（opus50tc2k 承改派 ⑤ 再修，板 3d0fc58f）。
// 原注：独立复验（tosonnet55tc2k）：把真 Gate 与 Orchestrator.ReconcileNode 接起来（装配里就是
// gate.OnConnect = orch.ReconcileNode），节点重连后，对账补发的 destroy_seat 必须能收到结果、
// 写下 reconcile_done。作者的用例分开测：hosted 里用假网关、nodegate 里只测回调「触发了」，没有测两者相接。
func TestZZ_ReconcileOnRealGateReachesDone(t *testing.T) {
	g, st, nodeID, secret := mkGate(t)
	ctx := context.Background()
	if err := st.CreateSeat(ctx, hosted.Seat{AgentID: "ag_recon1", NodeID: nodeID, CLI: "codex", LLM: "gpt", CredKind: "token_file",
		AccountRef: sql.NullString{}, Plan: "2c4g", ImageRef: "hub.local/dingwei/x@sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", CreatedBy: "zzc"}); err != nil {
		t.Fatalf("create seat: %v", err)
	}
	if err := st.SetSeatState(ctx, "ag_recon1", "destroyed", "", "destroy", json.RawMessage(`{"backup":"bk://x","ttl_days":14,"reconcile_pending":true}`)); err != nil {
		t.Fatal(err)
	}
	orch := hosted.NewOrchestrator(st, nil, nil, g, nil, nil)
	g.OnConnect = orch.ReconcileNode // 与 wireHosted 同一接法
	g.SetSendTimeout(3 * time.Second)

	srv := httptest.NewServer(g.Handler())
	defer srv.Close()
	conn := dial(t, srv, nodeID, secret)
	defer conn.CloseNow()
	// 假节点：收到任何指令就回 ok
	go func() {
		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var rc struct {
				CommandID string `json:"command_id"`
				Type      string `json:"type"`
			}
			if json.Unmarshal(raw, &rc) != nil || rc.CommandID == "" {
				continue
			}
			t.Logf("ZZ node received %s", rc.Type)
			resp, _ := json.Marshal(map[string]any{"command_id": rc.CommandID, "ok": true})
			_ = conn.Write(ctx, websocket.MessageText, resp)
		}
	}()
	// 等最多 8 秒（>sendMax 3s）：对账应在这期间完成
	start := time.Now()
	deadline := start.Add(8 * time.Second)
	for time.Now().Before(deadline) {
		ids, err := st.SeatsPendingReconcile(ctx, nodeID)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) == 0 {
			t.Logf("ZZ reconcile finished after %v", time.Since(start))
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	ids, _ := st.SeatsPendingReconcile(ctx, nodeID)
	t.Fatalf("seat still pending reconcile after 8s although the node is connected and answers ok: %v", ids)
}
