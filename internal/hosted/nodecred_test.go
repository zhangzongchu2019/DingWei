package hosted

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNodeCredentialLifecycle(t *testing.T) {
	db := dsnpool(t)
	ctx := context.Background()
	s := NewStore(db)
	if err := s.DropSchema(ctx); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// 登记 → 认证通
	secret, err := s.RegisterNode(ctx, Node{ID: "nd_c1", Name: "node-c1",
		Labels: json.RawMessage(`{"site":"tc","spec":"8c16g","pool":"p1"}`),
		DockerVersion: "29.1.3", ProtocolVersion: 1}, "tester")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := s.AuthenticateNode(ctx, "nd_c1", secret); err != nil {
		t.Fatalf("authenticate 应通: %v", err)
	}
	// 错口令必拒（反向）
	if err := s.AuthenticateNode(ctx, "nd_c1", "wrong-secret"); err != ErrNodeAuth {
		t.Fatalf("错口令应 ErrNodeAuth, 得 %v", err)
	}
	// 不存在的节点必拒
	if err := s.AuthenticateNode(ctx, "nd_nope", secret); err != ErrNodeAuth {
		t.Fatalf("未知节点应 ErrNodeAuth, 得 %v", err)
	}

	// 轮换 → 旧凭据即拒、新凭据通（组长定；反向：不轮换旧凭据仍通 ⇒ 红）
	newSecret, err := s.RotateNodeCredential(ctx, "nd_c1", "tester")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if err := s.AuthenticateNode(ctx, "nd_c1", secret); err != ErrNodeAuth {
		t.Fatalf("轮换后旧凭据应即拒, 得 %v", err)
	}
	if err := s.AuthenticateNode(ctx, "nd_c1", newSecret); err != nil {
		t.Fatalf("轮换后新凭据应通: %v", err)
	}

	// 撤销 → 一切必拒（撤销优先于口令比对）
	if err := s.RevokeNodeCredential(ctx, "nd_c1", "tester"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := s.AuthenticateNode(ctx, "nd_c1", newSecret); err != ErrNodeRevoked {
		t.Fatalf("撤销后应 ErrNodeRevoked, 得 %v", err)
	}

	// 审计：register/rotate/revoke 各一条
	var n int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM g4_audit WHERE target='nd_c1'
AND action IN ('node.register','node.credential_rotate','node.credential_revoke')`).Scan(&n); err != nil {
		t.Fatalf("audit count: %v", err)
	}
	if n != 3 {
		t.Fatalf("审计应为 3 条, 得 %d", n)
	}
}
