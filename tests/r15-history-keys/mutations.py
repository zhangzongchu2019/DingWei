#!/usr/bin/env python3
"""Three semantic regressions for the R15 new-platform canonical history key."""
from pathlib import Path
import os
import subprocess

ROOT = Path(__file__).resolve().parents[2]
CASES = [
    (
        "reader-owner-reverts-to-owner-lc",
        'owner, agent, session = AgentKeyID(ag.OwnerLC), ag.ID, ag.NameLC()',
        'owner, agent, session = ag.OwnerLC, ag.ID, ag.NameLC()',
        "HISTORY_NEWPLAT_ROUNDTRIP",
    ),
    (
        "reader-session-reverts-to-path-name",
        'owner, agent, session = AgentKeyID(ag.OwnerLC), ag.ID, ag.NameLC()',
        'owner, agent = AgentKeyID(ag.OwnerLC), ag.ID',
        "HISTORY_NEWPLAT_ROUNDTRIP",
    ),
    (
        "writer-owner-drops-u-prefix",
        'Append(context.Background(), owner, agent, c.sessionName, eventID,',
        'Append(context.Background(), strings.TrimPrefix(owner, "u_"), agent, c.sessionName, eventID,',
        "HISTORY_NEWPLAT_ROUNDTRIP",
    ),
]
BUILD_ERRORS = ("build failed", "syntax error", "undefined:", "expected '", "cannot use ")


def main():
    path = ROOT / "internal/m8/terminal.go"
    original = path.read_text()
    for name, old, new, marker in CASES:
        if old not in original:
            raise SystemExit(f"mutation source not found: {name}")
        path.write_text(original.replace(old, new, 1))
        try:
            env = dict(os.environ, HUB_TEST_PG_STRICT="1")
            proc = subprocess.run(
                ["go", "test", "-p", "1", "-count=1", "./internal/m8", "-run", "^TestNewPlatformSessionHistoryRoundTripByFullName$"],
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
