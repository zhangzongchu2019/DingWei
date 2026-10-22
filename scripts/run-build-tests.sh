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
tools/sessionhelper/tests/test_update_release_core.py
tools/sessionhelper/tests/test_cwd_import.py
tools/sessionhelper/tests/test_boot_origin.py
tools/sessionhelper/tests/test_legacy_mcp_entry.py
tools/sessionhelper/tests/test_sqlite_connection_lifetime.py
tools/sessionhelper/tests/test_codex_stale_thread.py
tools/sessionhelper/tests/test_daemon_identity.py
tools/sessionhelper/tests/test_mcp_shim_transport_errors.py
tools/sessionhelper/tests/test_update_core_review_regressions.py
tools/sessionhelper/tests/test_self_update_offer.py
tools/sessionhelper/tests/test_connection_receive_quiescence.py
tools/sessionhelper/tests/test_connection_reconnect_backoff.py
tools/sessionhelper/tests/test_connection_claim_cancellation.py
tools/sessionhelper/tests/test_terminal_start_observation.py
tools/sessionhelper/tests/test_roster_snapshot.py
tools/sessionhelper/tests/test_agent_disk_metrics.py
tools/sessionhelper/tests/test_setup.py
tools/sessionhelper/tests/test_env_read_single_entry.py
tools/sessionhelper/tests/test_claim_enroll.py
tools/sessionhelper/tests/test_desktop.py
tools/sessionhelper/tests/test_task_flow_r32.py
tools/sessionhelper/tests/test_project_context.py
tools/sessionhelper/tests/test_task_blocker.py
tools/sessionhelper/tests/test_pilot_question.py
tools/sessionhelper/tests/test_spec_clarification.py
tools/sessionhelper/tests/test_pilot_template.py
tools/sessionhelper/tests/test_spec_review_r32.py
tools/sessionhelper/tests/test_desktop_dispatch.py
tools/sessionhelper/tests/test_desktop_endpoint.py
tools/sessionhelper/tests/test_outbound_receipt.py
tools/sessionhelper/tests/test_desktop_host.py
tools/sessionhelper/tests/test_desktop_pilot.py
tools/sessionhelper/tests/test_desktop_wait.py
tools/sessionhelper/tests/test_desktop_stop_hook.py
tools/sessionhelper/tests/test_source_kit.py
tools/sessionhelper/tests/test_macos_build.py
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
view-service/test_agent_disk_resource.py
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
echo 'BUILD_TESTS_OK python=61 shell=2'
