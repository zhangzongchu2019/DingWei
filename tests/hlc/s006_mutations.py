#!/usr/bin/env python3
"""撤掉飞书回调封口，S006迟到回调必须红在DB访问断言。"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    source = root / "internal/feishu/feishu.go"
    original = source.read_text()
    guard = "if !ok {\n\t\tg.receiver.rejected.Add(1)\n\t\treturn ErrReceiverDraining\n\t}"
    assert original.count(guard) == 1, "回调封口变异锚已变化"
    replacement = original.replace(guard, "if !ok {\n\t\tdone = func() {}\n\t}")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="hlc-s006-mutation-") as tmp:
        temp = Path(tmp)
        (temp / "home").mkdir()
        mutant = temp / "feishu.go"
        mutant.write_text(replacement)
        overlay = temp / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {str(source): str(mutant)}}))
        env = {key: os.environ[key] for key in
               ("PATH", "GOCACHE", "GOMODCACHE", "GOPATH", "GOROOT") if key in os.environ}
        env.update(HOME=str(temp / "home"), LANG="C.UTF-8", TZ="UTC")
        command = ["go", "test", "-race", "-overlay", str(overlay), "./internal/feishu",
                   "-run", "^TestReceiverS006InflightAndLateCallbackNoDB$", "-count=1", "-timeout=30s", "-v"]
        started = datetime.datetime.now(datetime.timezone.utc).isoformat()
        result = subprocess.run(command, cwd=root, env=env, text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=60)
        log = output / "no-ingress-seal.log"
        log.write_text(result.stdout)
        receipt = dict(mutation="no-ingress-seal", command=command, started_utc=started,
                       finished_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                       rc=result.returncode, target_assertion="S006_POST_SEAL_DB_ZERO",
                       target_red=result.returncode == 1 and "S006_POST_SEAL_DB_ZERO" in result.stdout,
                       source_sha256=hashlib.sha256(source.read_bytes()).hexdigest(),
                       mutant_sha256=hashlib.sha256(replacement.encode()).hexdigest(),
                       log=log.name, log_sha256=hashlib.sha256(log.read_bytes()).hexdigest())
        (output / "mutation.json").write_text(json.dumps(receipt, indent=2) + "\n")
        print(json.dumps(receipt))
        if not receipt["target_red"]:
            raise SystemExit("S006撤封口未在DB访问断言变红")


if __name__ == "__main__":
    main()
