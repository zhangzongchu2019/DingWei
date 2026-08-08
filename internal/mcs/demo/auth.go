// Package demo —— 带任务面板的演示版三模拟件+面板页(张宗楚 07-24"有更好"低优先件)。
//
// ⚠定位:演示/模拟级,不在 P3 评审范围;不碰已审核心(board/route/mcsd 零改动,纯调用方)。
// 生产化须按 criteria 过两审:真消息源/真鉴权=P5,管理面=P4(bd4baa2 拍板归属)。
//
// 本文件=模拟件②【鉴权盖章服务】接口定义+mock——即"把飞书拆成独立服务"的那个接口:
// 服务职责只有一件事:对每条进来的消息,回答"这是谁说的、是不是业主"(盖身份章)。
// 系统只认章、不认消息正文自称(C5 5.0 principal 非 from 的根)。
// 生产实现=飞书/hub 会话绑定(P5);演示实现=MockAuth 显式登记。
package demo

import (
	"context"
	"fmt"
	"sync"
)

// Stamp 身份章:经鉴权的主体+是否业主。
type Stamp struct {
	Principal string // 合法 zzc-* 身份(路由 gate-1 校格式)
	IsOwner   bool   // 业主(C6 应急原语授权面)
}

// Authenticator 鉴权盖章服务接口(★拆分点:生产=飞书→hub 绑定服务;演示=MockAuth)。
// 未知会话必须返回错误(fail-closed:盖不了章的消息不得进收件面)。
type Authenticator interface {
	StampSession(ctx context.Context, sessionRef string) (Stamp, error)
}

// MockAuth 演示鉴权:显式登记 会话引用→身份章;未登记=拒(不默认放行)。
type MockAuth struct {
	mu       sync.Mutex
	sessions map[string]Stamp
}

func NewMockAuth() *MockAuth { return &MockAuth{sessions: map[string]Stamp{}} }

// Bind 登记一个会话的身份章(owner=true 即标记为业主)。
func (m *MockAuth) Bind(sessionRef, principal string, owner bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sessionRef] = Stamp{Principal: principal, IsOwner: owner}
}

// StampSession 实现 Authenticator。未登记会话→错误(fail-closed)。
func (m *MockAuth) StampSession(_ context.Context, sessionRef string) (Stamp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionRef]
	if !ok {
		return Stamp{}, fmt.Errorf("demo/auth: 会话 %q 未登记,拒绝盖章(fail-closed)", sessionRef)
	}
	return s, nil
}

// Owners 当前业主 principal 集(喂 board.SetOwnerPrincipals / route.EmergencyIDs)。
func (m *MockAuth) Owners() map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]bool{}
	for _, s := range m.sessions {
		if s.IsOwner {
			out[s.Principal] = true
		}
	}
	return out
}
