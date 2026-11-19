package mcpgw

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

var errDCRRate = errors.New("mcpgw: DCR rate limited")
var errDCRCapacity = errors.New("mcpgw: DCR capacity")

// DynamicStore deliberately does not share the short OAuth grant capacity.
// Admission charges attempts even when metadata is later rejected.
type DynamicStore interface {
	Admit(context.Context, string) error
	Register(context.Context, string, Client) (Client, time.Time, time.Time, error)
	Resolve(context.Context, string) (Client, error)
	Redeemed(context.Context, string, extshared.Principal) error
	Prune(context.Context) error
}

type SQLDynamicClients struct {
	db       *repo.Pool
	access   repo.AccessCtx
	resource string
	aead     cipher.AEAD
	hmacKey  []byte
	boundTTL time.Duration
}

func NewSQLDynamicClients(ctx context.Context, db *repo.Pool, access repo.AccessCtx, resource string, key []byte, boundTTL time.Duration) (*SQLDynamicClients, error) {
	policy := DCRPolicy{BoundTTL: boundTTL}
	if e := policy.validate(); e != nil {
		return nil, e
	}
	if db == nil || resource == "" || len(key) != 32 {
		return nil, errors.New("mcpgw: DCR database/key missing")
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	for _, query := range []string{
		`SELECT resource,client_id,sealed,source_hash,created_ns,expires_ns,bound FROM mcpgw_dcr_client WHERE false`,
		`SELECT resource,client_id,principal_hash,redeemed_ns,expires_ns FROM mcpgw_dcr_binding WHERE false`,
		`SELECT resource,bucket,tokens,updated_ns FROM mcpgw_dcr_bucket WHERE false`,
	} {
		rows, e := db.Query(ctx, access, query)
		if e != nil {
			return nil, errors.New("mcpgw: DCR migration missing")
		}
		rows.Close()
	}
	return &SQLDynamicClients{db: db, access: access, resource: resource, aead: a, hmacKey: append([]byte(nil), key...), boundTTL: policy.BoundTTL}, nil
}
func (s *SQLDynamicClients) digest(domain, value string) string {
	h := hmac.New(sha256.New, s.hmacKey)
	h.Write([]byte("dingwei/mcp-dcr/" + domain + "\x00" + s.resource + "\x00" + value))
	return hex.EncodeToString(h.Sum(nil))
}
func (s *SQLDynamicClients) transaction(ctx context.Context, fn func(context.Context, repo.Tx, int64) error) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.db.InTx(ctx, s.access, nil, func(tx repo.Tx) error {
		if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "mcpgw-dcr/"+s.resource); e != nil {
			return e
		}
		var now int64
		if e := tx.QueryRow(ctx, `SELECT (extract(epoch FROM clock_timestamp())*1000000000)::bigint`).Scan(&now); e != nil {
			return e
		}
		return fn(ctx, tx, now)
	})
}
func (s *SQLDynamicClients) prune(ctx context.Context, tx repo.Tx, now int64) error {
	// A five-minute grace keeps snapshots created before registration expiry
	// redeemable without reviving a client through unauthenticated reads.
	// Delete children first, then child-free parents. ON DELETE CASCADE alone
	// would make a 256-client batch unbounded for a widely shared client ID.
	_, e := tx.Exec(ctx, `DELETE FROM mcpgw_dcr_binding WHERE (resource,client_id,principal_hash) IN (SELECT resource,client_id,principal_hash FROM mcpgw_dcr_binding WHERE resource=$1 AND expires_ns<=$2 ORDER BY expires_ns,client_id,principal_hash LIMIT 256)`, s.resource, now-int64(oauthTTL))
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `DELETE FROM mcpgw_dcr_client WHERE (resource,client_id) IN (SELECT resource,client_id FROM mcpgw_dcr_client c WHERE resource=$1 AND expires_ns<=$2 AND NOT EXISTS(SELECT 1 FROM mcpgw_dcr_binding b WHERE b.resource=c.resource AND b.client_id=c.client_id) ORDER BY expires_ns,client_id LIMIT 256)`, s.resource, now-int64(oauthTTL))
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `DELETE FROM mcpgw_dcr_bucket WHERE (resource,bucket) IN (SELECT resource,bucket FROM mcpgw_dcr_bucket WHERE resource=$1 AND bucket<>'global' AND updated_ns<=$2 ORDER BY updated_ns,bucket LIMIT 256)`, s.resource, now-int64(2*time.Hour))
	return e
}
func (s *SQLDynamicClients) Prune(ctx context.Context) error {
	return s.transaction(ctx, func(ctx context.Context, tx repo.Tx, now int64) error { return s.prune(ctx, tx, now) })
}
func (s *SQLDynamicClients) bucket(ctx context.Context, tx repo.Tx, key string, rate, burst float64, now int64) (bool, error) {
	var tokens float64
	var updated int64
	e := tx.QueryRow(ctx, `SELECT tokens,updated_ns FROM mcpgw_dcr_bucket WHERE resource=$1 AND bucket=$2`, s.resource, key).Scan(&tokens, &updated)
	if errors.Is(e, sql.ErrNoRows) {
		tokens = burst
		updated = now
	} else if e != nil {
		return false, e
	}
	if now > updated {
		tokens = math.Min(burst, tokens+float64(now-updated)/float64(time.Minute)*rate)
		updated = now
	}
	allowed := tokens >= 1
	if allowed {
		tokens--
	}
	_, e = tx.Exec(ctx, `INSERT INTO mcpgw_dcr_bucket(resource,bucket,tokens,updated_ns) VALUES($1,$2,$3,$4) ON CONFLICT(resource,bucket) DO UPDATE SET tokens=excluded.tokens,updated_ns=excluded.updated_ns`, s.resource, key, tokens, updated)
	return allowed, e
}
func (s *SQLDynamicClients) Admit(ctx context.Context, source string) error {
	return s.transaction(ctx, func(ctx context.Context, tx repo.Tx, now int64) error {
		if e := s.prune(ctx, tx, now); e != nil {
			return e
		}
		ok, e := s.bucket(ctx, tx, "global", 60, 20, now)
		if e != nil {
			return e
		}
		if !ok {
			return &repo.CommitErr{Err: errDCRRate}
		}
		key := "source/" + s.digest("source", source)
		var count int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM mcpgw_dcr_bucket WHERE resource=$1 AND bucket<>'global'`, s.resource).Scan(&count); e != nil {
			return e
		}
		if count >= 4096 {
			var exists bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mcpgw_dcr_bucket WHERE resource=$1 AND bucket=$2)`, s.resource, key).Scan(&exists); e != nil {
				return e
			}
			if !exists {
				return &repo.CommitErr{Err: errDCRCapacity}
			}
		}
		ok, e = s.bucket(ctx, tx, key, 5, 5, now)
		if e != nil {
			return e
		}
		if !ok {
			return &repo.CommitErr{Err: errDCRRate}
		}
		return nil
	})
}
func (s *SQLDynamicClients) Register(ctx context.Context, source string, c Client) (Client, time.Time, time.Time, error) {
	var created, expiry time.Time
	id, e := randomValue()
	if e != nil {
		return c, created, expiry, e
	}
	c.ID = dcrPrefix + id
	payload, e := json.Marshal(c)
	if e != nil {
		return c, created, expiry, e
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e = io.ReadFull(rand.Reader, nonce); e != nil {
		return c, created, expiry, e
	}
	sealed := s.aead.Seal(nonce, nonce, payload, []byte("dcr/"+s.resource+"/"+c.ID))
	sourceHash := s.digest("source", source)
	e = s.transaction(ctx, func(ctx context.Context, tx repo.Tx, now int64) error {
		if e := s.prune(ctx, tx, now); e != nil {
			return e
		}
		var total, own int
		if e := tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE source_hash=$2) FROM mcpgw_dcr_client WHERE resource=$1 AND expires_ns>$3`, s.resource, sourceHash, now).Scan(&total, &own); e != nil {
			return e
		}
		if total >= 4096 || own >= 128 {
			return errDCRCapacity
		}
		created = time.Unix(0, now).UTC()
		expiry = created.Add(dcrUnboundTTL)
		_, e := tx.Exec(ctx, `INSERT INTO mcpgw_dcr_client(resource,client_id,sealed,source_hash,created_ns,expires_ns) VALUES($1,$2,$3,$4,$5,$6)`, s.resource, c.ID, sealed, sourceHash, now, expiry.UnixNano())
		return e
	})
	return c, created, expiry, e
}
func (s *SQLDynamicClients) Resolve(ctx context.Context, id string) (Client, error) {
	var c Client
	var sealed []byte
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	row, e := s.db.QueryRow(ctx, s.access, `SELECT sealed FROM mcpgw_dcr_client WHERE resource=$1 AND client_id=$2 AND expires_ns>(extract(epoch FROM clock_timestamp())*1000000000)::bigint AND (NOT bound OR EXISTS(SELECT 1 FROM mcpgw_dcr_binding b WHERE b.resource=mcpgw_dcr_client.resource AND b.client_id=mcpgw_dcr_client.client_id AND b.expires_ns>(extract(epoch FROM clock_timestamp())*1000000000)::bigint))`, s.resource, id)
	if e != nil {
		return c, e
	}
	if e = row.Scan(&sealed); e != nil {
		return c, errRecord
	}
	if len(sealed) < s.aead.NonceSize() {
		return c, errRecord
	}
	raw, e := s.aead.Open(nil, sealed[:s.aead.NonceSize()], sealed[s.aead.NonceSize():], []byte("dcr/"+s.resource+"/"+id))
	if e != nil {
		return c, errRecord
	}
	if json.Unmarshal(raw, &c) != nil || c.ID != id || c.AuthMethod != "none" {
		return Client{}, errRecord
	}
	return c, nil
}
func (s *SQLDynamicClients) Redeemed(ctx context.Context, id string, p extshared.Principal) error {
	if p.Provider == "" || p.TenantKey == "" || p.Issuer == "" || p.Subject == "" {
		return errRecord
	}
	// JSON tuple avoids ambiguous delimiter-based identities. Never seat ID.
	key, _ := json.Marshal([]string{p.Provider, p.TenantKey, p.Issuer, p.Subject})
	subject := s.digest("principal", string(key))
	return s.transaction(ctx, func(ctx context.Context, tx repo.Tx, now int64) error {
		var present bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mcpgw_dcr_client WHERE resource=$1 AND client_id=$2 AND expires_ns>$3)`, s.resource, id, now-int64(oauthTTL)).Scan(&present); e != nil {
			return e
		}
		if !present {
			return errRecord
		}
		expiry := now + int64(s.boundTTL)
		if _, e := tx.Exec(ctx, `INSERT INTO mcpgw_dcr_binding(resource,client_id,principal_hash,redeemed_ns,expires_ns) VALUES($1,$2,$3,$4,$5) ON CONFLICT(resource,client_id,principal_hash) DO UPDATE SET redeemed_ns=excluded.redeemed_ns,expires_ns=excluded.expires_ns`, s.resource, id, subject, now, expiry); e != nil {
			return e
		}
		// RETURNING identifies at most the evicted associations. Recompute only
		// their clients, excluding the removed subject (a modifying CTE's table
		// reads use the pre-delete snapshot). Keep five minutes for old grants.
		if _, e := tx.Exec(ctx, `WITH removed AS (DELETE FROM mcpgw_dcr_binding WHERE resource=$1 AND principal_hash=$2 AND client_id IN (SELECT client_id FROM mcpgw_dcr_binding WHERE resource=$1 AND principal_hash=$2 AND expires_ns>$3 ORDER BY redeemed_ns DESC,(client_id=$4) DESC,client_id DESC OFFSET 16) RETURNING client_id) UPDATE mcpgw_dcr_client c SET expires_ns=GREATEST($3,COALESCE((SELECT max(b.expires_ns) FROM mcpgw_dcr_binding b WHERE b.resource=c.resource AND b.client_id=c.client_id AND b.principal_hash<>$2),$3)) WHERE c.resource=$1 AND c.bound AND c.client_id IN (SELECT client_id FROM removed)`, s.resource, subject, now, id); e != nil {
			return e
		}
		// Eviction of one person's association cannot delete another person's client.
		_, e := tx.Exec(ctx, `UPDATE mcpgw_dcr_client c SET bound=true,expires_ns=COALESCE((SELECT max(expires_ns) FROM mcpgw_dcr_binding b WHERE b.resource=c.resource AND b.client_id=c.client_id),$3) WHERE c.resource=$1 AND c.client_id=$2`, s.resource, id, expiry)
		return e
	})
}
