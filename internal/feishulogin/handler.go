package feishulogin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// Handler provides B's two integration functions. It does not implement an MCP
// authorization server and never writes logs or audits containing credentials.
type Handler struct {
	allowUnpaired         bool
	allowReauthentication bool
	appID                 string
	teamTenantKey         string
	redirectURI           string
	oauth                 OAuthClient
	states                StateStore
	tokens                TokenIssuer
	stateTTL              time.Duration
	now                   func() time.Time
	random                io.Reader
	defaults              DefaultTargets
	connectCSP            string
}

func NewHandler(cfg Config) (*Handler, error) {
	if cfg.AppID == "" || cfg.TeamTenantKey == "" || strings.TrimSpace(cfg.TeamTenantKey) != cfg.TeamTenantKey ||
		!validRedirectURI(cfg.RedirectURI) || cfg.OAuth == nil || cfg.States == nil || cfg.Tokens == nil {
		return nil, ErrConfig
	}
	if cfg.StateTTL == 0 {
		cfg.StateTTL = DefaultStateTTL
	}
	if cfg.StateTTL < 0 {
		return nil, ErrConfig
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Random == nil {
		cfg.Random = rand.Reader
	}
	return &Handler{
		allowUnpaired: cfg.AllowUnpaired, allowReauthentication: cfg.AllowReauthentication, appID: cfg.AppID, teamTenantKey: cfg.TeamTenantKey, redirectURI: cfg.RedirectURI,
		oauth: cfg.OAuth, states: cfg.States, tokens: cfg.Tokens, stateTTL: cfg.StateTTL,
		now: cfg.Now, random: cfg.Random, defaults: cfg.Defaults,
		connectCSP: connectPageCSPFor(authorizeOriginOf(cfg.OAuth, cfg.RedirectURI)),
	}, nil
}

// Start begins Feishu login and binds the complete issuance intent to state.
func (h *Handler) Start(ctx context.Context, req StartRequest) (StartResult, error) {
	if req.Automatic {
		if (!h.allowUnpaired && !h.allowReauthentication && (h.defaults == nil || len(h.defaults.Allowed(h.teamTenantKey)) == 0)) || req.ShortName != "" {
			return StartResult{}, ErrInvalidStart
		}
		if req.TargetAgent != "" && !h.allowedTarget(ctx, req.TargetAgent) {
			return StartResult{}, ErrInvalidStart
		}
	} else if !extshared.ValidShortName(req.ShortName) || len(req.ShortName) > 8 || !validTargetAgentFullName(req.TargetAgent) {
		return StartResult{}, ErrInvalidStart
	}
	state, err := h.newState()
	if err != nil {
		return StartResult{}, ErrInvalidState
	}
	authorizationURL, err := h.oauth.AuthorizationURL(state, h.redirectURI)
	if err != nil {
		return StartResult{}, ErrInvalidStart
	}
	now := h.now()
	attempt := LoginAttempt{
		StateHash: hashState(state), Purpose: StatePurposeOAuth, AppID: h.appID, RedirectURI: h.redirectURI,
		Request: IssueRequest{
			ShortName: req.ShortName, TargetAgent: req.TargetAgent, AttemptID: req.AttemptID, Automatic: req.Automatic,
		},
		CreatedAt: now, ExpiresAt: now.Add(h.stateTTL),
	}
	if err := h.states.Create(ctx, attempt); err != nil {
		return StartResult{}, err
	}
	return StartResult{AuthorizationURL: authorizationURL, ExpiresAt: attempt.ExpiresAt}, nil
}

// Callback consumes state before any upstream call and invokes A only after the
// server-fetched identity has passed tenant, subject, and display-name checks.
func (h *Handler) Callback(ctx context.Context, req CallbackRequest) (CallbackResult, error) {
	if req.Selection != "" {
		return h.selectTarget(ctx, req)
	}
	if req.State == "" {
		return CallbackResult{}, callbackFailure(FailureOther, TargetCategoryUnverified, req.AttemptID, ErrInvalidCallback)
	}
	attempt, err := h.states.Consume(ctx, hashState(req.State), h.now())
	if err != nil {
		reason := FailureOther
		cause := ErrStateUnavailable
		if errors.Is(err, ErrInvalidState) || errors.Is(err, ErrStateExpired) {
			reason = FailureAuthorizationUsed
			cause = ErrInvalidState
			if errors.Is(err, ErrStateExpired) {
				cause = ErrStateExpired
			}
		}
		return CallbackResult{}, callbackFailure(reason, TargetCategoryUnverified, req.AttemptID, cause)
	}
	attemptID := attempt.Request.AttemptID
	if attemptID == "" {
		attemptID = req.AttemptID
	}
	if req.AttemptID != "" && attempt.Request.AttemptID != req.AttemptID {
		return CallbackResult{}, callbackFailure(FailureAuthorizationUsed, TargetCategoryUnverified, req.AttemptID, ErrInvalidState)
	}
	if req.Code == "" {
		return CallbackResult{}, callbackFailure(FailureOther, TargetCategoryUnverified, attemptID, ErrInvalidCallback)
	}
	if attempt.Purpose != StatePurposeOAuth || attempt.AppID != h.appID || attempt.RedirectURI != h.redirectURI {
		return CallbackResult{}, callbackFailure(FailureAuthorizationUsed, TargetCategoryUnverified, attemptID, ErrInvalidState)
	}
	accessToken, err := h.oauth.ExchangeCode(ctx, req.Code, attempt.RedirectURI)
	if err != nil {
		return CallbackResult{}, callbackFailure(FailureFeishuUnavailable, TargetCategoryUnverified, attemptID, ErrUpstream)
	}
	profile, err := h.oauth.UserInfo(ctx, accessToken)
	if err != nil {
		return CallbackResult{}, callbackFailure(FailureFeishuUnavailable, TargetCategoryUnverified, attemptID, ErrUpstream)
	}
	proof, err := ResolvePrincipal(profile, h.appID, h.teamTenantKey)
	if err == ErrDisplayName && attempt.Request.Automatic {
		fallback := profile
		fallback.EnName = "pm"
		proof, err = ResolvePrincipal(fallback, h.appID, h.teamTenantKey)
	}
	if err != nil {
		reason := FailureOther
		if errors.Is(err, extshared.ErrTenantRejected) {
			reason = FailureTenantRejected
		}
		cause := ErrInvalidCallback
		if reason == FailureTenantRejected {
			cause = extshared.ErrTenantRejected
		} else if errors.Is(err, ErrIdentityIncomplete) {
			cause = ErrIdentityIncomplete
		} else if errors.Is(err, ErrDisplayName) {
			cause = ErrDisplayName
		}
		return CallbackResult{}, callbackFailure(reason, TargetCategoryUnverified, attemptID, cause)
	}
	attempt.Request.Principal = proof
	if attempt.Request.Automatic {
		attempt.Request.ShortName = AutomaticShortName(profile, h.now())
		targets := h.validTargets(ctx)
		existing, e := h.existingSeats(ctx, proof)
		if e != nil {
			return CallbackResult{}, ErrInvalidStart
		}
		if h.allowUnpaired || len(existing) > 0 || (h.defaults != nil && len(h.defaults.Allowed(h.teamTenantKey)) > 1) {
			return h.prepareSelection(ctx, attempt, targets)
		}
		if !h.allowedTarget(ctx, attempt.Request.TargetAgent) {
			return CallbackResult{}, ErrInvalidStart
		}
	}
	return h.issue(ctx, attempt)
}

func (h *Handler) issue(ctx context.Context, attempt LoginAttempt) (CallbackResult, error) {
	attemptID := attempt.Request.AttemptID
	if !h.now().Before(attempt.ExpiresAt) {
		return CallbackResult{}, ErrStateExpired
	}
	if attempt.Request.WithoutPair && (!h.allowUnpaired || attempt.Request.TargetAgent != "") {
		return CallbackResult{}, ErrInvalidStart
	}
	if attempt.Request.ExistingSeatID != "" && (!h.allowReauthentication || attempt.Request.WithoutPair || attempt.Request.TargetAgent != "") {
		return CallbackResult{}, ErrInvalidStart
	}
	if attempt.Request.Automatic && attempt.Request.ExistingSeatID == "" && !attempt.Request.WithoutPair && !h.allowedTarget(ctx, attempt.Request.TargetAgent) {
		return CallbackResult{}, ErrInvalidStart
	}
	issued, err := h.tokens.IssueToken(ctx, attempt.Request)
	if err != nil {
		reason := FailureOther
		targetCategory := TargetCategoryUnverified
		cause := error(ErrTokenIssue)
		if errors.Is(err, extshared.ErrTenantRejected) {
			reason = FailureTenantRejected
			cause = extshared.ErrTenantRejected
		} else if errors.Is(err, extshared.ErrTargetNotInAG) {
			reason = FailureTargetUnavailable
			targetCategory = TargetCategoryExists
			if errors.Is(err, extshared.ErrTargetMissing) {
				targetCategory = TargetCategoryMissing
			}
		}
		return CallbackResult{}, callbackFailure(reason, targetCategory, attemptID, cause)
	}
	return CallbackResult{
		AttemptID: attempt.Request.AttemptID, Token: issued.Token, SeatID: issued.SeatID,
		ExternalAgentFullName: issued.ExternalAgentFullName, Group: issued.Group,
		ExpiresAt: issued.ExpiresAt, Audience: issued.Audience, Scopes: issued.Scopes,
	}, nil
}

func hashState(state string) string {
	sum := sha256.Sum256([]byte(state))
	return hex.EncodeToString(sum[:])
}

// CallbackRequestFromQuery is B's narrow adapter from a Feishu callback URL.
// It deliberately ignores short_name, target_agent, identity, and redirect_uri
// query keys: the issuance intent comes only from the consumed server-side state.
func CallbackRequestFromQuery(query url.Values) CallbackRequest {
	return CallbackRequest{State: query.Get("state"), Code: query.Get("code")}
}

func (h *Handler) newState() (string, error) {
	var random [32]byte
	if _, err := io.ReadFull(h.random, random[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random[:]), nil
}

// validTargetAgentFullName checks shape only. It never resolves or enumerates
// Agents; A remains responsible for the authoritative active-target check.
func validTargetAgentFullName(fullName string) bool {
	i := strings.LastIndexByte(fullName, '.')
	return i > 0 && i < len(fullName)-1 &&
		identity.ValidAgentName(fullName[:i]) && identity.ValidShortName(fullName[i+1:])
}
