package identity_test

import (
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/identitytest"
	"testing"
)

func TestR31AdminAgentMemoryContract(t *testing.T) {
	identitytest.RunAdminAgents(t, func(*testing.T) identity.Store { return identity.NewMemStore() })
}
func TestR31AdminAgentPGContract(t *testing.T) { identitytest.RunAdminAgents(t, pgFactory(t)) }
