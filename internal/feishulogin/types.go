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
)

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
// AttemptID are bound to state when the flow starts. Principal remains empty in
// storage and is populated only from the server-verified Feishu identity during
// Callback, immediately before token issuance.
type IssueRequest struct {
	Principal   extshared.Principal // server-verified identity; never accepted from the browser
	ShortName   string              // external Agent short name, validated by extshared rules
	TargetAgent string              // target helper's canonical full name
	AttemptID   string              // optional caller-stable login attempt / idempotency identifier
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
	AppID         string
	TeamTenantKey string
	RedirectURI   string
	OAuth         OAuthClient
	States        StateStore
	Tokens        TokenIssuer
	StateTTL      time.Duration
	Now           func() time.Time
	Random        io.Reader
}

// StartRequest is supplied by B after validating its enclosing MCP OAuth flow.
type StartRequest struct {
	ShortName   string
	TargetAgent string
	AttemptID   string // optional opaque login attempt / idempotency identifier
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
}

// CallbackResult is the single local-token disclosure point in this package.
// The non-secret issuance metadata is forwarded unchanged for B's OAuth response.
type CallbackResult struct {
	AttemptID             string
	Token                 string
	SeatID                string
	ExternalAgentFullName string
	Group                 extshared.AGCode
	ExpiresAt             time.Time
	Audience              string
	Scopes                []string
}
