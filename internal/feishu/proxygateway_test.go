package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProxyGatewaySend(t *testing.T) {
	var got OutMessage
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]string{"msg_id": "m-9"})
	}))
	defer srv.Close()

	gw := NewProxyGateway(srv.URL, "tok")
	msgID, err := gw.Send(context.Background(), OutMessage{BotChannelID: "b", ToID: "ou_x", Text: "hi"})
	if err != nil || msgID != "m-9" {
		t.Fatalf("id=%q err=%v", msgID, err)
	}
	if auth != "Bearer tok" || got.ToID != "ou_x" || got.Text != "hi" {
		t.Fatalf("auth=%q got=%+v", auth, got)
	}
}

func TestProxyGatewaySendErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	if _, err := NewProxyGateway(srv.URL, "bad").Send(context.Background(), OutMessage{Text: "x"}); err == nil {
		t.Fatal("expected error on non-2xx")
	}
}
