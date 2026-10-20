package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const PilotTemplateVersion = "pilot-spec-light/1"

type TemplateFields struct {
	Acceptance  string `json:"acceptance"`
	Method      string `json:"test_method"`
	Deliverable string `json:"deliverable"`
	Environment string `json:"environment"`
	Executor    string `json:"executor"`
	Reviewer    string `json:"reviewer,omitempty"`
	Signoff     string `json:"signoff"`
}
type TemplateEntry struct {
	ID                 string         `json:"id"`
	Kind               string         `json:"kind"` // requirement or case; both expand the same six fields.
	RequirementID      string         `json:"requirement_id,omitempty"`
	OwnerRef           string         `json:"owner_ref"`
	Title              string         `json:"title"`
	Fields             TemplateFields `json:"fields"`
	BusinessWriteLimit int            `json:"business_write_limit"`
}
type TemplateParent struct {
	SpecID   string `json:"spec_id"`
	Revision int    `json:"revision"`
	SHA      string `json:"content_sha256"`
}
type TemplateFull struct {
	AuthorizationNone            bool              `json:"authorization_none"`
	AuthorizationPurpose         string            `json:"authorization_purpose"`
	AnalyzeReference             string            `json:"analyze_reference"`
	DeliverableContractReference string            `json:"deliverable_contract_reference"`
	ProbesReference              string            `json:"probes_reference"`
	Attributes                   map[string]string `json:"attributes"`
	RuleVersion                  string            `json:"rule_version"`
	RuleIDs                      []string          `json:"rule_ids"`
	ContentFaces                 map[string]string `json:"content_faces"`
	AuthorizationReference       string            `json:"authorization_reference"`
	AuthorizationScope           string            `json:"authorization_scope"`
	AuthorizationExpiresAt       string            `json:"authorization_expires_at"`
	ReviewPrincipal              string            `json:"review_principal"`
	ReviewReference              string            `json:"review_reference"`
}
type TemplateDocument struct {
	RiskAssessment         string          `json:"risk_assessment"`
	TemplateVersion        string          `json:"template_version"`
	Kind                   string          `json:"kind"`
	Title                  string          `json:"title"`
	Goal                   string          `json:"goal"`
	Scope                  string          `json:"scope"`
	TeamMode               string          `json:"team_mode"`
	SourceID               string          `json:"source_id"`
	Parent                 *TemplateParent `json:"parent,omitempty"`
	TargetSpecID           string          `json:"target_spec_id,omitempty"`
	ExpectedRevision       int             `json:"expected_revision,omitempty"`
	PredecessorSpecID      string          `json:"predecessor_spec_id,omitempty"`
	ChangeReason           string          `json:"change_reason,omitempty"`
	RequestedMode          string          `json:"requested_mode"`
	Production             bool            `json:"production"`
	Security               bool            `json:"security"`
	DowngradeReason        string          `json:"downgrade_reason,omitempty"`
	RiskClearanceReference string          `json:"risk_clearance_reference,omitempty"`
	Sample                 string          `json:"sample,omitempty"`
	Defaults               TemplateFields  `json:"defaults"`
	Entries                []TemplateEntry `json:"entries"`
	Full                   TemplateFull    `json:"full"`
}
type TemplateSource struct {
	ID           string `json:"id"`
	Owner        string `json:"-"`
	URL          string `json:"source_url"`
	DocumentKind string `json:"document_kind"`
	DocumentID   string `json:"document_id"`
	Revision     string `json:"source_revision"`
	Kind         string `json:"source_kind"`
	State        string `json:"state"`
	Content      string `json:"content_markdown,omitempty"`
	SHA          string `json:"content_sha256"`
	CreatedAt    string `json:"created_at"`
}
type TemplateMissing struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}
type TemplateRole struct {
	EntryID    string `json:"entry_id"`
	Role       string `json:"role"`
	Principal  string `json:"principal"`
	Accepted   bool   `json:"accepted"`
	AcceptedAt string `json:"accepted_at,omitempty"`
}
type TemplateImpact struct {
	Predecessor string   `json:"predecessor_spec_id"`
	Revision    int      `json:"predecessor_revision"`
	Successor   string   `json:"successor_spec_id"`
	TaskCount   int      `json:"task_count"`
	TaskIDs     []string `json:"task_ids"`
	Limit       int      `json:"limit"`
}
type TemplateView struct {
	Summary             bool              `json:"summary"`
	MissingCount        int               `json:"missing_count,omitempty"`
	Impact              *TemplateImpact   `json:"impact,omitempty"`
	ID                  string            `json:"draft_id"`
	Owner               string            `json:"owner_principal"`
	Version             int               `json:"version"`
	State               string            `json:"state"`
	Document            TemplateDocument  `json:"document"`
	PlanSHA             string            `json:"plan_sha256"`
	ContentSHA          string            `json:"content_sha256"`
	ContentMarkdown     string            `json:"content_markdown,omitempty"`
	Mode                string            `json:"mode"`
	Source              TemplateSource    `json:"source"`
	Missing             []TemplateMissing `json:"missing"`
	Roles               []TemplateRole    `json:"roles"`
	CanConfirm          bool              `json:"can_confirm"`
	SpecID              string            `json:"spec_id,omitempty"`
	SpecRevision        int               `json:"spec_revision,omitempty"`
	ExecutionAuthorized bool              `json:"execution_authorized"`
	BusinessSignedOff   bool              `json:"business_signed_off"`
}
type TemplateOperation struct {
	RequestID       string            `json:"request_id"`
	ExpectedVersion int               `json:"expected_version"`
	Operation       string            `json:"operation"`
	Document        *TemplateDocument `json:"document,omitempty"`
	PlanSHA         string            `json:"plan_sha256,omitempty"`
	EntryIDs        []string          `json:"entry_ids,omitempty"`
	Role            string            `json:"role,omitempty"`
	Decision        string            `json:"decision,omitempty"`
	Reason          string            `json:"reason,omitempty"`
	ConfirmHuman    bool              `json:"confirm_human"`
}
type TemplateReferences struct {
	ID, Owner string
	Version   int
	State     string
	Document  TemplateDocument
}
type TemplatePrincipalSnapshot struct {
	Active      map[string]bool
	AgentOwners map[string]string
}

var templateID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
var templateDocPath = regexp.MustCompile(`^/(docx|docs|wiki)/([A-Za-z0-9_-]{1,128})/?$`)

func ParseTemplateSourceURL(raw string) (string, string, string, error) {
	if len(raw) > 2048 {
		return "", "", "", ErrTaskFlowInvalid
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" {
		return "", "", "", ErrTaskFlowInvalid
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || !(strings.HasSuffix(host, ".feishu.cn") || strings.HasSuffix(host, ".larksuite.com")) {
		return "", "", "", ErrTaskFlowInvalid
	}
	// Accept DNS labels only; the connector receives document identity, not an
	// arbitrary URL. No DNS resolution, redirect or user-cookie forwarding here.
	for _, label := range strings.Split(host, ".") {
		if !regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`).MatchString(label) {
			return "", "", "", ErrTaskFlowInvalid
		}
	}
	match := templateDocPath.FindStringSubmatch(u.EscapedPath())
	if match == nil {
		return "", "", "", ErrTaskFlowInvalid
	}
	u.Host = host
	u.Path = "/" + match[1] + "/" + match[2]
	u.RawPath = ""
	return u.String(), match[1], match[2], nil
}
func templateUnknown(v string) bool {
	s := strings.TrimSpace(strings.ToLower(v))
	if s == "" {
		return true
	}
	for _, bad := range []string{"unknown", "tbd", "todo", "null", "none", "n/a", "未定", "待定", "未认领", "待认领", "未配置", "待配置", "未知", "待补", "待确认"} {
		if s == bad || strings.HasPrefix(s, bad+":") || strings.HasPrefix(s, bad+"：") || strings.HasPrefix(s, bad+" ") {
			return true
		}
	}
	return false
}
func templateEffectiveFields(v, d TemplateFields) TemplateFields {
	if strings.TrimSpace(v.Acceptance) == "" {
		v.Acceptance = d.Acceptance
	}
	if strings.TrimSpace(v.Method) == "" {
		v.Method = d.Method
	}
	if strings.TrimSpace(v.Deliverable) == "" {
		v.Deliverable = d.Deliverable
	}
	if strings.TrimSpace(v.Environment) == "" {
		v.Environment = d.Environment
	}
	if strings.TrimSpace(v.Executor) == "" {
		v.Executor = d.Executor
	}
	if strings.TrimSpace(v.Reviewer) == "" {
		v.Reviewer = d.Reviewer
	}
	if strings.TrimSpace(v.Signoff) == "" {
		v.Signoff = d.Signoff
	}
	return v
}
func templateDigest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func templatePrincipal(p string) bool {
	return strings.HasPrefix(p, "user:") && validBlockerPrincipal(p) || strings.HasPrefix(p, "agent:") && validBlockerPrincipal(p)
}
func templateRoles(d TemplateDocument) []TemplateRole {
	roles := []TemplateRole{}
	for _, entry := range d.Entries {
		f := templateEffectiveFields(entry.Fields, d.Defaults)
		for _, r := range []struct{ k, p string }{{"executor", f.Executor}, {"reviewer", f.Reviewer}, {"signoff", f.Signoff}} {
			roles = append(roles, TemplateRole{EntryID: entry.ID, Role: r.k, Principal: r.p})
		}
	}
	if d.RequestedMode == "full" || d.Production || d.Security {
		roles = append(roles, TemplateRole{EntryID: "__full__", Role: "full_review", Principal: d.Full.ReviewPrincipal})
	}
	return roles
}
func templateExpand(d TemplateDocument) TemplateDocument {
	if d.RiskAssessment == "" {
		d.RiskAssessment = "unassessed"
	}
	if d.Full.Attributes["安全面"] == "是" {
		d.Security = true
	}
	if v := d.Full.Attributes["在产影响"]; v == "灰度可回滚" || v == "直接影响在产" {
		d.Production = true
	}
	if d.Production || d.Security {
		d.RiskAssessment = "declared"
	}
	d.Entries = append([]TemplateEntry(nil), d.Entries...)
	d.TemplateVersion = PilotTemplateVersion
	for i := range d.Entries {
		d.Entries[i].Fields = templateEffectiveFields(d.Entries[i].Fields, d.Defaults)
	}
	if d.Production || d.Security {
		d.RequestedMode = "full"
	}
	if d.RequestedMode == "" {
		d.RequestedMode = "light"
	}
	if d.TeamMode == "" {
		d.TeamMode = "single"
	}
	return d
}
func templateNow(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func templateAudit(ctx context.Context, tx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, draft, spec, actor, op string, detail any, now time.Time) error {
	raw, _ := json.Marshal(detail)
	_, err := tx.ExecContext(ctx, `INSERT INTO pilot_template_audit(draft_id,spec_id,actor_id,operation,details_json,created_at) VALUES(?,?,?,?,?,?)`, draft, spec, actor, op, string(raw), templateNow(now))
	return err
}

func templateContentSHA(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}
