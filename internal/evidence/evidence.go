// Package evidence implements the M4 AI transcript evidence pipeline.
package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/llm"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// Session is a transcript source unit. OwnerKey is an attribution hint.
type Session struct {
	ID              string
	Source          string
	OwnerKey        string
	OwnerConfidence float64
}

// Chunk is an incremental transcript slice. Text is never persisted.
type Chunk struct {
	SessionID       string
	Source          string
	Offset          int64
	Text            string
	OccurredAt      time.Time
	OwnerKey        string
	OwnerConfidence float64
}

// Adapter reads transcript sessions incrementally.
type Adapter interface {
	ListSessions(ctx context.Context) ([]Session, error)
	ReadIncremental(ctx context.Context, session Session) ([]Chunk, error)
}

type Service struct {
	Repo    store.Repository
	Adapter Adapter
	LLM     llm.Provider
	Clock   clock.Clock
}

type RunResult struct {
	SessionsScanned int
	ChunksScanned   int
	EvidenceWritten int
	Reconciled      int
	SkippedOptOut   int
	Paused          bool
}

type extractedItem struct {
	WorkItem      string   `json:"work_item"`
	ActionType    string   `json:"action_type"`
	Artifact      string   `json:"artifact"`
	Files         []string `json:"files"`
	OccurredAt    string   `json:"occurred_at"`
	Confidence    float64  `json:"confidence"`
	MappedTaskKey string   `json:"mapped_task_key"`
	RawExcerpt    string   `json:"raw_excerpt"`
}

type extractionPayload struct {
	Items []extractedItem `json:"items"`
}

func New(repo store.Repository, adapter Adapter, provider llm.Provider, clk clock.Clock) *Service {
	if clk == nil {
		clk = clock.Real{}
	}
	return &Service{Repo: repo, Adapter: adapter, LLM: provider, Clock: clk}
}

func (s *Service) RunOnce(ctx context.Context) (RunResult, error) {
	if s.Adapter == nil || s.LLM == nil {
		return RunResult{Paused: true}, llm.ErrAllDown
	}
	sessions, err := s.Adapter.ListSessions(ctx)
	if err != nil {
		return RunResult{}, err
	}
	var result RunResult
	result.SessionsScanned = len(sessions)
	owners := map[string]bool{}
	for _, session := range sessions {
		chunks, err := s.Adapter.ReadIncremental(ctx, session)
		if err != nil {
			return result, err
		}
		result.ChunksScanned += len(chunks)
		for _, chunk := range chunks {
			owner := firstNonEmpty(chunk.OwnerKey, session.OwnerKey)
			if owner == "" {
				continue
			}
			member, err := s.Repo.GetMemberByOwnerKey(ctx, owner)
			if err != nil {
				return result, err
			}
			if member == nil || !member.Active {
				continue
			}
			if member.EvidenceOptOut {
				result.SkippedOptOut++
				continue
			}
			items, err := s.extract(ctx, chunk)
			if err != nil {
				if errors.Is(err, llm.ErrAllDown) {
					result.Paused = true
				}
				return result, err
			}
			for _, item := range items {
				ev, err := s.toEvidence(ctx, owner, session, chunk, item)
				if err != nil {
					return result, err
				}
				if err := s.Repo.AddAIEvidence(ctx, ev); err != nil {
					return result, err
				}
				result.EvidenceWritten++
				owners[owner] = true
			}
		}
	}
	for owner := range owners {
		n, err := s.ReconcileOwner(ctx, owner)
		if err != nil {
			return result, err
		}
		result.Reconciled += n
	}
	return result, nil
}

func (s *Service) AssociateEvidence(ctx context.Context, ownerKey, evidenceID, taskKey string) error {
	if err := s.Repo.MapAIEvidenceToTask(ctx, evidenceID, ownerKey, taskKey); err != nil {
		return err
	}
	_, err := s.ReconcileOwner(ctx, ownerKey)
	return err
}

func (s *Service) extract(ctx context.Context, chunk Chunk) ([]extractedItem, error) {
	system := "从 AI 助手 transcript 中抽取工作相关摘要，只返回 JSON: {\"items\":[{\"work_item\":\"\",\"action_type\":\"\",\"artifact\":\"\",\"files\":[],\"occurred_at\":\"RFC3339\",\"confidence\":0.0,\"mapped_task_key\":\"\",\"raw_excerpt\":\"\"}]}"
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		out, err := s.LLM.Complete(ctx, system, chunk.Text)
		if err != nil {
			return nil, err
		}
		items, err := parseExtraction(out)
		if err == nil {
			return items, nil
		}
		last = err
	}
	return nil, last
}

func parseExtraction(out string) ([]extractedItem, error) {
	var payload extractionPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		return nil, fmt.Errorf("invalid evidence extraction json: %w", err)
	}
	var items []extractedItem
	for _, item := range payload.Items {
		item.WorkItem = strings.TrimSpace(item.WorkItem)
		if item.WorkItem == "" {
			return nil, errors.New("invalid evidence extraction schema: empty work_item")
		}
		if item.Confidence <= 0 || item.Confidence > 1 {
			return nil, errors.New("invalid evidence extraction schema: confidence out of range")
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) toEvidence(ctx context.Context, owner string, session Session, chunk Chunk, item extractedItem) (model.AIEvidence, error) {
	files, _ := json.Marshal(item.Files)
	occurred := chunk.OccurredAt
	if item.OccurredAt != "" {
		if t, err := time.Parse(time.RFC3339, item.OccurredAt); err == nil {
			occurred = t
		}
	}
	if occurred.IsZero() {
		occurred = s.Clock.Now()
	}
	mapped := strings.TrimSpace(item.MappedTaskKey)
	if mapped == "" {
		schedules, err := s.Repo.ListSchedules(ctx, owner)
		if err != nil {
			return model.AIEvidence{}, err
		}
		mapped = mapToTask(item.WorkItem, schedules)
	}
	raw := firstNonEmpty(item.RawExcerpt, chunk.Text)
	return model.AIEvidence{
		OwnerKey:       owner,
		SessionID:      firstNonEmpty(chunk.SessionID, session.ID),
		SessionSource:  firstNonEmpty(chunk.Source, session.Source),
		WorkItem:       item.WorkItem,
		Artifact:       item.Artifact,
		FilesJSON:      string(files),
		ActionType:     item.ActionType,
		OccurredAt:     occurred,
		MappedTaskKey:  mapped,
		Confidence:     item.Confidence,
		RawExcerptHash: hashText(raw),
	}, nil
}

func (s *Service) ReconcileOwner(ctx context.Context, owner string) (int, error) {
	schedules, err := s.Repo.ListSchedules(ctx, owner)
	if err != nil {
		return 0, err
	}
	progress, err := s.Repo.LatestProgress(ctx, owner)
	if err != nil {
		return 0, err
	}
	evidence, err := s.Repo.ListAIEvidence(ctx, owner, "")
	if err != nil {
		return 0, err
	}
	progressByTask := map[string]model.Progress{}
	for _, p := range progress {
		progressByTask[p.TaskKey] = p
	}
	evidenceCount := map[string]int{}
	for _, ev := range evidence {
		key := ev.MappedTaskKey
		if key == "" {
			key = ev.WorkItem
		}
		evidenceCount[key]++
	}
	tasks := map[string]bool{}
	for _, sc := range schedules {
		tasks[sc.Task] = true
	}
	for task := range progressByTask {
		tasks[task] = true
	}
	for task := range evidenceCount {
		tasks[task] = true
	}
	written := 0
	for task := range tasks {
		p, hasProgress := progressByTask[task]
		self := selfStatus(p, hasProgress)
		count := evidenceCount[task]
		verdict := verdict(self, count)
		detail, _ := json.Marshal(map[string]any{
			"manual_link_available": true,
			"evidence_count":        count,
		})
		if err := s.Repo.UpsertReconciliation(ctx, model.Reconciliation{
			OwnerKey:      owner,
			TaskKey:       task,
			SelfStatus:    self,
			EvidenceCount: count,
			Verdict:       verdict,
			ComputedAt:    s.Clock.Now(),
			DetailJSON:    string(detail),
		}); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

func mapToTask(workItem string, schedules []model.Schedule) string {
	w := strings.ToLower(workItem)
	for _, sc := range schedules {
		task := strings.ToLower(sc.Task)
		if task != "" && (strings.Contains(w, task) || strings.Contains(task, w)) {
			return sc.Task
		}
	}
	return ""
}

func selfStatus(p model.Progress, ok bool) string {
	if !ok {
		return "none"
	}
	if p.Percent >= 100 || strings.Contains(p.Note, "完成") {
		return "completed"
	}
	return "in_progress"
}

func verdict(self string, evidenceCount int) string {
	switch {
	case self == "completed" && evidenceCount > 0:
		return "confirmed"
	case self == "completed":
		return "suspected_lag"
	case self == "in_progress" && evidenceCount > 0:
		return "confirmed"
	case self == "in_progress":
		return "partial"
	case self == "none" && evidenceCount > 0:
		return "ahead"
	default:
		return "no_evidence"
	}
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
