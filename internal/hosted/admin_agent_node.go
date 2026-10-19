package hosted

import (
	"context"
	"database/sql"
	"errors"
)

// AgentNodeName is a read-only inventory projection. It needs neither a master
// key nor the creation orchestrator and never reads account/credential columns.
// A database that has never enabled hosted seats may have no hosted tables.
func (s *Store) AgentNodeName(ctx context.Context, agentID string) (string, error) {
	row, err := s.qrow(ctx, `SELECT n.name FROM g4_hosted_seat s JOIN g4_node n ON n.id=s.node_id WHERE s.agent_id=$1`, agentID)
	var name string
	if err == nil {
		err = row.Scan(&name)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) && state.SQLState() == "42P01" {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return name, nil
}
