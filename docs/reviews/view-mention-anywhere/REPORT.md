# S-VIEW-MENTION-ANYWHERE — author evidence

Base: integration/spec5ff-r10, 89278a47d1a27465f1b284bafcc065ce87cfa7fa.

The popup and selected-insert checks now share mentionPrefixAllowed: start of text or a preceding character outside ASCII A-Z/a-z/0-9/dot. Chinese text, punctuation and parentheses can precede a mention. Name characters still block email/mid-word triggers. The existing trailing-name-character boundary is unchanged. Only popup-picked records supply structured targets; typed text never adds a target. No Hub/helper/routing/authorization change.

Product file: view-service/renderer-canary-sse.html. Existing two mention browser suites have their mutation anchors updated to the actual predicate; no existing assertion is removed. The route suite adds selected-full-name send cases for Chinese, parentheses, Chinese full stop and legacy whitespace, name-character negative cases (a/Z/0/dot), and Chinese-adjacent typed-text NOFALLBACK.

Raw logs are stored as deterministic .log.gz files to preserve exact output bytes (including original trailing whitespace) without introducing textual diff-check errors. Uncompressed originals remain in the private evidence directory; manifest records both compressed and original SHA256.

## Commands and results

Browser environment: NODE_PATH=/data/ai-dev-data/a1-enroll-browser-private/node_modules PW_CHANNEL=chrome. Actual system Chrome with local HTTP fixtures only; no real Agent forwarding, production browser, or production access.

- node view-service/renderer-mention-candidates-regression.js: 22/22, rc0.
- node view-service/renderer-mention-route-regression.js: 15/15, rc0 (final source rerun also rc0).
- node view-service/renderer-mention-reply-regression.js: 7/7, rc0.
- MUTATION=old-prefix node view-service/renderer-mention-route-regression.js: rc1; exactly the Chinese/parenthesis/full-stop cases fail waiting for popup selection. Actual source is changed only in the fixture's private served copy.
- MUTATION=old-insert-prefix with same route command: rc1; exactly those three cases fail structured-target equality after real selection, proving send-time recognition matters independently.
- MUTATION=trigger-mid-word node view-service/renderer-mention-candidates-regression.js: rc1 at email guard; mutation anchor valid.
- node internal/m8/testdata/renderer_integrity_recovery.cjs: 22, rc0.
- node internal/m8/testdata/renderer_history_priority.cjs: 7, rc0.
- node internal/m8/testdata/managed_recovery_result.cjs: 3, rc0.
- node tests/a4-overflow-preserve.cjs: 4, rc0.
- node tests/a4-notice-tone.cjs: rc0.
- node tests/a4-epoch-notice.cjs: rc0.
- node tests/a5-replay-state.cjs: 8 plus syntax, rc0.
- node tests/a5-real-gap-timer.cjs: 9, rc0.
- node tests/a6-agent-disk-real.cjs: 27, rc0.
- node tests/a6-agent-disk-unknown.cjs: 8 plus syntax, rc0.
- cd view-service && node renderer-t086-deferred-clear-regression.js: 8, rc0. The first invocation from repository root failed ENOENT due to this script's cwd contract; its original log/rc1 are retained, not counted as PASS.
- node tests/file-card-streaming.cjs: 10, rc0.
- node view-service/renderer-markdown-image-regression.js: 15, rc0.
- node view-service/renderer-markdown-injection-regression.js: 11, rc0; limited to that suite's specific payload shapes, not a general security claim.
- PATH=/data/ai-dev-data/helper-deps-lock-venv/bin:$PATH sh scripts/run-build-tests.sh: rc0, BUILD_TESTS_OK python=42 shell=2.
- git diff --check: rc0.

No separate full Go/helper rerun was needed for this renderer-only change; the canonical gate's own Go/loopback checks ran normally. No merge, deploy or restart. Independent review pending togpt60lunazzc.
