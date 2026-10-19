package identity

import (
	"context"
	"database/sql"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// AuthorizeHostedNodeTx owns identity interpretation. Row locks serialize
// admission with user disabling, member removal, Agent suspension and deletion.
func AuthorizeHostedNodeTx(ctx context.Context, tx repo.Tx, user, scope string, users, groups []string) (bool, error) {
	var state, role string
	err := tx.QueryRow(ctx, `SELECT state,type FROM np_user WHERE lower(user_id)=$1 FOR SHARE`, user).Scan(&state, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if state != "active" {
		return false, nil
	}
	if role == string(UserPlatformAdmin) {
		return true, nil
	}
	if scope == "public" {
		return true, nil
	}
	if scope != "restricted" {
		return false, nil
	}
	for _, id := range users {
		if id == user {
			return true, nil
		}
	}
	for _, code := range groups {
		var creator string
		err := tx.QueryRow(ctx, `SELECT creator_lc FROM np_group WHERE code=$1 FOR SHARE`, code).Scan(&creator)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		if creator == user {
			return true, nil
		}
		rows, err := tx.Query(ctx, `SELECT a.id FROM np_group_member m JOIN np_agent a ON a.id=m.agent_id WHERE m.code=$1 AND a.owner_lc=$2 AND a.state='active' ORDER BY a.id FOR SHARE OF a,m`, code, user)
		if err != nil {
			return false, err
		}
		ok := rows.Next()
		err = rows.Err()
		rows.Close()
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}
