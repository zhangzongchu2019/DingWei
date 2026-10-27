package m8

import "errors"

// deliveryFailureLogReason is internal diagnostics only. Public notices and
// failure-dock payloads keep groupingRejectionReason's existing generic reason.
// Read existing admission state; never create a marker, wait, or change routing.
func (h *Hub) deliveryFailureLogReason(err error, target string) string {
	var rejection groupingRejection
	if !errors.Is(err, errGroupAgentNotFound) || errors.As(err, &rejection) {
		return groupingRejectionReason(err)
	}
	to, parseErr := parseAddress(target)
	if parseErr == nil && to.Kind == addressSession {
		h.mu.Lock()
		pending := h.capabilityPendingAdmissions[terminalKey(to.KeyID, to.SessionName)] != nil
		published := h.sessionClients[to.KeyID][to.SessionName] != nil
		h.mu.Unlock()
		if pending && !published {
			return "target_not_published"
		}
	}
	// An unresolved/offline target is not proof of an ACL denial, or of a
	// handshake race. Missing early markers and aliases stay explicitly unknown.
	return "target_offline_or_unresolved"
}
