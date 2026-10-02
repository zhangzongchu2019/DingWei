package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// 运维重置口令（组长令 2026-10-01：「忘记口令」做小工具）：运维在 Hub 主机上执行，连 Hub 用的同一个 PG 库（DW_NEWPLAT_PG_DSN）。
// 只认库里存在、且本就用口令登录的用户；输出一次性新口令（只此一次），置必须改密，吊销该用户全部会话，写一笔审计（不含口令）。
// 新口令由执行者经消息以外的渠道交给本人。

const resetPasswordUsage = "workpulse reset-password --user-id <ID>"

func runResetPassword(ctx context.Context, args []string, out io.Writer, dsn string) error {
	fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	userID := fs.String("user-id", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *userID == "" {
		return errors.New("usage: " + resetPasswordUsage)
	}
	store, _, closeStore, err := openNewPlatformStore(ctx, dsn)
	if err != nil {
		return fmt.Errorf("reset-password: %w", err)
	}
	defer closeStore()
	pw, err := identity.NewOnboarding(identity.NewService(store, nil, nil)).ResetPassword(ctx, *userID)
	switch {
	case errors.Is(err, identity.ErrNotFound):
		return errors.New("reset-password refused: no such user; nothing changed")
	case errors.Is(err, identity.ErrNoPasswordLogin):
		return errors.New("reset-password refused: this user logs in via Feishu, not with a password; nothing changed")
	case err != nil:
		return fmt.Errorf("reset-password: %w", err)
	}
	_, err = fmt.Fprintf(out, "PASSWORD_RESET user_id=%s\nnew_password=%s\n"+
		"All sessions of this user were revoked. The password is shown only once and must be changed at first login. Hand it over outside chat.\n",
		*userID, pw)
	return err
}
