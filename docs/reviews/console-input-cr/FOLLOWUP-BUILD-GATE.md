# Console input CR: r45 build-gate followup

Parent: c93a7cb18567d7991f18265a9116442238a4dad8. Branch: opus50tc2k/console-input-cr. This followup changes one stale test expectation; production code is unchanged.

## Behavior and helper compatibility

Not appending CR is the intended complete-message behavior, as specified in REPORT.md paragraphs 2–3 and implemented by c93a7cb1. The reliable envelope body is message content, not a raw PTY keystroke sequence. The preexisting assertion at view-service/test_view_page_store.py:409 expected an obsolete injected CR and now expects exactly @Beta hi, preserving the complete viewer/token/text/mentions object and receipt assertions.

The earlier supplementary conclusion “旧 helper 均自发回车” is cited here with its concrete source basis: tools/sessionhelper/sessionhelper/cli.py:460–481, CLIAdapter.inject_text / submit_enter. The same code exists at the pre-fix parent 330a67d1dd47e6891741828f9ed8ab2597a83c20: PTY writes prepared text, waits, then independently sends CR; tmux writes literal text then independently sends Enter. Neither path depends on a CR suffix in the envelope. CLIAdapter.handle_delivery routes transcript, opencode_db and ordinary CLI delivery through inject_text (cli.py:350–408); transcript confirmation retry also calls submit_enter at :617. Protocol DriverAdapter._bounded_deliver calls driver.deliver(command.delivery_id, command.text), rather than the raw terminal line buffer (driver_adapter.py:419–434). Raw terminal write_terminal_input remains separate and unchanged.

This followup rechecked those existing code paths and replayed actual CLIAdapter.inject_text with mocked output transports for PTY and tmux: clean payload plus independent submit, 2 PASS, no actual child/tmux invocation. Evidence: raw/helper-submit-followup.log. The prior shorthand describes these supported paths; it is not a claim that every historical deployed helper binary was audited.

## Other CR expectations

Command: rg -n -F '\r' view-service --glob '*test*.py'. Before the fix there were exactly two literal matches: the obsolete :409 payload expectation and test_view_service.py:165. The latter is the intentional Last-Event-ID CRLF header-injection rejection case, not an input submission expectation, so it stays. After the fix only the header-injection case remains. No other view-service test expects a CR suffix. Existing c93 normalization and mention tests remain intact.

## Validation

Full canonical gate command: PATH=/tmp/r31-build-check-venv/bin:$PATH scripts/run-build-tests.sh; complete output: raw/build-gate-followup.log. Related page command: TMPDIR=/tmp/ctclose-go go test ./internal/m8 -run 'View|Terminal|Console|Mux|Asset|Invariant|Page' -count=1; complete output: raw/m8-pages-followup.log.

Git diff --check passes. No deployment, restart, shared database operation, provider call or GitHub push.

Final result: scripts/run-build-tests.sh rc0; BUILD_TESTS_OK helper_profiles=2 python=27 shell=2. Unittest total **2476**, failures **0**: helper Claude 1087/1087, helper Codex 1087/1087, other Python 302 cases across 27 files. Both shell scripts pass (0 failures); their embedded Go scenarios are recorded in the full log and are not counted as Python unittest cases. Related m8 View/Terminal/Console/Mux/Asset/Invariant/Page selection rc0, 34.730s. Helper PTY/tmux replay 2 PASS, 0 failures. Machine-readable counts and raw-log hashes: FOLLOWUP-RESULT.json.
