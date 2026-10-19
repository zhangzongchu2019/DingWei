package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// 运维提升平台管理者（R6）：运维在 Hub 主机上执行，连 Hub 用的同一个 PG 库（DW_NEWPLAT_PG_DSN）。
// --user 先按 user_id、再按 short_name 找；目标须存在且 active，且尚不是 platform_admin。
// 一个事务里改类型并写一笔审计（actor=promote-admin，detail 记原类型）；不动口令、不动会话、不输出任何秘密。

const promoteAdminUsage = "workpulse promote-admin --user <user_id or short_name>"

func runPromoteAdmin(ctx context.Context, args []string, out io.Writer, dsn string) error {
	fs := flag.NewFlagSet("promote-admin", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	user := fs.String("user", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *user == "" {
		return errors.New("usage: " + promoteAdminUsage)
	}
	store, _, closeStore, err := openNewPlatformStore(ctx, dsn)
	if err != nil {
		return fmt.Errorf("promote-admin: %w", err)
	}
	defer closeStore()
	u, old, err := identity.NewOnboarding(identity.NewService(store, nil, nil)).PromoteToPlatformAdmin(ctx, *user)
	switch {
	case errors.Is(err, identity.ErrNotFound):
		return errors.New("promote-admin refused: no such user; nothing changed")
	case errors.Is(err, identity.ErrConflict):
		return errors.New("promote-admin refused: the value matches one user's user_id and another user's short_name; pass the exact user_id; nothing changed")
	case errors.Is(err, identity.ErrUserNotActive):
		return errors.New("promote-admin refused: user is not active (disabled or not yet bound); nothing changed")
	case errors.Is(err, identity.ErrAlreadyPlatformAdmin):
		return errors.New("promote-admin refused: user is already platform_admin; nothing changed")
	case err != nil:
		return fmt.Errorf("promote-admin: %w", err)
	}
	_, err = fmt.Fprintf(out, "PROMOTED_PLATFORM_ADMIN user_id=%s short_name=%s old_type=%s\n", u.UserID, u.ShortName, old)
	return err
}
