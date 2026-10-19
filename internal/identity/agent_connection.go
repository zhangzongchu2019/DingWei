package identity

import (
	"context"
	"net"
	"time"
)

type AgentConnection struct {
	IP string    `json:"ip"`
	At time.Time `json:"at"`
}

// ConnectionStore deliberately does not add IPs to general Agent structs or
// group/event projections. Every read rechecks the human reader's role/owner.
type ConnectionStore interface {
	RecordAgentConnection(context.Context, string, string, time.Time) error
	ReadAgentConnection(context.Context, string, string) (AgentConnection, error)
}

func (s *PGStore) RecordAgentConnection(ctx context.Context, id, ip string, at time.Time) error {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ErrBadRequest
	}
	_, err := s.pool.Exec(ctx, s.t, `UPDATE np_agent SET last_source_ip=$2,last_source_at=$3 WHERE id=$1 AND origin='selfhost' AND state='active' AND (last_source_at IS NULL OR last_source_at <= $3)`, id, parsed.String(), at.UTC())
	return err
}
func (s *PGStore) ReadAgentConnection(ctx context.Context, reader, id string) (AgentConnection, error) {
	var out AgentConnection
	row, err := s.queryRow(ctx, `SELECT COALESCE(a.last_source_ip,''),COALESCE(a.last_source_at,'0001-01-01'::timestamptz) FROM np_agent a JOIN np_user u ON lower(u.user_id)=$1 WHERE a.id=$2 AND a.origin='selfhost' AND u.state='active' AND (a.owner_lc=$1 OR u.type='platform_admin')`, reader, id)
	if err != nil {
		return out, err
	}
	err = row.Scan(&out.IP, &out.At)
	if noRows(err) {
		return AgentConnection{}, ErrForbidden
	}
	out.At = out.At.UTC()
	return out, err
}
func (m *MemStore) RecordAgentConnection(_ context.Context, id, ip string, at time.Time) error {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ErrBadRequest
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.agents[id]
	if a == nil || a.Origin != OriginSelfhost || a.State != AgentActive {
		return nil
	}
	if m.connections == nil {
		m.connections = map[string]AgentConnection{}
	}
	if !m.connections[id].At.After(at) {
		m.connections[id] = AgentConnection{IP: parsed.String(), At: at.UTC()}
	}
	return nil
}
func (m *MemStore) ReadAgentConnection(_ context.Context, reader, id string) (AgentConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, u := m.agents[id], m.users[reader]
	if a == nil || u == nil || u.State != UserActive || a.Origin != OriginSelfhost || (a.OwnerLC != reader && u.Type != UserPlatformAdmin) {
		return AgentConnection{}, ErrForbidden
	}
	return m.connections[id], nil
}
