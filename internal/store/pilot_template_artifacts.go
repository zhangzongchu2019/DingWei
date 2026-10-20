package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Artifacts are deterministic scaffolding. No imported/generated checkbox is a
// successful test, reviewer receipt, authorization or business signoff.
func TemplateArtifacts(v TemplateView) map[string]string {
	constitution := "# Pilot constitution · " + PilotTemplateVersion + "\n\nSource: " + v.Source.ID + " SHA256 " + v.Source.SHA + "\n\n- Evidence remains factual; unknown is incomplete.\n- No real action without specific authorization.\n- Received/done/Agent review is not human business signoff.\n- Immutable revisions; changed plans require new responsibility receipts and fresh Agent review.\n- Legacy specs are not silently enrolled.\n"
	var review strings.Builder
	fmt.Fprintf(&review, "# Independent reviewer checklist · %s\n\nPlan %s / draft %s version %d\n\n", PilotTemplateVersion, v.PlanSHA, v.ID, v.Version)
	for _, e := range v.Document.Entries {
		fmt.Fprintf(&review, "- [ ] %s: source quote, six fields, environment, budget and independent role checked; actual evidence/reference still required.\n", e.ID)
	}
	review.WriteString("\nAll items begin NOT_RUN. Actual Agent review uses R8; human handles each issue before finalization. These boxes cannot finalize a spec.\n")
	analysis, _ := json.MarshalIndent(map[string]any{"template_version": PilotTemplateVersion, "kind": "structural_missing_fields_only", "draft_id": v.ID, "version": v.Version, "plan_sha256": v.PlanSHA, "missing": v.Missing, "roles": v.Roles, "independent_review_status": "use_actual_R8_gate", "execution_status": "NOT_RUN", "executed": false, "business_signed_off": false, "execution_authorized": false}, "", "  ")
	contract, _ := json.MarshalIndent(v.Document, "", "  ")
	return map[string]string{"constitution.md": constitution, "spec.md": v.ContentMarkdown, "reviewer-checklist.md": review.String(), "analyze.json": string(analysis) + "\n", "contract.json": string(contract) + "\n"}
}
