// unlock.go —— C-P0-4:Unlocker 两实现(契约 §2.3;BR-12)。
//   - MockUnlocker:登记制 fail-closed(demo MockAuth 同型)——测试/E门夹具用,不连真飞书;
//   - FeishuUnlocker:生产件骨架——真接线属 P5 授权域(业主授权面),本批只冻形状,
//     未接线前恒拒(fail-closed),防"骨架被误当真件"静默放行。
package bridge

import (
	"context"
	"sync"
)

// MockUnlocker 登记制解锁 mock:显式 Register 过的 code 才 granted,未登记一律拒。
type MockUnlocker struct {
	mu    sync.Mutex
	codes map[string]string // code → principal(盖章身份,供审计描述)
}

func NewMockUnlocker() *MockUnlocker {
	return &MockUnlocker{codes: map[string]string{}}
}

// Register 登记一个可解锁 code(测试脚本/夹具前置步骤)。
func (m *MockUnlocker) Register(code, principal string) {
	m.mu.Lock()
	m.codes[code] = principal
	m.mu.Unlock()
}

// Revoke 注销 code(负例:已批准后撤销)。
func (m *MockUnlocker) Revoke(code string) {
	m.mu.Lock()
	delete(m.codes, code)
	m.mu.Unlock()
}

func (m *MockUnlocker) Unlock(_ context.Context, code string) UnlockOutcome {
	m.mu.Lock()
	principal, ok := m.codes[code]
	m.mu.Unlock()
	if code == "" {
		return UnlockOutcome{Granted: false, Reason: "空 code,拒(fail-closed)"}
	}
	if !ok {
		return UnlockOutcome{Granted: false, Reason: "code 未登记,拒(fail-closed)"}
	}
	return UnlockOutcome{Granted: true, Reason: "mock 盖章: " + principal}
}

// FeishuUnlocker 生产件骨架:飞书盖章服务独立化的接口占位。
// 真实现(独立盖章服务+业主授权原子共切)=P5 域,不在 C-P0 批;
// 未接线前 Unlock 恒拒——绝不 granted,防骨架混入生产链静默放行。
type FeishuUnlocker struct {
	// Endpoint 独立盖章服务地址(P5 接线时注入;空=未接线)。
	Endpoint string
}

func (f *FeishuUnlocker) Unlock(context.Context, string) UnlockOutcome {
	return UnlockOutcome{Granted: false,
		Reason: "feishu unlock 未接线(P5 授权域,本件为 C-P0-4 骨架,恒拒 fail-closed)"}
}

// 编译期断言:两实现满足契约面。
var (
	_ Unlocker = (*MockUnlocker)(nil)
	_ Unlocker = (*FeishuUnlocker)(nil)
)
