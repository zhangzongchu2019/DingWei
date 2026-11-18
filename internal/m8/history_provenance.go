package m8

import (
	"context"
	"log"

	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func (h *Hub) rememberHistoryInputProvenance(key, session, delivery, message string, p displayProvenance, target ...*sessionClient) {
	// Use the authenticated connection's immutable identity, never helper data or
	// an address label. Do not borrow identity/PG connections inside route locks.
	var c *sessionClient
	if len(target) > 0 {
		c = target[0] // identity captured by routing, before enqueue/name reuse
	} else {
		h.mu.Lock()
		c = h.sessionClients[key][session]
		h.mu.Unlock()
	}
	if c != nil && (c.keyID != key || c.sessionName != session) {
		log.Printf("history_provenance_unavailable reason=identity_mismatch")
		return
	}
	owner, agent := "", ""
	if c != nil {
		owner = c.ownerKey
		agent = c.metricsAgentID
		if agent == "" {
			agent = c.keyID
		}
	}
	if owner == "" || agent == "" {
		log.Printf("history_provenance_unavailable reason=identity_unavailable")
		return
	} // absent target identity is fail-closed
	err := h.SessionHistory.RememberInputProvenance(context.Background(), owner, agent, session, delivery, store.HistoryInputProvenance{SourceMessageID: message, Kind: p.Kind, DisplayName: p.DisplayName, TrustDecision: p.TrustDecision, AttestedBy: p.AttestedBy}, h.now())
	if err != nil {
		log.Printf("history_provenance_unavailable reason=store_error error_type=%T", err)
	}
}
