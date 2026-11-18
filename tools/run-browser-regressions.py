#!/usr/bin/env python3
"""Run every browser regression suite and emit one machine receipt.

Exit codes follow the repository release convention:
  0: every suite passed
  2: at least one suite assertion failed
  3: the local environment cannot run the suites
"""

from __future__ import annotations

import argparse
import os
from pathlib import Path
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import time


ROOT = Path(__file__).resolve().parents[1]
SUITES = (
    ("history_provenance", "node", ROOT / "view-service/history-provenance-browser-regression.js"),
    ("renderer_model_admission", "node", ROOT / "view-service/renderer-model-admission-regression.js"),
    ("mcp_zero_consent", "python3", ROOT / "docs/reviews/mcp-zero-consent/browser_regression.py"),
    ("sub_agent_tree", "node", ROOT / "view-service/sub-agent-tree-browser-regression.js"),
    ("openspec_binding_admin", "node", ROOT / "view-service/openspec-binding-admin-regression.js"),
    ("r14_sticky_browser", "node", ROOT / "view-service/r14-sticky-browser-regression.js"),
    ("coordinate_review_browser", "node", ROOT / "view-service/coordinate-review-browser-regression.js"),
    ("openspec_read_browser", "node", ROOT / "view-service/openspec-read-browser-regression.js"),
    ("renderer_0206_file_render", "node", ROOT / "view-service/renderer-0206-file-render-regression.js"),
    ("trusted_origins_file_flow", "node", ROOT / "view-service/trusted-origins-file-flow-regression.js"),
    ("console_browser", "node", ROOT / "view-service/console-browser-regression.js"),
    ("console_temp_groups_offstate", "node", ROOT / "view-service/console-temp-groups-offstate-regression.js"),
    ("console_temp_groups", "node", ROOT / "view-service/console-temp-groups-regression.js"),
    ("renderer_in_console_iframe", "node", ROOT / "view-service/renderer-in-console-iframe-regression.js"),
    ("history_event_object", "node", ROOT / "view-service/history-event-object-regression.js"),
    ("view_input_capture_replay", "node", ROOT / "view-service/capture-input-replay-regression.js"),
    ("renderer_sse_reconnect", "node", ROOT / "view-service/renderer-sse-reconnect.js"),
    ("r15_session_history_pagination", "node", ROOT / "view-service/r15-session-history-browser-regression.js"),
    ("renderer_timeline_order", "node", ROOT / "view-service/renderer-timeline-order-regression.js"),
    ("renderer_epoch_order", "node", ROOT / "view-service/renderer-epoch-order-regression.js"),
    ("r15_session_history_mutations", "node", ROOT / "view-service/r15-session-history-mutations.js"),
    ("r15_follow_release_after_replay", "node", ROOT / "view-service/r15-follow-release-after-replay-regression.js"),
    ("renderer_failcard", "node", ROOT / "view-service/renderer-failcard-regression.js"),
    ("renderer_t054_errorboard", "node", ROOT / "view-service/renderer-t054-errorboard-regression.js"),
    ("renderer_t074_delivery_id", "node", ROOT / "view-service/renderer-t074-delivery-id-regression.js"),
    ("renderer_card_status", "node", ROOT / "view-service/renderer-card-status-regression.js"),
    ("renderer_provenance_icon", "node", ROOT / "view-service/renderer-provenance-icon-regression.js"),
    ("renderer_agent_icon_admission", "node", ROOT / "view-service/renderer-agent-icon-admission-regression.js"),
    ("renderer_storage_denied", "node", ROOT / "view-service/renderer-storage-denied-regression.js"),
    ("terminal_storage_denied", "node", ROOT / "view-service/terminal-storage-denied-regression.js"),
    ("renderer_t086_empty_ring_e2e", "node", ROOT / "view-service/renderer-t086-empty-ring-e2e-regression.js"),
    ("renderer_replay_seq", "node", ROOT / "view-service/renderer-replay-seq-regression.js"),
    ("renderer_offturn_fold", "node", ROOT / "view-service/renderer-offturn-fold-regression.js"),
    ("renderer_upload_ticket_timeout", "node", ROOT / "view-service/renderer-upload-ticket-timeout-regression.js"),
    ("renderer_flood_stress", "node", ROOT / "view-service/renderer-flood-stress-regression.js"),
    ("renderer_c2", "node", ROOT / "view-service/renderer-c2-regression.js"),
    ("renderer_delivery_status", "node", ROOT / "view-service/renderer-delivery-status-regression.js"),
    ("renderer_retry_role", "node", ROOT / "view-service/renderer-retry-role-regression.js"),
    ("console_specs_browser", "node", ROOT / "view-service/console-specs-browser-regression.js"),
    ("console_spec_workflow", "node", ROOT / "view-service/console-spec-workflow-regression.js"),
    ("console_task_flow_r32", "node", ROOT / "view-service/r32-task-flow-browser-regression.js"),
    ("pm_spec_flow", "node", ROOT / "view-service/pm-spec-flow-browser-regression.js"),
    ("md_render_core_sync", "python3", ROOT / "view-service/test_md_render_core.py"),
    ("md_render_core_parity", "node", ROOT / "view-service/md-render-core-parity.js"),
    ("renderer_markdown_image", "node", ROOT / "view-service/renderer-markdown-image-regression.js"),
    ("renderer_markdown_injection", "node", ROOT / "view-service/renderer-markdown-injection-regression.js"),
    ("renderer_failure_dock_embedded", "node", ROOT / "view-service/renderer-failure-dock-embedded-regression.js"),
    ("renderer_failure_dock", "node", ROOT / "view-service/renderer-failure-dock-regression.js"),
)

# Only names are recorded; values may contain local paths or credentials. These
# are the caller-controlled inputs that can replace the repository artifact or
# its fixtures/mock contract. Runner-generated MOCK_BASE/CERT/KEY values are not
# overrides, but a caller-supplied value is still reported rather than hidden.
OVERRIDE_ENV_NAMES = (
    # history_provenance invokes the actual Go Hub with explicit offline caches.
    "PATH", "GOPATH", "GOMODCACHE", "GOCACHE",
    "FIXTURES", "MOCK_BASE", "MOCK_CERT", "MOCK_KEY", "MOCK_SESSION",
    "HUB_V2_GOLDEN_FILE", "NODE_PATH", "PLAYWRIGHT_BROWSERS_PATH", "PW_CHANNEL",
    "RENDERER_FILE", "RENDERER_OVERRIDE", "TIMELINE_DEBUG",
    "STALE_SCREENSHOT",  # model admission suite: optional screenshot output path
    "MUTATE_HISTORY_EVENT_OBJECT",  # history_event_object: one-cut negative control
    "MUTATE_HISTORY_SSE_INPUT_BUFFER",  # history_event_object: remove initial SSE buffering for the replay regression
    "MUTATE_HISTORY_INPUT_RECEIVED_AT",  # history_event_object: remove the received_at fallback for history inputs without ts
    "HISTORY_REPLAY_BASELINE",  # history_event_object: explicit pre-fix comparison only
    # renderer_flood_stress：反向期望、读数落盘路径、推入时限（取值固定在套件里，这三项只改判法／落点／时限）
    "STRESS_EXPECT_MUTANT", "STRESS_READINGS", "STRESS_DEADLINE_S",
)
# These are overwritten by the runner before the consumer sees them. They are
# excluded from the coverage requirement, although caller-supplied values are
# still conservatively disclosed in the receipt.
RUNNER_CONTROLLED_ENV_NAMES = {"MOCK_BASE", "MOCK_CERT", "MOCK_KEY", "SHOTS"}
# Node and Playwright consume these without a process.env read in suite source.
IMPLICIT_ENV_NAMES = {"NODE_PATH", "PLAYWRIGHT_BROWSERS_PATH"}


class EnvironmentUnavailable(RuntimeError):
    pass


def run(command: list[str], *, env: dict[str, str] | None = None, capture: bool = False) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        command,
        cwd=ROOT,
        env=env,
        text=True,
        stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.STDOUT if capture else None,
        check=False,
    )


def require_command(name: str) -> None:
    if shutil.which(name) is None:
        raise EnvironmentUnavailable(f"missing command: {name}")


def unregistered_environment_inputs(extra_source: str = "") -> set[str]:
    """Mechanically find suite environment inputs missing from the receipt registry."""
    source_paths = [path for _, _, path in SUITES] + [
        ROOT / "view-service/renderer-0206-contract-mock.py",
        Path(__file__).resolve(),
    ]
    chunks = []
    for path in source_paths:
        try:
            chunks.append(path.read_text(encoding="utf-8"))
        except OSError as error:
            raise EnvironmentUnavailable(f"cannot inspect environment reads in {path}: {error}") from error
    chunks.append(extra_source)
    discovered = set(IMPLICIT_ENV_NAMES)
    patterns = (
        r"process\.env\.([A-Z][A-Z0-9_]*)",
        r"env\.get\([\"']([A-Z][A-Z0-9_]*)[\"']\)",
        r"os\.environ(?:\.get)?\([\"']([A-Z][A-Z0-9_]*)[\"']",
        r"os\.environ\[[\"']([A-Z][A-Z0-9_]*)[\"']\]",
    )
    for source in chunks:
        for pattern in patterns:
            discovered.update(re.findall(pattern, source))
    return discovered - RUNNER_CONTROLLED_ENV_NAMES - set(OVERRIDE_ENV_NAMES)


def check_environment_input_registry() -> None:
    missing = unregistered_environment_inputs()
    if missing:
        raise EnvironmentUnavailable(
            "environment inputs missing from OVERRIDE_ENV_NAMES: " + ", ".join(sorted(missing))
        )


def selftest_environment_input_registry() -> int:
    missing = unregistered_environment_inputs()
    synthetic = "DW_UNREGISTERED_BROWSER_INPUT"
    caught = unregistered_environment_inputs(f"process.env.{synthetic}\n")
    if missing or caught != {synthetic}:
        print(
            "environment registry selftest failed: baseline=%s synthetic=%s"
            % (sorted(missing), sorted(caught)),
            file=sys.stderr,
        )
        return 2
    print("environment registry selftest passed: an unregistered process.env input is rejected")
    return 0


def probe_environment(env: dict[str, str]) -> None:
    check_environment_input_registry()
    for command in ("node", "python3", "openssl", "git", "date"):
        require_command(command)
    missing = [str(path.relative_to(ROOT)) for _, _, path in SUITES if not path.is_file()]
    mock = ROOT / "view-service/renderer-0206-contract-mock.py"
    if not mock.is_file():
        missing.append(str(mock.relative_to(ROOT)))
    if missing:
        raise EnvironmentUnavailable("missing suite files: " + ", ".join(missing))

    resolved = run(["node", "-e", "require.resolve('playwright')"], env=env, capture=True)
    if resolved.returncode != 0:
        raise EnvironmentUnavailable("Node cannot resolve playwright (set NODE_PATH to its node_modules directory)")

    # The file-render suite launches both its configured browser and, in one case,
    # Playwright's bundled Chromium. Probe both before running assertions so a
    # missing browser is rc=3 rather than masquerading as a product failure.
    channels = [None]
    if env.get("PW_CHANNEL"):
        channels.append(env["PW_CHANNEL"])
    for channel in channels:
        launch = "{}" if channel is None else "{channel:%r}" % channel
        script = (
            "const {chromium}=require('playwright');"
            f"chromium.launch({launch}).then(b=>b.close()).then(()=>process.exit(0))"
            ".catch(e=>{console.error(e.message);process.exit(1)})"
        )
        probed = run(["node", "-e", script], env=env, capture=True)
        if probed.returncode != 0:
            label = "bundled chromium" if channel is None else f"channel {channel}"
            detail = (probed.stdout or "").strip().splitlines()
            suffix = f": {detail[-1]}" if detail else ""
            raise EnvironmentUnavailable(f"cannot launch Playwright {label}{suffix}")


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def wait_for_port(process: subprocess.Popen[str], port: int) -> None:
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        if process.poll() is not None:
            output = process.stdout.read().strip() if process.stdout else ""
            raise EnvironmentUnavailable(f"contract mock exited before readiness: {output}")
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=0.2):
                return
        except OSError:
            time.sleep(0.1)
    raise EnvironmentUnavailable("contract mock did not become ready within 10 seconds")


def generate_certificate(directory: Path) -> tuple[Path, Path]:
    cert = directory / "cert.pem"
    key = directory / "key.pem"
    generated = run([
        "openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
        "-keyout", str(key), "-out", str(cert), "-days", "1",
        "-subj", "/CN=127.0.0.1",
    ], capture=True)
    if generated.returncode != 0:
        raise EnvironmentUnavailable("openssl could not generate the temporary mock certificate")
    return cert, key


def utc_now() -> str:
    result = run(["date", "-u", "+%Y-%m-%dT%H:%M:%SZ"], capture=True)
    if result.returncode != 0:
        raise EnvironmentUnavailable("date -u failed")
    return result.stdout.strip()


def commit_sha() -> str:
    result = run(["git", "rev-parse", "HEAD"], capture=True)
    if result.returncode != 0:
        raise EnvironmentUnavailable("git rev-parse HEAD failed")
    return result.stdout.strip()


def read_loadavg() -> str:
    """First three /proc/loadavg values; 'unavailable' where the file cannot be read."""
    try:
        return " ".join(Path("/proc/loadavg").read_text(encoding="utf-8").split()[:3])
    except OSError:
        return "unavailable"


def receipt(timestamp: str, sha: str, overrides: tuple[str, ...], results: dict[str, int], aggregate: int, flood_loadavg: dict[str, str] | None = None) -> str:
    lines = [
        "DW_BROWSER_RECEIPT_V1_BEGIN",
        f"generated_at_utc={timestamp}",
        f"commit_sha={sha}",
        "overrides=" + (",".join(overrides) if overrides else "none"),
    ]
    lines.extend(f"suite.{name}.rc={results.get(name, 3)}" for name, _, _ in SUITES)
    if flood_loadavg is not None:
        lines.append(f"suite.renderer_flood_stress.loadavg_start={flood_loadavg.get('start', 'unavailable')}")
        lines.append(f"suite.renderer_flood_stress.loadavg_end={flood_loadavg.get('end', 'unavailable')}")
    lines.extend((f"aggregate_rc={aggregate}", "DW_BROWSER_RECEIPT_V1_END"))
    return "\n".join(lines) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--receipt", type=Path, help="also write the complete machine receipt to this file")
    parser.add_argument("--selftest-env-coverage", action="store_true")
    args = parser.parse_args()

    if args.selftest_env_coverage:
        return selftest_environment_input_registry()

    env = os.environ.copy()
    overrides = tuple(name for name in OVERRIDE_ENV_NAMES if env.get(name))
    results: dict[str, int] = {}
    flood_loadavg: dict[str, str] | None = None
    aggregate = 3
    mock: subprocess.Popen[str] | None = None
    try:
        probe_environment(env)
        with tempfile.TemporaryDirectory(prefix="dw-browser-gate-") as temporary:
            temporary_path = Path(temporary)
            cert, key = generate_certificate(temporary_path)
            port = free_port()
            mock_env = env | {"MOCK_CERT": str(cert), "MOCK_KEY": str(key)}
            mock = subprocess.Popen(
                ["python3", str(ROOT / "view-service/renderer-0206-contract-mock.py"), str(port)],
                cwd=ROOT,
                env=mock_env,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
            )
            wait_for_port(mock, port)
            suite_env = env | {
                "MOCK_BASE": f"https://127.0.0.1:{port}",
                "SHOTS": str(temporary_path / "shots"),
            }
            for name, interpreter, path in SUITES:
                print(f"\n=== browser suite: {name} ===", flush=True)
                if name == "renderer_flood_stress":
                    flood_loadavg = {"start": read_loadavg()}
                completed = run([interpreter, str(path)], env=suite_env)
                results[name] = completed.returncode
                if name == "renderer_flood_stress" and flood_loadavg is not None:
                    flood_loadavg["end"] = read_loadavg()
            if len(results) != len(SUITES):
                raise EnvironmentUnavailable(
                    f"only ran {len(results)}/{len(SUITES)} browser suites; an incomplete run cannot pass"
                )
            aggregate = 0 if all(code == 0 for code in results.values()) else 2
    except EnvironmentUnavailable as error:
        print(f"ENVIRONMENT_UNAVAILABLE: {error}", file=sys.stderr)
        aggregate = 3
    finally:
        if mock is not None and mock.poll() is None:
            mock.terminate()
            try:
                mock.wait(timeout=3)
            except subprocess.TimeoutExpired:
                mock.kill()
                mock.wait()

    try:
        rendered = receipt(utc_now(), commit_sha(), overrides, results, aggregate, flood_loadavg)
    except EnvironmentUnavailable as error:
        print(f"ENVIRONMENT_UNAVAILABLE: {error}", file=sys.stderr)
        return 3
    print("\n" + rendered, end="")
    if args.receipt:
        args.receipt.write_text(rendered, encoding="utf-8")
    return aggregate


if __name__ == "__main__":
    raise SystemExit(main())
