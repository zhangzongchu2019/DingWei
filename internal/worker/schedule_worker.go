// Package worker wires inbound messages to deterministic WorkPulse handlers.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/router"
	"github.com/zhangzongchu2019/dingwei/internal/schedule"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// Processor consumes inbound messages and emits plain-text receipts.
type Processor struct {
	Inbound  bus.Queue
	Outbound bus.Queue
	Feishu   feishu.Gateway
	Schedule *schedule.Service
	Repo     store.Repository
	Prefix   PrefixDispatcher
	Coord    ChangeCoordinator
	Triggers []string
}

type PrefixDispatcher interface {
	Dispatch(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, error)
}

type ChangeCoordinator interface {
	OnProgressChanged(ctx context.Context, ownerKey, taskKey, note string) error
}

// ProcessOne claims and handles one inbound message. It returns false when the queue is empty.
func (p *Processor) ProcessOne(ctx context.Context) (bool, error) {
	msg, err := p.Inbound.Dequeue(ctx)
	if err != nil {
		return false, err
	}
	if msg == nil {
		return false, nil
	}
	if err := p.handle(ctx, *msg); err != nil {
		_ = p.Inbound.Fail(ctx, msg.ID, err.Error())
		return true, err
	}
	if err := p.Inbound.Ack(ctx, msg.ID); err != nil {
		return true, err
	}
	return true, nil
}

// Run polls the inbound queue until ctx is cancelled.
func (p *Processor) Run(ctx context.Context, idle time.Duration) {
	if idle <= 0 {
		idle = 200 * time.Millisecond
	}
	for {
		processed, _ := p.ProcessOne(ctx)
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(idle):
		}
	}
}

func (p *Processor) handle(ctx context.Context, msg model.Message) error {
	text := extractText(msg.Content)
	if p.Prefix != nil {
		result, err := p.Prefix.Dispatch(ctx, msg, text)
		if err != nil {
			return err
		}
		if result.Matched {
			if result.Reply == "" {
				return nil
			}
			return p.sendReply(ctx, msg, result.Reply)
		}
		return nil
	}
	decision := router.Decide(text, p.Triggers)
	if decision.Level != router.LevelCommand {
		if reply, err := p.handleQuery(ctx, msg, text); err != nil {
			reply = "处理失败：" + err.Error()
			return p.sendReply(ctx, msg, reply)
		} else if reply != "" {
			return p.sendReply(ctx, msg, reply)
		}
		return nil
	}

	reply, err := p.handleCommand(ctx, msg, decision.Command, text)
	if err != nil {
		reply = "处理失败：" + err.Error()
	}
	if reply == "" {
		return nil
	}
	return p.sendReply(ctx, msg, reply)
}

func (p *Processor) handleCommand(ctx context.Context, msg model.Message, cmd router.Command, text string) (string, error) {
	if p.Schedule == nil {
		return "", errors.New("schedule service not configured")
	}
	owner := ownerKey(msg)
	actor, err := p.actor(ctx, owner)
	if err != nil {
		return "", err
	}
	switch cmd {
	case router.CmdConfirm:
		if actor.Role == model.RoleCollaborator {
			return "权限不足：协作角色不能确认排期变更。", nil
		}
		return p.Schedule.Confirm(ctx, owner)
	case router.CmdCancel:
		return p.Schedule.Cancel(ctx, owner)
	case router.CmdAppeal:
		return p.handleAppeal(ctx, owner, text)
	case router.CmdAdd, router.CmdDelete, router.CmdModify, router.CmdPostpone, router.CmdReplace:
		if actor.Role == model.RoleCollaborator {
			return "权限不足：协作角色不能修改排期，可提交进度、结果或风险。", nil
		}
		return p.Schedule.Handle(ctx, owner, text)
	case router.CmdProgress, router.CmdResult, router.CmdDone:
		return p.handleProgress(ctx, owner, cmd, text)
	case router.CmdRisk:
		return p.handleRisk(ctx, owner, text)
	case router.CmdHelp:
		return "排期：+ MM/DD-MM/DD 任务；- 关键词；改 关键词 MM/DD-MM/DD；顺延 MM/DD +N天；全量 后跟多行排期；确认 / 取消 / 申诉。\n进度：进度 关键词 内容；完成 关键词；结果 关键词 内容。\n风险：风险 内容；风险解除 关键词。\n查询：我的排期 / 我的进度 / 我的风险 / 本周谁在做什么。", nil
	default:
		return "暂不支持该指令。", nil
	}
}

func (p *Processor) handleAppeal(ctx context.Context, owner, text string) (string, error) {
	repo := p.repo()
	pending, err := repo.GetPending(ctx, owner)
	if err != nil {
		return "", err
	}
	if pending == nil {
		return "没有可申诉的待确认变更。", nil
	}
	if err := repo.SetPendingStatus(ctx, pending.ID, "cancelled"); err != nil {
		return "", err
	}
	reason := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "申诉"))
	if reason == "" {
		reason = "未填写原因"
	}
	_ = repo.WriteAudit(ctx, owner, "schedule_appeal", reason)
	if err := p.notifyManagers(ctx, fmt.Sprintf("排期代改申诉：%s 申诉待确认变更，原因：%s", owner, reason)); err != nil {
		return "", err
	}
	return "已提交申诉，并取消本次待确认变更。", nil
}

func (p *Processor) handleProgress(ctx context.Context, owner string, cmd router.Command, text string) (string, error) {
	repo := p.repo()
	taskKey, note, err := parseProgressText(cmd, text)
	if err != nil {
		return "", err
	}
	schedules, err := repo.ListSchedules(ctx, owner)
	if err != nil {
		return "", err
	}
	matches := matchSchedules(schedules, taskKey)
	if len(matches) > 0 {
		taskKey = matches[0].Task
	}
	percent := 0
	if cmd == router.CmdDone {
		percent = 100
		note = "完成"
		for _, sc := range matches {
			sc.Status = "done"
			if err := repo.UpsertSchedule(ctx, sc); err != nil {
				return "", err
			}
		}
	}
	if err := repo.AddProgress(ctx, model.Progress{
		OwnerKey: owner,
		TaskKey:  taskKey,
		Note:     note,
		Percent:  percent,
		Source:   "self",
	}); err != nil {
		return "", err
	}
	coordWarning := ""
	if p.Coord != nil {
		if err := p.Coord.OnProgressChanged(ctx, owner, taskKey, note); err != nil {
			coordWarning = fmt.Sprintf("；但变更联动失败：%v", err)
		}
	}
	if len(matches) == 0 {
		return fmt.Sprintf("已记录进度：%s。未匹配到排期关键词，将按独立事项留痕%s。", taskKey, coordWarning), nil
	}
	return fmt.Sprintf("已记录进度：%s%s。", taskKey, coordWarning), nil
}

func (p *Processor) notifyManagers(ctx context.Context, text string) error {
	if p.Outbound == nil {
		return nil
	}
	members, err := p.repo().ListMembers(ctx)
	if err != nil {
		return err
	}
	content, _ := json.Marshal(map[string]string{"text": text})
	for _, m := range members {
		if m.Active && m.Role == model.RoleManager {
			if err := p.Outbound.Enqueue(ctx, model.Message{
				ChatEntityID: m.OwnerKey,
				BotChannelID: botChannel(m.OwnerKey),
				ChatType:     model.ChatPersonal,
				Content:      string(content),
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Processor) handleRisk(ctx context.Context, owner, text string) (string, error) {
	repo := p.repo()
	content := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "风险"))
	if strings.HasPrefix(content, "解除") {
		kw := strings.TrimSpace(strings.TrimPrefix(content, "解除"))
		if kw == "" {
			return "", errors.New("风险解除格式：风险解除 关键词")
		}
		n, err := repo.ResolveRisks(ctx, owner, kw)
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "未匹配到待解除风险。", nil
		}
		return fmt.Sprintf("已解除 %d 条风险。", n), nil
	}
	if content == "" {
		return "", errors.New("风险格式：风险 内容")
	}
	reporters, _ := json.Marshal([]string{owner})
	if err := repo.ReportRisk(ctx, model.Risk{
		OwnerKey:      owner,
		Content:       content,
		Status:        "open",
		ReportersJSON: string(reporters),
	}); err != nil {
		return "", err
	}
	return "已记录风险，并归集到风险列表。", nil
}

func botChannel(ownerKey string) string {
	if i := strings.Index(ownerKey, ":"); i > 0 {
		return ownerKey[:i]
	}
	return "default"
}

func (p *Processor) handleQuery(ctx context.Context, msg model.Message, text string) (string, error) {
	t := strings.TrimSpace(text)
	if t == "" {
		return "", nil
	}
	owner := ownerKey(msg)
	actor, err := p.actor(ctx, owner)
	if err != nil {
		return "", err
	}
	switch {
	case strings.Contains(t, "我的排期"):
		return p.renderSchedule(ctx, owner)
	case strings.Contains(t, "我的进度"):
		return p.renderProgress(ctx, owner)
	case strings.Contains(t, "我的风险"):
		return p.renderRisks(ctx, owner)
	case strings.Contains(t, "本周谁在做什么") || strings.Contains(t, "团队排期"):
		if actor.Role != model.RoleManager {
			return "权限不足：仅管理者可查询团队排期。", nil
		}
		return p.renderTeamSchedule(ctx)
	}
	return "", nil
}

func (p *Processor) renderSchedule(ctx context.Context, owner string) (string, error) {
	items, err := p.repo().ListSchedules(ctx, owner)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "当前没有排期。", nil
	}
	var b strings.Builder
	b.WriteString("我的排期：")
	for _, sc := range items {
		fmt.Fprintf(&b, "\n- %s~%s %s [%s]", sc.StartDate, sc.EndDate, sc.Task, sc.Status)
	}
	return b.String(), nil
}

func (p *Processor) renderProgress(ctx context.Context, owner string) (string, error) {
	items, err := p.repo().LatestProgress(ctx, owner)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "当前没有进度记录。", nil
	}
	var b strings.Builder
	b.WriteString("我的最新进度：")
	for _, it := range items {
		fmt.Fprintf(&b, "\n- %s：%s", it.TaskKey, it.Note)
		if it.Percent > 0 {
			fmt.Fprintf(&b, "（%d%%）", it.Percent)
		}
	}
	return b.String(), nil
}

func (p *Processor) renderRisks(ctx context.Context, owner string) (string, error) {
	items, err := p.repo().ListOpenRisks(ctx, owner)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "当前没有未解除风险。", nil
	}
	var b strings.Builder
	b.WriteString("我的未解除风险：")
	for _, it := range items {
		fmt.Fprintf(&b, "\n- %s", it.Content)
	}
	return b.String(), nil
}

func (p *Processor) renderTeamSchedule(ctx context.Context) (string, error) {
	members, err := p.repo().ListMembers(ctx)
	if err != nil {
		return "", err
	}
	if len(members) == 0 {
		return "当前没有成员配置。", nil
	}
	var b strings.Builder
	b.WriteString("团队排期：")
	for _, m := range members {
		items, err := p.repo().ListSchedules(ctx, m.OwnerKey)
		if err != nil {
			return "", err
		}
		if len(items) == 0 {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = m.OwnerKey
		}
		fmt.Fprintf(&b, "\n%s:", name)
		for _, sc := range items {
			fmt.Fprintf(&b, "\n- %s~%s %s [%s]", sc.StartDate, sc.EndDate, sc.Task, sc.Status)
		}
	}
	if b.String() == "团队排期：" {
		return "当前没有团队排期。", nil
	}
	return b.String(), nil
}

func (p *Processor) sendReply(ctx context.Context, msg model.Message, text string) error {
	if p.Feishu == nil {
		return errors.New("feishu gateway not configured")
	}
	out := feishu.OutMessage{
		BotChannelID: msg.BotChannelID,
		ToID:         msg.ChatEntityID,
		ToType:       string(msg.ChatType),
		Text:         text,
	}
	if msg.ChatType == model.ChatGroup {
		out.ToID = strings.TrimPrefix(msg.ChatEntityID, msg.BotChannelID+":group:")
	}
	if msg.ChatType == model.ChatPersonal {
		out.ToID = strings.TrimPrefix(msg.ChatEntityID, msg.BotChannelID+":personal:")
	}
	feishuMsgID, err := p.Feishu.Send(ctx, out)
	if err != nil {
		return err
	}
	if p.Outbound != nil {
		content, _ := json.Marshal(map[string]string{"text": text})
		if err := p.Outbound.Enqueue(ctx, model.Message{
			ChatEntityID: msg.ChatEntityID,
			BotChannelID: msg.BotChannelID,
			FeishuMsgID:  feishuMsgID,
			ChatType:     msg.ChatType,
			Content:      string(content),
			Status:       "done",
		}); err != nil {
			return err
		}
	}
	return nil
}

func ownerKey(msg model.Message) string {
	if msg.ChatType == model.ChatGroup && msg.SenderOpenID != "" {
		return msg.BotChannelID + ":personal:" + msg.SenderOpenID
	}
	return msg.ChatEntityID
}

func (p *Processor) actor(ctx context.Context, owner string) (model.Member, error) {
	m, err := p.repo().GetMemberByOwnerKey(ctx, owner)
	if err != nil {
		return model.Member{}, err
	}
	if m == nil {
		return model.Member{OwnerKey: owner, Role: model.RoleMember, Active: true}, nil
	}
	if !m.Active {
		return model.Member{}, errors.New("账号未启用")
	}
	if m.Role == "" {
		m.Role = model.RoleMember
	}
	return *m, nil
}

func (p *Processor) repo() store.Repository {
	if p.Repo != nil {
		return p.Repo
	}
	return p.Schedule.Repo
}

func parseProgressText(cmd router.Command, text string) (taskKey, note string, err error) {
	fields := strings.Fields(strings.TrimSpace(text))
	if cmd == router.CmdDone {
		if len(fields) < 2 {
			return "", "", errors.New("完成格式：完成 任务关键词")
		}
		return strings.TrimSpace(strings.TrimPrefix(text, fields[0])), "完成", nil
	}
	if len(fields) < 3 {
		if cmd == router.CmdResult {
			return "", "", errors.New("结果格式：结果 任务关键词 内容")
		}
		return "", "", errors.New("进度格式：进度 任务关键词 内容")
	}
	rest := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	note = strings.TrimSpace(strings.TrimPrefix(rest, fields[1]))
	if note == "" {
		return "", "", errors.New("进度格式：进度 任务关键词 内容")
	}
	return fields[1], note, nil
}

func matchSchedules(items []model.Schedule, keyword string) []model.Schedule {
	var out []model.Schedule
	for _, sc := range items {
		if strings.Contains(sc.Task, keyword) {
			out = append(out, sc)
		}
	}
	return out
}

func extractText(content string) string {
	var obj map[string]any
	if err := json.Unmarshal([]byte(content), &obj); err == nil {
		for _, key := range []string{"text", "content"} {
			if v, ok := obj[key].(string); ok {
				return strings.TrimSpace(v)
			}
		}
	}
	return strings.TrimSpace(content)
}
