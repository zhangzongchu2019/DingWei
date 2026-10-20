package main

import (
	"net/http"
	"net/url"

	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/obs"
)

// registerConsoleRoutes console 页、view 页与 ws-view，以及 /api/console/* 等接口的注册。
// run 与路由全扫描用例（console_routes_test.go）调用的是同一份 —— 新增一条路由而漏包认人中间件，扫描用例就会红
// （组长裁定 140013Z ③）。新平台模式下的身份、登录与入网路由由 wireNewPlatform 另行注册。
func registerConsoleRoutes(mux httpapi.Mux, prefixHub *m8.Hub, newplat bool) {
	mux.HandleFunc("GET /ws/view/{sessionName}", prefixHub.RequireConsoleAgent("view", prefixHub.HandleTerminalViewWS))
	mux.HandleFunc("GET /view/{sessionName}", prefixHub.RequireConsoleAgent("view", prefixHub.HandleTerminalViewPage))
	mux.HandleFunc("GET /tasks", prefixHub.RequireConsolePrincipal(prefixHub.HandleViewTasks))
	mux.HandleFunc("GET /console", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsolePage))
	// DW123-PER-RELEASE-OBSERVABILITY 首片：全站运维快照只给 active platform_admin（组长 2026-10-13T08:30:38Z 补裁）。
	mux.HandleFunc("GET /api/console/monitor/snapshot", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAdmin(obs.Default.SnapshotHandler)))
	// 独立监控页（不改 consolePageHTML 与 renderer），与快照接口同一套 guard（组长 2026-10-13T08:59:04Z）。
	mux.HandleFunc("GET /console/monitor", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAdmin(obs.MonitorPageHandler)))
	if !newplat {
		// 新平台下这三条由 wireNewPlatform 的新实现替换，不并存（同路径重复注册会 panic）。
		mux.HandleFunc("GET /console/login", prefixHub.HandleConsoleLoginPage)
		mux.HandleFunc("GET /console/login/status", prefixHub.HandleConsoleLoginStatus)
		mux.HandleFunc("GET /api/console/agents", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleAgents))
	}
	if newplat {
		mux.HandleFunc("GET /api/console/helper-updates", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleHelperUpdates))
		mux.HandleFunc("POST /api/console/helper-updates", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleHelperUpdates))
	}
	mux.HandleFunc("GET /api/console/agent-groups", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleAgentGroups))
	mux.HandleFunc("POST /api/console/agent-groups", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleAgentGroups))
	mux.HandleFunc("DELETE /api/console/agent-groups/{code}", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleAgentGroup))
	mux.HandleFunc("POST /api/console/agent-groups/{code}/members", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleAgentGroup))
	mux.HandleFunc("DELETE /api/console/agent-groups/{code}/members/{agentID}", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleAgentGroup))
	mux.HandleFunc("POST /api/console/agent-groups/{code}/additions", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleAgentGroup))
	mux.HandleFunc("POST /api/console/agent-groups/{code}/communication", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleAgentGroup))
	mux.HandleFunc("GET /api/console/platform", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", prefixHub.HandleConsolePlatform)))
	mux.HandleFunc("POST /api/console/platform/submissions", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsolePlatformSubmit)))
	mux.HandleFunc("POST /api/console/platform/items", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("add", prefixHub.HandleConsolePlatformAdd)))
	mux.HandleFunc("PATCH /api/console/platform/items/{itemID}", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("edit", prefixHub.HandleConsolePlatformEdit)))
	mux.HandleFunc("DELETE /api/console/platform/items/{itemID}", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("delete", prefixHub.HandleConsolePlatformDelete)))
	mux.HandleFunc("POST /api/console/platform/items/{itemID}/approve", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("approve", prefixHub.HandleConsolePlatformApprove)))
	// specs 与 task-board：两种模式都注册（R-V2-SPEC-TASK-SHIM-IMPL G4，定案甲）。新平台下认人、Agent 侧鉴权走新平台分支，
	// 身份经垫片投影进旧表，处理器与存储不变。共享链接沿 095 口径（GET 公开、写凭链接里的编辑能力令牌）。
	registerSpecTaskRoutes(mux, prefixHub)
	mux.HandleFunc("POST /api/console/agents/{sessionName}/capability", prefixHub.RequireConsoleAgent("interact", prefixHub.HandleConsoleCapability))
	mux.HandleFunc("POST /api/console/agents/{sessionName}/file-upload-ticket", prefixHub.RequireConsoleAgent("interact", prefixHub.HandleConsoleFileUploadTicket))
	mux.HandleFunc("POST /api/console/files/{fileID}/archive", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleFileArchive))
	mux.HandleFunc("POST /view/{sessionName}/unlock-cookie", prefixHub.RequireConsoleAgent("interact", prefixHub.HandleTerminalViewCookie))
	// V11 ＠候选：Hub 自己答，不反代给 view-service（比下面的子路径更具体，优先匹配）。
	mux.HandleFunc("GET /view/{sessionName}/mention-candidates", prefixHub.RequireConsoleAgent("view", prefixHub.HandleViewMentionCandidates))
	mux.HandleFunc("/view/{sessionName}/{rest...}", prefixHub.RequireConsoleViewSubPath(prefixHub.HandleViewSubPath)) // 新 view 子路径(events/input/status)→ 反代 View 服务
}

var _ httpapi.Mux = (*http.ServeMux)(nil)

// registerSpecTaskRoutes specs 与 task-board 两组功能的全部路由（页面、console 接口、Agent capability 接口、共享链接）。新旧平台都注册。
func registerSpecTaskRoutes(mux httpapi.Mux, prefixHub *m8.Hub) {
	mux.HandleFunc("GET /console/specs", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", prefixHub.HandleConsoleSpecsPage)))
	// The task board lives in the Console workspace. Keep the old page URL only
	// as a non-cached compatibility entrance; do not retain a parallel nav page.
	mux.HandleFunc("GET /console/tasks", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("embedded") == "1" {
			prefixHub.HandleConsoleTaskBoardPage(w, r)
			return
		}
		target := "/console?task_manager=1"
		if specID := r.URL.Query().Get("spec_id"); specID != "" {
			target += "&spec_id=" + url.QueryEscape(specID)
		}
		http.Redirect(w, r, target, http.StatusFound)
	})))
	mux.HandleFunc("GET /console/specs/{specID}", prefixHub.HandleSharedConsoleSpecPage) // 旧平台共享 spec 页
	mux.HandleFunc("GET /console/spec-assets/{name}", prefixHub.HandleConsoleSpecAsset)
	mux.HandleFunc("GET /api/console/specs", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", prefixHub.HandleConsoleSpecs)))
	mux.HandleFunc("GET /api/console/task-board", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", prefixHub.HandleConsoleTaskBoard)))
	mux.HandleFunc("PATCH /api/console/task-board/tasks/{taskID}", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleTaskBoardAssignment)))
	mux.HandleFunc("POST /api/console/task-driver/specs/{specID}/start", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleTaskDriverStart)))
	mux.HandleFunc("POST /api/console/task-driver/specs/{specID}/{revision}/review", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleTaskDriverReview)))
	mux.HandleFunc("POST /api/console/task-driver/specs/{specID}/{revision}/deploy", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleTaskDriverDeploy)))
	mux.HandleFunc("GET /api/console/task-driver/specs/{specID}/{revision}", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", prefixHub.HandleConsoleTaskDriverStatus)))
	mux.HandleFunc("GET /api/agent/console/task-driver/specs/{specID}/{revision}/tasks", prefixHub.RequireConsoleSpecAgentCapability(func(*http.Request) string { return m8.ConsoleAgentCapabilityTasks }, prefixHub.HandleAgentConsoleTasks))
	mux.HandleFunc("POST /api/console/specs", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpecs)))
	for _, kind := range []string{"agent-review", "agent-criteria"} {
		for _, method := range []string{"GET", "POST"} {
			kind := kind
			mux.HandleFunc(method+" /api/console/specs/{specID}/"+kind, prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction(map[string]string{"GET": "read", "POST": "submit"}[method], func(w http.ResponseWriter, r *http.Request) {
				r.SetPathValue("kind", kind)
				prefixHub.HandleConsoleSpecAgentRequest(w, r)
			})))
		}
	}
	mux.HandleFunc("POST /api/console/specs/{specID}/agent-criteria/{requestID}/{action}", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpecAgentConfirm)))
	mux.HandleFunc("POST /api/console/specs/{specID}/agent-review/{requestID}/cancel", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("action", "cancel")
		prefixHub.HandleConsoleSpecAgentConfirm(w, r)
	})))
	mux.HandleFunc("GET /api/agent/console/spec-review-requests", prefixHub.RequireAgentTaskFlow(prefixHub.HandleAgentSpecReview))
	mux.HandleFunc("POST /api/agent/console/spec-review-requests/{requestID}/result", prefixHub.RequireAgentTaskFlow(prefixHub.HandleAgentSpecReview))
	mux.HandleFunc("GET /api/agent/console/project-context", prefixHub.HandleProjectContext)
	for _, method := range []string{"GET", "POST"} {
		for _, suffix := range []string{"", "/{blockerID}"} {
			mux.HandleFunc(method+" /api/agent/console/tasks/{taskID}/blockers"+suffix, prefixHub.RequireAgentBlocker(prefixHub.HandleTaskBlockers))
			action := "read"
			if method == "POST" {
				action = "submit"
			}
			mux.HandleFunc(method+" /api/console/tasks/{taskID}/blockers"+suffix, prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction(action, prefixHub.HandleTaskBlockers)))
		}
	}
	mux.HandleFunc("GET /api/agent/console/tasks", prefixHub.RequireAgentTaskFlow(prefixHub.HandleAgentTaskFlow))
	mux.HandleFunc("PATCH /api/agent/console/tasks/{taskID}", prefixHub.RequireAgentTaskFlow(prefixHub.HandleAgentTaskFlow))
	mux.HandleFunc("POST /api/agent/console/tasks/{taskID}/cases/{caseAction}", prefixHub.RequireAgentTaskFlow(prefixHub.HandleAgentTaskFlow))
	mux.HandleFunc("POST /api/console/specs/{specID}/presplit/{requestID}/confirm", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleTaskCandidateConfirm)))
	mux.HandleFunc("POST /api/agent/console/specs", prefixHub.RequireAgentSpecDirect(prefixHub.HandleConsoleSpecs))
	mux.HandleFunc("GET /api/console/specs/{specID}", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", prefixHub.HandleConsoleSpec)))
	mux.HandleFunc("PATCH /api/console/specs/{specID}", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpec)))
	mux.HandleFunc("PATCH /api/agent/console/specs/{specID}", prefixHub.RequireAgentSpecDirect(prefixHub.HandleConsoleSpec))
	mux.HandleFunc("POST /api/agent/console/specs/{specID}/inferences", prefixHub.RequireConsoleSpecAgentCapability(func(r *http.Request) string { return m8.ConsoleAgentSpecCapabilityPage(r.PathValue("specID")) }, prefixHub.HandleConsoleSpecAgentInference))
	// Shared spec links are capability-authorized, not identity-authorized. GET is public;
	// PATCH requires the opaque edit capability inside HandleConsoleSpec.
	mux.HandleFunc("GET /api/specs/shared/{specID}", prefixHub.HandleConsoleSpec)
	mux.HandleFunc("PATCH /api/specs/shared/{specID}", prefixHub.HandleConsoleSpec)
	mux.HandleFunc("POST /api/specs/shared/{specID}/versions", prefixHub.HandleConsoleSpecNewVersion)
	mux.HandleFunc("POST /api/console/specs/{specID}/close-editing", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpecCloseEditing)))
	mux.HandleFunc("POST /api/console/specs/{specID}/reopen-editing", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpecReopenEditing)))
	mux.HandleFunc("POST /api/console/specs/{specID}/versions", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpecNewVersion)))
	mux.HandleFunc("POST /api/console/specs/{specID}/derive", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpecDerive)))
	mux.HandleFunc("POST /api/console/specs/{specID}/assist/inference", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpecInferenceAssist)))
	mux.HandleFunc("POST /api/console/specs/{specID}/archive", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", prefixHub.HandleConsoleSpecArchive)))
	mux.HandleFunc("DELETE /api/console/specs/{specID}", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", prefixHub.HandleConsoleSpecArchive)))
	mux.HandleFunc("POST /api/console/specs/archive", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", prefixHub.HandleConsoleSpecsArchiveBatch)))
	mux.HandleFunc("POST /api/console/specs/{specID}/unarchive", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", prefixHub.HandleConsoleSpecUnarchive)))
	mux.HandleFunc("GET /api/console/task-proposals/ready", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("approve", prefixHub.HandleConsoleTaskProposalsReady)))
	mux.HandleFunc("PATCH /api/console/task-proposals/{proposalID}/plan", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleTaskProposalPlan)))
	mux.HandleFunc("DELETE /api/console/task-proposals/{proposalID}", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpecTaskDelete)))
	mux.HandleFunc("POST /api/console/specs/{specID}/tasks", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpecTasks)))
	mux.HandleFunc("GET /api/console/specs/{specID}/presplit", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", prefixHub.HandleConsoleSpecPresplit)))
	mux.HandleFunc("POST /api/console/specs/{specID}/presplit", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpecPresplit)))
	mux.HandleFunc("POST /api/console/specs/{specID}/workflow/{action}", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpecWorkflow)))
	mux.HandleFunc("GET /api/console/spec-editors", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleSpecEditors))
}
