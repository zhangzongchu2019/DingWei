#!/bin/bash
E=/tmp/r62m-followup
H=/tmp/r62mh-fresh
mkdir -p $H/codex $H/claude $H/config $H/state $H/data $H/cache
date -u +%FT%TZ > $E/G3d.start
for anchor in r62 main; do
  if [ "$anchor" = r62 ]; then W=/tmp/wt-r62-mobile; else W=/tmp/wt-cli-survey; fi
  cd "$W" || exit 9
  git rev-parse HEAD > $E/G3d-$anchor.anchor
  env -i HOME=$H TMPDIR=/tmp/r62m-tmp \
    PATH=/opt/dingwei/sessionhelper-venv/bin:/usr/local/bin:/usr/bin:/bin \
    LANG=C.UTF-8 USER=is3 LOGNAME=is3 \
    GOPATH=/home/is3/go GOMODCACHE=/home/is3/go/pkg/mod GOCACHE=/home/is3/.cache/go-build \
    GOPROXY=off GOFLAGS=-mod=mod \
    CODEX_HOME=$H/codex CLAUDE_CONFIG_DIR=$H/claude \
    XDG_CONFIG_HOME=$H/config XDG_STATE_HOME=$H/state XDG_DATA_HOME=$H/data XDG_CACHE_HOME=$H/cache \
    SH_DRIVER=codex HELPER_TEST_DRIVER=codex SH_PROVISION_NO_EXIT=1 \
    SH_MCP_IPC_SOCK=$H/ipc.sock DINGWEI_MCP_IPC_SOCK=$H/ipc.sock \
    PYTHONPATH="$W/tools/sessionhelper:$W/tools/sessionhelper/tests:$W/view-service" \
    python3 -m unittest -v test_guide_staging.GuideRealAdapterBoundaryTest.test_claude_stream_json_user_frame_boundary_counts_zero_one_two \
    > $E/G3d-helper-single-$anchor.log 2>&1
  echo $? > $E/G3d-helper-single-$anchor.rc
done
date -u +%FT%TZ > $E/G3d.end
echo done > $E/G3d.done
