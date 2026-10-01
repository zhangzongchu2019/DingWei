package main

import (
	"net/http"

	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

// registerConsoleRoutes console 页、view 页与 ws-view，以及 /api/console/* 等接口的注册。
// run 与路由全扫描用例（console_routes_test.go）调用的是同一份 —— 新增一条路由而漏包认人中间件，扫描用例就会红
// （组长裁定 140013Z ③）。新平台模式下的身份、登录与入网路由由 wireNewPlatform 另行注册。
func registerConsoleRoutes(mux httpapi.Mux, prefixHub *m8.Hub, newplat bool) {
	mux.HandleFunc("GET /ws/view/{sessionName}", prefixHub.RequireConsoleAgent("view", prefixHub.HandleTerminalViewWS))
	mux.HandleFunc("GET /view/{sessionName}", prefixHub.RequireConsoleAgent("view", prefixHub.HandleTerminalViewPage))
	mux.HandleFunc("GET /console", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsolePage))
	if !newplat {
		// 新平台下这三条由 wireNewPlatform 的新实现替换，不并存（同路径重复注册会 panic）。
		mux.HandleFunc("GET /console/login", prefixHub.HandleConsoleLoginPage)
		mux.HandleFunc("GET /console/login/status", prefixHub.HandleConsoleLoginStatus)
		mux.HandleFunc("GET /api/console/agents", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleAgents))
	}
	mux.HandleFunc("GET /api/console/agent-groups", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleAgentGroups))
	mux.HandleFunc("POST /api/console/agent-groups", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleAgentGroups))
	mux.HandleFunc("DELETE /api/console/agent-groups/{code}", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleAgentGroup))
	mux.HandleFunc("POST /api/console/agent-groups/{code}/members", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleAgentGroup))
	mux.HandleFunc("DELETE /api/console/agent-groups/{code}/members/{agentID}", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleAgentGroup))
	mux.HandleFunc("POST /api/console/agent-groups/{code}/additions", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleAgentGroup))
	mux.HandleFunc("GET /api/console/platform", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", prefixHub.HandleConsolePlatform)))
	mux.HandleFunc("POST /api/console/platform/submissions", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsolePlatformSubmit)))
	mux.HandleFunc("POST /api/console/platform/items", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("add", prefixHub.HandleConsolePlatformAdd)))
	mux.HandleFunc("PATCH /api/console/platform/items/{itemID}", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("edit", prefixHub.HandleConsolePlatformEdit)))
	mux.HandleFunc("DELETE /api/console/platform/items/{itemID}", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("delete", prefixHub.HandleConsolePlatformDelete)))
	mux.HandleFunc("POST /api/console/platform/items/{itemID}/approve", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("approve", prefixHub.HandleConsolePlatformApprove)))
	mux.HandleFunc("GET /console/specs", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", prefixHub.HandleConsoleSpecsPage)))
	mux.HandleFunc("GET /console/tasks", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", prefixHub.HandleConsoleTaskBoardPage)))
	if !newplat {
		// 旧平台共享 spec 页：新平台第一版不含 specs，不注册 ⇒ 404（组长裁定 140013Z ④）。
		mux.HandleFunc("GET /console/specs/{specID}", prefixHub.HandleSharedConsoleSpecPage)
	}
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
	mux.HandleFunc("POST /api/agent/console/specs", prefixHub.RequireConsoleSpecAgentCapability(func(*http.Request) string { return m8.ConsoleAgentCapabilitySpecsNew }, prefixHub.HandleConsoleSpecs))
	mux.HandleFunc("GET /api/console/specs/{specID}", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", prefixHub.HandleConsoleSpec)))
	mux.HandleFunc("PATCH /api/console/specs/{specID}", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpec)))
	mux.HandleFunc("PATCH /api/agent/console/specs/{specID}", prefixHub.RequireConsoleSpecAgentCapability(func(r *http.Request) string { return m8.ConsoleAgentSpecCapabilityPage(r.PathValue("specID")) }, prefixHub.HandleConsoleSpec))
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
	mux.HandleFunc("POST /api/console/specs/{specID}/archive", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("delete", prefixHub.HandleConsoleSpecArchive)))
	mux.HandleFunc("GET /api/console/task-proposals/ready", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("approve", prefixHub.HandleConsoleTaskProposalsReady)))
	mux.HandleFunc("PATCH /api/console/task-proposals/{proposalID}/plan", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleTaskProposalPlan)))
	mux.HandleFunc("DELETE /api/console/task-proposals/{proposalID}", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpecTaskDelete)))
	mux.HandleFunc("POST /api/console/specs/{specID}/tasks", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpecTasks)))
	mux.HandleFunc("GET /api/console/specs/{specID}/presplit", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("read", prefixHub.HandleConsoleSpecPresplit)))
	mux.HandleFunc("POST /api/console/specs/{specID}/presplit", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpecPresplit)))
	mux.HandleFunc("POST /api/console/specs/{specID}/workflow/{action}", prefixHub.RequireConsolePrincipal(prefixHub.RequireConsolePlatformAction("submit", prefixHub.HandleConsoleSpecWorkflow)))
	mux.HandleFunc("GET /api/console/spec-editors", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleSpecEditors))
	mux.HandleFunc("POST /api/console/agents/{sessionName}/capability", prefixHub.RequireConsoleAgent("interact", prefixHub.HandleConsoleCapability))
	mux.HandleFunc("POST /api/console/agents/{sessionName}/file-upload-ticket", prefixHub.RequireConsoleAgent("interact", prefixHub.HandleConsoleFileUploadTicket))
	mux.HandleFunc("POST /api/console/files/{fileID}/archive", prefixHub.RequireConsolePrincipal(prefixHub.HandleConsoleFileArchive))
	mux.HandleFunc("POST /view/{sessionName}/unlock-cookie", prefixHub.RequireConsoleAgent("interact", prefixHub.HandleTerminalViewCookie))
	// V11 ＠候选：Hub 自己答，不反代给 view-service（比下面的子路径更具体，优先匹配）。
	mux.HandleFunc("GET /view/{sessionName}/mention-candidates", prefixHub.RequireConsoleAgent("view", prefixHub.HandleViewMentionCandidates))
	mux.HandleFunc("/view/{sessionName}/{rest...}", prefixHub.RequireConsoleViewSubPath(prefixHub.HandleViewSubPath)) // 新 view 子路径(events/input/status)→ 反代 View 服务
}

var _ httpapi.Mux = (*http.ServeMux)(nil)
