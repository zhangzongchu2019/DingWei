package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/subagent"
)

func TestCheckAllCommandBlocksUnpreparedRoot(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "hub.sqlite")
	db, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"root-a", "root-b"} {
		if err = db.BeginSubAgentConnection(ctx, root, 1); err != nil {
			t.Fatal(err)
		}
		if _, err = db.ReportSubAgentTree(ctx, root, subagent.Snapshot{ConnSeq: 1, Revision: 1, Nodes: []subagent.Node{{NodeID: "a", Name: "aa", State: "active", Generation: 1}}}); err != nil {
			t.Fatal(err)
		}
		scope, err := db.ResolveSubAgent(ctx, root, 1, []string{"aa"})
		if err != nil {
			t.Fatal(err)
		}
		if err = db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: root + "-mail", KeyID: "k", SessionName: "parent", Envelope: `{}`, SubRoute: &subagent.DeliveryRoute{To: scope}}); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	prepare := func(root string) {
		t.Helper()
		var out, log bytes.Buffer
		if rc := run([]string{"--hub-db", path, "--root-agent-id", root, "--writers-stopped", "--feature-off", "--helper-sealed"}, &out, &log); rc != 0 {
			t.Fatal("single-root preparation", rc, log.String())
		}
	}
	check := func(wantRC, wantPending int, wantRoot string) {
		t.Helper()
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var out, log bytes.Buffer
		rc := run([]string{"--hub-db", path, "--check-all"}, &out, &log)
		var result store.SubAgentRollbackCheck
		if err := json.Unmarshal(out.Bytes(), &result); err != nil || rc != wantRC || result.Pending != wantPending {
			t.Fatal("AA009_CHECK_CLI_EXIT", rc, result, err, log.String())
		}
		if wantRoot != "" && (len(result.Roots) != 1 || result.Roots[0].RootAgentID != wantRoot || result.Roots[0].Count != 1 || len(result.Roots[0].DeliveryIDs) != 1) {
			t.Fatal("AA009_CHECK_CLI_ROOT", result)
		}
		after, err := os.ReadFile(path)
		if err != nil || sha256.Sum256(before) != sha256.Sum256(after) {
			t.Fatal("AA009_CHECK_CLI_WROTE_DATABASE", err)
		}
	}
	check(1, 2, "")
	prepare("root-a")
	check(1, 1, "root-b")
	prepare("root-b")
	check(0, 0, "")
}

func TestCheckAllCommandRejectsUnsafeInputs(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.sqlite")
	for _, args := range [][]string{{}, {"--hub-db", missing, "--check-all"}, {"--hub-db", missing, "--check-all", "--root-agent-id", "root-a"}, {"--hub-db", missing, "--check-all", "--feature-off"}, {"--hub-db", missing, "extra"}} {
		var out, log bytes.Buffer
		if rc := run(args, &out, &log); rc != 2 || out.Len() != 0 {
			t.Fatal("AA009_CHECK_INPUT_GATE", args, rc, out.String())
		}
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("AA009_CHECK_MISSING_FILE_CREATED", err)
	}
	path := filepath.Join(t.TempDir(), "unmigrated.sqlite")
	db, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out, log bytes.Buffer
	if rc := run([]string{"--hub-db", path, "--check-all"}, &out, &log); rc != 1 || out.Len() != 0 {
		t.Fatal("AA009_CHECK_SCHEMA_FAIL_CLOSED", rc, out.String())
	}
	after, err := os.ReadFile(path)
	if err != nil || sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("AA009_CHECK_AUTOMIGRATED", err)
	}
}
