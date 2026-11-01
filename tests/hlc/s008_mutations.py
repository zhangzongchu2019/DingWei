#!/usr/bin/env python3
"""S008反证：替换已有源文件，默认vet与race开启，不改工作树。"""
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
    source = root / "internal/bus/async_dbqueue.go"
    original = source.read_text()
    drain = "flushParent = stopCtx\n\t\t\tdrain()"
    first = "if q.failure == nil {"
    assert original.count(drain) == 2, "排尽变异锚已变化"
    assert original.count(first) == 1, "首错变异锚已变化"
    variants = [
        ("no-channel-drain", original.replace(drain, "flushParent = stopCtx\n\t\t\tflush()"),
         "TestShutdownQueueS008BufferedMultipleBatches", "S008_ALL_BUFFERED_COMMITTED"),
        ("overwrite-first-error", original.replace(first, "if true {"),
         "TestShutdownQueueS008FirstFailureAndUncommittedCount", "S008_FIRST_ERROR_AND_REMAINDER"),
    ]
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    results = []
    for name, replacement, test, marker in variants:
        with tempfile.TemporaryDirectory(prefix="hlc-s008-mutation-") as tmp:
            temp = Path(tmp)
            (temp / "home").mkdir()
            mutant = temp / "async_dbqueue.go"
            mutant.write_text(replacement)
            overlay = temp / "overlay.json"
            overlay.write_text(json.dumps({"Replace": {str(source): str(mutant)}}))
            env = {key: os.environ[key] for key in
                   ("PATH", "GOCACHE", "GOMODCACHE", "GOPATH", "GOROOT") if key in os.environ}
            env.update(HOME=str(temp / "home"), LANG="C.UTF-8", TZ="UTC")
            command = ["go", "test", "-race", "-overlay", str(overlay), "./internal/bus",
                       "-run", "^" + test + "$", "-count=1", "-timeout=30s", "-v"]
            started = datetime.datetime.now(datetime.timezone.utc).isoformat()
            result = subprocess.run(command, cwd=root, env=env, text=True,
                                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=60)
            log = output / (name + ".log")
            log.write_text(result.stdout)
            receipt = dict(mutation=name, command=command, started_utc=started,
                           finished_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                           rc=result.returncode, target_assertion=marker,
                           target_red=result.returncode == 1 and marker in result.stdout,
                           source_sha256=hashlib.sha256(source.read_bytes()).hexdigest(),
                           mutant_sha256=hashlib.sha256(replacement.encode()).hexdigest(),
                           log=log.name, log_sha256=hashlib.sha256(log.read_bytes()).hexdigest())
            results.append(receipt)
            print(json.dumps(receipt), flush=True)
    (output / "mutations.json").write_text(json.dumps(results, indent=2) + "\n")
    if not all(row["target_red"] for row in results):
        raise SystemExit("S008变异未在目标断言红")


if __name__ == "__main__":
    main()
