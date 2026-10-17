# S-CLAUDE-BUILD-ENTRY — author delivery

Repository `git@git.code.tencent.com:wego-ai/dingwei.git`; branch `review/coder/claude-build-entry`. Baseline r26 `f8ac0a50ddce41197850b445a37dbc006c7aaa08` / tree `489e1793abb03042f758590c2fd43d89aa81fa5f`.

Frozen product source `cb5d9d3a0e5eb81e94abd0e996fe5f0fd520f7b1` / tree `aeaa978d8b583d04701b2ab5aad2209caf212dbf`. Source-to-delivery changes are evidence only; final delivery commit/tree are sent separately after commit. This is implementation/local testing awaiting opus50ts independent review, not image/runtime or deployment acceptance.

## Implemented behavior

The Claude wrapper now has a real, explicitly gated offline build/inspect entry instead of unconditional exit2. No download, push, source selection fallback or combo activation is added.

Required inputs retain the Git/hash-bound reviewed packager interface, with mandatory independently inventoried Engine ID and explicit local image tag added. Socket validation is metadata-only, requires an existing Unix socket, rejects the default socket and lexical/symlink aliases, and uses a single resolved endpoint for all Docker argv. Both supplied and resolved paths reject whitespace/control characters and URI escape/query/fragment delimiters: shell trailing-newline trimming or URL interpretation must not select another endpoint.

Ordering deliberately preserves Claude's stricter pre-Docker input contract: variables/socket metadata → actual approved packager → readonly Docker info exact-ID comparison → offline build → image inspect. This differs from H3's info-before-generation ordering only by preparing the verified offline context before info. Invalid material never invokes Docker (including info). Wrong identity may leave that verified context for inspection, but never runs build/inspect and never removes existing inputs. Info must be nonempty/exact; failures are not retried.

The packager child uses `python3 -B` to avoid writing import bytecode into a source checkout with no ignore rules. This is operational child-process behavior, not a workaround in the outer test/canonical environment. The success fixture asserts the synthetic source has no pycache and remains Git-clean. The approved packager itself and its validators/locks/map are unchanged.

Build argv: explicit canonical `--host`, `build --platform=linux/amd64 --network=none --pull=false --build-arg SOURCE_COMMIT=<exact source> --tag <local tag> <verified context>`. Then `image inspect --format '{{.Id}}' <same tag>`. Only sha256 plus 64 lowercase hex digits is emitted as the local immutable image ID; failed/empty/malformed inspection is nonzero. This ID is not a registry manifest digest; push and digest registration remain separately authorized.

All actual Docker invocations in these tests use a fake executable which only reads the generated BUILD-INPUTS and logs argv. The Unix socket fixture is bound but never listens/connects to a daemon. Default-socket-exists testing substitutes only filesystem stat metadata in the exact embedded guard program; it does not bind or connect the default socket. Fixture image IDs are synthetic, not built artifacts.

## Preserved scope

Product delta from r26 is exactly three Claude paths: build-image.sh, PACKAGER-CONTRACT.md, test_claude_start.py. SOURCE.json binds 20 source/reference files and unchanged helper/internal/cmd/view-service/scripts trees. All seven R3 ClaudeStart methods remain AST-identical. All preexisting ClaudePackager methods except the intentionally replaced always-refuse wrapper test remain AST-identical; eight new tests replace that one test, for 28 total (21→28). The original polluted-context negative is retained and broadened to actual shell-path lock/Claude/native/Node/guard/dirt negatives. Prior input pollution/type/FD/composition tests are preserved. No Go/helper/runtime/lock/map/Dockerfile change; no version bump is needed for a build-wrapper-only change. `verified=false` remains exact.

## Final exact local gates (cb5d9d3 only)

| Gate | Reading |
| --- | --- |
| Direct Claude fixture | 28 tests, rc0, 14.129s |
| H3 unchanged regression | 38 tests, rc0, 2.324s |
| Entrypoint contract | 11 tests, rc0, 0.073s |
| Shell syntax | sh -n, rc0 |
| Original ./build.sh --test-only | rc0, 102.749s, BUILD_TESTS_OK python=49 shell=2 |
| Disposable PG claim parent + new/reuse/lost_ack/takeover/cancel_then_new | all PASS; no PG skip |

Canonical log SHA256 `a422a3a6e39cac1a2d0ee458e4be29860d6ff990cacfbfd18625e59c6f3e0817`. No outer -B/PYTHONDONTWRITEBYTECODE/PYTHONPYCACHEPREFIX, no PG suppression/wrapper, no external DSN. Existing cached tools and private HOME/TMP/GOCACHE isolate the run. Canonical includes its existing synthetic actual helper/MCP and disposable localhost PG fixtures, not any live seat. An earlier intermediate e484 run is not substituted for these final exact readings.

## Withdrawals: unchanged assertions, actual source changes

Each row runs fixed rc0 → withdrawal assertion-rc1 → restored rc0. One targeted method runs per phase; zero-test/error-only red is rejected by the harness. CONTROLS.json binds source, argv, product/log SHA and outcome; run-controls.py preserves the exact mutation and restores bytes in finally.

1. Restore unconditional post-prepare refusal: valid info/build/inspect sequence test turns red.
2. Remove exact Engine-ID guard: mismatched/empty/multiline identities incorrectly build; negative turns red.
3. Remove default-alias guard: existing-default metadata model turns red.
4. Remove socket type check: regular-file endpoint negative turns red.
5. Remove --network=none: exact offline build argv assertion turns red.
6. Skip actual image inspect: exact three-command/order assertion turns red.
7. Remove child -B: no-ignore clean synthetic source is polluted; valid-entry assertion turns red.
8. Remove inspected-ID validation: malformed/empty-ID negatives turn red.
9. Remove URI ambiguity rejection: raw/resolved ambiguous socket negatives turn red.

Additional green tests cover all required variables, missing/regular/non-unix endpoints, accepted isolated symlink normalization, artifact hash/lock/dirt refusal before ANY fake Docker call, failed info/build/inspect with no retry or false success, and exact local image-ID output. These are inert local fixtures, not real Engine/CLI/image qualification.

## Cleanup and execution boundary

CLEANUP.json records both initial/final private homes and temporary roots, restored detached mutation checkout, private Go cache and process scans. Cache219523938 bytes→go clean232 bytes→removed; both HOME148398185-byte copies, both TMP569765-byte roots and mutation worktree32610829 bytes were removed. Owned commandline/cwd scan and postmaster.pid residue are zero. Main generated bytecode was removed after the gate; tracked author source is clean. Shared module and PG binary caches were not removed. Raw scripts/logs remain; historical harness paths refer to disposed fixtures, so use the repository's self-contained unittest methods to rerun, rather than blindly invoking the archived harness.

No actual Docker/client/daemon/Engine, image or release build, target host, real credentials/Agent, registry push or deployment action occurred. The earlier R2 branch remains frozen. Owner build/Engine/host approval, actual image/venv/UID/CA/Claude/MCP acceptance and combo verification still require their own gates. Four-seat freeze remains unchanged.
