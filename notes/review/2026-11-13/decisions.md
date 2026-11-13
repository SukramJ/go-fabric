# Owner decisions, review II

Each put as a wizard step with the recommendation first; the owner's choice
is the row. IDs are the 2026-11 register's or `R2-…`.

## 2026-11-13, phase 0

| # | Subject | Decision |
| --- | --- | --- |
| 1 | W1-wire-07, BTP handshake with window 0 | **Refuse, as chip does** — a deliberate divergence from matter.js with a `by_design` row; the device is a BLE peripheral since ADR 0031 |
| 2 | W2-conc-05, critical events past the subscription's floor | **Mirror matter.js**: urgency from the subscriber's `isUrgent` path flag, floor and DelayReportData honoured; the stale row goes |
| 3 | W2-wire-09, always-present empty `attributeReports` | **Omit as matter.js does**; fixtures regenerated |
| 4 | W3-clucore-06, NetworkInterfaces | **Mirror matter.js**: up to 8 entries, no synthetic `eth0` |
| 5 | W4-ctlsec-08, fail-safe after a context abort | **Disarm on abort** (ArmFailSafe(0) while the session stands); Ahead row |
| 6 | W1-sec-05, handshake secrets | **Zeroise** after key derivation; Ahead row, with Go's limit stated |
| 7 | API breaks (W2-api-06/07/09, W5-api-18, W3-clugen-09, W3-clucore-07) | **Bundled in phase 5**, one architecture pull request with migration lines |
| 8 | W5-ex-07, group/binding provisioning helper | **Feature issue**; struck in the review with the reference |
| 9 | R2-P0-sec-A03, PASE close after CommissioningComplete | **Moves into `bridge`**; `node` calls it |
| 10 | Wi-Fi credentials and CASE resumption secrets at rest (R2-P0-sec-D01 + E06) | **Document now** (security.md, store/doc.go, the key table); uniform sealing is a phase 5 architecture question |
| 11 | R2-P0-bound-C01, OTA image size | `MaxImageSize`, **default 256 MiB**, settable (a judgement call: no measured image sizes) |
| 12 | R2-P0-test-D01, `wifi/wpasupplicant` not a required check | **The assistant changes main's branch protection** via `gh api`, once the phase 0 pull request carries the guard |
| 13 | R2-P0-par-D01, 956 citations at the old pin, 416 drifts | **Phase 4 re-reads each** at `db021da0` (no mechanical replacement) and adds a guard that ratchets citations at another commit to zero; overrides `pin-lifts.md` "never by a sweep" for this review |
| 14 | R2-P0-test-B09, fabric-scoped list writes without an accessing fabric | **Gate in `im/`**; the servers' own refusals stay as the second line (closes R2-P0-acl-B01's class) |
| 15 | R2-P0-store-B06, controller-written non-volatile attributes on kit servers | **The module's duty**; scope and cut set in phase 2 (persistence flow) |
| 16 | chip-tool on the Mac | **`make chiptool-container`** in the phase 0 pull request: one `linux/arm64` container on a bridge network, never the host network; known limits recorded (the groups leg, CAMERA-GO) |
| 17 | N-1 (first fix wave): 40 kit servers list a mandatory attribute without a value | **`device.New` refuses, as matter.js does** (`conformance-compiler.ts:717-721` at `db021da0`); a break for 19 kit types with bare defaults, so it goes into the phase 5 bundle with its migration line |
| 18 | `codeql (go)` is a required check although `codeql.yml`'s header says it is deliberately not | **Stays required**; the header is corrected |
| 19 | W3-clusec-10 removes the public `OperationalCredentials.SetAttestationChallenge` | **Stays in the phase 0 pull request** — the method is the weakness; `### Removed` line and the migration guide name the replacement |
| 20 | How phases 1–7 run | **Review and verification as a workflow** per phase (inventory → reviewer per theme → an independent verifier per finding, structured findings as the result); fix waves, integration and the wizard stay hand-led. A phase with more than ten agents needs the owner's word on the size |
| 21 | Models (revises "Fable everywhere" of the plan) | **Individual tasks may go to Opus where the result is not worse** (owner 2026-11-13). Applied as CLAUDE.md's model table: Opus for implementing a finished plan a guard checks (mechanical fixes after the first of a kind, a CHANGELOG entry for a finished diff, regenerating a ratchet); Fable for reviews, verification, every statement about matter.js or chip that enters the repository, and the security paths |
| 22 | R2-P0-rv-B01 (diff review, high): the group reception bound of 15 senders with eviction lets a party without the key replay recorded group messages | **Back to unbounded, as matter.js**; a `by_design` row says the table grows only through holders of the group key; a bound without that weakness is designed in phase 3 and put to the owner |
| 23 | R2-P0-rv-A01 (diff review, high): the unsecured session's retransmit bound (16 slots for all, refuse the newest, 60 s) lets any LAN peer block every handshake | **Evict the oldest, always send the reply**: a full table costs the oldest entry its retransmissions only; entries end with their handshake; the failure StatusReport is retransmitted as both references do; A02, A04, A05, A07 fixed with it. The module's own rule, an Ahead row |
| 24 | Models, again (owner 2026-11-13, in his words: "Du musst mehr Opus einsetzen, um den Tokenverbrauch besser in den Griff zu bekommen") | **Opus is the default from here on**, as CLAUDE.md's model table has it: every implementation (fixes, rework, lint, tests, docs, CHANGELOG, ratchets, bookkeeping tables) and the code review of a diff (correctness, concurrency, error paths, tests). **Fable only where a statement becomes a cited fact or a security judgement**: the review and verification of the security paths (`secure/`, `commissioning/`, ACL, the core servers), every citation and register line against the pin, verifying the high and medium findings of an Opus review, and planning. Sonnet or a script for inventories. Phase 0 spent about 10.3 M subagent tokens, 0.56 M of them on Opus |
| 25 | The review notes and the public repository | **What is fixed goes out; open security items are held back** until the pull request that fixes them: the threat model, the reports that mention those items, and their rows in the dispositions table (one line says how many are held back) |
| 26 | W2-wire-13, a device name (DN) longer than 32 bytes | **Refused at `node.Config` validation**, in the phase 5 bundle with its migration line |
| 27 | How the phase 0 pull request merges | **Squash, auto-merge on green CI**; the single commits stay retrievable through the pull request's reference, and the dispositions table says so |

Not re-asked: TC-WASHERCTRL-2.2 keeps the owner's 2026-11-02 decision (no
`by_design` row; struck). Applied from CLAUDE.md without a question: FIXED
texts leave the live findings register; `by_design` rows for confirmed
parity; Ahead rows for bounds matter.js lacks and chip has.
