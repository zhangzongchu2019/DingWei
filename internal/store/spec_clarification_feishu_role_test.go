package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Isolate the last common append boundary from the transport's independent
// identity/subject/PM checks. Otherwise those guards mask withdrawal of :274.
// Real bound-member ingress is separately exercised in m8; this is not an
// assertion that a non-PM can reach this seam through production ingress.
func TestClarificationFeishuOriginRequiresPMAtAppendBoundary(t *testing.T) {
	for _, role := range []string{"deputy", "technical_reader", "pm"} {
		t.Run(role, func(t *testing.T) {
			db, now, q := clarificationFixture(t)
			ctx := context.Background()
			spec, err := db.QuestionAuthorizationContext(ctx, q.SpecID, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.SetClarificationPolicy(ctx, spec, BlockerActor{UserID: "pm"}, ClarificationPolicyUpdate{ExpectedVersion: 1, PMUser: "pm", DeputyUser: "deputy"}, func() error { return nil }, now); err != nil {
				t.Fatal(err)
			}
			actor := BlockerActor{UserID: role}
			u := clarificationUpdate("feishu-role-boundary")
			if role == "technical_reader" {
				actor.UserID = "owner-a"
				u.Kind = "technical"
			}
			c, err := db.QuestionAuthorizationContext(ctx, "", "", q.ID)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := db.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			result, err := db.appendClarificationTx(ctx, tx, c, actor, q.ID, u, true, func() error { return nil }, now, clarificationOrigin{Source: "feishu", Parent: strings.Repeat("b", 64)})
			if role == "pm" {
				if err != nil || result.Post.Role != "pm" || result.Post.Source != "feishu" {
					tx.Rollback()
					t.Fatal("CLARIFICATION_PM_ORIGIN_POSITIVE_FAILED", result, err)
				}
			} else if !errors.Is(err, ErrTaskFlowPermission) {
				tx.Rollback()
				t.Fatal("CLARIFICATION_FEISHU_NON_PM_ORIGIN_ACCEPTED_"+role, err, result.Post.Role)
			}
			if err = tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			// Both non-PM roles have a valid Console contribution. This proves that
			// refusal above is the Feishu-only role boundary, not a generic bad actor.
			console, err := clarificationApply(t, db, now, actor, q, u)
			if err != nil || console.Post.Source != "console" || console.Post.Floor != 1 {
				t.Fatal("CLARIFICATION_CONSOLE_ROLE_POSITIVE_FAILED", role, console, err)
			}
		})
	}
}
