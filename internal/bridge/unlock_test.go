package bridge

// BR-12:mock unlock 登记制 fail-closed(契约 §2.3;冻结骨架 docs/bridge-contract-face-v0.1.md)。

import (
	"context"
	"testing"
)

func TestBR12_MockUnlockRegisteredGranted(t *testing.T) {
	u := NewMockUnlocker()
	u.Register("code-1", "zzc-owner-demo")
	oc := u.Unlock(context.Background(), "code-1")
	if !oc.Granted || oc.Reason == "" {
		t.Fatalf("已登记应 granted+带盖章描述:%+v", oc)
	}
}

func TestBR12_MockUnlockUnregisteredFailClosed(t *testing.T) {
	u := NewMockUnlocker()
	for _, code := range []string{"ghost", ""} {
		oc := u.Unlock(context.Background(), code)
		if oc.Granted || oc.Reason == "" {
			t.Fatalf("未登记/空 code 必须拒且带 reason(fail-closed):%q→%+v", code, oc)
		}
	}
	// 负例:登记后撤销→拒
	u.Register("code-2", "p")
	u.Revoke("code-2")
	if oc := u.Unlock(context.Background(), "code-2"); oc.Granted {
		t.Fatal("撤销后仍放行")
	}
}

func TestBR12_FeishuSkeletonAlwaysDenies(t *testing.T) {
	f := &FeishuUnlocker{}
	if oc := f.Unlock(context.Background(), "any"); oc.Granted {
		t.Fatal("生产件骨架未接线必须恒拒(防混入生产链静默放行)")
	}
	f2 := &FeishuUnlocker{Endpoint: "http://127.0.0.1:1"}
	if oc := f2.Unlock(context.Background(), "any"); oc.Granted {
		t.Fatal("设了 Endpoint 但无真实现仍必须拒")
	}
}
