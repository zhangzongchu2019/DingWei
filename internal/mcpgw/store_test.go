package mcpgw

import (
	"context"
	"database/sql"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
)

func TestStoreEncryptedRestartAndSingleConsumption(t *testing.T) {
	ctx := context.Background()
	key := randomKey(t)
	db, s := testDBKey(t, key)
	now := time.Now()
	payload := []byte(`{"Token":"private-local-token"}`)
	if e := s.Put(ctx, "code", hash("raw-code"), payload, now.Add(time.Minute), now); e != nil {
		t.Fatal(e)
	}
	var sealed []byte
	var storedHash string
	if e := storeRow(t, db, `SELECT key_hash,sealed FROM mcpgw_oauth`).Scan(&storedHash, &sealed); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(sealed), "private-local-token") || storedHash == "raw-code" {
		t.Fatal("plaintext stored")
	}
	// 新连接与新实例只持有原持久密钥，不依赖旧 Go 对象。
	var path string
	if e := storeRow(t, db, `SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); e != nil {
		t.Fatal(e)
	}
	db2, e := repo.OpenSQLite(ctx, path+"?_pragma=busy_timeout(10000)")
	if e != nil {
		t.Fatal(e)
	}
	defer db2.Close()
	s2, e := NewSQLStore(ctx, db2, testAccess(t), testOrigin+"/mcp", key)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := s2.Take(ctx, "code", hash("raw-code"), now)
	if e != nil || string(raw) != string(payload) {
		t.Fatal("restart failed to consume")
	}
	if _, e = s.Take(ctx, "code", hash("raw-code"), now); e == nil {
		t.Fatal("replay across connections accepted")
	}
	if e = s.Put(ctx, "code", hash("expired"), payload, now.Add(time.Minute), now); e != nil {
		t.Fatal(e)
	}
	if _, e = s2.Take(ctx, "code", hash("expired"), now.Add(time.Minute)); e == nil {
		t.Fatal("exact expiry accepted")
	}
}
func TestStoreCiphertextBindingAndWrongKey(t *testing.T) {
	ctx := context.Background()
	db, s := testDB(t)
	now := time.Now()
	if e := s.Put(ctx, "attempt", hash("one"), []byte("payload"), now.Add(time.Minute), now); e != nil {
		t.Fatal(e)
	}
	wrong, e := NewSQLStore(ctx, db, testAccess(t), testOrigin+"/mcp", randomKey(t))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = wrong.Get(ctx, "attempt", hash("one"), now); e == nil {
		t.Fatal("wrong key decrypted")
	}
	if _, e = db.Exec(ctx, testAccess(t), `UPDATE mcpgw_oauth SET key_hash=$1 WHERE key_hash=$2`, hash("two"), hash("one")); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Get(ctx, "attempt", hash("two"), now); e == nil {
		t.Fatal("ciphertext moved to another key")
	}
}
func TestDurableCStateAdapter(t *testing.T) {
	ctx := context.Background()
	_, s := testDB(t)
	now := time.Now()
	a := feishulogin.LoginAttempt{StateHash: hash("csrf"), AppID: "app", Purpose: feishulogin.StatePurposeConnectForm, RedirectURI: testOrigin + "/mcp/oauth/feishu/callback", CreatedAt: now, ExpiresAt: now.Add(time.Minute), Request: feishulogin.IssueRequest{AttemptID: "bound"}}
	adapter := LoginStates{Store: s}
	if e := adapter.Create(ctx, a); e != nil {
		t.Fatal(e)
	}
	out, e := adapter.Consume(ctx, a.StateHash, now)
	if e != nil || out.Request.AttemptID != "bound" || out.Purpose != a.Purpose {
		t.Fatal("C state lost")
	}
	if _, e = adapter.Consume(ctx, a.StateHash, now); e == nil {
		t.Fatal("C state replay")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestClientMetadataFetchAndValidation(t *testing.T) {
	good := `{"client_id":"` + clientID + `","client_name":"Example","redirect_uris":["` + clientRedirect + `"],"grant_types":["authorization_code"],"response_types":["code"],"token_endpoint_auth_method":"none"}`
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"valid", good, true},
		{"wrong_id", strings.Replace(good, clientID, "https://other.example/client.json", 1), false},
		{"missing_name", strings.Replace(good, `"client_name":"Example",`, "", 1), false},
		{"fragment", strings.Replace(good, clientRedirect, clientRedirect+"#fragment", 1), false},
		{"nonloopback_http", strings.Replace(good, clientRedirect, "http://evil.example/callback", 1), false},
		{"confidential", strings.Replace(good, `"none"`, `"client_secret_post"`, 1), false},
		{"refresh", strings.Replace(good, `"authorization_code"`, `"refresh_token"`, 1), false},
		{"oversized", strings.Repeat(" ", 65537), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "" || r.URL.String() != clientID {
					t.Fatal("wrong request boundary")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}
			_, e := fetchClient(context.Background(), clientID, client)
			if (e == nil) != tc.ok {
				t.Fatalf("CIMD validation err=%v", e)
			}
		})
	}
}
func TestCIMDRejectsPrivateDestinations(t *testing.T) {
	for _, raw := range []string{"http://example.com/client.json", "https://example.com", "https://user:pass@example.com/client.json", "https://example.com/client.json#x", "https://127.0.0.1/client.json", "https://[::1]/client.json", "https://169.254.169.254/client.json"} {
		if _, e := (MetadataClients{}).Resolve(context.Background(), raw); e == nil {
			t.Fatalf("unsafe CIMD URL allowed %s", raw)
		}
	}
	for _, raw := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.64.1.1", "192.0.2.1", "198.18.1.1", "240.1.1.1", "::1", "fd00::1", "fe80::1", "2001:db8::1", "2002:7f00:1::"} {
		if publicIP(net.ParseIP(raw)) {
			t.Fatalf("special IP public: %s", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if !publicIP(net.ParseIP(raw)) {
			t.Fatalf("public IP rejected %s", raw)
		}
	}
}

func storeRow(t *testing.T, p *repo.Pool, q string, args ...any) *sql.Row {
	t.Helper()
	r, e := p.QueryRow(context.Background(), testAccess(t), q, args...)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
