package m8

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/consolemux"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

// A local feed registry never pretends to be the global transport authority.
// Slots are shared PG. Multi-Hub routing must keep a feed's control requests on
// its creating authority, or return a visible retry instead of a second writer.
type consoleMuxRegistry struct {
	mu         sync.Mutex
	h          *Hub
	authorizer consolemux.Authorizer
	slots      consolemux.Slots
	budget     *consolemux.Budget
	origin     string
	secret     [32]byte
	feeds      map[string]*consoleMuxFeed
}
type consoleMuxDesired struct {
	FullName string `json:"full_name"`
	Instance string `json:"iframe_instance"`
	After    string `json:"after,omitempty"`
}
type consoleMuxScope struct {
	Desired       consoleMuxDesired
	Agent         consolemux.Agent
	Generation    string
	Round         uint64
	TargetName    string
	Target        ViewTarget
	ResetBaseline string
}
type consoleMuxControl struct {
	RequestID      string              `json:"request_id"`
	AuthContext    string              `json:"auth_context"`
	Instance       string              `json:"console_instance,omitempty"`
	Revision       uint64              `json:"expected_revision,omitempty"`
	Subscriptions  []consoleMuxDesired `json:"subscriptions"`
	Refill         []string            `json:"refill,omitempty"`
	Applied        []consoleMuxApplied `json:"applied,omitempty"`
	AckOnly        bool                `json:"ack_only,omitempty"`
	ResetRequestID string              `json:"reset_request_id,omitempty"`
	Reset          *consoleMuxReset    `json:"reset,omitempty"`
}
type consoleMuxReset struct {
	FullName   string `json:"full_name"`
	Instance   string `json:"iframe_instance"`
	Generation string `json:"generation"`
	Round      string `json:"replay_round"`
}
type consoleMuxApplied struct {
	FullName   string `json:"full_name"`
	Generation string `json:"generation"`
	After      string `json:"after"`
}
type consoleMuxResult struct {
	Revision uint64
	Hash     [32]byte
	Status   int
	Body     any
}
type consoleMuxFeed struct {
	mu                               sync.Mutex
	control                          chan struct{}
	ID, Session, Principal, Instance string
	Revision                         uint64
	ResetRevision                    uint64
	Scopes                           map[string]*consoleMuxScope
	Retired                          map[string]*consoleMuxScope
	RetiredOrder                     []string
	Requests                         map[string]consoleMuxResult
	RequestOrder                     []string
	Owner                            consolemux.Owner
	Gate                             *consolemux.Gate
	Cancel                           context.CancelFunc
	Done                             chan struct{}
	RecoverUntil                     time.Time
	Closed                           bool
	Changed                          chan struct{}
	// Queue, bridge workers, and credit state belong to one active transport only.
	Stream      *consoleMuxStream
	Bridges     map[string]*consoleMuxBridge
	Scratch     chan struct{}
	expiryTimer *time.Timer
}

func (h *Hub) ConfigureConsoleMux(a consolemux.Authorizer, slots consolemux.Slots, b *consolemux.Budget) error {
	if a == nil || slots == nil || b == nil {
		return errors.New("mux authority required")
	}
	u, err := url.Parse(h.PublicOrigin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("invalid mux public origin")
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	origin := strings.ToLower(u.Scheme) + "://" + host
	if u.Path != "" && u.Path != "/" {
		return errors.New("invalid mux origin path")
	}
	reg := &consoleMuxRegistry{h: h, authorizer: a, slots: slots, budget: b, origin: origin, feeds: map[string]*consoleMuxFeed{}}
	if _, err := rand.Read(reg.secret[:]); err != nil {
		return err
	}
	h.consoleMux = reg
	return nil
}
func muxID() (string, error) {
	var b [16]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}
func muxSeq(n uint64) string   { return strconv.FormatUint(n, 10) }
func muxValidID(s string) bool { return viewV2NoncePattern.MatchString(s) }
func (m *consoleMuxRegistry) authContext(session string) string {
	mac := hmac.New(sha256.New, m.secret[:])
	mac.Write([]byte(m.origin))
	mac.Write([]byte{0})
	mac.Write([]byte(session))
	return hex.EncodeToString(mac.Sum(nil))
}
func (m *consoleMuxRegistry) authorize(ctx context.Context, hash string, names []string) (consolemux.Authorization, time.Time, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var out consolemux.Authorization
	err := m.budget.Do(ctx, func(ctx context.Context) error {
		var err error
		out, err = m.authorizer.AuthorizeConsoleMux(ctx, hash, names)
		return err
	})
	if err == nil && !time.Now().Before(start.Add(time.Second)) {
		err = consolemux.ErrDenied
	}
	return out, start, err
}
func (m *consoleMuxRegistry) actor(w http.ResponseWriter, r *http.Request, names []string) (consolemux.Authorization, string, time.Time, bool) {
	c, err := r.Cookie(httpapi.SessionCookie)
	if err != nil || c.Value == "" {
		writeAgentWSError(w, 401, "unauthenticated")
		return consolemux.Authorization{}, "", time.Time{}, false
	}
	if (r.Method != "GET" && r.Method != "HEAD" || r.Header.Get("Origin") != "") && r.Header.Get("Origin") != m.h.PublicOrigin {
		writeAgentWSError(w, 403, "origin_mismatch")
		return consolemux.Authorization{}, "", time.Time{}, false
	}
	hash := identity.HashSecret(c.Value)
	auth, start, err := m.authorize(r.Context(), hash, names)
	if err != nil {
		code := 503
		if errors.Is(err, consolemux.ErrDenied) {
			code = 401
		}
		writeAgentWSError(w, code, "authorization_unavailable")
		return auth, "", start, false
	}
	return auth, hash, start, true
}
func muxJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
func (h *Hub) HandleConsoleMuxContext(w http.ResponseWriter, r *http.Request) {
	m := h.consoleMux
	if m == nil {
		http.NotFound(w, r)
		return
	}
	_, hash, _, ok := m.actor(w, r, nil)
	if !ok {
		return
	}
	muxJSON(w, 200, map[string]any{"protocol_version": 1, "auth_context": m.authContext(hash)})
}
func muxDecode(w http.ResponseWriter, r *http.Request) (consoleMuxControl, []string, [32]byte, error) {
	var req consoleMuxControl
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	if err != nil {
		return req, nil, [32]byte{}, err
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, nil, [32]byte{}, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return req, nil, [32]byte{}, errors.New("trailing JSON")
	}
	if !muxValidID(req.RequestID) || len(req.Subscriptions) > 8 || len(req.Applied) > 8 || len(req.Refill) > 8 {
		return req, nil, [32]byte{}, errors.New("invalid control")
	}
	if (req.Reset == nil) != (req.ResetRequestID == "") || (req.Reset != nil && (!muxValidID(req.ResetRequestID) || req.AckOnly || !muxValidID(req.Reset.Generation) || !muxValidID(req.Reset.Instance))) {
		return req, nil, [32]byte{}, errors.New("invalid_reset")
	}
	seen := map[string]bool{}
	names := make([]string, 0, len(req.Subscriptions))
	for i := range req.Subscriptions {
		d := &req.Subscriptions[i]
		d.FullName = strings.ToLower(strings.TrimSpace(d.FullName))
		dot := strings.LastIndex(d.FullName, ".")
		if dot < 1 || !identity.ValidAgentName(d.FullName[:dot]) || !identity.ValidShortName(d.FullName[dot+1:]) || !muxValidID(d.Instance) || seen[d.FullName] {
			return req, nil, [32]byte{}, errors.New("invalid subscription")
		}
		if d.After != "" {
			ep, seq, valid := parseSSEAfter(d.After)
			if !valid || ep+"-"+muxSeq(seq) != d.After {
				return req, nil, [32]byte{}, errMuxInvalidCursor
			}
		}
		seen[d.FullName] = true
		names = append(names, d.FullName)
	}
	sort.Strings(names)
	return req, names, sha256.Sum256(body), nil
}
func (m *consoleMuxRegistry) feed(w http.ResponseWriter, r *http.Request, hash, principal string) *consoleMuxFeed {
	m.mu.Lock()
	f := m.feeds[r.PathValue("feed_id")]
	m.mu.Unlock()
	if f != nil {
		f.mu.Lock()
		expired := f.Cancel == nil && !time.Now().Before(f.RecoverUntil)
		f.mu.Unlock()
		if expired {
			m.mu.Lock()
			if m.feeds[f.ID] == f {
				delete(m.feeds, f.ID)
			}
			m.mu.Unlock()
			f = nil
		}
	}
	if f == nil || f.Session != hash || f.Principal != principal {
		writeAgentWSError(w, 404, "not_found")
		return nil
	}
	return f
}
func (f *consoleMuxFeed) metadata() any {
	f.mu.Lock()
	defer f.mu.Unlock()
	subs := make([]map[string]any, 0, len(f.Scopes))
	for _, s := range f.Scopes {
		subs = append(subs, map[string]any{"full_name": s.Agent.FullName, "agent_id": s.Agent.ID, "iframe_instance": s.Desired.Instance, "generation": s.Generation, "after": s.Desired.After, "replay_round": muxSeq(s.Round), "reset_baseline": s.ResetBaseline})
	}
	sort.Slice(subs, func(i, j int) bool { return subs[i]["full_name"].(string) < subs[j]["full_name"].(string) })
	return map[string]any{"feed_id": f.ID, "revision": f.Revision, "protocol_version": 1, "subscriptions": subs, "closed": f.Closed, "streaming": f.Cancel != nil}
}
func (h *Hub) HandleConsoleMuxMetadata(w http.ResponseWriter, r *http.Request) {
	m := h.consoleMux
	if m == nil {
		http.NotFound(w, r)
		return
	}
	a, hash, _, ok := m.actor(w, r, nil)
	if !ok {
		return
	}
	f := m.feed(w, r, hash, a.Principal)
	if f != nil {
		muxJSON(w, 200, f.metadata())
	}
}
func (m *consoleMuxRegistry) resolveScopes(auth consolemux.Authorization, req consoleMuxControl, old map[string]*consoleMuxScope) (map[string]*consoleMuxScope, int, error) {
	next := map[string]*consoleMuxScope{}
	if req.Reset != nil {
		found := false
		for _, d := range req.Subscriptions {
			if d.FullName == req.Reset.FullName {
				found = true
			}
		}
		if !found {
			return nil, 400, errors.New("invalid_reset_scope")
		}
	}
	refill := map[string]bool{}
	for _, name := range req.Refill {
		refill[name] = true
	}
	for _, d := range req.Subscriptions {
		a, ok := auth.Agents[d.FullName]
		if !ok {
			return nil, 404, errors.New("not_found")
		}
		name, target := loadViewRouting().resolve(a.FullName, a.Owner)
		if target.Builtin || !target.ViewUnlockV2 || target.URL == "" || target.HubToVSSecretFile == "" || target.VSToHubSecretFile == "" {
			return nil, 503, errors.New("aggregate_target_unavailable")
		}
		snap, ok := m.h.consoleMuxSnapshot(a)
		if !ok {
			return nil, 404, errors.New("agent_offline")
		}
		prior := old[d.FullName]
		resetting := req.Reset != nil && req.Reset.FullName == d.FullName
		baseline := ""
		if resetting {
			reset := req.Reset
			if prior == nil || prior.Agent.ID != a.ID || prior.Desired.Instance != reset.Instance || prior.Generation != reset.Generation || muxSeq(prior.Round) != reset.Round {
				return nil, 409, &muxScopeControlError{d.FullName, errors.New("stale_reset_scope")}
			}
			if snap.H >= ^uint64(0)-1 {
				return nil, 409, errors.New("seq_exhausted")
			}
			after := snap.H
			if snap.Floor > 0 {
				after = snap.Floor - 1
			}
			d.After = snap.Epoch + "-" + muxSeq(after)
			baseline = d.After
		} else if prior != nil && prior.Agent.ID == a.ID && (prior.Desired.After != "" || prior.Desired.Instance != d.Instance) {
			oldEp, oldN, _ := parseSSEAfter(prior.Desired.After)
			ep, n, valid := parseSSEAfter(d.After)
			if !valid || ep != oldEp || n < oldN {
				return nil, 409, &muxScopeControlError{d.FullName, errors.New("resync_required")}
			}
		}
		if _, _, err := selectMuxReplay(snap, d.After, d.After != ""); err != nil {
			code := 400
			if errors.Is(err, errMuxCursorAhead) {
				code = 409
			}
			return nil, code, &muxScopeControlError{d.FullName, err}
		}
		if !resetting && prior != nil && prior.Agent.ID == a.ID && prior.Desired.Instance == d.Instance {
			copy := *prior
			copy.Desired = d
			if refill[d.FullName] {
				copy.Round++
			}
			next[d.FullName] = &copy
		} else {
			generation, err := muxID()
			if err != nil {
				return nil, 503, err
			}
			round := uint64(1)
			if resetting {
				round = prior.Round + 1
				if round == 0 {
					return nil, 409, errors.New("round_exhausted")
				}
			}
			next[d.FullName] = &consoleMuxScope{Desired: d, Agent: a, Generation: generation, Round: round, TargetName: name, Target: target, ResetBaseline: baseline}
		}
	}
	return next, 200, nil
}
func (h *Hub) consoleMuxSnapshot(a consolemux.Agent) (sessionSeqSnapshot, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.terminals[terminalKey(AgentKeyID(a.Owner), a.Session)]
	if st == nil {
		return sessionSeqSnapshot{}, false
	}
	st.ensureEventEpochLocked()
	h.adoptRingLocked(st)
	s := sessionSeqSnapshot{LostThrough: st.eventRingLostThrough, HistoryTruncated: st.eventRingTruncated, Epoch: st.eventEpoch, H: st.eventSeq, Replay: st.eventRingSnapshotLocked(), Empty: len(st.eventRing) == 0}
	for _, e := range s.Replay {
		if e.Seq > 0 {
			s.Floor = e.Seq
			break
		}
	}
	return s, true
}
func (h *Hub) HandleConsoleMuxCreate(w http.ResponseWriter, r *http.Request) {
	m := h.consoleMux
	if m == nil {
		http.NotFound(w, r)
		return
	}
	req, names, digest, err := muxDecode(w, r)
	if err != nil {
		writeAgentWSError(w, 400, "invalid_control")
		return
	}
	if !muxValidID(req.Instance) || req.Reset != nil {
		writeAgentWSError(w, 400, "invalid_instance")
		return
	}
	auth, hash, _, ok := m.actor(w, r, names)
	if !ok {
		return
	}
	if !hmac.Equal([]byte(req.AuthContext), []byte(m.authContext(hash))) {
		writeAgentWSError(w, 403, "auth_context_changed")
		return
	}
	scopes, code, err := m.resolveScopes(auth, req, nil)
	if err != nil {
		muxWriteScopeError(w, code, err)
		return
	}
	m.mu.Lock()
	now := time.Now()
	for id, f := range m.feeds {
		f.mu.Lock()
		expired := f.Cancel == nil && !now.Before(f.RecoverUntil)
		f.mu.Unlock()
		if expired {
			delete(m.feeds, id)
		}
	}
	for _, f := range m.feeds {
		if f.Session == hash && f.Instance == req.Instance {
			m.mu.Unlock()
			f.mu.Lock()
			prior, exists := f.Requests[req.RequestID]
			f.mu.Unlock()
			if exists && prior.Hash == digest {
				muxJSON(w, prior.Status, prior.Body)
			} else {
				writeAgentWSError(w, 409, "console_instance_exists")
			}
			return
		}
	}
	if len(m.feeds) >= 8 {
		m.mu.Unlock()
		writeAgentWSError(w, 503, "feed_capacity")
		return
	}
	id, err := muxID()
	if err != nil {
		m.mu.Unlock()
		writeAgentWSError(w, 503, "unavailable")
		return
	}
	f := &consoleMuxFeed{ID: id, Session: hash, Principal: auth.Principal, Instance: req.Instance, Revision: 1, Scopes: scopes, Requests: map[string]consoleMuxResult{}, control: make(chan struct{}, 1), Changed: make(chan struct{}, 1), RecoverUntil: now.Add(time.Minute)}
	f.control <- struct{}{}
	m.feeds[id] = f
	m.mu.Unlock()
	m.expireLater(f)
	owner, err := consolemux.NewOwner(auth.Principal, m.origin, hash, req.Instance)
	if err == nil {
		owner, err = m.slots.Acquire(r.Context(), owner)
	}
	if err != nil {
		m.mu.Lock()
		delete(m.feeds, id)
		m.mu.Unlock()
		status := 503
		if errors.Is(err, consolemux.ErrInUse) {
			status = 409
		}
		writeAgentWSError(w, status, "console_in_use")
		return
	}
	f.mu.Lock()
	if f.Closed {
		f.mu.Unlock()
		_ = m.slots.Release(r.Context(), owner)
		writeAgentWSError(w, 410, "feed_closed")
		return
	}
	f.Owner = owner
	f.mu.Unlock()
	meta := f.metadata()
	f.mu.Lock()
	f.Requests[req.RequestID] = consoleMuxResult{Hash: digest, Status: 201, Body: meta, Revision: 1}
	f.RequestOrder = []string{req.RequestID}
	f.mu.Unlock()
	muxJSON(w, 201, meta)
}
func (h *Hub) HandleConsoleMuxReplace(w http.ResponseWriter, r *http.Request) {
	m := h.consoleMux
	if m == nil {
		http.NotFound(w, r)
		return
	}
	req, names, digest, err := muxDecode(w, r)
	if err != nil {
		writeAgentWSError(w, 400, "invalid_control")
		return
	}
	auth, hash, _, ok := m.actor(w, r, names)
	if !ok {
		return
	}
	f := m.feed(w, r, hash, auth.Principal)
	if f == nil {
		return
	}
	if !hmac.Equal([]byte(req.AuthContext), []byte(m.authContext(hash))) {
		writeAgentWSError(w, 403, "auth_context_changed")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	select {
	case <-f.control:
	case <-ctx.Done():
		writeAgentWSError(w, 503, "control_timeout")
		return
	}
	defer func() { f.control <- struct{}{} }()
	f.mu.Lock()
	cacheID := req.RequestID
	if req.Reset != nil {
		cacheID = "reset:" + req.ResetRequestID
	}
	prior, exists := f.Requests[cacheID]
	rev := f.Revision
	resetRevision := f.ResetRevision
	old := map[string]*consoleMuxScope{}
	for k, v := range f.Scopes {
		copy := *v
		old[k] = &copy
	}
	closed := f.Closed
	stream := f.Stream
	retired := map[string]*consoleMuxScope{}
	for k, v := range f.Retired {
		c := *v
		retired[k] = &c
	}
	f.mu.Unlock()
	if exists {
		if prior.Hash != digest {
			writeAgentWSError(w, 409, "request_id_conflict")
		} else if prior.Revision < resetRevision {
			writeAgentWSError(w, 409, "reset_generation_changed")
		} else {
			muxJSON(w, prior.Status, prior.Body)
		}
		return
	}
	if closed {
		writeAgentWSError(w, 410, "feed_closed")
		return
	}
	if req.Revision != rev {
		writeAgentWSError(w, 409, "revision_conflict")
		return
	}
	if req.AckOnly {
		if stream == nil {
			writeAgentWSError(w, 409, "transport_missing")
			return
		}
		if err := stream.ack(req.Applied, old); err != nil {
			writeAgentWSError(w, 400, "invalid_ack")
			return
		}
		f.mu.Lock()
		for _, ack := range req.Applied {
			if sc := f.Scopes[ack.FullName]; sc != nil && sc.Generation == ack.Generation {
				ep, n, ok := parseSSEAfter(ack.After)
				oldEp, oldN, oldOK := parseSSEAfter(sc.Desired.After)
				if ok && (!oldOK || ep != oldEp || n > oldN) {
					sc.Desired.After = ack.After
				}
			}
		}
		f.mu.Unlock()
	} else {
		resolveOld := map[string]*consoleMuxScope{}
		for k, v := range retired {
			resolveOld[k] = v
		}
		for k, v := range old {
			resolveOld[k] = v
		}
		next, code, err := m.resolveScopes(auth, req, resolveOld)
		if err != nil {
			muxWriteScopeError(w, code, err)
			return
		}
		f.mu.Lock()
		if f.Revision != rev {
			f.mu.Unlock()
			writeAgentWSError(w, 409, "revision_conflict")
			return
		}
		if f.Stream != nil {
			for name, prior := range f.Scopes {
				sc := next[name]
				if sc == nil || sc.Generation != prior.Generation || sc.Round != prior.Round {
					f.Stream.resetAgent(prior.Agent.ID)
				}
			}
		}
		for name, sc := range f.Scopes {
			if next[name] == nil {
				f.retireScopeLocked(name, sc)
			}
		}

		for name := range next {
			delete(f.Retired, name)
		}
		f.Scopes = next
		f.Revision++
		if req.Reset != nil {
			f.ResetRevision = f.Revision
		}
		if f.Gate != nil {
			nextGenerations := map[string]string{}
			for _, sc := range next {
				nextGenerations[sc.Agent.ID] = sc.Generation
			}
			f.Gate.Retag(nextGenerations)
		}
		f.mu.Unlock()
		select {
		case f.Changed <- struct{}{}:
		default:
		}
	}
	meta := f.metadata()
	f.mu.Lock()
	f.Requests[cacheID] = consoleMuxResult{Hash: digest, Status: 200, Body: meta, Revision: f.Revision}
	f.RequestOrder = append(f.RequestOrder, cacheID)
	if len(f.RequestOrder) > 16 {
		id := f.RequestOrder[0]
		f.RequestOrder = f.RequestOrder[1:]
		delete(f.Requests, id)
	}
	f.mu.Unlock()
	muxJSON(w, 200, meta)
}
func (m *consoleMuxRegistry) stop(f *consoleMuxFeed, ctx context.Context) error {
	f.mu.Lock()
	f.Closed = true
	if f.expiryTimer != nil {
		f.expiryTimer.Stop()
		f.expiryTimer = nil
	}
	gate, cancel, done, owner := f.Gate, f.Cancel, f.Done, f.Owner
	f.Scopes = map[string]*consoleMuxScope{}
	f.mu.Unlock()
	if gate != nil {
		gate.Revoke("")
	}
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	err := m.slots.Release(ctx, owner)
	if err == nil {
		m.mu.Lock()
		if m.feeds[f.ID] == f {
			delete(m.feeds, f.ID)
		}
		m.mu.Unlock()
	}
	return err
}
func (h *Hub) HandleConsoleMuxDelete(w http.ResponseWriter, r *http.Request) {
	m := h.consoleMux
	if m == nil {
		http.NotFound(w, r)
		return
	}
	a, hash, _, ok := m.actor(w, r, nil)
	if !ok {
		return
	}
	f := m.feed(w, r, hash, a.Principal)
	if f == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := m.stop(f, ctx); err != nil {
		writeAgentWSError(w, 503, "close_unavailable")
		return
	}
	muxJSON(w, 200, map[string]bool{"closed": true})
}
func (h *Hub) RevokeConsoleMuxSession(ctx context.Context, hash string) error {
	m := h.consoleMux
	if m == nil {
		return nil
	}
	m.mu.Lock()
	var feeds []*consoleMuxFeed
	for _, f := range m.feeds {
		if f.Session == hash {
			feeds = append(feeds, f)
		}
	}
	m.mu.Unlock()
	var errs []error
	for _, f := range feeds {
		if err := m.stop(f, ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
func (h *Hub) revokeConsoleMuxUser(principal string) {
	m := h.consoleMux
	if m == nil {
		return
	}
	m.mu.Lock()
	var fs []*consoleMuxFeed
	for _, f := range m.feeds {
		if f.Principal == principal {
			fs = append(fs, f)
		}
	}
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, f := range fs {
		_ = m.stop(f, ctx)
	}
}
func (h *Hub) revokeConsoleMuxAgent(agentID string) {
	m := h.consoleMux
	if m == nil {
		return
	}
	m.mu.Lock()
	var fs []*consoleMuxFeed
	for _, f := range m.feeds {
		fs = append(fs, f)
	}
	m.mu.Unlock()
	for _, f := range fs {
		f.mu.Lock()
		for name, s := range f.Scopes {
			if s.Agent.ID == agentID {
				delete(f.Scopes, name)
				f.Revision++
				if f.Gate != nil {
					f.Gate.Revoke(agentID)
				}
				if f.Stream != nil {
					f.Stream.removeAgent(s)
				}
			}
		}
		f.mu.Unlock()
		select {
		case f.Changed <- struct{}{}:
		default:
		}
	}
}

// The metadata retention deadline is enforced even when no future request arrives.
func (m *consoleMuxRegistry) expireLater(f *consoleMuxFeed) {
	f.mu.Lock()
	if f.expiryTimer != nil {
		f.expiryTimer.Stop()
	}
	delay := time.Until(f.RecoverUntil)
	if delay < 0 {
		delay = 0
	}
	f.expiryTimer = time.AfterFunc(delay, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.Cancel == nil && !time.Now().Before(f.RecoverUntil) && m.feeds[f.ID] == f {
			delete(m.feeds, f.ID)
			f.Scopes = nil
			f.Requests = nil
			f.RequestOrder = nil
			f.expiryTimer = nil
		}
	})
	f.mu.Unlock()
}
func (h *Hub) closeConsoleMux() {
	m := h.consoleMux
	if m == nil {
		return
	}
	m.mu.Lock()
	var feeds []*consoleMuxFeed
	for _, f := range m.feeds {
		feeds = append(feeds, f)
	}
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, f := range feeds {
		_ = m.stop(f, ctx)
	}
}

// Only bounded scope metadata survives a stop; never payload or positive leases.
func (f *consoleMuxFeed) retireScopeLocked(name string, sc *consoleMuxScope) {
	if f.Retired == nil {
		f.Retired = map[string]*consoleMuxScope{}
	}
	for i := len(f.RetiredOrder) - 1; i >= 0; i-- {
		if f.RetiredOrder[i] == name {
			f.RetiredOrder = append(f.RetiredOrder[:i], f.RetiredOrder[i+1:]...)
		}
	}
	c := *sc
	f.Retired[name] = &c
	f.RetiredOrder = append(f.RetiredOrder, name)
	for len(f.RetiredOrder) > 8 {
		name := f.RetiredOrder[0]
		f.RetiredOrder = f.RetiredOrder[1:]
		delete(f.Retired, name)
	}
}

type muxScopeControlError struct {
	name  string
	cause error
}

func (e *muxScopeControlError) Error() string { return e.cause.Error() }
func (e *muxScopeControlError) Unwrap() error { return e.cause }
func muxWriteScopeError(w http.ResponseWriter, code int, err error) {
	var scope *muxScopeControlError
	if errors.As(err, &scope) {
		muxJSON(w, code, map[string]string{"error": scope.Error(), "full_name": scope.name})
		return
	}
	writeAgentWSError(w, code, err.Error())
}
