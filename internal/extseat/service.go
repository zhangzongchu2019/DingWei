package extseat

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// Service 不拥有数据库生命周期；调用方装配已迁移的身份 PG 与 D 路消费者。
type Service struct {
	// OnBackgroundError 由装配提供；不得记录Bearer或正文。
	OnBackgroundError        func(error)
	OnFeishuCopyEnqueueError func(error)
	// Resource 是本地令牌唯一受保护资源标识，与协议层的metadata保持一致。
	Resource       string
	Pool           *repo.Pool
	Access         repo.AccessCtx
	Identity       *identity.Service
	Groups         Groups
	Helpers        Helpers
	FeishuCopies   FeishuCopyQueue
	Now            func() time.Time
	TenantKey      string
	Rand           io.Reader
	QueueLimit     int
	ExternalGroups bool // 仅控制新开户/新入组；已存关系在回退时保留
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s *Service) random(n int) (string, error) {
	b := make([]byte, n)
	r := s.Rand
	if r == nil {
		r = rand.Reader
	}
	_, e := io.ReadFull(r, b)
	return hex.EncodeToString(b), e
}
func (s *Service) tx(ctx context.Context, fn func(repo.Tx) error) error {
	if s.Pool == nil || s.Identity == nil || s.Groups == nil {
		return extshared.ErrNotPermitted
	}
	return s.Pool.InTx(ctx, s.Access, nil, fn)
}
func (s *Service) limit() int {
	if s.QueueLimit > 0 {
		return s.QueueLimit
	}
	return 1000
}
func digest(v any) string { b, _ := json.Marshal(v); return identity.HashSecret(string(b)) }
func validPrincipal(p extshared.Principal) bool {
	return p.Provider == "feishu" && p.TenantKey != "" && p.Issuer != "" && p.Subject != "" && p.DisplayName != "" &&
		!strings.ContainsAny(p.Provider+p.TenantKey+p.Issuer+p.Subject, "|\x00")
}

// IssueToken 只供已验证飞书/OAuth交换内部调用，不是可公开任意签发端点。
func (s *Service) IssueToken(ctx context.Context, p extshared.Principal, short, target string) (string, Seat, string, error) {
	return s.issueToken(ctx, p, short, target, false)
}

func (s *Service) IssueTokenWithoutPair(ctx context.Context, p extshared.Principal, short string) (string, Seat, string, error) {
	if !s.ExternalGroups {
		return "", Seat{}, "", extshared.ErrNotPermitted
	}
	return s.issueToken(ctx, p, short, "", true)
}

func (s *Service) issueToken(ctx context.Context, p extshared.Principal, short, target string, unpaired bool) (string, Seat, string, error) {
	var seat Seat
	if s.Identity == nil || s.Groups == nil {
		return "", seat, "", extshared.ErrNotPermitted
	}
	if s.Resource == "" {
		return "", seat, "", extshared.ErrNotPermitted
	}
	if err := (extshared.SameTenantAdmission{}).Check(ctx, p, s.TenantKey); err != nil {
		s.auditTenantDeny(ctx, p)
		return "", seat, "", extshared.ErrTenantRejected
	}
	if !validPrincipal(p) || !extshared.ValidShortName(short) {
		return "", seat, "", extshared.ErrNotPermitted
	}
	var ag *identity.Agent
	if !unpaired {
		resolved, u, err := s.Identity.ResolveFullName(ctx, target)
		if errors.Is(err, identity.ErrNotFound) {
			return "", seat, "", errors.Join(extshared.ErrTargetNotInAG, extshared.ErrTargetMissing)
		}
		if err != nil || resolved.State != identity.AgentActive || u.State != identity.UserActive || resolved.Origin == identity.OriginExternal {
			return "", seat, "", extshared.ErrTargetNotInAG
		}
		ag = resolved
	}
	id, e := s.random(16)
	if e != nil {
		return "", seat, "", e
	}
	tid, e := s.random(16)
	if e != nil {
		return "", seat, "", e
	}
	secret, e := s.random(32)
	if e != nil {
		return "", seat, "", e
	}
	raw := tid + "." + secret
	now := s.now()
	ph := digest([]string{p.Provider, p.TenantKey, p.Issuer, p.Subject})
	e = s.tx(ctx, func(tx repo.Tx) error {
		// 串行同主体首次开户；不按显示名或union_id跨域合并。
		if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, ph); e != nil {
			return e
		}
		var owner string
		e := tx.QueryRow(ctx, `SELECT owner_lc FROM np_external_principal WHERE id=$1`, ph).Scan(&owner)
		if errors.Is(e, sql.ErrNoRows) {
			e = tx.QueryRow(ctx, `SELECT i.user_id_lc FROM np_identity i JOIN np_user u ON u.user_id_lc=i.user_id_lc WHERE i.provider=$1 AND i.issuer=$2 AND i.subject=$3 AND u.state='active'`, p.Provider, p.Issuer, p.Subject).Scan(&owner)
			if errors.Is(e, sql.ErrNoRows) {
				owner = "ex" + ph[:24]
				shortOwner := "ex" + ph[:14]
				if _, e = tx.Exec(ctx, `INSERT INTO np_user(user_id,user_id_lc,short_name,short_name_lc,display_name,type,state,password_hash,must_change_password,created_at) VALUES($1,$1,$2,$2,$3,'member','active','',false,$4)`, owner, shortOwner, p.DisplayName, now); e != nil {
					return e
				}
				if _, e = tx.Exec(ctx, `INSERT INTO np_identity(provider,issuer,subject,user_id_lc) VALUES($1,$2,$3,$4)`, p.Provider, p.Issuer, p.Subject, owner); e != nil {
					return e
				}
			} else if e != nil {
				return e
			}
			_, e = tx.Exec(ctx, `INSERT INTO np_external_principal(id,provider,tenant_key,issuer,subject,union_id,display_name,owner_lc,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, ph, p.Provider, p.TenantKey, p.Issuer, p.Subject, p.UnionID, p.DisplayName, owner, now)
			if e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		seat = Seat{ID: "ex_" + id, AgentID: "ex_" + id, OwnerLC: owner, Principal: p, TokenID: tid, AuthGeneration: 1, ExpiresAt: now.Add(extshared.TokenTTL)}
		if unpaired {
			var existingID, existingName, ownerShort string
			var existingGeneration int64
			err := tx.QueryRow(ctx, `SELECT s.agent_id,a.name,s.auth_generation,u.short_name FROM np_extseat s JOIN np_agent a ON a.id=s.agent_id JOIN np_user u ON u.user_id_lc=a.owner_lc WHERE s.principal_id=$1 AND s.short_name=$2 AND s.ag_code IS NULL AND s.revoked_at IS NULL AND a.state='active' AND u.state='active' FOR UPDATE OF s,a,u`, ph, short).Scan(&existingID, &existingName, &existingGeneration, &ownerShort)
			if err == nil {
				seat.ID, seat.AgentID, seat.Name, seat.FullName, seat.AuthGeneration = existingID, existingID, existingName, existingName+"."+ownerShort, existingGeneration
				_, err = tx.Exec(ctx, `INSERT INTO np_extseat_token(id,token_hash,seat_id,auth_generation,created_at,last_auth_at,expires_at,audience,scopes) VALUES($1,$2,$3,$4,$5,$5,$6,$7,$8)`, tid, identity.HashSecret(raw), seat.AgentID, existingGeneration, now, seat.ExpiresAt, s.Resource, extshared.ScopeString())
				if err != nil {
					return err
				}
				return s.audit(ctx, tx, owner, "extseat.reauthenticated", seat.ID, now)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		calls := 0
		create := func(g Group) error {
			calls++
			if calls != 1 || (!unpaired && (g.Generation < 1 || g.Target.ID != ag.ID)) {
				return extshared.ErrNotPermitted
			}
			name, e := extshared.AgentName(short, g.Code)
			if unpaired {
				name, e = extshared.UnpairedAgentName(short, seat.AgentID)
			}
			if e != nil {
				return e
			}
			seat.Name = name
			seat.Group = g.Code
			seat.Generation = g.Generation
			var ownerShort string
			if e = tx.QueryRow(ctx, `SELECT short_name FROM np_user WHERE user_id_lc=$1 AND state='active' FOR UPDATE`, owner).Scan(&ownerShort); e != nil {
				return e
			}
			seat.FullName = name + "." + ownerShort
			if _, e = tx.Exec(ctx, `INSERT INTO np_agent(id,owner_lc,name,name_lc,group_code,origin,state,created_at) VALUES($1,$2,$3,$4,$5,'external','active',$6)`, seat.AgentID, owner, name, strings.ToLower(name), string(g.Code), now); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO np_extseat(agent_id,principal_id,short_name,ag_code,ag_generation,created_at) VALUES($1,$2,$3,$4,$5,$6)`, seat.AgentID, ph, short, optionalAG(g.Code), optionalGeneration(g.Generation), now); e != nil {
				return e
			}
			_, e = tx.Exec(ctx, `INSERT INTO np_extseat_token(id,token_hash,seat_id,auth_generation,created_at,last_auth_at,expires_at,audience,scopes) VALUES($1,$2,$3,1,$4,$4,$5,$6,$7)`, tid, identity.HashSecret(raw), seat.AgentID, now, seat.ExpiresAt, s.Resource, extshared.ScopeString())
			return e
		}
		var g Group
		if unpaired {
			e = create(g)
		} else {
			g, e = s.Groups.CreateTempGroup(ctx, tx, CreateGroupRequest{Principal: p, ExternalAgentID: seat.AgentID, OwnerLC: owner, Target: *ag, At: now, CreateExternal: create})
		}
		if e != nil {
			return e
		}
		if calls != 1 || seat.Group != g.Code || seat.Generation != g.Generation {
			return extshared.ErrNotPermitted
		}
		return s.audit(ctx, tx, owner, "extseat.issued", seat.ID, now)
	})
	if e != nil {
		return "", Seat{}, "", e
	}
	seat.proof = identity.HashSecret(raw)
	return raw, seat, string(seat.Group), nil
}

// load 锁定令牌、席位、Agent与属主；吊销检查在续期前，所有业务入口复用。
func (s *Service) load(ctx context.Context, tx repo.Tx, tokenID, proof string, now time.Time, locking ...bool) (Seat, error) {
	lock := " FOR UPDATE OF t,s,a,u"
	if len(locking) > 0 && !locking[0] {
		lock = ""
	}
	var a Seat
	var hash, astate, ustate, audience, scopes string
	var tr, sr sql.NullTime
	var last time.Time
	var seen sql.NullTime
	e := tx.QueryRow(ctx, `SELECT t.id,t.token_hash,t.expires_at,t.last_auth_at,s.last_seen_at,t.revoked_at,s.revoked_at,s.agent_id,s.auth_generation,t.auth_generation,COALESCE(s.ag_code,''),COALESCE(s.ag_generation,0),a.name,a.owner_lc,a.state,u.short_name,u.state,p.provider,p.tenant_key,p.issuer,p.subject,p.union_id,p.display_name,t.audience,t.scopes FROM np_extseat_token t JOIN np_extseat s ON s.agent_id=t.seat_id JOIN np_agent a ON a.id=s.agent_id JOIN np_user u ON u.user_id_lc=a.owner_lc JOIN np_external_principal p ON p.id=s.principal_id WHERE t.id=$1`+lock, tokenID).Scan(&a.TokenID, &hash, &a.ExpiresAt, &last, &seen, &tr, &sr, &a.AgentID, &a.AuthGeneration, &a.tokenGeneration, &a.Group, &a.Generation, &a.Name, &a.OwnerLC, &astate, &a.ownerShort, &ustate, &a.Principal.Provider, &a.Principal.TenantKey, &a.Principal.Issuer, &a.Principal.Subject, &a.Principal.UnionID, &a.Principal.DisplayName, &audience, &scopes)
	if errors.Is(e, sql.ErrNoRows) {
		return a, extshared.ErrUnauthenticated
	}
	if e != nil {
		return a, e
	}
	now = s.now() // 锁等待结束后采样，不能用请求排队前的时刻复活已过期令牌。
	a.checkedAt = now
	if subtle.ConstantTimeCompare([]byte(hash), []byte(proof)) != 1 {
		return a, extshared.ErrUnauthenticated
	}
	if tr.Valid || sr.Valid || astate != "active" || ustate != "active" || a.AuthGeneration != a.tokenGeneration {
		return a, extshared.ErrTokenRevoked
	}
	if !now.Before(a.ExpiresAt) {
		return a, extshared.ErrTokenExpired
	}
	if now.Before(last) || (seen.Valid && now.Before(seen.Time)) {
		return a, extshared.ErrNotPermitted
	}
	if s.Resource == "" || audience != s.Resource || scopes != extshared.ScopeString() {
		return a, extshared.ErrNotPermitted
	}
	if err := (extshared.SameTenantAdmission{}).Check(ctx, a.Principal, s.TenantKey); err != nil {
		return a, extshared.ErrTenantRejected
	}
	a.ID = a.AgentID
	a.FullName = a.Name + "." + a.ownerShort
	a.proof = proof
	return a, nil
}
func (s *Service) authTx(ctx context.Context, tx repo.Tx, a Seat, now time.Time) (Seat, Group, error) {
	fresh, e := s.load(ctx, tx, a.TokenID, a.proof, now)
	if e != nil {
		return fresh, Group{}, e
	}
	if a.AgentID != fresh.AgentID || a.Generation != fresh.Generation || a.AuthGeneration != fresh.AuthGeneration {
		return fresh, Group{}, extshared.ErrUnauthenticated
	}
	if fresh.Group == "" {
		return fresh, Group{}, extshared.ErrNotPermitted
	}
	g, e := s.Groups.RecheckGroup(ctx, tx, fresh, fresh.checkedAt)
	if e != nil {
		return fresh, g, e
	}
	fresh.checkedAt, e = s.current(fresh, g)
	return fresh, g, e
}
func (s *Service) Authenticate(ctx context.Context, bearer string) (Seat, error) {
	var a Seat
	id, secret, ok := strings.Cut(bearer, ".")
	if !ok || len(id) != 32 || len(secret) != 64 {
		return a, extshared.ErrUnauthenticated
	}
	now := s.now()
	e := s.tx(ctx, func(tx repo.Tx) error {
		var e error
		a, e = s.load(ctx, tx, id, identity.HashSecret(bearer), now)
		if e != nil {
			return e
		}
		upgraded, e := s.hasOrdinaryTx(ctx, tx, a.AgentID)
		if e != nil {
			return e
		}
		if a.Group != "" && !upgraded {
			g, err := s.Groups.RecheckGroup(ctx, tx, a, a.checkedAt)
			if err != nil {
				return err
			}
			now, err = s.current(a, g)
			if err != nil {
				return err
			}
		} else {
			now = s.now()
		}

		expiry := now.Add(extshared.TokenTTL)
		r, e := tx.Exec(ctx, `UPDATE np_extseat_token SET expires_at=GREATEST(expires_at,$2),last_auth_at=$3 WHERE id=$1 AND revoked_at IS NULL AND expires_at>$3 AND last_auth_at<=$3 AND auth_generation=$4`, id, expiry, now, a.AuthGeneration)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return extshared.ErrTokenRevoked
		}
		if expiry.After(a.ExpiresAt) {
			a.ExpiresAt = expiry
		}
		return nil
	})
	if e != nil {
		return Seat{}, e
	}
	return a, nil
}
func (s *Service) Recheck(ctx context.Context, a Seat) error {
	return s.tx(ctx, func(tx repo.Tx) error { _, _, e := s.authTx(ctx, tx, a, s.now()); return e })
}
func (s *Service) seen(ctx context.Context, tx repo.Tx, a Seat, now time.Time) error {
	_, e := tx.Exec(ctx, `UPDATE np_extseat SET last_seen_at=$2 WHERE agent_id=$1 AND (last_seen_at IS NULL OR last_seen_at<=$2)`, a.AgentID, now)
	return e
}
func (s *Service) Revoke(ctx context.Context, seatID string) error {
	now := s.now()
	return s.tx(ctx, func(tx repo.Tx) error {
		if _, e := tx.Exec(ctx, `UPDATE np_extseat_token SET revoked_at=$2 WHERE seat_id=$1 AND revoked_at IS NULL`, seatID, now); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `UPDATE np_extseat SET revoked_at=$2,auth_generation=auth_generation+1 WHERE agent_id=$1 AND revoked_at IS NULL`, seatID, now); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `UPDATE np_agent SET state='revoked',revoked_at=$2 WHERE id=$1 AND origin='external'`, seatID, now)
		if e != nil {
			return e
		}
		return s.audit(ctx, tx, seatID, "extseat.revoked", seatID, now)
	})
}

// TouchGroup 只供受理/首次ACK在其原子事务中调用，不是公开通讯工具。
func (s *Service) TouchGroup(ctx context.Context, tx repo.Tx, g Group, event string, kind extshared.GroupUseKind, at time.Time) error {
	if event == "" || (kind != extshared.UseAcceptedSend && kind != extshared.UseFirstReceivedAck) {
		return extshared.ErrNotPermitted
	}
	return s.Groups.TouchGroup(ctx, tx, g, event, kind, at)
}

// audit只记稳定ID与动作；不记Bearer/hash、消息正文或飞书原始响应。
func (s *Service) audit(ctx context.Context, tx repo.Tx, actor, action, target string, at time.Time) error {
	_, e := tx.Exec(ctx, `INSERT INTO audit_log(actor,action,target,after_json,ts) VALUES($1,$2,$3,'{}',$4)`, actor, action, target, at)
	return e
}

// current 在锁/消费者调用之后重取时钟，expiry整点先拒绝，再允许副作用。
func (s *Service) current(a Seat, g Group) (time.Time, error) {
	now := s.now()
	if now.Before(a.checkedAt) {
		return now, extshared.ErrNotPermitted
	}
	if !now.Before(a.ExpiresAt) {
		return now, extshared.ErrTokenExpired
	}
	if g.Code != a.Group || g.Generation != a.Generation || !now.Before(g.ExpiresAt) {
		return now, extshared.ErrGroupDissolved
	}
	return now, nil
}

// auditTenantDeny reuses the identity audit store (the same audit_log as seat
// issuance). No resource lookup, target name, supplied tenant or token is logged.
func (s *Service) auditTenantDeny(ctx context.Context, p extshared.Principal) {
	if s.Identity.Store == nil {
		log.Print("extseat: deny audit store unavailable")
		return
	}
	detail, _ := json.Marshal(map[string]string{"tenant": s.TenantKey, "op": "extseat.issue_token", "decision": "deny"})
	event := identity.AuditEvent{Actor: digest([]string{p.Provider, p.Issuer, p.Subject}), Action: "extseat.issue_denied", Detail: string(detail), At: s.now()}
	if err := s.Identity.Store.AppendAudit(ctx, event); err != nil {
		log.Print("extseat: deny audit write failed")
	}
}

func optionalAG(v extshared.AGCode) any {
	if v == "" {
		return nil
	}
	return string(v)
}
func optionalGeneration(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
