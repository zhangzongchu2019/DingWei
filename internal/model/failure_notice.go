package model

import (
	"crypto/sha256"
	"encoding/hex"
)

// FailureNoticeProvenance is set only by the Hub repository, never client metadata.
const FailureNoticeProvenance = "hub_delivery_failure_notice_v1"

func FailureNoticeDeliveryID(deliveryID string) string {
	sum := sha256.Sum256([]byte("delivery-failure-notice-v1\x00" + deliveryID))
	return "dw-dlv-failure-" + hex.EncodeToString(sum[:])
}

func DeliveryFailureReasonAllowed(reason string) bool {
	switch reason {
	case "retry_exhausted", "delivery_ack_unsupported", "delivery_capability_unconfirmed_expired", "delivery_expired":
		return true
	}
	return false
}
