# Rollout impact and compatibility contract

This describes the candidate, not a deployment order. Approved design is 8d02f432; reviewed D3 switch implementation 555a33bb is an ancestor via merge 2495115f. All source/build inputs must be fetched from Git. No live seat, Hub, provider or host was changed for these checks.

## Helper 0.2.3.8 and older seats

Hub/helper compatibility remains major/minor based: this feature does not reject the whole connection of an enrolled 0.2.3.7 or 0.2.3.6 helper. The following are source/protocol compatibility conclusions, not a claim that these live seats were upgraded or probed.

| Tool/operation | Existing 0.2.3.7 dataConnector seats | Existing 0.2.3.6 oaagent |
|---|---|---|
| send_message, ordinary delivery, dw_reply_message | Existing behavior/authentication unchanged | Unchanged |
| dw_inbox | Existing driver read/local mark behavior unchanged | Existing 0.2.3.6 behavior unchanged; the separate factory-shim transport issue is not fixed by a Hub release |
| project_context | Existing authorization/pagination/budget; bounded new discussion data added and cursor invalidated by new floors | Tool exists; same Hub contract, subject to the separately diagnosed local MCP transport |
| dw_task_list/update, R8 pre-review tools, spec create/update | Existing R9/R8 authorization, immutable/final/template gates retained | Existing tools retained; no new blanket 426 |
| D2 blocker list/set/input/recheck | Existing authorization, shared limits and wake/recheck semantics retained | These r33 tools are absent from the stock 0.2.3.6 shim; absence is not HTTP 426 |
| dw_task_question_list | Existing read path retained; old private history stays under its old ACL; legacy answers labelled unverified, can_answer=false | D3 tool absent from stock shim |
| dw_task_question_create | Stock 0.2.3.7 request lacks required entry_refs: Hub returns 426 clarification_requires_anchored_tools_upgrade_helper | D3 tool absent from stock shim; a manually issued old request receives the same Hub refusal |
| dw_task_question_input | Existing untyped write is now 426; upgrade to dw_spec_clarifications technical with valid entry_refs | D3 tool absent from stock shim |
| dw_spec_clarifications (list/technical/status) | Absent until shim AND helper upgraded together to 0.2.3.8 | Absent until upgrade; copying only new shim onto an old helper gives explicit unsupported-upgrade-helper, not an authorized write |
| dw_spec_template responsibility operations | Existing contract retained | Tool absent from stock 0.2.3.6 |

The Hub also refuses the old human substantive-answer path with 426; an Agent attempting human authority is still 403. An older client may show its generic request error rather than the new descriptive error name. No operator should interpret this as automatic retry permission. question create/input impact is deliberate for all specs, including those without a PM: closing the thread feature does not restore broad old writes. New 0.2.3.8 keeps the old input tool only as a refused compatibility entry; use the new anchored tool instead.

Console question creation, PM/deputy replies, normal login and trusted native Feishu replies are Hub paths and do not require a helper upgrade. Current Agent task context reads also do not require an upgrade. If trial Agent question creation/technical input is required in the same batch, upgrade those seats; otherwise explicitly defer those operations rather than claim them compatible. No automatic capability/tool exposure upgrade is performed here.

## Private image file-service

Build both `workpulse` and `file-service` from the same accepted candidate using the repository `build.sh` (the sole release build entry, with exact SOURCE_COMMIT, external OUT directory, approved Go toolchain and offline locked Python environment). Its four outputs include the actual `file-service` binary from `./cmd/file-service`. There is no alternate image helper binary and no source/object-storage transfer in this work.

New file-service internal endpoint: `/internal/files/clarification-images`, POST/GET, under the existing issuer-authenticated internal listener. It creates one additive owned SQLite table, `clarification_private_image`, in FS_DATABASE_PATH at service initialization. This is file-service initialization, not Hub schema_migrations and not PG. Store image bytes only in that private database; the Hub 0063 tables contain ACL/hash/type/link metadata. Back up the file-service database and its sidecars with the approved consistent-backup method before switching binaries. New startup validates the resolved database path is outside the public FS_DATA_ROOT, including existing symlink ancestors; a conflicting deployment path must STOP, not silently move or copy private bytes.

Existing configuration keys are reused: Hub WP_FILE_ISSUER_URL, WP_FILE_PUBLIC_URL, WP_FILE_ISSUER_TOKEN and service FS_INTERNAL_ADDR, FS_DATABASE_PATH, FS_DATA_ROOT, FS_INTERNAL_ISSUER_TOKEN plus existing audit configuration. This document lists keys only, not values. Do not expose this endpoint as an unauthenticated public download. Private images have a separate 1 GiB fail-closed cap; there is no automatic cleanup/expiry or migration into public objects. JPEG/PNG are fully decoded and dimension bounded; WebP is bounded/structurally validated RIFF VP8/VP8L, with no animated/metadata payloads, not a complete WebP bitstream decode.

**Attachment-later option:** there is no separate image feature flag in this candidate. Text explanations/replies do not depend on file-service and can ship first with the existing service. New upload against an old service fails explicitly (old endpoint 404 becomes a failed upload, generally Hub 500; no configured client yields 503 private_images_pending_config). The Console says the private file service may be pending, permissions lost or the file invalid; no file ID is associated, and it blocks a post while selected files have not all uploaded. User must clear/reselect attachments to submit text. No public URL fallback. Do not describe this as a hidden/disabled attachment control: the control remains visible with explicit failure. No images have been accepted in this split rollout. Once images have been accepted, preserve/serve them with the compatible file-service; rolling that service back would make them unavailable and needs its own controlled plan.

## Hub SQLite 0061–0063

- 0061: append-only current policy/history, floor/link/request metadata, immutable-body triggers and legacy line anchors. Published checkpoint bytes retained.
- 0062: trusted reply metadata, durable dual provider IDs, exact notice parent and early inbox/receipt ledger, typed question anchors and login continuation. Published checkpoint bytes retained.
- 0063: private image ACL/link metadata and explicit old PM backfill. No PG schema change.

r33 has 59 SQL migration files (0057 was deliberately unused); the candidate has 62, not 63. Expected healthy current ledger **59 → 62**. A different live pre-count is a drift gate, not permission to manufacture ledger entries. Migrate runs per-file transactions and ledger checksums: injected failure inserting 0062 rolls back its DDL/column, keeps already committed 0061 (60 rows), and a corrected rerun reaches 62. Repeated successful Migrate keeps 62 and policy/history identical.

0063 backfill reads only `pilot_question_policy`: the spec owner's previously explicit PM, original updated_by/updated_at, blank deputy, version 1. It does not infer authority from question.responsible, leader, editor, current group membership or historical answer writers. An existing new current-policy row is retained; matching history only inserted if absent. Runtime PG active-user/group/binding checks still apply to every request: copied historical selection is not an exemption. Old answers are not converted into authoritative new floors. Test evidence includes actual old-r33 seeded policy, upgrade, exact backfill comparison and repeat Migrate with unchanged policy/history.

## Rollback boundaries

Before any binary rollback: freeze thread/D3/D2/D1 writes and workers, close real Feishu ingress/outbound as appropriate, preserve Hub DB/sidecars, file-service DB, inbox/outbox and unknown receipts. Old r33 does not understand the new D3/clarification flags; setting them to 0 cannot stop an old binary from sending. New flags off never restore old broad reply rights. Exact old r33 libraries and a loopback old Hub were tested reading a disposable upgraded copy and preserving immutable discussion data; this is a read-compatibility result only. After accepted new writes, old binaries must not write the live database. Snapshot restore was tested locally; operational backup/restore, rollback version and any later reopening remain controlled by leader/prodops.
