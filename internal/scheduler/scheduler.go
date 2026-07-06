// Package scheduler implements the R8 system-level schedule coordinator.
package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/coordination"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

var (
	aggregateBRTagRE        = regexp.MustCompile(`(?i)<br\s*/?>`)
	aggregateHTMLTagRE      = regexp.MustCompile(`(?i)</?[^>]+>`)
	aggregateMarkdownLinkRE = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)\s]+)[^)]*\)`)
	aggregateURLRE          = regexp.MustCompile(`https?://\S+`)
	aggregateWhitespaceRE   = regexp.MustCompile(`\s+`)
	aggregateGanttLineRE    = regexp.MustCompile(`^[^:：\n]{1,80}[:：]\s*[A-Za-z0-9_.-]+,\s*\d{4}-\d{1,2}-\d{1,2},\s*\d{4}-\d{1,2}-\d{1,2}`)
	aggregateGanttMarkerRE  = regexp.MustCompile(`(?i)(^|[,\s])(milestone|active|done|crit)([,\s]|$)`)
	aggregateDurationRE     = regexp.MustCompile(`,\s*\d+d\s*$`)
	aggregateRuleLineRE     = regexp.MustCompile(`^[\s|:\-]+$`)
)

type Runner interface {
	Run(ctx context.Context, prompt string) (string, error)
}

type Config struct {
	TeamFile           string
	PersonalDir        string
	BackupDir          string
	ReportDir          string
	TranscriptDirs     []string
	NotifyChatID       string
	NotifyBotID        string
	GroupNotifyCron    string
	PersonalNotifyCron string
	EvidenceCron       string
	EvidenceTZ         string
	CollectRetainDays  int
	Command            string
	ConfigDir          string
	Timeout            time.Duration
}

type Service struct {
	Config   Config
	Repo     store.Repository
	Runner   Runner
	Clock    clock.Clock
	Outbound bus.Queue
	Legacy   *coordination.Service
}

func New(cfg Config, runner Runner, clk clock.Clock, outbound bus.Queue) *Service {
	if clk == nil {
		clk = clock.Real{}
	}
	cfg = normalizeConfig(cfg)
	if runner == nil {
		runner = CLIRunner{Command: cfg.Command, ConfigDir: cfg.ConfigDir, Timeout: cfg.Timeout}
	}
	return &Service{Config: cfg, Runner: runner, Clock: clk, Outbound: outbound}
}

func normalizeConfig(cfg Config) Config {
	if cfg.TeamFile == "" {
		cfg.TeamFile = "docs/AI研究任务排期/AI-研究工作内容清单.md"
	}
	if cfg.PersonalDir == "" {
		cfg.PersonalDir = "docs/AI研究任务排期"
	}
	if cfg.BackupDir == "" {
		cfg.BackupDir = filepath.Join(cfg.PersonalDir, "backup")
	}
	if cfg.ReportDir == "" {
		cfg.ReportDir = cfg.PersonalDir
	}
	if cfg.NotifyBotID == "" {
		cfg.NotifyBotID = "default"
	}
	if cfg.GroupNotifyCron == "" {
		cfg.GroupNotifyCron = "0 0"
	}
	if cfg.PersonalNotifyCron == "" {
		cfg.PersonalNotifyCron = "0 0,6"
	}
	if cfg.EvidenceCron == "" {
		cfg.EvidenceCron = "0 0,6 * * *"
	}
	if cfg.EvidenceTZ == "" {
		cfg.EvidenceTZ = "UTC"
	}
	if cfg.CollectRetainDays <= 0 {
		cfg.CollectRetainDays = 31
	}
	if cfg.Command == "" {
		cfg.Command = "claude"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 3 * time.Minute
	}
	return cfg
}

func (s *Service) EvidenceCronConfig(ctx context.Context) (string, string) {
	cfg := s.effectiveConfig(ctx)
	return cfg.EvidenceCron, cfg.EvidenceTZ
}

func (s *Service) GroupNotifyCronConfig(ctx context.Context) (string, string) {
	cfg := s.effectiveConfig(ctx)
	return cfg.GroupNotifyCron, cfg.EvidenceTZ
}

func (s *Service) PersonalNotifyCronConfig(ctx context.Context) (string, string) {
	cfg := s.effectiveConfig(ctx)
	return cfg.PersonalNotifyCron, cfg.EvidenceTZ
}

func (s *Service) OnScheduleChanged(ctx context.Context, ownerKey, reason string) error {
	if s.Legacy != nil {
		if err := s.Legacy.OnScheduleChanged(ctx, ownerKey, reason); err != nil {
			return err
		}
	}
	_, err := s.Coordinate(ctx, reason)
	return err
}

func (s *Service) HandleSystemRequest(ctx context.Context, serviceName, action, body string, source model.Message) (string, error) {
	if serviceName != "scheduler" {
		return "", fmt.Errorf("unknown system service %s", serviceName)
	}
	switch action {
	case "record", "evidence":
		owner, err := s.RecordProgressReport(ctx, body, source)
		if err != nil {
			return "", err
		}
		if owner == "" {
			return "已记录进度上报", nil
		}
		return "已记录进度上报：" + owner, nil
	case "coordinate":
		projectID := s.resolveProject(ctx, source)
		path, err := s.CoordinateProject(ctx, projectID, body)
		if err != nil {
			return "", err
		}
		return "系统调度器已协调团队排期：" + path, nil
	case "", "auto":
		projectID := s.resolveProject(ctx, source)
		path, err := s.CoordinateProject(ctx, projectID, body)
		if err != nil {
			return "", err
		}
		return "系统调度器已协调团队排期：" + path, nil
	default:
		return "", fmt.Errorf("unknown scheduler action %s", action)
	}
}

func (s *Service) RecordProgressReport(ctx context.Context, body string, source model.Message) (string, error) {
	if s.Repo == nil {
		return "", errors.New("scheduler repository is not configured")
	}
	owner := s.resolveProgressOwner(ctx, source)
	note := strings.TrimSpace(body)
	if note == "" {
		note = "自然语言进度上报"
	}
	if err := s.Repo.AddProgress(ctx, model.Progress{
		OwnerKey:   owner,
		TaskKey:    "自然语言进度上报",
		Note:       note,
		ReportedAt: s.Clock.Now().UTC(),
		Source:     "feishu",
	}); err != nil {
		return "", err
	}
	_ = s.Repo.WriteAudit(ctx, owner, "schedule_progress_record", note)
	return owner, nil
}

func (s *Service) resolveProgressOwner(ctx context.Context, source model.Message) string {
	chat := strings.TrimSpace(source.ChatEntityID)
	sender := strings.TrimSpace(source.SenderOpenID)
	if s.Repo != nil {
		if members, err := s.Repo.ListMembers(ctx); err == nil {
			for _, member := range members {
				if !member.Active {
					continue
				}
				if member.OwnerKey == chat {
					return member.OwnerKey
				}
				if member.FeishuOpenID != "" && (member.FeishuOpenID == sender || strings.Contains(chat, member.FeishuOpenID)) {
					return member.OwnerKey
				}
			}
		}
	}
	if chat != "" {
		return chat
	}
	if sender != "" {
		return sender
	}
	return "unknown"
}

func (s *Service) resolveProject(ctx context.Context, source model.Message) string {
	if s.Repo == nil {
		return "proj:default"
	}
	chatID := feishuGroupChatID(source)
	if chatID != "" {
		if project, err := s.Repo.GetProjectByGroupChat(ctx, chatID); err == nil && project != nil {
			return project.ID
		}
	}
	return "proj:default"
}

// ResolveProjectForTest exposes resolveProject for acceptance tests.
func (s *Service) ResolveProjectForTest(source model.Message) string {
	return s.resolveProject(context.Background(), source)
}

func feishuGroupChatID(source model.Message) string {
	if source.ChatType != "" && source.ChatType != model.ChatGroup {
		return ""
	}
	chat := strings.TrimSpace(source.ChatEntityID)
	if chat == "" {
		return ""
	}
	parts := strings.Split(chat, ":")
	if len(parts) >= 3 && parts[len(parts)-2] == "group" {
		return parts[len(parts)-1]
	}
	if strings.HasPrefix(chat, "oc_") {
		return chat
	}
	return ""
}

func (s *Service) Coordinate(ctx context.Context, change string) (string, error) {
	if s.Repo != nil {
		return s.CoordinateProject(ctx, "proj:default", change)
	}
	teamRaw, err := os.ReadFile(s.Config.TeamFile)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	// ② 保护区：把 <!-- WP:KEEP … --> 人工策展块抠出换占位符，不喂给 LLM。
	maskedTeam, keep := maskKeepBlocks(string(teamRaw))
	snapshot, err := s.personalSnapshot(maskedTeam)
	if err != nil {
		return "", err
	}
	// ① prompt 硬化：点名逐字保留未受影响段落 + 原样保留占位注释。
	prompt := "你是 WorkPulse 系统级排期协调器。请根据团队总排期、个人排期和本次变更，输出更新后的团队总排期 Markdown。必须保持原 Markdown 结构、标题层级、列表风格和整体排版，只改必要内容；不受本次变更影响的段落必须逐字保留，不得删除、重排或改写风格；文中形如 `<!-- WP:KEEP:N -->`（N 为数字）的占位注释必须原样逐字保留，不得删除、改写、翻译或移动位置；不要新增时间戳、生成时间、额外标题、解释文字或多余空行；只输出完整 Markdown 正文。\n\n" +
		"本次变更:\n" + strings.TrimSpace(change) + "\n\n" + snapshot
	next, err := s.Runner.Run(ctx, prompt)
	if err != nil {
		return "", err
	}
	next = strings.TrimSpace(next)
	if next == "" {
		return "", errors.New("scheduler returned empty team schedule")
	}
	// ② 缝回：把占位符替换成原始保护块（字节级不变）。
	restored, missing := restoreKeepBlocks(next, keep)
	// ③ 写后断言：占位符丢失 或 mermaid 围栏减少 → 拒绝写入，保留原文件。
	if len(missing) > 0 {
		return "", fmt.Errorf("协调器丢失 %d 个保护块占位符 %v，已拒绝写入（保留原文件不变）", len(missing), missing)
	}
	if got, want := strings.Count(restored, "```mermaid"), strings.Count(string(teamRaw), "```mermaid"); got < want {
		return "", fmt.Errorf("协调器丢失 mermaid 代码块：got=%d want>=%d，已拒绝写入（保留原文件不变）", got, want)
	}
	if err := s.backupFiles(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(s.Config.TeamFile), 0o750); err != nil {
		return "", err
	}
	return s.Config.TeamFile, os.WriteFile(s.Config.TeamFile, []byte(restored+"\n"), 0o640)
}

func (s *Service) CoordinateProject(ctx context.Context, projectID, change string) (string, error) {
	if s.Repo == nil {
		return s.Coordinate(ctx, change)
	}
	if strings.TrimSpace(projectID) == "" {
		projectID = "proj:default"
	}
	prev, err := s.Repo.LatestScheduleDoc(ctx, projectID, "team", "")
	if err != nil {
		return "", err
	}
	prevContent := ""
	if prev != nil {
		prevContent = prev.Content
	}
	maskedTeam, keep := maskKeepBlocks(prevContent)
	snapshot, err := s.readScheduleSnapshotProject(ctx, projectID, maskedTeam)
	if err != nil {
		return "", err
	}
	prompt := "你是 WorkPulse 系统级排期协调器。请根据团队总排期、个人排期和本次变更，输出更新后的团队总排期 Markdown。必须保持原 Markdown 结构、标题层级、列表风格和整体排版，只改必要内容；不受本次变更影响的段落必须逐字保留，不得删除、重排或改写风格；文中形如 `<!-- WP:KEEP:N -->`（N 为数字）的占位注释必须原样逐字保留，不得删除、改写、翻译或移动位置；不要新增时间戳、生成时间、额外标题、解释文字或多余空行；只输出完整 Markdown 正文。\n\n" +
		"本次变更:\n" + strings.TrimSpace(change) + "\n\n" + snapshot
	next, err := s.Runner.Run(ctx, prompt)
	if err != nil {
		return "", err
	}
	next = strings.TrimSpace(next)
	restored, missing := restoreKeepBlocks(next, keep)
	if len(missing) > 0 {
		return "", fmt.Errorf("协调器丢失 %d 个保护块占位符 %v，已拒绝写入", len(missing), missing)
	}
	if err := validateTeamDoc(restored, prevContent); err != nil {
		return "", err
	}
	doc, err := s.Repo.AppendScheduleDoc(ctx, model.ScheduleDoc{
		ProjectID: projectID,
		Kind:      "team",
		Content:   restored,
		Source:    "coordinate",
		CreatedBy: "scheduler",
	})
	if err != nil {
		return "", err
	}
	if err := s.NotifyProject(ctx, projectID, fmt.Sprintf("系统调度器已更新团队排期：%s v%d", projectID, doc.Version)); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/team/v%d", projectID, doc.Version), nil
}

func ValidateTeamDoc(content, prev string) error {
	return validateTeamDoc(content, prev)
}

func validateTeamDoc(content, prev string) error {
	text := strings.TrimSpace(content)
	if text == "" {
		return errors.New("team schedule is empty")
	}
	if len([]rune(text)) < 20 {
		return fmt.Errorf("team schedule too short: %d chars", len([]rune(text)))
	}
	if !utf8.ValidString(content) {
		return errors.New("team schedule is not valid UTF-8")
	}
	if strings.Count(content, "```")%2 != 0 {
		return errors.New("team schedule has unbalanced markdown code fences")
	}
	if got, want := strings.Count(content, "```mermaid"), strings.Count(prev, "```mermaid"); got < want {
		return fmt.Errorf("协调器丢失 mermaid 代码块：got=%d want>=%d", got, want)
	}
	if got, want := len(keepBlockRe.FindAllString(content, -1)), len(keepBlockRe.FindAllString(prev, -1)); got < want {
		return fmt.Errorf("KEEP 保护块数量减少：got=%d want>=%d", got, want)
	}
	return nil
}

// keepBlockRe 匹配 <!-- WP:KEEP … --> … <!-- /WP:KEEP --> 之间的人工策展保护块。
var keepBlockRe = regexp.MustCompile(`(?s)<!--\s*WP:KEEP\b.*?-->.*?<!--\s*/WP:KEEP\s*-->`)

// maskKeepBlocks 把每个保护块换成 <!-- WP:KEEP:i --> 占位符，返回脱敏文本与原块切片。
func maskKeepBlocks(text string) (string, []string) {
	var blocks []string
	masked := keepBlockRe.ReplaceAllStringFunc(text, func(m string) string {
		i := len(blocks)
		blocks = append(blocks, m)
		return fmt.Sprintf("<!-- WP:KEEP:%d -->", i)
	})
	return masked, blocks
}

// restoreKeepBlocks 把占位符替回原始保护块；返回替换后文本与未找到的占位符下标。
func restoreKeepBlocks(text string, blocks []string) (string, []int) {
	var missing []int
	for i, blk := range blocks {
		ph := fmt.Sprintf("<!-- WP:KEEP:%d -->", i)
		if strings.Contains(text, ph) {
			text = strings.Replace(text, ph, blk, 1)
		} else {
			missing = append(missing, i)
		}
	}
	return text, missing
}

func (s *Service) RunEvidence(ctx context.Context, reason string) (string, error) {
	if s.Repo != nil {
		return s.RunEvidenceProject(ctx, "proj:default", reason)
	}
	snapshot, err := s.readScheduleSnapshot()
	if err != nil {
		return "", err
	}
	transcripts := s.evidenceSnapshot(ctx)
	prompt := "你是 WorkPulse 系统级佐证分析器。请根据日程计划和按成员组织的 AI 会话内容，输出纯文本佐证报告。报告必须按成员组织，每个成员包含：已执行、无证据、计划外三部分。禁止 Markdown 表格。\n\n" +
		"触发原因:\n" + strings.TrimSpace(reason) + "\n\n日程计划:\n" + snapshot + "\n\nAI会话内容:\n" + transcripts
	report, err := s.Runner.Run(ctx, prompt)
	if err != nil {
		return "", err
	}
	report = strings.TrimSpace(report)
	if report == "" {
		return "", errors.New("scheduler returned empty evidence report")
	}
	if err := os.MkdirAll(s.Config.ReportDir, 0o750); err != nil {
		return "", err
	}
	name := "佐证报告-" + s.Clock.Now().UTC().Format("20060102-150405") + ".md"
	path := filepath.Join(s.Config.ReportDir, name)
	if err := os.WriteFile(path, []byte(report+"\n"), 0o640); err != nil {
		return "", err
	}
	if err := s.Notify(ctx, summarizeReport(report, path)); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Service) RunEvidenceProject(ctx context.Context, projectID, reason string) (string, error) {
	if s.Repo == nil {
		return s.RunEvidence(ctx, reason)
	}
	if sources, err := s.Repo.ListProjectAggregateSources(ctx, projectID); err != nil {
		return "", err
	} else if len(sources) > 0 {
		return s.RunAggregateProject(ctx, projectID, reason)
	}
	snapshot, err := s.readScheduleSnapshotProject(ctx, projectID, "")
	if err != nil {
		return "", err
	}
	transcripts := s.evidenceSnapshotProject(ctx, projectID)
	prompt := "你是 WorkPulse 系统级佐证分析器。请根据日程计划和按成员组织的 AI 会话内容，输出纯文本佐证报告。报告必须按成员组织，每个成员包含：已执行、无证据、计划外三部分。禁止 Markdown 表格。\n\n" +
		"触发原因:\n" + strings.TrimSpace(reason) + "\n\n日程计划:\n" + snapshot + "\n\nAI会话内容:\n" + transcripts
	report, err := s.Runner.Run(ctx, prompt)
	if err != nil {
		return "", err
	}
	report = strings.TrimSpace(report)
	if report == "" {
		return "", errors.New("scheduler returned empty evidence report")
	}
	if err := s.NotifyProject(ctx, projectID, summarizeReport(report, projectID)); err != nil {
		return "", err
	}
	return report, nil
}

func (s *Service) RunAggregateProject(ctx context.Context, projectID, reason string) (string, error) {
	if s.Repo == nil {
		return "", errors.New("scheduler repository is not configured")
	}
	project, err := s.Repo.GetProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	if project == nil {
		return "", fmt.Errorf("project %s not found", projectID)
	}
	summary, err := s.aggregateProjectSummary(ctx, *project, map[string]bool{}, map[string]bool{})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(reason) != "" {
		summary = "触发原因：" + strings.TrimSpace(reason) + "\n\n" + summary
	}
	if err := s.NotifyProject(ctx, projectID, summary); err != nil {
		return "", err
	}
	return summary, nil
}

func (s *Service) RunWeeklyProjectReports(ctx context.Context, reason string) ([]model.ProjectWeeklyReport, error) {
	if s.Repo == nil {
		return nil, errors.New("scheduler repository is not configured")
	}
	projects, err := s.ReportableWeeklyProjects(ctx)
	if err != nil {
		return nil, err
	}
	reports := make([]model.ProjectWeeklyReport, 0, len(projects))
	for _, project := range projects {
		report, err := s.RunWeeklyProjectReport(ctx, project.ID, reason)
		if err != nil {
			return reports, err
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func (s *Service) ReportableWeeklyProjects(ctx context.Context) ([]model.Project, error) {
	if s.Repo == nil {
		return nil, errors.New("scheduler repository is not configured")
	}
	projects, err := s.Repo.ListProjects(ctx, true)
	if err != nil {
		return nil, err
	}
	aggregateIDs, err := s.Repo.ListAggregateProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	aggregates := map[string]bool{}
	for _, id := range aggregateIDs {
		aggregates[id] = true
	}
	var out []model.Project
	for _, project := range projects {
		if strings.TrimSpace(project.OwnerKey) == "" || aggregates[project.ID] {
			continue
		}
		out = append(out, project)
	}
	return out, nil
}

func (s *Service) RunWeeklyProjectReport(ctx context.Context, projectID, reason string) (model.ProjectWeeklyReport, error) {
	if s.Repo == nil {
		return model.ProjectWeeklyReport{}, errors.New("scheduler repository is not configured")
	}
	project, err := s.Repo.GetProject(ctx, projectID)
	if err != nil {
		return model.ProjectWeeklyReport{}, err
	}
	if project == nil || !project.Active {
		return model.ProjectWeeklyReport{}, fmt.Errorf("project %s not found or inactive", projectID)
	}
	if strings.TrimSpace(project.OwnerKey) == "" {
		return model.ProjectWeeklyReport{}, fmt.Errorf("project %s owner_key is empty", projectID)
	}
	weekStart, weekEnd := weeklyReportWindow(s.Clock.Now().UTC())
	progress, err := s.Repo.ListProgressBetween(ctx, project.OwnerKey, weekStart, weekEnd)
	if err != nil {
		return model.ProjectWeeklyReport{}, err
	}
	evidence, err := s.Repo.ListAIEvidenceBetween(ctx, weekStart, weekEnd)
	if err != nil {
		return model.ProjectWeeklyReport{}, err
	}
	teamDoc, err := s.Repo.LatestScheduleDoc(ctx, project.ID, "team", "")
	if err != nil {
		return model.ProjectWeeklyReport{}, err
	}
	ownerDoc, err := s.Repo.LatestScheduleDoc(ctx, project.ID, "personal", project.OwnerKey)
	if err != nil {
		return model.ProjectWeeklyReport{}, err
	}
	prompt := weeklyProjectReportPrompt(*project, weekStart, weekEnd, reason, progress, evidence, teamDoc, ownerDoc)
	content, err := s.Runner.Run(ctx, prompt)
	if err != nil {
		return model.ProjectWeeklyReport{}, err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return model.ProjectWeeklyReport{}, errors.New("scheduler returned empty weekly project report")
	}
	report := model.ProjectWeeklyReport{
		ID:        "weekly-" + project.ID + "-" + weekStart.Format("20060102"),
		ProjectID: project.ID,
		Week:      weekStart.Format("2006-01-02"),
		Content:   content,
		CreatedAt: s.Clock.Now().UTC(),
	}
	if err := s.Repo.UpsertProjectWeeklyReport(ctx, report); err != nil {
		return model.ProjectWeeklyReport{}, err
	}
	return report, nil
}

func weeklyReportWindow(now time.Time) (time.Time, time.Time) {
	date := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	daysSinceMonday := (int(date.Weekday()) + 6) % 7
	start := date.AddDate(0, 0, -daysSinceMonday)
	return start, start.AddDate(0, 0, 7)
}

func weeklyProjectReportPrompt(project model.Project, weekStart, weekEnd time.Time, reason string, progress []model.Progress, evidence []model.AIEvidence, teamDoc, ownerDoc *model.ScheduleDoc) string {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "你是 DingWei 项目周报生成器。请为非聚合项目生成一份纯文本周报。\n项目：%s (%s)\n负责人 owner_key：%s\n周窗口 UTC：%s 至 %s\n", firstNonEmpty(project.Name, project.ID), project.ID, project.OwnerKey, weekStart.Format("2006-01-02"), weekEnd.Add(-time.Nanosecond).Format("2006-01-02"))
	if strings.TrimSpace(reason) != "" {
		_, _ = fmt.Fprintf(&b, "触发原因：%s\n", strings.TrimSpace(reason))
	}
	b.WriteString("\n必须遵守：\n")
	b.WriteString("1. 「本周负责人进度提交」是权威主干；做了什么以这些 M2 进度提交为准。\n")
	b.WriteString("2. 「AI交互佐证」来自全平台 SH_COLLECT/M4，只能用于印证、补全背景或辅助说明，绝不得把交互内容写成已完成事项。\n")
	b.WriteString("3. 如果负责人本周未提交进度，则不要编造完成项，只按项目排期计划节奏展示，并说明本周未见负责人完成事物提交。\n")
	b.WriteString("4. 禁止 Markdown 表格，控制在 10 行左右，负责人关系不要在正文显性露出。\n\n")
	b.WriteString("本周负责人进度提交（权威主干）：\n")
	b.WriteString(formatWeeklyProgress(progress))
	b.WriteString("\n\n项目排期计划：\n")
	b.WriteString(formatWeeklyScheduleDocs(teamDoc, ownerDoc))
	b.WriteString("\n\nAI交互佐证（仅印证与补全，不得当完成事项）：\n")
	b.WriteString(formatWeeklyEvidence(evidence, 60))
	return b.String()
}

func formatWeeklyProgress(progress []model.Progress) string {
	if len(progress) == 0 {
		return "无负责人本周进度提交。"
	}
	var b strings.Builder
	for _, p := range progress {
		_, _ = fmt.Fprintf(&b, "- %s · %s · %d%% · %s\n", p.ReportedAt.UTC().Format("2006-01-02"), firstNonEmpty(p.TaskKey, "未归类事项"), p.Percent, strings.TrimSpace(p.Note))
	}
	return strings.TrimSpace(b.String())
}

func formatWeeklyScheduleDocs(teamDoc, ownerDoc *model.ScheduleDoc) string {
	var b strings.Builder
	b.WriteString("团队排期摘要：\n")
	if teamDoc == nil || strings.TrimSpace(teamDoc.Content) == "" {
		b.WriteString("暂无团队排期。\n")
	} else {
		b.WriteString(summarizePersonalDoc(teamDoc.Content))
		b.WriteString("\n")
	}
	b.WriteString("负责人个人排期摘要：\n")
	if ownerDoc == nil || strings.TrimSpace(ownerDoc.Content) == "" {
		b.WriteString("暂无负责人个人排期。")
	} else {
		b.WriteString(summarizePersonalDoc(ownerDoc.Content))
	}
	return b.String()
}

func formatWeeklyEvidence(evidence []model.AIEvidence, limit int) string {
	if len(evidence) == 0 {
		return "无本周 AI 交互佐证。"
	}
	var b strings.Builder
	for i, ev := range evidence {
		if i >= limit {
			_, _ = fmt.Fprintf(&b, "- 其余 %d 条佐证略。\n", len(evidence)-limit)
			break
		}
		_, _ = fmt.Fprintf(&b, "- %s · %s · %s", ev.OccurredAt.UTC().Format("2006-01-02"), firstNonEmpty(ev.OwnerKey, "unknown"), strings.TrimSpace(ev.WorkItem))
		if strings.TrimSpace(ev.Artifact) != "" {
			_, _ = fmt.Fprintf(&b, "；产物：%s", strings.TrimSpace(ev.Artifact))
		}
		if strings.TrimSpace(ev.MappedTaskKey) != "" {
			_, _ = fmt.Fprintf(&b, "；映射任务：%s", strings.TrimSpace(ev.MappedTaskKey))
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func (s *Service) aggregateProjectSummary(ctx context.Context, project model.Project, visited map[string]bool, seenLines map[string]bool) (string, error) {
	if visited[project.ID] {
		return firstNonEmpty(project.Name, project.ID) + "：聚合来源存在循环，已跳过。", nil
	}
	visited[project.ID] = true
	sources, err := s.Repo.ListProjectAggregateSources(ctx, project.ID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "%s 聚合通知\n", firstNonEmpty(project.Name, project.ID))
	if len(sources) == 0 {
		doc, err := s.Repo.LatestScheduleDoc(ctx, project.ID, "team", "")
		if err != nil {
			return "", err
		}
		appendAggregateSourceSummary(&b, firstNonEmpty(project.Name, project.ID), summarizeTeamDoc(doc), seenLines)
		return b.String(), nil
	}
	for _, source := range sources {
		childSources, err := s.Repo.ListProjectAggregateSources(ctx, source.ID)
		if err != nil {
			return "", err
		}
		if len(childSources) > 0 {
			child, err := s.aggregateProjectSummary(ctx, source, visited, seenLines)
			if err != nil {
				return "", err
			}
			b.WriteString(child)
			if !strings.HasSuffix(child, "\n") {
				b.WriteString("\n")
			}
			continue
		}
		doc, err := s.Repo.LatestScheduleDoc(ctx, source.ID, "team", "")
		if err != nil {
			return "", err
		}
		appendAggregateSourceSummary(&b, firstNonEmpty(source.Name, source.ID), summarizeTeamDoc(doc), seenLines)
	}
	return strings.TrimSpace(b.String()), nil
}

func summarizeTeamDoc(doc *model.ScheduleDoc) string {
	if doc == nil || strings.TrimSpace(doc.Content) == "" {
		return "暂无团队排期。"
	}
	return summarizeAggregateTeamDoc(doc.Content)
}

func appendAggregateSourceSummary(b *strings.Builder, name, summary string, seen map[string]bool) {
	lines := strings.Split(strings.TrimSpace(summary), "\n")
	wrote := false
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key := normalizeAggregateDedupeKey(line)
		if seen[key] {
			continue
		}
		seen[key] = true
		if !wrote {
			_, _ = fmt.Fprintf(b, "%s：%s\n", name, line)
			wrote = true
			continue
		}
		if i < 3 {
			_, _ = fmt.Fprintf(b, "  %s\n", line)
		}
	}
	if !wrote {
		_, _ = fmt.Fprintf(b, "%s：暂无团队排期。\n", name)
	}
}

func summarizeAggregateTeamDoc(content string) string {
	lines := cleanAggregateSummaryLines(content, 3)
	if len(lines) == 0 {
		return "暂无团队排期。"
	}
	return strings.Join(lines, "\n")
}

func cleanAggregateSummaryLines(content string, limit int) []string {
	content = aggregateBRTagRE.ReplaceAllString(content, "\n")
	var out []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(strings.TrimSpace(content), "\n") {
		if len(out) >= limit {
			break
		}
		if isNoisyAggregateLine(raw) {
			continue
		}
		line := cleanAggregateLine(raw)
		if line == "" || !isMainlineAggregateLine(line, len(out) == 0) {
			continue
		}
		if utf8.RuneCountInString(line) > 140 {
			runes := []rune(line)
			line = string(runes[:140]) + "..."
		}
		key := strings.ToLower(line)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, line)
	}
	return out
}

func isNoisyAggregateLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(trimmed, "|") {
		return true
	}
	if aggregateRuleLineRE.MatchString(trimmed) {
		return true
	}
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, ">") {
		return true
	}
	switch {
	case lower == "gantt", strings.HasPrefix(lower, "gantt "):
		return true
	case strings.HasPrefix(lower, "dateformat "), strings.HasPrefix(lower, "axisformat "):
		return true
	case strings.HasPrefix(lower, "section "), strings.HasPrefix(lower, "title "):
		return true
	}
	return aggregateGanttLineRE.MatchString(trimmed) || aggregateGanttMarkerRE.MatchString(trimmed) || aggregateDurationRE.MatchString(trimmed)
}

func cleanAggregateLine(line string) string {
	line = strings.TrimSpace(line)
	line = aggregateMarkdownLinkRE.ReplaceAllString(line, "$1")
	line = aggregateURLRE.ReplaceAllString(line, "")
	line = aggregateHTMLTagRE.ReplaceAllString(line, "")
	line = strings.ReplaceAll(line, "\\", "")
	line = strings.TrimSpace(line)
	line = strings.TrimLeft(line, "-*•·●○◦▪▫ \t")
	line = strings.Trim(line, "`*_ \t")
	line = aggregateWhitespaceRE.ReplaceAllString(line, " ")
	return strings.TrimSpace(line)
}

func isMainlineAggregateLine(line string, first bool) bool {
	if line == "" || isHollowAggregateFragment(line) {
		return false
	}
	if first && utf8.RuneCountInString(line) <= 120 {
		return true
	}
	keywords := []string{"橙点", "里程碑", "关键", "结论", "主线", "进展", "风险", "完成", "推进", "交付", "上线", "灰度", "阻塞", "待定", "本周", "下周", "目标", "已", "需"}
	for _, keyword := range keywords {
		if strings.Contains(line, keyword) {
			return true
		}
	}
	return strings.HasPrefix(line, "🟠") || strings.HasPrefix(line, "🔶")
}

func isHollowAggregateFragment(line string) bool {
	han := 0
	for _, r := range line {
		if r >= '\u4e00' && r <= '\u9fff' {
			han++
		}
	}
	if han == 0 && utf8.RuneCountInString(line) <= 24 {
		return true
	}
	if han < 6 && utf8.RuneCountInString(line) <= 16 {
		return true
	}
	if han <= 6 {
		emptyPhrases := []string{"无需显卡支持", "无需GPU支持", "无需 gpu 支持", "无需 GPU 支持"}
		for _, phrase := range emptyPhrases {
			if strings.EqualFold(line, phrase) {
				return true
			}
		}
	}
	return false
}

func normalizeAggregateDedupeKey(line string) string {
	return strings.ToLower(aggregateWhitespaceRE.ReplaceAllString(strings.TrimSpace(line), " "))
}

func (s *Service) RunEvidenceAllProjects(ctx context.Context, reason string) error {
	if s.Repo == nil {
		_, err := s.RunEvidence(ctx, reason)
		return err
	}
	projects, err := s.Repo.ListProjects(ctx, true)
	if err != nil {
		return err
	}
	for _, p := range projects {
		if _, err := s.RunEvidenceProject(ctx, p.ID, reason); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) RunGroupNotifications(ctx context.Context, reason string) error {
	if s.Repo == nil {
		_, err := s.RunEvidence(ctx, reason)
		return err
	}
	projects, err := s.Repo.ListProjects(ctx, true)
	if err != nil {
		return err
	}
	for _, p := range projects {
		if strings.TrimSpace(p.EvidenceCron) != "" {
			continue
		}
		if _, err := s.RunEvidenceProject(ctx, p.ID, reason); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) RunPersonalReminders(ctx context.Context, reason string) error {
	if s.Repo == nil || s.Outbound == nil {
		return nil
	}
	members, err := s.Repo.ListMembers(ctx)
	if err != nil {
		return err
	}
	cfg := s.effectiveConfig(ctx)
	botID := firstNonEmpty(cfg.NotifyBotID, s.Config.NotifyBotID, "unifiedrobot")
	for _, member := range members {
		if !member.Active || strings.TrimSpace(member.FeishuOpenID) == "" {
			continue
		}
		doc, err := s.latestPersonalDocForMember(ctx, member.OwnerKey)
		if err != nil {
			return err
		}
		text := personalReminderText(member, doc, reason, s.Clock.Now().UTC())
		content, _ := json.Marshal(map[string]string{"text": text})
		if err := s.Outbound.Enqueue(ctx, model.Message{
			ID:           "scheduler-personal-" + member.OwnerKey + "-" + s.Clock.Now().UTC().Format("20060102150405.000000000"),
			ChatEntityID: botID + ":personal:" + member.FeishuOpenID,
			BotChannelID: botID,
			ChatType:     model.ChatPersonal,
			Content:      string(content),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) latestPersonalDocForMember(ctx context.Context, ownerKey string) (*model.ScheduleDoc, error) {
	projects, err := s.Repo.ListProjects(ctx, true)
	if err != nil {
		return nil, err
	}
	var latest *model.ScheduleDoc
	for _, project := range projects {
		members, err := s.Repo.ListProjectMembers(ctx, project.ID)
		if err != nil {
			return nil, err
		}
		if !projectHasMember(members, ownerKey) {
			continue
		}
		doc, err := s.Repo.LatestScheduleDoc(ctx, project.ID, "personal", ownerKey)
		if err != nil {
			return nil, err
		}
		if doc == nil {
			continue
		}
		if latest == nil || doc.CreatedAt.After(latest.CreatedAt) || doc.Version > latest.Version && doc.CreatedAt.Equal(latest.CreatedAt) {
			cp := *doc
			latest = &cp
		}
	}
	return latest, nil
}

func projectHasMember(members []model.Member, ownerKey string) bool {
	for _, member := range members {
		if member.OwnerKey == ownerKey && member.Active {
			return true
		}
	}
	return false
}

func personalReminderText(member model.Member, doc *model.ScheduleDoc, reason string, now time.Time) string {
	name := firstNonEmpty(member.DisplayName, member.OwnerKey)
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "个人排期提醒：%s\n", name)
	if strings.TrimSpace(reason) != "" {
		_, _ = fmt.Fprintf(&b, "触发：%s\n", strings.TrimSpace(reason))
	}
	_, _ = fmt.Fprintf(&b, "时间：%s UTC\n\n", now.Format("2006-01-02 15:04"))
	b.WriteString("个人排期摘要：\n")
	if doc == nil || strings.TrimSpace(doc.Content) == "" {
		b.WriteString("暂无个人排期。\n")
	} else {
		b.WriteString(summarizePersonalDoc(doc.Content))
		b.WriteString("\n")
	}
	b.WriteString("\n待办提醒：请按个人排期推进今日任务；如计划变化，请及时更新 personal 排期。")
	return b.String()
}

func summarizePersonalDoc(content string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) >= 6 {
			break
		}
	}
	if len(out) == 0 {
		return "暂无个人排期。"
	}
	summary := strings.Join(out, "\n")
	if utf8.RuneCountInString(summary) <= 600 {
		return summary
	}
	runes := []rune(summary)
	return string(runes[:600]) + "..."
}

func (s *Service) Notify(ctx context.Context, text string) error {
	cfg := s.effectiveConfig(ctx)
	if s.Outbound == nil || strings.TrimSpace(cfg.NotifyChatID) == "" {
		return nil
	}
	content, _ := json.Marshal(map[string]string{"text": text})
	return s.Outbound.Enqueue(ctx, model.Message{
		ID:           "scheduler-" + s.Clock.Now().UTC().Format("20060102150405.000000000"),
		ChatEntityID: cfg.NotifyBotID + ":group:" + cfg.NotifyChatID,
		BotChannelID: cfg.NotifyBotID,
		ChatType:     model.ChatGroup,
		Content:      string(content),
	})
}

func (s *Service) NotifyProject(ctx context.Context, projectID, text string) error {
	if s.Repo == nil {
		return s.Notify(ctx, text)
	}
	target, err := s.ResolveNotifyTarget(ctx, projectID)
	if err != nil {
		return err
	}
	if s.Outbound == nil || target.ChatID == "" {
		return nil
	}
	botID := firstNonEmpty(target.BotID, s.Config.NotifyBotID, "unifiedrobot")
	content, _ := json.Marshal(map[string]string{"text": text})
	return s.Outbound.Enqueue(ctx, model.Message{
		ID:           "scheduler-" + s.Clock.Now().UTC().Format("20060102150405.000000000"),
		ChatEntityID: botID + ":group:" + target.ChatID,
		BotChannelID: botID,
		ChatType:     model.ChatGroup,
		Content:      string(content),
	})
}

type NotifyTarget struct {
	ChatID string
	BotID  string
	Source string
}

func (s *Service) ResolveNotifyTarget(ctx context.Context, projectID string) (NotifyTarget, error) {
	cfg := s.effectiveConfig(ctx)
	target := NotifyTarget{ChatID: cfg.NotifyChatID, BotID: cfg.NotifyBotID, Source: "global"}
	if s.Repo == nil {
		return target, nil
	}
	visited := map[string]bool{}
	current := strings.TrimSpace(projectID)
	if current == "" {
		current = "proj:default"
	}
	for current != "" {
		if visited[current] {
			return target, nil
		}
		visited[current] = true
		project, err := s.Repo.GetProject(ctx, current)
		if err != nil {
			return NotifyTarget{}, err
		}
		if project == nil {
			if current == "proj:default" {
				return target, nil
			}
			current = "proj:default"
			continue
		}
		if strings.TrimSpace(project.NotifyChatID) != "" {
			return NotifyTarget{
				ChatID: strings.TrimSpace(project.NotifyChatID),
				BotID:  firstNonEmpty(project.NotifyBotID, cfg.NotifyBotID),
				Source: project.ID,
			}, nil
		}
		current = strings.TrimSpace(project.ParentID)
	}
	return target, nil
}

func (s *Service) effectiveConfig(ctx context.Context) Config {
	cfg := s.Config
	if s.Repo == nil {
		return cfg
	}
	items, err := s.Repo.ListAppConfig(ctx)
	if err != nil {
		return cfg
	}
	values := map[string]string{}
	for _, item := range items {
		values[item.Key] = jsonString(item.ValueJSON)
	}
	if v := strings.TrimSpace(values["schedule.notify_chat"]); v != "" {
		cfg.NotifyChatID = v
	}
	if v := strings.TrimSpace(values["schedule.notify_bot"]); v != "" {
		cfg.NotifyBotID = v
	}
	if v := strings.TrimSpace(values["notify.group_cron"]); v != "" {
		cfg.GroupNotifyCron = v
	}
	if v := strings.TrimSpace(values["notify.personal_cron"]); v != "" {
		cfg.PersonalNotifyCron = v
	}
	if v := strings.TrimSpace(values["schedule.transcript_dirs"]); v != "" {
		cfg.TranscriptDirs = splitCSV(v)
	}
	if v := strings.TrimSpace(values["schedule.evidence_cron"]); v != "" {
		cfg.EvidenceCron = v
	}
	if v := strings.TrimSpace(values["schedule.evidence_tz"]); v != "" {
		cfg.EvidenceTZ = v
	}
	if v := strings.TrimSpace(values["collect.retain_days"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.CollectRetainDays = n
		}
	}
	return cfg
}

func (s *Service) readScheduleSnapshot() (string, error) {
	team, err := os.ReadFile(s.Config.TeamFile)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return s.personalSnapshot(string(team))
}

func (s *Service) readScheduleSnapshotProject(ctx context.Context, projectID, teamOverride string) (string, error) {
	if s.Repo == nil {
		return s.readScheduleSnapshot()
	}
	teamText := teamOverride
	if teamText == "" {
		team, err := s.Repo.LatestScheduleDoc(ctx, projectID, "team", "")
		if err != nil {
			return "", err
		}
		if team != nil {
			teamText = team.Content
		}
	}
	var b strings.Builder
	b.WriteString("## 团队总排期\n")
	b.WriteString(teamText)
	b.WriteString("\n\n## 个人排期\n")
	members, err := s.Repo.ListProjectMembers(ctx, projectID)
	if err != nil {
		return "", err
	}
	for _, m := range members {
		doc, err := s.Repo.LatestScheduleDoc(ctx, projectID, "personal", m.OwnerKey)
		if err != nil {
			return "", err
		}
		if doc == nil {
			continue
		}
		label := firstNonEmpty(m.DisplayName, m.OwnerKey)
		b.WriteString("\n### " + label + "\n")
		b.WriteString(doc.Content)
		b.WriteString("\n")
	}
	return b.String(), nil
}

// personalSnapshot 拼装喂给协调器/佐证器的快照：给定团队总排期正文（可能已脱敏保护块）+ 个人排期文件。
func (s *Service) personalSnapshot(teamText string) (string, error) {
	var b strings.Builder
	b.WriteString("## 团队总排期\n")
	b.WriteString(teamText)
	b.WriteString("\n\n## 个人排期\n")
	files, _ := filepath.Glob(filepath.Join(s.Config.PersonalDir, "工作计划-*.md"))
	sort.Strings(files)
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		b.WriteString("\n### " + filepath.Base(path) + "\n")
		b.Write(data)
	}
	return b.String(), nil
}

func (s *Service) backupFiles() error {
	stamp := s.Clock.Now().UTC().Format("20060102-150405")
	dstDir := filepath.Join(s.Config.BackupDir, stamp)
	if err := os.MkdirAll(dstDir, 0o750); err != nil {
		return err
	}
	paths := []string{s.Config.TeamFile}
	personal, _ := filepath.Glob(filepath.Join(s.Config.PersonalDir, "工作计划-*.md"))
	paths = append(paths, personal...)
	for _, path := range paths {
		if err := copyIfExists(path, filepath.Join(dstDir, filepath.Base(path))); err != nil {
			return err
		}
	}
	return nil
}

func copyIfExists(src, dst string) error {
	in, err := os.Open(src)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func readTranscriptSnapshot(dirs []string) string {
	var b strings.Builder
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			path := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			b.WriteString("\n### " + path + "\n")
			if len(data) > 64*1024 {
				data = data[len(data)-64*1024:]
			}
			b.Write(data)
		}
	}
	if strings.TrimSpace(b.String()) == "" {
		return "暂无 transcript 数据。"
	}
	return b.String()
}

func (s *Service) evidenceSnapshot(ctx context.Context) string {
	cfg := s.effectiveConfig(ctx)
	s.cleanupCollectMessages(ctx, cfg)
	var b strings.Builder
	if s.Repo != nil {
		if members, err := s.Repo.ListMembers(ctx); err == nil {
			sessionMap := s.memberSessions(ctx, members)
			messages := s.recentMessagesByMember(ctx, members)
			for _, m := range members {
				if !m.Active || m.EvidenceOptOut {
					continue
				}
				b.WriteString("\n## 成员：" + firstNonEmpty(m.DisplayName, m.OwnerKey) + " (" + m.OwnerKey + ")\n")
				if ss := sessionMap[m.OwnerKey]; len(ss) > 0 {
					b.WriteString("关联会话：" + strings.Join(ss, "、") + "\n")
				} else {
					b.WriteString("关联会话：暂无\n")
				}
				if ms := messages[m.OwnerKey]; len(ms) > 0 {
					b.WriteString("最近会话消息：\n")
					for _, line := range ms {
						b.WriteString("- " + line + "\n")
					}
				} else {
					b.WriteString("最近会话消息：暂无\n")
				}
			}
		}
	}
	extra := readTranscriptSnapshot(cfg.TranscriptDirs)
	if strings.TrimSpace(extra) != "" && extra != "暂无 transcript 数据。" {
		b.WriteString("\n## 外部 transcript 目录\n")
		b.WriteString(extra)
	}
	if strings.TrimSpace(b.String()) == "" {
		return "暂无按成员关联的 AI 会话数据。"
	}
	return b.String()
}

func (s *Service) evidenceSnapshotProject(ctx context.Context, projectID string) string {
	if s.Repo == nil {
		return s.evidenceSnapshot(ctx)
	}
	project, _ := s.Repo.GetProject(ctx, projectID)
	cfg := s.effectiveConfig(ctx)
	if project != nil {
		if project.TranscriptDirs != "" {
			cfg.TranscriptDirs = splitCSV(project.TranscriptDirs)
		}
	}
	s.cleanupCollectMessages(ctx, cfg)
	members, err := s.Repo.ListProjectMembers(ctx, projectID)
	if err != nil {
		return "暂无按成员关联的 AI 会话数据。"
	}
	members = existingActiveMembers(members)
	if len(members) == 0 && projectID == "proj:default" {
		if all, err := s.Repo.ListMembers(ctx); err == nil {
			members = existingActiveMembers(all)
		}
	}
	var b strings.Builder
	sessionMap := s.memberSessions(ctx, members)
	messages := s.recentMessagesByMember(ctx, members)
	for _, m := range members {
		if !m.Active || m.EvidenceOptOut {
			continue
		}
		b.WriteString("\n## 成员：" + firstNonEmpty(m.DisplayName, m.OwnerKey) + " (" + m.OwnerKey + ")\n")
		if ss := sessionMap[m.OwnerKey]; len(ss) > 0 {
			b.WriteString("关联会话：" + strings.Join(ss, "、") + "\n")
		} else {
			b.WriteString("关联会话：暂无\n")
		}
		if ms := messages[m.OwnerKey]; len(ms) > 0 {
			b.WriteString("最近会话消息：\n")
			for _, line := range ms {
				b.WriteString("- " + line + "\n")
			}
		} else {
			b.WriteString("最近会话消息：暂无\n")
		}
	}
	extra := readTranscriptSnapshot(cfg.TranscriptDirs)
	if strings.TrimSpace(extra) != "" && extra != "暂无 transcript 数据。" {
		b.WriteString("\n## 外部 transcript 目录\n")
		b.WriteString(extra)
	}
	if strings.TrimSpace(b.String()) == "" {
		return "暂无按成员关联的 AI 会话数据。"
	}
	return b.String()
}

func existingActiveMembers(members []model.Member) []model.Member {
	var out []model.Member
	for _, m := range members {
		if m.ID == "" {
			continue
		}
		if !m.Active {
			continue
		}
		out = append(out, m)
	}
	return out
}

func (s *Service) cleanupCollectMessages(ctx context.Context, cfg Config) {
	if s.Repo == nil || cfg.CollectRetainDays <= 0 {
		return
	}
	_, _ = s.Repo.DeleteCollectMessagesBefore(ctx, s.Clock.Now().UTC().AddDate(0, 0, -cfg.CollectRetainDays))
}

func (s *Service) memberSessions(ctx context.Context, members []model.Member) map[string][]string {
	out := map[string][]string{}
	if s.Repo == nil {
		return out
	}
	byOpenID := map[string]string{}
	for _, m := range members {
		if m.FeishuOpenID != "" {
			byOpenID[m.FeishuOpenID] = m.OwnerKey
		}
		byOpenID[m.OwnerKey] = m.OwnerKey
	}
	endpoints, err := s.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return out
	}
	for _, ep := range endpoints {
		accounts, err := s.Repo.ListAPIKeyAccounts(ctx, ep.KeyID)
		if err != nil {
			continue
		}
		for _, account := range accounts {
			if owner := ownerForAccount(account, byOpenID); owner != "" {
				out[owner] = append(out[owner], ep.SessionName+"#"+ep.KeyID)
			}
		}
	}
	for owner := range out {
		sort.Strings(out[owner])
		out[owner] = uniqueStrings(out[owner])
	}
	return out
}

func (s *Service) recentMessagesByMember(ctx context.Context, members []model.Member) map[string][]string {
	out := map[string][]string{}
	if s.Repo == nil {
		return out
	}
	byKeyID := s.collectOwnersByKey(ctx, members)
	all, err := s.Repo.RecentMessages(ctx, model.MessageFilter{Limit: 200})
	if err != nil {
		return out
	}
	for _, m := range members {
		if !m.Active || m.EvidenceOptOut {
			continue
		}
		for _, msg := range all {
			if messageBelongsToMember(msg, m, byKeyID) {
				out[m.OwnerKey] = append(out[m.OwnerKey], messageEvidenceLine(msg))
				if len(out[m.OwnerKey]) >= 20 {
					break
				}
			}
		}
	}
	return out
}

func (s *Service) collectOwnersByKey(ctx context.Context, members []model.Member) map[string]string {
	out := map[string]string{}
	byOpenID := map[string]string{}
	for _, m := range members {
		if !m.Active || m.EvidenceOptOut {
			continue
		}
		if m.FeishuOpenID != "" {
			byOpenID[m.FeishuOpenID] = m.OwnerKey
		}
		byOpenID[m.OwnerKey] = m.OwnerKey
	}
	endpoints, err := s.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return out
	}
	for _, ep := range endpoints {
		accounts, err := s.Repo.ListAPIKeyAccounts(ctx, ep.KeyID)
		if err != nil {
			continue
		}
		for _, account := range accounts {
			if owner := ownerForAccount(account, byOpenID); owner != "" {
				out[ep.KeyID] = owner
				break
			}
		}
	}
	return out
}

func ownerForAccount(account string, byOpenID map[string]string) string {
	if owner := byOpenID[account]; owner != "" {
		return owner
	}
	parts := strings.Split(account, ":")
	if len(parts) > 0 {
		if owner := byOpenID[parts[len(parts)-1]]; owner != "" {
			return owner
		}
	}
	return ""
}

func messageBelongsToMember(msg model.Message, m model.Member, collectOwners map[string]string) bool {
	if msg.Direction == model.DirectionCollect {
		_, keyID, ok := strings.Cut(msg.ChatEntityID, "#")
		return ok && collectOwners[keyID] == m.OwnerKey
	}
	if msg.ChatEntityID == m.OwnerKey {
		return true
	}
	if m.FeishuOpenID != "" && strings.Contains(msg.ChatEntityID, ":"+m.FeishuOpenID) {
		return true
	}
	return false
}

func messageEvidenceLine(msg model.Message) string {
	content := strings.TrimSpace(msg.Content)
	if msg.Direction != model.DirectionCollect {
		return content
	}
	var payload struct {
		Text    string `json:"text"`
		Role    string `json:"role"`
		Session string `json:"session"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return content
	}
	prefix := strings.TrimSpace(payload.Session)
	if payload.Role != "" {
		if prefix != "" {
			prefix += "/" + payload.Role
		} else {
			prefix = payload.Role
		}
	}
	if prefix == "" {
		return strings.TrimSpace(payload.Text)
	}
	return prefix + "：" + strings.TrimSpace(payload.Text)
}

func summarizeReport(report, path string) string {
	lines := nonEmptyLines(report)
	executed, noEvidence, unplanned := countStatusLines(lines)
	var out []string
	out = append(out, "日程变更 + 进度状态更新")
	out = append(out, "关键日程变更："+firstMatchingLine(lines, []string{"日程变更", "顺延", "调整", "延期"}, "无"))
	out = append(out, fmt.Sprintf("进度概览：已执行 %d 项 / 无证据 %d 项 / 计划外 %d 项", executed, noEvidence, unplanned))
	out = append(out, "主线："+mainLine(lines))
	out = append(out, "完整佐证报告见 "+path)
	if len(out) > 10 {
		out = out[:10]
	}
	return strings.Join(out, "\n")
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.Trim(line, "-*# "))
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func countStatusLines(lines []string) (executed, noEvidence, unplanned int) {
	for _, line := range lines {
		switch {
		case strings.Contains(line, "已执行"):
			executed++
		case strings.Contains(line, "无证据") || strings.Contains(line, "未执行"):
			noEvidence++
		case strings.Contains(line, "计划外"):
			unplanned++
		}
	}
	return
}

func firstMatchingLine(lines []string, needles []string, fallback string) string {
	for _, line := range lines {
		for _, n := range needles {
			if strings.Contains(line, n) {
				return line
			}
		}
	}
	return fallback
}

func mainLine(lines []string) string {
	for _, line := range lines {
		if strings.Contains(line, "主线") || strings.Contains(line, "重点") || strings.Contains(line, "风险") {
			return line
		}
	}
	for _, line := range lines {
		if !strings.Contains(line, "已执行") && !strings.Contains(line, "无证据") && !strings.Contains(line, "计划外") {
			return line
		}
	}
	return "本时段按计划推进，细节见完整报告。"
}

func jsonString(raw string) string {
	var s string
	if err := json.Unmarshal([]byte(raw), &s); err == nil {
		return s
	}
	return strings.Trim(raw, `"`)
}

func splitCSV(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := values[:0]
	var last string
	for i, v := range values {
		if i == 0 || v != last {
			out = append(out, v)
			last = v
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

type CLIRunner struct {
	Command   string
	ConfigDir string
	Timeout   time.Duration
}

func (r CLIRunner) Run(ctx context.Context, prompt string) (string, error) {
	if strings.TrimSpace(r.Command) == "" {
		return "", errors.New("WP_SCHEDULER_CLI is empty")
	}
	timeout := r.Timeout
	if timeout == 0 {
		timeout = 3 * time.Minute
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(callCtx, "bash", "-lc", r.Command)
	if r.ConfigDir != "" {
		cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+r.ConfigDir)
	}
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
