# S-R62-CLI-A-FIX

- Executor: `togpt60lunazzc.zzc`
- Base: `main` `206d41b26dab646c0e50a111eed5a45ccf7c72f6`
- Initial implementation source: `0525ee8c9ee000a1deda7b58b9b99e6eb04a816f`; follow-up paired-rollout/OpenCode test source: `29ecf0e74` (full commit is recorded in branch history).
- Scope: Codex CLI transcript parsing and timeout ACK correctness; TUI stop diagnosis only. No live seat, Hub, or deployment changes.
- Survey evidence: `review/qwen38mvb4/native-cli-view-survey-20261118` tip `425e32db`, especially `REPORT.md` §§3.2, 3.5 and `logs/t1-codex-0159` / `logs/t2-codex-0170`.

## 1. Codex transcript shape and implementation

For the CLI mode support contract, Codex CLI 0.159 and later are supported; the rollout pairing was directly verified on 0.159.2 and 0.170.0. Versions older than 0.159 are outside the supported range: they may emit both legacy `event_msg` / `agent_message` and `response_item` assistant records for the same message, which can produce duplicate mirror text. The parser retains the legacy branch for compatibility, but that does not make older mixed-shape rollouts supported.

The survey's actual Codex 0.159.2 rollout contains both an `event_msg` / `item_completed` `AgentMessage` and the corresponding `response_item` whose payload is `{type: message, role: assistant}`. Both carry the same message ID and text. The helper now parses the assistant `response_item` with `output_text`/`text` content, keeping the existing `event_msg`/`agent_message` and user-message branches. It intentionally does not parse `item_completed` too, because doing so would yield the same answer twice from the observed transcript.

The regression fixture `tools/sessionhelper/tests/fixtures/codex-response-item-assistant-real.jsonl` is copied byte-for-byte from the survey's T1 rollout assistant `response_item` row; it is not a hand-authored approximation. The fixture test asserts the extracted answer. A second test models a confirmed user turn followed by no answer: `CLIAnswerTimeout` is raised with `reason=cli_answer_timeout`, `retryable=false`; `delivery_failure_ack_fields` therefore emits a `failed` ACK, rather than returning the timeout placeholder and allowing a `processed` ACK. The failure is non-retryable because the user turn was confirmed in the CLI transcript; retrying could inject the same user request twice. The persistent worker records local state `failed`, and Hub v1 handles that ACK as a terminal failed delivery. Existing `agent_message` parsing remains covered.

### ACK reader / effect map

- `tools/sessionhelper/sessionhelper/app.py:130` maps helper exceptions to ACK reason/retryability; `app.py:1514-1558` receives the durable-worker exception, persists `DeliveryInbox.finish(..., error)`, and emits `delivery_ack state=failed` with those fields.
- The v1 wire ACK's sole product consumer is Hub `internal/m8/hub.go:4733-4864` (`handleSessionDeliveryAck`): retryable failures enter bounded retry/backoff; non-retryable failures set `dead` and publish a failed delivery update. It does not interpret a timeout placeholder specially.
- Hub v2 processing is separate (`internal/m8/delivery_ack_v2.go`) and is not advertised by CLI mode. View sees Hub-published delivery updates, not the helper ACK frame directly. No other production code reads this helper ACK wire format.

## 2. TUI diagnosis and limit

Survey evidence shows the helper reached its Codex readiness prompt, accepted the input, confirmed the user turn in the rollout, and continued relaying PTY output (T1: 252 frames / 20,045 characters; T2: 129 frames / 18,022 characters). The Codex rollout itself records assistant completion (`item_completed` and assistant `response_item`) while the visible TUI remained on the working spinner. This rules out a failure to start the child, match its ready pattern, inject the prompt, or relay all terminal output as the cause of the observed stuck state. The strongest supported diagnosis is a Codex TUI rendering/completion incompatibility with the tested Qwen-compatible provider stream; it is not yet an isolated provider root cause.

This checkout cannot complete the requested one-shot direct-TUI control: its installed Codex is 0.157.1, not survey versions 0.159.2 or 0.170.0; neither surveyed binary is present here, and the isolated environment has no Qwen provider credential. I did not substitute a different version/provider or access a live seat. Therefore the direct-outside-helper comparison remains unperformed. To fully discriminate provider stream compatibility from helper PTY rendering, run the exact surveyed Codex binary and same isolated provider configuration once directly in a clean HOME/CODEX_HOME and compare the TUI terminal completion with the rollout `task_complete`; do not reuse or export shell snapshots from CODEX_HOME.

## 3. Verification

- Targeted parser/timeout and end-to-end synchronous relay tests: 4 tests, OK.
- Mutation: removed the new `response_item` assistant branch. The integrated CLIAdapter regression failed at the business assertion `真实 assistant 消息被漏读，回合被错误降为答案超时占位`, rc 1; no import or compile error. The original implementation was restored before the full-suite runs.
- Full helper suite: `scripts/test-helper-suite.py --driver all`, launched with `env -i`, locked Python 3.12.3 venv, isolated HOME/TMP, and the runner's per-profile temporary IPC sockets. It runs Claude and Codex profiles serially. All six profile runs passed:

  | Round | Claude | Codex | Result |
  | --- | ---: | ---: | --- |
  | 1 | 1263 tests, 493.167s | 1263 tests, 490.909s | rc 0 |
  | 2 | 1263 tests, 493.094s | 1264 tests, 491.905s | rc 0 |
  | 3 | 1264 tests, 491.549s | 1264 tests, 492.354s | rc 0 |

  The regression was added during round 1, so the extra test appears in later profile invocations as the runner advanced. Raw console logs are retained locally at `/tmp/r62-cli-a-canonical-round{1,2,3}.log`.
- Direct TUI control: not run, for the version/credential limitation above.

## 4. Change inventory

Expected changed paths are limited to `tools/sessionhelper/sessionhelper/cli.py`, `tools/sessionhelper/tests/test_sessionhelper.py`, the Codex transcript fixtures, and this report/evidence. No Hub or production files outside the helper adapter are modified.

## 5. Follow-up regression checks

- The 0.159.2 and 0.170.0 survey rollouts both contain an `event_msg` `item_completed` with an `AgentMessage` item and a `response_item` assistant row sharing the same message ID. The helper parser intentionally consumes only the assistant `response_item` for this observed shape; `item_completed` is not parsed as text. The legacy `event_msg` / `agent_message` shape remains supported. New paired fixtures were extracted byte-for-byte from the survey's two real rollout records, and the test asserts exactly one parsed assistant text event for each version. This rules out duplicate mirror text for the observed pair without guessing from text equality.
- Added OpenCode regressions through `CLIAdapter.handle_delivery`: successful synchronous delivery continues through its SQLite message/part reply reader, returns the collected reply, and emits one OpenCode mirror event. For no-answer, the already-admitted turn raises `CLIAnswerTimeout` (`reason=cli_answer_timeout`, `retryable=false`) and produces a nonretryable failed ACK; it does not return a timeout placeholder as a processed answer. Bypassing the OpenCode database branch fails the success business assertion.
- The OpenCode `opencode_db` branch changes all three former placeholder returns: `no_user` now raises `CLIInjectionNotReceived` (injection not confirmed), while `db_missing` and `no_answer` raise `CLIAnswerTimeout` (the path cannot produce a confirmed synchronous reply). The latter remain nonretryable because the input may already have been submitted; replay could duplicate the user turn. The no-answer regression fails if that raise is changed back to its former placeholder return.
- Four focused tests covering the paired rollout records, OpenCode routing, the Codex assistant fixture, and timeout-as-failed ACK passed. Raw log: `evidence/focused-c1-c3.log`. The OpenCode route mutation was killed at `OpenCode must keep using its SQLite message/part reader`; raw log: `evidence/mutation-opencode-db-route.log`.
- Mutation removing the new Codex assistant `response_item` branch fails at the existing synchronous-reply business assertion `真实 assistant 消息被漏读，回合被错误降为答案超时占位`, not at import/compile; log `evidence/mutation-codex-response-item.log`. A separate mutation that parses `item_completed` AgentMessage text as well produces two mirror events for both real version-paired fixtures; the C3 regression fails on the duplicate list (2 instead of 1), log `evidence/mutation-codex-duplicate-event.log`.
- Two additional complete helper-suite rounds ran on the final test source with `env -i`, locked Python 3.12.3, separate HOME/TMP, explicit `DW_DESKTOP_INTEGRATION_PYTHON`, and the runner's isolated profile IPC sockets. Round 4: Claude 1266 tests / 495.874s, Codex 1266 / 494.190s, rc=0. Round 5: Claude 1266 / 494.747s, Codex 1266 / 495.123s, rc=0. Raw logs are `evidence/helper-round4.log` and `evidence/helper-round5.log`.
- C2 follow-up focused run: four targeted tests passed, including OpenCode success and no-answer, Codex response-item answer recovery, and Codex timeout-as-failed ACK. The OpenCode no-answer test asserts `CLIAnswerTimeout`, `reason=cli_answer_timeout`, `retryable=false`, and failed ACK fields. Its raw log is `evidence/c2-opencode-no-answer-focused.log`.
- C2 mutation changed only the OpenCode `no_answer` raise back to its prior timeout-placeholder return. The new test failed at `AssertionError: CLIAnswerTimeout not raised` (rc 1), a contract assertion rather than a compile error; raw log: `evidence/c2-opencode-placeholder-mutation.log`.
