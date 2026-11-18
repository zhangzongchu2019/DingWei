#!/bin/bash
E=/tmp/r62m-z3
W=/tmp/wt-r62-mobile
SRC=95e4808435ca03aa8361d5644fdcb4d3556a9dcf
mkdir -p "$E" /tmp/r62mz3-tmp
date -u +%FT%TZ > "$E/ALL.start"
cd "$W" || exit 9
git rev-parse HEAD > "$E/ALL.source"
for i in 1 2 3 4 5; do
  date -u +%FT%TZ > "$E/r$i.start"
  env -i HOME=/home/is3 TMPDIR=/tmp/r62mz3-tmp \
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
    go test ./internal/m8 -count=1 -p 1 -timeout 90m -v > "$E/r$i.log" 2>&1
  echo $? > "$E/r$i.rc"
  date -u +%FT%TZ > "$E/r$i.end"
done
date -u +%FT%TZ > "$E/ALL.end"
echo done > "$E/ALL.done"
