package m8

import (
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestR31AdminAgentCapabilityIsLiveAndIndependentOfHosted(t *testing.T) {
	v := newViewEnv(t)
	for _, u := range []identity.User{{UserID: "rootadmin", ShortName: "ra", Type: identity.UserPlatformAdmin, State: identity.UserActive}, {UserID: "manager", ShortName: "mg", Type: identity.UserManager, State: identity.UserActive}} {
		if err := v.svc.Store.CreateUser(v.ctx, u); err != nil {
			t.Fatal(err)
		}
		if err := v.svc.Store.CreateSession(v.ctx, identity.Session{TokenHash: identity.HashSecret("tok-" + u.UserID), UserIDLC: u.UserID, ExpiresAt: v.svc.Now().Add(identity.SessionTTL)}); err != nil {
			t.Fatal(err)
		}
	}
	handler := v.hub.RequireConsolePrincipal(v.hub.RequireConsolePlatformAction("read", v.hub.HandleConsolePlatform))
	for _, user := range []string{"rootadmin", "manager", "zzc"} {
		request := httptest.NewRequest("GET", "/api/console/platform", nil)
		request.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: "tok-" + user})
		w := httptest.NewRecorder()
		handler(w, request)
		var response map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || response["agent_list_admin"] != (user == "rootadmin") {
			t.Fatalf("ADMIN_AGENT_CAPABILITY_MISSING: %s %d %s", user, w.Code, w.Body.String())
		}
	}
}
