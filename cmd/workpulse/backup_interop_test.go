package main

// T-G4-53 返修（台账 lead-g4-t53-review）：真节点导出器（nodeagent.NewExportSeatHandler，钉证 TLS）
// 对真 Hub 接收端（hosted.HubBackuper.BackupHandler）——sha256 trailer 两边协议对得上、凭据文件不进落盘件。
// 两侧各自的用例都用对方的假件；这一格把两个真件接在一起。

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

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
	"github.com/zhangzongchu2019/dingwei/internal/nodeagent"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// gwToNodeAgent 网关：export_seat 直接交给真 nodeagent 处理器（同步等它回传完）。
type gwToNodeAgent struct{ h nodeagent.Handler }

func (g gwToNodeAgent) Send(ctx context.Context, nodeID string, cmd hosted.NodeCommand) (hosted.NodeResult, error) {
	if cmd.Type != "export_seat" {
		return hosted.NodeResult{OK: true}, nil
	}
	if _, err := g.h(ctx, cmd.Params); err != nil {
		return hosted.NodeResult{OK: false, Error: err.Error()}, nil
	}
	return hosted.NodeResult{OK: true}, nil
}

func TestBackupRealNodeAgentToRealHub(t *testing.T) {
	ctx := context.Background()
	st := hosted.NewStore(pgtest.Pool(t))
	if err := st.DropSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}
	secret, err := st.RegisterNode(ctx, hosted.Node{ID: "nd_io", Name: "io", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}, "admin")
	if err != nil {
		t.Fatal(err)
	}

	backupDir := t.TempDir()
	mux := http.NewServeMux()
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	pin := sha256.Sum256(srv.Certificate().Raw)

	seatRoot := t.TempDir()
	seat := filepath.Join(seatRoot, "ag_io")
	for name, body := range map[string]string{
		"work/notes.md":      strings.Repeat("卷内容 ", 5000),
		"account-credential": "SECRET-ACCOUNT-TOKEN",
		"enroll-token.json":  `{"poll_token":"SECRET-POLL"}`,
	} {
		p := filepath.Join(seat, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hubWSS := "wss://" + strings.TrimPrefix(srv.URL, "https://") + "/api/node/connect"
	export, err := nodeagent.NewExportSeatHandler(seatRoot, hubWSS, func() (string, error) { return secret, nil }, hex.EncodeToString(pin[:]))
	if err != nil {
		t.Fatal(err)
	}
	bk := hosted.NewHubBackuper(st, gwToNodeAgent{h: export}, srv.URL, backupDir)
	mux.Handle("POST /api/node/backup/{id}", bk.BackupHandler())

	ref, err := bk.BackupSeatVolume(ctx, "nd_io", "ag_io")
	if err != nil {
		t.Fatalf("real node agent → real hub backup failed: %v", err)
	}
	f, err := os.Open(ref)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		seen[hdr.Name] = true
	}
	if !seen["ag_io/work/notes.md"] || seen["ag_io/account-credential"] || seen["ag_io/enroll-token.json"] {
		t.Fatalf("stored backup entries %v: want data kept, credential files excluded", seen)
	}
}
