# H3 hosted image definition, no build

Base product r19 `8a3daeaf`, plus H1 product e4edb706, single-environment-entry repair f4c1e9f (equivalent cherry-pick65619ab), and H1 framing/UTF-8 fix8e0b9e37 (equivalent cherry-pick on this branch). Ref `review/coder/hosted-image-0234`. The helper runtime subtree is the H1 0.2.3.4 candidate; H3 only changes hosted image definition, preparation/build/start scripts, tests and documentation. H1's independent acceptance remains required before release.

Offline candidate definition includes fixed base digest/Linux amd64, exact Git source-kit/new layout, original four-pin hash lock, same-final-path venv, hash-checked wheel/deb/Codex inputs, UID10001 and absolute launch/MCP config. Existing credential atomic-write/renewal/marker/ownership behavior is retained; no source or venv image is built here. Nodeagent must still enforce read-only-rootfs/init/no-capabilities/private-work constraints; a Dockerfile cannot enforce the future caller's HostConfig. Driver mode remains node-injected. Restricted permissions are forced in helper and shim; legacy upload-only is off, without opening full IPC. Future build is local-only, explicit isolated socket/no default Engine and no registry publication.

32 local Python fixtures rc0: original20 starter assertions retained with the intentional permission-mode expectation updated, plus12 new input/assembly/config/security tests. Native/wheel/deb fixtures are synthetic inert data, not product artifacts. Assembly orchestration deliberately mocks Git/source-kit/native/deb providers; actual input validators are independently exercised. Tests validate actual prepare() output TOML, matching helper/shim state/root/socket, protected auth preservation, malformed inputs and fail-closed defaults. Source-kit packaging itself is exercised by H1's existing packaging tests; H3 does not claim a real context generation using complete production artifacts.

Two source-only mutations rc1: removing restricted flag from emitted MCP config; removing require-hashes from the Dockerfile definition. The latter is a definition contract check, NOT a failed real build. Raw outputs/edits/SHA are retained. No test expectations were changed for mutants. Python definition imports and shell syntax checks pass; no Docker daemon, image build/import/push, pip installation, helper execution, real credential/PG/host/Agent or job activity. No dependency installations.

Untested gates: cached base identity/actual Python ABI and wheels/platform compatibility; offline BuildKit base resolution/egress; dpkg install/CA and native execution; real UID10001/read-only/tmpfs compatibility; actual Codex MCP discovery/model/login; cgroup/XFS/network and complete Hub enrollment. No unattended helper self-update or container RestartPolicy is supplied. See deploy/ops/hosted-image/BUILD-0234.md. Build and pilot remain separately authorized.

Follow-up hardening: build-image.sh rejects symlink/resolved aliases of the default Docker socket before context preparation; the fixture proves rejection without connecting to Docker. H1 and H3 helper runtime subtree hashes are recorded in PROVENANCE.json and must remain identical. raw/start.log and raw/definition.log retain earlier-stage checks; raw/combined.log is the final32-case result.


## R1/R2 follow-up: factory-only hosted state

This candidate supports factory-only startup, not unattended slot migration. Before
reading LLM credentials or creating login/config files, hosted-start rejects a
symlinked/non-directory state root or update path, and ANY nonempty update directory:
current/pending/previous journals (even null/malformed), slots, links and special files.
Absent/empty update directory passes. Journal/slot contents are never read/imported,
removed or rewritten. Existing history requires separately reviewed stopped migration;
do not delete it to force startup. Even a stated034 slot is refused because version
text alone cannot establish trusted restricted-mode source identity.
No concurrent state writer or later slot introduction is supported. This is a startup
precondition, not a sandbox against malicious same-UID writers; future updater and
migration require authenticated source-policy and lifecycle review.

Default Engine rejection resolves paths and refuses /var/run/docker.sock,
/run/docker.sock and symlink aliases before preparing context. This accidental-default
fence cannot establish that any other socket belongs to an approved isolated builder;
the operator must independently verify that identity.

Final successor:34 fixture tests rc0, original20 authentication/start tests retained.
Two added actual source withdrawals rc1: remove state gate, remove socket alias block.
Assertions unchanged across withdrawals; exact diagnostic ensures a generic missing-env
failure cannot substitute for the state refusal. Raw logs and hashes are in rework/.
The first local state-withdrawal attempt exposed that test weakness (missing env also
returned2); the assertion was strengthened before recording final controls. Earlier
32-case and original two definition controls remain historical evidence.
No Docker/pip/helper execution, context/image build, host, key, PG or real Agent action.


### R2 approved isolated Engine preflight supplement

Before context preparation, the resolved socket must exist and be a Unix socket;
missing or regular-file endpoints STOP. HOSTED_BUILD_ENGINE_ID is mandatory, obtained
from independently approved isolated-builder inventory (never approve the default host
Engine or learn-and-accept an arbitrary endpoint in the same command). A read-only
`docker --host "$HOSTED_BUILD_ENGINE" info --format '{{.ID}}'` must exactly match that ID
before preparation/build/inspect. Missing ID, daemon failure or mismatch STOP.
These variables encode an external owner/leader approval prerequisite, not automatic
permission or proof of isolation. Operator must verify isolation/ownership/access and
fence concurrent endpoint/daemon replacement; socket metadata and ID checks are not
an adversarial TOCTOU/authentication boundary. No automatic fallback or resend.
This read-only Engine call occurs only in a separately authorized future build run.

35 local fixture tests pass; no real Docker invocation: preflight uses a text-only
substitute and a locally bound inert Unix socket (no listen/connect). Missing/regular
socket and absent/mismatched approved ID stop before context/build. Two further source
withdrawals (socket metadata gate; identity preflight) turn red with unchanged tests.
No actual Engine, image build, pip/helper execution or host operation is validated.


## H1 UTF-8 identity synchronization

Synchronizes exact ipc_listener.py and its regression test from H1 successor
38750fe51b1d084eff4baff317c7672f192be096. Helper runtime subtree is now
0bfc7b1f898971849922b865112f4ae0cc97df2c on both branches. H1 independently remains
a prerequisite. Its12 targeted tests and one actual source withdrawal apply to this
same listener source; H3 did not execute helper. Earlier35 H3 fixture results apply
to unchanged image/start/Engine definitions, not a new full runtime acceptance.


## Lock-only normal-restart correction (supersedes the initial empty-only rule)

Normal pristine factory startup calls State.select/locked and leaves update/state.lock.
The hosted startup gate now accepts absent/empty update state OR exactly one state.lock
with regular-file type, seat UID10001 owner, mode0600, size0 and linkcount1. It opens
state/update directories with O_DIRECTORY|O_NOFOLLOW, lists names via that fd, and
stats the sole lock relative to that fd without following links. No lock/journal
contents are read. A foreign owner, symlink, FIFO, directory, nonempty or nonprivate
lock, hardlink, extra journal/slot/entry all STOP with HELPER_UPDATE_STATE. No clearing,
migration or version-string compatibility inference. Concurrent writers remain fenced;
this is not a malicious same-UID mutation sandbox or authorization for self-update.

36 inert fixtures rc0, including prepare/auth-preservation/config success on simulated
second boot with a normal lock left behind; lock-only check repeated without mutation;
all old selected-journal negatives retained. No helper/guard execution. Two controls
turn red with unchanged tests: restore overstrict empty-only gate; remove lock metadata
validation. Exact hashes/raw logs in lock-only/. Earlier d19 REWORK is retained as
historical; this successor needs separate independent review.


## Complete canonical wiring correction

Leader's isolated integration found test_build_definition.py absent from the canonical
runner although discovery found it. This H3 defect is repaired: wire it immediately
after test_start.py, update deploy/view discovery23→24 with a reason comment, and
canonical Python47→48 counts/assertions/final marker; shell remains2. build.sh and
H4 nodeagent release changes are untouched. H4's same-file edits must preserve the
24 discovery and48 Python assertions when integrated.

Full `sh ./build.sh --test-only` rc0,179.775s,48Python+2shell. No exclusion/wrapper:
temporary-PG parent and all5subcases PASS; real local Hub/desktop roundtrip PASS.
Private HOME/shortTMPDIR; test fixture processes/roots absent afterward, owned Go
cache removed. No release/image build or external/production PG/host operation.
10 build-entrypoint contract tests also green. Remove only the newly wired path or
restore discovery23: unchanged contract test becomes red. Source hashes, full raw
log, withdrawals, result and cleanup in canonical-wiring/. This supplements earlier
35/36 fixture evidence and is the authoritative complete canonical for this wiring
successor, not an inference of independent review or deployment authorization.
