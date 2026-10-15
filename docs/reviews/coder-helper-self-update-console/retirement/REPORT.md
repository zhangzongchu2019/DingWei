# Public provision retirement — author checkpoint

Source: `16f86731346667b33bd94f2bc2d839021788a957`, tree `78b9b0828cac2a9103e0c595eac6170bbd680d56`, direct parent r12 `6c4600478a4c7c769cb6adaa1024e4504208f39b`. This is the retirement subset of S-HELPER-SELF-UPDATE-DW123, not the Console UI or complete self-update feature.

## Behavior

Anonymous `/dl`, root and descendants, returns404 for every method without reading/deleting disk artifacts. The authenticated legacy provision form rejects multipart before any body read and returns410 `public_upload_retired`; `package_self` is also retired. Upload and online packaging controls/code are removed. Existing skill/MCP URL delivery validation and ACK behavior remain. Historical documents are marked or migrated; old reports are not rewritten as current evidence. No old external links were probed.

Legacy admin URL-only `update_self` is unchanged in this subset pending the core catalog/job boundary; the new Console must select registered catalog entries only. No catalog API or Console update interface is invented here. Actual core integration, catalog authorization, lifecycle and real-container gates remain OPEN.

## Author verification

- `go test ./internal/admin ./cmd/workpulse -run 'TestAdmin|TestProvisionDownload' -count=1`: rc0. Exact final-source targeted rerun in `targeted.log.txt`.
- `scripts/run-build-tests.sh`: rc0, `BUILD_TESTS_OK python=42 shell=2`; isolated HOME/short TMPDIR, locked websockets15.0.1/pexpect4.9.0 venv, existing Go caches reused. Full command/environment use no production credentials.
- `DW_SPEC_E2E_PYTHON=<locked-venv>/bin/python go test -race ./internal/admin ./cmd/workpulse -count=1 -timeout12m`: rc1. Workpulse passed (207.903s); admin failed (167.307s) ONLY `TestGateDoorABytesMatchThePinnedBaseline`: removal of the public upload/package controls changes the legacy provision-page bytes. No data race was reported. The pin explicitly requires leader disposition before changing it; original pin remains unchanged and failure was reported (13fb2de768c8). This is not an all-green full-package claim.
- `git diff --check 6c460047..16f86731`: rc0.
- Four surgical isolated Go overlays: download serving, upload-before-read predicate, package_self predicate, upload UI; each rc1 at the intended behavioral assertion. The runner and raw logs are included. A first download overlay draft had an import compile failure; retained separately and NOT counted as a valid red.

No deployment, package upload, real Agent operation or production access. Tests use temporary files and synthetic authentication. Evidence is author execution, not independent review.

## Exact parent output attribution

The unmodified dump fixture was run on candidate source and with only `internal/admin/admin.go` replaced by its exact r12 blob through a Go overlay; both dumper invocations rc0. Both retain20records. Only the authenticated `GET /admin/provision` record differs; all19others are byte-identical. Parent dump hashes to the existing baseline pin. Raw redacted fixture dumps and unified diff are included; no real credentials are used. The pin is not silently changed.
