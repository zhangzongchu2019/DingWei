# View Service release order

This template is mandatory for the `view-service` artifact type. It does not
grant deployment authority.

- Target environment/container:
- Full 40-character source commit:
- Reachable Git ref:
- Package `MANIFEST.sha256` digest:
- Package verification command and result:
- Coordinator request/approval coordinate:
- Owner authorization coordinate:
- Executor assigned by coordinator:
- Pre-image backup directory and three file digests:
- Forward-image backup directory and three file digests:
- Rollback target and three file digests:

Before and after a T-087 renderer deployment, record all three observations
with UTC application timestamps:

1. Feishu WebSocket lifecycle sequence.
2. Feishu ingress counts for `event_received`, `normalized`, and
   `message_enqueued`.
3. Application/container time.

Required execution checks:

1. Run `install` without `--go`; it must end with `PRECHECK_ONLY writes=0`.
2. Confirm all three runtime files are compared. Sampling is forbidden.
3. Execute only after the authorization fields above are complete.
4. Copy the three runtime files read-only from the container into a fresh
   temporary flat directory, then run `identify-online <snapshot> --flat`
   without deployment records. Record `IDENTIFIED`, `AMBIGUOUS`, or
   `NOT_IN_REPO` without weakening the result. `/app/vs` is not a host path.
5. For Python changes, confirm `__pycache__` was removed before restart and
   perform a behavior-level health check. Renderer-only changes are hot
   updates and do not restart viewservice.
6. On any failure, restore every pre-image with its recorded uid/gid/mode and
   verify all three rollback digests.
