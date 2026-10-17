package nodeagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var ErrSoftQuota = errors.New("soft_quota_blocked")

const managedSeatLabel = "dingwei.node_id"
const managedSeatIDLabel = "dingwei.seat_id"

var safeContainerID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

type quotaEvent struct {
	WriterKind string    `json:"writer_kind,omitempty"`
	ID         string    `json:"id"`
	SeatID     string    `json:"seat_id"`
	Reason     string    `json:"reason"`
	At         time.Time `json:"at"`
}
type softSeat struct {
	WriterKind   string        `json:"writer_kind,omitempty"`
	WriterRoot   string        `json:"writer_root,omitempty"` // Private local path, never reported.
	Retiring     bool          `json:"retiring,omitempty"`    // Engine removal confirmed; no remaining writer.
	UsageWarning bool          `json:"usage_warning"`
	SpaceWarning bool          `json:"space_warning"`
	writerGate   chan struct{} // Per-seat only; never serialized or reported.
	UsageUnknown bool          `json:"usage_unknown"`
	Limit        int64         `json:"limit_bytes"`
	Resources    seatResources `json:"resources"`
	CIDs         []string      `json:"container_ids"`
	Usage        diskUsage     `json:"usage"`
	MeasuredAt   time.Time     `json:"measured_at"`
	CreatedAt    time.Time     `json:"created_at"`
	Hold         string        `json:"hold,omitempty"`
	StopState    string        `json:"stop_state,omitempty"`
	Warn         bool          `json:"warn"`
}
type softState struct {
	LastSpaceAt      time.Time            `json:"last_space_at"`
	UnknownStartedAt time.Time            `json:"unknown_started_at"`
	Root             string               `json:"seat_root"`
	Active           bool                 `json:"active"`
	Version          int                  `json:"version"`
	NodeID           string               `json:"node_id"`
	Seats            map[string]*softSeat `json:"seats"`
	Events           []quotaEvent         `json:"events"`
}
type softQuota struct {
	retiredLock                                *os.File
	readInventory                              func(string) ([]os.DirEntry, error)
	persistMu                                  sync.Mutex
	persistOnce                                sync.Once
	persistQuitOnce                            sync.Once
	persistRequests                            chan stateWrite
	persistQuit                                chan struct{}
	persistDone                                chan struct{}
	persisting                                 int
	writeState                                 func(string, []byte) error
	sampling                                   bool
	samplingAt                                 time.Time
	spaceKnown                                 bool
	spaceErr                                   error
	inventory                                  []string
	inventoryAt                                time.Time
	inventoryErr                               error
	sampledCPU, sampledTotal, sampledAvailable int64
	resourcesAt                                time.Time
	resourcesErr                               error

	resources          func() (int64, int64, int64, error)
	mu                 sync.Mutex
	enforceMu          sync.Mutex
	policy             NodePolicy
	root, path, nodeID string
	state              softState
	broken             bool
	space              fsSpace
	spaceAt            time.Time
	now                func() time.Time
	usage              func(context.Context, string, string) (diskUsage, error)
	disk               func(string) (fsSpace, error)
	// Held for this agent's lifetime. Prevents two controllers from racing state.
	lock *os.File
}

func newSoftQuota(p NodePolicy, root, path, nodeID string) (*softQuota, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if p.Mode != "soft" || nodeID == "" {
		return nil, fmt.Errorf("soft quota requires explicit mode/node")
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, fmt.Errorf("soft seat root must be absolute and canonical")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode().Perm()&0022 != 0 {
		return nil, fmt.Errorf("soft seat root must be a protected directory")
	}
	rel, e := filepath.Rel(root, path)
	if e != nil || !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return nil, fmt.Errorf("soft state must be outside seat root")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	// Private state is outside /work. Refuse permissive or aliased directories.
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("soft quota state directory must be private")
	}
	lockfd, err := syscall.Open(path+".lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	lock := os.NewFile(uintptr(lockfd), "soft-quota-lock")
	var lockStat syscall.Stat_t
	if syscall.Fstat(lockfd, &lockStat) != nil || lockStat.Mode&syscall.S_IFMT != syscall.S_IFREG || lockStat.Mode&0777 != 0600 || lockStat.Nlink != 1 || lockStat.Uid != uint32(os.Geteuid()) {
		lock.Close()
		return nil, fmt.Errorf("soft quota lock metadata invalid")
	}
	if err = syscall.Flock(lockfd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, fmt.Errorf("soft quota controller already running")
	}
	q := &softQuota{readInventory: os.ReadDir, writeState: writeSoftState, resources: hostResourceBudget, policy: p, root: root, path: path, nodeID: nodeID, now: time.Now, usage: measureUsage, disk: readSpace, lock: lock, state: softState{Version: 1, NodeID: nodeID, Root: root, Active: true, UnknownStartedAt: time.Now(), Seats: map[string]*softSeat{}}}
	fail := func(err error) (*softQuota, error) {
		q.mu.Lock()
		q.retiredLock = lock
		q.lock = nil
		q.mu.Unlock()
		if q.persistDone == nil {
			lock.Close()
		} else {
			q.stopPersister()
		}
		return nil, err
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err == syscall.ENOENT {
		if err = q.persist(); err != nil {
			return fail(err)
		}
		return q, nil
	}
	if err != nil {
		return fail(err)
	}
	f := os.NewFile(uintptr(fd), "soft-quota-state")
	defer f.Close()
	info, err = f.Stat()
	var fileStat syscall.Stat_t
	if syscall.Fstat(fd, &fileStat) != nil || fileStat.Nlink != 1 || fileStat.Uid != uint32(os.Geteuid()) {
		return fail(fmt.Errorf("soft quota state owner/link invalid"))
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 4<<20 {
		return fail(fmt.Errorf("soft quota state metadata invalid"))
	}
	raw, err := io.ReadAll(io.LimitReader(f, 4<<20))
	if err != nil {
		return fail(err)
	}
	var stored softState
	if err = json.Unmarshal(raw, &stored); err != nil {
		return fail(fmt.Errorf("soft quota state corrupt"))
	}
	q.state = stored
	if q.state.LastSpaceAt.IsZero() && q.state.UnknownStartedAt.IsZero() {
		return fail(fmt.Errorf("soft quota missing snapshot-age anchor"))
	}
	if q.state.Version != 1 || q.state.NodeID != nodeID || q.state.Root != root || q.state.Seats == nil || len(q.state.Events) > 256 {
		return fail(fmt.Errorf("soft quota state identity/version invalid"))
	}
	for id, s := range q.state.Seats {
		if !validSeatID(id) || s == nil || !validSoftWriter(id, s) || s.Limit <= 0 || s.CreatedAt.IsZero() || s.Resources.validate() != nil || s.Resources.DiskMB > 1<<40 || s.Limit != s.Resources.DiskMB*1024*1024 || len(s.CIDs) > 4 || s.Usage.Allocated < 0 || s.Usage.Apparent < 0 {
			return fail(fmt.Errorf("soft quota seat state invalid"))
		}
	}
	for _, s := range q.state.Seats {
		if s.WriterKind == "login" && (!loginRootOutside(s.WriterRoot, root) || !loginRootOutside(s.WriterRoot, filepath.Dir(path))) {
			return fail(fmt.Errorf("soft quota login root aliases protected state"))
		}
		for _, cid := range s.CIDs {
			if !safeContainerID.MatchString(cid) {
				return fail(fmt.Errorf("soft quota container identity invalid"))
			}
		}
	}
	if q.state.Active {
		for id := range q.state.Seats {
			q.holdLocked(id, "unclean_restart")
		}
	}
	q.state.Active = true
	if err = q.persist(); err != nil {
		return fail(err)
	}
	return q, nil
}
func (q *softQuota) close() {
	q.mu.Lock()
	lock := q.lock
	if lock == nil {
		q.mu.Unlock()
		q.stopPersister()
		return
	}
	if !q.broken {
		for id, s := range q.state.Seats {
			if s.WriterKind == "login" && !s.Retiring {
				q.holdLocked(id, "unclean_restart")
			}
		}
		q.state.Active = false
		_ = q.persistLocked()
	}
	q.lock = nil
	q.retiredLock = lock
	q.mu.Unlock()
	if q.persistDone == nil {
		lock.Close()
	} else {
		q.stopPersister()
	}
}
func (q *softQuota) persist() error { q.mu.Lock(); defer q.mu.Unlock(); return q.persistLocked() }
func writeSoftState(path string, raw []byte) error {
	var err error
	f, err := os.CreateTemp(filepath.Dir(path), ".soft-quota-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err == nil {
		dir, e := os.Open(filepath.Dir(path))
		if e != nil {
			err = e
		} else {
			err = dir.Sync()
			dir.Close()
		}
	}
	return err
}
func (q *softQuota) eventLocked(id, reason string) {
	if len(q.state.Events) >= 256 {
		q.broken = true
		return
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		q.broken = true
		return
	}
	kind := ""
	if s := q.state.Seats[id]; s != nil {
		kind = s.WriterKind
	}
	q.state.Events = append(q.state.Events, quotaEvent{ID: hex.EncodeToString(b[:]), SeatID: id, Reason: reason, At: q.now().UTC(), WriterKind: kind})
}
func (q *softQuota) holdLocked(id, reason string) {
	s := q.state.Seats[id]
	if s.Hold == "" && !s.Retiring {
		s.Hold = reason
		s.StopState = "pending"
		q.eventLocked(id, reason)
	}
}
func (q *softQuota) admitLocked(add int64) error {
	if q.broken || q.persisting > 0 {
		return fmt.Errorf("%w: private_state_unavailable", ErrSoftQuota)
	}
	if err := q.resourceBudgetLocked(seatResources{}); err != nil {
		return err
	}
	if q.rootUnknownLocked() {
		return fmt.Errorf("%w: space_unknown", ErrSoftQuota)
	}
	if q.inventoryErr != nil || q.inventoryAt.IsZero() || q.now().Sub(q.inventoryAt) > q.policy.MaxAge || q.now().Before(q.inventoryAt) {
		return fmt.Errorf("%w: inventory_unknown", ErrSoftQuota)
	}
	for _, name := range q.inventory {
		if _, known := q.state.Seats[name]; !known {
			return fmt.Errorf("%w: unmanaged_seat_directory", ErrSoftQuota)
		}
	}
	reserve, inodes := q.admissionReserves(q.space)
	if q.space.FreeFiles < inodes {
		return fmt.Errorf("%w: inode_reserve", ErrSoftQuota)
	}
	available := q.space.Free - reserve - q.policy.ImageReserveBytes
	for _, s := range q.state.Seats {
		if s.UsageUnknown {
			return fmt.Errorf("%w: usage_unknown", ErrSoftQuota)
		}
		since := s.MeasuredAt
		if since.IsZero() {
			since = s.CreatedAt
		}
		if q.now().Sub(since) > q.policy.MaxAge || q.now().Sub(since) < 0 {
			return fmt.Errorf("%w: stale_usage", ErrSoftQuota)
		}
		committed := max(int64(0), s.Limit-s.Usage.Allocated)
		if committed > available {
			return fmt.Errorf("%w: disk_reserve", ErrSoftQuota)
		}
		available -= committed
	}
	if add < 0 || add > available {
		return fmt.Errorf("%w: disk_reserve", ErrSoftQuota)
	}
	return nil
}
func (q *softQuota) reserve(id string, r seatResources) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if loginDirPattern.MatchString(id) || r.DiskMB <= 0 || r.DiskMB > 1<<40 {
		return fmt.Errorf("%w: positive bounded disk limit required", ErrSoftQuota)
	}
	if _, exists := q.state.Seats[id]; exists {
		return fmt.Errorf("%w: seat_already_registered", ErrSoftQuota)
	}
	count := 0
	for _, s := range q.state.Seats {
		if s.WriterKind == "" {
			count++
		}
	}
	if count >= q.policy.MaxSeats {
		return fmt.Errorf("%w: seat_count", ErrSoftQuota)
	}
	limit := r.DiskMB * 1024 * 1024
	if err := q.resourceBudgetLocked(r); err != nil {
		return err
	}
	if err := q.admitLocked(limit); err != nil {
		return err
	}
	q.state.Seats[id] = &softSeat{Limit: limit, Resources: r, CreatedAt: q.now(), CIDs: []string{}}
	return q.persistLocked()
}
func (q *softQuota) track(id, cid string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	s := q.state.Seats[id]
	if s == nil || !safeContainerID.MatchString(cid) || len(s.CIDs) >= 4 {
		return fmt.Errorf("%w: unregistered_container", ErrSoftQuota)
	}
	for _, c := range s.CIDs {
		if c == cid {
			return nil
		}
	}
	s.CIDs = append(s.CIDs, cid)
	return q.persistLocked()
}
func (q *softQuota) forget(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.state.Seats, id)
	return q.persistLocked()
}
func (q *softQuota) commandGate(cmd Command) error {
	switch cmd.Type {
	case CmdCreateSeat, CmdStartSeat, CmdRestartSeat, CmdUpgradeSeat, CmdRefreshCredSeat, CmdLoginStart, CmdPullImage:
	default:
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := q.admitLocked(0); err != nil {
		return err
	}
	var p struct {
		SeatID string `json:"seat_id"`
	}
	if json.Unmarshal(cmd.Params, &p) != nil {
		return ErrParams
	}
	if p.SeatID != "" && cmd.Type != CmdCreateSeat && q.state.Seats[p.SeatID] == nil {
		return fmt.Errorf("%w: seat_not_registered", ErrSoftQuota)
	}
	if s := q.state.Seats[p.SeatID]; s != nil && s.Hold != "" {
		return fmt.Errorf("%w: held", ErrSoftQuota)
	}
	return nil
}

// Writer operations and automatic stop share a gate, but never hold the state
// mutex during a walk or Engine request. Reserve can durably hold while either
// is blocked; a stop runs after any in-flight start, never before a late start.
func (q *softQuota) acquireWriter(ctx context.Context, id string) (func(), error) {
	q.mu.Lock()
	seat := q.state.Seats[id]
	if seat == nil {
		q.mu.Unlock()
		return nil, ErrSoftQuota
	}
	if seat.writerGate == nil {
		seat.writerGate = make(chan struct{}, 1)
	}
	gate := seat.writerGate
	q.mu.Unlock()
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (q *softQuota) start(ctx context.Context, id string, fn func(context.Context) error) error {
	q.mu.Lock()
	if err := q.admitLocked(0); err != nil {
		q.mu.Unlock()
		return err
	}
	seat := q.state.Seats[id]
	if seat == nil || seat.Hold != "" {
		q.mu.Unlock()
		return fmt.Errorf("%w: held or unregistered", ErrSoftQuota)
	}
	q.mu.Unlock()
	c, cancel := context.WithTimeout(ctx, q.policy.ScanBudget)
	usage, err := q.writerUsage(c, id, seat)
	if c.Err() != nil {
		err = c.Err()
	}
	cancel()
	q.mu.Lock()
	if q.state.Seats[id] != seat {
		q.mu.Unlock()
		return ErrSoftQuota
	}
	if err != nil {
		seat.UsageUnknown = true
		_ = q.persistLocked()
		q.mu.Unlock()
		return fmt.Errorf("%w: usage_unknown", ErrSoftQuota)
	}
	seat.UsageUnknown = false
	seat.Usage, seat.MeasuredAt = usage, q.now()
	if usage.charged() >= seat.Limit {
		q.holdLocked(id, "disk_limit")
	}
	if err = q.admitLocked(0); err == nil && seat.Hold != "" {
		err = ErrSoftQuota
	}
	pe := q.persistLocked()
	q.mu.Unlock()
	if err != nil {
		return err
	}
	if pe != nil {
		return fmt.Errorf("%w: private_state_unavailable", ErrSoftQuota)
	}
	gateCtx, gateCancel := context.WithTimeout(ctx, q.policy.StopTimeout)
	defer gateCancel()
	release, gateErr := q.acquireWriter(gateCtx, id)
	if gateErr != nil {
		return gateErr
	}
	defer release()
	q.mu.Lock()
	gateErr = q.admitLocked(0)
	if q.state.Seats[id] != seat || seat.Hold != "" {
		gateErr = ErrSoftQuota
	}
	q.mu.Unlock()
	if gateErr != nil {
		return gateErr
	}
	c2, cancel2 := context.WithTimeout(ctx, q.policy.StopTimeout)
	defer cancel2()
	err = fn(c2)
	q.mu.Lock()
	held := q.state.Seats[id] != seat || seat.Hold != "" || q.broken
	q.mu.Unlock()
	if held {
		return fmt.Errorf("%w: held during start", ErrSoftQuota)
	}
	return err
}

// Measurements run on a snapshot without the state mutex. A forgotten/replaced
// seat or newer writer measurement cannot receive a stale result.
func (q *softQuota) measure(ctx context.Context) {
	q.mu.Lock()
	snapshot := make(map[string]*softSeat, len(q.state.Seats))
	for id, seat := range q.state.Seats {
		if !seat.Retiring && !(seat.WriterKind == "login" && len(seat.CIDs) == 0) {
			snapshot[id] = seat
		}
	}
	began := q.now()
	q.mu.Unlock()
	c, cancel := context.WithTimeout(ctx, q.policy.ScanBudget)
	defer cancel()
	for id, seat := range snapshot {
		u, err := q.writerUsage(c, id, seat)
		if c.Err() != nil {
			err = c.Err()
		}
		q.mu.Lock()
		if q.state.Seats[id] != seat || seat.Retiring || seat.MeasuredAt.After(began) {
			q.mu.Unlock()
			continue
		}
		if err == nil {
			seat.UsageWarning = false
			seat.UsageUnknown = false
			seat.Usage, seat.MeasuredAt = u, q.now()
			if u.charged() >= seat.Limit {
				q.holdLocked(id, "disk_limit")
			}
			if u.charged() >= seat.Limit/100*q.policy.WarnPercent && !seat.Warn {
				seat.Warn = true
				q.eventLocked(id, "disk_warning")
			}
			if u.charged() < seat.Limit/100*q.policy.RecoveryPercent {
				seat.Warn = false
			}
		} else {
			if !seat.UsageWarning {
				seat.UsageWarning = true
				q.eventLocked(id, "usage_warning")
			}
			seat.UsageUnknown = true
			since := seat.MeasuredAt
			if since.IsZero() {
				since = seat.CreatedAt
			}
			if q.now().Sub(since) > q.policy.MaxAge || q.now().Sub(since) < 0 {
				q.holdLocked(id, "usage_unknown")
			}
		}
		q.mu.Unlock()
	}
	q.mu.Lock()
	_ = q.persistLocked()
	for id := range q.state.Seats {
		if q.broken {
			q.holdLocked(id, "private_state_unavailable")
		}
	}
	q.mu.Unlock()
}
func (q *softQuota) scan(ctx context.Context, stop func(context.Context, string, string) error) {
	q.measure(ctx)
	q.enforce(ctx, stop)
}
func (d *dockerOps) stopOwned(ctx context.Context, id, cid string) error {
	release, err := d.soft.acquireWriter(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	if !d.soft.recorded(id, cid) {
		return fmt.Errorf("unrecorded stop target")
	}

	var v struct {
		ID     string `json:"Id"`
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		Mounts []struct{ Source, Destination string } `json:"Mounts"`
		State  struct{ Running bool }                 `json:"State"`
	}
	if err := d.eng.call(ctx, "GET", "/containers/"+cid+"/json", nil, &v); err != nil {
		var ee *engineError
		if errors.As(err, &ee) && ee.Status == 404 {
			return nil
		}
		return err
	}
	d.soft.mu.Lock()
	writer := *d.soft.state.Seats[id]
	d.soft.mu.Unlock()
	dest, source, label, labelID := "/work", seatDir(d.seatRoot, id), managedSeatIDLabel, id
	if writer.WriterKind == "login" {
		dest, source, label, labelID = "/h", filepath.Join(writer.WriterRoot, id), loginLabel, strings.TrimPrefix(id, "login-")
	}
	if v.ID != cid || v.Config.Labels[managedSeatLabel] != d.soft.nodeID || v.Config.Labels[label] != labelID {
		return fmt.Errorf("container ownership mismatch")
	}
	bound := false
	for _, m := range v.Mounts {
		if m.Destination == dest && m.Source == source {
			bound = true
		}
	}
	if !bound {
		return fmt.Errorf("container mount mismatch")
	}
	if !v.State.Running {
		return nil
	}
	if err := d.eng.containerStop(ctx, cid); err != nil {
		return err
	}
	info, err := d.eng.containerInspect(ctx, cid)
	if err != nil {
		return err
	}
	if info.State.Running {
		return fmt.Errorf("container still running")
	}
	return nil
}

// Three bounded workers: one scan, one reserve sampler and one enforcer. Neither
// a slow scan nor Engine stop serializes the reserve ticker. Signals coalesce;
// no goroutine is spawned per tick, and cancellation joins all workers.
func (d *dockerOps) runSoft(ctx context.Context) {
	enforce := make(chan struct{}, 1)
	notify := func() {
		select {
		case enforce <- struct{}{}:
		default:
		}
	}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(d.policy.ScanInterval)
		defer ticker.Stop()
		for {
			d.soft.measure(ctx)
			notify()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-enforce:
				d.soft.enforce(ctx, d.stopOwned)
			}
		}
	}()
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(d.policy.ReserveInterval)
		defer ticker.Stop()
		for {
			d.soft.sampleNode()
			notify()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	defer wg.Wait()
	ticker := time.NewTicker(d.policy.ReserveInterval)
	defer ticker.Stop()
	for {
		d.soft.checkReserve()
		notify()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Overflow-safe rounded-up percentages of the actual filesystem totals.
func (q *softQuota) admissionReserves(space fsSpace) (int64, uint64) {
	bytes := space.Total/100*q.policy.ReservePercent + (space.Total%100*q.policy.ReservePercent+99)/100
	inodes := space.Files/100*q.policy.ReserveInodePercent + (space.Files%100*q.policy.ReserveInodePercent+99)/100
	return max(q.policy.ReserveBytes, bytes), max(q.policy.ReserveInodes, inodes)
}

// Byte emergency uses its own configured percentage of R. The separately
// configured inode emergency is the leader-added scope in ruling eeb21f5b A1.
func (q *softQuota) emergencyThreshold(space fsSpace) int64 {
	reserve, _ := q.admissionReserves(space)
	percent := q.policy.EmergencyReservePercent
	return reserve/100*percent + (reserve%100*percent+99)/100
}
func (q *softQuota) inodeEmergencyThreshold(space fsSpace) uint64 {
	_, reserve := q.admissionReserves(space)
	percent := q.policy.InodeEmergencyReservePercent
	return reserve/100*percent + (reserve%100*percent+99)/100
}
func (q *softQuota) checkReserve() {
	q.mu.Lock()
	defer q.mu.Unlock()
	unknown := q.rootUnknownLocked()
	anchor := q.state.LastSpaceAt
	if anchor.IsZero() {
		anchor = q.state.UnknownStartedAt
	}
	for id, seat := range q.state.Seats {
		if unknown && !seat.SpaceWarning {
			seat.SpaceWarning = true
			q.eventLocked(id, "usage_warning")
		}
		if !unknown {
			seat.SpaceWarning = false
		}
		if unknown && (q.now().Sub(anchor) > q.policy.MaxAge || q.now().Before(anchor)) {
			q.holdLocked(id, "usage_unknown")
		}
		if !unknown && q.space.Free < q.emergencyThreshold(q.space) {
			q.holdLocked(id, "node_space_emergency")
		}
		if !unknown && q.space.FreeFiles < q.inodeEmergencyThreshold(q.space) {
			q.holdLocked(id, "node_inode_emergency")
		}
		if q.broken {
			q.holdLocked(id, "private_state_unavailable")
		}
		since := seat.MeasuredAt
		if since.IsZero() {
			since = seat.CreatedAt
		}
		if q.now().Sub(since) > q.policy.MaxAge || q.now().Before(since) {
			q.holdLocked(id, "usage_unknown")
		}
	}
	// The enforcer persists intent before Engine IO; reserve itself never waits
	// for a filesystem write. Persistence failure still causes fail-closed stops.
}
func (q *softQuota) enforce(ctx context.Context, stop func(context.Context, string, string) error) {
	q.enforceMu.Lock()
	defer q.enforceMu.Unlock()
	q.stopHeld(ctx, stop)
}

// softDirectory creates no XFS project and never changes a limit to zero. Clear
// is reachable only through an explicit destroy, never from the monitor/failure.
type softDirectory struct{}

func (softDirectory) Setup(root, id string, limit int64) error {
	if !validSeatID(id) || limit <= 0 {
		return ErrParams
	}
	if err := os.Mkdir(seatDir(root, id), 0700); err != nil {
		return err
	}
	return chownForSeat(seatDir(root, id))
}
func (softDirectory) Clear(root, id string) error {
	if !validSeatID(id) {
		return ErrParams
	}
	return os.RemoveAll(seatDir(root, id))
}

func (d *dockerOps) startWriter(ctx context.Context, id, cid string, restart bool) error {
	fn := func(c context.Context) error {
		if restart {
			return d.eng.containerRestart(c, cid)
		}
		return d.eng.containerStart(c, cid)
	}
	if d.soft != nil {
		return d.soft.start(ctx, id, func(c context.Context) error {
			if !d.soft.recorded(id, cid) {
				return fmt.Errorf("%w: unregistered_container", ErrSoftQuota)
			}
			return fn(c)
		})
	}
	return fn(ctx)
}
func (d *dockerOps) policyValue() NodePolicy {
	if d.policy.Mode == "" {
		return DefaultNodePolicy()
	}
	return d.policy
}
func (p NodePolicy) logConfig() map[string]any {
	return map[string]any{"Type": "local", "Config": map[string]string{"max-size": strconv.FormatInt(p.LogMaxSizeMB, 10) + "m", "max-file": strconv.FormatInt(p.LogMaxFiles, 10)}}
}

// Capacity snapshots copy only metadata, never private state paths/credentials.
func (q *softQuota) report() (map[string]softSeat, []quotaEvent, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	seats := map[string]softSeat{}
	for id, s := range q.state.Seats {
		v := *s
		v.CIDs = nil
		v.WriterRoot = ""
		seats[id] = v
	}
	events := append([]quotaEvent{}, q.state.Events...)
	ready := q.admitLocked(0) == nil
	return seats, events, ready
}
func (q *softQuota) acknowledge(ids []string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	remaining := q.state.Events[:0]
	for _, e := range q.state.Events {
		if !set[e.ID] {
			remaining = append(remaining, e)
		}
	}
	q.state.Events = remaining
	return q.persistLocked()
}

func (d *dockerOps) preflightSeat(id string) error {
	if d.soft == nil {
		return nil
	}
	b, _ := json.Marshal(map[string]string{"seat_id": id})
	return d.soft.commandGate(Command{Type: CmdStartSeat, Params: b})
}

func hostResourceBudget() (cpu, total, available int64, err error) {
	mt, mf, e := memInfoMB("/proc/meminfo")
	return int64(runtime.NumCPU()) * 1000, mt << 20, mf << 20, e
}
func (q *softQuota) resourceBudgetLocked(add seatResources) error {
	if q.resourcesErr != nil || q.resourcesAt.IsZero() || q.now().Sub(q.resourcesAt) > q.policy.MaxAge || q.now().Before(q.resourcesAt) || q.samplingExpiredLocked() {
		return fmt.Errorf("%w: resource_unknown", ErrSoftQuota)
	}
	cpu, total, available := q.sampledCPU, q.sampledTotal, q.sampledAvailable
	cpu -= q.policy.ReserveCPUMillicores
	total -= q.policy.ReserveMemoryBytes
	for _, s := range q.state.Seats {
		cpu -= s.Resources.cpuMillicores()
		total -= s.Resources.MemoryMB << 20
	}
	if cpu < add.cpuMillicores() || total < add.MemoryMB<<20 || available-q.policy.ReserveMemoryBytes < add.MemoryMB<<20 {
		return fmt.Errorf("%w: host_resource_reserve", ErrSoftQuota)
	}
	return nil
}
func (r seatResources) cpuMillicores() int64 {
	if r.CPUShares == 0 && r.CPULimitMillicores == 0 {
		return 0
	}
	if r.CPULimitMillicores > 0 {
		return r.CPULimitMillicores
	}
	return max(int64(10), int64(r.CPUShares)*1000/1024)
}
func (q *softQuota) checkResources(id string, r seatResources) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	s := q.state.Seats[id]
	if s == nil || s.Resources != r {
		return fmt.Errorf("%w: unapproved_resource_change", ErrSoftQuota)
	}
	return nil
}

func (q *softQuota) recorded(id, cid string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	s := q.state.Seats[id]
	if s == nil {
		return false
	}
	for _, c := range s.CIDs {
		if c == cid {
			return true
		}
	}
	return false
}
func (q *softQuota) untrack(id, cid string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	s := q.state.Seats[id]
	if s == nil {
		return ErrSoftQuota
	}
	out := s.CIDs[:0]
	for _, v := range s.CIDs {
		if v != cid {
			out = append(out, v)
		}
	}
	s.CIDs = out
	return q.persistLocked()
}

// Snapshot and durable intent are protected; Engine IO is outside the state
// mutex. Failed/pending calls never claim stopped. Forgotten seats are ignored.
func (q *softQuota) stopHeld(ctx context.Context, stop func(context.Context, string, string) error) {
	type target struct {
		id, cid string
		seat    *softSeat
	}
	type result struct {
		target
		err error
	}
	q.mu.Lock()
	targets := []target{}
	seats := map[string]*softSeat{}
	for id, seat := range q.state.Seats {
		if seat.Hold != "" {
			seats[id] = seat
			seat.StopState = "pending"
			for _, cid := range seat.CIDs {
				targets = append(targets, target{id, cid, seat})
			}
		}
	}
	_ = q.persistLocked()
	q.mu.Unlock()
	c, cancel := context.WithTimeout(ctx, q.policy.StopTimeout)
	defer cancel()
	results := make(chan result, len(targets))
	for _, item := range targets {
		go func(item target) { results <- result{item, stop(c, item.id, item.cid)} }(item)
	}
	failed := map[string]bool{}
	for range targets {
		r := <-results
		if r.err != nil {
			failed[r.id] = true
		}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for id, seat := range seats {
		if q.state.Seats[id] == seat {
			seat.StopState = "stopped"
			if failed[id] {
				seat.StopState = "unknown"
			}
		}
	}
	_ = q.persistLocked()
}
