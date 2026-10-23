# S-NODEAGENT-TMPFS-BLOCKS — test-only review

Base: r37 `29e63f9963dedcc32010ca89265d78ba12664bf2` (tree `65cf4df2ea3d928aff263dc03c63dbaff81175e6`). Branch: `review/coder/nodeagent-tmpfs-blocks`. Only executable change: `internal/nodeagent/softquota_login_test.go`, in `TestSoftLoginMeasurementOwnershipAndRestart`; no product accounting change.

The test used an empty login directory as evidence of allocated usage. On the tested tmpfs its directory metadata occupies zero st_blocks; on ext4 it occupies eight. Write an 18 KiB regular payload inside the actual login writer directory and obtain its own st_blocks and size. Require a nonzero allocated fixture and measured login allocated/apparent usage at least as large as that file's actual values. Do not assume either directory's block allocation, skip tmpfs, fake the scanner, or reduce the assertion to mere existence. Existing wrong-owner stop prevention, correct-owner enforcement, held-data preservation and restart fencing assertions remain intact.

Validation: Go 1.22.2, Linux amd64, uid 1001. Test TMPDIR values are `/dev/shm/coder-tb` (tmpfs) and `/data/ai-dev-data/tmp-coder-tb` (ext4). Both use `GOTMPDIR=/data/ai-dev-data/tmp-coder-go` on ext4 solely for executable Go build artifacts because /dev/shm may be noexec; t.TempDir fixtures still follow the distinct TMPDIR values. No production Docker/Hub or credential access; Engine/Hub doubles only. Test runs used `-count=1`, without cache reuse of test results.

| Check | tmpfs | ext4 |
|---|---|---|
| Original r37 target test | FAIL, original login metadata measurement absent | PASS |
| Fixed target test | PASS | PASS |
| Full `./internal/nodeagent` package | PASS (4.428s) | PASS (5.917s) |
| Withdraw fix via original test source overlay | FAIL, original assertion | PASS (same control as original ext4 row) |

Target command: `TMPDIR=<path> GOTMPDIR=/data/ai-dev-data/tmp-coder-go go test ./internal/nodeagent -run '^TestSoftLoginMeasurementOwnershipAndRestart$' -count=1 -v`. Package command removes `-run` and `-v`. For withdrawal, Go `-overlay /data/ai-dev-data/nodeagent-tmpfs-blocks-evidence/withdraw-overlay.json` substitutes only the target test file with byte-exact `git show 29e63f99:internal/nodeagent/softquota_login_test.go`; candidate worktree remains fixed throughout. Original baseline tmpfs was run before the edit; original ext4 was run with the same overlay after the edit. Raw logs adjacent to this report; filesystem-proof.json records an independent empty-directory/payload metadata probe. Source-containing original/overlay files remain local, never uploaded to a file service.

`gofmt` applied; `git diff --check` clean. No full repository/build/release gate claimed, no deployment. Await independent review.
