# H file-service deployment contract

This file contains coordinates and executable checks, not credentials. Secret
values and the paths that contain them are supplied only by the authorized
production executor.

## MinIO artifact and service

- Server version: `RELEASE.2025-09-07T16-13-09Z.hotfix.28f01c988`.
- Official linux-amd64 artifact:
  `https://dl.min.io/community/server/minio/hotfixes/linux-amd64/archive/minio.RELEASE.2025-09-07T16-13-09Z.hotfix.28f01c988`.
- Official sha256: `86e8afc8409b7b803ce91ea91f7d3551aff93ea63d8d242e64a317e95d4cd9d5`.
- Install the verified binary at `/opt/dingwei/minio/bin/minio`, mode `0755`,
  owned by `root:root`. Run `deploy/minio.service` as `dingwei-minio`.
- Persistent data directory: `/var/lib/dingwei-minio`, owner
  `dingwei-minio:dingwei-minio`, mode `0750`.
- S3 listener: `127.0.0.1:9000`; administrative console:
  `127.0.0.1:9001`. Neither is exposed by public nginx.
- Bucket: `dingwei-files`. An authorized MinIO administrator creates it once.
- Runtime credential categories: MinIO server root identity/password (held by
  the MinIO service and authorized administrator, never by file-service), then
  a dedicated file-service access-key identifier and secret-key credential.
  Values and credential-file locations are not stored in Git.
- Attach `deploy/minio-file-service-policy.json` to the dedicated identity.
  It grants object Put/Get (HEAD/Stat uses GetObject authorization) and Delete
  only under `dingwei-files/*`; it grants no bucket administration, policy,
  identity, console, or server-admin action.

The upstream project now recommends source builds and calls historical binary
releases legacy. This deployment intentionally pins the official hotfix
artifact above rather than using an unversioned `latest`; changing it requires
a new reviewed artifact coordinate and checksum.

## file-service coordinates

- Build source: the release anchor, using `./build.sh <clean-output-dir>`.
  The artifact is `<output-dir>/file-service`; the build output must name the
  exact clean commit and print its sha256.
- Install the verified artifact at
  `/opt/dingwei/file-service/bin/file-service`, `root:root`, mode `0755`.
- Run `deploy/file-service.service` as `dingwei-file`.
- Persistent directory: `/var/lib/dingwei-file-service`, owner
  `dingwei-file:dingwei-file`, mode `0750`.
- Metadata database: `/var/lib/dingwei-file-service/file-service.db`.
- Public listener: `127.0.0.1:8793`; internal issuer/audit listener:
  `127.0.0.1:8794`. Only the two public `/files/` routes in
  `deploy/nginx-dingwei.conf` may be exposed.

The authorized environment supplies these exact non-secret settings:

```text
FS_ADDR=127.0.0.1:8793
FS_INTERNAL_ADDR=127.0.0.1:8794
FS_DATABASE_PATH=/var/lib/dingwei-file-service/file-service.db
FS_MAX_FILE_BYTES=16777216
FS_TOTAL_QUOTA_BYTES=536870912000
FS_RETENTION=1440h
FS_TOMBSTONE_RETENTION=2160h
FS_SWEEP_INTERVAL=1m
FS_CAPABILITY_MAX_AGE=2m
FS_MINIO_ENDPOINT=127.0.0.1:9000
FS_MINIO_BUCKET=dingwei-files
FS_MINIO_USE_TLS=false
```

It also supplies four credential categories without putting values or paths in
Git: MinIO access-key identifier, MinIO secret-key credential, internal issuer
bearer, and internal audit bearer. Missing credentials must prevent startup.

`/readyz` is not MinIO evidence. After both services start, the authorized
executor runs `deploy/file-service-minio-probe.sh`. It obtains a real upload
ticket, writes a unique tiny object through file-service, obtains a retrieval
ticket, reads the bytes back from MinIO through file-service, compares them,
and prints `FILE_SERVICE_MINIO_OK`. The probe is a write action and leaves the
tiny object under normal retention; it is run only inside the deployment
authorization.

## Seed dry-run isolation

Do not start a second WorkPulse process. Run:

```text
<verified-candidate-workpulse> validate-console-seed <candidate-seed.json>
```

The command uses the same Go strict decoder and semantic validation as normal
startup, then exits with `CONSOLE_SEED_VALID`. Its command path is before
bootstrap loading and `run()`: it opens no database, constructs no Hub or
Feishu transport, starts no scheduler, creates no socket, and listens on no
address. Tests inject a sentinel server function and prove it is never called
for either valid or invalid seeds. Therefore no `FEISHU_TRANSPORT` or scheduler
disable flag is part of this safety boundary.

## Helper canary and rollback

- Canary target: `coder56solzzc` only.
- Source: the same release anchor's complete `tools/sessionhelper` package.
- Version source: repository `VERSION` and
  `tools/sessionhelper/sessionhelper/__init__.py`; both must agree and be
  strictly greater than the canary's live reported version.
- Upgrade: package through the Hub provision path, verify artifact digest,
  send `update_self` only to the canary, then require reconnect, the expected
  reported version, and retrieval of a real file before fleet expansion.
- Automatic rollback covers failure to reconnect. Once a version reconnects
  successfully, the monotonic gate rejects a lower/equal version. Semantic
  rollback therefore means repackaging the previous known-good content under
  a strictly higher version and repeating the same one-seat canary.

## Expiry acceptance data

Two valid methods exist; both are write actions and must be listed explicitly
in the authorization.

1. **Recommended: isolated short-retention instance.** Start a temporary
   file-service process with a dedicated temporary metadata DB and acceptance
   bucket, `FS_RETENTION=2s` and `FS_SWEEP_INTERVAL=1s`. Upload one probe object,
   observe it ready, wait at least one sweep after expiry, then prove both the
   object bytes and ready metadata are gone/tombstoned. This writes and deletes
   only disposable acceptance data; it does not modify production metadata.
   Cost: an extra temporary process, bucket and credential, plus cleanup.
2. **Production-row time travel.** Upload a uniquely identified probe through
   the normal API, back up the metadata DB, then update only that row's
   `expires_at` to a past RFC3339Nano timestamp. Wait one sweep and prove the
   MinIO object was deleted and the row became `expired` with `removed_at`.
   This is a direct production database mutation (highest-risk class), bypasses
   the application write path, and requires separate explicit authorization,
   a recorded pre-image and rollback plan. It is faster but not the default.

Quota eviction is not an acceptable substitute: it proves the quota path, not
the 60-day retention path.
