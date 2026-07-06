// Package reminder implements M7 scheduled notifications.
package reminder

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// Service scans schedules and risks, then writes outbound reminder messages.
type Service struct {
	Repo     store.Repository
	Outbound bus.Queue
	Clock    clock.Clock
	Loc      *time.Location
}

func New(repo store.Repository, outbound bus.Queue, clk clock.Clock, loc *time.Location) *Service {
	if clk == nil {
		clk = clock.Real{}
	}
	if loc == nil {
		loc, _ = time.LoadLocation("Asia/Shanghai")
		if loc == nil {
			loc = time.UTC
		}
	}
	return &Service{Repo: repo, Outbound: outbound, Clock: clk, Loc: loc}
}

// RunOnce executes one reminder scan. It returns the number of outbound messages enqueued.
func (s *Service) RunOnce(ctx context.Context) (int, error) {
	members, err := s.Repo.ListMembers(ctx)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, m := range members {
		n, err := s.remindMemberSchedule(ctx, m)
		if err != nil {
			return sent, err
		}
		sent += n
	}
	n, err := s.broadcastRisks(ctx, members)
	if err != nil {
		return sent, err
	}
	return sent + n, nil
}

func (s *Service) remindMemberSchedule(ctx context.Context, m model.Member) (int, error) {
	items, err := s.Repo.ListSchedules(ctx, m.OwnerKey)
	if err != nil {
		return 0, err
	}
	today := s.Clock.Now().In(s.Loc).Format("2006-01-02")
	tomorrow := s.Clock.Now().In(s.Loc).AddDate(0, 0, 1).Format("2006-01-02")
	sent := 0
	for _, sc := range items {
		if terminal(sc.Status) {
			continue
		}
		if sc.StartDate == today || sc.StartDate == tomorrow {
			kind := "daily_task"
			key := sc.ID + ":" + sc.StartDate
			ok, err := s.Repo.RecordNotification(ctx, kind, m.OwnerKey, key, today)
			if err != nil {
				return sent, err
			}
			if ok {
				if err := s.enqueue(ctx, m.OwnerKey, fmt.Sprintf("任务提醒：%s %s~%s %s", displayName(m), sc.StartDate, sc.EndDate, sc.Task)); err != nil {
					return sent, err
				}
				sent++
			}
		}
		if sc.EndDate <= tomorrow {
			kind := "deadline_warning"
			key := sc.ID + ":" + sc.EndDate
			ok, err := s.Repo.RecordNotification(ctx, kind, m.OwnerKey, key, today)
			if err != nil {
				return sent, err
			}
			if ok {
				if err := s.enqueue(ctx, m.OwnerKey, fmt.Sprintf("Deadline 预警：%s 截止 %s，请确认进展。", sc.Task, sc.EndDate)); err != nil {
					return sent, err
				}
				sent++
			}
		}
	}
	return sent, nil
}

func (s *Service) broadcastRisks(ctx context.Context, members []model.Member) (int, error) {
	risks, err := s.Repo.ListOpenRisks(ctx, "")
	if err != nil {
		return 0, err
	}
	today := s.Clock.Now().In(s.Loc).Format("2006-01-02")
	recipients := riskRecipients(members)
	sent := 0
	for _, risk := range risks {
		if risk.ID == "" {
			continue
		}
		for _, owner := range recipientsForRisk(risk, recipients) {
			ok, err := s.Repo.RecordNotification(ctx, "risk_open", owner, risk.ID, today)
			if err != nil {
				return sent, err
			}
			if !ok {
				continue
			}
			if err := s.enqueue(ctx, owner, fmt.Sprintf("风险周知：%s", risk.Content)); err != nil {
				return sent, err
			}
			sent++
		}
	}
	return sent, nil
}

func (s *Service) enqueue(ctx context.Context, ownerKey, text string) error {
	content, _ := json.Marshal(map[string]string{"text": text})
	return s.Outbound.Enqueue(ctx, model.Message{
		ChatEntityID: ownerKey,
		BotChannelID: botChannel(ownerKey),
		ChatType:     model.ChatPersonal,
		Content:      string(content),
	})
}

func terminal(status string) bool {
	switch status {
	case "done", "canceled", "cancelled":
		return true
	}
	return false
}

func displayName(m model.Member) string {
	if m.DisplayName != "" {
		return m.DisplayName
	}
	return m.OwnerKey
}

func botChannel(ownerKey string) string {
	if i := strings.Index(ownerKey, ":"); i > 0 {
		return ownerKey[:i]
	}
	return "default"
}

func riskRecipients(members []model.Member) map[string]model.Member {
	out := map[string]model.Member{}
	for _, m := range members {
		if !m.Active {
			continue
		}
		if m.Role == model.RoleManager {
			out[m.OwnerKey] = m
		}
	}
	return out
}

func recipientsForRisk(risk model.Risk, managers map[string]model.Member) []string {
	seen := map[string]bool{}
	var out []string
	if risk.OwnerKey != "" {
		seen[risk.OwnerKey] = true
		out = append(out, risk.OwnerKey)
	}
	for owner := range managers {
		if !seen[owner] {
			out = append(out, owner)
		}
	}
	return out
}
