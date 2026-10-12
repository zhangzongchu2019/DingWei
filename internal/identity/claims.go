package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const ClaimTTL = 10 * time.Minute
const ClaimAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
const ClaimCodeLen = 12

// DeviceClaim is unowned until a trusted Feishu sender claims it. No credential or raw code is stored.
type DeviceClaim struct {
	ID                string
	CodeDigest        string
	PollTokenHash     string
	UserID            string
	RequestedUserID   string
	ShortName         string
	AgentName         string
	DeviceFingerprint string
	ClientVersion     string
	Mode              string
	RequestedAt       time.Time
	ExpiresAt         time.Time
	State             string
	AgentID           string
	CredentialHash    string
	LastError         string
}

type ClaimStore interface {
	CreateDeviceClaim(context.Context, DeviceClaim) error
	GetDeviceClaim(context.Context, string) (*DeviceClaim, error)
	// ClaimDevice atomically verifies ownership, creates/binds a member if necessary,
	// creates its ungrouped selfhost agent and consumes the code. Audit is in the same transaction.
	ClaimDevice(context.Context, string, Identity, string, string, time.Time) (*DeviceClaim, error)
	// AckDeviceClaim closes credential retrieval after the installer durably saved it.
	AckDeviceClaim(context.Context, string, string, time.Time) error
	// Cancellation may close only an unclaimed application, including expired
	// ones. Issued credentials are never revoked or silently re-enrolled.
	CancelDeviceClaim(context.Context, string, string) error
}

type ClaimRequest struct {
	UserID            string `json:"requested_user_id"`
	ShortName         string `json:"requested_short_name"`
	AgentName         string `json:"agent_name"`
	CodeDigest        string `json:"code_digest"`
	DeviceFingerprint string `json:"device_fingerprint"`
	ClientVersion     string `json:"client_version"`
	Mode              string `json:"mode"`
}

type ClaimResult struct {
	ID, PollToken string
	ExpiresAt     time.Time
}
type ClaimPollResult struct {
	State, UserID, AgentName, FullName, Credential, LastError string
	OwnerReused                                               bool
}

func ValidClaimCode(code string) bool {
	if len(code) != ClaimCodeLen {
		return false
	}
	for _, ch := range code {
		if !strings.ContainsRune(ClaimAlphabet, ch) {
			return false
		}
	}
	return true
}

func (o *Onboarding) claimStore() (ClaimStore, error) {
	st, ok := o.Store.(ClaimStore)
	if !ok || o.CodeKey == "" {
		return nil, ErrNotAllowed
	}
	return st, nil
}

func (o *Onboarding) ApplyDeviceClaim(ctx context.Context, r ClaimRequest, ip string) (*ClaimResult, error) {
	st, err := o.claimStore()
	if err != nil {
		return nil, err
	}
	if !ValidUserID(r.UserID) || !ValidShortName(r.ShortName) || !ValidAgentName(r.AgentName) ||
		(o.NameAllowed != nil && !o.NameAllowed(r.AgentName)) || !isHex(r.CodeDigest, 64) ||
		!isHex(r.DeviceFingerprint, 64) || len(r.ClientVersion) > 64 || r.ClientVersion == "" ||
		(r.Mode != "desktop" && r.Mode != "driver") {
		return nil, ErrBadRequest
	}
	now := o.Now()
	if err = o.takeApplySlot(ip, now); err != nil {
		return nil, err
	}
	id, err := o.randHex(16)
	if err != nil {
		return nil, err
	}
	token, err := o.randHex(32)
	if err != nil {
		return nil, err
	}
	c := DeviceClaim{ID: id, CodeDigest: Lower(r.CodeDigest), PollTokenHash: HashSecret(token), UserID: r.UserID, RequestedUserID: r.UserID,
		ShortName: r.ShortName, AgentName: r.AgentName, DeviceFingerprint: r.DeviceFingerprint,
		ClientVersion: r.ClientVersion, Mode: r.Mode, RequestedAt: now, ExpiresAt: now.Add(ClaimTTL), State: "awaiting_feishu"}
	if err = st.CreateDeviceClaim(ctx, c); err != nil {
		return nil, err
	}
	return &ClaimResult{ID: id, PollToken: token, ExpiresAt: c.ExpiresAt}, nil
}

// Separate domain and random claim ID ensure that claim credentials cannot be derived from registration codes.
func (o *Onboarding) claimCredential(id string) string {
	h := hmac.New(sha256.New, []byte(o.CodeKey))
	h.Write([]byte("dingwei/device-claim/credential/v1\x00" + id))
	return CredentialPrefix + hex.EncodeToString(h.Sum(nil))
}

func (o *Onboarding) HandleDeviceClaim(ctx context.Context, sender Identity, code string) string {
	st, err := o.claimStore()
	if err != nil {
		return "自助配对尚未启用，请使用 Web 审核。"
	}
	code = NormalizeCode(code)
	if sender.Provider != "feishu" || sender.Issuer == "" || sender.Subject == "" || !ValidClaimCode(code) {
		return "配对码无效或无法确认飞书身份。"
	}
	// Limit attempts per authenticated sender, not body-provided identity.
	if err = o.takeApplySlot("claim-feishu:"+HashSecret(sender.Issuer+"\x00"+sender.Subject), o.Now()); err != nil {
		return "配对尝试过于频繁，请稍后重试。"
	}
	agID, err := o.randHex(16)
	if err != nil {
		return "配对暂不可用，请稍后重试。"
	}
	// Credential generation must use the claim's random ID; the store passes it through the callback-independent ID lookup.
	c, err := findDeviceClaimByCode(ctx, st, HashSecret(code))
	if err != nil {
		return "配对码无效、已使用或已过期。"
	}
	claimID := c.ID
	hash := HashSecret(o.claimCredential(claimID))
	c, err = st.ClaimDevice(ctx, HashSecret(code), sender, "ag_"+agID, hash, o.Now())
	if err != nil {
		if errors.Is(err, ErrConflict) || errors.Is(err, ErrNameTaken) {
			o.claimFailure(ctx, st, claimID, "name_conflict")
			return "账号、简称或 Agent 名称冲突，请在安装端修改后重新申请。"
		}
		if errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotAllowed) {
			o.claimFailure(ctx, st, claimID, "owner_mismatch")
			return "此账号不属于当前飞书身份，或账号不可用；未修改任何身份。"
		}
		return "配对码无效、已使用或已过期。"
	}
	return "配对成功：用户 " + c.UserID + "，Agent " + c.AgentName + "。请返回安装端完成连接。"
}

// Lookup is optional so Store's existing enrollment contract remains unchanged.
type claimCodeLookup interface {
	DeviceClaimByCode(context.Context, string) (*DeviceClaim, error)
}

func findDeviceClaimByCode(ctx context.Context, st ClaimStore, digest string) (*DeviceClaim, error) {
	lookup, ok := st.(claimCodeLookup)
	if !ok {
		return nil, ErrNotAllowed
	}
	return lookup.DeviceClaimByCode(ctx, digest)
}

func (o *Onboarding) PollDeviceClaim(ctx context.Context, id, token string) (*ClaimPollResult, error) {
	st, err := o.claimStore()
	if err != nil {
		return nil, err
	}
	c, err := st.GetDeviceClaim(ctx, id)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(HashSecret(token)), []byte(c.PollTokenHash)) != 1 {
		return nil, ErrNotFound
	}
	if !o.Now().Before(c.ExpiresAt) {
		return nil, ErrExpired
	}
	if err = o.claimPollSlot(id); err != nil {
		return nil, err
	}
	r := &ClaimPollResult{State: c.State, LastError: c.LastError}
	if c.State != "credential_ready" && c.State != "completed" {
		return r, nil
	}
	ag, err := o.Store.GetAgent(ctx, c.AgentID)
	if err != nil {
		return nil, err
	}
	u, err := o.Store.GetUser(ctx, c.UserID)
	if err != nil {
		return nil, err
	}
	if u.State != UserActive || ag.State != AgentActive || ag.OwnerLC != u.UserIDLC() {
		return nil, ErrNotAllowed
	}
	r.OwnerReused = Lower(c.RequestedUserID) != Lower(u.UserID)
	r.UserID = u.UserID
	r.AgentName = ag.Name
	r.FullName = ag.Name + "." + u.ShortName
	if c.State == "credential_ready" {
		credential := o.claimCredential(c.ID)
		if subtle.ConstantTimeCompare([]byte(HashSecret(credential)), []byte(ag.CredentialHash)) != 1 {
			return nil, ErrNotAllowed
		}
		r.Credential = credential
	}
	return r, nil
}
func (o *Onboarding) claimPollSlot(id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := o.Now()
	last := o.lastPoll["claim:"+id]
	if !last.IsZero() && now.Sub(last) < PollMinInterval {
		return &RateLimitedError{RetryAfter: PollMinInterval - now.Sub(last)}
	}
	o.lastPoll["claim:"+id] = now
	return nil
}
func (o *Onboarding) AckDeviceClaim(ctx context.Context, id, token string) error {
	st, err := o.claimStore()
	if err != nil {
		return err
	}
	return st.AckDeviceClaim(ctx, id, HashSecret(token), o.Now())
}

// Only safe machine codes reach the installer's private poll; no sender identity is disclosed.
func (o *Onboarding) claimFailure(ctx context.Context, st ClaimStore, id, code string) {
	if recorder, ok := st.(interface {
		DeviceClaimError(context.Context, string, string) error
	}); ok {
		_ = recorder.DeviceClaimError(ctx, id, code)
	}
}

func (o *Onboarding) CancelDeviceClaim(ctx context.Context, id, token string) error {
	st, err := o.claimStore()
	if err != nil {
		return err
	}
	return st.CancelDeviceClaim(ctx, id, HashSecret(token))
}
