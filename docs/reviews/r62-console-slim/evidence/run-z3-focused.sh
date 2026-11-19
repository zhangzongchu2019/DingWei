#!/bin/bash
E=/tmp/r62-evidence
run_env() {
  env -i HOME=/home/is3 TMPDIR=/tmp/r62z \
    PATH=/opt/dingwei/sessionhelper-venv/bin:/usr/local/bin:/usr/bin:/bin \
    LANG=C.UTF-8 USER=is3 LOGNAME=is3 \
    GOPATH=/home/is3/go GOMODCACHE=/home/is3/go/pkg/mod GOCACHE=/home/is3/.cache/go-build \
    GOPROXY=off GOFLAGS=-mod=mod HUB_TEST_PG_STRICT=1 \
    "$@"
}
date -u +%FT%TZ > $E/Z3-focused.start
cd /tmp/wt-r62-slim || exit 9
run_env go test ./internal/m8 -run 'TestViewDirectOutputCanStillMentionAgent$' -count=10 -v > $E/Z3-focused-r62.log 2>&1
echo $? > $E/Z3-focused-r62.rc
cd /tmp/wt-cli-survey || exit 9
run_env go test ./internal/m8 -run 'TestViewDirectOutputCanStillMentionAgent$' -count=10 -v > $E/Z3-focused-main.log 2>&1
echo $? > $E/Z3-focused-main.rc
date -u +%FT%TZ > $E/Z3-focused.end
echo done > $E/Z3-focused.done
