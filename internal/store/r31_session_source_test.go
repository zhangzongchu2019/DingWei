package store

import (
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"testing"
)

func TestR31PrivateSessionSourceClearsHistoricalIPAndPreservesLegacy(t *testing.T) {
	db, ctx := newTestSQLite(t)
	for _, key := range []string{"u_owner", "FB-legacy"} {
		if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: key, SessionName: "agent", ClientIP: "203.0.113.77", Active: true}); err != nil {
			t.Fatal(err)
		}
	}
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range endpoints {
		if ep.KeyID == "u_owner" && ep.ClientIP != "" {
			t.Fatal("historical credential source visible before reconnect")
		}
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "u_owner", SessionName: "agent", PrivateClientIP: true, ClientIP: "198.51.100.99", Active: true}); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.db.QueryRowContext(ctx, `SELECT client_ip FROM session_endpoint WHERE key_id='u_owner'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != "" {
		t.Fatal("credential reconnect retained historical raw IP")
	}
	for _, active := range []bool{true, false} {
		if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-legacy", SessionName: "agent", Active: active}); err != nil {
			t.Fatal(err)
		}
		if err := db.db.QueryRowContext(ctx, `SELECT client_ip FROM session_endpoint WHERE key_id='FB-legacy'`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if raw != "203.0.113.77" {
			t.Fatal("legacy reconnect/offline lost retained IP")
		}
	}
}
