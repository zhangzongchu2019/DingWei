#!/usr/bin/env python3
"""Exercise the two required performance-gate mutations and restore sources."""
import datetime as dt
import os
from pathlib import Path
import subprocess


ROOT = Path(__file__).resolve().parents[3]
TARGET = ROOT / "internal/m8/session_history_test.go"
LOG = Path(__file__).with_name("perf-mutations.log")


def run(label, command, env=None):
    result = subprocess.run(command, cwd=ROOT, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    return f"\n=== {label} utc={dt.datetime.now(dt.timezone.utc).isoformat(timespec='seconds')} rc={result.returncode} ===\n{result.stdout}", result.returncode


def main():
    started = dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds")
    original = TARGET.read_text()
    entries = []
    try:
        dropped = original.replace("return additional > sessionHistoryPerfBudget", "return false", 1)
        if dropped == original:
            raise RuntimeError("p99 assertion mutation anchor not found")
        TARGET.write_text(dropped)
        entries.append(run("drop_p99_budget_assertion", ["go", "test", "./internal/m8", "-run", "^TestSessionHistoryPerformanceGuardsAreLive$", "-count=1", "-v"]))
        TARGET.write_text(original)

        before = """\t\t\t\t\tstart := time.Now()
\t\t\t\t\th.handleSessionEventEnvelopeContext(context.Background(), c, model.Envelope{ID: fmt.Sprintf(\"%d-%d\", a, n), Body: `{"kind":"assistant_text","text":"performance probe"}`, Meta: map[string]any{\"type\": terminalSessionEventType}})
\t\t\t\t\tlatencies[a*pushes+n] = time.Since(start)"""
        after = """\t\t\t\t\teventID := fmt.Sprintf(\"%d-%d\", a, n)
\t\t\t\t\tif enabled {
\t\t\t\t\t\tif _, err := h.SessionHistory.Append(context.Background(), \"owner\", c.metricsAgentID, c.sessionName, eventID, terminalSessionEventType, []byte(`{"kind":"assistant_text","text":"performance probe"}`), time.Now()); err != nil {
\t\t\t\t\t\t\tt.Errorf(\"persist probe: %v\", err)
\t\t\t\t\t\t}
\t\t\t\t\t}
\t\t\t\t\tstart := time.Now()
\t\t\t\t\th.publishSessionEvent(c.keyID, c.sessionName, `{"kind":"assistant_text","text":"performance probe"}`)
\t\t\t\t\tlatencies[a*pushes+n] = time.Since(start)"""
        moved = original.replace(before, after, 1)
        if moved == original:
            raise RuntimeError("timer-boundary mutation anchor not found")
        TARGET.write_text(moved)
        isolated_env = {
            "PATH": "/usr/local/go/bin:/usr/bin:/bin",
            "HOME": "/tmp/dw-r15-perf-isolated-home",
            "TMPDIR": "/tmp",
            "GOPATH": "/home/dev/go",
            "GOMODCACHE": "/home/dev/go/pkg/mod",
            "GOCACHE": "/home/dev/.cache/go-build",
            "GOTOOLCHAIN": "local",
            "DW_SESSION_HISTORY_PERF": "1",
        }
        entries.append(run("move_persisted_publish_outside_timer", ["go", "test", "./internal/m8", "-run", "^TestSessionHistoryTwentyAgentPublishP99$", "-count=1", "-v"], isolated_env))
    finally:
        TARGET.write_text(original)

    lines = [
        f"started_utc={started}",
        "mutations must return nonzero with the named performance predicate red",
    ]
    passed = True
    for (entry, rc), expected in zip(entries, ("over-budget performance sample was not rejected", "too small to include the synchronous durable append")):
        red = rc != 0 and expected in entry
        passed &= red
        lines.append(entry)
        lines.append(f"MUTATION_RESULT expected_red={str(red).lower()} reason={expected}")
    LOG.write_text("\n".join(lines) + "\n")
    print("\n".join(line for line in lines if line.startswith("MUTATION_RESULT")))
    print(f"RAW_LOG={LOG}")
    return int(not passed or len(entries) != 2)


if __name__ == "__main__":
    raise SystemExit(main())
