# Authorized narrow admin byte-pin rebaseline

Source/test commit `04a170c2a9691b235df0ca7edb1447f2f7ca8afe`, tree `53dcb731b9e722d56c35abcd8b553e8485172432`; parent `e6e374f766dd7c964713280e0d60d5c9f35bcb9f`. This commit changes only `internal/admin/gate_baseline_pin_test.go`; no runtime change.

Leader explicitly permitted a narrow rebaseline on2026-10-15 after initial report13fb2de768c8. Old SHA256 `ea1766cb001d2cc60f0235d3b3b4725f4f18776847b1102af88651785f4356cd`; new `4c7b45af8b8b0f4f950a1eae23ddda12175a5ae4daf0d67110910e87a456f859`. The commit message records both hashes, ruling, existing20-record coverage and treatment of the report-first instruction: preserve the instruction and append this specific approval to its constant comment.

## Parent/candidate output scope

The exact r12 `admin.go` versus source16f86731 outputs and unified diff were committed BEFORE the rebaseline under `../retirement/doorA-parent.txt`, `doorA-current.txt`, `doorA.diff.txt` (e6e374f7). Both dumper invocations rc0;20records retained,19byte-identical. The only changed record is authenticated `GET /admin/provision`: removed file-upload/online-package controls and multipart attribute, related retirement notice and existing-URL send wording. No other page, status, header, authentication or action diff appears. The initial full-package rc1 log remains intact under `../retirement/affected-full-race.log.txt`.

## Author tests

- `go test -race ./internal/admin -run '^TestGateDoorABytesMatchThePinnedBaseline$' -count=1`: rc0/7.375s. The unchanged dumper runs3times and checks all20records.
- Isolated Go overlay restores the actual upload/package controls while retaining new pin: the SAME byte-pin test returns expected rc1 at its byte mismatch assertion. `withdraw.py` reproduces it; raw output included.
- `go test -race ./internal/admin -count=1 -timeout12m`: rc0/152.224s on exact04a170c2. Raw final package output included. The original pre-approvalrc1 is retained, not overwritten.
- Previous exact runtime workpulse fullrace passed and canonical gate was42+2zero; runtime blobs are unchanged by this test-only commit. No redundant build/workpulse rerun claimed.

Author evidence only; independent reviewer verdict pending. No deployment/upload/realAgent operation. Console single/batch/status remains a separate incomplete peripheral task awaiting coreHTTP contract.

The additional `verify-output-scope.py` rechecks the original dump hashes,20records/19unchanged, and exact two authorized page replacements. It returns0; this supplements the raw unified diff rather than replacing reviewer line inspection.
