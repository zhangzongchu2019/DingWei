package version

import (
	"crypto/sha256"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// A coherent rollback of all three version lines must not silently turn the
// approved release back into the prior release.
func TestHelper02319ReleasePins(t *testing.T) {
	const release = "0.2.3.19"
	root, err := os.ReadFile("../../VERSION")
	if err != nil {
		t.Fatal(err)
	}
	helper, err := os.ReadFile("../../tools/sessionhelper/sessionhelper/__init__.py")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`__version__\s*=\s*"([0-9.]+)"`).FindSubmatch(helper)
	if strings.TrimSpace(string(root)) != release || Version != release || len(m) != 2 || string(m[1]) != release {
		t.Fatal("0.2.3.19 r44 integration requires root/Go/helper pins; 0.2.3.18 is the prior release")
	}
	lock, err := os.ReadFile("../../tools/sessionhelper/requirements-runtime.lock")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(lock)) != "5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0" {
		t.Fatal("runtime lock changed; r43 version bump cannot change dependencies")
	}
}
