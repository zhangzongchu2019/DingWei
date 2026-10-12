package m8

import (
	"bytes"
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
	"golang.org/x/crypto/bcrypt"
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
	evidence := &muxOutputEvidence{}
	var vsPIDs []int
	var seedState, seedKey string
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
	resetMode := os.Getenv("MUX_INVALIDATION") == "reset"
	passwordHash := ""
	if resetMode {
		encoded, err := bcrypt.GenerateFromPassword([]byte("Local-Test-Initial-0042"), bcrypt.DefaultCost)
		if err != nil {
			t.Fatal(err)
		}
		passwordHash = string(encoded)
	}
	if err := store.CreateUser(ctx, identity.User{UserID: "owner", ShortName: "owner", Type: identity.UserMember, State: identity.UserActive, PasswordHash: passwordHash, CreatedAt: time.Now()}); err != nil {
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
	mux.HandleFunc("PUT /api/console/event-feeds/{feed_id}", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var req consoleMuxControl
		json.Unmarshal(raw, &req)
		before := map[string]string{}
		h.consoleMux.mu.Lock()
		f := h.consoleMux.feeds[r.PathValue("feed_id")]
		h.consoleMux.mu.Unlock()
		if f != nil {
			f.mu.Lock()
			for k, sc := range f.Scopes {
				before[k] = sc.Generation
			}
			f.mu.Unlock()
		}
		tap := &muxControlStatusTap{ResponseWriter: w}
		h.HandleConsoleMuxReplace(tap, r)
		if tap.status == 400 {
			t.Logf("CONTROL400 applied=%+v active_generations=%v", req.Applied, before)
		}
	})
	mux.HandleFunc("DELETE /api/console/event-feeds/{feed_id}", h.HandleConsoleMuxDelete)
	mux.HandleFunc("GET /api/console/event-feeds/{feed_id}/events", func(w http.ResponseWriter, r *http.Request) {
		h.HandleConsoleMuxEvents(&muxOutputTap{ResponseWriter: w, e: evidence}, r)
	})
	mux.HandleFunc("POST /internal/console/cost-seed-authority", h.HandleConsoleMuxCostSeedAuthority)
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
			if v.Session == hash && v.ID == r.URL.Query().Get("feed_id") {
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
				w.WriteHeader(204)
				return
			}
		}
		http.Error(w, "requested feed is not active", 409)
	})
	mux.HandleFunc("POST /test-db-revoke", func(w http.ResponseWriter, r *http.Request) {
		commitLowerBound := time.Now()
		var invalidationErr error
		if resetMode {
			resetOB := identity.NewOnboarding(identity.NewService(&muxResetStore{Store: store, evidence: evidence}, nil, nil))
			// Keep publishing throughout password work and the invalidating transaction.
			resetDone := make(chan struct{})
			go func() {
				defer close(resetDone)
				until := time.Now().Add(9 * time.Second)
				for time.Now().Before(until) {
					h.publishSessionEvent(AgentKeyID("owner"), "agent04", `{"kind":"assistant_text","text":"reset-continuous-source-probe"}`)
					time.Sleep(10 * time.Millisecond)
				}
			}()
			invalidationErr = exerciseMuxResetChain(r.Context(), resetOB, store, token)
			<-resetDone
		} else {
			invalidationErr = store.RevokeSession(r.Context(), hash, time.Now())
		}
		if invalidationErr != nil {
			t.Errorf("authoritative invalidation chain: %v", invalidationErr)
			http.Error(w, "DB error", 500)
			return
		}
		evidence.mu.Lock()
		if !resetMode {
			evidence.commit = commitLowerBound
		}
		evidence.probeDone = make(chan struct{})
		done := evidence.probeDone
		evidence.mu.Unlock()
		go func() {
			defer close(done)
			end := time.Now().Add(2500 * time.Millisecond)
			for time.Now().Before(end) {
				h.publishSessionEvent(AgentKeyID("owner"), "agent04", `{"kind":"assistant_text","text":"post-authoritative-commit-probe"}`)
				evidence.mu.Lock()
				evidence.probes++
				evidence.mu.Unlock()
				time.Sleep(10 * time.Millisecond)
			}
		}()
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /test-reset-history", func(w http.ResponseWriter, r *http.Request) {
		m := h.consoleMux
		m.mu.Lock()
		f := m.feeds[r.URL.Query().Get("feed_id")]
		m.mu.Unlock()
		if f == nil {
			http.Error(w, "missing", 404)
			return
		}
		f.mu.Lock()
		cancel, done := f.Cancel, f.Done
		f.mu.Unlock()
		if cancel == nil {
			http.Error(w, "inactive", 409)
			return
		}
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			http.Error(w, "disconnect not quiescent", 503)
			return
		}
		for i := 0; i < 1002; i++ {
			h.publishSessionEvent(AgentKeyID("owner"), "agent05", fmt.Sprintf(`{"kind":"assistant_text","text":"history-cut-%d","turn_id":"history-cut"}`, i))
		}
		f.mu.Lock()
		agent := f.Scopes["agent05.owner"].Agent
		f.mu.Unlock()
		snap, _ := h.consoleMuxSnapshot(agent)
		muxJSON(w, 200, map[string]string{"epoch": snap.Epoch, "H": muxSeq(snap.H)})
	})
	mux.HandleFunc("POST /test-maximum/{name}", func(w http.ResponseWriter, r *http.Request) { handleMuxMaximumFrame(h, w, r) })
	mux.HandleFunc("POST /test-ring-resource-load", func(w http.ResponseWriter, r *http.Request) {
		evidence.mu.Lock()
		pids := append([]int(nil), vsPIDs...)
		evidence.mu.Unlock()
		json.NewEncoder(w).Encode(retainedRingResourceLoad(h, pids))
	})
	mux.HandleFunc("GET /test-resources", func(w http.ResponseWriter, r *http.Request) {
		evidence.mu.Lock()
		pids := append([]int(nil), vsPIDs...)
		evidence.mu.Unlock()
		json.NewEncoder(w).Encode(muxResourceSample(h, pids))
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
		if os.Getenv("MUX_SEED") == "1" && index == 1 {
			seedState = filepath.Join(root, name+"-state.json")
			seedKey = reverse
			os.WriteFile(seedState, []byte(`{"old-agent04":{"cost_total":12.5,"cost_last":2.5,"turns":7}}`), 0600)
			h.publishSessionEvent(AgentKeyID("owner"), "agent04", `{"kind":"state_change","data":{"change":"turn_completed","cost_usd":10,"num_turns":6}}`)
			h.publishSessionEvent(AgentKeyID("owner"), "agent04", `{"kind":"state_change","data":{"change":"turn_completed","cost_usd":12.5}}`)
		}
		python := exec.Command("python3", "view_service.py")
		python.Dir = "../../view-service"
		python.Env = append(os.Environ(), "VS_PORT="+fmt.Sprint(port), "VS_HUB_INTERNAL="+server.URL, "VS_VIEW_UNLOCK_V2=1", "VS_TARGET_NAME="+name, "VS_HUB_TO_VS_SECRET_FILE="+forward, "VS_TO_HUB_SECRET_FILE="+reverse, "VS_PAGE_DB="+filepath.Join(root, name+"-pages.db"), "VS_STATE_FILE="+filepath.Join(root, name+"-state.json"))
		python.Stdout = io.Discard
		python.Stderr = os.Stderr
		if err := python.Start(); err != nil {
			t.Fatal(err)
		}
		evidence.mu.Lock()
		vsPIDs = append(vsPIDs, python.Process.Pid)
		evidence.mu.Unlock()
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
	if os.Getenv("MUX_SEED") == "1" {
		exerciseCostSeedProduct(t, h, server.URL, token, seedState, seedKey)
	}
	if os.Getenv("MUX_R4") == "1" {
		exerciseR4Control(t, h, store, server.URL)
	}
	chrome := exec.Command("node", "testdata/console_mux_product.cjs")
	chrome.Env = append(os.Environ(), "MUX_BASE="+server.URL, "MUX_TEST_SESSION="+token, "MUX_PUPPETEER="+module)
	out, err := chrome.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal("actual Chrome product integration", err)
	}
	evidence.check(t)
	if resetMode {
		t.Log("RESET_REAL_CHAIN PG transaction/Onboarding reset: both old sessions denied; old password denied; temporary login must-change and mux denied; change-password restores fresh authority; old cookies remain denied; no reset callback installed")
	}
}

type muxControlStatusTap struct {
	http.ResponseWriter
	status int
}

func (w *muxControlStatusTap) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
