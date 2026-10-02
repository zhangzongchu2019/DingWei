package nodeagent

import (
	"errors"
	"testing"
)

// M1 返修 阻断1（挂载点）：findmnt --target 真实解析，.68 正反用例。
func TestResolveMountPointNode68(t *testing.T) {
	orig := findmntTarget
	defer func() { findmntTarget = orig }()
	findmntTarget = func(path string) (string, error) {
		switch path {
		case "/data1/dingwei-node68/agents":
			return "/data1/dingwei-node68", nil
		case "/data/dingwei-newplat/agents":
			return "/data", nil
		default:
			return "", errors.New("findmnt: 无挂载点")
		}
	}
	m, err := resolveMountPoint("/data1/dingwei-node68/agents")
	if err != nil || m != "/data1/dingwei-node68" {
		t.Fatalf(".68 应解析到 /data1/dingwei-node68, 得 %q err=%v", m, err)
	}
	if _, err := resolveMountPoint("/nonexistent/agents"); err == nil {
		t.Fatal("无挂载点必须拒（fail-closed，不得错指）")
	}
}

// M1 返修 阻断2（属主 10001）：Setup 建目录即 chown。
func TestSetupChownsSeatDirTo10001(t *testing.T) {
	orig := chownForSeatFn
	defer func() { chownForSeatFn = orig }()
	var chowned []string
	chownForSeatFn = func(path string) error {
		chowned = append(chowned, path)
		return nil
	}
	root := t.TempDir()
	q := xfsQuota{}
	if err := q.Setup(root, "ag_c1", 0); err != nil {
		t.Fatalf("setup: %v", err)
	}
	want := seatDir(root, "ag_c1")
	if len(chowned) != 1 || chowned[0] != want {
		t.Fatalf("Setup 必须 chown 席位目录给 10001: %v", chowned)
	}
}
