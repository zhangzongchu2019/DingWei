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
