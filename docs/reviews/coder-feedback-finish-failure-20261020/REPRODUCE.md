# Send accepted / completion write failure regression

Fetch wego-ai/dingwei candidate branch and annotated input tag
refs/tags/evidence/coder/feedback-finish-failure-c5521305 through Git. The tag
resolves to tree c55213054dca5c257b9d50f125ce329b39f12876; use an ordinary fetch
for tree-only tags, without --depth=1 unless also fetching a commit.

```
python3 tests/pilot-feedback/withdrawals.py --commit c55213054dca5c257b9d50f125ce329b39f12876 --output /tmp/d3-finish-withdrawals
python3 tests/pilot-feedback/gates.py --commit c55213054dca5c257b9d50f125ce329b39f12876 --output /tmp/d3-finish-gates --gates vet canonical race go
python3 docs/reviews/coder-feedback-finish-failure-20261020/VERIFY.py --commit HEAD
```

The new case is named sent-finish-failure-attempted-boundary. --only can select
it; the full candidate run also covers all previous 45 withdrawals. It removes
the real production attempted=true assignment. An injected failure of the real
FinishQuestionNotice call follows a synthetic provider success; the real SQLite
and real worker remain in use. DeferUnsent calls are counted and delegated, so
a withdrawn attempt flag actually exposes queue-reset/repeated sends rather
than being hidden by a no-op fake reset. Three worker rounds are observed before
asserting, ensuring both first dispatching and next unknown and no resend.

Runners export Git bytes into private HOME/cleared/offline environments, with
locked helper venv /data/ai-dev-data/helper-deps-lock-venv and embedded-PG binary
cache only. No DSN/production host/seat/real Feishu is used. Legacy runner
receipt commit fields identify the resolved input tree. The full helper suite
and browser inputs are unchanged and their earlier reads are explicitly reused,
not reported as newly rerun. The helper module baseline/restored tests are new.
