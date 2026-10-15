# Bounded storage review corrections

Independent reviewer opus50ts requested two corrections on slice1. This append is based on the complete previously submitted candidate1dd0f096d936a3696734b9f849d416166e8ac618; it changes only store factory/sweep code, its tests and docs/evidence. Routing and helper product bytes are unchanged. It does not claim the rest of the candidate has independent approval.

## Historical policy

Historical dead backfill now permits exactly delivery_ack_unsupported, delivery_capability_unconfirmed_expired and delivery_expired. All historical retry_exhausted rows are excluded, including explicit recent codes. The30-minute cutoff remains the terminal updated_at field and excludes future timestamps. A recent explicit retry_exhausted negative and31-minute negative are pinned in TestFailureNoticeRecentDeadBackfillAndConcurrentSweep; accepted count is3, not4. New terminal retry transitions still generate the durable notice atomically and preserve original LastError.

## Body contract

First line remains【系统投递失败通知】. Body now identifies the persisted destination SessionName, never envelope.To/account key; it renders only letters/digits/dot/underscore/hyphen, at most64 runes and192 UTF-8 bytes. Invalid, control/bidi/markup/command-shaped or oversized names use a fixed generic target. Names are rejected whole rather than quoting partially sanitized command text. Four fixed Chinese meanings are selected by the allowlisted reason, with the reason code retained. No body, arbitrary error, key, secret or address is copied into text; no ASCII at/hash can appear.

TestFailureNoticeSafeTargetAndChineseReason exercises actual generated notices for all four new-transition reasons with a synthetic secret raw destination/address and private body, and checks safe target + exact Chinese meaning + absence of those values. Additional sanitizer boundary negatives cover routing characters, commands, newline, markup, bidi and oversized multibyte names. The64-character positive remains valid.

## Valid withdrawals

- Restore only the historical SQL retry_exhausted member: the real sweep produces4 notices, expected3; rc1.
- Restore only the old generic body literal, retaining new helper functions so the test compiles: all four reason subtests fail safe target/Chinese meaning assertions; rc1.

Initial attempts overlaid the entire parent source while the new test referenced sanitizer helpers, causing undefined-function compile failures. Those are explicitly inconclusive, retained separately and not counted as regression evidence. Corrected surgical overlays compile and fail the intended behavioral assertions.

Commands and final results are in REWORK2.json. No production/DB/deployment action. Full helper641 success belongs to unchanged helper/routing predecessor bytes; the new storage change is tested by fresh store/m8 normal and race matrices plus the canonical build gate. Existing FINAL.json pins the predecessor snapshot, not this append; REWORK2.json is the current correction manifest.

Final fresh results: store/m8 targeted race rc0 (store49.901s, m859.765s); complete store/migrate/pgtest/m8 packages rc0; canonical build rc0, python42/shell2. The first build attempt duplicated Go caches under isolated HOME and was deliberately stopped with rc143 before ENOSPC. Only this task's duplicate caches were removed (read-only module directories required owner chmod); explicit existing GOCACHE/GOMODCACHE fixed the rerun. This aborted attempt is pinned, not passed. No product test failure is omitted.

Test-only follow-up: each of the three permitted historical reasons now has its own31-minute negative. Positive concurrent count stays3 and recent historical retry_exhausted remains excluded. The current test source pin is in THREE-CUTOFFS.json; product bytes remain exactly c71. Store focused race rc0. Changing only30-minute cutoff to32 makes the test fail with actual7 expected3, rc1.
