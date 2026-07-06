// Package coordination handles M3 change linkage: global schedule export and impact notices.
package coordination

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type Service struct {
	Repo               store.Repository
	Outbound           bus.Queue
	Clock              clock.Clock
	GlobalSchedulePath string
}

type Export struct {
	GeneratedAt time.Time        `json:"generated_at"`
	Members     []MemberSchedule `json:"members"`
}

type MemberSchedule struct {
	OwnerKey    string           `json:"owner_key"`
	DisplayName string           `json:"display_name,omitempty"`
	Role        model.Role       `json:"role"`
	Schedules   []model.Schedule `json:"schedules"`
	Progress    []model.Progress `json:"latest_progress"`
}

func New(repo store.Repository, outbound bus.Queue, clk clock.Clock, path string) *Service {
	if clk == nil {
		clk = clock.Real{}
	}
	return &Service{Repo: repo, Outbound: outbound, Clock: clk, GlobalSchedulePath: path}
}

func (s *Service) OnScheduleChanged(ctx context.Context, ownerKey, reason string) error {
	if err := s.SyncGlobalSchedule(ctx); err != nil {
		return err
	}
	return s.NotifyImpact(ctx, ownerKey, "排期变更", reason)
}

func (s *Service) OnProgressChanged(ctx context.Context, ownerKey, taskKey, note string) error {
	if err := s.SyncGlobalSchedule(ctx); err != nil {
		return err
	}
	reason := strings.TrimSpace(taskKey)
	if note != "" {
		reason = strings.TrimSpace(reason + "：" + note)
	}
	return s.NotifyImpact(ctx, ownerKey, "进度变更", reason)
}

func (s *Service) SyncGlobalSchedule(ctx context.Context) error {
	if strings.TrimSpace(s.GlobalSchedulePath) == "" {
		return nil
	}
	members, err := s.Repo.ListMembers(ctx)
	if err != nil {
		return err
	}
	export := Export{GeneratedAt: s.Clock.Now().UTC()}
	for _, m := range members {
		if !m.Active || m.Role == model.RoleSystem {
			continue
		}
		schedules, err := s.Repo.ListSchedules(ctx, m.OwnerKey)
		if err != nil {
			return err
		}
		progress, err := s.Repo.LatestProgress(ctx, m.OwnerKey)
		if err != nil {
			return err
		}
		export.Members = append(export.Members, MemberSchedule{
			OwnerKey:    m.OwnerKey,
			DisplayName: m.DisplayName,
			Role:        m.Role,
			Schedules:   schedules,
			Progress:    progress,
		})
	}
	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return err
	}
	path := s.GlobalSchedulePath
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Service) NotifyImpact(ctx context.Context, ownerKey, kind, reason string) error {
	if s.Outbound == nil {
		return nil
	}
	members, err := s.Repo.ListMembers(ctx)
	if err != nil {
		return err
	}
	recipients := map[string]bool{}
	if ownerKey != "" {
		recipients[ownerKey] = true
	}
	for _, m := range members {
		if m.Role == model.RoleManager && m.Active {
			recipients[m.OwnerKey] = true
		}
	}
	for owner := range recipients {
		text := fmt.Sprintf("%s通知：%s", kind, reason)
		if owner != ownerKey && ownerKey != "" {
			text = fmt.Sprintf("%s通知：%s（影响成员：%s）", kind, reason, ownerKey)
		}
		if err := s.Outbound.Enqueue(ctx, model.Message{
			ChatEntityID: owner,
			BotChannelID: botChannel(owner),
			ChatType:     model.ChatPersonal,
			Content:      textContent(text),
		}); err != nil {
			return err
		}
	}
	return nil
}

func botChannel(ownerKey string) string {
	if i := strings.Index(ownerKey, ":"); i > 0 {
		return ownerKey[:i]
	}
	return "default"
}

func textContent(text string) string {
	b, _ := json.Marshal(map[string]string{"text": text})
	return string(b)
}
