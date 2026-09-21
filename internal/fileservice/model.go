package fileservice

import "time"

type State string

const (
	StatePending  State = "pending"
	StateReady    State = "ready"
	StateDeleting State = "deleting"
	StateEvicted  State = "evicted"
	StateExpired  State = "expired"
	StateFailed   State = "failed"
)

type File struct {
	ID, ObjectKey, OwnerKey, SessionName, OriginalName, MediaType, SHA256 string
	RetentionClass                                                        string
	SizeBytes                                                             int64
	State                                                                 State
	Archived                                                              bool
	CreatedAt, ExpiresAt                                                  time.Time
	ArchivedAt, UnarchivedAt                                              *time.Time
	RemovedAt                                                             *time.Time
	RemovalReason                                                         string
}

const (
	RetentionTemporary     = "temporary"
	RetentionSpecPermanent = "spec_permanent"
)

// Capability is deliberately an input contract, not an issuer. The owner has
// not selected the WSS-to-HTTP identity mechanism; callers must supply a
// verifier and there is no anonymous fallback.
type Capability struct {
	Subject, FileID, Method, Range string
	ExpiresAt                      time.Time
}

type CapabilityVerifier interface {
	Verify(raw, method, fileID, byteRange string, now time.Time) (Capability, error)
}
