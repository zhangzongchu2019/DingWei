package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// HistoryInputProvenance contains only canonical Hub evidence, never event bodies.
// Callers must derive it from authenticated ingress, not helper JSON.
type HistoryInputProvenance struct {
	SourceMessageID string
	Kind            string `json:"kind"`
	DisplayName     string `json:"display_name"`
	TrustDecision   string `json:"trust_decision"`
	AttestedBy      string `json:"attested_by"`
}

func (p HistoryInputProvenance) valid() bool {
	switch p.Kind {
	case "agent", "feishu", "human", "system":
	default:
		return false
	}
	return p.SourceMessageID != "" && p.DisplayName != "" && p.TrustDecision == "trusted" && p.AttestedBy == "hub"
}

// RememberInputProvenance is independent of delivery durability. Failure must never
// undo or retry a delivery. Conflicting repeats become permanent, bounded tombstones.
func (h *SessionHistory) RememberInputProvenance(ctx context.Context, owner, agent, session, delivery string, p HistoryInputProvenance, now time.Time) error {
	if h == nil || !h.enabled.Load() {
		return ErrSessionHistoryDisabled
	}
	if owner == "" || agent == "" || session == "" || delivery == "" || !p.valid() {
		return errors.New("invalid history provenance")
	}
	n := int64(len(owner) + len(agent) + len(session) + len(delivery) + len(p.SourceMessageID) + len(p.Kind) + len(p.DisplayName) + len(p.TrustDecision) + len(p.AttestedBy) + 192)
	if n > 4096 {
		return errors.New("history provenance exceeds 4KiB")
	}
	ctx, cancel := context.WithTimeout(ctx, SessionHistoryTxnLimit)
	defer cancel()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var enabled int
	if err = tx.QueryRowContext(ctx, `SELECT enabled FROM session_history_runtime_state WHERE singleton=1`).Scan(&enabled); err != nil {
		return err
	}
	if enabled != 1 {
		return ErrSessionHistoryDisabled
	}
	k := historyKey{owner, agent, session}
	g, err := ensureHistoryGeneration(ctx, tx, k)
	if err != nil {
		return err
	}
	var prior HistoryInputProvenance
	var conflicted int
	err = tx.QueryRowContext(ctx, `SELECT source_message_id,source_kind,display_name,trust_decision,attested_by,conflicted FROM session_history_provenance WHERE owner_key=? AND agent_key_id=? AND session_name=? AND generation=? AND delivery_id=?`, owner, agent, session, g.value, delivery).Scan(&prior.SourceMessageID, &prior.Kind, &prior.DisplayName, &prior.TrustDecision, &prior.AttestedBy, &conflicted)
	if err == nil {
		if prior != p {
			_, err = tx.ExecContext(ctx, `UPDATE session_history_provenance SET conflicted=1 WHERE owner_key=? AND agent_key_id=? AND session_name=? AND generation=? AND delivery_id=?`, owner, agent, session, g.value, delivery)
			if err != nil {
				return err
			}
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var own, total int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(retained_bytes),0) FROM session_history_generation WHERE owner_key=? AND agent_key_id=?`, owner, agent).Scan(&own); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT retained_bytes FROM session_history_runtime_state WHERE singleton=1`).Scan(&total); err != nil {
		return err
	}
	if own+n > SessionHistoryAgentMaxBytes || total+n > SessionHistoryGlobalMaxBytes {
		return errors.New("history provenance capacity")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO session_history_provenance(owner_key,agent_key_id,session_name,generation,delivery_id,source_message_id,source_kind,display_name,trust_decision,attested_by,observed_at_ms,expires_at_ms,record_bytes) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, owner, agent, session, g.value, delivery, p.SourceMessageID, p.Kind, p.DisplayName, p.TrustDecision, p.AttestedBy, now.UnixMilli(), now.Add(SessionHistoryRetain).UnixMilli(), n)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// enrichHistoryInput runs only inside the event INSERT transaction. Returning an
// error rolls back both consumption and the event; in particular oversize never
// consumes evidence or writes a second event.
func enrichHistoryInput(ctx context.Context, tx *sql.Tx, r *historyAppendRequest, generation string) ([]byte, int64, error) {
	var event map[string]json.RawMessage
	if json.Unmarshal(r.payload, &event) != nil {
		return r.payload, r.recordBytes, nil
	}
	var kind string
	_ = json.Unmarshal(event["kind"], &kind)
	if kind != "user_input" {
		return r.payload, r.recordBytes, nil
	}
	// Defense in depth: a caller cannot smuggle a provenance block through Append.
	clean := r.payload
	cleanBytes := r.recordBytes
	if _, claimed := event["provenance"]; claimed {
		delete(event, "provenance")
		var err error
		clean, err = json.Marshal(event)
		if err != nil {
			return nil, 0, err
		}
		cleanBytes += int64(len(clean) - len(r.payload))
	}
	var data struct {
		Lifecycle string `json:"lifecycle"`
	}
	var delivery, origin, turn string
	_ = json.Unmarshal(event["delivery_id"], &delivery)
	_ = json.Unmarshal(event["origin_delivery_id"], &origin)
	_ = json.Unmarshal(event["turn_id"], &turn)
	if json.Unmarshal(event["data"], &data) != nil || data.Lifecycle != "admitted" || delivery == "" || delivery != origin || turn == "" {
		return clean, cleanBytes, nil
	}
	var p HistoryInputProvenance
	err := tx.QueryRowContext(ctx, `SELECT source_message_id,source_kind,display_name,trust_decision,attested_by FROM session_history_provenance WHERE owner_key=? AND agent_key_id=? AND session_name=? AND generation=? AND delivery_id=? AND conflicted=0 AND expires_at_ms>?`, r.key.owner, r.key.agent, r.key.session, generation, origin, r.now.UnixMilli()).Scan(&p.SourceMessageID, &p.Kind, &p.DisplayName, &p.TrustDecision, &p.AttestedBy)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, 0, err
	}
	matched := err == nil && p.valid()
	if !matched {
		return clean, cleanBytes, nil
	}
	if matched {
		// SourceMessageID is correlation-only and never serialized into the display block.
		event["provenance"], err = json.Marshal(map[string]string{"kind": p.Kind, "display_name": p.DisplayName, "trust_decision": p.TrustDecision, "attested_by": p.AttestedBy})
		if err != nil {
			return nil, 0, err
		}
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, 0, err
	}
	n := r.recordBytes + int64(len(payload)-len(r.payload))
	if n > SessionHistoryEventMaxBytes {
		return nil, 0, fmt.Errorf("%w: %d", ErrSessionHistoryOversize, n)
	}
	if matched {
		_, err = tx.ExecContext(ctx, `DELETE FROM session_history_provenance WHERE owner_key=? AND agent_key_id=? AND session_name=? AND generation=? AND delivery_id=?`, r.key.owner, r.key.agent, r.key.session, generation, origin)
	}
	return payload, n, err
}

// Sidecar evictions share event selectors but never create a body/gap marker.
func historyEvictionRows(ctx context.Context, tx *sql.Tx, owner, agent string) ([]historyRowKey, error) {
	condition := ""
	args := []any{}
	if owner != "" {
		condition = " WHERE owner_key=? AND agent_key_id=?"
		args = []any{owner, agent}
	}
	q := `SELECT owner_key,agent_key_id,session_name,generation,seq,record_bytes,received_at_ms,delivery_id FROM (` +
		`SELECT owner_key,agent_key_id,session_name,generation,seq,record_bytes,received_at_ms,'' AS delivery_id FROM session_history_event` + condition + ` UNION ALL ` +
		`SELECT owner_key,agent_key_id,session_name,generation,0 AS seq,record_bytes,observed_at_ms AS received_at_ms,delivery_id FROM session_history_provenance` + condition + `) ORDER BY received_at_ms,owner_key,agent_key_id,session_name,seq LIMIT ?`
	args = append(args, args...)
	args = append(args, SessionHistoryBatch)
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []historyRowKey
	for rows.Next() {
		var r historyRowKey
		if err = rows.Scan(&r.owner, &r.agent, &r.session, &r.generation, &r.seq, &r.bytes, &r.at, &r.delivery); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func deleteProvenanceBatch(ctx context.Context, tx *sql.Tx, condition string, args ...any) error {
	rows, err := tx.QueryContext(ctx, `SELECT owner_key,agent_key_id,session_name,generation,delivery_id FROM session_history_provenance WHERE `+condition+` LIMIT 500`, args...)
	if err != nil {
		return err
	}
	var keys [][5]string
	for rows.Next() {
		var k [5]string
		if err = rows.Scan(&k[0], &k[1], &k[2], &k[3], &k[4]); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, k)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, k := range keys {
		if _, err = tx.ExecContext(ctx, `DELETE FROM session_history_provenance WHERE owner_key=? AND agent_key_id=? AND session_name=? AND generation=? AND delivery_id=?`, k[0], k[1], k[2], k[3], k[4]); err != nil {
			return err
		}
	}
	return nil
}
