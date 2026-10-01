package identity

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
	"unicode"
	"unicode/utf8"
)

// 新平台编组（组长裁定 132246Z：甲 —— identity 建 np_group／np_group_member，成员引 np_agent.id；旧编组数据不迁）。
// 语义按旧平台平移（internal/store/sqlite.go 的 agent_group*）：组名全局唯一、1～8 个字母或数字；组码 32 位随机；
// 建组人管理（删除、关闭添加、剔除）；组长不可剔除；成员的属主可自行退出；关闭添加后不能再入。
// 旧平台为「无建组人的历史组」留的管理分支在新平台不存在（旧数据不迁），故不移植。

// Group 一个编组。
type Group struct {
	Code            string
	ShortName       string
	CreatorLC       string
	LeaderAgentID   string
	AdditionsClosed bool
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
)

// GroupCodeLen 组码长度；字母表与旧平台 randomGroupSuffix 相同。
const (
	GroupCodeLen      = 32
	groupCodeAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
)

// ValidGroupName 组名：1～8 个字母或数字（含中文等 Unicode 字母），首尾不留空白（与旧平台 validConsoleAgentGroupName 同）。
func ValidGroupName(name string) bool {
	if n := utf8.RuneCountInString(name); n < 1 || n > 8 {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return false
		}
	}
	return true
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
	if !ValidGroupName(shortName) {
		return nil, ErrBadRequest
	}
	code, err := randFrom(s.Rand, groupCodeAlphabet, GroupCodeLen)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	g := Group{Code: code, ShortName: shortName, CreatorLC: Lower(actorLC), LeaderAgentID: leaderAgentID, CreatedAt: now, UpdatedAt: now}
	if err := s.Store.CreateGroup(ctx, g); err != nil {
		return nil, err
	}
	s.audit(ctx, g.CreatorLC, "group.create", code, "leader="+leaderAgentID)
	return &g, nil
}

// JoinGroup 把 actorLC 自己的 Agent 加入编组。
func (s *Service) JoinGroup(ctx context.Context, actorLC, code, agentID string) error {
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
		if limit := 8 - len(digits); len(base) > limit {
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
	// Insert 选中后插入的写法：在「所在全部编组成员的并集」（含自己）里短名唯一 ⇒ 短名，否则 full_name（业主裁定 9.5）。
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
		c := MentionCandidate{Agent: *ag, FullName: s.fullName(ctx, ag)}
		c.Insert = c.FullName
		if names[ag.NameLC()] == 1 {
			c.Insert = ag.Name
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent.ID < out[j].Agent.ID })
	return out, nil
}
