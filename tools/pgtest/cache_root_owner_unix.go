//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func privateTempCacheRoot() (string, error) {
	return filepath.Join(os.TempDir(), fmt.Sprintf("dingwei-pgtest-cache-%d", os.Getuid())), nil
}

func checkCacheRootOwner(info os.FileInfo, path string) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("PG 临时缓存目录属主不是当前 uid=%d: %s", os.Getuid(), path)
	}
	return nil
}
