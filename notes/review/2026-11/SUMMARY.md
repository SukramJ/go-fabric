# Whole-codebase review — summary (2026-11-01, HEAD 769ea3c2)

Pins: matter.js 862ad75f, connectedhomeip 6170af84. Six waves, 24 reviewers
(10 Fable, 12 Opus, 2 Sonnet maps), every high/medium finding re-read by
Fable against both sides before it stands. Per-wave verdicts:
`wave-{1..6}-verdict.md`; per-theme findings: `wave-N-<theme>.md`.

| Wave | Scope | high | medium | low | note |
| --- | --- | --- | --- | --- | --- |
| W1 | transport, secure, tlv | 0 | 2 | 10 | 14 |
| W2 | bridge, im, endpoint, mdns, commissioning, store, groups, icd | 1 | 9 | 19 | 21 |
| W3 | cluster (core, generator, 30 device packages) | 1 | 14 | 27 | 16 |
| W4 | client, controller, binding, thread, bdx, BLE | 1 | 8 | 15 | 16 |
| W5 | node, device, contract, surface, examples, fabrictest | 1 | 9 | 18 | 7 |
| W6 | tests and guards, registers, docs | 2 | 15 | 36 | 9 |
| **Total** | | **6** | **57** | **125** | **83** |

## The six high findings (all confirmed)

1. **W2-wire-01 / W2-sec-01** — IM Read/Write/Invoke/Subscribe on the unsecured session 0 run without ACL (`bridge/receive.go:268`, `im/read.go:490`); unauthenticated LAN access to every attribute and non-fabric-scoped command.
2. **W3-clusec-01** — an ACL/Extension write without an accessing fabric is scoped to the client's own entry or fabric 1 (`cluster/core/access_control.go:676-681`); a passcode holder (or session 0) can replace another fabric's ACL.
3. **W4-ctlint-01** — the DCL CRL decoder recurses unbounded and reads the body unbounded (`controller/dcl/crl.go:87`, `revocation.go:352`); a vendor-hosted CRL crashes the controller (fatal stack overflow).
4. **W5-ex-01** — SQLite paths are built into `file:` URIs unescaped at four sites; `#`/`?` opens another file and silently drops the pragmas (fabrictest subtests hit it).
5. **W6-test-01** — five CI guards are not required checks; a red `matter.js citations` run on main was auto-merged.
6. **W6-reg-01** — a FIXED register entry (PASE close after CommissioningComplete) was never true in the module; it hid W2-sec-02.

## Security-relevant medium findings

W2-sec-02 (PASE keeps Administer after CommissioningComplete), W3-clusec-02 (UpdateNOC not bound to the fail-safe's fabric), W6-reg-02 (resumption records survive UpdateNOC), W5-ex-05 (key database created world-readable), the fabric-blind fail-safe class (W3-clusec-03, W3-club1-02, W3-clua2-01 — chip authority, owner decision).

## Owner decisions collected

1. Fabric-aware fail-safe port (chip refuses CSR/AddNOC/AddTrustedRoot/Thread dataset/AddICAC from a fabric other than the fail-safe's; matter.js shares the gap in OpCreds).
2. API breaks in the facade: `Node.CloseWindow` + hide the bridge window type (W5-api-01), one reachable input (W5-api-02), `node.New(ctx, …)` or store work in Start (W5-api-03); five duplicate-shape pairs (W5-api lows).
3. TimeSynchronization mounted by default (W6-reg-05): keep and record, or mirror matter.js (not mounted).
4. Ahead-of-matter.js candidates: CSR nonce comparison (W4-ctlsec-01), CAT list validation in the CA (W4-ctlsec-02), DAC notAfter check (W4-ctlsec note), ClosureControl and Joint Fabric register rows.
5. How to bundle the fixes (decided after this summary).

## Already decided (2026-11-01)

W1-sec-01 fail closed without API break; no hotfix for W2-wire-01, all fixes after the review; model split Fable (security + verification) / Opus (reviews, fixes) / Sonnet (maps).

## Token note

Roughly 6.4 M subagent tokens for the review. The Sonnet map of W3 group A cost 368 k (line-by-line reading) against 86 k for the script-generated group B map; W5/W6 ran without maps.

## Owner decisions 2026-11-01 (after the summary)

- Fixes: security first (PR 1 on `review/security`), then one PR per wave W1–W6 for the remaining mediums; **one tag only after all fix PRs** (owner 2026-11-01, no v0.15.7 after PR 1).
- Fail-safe: adopt chip — the fail-safe port takes the accessing fabric; Ahead-of-matter.js row for OpCreds.
- API breaks W5-api-01/02/03 and the five duplicate-shape pairs: all in the W5 PR, migration lines in the CHANGELOG, loom follow-up issue.
- TimeSynchronization stays mounted by default; the by_design row is corrected to record the real divergence.
- The review notes are committed with PR 1 (not before: the register describes open vulnerabilities).

## PR 1 verification (Fable, 2026-11-01, PR #359)

Every fix read against the pins (matter.js 862ad75f, chip 6170af84); every new citation checked line by line. All 13 items hold. Read into the W1, W2, W3, W4, W6 verdict files when the wave PRs start.

- Item 1 (W2-wire-01): the request is dropped without a reply — matter.js `ExchangeManager.ts:391-412` opens no exchange; the W2 verdict's StatusResponse(InvalidAction) is withdrawn.
- Item 2 (W3-clusec-01): a local write without a request context keeps the entries' fabric (ported matter.js test `setStateOf`); the IM path never does.
- Item 3 (W2-sec-02): sessions closed by ID, not `ClosePASESessions`, so a second commissioning within the 100 ms grace keeps its PASE session; the PASE marker survives `AdoptFabricIndex` (`secure/operational/manager.go:472-485`), so the adopted session goes too.
- Item 5: the port reports `(fabricIndex, armed)`; UpdateNOC needs the armed state alone for FailsafeRequired and the fabric for ConstraintError. Ahead row BD-chip-FailSafeOfTheAccessingFabric.
- Item 7: the agent's CRL bound took the vendor's DataFileSize as the limit; a point announcing more than 1 MiB is now rejected before the download. OTA's bound rewritten for CodeQL (`go/incorrect-integer-conversion`).
- Item 8: the opener moved to `store.OpenSQLite` so `controller.OpenDB` can use it (archcheck: roles must not import the facade); `url.URL.OmitHost` added — without it a relative path became `file://rel.db` and SQLite refused the authority.
- Item 10 remainder: a NOC without ICAC has no AKID check against the root SKID (the verifier holds the root key only); the signature check against the root key makes it a shape gap, not an authentication gap → W1 PR, additive constructor.
- Open for the wave PRs: `core.ThreadNetworkCommissioning` fabric-aware (chip `NetworkCommissioningCluster.cpp:157`) → W3; fabric-0 bypasses `im/read.go:731,757`, `bridge/subscribe.go:385`, `endpoint/dispatcher.go:1021` fail closed → W2.

## Closing (2026-11-02, after the W6 fix PR)

The review's fixes landed in seven pull requests: PR 1, the security
bundle (#359); W2 (#360); W1 (#361); W3 (#365); W4 (#366); W5 (#367); W6
(this wave's PR, `review/w6`). #364 fixed a flake between them. One tag
follows them all (owner 2026-11-01).

What stayed open, per wave (the verdict files have the detail):

| Wave | Open |
| --- | --- |
| W1 | the `note` entries, among them the BTP window 0 accepted (matter.js parity; chip rejects) |
| W2 | the lows and notes outside the W2 PR's scope (`wave-2-verdict.md`); W2-wire-06 partly (grandcat's PTR/SRV/TXT TTL stays 3200, L7-D02) |
| W3 | notes W3-clusec-09/10, clucore-06/07/09/12/13, clugen-04/05/06/09/10, club1-09/10, clua2-04 |
| W4 | parity notes W4-ctlnet-07, W4-ctlsec-08; W4-ctlsec-03 (DAC/PAI notAfter) stays with matter.js |
| W5 | W5-ex-10 (three facade gaps the reference bridge still reaches below the facade for), W5-ex-07 (a group/bind provisioning helper as a facade candidate) |
| W6 | notes W6-test-14, -15, -17; the four chip cases item 1 excluded (none a module defect); 250 citation drifts on their ratchet |
