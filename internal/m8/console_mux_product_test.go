package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/consolemux"
	"github.com/zhangzongchu2019/dingwei/internal/fileservice"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/identity/identitytest"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This runs the actual Go handlers, primary PG, Python VS and production renderer
// over HTTP/1.1. The test-only publisher represents incoming Agent events, not auth.
func TestMuxProductHTTP1EightSeats(t *testing.T) {
	module := os.Getenv("MUX_PUPPETEER")
	if module == "" {
		t.Skip("set MUX_PUPPETEER to installed puppeteer-core for actual Chrome product integration")
	}
	ctx := context.Background()
	root := t.TempDir()
	h, _, _ := newTestHub(t)
	dsn := pgtest.DSN(t)
	admin, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	access, err := repo.NewAccessCtx("test", "mux-product")
	if err != nil {
		t.Fatal(err)
	}
	suffix, _ := muxID()
	schema := "mux_product_" + suffix
	if _, err := admin.Exec(ctx, access, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, access, "DROP SCHEMA "+schema+" CASCADE")
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	pool, err := repo.OpenPG(ctx, dsn+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, ddl := range append(identitytest.PGTestSchema(), consolemux.PGSchemaSQL) {
		if _, err := pool.Exec(ctx, access, ddl); err != nil {
			t.Fatal(err)
		}
	}
	store := identity.NewPGStore(pool)
	if err := store.CreateUser(ctx, identity.User{UserID: "owner", ShortName: "owner", Type: identity.UserMember, State: identity.UserActive, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	token, _ := muxID()
	hash := identity.HashSecret(token)
	if err := store.CreateSession(ctx, identity.Session{TokenHash: hash, UserIDLC: "owner", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	credentials := map[string]string{}
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("agent%02d", i)
		if _, err := pool.Exec(ctx, access, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES($1,'owner',$2,$2,'selfhost','active',clock_timestamp())`, name, name); err != nil {
			t.Fatal(err)
		}
		if i == 7 {
			h.mu.Lock()
			st := h.terminalForSession(AgentKeyID("owner"), name)
			st.eventSeq = 9007199254740992
			h.mu.Unlock()
		}
		cred, _ := muxID()
		credentials[name] = cred
		if _, err := pool.Exec(ctx, access, "UPDATE np_agent SET credential_hash=$1 WHERE id=$2", identity.HashSecret(cred), name); err != nil {
			t.Fatal(err)
		}
		h.publishSessionEvent(AgentKeyID("owner"), name, `{"kind":"assistant_text","text":"product-initial-`+name+`","data":{}}`)
	}
	h.Identity = identity.NewService(store, nil, nil)
	h.Onboarding = identity.NewOnboarding(h.Identity)
	h.AgentCredentialOnly = true
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	h.PublicOrigin = server.URL
	budget := consolemux.NewBudget()
	if err := h.ConfigureConsoleMux(store, store.ConsoleMuxSlots(budget), budget); err != nil {
		t.Fatal(err)
	}
	h.Identity.OnSessionRevoked = h.RevokeConsoleMuxSession
	mux.HandleFunc("GET /console", h.RequireConsolePrincipal(h.HandleConsolePage))
	mux.HandleFunc("GET /view/{sessionName}", h.RequireConsoleAgent("view", h.HandleTerminalViewPage))
	mux.HandleFunc("/view/{sessionName}/{rest...}", h.RequireConsoleViewSubPath(h.HandleViewSubPath))
	mux.HandleFunc("GET /ws/agent", h.HandleAgentWS)
	mux.HandleFunc("POST /api/console/agents/{sessionName}/capability", h.RequireConsoleAgent("interact", h.HandleConsoleCapability))
	mux.HandleFunc("POST /api/console/agents/{sessionName}/file-upload-ticket", h.RequireConsoleAgent("interact", h.HandleConsoleFileUploadTicket))
	mux.HandleFunc("GET /api/console/event-context", h.HandleConsoleMuxContext)
	mux.HandleFunc("POST /api/console/event-feeds", h.HandleConsoleMuxCreate)
	mux.HandleFunc("GET /api/console/event-feeds/{feed_id}", h.HandleConsoleMuxMetadata)
	mux.HandleFunc("PUT /api/console/event-feeds/{feed_id}", h.HandleConsoleMuxReplace)
	mux.HandleFunc("DELETE /api/console/event-feeds/{feed_id}", h.HandleConsoleMuxDelete)
	mux.HandleFunc("GET /api/console/event-feeds/{feed_id}/events", h.HandleConsoleMuxEvents)
	mux.HandleFunc("POST /internal/console/event-feeds/{feed_id}/bridges/{bridge_id}/events", h.HandleInternalConsoleMuxEvents)
	mux.HandleFunc("GET /internal/view/{session}/events", h.HandleInternalViewEvents)
	mux.HandleFunc("POST /internal/view-v2/{session}/input", h.HandleInternalViewV2Input)
	mux.HandleFunc("POST /internal/view/authorize", h.HandleInternalViewAuthorize)
	api := &httpapi.API{Svc: h.Identity, Claims: h.Onboarding, Actor: (&httpapi.N3{OB: h.Onboarding}).Actor, PublicOrigin: server.URL, Online: h.AgentOnline}
	api.Register(mux)
	mux.HandleFunc("POST /test-publish/{name}", func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, muxFrameMax))
		if err != nil || !json.Valid(b) {
			http.Error(w, "bad test event", 400)
			return
		}
		h.publishSessionEvent(AgentKeyID("owner"), r.PathValue("name"), string(b))
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /test-disconnect", func(w http.ResponseWriter, r *http.Request) {
		m := h.consoleMux
		m.mu.Lock()
		var f *consoleMuxFeed
		for _, v := range m.feeds {
			if v.Session == hash {
				f = v
				break
			}
		}
		m.mu.Unlock()
		if f != nil {
			f.mu.Lock()
			cancel := f.Cancel
			f.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /test-db-revoke", func(w http.ResponseWriter, r *http.Request) {
		if err := store.RevokeSession(r.Context(), hash, time.Now()); err != nil {
			http.Error(w, "DB error", 500)
			return
		}
		w.WriteHeader(204)
	})
	// Two real VS targets with independent signing keys and persistent projections.
	routing := ViewRouting{Default: "target-a", Sessions: map[string]string{}, Targets: map[string]ViewTarget{}}
	for index, name := range []string{"target-a", "target-b"} {
		forwardKey, _ := muxID()
		forwardKey += forwardKey
		reverseKey, _ := muxID()
		reverseKey += reverseKey
		forward := filepath.Join(root, name+"-forward")
		reverse := filepath.Join(root, name+"-reverse")
		os.WriteFile(forward, []byte(forwardKey), 0600)
		os.WriteFile(reverse, []byte(reverseKey), 0600)
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		listener.Close()
		vsURL := fmt.Sprintf("http://127.0.0.1:%d", port)
		routing.Targets[name] = ViewTarget{URL: vsURL, ViewUnlockV2: true, HubToVSSecretFile: forward, VSToHubSecretFile: reverse}
		if index == 1 {
			for i := 4; i < 8; i++ {
				routing.Sessions[fmt.Sprintf("agent%02d.owner", i)] = name
			}
		}
		python := exec.Command("python3", "view_service.py")
		python.Dir = "../../view-service"
		python.Env = append(os.Environ(), "VS_PORT="+fmt.Sprint(port), "VS_HUB_INTERNAL="+server.URL, "VS_VIEW_UNLOCK_V2=1", "VS_TARGET_NAME="+name, "VS_HUB_TO_VS_SECRET_FILE="+forward, "VS_TO_HUB_SECRET_FILE="+reverse, "VS_PAGE_DB="+filepath.Join(root, name+"-pages.db"), "VS_STATE_FILE="+filepath.Join(root, name+"-state.json"))
		python.Stdout = io.Discard
		python.Stderr = os.Stderr
		if err := python.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = python.Process.Kill(); _ = python.Wait() }()
		ready := false
		for i := 0; i < 100; i++ {
			resp, e := http.Get(vsURL + "/tasks")
			if e == nil {
				resp.Body.Close()
				ready = true
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !ready {
			t.Fatal("VS failed to start")
		}
	}
	encoded, _ := json.Marshal(routing)
	config := filepath.Join(root, "routing.json")
	os.WriteFile(config, encoded, 0600)
	t.Setenv("VIEW_ROUTING_CONFIG", config)
	resetViewRoutingCacheForTest()
	// Real authenticated Agent WS registrations, with a reader representing helper transport.
	for _, cred := range credentials {
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/agent?tool=test&version=0.3.0", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + cred}}})
		if err != nil {
			t.Fatal(err)
		}
		reader(conn)
		defer conn.CloseNow()
	}
	fileRepo, err := fileservice.OpenRepository(filepath.Join(root, "files.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer fileRepo.Close()
	objects, err := fileservice.NewLocalObjectStore(filepath.Join(root, "objects"))
	if err != nil {
		t.Fatal(err)
	}
	clock := fileservice.RealClock{}
	tickets := fileservice.NewUploadTicketStore(clock, time.Minute)
	fs := &fileservice.Service{Manager: &fileservice.Manager{Repo: fileRepo, Objects: objects, Clock: clock, MaxFileBytes: 1 << 20, QuotaBytes: 16 << 20, Retention: 7 * 24 * time.Hour, ObjectPrefix: "files"}, UploadTickets: tickets, IssuerToken: token, PublicBaseURL: server.URL}
	fileInternal := httptest.NewServer(fs.InternalHandler())
	defer fileInternal.Close()
	issuer, err := NewHTTPFileTicketIssuer(fileInternal.URL, server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	h.fileIssuer = issuer
	mux.Handle("POST /files/upload", fileservice.HTTPHandler{Manager: fs.Manager, Uploads: tickets, PublicBaseURL: server.URL})
	chrome := exec.Command("node", "testdata/console_mux_product.cjs")
	chrome.Env = append(os.Environ(), "MUX_BASE="+server.URL, "MUX_TEST_SESSION="+token, "MUX_PUPPETEER="+module)
	out, err := chrome.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal("actual Chrome product integration", err)
	}
}
