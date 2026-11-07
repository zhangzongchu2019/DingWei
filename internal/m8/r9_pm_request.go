package m8

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/r9pm"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/trustedorigin"
)

var ErrR9PMNotConfigured = errors.New("未配置 PM")

type r9PMRequestStore interface {
	GetR9PMResponsibility(context.Context, store.R9PMKey) (*store.R9PMResponsibility, error)
	CreateR9PMRequest(context.Context, store.R9PMCreateInput) (store.R9PMRequest, bool, error)
}
type R9PMDecisionResult struct {
	Request store.R9PMRequest
	Created bool
	Status  string
}

// RequestR9PMDecision reads one proposal at the immutable commit, parses the complete
// decision section, resolves the sole configured PM, and atomically creates both channel rows.
func (h *Hub) RequestR9PMDecision(ctx context.Context, actor string, c specread.Coordinate, changeDirectory, pointID string) (R9PMDecisionResult, error) {
	if !R9PMDecisionRequestEnabled() {
		return R9PMDecisionResult{}, errors.New("R9 PM decision request disabled")
	}
	if h.SpecReader == nil || h.Identity == nil || h.Identity.Store == nil {
		return R9PMDecisionResult{}, specread.ErrUnavailable
	}
	repo, ok := h.Repo.(r9PMRequestStore)
	if !ok {
		return R9PMDecisionResult{}, errors.New("R9 PM store unavailable")
	}
	if actor == "" || c.Commit == "" || changeDirectory == "" || pointID == "" || strings.ContainsAny(changeDirectory, "\\\x00\r\n") {
		return R9PMDecisionResult{}, specread.ErrInvalid
	}
	c.Path = "openspec/changes/" + changeDirectory + "/proposal.md"
	read, err := h.SpecReader.Read(ctx, actor, c, "")
	if err != nil {
		return R9PMDecisionResult{}, err
	}
	points, err := r9pm.ParseDecisionPoints(read.Text)
	if err != nil {
		return R9PMDecisionResult{}, err
	}
	var point *r9pm.Point
	for i := range points {
		if points[i].ID == pointID {
			point = &points[i]
			break
		}
	}
	if point == nil {
		return R9PMDecisionResult{}, fmt.Errorf("%w: point not present", r9pm.ErrIncomplete)
	}
	key := store.R9PMKey{Provider: c.Provider, Instance: c.Instance, RepositoryID: c.Repository, ChangeDirectory: changeDirectory, PointID: pointID}
	mapping, err := repo.GetR9PMResponsibility(ctx, key)
	if errors.Is(err, store.ErrR9PMNotFound) || err == nil && mapping.PMUserIDLC == "" {
		return R9PMDecisionResult{Status: "未配置 PM"}, ErrR9PMNotConfigured
	}
	if err != nil {
		return R9PMDecisionResult{}, err
	}
	pm, err := h.Identity.Store.GetUser(ctx, mapping.PMUserIDLC)
	if err != nil || pm == nil || pm.State != identity.UserActive {
		return R9PMDecisionResult{Status: "未配置 PM"}, ErrR9PMNotConfigured
	}
	feishuID, err := h.Identity.Store.GetIdentity(ctx, pm.UserIDLC(), "feishu")
	if err != nil || feishuID == nil || feishuID.Issuer == "" || feishuID.Subject == "" {
		return R9PMDecisionResult{}, errors.New("configured PM has no active private Feishu identity")
	}
	summary, _ := json.Marshal(struct {
		Question       string   `json:"question"`
		Options        []string `json:"options"`
		Recommendation string   `json:"recommendation"`
		Source         string   `json:"source"`
	}{point.Question, point.Options, point.Recommendation, read.Source})
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return R9PMDecisionResult{}, err
	}
	id := hex.EncodeToString(nonce[:])
	req, created, err := repo.CreateR9PMRequest(ctx, store.R9PMCreateInput{RequestID: id, R9PMKey: key, CommitSHA: c.Commit, ContentSHA256: point.ContentSHA256(pm.UserIDLC()), PMUserIDLC: pm.UserIDLC(), ConsoleRecipient: pm.UserIDLC(), FeishuRecipient: feishuID.Subject, FeishuIssuer: feishuID.Issuer, SummaryJSON: string(summary), CreatedByUserIDLC: actor})
	if err != nil {
		return R9PMDecisionResult{}, err
	}
	return R9PMDecisionResult{Request: req, Created: created, Status: req.Status}, nil
}

type r9PMRequestHandlerInput struct {
	Provider        string `json:"provider"`
	Instance        string `json:"instance"`
	Repository      string `json:"repository_id"`
	Commit          string `json:"commit_sha"`
	ChangeDirectory string `json:"change_directory"`
	PointID         string `json:"point_id"`
}

func (h *Hub) HandleR9PMRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !R9PMDecisionRequestEnabled() {
		http.Error(w, "R9 PM decision request disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	actor := platformPrincipal(r)
	if actor == "" {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	origin, e := url.Parse(r.Header.Get("Origin"))
	if e != nil || !trustedorigin.AuthorityMatches(origin.Host, r.Host, len(h.TrustedOrigins) > 0) || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" || !h.originAllowed(r.Header.Get("Origin"), h.PublicOrigin == "" || r.Header.Get("Origin") == h.PublicOrigin, len(r.Header.Values("Origin"))) || r.Header.Get("Sec-Fetch-Site") == "cross-site" || r.Header.Get("Content-Type") != "application/json" || r.URL.RawQuery != "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var in r9PMRequestHandlerInput
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	result, err := h.RequestR9PMDecision(r.Context(), actor, specread.Coordinate{Provider: in.Provider, Instance: in.Instance, Repository: in.Repository, Commit: in.Commit}, in.ChangeDirectory, in.PointID)
	if errors.Is(err, ErrR9PMNotConfigured) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "未配置 PM"})
		return
	}
	if err != nil {
		http.Error(w, "R9 PM request unavailable", http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
