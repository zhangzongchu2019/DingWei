# Reproduce

Fetch the original branch and tag from wego-ai/dingwei via Git; use frozen test source a94070b18e1f1fd64817536390777f74f43c7177 in a private detached checkout.

1. `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/store ./internal/m8 -run 'TestClarification(FeishuRejectsBoundNonPMMembers|FeishuOriginRequiresPMAtAppendBoundary)$' -count=1 -v`.
2. Repeat with `-race`.
3. Unset HUB_TEST_PG_DSN; set HUB_TEST_PG_STRICT=1 HUB_TEST_PG_SKIP_OK=0 HUB_TEST_PG_EMBEDDED=1 and private0700 TMPDIR. Run `go test ./internal/store ./internal/m8 ./cmd/workpulse -json -count=1 -timeout=10m`; retain stdout/stderr/rc separately.
4. `go vet ./internal/store ./internal/m8 ./cmd/workpulse`.
5. `python3 tests/spec-clarification/bound_nonpm_mutation.py --commit a94070b18e1f1fd64817536390777f74f43c7177 --output <private evidence directory>`. Uses a disposable local worktree, original/withdrawn/restored phases and exact behavioral markers. It does not touch the main checkout's product file.
6. `python3 docs/reviews/coder-spec-clarification-bound-nonpm-20261021/VERIFY.py`.

No network dependency installation or live provider/Hub/seat access is needed. Embedded PG/module caches must be pre-provisioned. The test-only layer is intentionally distinct from the real-bound-identity ingress layer to avoid false confidence from redundant earlier refusals.
