// Package schedule 实现 M1 排期管理的「两步式写」：解析 → diff 预览 → 确认/取消 → 应用。
// 规范 §M1 / §15.2（pending 状态机）。
package schedule

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// Service M1 排期服务。
type Service struct {
	Repo        store.Repository
	Clock       clock.Clock
	Loc         *time.Location
	TTL         time.Duration // pending 过期时间
	Coordinator Coordinator
}

type Coordinator interface {
	OnScheduleChanged(ctx context.Context, ownerKey, reason string) error
}

// New 构造（loc 为空默认 Asia/Shanghai，§15.4）。
func New(repo store.Repository, clk clock.Clock, loc *time.Location) *Service {
	if loc == nil {
		loc, _ = time.LoadLocation("Asia/Shanghai")
		if loc == nil {
			loc = time.UTC
		}
	}
	return &Service{Repo: repo, Clock: clk, Loc: loc, TTL: 10 * time.Minute}
}

// Handle 处理排期编辑指令：解析 → diff → 存 pending → 返回预览文本。
func (s *Service) Handle(ctx context.Context, ownerKey, text string) (string, error) {
	ops, err := ParseLines(text, s.Clock.Now(), s.Loc)
	if err != nil {
		return "", err
	}
	if len(ops) == 0 {
		return "未解析到排期指令。格式：+ MM/DD-MM/DD 任务 / - 关键词 / 改 关键词 MM/DD-MM/DD / 顺延 MM/DD +N天 / 全量", nil
	}
	current, err := s.Repo.ListSchedules(ctx, ownerKey)
	if err != nil {
		return "", err
	}
	changes, preview, err := ComputeDiff(current, ops, ownerKey, s.Loc)
	if err != nil {
		return "", err
	}
	if len(changes) == 0 {
		if msg := noMatchMessage(ops); msg != "" {
			return msg, nil
		}
		return "无变化。", nil
	}
	payload, err := json.Marshal(changes)
	if err != nil {
		return "", err
	}
	if _, err := s.Repo.PutPending(ctx, ownerKey, string(payload), s.Clock.Now().Add(s.TTL)); err != nil {
		return "", err
	}
	return preview, nil
}

// Confirm 应用待确认变更。
func (s *Service) Confirm(ctx context.Context, ownerKey string) (string, error) {
	p, err := s.Repo.GetPending(ctx, ownerKey)
	if err != nil {
		return "", err
	}
	if p == nil {
		return "没有待确认的变更（可能已生效/取消/超时）。", nil
	}
	var changes []Change
	if err := json.Unmarshal([]byte(p.PayloadJSON), &changes); err != nil {
		return "", err
	}
	applied := 0
	for _, c := range changes {
		switch c.Action {
		case "insert":
			ns := c.New
			ns.ID = newID()
			if err := s.Repo.UpsertSchedule(ctx, ns); err != nil {
				return "", err
			}
		case "delete":
			if err := s.Repo.DeleteScheduleByID(ctx, c.Old.ID); err != nil {
				return "", err
			}
		case "update":
			if c.Old.ID != "" {
				if err := s.Repo.DeleteScheduleByID(ctx, c.Old.ID); err != nil {
					return "", err
				}
			}
			ns := c.New
			ns.ID = newID()
			if err := s.Repo.UpsertSchedule(ctx, ns); err != nil {
				return "", err
			}
		}
		applied++
	}
	if err := s.Repo.SetPendingStatus(ctx, p.ID, "confirmed"); err != nil {
		return "", err
	}
	if err := s.appendPersonalDoc(ctx, ownerKey); err != nil {
		return "", err
	}
	_ = s.Repo.WriteAudit(ctx, ownerKey, "schedule_confirm", fmt.Sprintf("%d changes", applied))
	if s.Coordinator != nil {
		if err := s.Coordinator.OnScheduleChanged(ctx, ownerKey, fmt.Sprintf("%d 项排期变更已生效", applied)); err != nil {
			return fmt.Sprintf("✅ 已生效，共 %d 项变更；但变更联动失败：%v", applied, err), nil
		}
	}
	return fmt.Sprintf("✅ 已生效，共 %d 项变更。", applied), nil
}

func (s *Service) appendPersonalDoc(ctx context.Context, ownerKey string) error {
	items, err := s.Repo.ListSchedules(ctx, ownerKey)
	if err != nil {
		return err
	}
	content := renderPersonalDoc(ownerKey, items)
	_, err = s.Repo.AppendScheduleDoc(ctx, model.ScheduleDoc{
		ProjectID: "proj:default",
		Kind:      "personal",
		OwnerKey:  ownerKey,
		Content:   content,
		Source:    "nl",
		CreatedBy: ownerKey,
	})
	return err
}

func renderPersonalDoc(ownerKey string, items []model.Schedule) string {
	out := "# 工作计划-" + ownerKey + "\n\n"
	if len(items) == 0 {
		return out + "暂无排期。\n"
	}
	for _, item := range items {
		dates := item.StartDate
		if item.EndDate != "" && item.EndDate != item.StartDate {
			dates += "-" + item.EndDate
		}
		out += fmt.Sprintf("- %s %s [%s]\n", dates, item.Task, item.Status)
	}
	return out
}

// Cancel 放弃待确认变更。
func (s *Service) Cancel(ctx context.Context, ownerKey string) (string, error) {
	p, err := s.Repo.GetPending(ctx, ownerKey)
	if err != nil {
		return "", err
	}
	if p == nil {
		return "没有待确认的变更。", nil
	}
	if err := s.Repo.SetPendingStatus(ctx, p.ID, "cancelled"); err != nil {
		return "", err
	}
	return "已取消本次变更。", nil
}

var _ = model.Schedule{}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func noMatchMessage(ops []Op) string {
	for _, op := range ops {
		switch op.Kind {
		case OpDelete, OpModify:
			return fmt.Sprintf("未匹配到排期关键词「%s」，请检查关键词或先查看当前排期。", op.Keyword)
		case OpPostpone:
			return fmt.Sprintf("未找到 %s 及之后可顺延的排期。", op.Anchor)
		}
	}
	return ""
}
