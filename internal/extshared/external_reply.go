package extshared

import (
	"os"
	"strings"
)

const ExternalAutoReplyDisabledEnv = "DW_MCP_EXTERNAL_AUTO_REPLY_DISABLED"
const ExternalReplyWarning = "对方请求自动回程：本轮回复将原样发给该外部 Agent"
const ExternalReplyDisabledWarning = "对方请求自动回程，本席位未开启，请按全名手动回复"

// The list is server configuration, never client-controlled envelope metadata.
// Sensitive leader seat is disabled by default; the others retain opt-in reply.
func ExternalAutoReplyAllowed(targetFullName string) bool {
	value, configured := os.LookupEnv(ExternalAutoReplyDisabledEnv)
	if !configured {
		value = "opus50tc2k.zzc,prodops.zzc"
	}
	for _, name := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		if strings.Count(name, ".") != 1 || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
			return false
		} // malformed configuration fails closed
		if strings.EqualFold(name, targetFullName) {
			return false
		}
	}
	return true
}
func ExternalMessagePrefix(source, display, short, target string, expect bool) string {
	header := ExternalSenderHeader(source, display, short, target)
	if expect {
		warning := ExternalReplyWarning
		if !ExternalAutoReplyAllowed(target) {
			warning = ExternalReplyDisabledWarning
		}
		header += "\n" + warning
	}
	return header + "\n"
}
