package model

import "time"

// OutboundIntent is a retained, body-free acceptance fence. Deleting delivery
// payloads must not reopen its source ID inside the recovery/retention window.
type OutboundIntent struct {
	SenderID        string
	SourceMessageID string
	TargetID        string
	Digest          string
	DeliveryID      string
	CreatedAt       time.Time
}
