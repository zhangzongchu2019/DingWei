#!/bin/bash
E=/tmp/r62m-followup
mkdir -p /tmp/r62mh/codex /tmp/r62mh/claude /tmp/r62mh/config /tmp/r62mh/state /tmp/r62mh/data /tmp/r62mh/cache
base_env() {
  env -i HOME=/tmp/r62mh TMPDIR=/tmp/r62m-tmp \
    PATH=/opt/dingwei/sessionhelper-venv/bin:/usr/local/bin:/usr/bin:/bin \
    LANG=C.UTF-8 USER=is3 LOGNAME=is3 \
    GOPATH=/home/is3/go GOMODCACHE=/home/is3/go/pkg/mod GOCACHE=/home/is3/.cache/go-build \
    GOPROXY=off GOFLAGS=-mod=mod \
    NODE_PATH=/home/is3/pwrun/node_modules PLAYWRIGHT_BROWSERS_PATH=/home/is3/.cache/ms-playwright \
    CODEX_HOME=/tmp/r62mh/codex CLAUDE_CONFIG_DIR=/tmp/r62mh/claude \
    XDG_CONFIG_HOME=/tmp/r62mh/config XDG_STATE_HOME=/tmp/r62mh/state \
    XDG_DATA_HOME=/tmp/r62mh/data XDG_CACHE_HOME=/tmp/r62mh/cache \
    SH_DRIVER=codex HELPER_TEST_DRIVER=codex SH_PROVISION_NO_EXIT=1 \
    SH_MCP_IPC_SOCK=/tmp/r62mh/ipc.sock DINGWEI_MCP_IPC_SOCK=/tmp/r62mh/ipc.sock \
    "$@"
}
date -u +%FT%TZ > $E/G3c.start
for anchor in r62 main; do
  if [ "$anchor" = r62 ]; then W=/tmp/wt-r62-mobile; else W=/tmp/wt-cli-survey; fi
  cd "$W" || exit 9
  git rev-parse HEAD > $E/G3c-$anchor.anchor
  base_env PYTHONPATH="$W/tools/sessionhelper:$W/tools/sessionhelper/tests:$W/view-service" \
    python3 -m unittest -v test_guide_staging.GuideRealAdapterBoundaryTest.test_claude_stream_json_user_frame_boundary_counts_zero_one_two \
    > $E/G3c-helper-single-$anchor.log 2>&1
  echo $? > $E/G3c-helper-single-$anchor.rc
done
cd /tmp/wt-r62-mobile || exit 9
W=/tmp/wt-r62-mobile
export PYTHONPATH="$W/tools/sessionhelper:$W/view-service"
export TMPDIR=/tmp/r62m-tmp
export NODE_PATH=/home/is3/pwrun/node_modules
export PLAYWRIGHT_BROWSERS_PATH=/home/is3/.cache/ms-playwright
export PATH=/opt/dingwei/sessionhelper-venv/bin:/usr/local/bin:/usr/bin:/bin
{
sed -n "/^PYTHON_TESTS='/,/^'/p" scripts/run-build-tests.sh | sed "1d;\$d" | while IFS= read -r tf; do
  [ -n "$tf" ] || continue
  echo "TEST python $tf"
  python3 -m unittest -q "$tf" || echo "PYTHON_TEST_FAILED $tf"
done
sed -n "/^SHELL_TESTS='/,/^'/p" scripts/run-build-tests.sh | sed "1d;\$d" | while IFS= read -r tf; do
  [ -n "$tf" ] || continue
  echo "TEST shell $tf"
  bash "$tf" || echo "SHELL_TEST_FAILED $tf"
done
echo PYTHON_SHELL_SEGMENT_DONE
} > $E/G3b-python-shell.log 2>&1
echo $? > $E/G3b-python-shell.rc
date -u +%FT%TZ > $E/G3c.end
echo done > $E/G3c.done
