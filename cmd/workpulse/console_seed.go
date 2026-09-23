package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

type consoleSeedStore interface {
	UpsertConsolePrincipal(context.Context, model.ConsolePrincipal) error
	UpsertConsoleAgent(context.Context, model.ConsoleAgent) error
	UpsertConsoleAgentGrant(context.Context, model.ConsoleAgentGrant) error
	UpsertConsolePrincipalRole(context.Context, model.ConsolePrincipalRole) error
	UpsertConsolePlatformRole(context.Context, model.ConsolePlatformRole) error
}

type consoleSeed struct {
	Principals []struct {
		ID              string `json:"id"`
		Provider        string `json:"provider"`
		ProviderIssuer  string `json:"provider_issuer"`
		ProviderSubject string `json:"provider_subject"`
		DisplayName     string `json:"display_name"`
		Active          *bool  `json:"active"`
	} `json:"principals"`
	Roles []struct {
		PrincipalID string `json:"principal_id"`
		Role        string `json:"role"`
		OwnerKey    string `json:"owner_key"`
		Active      *bool  `json:"active"`
	} `json:"roles"`
	PlatformRoles []struct {
		PrincipalID string `json:"principal_id"`
		Role        string `json:"role"`
		Active      *bool  `json:"active"`
	} `json:"platform_roles"`
	Agents []struct {
		ID             string `json:"id"`
		KeyID          string `json:"key_id"`
		SessionName    string `json:"session_name"`
		OwnerPrincipal string `json:"owner_principal"`
		Active         *bool  `json:"active"`
		Grants         []struct {
			PrincipalID string `json:"principal_id"`
			Permission  string `json:"permission"`
		} `json:"grants"`
	} `json:"agents"`
}

// seedConsoleACL is the only bootstrap writer for the v0.1 console ACL. The
// file is an explicit declaration; it never derives ownership from member,
// seen_person, key_id, or a session-name prefix.
func seedConsoleACL(ctx context.Context, st consoleSeedStore, path string) error {
	seed, err := parseConsoleSeed(path)
	if err != nil {
		return err
	}
	if err := validateConsoleSeed(seed); err != nil {
		return err
	}
	for _, p := range seed.Principals {
		value := model.ConsolePrincipal{ID: p.ID, Provider: p.Provider, ProviderIssuer: p.ProviderIssuer, ProviderSubject: p.ProviderSubject, DisplayName: p.DisplayName, Active: *p.Active}
		if err := st.UpsertConsolePrincipal(ctx, value); err != nil {
			return fmt.Errorf("seed principal %s: %w", p.ID, err)
		}
	}
	for _, entry := range seed.Roles {
		value := model.ConsolePrincipalRole{PrincipalID: entry.PrincipalID, Role: entry.Role, OwnerKey: entry.OwnerKey, Active: *entry.Active}
		if err := st.UpsertConsolePrincipalRole(ctx, value); err != nil {
			return fmt.Errorf("seed principal role %s/%s: %w", entry.PrincipalID, entry.Role, err)
		}
	}
	for _, entry := range seed.PlatformRoles {
		value := model.ConsolePlatformRole{PrincipalID: entry.PrincipalID, Role: entry.Role, Active: *entry.Active}
		if err := st.UpsertConsolePlatformRole(ctx, value); err != nil {
			return fmt.Errorf("seed platform role %s/%s: %w", entry.PrincipalID, entry.Role, err)
		}
	}
	for _, entry := range seed.Agents {
		value := model.ConsoleAgent{ID: entry.ID, KeyID: entry.KeyID, SessionName: entry.SessionName, OwnerPrincipal: entry.OwnerPrincipal, Active: *entry.Active}
		if err := st.UpsertConsoleAgent(ctx, value); err != nil {
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

func validateConsoleSeedFile(path string) error {
	seed, err := parseConsoleSeed(path)
	if err != nil {
		return err
	}
	return validateConsoleSeed(seed)
}

func parseConsoleSeed(path string) (consoleSeed, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return consoleSeed{}, errors.New("console seed path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return consoleSeed{}, fmt.Errorf("read console seed: %w", err)
	}
	var seed consoleSeed
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&seed); err != nil {
		return consoleSeed{}, fmt.Errorf("parse console seed: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return consoleSeed{}, errors.New("parse console seed: trailing JSON value")
	}
	return seed, nil
}

func validateConsoleSeed(seed consoleSeed) error {
	if len(seed.Principals) == 0 || len(seed.Agents) == 0 {
		return errors.New("console seed requires principals and agents")
	}
	principals := make(map[string]bool, len(seed.Principals))
	shortNames := make(map[string]string, len(seed.Principals))
	for _, p := range seed.Principals {
		if p.Active == nil {
			return fmt.Errorf("principal %s: active is required", p.ID)
		}
		short := strings.TrimSpace(p.DisplayName)
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Provider) == "" || strings.TrimSpace(p.ProviderIssuer) == "" || strings.TrimSpace(p.ProviderSubject) == "" || short == "" {
			return fmt.Errorf("principal %s: identity fields are required", p.ID)
		}
		if prior, exists := shortNames[short]; exists {
			return fmt.Errorf("principal display_name %q conflicts: %s,%s; owner must choose a unique name", short, prior, p.ID)
		}
		shortNames[short] = p.ID
		if principals[p.ID] {
			return fmt.Errorf("principal %s: duplicate id", p.ID)
		}
		principals[p.ID] = true
	}
	for _, entry := range seed.Roles {
		if entry.Active == nil {
			return fmt.Errorf("principal role %s/%s: active is required", entry.PrincipalID, entry.Role)
		}
		if !principals[entry.PrincipalID] || entry.Role != "owner" || strings.TrimSpace(entry.OwnerKey) == "" {
			return fmt.Errorf("principal role %s/%s: invalid role or owner scope", entry.PrincipalID, entry.Role)
		}
	}
	for _, entry := range seed.PlatformRoles {
		if entry.Active == nil {
			return fmt.Errorf("platform role %s/%s: active is required", entry.PrincipalID, entry.Role)
		}
		if !principals[entry.PrincipalID] || (entry.Role != "management" && entry.Role != "owner") {
			return fmt.Errorf("platform role %s/%s: invalid role", entry.PrincipalID, entry.Role)
		}
	}
	agents := make(map[string]bool, len(seed.Agents))
	for _, entry := range seed.Agents {
		if entry.Active == nil {
			return fmt.Errorf("agent %s: active is required", entry.ID)
		}
		if strings.TrimSpace(entry.ID) == "" || strings.TrimSpace(entry.KeyID) == "" || strings.TrimSpace(entry.SessionName) == "" || !principals[entry.OwnerPrincipal] {
			return fmt.Errorf("agent %s: identity fields and declared owner are required", entry.ID)
		}
		if agents[entry.ID] {
			return fmt.Errorf("agent %s: duplicate id", entry.ID)
		}
		agents[entry.ID] = true
		if len(entry.Grants) == 0 {
			return fmt.Errorf("agent %s has no explicit grants", entry.ID)
		}
		for _, grant := range entry.Grants {
			if !principals[grant.PrincipalID] || (grant.Permission != "view" && grant.Permission != "interact") {
				return fmt.Errorf("agent %s: invalid grant", entry.ID)
			}
		}
	}
	return nil
}
