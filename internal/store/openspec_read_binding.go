package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
)

func (s *SQLite) SpecReadBinding(ctx context.Context, principal, provider, instance string) (*specread.Binding, error) {
	b := specread.Binding{Principal: principal, Provider: provider, Instance: instance}
	err := s.db.QueryRowContext(ctx, `SELECT b.account_id,b.login FROM openspec_read_binding b JOIN console_principal p ON p.id=b.principal WHERE b.principal=? AND b.provider=? AND b.instance=? AND p.active=1`, principal, provider, instance).Scan(&b.AccountID, &b.Login)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}
func (s *SQLite) SetSpecReadBinding(ctx context.Context, actor string, b specread.Binding) error {
	if actor == "" || !b.Valid() {
		return specread.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO openspec_read_binding(principal,provider,instance,account_id,login,updated_by,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(principal,provider,instance) DO UPDATE SET account_id=excluded.account_id,login=excluded.login,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, b.Principal, b.Provider, b.Instance, b.AccountID, b.Login, actor, s.now())
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO openspec_read_binding_audit(actor,principal,provider,instance,account_id,login,operation,created_at) VALUES(?,?,?,?,?,?,?,?)`, actor, b.Principal, b.Provider, b.Instance, b.AccountID, b.Login, "set", s.now())
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *SQLite) DeleteSpecReadBinding(ctx context.Context, actor, principal, provider, instance string) error {
	if actor == "" || principal == "" || provider != "github" || instance != "github.com" {
		return specread.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO openspec_read_binding_audit(actor,principal,provider,instance,account_id,login,operation,created_at) SELECT ?,principal,provider,instance,account_id,login,'delete',? FROM openspec_read_binding WHERE principal=? AND provider=? AND instance=?`, actor, s.now(), principal, provider, instance)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM openspec_read_binding WHERE principal=? AND provider=? AND instance=?`, principal, provider, instance)
	if err != nil {
		return err
	}
	return tx.Commit()
}
