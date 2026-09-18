package filepolicy

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestUploadLimitMatchesNginx(t *testing.T) {
	if MaxUploadBytes != 52_428_800 {
		t.Fatalf("MaxUploadBytes=%d", MaxUploadBytes)
	}
	raw, err := os.ReadFile("../../deploy/nginx-dingwei.conf")
	if err != nil {
		t.Fatal(err)
	}
	const directive = "client_max_body_size 50m;"
	if count := strings.Count(string(raw), directive); count != 1 {
		t.Fatalf("%q count=%d", directive, count)
	}
}

func TestDefaultRetentionMatchesContract(t *testing.T) {
	if DefaultRetention != 7*24*time.Hour {
		t.Fatalf("DefaultRetention=%s", DefaultRetention)
	}
}
