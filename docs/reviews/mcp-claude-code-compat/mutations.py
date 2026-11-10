#!/usr/bin/env python3
"""Four semantic mutations for S-MCP-CLAUDE-COMPAT; compile errors are not red."""
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[3]
CASES = [
    ("localhost rejection", "internal/mcpgw/clients.go", "host := u.Hostname()\n\tip := net.ParseIP(host)\n\thttpLoopback := ip != nil && ip.IsLoopback() || strings.EqualFold(host, \"localhost\")", "host := u.Hostname()\n\tip := net.ParseIP(host)\n\thttpLoopback := ip != nil && ip.IsLoopback()\n\tif strings.EqualFold(host, \"localhost\") { return false }", "TestClaudeCodeLocalhostRedirectPortAndExactPath", "CLAUDE_LOCALHOST"),
    ("refresh_token metadata rejection", "internal/mcpgw/clients.go", "case \"refresh_token\":\n\t\t\t// CIMD may advertise refresh support; this gateway still only issues\n\t\t\t// authorization-code grants at the token endpoint.", "case \"refresh_token\":\n\t\t\treturn errParams", "TestClaudeCodeGrantTypesPermitRefreshMetadataOnly", "CLAUDE_REFRESH_GRANT"),
    ("CSP localhost source omitted", "internal/mcpgw/oauth.go", "127\\.0\\.0\\.1|\\[::1\\]|localhost", "127\\.0\\.0\\.1|\\[::1\\]", "TestWithLoopbackFormActionOnlyExactLoopback", "CLAUDE_CSP_LOCALHOST_ORIGIN"),
    ("localhost path prefix matching", "internal/mcpgw/clients.go", "allowed.EscapedPath() != requested.EscapedPath()", "!strings.HasPrefix(requested.EscapedPath(), allowed.EscapedPath())", "TestClaudeCodeLocalhostRedirectPortAndExactPath", "CLAUDE_LOCALHOST_PATH_EXACT"),
]

def run(args):
    return subprocess.run(args, cwd=ROOT, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)

for name, rel, old, new, test, marker in CASES:
    path = ROOT / rel
    source = path.read_text()
    if source.count(old) != 1:
        raise SystemExit(f"{name}: mutation anchor count {source.count(old)}")
    path.write_text(source.replace(old, new, 1))
    try:
        p = run(["go", "test", "-count=1", "-run", f"^{test}$", "./internal/mcpgw"])
        output = p.stdout
        compile_error = any(s in output for s in ("[build failed]", "undefined:", "syntax error:", "cannot use "))
        red = p.returncode != 0 and marker in output and not compile_error
        print(f"{'RED' if red else 'BAD'} {name}: rc={p.returncode} marker={marker in output} compile_error={compile_error}")
        if not red:
            print(output)
            raise SystemExit(1)
    finally:
        path.write_text(source)
print("MUTATIONS 4/4 RED; compile failures excluded")
