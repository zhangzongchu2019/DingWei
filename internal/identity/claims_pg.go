package identity

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

func claimScan(r rowScanner) (*DeviceClaim, error) {
	var b []byte
	if err := r.Scan(&b); err != nil {
		if noRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var c DeviceClaim
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
func (s *PGStore) CreateDeviceClaim(ctx context.Context, c DeviceClaim) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, s.t, `INSERT INTO np_device_claim(id,code_digest,expires_at,body) VALUES($1,$2,$3,$4)`, c.ID, c.CodeDigest, c.ExpiresAt.UTC(), string(b))
	if code, _ := pgCode(err); code == pgUniqueViolation {
		return ErrConflict
	}
	return err
}
func (s *PGStore) GetDeviceClaim(ctx context.Context, id string) (*DeviceClaim, error) {
	r, e := s.queryRow(ctx, `SELECT body FROM np_device_claim WHERE id=$1`, id)
	if e != nil {
		return nil, e
	}
	return claimScan(r)
}
func (s *PGStore) DeviceClaimByCode(ctx context.Context, digest string) (*DeviceClaim, error) {
	r, e := s.queryRow(ctx, `SELECT body FROM np_device_claim WHERE code_digest=$1`, digest)
	if e != nil {
		return nil, e
	}
	return claimScan(r)
}
func writeClaim(ctx context.Context, tx repo.Tx, c *DeviceClaim) error {
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE np_device_claim SET body=$2 WHERE id=$1`, c.ID, string(b))
	return e
}
func (s *PGStore) ClaimDevice(ctx context.Context, digest string, ident Identity, agentID, hash string, now time.Time) (*DeviceClaim, error) {
	var out *DeviceClaim
	err := s.inTx(ctx, func(tx repo.Tx) error {
		// Serialize self-service registrations, including two different claims for the same identity.
		// Row locks below also serialize administrative state changes and revocations.
		if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(22102)`); e != nil {
			return e
		}
		c, e := claimScan(tx.QueryRow(ctx, `SELECT body FROM np_device_claim WHERE code_digest=$1 FOR UPDATE`, digest))
		if e != nil {
			return e
		}
		if !now.Before(c.ExpiresAt) {
			return ErrExpired
		}
		if c.State != "awaiting_feishu" {
			return ErrNotPending
		}
		var bound string
		e = tx.QueryRow(ctx, `SELECT user_id_lc FROM np_identity WHERE provider=$1 AND issuer=$2 AND subject=$3 FOR UPDATE`, ident.Provider, ident.Issuer, ident.Subject).Scan(&bound)
		if e != nil && !noRows(e) {
			return e
		}
		u, e := scanUser(tx.QueryRow(ctx, `SELECT `+userCols+` FROM np_user WHERE user_id_lc=$1 FOR UPDATE`, Lower(c.UserID)))
		if e != nil && !noRows(e) {
			return e
		}
		if bound != "" {
			if u != nil && bound != u.UserIDLC() {
				return ErrForbidden
			}
			if u == nil {
				u, e = scanUser(tx.QueryRow(ctx, `SELECT `+userCols+` FROM np_user WHERE user_id_lc=$1 FOR UPDATE`, bound))
				if e != nil {
					return e
				}
			}
			if u.State != UserActive {
				return ErrForbidden
			}
		} else if u != nil {
			return ErrForbidden
		}
		if u == nil {
			u = &User{UserID: c.UserID, ShortName: c.ShortName, DisplayName: c.UserID, Type: UserMember, State: UserActive, CreatedAt: now}
			if _, e = tx.Exec(ctx, insertUserSQL, insertUserArgs(*u)...); e != nil {
				if code, _ := pgCode(e); code == pgUniqueViolation {
					return ErrConflict
				}
				return e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO np_identity(provider,issuer,subject,user_id_lc) VALUES($1,$2,$3,$4)`, ident.Provider, ident.Issuer, ident.Subject, u.UserIDLC()); e != nil {
				if code, _ := pgCode(e); code == pgUniqueViolation {
					return ErrConflict
				}
				return e
			}
		}
		ag := Agent{ID: agentID, OwnerLC: u.UserIDLC(), Name: c.AgentName, Origin: OriginSelfhost, State: AgentActive, CredentialHash: hash, CreatedAt: now}
		if e = insertAgent(ctx, tx, ag); e != nil {
			return e
		}
		c.UserID = u.UserID
		c.ShortName = u.ShortName
		c.LastError = ""
		c.State = "credential_ready"
		c.AgentID = ag.ID
		c.CredentialHash = hash
		if e = writeClaim(ctx, tx, c); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO audit_log(actor,action,target,after_json,ts) VALUES($1,$2,$3,$4,$5)`, u.UserIDLC(), "enroll.feishu_claim", c.ID, `{}`, now.UTC()); e != nil {
			return e
		}
		out = c
		return nil
	})
	return out, err
}
func (s *PGStore) AckDeviceClaim(ctx context.Context, id, tokenHash string, now time.Time) error {
	return s.inTx(ctx, func(tx repo.Tx) error {
		c, e := claimScan(tx.QueryRow(ctx, `SELECT body FROM np_device_claim WHERE id=$1 FOR UPDATE`, id))
		if e != nil {
			return e
		}
		if subtle.ConstantTimeCompare([]byte(c.PollTokenHash), []byte(tokenHash)) != 1 {
			return ErrNotFound
		}
		if !now.Before(c.ExpiresAt) {
			return ErrExpired
		}
		if c.State == "completed" {
			return nil
		}
		if c.State != "credential_ready" {
			return ErrNotPending
		}
		c.State = "completed"
		return writeClaim(ctx, tx, c)
	})
}

func (s *PGStore) DeviceClaimError(ctx context.Context, id, code string) error {
	return s.inTx(ctx, func(tx repo.Tx) error {
		c, e := claimScan(tx.QueryRow(ctx, `SELECT body FROM np_device_claim WHERE id=$1 FOR UPDATE`, id))
		if e != nil {
			return e
		}
		if c.State != "awaiting_feishu" {
			return nil
		}
		c.LastError = code
		return writeClaim(ctx, tx, c)
	})
}
