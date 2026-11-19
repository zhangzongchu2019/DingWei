#!/bin/bash
E=/tmp/r62mf-evidence
W=/tmp/wt-r62-mobile
mkdir -p "$E" /tmp/r62mf-tmp
date -u +%FT%TZ > "$E/M1.start"
cd "$W" || exit 9
git rev-parse HEAD > "$E/M1.source"
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
for i in 1 2 3; do
  run_env node view-service/console-mobile-regression.js > "$E/m1-baseline-$i.log" 2>&1
  echo $? > "$E/m1-baseline-$i.rc"
done
for k in mobile-no-drawer mobile-no-drawer-2 mobile-tabs-wrap mobile-input-wide mobile-no-viewport mobile-session-small-targets; do
  knife=${k%-2}
  run_env node view-service/console-mobile-regression.js --mutate="$knife" > "$E/m1-knife-$k.log" 2>&1
  echo $? > "$E/m1-knife-$k.rc"
done
date -u +%FT%TZ > "$E/M1.end"
echo done > "$E/M1.done"
