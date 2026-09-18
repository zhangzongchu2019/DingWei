# H file-service deployment contract

This file contains coordinates and executable checks, not credentials. Secret
values and the paths that contain them are supplied only by the authorized
production executor.

## Local object storage

Object bytes are owned directly by file-service under
`/var/lib/dingwei-file-service/objects`; MinIO is not deployed. Paths have the
form `files/YY/MMDD/<id>-<timestamp>` or `files/pinned/<id>-<timestamp>`.
Original names remain metadata only and never participate in a disk path.
Files are atomically committed with explicit mode `0640`; the service unit
fixes `UMask=0077`, so file-service uses `fchmod` after creation. The data root
must be on a filesystem mounted `noexec` by the
authorized executor; the application-level mode is not a substitute for that
mount property.

## file-service coordinates

- Build source: the release anchor, using `./build.sh <clean-output-dir>`.
  The artifact is `<output-dir>/file-service`; the build output must name the
  exact clean commit and print its sha256.
- Install the verified artifact at
  `/opt/dingwei/file-service/bin/file-service`, `root:root`, mode `0755`.
- Run `deploy/file-service.service` as `dingwei-file`.
- Create the dedicated read-only group `dingwei-file-read` and add nginx user
  `www-data` to it; do not add nginx to the `dingwei-file` primary group.
- Persistent directory: `/var/lib/dingwei-file-service`, owner
  `dingwei-file:dingwei-file-read`, mode `0710` (traversable but not listable).
- Object root and every object subdirectory: group `dingwei-file-read`, mode
  `2750`; setgid makes new descendants inherit only this dedicated group.
- Object files: group `dingwei-file-read`, mode `0640` after an explicit
  `fchmod`, so systemd `UMask=0077` cannot remove group-read access.
- Metadata database: `/var/lib/dingwei-file-service/file-service.db`, owner
  `dingwei-file:dingwei-file`, mode `0600`; it must never belong to
  `dingwei-file-read` and must not be readable by that group.
- Public listener: `127.0.0.1:8793`; internal issuer/audit listener:
  `127.0.0.1:8794`. Only the two public `/files/` routes in
  `deploy/nginx-dingwei.conf` may be exposed.

The authorized environment supplies these exact non-secret settings:

```text
FS_ADDR=127.0.0.1:8793
FS_INTERNAL_ADDR=127.0.0.1:8794
FS_DATABASE_PATH=/var/lib/dingwei-file-service/file-service.db
FS_DATA_ROOT=/var/lib/dingwei-file-service/objects
FS_OBJECT_PREFIX=files
FS_MAX_FILE_BYTES=52428800
FS_TOTAL_QUOTA_BYTES=536870912000
FS_RETENTION=168h
FS_TOMBSTONE_RETENTION=2160h
FS_SWEEP_INTERVAL=1m
FS_CAPABILITY_MAX_AGE=2m
```

It supplies two credential categories without putting values or paths in Git:
the internal issuer bearer and internal audit bearer. Missing credentials must
prevent startup.

`/readyz` is not storage evidence. After file-service starts, the authorized
executor runs `deploy/file-service-storage-probe.sh`. It obtains a real upload
ticket, writes a unique tiny object through file-service, obtains a retrieval
ticket, reads the bytes back from local storage through file-service, compares them,
and prints `FILE_SERVICE_STORAGE_OK`. The probe is a write action and leaves the
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

1. **Recommended: candidate-binary isolated verification.** Run
   `<verified-candidate-file-service> verify-retention --work-dir <empty-temp-parent>`.
   The command creates and later removes its own child directory. It uses the
   production Repository, LocalObjectStore and Manager methods to upload two
   ordinary objects and one pinned object, advances only its process-local
   FixedClock, and invokes the production sweeper. Success is the single line
   `RETENTION_DIRECTORY_OK`; it means the whole dated directory disappeared,
   both ordinary rows became expired, and pinned metadata and bytes remained.
   This command is dispatched before ConfigFromEnv and run(), so it opens no
   listener and reads no production service configuration. It changes no
   production metadata, data root or host clock.
2. **Disposable isolated-instance time travel.** Start a temporary instance
   with a dedicated metadata DB and data root, upload only acceptance objects,
   stop it, move their dated directory and update all corresponding object keys
   and expiry rows as one recorded test fixture, then restart and sweep. This is
   direct mutation of disposable acceptance data and requires cleanup. Never
   time-travel one production row: directory deletion would correctly remove
   its whole day while leaving sibling metadata inconsistent.

Default retention is exactly 7 days from each upload timestamp. The sweeper
deletes eligible objects one by one and always skips archived objects.

Quota eviction is not an acceptable substitute: it proves the quota path, not
the 60-day retention path.
