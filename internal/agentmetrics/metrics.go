// Bounded disk wire/cache. Current connection authority is supplied by the Hub ingress.
package agentmetrics

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"sync"
	"time"
)

const MaxBytes = 2048
const MaxSafe = 1<<53 - 1
const Lifetime = 360 * time.Second
const SampleInterval = 30 * time.Second

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)
var errInvalid = errors.New("invalid_agent_disk_metric")

type Wire struct {
	Schema     string `json:"schema"`
	Status     string `json:"status"`
	Producer   string `json:"producer"`
	Instance   string `json:"sample_instance"`
	Seq        string `json:"sample_seq"`
	SampledAt  int64  `json:"sampled_at_ms"`
	AgeMS      int64  `json:"elapsed_send_age_ms"`
	Basis      string `json:"path_basis"`
	Filesystem string `json:"filesystem_instance,omitempty"`
	Used       *int64 `json:"used_bytes,omitempty"`
	Total      *int64 `json:"total_bytes,omitempty"`
	Available  *int64 `json:"available_bytes,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

func Parse(raw []byte, now time.Time) (Wire, uint64, error) {
	var w Wire
	if len(raw) == 0 || len(raw) > MaxBytes {
		return w, 0, errInvalid
	}
	// Reject duplicate keys and trailing JSON before fixed-field decoding.
	d := json.NewDecoder(bytes.NewReader(raw))
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return w, 0, errInvalid
	}
	keys := map[string]json.RawMessage{}
	for d.More() {
		tok, e = d.Token()
		if e != nil {
			return w, 0, errInvalid
		}
		k, ok := tok.(string)
		if !ok {
			return w, 0, errInvalid
		}
		if _, exists := keys[k]; exists {
			return w, 0, errInvalid
		}
		var v json.RawMessage
		if d.Decode(&v) != nil {
			return w, 0, errInvalid
		}
		keys[k] = v
	}
	if _, e = d.Token(); e != nil {
		return w, 0, errInvalid
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return w, 0, errInvalid
	}
	required := []string{"schema", "status", "producer", "sample_instance", "sample_seq", "sampled_at_ms", "elapsed_send_age_ms", "path_basis"}
	allowed := map[string]bool{"filesystem_instance": true, "used_bytes": true, "total_bytes": true, "available_bytes": true, "reason": true}
	for _, k := range required {
		v, ok := keys[k]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return w, 0, errInvalid
		}
		allowed[k] = true
	}
	for k := range keys {
		if !allowed[k] || bytes.Equal(bytes.TrimSpace(keys[k]), []byte("null")) {
			return w, 0, errInvalid
		}
	}
	canonical, _ := json.Marshal(keys)
	d = json.NewDecoder(bytes.NewReader(canonical))
	d.DisallowUnknownFields()
	if d.Decode(&w) != nil {
		return w, 0, errInvalid
	}
	n, e := strconv.ParseUint(w.Seq, 10, 64)
	if e != nil || n == 0 || strconv.FormatUint(n, 10) != w.Seq || !hex32.MatchString(w.Instance) || w.Schema != "agent-disk/v1" || w.Producer != "helper" || w.Basis != "cli_workdir_filesystem" || w.AgeMS < 0 || w.AgeMS > 5000 || w.SampledAt <= 0 || w.SampledAt > MaxSafe {
		return w, 0, errInvalid
	}
	delta := now.UnixMilli() - w.SampledAt
	if delta < -30000 || delta > 30000+w.AgeMS {
		return w, 0, errInvalid
	}
	if w.Status == "ok" {
		if w.Reason != "" || !hex32.MatchString(w.Filesystem) || w.Used == nil || w.Total == nil || w.Available == nil || *w.Used < 0 || *w.Total <= 0 || *w.Total > MaxSafe || *w.Used > *w.Total || *w.Available < 0 || *w.Available > *w.Total {
			return w, 0, errInvalid
		}
	} else if w.Status == "unavailable" {
		if w.Used != nil || w.Total != nil || w.Available != nil || w.Filesystem != "" {
			return w, 0, errInvalid
		}
		switch w.Reason {
		case "workdir_unbound", "namespace_unbound", "sample_failed", "unsupported_mode":
		default:
			return w, 0, errInvalid
		}
	} else {
		return w, 0, errInvalid
	}
	return w, n, nil
}

// Binding must be supplied from current authenticated connection/Identity state.
// A test Binding is not a production proof; no body fields are used for it.
type Binding struct {
	Agent, Connection string
	Current           bool
}
type Snapshot struct {
	Agent       string `json:"agent_id"`
	Connection  string `json:"connection_generation"`
	Status      string `json:"status"`
	Source      string `json:"source"`
	Sample      Wire   `json:"sample"`
	ReceivedAt  int64  `json:"received_at_ms"`
	AgeMS       int64  `json:"age_ms"`
	ExpiresInMS int64  `json:"expires_in_ms"`
}
type Cache struct {
	mu                sync.Mutex
	binding           Binding
	record            Wire
	seq               uint64
	digest            [32]byte
	received, timeEnd time.Time
}

func New(binding Binding) *Cache { return &Cache{binding: binding} }
func (c *Cache) matches(b Binding) bool {
	return b.Current && b.Agent != "" && b.Connection != "" && c.binding.Agent == b.Agent && c.binding.Connection == b.Connection
}
func (c *Cache) Accept(raw []byte, b Binding, now time.Time) (bool, error) {
	w, n, e := Parse(raw, now)
	if e != nil {
		return false, e
	}
	digest := sha256.Sum256(raw)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.matches(b) {
		return false, errInvalid
	}
	if c.seq != 0 {
		if w.Instance != c.record.Instance || n < c.seq {
			return false, errInvalid
		}
		if n == c.seq {
			if digest != c.digest {
				return false, errInvalid
			}
			return false, nil
		}
	}
	// A current connection may replace its snapshot no more than once per period.
	if c.seq != 0 && now.Sub(c.received) < SampleInterval {
		return false, errInvalid
	}
	c.record, c.seq, c.digest = w, n, digest
	c.received = now
	c.timeEnd = now.Add(Lifetime - time.Duration(w.AgeMS)*time.Millisecond)
	return true, nil
}
func (c *Cache) Read(b Binding, now time.Time) Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := Snapshot{Agent: b.Agent, Connection: b.Connection, Status: "unavailable", Source: "helper_self_report"}
	if !c.matches(b) || c.seq == 0 {
		return out
	}
	out.ReceivedAt = c.received.UnixMilli()
	age := now.Sub(c.received)
	if age < 0 {
		return out
	}
	out.AgeMS = age.Milliseconds() + c.record.AgeMS
	if !now.Before(c.timeEnd) {
		out.Status = "expired"
		return out
	}
	out.Status = c.record.Status
	out.Sample = c.record
	// Returned snapshots must not permit alias mutation of the cached values.
	clone := func(p *int64) *int64 {
		if p == nil {
			return nil
		}
		v := *p
		return &v
	}
	out.Sample.Used = clone(c.record.Used)
	out.Sample.Total = clone(c.record.Total)
	out.Sample.Available = clone(c.record.Available)
	out.ExpiresInMS = c.timeEnd.Sub(now).Milliseconds()
	return out
}
