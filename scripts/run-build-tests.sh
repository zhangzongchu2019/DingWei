#!/bin/sh
# Canonical Python/shell test list shared by build.sh, make test, and CI.
set -eu

ROOT="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# The view-service tests import sibling modules as top-level Python modules.
# Keep the source directory explicit so callers do not depend on their cwd.
PYTHONPATH="$ROOT/tools/sessionhelper:$ROOT/view-service${PYTHONPATH:+:$PYTHONPATH}"
export PYTHONPATH

PYTHON_TESTS='
tools/sessionhelper/test_supervise_connection_loops.py
tools/sessionhelper/tests/test_connection_receive_quiescence.py
tools/sessionhelper/tests/test_connection_reconnect_backoff.py
tools/sessionhelper/tests/test_connection_claim_cancellation.py
tools/sessionhelper/tests/test_terminal_start_observation.py
tools/sessionhelper/tests/test_roster_snapshot.py
deploy/image/supervisord/test_verify_supervisor_template.py
deploy/ops/hosted-image/test_start.py
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
view-service/test_view_unlock_v2_integration.py
'
SHELL_TEST='deploy/test-view-service-release.sh'

printf '%s\n' "$PYTHON_TESTS" | while IFS= read -r test_file; do
  [ -n "$test_file" ] || continue
  echo "TEST python $test_file"
  python3 -m unittest -q "$test_file"
done

echo "TEST shell $SHELL_TEST"
bash "$SHELL_TEST"
echo 'BUILD_TESTS_OK python=22 shell=1'
