#!/usr/bin/env python3
"""Semantic one-at-a-time mutations for S-MCP-AUTH-DIAG regression assertions."""
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
CASES = [
    ("form-copy", "internal/feishulogin/connect.go", "本次接入的 Codex 自己的名字", "本次接入的用户名称",
     "./internal/feishulogin", "TestConnectPageGETRendersSelfContainedValidatedForm", "CONNECT_PAGE_IDENTITY_HINTS"),
    ("missing-target-category", "internal/extseat/service.go",
     "errors.Join(extshared.ErrTargetNotInAG, extshared.ErrTargetMissing)", "extshared.ErrTargetNotInAG",
     "./internal/extseat", "TestIssueTokenMissingTargetClassificationPG", "TARGET_MISSING_CLASSIFICATION"),
    ("tenant-reason", "internal/feishulogin/handler.go", "reason = FailureTenantRejected", "reason = FailureOther",
     "./internal/feishulogin", "TestCallbackFailureReasonCodesAndTargetCategory", "CALLBACK_FAILURE_REASON_MAPPING"),
    ("html-explanation", "internal/mcpgw/callback_diagnostics.go", ", Description: description}", ", Description: oauthCode}",
     "./internal/mcpgw", "TestCallbackFailureReasonsPageAndStructuredLog", "CALLBACK_FAILURE_ERROR_MAPPING"),
    ("attempt-prefix", "internal/feishulogin/types.go", "prefix = prefix[:8]", "prefix = prefix",
     "./internal/feishulogin", "TestCallbackFailureReasonCodesAndTargetCategory", "CALLBACK_FAILURE_REASON_MAPPING"),
    ("utc-log", "internal/mcpgw/callback_diagnostics.go", '\n\t\t\t"utc", g.cfg.Now().UTC().Format(time.RFC3339Nano),', "",
     "./internal/mcpgw", "TestCallbackFailureReasonsPageAndStructuredLog", "CALLBACK_FAILURE_UTC"),
    ("no-leak", "internal/mcpgw/callback_diagnostics.go", '"reason_code", string(reason),',
     '"reason_code", string(reason), "open_id", "open_id-secret",',
     "./internal/mcpgw", "TestCallbackFailureReasonsPageAndStructuredLog", "CALLBACK_FAILURE_NO_LEAK"),
    ("oauth-error-semantics", "internal/mcpgw/callback_diagnostics.go", 'Encode(callbackFailureBody{Error: oauthCode', 'Encode(callbackFailureBody{Error: "server_error"',
     "./internal/mcpgw", "TestCallbackFailureJSONKeepsOAuthErrorAndAddsDescription", "CALLBACK_FAILURE_JSON_ERROR_SEMANTICS"),
    ("origin-guard-log", "internal/mcpgw/gateway.go",
     'g.failCallback(w, r, http.StatusForbidden, "origin_rejected", feishulogin.FailureOther, "", feishulogin.TargetCategoryUnverified)',
     'writeJSON(w, 403, map[string]string{"error": "origin_rejected"})',
     "./internal/mcpgw", "TestCallbackOriginFailureUsesSafeDiagnostics", "CALLBACK_GUARD_DIAGNOSTICS"),
    ("callback-attempt-mismatch", "internal/feishulogin/handler.go",
     "if req.AttemptID != \"\" && attempt.Request.AttemptID != req.AttemptID {", "if false {",
     "./internal/feishulogin", "TestCallbackAttemptMismatchRejectsBeforeIssueAndConsumesState", "CALLBACK_ATTEMPT_MISMATCH_BEFORE_ISSUE"),
    ("callback-csp-policy", "internal/mcpgw/callback_diagnostics.go", "default-src 'none'; ", "",
     "./internal/mcpgw", "TestCallbackFailureReasonsPageAndStructuredLog", "CALLBACK_FAILURE_CSP_POLICY"),
    ("browser-csp-count", "internal/mcpgw/browser_e2e_test.go", "console.log('CSP_ERRORS ' + cspErrors);",
     "console.log('CSP_ERRORS ' + (cspErrors + 1));", "./internal/mcpgw",
     "TestBrowserCallbackExplainsMissingTarget", "BROWSER_CSP_ERRORS"),
]


def main():
    results = []
    for name, relative, old, new, package, test, marker in CASES:
        path = ROOT / relative
        original = path.read_text()
        if old not in original:
            raise SystemExit(f"mutation source not found: {name}: {relative}: {old!r}")
        path.write_text(original.replace(old, new, 1))
        try:
            environment = None
            if name == "browser-csp-count":
                import os
                environment = dict(os.environ, DW_BROWSER_E2E="1",
                                   NODE_PATH="/tmp/dw-filecard-playwright/node_modules")
            proc = subprocess.run(["go", "test", "-count=1", package, "-run", f"^{test}$"],
                                  cwd=ROOT, text=True, stdout=subprocess.PIPE,
                                  stderr=subprocess.STDOUT, timeout=180, env=environment)
            build_error = any(s in proc.stdout.lower() for s in
                              ("build failed", "syntax error", "undefined:", "expected '"))
            hit = marker in proc.stdout and proc.returncode != 0 and not build_error
            if not hit:
                print(proc.stdout[-5000:])
                raise SystemExit(f"MUTATION_NOT_KILLED name={name} rc={proc.returncode} marker={marker} build_error={build_error}")
            results.append({"mutation": name, "rc": proc.returncode, "marker": marker, "semantic_red": True})
            print(f"MUTATION_RED {name} marker={marker}", flush=True)
        finally:
            path.write_text(original)
    print(f"MUTATION_SUMMARY {len(results)}/{len(CASES)} semantic reds")


if __name__ == "__main__":
    main()
