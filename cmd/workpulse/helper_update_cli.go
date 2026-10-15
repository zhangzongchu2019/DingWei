package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/config"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

const helperUpdateRegisterUsage = "workpulse helper-update-register --operator <ops-id> --sha256 <sha256> --lock-sha256 <sha256> --version <version> --url <artifact-url> --file-service-origin <origin>"

// runHelperUpdateRegister is deliberately a Hub-host operator command. Console
// routes may select only entries created here and never accept arbitrary URLs.
func runHelperUpdateRegister(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("helper-update-register", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	operator := fs.String("operator", "", "")
	sha := fs.String("sha256", "", "")
	lock := fs.String("lock-sha256", "", "")
	version := fs.String("version", "", "")
	url := fs.String("url", "", "")
	fileServiceOrigin := fs.String("file-service-origin", "", "")
	platform := fs.String("platform", "linux", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || strings.TrimSpace(*operator) == "" {
		return errors.New("usage: " + helperUpdateRegisterUsage)
	}
	bs := config.LoadBootstrap()
	db, err := store.OpenSQLite(bs.DBPath)
	if err != nil {
		return fmt.Errorf("helper-update-register: %w", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("helper-update-register: %w", err)
	}
	hub := m8.New(db, m8.ConsoleAuthMode{})
	defer hub.Close()
	if err := hub.RegisterHelperUpdateCatalog(ctx, *operator, m8.HelperUpdateCatalog{
		SHA256: *sha, LockSHA256: *lock, Version: *version, URL: *url, FileServiceOrigin: *fileServiceOrigin, Platform: *platform,
	}); err != nil {
		return fmt.Errorf("helper-update-register: %w", err)
	}
	_, err = fmt.Fprintf(out, "HELPER_UPDATE_CATALOG_REGISTERED sha256=%s version=%s\n", strings.ToLower(strings.TrimSpace(*sha)), strings.TrimSpace(*version))
	return err
}
