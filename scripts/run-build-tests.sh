#!/bin/sh
# Canonical Python/shell test list shared by build.sh, make test, and CI.
set -eu

ROOT="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# Socket-backed tests need room for their own names under the Unix path limit.
# Keep the caller's value for diagnostics, but run the suite under a short root.
RBT_CALLER_TMPDIR="${TMPDIR-}"
export RBT_CALLER_TMPDIR
if ! RBT_EFFECTIVE_TMPDIR="$(mktemp -d /tmp/rbt.XXXXXX 2>/dev/null)"; then
  echo "ENVIRONMENT_UNAVAILABLE: cannot create short TMPDIR under /tmp" >&2
  exit 2
fi
if [ "${#RBT_EFFECTIVE_TMPDIR}" -gt 40 ]; then
  rbt_tmpdir_length=${#RBT_EFFECTIVE_TMPDIR}
  rmdir "$RBT_EFFECTIVE_TMPDIR" 2>/dev/null || true
  echo "ENVIRONMENT_UNAVAILABLE: short TMPDIR path is ${rbt_tmpdir_length} bytes; maximum is 40" >&2
  exit 2
fi
TMPDIR="$RBT_EFFECTIVE_TMPDIR"
export TMPDIR
printf 'TMPDIR caller=%s effective=%s\n' "${#RBT_CALLER_TMPDIR}" "$TMPDIR"
cleanup_rbt_tmpdir() {
  rm -rf -- "$RBT_EFFECTIVE_TMPDIR"
}
trap cleanup_rbt_tmpdir EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# 质量闸先于测试执行；set -e 保留每个闸的真实失败退出码。
for gate in repo-boundary-gate dependency-ratchet tenant-isolation-gate; do
  echo "GATE $gate"
  sh "scripts/$gate.sh"
done

# The view-service tests import sibling modules as top-level Python modules.
# Keep the source directory explicit so callers do not depend on their cwd.
PYTHONPATH="$ROOT/tools/sessionhelper:$ROOT/view-service${PYTHONPATH:+:$PYTHONPATH}"
export PYTHONPATH

# The helper gate validates its complete inventory before importing any tests.
python3 scripts/run-helper-build-tests.py

PYTHON_TESTS='
deploy/image/supervisord/test_verify_supervisor_template.py
deploy/ops/hosted-image/test_start.py
deploy/ops/hosted-image/test_build_definition.py
deploy/ops/hosted-claude-image/test_claude_start.py
deploy/test_build_entrypoints.py
deploy/test_file_service_deploy.py
deploy/test_generate_console_seed.py
deploy/test_invariants_alert_route.py
deploy/test_invariants_expected_hashes.py
deploy/test_preview_console.py
deploy/test_validate_console_seed.py
view-service/test_check_public_view_deploy.py
view-service/test_md_render_core.py
view-service/test_provenance_flag_injection.py
view-service/test_renderer_iter2.py
view-service/test_view_page_store.py
view-service/test_view_service.py
view-service/test_view_error_logging.py
view-service/test_health_auth.py
view-service/test_agent_disk_resource.py
view-service/test_inbox_lag_resource.py
view-service/test_view_refresh.py
view-service/test_mux_projection.py
view-service/test_mux_projection_failure.py
view-service/test_cost_paused.py
view-service/test_mux_cost_seed.py
view-service/test_view_unlock_v2_integration.py
'
SHELL_TESTS='
deploy/test-view-service-release.sh
scripts/test-desktop-hub-roundtrip.sh
'

printf '%s\n' "$PYTHON_TESTS" | while IFS= read -r test_file; do
  [ -n "$test_file" ] || continue
  echo "TEST python $test_file"
  python3 -m unittest -q "$test_file"
done

printf '%s\n' "$SHELL_TESTS" | while IFS= read -r test_file; do
  [ -n "$test_file" ] || continue
  echo "TEST shell $test_file"
  bash "$test_file"
done
echo 'BUILD_TESTS_OK helper_profiles=2 python=27 shell=2'
