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
import shutil
import socket
import subprocess
import sys
import tempfile
import time


ROOT = Path(__file__).resolve().parents[1]
SUITES = (
    ("renderer_0206_file_render", ROOT / "view-service/renderer-0206-file-render-regression.js"),
    ("console_browser", ROOT / "view-service/console-browser-regression.js"),
    ("renderer_in_console_iframe", ROOT / "view-service/renderer-in-console-iframe-regression.js"),
)


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


def probe_environment(env: dict[str, str]) -> None:
    for command in ("node", "python3", "openssl", "git", "date"):
        require_command(command)
    missing = [str(path.relative_to(ROOT)) for _, path in SUITES if not path.is_file()]
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


def receipt(timestamp: str, sha: str, results: dict[str, int], aggregate: int) -> str:
    lines = [
        "DW_BROWSER_RECEIPT_V1_BEGIN",
        f"generated_at_utc={timestamp}",
        f"commit_sha={sha}",
    ]
    lines.extend(f"suite.{name}.rc={results.get(name, 3)}" for name, _ in SUITES)
    lines.extend((f"aggregate_rc={aggregate}", "DW_BROWSER_RECEIPT_V1_END"))
    return "\n".join(lines) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--receipt", type=Path, help="also write the complete machine receipt to this file")
    args = parser.parse_args()

    env = os.environ.copy()
    results: dict[str, int] = {}
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
            for name, path in SUITES:
                print(f"\n=== browser suite: {name} ===", flush=True)
                completed = run(["node", str(path)], env=suite_env)
                results[name] = completed.returncode
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
        rendered = receipt(utc_now(), commit_sha(), results, aggregate)
    except EnvironmentUnavailable as error:
        print(f"ENVIRONMENT_UNAVAILABLE: {error}", file=sys.stderr)
        return 3
    print("\n" + rendered, end="")
    if args.receipt:
        args.receipt.write_text(rendered, encoding="utf-8")
    return aggregate


if __name__ == "__main__":
    raise SystemExit(main())
