package fileservice

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Repository struct{ db *sql.DB }

func OpenRepository(path string) (*Repository, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create metadata directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	r := &Repository{db: db}
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS files (
id TEXT PRIMARY KEY, object_key TEXT NOT NULL UNIQUE, owner_key TEXT NOT NULL,
session_name TEXT NOT NULL, original_name TEXT NOT NULL, media_type TEXT NOT NULL,
size_bytes INTEGER NOT NULL, sha256 TEXT NOT NULL, state TEXT NOT NULL,
	created_at TEXT NOT NULL, expires_at TEXT NOT NULL, archived INTEGER NOT NULL DEFAULT 0,
	archived_at TEXT, removed_at TEXT, removal_reason TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS files_ready_fifo ON files(state,created_at,id);`); err != nil {
		db.Close()
		return nil, err
	}
	for _, column := range []struct{ name, ddl string }{
		{"archived", `ALTER TABLE files ADD COLUMN archived INTEGER NOT NULL DEFAULT 0`},
		{"archived_at", `ALTER TABLE files ADD COLUMN archived_at TEXT`},
	} {
		present, checkErr := sqliteColumnExists(db, "files", column.name)
		if checkErr != nil {
			db.Close()
			return nil, checkErr
		}
		if !present {
			if _, err = db.Exec(column.ddl); err != nil {
				db.Close()
				return nil, err
			}
		}
	}
	return r, nil
}

func sqliteColumnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (r *Repository) Close() error { return r.db.Close() }

func (r *Repository) InsertReady(ctx context.Context, f File) error {
	if f.State != StateReady {
		return fmt.Errorf("insert requires ready state")
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO files(id,object_key,owner_key,session_name,original_name,media_type,size_bytes,sha256,state,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		f.ID, f.ObjectKey, f.OwnerKey, f.SessionName, f.OriginalName, f.MediaType, f.SizeBytes, f.SHA256, string(f.State), stamp(f.CreatedAt), stamp(f.ExpiresAt))
	return err
}

func (r *Repository) Get(ctx context.Context, id string) (File, error) {
	var f File
	var created, expires string
	var removed sql.NullString
	var archived int
	var archivedAt sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT id,object_key,owner_key,session_name,original_name,media_type,size_bytes,sha256,state,created_at,expires_at,archived,archived_at,removed_at,removal_reason FROM files WHERE id=?`, id).Scan(
		&f.ID, &f.ObjectKey, &f.OwnerKey, &f.SessionName, &f.OriginalName, &f.MediaType, &f.SizeBytes, &f.SHA256, &f.State, &created, &expires, &archived, &archivedAt, &removed, &f.RemovalReason)
	if err != nil {
		return File{}, err
	}
	f.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return File{}, err
	}
	f.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return File{}, err
	}
	f.Archived = archived != 0
	if archivedAt.Valid {
		t, e := time.Parse(time.RFC3339Nano, archivedAt.String)
		if e != nil {
			return File{}, e
		}
		f.ArchivedAt = &t
	}
	if removed.Valid {
		t, e := time.Parse(time.RFC3339Nano, removed.String)
		if e != nil {
			return File{}, e
		}
		f.RemovedAt = &t
	}
	return f, nil
}

func (r *Repository) ReadyBytes(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size_bytes),0) FROM files WHERE state='ready'`).Scan(&n)
	return n, err
}

// ListStable backs the authenticated internal audit endpoint. Its ordering is
// part of the acceptance contract, not an implementation detail.
func (r *Repository) ListStable(ctx context.Context, state State, limit int) ([]File, error) {
	if limit <= 0 || limit > 1000 {
		return nil, fmt.Errorf("limit must be between 1 and 1000")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM files WHERE (?='' OR state=?) ORDER BY created_at,id LIMIT ?`, string(state), string(state), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]File, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		f, err := r.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
func (r *Repository) RemovalCandidates(ctx context.Context, now time.Time, quota int64) ([]File, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM files WHERE state='ready' AND archived=0 ORDER BY CASE WHEN expires_at<=? THEN 0 ELSE 1 END,created_at,id`, stamp(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	var total int64
	if total, err = r.ReadyBytes(ctx); err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		f, e := r.Get(ctx, id)
		if e != nil {
			return nil, e
		}
		if !f.ExpiresAt.After(now) || total > quota {
			out = append(out, f)
			total -= f.SizeBytes
		}
	}
	return out, rows.Err()
}

func (r *Repository) ListDeleting(ctx context.Context) ([]File, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM files WHERE state='deleting' ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		f, err := r.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// MarkDeleting durably records intent before touching object storage. A crash
// or metadata failure after Delete is therefore recoverable by the next sweep.
func (r *Repository) MarkDeleting(ctx context.Context, id, reason string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE files SET state='deleting',removal_reason=? WHERE id=? AND state='ready'`, reason, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("file is not ready for deletion")
	}
	return nil
}

func (r *Repository) FinishDeleting(ctx context.Context, id string, at time.Time) error {
	var reason string
	if err := r.db.QueryRowContext(ctx, `SELECT removal_reason FROM files WHERE id=? AND state='deleting'`, id).Scan(&reason); err != nil {
		return err
	}
	state := StateEvicted
	if reason == "retention" {
		state = StateExpired
	}
	result, err := r.db.ExecContext(ctx, `UPDATE files SET state=?,removed_at=? WHERE id=? AND state='deleting'`, string(state), stamp(at), id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("deletion state changed")
	}
	return nil
}

func (r *Repository) SetArchived(ctx context.Context, id string, archived bool, at time.Time) error {
	value := 0
	var archivedAt any
	if archived {
		value, archivedAt = 1, stamp(at)
	}
	result, err := r.db.ExecContext(ctx, `UPDATE files SET archived=?,archived_at=? WHERE id=? AND state='ready'`, value, archivedAt, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("file is not ready for archive change")
	}
	return nil
}

func (r *Repository) PurgeTombstones(ctx context.Context, before time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM files WHERE state IN ('expired','evicted') AND removed_at IS NOT NULL AND removed_at<?`, stamp(before))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
func (r *Repository) Tombstone(ctx context.Context, id string, state State, at time.Time, reason string) error {
	if state != StateEvicted && state != StateExpired {
		return fmt.Errorf("invalid tombstone state")
	}
	_, err := r.db.ExecContext(ctx, `UPDATE files SET state=?,removed_at=?,removal_reason=? WHERE id=? AND state='ready'`, string(state), stamp(at), reason, id)
	return err
}
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
