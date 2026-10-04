package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// The public capability path attests owner "zzc" to view-service; this cell pins the
// remaining signed view-service → Hub hop against a real /ws/agent connection.
func TestNewPlatformConsolePrincipalViewInputReachesAgent(t *testing.T) {
	e := newShimEnv(t)
	conn, status, bodyText := e.dial(e.zKimiCred, "")
	if conn == nil {
		t.Fatalf("agent dial: %d %s", status, bodyText)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "kimi")

	secret := []byte("abcdef0123456789abcdef0123456789")
	tmp := t.TempDir()
	secretFile := filepath.Join(tmp, "vs-to-hub.key")
	if err := os.WriteFile(secretFile, secret, 0o600); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(tmp, "routes.json")
	rawConfig, _ := json.Marshal(map[string]any{
		"sessions": map[string]string{"kimi.zzc": "canary"},
		"targets": map[string]any{"canary": map[string]any{
			"url": "http://127.0.0.1:19321", "view_unlock_v2": true,
			"vs_to_hub_secret_file": secretFile,
		}},
	})
	if err := os.WriteFile(configFile, rawConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIEW_ROUTING_CONFIG", configFile)
	resetViewRoutingCacheForTest()
	t.Cleanup(resetViewRoutingCacheForTest)

	requestBody, _ := json.Marshal(viewV2InputRequest{
		Session: "kimi.zzc", PageID: "view-page-owned-e2e", RequestID: "0123456789abcdef0123456789abcdef",
		Text: "owner input", PrincipalID: "zzc",
	})
	path := "/internal/view-v2/kimi.zzc/input"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "00112233445566778899aabbccddeeff"
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(requestBody))
	req.SetPathValue("session", "kimi.zzc")
	req.Header.Set("X-DW-VS-Target", "canary")
	req.Header.Set("X-DW-Timestamp", ts)
	req.Header.Set("X-DW-Nonce", nonce)
	req.Header.Set("X-DW-Signature", signViewServiceRequest(secret, vsToHubDirection, http.MethodPost, path, ts, nonce, requestBody))
	rr := httptest.NewRecorder()
	e.hub.HandleInternalViewV2Input(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("signed view input: %d %s", rr.Code, rr.Body.String())
	}

	readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	env := readEnvelope(t, readCtx, conn)
	if env.Body != "owner input\r" || metaString(env.Meta, "console_principal_id") != "zzc" {
		t.Fatalf("agent envelope: %+v", env)
	}
	from, err := parseAddress(env.From)
	attested := model.Envelope{Meta: map[string]any{hubIngressMetaKey: hubIngressViewV2, "console_principal_id": "zzc"}}
	if err != nil || !e.hub.newPlatformSenderExempt(e.ctx, from, attested) {
		t.Fatalf("console principal did not receive view grouping exemption: from=%+v err=%v meta=%v", from, err, env.Meta)
	}
}
