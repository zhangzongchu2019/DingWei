package main

import (
	"os"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

// Startup snapshot. The independent inbound flag does not enable D3 outbound
// and the outbound switch never enables inbound interpretation of a reply.
func configureSpecClarification(h *m8.Hub) {
	h.SpecClarificationEnabled = strings.TrimSpace(os.Getenv("DW_SPEC_CLARIFICATION_ENABLED")) == "1"
	h.SpecClarificationFeishuReplyEnabled = strings.TrimSpace(os.Getenv("DW_SPEC_CLARIFICATION_FEISHU_REPLY_ENABLED")) == "1"
}
