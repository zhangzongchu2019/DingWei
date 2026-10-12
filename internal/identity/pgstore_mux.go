package identity

import (
	"context"
	"database/sql"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/consolemux"
	"time"
)

// ConsoleMuxSlots reuses the identity store's platform access context. Slot SQL
// still explicitly scopes every statement to principal and canonical origin.
func (s *PGStore) ConsoleMuxSlots(b *consolemux.Budget) consolemux.Slots {
	return consolemux.NewPGSlots(s.pool, s.t, b)
}

// AuthorizeConsoleMux checks fresh primary authority, including expiry using
// database time. Its caller accounts the entire round in the shared DB budget.
func (s *PGStore) AuthorizeConsoleMux(ctx context.Context, hash string, names []string) (consolemux.Authorization, error) {
	start := time.Now()
	out := consolemux.Authorization{Agents: map[string]consolemux.Agent{}}
	if err := muxWorkDeadline(ctx); err != nil {
		return out, err
	}
	row, err := s.queryRow(ctx, `SELECT s.user_id_lc,s.expires_at,clock_timestamp()
 FROM np_session s JOIN np_user u ON u.user_id_lc=s.user_id_lc
 WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()
 AND u.state='active' AND NOT u.must_change_password`, hash)
	if err != nil {
		return out, err
	}
	var expiry, dbNow time.Time
	if err := row.Scan(&out.Principal, &expiry, &dbNow); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, consolemux.ErrDenied
		}
		return out, err
	}
	out.Expiry = start.Add(expiry.Sub(dbNow))
	if len(names) == 0 {
		return out, nil
	}
	if err := muxWorkDeadline(ctx); err != nil {
		return out, err
	}
	rows, err := s.pool.Query(ctx, s.t, `SELECT a.id,a.owner_lc,lower(a.name),
 lower(a.name)||'.'||lower(u.short_name)
 FROM np_agent a JOIN np_user u ON u.user_id_lc=a.owner_lc
 WHERE a.owner_lc=$1 AND a.state='active'
 AND lower(a.name)||'.'||lower(u.short_name)=ANY($2::text[])`, out.Principal, names)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var a consolemux.Agent
		if err := rows.Scan(&a.ID, &a.Owner, &a.Session, &a.FullName); err != nil {
			return out, err
		}
		out.Agents[a.FullName] = a
	}
	return out, rows.Err()
}

func muxWorkDeadline(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d, ok := ctx.Deadline(); ok && !time.Now().Before(d) {
		return context.DeadlineExceeded
	}
	return nil
}
