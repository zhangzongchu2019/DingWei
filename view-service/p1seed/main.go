// Command p1seed 给 P1 真页面脚本（view-service/console-spec-task-p1-real-regression.js）播第二个用户。
//
// 为什么要它：新平台本版只有初始化脚本建的管理用户有口令（见 identity.User.PasswordHash 注释），
// 其余用户只能走飞书登录；而「他人 Agent 拉不到本属主任务」的跨属主格需要第二个能经 HTTP 登录并批准入网的用户。
// 本工具只在测试里用：连同一个临时 PG，按身份存储的正门建一个带 bcrypt 口令、直接 active、无需改密的普通用户。
// 不进任何服务、不碰生产；口令由调用方给、不打印。
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: p1seed <pg-dsn> <user-id> <short-name> <password>")
		os.Exit(2)
	}
	dsn, userID, shortName, password := os.Args[1], os.Args[2], os.Args[3], os.Args[4]
	ctx := context.Background()
	pool, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "p1seed: open pg:", err)
		os.Exit(1)
	}
	defer pool.Close()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintln(os.Stderr, "p1seed: hash:", err)
		os.Exit(1)
	}
	err = identity.NewPGStore(pool).CreateUser(ctx, identity.User{
		UserID: userID, ShortName: shortName, DisplayName: userID, Type: identity.UserMember,
		State: identity.UserActive, PasswordHash: string(hash), MustChangePassword: false, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "p1seed: create user:", err)
		os.Exit(1)
	}
	fmt.Println("P1SEED_USER_CREATED user_id=" + userID)
}
