#!/usr/bin/env python3
"""Semantic regressions for R15 new-platform history keys and purge hooks."""
from pathlib import Path
import os
import subprocess

ROOT = Path(__file__).resolve().parents[2]
CASES = [
    (
        "internal/m8/terminal.go",
        "reader-owner-reverts-to-owner-lc",
        'owner, agent, session = AgentKeyID(ag.OwnerLC), ag.ID, ag.NameLC()',
        'owner, agent, session = ag.OwnerLC, ag.ID, ag.NameLC()',
        "HISTORY_NEWPLAT_ROUNDTRIP",
        "^TestNewPlatformSessionHistoryRoundTripByFullName$",
    ),
    (
        "internal/m8/terminal.go",
        "reader-session-reverts-to-path-name",
        'owner, agent, session = AgentKeyID(ag.OwnerLC), ag.ID, ag.NameLC()',
        'owner, agent = AgentKeyID(ag.OwnerLC), ag.ID',
        "HISTORY_NEWPLAT_ROUNDTRIP",
        "^TestNewPlatformSessionHistoryRoundTripByFullName$",
    ),
    (
        "internal/m8/terminal.go",
        "writer-owner-drops-u-prefix",
        'Append(context.Background(), owner, agent, c.sessionName, eventID,',
        'Append(context.Background(), strings.TrimPrefix(owner, "u_"), agent, c.sessionName, eventID,',
        "HISTORY_NEWPLAT_ROUNDTRIP",
        "^TestNewPlatformSessionHistoryRoundTripByFullName$",
    ),
    (
        "internal/m8/newplat_shim.go",
        "revoke-purge-owner-reverts-to-owner-lc",
        "RequestAgentDeletion(context.Background(), AgentKeyID(agent.OwnerLC), agent.ID, h.now())",
        "RequestAgentDeletion(context.Background(), agent.OwnerLC, agent.ID, h.now())",
        "HISTORY_NEWPLAT_PURGE_ON_REVOKE",
        "^TestNewPlatformSessionHistoryPurgeHooks/agent_revocation_removes_only_that_agent_history$",
    ),
    (
        "internal/m8/newplat_shim.go",
        "user-disable-purge-owner-reverts-to-owner-lc",
        "RequestAgentDeletion(ctx, AgentKeyID(ag.OwnerLC), ag.ID, h.now())",
        "RequestAgentDeletion(ctx, ag.OwnerLC, ag.ID, h.now())",
        "HISTORY_NEWPLAT_PURGE_ON_USER_DISABLE",
        "^TestNewPlatformSessionHistoryPurgeHooks/user_disable_removes_all_that_user's_Agent_history_only$",
    ),
]
BUILD_ERRORS = ("build failed", "syntax error", "undefined:", "expected '", "cannot use ")


def main():
    for relpath, name, old, new, marker, test_name in CASES:
        path = ROOT / relpath
        original = path.read_text()
        if old not in original:
            raise SystemExit(f"mutation source not found: {name}")
        path.write_text(original.replace(old, new, 1))
        try:
            env = dict(os.environ, HUB_TEST_PG_STRICT="1")
            proc = subprocess.run(
                ["go", "test", "-p", "1", "-count=1", "./internal/m8", "-run", test_name],
                cwd=ROOT,
                env=env,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                timeout=180,
            )
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
