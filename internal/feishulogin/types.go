// Package feishulogin implements the Feishu upstream login for the MCP OAuth flow.
package feishulogin

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
)

const DefaultStateTTL = 5 * time.Minute

// StatePurpose prevents a form CSRF token and a Feishu OAuth state from being
// used interchangeably even when they share a StateStore.
type StatePurpose string

const (
	StatePurposeOAuth       StatePurpose = "feishu_oauth"
	StatePurposeConnectForm StatePurpose = "connect_form_csrf"
	StatePurposeSelection   StatePurpose = "verified_target_selection"
)

var (
	ErrConfig             = errors.New("feishulogin: invalid configuration")
	ErrInvalidStart       = errors.New("feishulogin: invalid login request")
	ErrInvalidCallback    = errors.New("feishulogin: invalid callback")
	ErrInvalidState       = errors.New("feishulogin: state rejected")
	ErrStateExpired       = errors.New("feishulogin: state expired")
	ErrUpstream           = errors.New("feishulogin: Feishu authentication failed")
	ErrIdentityIncomplete = errors.New("feishulogin: Feishu identity is incomplete")
	ErrDisplayName        = errors.New("feishulogin: display name is empty")
	ErrTokenIssue         = errors.New("feishulogin: local token issuance failed")
	ErrStateUnavailable   = errors.New("feishulogin: state service unavailable")
)

type CallbackFailureReason string

const (
	FailureTargetUnavailable CallbackFailureReason = "target_unavailable"
	FailureTenantRejected    CallbackFailureReason = "tenant_not_allowed"
	FailureAuthorizationUsed CallbackFailureReason = "authorization_expired_or_used"
	FailureFeishuUnavailable CallbackFailureReason = "feishu_temporarily_unavailable"
	FailureOther             CallbackFailureReason = "other"
)

type CallbackTargetCategory string

const (
	TargetCategoryExists     CallbackTargetCategory = "exists"
	TargetCategoryMissing    CallbackTargetCategory = "missing"
	TargetCategoryUnverified CallbackTargetCategory = "unverified"
)

// CallbackFailure contains only allow-listed diagnostics. AttemptPrefix is
// truncated before construction; neither target names nor upstream errors are
// retained. Cause is always a package sentinel, never an upstream error.
type CallbackFailure struct {
	ReasonCode     CallbackFailureReason
	AttemptPrefix  string
	TargetCategory CallbackTargetCategory
	cause          error
}

func (e *CallbackFailure) Error() string { return "feishulogin: callback failed" }
func (e *CallbackFailure) Unwrap() error { return e.cause }

func callbackFailure(reason CallbackFailureReason, target CallbackTargetCategory, attemptID string, cause error) error {
	prefix := attemptID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	if prefix == "" {
		prefix = "unknown"
	}
	if target == "" {
		target = TargetCategoryUnverified
	}
	return &CallbackFailure{ReasonCode: reason, AttemptPrefix: prefix, TargetCategory: target, cause: cause}
}

// CallbackFailureDetails returns the safe, bounded diagnostic fields for the
// gateway logger. It never returns callback state, code, identity, or target.
func CallbackFailureDetails(err error) (CallbackFailureReason, string, CallbackTargetCategory, bool) {
	var failure *CallbackFailure
	if !errors.As(err, &failure) {
		return FailureOther, "unknown", TargetCategoryUnverified, false
	}
	return failure.ReasonCode, failure.AttemptPrefix, failure.TargetCategory, true
}

// UserInfo contains only the non-sensitive fields returned by Feishu user_info.
type UserInfo struct {
	Name      string `json:"name"`
	EnName    string `json:"en_name"`
	OpenID    string `json:"open_id"`
	UnionID   string `json:"union_id"`
	TenantKey string `json:"tenant_key"`
}

// UserAccessToken is deliberately short-lived and is never persisted by this package.
type UserAccessToken string

func (UserAccessToken) String() string   { return "[redacted]" }
func (UserAccessToken) GoString() string { return "[redacted]" }

// OAuthClient is the Feishu protocol boundary. Implementations must not log code,
// client secrets, user access tokens, or local tokens.
type OAuthClient interface {
	AuthorizationURL(state, redirectURI string) (string, error)
	ExchangeCode(ctx context.Context, code, redirectURI string) (UserAccessToken, error)
	UserInfo(ctx context.Context, token UserAccessToken) (UserInfo, error)
}

// IssueRequest is the complete issuance intent. ShortName, TargetAgent, and
// AttemptID are bound to state when the flow starts. Automatic names are added
// only after the trusted profile. A pending multi-target selection stores that
// verified principal and the generated name in the encrypted, one-time state.
type IssueRequest struct {
	ExistingSeatID string              // 仅已验证主体显式选择的原席位
	WithoutPair    bool                // 仅消费已验证 selection state 后设置
	Principal      extshared.Principal // server-verified identity; never accepted from the browser
	ShortName      string              // external Agent short name, validated by extshared rules
	TargetAgent    string              // target helper's canonical full name
	AttemptID      string              // optional caller-stable login attempt / idempotency identifier
	Automatic      bool                // server-bound intent; never accepted from callback query
}

// IssuedToken is the A-lane result transported by the integration adapter.
// Token is transient plaintext: it is never persisted or logged by this package.
type IssuedToken struct {
	Token                 string           // local bearer plaintext; transient return value only
	SeatID                string           // A-lane internal seat identifier
	ExternalAgentFullName string           // external Agent's canonical full name
	Group                 extshared.AGCode // temporary AG assigned to the seat
	ExpiresAt             time.Time        // current local-token expiry from A
	Audience              string           // shared MCP protected-resource identifier
	Scopes                []string         // exactly the four communication capabilities; no subsets
}

// TokenIssuer is implemented by an A-lane adapter, not by this package.
// A owns token persistence, revocation, expiry, and the 14-day sliding renewal.
type TokenIssuer interface {
	IssueToken(ctx context.Context, req IssueRequest) (IssuedToken, error)
}

// LoginAttempt contains no raw state or authorization code.
type LoginAttempt struct {
	StateHash   string
	Purpose     StatePurpose
	AppID       string
	RedirectURI string
	Request     IssueRequest
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// StateStore atomically consumes state. Production deployments must provide a
// shared durable implementation so concurrent callbacks and restarts cannot replay it.
type StateStore interface {
	Create(ctx context.Context, attempt LoginAttempt) error
	Consume(ctx context.Context, stateHash string, now time.Time) (LoginAttempt, error)
}

// Config contains server-owned configuration. TeamTenantKey is independent and
// required; production wiring should populate it from DW_FEISHU_TEAM_TENANT_KEY.
type Config struct {
	AllowUnpaired         bool
	AllowReauthentication bool
	AppID                 string
	TeamTenantKey         string
	RedirectURI           string
	OAuth                 OAuthClient
	States                StateStore
	Tokens                TokenIssuer
	StateTTL              time.Duration
	Now                   func() time.Time
	Random                io.Reader
	Defaults              DefaultTargets
}

// StartRequest is supplied by B after validating its enclosing MCP OAuth flow.
type StartRequest struct {
	ShortName   string
	TargetAgent string
	AttemptID   string // optional opaque login attempt / idempotency identifier
	Automatic   bool
}

// StartResult tells B where to redirect the browser. Raw state occurs only in
// AuthorizationURL and is not separately persisted or returned.
type StartResult struct {
	AuthorizationURL string
	ExpiresAt        time.Time
}

// CallbackRequest is populated only from Feishu's callback query. Identity and
// tenant fields are never accepted from this input.
type CallbackRequest struct {
	State string
	Code  string
	// AttemptID is supplied by the gateway from its own encrypted state record.
	// It is never read from the Feishu callback query.
	AttemptID string
	Selection string
	Target    string
}

// CallbackResult is the single local-token disclosure point in this package.
// The non-secret issuance metadata is forwarded unchanged for B's OAuth response.
type CallbackResult struct {
	ExistingSeats []extshared.SeatChoice
	AllowUnpaired bool
	Selection     string
	Targets       []string
	Preferred     string

	AttemptID             string
	Token                 string
	SeatID                string
	ExternalAgentFullName string
	Group                 extshared.AGCode
	ExpiresAt             time.Time
	Audience              string
	Scopes                []string
}
