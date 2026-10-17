package nodeagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ValidateIsolatedSocket is a pre-dial guard. It rejects lexical/symlink aliases
// of the default Engine. Approval of this path is still an operator prerequisite.
func ValidateIsolatedSocket(path string) error {
	return validateIsolatedSocket(path, "/var/run/docker.sock")
}
func validateIsolatedSocket(path, defaultSocket string) error {
	p, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid isolated socket")
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return fmt.Errorf("isolated socket must exist")
	}
	defaultPath, _ := filepath.EvalSymlinks(defaultSocket)
	if p == "/run/docker.sock" || p == "/var/run/docker.sock" || (defaultPath != "" && p == defaultPath) {
		return fmt.Errorf("default Docker socket refused")
	}
	info, err := os.Stat(p)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("isolated socket must be an existing Unix socket")
	}
	return nil
}

// CheckSoftEngine is used before installing any handlers/consumer. It performs
// read-only API inspection, never installation, controller or firewall changes.
// A declared policy is not proof that a target kernel enforces these limits.
func CheckSoftEngine(ctx context.Context, sock, root string, p NodePolicy) error {
	if p.Mode != "soft" {
		return nil
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if err := ValidateIsolatedSocket(sock); err != nil {
		return err
	}
	return checkSoftEngine(ctx, newEngine(sock), root, p)
}
func checkSoftEngine(ctx context.Context, eng *engine, root string, p NodePolicy) error {
	var info struct {
		ID            string `json:"ID"`
		Root          string `json:"DockerRootDir"`
		CgroupDriver  string `json:"CgroupDriver"`
		CgroupVersion string `json:"CgroupVersion"`
		MemoryLimit   bool   `json:"MemoryLimit"`
		SwapLimit     bool   `json:"SwapLimit"`
		CpuCfsPeriod  bool   `json:"CpuCfsPeriod"`
		CpuCfsQuota   bool   `json:"CpuCfsQuota"`
		PidsLimit     bool   `json:"PidsLimit"`
	}
	if err := eng.call(ctx, "GET", "/info", nil, &info); err != nil {
		return fmt.Errorf("isolated Engine inspection failed")
	}
	if info.ID != p.EngineID || filepath.Clean(info.Root) != p.EngineRoot {
		return fmt.Errorf("isolated Engine identity/root mismatch")
	}
	if info.CgroupDriver != "systemd" || info.CgroupVersion != "2" || !info.MemoryLimit || !info.SwapLimit || !info.CpuCfsPeriod || !info.CpuCfsQuota || !info.PidsLimit {
		return fmt.Errorf("required cgroup hard limits unavailable")
	}
	// This implementation accounts one shared filesystem. Separate backing pools
	// require their own measured reserve contract and cannot be silently ignored.
	var a, b syscall.Stat_t
	if syscall.Stat(root, &a) != nil || syscall.Stat(p.EngineRoot, &b) != nil || a.Dev != b.Dev {
		return fmt.Errorf("seat/Engine filesystem accounting mismatch")
	}
	return nil
}
