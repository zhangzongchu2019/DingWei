package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func validateConsolePlatformItem(item model.ConsolePlatformItem) error {
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Kind) == "" ||
		strings.TrimSpace(item.Title) == "" || strings.TrimSpace(item.Href) == "" ||
		strings.TrimSpace(item.SubmittedBy) == "" ||
		(item.Status != "pending" && item.Status != "approved") {
		return errors.New("invalid console platform item")
	}
	return nil
}

func (s *SQLite) CreateConsolePlatformItem(ctx context.Context, item model.ConsolePlatformItem, now time.Time) error {
	if err := validateConsolePlatformItem(item); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO console_platform_item
		(id,kind,title,href,status,submitted_by,active,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		item.ID, item.Kind, item.Title, item.Href, item.Status, item.SubmittedBy, boolInt(item.Active),
		now.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *SQLite) ListConsolePlatformItems(ctx context.Context, includePending bool) ([]model.ConsolePlatformItem, error) {
	query := `SELECT id,kind,title,href,status,submitted_by,active,created_at,updated_at
		FROM console_platform_item WHERE active=1`
	if !includePending {
		query += ` AND status='approved'`
	}
	query += ` ORDER BY updated_at,id`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ConsolePlatformItem
	for rows.Next() {
		var item model.ConsolePlatformItem
		var active int
		var created, updated string
		if err := rows.Scan(&item.ID, &item.Kind, &item.Title, &item.Href, &item.Status, &item.SubmittedBy, &active, &created, &updated); err != nil {
			return nil, err
		}
		item.Active = active != 0
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLite) UpdateConsolePlatformItem(ctx context.Context, itemID, title, href string, now time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE console_platform_item SET title=?,href=?,updated_at=? WHERE id=? AND active=1`,
		strings.TrimSpace(title), strings.TrimSpace(href), now.UTC().Format(time.RFC3339Nano), itemID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *SQLite) ApproveConsolePlatformItem(ctx context.Context, itemID string, now time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE console_platform_item SET status='approved',updated_at=? WHERE id=? AND active=1 AND status='pending'`,
		now.UTC().Format(time.RFC3339Nano), itemID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *SQLite) DeleteConsolePlatformItem(ctx context.Context, itemID string, now time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE console_platform_item SET active=0,updated_at=? WHERE id=? AND active=1`,
		now.UTC().Format(time.RFC3339Nano), itemID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *SQLite) ConsolePlatformItem(ctx context.Context, itemID string) (*model.ConsolePlatformItem, error) {
	var item model.ConsolePlatformItem
	var active int
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,kind,title,href,status,submitted_by,active,created_at,updated_at
		FROM console_platform_item WHERE id=?`, itemID).
		Scan(&item.ID, &item.Kind, &item.Title, &item.Href, &item.Status, &item.SubmittedBy, &active, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.Active = active != 0
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err == nil {
		item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	}
	return &item, err
}
