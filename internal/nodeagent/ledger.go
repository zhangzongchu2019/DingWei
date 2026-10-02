package nodeagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ledger 幂等台账：command_id → 首次执行结果。
// 落盘（0600）⇒ 进程重启、断线重连后 Hub 重放旧指令，同样直接重报、不重复执行。
type ledger struct {
	path string
	mu   sync.Mutex
	m    map[string]Result
}

var errLedgerCorrupt = errors.New("nodeagent: ledger 文件损坏")

func loadLedger(path string) (*ledger, error) {
	l := &ledger{path: path, m: map[string]Result{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return l, nil
	}
	if err := json.Unmarshal(raw, &l.m); err != nil {
		return nil, fmt.Errorf("%w: %v", errLedgerCorrupt, err)
	}
	return l, nil
}

// Get 查台账：命中 ⇒（结果，true）。
func (l *ledger) Get(id string) (Result, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.m[id]
	return r, ok
}

// Put 写入并落盘（0600、原子替换）。
func (l *ledger) Put(r Result) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.m[r.CommandID] = r
	raw, err := json.Marshal(l.m)
	if err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

// 安装时的目录权限矫正（防 0777 临时目录）。
func secureLedgerDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o700)
}
