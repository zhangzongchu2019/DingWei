package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

var stockTemplateCases = []struct {
	ID, Title, Acceptance string
	Writes                int
}{
	{"B01-ownership", "可信绑定", "有当前可信绑定才继续，读权不代写权；身份不符拒绝", 0},
	{"B02-native-one", "原生一对一", "协议及目标完整集合精确匹配，不新建SKU", 0},
	{"B03-composite-one", "复合选一", "保留复合协议和原key；未选项明示舍弃，不造FormatID", 0},
	{"B04-missing-page", "分页缺页", "按正式分页合同证明未完整则拒绝；判据未有则保未验证", 0},
	{"B04-repeated-cursor", "游标重复", "游标重复或停滞不当作终止，按正式合同拒绝或保未验证", 0},
	{"B04-concurrent-change", "分页并发变化", "须有正式快照或检测保证，不把跨版本页面拼成稳定全集", 0},
	{"B04-sparse", "稀疏生命周期", "仅正式生命周期或稀疏语义可解释，不凭笛卡尔积补SKU", 0},
	{"B04-extra-sku", "目标多项", "目标完整集合与原选择集合精确比对，不在更大集合中随意挑选", 0},
	{"B04-foreign-mapping", "陌生映射", "拒绝scope/source/key/目标身份不符的原mapping，不自动升级身份", 0},
	{"B05-explicit-zero", "显式零", "只读准备保留显式0，不把0当缺失；本例不写", 0},
	{"B05-unknown", "数量未知", "缺失/null/非法或源无SKU时拒绝或保未知，不补0或占位SKU", 0},
	{"B06-applied-lost", "已应用丢响应", "单独写/故障授权后仅一次提交；同值读回不证明原事件，不close", 1},
	{"B06-cold-readonly", "冷启动只读", "已有FAILED/pending只核原任务状态和诊断，不补delta、不newkey、不清pending", 0},
}

// StockTemplateSample is a candidate contract, never an execution result or a
// selection of the first production scope. All roles/method/environment remain
// missing until a human supplies them and the actual principals accept.
func StockTemplateSample() TemplateDocument {
	d := TemplateDocument{TemplateVersion: PilotTemplateVersion, Kind: "child", Title: "STOCK所选范围样板（待完善）", Goal: "仅保留源草稿13例，不扩为完整STOCK或all14", Scope: "diaryToAlbum/existingMappedInventory", TeamMode: "single", RequestedMode: "light", RiskAssessment: "unassessed", Sample: "stock13"}
	for _, c := range stockTemplateCases {
		d.Entries = append(d.Entries, TemplateEntry{ID: c.ID, Kind: "case", OwnerRef: c.ID, Title: c.Title, BusinessWriteLimit: c.Writes, Fields: TemplateFields{Acceptance: c.Acceptance}})
	}
	return d
}
func templateValidate(d TemplateDocument, source TemplateSource, now time.Time) []TemplateMissing {
	missing := []TemplateMissing{}
	add := func(p, r string) { missing = append(missing, TemplateMissing{p, r}) }
	if d.TemplateVersion != PilotTemplateVersion {
		add("template_version", "unsupported_immutable_template")
	}
	if d.Kind != "parent" && d.Kind != "child" {
		add("kind", "parent_or_selected_child_required")
	}
	if templateUnknown(d.Title) || len(d.Title) > 512 {
		add("title", "required")
	}
	if templateUnknown(d.Goal) || len(d.Goal) > 4096 {
		add("goal", "required")
	}
	if templateUnknown(d.Scope) || len(d.Scope) > 2048 {
		add("scope", "explicit_selected_scope_required")
	}
	if d.TeamMode != "single" && d.TeamMode != "multi" {
		add("team_mode", "single_or_multi_required")
	}
	if d.RequestedMode != "light" && d.RequestedMode != "full" {
		add("requested_mode", "invalid_mode")
	}
	if d.RiskAssessment != "unassessed" && d.RiskAssessment != "declared" {
		add("risk_assessment", "invalid_assessment")
	}
	if source.ID == "" || source.State != "ready" || source.SHA == "" || source.Content == "" || source.ID != d.SourceID {
		add("source_id", "verified_source_snapshot_required")
	}
	if d.Kind == "child" && (d.Parent == nil || d.Parent.SpecID == "" || d.Parent.Revision < 1 || !fullSHA(d.Parent.SHA)) {
		add("parent", "immutable_parent_revision_sha_required")
	}
	if d.Kind == "parent" && d.Parent != nil {
		add("parent", "parent_cannot_have_a_parent")
	}
	if len(d.Entries) == 0 || len(d.Entries) > 100 {
		add("entries", "one_to_100_explicit_requirements_or_cases")
	}
	seen := map[string]bool{}
	for i, e := range d.Entries {
		p := fmt.Sprintf("entries[%d]", i)
		f := templateEffectiveFields(e.Fields, d.Defaults)
		if !templateID.MatchString(e.ID) || seen[e.ID] {
			add(p+".id", "stable_unique_id_required")
		}
		seen[e.ID] = true
		if e.Kind != "requirement" && e.Kind != "case" {
			add(p+".kind", "requirement_or_case_required")
		}
		if templateUnknown(e.Title) || len(e.Title) > 512 {
			add(p+".title", "required")
		}
		if templateUnknown(e.OwnerRef) || len(e.OwnerRef) > 4096 || !strings.Contains(source.Content, e.OwnerRef) {
			add(p+".owner_ref", "literal_source_quote_required")
		}
		for _, field := range []struct{ k, v string }{{"acceptance", f.Acceptance}, {"test_method", f.Method}, {"deliverable", f.Deliverable}, {"environment", f.Environment}, {"executor", f.Executor}, {"signoff", f.Signoff}} {
			if templateUnknown(field.v) || len(field.v) > 8192 {
				add(p+"."+field.k, "explicit_value_required_unknown_is_not_complete")
			}
		}
		if !templatePrincipal(f.Executor) {
			add(p+".executor", "explicit_platform_principal_required")
		}
		if !strings.HasPrefix(f.Signoff, "user:") || !templatePrincipal(f.Signoff) {
			add(p+".signoff", "human_business_signoff_responsibility_required")
		}
		// Even a single-person team needs a genuinely separate reviewing Agent;
		// same owner/different Agent remains valid. No machine business signoff.
		if !templatePrincipal(f.Reviewer) || d.TeamMode == "single" && !strings.HasPrefix(f.Reviewer, "agent:") {
			add(p+".reviewer", "independent_review_agent_or_multiplayer_reviewer_required")
		}
		if f.Executor == f.Reviewer {
			add(p+".reviewer", "executor_cannot_be_own_reviewer")
		}
		if e.BusinessWriteLimit < 0 || e.BusinessWriteLimit > 100000 {
			add(p+".business_write_limit", "nonnegative_budget_is_not_authorization")
		}
	}
	if d.Sample != "" && d.Sample != "stock13" {
		add("sample", "unsupported_sample")
	}
	if d.Sample == "stock13" {
		if len(d.Entries) != len(stockTemplateCases) {
			add("entries", "stock13_original_case_set_required")
		}
		for _, want := range stockTemplateCases {
			found := false
			for _, e := range d.Entries {
				if e.ID == want.ID {
					found = true
					if e.Kind != "case" || e.BusinessWriteLimit != want.Writes {
						add("entries."+e.ID, "stock13_original_kind_and_budget_required")
					}
				}
			}
			if !found {
				add("entries."+want.ID, "stock13_original_id_required")
			}
		}
	}
	if d.TargetSpecID != "" && d.PredecessorSpecID != "" {
		add("target_spec_id", "choose_editable_target_or_final_predecessor")
	}
	if (d.TargetSpecID != "" || d.PredecessorSpecID != "") && (templateUnknown(d.ChangeReason) || d.ExpectedRevision < 1) {
		add("change_reason", "explicit_change_reason_and_revision_required")
	}
	if d.RequestedMode == "full" {
		missing = append(missing, templateValidateFull(d, now)...)
	}
	sort.Slice(missing, func(i, j int) bool {
		if missing[i].Path != missing[j].Path {
			return missing[i].Path < missing[j].Path
		}
		return missing[i].Reason < missing[j].Reason
	})
	return missing
}
func fullSHA(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, r := range v {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func templateValidateFull(d TemplateDocument, now time.Time) []TemplateMissing {
	result := []TemplateMissing{}
	add := func(k, r string) { result = append(result, TemplateMissing{"full." + k, r}) }
	allowed := map[string][]string{"外部契约变更": {"无", "兼容新增", "破坏性"}, "数据模型变更": {"无", "加列加表", "改列删列改语义"}, "影响范围": {"单模块", "跨模块", "跨服务"}, "在产影响": {"不影响", "灰度可回滚", "直接影响在产"}, "回滚成本": {"重启即回滚", "需回滚数据", "不可回滚"}, "安全面": {"否", "是"}}
	for k, values := range allowed {
		ok := false
		for _, v := range values {
			if d.Full.Attributes[k] == v {
				ok = true
			}
		}
		if !ok {
			add("attributes."+k, "full_enum_required")
		}
	}
	if n, e := strconv.Atoi(d.Full.Attributes["预估节点数"]); e != nil || n < 1 || n > 100000 {
		add("attributes.预估节点数", "positive_estimate_required")
	}
	if templateUnknown(d.Full.RuleVersion) || len(d.Full.RuleIDs) == 0 {
		add("rule_ids", "external_rule_version_and_ids_required")
	}
	for _, id := range d.Full.RuleIDs {
		if !templateID.MatchString(id) {
			add("rule_ids", "invalid_rule_id")
		}
	}
	for _, face := range []string{"system_integration", "runtime_constraints", "estimates_with_basis_and_recheck", "environment", "design_constraints", "input_sources"} {
		if templateUnknown(d.Full.ContentFaces[face]) {
			add("content_faces."+face, "full_content_face_required")
		}
	}
	for _, ref := range []struct{ k, v string }{{"review_reference", d.Full.ReviewReference}, {"analyze_reference", d.Full.AnalyzeReference}, {"deliverable_contract_reference", d.Full.DeliverableContractReference}, {"probes_reference", d.Full.ProbesReference}} {
		if templateUnknown(ref.v) || len(ref.v) > 2048 {
			add(ref.k, "external_full_gate_evidence_required")
		}
	}
	if !strings.HasPrefix(d.Full.ReviewPrincipal, "user:") || !templatePrincipal(d.Full.ReviewPrincipal) {
		add("review_principal", "explicit_human_full_review_authority_required")
	}
	if d.Production && d.Full.AuthorizationNone {
		add("authorization_none", "production_actions_require_separate_explicit_authorization")
	}
	if !d.Full.AuthorizationNone {
		for _, field := range []struct{ k, v string }{{"authorization_reference", d.Full.AuthorizationReference}, {"authorization_scope", d.Full.AuthorizationScope}, {"authorization_purpose", d.Full.AuthorizationPurpose}} {
			invalid := templateUnknown(field.v)
			for _, word := range []string{"可能", "视情况", "相关", "适当", "如有必要"} {
				invalid = invalid || strings.Contains(field.v, word)
			}
			if invalid {
				add(field.k, "specific_authorization_scope_purpose_and_reference_required")
			}
		}
		expiry, err := time.Parse(time.RFC3339, d.Full.AuthorizationExpiresAt)
		if err != nil || !expiry.After(now) {
			add("authorization_expires_at", "unexpired_explicit_deadline_required")
		}
	}
	return result
}
func templateRenderedContent(d TemplateDocument, source TemplateSource) (string, string) {
	var body strings.Builder
	fmt.Fprintf(&body, "# %s\n\n目标：%s\n\n所选范围：%s\n\n模板：%s；档位：%s；风险评估：%s。\n\n来源：%s；来源方式：%s；版本：%s；SHA256：%s。\n", d.Title, d.Goal, d.Scope, d.TemplateVersion, d.RequestedMode, d.RiskAssessment, source.URL, source.Kind, source.Revision, source.SHA)
	if d.Parent != nil {
		fmt.Fprintf(&body, "\n父 spec：%s 修订%d SHA256 %s。\n", d.Parent.SpecID, d.Parent.Revision, d.Parent.SHA)
	}
	for _, e := range d.Entries {
		f := e.Fields
		fmt.Fprintf(&body, "\n## %s %s\n\n来源逐字引用：%s\n\n验收标准：%s\n\n测试方法：%s\n\n交付物合同：%s\n\n环境：%s\n\n执行责任：%s；复核责任：%s；业务签收责任：%s。\n\n业务写上限：%d（不是动作授权）；初始结果：NOT_RUN，executed=false，业务签收=false。\n", e.ID, e.Title, e.OwnerRef, f.Acceptance, f.Method, f.Deliverable, f.Environment, f.Executor, f.Reviewer, f.Signoff, e.BusinessWriteLimit)
	}
	raw, _ := json.MarshalIndent(d, "", "  ")
	fmt.Fprintf(&body, "\n## 不可变模板合同\n\n```json\n%s\n```\n\nAgent只提供建议和技术证据；以上责任接受不是实际执行、测试通过或人类业务签收。完整档外部门结果按各自证据与签字确认，不由模板自动伪造。\n", raw)
	sum := sha256.Sum256([]byte(body.String()))
	return body.String(), hex.EncodeToString(sum[:])
}
