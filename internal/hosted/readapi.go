package hosted

// T-G4-35 · console hosted 读接口三件套之两件：options 读模型＋jobs 进度（第三件 hosted
// 状态叠加见 overlay.go）。契约 v2 第一节字段逐字对齐；页面只认机器码与这些字段。
//
// 归属与鉴权在 handler（N3.Actor＋identity 归属判定），本层只做读模型装配。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// ---------- options 读模型（GET /api/console/hosted/options） ----------

type NodeOption struct {
	QuotaMode     string `json:"quota_mode,omitempty"`
	QuotaNotice   string `json:"quota_notice,omitempty"`
	QuotaEligible *bool  `json:"quota_eligible,omitempty"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Pool          string `json:"pool"`
	Online        bool   `json:"online"`
	FreeMemGB     int64  `json:"free_mem_gb"`
	FreeVCPU      int    `json:"free_vcpu"`
	FreeDiskGB    int64  `json:"free_disk_gb"`
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
	Kind  string `json:"kind"`   // api_key|token_file
	Last4 string `json:"last4"`  // token_file 类为空串（页面改显示类别＋登记时间——见 HostedItem 同口径）
	InUse int    `json:"in_use"` // 0.2.1.101：本号已被本人多少个未销毁 Agent 引用（>0 时页面提示「（已被 N 个 Agent 使用）」，不拦）
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

// QuotaView 契约第一节：quota: {remaining:{agents, vcpu, mem_gb, disk_gb}}——多包一层 remaining（页面读 opts.quota.remaining）。
type QuotaView struct {
	Remaining QuotaRemaining `json:"remaining"`
}

type OptionsView struct {
	Nodes    []NodeOption    `json:"nodes"`
	Combos   []ComboOption   `json:"combos"`
	Accounts []AccountOption `json:"accounts"`
	Plans    []PlanOption    `json:"plans"`
	Quota    QuotaView       `json:"quota"`
	Models   []string        `json:"models"` // 包 B2：在线节点上报清单的并集（节点下线即消失）
}

// Options 新建表单数据源（一次返回不拆；本期几十条级不分页——契约第一节）。
// 账号＝属主本人且未停用（ListUsableAccounts 口径；0.2.1.101 起本人多席共用、不再因已绑而消失）。
func (o *Orchestrator) Options(ctx context.Context, ownerLC string) (OptionsView, error) {
	// 四个列表一律非 nil：空时序列化为 []，不是 null（页面按数组遍历）。
	v := OptionsView{Nodes: []NodeOption{}, Combos: []ComboOption{}, Accounts: []AccountOption{}, Plans: []PlanOption{}, Models: []string{}}

	// 节点：剩余内存/磁盘来自容量上报（心跳真采集）；free_vcpu＝cpu_total−在席套餐占用
	nodes, err := o.st.ListNodes(ctx)
	if err != nil {
		return v, err
	}
	modelsSeen := map[string]bool{}
	for _, n := range nodes {
		var c struct {
			CPUTotal   int      `json:"cpu_total"`
			MemFreeMB  int64    `json:"mem_free_mb"`
			DiskFreeMB int64    `json:"disk_free_mb"`
			Models     []string `json:"models"`
		}
		_ = json.Unmarshal(n.Capacity, &c)
		if n.State == "online" { // 修订 4：下线节点的清单不进并集
			for _, m := range c.Models {
				if !modelsSeen[m] {
					modelsSeen[m] = true
					v.Models = append(v.Models, m)
				}
			}
		}
		usedVCPU, uerr := o.st.NodeUsedVCPU(ctx, n.ID)
		if uerr != nil {
			return v, uerr
		}
		var pool string
		var labels map[string]string
		if json.Unmarshal(n.Labels, &labels) == nil {
			pool = labels["pool"]
		}
		mode, _ := registeredQuota(n)
		notice := ""
		if mode == "soft" {
			notice = "无硬磁盘配额"
		}
		eligible := nodeQuotaEligible(n)
		var publicEligible *bool
		publicMode := ""
		if mode != "hard" || !eligible {
			publicEligible = &eligible
			publicMode = mode
		}
		v.Nodes = append(v.Nodes, NodeOption{
			QuotaMode: publicMode, QuotaNotice: notice, QuotaEligible: publicEligible,
			ID: n.ID, Name: n.Name, Pool: pool, Online: n.State == "online",
			FreeMemGB: c.MemFreeMB / 1024, FreeVCPU: max(0, c.CPUTotal-usedVCPU), FreeDiskGB: c.DiskFreeMB / 1024,
		})
	}

	// 组合验通表（唯一事实源；不在表＝不兼容——页面联动过滤遵守）
	for _, c := range o.combos.List() {
		v.Combos = append(v.Combos, ComboOption{CLI: c.CLI, LLM: c.LLM, CredKind: c.CredKind, Verified: c.Verified})
	}

	// 账号：属主本人（永不带 secret——B7）
	accts, err := o.st.ListUsableAccounts(ctx, ownerLC, "", "")
	if err != nil {
		return v, err
	}
	for _, a := range accts {
		v.Accounts = append(v.Accounts, AccountOption{ID: a.ID, Name: a.Name, LLM: a.LLM, Kind: a.Kind, Last4: a.Last4, InUse: a.InUse})
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

// JobView 进度蒙版读面（契约第一节）：stage＋失败机器码 reason；创建完成为 online，
// 生命周期完成为 completed；agent_id 仅完成时给。
// 无 cancelled——排队不可取消、无取消端点。
type JobView struct {
	Stage   string `json:"stage"` // queued|pulling|starting|running|online|completed|failed
	Reason  string `json:"reason,omitempty"`
	AgentID string `json:"agent_id,omitempty"`
	Model   string `json:"model,omitempty"` // 包 B2 修订 3：席位生效模型（空＝节点默认，键缺席——旧客户端与既有精确体测试不受影响）
}

// JobProgressFor jobs 端点的读面，归属按席位 created_by 判（组长裁定 lead-g4-joint-run-1 第 2 条）：
// 创建失败的任务已 Retire 了 Agent，身份侧「名下 Agent」里再也找不到它，按身份归属判会把业主自己的失败任务
// 答成 not_owner、蒙版永远等不到 failed。席位行不随 Retire 删除，created_by 就是建席的业主。
// 不存在与非本人同答 ErrNotOwner：不暴露别人的 job 是否存在。
func (o *Orchestrator) JobProgressFor(ctx context.Context, jobID, actorLC string) (JobView, error) {
	seat, err := o.st.GetSeat(ctx, jobID)
	if errors.Is(err, ErrNotFound) {
		return JobView{}, ErrNotOwner
	}
	if err != nil {
		return JobView{}, err
	}
	if actorLC == "" || seat.CreatedBy != actorLC {
		return JobView{}, ErrNotOwner
	}
	return jobViewOf(seat), nil
}

// JobProgress 席位状态机对外读面（B1 进度数据源真实——写死阶段的反向由本函数与状态机同库保证）。
// 归属判定在 handler（jobID＝agent_id，非本人 ⇒ not_owner/404 由 handler 择一，本层不含归属信息）。
func (o *Orchestrator) JobProgress(ctx context.Context, jobID string) (JobView, error) {
	seat, err := o.st.GetSeat(ctx, jobID)
	if err != nil {
		return JobView{}, err
	}
	return jobViewOf(seat), nil
}

func jobViewOf(seat Seat) JobView {
	withModel := func(v JobView) JobView { v.Model = seat.Model; return v }
	switch seat.State {
	case "queued", "pulling", "starting":
		return withModel(JobView{Stage: seat.State})
	case "online":
		return withModel(JobView{Stage: "online", AgentID: seat.AgentID})
	case "busy":
		return withModel(JobView{Stage: "online", AgentID: seat.AgentID}) // 升级/重启中：席位仍在线（蒙版用于创建链）
	default: // error/failed/destroyed 等 ⇒ failed，reason＝状态机记录的机器码
		return withModel(JobView{Stage: "failed", Reason: jobFailReason(seat.StateDetail)})
	}
}

// jobFailReasons 契约第四节「进度」一栏的七个机器码——jobs.reason 只出这七个（页面按码配中文）。
var jobFailReasons = map[string]bool{
	"queue_timeout": true, "pull_timeout": true, "start_timeout": true,
	"image_pull_failed": true, "start_failed": true, "credential_inject_failed": true, "node_offline": true,
}

// jobFailReason 状态机记下的失败原因若不是这七个码（空、Go 错误文本、生命周期操作留下的其他记号），
// 一律收成 start_failed：页面只认码，原文留在席位事件与审计里供诊断，不外漏。
func jobFailReason(detail string) string {
	if jobFailReasons[detail] {
		return detail
	}
	return "start_failed"
}
