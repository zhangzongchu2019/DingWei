package hosted

import (
	"os"
	"path/filepath"
	"testing"
)

// 装配②：静态登记表 fail-closed——空表全不兼容/无镜像即拒；畸形文件启动期报错。
func TestStaticRegistriesFailClosed(t *testing.T) {
	parallelHostedTest(t)
	dir := t.TempDir()
	// 空路径 ⇒ 空表：一切组合不兼容、无登记镜像
	c0, err := LoadCombos("")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c0.Lookup("codex", "gpt", "token_file"); ok {
		t.Fatal("空表不得有任何组合")
	}
	i0, err := LoadImages("")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := i0.Latest("codex"); ok {
		t.Fatal("空登记不得有镜像")
	}
	// 正常表：verified 逐字、List 全量
	good := filepath.Join(dir, "combos.json")
	os.WriteFile(good, []byte(`[{"cli":"codex","llm":"gpt","cred_kind":"token_file","verified":true},
		{"cli":"codex","llm":"gpt","cred_kind":"api_key","verified":false},
		{"cli":"claude","llm":"claude","cred_kind":"token_file","verified":false}]`), 0o600)
	c1, err := LoadCombos(good)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := c1.Lookup("codex", "gpt", "token_file"); !ok || !v {
		t.Fatal("验通组合应 true")
	}
	if v, ok := c1.Lookup("codex", "gpt", "api_key"); !ok || v {
		t.Fatal("未验通组合应 verified=false")
	}
	if v, ok := c1.Lookup("claude", "claude", "token_file"); !ok || v {
		t.Fatal("Claude entry must exist but remain unverified")
	}
	if len(c1.List()) != 3 {
		t.Fatal("List 应全量")
	}
	// 畸形：JSON 坏 / 行缺字段 ⇒ 报错
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`{not json`), 0o600)
	if _, err := LoadCombos(bad); err == nil {
		t.Fatal("畸形 JSON 必须报错")
	}
	os.WriteFile(bad, []byte(`[{"cli":"codex"}]`), 0o600)
	if _, err := LoadCombos(bad); err == nil {
		t.Fatal("缺字段行必须报错")
	}
	// 镜像：无 digest 的 ref ⇒ 拒
	badimg := filepath.Join(dir, "badimg.json")
	os.WriteFile(badimg, []byte(`{"codex":"hub.local/dingwei/base-codex:latest"}`), 0o600)
	if _, err := LoadImages(badimg); err == nil {
		t.Fatal("无 digest 的镜像 ref 必须拒")
	}
	goodimg := filepath.Join(dir, "img.json")
	os.WriteFile(goodimg, []byte(`{"codex":"hub.local/dingwei/base-codex@sha256:`+
		`0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`), 0o600)
	i1, err := LoadImages(goodimg)
	if err != nil {
		t.Fatal(err)
	}
	if ref, ok := i1.Latest("codex"); !ok || ref == "" {
		t.Fatal("登记镜像应可取")
	}
	// 本机 image ID 形态（a26a825f528a：只收登记的裸 sha256 形）收；畸形仍拒
	localImg := filepath.Join(dir, "local.json")
	os.WriteFile(localImg, []byte(`{"codex":"local@sha256:`+
		`0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`), 0o600)
	i2, err := LoadImages(localImg)
	if err != nil {
		t.Fatalf("本机 ID 形态应收: %v", err)
	}
	if _, ok := i2.Latest("codex"); !ok {
		t.Fatal("本机 ID 登记应可取")
	}
	os.WriteFile(localImg, []byte(`{"codex":"sha256:tooshort"}`), 0o600)
	if _, err := LoadImages(localImg); err == nil {
		t.Fatal("短 digest 必须拒")
	}
}
