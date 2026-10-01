package hosted

// T-G4-35 · console hosted 读接口三件套之两件：options 读模型＋jobs 进度（第三件 hosted
// 状态叠加见 overlay.go）。契约 v2 第一节字段逐字对齐；页面只认机器码与这些字段。
//
// 归属与鉴权在 handler（N3.Actor＋identity 归属判定），本层只做读模型装配。

import (
	"context"
	"encoding/json"
	"fmt"
)

// ---------- options 读模型（GET /api/console/hosted/options） ----------

type NodeOption struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Pool       string `json:"pool"`
	Online     bool   `json:"online"`
	FreeMemGB  int64  `json:"free_mem_gb"`
	FreeVCPU   int    `json:"free_vcpu"`
	FreeDiskGB int64  `json:"free_disk_gb"`
}

type ComboOption struct {
	CLI      string `json:"cli"`
	LLM      string `json:"llm"`
	CredKind string `json:"cred_kind"`
	Verified bool   `json:"verified"` // false＝未验通置灰（B12 逐家验通制）；不在表＝不兼容不可选
}

type AccountOption struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	LLM   string `json:"llm"`
	Kind  string `json:"kind"`  // api_key|token_file
	Last4 string `json:"last4"` // token_file 类为空串（页面改显示类别＋登记时间——见 HostedItem 同口径）
}

type PlanOption struct {
	ID    string `json:"id"`
	Name  string `json:"name"` // 如 2C/4G
	VCPU  int    `json:"vcpu"`
	MemGB int64  `json:"mem_gb"`
	Fits  bool   `json:"fits"` // false＝超剩余额度置灰（S3/B5；按全局剩余算，手选节点容量由创建时 node_no_capacity 兜底）
}

type QuotaRemaining struct {
	Agents int64 `json:"agents"`
	VCPU   int64 `json:"vcpu"`
	MemGB  int64 `json:"mem_gb"`
	DiskGB int64 `json:"disk_gb"`
}

// OptionsQuota 契约第一节的形状：quota 包一层 remaining（页面按 opts.quota.remaining 读——
// tosonnet50ts 复验🔴：扁平序列化让页面读出 undefined）。
type OptionsQuota struct {
	Remaining QuotaRemaining `json:"remaining"`
}

type OptionsView struct {
	Nodes    []NodeOption    `json:"nodes"`
	Combos   []ComboOption   `json:"combos"`
	Accounts []AccountOption `json:"accounts"`
	Plans    []PlanOption    `json:"plans"`
	Quota    OptionsQuota    `json:"quota"`
}

// Options 新建表单数据源（一次返回不拆；本期几十条级不分页——契约第一节）。
// 账号＝本人被授权且未绑定（ListUsableAccounts 口径；席位创建后该账号即绑定，下拉开不出）。
func (o *Orchestrator) Options(ctx context.Context, ownerLC string) (OptionsView, error) {
	// 空列表序列化成 [] 而不是 null（契约示例是数组；nil 切片会出 null——tosonnet50ts 复验小意见）
	v := OptionsView{
		Nodes: []NodeOption{}, Combos: []ComboOption{}, Accounts: []AccountOption{}, Plans: []PlanOption{},
	}

	// 节点：剩余内存/磁盘来自容量上报（心跳真采集）；free_vcpu＝cpu_total−在席套餐占用
	nodes, err := o.st.ListNodes(ctx)
	if err != nil {
		return v, err
	}
	for _, n := range nodes {
		var c struct {
			CPUTotal   int   `json:"cpu_total"`
			MemFreeMB  int64 `json:"mem_free_mb"`
			DiskFreeMB int64 `json:"disk_free_mb"`
		}
		_ = json.Unmarshal(n.Capacity, &c)
		usedVCPU, uerr := o.st.NodeUsedVCPU(ctx, n.ID)
		if uerr != nil {
			return v, uerr
		}
		var pool string
		var labels map[string]string
		if json.Unmarshal(n.Labels, &labels) == nil {
			pool = labels["pool"]
		}
		v.Nodes = append(v.Nodes, NodeOption{
			ID: n.ID, Name: n.Name, Pool: pool, Online: n.State == "online",
			FreeMemGB: c.MemFreeMB / 1024, FreeVCPU: max(0, c.CPUTotal-usedVCPU), FreeDiskGB: c.DiskFreeMB / 1024,
		})
	}

	// 组合验通表（唯一事实源；不在表＝不兼容——页面联动过滤遵守）
	for _, c := range o.combos.List() {
		v.Combos = append(v.Combos, ComboOption{CLI: c.CLI, LLM: c.LLM, CredKind: c.CredKind, Verified: c.Verified})
	}

	// 账号：本人可用范围（永不带 secret——B7）
	accts, err := o.st.ListUsableAccounts(ctx, ownerLC, "", "")
	if err != nil {
		return v, err
	}
	for _, a := range accts {
		v.Accounts = append(v.Accounts, AccountOption{ID: a.ID, Name: a.Name, LLM: a.LLM, Kind: a.Kind, Last4: a.Last4})
	}

	// 套餐 fits＋剩余额度（同一读数，免两次取）
	quota, usage, err := NewQuotaService(o.st, o.idp).quotaAndUsage(ctx, ownerLC)
	if err != nil {
		return v, err
	}
	v.Quota.Remaining = QuotaRemaining{
		Agents: max0(int64(quota.AgentsLimit) - int64(usage.Agents)),
		VCPU:   max0(int64(quota.VCPULimit) - int64(usage.VCPU)),
		MemGB:  max0(quota.MemMBLimit-usage.MemMB) / 1024,
		DiskGB: max0(quota.DiskMBLimit-usage.DiskMB) / 1024,
	}
	for _, pid := range PlanIDs() {
		p, _ := PlanByID(pid)
		fits := int64(quota.AgentsLimit)-int64(usage.Agents) >= 1 &&
			int64(quota.VCPULimit)-int64(usage.VCPU) >= int64(p.VCPU) &&
			quota.MemMBLimit-usage.MemMB >= p.MemMB &&
			quota.DiskMBLimit-usage.DiskMB >= p.DiskMB
		v.Plans = append(v.Plans, PlanOption{
			ID: p.ID, Name: fmt.Sprintf("%dC/%dG", p.VCPU, p.MemMB/1024),
			VCPU: p.VCPU, MemGB: p.MemMB / 1024, Fits: fits,
		})
	}
	return v, nil
}

// ---------- jobs 进度（GET /api/console/hosted/jobs/{job_id}） ----------

// JobView 进度蒙版读面（契约第一节）：stage＋失败机器码 reason；agent_id 仅 online 时给。
// 无 cancelled——排队不可取消、无取消端点。
type JobView struct {
	Stage   string `json:"stage"` // queued|pulling|starting|online|failed
	Reason  string `json:"reason,omitempty"`
	AgentID string `json:"agent_id,omitempty"`
}

// JobProgress 席位状态机对外读面（B1 进度数据源真实——写死阶段的反向由本函数与状态机同库保证）。
// 归属判定在 handler（jobID＝agent_id，非本人 ⇒ not_owner/404 由 handler 择一，本层不含归属信息）。
func (o *Orchestrator) JobProgress(ctx context.Context, jobID string) (JobView, error) {
	seat, err := o.st.GetSeat(ctx, jobID)
	if err != nil {
		return JobView{}, err
	}
	switch seat.State {
	case "queued", "pulling", "starting":
		return JobView{Stage: seat.State}, nil
	case "online":
		return JobView{Stage: "online", AgentID: seat.AgentID}, nil
	case "busy":
		return JobView{Stage: "online", AgentID: seat.AgentID}, nil // 升级/重启中：席位仍在线（蒙版用于创建链）
	default: // error/failed/destroyed 等 ⇒ failed
		// 🔴 reason 只许契约机器码（tosonnet50ts 复验小意见：StateDetail 若混入非机器码文本
		//    会漏给页面）——白名单外一律 start_failed 兜底
		return JobView{Stage: "failed", Reason: jobReasonMachineCode(seat.StateDetail)}, nil
	}
}

// jobReasonMachineCode 失败原因白名单（契约第一节七个机器码；非白名单一律 start_failed）。
func jobReasonMachineCode(detail string) string {
	switch detail {
	case "queue_timeout", "pull_timeout", "start_timeout",
		"image_pull_failed", "start_failed", "credential_inject_failed", "node_offline":
		return detail
	}
	return "start_failed"
}
