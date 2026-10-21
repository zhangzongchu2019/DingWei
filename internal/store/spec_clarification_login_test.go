package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestClarificationLoginContinuationCookieExpiryAndSingleUse(t *testing.T) {
	db, now, _ := clarificationFixture(t)
	ctx := context.Background()
	state := strings.Repeat("a", 64)
	cookie := strings.Repeat("b", 64)
	if e := db.CreateClarificationLogin(ctx, state, cookie, "question-one", now); e != nil {
		t.Fatal(e)
	}
	if _, e := db.ConsumeClarificationLogin(ctx, state, strings.Repeat("c", 64), now); !errors.Is(e, ErrTaskFlowConflict) {
		t.Fatal("CLARIFICATION_RETURN_COOKIE_UNBOUND", e)
	}
	if q, e := db.ConsumeClarificationLogin(ctx, state, cookie, now); e != nil || q != "question-one" {
		t.Fatal(q, e)
	}
	if _, e := db.ConsumeClarificationLogin(ctx, state, cookie, now); !errors.Is(e, ErrTaskFlowConflict) {
		t.Fatal("CLARIFICATION_RETURN_REPLAYED", e)
	}
	state = strings.Repeat("d", 64)
	if e := db.CreateClarificationLogin(ctx, state, cookie, "q", now); e != nil {
		t.Fatal(e)
	}
	if _, e := db.ConsumeClarificationLogin(ctx, state, cookie, now.Add(11*time.Minute)); !errors.Is(e, ErrTaskFlowConflict) {
		t.Fatal("CLARIFICATION_RETURN_EXPIRED_ACCEPTED", e)
	}
}
