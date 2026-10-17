package nodeagent

// Login writers share the node's persistent quota ledger and start/stop gate.
// Their private tmpfs path is local-only; capacity exposes a typed writer ID.
import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func loginRootOutside(root, protected string) bool {
	rel, err := filepath.Rel(protected, root)
	return err == nil && rel != "." && (rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func validSoftWriter(id string, s *softSeat) bool {
	if s.WriterKind == "" {
		return s.WriterRoot == "" && !s.Retiring && !loginDirPattern.MatchString(id)
	}
	return s.WriterKind == "login" && loginDirPattern.MatchString(id) && filepath.IsAbs(s.WriterRoot) && filepath.Clean(s.WriterRoot) == s.WriterRoot
}

func (q *softQuota) loginStartGate(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.lock == nil {
		return ErrSoftQuota
	}
	if err := q.admitLocked(0); err != nil {
		return err
	}
	if s := q.state.Seats["login-"+id]; s != nil && s.Hold != "" {
		return ErrSoftQuota
	}
	return nil
}

func (q *softQuota) reserveLogin(id, root string) error {
	if !loginIDPattern.MatchString(id) || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return ErrSoftQuota
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || resolved != root {
		return ErrSoftQuota
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return ErrSoftQuota
	}
	// Login workspaces may share /dev/shm, but may never alias seat/private state.
	for _, protected := range []string{q.root, filepath.Dir(q.path)} {
		if !loginRootOutside(root, protected) {
			return ErrSoftQuota
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "login-"+id)); !os.IsNotExist(err) {
		return fmt.Errorf("%w: existing_login_directory", ErrSoftQuota)
	}
	r := seatResources{MemoryMB: loginMemBytes >> 20, CPUShares: 256, CPULimitMillicores: 250, DiskMB: loginMemBytes >> 20}
	q.mu.Lock()
	defer q.mu.Unlock()
	key := "login-" + id
	if q.lock == nil || q.state.Seats[key] != nil {
		return ErrSoftQuota
	}
	count := 0
	for _, s := range q.state.Seats {
		if s.WriterKind == "login" {
			count++
		}
	}
	if count >= loginPerNodeMax {
		return ErrLoginBusy
	}
	if err := q.resourceBudgetLocked(r); err != nil {
		return err
	}
	if err := q.admitLocked(r.DiskMB * 1024 * 1024); err != nil {
		return err
	}
	q.state.Seats[key] = &softSeat{WriterKind: "login", WriterRoot: root, Resources: r, Limit: r.DiskMB * 1024 * 1024, CreatedAt: q.now(), MeasuredAt: q.now(), CIDs: []string{}}
	return q.persistLocked()
}
func (q *softQuota) writerUsage(ctx context.Context, id string, s *softSeat) (diskUsage, error) {
	if s.WriterKind == "login" {
		return measureUsage(ctx, s.WriterRoot, id)
	}
	return q.usage(ctx, q.root, id)
}
func (q *softQuota) knownLogin(id, root string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	s := q.state.Seats["login-"+id]
	return s != nil && s.WriterKind == "login" && s.WriterRoot == root
}
func (q *softQuota) preserveLogin(id, root string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	s := q.state.Seats["login-"+id]
	return q.broken || q.lock == nil || s == nil || s.WriterKind != "login" || s.WriterRoot != root || s.Hold != ""
}
func (q *softQuota) blockUnknownLogin() {
	q.mu.Lock()
	defer q.mu.Unlock()
	// Never adopt or delete an unrecorded writer. Fail closed until operator reconciliation.
	q.broken = true
}

// cleanupSoft serializes with start/stop. A held writer is retained, including
// after restart/cancel. Only a confirmed removed container may retire its data.
func (l *loginOps) cleanupSoft(ctx context.Context, id string) {
	q := l.d.soft
	key := "login-" + id
	c, cancel := context.WithTimeout(ctx, q.policy.StopTimeout)
	defer cancel()
	release, err := q.acquireWriter(c, key)
	if err != nil {
		return
	}
	defer release()
	if q.preserveLogin(id, l.root) {
		return
	}
	q.mu.Lock()
	s := q.state.Seats[key]
	cids := append([]string(nil), s.CIDs...)
	q.mu.Unlock()
	for _, cid := range cids {
		if err = l.d.inspectLoginOwner(c, key, cid); err != nil {
			break
		}
		if err = l.d.eng.containerRemove(c, cid); err != nil {
			break
		}
	}
	if err == nil {
		_, network := loginNames(id)
		err = l.d.eng.networkRemove(c, network)
	}
	q.mu.Lock()
	if err != nil {
		q.holdLocked(key, "usage_unknown")
		_ = q.persistLocked()
		q.mu.Unlock()
		return
	}
	if q.broken || q.lock == nil || s.Hold != "" {
		q.mu.Unlock()
		return
	}
	s.Retiring = true
	err = q.persistLocked()
	q.mu.Unlock()
	if err != nil {
		return
	}
	if os.RemoveAll(l.dir(id)) != nil {
		return
	}
	_ = q.finishLogin(key, s)
}

func (d *dockerOps) inspectLoginOwner(ctx context.Context, key, cid string) error {
	var v struct {
		ID     string                                 `json:"Id"`
		Config struct{ Labels map[string]string }     `json:"Config"`
		Mounts []struct{ Source, Destination string } `json:"Mounts"`
	}
	err := d.eng.call(ctx, "GET", "/containers/"+cid+"/json", nil, &v)
	var ee *engineError
	if errors.As(err, &ee) && ee.Status == 404 {
		return nil
	}
	if err != nil {
		return err
	}
	d.soft.mu.Lock()
	s := d.soft.state.Seats[key]
	valid := s != nil && s.WriterKind == "login"
	root := ""
	if valid {
		root = s.WriterRoot
	}
	d.soft.mu.Unlock()
	if !valid || v.ID != cid || v.Config.Labels[managedSeatLabel] != d.soft.nodeID || v.Config.Labels[loginLabel] != strings.TrimPrefix(key, "login-") {
		return ErrSoftQuota
	}
	for _, m := range v.Mounts {
		if m.Destination == "/h" && m.Source == filepath.Join(root, key) {
			return nil
		}
	}
	return ErrSoftQuota
}

// Ordinary seat teardown must not erase a login writer's budget or stop target.
func (q *softQuota) finishLogin(key string, writer *softSeat) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.broken || q.lock == nil || q.state.Seats[key] != writer || writer.WriterKind != "login" || !writer.Retiring || writer.Hold != "" {
		return ErrSoftQuota
	}
	delete(q.state.Seats, key)
	return q.persistLocked()
}
