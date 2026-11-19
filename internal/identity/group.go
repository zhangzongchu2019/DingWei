package identity

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// 新平台编组（组长裁定 132246Z：甲 —— identity 建 np_group／np_group_member，成员引 np_agent.id；旧编组数据不迁）。
// 语义按旧平台平移（internal/store/sqlite.go 的 agent_group*）：组名全局唯一、1～24 个字母、数字或下划线（至少一个字母或数字）；组码 32 位随机；
// 建组人管理（删除、关闭添加、剔除）；组长不可剔除；成员的属主可自行退出；关闭添加后不能再入。
// 旧平台为「无建组人的历史组」留的管理分支在新平台不存在（旧数据不迁），故不移植。

// Group 一个编组。
type Group struct {
	Code            string
	ShortName       string
	CreatorLC       string
	LeaderAgentID   string
	AdditionsClosed bool
	LeaderOnly      bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// GroupMember 编组里的一个 Agent。
type GroupMember struct {
	Code      string
	AgentID   string
	CreatedAt time.Time
}

var (
	ErrGroupNameTaken        = errors.New("identity: group name taken")
	ErrGroupClosed           = errors.New("identity: group additions closed")
	ErrGroupMemberExists     = errors.New("identity: agent already in group")
	ErrGroupMemberNotFound   = errors.New("identity: agent not in group")
	ErrGroupLeaderProtected  = errors.New("identity: group leader cannot be removed")
	ErrGroupAgentUnavailable = errors.New("identity: agent unavailable (not found, revoked or not owned)")
	ErrGroupNotFound         = errors.New("identity: group not found")
	ErrGroupNameInvalid      = errors.New("identity: group name invalid")
)

// GroupChoice 批准自备 Agent 或新建托管 Agent 时选的编组：可不选（业主令 15:34:17Z，板 8582acc4：编组不是必须的），
// 选则二选一。
// 新用户名下既无 Agent 也无组：建组要有组长、批准又要选组 ⇒ 允许就地新建（本席建议甲，e5c6244635a6）。
type GroupChoice struct {
	Code    string // 已有组码：须存在且未关闭添加（即 GroupAllowed），同一事务入组
	NewName string // 新组名：同一事务建组，以新 Agent 为组长
	// 两个都空 ⇒ 不入组（Agent.GroupID 为空、不写成员行）
}

// resolveGroupChoice 校验选择并在新建时备好 Group（组码随机）；返回写进 Agent.GroupID 的组码。都不给 ⇒ ("", nil)：不入组。
func (s *Service) resolveGroupChoice(c GroupChoice, ownerLC, agentID string, now time.Time) (string, *Group, error) {
	switch {
	case c.Code == "" && c.NewName == "":
		return "", nil, nil
	case c.Code != "" && c.NewName != "":
		return "", nil, ErrBadRequest
	case c.Code != "":
		return c.Code, nil, nil
	}
	c.NewName = NormalizeGroupName(c.NewName)
	if !ValidGroupName(c.NewName) {
		return "", nil, ErrGroupNameInvalid
	}
	code, err := randFrom(s.Rand, groupCodeAlphabet, GroupCodeLen)
	if err != nil {
		return "", nil, err
	}
	g := &Group{Code: code, ShortName: c.NewName, CreatorLC: Lower(ownerLC), LeaderAgentID: agentID, CreatedAt: now, UpdatedAt: now}
	return code, g, nil
}

// GroupCodeLen 组码长度；字母表与旧平台 randomGroupSuffix 相同。
const (
	GroupCodeLen      = 32
	groupCodeAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
)

// NormalizeGroupName 编组名清洗（R-V2-0102-GROUP-FIXES B）：去掉 Unicode 格式类字符（\p{Cf}：零宽空格／连接符、BOM、U+2060 等，
// 输入法与粘贴常带入、肉眼不可见），再去首尾空白。三个建组入口（编组页、入网审批、托管建席）与 identity 内部都用它，
// 校验与落库用同一个清洗后的值。规则本身（ValidGroupName）不变。
func NormalizeGroupName(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s))
}

// MaxGroupNameRunes 组名上限（按 Unicode 码点计）。
const MaxGroupNameRunes = 24

// ValidGroupName 组名：1～24 个码点，每个是 Unicode 字母（含中文、日文、韩文）、Unicode 数字或 ASCII 下划线，
// 且至少含一个字母或数字（只有下划线不合法）；空格、连字符、点号及其他符号不合法，首尾不留空白。
// 与旧平台 validConsoleAgentGroupName、m8 validConsoleGroupName 同一规则（m8 group_name_rule_parity_test.go 钉住）。
func ValidGroupName(name string) bool {
	if n := utf8.RuneCountInString(name); n < 1 || n > MaxGroupNameRunes {
		return false
	}
	alnum := false
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			alnum = true
		case r == '_':
		default:
			return false
		}
	}
	return alnum
}

// GroupMemberInfo 成员与其 Agent（只含未吊销的 Agent）。
type GroupMemberInfo struct {
	Agent    Agent
	FullName string
	JoinedAt time.Time
}

// GroupInfo 一个编组的完整视图：组长、建组人、成员，以及当前用户能否管理。
type GroupInfo struct {
	Group
	Leader         *Agent // 组长 Agent；已吊销时仍给出（名字照旧），但不在 Members 里
	LeaderFullName string
	Creator        *User
	Members        []GroupMemberInfo
	CanManage      bool
}

// CreateGroup 以 leaderAgentID（须属 actorLC 且未吊销）为组长建组；组名冲突 ⇒ ErrGroupNameTaken。
func (s *Service) CreateGroup(ctx context.Context, actorLC, leaderAgentID, shortName string) (*Group, error) {
	shortName = NormalizeGroupName(shortName)
	if !ValidGroupName(shortName) {
		return nil, ErrBadRequest
	}
	code, err := randFrom(s.Rand, groupCodeAlphabet, GroupCodeLen)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	g := Group{Code: code, ShortName: shortName, CreatorLC: Lower(actorLC), LeaderAgentID: leaderAgentID, CreatedAt: now, UpdatedAt: now}
	leader, err := s.Store.GetAgent(ctx, leaderAgentID)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrGroupAgentUnavailable
	}
	if err != nil {
		return nil, err
	}
	if leader.Origin == OriginExternal {
		store, ok := s.Store.(interface {
			CreateExternalOwnedGroup(context.Context, Group, string) error
		})
		if !ok || !s.ExternalGroups {
			return nil, ErrGroupAgentUnavailable
		}
		err = store.CreateExternalOwnedGroup(ctx, g, s.CommunicationTenantKey)
	} else {
		err = s.Store.CreateGroup(ctx, g)
	}
	if err != nil {
		return nil, err
	}
	s.audit(ctx, g.CreatorLC, "group.create", code, "leader="+leaderAgentID)
	return &g, nil
}

// JoinGroup 把 actorLC 自己的 Agent 加入编组。
func (s *Service) JoinGroup(ctx context.Context, actorLC, code, agentID string) error {
	agent, err := s.Store.GetAgent(ctx, agentID)
	if errors.Is(err, ErrNotFound) {
		return ErrGroupAgentUnavailable
	}
	if err != nil {
		return err
	}
	if agent.Origin == OriginExternal {
		if !s.ExternalGroups {
			return ErrGroupAgentUnavailable
		}
		store, ok := s.Store.(interface {
			JoinExternalGroup(context.Context, string, string, string, string, time.Time) error
		})
		if !ok {
			return ErrGroupAgentUnavailable
		}
		if err := store.JoinExternalGroup(ctx, code, agentID, Lower(actorLC), s.CommunicationTenantKey, s.Now()); err != nil {
			return err
		}
		s.audit(ctx, Lower(actorLC), "group.join", code, "agent="+agentID)
		return nil
	}
	if err := s.Store.JoinGroup(ctx, code, agentID, Lower(actorLC), s.Now()); err != nil {
		return err
	}
	s.audit(ctx, Lower(actorLC), "group.join", code, "agent="+agentID)
	return nil
}

// LeaveGroup 把成员移出编组：成员的属主或建组人可为；组长不可移出。
func (s *Service) LeaveGroup(ctx context.Context, actorLC, code, agentID string) error {
	if err := s.Store.LeaveGroup(ctx, code, agentID, Lower(actorLC)); err != nil {
		return err
	}
	s.audit(ctx, Lower(actorLC), "group.leave", code, "agent="+agentID)
	return nil
}

// SetGroupAdditionsClosed 关闭／重开添加，建组人才可。
func (s *Service) SetGroupAdditionsClosed(ctx context.Context, actorLC, code string, closed bool) error {
	if err := s.Store.SetGroupAdditionsClosed(ctx, code, Lower(actorLC), closed, s.Now()); err != nil {
		return err
	}
	s.audit(ctx, Lower(actorLC), "group.additions", code, fmt.Sprintf("closed=%t", closed))
	return nil
}

// DeleteGroup 删除编组（成员关系随之删除），建组人才可。
func (s *Service) DeleteGroup(ctx context.Context, actorLC, code string) error {
	if err := s.Store.DeleteGroup(ctx, code, Lower(actorLC)); err != nil {
		return err
	}
	s.audit(ctx, Lower(actorLC), "group.delete", code, "")
	return nil
}

// ListVisibleGroups actorLC 看得见的编组：自己建的，或成员里有自己（未吊销）的 Agent。顺序同 ListGroups。
func (s *Service) ListVisibleGroups(ctx context.Context, actorLC string) ([]GroupInfo, error) {
	actor := Lower(actorLC)
	groups, err := s.Store.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]GroupInfo, 0)
	for _, g := range groups {
		info, err := s.groupInfo(ctx, g, actor)
		if err != nil {
			return nil, err
		}
		visible := info.CanManage
		for _, m := range info.Members {
			visible = visible || m.Agent.OwnerLC == actor
		}
		if visible {
			out = append(out, *info)
		}
	}
	return out, nil
}

func (s *Service) groupInfo(ctx context.Context, g Group, actor string) (*GroupInfo, error) {
	info := &GroupInfo{Group: g, CanManage: g.CreatorLC == actor}
	if leader, err := s.Store.GetAgent(ctx, g.LeaderAgentID); err == nil {
		info.Leader, info.LeaderFullName = leader, s.fullName(ctx, leader)
	}
	if creator, err := s.Store.GetUser(ctx, g.CreatorLC); err == nil {
		info.Creator = creator
	}
	members, err := s.Store.ListGroupMembers(ctx, g.Code)
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		ag, err := s.Store.GetAgent(ctx, m.AgentID)
		if err != nil || !ag.Current() {
			continue // 已吊销的 Agent 不再算成员（旧成员行留作历史，不展示）
		}
		info.Members = append(info.Members, GroupMemberInfo{Agent: *ag, FullName: s.fullName(ctx, ag), JoinedAt: m.CreatedAt})
	}
	return info, nil
}

// SuggestGroupNames 组名冲突时给的备选：原名截断后接数字，避开已有组名（与旧平台 suggestedConsoleGroupNames 同法）。
func (s *Service) SuggestGroupNames(ctx context.Context, name string, count int) ([]string, error) {
	groups, err := s.Store.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	used := make(map[string]bool, len(groups))
	for _, g := range groups {
		used[g.ShortName] = true
	}
	out := make([]string, 0, count)
	runes := []rune(name)
	for suffix := 1; len(out) < count; suffix++ {
		digits := []rune(fmt.Sprint(suffix))
		base := runes
		if limit := MaxGroupNameRunes - len(digits); len(base) > limit {
			base = base[:limit]
		}
		if c := string(base) + string(digits); !used[c] {
			out = append(out, c)
			used[c] = true
		}
	}
	return out, nil
}

// sortGroups 编组的确定顺序：建组时刻升序，同时刻按组码。
func sortGroups(gs []Group) {
	sort.Slice(gs, func(i, j int) bool {
		if !gs[i].CreatedAt.Equal(gs[j].CreatedAt) {
			return gs[i].CreatedAt.Before(gs[j].CreatedAt)
		}
		return gs[i].Code < gs[j].Code
	})
}

// sortGroupMembers 成员的确定顺序：入组时刻升序，同时刻按 agent_id。
func sortGroupMembers(ms []GroupMember) {
	sort.Slice(ms, func(i, j int) bool {
		if !ms[i].CreatedAt.Equal(ms[j].CreatedAt) {
			return ms[i].CreatedAt.Before(ms[j].CreatedAt)
		}
		return ms[i].AgentID < ms[j].AgentID
	})
}

// MentionCandidate V11 ＠候选的一项（不含在线状态与 CLI 类型，那两样在 Hub）。
type MentionCandidate struct {
	Agent    Agent
	FullName string
	// Insert uses the public full name for another owner. Same-owner unique short names remain short.
	// 并集算上自己：自己与某候选同短名时，写短名对 Hub 来说就是歧义，必须写全名。
	Insert string
}

// MentionCandidates V11（组长 9f0cd6ab）：selfID 所在全部编组的成员并集，去掉自己与已吊销的；只列 Agent。
// 顺序由调用方定（Hub 按在线优先、再按短名）。
func (s *Service) MentionCandidates(ctx context.Context, selfID string) ([]MentionCandidate, error) {
	codes, err := s.Store.ListAgentGroups(ctx, selfID)
	if err != nil {
		return nil, err
	}
	union := map[string]*Agent{}
	for _, code := range codes {
		members, err := s.Store.ListGroupMembers(ctx, code)
		if err != nil {
			return nil, err
		}
		for _, m := range members {
			if _, seen := union[m.AgentID]; seen {
				continue
			}
			ag, err := s.Store.GetAgent(ctx, m.AgentID)
			if err != nil || !ag.Current() {
				continue
			}
			union[m.AgentID] = ag
		}
	}
	names := map[string]int{}
	for _, ag := range union {
		names[ag.NameLC()]++
	}
	out := make([]MentionCandidate, 0, len(union))
	for id, ag := range union {
		if id == selfID {
			continue
		}
		allowed, err := s.AgentsCanCommunicate(ctx, union[selfID], ag)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		c := MentionCandidate{Agent: *ag, FullName: s.fullName(ctx, ag)}
		c.Insert = c.FullName
		if union[selfID] != nil && ag.OwnerLC == union[selfID].OwnerLC && names[ag.NameLC()] == 1 {
			c.Insert = ag.Name
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent.ID < out[j].Agent.ID })
	return out, nil
}

// SetGroupLeaderOnly changes the group communication policy; only its creator can change it.
func (s *Service) SetGroupLeaderOnly(ctx context.Context, actorLC, code string, leaderOnly bool) error {
	return s.Store.SetGroupLeaderOnly(ctx, code, Lower(actorLC), leaderOnly, s.Now())
}

// AgentsCanCommunicate is also the visibility policy for online Agent directories.
// Shared leader-only groups deny member-to-member communication even if another shared group allows it.
// Ungrouped Agents have no implicit owner-wide group.
func (s *Service) AgentsCanCommunicate(ctx context.Context, source, target *Agent) (bool, error) {
	if source == nil || target == nil || source.State != AgentActive || target.State != AgentActive {
		return false, nil
	}
	if source.ID == target.ID {
		return true, nil
	}
	if source.Origin == OriginExternal || target.Origin == OriginExternal {
		store, ok := s.Store.(interface {
			ExternalAgentsCanCommunicate(context.Context, string, string, string, time.Time) (bool, error)
		})
		if !ok {
			return false, nil
		}
		return store.ExternalAgentsCanCommunicate(ctx, source.ID, target.ID, s.CommunicationTenantKey, s.Now())
	}
	sourceCodes, err := s.Store.ListAgentGroups(ctx, source.ID)
	if err != nil {
		return false, err
	}
	targetCodes, err := s.Store.ListAgentGroups(ctx, target.ID)
	if err != nil {
		return false, err
	}
	targetSet := map[string]bool{}
	for _, code := range targetCodes {
		targetSet[code] = true
	}
	allowed := false
	for _, code := range sourceCodes {
		if !targetSet[code] {
			continue
		}
		g, err := s.Store.GetGroup(ctx, code)
		if err != nil {
			return false, err
		}
		if g.LeaderOnly && source.ID != g.LeaderAgentID && target.ID != g.LeaderAgentID {
			return false, nil
		}
		allowed = true
	}
	return allowed, nil
}
