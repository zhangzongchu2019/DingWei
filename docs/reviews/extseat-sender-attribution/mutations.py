#!/usr/bin/env python3
"""八项逐个运行的语义变异；编译失败不计作成功。"""
from pathlib import Path
import os
import subprocess

ROOT = Path(__file__).resolve().parents[3]
CASES = [
    ("header-removed", "internal/m8/extseat.go",
     'externalSeatSenderHeader(sourceFullName, d.SourceOwnerDisplayName, d.SourceOwnerShortName, targetFullName) + "\\n" + d.Body',
     'strings.Repeat("", len(externalSeatSenderHeader(sourceFullName, d.SourceOwnerDisplayName, d.SourceOwnerShortName, targetFullName))) + "\\n" + d.Body',
     "./internal/m8", "TestExternalSeatSenderReplyRoundTripPGAndHub", "EXTSEAT_SENDER_HEADER_FROM_LOCKED_IDENTITY"),
    ("meta-removed", "internal/m8/extseat.go",
     ', "origin_kind": "extseat", "source_display_name": sourceFullName, "source_external": true',
     '', "./internal/m8", "TestExternalProjectionIdempotentPreservesHelperQueue", "EXTSEAT_SENDER_META"),
    ("header-from-body", "internal/m8/extseat.go",
     'externalSeatSenderHeader(sourceFullName, d.SourceOwnerDisplayName, d.SourceOwnerShortName, targetFullName)',
     'externalSeatSenderHeader(d.Body, d.SourceOwnerDisplayName, d.SourceOwnerShortName, targetFullName)',
     "./internal/m8", "TestExternalSeatSenderReplyRoundTripPGAndHub", "EXTSEAT_SENDER_HEADER_FROM_LOCKED_IDENTITY"),
    ("header-time-breaks-idempotence", "internal/m8/extseat.go",
     'externalSeatSenderHeader(sourceFullName, d.SourceOwnerDisplayName, d.SourceOwnerShortName, targetFullName) + "\\n" + d.Body',
     'externalSeatSenderHeader(sourceFullName, d.SourceOwnerDisplayName, d.SourceOwnerShortName, targetFullName) + fmt.Sprint(h.now().UnixNano()) + "\\n" + d.Body',
     "./internal/m8", "TestExternalProjectionIdempotentPreservesHelperQueue", "EXTSEAT_SENDER_IDEMPOTENCE"),
    ("reply-route-skipped", "internal/m8/extseat.go",
     'if _, _, ok := extshared.SplitAgentName(strings.Replace(name, "_ag", "_AG", 1)); !ok {',
     'if _, _, ok := extshared.SplitAgentName(strings.Replace(name, "_ag", "_AG", 1)); ok {', "./internal/m8", "TestExternalSeatSenderReplyRoundTripPGAndHub", "EXTSEAT_REPLY_ROUTE_E2E"),
    ("non-target-reply-guard-removed", "internal/extseat/inbox.go",
     'if g.Target.ID != sourceAgentID {\n\t\t\treturn extshared.ErrTargetNotInAG\n\t\t}',
     'if false {\n\t\t\treturn extshared.ErrTargetNotInAG\n\t\t}', "./internal/m8", "TestExternalSeatSenderReplyRoundTripPGAndHub", "EXTSEAT_REPLY_ONLY_FROM_TARGET"),
    ("reply-route-after-grouping", "internal/m8/hub.go",
     '\t\t\t\tif handled, routeErr := h.routeExternalSeat(ctx, full.keyID, full.sessionName, env); handled {\n\t\t\t\t\treturn routeErr\n\t\t\t\t}\n\t\t\t\tif err := h.enforceAgentGrouping(ctx, from, env, full.keyID, full.sessionName); err != nil {\n\t\t\t\t\treturn err\n\t\t\t\t}',
     '\t\t\t\tif err := h.enforceAgentGrouping(ctx, from, env, full.keyID, full.sessionName); err != nil {\n\t\t\t\t\treturn err\n\t\t\t\t}\n\t\t\t\tif handled, routeErr := h.routeExternalSeat(ctx, full.keyID, full.sessionName, env); handled {\n\t\t\t\t\treturn routeErr\n\t\t\t\t}', "./internal/m8", "TestExternalSeatSenderReplyRoundTripPGAndHub", "EXTSEAT_REPLY_ONLY_FROM_TARGET"),
    ("expect-reply-policy-write-removed", "internal/m8/extseat.go",
     'env.Meta[metaKeyReplyPolicy] = replyPolicyExpectReply',
     'env.Meta["mutated_reply_policy"] = replyPolicyExpectReply',
     "./internal/m8", "TestExternalSeatSenderReplyRoundTripPGAndHub", "EXTSEAT_EXPECT_REPLY_POLICY"),
]

BUILD_ERRORS = ("build failed", "syntax error", "undefined:", "expected '", "cannot use ")

def main():
    for name, rel, old, new, package, test, marker in CASES:
        path = ROOT / rel
        original = path.read_text()
        if old not in original:
            raise SystemExit(f"mutation source not found: {name}")
        path.write_text(original.replace(old, new, 1))
        try:
            env = dict(os.environ, HUB_TEST_PG_STRICT="1")
            proc = subprocess.run(["go", "test", "-p", "1", "-count=1", package, "-run", f"^{test}$"],
                                 cwd=ROOT, env=env, text=True, stdout=subprocess.PIPE,
                                 stderr=subprocess.STDOUT, timeout=180)
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
