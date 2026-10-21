package store

import (
	"context"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/clarificationimage"
	"strings"
	"testing"
)

func TestClarificationPrivateImageActorQuestionAndImmutableLink(t *testing.T) {
	db, now, q := clarificationFixture(t)
	ctx := context.Background()
	c, e := db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	if e != nil {
		t.Fatal(e)
	}
	f := clarificationimage.File{ID: strings.Repeat("e", 64), SHA: strings.Repeat("a", 64), Media: "image/png", Size: 24}
	actor := BlockerActor{UserID: "pm"}
	check := func() error { return nil }
	if e = db.RegisterClarificationImage(ctx, c, actor, q.ID, f, check, now); e != nil {
		t.Fatal(e)
	}
	if _, e = db.ReadClarificationImageMetadata(ctx, c, BlockerActor{AgentID: "dev"}, q.ID, f.ID, check); !errors.Is(e, ErrTaskFlowPermission) {
		t.Fatal("CLARIFICATION_UNLINKED_IMAGE_DISCLOSED", e)
	}
	bad := clarificationUpdate("steal-staged-image")
	bad.Kind = "technical"
	bad.Files = []string{f.ID}
	if _, e = db.AppendClarification(ctx, c, BlockerActor{AgentID: "dev"}, q.ID, bad, true, check, now); !errors.Is(e, ErrTaskFlowPermission) {
		t.Fatal("CLARIFICATION_OTHER_ACTOR_STAGED_IMAGE_ATTACHED", e)
	}
	u := clarificationUpdate("image-post")
	u.Files = []string{f.ID}
	out, e := db.AppendClarification(ctx, c, actor, q.ID, u, true, check, now)
	if e != nil || len(out.Post.Images) != 1 {
		t.Fatal(out, e)
	}
	c, e = db.QuestionAuthorizationContext(ctx, "", "", q.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.ReadClarificationImageMetadata(ctx, c, BlockerActor{AgentID: "dev"}, q.ID, f.ID, check); e != nil {
		t.Fatal("CLARIFICATION_LINKED_IMAGE_UNREADABLE", e)
	}
	if _, e = db.AppendClarification(ctx, c, actor, q.ID, u, true, check, now); e != nil {
		t.Fatal("CLARIFICATION_IMAGE_REPLAY_FAILED", e)
	}
	u.RequestID = "reuse-image"
	if _, e = db.AppendClarification(ctx, c, actor, q.ID, u, true, check, now); !errors.Is(e, ErrTaskFlowPermission) {
		t.Fatal("CLARIFICATION_IMAGE_RELINKED", e)
	}
	for _, sql := range []string{`DELETE FROM spec_clarification_post_image`, `UPDATE spec_clarification_image SET size_bytes=1`} {
		if _, e = db.db.Exec(sql); e == nil {
			t.Fatal("CLARIFICATION_IMAGE_MUTABLE", sql)
		}
	}
}
