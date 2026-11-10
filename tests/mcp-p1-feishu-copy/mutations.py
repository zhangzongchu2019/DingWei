#!/usr/bin/env python3
"""FEISHU-COPY semantic mutations; compile failures never count as killed."""
from pathlib import Path
import os
import subprocess

ROOT = Path(__file__).resolve().parents[2]
CASES = [
    ("header-not-shared", "internal/extshared/limits.go",
     'return fmt.Sprintf("【来自外部 Agent %s（MCP，授权人 %s）→ %s；回复请发给 %s】", source, owner, target, source)',
     'return fmt.Sprintf("【来自外部 Agent %s（MCP，授权人 %s）→ %s；回复请发给 %s】", target, owner, target, source)',
     "./internal/extshared", "TestExternalSenderHeader", "MCP_COPY_HEADER_SHARED"),
    ("copy-byte-limit-removed", "internal/extseat/feishu_copy.go",
     "const FeishuCopyMaxBytes = 20000", "const FeishuCopyMaxBytes = 30000",
     "./internal/extseat", "TestFeishuCopyTextTruncatesOnlyCopy", "MCP_COPY_MAX_BYTES"),
    ("readonly-notice-removed", "internal/extseat/feishu_copy.go",
     'prefix := header + "\\n【只读提醒】飞书回复不会转发到 MCP。\\n【" + label + "】\\n"',
     'prefix := header + "\\n【提醒】\\n【" + label + "】\\n"',
     "./internal/extseat", "TestFeishuCopyTextTruncatesOnlyCopy", "MCP_COPY_TRUNCATE_ONLY_COPY"),
    ("reverse-direction-label-removed", "internal/extseat/feishu_copy.go",
     "if event.Direction == FeishuCopyInternalToExternal {", "if false {",
     "./internal/extseat", "TestFeishuCopyTextMarksInternalReply", "MCP_COPY_BIDIRECTIONAL_HEADER"),
    ("copy-reply-not-suppressed", "internal/m8/hub.go",
	     "if isCopy {\n\t\t\treturn model.PrefixDispatchResult{Matched: true}, nil\n\t\t}",
	     "if isCopy {\n\t\t\treturn model.PrefixDispatchResult{}, nil\n\t\t}",
     "./internal/m8", "TestExternalFeishuCopyReplyDoesNotRouteBackIntoMCP", "MCP_COPY_REPLY_LOOP_SUPPRESSED"),
    ("default-switch-off", "cmd/workpulse/r53_extseat.go",
     'copyDisabled: copyEnabled == "0",', 'copyDisabled: copyEnabled != "0",',
     "./cmd/workpulse", "TestR53FeishuCopyEnabledByDefault", "MCP_COPY_DEFAULT_ENABLED"),
    ("enqueue-failure-blocks-main", "internal/extseat/inbox.go",
     "ROLLBACK TO SAVEPOINT extseat_feishu_copy_enqueue", "ROLLBACK",
     "./internal/extseat", "TestCorePG/FeishuCopyEnqueueFailureIsolatedFromPrimarySend", "MCP_COPY_FAILURE_ISOLATION"),
    ("copy-request-id-removed", "cmd/workpulse/r53_extseat.go",
     'requestID := feishu.DeriveRequestID("reply", event.ID, "mcp-feishu-copy")',
     'requestID := ""',
     "./cmd/workpulse", "TestR53FeishuCopySenderIdempotencyRecipient", "MCP_COPY_REQUEST_ID"),
    ("copy-sent-to-target-name", "cmd/workpulse/r53_extseat.go",
     "ToID: event.Subject", "ToID: event.TargetName",
     "./cmd/workpulse", "TestR53FeishuCopySenderIdempotencyRecipient", "MCP_COPY_RECIPIENT"),
    ("copy-can-send-bypass", "cmd/workpulse/r53_extseat.go",
     "ch.Active && ch.CanSend && ch.AppID == event.Issuer", "ch.Active && ch.AppID == event.Issuer",
     "./cmd/workpulse", "TestR53FeishuCopySenderRejectsUnusableChannels", "MCP_COPY_CHANNEL_CAN_SEND"),
]
BUILD_ERRORS = ("build failed", "syntax error", "undefined:", "expected '", "cannot use ")


def main():
    for name, rel, old, new, package, test, marker in CASES:
        path = ROOT / rel
        original = path.read_text()
        if old not in original:
            raise SystemExit(f"MUTATION_SOURCE_NOT_FOUND {name}")
        path.write_text(original.replace(old, new, 1))
        try:
            env = dict(os.environ, HUB_TEST_PG_STRICT="1")
            proc = subprocess.run(["go", "test", "-p", "1", "-count=1", package, "-run", f"^{test}$"],
                                  cwd=ROOT, env=env, text=True, stdout=subprocess.PIPE,
                                  stderr=subprocess.STDOUT, timeout=240)
            output = proc.stdout
            build_error = any(token in output.lower() for token in BUILD_ERRORS)
            if proc.returncode == 0 or marker not in output or build_error:
                print(output[-5000:])
                raise SystemExit(f"MUTATION_NOT_KILLED name={name} rc={proc.returncode} marker={marker} build_error={build_error}")
            print(f"MUTATION_RED {name} marker={marker}", flush=True)
        finally:
            path.write_text(original)
    print(f"MUTATION_SUMMARY {len(CASES)}/{len(CASES)} semantic reds")


if __name__ == "__main__":
    main()
