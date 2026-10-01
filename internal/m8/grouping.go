package m8

import (
	"context"
	"errors"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

const groupAgentNotFoundText = "组内找不到该Agent"

var errGroupAgentNotFound = errors.New(groupAgentNotFoundText)

type groupingRejection struct{ reason string }

func (err groupingRejection) Error() string { return groupAgentNotFoundText }
func (err groupingRejection) Is(target error) bool {
	return target == errGroupAgentNotFound
}

func newGroupingRejection(reason string) error {
	return groupingRejection{reason: reason}
}

func groupingRejectionReason(err error) string {
	var rejection groupingRejection
	if errors.As(err, &rejection) && rejection.reason != "" {
		return rejection.reason
	}
	return "cross_group_denied"
}

type agentEndpointIdentity struct {
	KeyID, SessionName, OwnerKey string
}

type groupingConsoleOwnerRepository interface {
	ResolveConsoleOwnerScope(context.Context, string) (string, error)
}

func (h *Hub) endpointIdentity(ctx context.Context, keyID, sessionName string) (agentEndpointIdentity, bool, error) {
	if h.Repo == nil || keyID == "" || sessionName == "" {
		return agentEndpointIdentity{}, false, nil
	}
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return agentEndpointIdentity{}, false, err
	}
	for _, endpoint := range endpoints {
		if endpoint.KeyID == keyID && endpoint.SessionName == sessionName && endpoint.Active {
			return agentEndpointIdentity{KeyID: keyID, SessionName: sessionName, OwnerKey: endpoint.OwnerKey}, true, nil
		}
	}
	owner := h.ownerKeyForKey(ctx, keyID)
	if owner == "" {
		return agentEndpointIdentity{}, false, nil
	}
	return agentEndpointIdentity{KeyID: keyID, SessionName: sessionName, OwnerKey: owner}, true, nil
}

func (h *Hub) sourceAgentIdentity(ctx context.Context, from address, env model.Envelope) (agentEndpointIdentity, bool, error) {
	if from.Kind != addressSession || from.SessionName == systemAddressName {
		return agentEndpointIdentity{}, false, nil
	}
	keyID, sessionName := from.KeyID, from.SessionName
	// Cross-owner selector preparation rewrites the wire address to the target
	// key. These Hub-authored fields retain the actual sender identity.
	if sourceKey := metaString(env.Meta, "source_key_id"); sourceKey != "" {
		keyID = sourceKey
		if sourceSession := metaString(env.Meta, "source_session_name"); sourceSession != "" {
			sessionName = sourceSession
		}
	}
	return h.endpointIdentity(ctx, keyID, sessionName)
}

func (h *Hub) ownerSenderExempt(ctx context.Context, from address, env model.Envelope) (bool, error) {
	if metaString(env.Meta, hubIngressMetaKey) == hubIngressViewV2 {
		principalID := metaString(env.Meta, "console_principal_id")
		repo, ok := h.Repo.(groupingConsoleOwnerRepository)
		if principalID == "" || !ok {
			return false, nil
		}
		ownerScope, err := repo.ResolveConsoleOwnerScope(ctx, principalID)
		return ownerScope != "", err
	}
	if from.Kind != addressFeishu || h.Repo == nil {
		return false, nil
	}
	openID := strings.TrimSpace(from.OpenID)
	if sender := metaString(env.Meta, "sender_open_id"); sender != "" {
		openID = sender
	}
	if sender := metaString(env.Meta, "source_sender_openid"); sender != "" {
		openID = sender
	}
	if openID == "" {
		return false, nil
	}
	members, err := h.Repo.ListMembers(ctx)
	if err != nil {
		return false, err
	}
	for _, member := range members {
		if member.Active && strings.TrimSpace(member.FeishuOpenID) == openID {
			return true, nil
		}
	}
	return false, nil
}

func (h *Hub) groupingKeys(ctx context.Context, endpoint agentEndpointIdentity) (map[string]bool, error) {
	groups, err := h.Repo.ListAgentGroupsForEndpoint(ctx, endpoint.KeyID, endpoint.SessionName)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(groups)+1)
	for _, group := range groups {
		keys[group.Code] = true
	}
	if len(keys) == 0 {
		// The default group is deliberately owner-scoped. It preserves existing
		// same-owner communication without silently joining every owner globally.
		keys["default-owner:"+endpoint.OwnerKey] = true
	}
	return keys, nil
}

func (h *Hub) enforceAgentGrouping(ctx context.Context, from address, env model.Envelope, targetKeyID, targetSession string) error {
	if h.Identity != nil {
		return h.enforceNewPlatformGrouping(ctx, from, env, targetKeyID, targetSession)
	}
	if h.Repo == nil {
		return nil
	}
	groups, err := h.Repo.ListAgentGroups(ctx)
	if err != nil {
		return err
	}
	// An empty grouping table is the migration/rollback state. Preserve the
	// pre-G routing behavior until an owner creates the first explicit group.
	if len(groups) == 0 {
		activated, stateErr := h.Repo.AgentGroupingActivated(ctx)
		if stateErr != nil {
			return stateErr
		}
		if !activated {
			return nil
		}
	}
	if exempt, err := h.ownerSenderExempt(ctx, from, env); err != nil || exempt {
		return err
	}
	// The channel is not an authority boundary. A Feishu sender who is not an
	// active registered owner gets no implicit group membership merely because
	// the message arrived through the same bot/channel.
	if from.Kind == addressFeishu {
		return newGroupingRejection("sender_not_active_member")
	}
	if metaString(env.Meta, hubIngressMetaKey) == hubIngressViewV2 {
		return newGroupingRejection("sender_not_active_member")
	}
	source, isAgent, err := h.sourceAgentIdentity(ctx, from, env)
	if err != nil || !isAgent {
		return err
	}
	target, found, err := h.endpointIdentity(ctx, targetKeyID, targetSession)
	if err != nil {
		return err
	}
	if !found {
		return newGroupingRejection("target_endpoint_not_found")
	}
	sourceGroups, err := h.groupingKeys(ctx, source)
	if err != nil {
		return err
	}
	targetGroups, err := h.groupingKeys(ctx, target)
	if err != nil {
		return err
	}
	for code := range sourceGroups {
		if targetGroups[code] {
			return nil
		}
	}
	return newGroupingRejection("cross_group_denied")
}

func (h *Hub) resolveSessionByFullName(ctx context.Context, fullName string) (model.SessionEndpoint, bool, error) {
	if h.Repo == nil || strings.TrimSpace(fullName) == "" {
		return model.SessionEndpoint{}, false, nil
	}
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return model.SessionEndpoint{}, false, err
	}
	var hit model.SessionEndpoint
	count := 0
	for _, endpoint := range endpoints {
		if endpoint.Active && (endpoint.SessionName == fullName || (endpoint.FullSessionName != "" && endpoint.FullSessionName == fullName)) {
			hit = endpoint
			count++
		}
	}
	return hit, count == 1, nil
}
