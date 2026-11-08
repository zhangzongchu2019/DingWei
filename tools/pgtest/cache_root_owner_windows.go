//go:build windows

package main

import (
	"errors"
	"os"
)

func privateTempCacheRoot() (string, error) {
	return "", errors.New("Windows 上无法构造并验证 uid 隔离的 PG 临时缓存目录")
}

func checkCacheRootOwner(os.FileInfo, string) error {
	return errors.New("Windows 上无法验证 PG 临时缓存目录属主")
}
