package model

import "time"

// OutboundIntent is a retained, body-free acceptance fence. Deleting delivery
// payloads must never turn its source ID into a new business request.
type OutboundIntent struct {
	SenderID        string
	SourceMessageID string
	TargetID        string
	Digest          string
	DeliveryID      string
	CreatedAt       time.Time
}
