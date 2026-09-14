package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

type consoleSeedStore interface {
	UpsertConsolePrincipal(context.Context, model.ConsolePrincipal) error
	UpsertConsoleAgent(context.Context, model.ConsoleAgent) error
	UpsertConsoleAgentGrant(context.Context, model.ConsoleAgentGrant) error
}

type consoleSeed struct {
	Principals []model.ConsolePrincipal `json:"principals"`
	Agents     []struct {
		model.ConsoleAgent
		Grants []struct {
			PrincipalID string `json:"principal_id"`
			Permission  string `json:"permission"`
		} `json:"grants"`
	} `json:"agents"`
}

// seedConsoleACL is the only bootstrap writer for the v0.1 console ACL. The
// file is an explicit declaration; it never derives ownership from member,
// seen_person, key_id, or a session-name prefix.
func seedConsoleACL(ctx context.Context, st consoleSeedStore, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("WP_CONSOLE_SEED_FILE is required when console auth is enabled")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read console seed: %w", err)
	}
	var seed consoleSeed
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&seed); err != nil {
		return fmt.Errorf("parse console seed: %w", err)
	}
	if len(seed.Principals) == 0 || len(seed.Agents) == 0 {
		return errors.New("console seed requires principals and agents")
	}
	for _, p := range seed.Principals {
		if err := st.UpsertConsolePrincipal(ctx, p); err != nil {
			return fmt.Errorf("seed principal %s: %w", p.ID, err)
		}
	}
	for _, entry := range seed.Agents {
		if len(entry.Grants) == 0 {
			return fmt.Errorf("agent %s has no explicit grants", entry.ID)
		}
		if err := st.UpsertConsoleAgent(ctx, entry.ConsoleAgent); err != nil {
			return fmt.Errorf("seed agent %s: %w", entry.ID, err)
		}
		for _, grant := range entry.Grants {
			if err := st.UpsertConsoleAgentGrant(ctx, model.ConsoleAgentGrant{PrincipalID: grant.PrincipalID, AgentID: entry.ID, Permission: grant.Permission, Active: true}); err != nil {
				return fmt.Errorf("seed grant %s: %w", entry.ID, err)
			}
		}
	}
	return nil
}
