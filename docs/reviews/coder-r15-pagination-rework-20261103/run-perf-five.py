#!/usr/bin/env python3
"""Run five isolated paired R15 publish-latency measurements."""
import datetime as dt
import os
from pathlib import Path
import re
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[3]
LOG = Path(__file__).with_name("perf-five.log")
METRIC = re.compile(r"paired_additional_p99=([^ ]+) threshold=([^\s]+)")


def utc_now():
    return dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds")


def main():
    home = Path("/tmp/dw-r15-perf-isolated-home")
    home.mkdir(parents=True, exist_ok=True)
    env = {
        "PATH": "/usr/local/go/bin:/usr/bin:/bin",
        "HOME": str(home),
        "TMPDIR": "/tmp",
        "GOPATH": "/home/dev/go",
        "GOMODCACHE": "/home/dev/go/pkg/mod",
        "GOCACHE": "/home/dev/.cache/go-build",
        "GOTOOLCHAIN": "local",
        "DW_SESSION_HISTORY_PERF": "1",
    }
    header = [
        f"started_utc={utc_now()}",
        f"cpu_count={os.cpu_count()}",
        f"loadavg_1_5_15={os.getloadavg()}",
        "environment=env-i; inherited SH_*, DW_*, DINGWEI_* and ambient config omitted",
        "command=go test ./internal/m8 -run ^TestSessionHistoryTwentyAgentPublishP99$ -count=1 -v",
    ]
    print("\n".join(header), flush=True)
    results = []
    with LOG.open("w", encoding="utf-8") as log:
        log.write("\n".join(header) + "\n")
        for run in range(1, 6):
            command = ["go", "test", "./internal/m8", "-run", "^TestSessionHistoryTwentyAgentPublishP99$", "-count=1", "-v"]
            result = subprocess.run(command, cwd=ROOT, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            metrics = METRIC.findall(result.stdout)
            block = f"\n=== ROUND {run} ended_utc={utc_now()} rc={result.returncode} ===\n{result.stdout}"
            print(f"ROUND {run}: rc={result.returncode} metric={metrics[-1] if metrics else 'missing'}", flush=True)
            log.write(block)
            log.flush()
            if result.returncode != 0 or len(metrics) != 1:
                results.append((run, None, "test failed or metric missing/duplicated"))
                continue
            measured, threshold = metrics[0]
            value = float(measured.removesuffix("ms"))
            limit = float(threshold.removesuffix("ms"))
            results.append((run, value, "PASS" if value <= limit else f"FAIL > {threshold}"))
        log.write(f"\nfinished_utc={utc_now()}\n")
    print("RESULTS=" + repr(results))
    print(f"RAW_LOG={LOG}")
    return int(len(results) != 5 or any(status != "PASS" for _, _, status in results))


if __name__ == "__main__":
    raise SystemExit(main())
