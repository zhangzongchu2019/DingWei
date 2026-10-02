package nodeagent

// T-G4-53 返修（台账 lead-g4-t53-review，opus50tc2k）：导出排除凭据、sha256 trailer、Hub 拒收与 tar 非零即失败。

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

type uploadSeen struct {
	body    []byte
	trailer string
}

// mkExportDeps 直接构造处理器：Hub 端点由 status 决定应答，tarCmd 可注入（nil＝真 tar）。
func mkExportDeps(t *testing.T, files map[string]string, status int, tarCmd func(context.Context, ...string) *exec.Cmd) (Handler, string, *uploadSeen) {
	t.Helper()
	root := t.TempDir()
	dir := seatDir(root, "ag_x1")
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	seen := &uploadSeen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.body, _ = io.ReadAll(r.Body)
		seen.trailer = r.Trailer.Get(BackupSHA256Trailer)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	d := &exportSeatDeps{seatRoot: root, hubOrigin: srv.URL, secretFn: func() (string, error) { return "node-secret", nil },
		hc: http.DefaultClient, tarCmd: tarCmd}
	return d.handle, srv.URL, seen
}

func exportParams(base string) json.RawMessage {
	return json.RawMessage(`{"seat_id":"ag_x1","upload_url":"` + base + `/api/node/backup/bk_t"}`)
}

func tarEntries(t *testing.T, body []byte) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("not gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		c, _ := io.ReadAll(tr)
		out[hdr.Name] = string(c)
	}
}

// 🔴 缺陷一（复验 REVIEW-G4-T53）：席位根目录下的 account-credential 与 enroll-token.json 不得进备份；
// 数据子目录里同名的用户文件照常备份（排除只认席位根这一处）。
func TestExportSeatExcludesCredentialFiles(t *testing.T) {
	h, base, seen := mkExportDeps(t, map[string]string{
		"work.log":                "data",
		"account-credential":      "SECRET-ACCOUNT-TOKEN",
		"enroll-token.json":       `{"poll_token":"SECRET-POLL"}`,
		"data/account-credential": "user file that happens to share the name",
	}, 200, nil)
	if _, err := h(context.Background(), exportParams(base)); err != nil {
		t.Fatal(err)
	}
	got := tarEntries(t, seen.body)
	var names []string
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, bad := range []string{"ag_x1/account-credential", "ag_x1/enroll-token.json"} {
		if _, ok := got[bad]; ok {
			t.Errorf("credential file %s leaked into backup; entries %v", bad, names)
		}
	}
	for _, s := range []string{"SECRET-ACCOUNT-TOKEN", "SECRET-POLL"} {
		if bytes.Contains(seen.body, []byte(s)) {
			t.Errorf("secret %q present in backup stream", s)
		}
	}
	if got["ag_x1/work.log"] != "data" || got["ag_x1/data/account-credential"] == "" {
		t.Errorf("data files must be kept; entries %v", names)
	}
}

// sha256 随请求 trailer 送出，且等于实际送出字节的 sha256。
func TestExportSeatSendsSHA256Trailer(t *testing.T) {
	h, base, seen := mkExportDeps(t, map[string]string{"work.log": "data"}, 200, nil)
	if _, err := h(context.Background(), exportParams(base)); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(seen.body)
	if seen.trailer == "" || seen.trailer != hex.EncodeToString(sum[:]) {
		t.Fatalf("trailer %q, want sha256 of the %d bytes sent", seen.trailer, len(seen.body))
	}
}

// 复验存活刀 B11：Hub 拒收（非 2xx）⇒ 导出失败，不得报 exported。
func TestExportSeatFailsWhenHubRejects(t *testing.T) {
	for _, status := range []int{422, 500, 401} {
		h, base, _ := mkExportDeps(t, map[string]string{"work.log": "data"}, status, nil)
		if out, err := h(context.Background(), exportParams(base)); err == nil {
			t.Errorf("HTTP %d: export reported success %s", status, out)
		}
	}
}

// 复验存活刀 B12：tar 非零退出 ⇒ 导出失败（哪怕它吐出了一段字节、Hub 也回了 200）。
func TestExportSeatFailsWhenTarFails(t *testing.T) {
	failingTar := func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "printf 'partial-archive'; exit 2")
	}
	h, base, _ := mkExportDeps(t, map[string]string{"work.log": "data"}, 200, failingTar)
	if out, err := h(context.Background(), exportParams(base)); err == nil {
		t.Fatalf("tar exited 2 but export reported success %s", out)
	}
}
