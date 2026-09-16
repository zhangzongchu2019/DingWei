package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

type fakeQueue struct {
	got  []model.Message
	fail error
}

func (f *fakeQueue) Enqueue(_ context.Context, m model.Message) error {
	if f.fail != nil {
		return f.fail
	}
	f.got = append(f.got, m)
	return nil
}
func (f *fakeQueue) Dequeue(context.Context) (*model.Message, error) { return nil, nil }
func (f *fakeQueue) Ack(context.Context, string) error               { return nil }
func (f *fakeQueue) Fail(context.Context, string, string) error      { return nil }

func notifyReq(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/internal/notify", strings.NewReader(body))
}

// 🔴 本测试的重点不是"它能不能发",是"它会不会在【发不出去时假装成功】" ——
// 那正是本项目当日最贵的一族错误(失败没有声音)。
func TestInternalNotify(t *testing.T) {
	t.Setenv("WP_ALERT_CHAT_ENTITY", "chat-x")
	t.Setenv("WP_ALERT_BOT_CHANNEL", "bot-y")

	t.Run("入队成功返回 202 且正文落到队列", func(t *testing.T) {
		q := &fakeQueue{}
		h := makeInternalNotifyHandler(q, nilLogger())
		rr := httptest.NewRecorder()
		h(rr, notifyReq(`{"text":"巡检失败:3 条断言不成立"}`))
		if rr.Code != http.StatusAccepted {
			t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
		}
		if len(q.got) != 1 {
			t.Fatalf("入队条数=%d", len(q.got))
		}
		m := q.got[0]
		if m.ChatEntityID != "chat-x" || m.BotChannelID != "bot-y" {
			t.Fatalf("目标/渠道不对: %+v", m)
		}
		var c map[string]any
		if err := json.Unmarshal([]byte(m.Content), &c); err != nil || c["text"] == "" {
			t.Fatalf("正文未正确编码: %s", m.Content)
		}
		if !strings.HasPrefix(m.ID, "alert-") {
			t.Fatalf("ID 应可一眼辨识来源: %s", m.ID)
		}
	})

	// ---------- 🔴 fail-closed:下面几条才是本测试存在的理由 ----------
	t.Run("未配置目标 ⇒ 404 而非静默成功", func(t *testing.T) {
		t.Setenv("WP_ALERT_CHAT_ENTITY", "")
		q := &fakeQueue{}
		h := makeInternalNotifyHandler(q, nilLogger())
		rr := httptest.NewRecorder()
		h(rr, notifyReq(`{"text":"x"}`))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("未配置时必须 404,得到 %d", rr.Code)
		}
		if len(q.got) != 0 {
			t.Fatal("未配置时不得入队")
		}
		if !strings.Contains(rr.Body.String(), "WP_ALERT_CHAT_ENTITY") {
			t.Fatal("必须说明缺了什么 —— 否则排查者只知道「不行」,不知道「为什么不行」")
		}
	})

	t.Run("队列为 nil ⇒ 404 而非 panic 或静默成功", func(t *testing.T) {
		h := makeInternalNotifyHandler(nil, nilLogger())
		rr := httptest.NewRecorder()
		h(rr, notifyReq(`{"text":"x"}`))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("code=%d", rr.Code)
		}
	})

	t.Run("入队失败 ⇒ 503,不得回 202", func(t *testing.T) {
		q := &fakeQueue{fail: context.DeadlineExceeded}
		h := makeInternalNotifyHandler(q, nilLogger())
		rr := httptest.NewRecorder()
		h(rr, notifyReq(`{"text":"x"}`))
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("入队失败必须让调用方知道,得到 %d", rr.Code)
		}
	})

	t.Run("空正文与未知字段一律拒", func(t *testing.T) {
		for _, body := range []string{`{"text":""}`, `{"text":"  "}`, `{}`,
			`{"text":"x","extra":1}`, `not-json`} {
			q := &fakeQueue{}
			h := makeInternalNotifyHandler(q, nilLogger())
			rr := httptest.NewRecorder()
			h(rr, notifyReq(body))
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("body=%s 应 400,得到 %d", body, rr.Code)
			}
			if len(q.got) != 0 {
				t.Fatalf("body=%s 不得入队", body)
			}
		}
	})
}
