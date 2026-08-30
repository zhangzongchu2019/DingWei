# SP3 review history and vote-effect boundary

## Legacy rounds — archive only

The earlier R1–R4 cycle ended at `5aca86595d505c9a07c3c56168eae52d9799e19e`. Its R4 Kimi review reported content PASS and three non-blocking observations, but the verdict was later suspended when the owner changed the round contract. Every R1–R4 vote is retained only for audit and differential-review context.

`legacy_vote_effect = archive_only_no_transfer`

No legacy vote counts toward N1–N8, no legacy PASS is copied to a new anchor, and no old recusal/narrow vote satisfies a new-anchor surface.

## Active protocol

- unique owner: `zzc-chatgpt56solzhangzongchu-2642`
- main-review rotation: `zzc-opus50tchangshark-2642` then `zzc-kimi30kqtuxerh-2642`, alternating
- rounds: N1–N8 maximum
- N9 and later are forbidden
- one full-document main review per round
- final owner correction: `zzc-opus50tchangshark-2642` remains permanently recused from `AC-SP3-08`; the one narrow replacement is selected in order from qwen38max then sonnet50tchangshark
- each recused acceptance surface requires exactly one no-involvement narrow PASS on the same frozen anchor
- final aggregation requires every effective surface PASS; no majority rule

## N1 — valid NO, closed as input to N2

- frozen anchor: `9b13c662b6f54b6865c1e55f3e5f6d56afa53c6f`
- main ballot: `zzc-opus50tchangshark-2642`, full document except permanent recusal `AC-SP3-08`, verdict NO
- sole narrow ballot: `zzc-qwen38maxkhovvob4-2642`, `AC-SP3-08`, declared `no_involvement`, verdict NO
- aggregation result: NO; the three main-review findings concerned ballot-gate evidence, roster single-sourcing and selftest count ratcheting; the narrow finding concerned row-scoped canonical enforcement of `AC-SP3-08`
- effect: findings are merged into v1.9/N2; neither N1 ballot transfers PASS effect to N2
