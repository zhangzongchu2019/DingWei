package store

import "context"

// ShutdownSnapshot is captured before closing SQLite; no message content leaves
// the repository. Durable queued work is allowed and must not be cleared merely
// to make shutdown look empty.
func (s *SQLite) ShutdownSnapshot(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{}
	for _, query := range []struct{ prefix, sql string }{{"message_", "SELECT status, COUNT(*) FROM message GROUP BY status"}, {"delivery_", "SELECT state, COUNT(*) FROM session_delivery GROUP BY state"}} {
		rows, err := s.db.QueryContext(ctx, query.sql)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var state string
			var count int64
			if err := rows.Scan(&state, &count); err != nil {
				rows.Close()
				return nil, err
			}
			out[query.prefix+state] = count
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
