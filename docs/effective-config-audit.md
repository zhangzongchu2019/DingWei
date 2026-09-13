# Workpulse effective_config environment audit

`effective_config` is a focused startup snapshot, not a claim that every
environment variable read anywhere by Workpulse is printed. T-093 audited the
full set so absence from this line cannot be mistaken for absence from the
process environment.

Printed with effective value and source:

- `WP_ADDR`, `WP_DB_PATH`, `WP_HUB_ID`, `WP_NAME_ENFORCE`,
  `WP_VERSION_ENFORCE`.

Deliberately excluded secrets (the name may be documented; the value must
never enter startup logs):

- `WP_ADMIN_INIT_PASSWORD`, `WP_L2_DEEPSEEK_API_KEY`, `WP_SECRET_KEY`,
  `WP_SYSTEM_V_TASK_SECRET`, `DATAD_TOKEN`, `FEISHU_APP_SECRET`,
  `FEISHU_PROXY_TOKEN`.

Non-secret feature, path, routing, and tuning settings remain outside the
focused snapshot. They are intentionally not represented as absent; diagnose
them from their owning subsystem or `/proc/<pid>/environ`:

- `WP_AGENT_HANDSHAKE_ENFORCE`, `WP_INTERNAL_ADDR`, `WP_VIEW_BASE_URL`,
  `WP_ONBOARD_PUSH_INTERVAL`, `WP_SECOPS_ADMIN_OPENID`,
  `WP_SECOPS_ADMIN_OWNER_KEY`, `WP_APPLY_KEY_APPROVER_OPENID`;
- `WP_AMR_OBSERVE_SESSIONS`, `WP_AMR_ROUTE_SESSIONS`,
  `WP_AMR_QUEUE_CAP`, `WP_AMR_NOTICE_QUEUE_CAP`,
  `WP_AMR_MAX_TURNS_PER_SESSION`, `WP_AMR_MAX_TURN_AGE`,
  `WP_AMR_MAX_TURN_BYTES`, `WP_AMR_MAX_COMPLETED_PER_SESSION`,
  `WP_AMR_TURN_TTL`, `WP_AMR_ROUTE_TIMEOUT`, `WP_AMR_MAX_TARGETS`,
  `WP_AMR_STRUCTURED_FAILURE_MIN_VERSION`;
- `WP_HUB_LIVEZ_LOCK_TIMEOUT`, `WP_HUB_LIVEZ_DB_TIMEOUT`,
  `WP_HUB_LIVEZ_ACCEPT_MAX_AGE`, `WP_RESTART_STABILITY_WINDOW`,
  `WP_HUB_LOCK_WATCHDOG_INTERVAL`,
  `WP_HUB_LOCK_WATCHDOG_PROBE_TIMEOUT`,
  `WP_HUB_LOCK_WATCHDOG_CONFIRMATIONS`,
  `WP_HUB_LOCK_WATCHDOG_FAILURES`,
  `WP_HUB_LOCK_WATCHDOG_CONFIRM_INTERVAL`;
- `WP_WRITE_QUEUE_SIZE`, `WP_WRITE_BATCH_SIZE`, `WP_WRITE_FLUSH_MS`;
- `WP_GLOBAL_SCHEDULE_PATH`, `WP_SCHEDULER_CLI`,
  `WP_SCHEDULER_CONFIG_DIR`, `WP_SCHEDULE_BACKUP_DIR`,
  `WP_SCHEDULE_NOTIFY_CHAT`, `WP_SCHEDULE_NOTIFY_BOT`,
  `WP_SCHEDULE_EVIDENCE_CRON`, `WP_SCHEDULE_EVIDENCE_TZ`,
  `WP_SCHEDULE_PERSONAL_DIR`, `WP_SCHEDULE_TEAM_FILE`,
  `WP_TRANSCRIPT_DIRS`, `WP_PROVISION_DL_DIR`,
  `WP_PROVISION_ALLOWED_HOSTS`;
- `WP_ADMIN_USER`, `WP_DATA_DIR`, `WP_L2_DEEPSEEK_BASE_URL`,
  `WP_L2_DEEPSEEK_MODEL`, `WP_L2_WORKERS`, `DATAD_URL`, `FCS_RPC_URL`,
  `FEISHU_TRANSPORT`, `FEISHU_BOT_CHANNEL_ID`, `FEISHU_APP_ID`,
  `FEISHU_PROXY_URL`, `VIEW_ROUTING_CONFIG`, `REPLY_ORIGIN_DEDUP_TTL`,
  `SH_PROVISION_ALLOWED_HOSTS`.

Test-only diagnostics such as `WP_DEBUG_HOLD_LOCK` are also excluded from the
production snapshot. `NOTIFY_SOCKET` and `WATCHDOG_USEC` belong to the service
manager contract rather than DingWei configuration.
