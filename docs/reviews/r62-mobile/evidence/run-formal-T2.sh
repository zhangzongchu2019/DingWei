#!/bin/bash
E=/tmp/r62m-formal
W=/tmp/wt-r62-mobile
SRC=3700c995c20bcf93c458ea10f6c550f501854520
mkdir -p "$E" /tmp/r62-tmp
date -u +%FT%TZ > "$E/ALL.start"
cd "$W" || exit 9
git rev-parse HEAD > "$E/ALL.source"
run_env() {
  env -i HOME=/home/is3 TMPDIR=/tmp/r62mf-tmp \
    PATH=/opt/dingwei/sessionhelper-venv/bin:/usr/local/bin:/usr/bin:/bin \
    LANG=C.UTF-8 USER=is3 LOGNAME=is3 \
    GOPATH=/home/is3/go GOMODCACHE=/home/is3/go/pkg/mod GOCACHE=/home/is3/.cache/go-build \
    GOPROXY=off GOFLAGS=-mod=mod \
    HUB_TEST_PG_STRICT=1 \
    DW_DESKTOP_INTEGRATION_PYTHON=/opt/dingwei/sessionhelper-venv/bin/python3 \
    OPENSPEC_TELEMETRY=0 \
    NODE_PATH=/home/is3/pwrun/node_modules \
    PLAYWRIGHT_BROWSERS_PATH=/home/is3/.cache/ms-playwright \
    SOURCE_COMMIT=$SRC \
    "$@"
}
date -u +%FT%TZ > "$E/G1.start"
run_env go test ./internal/m8 -count=1 -p 1 -timeout 90m -v > "$E/G1-m8.log" 2>&1
echo $? > "$E/G1-m8.rc"
date -u +%FT%TZ > "$E/G1.end"
date -u +%FT%TZ > "$E/G2.start"
run_env python3 tools/run-browser-regressions.py --receipt "$E/G2-browser.receipt" > "$E/G2-browser.log" 2>&1
echo $? > "$E/G2-browser.rc"
date -u +%FT%TZ > "$E/G2.end"
date -u +%FT%TZ > "$E/G3.start"
run_env sh scripts/run-build-tests.sh > "$E/G3-build-tests.log" 2>&1
echo $? > "$E/G3-build-tests.rc"
date -u +%FT%TZ > "$E/G3.end"
date -u +%FT%TZ > "$E/ALL.end"
echo done > "$E/ALL.done"
