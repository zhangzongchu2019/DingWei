"""独立真 Hub/PG + Chromium 单目标、多目标授权回归；不使用在线凭据。"""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]

def run_mode(mode, output, cut=None, chain="consent"):
    mode_label = chain+"-"+mode
    run = output / mode_label
    env = {"PATH": "/usr/bin:/bin:/home/dev/.local/bin", "HOME": "/home/dev", "GOPROXY": "off", "HUB_TEST_PG_STRICT": "1", "NODE_PATH": os.environ.get("NODE_PATH", "/tmp/coder-a3-playwright/node_modules")}
    hub = subprocess.Popen(["python3", str(HERE / "probe_launcher.py"), "--run-dir", str(run), "--browser-mode", mode, "--browser-chain", chain] + (["--browser-cut", cut] if cut else []), cwd=ROOT, env=env)
    rc = 1
    try:
        deadline = time.monotonic() + 120
        while not (run / "ready.json").exists():
            if hub.poll() is not None:
                raise RuntimeError("isolated Hub failed; see hub.stdout")
            if time.monotonic() > deadline:
                raise RuntimeError("isolated Hub readiness timeout")
            time.sleep(.1)
        result = subprocess.run(["node", str(HERE / "browser_probe.js"), str(run / "ready.json"), mode, chain], cwd=ROOT, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
        (run / "browser.stdout").write_bytes(result.stdout)
        print(result.stdout.decode(), end="", flush=True)
        rc = result.returncode
    finally:
        if run.exists():
            (run / "stop").touch()
        try:
            hub.wait(timeout=60)
        except subprocess.TimeoutExpired:
            hub.terminate()
            hub.wait(timeout=10)
        if run.exists():
            (run / "browser-result.json").write_text(json.dumps({"mode": mode, "chain": chain, "browser_rc": rc, "hub_rc": hub.returncode, "real_credentials_used": False}, indent=2)+"\n")
    if rc or hub.returncode:
        raise RuntimeError("browser/Hub failed: " + mode)

if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path)
    parser.add_argument("--mode", choices=("single", "multi"))
    parser.add_argument("--cut", choices=("connect-csp", "selection-csp", "loopback-csp"))
    parser.add_argument("--chain", choices=("immediate", "consent"))
    args = parser.parse_args()
    if args.output:
        args.output.mkdir(parents=True, exist_ok=False)
        for mode in ([args.mode] if args.mode else ("single", "multi")):
            for chain in ([args.chain] if args.chain else ("immediate", "consent")):
                run_mode(mode, args.output, args.cut, chain)
    else:
        with tempfile.TemporaryDirectory(prefix="zc-browser-") as directory:
            for mode in ([args.mode] if args.mode else ("single", "multi")):
                for chain in ("immediate", "consent"):
                    run_mode(mode, Path(directory), args.cut, chain)
