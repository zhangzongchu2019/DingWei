package nodeagent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
)

// NodePolicy is operator configuration, never supplied in a seat command. Hard
// remains the default; a failed hard quota never selects soft automatically.
type NodePolicy struct {
	MinimumSeatCPUMillicores int64         `json:"minimum_seat_cpu_millicores"`
	MinimumSeatMemoryBytes   int64         `json:"minimum_seat_memory_bytes"`
	MinimumSeatDiskBytes     int64         `json:"minimum_seat_disk_bytes"`
	StateWriteBudget         time.Duration `json:"state_write_budget"`
	ShmBytes                 int64         `json:"shm_bytes"`
	SoftApprovalRef          string        `json:"soft_approval_ref,omitempty"`
	HostRootTrustRef         string        `json:"host_root_trust_ref,omitempty"`
	NetworkApprovalRef       string        `json:"network_approval_ref,omitempty"`
	EngineID                 string        `json:"engine_id,omitempty"`
	EngineRoot               string        `json:"engine_root,omitempty"`
	Mode                     string        `json:"quota_mode"`
	ScanInterval             time.Duration `json:"scan_interval"`
	ScanBudget               time.Duration `json:"scan_budget"`
	MaxAge                   time.Duration `json:"max_age"`
	StopTimeout              time.Duration `json:"stop_timeout"`
	ReserveInterval          time.Duration `json:"reserve_interval"`
	EmergencyReservePercent  int64         `json:"emergency_reserve_percent"`
	WarnPercent              int64         `json:"warn_percent"`
	RecoveryPercent          int64         `json:"recovery_percent"`
	ReserveBytes             int64         `json:"reserve_bytes"`
	ReservePercent           int64         `json:"reserve_percent"`
	ReserveInodes            uint64        `json:"reserve_inodes"`
	ReserveInodePercent      uint64        `json:"reserve_inode_percent"`
	MaxSeats                 int           `json:"max_seats"`
	ImageReserveBytes        int64         `json:"image_reserve_bytes"`
	ReserveCPUMillicores     int64         `json:"reserve_cpu_millicores"`
	ReserveMemoryBytes       int64         `json:"reserve_memory_bytes"`
	PidsLimit                int64         `json:"pids_limit"`
	LoginPidsLimit           int64         `json:"login_pids_limit"`
	LogMaxSizeMB             int64         `json:"log_max_size_mb"`
	LogMaxFiles              int64         `json:"log_max_files"`
}

func DefaultNodePolicy() NodePolicy {
	return NodePolicy{MinimumSeatCPUMillicores: 1000, MinimumSeatMemoryBytes: 1536 << 20, MinimumSeatDiskBytes: 40 << 30,
		StateWriteBudget: 5 * time.Second, Mode: "hard", ScanInterval: 30 * time.Second, ScanBudget: 5 * time.Second, MaxAge: 60 * time.Second,
		StopTimeout: 10 * time.Second, ReserveInterval: 5 * time.Second, WarnPercent: 80, RecoveryPercent: 75,
		EmergencyReservePercent: 50, ReserveBytes: 32 << 30, ReservePercent: 10, ReserveInodes: 65536, ReserveInodePercent: 1, MaxSeats: 4,
		ImageReserveBytes: 8 << 30, ShmBytes: 64 << 20, ReserveCPUMillicores: 2000, ReserveMemoryBytes: 4 << 30, PidsLimit: 512, LoginPidsLimit: 128, LogMaxSizeMB: 10, LogMaxFiles: 3}
}
func (p NodePolicy) Validate() error {
	if err := p.capacityPolicy().Validate(); err != nil {
		return fmt.Errorf("node policy: invalid minimum seat resources")
	}
	if p.Mode != "hard" && p.Mode != "soft" {
		return fmt.Errorf("NODEAGENT_QUOTA_MODE: expected hard or soft")
	}
	if p.Mode == "soft" && (p.SoftApprovalRef == "" || p.HostRootTrustRef == "" || p.NetworkApprovalRef == "") {
		return fmt.Errorf("soft mode requires explicit risk, host-root trust and network approval references; separate Engine does not resist host root")
	}
	if p.Mode == "soft" && (p.EngineID == "" || !filepath.IsAbs(p.EngineRoot) || filepath.Clean(p.EngineRoot) != p.EngineRoot || p.EngineRoot == "/" || p.EngineRoot == "/var/lib/docker") {
		return fmt.Errorf("soft mode requires approved NODEAGENT_ENGINE_ID and isolated NODEAGENT_ENGINE_ROOT")
	}
	if p.StateWriteBudget <= 0 || p.StateWriteBudget > 30*time.Second {
		return fmt.Errorf("node policy: invalid state write budget")
	}
	if p.ScanBudget <= 0 || p.ScanBudget > 30*time.Second || p.ScanInterval > 5*time.Minute || p.MaxAge > 10*time.Minute || p.StopTimeout > time.Minute || p.ScanInterval <= p.ScanBudget || p.MaxAge < p.ScanInterval+p.ScanBudget || p.ReserveInterval <= 0 || p.ReserveInterval > p.ScanInterval || p.StopTimeout <= 0 {
		return fmt.Errorf("node policy: invalid timing")
	}
	if p.EmergencyReservePercent <= 0 || p.EmergencyReservePercent >= 100 {
		return fmt.Errorf("node policy: invalid emergency reserve percentage")
	}
	if p.RecoveryPercent <= 0 || p.WarnPercent <= p.RecoveryPercent || p.WarnPercent >= 100 || p.ReserveBytes <= 0 || p.ReserveBytes > 1<<50 || p.ReservePercent <= 0 || p.ReservePercent >= 100 || p.ReserveInodes == 0 || p.ReserveInodePercent == 0 || p.ReserveInodePercent >= 100 || p.MaxSeats <= 0 || p.MaxSeats > 100 || p.ImageReserveBytes <= 0 || p.ImageReserveBytes > 1<<50 {
		return fmt.Errorf("node policy: invalid soft thresholds")
	}
	if p.ReserveCPUMillicores <= 0 || p.ReserveCPUMillicores > 1000000 || p.ReserveMemoryBytes <= 0 || p.ReserveMemoryBytes > 1<<50 {
		return fmt.Errorf("node policy: invalid host resource reserve")
	}
	if p.ShmBytes < 1<<20 || p.ShmBytes > 1<<30 {
		return fmt.Errorf("node policy: invalid shm limit")
	}
	if p.PidsLimit <= 0 || p.PidsLimit > 1048576 || p.LoginPidsLimit <= 0 || p.LoginPidsLimit > 1048576 || p.LogMaxSizeMB <= 0 || p.LogMaxSizeMB > 1024 || p.LogMaxFiles <= 0 || p.LogMaxFiles > 100 {
		return fmt.Errorf("node policy: invalid hard resource limits")
	}
	return nil
}

// ParseNodePolicy accepts only explicit, bounded values. An invalid value fails
// startup, including a misspelled mode; zero never disables a safety gate.
func ParseNodePolicy(getenv func(string) string) (NodePolicy, error) {
	p := DefaultNodePolicy()
	p.SoftApprovalRef = getenv("NODEAGENT_SOFT_APPROVAL_REF")
	p.HostRootTrustRef = getenv("NODEAGENT_HOST_ROOT_TRUST_REF")
	p.NetworkApprovalRef = getenv("NODEAGENT_NETWORK_APPROVAL_REF")
	p.EngineID = getenv("NODEAGENT_ENGINE_ID")
	p.EngineRoot = getenv("NODEAGENT_ENGINE_ROOT")
	if v := getenv("NODEAGENT_QUOTA_MODE"); v != "" {
		p.Mode = v
	}
	durations := map[string]*time.Duration{"STATE_WRITE_BUDGET": &p.StateWriteBudget, "SCAN_INTERVAL": &p.ScanInterval, "SCAN_BUDGET": &p.ScanBudget, "MAX_AGE": &p.MaxAge, "STOP_TIMEOUT": &p.StopTimeout, "RESERVE_INTERVAL": &p.ReserveInterval}
	for key, dst := range durations {
		if v := getenv("NODEAGENT_" + key); v != "" {
			d, e := time.ParseDuration(v)
			if e != nil {
				return p, fmt.Errorf("NODEAGENT_%s: invalid duration", key)
			}
			*dst = d
		}
	}
	ints := map[string]*int64{"EMERGENCY_RESERVE_PERCENT": &p.EmergencyReservePercent, "RESERVE_CPU_MILLICORES": &p.ReserveCPUMillicores, "RESERVE_MEMORY_BYTES": &p.ReserveMemoryBytes, "SHM_BYTES": &p.ShmBytes, "WARN_PERCENT": &p.WarnPercent, "RECOVERY_PERCENT": &p.RecoveryPercent, "RESERVE_BYTES": &p.ReserveBytes, "RESERVE_PERCENT": &p.ReservePercent, "IMAGE_RESERVE_BYTES": &p.ImageReserveBytes, "PIDS_LIMIT": &p.PidsLimit, "LOGIN_PIDS_LIMIT": &p.LoginPidsLimit, "LOG_MAX_SIZE_MB": &p.LogMaxSizeMB, "LOG_MAX_FILES": &p.LogMaxFiles}
	for key, dst := range ints {
		if v := getenv("NODEAGENT_" + key); v != "" {
			n, e := strconv.ParseInt(v, 10, 64)
			if e != nil {
				return p, fmt.Errorf("NODEAGENT_%s: invalid integer", key)
			}
			*dst = n
		}
	}
	for key, dst := range map[string]*int64{"MINIMUM_SEAT_CPU_MILLICORES": &p.MinimumSeatCPUMillicores, "MINIMUM_SEAT_MEMORY_BYTES": &p.MinimumSeatMemoryBytes, "MINIMUM_SEAT_DISK_BYTES": &p.MinimumSeatDiskBytes} {
		if v := getenv("NODEAGENT_" + key); v != "" {
			n, e := strconv.ParseInt(v, 10, 64)
			if e != nil {
				return p, fmt.Errorf("NODEAGENT_%s: invalid integer", key)
			}
			*dst = n
		}
	}
	for key, dst := range map[string]*uint64{"RESERVE_INODES": &p.ReserveInodes, "RESERVE_INODE_PERCENT": &p.ReserveInodePercent} {
		if v := getenv("NODEAGENT_" + key); v != "" {
			n, e := strconv.ParseUint(v, 10, 64)
			if e != nil {
				return p, fmt.Errorf("NODEAGENT_%s: invalid integer", key)
			}
			*dst = n
		}
	}
	if v := getenv("NODEAGENT_MAX_SEATS"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil {
			return p, fmt.Errorf("NODEAGENT_MAX_SEATS: invalid integer")
		}
		p.MaxSeats = n
	}
	return p, p.Validate()
}

// LoadNodePolicy reads configuration only; it never opens credentials or Docker.
func LoadNodePolicy() (NodePolicy, error) { return ParseNodePolicy(os.Getenv) }

func (p NodePolicy) capacityPolicy() seatcapacity.Policy {
	return seatcapacity.Policy{Minimum: seatcapacity.Resources{CPU: p.MinimumSeatCPUMillicores, Memory: p.MinimumSeatMemoryBytes, Disk: p.MinimumSeatDiskBytes}, MaxSeats: p.MaxSeats}
}

// Revision binds the approved registration to the actual node thresholds.
func (p NodePolicy) Revision() string {
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
