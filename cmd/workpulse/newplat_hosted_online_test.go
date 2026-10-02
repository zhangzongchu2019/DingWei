package main

// 板 ac33b1cb 的接线：wireHosted 把 Hub.OnHostedAgentOnline 接到 orch.MarkOnline，并起周期清扫。
// 原先两者都没接：MarkOnline 无生产调用方，SweepTimeouts 也没人周期调用。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

func TestWireHostedOnlineHookAndSweeper(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool := pgtest.Pool(t)
	mk := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(mk, []byte(strings.Repeat("k", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DW_G4_MASTER_KEY_FILE", mk)
	saved := hostedSweepEvery
	hostedSweepEvery = 50 * time.Millisecond
	t.Cleanup(func() { hostedSweepEvery = saved })

	hub := newplatHub(t)
	idStore := identity.NewMemStore()
	svc := identity.NewService(idStore, nil, nil)
	cfg := newPlatformConfig{publicOrigin: "https://x"}
	actor := func(*http.Request) (string, error) { return "", errors.New("unused") }
	on, err := wireHosted(ctx, http.NewServeMux(), hub, &cfg, pool, idStore, svc, actor)
	if err != nil || !on {
		t.Fatalf("wireHosted: on=%v err=%v", on, err)
	}
	if hub.OnHostedAgentOnline == nil || cfg.hosted == nil {
		t.Fatal("wireHosted must set Hub.OnHostedAgentOnline and cfg.hosted")
	}

	st := hosted.NewStore(pool)
	if err := st.UpsertNode(ctx, hosted.Node{ID: "nd_w", Name: "w", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}); err != nil {
		t.Fatal(err)
	}
	starting := func(id string) {
		t.Helper()
		if err := st.CreateSeat(ctx, hosted.Seat{AgentID: id, NodeID: "nd_w", CLI: "codex", LLM: "gpt", CredKind: "token_file",
			Plan: "2c4g", ImageRef: "r", CreatedBy: "zzc"}); err != nil {
			t.Fatal(err)
		}
		if err := st.SetSeatState(ctx, id, "starting", "", "start", nil); err != nil {
			t.Fatal(err)
		}
	}

	// ① Hub 回调 ⇒ starting → online
	starting("ag_w_conn")
	hub.OnHostedAgentOnline(ctx, "ag_w_conn")
	if seat, _ := st.GetSeat(ctx, "ag_w_conn"); seat.State != "online" {
		t.Fatalf("hub callback did not mark online: %+v", seat)
	}

	// ② 周期清扫真在跑：把一席 starting 的时刻拨回 10 分钟前，清扫应在几个周期内把它清成 start_timeout
	starting("ag_w_stuck")
	tc, _ := repo.NewAccessCtx("test", "wire-hosted-online")
	if _, err := pool.Exec(ctx, tc, `UPDATE g4_hosted_seat SET updated_at = now() - interval '10 minutes' WHERE agent_id = $1`, "ag_w_stuck"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		seat, _ := st.GetSeat(ctx, "ag_w_stuck")
		if seat.State == "error" && seat.StateDetail == "start_timeout" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sweeper never timed out the stuck seat: %+v", seat)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
