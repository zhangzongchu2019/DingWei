package fcs

// memdedup.go — phase-1 in-memory IngressDedup (claudek3 CR G1-3). The durable
// authoritative dedup store is FCS/A3-owned (A2 §4) — this in-memory map is the
// phase-1 skeleton; a process restart loses dedup state (acceptable for the
// skeleton, durable store is a later step).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
)

type memDedup struct {
	mu sync.Mutex
	m  map[string]string // botChannelID\x00nativeID → request_id
}

func newMemDedup() *memDedup { return &memDedup{m: map[string]string{}} }

func (d *memDedup) RequestIDFor(_ context.Context, botChannelID, nativeEventID string) (string, bool, error) {
	key := botChannelID + "\x00" + nativeEventID
	d.mu.Lock()
	defer d.mu.Unlock()
	if rid, ok := d.m[key]; ok {
		return rid, false, nil // already ingested — caller drops the duplicate
	}
	rid, err := mintRequestID()
	if err != nil {
		return "", false, err
	}
	d.m[key] = rid
	return rid, true, nil
}

// mintRequestID mints a random request_id (A2 §4.1: durable random, no native-id
// leak). FCS is the Feishu ingress owner; A0 leaves the request_id wire literal to
// the ingress owner — FCS uses req_ + 32 hex CSPRNG (crypto/rand, per A0's crypto/
// rand mandate). Returns an error rather than panicking (claudek3 CR low item).
func mintRequestID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "req_" + hex.EncodeToString(b[:]), nil
}

var _ IngressDedup = (*memDedup)(nil)
