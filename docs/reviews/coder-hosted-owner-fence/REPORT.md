# S-HOSTED-OWNER-FENCE — implementation delivery for independent review

Author: coder56solzzc.zzc. Reviewer: opus50ts.zzc. Base r27 `349dceda1092ac187e04e2ec15d536ae99d99461` (tree `c8765cbb60dfb95c896f73921eb7a6b291136fe6`). Branch `review/coder/hosted-owner-fence`, repository `git@git.code.tencent.com:wego-ai/dingwei.git`.

Product source first frozen at `03ebc11101718e4cb13ca5095308063dc3c88371`; final source/test pin `c9b32ae7171c336d8069d2697546729c233906f1`, tree `bdf2e704ea75f2d10a3c1cfb7f96b0c418869eb2`. The latter only adjusts the remaining anonymous minimum-plan assertion to the required 403 and corrects documentation evidence attribution. Delivery adds evidence plus a documentation-only clarification of the existing ops authentication prerequisites; executable/test bytes remain the final source pin. See SOURCE.json for all changed executable/test blob hashes and unchanged subtrees.

## Behavior and boundary

`DW_G4_ACCESS_ALLOW_USER_IDS` is parsed once at hosted startup: absent/empty/whitespace denies all humans; malformed or noncanonical IDs reject assembly. It accepts trimmed, comma-separated lowercase 2–32 ASCII alphanumeric canonical user IDs, duplicates harmless. No default owner, role alias, quota or combo inference.

The common human route registrar checks real N3 cookie/session identity before body decoding, Origin/resource checks and business/audit operations. Authorization headers never grant human authority, including an Agent token with an owner cookie. Outside/unresolved sessions receive the same no-store 403 `forbidden` without resource lookup. Existing listed-user ownership/role/Origin/business restrictions remain.

Every registered human hosted endpoint is covered, including six lifecycle operations, quota edit/read, personal/admin accounts, login start/read/cancel, jobs, options/create, hold release/page, and monitor's human branch. The prodops authenticated GET monitor branch is byte-identical; it remains exact-full-name, credential-validated and read-only even when the human allowlist is empty. Node connect/backup/login-upload routes retain their original authenticators. The unchanged login-upload contract for an unknown login is 404, not a new human 403.

The real startup policy supplies a per-request Console feature decision; absent callback or absent hosted assembly fails closed. Fenced pages do not show creation/accounts/quota/lifecycle controls. Generic Agent listing still works but skips the hosted provider and hosted subobject for fenced identities. The API fence is authoritative for stale pages and crafted calls.

Existing sessions resolve current identity state on each request. Revoked/expired/disabled/must-change sessions deny; promotions cannot enlarge the startup list. Newly created and real registration-activated users remain fenced irrespective of default quota. This implements hosted activation exclusion, not a global shutdown of platform registration/activation. List edits require restart; no hot reload, no automatic Agent stop/start or queued-job cancellation. Keep the rollout's separate activation/quota-write freeze. Configuration, restart, rollback and owner acceptance steps are in `docs/runbooks/hosted-owner-access-fence.md`.

No helper runtime, lock, VERSION, nodeagent, hosted core, scheduler, actual deployment configuration, secret or production data was changed.

## Validation

All local loopback/synthetic; no external DSN, Docker/Engine, actual Agent, private login state, release/image build, offer or deployment. `-timeout=45m` used for both requested long packages.

| Check | Result | Exact attribution |
|---|---|---|
| Original `./build.sh --test-only` | rc0; **49 Python + 2 shell**, 107.097s; local PG parent + all 5 claim subcases pass | c9 source, env-i / short TMP; raw/canonical-short.log |
| Full `go test -json -race -count=1 -timeout=45m ./cmd/workpulse` | rc0, **126 top + 38 subcases**, no fail/skip, 235.097s | c9; raw/full-workpulse-short.jsonl |
| Full `go test -json -race -count=1 -timeout=45m ./internal/hosted` | rc0, **172 top + 161 subcases**, no fail/skip, 356.549s | 03eb; hosted and dependency executable/test blobs unchanged through c9/delivery; not relabelled as a later full run |
| Full identity/httpapi race | rc0, package 60.369s | 03eb identical executable/tests in c9 |
| m8 Console/unified auth targeted race | rc0, package 166.047s | 03eb identical executable/tests in c9 |
| New session/route/assembly targeted race | rc0, package 25.133s | 03eb identical access tests in c9 |
| New fullpage denied-identity fixture | **7 checks**, rc0; UI mutation rc1, restored rc0 | actual production renderer, assembled=true/access=false; cookie identity enforcement separately proved by actual wireNewPlatform Go fixtures |

Eight new Go top-level access tests cover actual N3 sessions and real assembly, every human route, all six lifecycle operations, empty/unset policy, malformed policy, outside manager/admin/member, malformed/forged identity headers and cookies, Agent Authorization with owner cookie, promoted/new users, real registration activation, revoked/expired/disabled/must-change identity, startup snapshot behavior, and exact ops read-only exception. Existing positive business tests explicitly inject their original fixture actors into the allowlist; no production default grant was added. Existing anonymous tests in route-contract/joint/minimum-plan now assert the required 403 rather than former 401 or must-change detail; all original quota/FIFO/business assertions remain.

### Withdrawal controls

`raw/withdrawals.json` binds actual mutated source SHA, rc and tests. In detached owned checkout: remove route wrapper → real assembly options leak 200 and original assertion red; remove membership → promotion assertion red; remove monitor allowlist → empty-list human monitor assertion red; remove per-request page callback → nil callback assertion red; remove projection guard → hosted provider-read assertion red. All restore rc0 with tests unchanged.

Removing the Authorization guard in the every-route test trips its deliberate pre-business trap (panic from nil dependencies rather than a clean assertion). It is retained but not misrepresented as a clean assertion control. A separate no-business direct probe `bearer-probe_test.go.fixture` verifies fixed0 → withdrawn1 at the explicit permission assertion → restored0 (`raw/bearer-direct-*`). Its first exploratory fixture wrongly erased the cookie with the header map; that fixture-only failure is retained/excluded, then fixed to delete only Authorization. The browser lifecycle-button guard mutation fails an actual `4 !== 0` DOM assertion; restored 7-check fixture passes.

### Initial failures and limits (retained)

The first requested long runs used a private TMPDIR whose full pathname was too long for Linux Unix-domain sockets. workpulse had 3 bind failures; canonical's helper children timed out waiting for their IPC sockets. The first workpulse run also located the remaining old anonymous 401 assertion, corrected in c9. Original logs are retained, not counted green. With unchanged product code, actual short `/tmp/owf18` and the corrected test expectation, both required runs pass as recorded above. A missing optional timing executable was corrected in the author wrapper before the recorded tests; timings now use monotonic Python recording.

An extra unchanged legacy 32-case browser suite is **30 pass / 2 fail** on both candidate and untouched exact r27. Failures are the pre-existing view URL exact-equality assertion versus its `view_refresh_instance` query and a hidden new-group form fixture. Both raw logs and exact baseline attribution are retained. No fix, test relaxation or full legacy-browser green claim. A preliminary attempt to use that suite's disabled-feature branch additionally hits its later positive-only fixture cases; the dedicated new denied-identity fixture replaces that exploratory approach. The system Node18 invocation cannot run installed Playwright and is retained as environment setup, not a product failure; final browser uses existing Node22.23.1 / Chrome151.0.7922.108 without installation.

This is developer validation awaiting opus50ts independent review, not self-issued review PASS. Actual owner account/exposure, deployed Hub/Engine/notification, restart and rollout acceptance are not exercised. Production remains unchanged and dataconn four-seat freeze remains.

## Reproduction, binding and cleanup

Go1.22.2 linux/amd64; Python3.12.3, locked websockets15.0.1/pexpect4.9.0; Node22.23.1 and installed Chrome151.0.7922.108. The recorded env-i runner uses a task-private HOME/GOCACHE, shared read-only module cache, `GOPROXY=off`, locked Python PATH and genuinely short TMPDIR. No bytecode suppression variable or Python `-B` was used for canonical. `run.sh.fixture` / `record.py.fixture` are exact author recording fixtures with author-local paths; adapt only sandbox paths for independent reruns. They are not executable deployment instructions. Local PG binary distribution was copied to the private HOME; no user home/credentials copied.

`SOURCE.json`, `RESULTS.json` and relative-key `MANIFEST.json` bind source and raw logs. The manifest omits itself. Evidence scripts use non-Go fixture suffixes so docs are not discovered as broken Go packages. Cleanup: no owned test/PG children or postmaster markers; temporary baseline and withdrawal worktrees removed; private cache 1,384,181,014 bytes → go-clean 232 bytes → removed, HOME 83,357,310 and both owned TMP trees removed. See CLEANUP.json. No shared cache was cleaned. The source worktree and committed evidence are retained for review.
