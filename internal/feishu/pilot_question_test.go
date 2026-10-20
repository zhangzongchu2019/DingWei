package feishu

import (
	"context"
	"encoding/json"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPilotQuestionNativeSDKPrivateRecipientAndUUID(t *testing.T) {
	calls := 0
	var got map[string]any
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"SYNTHETIC_TEST_ONLY","expire":3600}`))
			return
		}
		calls++
		query = r.URL.RawQuery
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"message_id":"om_local"}}`))
	}))
	defer server.Close()
	g := &LarkGateway{botChannelID: "question-test", api: lark.NewClient("SYNTHETIC_APP", "SYNTHETIC_SECRET", lark.WithOpenBaseUrl(server.URL), lark.WithEnableTokenCache(false))}
	out := OutMessage{BotChannelID: "question-test", ToID: "ou_person", ToType: "personal", Text: "safe platform link", RequestID: "73b57d75-7fce-5b25-9c2c-04c3453ce827"}
	id, err := SendPilotQuestion(context.Background(), g, out)
	if err != nil || id != "om_local" || calls != 1 || got["uuid"] != out.RequestID || got["receive_id"] != "ou_person" || query != "receive_id_type=open_id" {
		t.Fatal("QUESTION_NATIVE_PRIVATE_UUID_LOST", id, err, calls, got, query)
	}
	for _, bad := range []OutMessage{
		{BotChannelID: "question-test", ToID: "oc_group", ToType: "personal", RequestID: out.RequestID},
		{BotChannelID: "question-test", ToID: "ou_person", ToType: "group", RequestID: out.RequestID},
		{BotChannelID: "question-test", ToName: "person", ToType: "personal", RequestID: out.RequestID},
		{BotChannelID: "wrong-bot", ToID: "ou_person", ToType: "personal", RequestID: out.RequestID},
		{BotChannelID: "question-test", ToID: "ou_person", ToType: "personal", RequestID: "fresh-random-unknown"},
	} {
		if _, err = SendPilotQuestion(context.Background(), g, bad); err == nil {
			t.Fatal("QUESTION_UNBOUND_PROVIDER_RECIPIENT", bad)
		}
	}
	if calls != 1 {
		t.Fatal("QUESTION_BAD_RECIPIENT_REACHED_PROVIDER", calls)
	}
}
