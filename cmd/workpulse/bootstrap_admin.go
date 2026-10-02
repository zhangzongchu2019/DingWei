package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// 初始化脚本：建新平台第一个管理用户（业主裁定，台账 8971bddf：取消常驻 systemdefault 与空库首启自动建号）。
// 运维在 Hub 所在主机执行，连 Hub 用的同一个 PG 库（DW_NEWPLAT_PG_DSN）；用户 ID 与简称由执行者给。
// 输出一次性初始口令（只此一次；首登强制改密），由执行者经消息以外的渠道交业主。
// 库中已有管理用户（platform_admin 或 manager，不论状态）即拒绝执行，什么也不改。

const bootstrapAdminUsage = "workpulse bootstrap-admin --user-id <ID> --short-name <short> [--display-name <name>]"

func runBootstrapAdmin(ctx context.Context, args []string, out io.Writer, dsn string) error {
	fs := flag.NewFlagSet("bootstrap-admin", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	userID := fs.String("user-id", "", "")
	shortName := fs.String("short-name", "", "")
	displayName := fs.String("display-name", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *userID == "" || *shortName == "" {
		return errors.New("usage: " + bootstrapAdminUsage)
	}
	store, _, closeStore, err := openNewPlatformStore(ctx, dsn)
	if err != nil {
		return fmt.Errorf("bootstrap-admin: %w", err)
	}
	defer closeStore()
	pw, err := identity.NewOnboarding(identity.NewService(store, nil, nil)).BootstrapAdmin(ctx, *userID, *shortName, *displayName)
	switch {
	case errors.Is(err, identity.ErrAdminExists):
		return errors.New("bootstrap-admin refused: a management user already exists; nothing changed")
	case errors.Is(err, identity.ErrBadRequest):
		return errors.New("bootstrap-admin: --user-id must be 2-32 letters/digits, --short-name 2-16 letters/digits, --display-name at most 64 bytes")
	case errors.Is(err, identity.ErrConflict):
		return errors.New("bootstrap-admin: user id or short name already taken; nothing changed")
	case err != nil:
		return fmt.Errorf("bootstrap-admin: %w", err)
	}
	_, err = fmt.Fprintf(out, "BOOTSTRAP_ADMIN_CREATED user_id=%s short_name=%s\ninitial_password=%s\n"+
		"The password is shown only once and must be changed at first login. Hand it to the owner outside chat.\n",
		*userID, *shortName, pw)
	return err
}
