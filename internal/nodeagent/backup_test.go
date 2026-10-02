package nodeagent

// export_seat 用例：tar 内容逐字、回传鉴权、外泄闸（upload_url 非本 Hub 必拒）、缺席拒。

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkExportRig(t *testing.T, seatFiles map[string]string) (Handler, *httptest.Server, *string) {
	t.Helper()
	root := t.TempDir()
	dir := seatDir(root, "ag_bk1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range seatFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	// HubURL 配置成 wss://<srv-host>/api/node/connect —— upload_url 须同源
	wssURL := "wss" + strings.TrimPrefix(srv.URL, "http") + "/api/node/connect"
	// httptest 是 http 不是 https——测试里把换算后的 https 源换回 http 源（只换 scheme，同源纪律不动）
	h, err := newExportSeatHandlerForTest(root, wssURL, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return h, srv, &gotAuth
}

// newExportSeatHandlerForTest 测试构造：绕过 TLS（httptest 是明文 http），同源校验逻辑不变。
func newExportSeatHandlerForTest(seatRoot, hubWSS, realBase string) (Handler, error) {
	if !strings.HasPrefix(hubWSS, "wss://") {
		return nil, os.ErrInvalid
	}
	origin := "https://" + strings.TrimPrefix(hubWSS, "wss://")
	if i := strings.Index(origin[len("https://"):], "/"); i >= 0 {
		origin = origin[:len("https://")+i]
	}
	d := &exportSeatDeps{
		seatRoot:  seatRoot,
		hubOrigin: origin,
		secretFn:  func() (string, error) { return "node-secret", nil },
		hc:        http.DefaultClient,
	}
	// 测试内把 https 源映射回 httptest 的 http 源
	d.hubOrigin = realBase
	return d.handle, nil
}

func TestExportSeatStreamsTarWithAuth(t *testing.T) {
	h, srv, authP := mkExportRig(t, map[string]string{"note.txt": "hello-backup"})
	out, err := h(context.Background(), json.RawMessage(
		`{"seat_id":"ag_bk1","upload_url":"`+srv.URL+`/api/node/backup/bk_1"}`))
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(string(out), "exported") {
		t.Fatalf("应答: %s", out)
	}
	if *authP != "Bearer node-secret" {
		t.Fatalf("回传必须带节点凭据: %q", *authP)
	}
}

// 直接级断言（tar 包内容）——单独一格，直接读 httptest 收到的 body。
func TestExportSeatTarContent(t *testing.T) {
	root := t.TempDir()
	dir := seatDir(root, "ag_bk2")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "work.log"), []byte("line-逐字"), 0o600)
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	h, err := newExportSeatHandlerForTest(root, "wss://hub.example/api/node/connect", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	out, err := h(context.Background(), json.RawMessage(
		`{"seat_id":"ag_bk2","upload_url":"`+srv.URL+`/api/node/backup/bk_2"}`))
	if err != nil {
		t.Fatal(err)
	}
	// 控制面补报（丙案）：结果载荷必须带 sha256，且等于回传流的真值
	var payload struct {
		SHA256 string `json:"sha256"`
	}
	if err := json.Unmarshal(out, &payload); err != nil || payload.SHA256 == "" {
		t.Fatalf("结果载荷必须补报 sha256: %s %v", out, err)
	}
	{
		sum := sha256.Sum256(body)
		if payload.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatalf("补报 sha256 应等于回传流真值: %s", payload.SHA256)
		}
	}
	gz, err := gzip.NewReader(strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("应是 gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	found := false
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		if strings.HasSuffix(hdr.Name, "work.log") {
			content, _ := io.ReadAll(tr)
			if string(content) != "line-逐字" {
				t.Fatalf("tar 内内容逐字: %q", content)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("tar 内缺 work.log")
	}
}

// 防外泄闸：upload_url 指向非本 Hub ⇒ 拒（不得把席位数据送到任意地址）。
func TestExportSeatRejectsForeignUploadURL(t *testing.T) {
	h, srv, _ := mkExportRig(t, map[string]string{"a": "b"})
	_ = srv
	if _, err := h(context.Background(), json.RawMessage(
		`{"seat_id":"ag_bk1","upload_url":"https://evil.example.com/x"}`)); err == nil ||
		!strings.Contains(err.Error(), "upload_url") {
		t.Fatalf("外址必须拒: %v", err)
	}
	// 缺席位目录 ⇒ 拒
	if _, err := h(context.Background(), json.RawMessage(
		`{"seat_id":"ag_gone","upload_url":"`+srv.URL+`/api/node/backup/bk_3"}`)); err == nil {
		t.Fatal("席位目录不在必须拒")
	}
}
