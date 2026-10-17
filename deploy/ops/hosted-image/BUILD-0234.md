# Hosted 0.2.3.4 image definition — no image built

H3 builds on H1's opt-in text-only mode. This is a candidate build definition and local fixture validation, not a container/image/runtime PASS or permission to build/deploy. H2's backup rule and H4's nodeagent release build are independent branches. Product runtime helper bytes must match the exact accepted H1 source before an image is released.

The future authorized builder uses a clean exact Git checkout containing this definition, with a fixed Linux/amd64 CPython3.12 base digest. `prepare_context.py` verifies SOURCE_COMMIT==HEAD, the unchanged four-pin runtime lock SHA, helper version0.2.3.4/new-layout package manifest, each wheel's name/version/SHA, exact runtime/security deb sets and hashes, and the Codex native archive's pinned SHA512/file count. Native traversal, duplicate names, links and special nodes are rejected. No network download, helper execution, Docker call, installed state, account or environment-file copy occurs during preparation. Output is a new private absolute context; only allowlisted Git source/validated binary inputs are copied. BUILD-INPUTS records source commit/tree, lock SHA and all context member hashes. Source exchange stays Git-only; the approved build context is never uploaded as arbitrary source storage.

`build-image.sh` requires explicit SOURCE_COMMIT, a new absolute HOSTED_BUILD_CONTEXT, an approved isolated unix HOSTED_BUILD_ENGINE and a local HOSTED_IMAGE_TAG. The default host socket and its resolved/symlink aliases are refused before context preparation. Supply `--repo`, `--wheelhouse`, `--native-tar`, `--runtime-debs`, `--security-debs` paths for approved local inputs. Fixed commit/output flags occur after caller args to prevent override. It builds locally and prints only image ID, without registry login/push. Any build/import/registry publication requires a separate owner grant.

Docker RUN operations are network=none. Venv install is no-index/require-hashes/only-binary from the wheelhouse, pip check occurs before removal of venv pip, then isolated metadata verification requires exactly the lock's four distributions. The final same-absolute-path venv contains runtime pins only. Runtime and security .deb files are offline, replacing the historical apt-get security fetch. The base image must already be approved and cached on the isolated builder; --pull=false plus --network=none is NOT proof that the daemon/BuildKit base resolver cannot contact a registry. The sandbox must fence builder egress and prove the pinned base resolves offline. Base availability, CPython ABI/minor, dpkg dependencies, CA trust, native execution, SBOM, installed contents and reproducibility remain untested until an authorized image build.

Final UID/GID10001, HOME=/work, no credentials in layers, root helper launcher/factory under /opt/dingwei/sessionhelper-src and absolute interpreter /opt/dingwei/sessionhelper-venv/bin/python. hosted-start preserves existing credential ownership/atomic auth+marker behavior and API-key wrapper, forces helper text-only IPC (legacy upload-only off), writes the matching restricted MCP stanza with explicit root/state/socket and never points at factory. Helper and shim thus use the same confirmed-slot selection. Nodeagent supplies driver/model/route env; the starter preserves SH_MODE. `/usr/local/bin/codex` remains the login-container override entry, and regular hosted-start execs the absolute venv interpreter. Init and **ReadonlyRootfs are enforced by nodeagent buildSpec**, not by a Dockerfile promise; test a real container with nodeagent's no-capabilities/read-only-rootfs/8MiB tmpfs,/work bind contract before pilot. The starter tests replace UID/IPC path locally and do not claim real UID10001/container compatibility.

This starter intentionally rewrites model/trust/MCP config on every image boot, consistent with its existing rewrite contract; it does not preserve user-added MCP entries. Auth renewal/marker ordering is retained. If the actual Codex app-server ignores the generated MCP config, source/ABI differs, tmpfs is too small, CA/proxy traffic fails, or nodeagent lacks supervision, STOP. No helper self-update lifecycle or container RestartPolicy has been added. Actual Codex model/MCP invocation, LLM account login and one-seat Hub/Agent acceptance need separately approved runtime tests and owner credentials. Nothing here authorizes those operations.


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
