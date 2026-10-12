package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"io"
	"net/http"
	"testing"
	"time"
)

// Called by the actual primary-PG/H1 product fixture before Chrome takes the
// single slot. Requests cross native HTTP cookie/Origin/context validation.
func exerciseR4Control(t *testing.T, h *Hub, store *identity.PGStore, base string) {
	t.Helper()
	ctx := context.Background()
	token, _ := muxID()
	other, _ := muxID()
	for _, tok := range []string{token, other} {
		if err := store.CreateSession(ctx, identity.Session{TokenHash: identity.HashSecret(tok), UserIDLC: "owner", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	ctl := h.consoleMux.authContext(identity.HashSecret(token))
	request := func(method, path, tok, origin string, body any) (int, map[string]any) {
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, base+path, bytes.NewReader(data))
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: tok})
		req.Header.Set("Origin", origin)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		var out map[string]any
		json.Unmarshal(raw, &out)
		return res.StatusCode, out
	}
	id, _ := muxID()
	instance, _ := muxID()
	first, _ := muxID()
	second, _ := muxID()
	req := consoleMuxControl{RequestID: id, AuthContext: ctl, Instance: instance, Subscriptions: []consoleMuxDesired{{FullName: "agent00.owner", Instance: first}, {FullName: "agent04.owner", Instance: second}}}
	code, meta := request("POST", "/api/console/event-feeds", token, base, req)
	if code != 201 {
		t.Fatalf("create: %d %v", code, meta)
	}
	feedID := meta["feed_id"].(string)
	path := "/api/console/event-feeds/" + feedID
	defer request("DELETE", path, token, base, nil)
	scopes := meta["subscriptions"].([]any)
	scope := scopes[0].(map[string]any)
	generation := scope["generation"].(string)
	f := h.consoleMux.feeds[feedID]
	old, owner := muxScopes(f)
	_ = owner
	snap, _ := h.consoleMuxSnapshot(old["agent00.owner"].Agent)
	bad := req
	bad.RequestID, _ = muxID()
	bad.Revision = 1
	bad.Subscriptions = append([]consoleMuxDesired(nil), req.Subscriptions...)
	bad.Subscriptions[0].After = snap.Epoch + "-" + muxSeq(snap.H+1)
	if code, _ := request("PUT", path, token, base, bad); code != 409 {
		t.Fatalf("ahead: %d", code)
	}
	f.mu.Lock()
	if f.Revision != 1 || f.Scopes["agent00.owner"].Generation != generation {
		t.Fatal("ahead mutated desired")
	}
	f.Stream = newMuxStream()
	f.mu.Unlock()
	reset := req
	reset.RequestID, _ = muxID()
	reset.ResetRequestID, _ = muxID()
	reset.Revision = 1
	reset.Reset = &consoleMuxReset{FullName: "agent00.owner", Instance: first, Generation: generation, Round: "1"}
	code, newMeta := request("PUT", path, token, base, reset)
	if code != 200 {
		t.Fatalf("reset: %d %v", code, newMeta)
	}
	fresh, _ := muxScopes(f)
	if fresh["agent00.owner"].Generation == generation || fresh["agent00.owner"].Round != 2 || fresh["agent04.owner"].Generation != old["agent04.owner"].Generation {
		t.Fatal("reset scope isolation")
	}
	if fresh["agent00.owner"].Desired.After != snap.Epoch+"-0" {
		t.Fatal("wrong reset cut")
	}
	if code, dup := request("PUT", path, token, base, reset); code != 200 || dup["revision"] != newMeta["revision"] {
		t.Fatal("reset retry not idempotent")
	}
	conflict := reset
	conflict.RequestID, _ = muxID()
	if code, _ := request("PUT", path, token, base, conflict); code != 409 {
		t.Fatal("same reset ID/different body accepted")
	}
	if code, _ := request("PUT", path, other, base, reset); code != 404 {
		t.Fatalf("different session: %d", code)
	}
	if code, _ := request("PUT", path, token, "https://foreign.invalid", reset); code != 403 {
		t.Fatalf("foreign origin: %d", code)
	}
	ack := consoleMuxControl{RequestID: id, AuthContext: ctl, Revision: 2, AckOnly: true, Applied: []consoleMuxApplied{{FullName: "agent00.owner", Generation: generation, After: snap.Epoch + "-0"}}}
	ack.RequestID, _ = muxID()
	if code, _ := request("PUT", path, token, base, ack); code != 400 {
		t.Fatalf("old generation ACK: %d", code)
	}
	// Establish a confirmed baseline; plain desired cannot silently reduce it.
	f.mu.Lock()
	f.Scopes["agent00.owner"].Desired.After = snap.Epoch + "-1"
	f.mu.Unlock()
	plain := req
	plain.RequestID, _ = muxID()
	plain.Revision = 2
	if code, _ := request("PUT", path, token, base, plain); code != 409 {
		t.Fatalf("plain decrease: %d", code)
	}
	if err := store.RevokeSession(ctx, identity.HashSecret(token), time.Now()); err != nil {
		t.Fatal(err)
	}
	if code, _ := request("PUT", path, token, base, reset); code != 401 {
		t.Fatalf("revoked duplicate reset: %d", code)
	}
	// Cleanup is direct after cookie revocation; no production authorization bypass.
	if err := h.consoleMux.stop(f, ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("R4_REAL_HTTP_CONTROL ahead409 unchanged/reset binding+new generation+round/idempotency/conflict/other session/Origin/revoked/old ACK/plain downgrade PASS")
}
