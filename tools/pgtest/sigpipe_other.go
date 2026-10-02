//go:build !unix

package main

// ignoreSigPipe 在没有 SIGPIPE 的平台上无事可做（Windows）。
//
// 🔴 不得读成「非 unix 平台上收尾在破裂管道下也验证过」—— 没验证过。
// 那一档本来就与本工具的其它平台桩一致（见 reap_other.go：不设 subreaper、不收尸、不杀进程），
// 理由是那些平台的 init 会正常收尸，没有「留成永久僵尸」这条缺陷。
func ignoreSigPipe() {}
