package identity_test

import (
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/identitytest"
)

func TestMemStoreContract(t *testing.T) {
	identitytest.Run(t, func(*testing.T) identity.Store { return identity.NewMemStore() })
}
