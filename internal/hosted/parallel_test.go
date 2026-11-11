package hosted

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// Six independent fixture owners overlap PG startup/cleanup without starting
// one server for every queued test. Hold the slot through all child tests and
// their cleanups: the original pgtest residue assertions remain active.
var hostedTestSlots = make(chan struct{}, 6)
var hostedPGWarm sync.Once

func parallelHostedTest(t *testing.T) {
	t.Helper()
	// An explicitly supplied DSN may refer to one shared database. Retain the
	// original serial semantics there; embedded fixtures have isolated databases.
	if strings.TrimSpace(os.Getenv(pgtest.DSNEnv)) != "" {
		return
	}
	t.Parallel()
	hostedTestSlots <- struct{}{}
	t.Cleanup(func() { <-hostedTestSlots })
}
