package httpapi_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestR31AgentListHostTypeAndBearerPrivacy(t *testing.T) {
	e := newEnv(t)
	app := e.do("POST", "/api/enroll/applications", applyBody("zzc"), nil).body["application_id"].(string)
	approved := e.do("POST", "/api/console/enroll/applications/"+app+"/approve", map[string]any{"pairing_code": code, "agent_name": "Selfie"}, owner("zzc"))
	if approved.status != 200 {
		t.Fatal(approved.body)
	}
	id := approved.body["agent_id"].(string)
	if err := e.store.RecordAgentConnection(context.Background(), id, "203.0.113.10", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"zzc", "lbl"} {
		r := e.do("GET", "/api/console/agents", nil, owner(actor))
		if r.status != 200 {
			t.Fatal(r.body)
		}
		raw, _ := json.Marshal(r.body)
		if actor == "zzc" {
			if !strings.Contains(string(raw), `"host":"203.0.113.10"`) || !strings.Contains(string(raw), `"agent_type":"selfhost"`) {
				t.Fatalf("owner missing source/type: %s", raw)
			}
		} else if strings.Contains(string(raw), "203.0.113.10") {
			t.Fatal("other user saw IP")
		}
	}
	hdr := owner("zzc")
	hdr["Authorization"] = "Bearer fixture"
	r := e.do("GET", "/api/console/agents", nil, hdr)
	raw, _ := json.Marshal(r.body)
	if strings.Contains(string(raw), "203.0.113.10") {
		t.Fatal("bearer inherited source IP authority")
	}
}
