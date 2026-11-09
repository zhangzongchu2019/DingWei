"""全库回退自检守卫撤除；仅 Go overlay、临时数据库和白名单环境。"""
import argparse
import datetime
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
CASES = [
    ("inventory", "internal/store/sub_agent_rollback_check.go",
     "WHERE NOT EXISTS", "WHERE 0 AND NOT EXISTS", "./internal/store",
     "^TestSubAgentRollbackCheckAllRoots$", "AA009_GLOBAL_ROLLBACK_PENDING"),
    ("cli-exit", "cmd/subagent-rollback/main.go",
     "if result.Pending != 0 {", "if false {", "./cmd/subagent-rollback",
     "^TestCheckAllCommandBlocksUnpreparedRoot$", "AA009_CHECK_CLI_EXIT"),
    ("read-only", "internal/store/sqlite.go",
     "?mode=ro&_pragma=query_only(ON)&_pragma=busy_timeout(5000)",
     "?mode=rw&_pragma=query_only(OFF)&_pragma=busy_timeout(5000)", "./internal/store",
     "^TestSubAgentRollbackReadOnlyOpen$", "AA009_READONLY_WRITE_ACCEPTED"),
    ("wal", "internal/store/sqlite.go",
     "?mode=ro&_pragma=query_only(ON)&_pragma=busy_timeout(5000)",
     "?mode=ro&_pragma=query_only(ON)&_pragma=busy_timeout(5000)&immutable=1", "./internal/store",
     "^TestSubAgentRollbackReadOnlyIncludesCommittedWAL$", "AA009_COMMITTED_WAL_IGNORED"),
]


def run(output):
    output.mkdir(exist_ok=False, parents=True)
    results = []
    for name, relative, old, new, package, pattern, target in CASES:
        directory = output / name
        directory.mkdir()
        with tempfile.TemporaryDirectory(prefix="m3-check-") as temporary:
            temporary = Path(temporary)
            home = temporary / "home"
            home.mkdir()
            environment = {
                "PATH": "/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin",
                "HOME": str(home), "TMPDIR": str(temporary),
                "GOPROXY": "off", "GOTOOLCHAIN": "local",
            }
            for key in ("GOCACHE", "GOMODCACHE"):
                environment[key] = subprocess.check_output(["go", "env", key], text=True).strip()
            source = (ROOT / relative).read_text()
            assert source.count(old) == 1, (name, "mutation anchor changed")
            mutant = temporary / "mutant.go"
            mutant.write_text(source.replace(old, new, 1))
            overlay = temporary / "overlay.json"
            overlay.write_text(json.dumps({"Replace": {str(ROOT / relative): str(mutant)}}))
            base = ["go", "test", package, "-run", pattern, "-count=1", "-v"]
            commands = [base, ["go", "test", "-vet=off", "-overlay=" + str(overlay)] + base[2:]]
            reads = []
            for phase, command in zip(("baseline", "mutant"), commands):
                start = datetime.datetime.now(datetime.timezone.utc).isoformat()
                result = subprocess.run(command, cwd=ROOT, env=environment,
                                        stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=180)
                (directory / (phase + ".stdout")).write_bytes(result.stdout)
                reads.append({"phase": phase, "command": command, "rc": result.returncode,
                              "start": start, "end": datetime.datetime.now(datetime.timezone.utc).isoformat()})
            text = (directory / "mutant.stdout").read_text()
            passed = reads[0]["rc"] == 0 and reads[1]["rc"] != 0 and target in text and "[build failed]" not in text
            row = {"name": name, "target": target, "target_red": passed, "runs": reads,
                   "uid": os.getuid(), "environment_names": sorted(environment)}
            (directory / "result.json").write_text(json.dumps(row, indent=2) + "\n")
            results.append(row)
            print(name, "target_red=" + str(passed), flush=True)
    (output / "RESULT.json").write_text(json.dumps(results, indent=2) + "\n")
    return int(not all(row["target_red"] for row in results))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    raise SystemExit(run(parser.parse_args().output.resolve()))
