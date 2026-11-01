# S-CONSOLE-INPUT-TRAILING-CR

Base: 330a67d1dd47e6891741828f9ed8ab2597a83c20. Branch: opus50tc2k/console-input-cr. This is a complete HTTP-message input fix; no raw terminal WebSocket/PTY key behavior, deployment, restart, version or dependency change.

The renderer already stripped trailing whitespace, so changing the textarea alone would not fix the observed Hub body: legacy View Service appended a CR submit key, and Hub V2/mention routing called terminalSubmission, which also appended CR. These complete messages use ordinary reliable envelopes rather than terminal_input frames, so the submit key became visible message text.

The renderer now normalizes CRLF/bare CR to LF, removes boundary blank lines (retaining its existing trailing whitespace cleanup), and refuses whitespace-only sends. View Service normalizes complete input and refuses whitespace before either forwarding mode, and no longer appends the legacy CR. Hub V2 and mention envelope bodies use the same normalization (CRLF/bare CR to LF, trim boundary newlines); the legacy HTTP input handler normalizes and rejects blank input before calling the existing routing function. The raw terminal WebSocket entry and routeTerminalInput byte semantics are unchanged. Interior blank lines and indentation survive. Authentication, principal/source metadata, raw signed-body replay hashes, request IDs and mention receipt behavior are retained.

Validation:

- New actual-renderer Chrome regression: tail CR, Windows multiline with interior blank line, indentation and click/Enter whitespace rejection PASS. raw/browser.log.
- Existing mention route browser: 15 PASS, 0 FAIL. raw/browser-mentions.log.
- Renderer normalization removed only in served fixture: rc1 (hello plus LF differs from hello). raw/mutation-browser.log.
- Hub normalizer mutated to append CR: rc1, five normalization cases red; source restored in finally and focused input/mention/real-service tests pass. raw/mutation-backend.log and raw/m8-restored.log.
- Real Python View Service -> Hub -> Agent WebSocket in both legacy/V2 modes: exact received body contains normalized middle LF and no trailing CR; whitespace input rejected. Direct signed V2 requests also reject blank text; direct legacy HTTP and mention coverage are included in the final View/Terminal run.
- Relevant Python suites (test_view_service, test_view_unlock_v2_integration, test_invariants_expected_hashes): 47 tests PASS. raw/python.log.
- Broad m8 View/Terminal suite: rc0 (22.959s), raw/m8-view-terminal-final.log. Initial expansion found one remaining old assertion requiring owner input plus CR; updated it to require the exact clean body while retaining principal/source checks. Initial failure preserved in raw/m8-view-terminal.log; no assertion was loosened to ignore whitespace.
- git diff --check: rc0. No existing renderer/view-service source SHA pin matched in deploy/scripts/internal/view-service.

Commands: browser scripts use NODE_PATH=/tmp/coder-a3-playwright/node_modules (the existing mention script also PW_CHANNEL=chrome); Go uses TMPDIR=/tmp/ctclose-go go test ./internal/m8 -run 'View|Terminal' -count=1; Python uses PYTHONPATH=view-service /tmp/r31-build-check-venv/bin/python3 -m unittest -q view-service/test_view_service.py view-service/test_view_unlock_v2_integration.py deploy/test_invariants_expected_hashes.py. This task uses the focused suites above; a full run-build-tests or helper suite is not claimed.

All tests are isolated loopback fixtures. Delivery is GongFeng only during the migration transition; no live Agent message, shared/staging/production operation or GitHub push.
