package fcs

// webhookingress_test.go — the FCS-process webhook ingress (走甲 phase-2) verifies +
// parses + feeds the SAME Ingest pipeline, and answers the url_verification
// challenge. Byte-equivalent to the hub's old handleFeishuWebhook, minus the local
// enqueue (which becomes fanout).

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// fakeProvider is a BotChannelProvider with one receivable channel and the given
// webhook keys (empty keys = unsigned plaintext accepted).
type fakeProvider struct{ enc, tok string }

func (fakeProvider) ListActive(context.Context) ([]BotChannelConfig, error) {
	return []BotChannelConfig{{ID: "dev", Name: "UnifiedRobot", CanReceive: true, CanSend: true}}, nil
}
func (fakeProvider) AppSecret(context.Context, string) (string, error) { return "", nil }
func (f fakeProvider) WebhookKeys(context.Context, string) (string, string, error) {
	return f.enc, f.tok, nil
}

type captureIngester struct {
	mu  sync.Mutex
	got []model.Message
}

func (c *captureIngester) Ingest(_ context.Context, m model.Message) error {
	c.mu.Lock()
	c.got = append(c.got, m)
	c.mu.Unlock()
	return nil
}

func webhookIngressServer(t *testing.T, prov BotChannelProvider, ing Ingester) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("POST /webhook/{channel}", NewWebhookIngress(prov, ing, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestWebhookIngressUnsignedFeedsIngest(t *testing.T) {
	ing := &captureIngester{}
	srv := webhookIngressServer(t, fakeProvider{}, ing) // no keys → unsigned plaintext

	body := `{"msg_id":"fm1","chat_type":"personal","open_id":"ou_1","sender_open_id":"ou_1","name":"Alice","text":"帮助"}`
	resp, err := http.Post(srv.URL+"/webhook/dev", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST webhook: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		msg, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 202 (%s)", resp.StatusCode, msg)
	}
	ing.mu.Lock()
	defer ing.mu.Unlock()
	if len(ing.got) != 1 {
		t.Fatalf("ingested %d messages, want 1", len(ing.got))
	}
	m := ing.got[0]
	if m.BotChannelID != "dev" || m.FeishuMsgID != "fm1" || m.ChatEntityID != "dev:personal:ou_1" || !strings.Contains(m.Content, "帮助") {
		t.Fatalf("parsed message wrong: %+v", m)
	}
}

func TestWebhookIngressAnswersChallenge(t *testing.T) {
	ing := &captureIngester{}
	srv := webhookIngressServer(t, fakeProvider{}, ing)

	body := `{"type":"url_verification","challenge":"cc-123"}`
	resp, err := http.Post(srv.URL+"/webhook/dev", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST challenge: %v", err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(out), "cc-123") {
		t.Fatalf("challenge response code=%d body=%s", resp.StatusCode, out)
	}
	ing.mu.Lock()
	defer ing.mu.Unlock()
	if len(ing.got) != 0 {
		t.Fatalf("url_verification must not ingest an event, got %d", len(ing.got))
	}
}

func TestWebhookIngressTokenMismatchFailsClosed(t *testing.T) {
	ing := &captureIngester{}
	srv := webhookIngressServer(t, fakeProvider{tok: "expected"}, ing)

	body := `{"header":{"token":"WRONG"},"type":"event","msg_id":"fm2","chat_type":"personal","open_id":"ou_1"}`
	resp, err := http.Post(srv.URL+"/webhook/dev", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		t.Fatalf("token mismatch must fail closed, got 202")
	}
	ing.mu.Lock()
	defer ing.mu.Unlock()
	if len(ing.got) != 0 {
		t.Fatalf("failed-verify webhook must not ingest, got %d", len(ing.got))
	}
}
