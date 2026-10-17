package nodeagent

import (
	"strings"
	"testing"
)

func TestSeatCapacityMinimumConfiguration(t *testing.T) {
	p, err := ParseNodePolicy(func(string) string { return "" })
	if err != nil || p.Mode != "hard" || p.MinimumSeatCPUMillicores != 1000 || p.MinimumSeatMemoryBytes != 1536<<20 || p.MinimumSeatDiskBytes != 40<<30 {
		t.Fatalf("defaults %+v: %v", p, err)
	}
	for key, value := range map[string]string{"CPU_MILLICORES": "2000", "MEMORY_BYTES": "2147483648", "DISK_BYTES": "85899345920"} {
		name := "NODEAGENT_MINIMUM_SEAT_" + key
		changed, err := ParseNodePolicy(func(k string) string {
			if k == name {
				return value
			}
			return ""
		})
		if err != nil || changed.Revision() == p.Revision() {
			t.Fatalf("%s not bound to policy revision: %+v %v", name, changed, err)
		}
		for _, bad := range []string{"0", "-1", "1.5", "9223372036854775808"} {
			if _, err := ParseNodePolicy(func(k string) string {
				if k == name {
					return bad
				}
				return ""
			}); err == nil || strings.Contains(err.Error(), bad) {
				t.Fatalf("%s invalid value did not fail closed with bounded error: %v", name, err)
			}
		}
	}
}
