package m8

import (
	"errors"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// R-V2-0103-SMALLFIXES A：发送方失败通知（display=none）的默认门槛固定为 0.2.1.100（依据见 amr.go
// amrNoDisplayNoticeDefaultMinVersion 头注），与 Hub 自身版本无关；env 覆盖口径不变。
// 断言面＝门槛谓词三值表与真发信（Hub → 会话连接）；实现承诺＝amrNoDisplayNoticeMinVersion 的默认值。
func TestNoDisplayNoticeFixedThreshold(t *testing.T) {
	t.Setenv("WP_AMR_NO_DISPLAY_NOTICE_MIN_VERSION", "") // 空 ⇒ 取默认常量

	if got := amrNoDisplayNoticeMinVersion(); got != "0.2.1.100" {
		t.Fatalf("默认门槛=%q，应为固定的 0.2.1.100（不随 Hub 版本走）", got)
	}
	for _, c := range []struct {
		version          string
		supports, tooOld bool
	}{
		{"0.2.1.99", false, true},  // 过旧 ⇒ 不发
		{"0.2.1.95", false, true},  // 歧义版本 ⇒ 保守不发
		{"0.2.1.100", true, false}, // 两托管席现役 ⇒ 发
		{"0.2.1.102", true, false},
		{"0.2.1.103", true, false},
		{"", false, false}, // 未知 ⇒ 照发、不打标记（三值第三行）
		{"not-a-version", false, false},
	} {
		if got := amrSupportsNoDisplayNotice(c.version); got != c.supports {
			t.Errorf("amrSupportsNoDisplayNotice(%q)=%v want %v", c.version, got, c.supports)
		}
		if got := amrClientTooOldForNoDisplayNotice(c.version); got != c.tooOld {
			t.Errorf("amrClientTooOldForNoDisplayNotice(%q)=%v want %v", c.version, got, c.tooOld)
		}
	}

	// env 覆盖仍有效：0.0.0.0 ⇒ 常开；更高门槛 ⇒ 0.2.1.100 也关。
	t.Setenv("WP_AMR_NO_DISPLAY_NOTICE_MIN_VERSION", "0.0.0.0")
	if !amrSupportsNoDisplayNotice("0.2.1.95") {
		t.Error("env=0.0.0.0 应常开")
	}
	t.Setenv("WP_AMR_NO_DISPLAY_NOTICE_MIN_VERSION", "0.2.1.102")
	if !amrClientTooOldForNoDisplayNotice("0.2.1.100") {
		t.Error("env 覆盖为 0.2.1.102 时 0.2.1.100 应判过旧")
	}
}

// 真发信：无 env 时，自报 0.2.1.100 的会话收到带 display=none 的通知；0.2.1.99 收不到；版本未知照发不打标记。
func TestNoDisplayNoticeFixedThresholdOnTheWire(t *testing.T) {
	t.Setenv("WP_AMR_NO_DISPLAY_NOTICE_MIN_VERSION", "")
	h, _, ctx := newTestHub(t)
	newConn := attachAMRNoticeClient(t, h, ctx, "thr-key", "thr-100")
	oldConn := attachAMRNoticeClient(t, h, ctx, "thr-key", "thr-099")
	unknownConn := attachAMRNoticeClient(t, h, ctx, "thr-key", "thr-unknown")
	cNew := setClientVersion(t, h, "thr-key", "thr-100", "0.2.1.100")
	cOld := setClientVersion(t, h, "thr-key", "thr-099", "0.2.1.99")
	cUnknown := setClientVersion(t, h, "thr-key", "thr-unknown", "")
	original := model.Envelope{ID: "dlv-thr", To: "@x", From: "s#thr-key", Body: "正文"}
	reason := errors.New("目标会话无法补全为在线 canonical 地址")

	h.writeDeliveryFailure(ctx, cNew, original, "@t100", reason)
	env, ok := tryReadNotice(ctx, newConn, 2*time.Second)
	if !ok {
		t.Fatal("自报 0.2.1.100 应收到发送方失败通知")
	}
	if got := metaString(env.Meta, "display"); got != "none" {
		t.Errorf("display=%q，应为 none", got)
	}

	h.writeDeliveryFailure(ctx, cOld, original, "@t099", reason)
	if env, ok := tryReadNotice(ctx, oldConn, 400*time.Millisecond); ok {
		t.Errorf("自报 0.2.1.99 不该收到通知，实收：%+v", env)
	}

	h.writeDeliveryFailure(ctx, cUnknown, original, "@tunk", reason)
	env, ok = tryReadNotice(ctx, unknownConn, 2*time.Second)
	if !ok {
		t.Fatal("版本未知应照发")
	}
	if v, has := env.Meta["display"]; has {
		t.Errorf("版本未知不该带 display=%v", v)
	}
}
