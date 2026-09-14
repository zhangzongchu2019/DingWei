# DRAFT · T-087 View Service release order

Status: **draft for `zzc-opus50ts-2642`; not an execution order and not an
authorization**. Production execution is forbidden until every blank approval
field below is completed and the real-container precheck passes.

## Immutable coordinates

- Target: production container `dingwei-gray`, runtime root `/app/vs`.
- Artifact source commit: `d6a4d07a3083b43e0b2e122f7652b11727dc96ab`.
- Artifact reachable ref: `fe/view-merge-20260913`.
- Deployment tool commit: `e737bca272b9c208e94d8bc32b99fd26eda278b6`.
- Deployment tool ref: `fix/t109-view-service-deploy-coder56solzzc-20260914`.
- Locally rebuilt package `MANIFEST.sha256` digest:
  `3bce0c539480e8f9dc22823256997a58dd1ac3675b1e858a219f9fe9bd61750d`.
- Coordinator application coordinate: **[TO BE FILLED BY RELEASE OWNER]**.
- Owner item-specific authorization coordinate: **[TO BE FILLED BY RELEASE OWNER]**.
- Executor assigned by coordinator: **[TO BE FILLED BY RELEASE OWNER]**.

## Complete three-file delta

| Runtime file | Pre-image at `336432fc` | Forward image at `d6a4d07a` | Action |
|---|---|---|---|
| `renderer-canary-sse.html` | blob `859349a3f328bd63b5dd0cbe2fa141b08d4e148c`; SHA-256 `24f8b406848622f51bcbf945ee4c01de305ca4ced43157424383aa4c7aa9ae46` | blob `b2ad64beecf6966888921a2316a56c6ceecc0c9b`; SHA-256 `11743b883cd224d8194b5ceebd863593041b85cc3079ec8b8131cd4780a9419e` | replace |
| `view_service.py` | blob `e0fe99fc71bf1700cd462bf741ead4dc8c0bb752`; SHA-256 `d3b9a35c6efe8b9d6fb25e29dc6ed496df8de2032ce398064446c803f9d947c5` | same blob and SHA-256 | no-op |
| `view_page_store.py` | blob `c73f03e5deada818a14b7db483e7f11b979fe133`; SHA-256 `5cc8e2b6ce98fc057b3583ff5f8bd429ceb1e3450304606408996a5b5c89778c` | same blob and SHA-256 | no-op |

Expected shape: renderer-only hot update, `py_changed=0`, no cache removal,
no `supervisorctl restart`, and final output `restart=0`. This removes restart
as a simultaneous variable; it does not determine the cause of the 2026-09-13
incident.

## Mandatory precheck — zero writes

From a clean checkout of the deployment-tool commit, rebuild the package from
the artifact commit. The package location is execution-local and must be
recorded by the executor.

```sh
./deploy/view-service-release.sh package \
  d6a4d07a3083b43e0b2e122f7652b11727dc96ab "$PACKAGE"
sha256sum "$PACKAGE/MANIFEST.sha256"
./deploy/view-service-release.sh install "$PACKAGE" \
  --expected-commit d6a4d07a3083b43e0b2e122f7652b11727dc96ab \
  --container dingwei-gray --backup-root /data/backups
```

Required result: package digest equals the value above; all three `FILE` rows
are printed; only renderer differs; final line is `PRECHECK_ONLY writes=0`.
Any mismatch stops this order. Do not add `--go` during precheck.

- Real-container precheck evidence/result: **[REQUIRED BEFORE ISSUE]**.

## Pre/post observations for T-087

Capture the same three observations immediately before and after application,
using container UTC timestamps and the same query commands on both sides:

| Observation | Before | After |
|---|---|---|
| Feishu WebSocket lifecycle sequence | **[REQUIRED]** | **[REQUIRED]** |
| ingress `event_received` / `normalized` / `message_enqueued` counts | **[REQUIRED]** | **[REQUIRED]** |
| application/container UTC time | **[REQUIRED]** | **[REQUIRED]** |

These observations distinguish whether an event reached the application. They
do not by themselves exclude a silent half-open connection or a Feishu-side
failure to send.

## Authorized execution

Only after the approval and precheck blanks are complete:

```sh
./deploy/view-service-release.sh install "$PACKAGE" \
  --expected-commit d6a4d07a3083b43e0b2e122f7652b11727dc96ab \
  --container dingwei-gray --backup-root /data/backups --go
```

Required output: `CACHE_BACKUP state=present|absent`, three-file verification,
and `INSTALLED ... restart=0`. The generated backup directory must retain both
the pre-image (`24f8b406…` renderer plus the two unchanged Python files) and the
forward image (`11743b88…` renderer plus those Python files), including each
pre-image uid/gid/mode.

## Post-application provenance and rollback gate

Do not consult deployment records for identification. Snapshot the running
files from the container, then identify their content against Git:

```sh
SNAPSHOT="$(mktemp -d)"
docker cp dingwei-gray:/app/vs/renderer-canary-sse.html "$SNAPSHOT/"
docker cp dingwei-gray:/app/vs/view_service.py "$SNAPSHOT/"
docker cp dingwei-gray:/app/vs/view_page_store.py "$SNAPSHOT/"
./deploy/view-service-release.sh identify-online "$SNAPSHOT" --flat
```

Observed before issuing this draft, using the package above and an all-ref scan:

```text
MATCH commit=d6a4d07a3083b43e0b2e122f7652b11727dc96ab
MATCH commit=5b86e429fe7f90cc2dbea64eefc76feddb4d39fa
AMBIGUOUS matches=2
```

The three runtime files are byte-identical at these two adjacent commits, so
content-only inspection cannot distinguish them. `PROVENANCE.json` must not be
used to manufacture uniqueness. `NOT_IN_REPO` always fails. This release order
remains a draft until the reviewer explicitly decides whether the exact
two-commit content-equivalence class is an acceptable result for this
renderer-only release; otherwise T-109's current exact-commit identification
gate is unsatisfiable for this artifact. In either case, require all three
forward SHA-256 values from the table, not a renderer-only sample.

Rollback uses the generated pre-image backup and must restore all three files
with their recorded uid/gid/mode. Required evidence is
`RESTORED_DIGESTS ok files=3`; `ROLLBACK_FAIL` or `ROLLBACK_INCOMPLETE` requires
manual incident handling and must not be reported as a successful rollback.

## Result

- Package path and package digest: **[REQUIRED]**.
- Generated pre/forward backup directory: **[REQUIRED]**.
- Post-application three-file SHA-256: **[REQUIRED]**.
- Content-only identification result: **[REQUIRED]**.
- Before/after observation result: **[REQUIRED]**.
- Final status (`PASS` / `FAIL` / `ROLLED_BACK` / `INCONCLUSIVE`): **[REQUIRED]**.
