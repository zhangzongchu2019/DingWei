"""Run three isolated guard-removal mutants and require each named test to fail."""
import json
import os
import argparse
from pathlib import Path
import re
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
OUT = Path("/tmp/subagent-compat-mutations")


def run(name, argv, env, cwd=ROOT):
    result = subprocess.run(argv, cwd=cwd, env=env, text=True, capture_output=True, timeout=180)
    (OUT / f"{name}.log").write_text(result.stdout + result.stderr)
    return result.returncode, result.stdout + result.stderr


def main():
    global OUT
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=OUT)
    OUT = parser.parse_args().output.resolve()
    OUT.mkdir(parents=True, exist_ok=False)
    clean = {k: v for k, v in os.environ.items()
             if not k.startswith(("SH_", "DINGWEI_", "DW_", "DATAD_"))
             and k not in ("CODEX_HOME", "CLAUDE_CONFIG_DIR")}
    clean.setdefault("GOCACHE", str(Path.home() / ".cache/go-build"))
    clean.setdefault("GOMODCACHE", str(Path.home() / "go/pkg/mod"))
    results = {}
    with tempfile.TemporaryDirectory(prefix="subagent-mutants-", dir="/tmp") as td:
        tmp = Path(td)
        amr_src = (ROOT / "internal/m8/amr.go").read_text()
        amr_filter = '\t\tname := strings.TrimSuffix(token, "!")\n\t\tif !amrTokenName.MatchString(name) || strings.Count(name, ".") < 2 {\n'
        amr_guard = '\t\tif strings.Count(token, ".") >= 2 {\n\t\t\tskipped = append(skipped, token)\n\t\t\tcontinue\n\t\t}\n'
        if amr_guard not in amr_src or amr_filter not in amr_src:
            raise RuntimeError("AMR guard source anchor missing")
        amr_mutant = tmp / "amr-mutant.go"
        amr_mutant.write_text(amr_src.replace(amr_filter, '\t\tif true {\n', 1).replace(amr_guard, "", 1))
        hub_src = (ROOT / "internal/m8/hub.go").read_text()
        hub_pattern = re.compile(r"func rejectUnnegotiatedSubAgentEnvelope\(env model\.Envelope\) error \{.*?\n\}\n\nfunc \(h \*Hub\) RouteEnvelope", re.S)
        hub_mutated, count = hub_pattern.subn(
            "func rejectUnnegotiatedSubAgentEnvelope(env model.Envelope) error {\n\treturn nil\n}\n\nfunc (h *Hub) RouteEnvelope", hub_src, count=1)
        if count != 1:
            raise RuntimeError("Hub guard source anchor missing")
        hub_mutant = tmp / "hub-mutant.go"
        hub_mutant.write_text(hub_mutated)

        for name, source in (("amr", amr_mutant), ("hub", hub_mutant)):
            overlay = tmp / f"{name}-overlay.json"
            overlay.write_text(json.dumps({"Replace": {str(ROOT / f"internal/m8/{'amr.go' if name == 'amr' else 'hub.go'}"): str(source)}}))
            target = "TestSubagentAMRMentionSkippedButRootKept" if name == "amr" else "TestUnnegotiatedSubPathNeverReachesParent"
            rc, log = run(name, ["go", "test", "-vet=off", f"-overlay={overlay}", "./internal/m8", "-run", f"^{target}$", "-count=1"], clean)
            expected = "AA005_CHILD_MENTION_NOT_SKIPPED_WITH_VALID_ROOT_RETAINED" if name == "amr" else "UNNEGOTIATED_SUBPATH_REACHED_PARENT"
            results[name] = {"rc": rc, "expected_assertion_red": expected in log, "assertion": expected}

        send_src = (ROOT / "tools/sessionhelper/sessionhelper/send_dingwei.py").read_text()
        guard = "    if unsupported_sub_agent_target(args[0]):\n"
        if guard not in send_src:
            raise RuntimeError("send.py guard source anchor missing")
        send_mutant = tmp / "send-mutant.py"
        send_mutant.write_text(send_src.replace(guard, "    if False and unsupported_sub_agent_target(args[0]):\n", 1))
        py_env = dict(clean, SEND_DINGWEI_TEST_MODULE=str(send_mutant), PYTHONDONTWRITEBYTECODE="1")
        rc, log = run("sendpy", ["python3", "-m", "unittest", "tools/sessionhelper/tests/test_ipc_send_addr.py", "-v"], py_env)
        results["sendpy"] = {"rc": rc, "expected_assertion_red": "test_three_segment_target_rejected_before_ipc" in log and "FAIL" in log, "assertion": "test_three_segment_target_rejected_before_ipc"}

    ok = all(v["rc"] != 0 and v["expected_assertion_red"] for v in results.values())
    (OUT / "RESULT.json").write_text(json.dumps({"mutations": results, "all_expected_red": ok}, indent=2) + "\n")
    print(json.dumps({"mutations": results, "all_expected_red": ok}, indent=2))
    return 0 if ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
