package m8

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type r10LegacyOnlyRepository struct{ store.Repository }

// 本夹具逐字节复用于 r48 和候选；只归零传输随机 ID 与墙钟，不改路由、正文、回执或 metadata。
func TestR10DisabledLegacyCompatibility(t *testing.T) {
	t.Setenv("DW_TASK_DISPATCH_LOCK_ENABLED", "")
	t.Setenv("FCS_RPC_URL", "")
	type reading struct {
		Case     string                     `json:"case"`
		Result   model.PrefixDispatchResult `json:"result"`
		Delivery *model.Envelope            `json:"delivery"`
	}
	var readings []reading
	for _, tc := range []struct {
		name, text, want                         string
		unknown, otherBot, hideCapability, group bool
	}{
		{name: "single_agent", text: "plain", want: "plain"},
		{name: "other_bot", text: "plain", otherBot: true},
		{name: "unknown_identity", text: "plain", unknown: true},
		{name: "invalid_explicit", text: "#Missing forbidden"},
		{name: "capability_hidden", text: "plain", want: "plain", hideCapability: true},
		{name: "group", text: "#opus50ts group", want: "group", group: true},
		{name: "command", text: "mirror on opus50ts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newAgentEnv(t)
			bindFeishuForAgentEnv(t, e, "ou_alice")
			if tc.hideCapability {
				e.hub.Repo = r10LegacyOnlyRepository{e.hub.Repo}
			}
			conn, status, body := e.dial(e.cred, "")
			if conn == nil {
				t.Fatalf("dial=%d %s", status, body)
			}
			defer conn.CloseNow()
			waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
			content, _ := json.Marshal(map[string]string{"text": tc.text})
			msg := model.Message{ID: "off-" + tc.name, FeishuMsgID: "off-" + tc.name, ChatEntityID: "devplatform:personal:ou_alice", BotChannelID: "devplatform", ChatType: model.ChatPersonal, SenderOpenID: "ou_alice", IngressProvenance: model.IngressFeishuWSAuthenticated, Content: string(content)}
			if tc.unknown {
				msg.SenderOpenID = "ou_unknown"
			}
			if tc.otherBot {
				msg.BotChannelID = "other-bot"
				msg.ChatEntityID = "other-bot:personal:ou_alice"
			}
			if tc.group {
				msg.ChatType = model.ChatGroup
				msg.ChatEntityID = "devplatform:group:oc_off"
			}
			res, err := e.hub.Dispatch(e.ctx, msg, tc.text)
			if err != nil {
				t.Fatal(err)
			}
			row := reading{Case: tc.name, Result: res}
			if (res.Matched && res.Reply == "") || tc.name == "command" {
				env := e.readEnvUntil(conn, 2*time.Second, func(env model.Envelope) bool {
					return env.Body == tc.want || metaString(env.Meta, "type") == "mirror_control"
				})
				if env == nil {
					t.Fatal("OFF_EXPECTED_LEGACY_DELIVERY_MISSING", tc.name, res)
				}
				for key := range env.Meta {
					if strings.HasPrefix(key, "r10_") {
						t.Fatal("OFF_R14_METADATA_LEAK", key)
					}
				}
				env.ID = ""
				env.TS = 0
				row.Delivery = env
			}
			readings = append(readings, row)
		})
	}
	raw, err := json.Marshal(readings)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("R10_OFF_COMPAT_JSON=%s", raw)
}
