#!/bin/bash
E=/tmp/r62mf-evidence
W=/tmp/wt-r62-mobile
SRC=403c7f9f5db02985fb981512b9b538f623c6b802
mkdir -p "$E" /tmp/r62mf-tmp
date -u +%FT%TZ > "$E/RUNNER.start"
cd "$W" || exit 9
git rev-parse HEAD > "$E/RUNNER.source"
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
  python3 tools/run-browser-regressions.py --receipt "$E/runner-full.receipt" > "$E/runner-full.log" 2>&1
echo $? > "$E/runner-full.rc"
date -u +%FT%TZ > "$E/RUNNER.end"
echo done > "$E/RUNNER.done"
