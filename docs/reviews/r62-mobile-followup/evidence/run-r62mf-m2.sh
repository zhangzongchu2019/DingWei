#!/bin/bash
E=/tmp/r62mf-evidence
W=/tmp/wt-r62-mobile
mkdir -p "$E" /tmp/r62mf-tmp
date -u +%FT%TZ > "$E/M2.start"
cd "$W" || exit 9
git rev-parse HEAD > "$E/M2.source"
run_env() {
  env -i HOME=/home/is3 TMPDIR=/tmp/r62mf-tmp \
    PATH=/opt/dingwei/sessionhelper-venv/bin:/usr/local/bin:/usr/bin:/bin \
    LANG=C.UTF-8 USER=is3 LOGNAME=is3 \
    GOPATH=/home/is3/go GOMODCACHE=/home/is3/go/pkg/mod GOCACHE=/home/is3/.cache/go-build \
    GOPROXY=off GOFLAGS=-mod=mod \
    NODE_PATH=/home/is3/pwrun/node_modules \
    PLAYWRIGHT_BROWSERS_PATH=/home/is3/.cache/ms-playwright \
    "$@"
}
for s in console-quota-regression console-platform-admin-regression console-task-board-regression console-panel-pointerevents-regression console-specs-theme-regression; do
  for i in 1 2 3; do
    run_env node "view-service/$s.js" > "$E/m2-standalone-$s-$i.log" 2>&1
    echo $? > "$E/m2-standalone-$s-$i.rc"
  done
done
date -u +%FT%TZ > "$E/M2.end"
echo done > "$E/M2.done"
