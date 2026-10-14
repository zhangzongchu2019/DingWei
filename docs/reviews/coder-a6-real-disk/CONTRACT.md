# A6 Agent execution-environment disk — offline candidate

Parent: 2d5bde739deba3876a5b63ce41d8ce1301271735 (first-batch integration tree 424852a803ae40e30ecf88c83f422149478c92da). This change does not incorporate A5 or S-A5-1. Review branch: review/coder/a6-real-disk.

The old View Service /home filesystem is still available in its legacy `disk` response, but cannot become the Agent disk label. A fresh sample from the credential-authenticated current helper connection is required for numbers. Missing, unsupported and expired data have no numeric fallback.

## Producer and authority

The helper observes the actual owned process rather than `SH_CLI_CWD`, upload-policy cwd, helper cwd or the VS host: Codex stdio app-server, Claude subprocess, local spawned OpenCode server, or PTY CLI. It resolves `/proc/<owned pid>/cwd`, requires a live process and the same mount namespace as the helper, and repeats the process check after sampling. The sampler holds a directory descriptor through fstatvfs, checks descriptor/named inode identity and closes it. Used = (blocks - bfree) * frsize; available = bavail * frsize. These distinguish filesystem use from reserved free blocks. No scan, file-body read or raw path/mount export is needed.

This is **helper self-report**, authenticated to an Agent; it is not device attestation. Same-privilege actors/namespace ABA are not ruled out. It reports the filesystem at sample time, not a continuously monitored working directory. First collection can legitimately find an adapter not started; no CLI is started merely to obtain a number.

Remote/external RPC drivers, desktop/LLM modes without an owned CLI process, tmux server sessions, non-Linux proc layouts, different mount namespaces, shared-key connections and hosted/node allocations remain unknown. In particular, a node host filesystem is not substituted for its hosted Agent container. A separately reviewed node allocation/namespace producer is still needed for hosted coverage. This candidate does not close A6 for every deployment mode.

The Hub's dedicated credential admission supplies stable Agent ID, owner, session name and current connection generation. Every accepted sample/read rechecks active Agent and active user, selfhost origin, credential-derived ID and current client pointer. Retired-and-recreated same-name Agents cannot inherit the old connection's data. JSON cannot select an Agent or owner. Samples live in one in-memory cache per connection; disconnect/replacement makes them unavailable to the current read. No database migration, event persistence or history reconstruction occurs.

## Optional wire and bounds

A compatible selfhost Hub advertises `X-DW-Agent-Metrics: agent-disk/v1` on the existing credential WS upgrade. Without this negotiated response the new helper does not collect or send the subtype. Old Hubs therefore cannot accidentally route an unknown metrics message into conversation. The new Hub consumes the subtype, including invalid samples, before conversation/ring/AMR/cost/ACK processing.

Existing envelope: meta.type = `agent_environment_metrics`, meta.no_mirror = true; body is at most 2048 UTF-8 bytes of one JSON object:

- Required: schema `agent-disk/v1`, producer `helper`, path_basis `cli_workdir_filesystem`, status `ok` or `unavailable`, sample_instance (32 lowercase hex), sample_seq (positive canonical uint64 decimal string), sampled_at_ms (UTC integer ms), elapsed_send_age_ms (integer 0..5000).
- For ok: filesystem_instance (opaque process-local 32 lowercase hex), used_bytes, total_bytes, available_bytes; integer bytes <= 2^53-1, total > 0, 0 <= used/available <= total. reason must be empty (the helper omits it; the receiver also accepts an explicit empty string).
- For unavailable: reason in workdir_unbound / namespace_unbound / sample_failed / unsupported_mode; all filesystem/numeric fields absent.
- No other, duplicate, case-variant or null keys; no trailing object. UTC clock skew window is -30 seconds through +30 seconds plus measured send age. A sample outside these bounds is rejected, not restamped.

Candidate numeric profile remains helper 300s, Hub new-sample replacement minimum 300s, TTL 360s, sampler await 2s, send age <=5s. Exact retransmission cannot renew receipt or expiry. Hub TTL is monotonic from first receipt less measured helper age; UTC tolerance is a clock check, not proof of zero transport delay. The UI anchors remaining TTL at its request start, charges request duration and does not renew a repeated sample. Local second ticks paint expiration. GB means 10^9 bytes; actual zero displays zero, small positive use displays <0.1 instead of rounded zero. Provenance and original sample time appear in the disk title.

**Polling phase limitation:** helper 300s plus page 300s with TTL 360s can yield only about 60s remaining lifetime at a status response, followed by visible expired state until the next refresh. The candidate preserves honest expiration. A 30s helper/Hub replacement profile with the existing page 300s/TTL360s was separately proposed to the leader; it has not silently replaced this profile or been field capacity-tested.

## Read path

The public path stays the existing authenticated status proxy and its already signed Hub subject. VS verifies that subject before resource lookup. The new private-only route is POST `/internal/view-v2/{session}/resources`, using the existing per-target VS-to-Hub key, direction `view-service-to-hub/v1`, target, timestamp and single-use nonce. Request <=2048 bytes contains only agent_id/full_name and must match path/current identity. Response <=4096 bytes contains agent_id/full_name/agent_disk. agent_disk carries stable ID, current connection generation, source `helper_self_report`, status, original sample and monotonic age/remaining TTL. It does not authorize input or create pages/capabilities.

VS uses its existing HUB_INTERNAL and secret configuration; no guessed target URL, new credential or secret parameter is introduced. The lookup has a 2s caller bound covering key read/setup/connect/body/decode, at most two daemon workers, bounded response bytes, no redirects or ambient proxy. Late results are discarded. The helper has at most one sampling worker across reconnects. A blocked kernel operation cannot be forcibly preempted by these thread deadlines; an outstanding worker is not multiplied. Secret/OS errors return fixed unavailable, never raw exceptions or private paths.

The original 5s complete status-request deadline, 300s resource cadence, visibility/generation/singleflight rules, page/cookie/Origin/principal/input authorization and paused-cost rules remain. Resources-only lookup does not initialize cost projection. The top-level legacy resource_scope still describes VS memory/time; only agent_disk carries the Agent source contract. VS runtime is kept in view_service.py, so its existing release file list does not omit a new Python runtime module. The new helper module is explicitly included in PyInstaller hiddenimports. VERSION is not bumped or published by this review candidate; release versioning remains part of the coordinated build/release set.
