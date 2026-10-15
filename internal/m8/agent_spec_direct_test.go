package m8

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func directSpecRequest(t *testing.T, e *shimEnv, method, id, credential, protocol, body string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/api/agent/console/specs"
	if id != "" {
		path += "/" + id
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.SetPathValue("specID", id)
	if credential != "" {
		r.Header.Set("Authorization", "Bearer "+credential)
	}
	if protocol != "" {
		r.Header.Set("X-DW-Spec-Write", protocol)
	}
	handler := e.hub.HandleConsoleSpecs
	if id != "" {
		handler = e.hub.HandleConsoleSpec
	}
	out := httptest.NewRecorder()
	e.hub.RequireAgentSpecDirect(handler)(out, r)
	return out
}

func TestAgentSpecDirectAuthenticationAndLegacyFailClosed(t *testing.T) {
	e := newShimEnv(t)
	for _, c := range []struct {
		credential, protocol string
		want                 int
	}{{"", "direct-v1", 401}, {"synthetic-invalid", "direct-v1", 403}, {e.zKimiCred, "", 426}, {e.zKimiCred, "direct-v1", 201}} {
		result := directSpecRequest(t, e, "POST", "", c.credential, c.protocol, validSpecBody)
		if result.Code != c.want {
			t.Fatalf("status=%d expected=%d", result.Code, c.want)
		}
		if c.want == 426 && !strings.Contains(result.Body.String(), "upgrade_required") {
			t.Fatal("no upgrade result")
		}
	}
	if err := e.svc.Store.SetUserState(e.ctx, "zzc", identity.UserDisabled, e.svc.Now()); err != nil {
		t.Fatal(err)
	}
	if result := directSpecRequest(t, e, "POST", "", e.zKimiCred, "direct-v1", validSpecBody); result.Code != 403 {
		t.Fatal("disabled owner can create")
	}
	if err := e.svc.Store.SetUserState(e.ctx, "zzc", identity.UserActive, e.svc.Now()); err != nil {
		t.Fatal(err)
	}
	agent, err := e.svc.Store.FindAgentByName(e.ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.Revoke(e.ctx, "zzc", agent.ID); err != nil {
		t.Fatal(err)
	}
	if result := directSpecRequest(t, e, "POST", "", e.zKimiCred, "direct-v1", validSpecBody); result.Code != 403 {
		t.Fatal("revoked agent can create")
	}
}

func TestAgentSpecDirectSelfOwnershipAttributionAndConflict(t *testing.T) {
	e := newShimEnv(t)
	rr := directSpecRequest(t, e, "POST", "", e.zKimiCred, "direct-v1", validSpecBody)
	if rr.Code != 201 {
		t.Fatalf("create=%d", rr.Code)
	}
	var spec model.ConsoleSpec
	if json.Unmarshal(rr.Body.Bytes(), &spec) != nil {
		t.Fatal("bad create")
	}
	update := strings.TrimSuffix(validSpecBody, "}") + `,"expected_revision":1,"created_by":"forged"}`
	for _, cred := range []string{e.cred, e.lKimiCr} {
		rr = directSpecRequest(t, e, "PATCH", spec.ID, cred, "direct-v1", update)
		if rr.Code != 403 || !strings.Contains(rr.Body.String(), "只能更新由当前 Agent 创建") {
			t.Fatalf("other agent update=%d %s", rr.Code, rr.Body.String())
		}
	}
	rr = directSpecRequest(t, e, "PATCH", spec.ID, e.zKimiCred, "direct-v1", update)
	if rr.Code != 200 {
		t.Fatalf("self update=%d %s", rr.Code, rr.Body.String())
	}
	rr = directSpecRequest(t, e, "PATCH", spec.ID, e.zKimiCred, "direct-v1", update)
	if rr.Code != 409 {
		t.Fatal("revision CAS bypassed")
	}
	attribution, err := e.db.ConsoleSpecAttribution(e.ctx, spec.ID)
	if err != nil || attribution.CreatorKind != "agent" || attribution.Creator == "forged" || attribution.LastEditedAt.IsZero() {
		t.Fatal("incorrect trusted attribution")
	}
	principal := callConsoleSpec(t, e.hub, "zzc", consolePlatformActionSubmit, http.MethodPost, "/api/console/specs", validSpecBody, e.hub.HandleConsoleSpecs)
	if principal.Code != 201 {
		t.Fatalf("principal create=%d", principal.Code)
	}
	var p model.ConsoleSpec
	_ = json.Unmarshal(principal.Body.Bytes(), &p)
	rr = directSpecRequest(t, e, "PATCH", p.ID, e.zKimiCred, "direct-v1", update)
	if rr.Code != 403 {
		t.Fatal("agent updated principal-created spec")
	}
}
