package m8

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"testing"
	"time"
)

func TestSpecGrantLegacyRequestAndPollFailFastWithoutWrites(t *testing.T) {
	v := newViewEnv(t)
	conn, status, _ := v.dial(v.zKimiCred, "")
	if status != 101 || conn == nil {
		t.Fatal("authenticated socket failed")
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	for _, kind := range []string{"spec_grant_request", "spec_grant_poll"} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		env := model.Envelope{ID: "legacy-" + kind, From: "kimi#u_zzc", To: "workpulse#u_zzc", Meta: map[string]any{"type": kind, "request_id": "legacy-correlation", "action": "create"}}
		raw, _ := json.Marshal(env)
		if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
			cancel()
			t.Fatal(err)
		}
		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				cancel()
				t.Fatalf("legacy response not immediate: %v", err)
			}
			var response model.Envelope
			if json.Unmarshal(raw, &response) != nil {
				continue
			}
			if metaString(response.Meta, "type") != "spec_grant_response" {
				continue
			}
			if metaString(response.Meta, "status") != "upgrade_required" || metaString(response.Meta, "request_id") != "legacy-correlation" {
				t.Fatal("missing explicit upgrade result")
			}
			break
		}
		cancel()
	}
	repo, _ := v.hub.consoleSpecs()
	specs, err := repo.ListConsoleSpecs(v.ctx, "zzc", false)
	if err != nil || len(specs) != 0 {
		t.Fatal("legacy request performed write")
	}
}
