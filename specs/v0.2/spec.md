# DingWei design/v0.2 unified baseline contract

## 1. Axis decision

- Architecture domains are the long-lived authority axis for semantics, schema, interfaces, events, and invariants.
- SP numbers remain delivery, review, and freeze envelopes. An SP PASS does not mean the whole related domain is implemented.
- Existing SP0/SP2/SP3/SP4/SP8 artifacts retain their reviewed identity and immutable historical anchors.
- New packages are derived from architecture §17 ownership: SP5-COL, SP6-AGT, SP6-MSG, and SP7-PLT. Non-contiguous SpecDoc rows are allowed.
- GOV=SP0+SP8 is a known staged exception. It does not create two owners for one architecture §17 row.

## 2. Existing-package equivalence gate

`domain-baseline.tsv` declares each existing SP's former chapter scope and its declared primary domain. `check.py` derives matching §17 rows and requires every row whose architecture owner differs from the declared domain to appear exactly once in `domain-mismatches.tsv`. Unregistered mismatches must remain zero.

Current result:

| SP | Primary domain | Legacy rows | Unregistered mismatch |
|---|---|---:|---:|
| SP0 | GOV | 1 | 0 |
| SP2 | WRK | 6 | 0 (3 registered GOV seams) |
| SP3 | KNO | 1 | 0 |
| SP4 | CAP | 2 | 0 |
| SP8 | GOV | 1 | 0 |

## 3. Cross-package obligations

`obligations.tsv` is the machine-readable ledger for the seven integration debts. `registered` means deliberately open and is not PASS; `closed-by-v0.2` must have an executable checker in this baseline. Removing a row, duplicating an ID, changing its owner, or claiming closure without its check is an error.

F.15 is owned as a complete traceability row by CAP/SP4. CAP designs domain classification and keeps F.15.5 #4 as its prototype obligation. Authorization remains owned by GOV/SP8 §24 and consumers fail closed until that seam is conformant.

## 4. Reachability and provenance

The baseline must retain ancestry for every complete source commit in `baseline-manifest.tsv`, plus historical interval anchor `c7ac0c3` and historical SP4 verdict anchor `4006cbe`. Every manifest source is a 40-hex commit that reproduces the recorded path digest; free-text verdicts do not belong in the source column. Protected immutable freeze tags are a second guard; they do not replace merge ancestry.

## 5. `docs/v0.1/` meaning

`docs/v0.1/` is a historical archive layer, not a branch/release sequence paired with `design/v0.2`. Current authority files remain at stable paths under `docs/`; the retired architecture remains only at `docs/v0.1/dingwei-architecture-design-v0.34.md`.
