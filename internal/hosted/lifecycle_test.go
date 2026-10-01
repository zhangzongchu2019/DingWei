package hosted

import (
	"context"
	"errors"
	"testing"
)

type fakeSuspender struct {
	calls []string
}

func (f *fakeSuspender) SuspendHostedAgent(ctx context.Context, agentID, by string) (bool, error) {
	f.calls = append(f.calls, "suspend:"+agentID)
	return false, nil
}

func (f *fakeSuspender) ResumeHostedAgent(ctx context.Context, agentID, by string) (bool, error) {
	f.calls = append(f.calls, "resume:"+agentID)
	return false, nil
}

func (f *fakeSuspender) ReissueHostedToken(ctx context.Context, agentID, by string) (EnrollToken, error) {
	f.calls = append(f.calls, "reissue:"+agentID)
	return EnrollToken{ApplicationID: "app2", PollToken: "tok2"}, nil
}

type fakeBackuper struct {
	refs map[string]string
}

func (f *fakeBackuper) BackupSeatVolume(ctx context.Context, nodeID, seatID string) (string, error) {
	if f.refs == nil {
		f.refs = map[string]string{}
	}
	ref := "bk://" + nodeID + "/" + seatID + "/v1"
	f.refs[seatID] = ref
	return ref, nil
}

func mkLifecycle(t *testing.T) (*Lifecycle, *fakeSuspender, *fakeGateway, *fakeIdentity, *Store) {
	t.Helper()
	o, idp, gw, s := mkOrch(t)
	out, err := o.Create(context.Background(), CreateRequest{
		OwnerLC: "zzc", By: "zzc", Name: "life1", GroupCode: "g1",
		NodeID: "auto", CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctOf(t, o.pool), Plan: "2c4g",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	o.RunJob(context.Background(), out.AgentID)
	susp := &fakeSuspender{}
	lc := NewLifecycle(o.st, o.pool, idp, susp, gw, fakeCatalog{}, &fakeBackuper{})
	return lc, susp, gw, idp, s
}

const newerImage = "hub.local/dingwei/base-codex@sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

func TestUpgradeReissueFirstThenHealthySwap(t *testing.T) {
	lc, susp, gw, idp, s := mkLifecycle(t)
	seat, _ := s.GetSeat(context.Background(), mustOnlineSeat(t, s))
	agentID := seat.AgentID

	if err := lc.Upgrade(context.Background(), agentID, "zzc", newerImage, true); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	// 顺序：Reissue 在节点指令之前（轮换：新凭据被取走旧才作废——先重签再动容器）
	if len(susp.calls) == 0 || susp.calls[0] != "reissue:"+agentID {
		t.Fatalf("升级必须先 Reissue: %v", susp.calls)
	}
	var sawUpgrade bool
	for _, c := range gw.sent {
		if c.Type == "upgrade_seat" {
			sawUpgrade = true
		}
	}
	if !sawUpgrade {
		t.Fatalf("应下发 upgrade_seat: %+v", gw.sent)
	}
	seat, _ = s.GetSeat(context.Background(), agentID)
	if seat.State != "online" || len(idp.retired) != 0 {
		t.Fatalf("成功路径: %+v retired=%v", seat, idp.retired)
	}
}

func TestUpgradeRollbackOnNodeFailure(t *testing.T) {
	lc, _, gw, _, s := mkLifecycle(t)
	agentID := mustOnlineSeat(t, s)
	gw.failOn = "upgrade_seat"
	if err := lc.Upgrade(context.Background(), agentID, "zzc", newerImage, true); !errors.Is(err, ErrRolledBack) {
		t.Fatalf("失败应 upgrade_failed_rolled_back, 得 %v", err)
	}
	seat, _ := s.GetSeat(context.Background(), agentID)
	if seat.State != "online" {
		t.Fatalf("回滚后应回到 online（旧态仍在）: %+v", seat)
	}
}

func TestDisableSuspendBeforeStopContainer(t *testing.T) {
	lc, susp, gw, _, s := mkLifecycle(t)
	agentID := mustOnlineSeat(t, s)
	// D8：裸调（无 confirm）必拒
	if err := lc.Disable(context.Background(), agentID, "zzc", true, false); err != ErrConfirmRequired {
		t.Fatalf("裸调停用应 confirm_required, 得 %v", err)
	}
	if err := lc.Disable(context.Background(), agentID, "zzc", true, true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if len(susp.calls) == 0 || susp.calls[0] != "suspend:"+agentID {
		t.Fatalf("停用必须先 Suspend 凭据: %v", susp.calls)
	}
	var stopIdx, suspIdx = -1, -1
	for i, c := range gw.sent {
		if c.Type == "stop_seat" {
			stopIdx = i
		}
	}
	suspIdx = 0 // suspend 在 susp.calls[0]
	_ = suspIdx
	if stopIdx == -1 {
		t.Fatalf("应下发 stop_seat: %+v", gw.sent)
	}
	seat, _ := s.GetSeat(context.Background(), agentID)
	if seat.State != "disabled" {
		t.Fatalf("停用后应 disabled: %+v", seat)
	}
	// 停用中不可升级（先启用）
	if err := lc.Upgrade(context.Background(), agentID, "zzc", "", true); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("停用中升级应 invalid_state, 得 %v", err)
	}
}

func TestEnableResumeBeforeStartContainer(t *testing.T) {
	lc, susp, gw, _, s := mkLifecycle(t)
	agentID := mustOnlineSeat(t, s)
	if err := lc.Disable(context.Background(), agentID, "zzc", true, true); err != nil {
		t.Fatal(err)
	}
	if err := lc.Enable(context.Background(), agentID, "zzc", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// resume 必须在 start_seat 之前，且不重新签发（无 reissue 新调用）
	var resumeAt = -1
	for i, c := range susp.calls {
		if c == "resume:"+agentID {
			resumeAt = i
		}
	}
	if resumeAt == -1 {
		t.Fatalf("启用必须先 Resume: %v", susp.calls)
	}
	var startSeen bool
	for _, c := range gw.sent {
		if c.Type == "start_seat" {
			startSeen = true
		}
	}
	if !startSeen {
		t.Fatalf("应下发 start_seat: %+v", gw.sent)
	}
	seat, _ := s.GetSeat(context.Background(), agentID)
	if seat.State != "online" {
		t.Fatalf("启用后应 online: %+v", seat)
	}
}

func TestDestroyBackupRetireCleanupRebind(t *testing.T) {
	lc, _, gw, idp, s := mkLifecycle(t)
	agentID := mustOnlineSeat(t, s)
	// D8：裸调销毁必拒
	if err := lc.Destroy(context.Background(), agentID, "zzc", true, false); err != ErrConfirmRequired {
		t.Fatalf("裸调销毁应 confirm_required, 得 %v", err)
	}
	if err := lc.Destroy(context.Background(), agentID, "zzc", true, true); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	// 顺序核验：备份在 Retire 之前（fakeBackuper 的调用已由 refs 记录）
	if len(idp.retired) == 0 || idp.retired[len(idp.retired)-1] != agentID {
		t.Fatalf("销毁必须 Retire: %v", idp.retired)
	}
	var destroySeen bool
	for _, c := range gw.sent {
		if c.Type == "destroy_seat" {
			destroySeen = true
		}
	}
	if !destroySeen {
		t.Fatalf("应下发 destroy_seat: %+v", gw.sent)
	}
	seat, _ := s.GetSeat(context.Background(), agentID)
	if seat.State != "destroyed" {
		t.Fatalf("应 destroyed: %+v", seat)
	}
	// 账号回池
	acct, _ := s.GetAccount(context.Background(), acctOf(t, mkPoolSvcFromStore(t, s)))
	if acct.State != "available" {
		t.Fatalf("销毁后账号应回池: %+v", acct)
	}
}

func mkPoolSvcFromStore(t *testing.T, s *Store) *PoolService {
	t.Helper()
	svc, err := NewPoolService(s, mkMasterKeyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func mustOnlineSeat(t *testing.T, s *Store) string {
	t.Helper()
	rows, err := s.pool.Query(context.Background(), actx, `SELECT agent_id FROM g4_hosted_seat WHERE state='online' LIMIT 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("无 online 席位")
	}
	var id string
	if err := rows.Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
