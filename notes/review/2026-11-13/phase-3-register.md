# Phase 3 — the dispositions

The commit ids below are those of the branch of this phase's pull request
(`review2/p3`, `8c9088ee..58d60b96`, 100 commits). If that pull request merges
squashed, as the three before it did, its single commits stay retrievable
through the pull request's reference. Items that are still open after this
branch and describe a weakness a party on the network could use are held back
with their text until the pull request that fixes them (owner decision 25);
the table carries their id, severity and phase only, and "Counts" says how
many. What this branch fixed is public as what the code does now.

Phase 3 was the adversarial review: seven reviewers each took one attacker's
place (owner decision 50) — an unauthenticated host on the device's link
(`lan-dev`), a holder of the passcode (`pase`), another fabric's administrator
(`admin`), a hostile device against the controller (`ctl-dev`), an
unauthenticated host on the controller's link (`ctl-lan`), a hostile BLE peer
(`ble`), a hostile Wi-Fi, DCL or OTA peer (`net`). Five of the seven ran on
Opus, after the provider's safeguards had stopped the first agent (`lan-dev`,
`pase`, `ctl-dev`, `ctl-lan`, `ble`); `admin`, `net` and the three verifiers
ran on the session's model, as the result file records it.

One row per finding of the three review runs — the seven roles (45 findings),
the diff review of the first fix wave (`dr-dev`, `dr-ctl`, `dr-net`; 21) and
the review of the rework after it (`rr-dev`, `rr-ctl`; 10) — one per item of
phases 0 to 2 a role was given and judged, one per owner decision of this
phase, one per item the fix agents, the citation passes and the coordinator
noticed and did not fix where it was noticed, and one per unit for what its
reviewer did not read. Sources: the two result files of the review workflow,
the two reports of the rework's review, the sixteen fix notes
(`phase-3-fix-*.md`), the reports of the first two citation passes, the
coordinator's open list, `decisions.md` (owner decisions 50 to 57), the
coordinator's note on the six commits after the fixes — the third citation
pass, a repaired test and the notes pass (`02f96a64`, `97850231`, `b0b7e8e0`:
CHANGELOG, `by_design.md`, the limits page) — and `git log
8c9088ee..58d60b96`. The notes are not part of this change; one that mentions
an item held back follows with the pull request that fixes that item (owner
decision 25).

How a row was placed — by rule:

- **fixed `<commit>`** — the commit is in `8c9088ee..58d60b96`, and its
  subject or its fix note's table names the ID. The fix notes carry the ids
  the commits had before they were rebased onto the branch; the branch's
  commit is the one with the same subject. For every finding of high or medium
  severity, the reviewer's or the verifier's, the files the commit touches
  were read off `git show --stat`; where it does not touch the file the
  finding names, the row says what it touches instead. "corrected by" names
  the finding of a later review that found the fix wanting and the commit that
  repaired it; "On the fix of" in the later finding's row names the same
  pairing from the other side. The pairing is the register editor's: by the
  finding a later one names, or by the mechanism it names and the commit that
  brought it. The row gives the state at `58d60b96` and says what the code
  does now, not what the defect allowed.
- **by_design `<row>`**, **Ahead `<row>`** — the row is in
  `notes/parity/by_design.md` at `58d60b96`.
- **parity, no change** — the behaviour is matter.js's at the pin; the lines
  were re-read for this file.
- **struck: `<reason>`** — the reason is the fix note's or the coordinator's;
  "not implemented" with the fix agent's reason is a struck row.
- **phase N: `<what>`** — placed by the plan's cut (`README.md`: 4 parity, 5
  architecture, tests, docs and the API bundle, 6 the full pass). The
  placement is the coordinator's where his open list names the item, the
  register editor's otherwise; the coordinator may move it.
- **held back** — the coordinator's list marks the item so, or, where his list
  does not name it, the register editor judged that the open item tells a
  party on the network what it can use; "Not placed, not verified" names the
  rows of the second kind.

Severity is the verifier's where a finding was verified — every high and
medium of the roles' review (28) and of the diff review (6) — with the
reviewer's in brackets where the verifier lowered it (12 and 3); a low or note
carries the reviewer's severity and was not verified before its fix. The
review of the rework had one reviewer per unit and no verifier; "PLAUSIBLE" in
a row is that reviewer's own word. "Re-read" after a reference's lines means
read for this file in the exported tree of the pin (matter.js `db021da0`,
connectedhomeip `6170af84`); a reference statement without it is its source's.

## The findings

### The seven roles

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P3-lan-dev-01 | medium | fixed `7254068c`, corrected by R2-P3-dr-dev-03 `de357a95` | `bridge/securechannel.go`, `bridge/case_provider.go`, `bridge/pase_provider.go` — a handshake's state is found by its initiator's node id and its exchange; the exported provider interfaces of `bridge` are keyed so (owner decision 51). Also S-1 of phase 0 and the provider part of U-ctr-1. The same key on the paths the fix did not reach: N3-hs-1, -2 and -4 |
| R2-P3-lan-dev-02 | medium | fixed `34799839`, corrected by R2-P3-dr-dev-02 `8a754e2b` and R2-P3-dr-dev-07 `2299af59` | `bridge/securechannel.go`, `bridge/handlers.go`, `node/security.go`, `secure/operational/manager.go` — a Sigma3, Sigma2_Resume, Pake1 or Pake3 whose handshake no Sigma1 or PBKDFParamRequest began builds no adapter and stakes no session id; a handshake that fails or is dropped unfinished returns its id at once; allocating at a full id space no longer walks the table |
| R2-P3-lan-dev-03 | low (medium) | fixed `afa2b964` | `bridge/securechannel.go` — while a PASE pairing is in progress a PASE message of any other handshake is ignored and not counted toward the failure limit |
| R2-P3-lan-dev-04 | low | fixed `557eb391` | `bridge/securechannel.go` — the same defect as R2-P3-pase-01, seen from the unauthenticated side; N-noc-6 of phase 2 |
| R2-P3-lan-dev-05 | low (medium) | fixed `5ab87724`; Ahead `BD-chip-TCP-InboundConnectionsBounded` | `transport/tcp/listener.go`, `bridge/bridge.go` — a failed accept is retried, and a `Serve` that ends with an error is logged at warn. The same defect as R2-P3-ctl-lan-04 |
| R2-P3-lan-dev-06 | low | fixed `57d75d19`; Ahead `BD-Matter-MdnsUnicastAnswersBounded` | `mdns/answer.go` — one source gets at most one unicast answer per service a second; further QU queries are answered by multicast. Part (a) of N-net-1 of phase 1 |
| R2-P3-lan-dev-07 | low | fixed `44fcec8c` | `bridge/ackpump.go` — the acknowledgement of an unsecured message goes to that message's own initiator. R2-P1-req2-11 of phase 1 |
| R2-P3-pase-01 | medium (high) | fixed `557eb391`; by_design `BD-Matter-PASE-EstablishedSessionGate` (withdrawn: no divergence any more) | `bridge/securechannel.go`, `bridge/pase_close.go`, `secure/operational/manager.go` — no new PASE exchange is accepted while a PASE session exists that is not closing, as matter.js (`PaseServer.ts:80-82`, the citation pass's reading); a session whose close is scheduled does not count. N-noc-6 of phase 2. What the gate asked of this module's own commissioner: N3-rwpase-1 and -2 |
| R2-P3-pase-02 | low (medium) | fixed `9a5d600f` | `cluster/core/general_commissioning.go` `armFailSafeFor` — a fail-safe the PASE auto-arm begins runs under MaxCumulativeFailsafeSeconds |
| R2-P3-pase-03 | low (medium) | fixed `7daa7ea3`, `df5c9775` (the test's lint) | `cluster/core/general_commissioning.go`, `node/security.go` — `ArmOnPaseEstablished` returns the arm's refusal; a PASE handshake whose session's fail-safe arm is refused fails, and the session is closed. `AutoArmOnPaseEstablished` is deprecated (announced by `f85e8cbc`, N3-gc-3) |
| R2-P3-pase-04 | low | fixed `25b92e35` | `bridge/commissioning_window.go` `SetBlocked` — the host's block ends a commissioning under way, the PASE sessions and the fail-safe armed over PASE with it, also after the window closed. R2-P2-rr-dev-08 of phase 2 |
| R2-P3-admin-01 | medium (high) | fixed `5a53e37e`, `d282c67c` (comment); Ahead `BD-chip-ChunkedWritesBounded` (rewritten) | `im/write.go`, `im/group.go` — a chunked write holds an attribute only once its access is granted, and a fabric-scoped attribute for the writing fabric only; owner decision 52, which changes decision 30 |
| R2-P3-admin-02 | medium | fixed `a014460c` | `endpoint/dispatcher.go`, `schema/attribute_access_gen.go` (generated) — an attribute is read at the data model's read privilege, which a server can only raise; `TestAttributeReadPrivilegeParity` holds the generated table to the snapshot |
| R2-P3-admin-03 | medium | fixed `fc8186bf`, corrected by R2-P3-dr-dev-01, -04, -05 and -06 `9199d9ec` and by R2-P3-rr-dev-03 `50deec8f`; Ahead `BD-Matter-SubscriptionEndsWithoutReadablePath`; by_design `BD-Matter-TimedAndQuotaDeferred` (the quota part rewritten) | `im/subscription/manager.go`, `bridge/subscribe.go` — a SubscribeRequest at its fabric's quota takes the place of the fabric's oldest subscription, as connectedhomeip, and a subscription whose subject may read none of its paths ends; owner decision 53. R2-P1-dr-req-09 and N-bridge2-1 of phase 1. The rule gives no share per peer inside a fabric: N3-acl-4 |
| R2-P3-admin-04 | low (medium) | fixed `22464710` (the bound); Ahead `BD-chip-BindingListBounded`; phase 5: the list's re-store per ADD | `cluster/core/binding.go` — the Binding list of an endpoint holds at most 64 entries, RESOURCE_EXHAUSTED past it. An ADD still re-reads and re-stores the whole list: the dispatcher's list handling and one stored blob per endpoint (`endpoint/dispatcher.go`, `node/persist.go`), now at most 64 entries |
| R2-P3-admin-05 | low | fixed `d08f0f35`, corrected by R2-P3-dr-dev-08 `65ce1568`; by_design `BD-Matter-CASE-IDExhaustionEvictsPlaceholdersOnly` (amended) | `secure/operational/manager.go` `AllocateID` — at a session id space full of live sessions the least recently active is closed and its id reused |
| R2-P3-admin-06 | low | fixed `743b9bf3`, `b29eed44`; Ahead `BD-Matter-FailSafeEndsWithItsFabric` | `cluster/core/general_commissioning.go` `FabricRemoved`, `node/node.go` — a fail-safe armed by a fabric expires, with its rollback, when that fabric is removed; the node's own window reopens at once when the last fabric's own fail-safe expires with it |
| R2-P3-ctl-dev-01 | high | fixed `d5a9c979`; by_design `BD-Matter-Controller-ResponderSessionsPerPeer` | `client/exchange/manager.go`, `client/handshake/server.go` — a peer holds at most five sessions it established with the controller's CASE responder, the least recently used closed, as matter.js. R2-P3-dr-ctl-03 is a finding on this fix |
| R2-P3-ctl-dev-02 | high | fixed `20c24d63`, corrected by R2-P3-dr-ctl-01 `f1ba40f4`, -02 `f59dfc60` and -04 `0376473d`; Ahead `BD-Matter-Controller-NodeImageBounded` | `controller/client/structure.go`, `controller/client/image_bounds.go` — a node's image keeps what the node's structure names or the request asked for, within a configurable quota per node (owner decision 54). The ServerList half of the rule is not applied: matter.js's tolerance of a reported cluster its ServerList does not name stays (owner decision 57) |
| R2-P3-ctl-dev-03 | medium | fixed `14bedfb4`; phase 5: the served read is assembled whole before it is chunked (N3-ctl-1) | `client/interaction/server.go` — the controller's served reads answer for its own node id only: a concrete path that names another node is answered UnsupportedNode, a wildcard one selects nothing |
| R2-P3-ctl-dev-04 | low | struck: not implemented — it would reverse owner decision 45 | `client/handshake/pase.go`, `client/handshake/case.go`, `client/handshake/server.go` — the secure session a handshake establishes keeps the intervals its peer stated; decision 45 clamps the unsecured session only. Both references accept the stated values (the fix note; not re-read) |
| R2-P3-ctl-dev-05 | low | phase 5: needs a decision on white-label devices | `controller/softwareupdate/manager.go`, `controller/commissioner/flow.go` — the vendor and product a device was attested as are not retained after commissioning; retaining and comparing them needs a field of the node's record, a rule for nodes commissioned before it and the choice of which pair counts |
| R2-P3-ctl-lan-01 | high | fixed `48e31f29`; Ahead `BD-Matter-Controller-StagedAddressesBounded` | `client/discovery/names.go` — the addresses staged for a host no SRV has named yet stand inside the cache's bound of 8192 records, five per host, dropped before any cached record, and expire at their graced TTL. The staged part of U-net-1 |
| R2-P3-ctl-lan-02 | medium | fixed `9bef9aa7`, corrected by R2-P3-dr-ctl-06 `b7212b30`; Ahead `BD-Matter-Controller-OneHandshakePerSigma1` | `client/handshake/server.go` — the CASE responder runs one handshake per Sigma1, remembered by its initiator random for 60 s, 256 at most. The commit changes the responder and its test, not the file the finding names (`client/exchange/manager.go`). R2-P3-dr-ctl-05 put the memory's two bounds under test. One thing the fix leaves is a held-back row (N3-ctl-3) |
| R2-P3-ctl-lan-03 | medium | fixed `5ab87724`, `2aed8e2a` (owner decision 56), corrected by R2-P3-dr-net-02 `4b585d23`; Ahead `BD-chip-TCP-InboundConnectionsBounded` | `transport/tcp/listener.go` — inbound connections are bounded in all: a field of the TCP configuration, 4 on the device and 256 on the controller by default |
| R2-P3-ctl-lan-04 | medium | fixed `5ab87724`, `d326a412` (docs) | `transport/tcp/listener.go`, `controller/tcp.go` — a failed accept is retried, and a `Serve` that ends with an error is logged at warn. The same defect as R2-P3-lan-dev-05 |
| R2-P3-ctl-lan-05 | low (medium) | parity, no change | `client/handshake/server.go` `resume` — the resumption exchange as matter.js has it: `CaseServer.ts:211` adds the resumed session, `:213` moves the resumption id, and only `:216` waits for the peer's Success (re-read); connectedhomeip activates the session on that status report (`CASESession.cpp:2315-2340`, `PairingSession.cpp:62-66`, re-read). The module's existing row is `BD-Matter-CASEServer-CloseOnUnfinishedEstablishment` (the verifier) |
| R2-P3-ctl-lan-06 | medium | fixed `5345b4ec`, `c8e69f85` (owner decision 55); Ahead `BD-Matter-Controller-CommissioningRaceBounded` | `client/discovery/session_kinds.go`, `controller/commissioner/commissioner.go` — a race attempt's stagger counts only the attempts still running; an attempt starts no later than 16 s after its device was found, no more than 16 run together, and one whose device has not answered PASE gives its place to a later candidate. The same defect as R2-P3-ble-01. What the decision leaves is held back: N3-rwrace-1 and -2, R2-P3-rr-ctl-02 |
| R2-P3-ctl-lan-07 | low | fixed `5801514d`; by_design `BD-Matter-PeerBusyDeadline` (amended) | `client/peer/peer.go`, `client/peer/connection.go` — only the address a session proved holds the peer on a Busy; another address of the node's host holds only its own attempt |
| R2-P3-ctl-lan-08 | low | struck: not implemented — the proposed fix breaks CASE to sleepy devices | `client/peer/connection.go` — the handshake's unsecured session is timed by the discovered intervals, clamped at one hour (owner decision 45); timing it by the lower of discovered and default would retransmit Sigma1 at the default pace to a device that advertised a long idle interval (the fix note). Both references accept an advertised interval up to one hour (the finding's citations, not re-read) |
| R2-P3-ctl-lan-09 | low | fixed `1520c5af` | `client/peer/connection.go` — a discovered address is attempted only when it is a unicast destination, as a hinted one. D-3 of phase 0 |
| R2-P3-ctl-lan-10 | note | parity, no change | `client/peer/connection.go` `classifyError` — a NoSharedTrustRoots status deletes the peer's resumption record, as matter.js: `PeerConnection.ts:703-707` (re-read; nothing at these lines looks at the address the status came from). The cost is one full CASE (the fix note) |
| R2-P3-ble-01 | medium | fixed `5345b4ec`, `c8e69f85` (owner decision 55); Ahead `BD-Matter-Controller-CommissioningRaceBounded` | `client/discovery/session_kinds.go` — the same defect as R2-P3-ctl-lan-06; its row |
| R2-P3-ble-02 | low (medium) | fixed `d1d42547`; Ahead `BD-Matter-Controller-BleScannerBounded` | `controller/ble/scanner.go` — the scanner holds at most 8192 records, forgetting the one seen longest ago, and finds a rotated address by its service data |
| R2-P3-ble-03 | low (medium) | fixed `59f4fa23`; by_design `BD-chip-BTP-RepeatedAckRefused` | `transport/btp/session.go` — an ack number already received is a protocol error that closes the session, as connectedhomeip. The send queue's bound is N3-disc-3 |
| R2-P3-ble-04 | low | fixed `279dc842` | `ble/bluez/central_linux.go` — the central forgets a device object BlueZ removed, its address and its advertisement. Linux only: the fix's test was compiled and vetted on the fix agent's machine, not run |
| R2-P3-ble-05 | low | fixed `4d9f424e`, corrected by R2-P3-dr-net-03 `1dd80820` and R2-P3-rr-dev-02 `bdd115fe`; by_design `BD-chip-BLE-WindowEndDisconnectsCentral` | `bridge/ble.go` — the end of a commissioning window disconnects a BLE central that has not completed PASE |
| R2-P3-net-01 | high | fixed `2e5c955f` (the driver), `7780fd17` (the rejoin), corrected by R2-P3-dr-net-04 `37bfb10c`, -05 `ea6718ce`, -06 `6f40740b` and -07 `e2491990`; by_design `BD-chip-WiFiRejoinBackoff`, `BD-chip-WpaSupplicantDriver` (amended: a seventh departure); phase 5: a retry after a lost link needs a link-state port | `wifi/wpasupplicant/driver.go`, `cluster/core/network_commissioning_wifi.go` — a failed join leaves wpa_supplicant on the network it was on; a node off its committed network rejoins it with a backoff on the node's clock. Of the two commits only `2e5c955f` touches the driver, the file the finding names. `contract.WiFiDriver` reports no link change, so after a lost link the rejoin is wpa_supplicant's own (the fix note) |
| R2-P3-net-02 | medium | fixed `7780fd17`; by_design `BD-chip-WiFiRejoinBackoff`, `BD-chip-WiFiNetworkCommissioningServer` (amended); phase 5: an expiry with nothing committed | `cluster/core/network_commissioning_wifi.go` `OnFailSafeExpired` — a fail-safe expiry brings the radio back to the committed network, and Connected follows the link. With no committed network at all the radio stays where the join put it: the driver port has no call to forget a network. R2-P3-dr-net-01 is the open finding on this fix |
| R2-P3-net-03 | low (medium) | struck: no code change — a recorded owner decision (2026-11-06); by_design `BD-Matter-OTARequestor-HTTPS` (one sentence corrected) | `cluster/core/ota_requestor_query.go` — the download is bounded in size and in stall time, not in total time; the entry's reasoning now says what the stall bound does and does not hold |
| R2-P3-net-04 | medium | fixed `b5d2b533`; by_design `BD-chip-WiFiScanResponseBounded` | `cluster/core/network_commissioning_wifi.go` — a ScanNetworksResponse carries at most 15 results, the strongest, so that it fits its message |
| R2-P3-net-05 | low (medium) | fixed `343f8fd6`; Ahead `BD-Matter-Controller-DclRequestsBounded` | `controller/dcl/revocation.go` — a revocation lookup bounds the CRL downloads it has open and its own duration |
| R2-P3-net-06 | low | fixed `343f8fd6`; Ahead `BD-Matter-Controller-DclRequestsBounded` | `controller/dcl/ota.go`, `controller/dcl/client.go` — a download keeps https across redirects. One thing the fix leaves is a held-back row (N3-net-4) |
| R2-P3-net-07 | low | fixed `343f8fd6`; Ahead `BD-Matter-Controller-DclRequestsBounded` | `controller/dcl/github.go` — every GitHub request has a deadline |
| R2-P3-net-08 | note | fixed `11a7d3de` (docs) | `docs/guides/ports.md` — the OtaApplier row states what the requestor checks before `ApplyUpdate` and what is the applier's |

### The diff review of the first fix wave

Three reviewers read the first wave as a diff (`8c9088ee..dd6e5bf3`); its six
findings of high or medium severity were verified.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P3-dr-dev-01 | low (medium) | fixed `9199d9ec`, `c8baf5c3` (comment), corrected by R2-P3-rr-dev-03 `50deec8f`; Ahead `BD-Matter-SubscriptionEndsWithoutReadablePath` (amended) | `bridge/subscribe.go`, `im/write.go` (`WriteHolds.Held`) — a subscription ends for want of access only at two observations of one unchanged ACL, never during a chunked write of that ACL. On the fix of R2-P3-admin-03 |
| R2-P3-dr-dev-02 | low | fixed `8a754e2b`, corrected by R2-P3-rr-dev-01 `8463567c` | `bridge/securechannel.go` — a Sigma3 that arrives again after its handshake completed is answered with the success report. On the fix of R2-P3-lan-dev-02 |
| R2-P3-dr-dev-03 | low | fixed `de357a95`, corrected by R2-P3-rr-dev-04 `2f3a58b7`; by_design `BD-Matter-CASE-HandshakeStateBounded` | `bridge/securechannel.go`, `bridge/case_provider.go` — the Sigma1 records and a `PerExchangeCaseProvider`'s adapters are bounded at the unsecured windows' cap (256), the oldest losing its place, and a Sigma1 that left no handshake state leaves no record. On the fix of R2-P3-lan-dev-01 |
| R2-P3-dr-dev-04 | low | fixed `9199d9ec` | `bridge/subscribe.go` — a path the node no longer serves is no refusal. On the fix of R2-P3-admin-03 |
| R2-P3-dr-dev-05 | low | fixed `9199d9ec` | `bridge/subscribe.go` — nothing is decided on an ACL read error. The reviewer's PLAUSIBLE was confirmed by the fix's test. On the fix of R2-P3-admin-03 |
| R2-P3-dr-dev-06 | low | fixed `9199d9ec` | `bridge/subscribe.go` — the walk that decides runs after an ACL or topology change only, not before every report. N3-acl-3. On the fix of R2-P3-admin-03 |
| R2-P3-dr-dev-07 | low | fixed `2299af59`, `0102567b` (README); by_design `BD-Matter-CASE-IDExhaustionEvictsPlaceholdersOnly` (amended again) | `secure/operational/manager.go`, `node/security.go` — a handshake releases its own session id stake (`AllocateStake`, `ReleaseStake`); `ReleaseID` is deprecated. On the fix of R2-P3-lan-dev-02 |
| R2-P3-dr-dev-08 | note | fixed `65ce1568`; by_design `BD-Matter-CASE-IDExhaustionEvictsPlaceholdersOnly` (amended again) | `secure/sigma/protocol.go`, `node/security.go` — the CASE responder stakes its session id once a Sigma1 is accepted. On the fix of R2-P3-admin-05 |
| R2-P3-dr-ctl-01 | high | fixed `f1ba40f4`; Ahead `BD-Matter-Controller-NodeImageBounded` (amended) | `controller/client/image_bounds.go` — a cut-back at load erases from the store only what it drops; kept endpoints, the node's record, its settings and its ICD entry stay. On the fix of R2-P3-ctl-dev-02 |
| R2-P3-dr-ctl-02 | high | fixed `f59dfc60`; Ahead `BD-Matter-Controller-NodeImageBounded` (amended) | `controller/client/image_bounds.go`, `controller/client/cache.go` — the image quota charges each endpoint, cluster, attribute and TLV element what it holds in memory, so it bounds the image's heap and the store's rows. `CHANGELOG.md` and the by_design entry describe this measure since `02f96a64` and `97850231` (N3-finimg-4). On the fix of R2-P3-ctl-dev-02 |
| R2-P3-dr-ctl-03 | low (medium) | parity, no change; by_design `BD-Matter-Controller-ResumedSessionCountedAtRegistration` (a parity statement, no divergence) | `client/handshake/server.go` — a resumed responder session is added, and the cap per peer applied to it, before the peer's Success, as matter.js: `CaseServer.ts:211` before `:216`; `SessionManager.ts:312-318` runs the eviction for every added responder session, the function at `:699-726` (re-read; the fix note cites `:697-725`, which begins in the comment above it). Whether connectedhomeip caps a peer's sessions was not searched (the fix note). On the fix of R2-P3-ctl-dev-01 |
| R2-P3-dr-ctl-04 | low | fixed `0376473d`; Ahead `BD-Matter-Controller-NodeImageBounded` (amended) | `controller/client/scope.go` — a read path that names only an attribute asks for it on every endpoint and cluster, and the image keeps those values. On the fix of R2-P3-ctl-dev-02 |
| R2-P3-dr-ctl-05 | low | fixed `a2835aa8` (tests) | `client/handshake/server_sigma1_memory_test.go` — the code was right; the Sigma1 memory's lifetime and its count bound are under test, on a fake clock. On the fix of R2-P3-ctl-lan-02 |
| R2-P3-dr-ctl-06 | note | fixed `b7212b30`; Ahead `BD-Matter-Controller-OneHandshakePerSigma1` (amended) | `client/handshake/server.go` — a Sigma1 whose Sigma2 or Sigma2_Resume the socket refused is forgotten, so its retransmission starts the handshake. The reviewer's PLAUSIBLE was confirmed by the fix's test. On the fix of R2-P3-ctl-lan-02 |
| R2-P3-dr-net-01 | low (medium) | phase 5: needs a port to forget a network; by_design `BD-chip-WiFiRejoinBackoff` records it as not closed | `wifi/wpasupplicant/driver.go`, `cluster/core/network_commissioning_wifi.go` — what the coordinator asked of the fix, that the driver's put-back and the cluster's rejoin agree on where the radio is, is `37bfb10c`; after a failed rejoin of the committed network at a fail-safe expiry the driver still puts the radio back on the reverted, uncommitted network. Not held back: it takes an administrator's network change and an outage (the coordinator). On the fix of R2-P3-net-01 and -02 |
| R2-P3-dr-net-02 | medium | fixed `4b585d23`; Ahead `BD-chip-TCP-InboundConnectionsBounded` (amended) | `transport/tcp/listener.go`, `transport/tcp/conn.go` — at its bound a listener gives the place of the connection idle longest past the session idle timeout to a new one. One thing the fix leaves is a held-back row (N3-finnet-4). On the fix of R2-P3-ctl-lan-03 |
| R2-P3-dr-net-03 | low | fixed `1dd80820`, corrected by R2-P3-rr-dev-02 `bdd115fe`; by_design `BD-Matter-BLE-CentralWithoutPASEGivenUp` | `bridge/ble.go` — a central without a secure session or a PASE handshake in flight gives up the BLE place one handshake bound after BTP, with or without a window. On the fix of R2-P3-ble-05 |
| R2-P3-dr-net-04 | low | fixed `37bfb10c`; by_design `BD-chip-WiFiRejoinBackoff` (amended) | `contract/wifi.go` (`WiFiLinkReporter`, new and optional), `cluster/core/network_commissioning_wifi.go`, `node/wifi_driver.go` — after a failed join the server reads where the driver put the radio back. On the fix of R2-P3-net-01 |
| R2-P3-dr-net-05 | low | fixed `ea6718ce`, corrected by R2-P3-rr-dev-05 `960a7125`; by_design `BD-chip-WiFiRejoinBackoff` (amended) | `cluster/core/network_commissioning_wifi.go` — a rejoin attempt of the committed network, the first at Start included, holds the driver until it ends, through a fail-safe expiry. On the fix of R2-P3-net-01 |
| R2-P3-dr-net-06 | low | fixed `6f40740b`; by_design `BD-chip-WiFiRejoinBackoff` (amended) | `cluster/core/network_commissioning_wifi.go` — a NetworkCommissioning command cancels a running rejoin attempt and takes its turn instead of answering Busy. PLAUSIBLE, the reviewer's. On the fix of R2-P3-net-01 |
| R2-P3-dr-net-07 | low | fixed `e2491990`; by_design `BD-chip-WiFiRejoinBackoff` (amended) | `cluster/core/network_commissioning_wifi.go` — the rejoin runs only while InterfaceEnabled is true and starts again when it is written true. On the fix of R2-P3-net-01 |

### The review of the rework

One reviewer per unit read the rework after the diff review as a whole
(`dd6e5bf3..c8baf5c3`).

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P3-rr-dev-01 | high | fixed `8463567c`, `3b6b250f` (the citation) | `bridge/handlers.go`, `bridge/securechannel.go` — a Sigma3 after its handshake completed changes nothing about the established session; only the completing Sigma3's bytes over its route get the success report again, without refreshing the handshake's adapter. On the fix of R2-P3-dr-dev-02 |
| R2-P3-rr-dev-02 | medium | fixed `bdd115fe`; by_design `BD-Matter-BLE-CentralWithoutPASEGivenUp` | `bridge/ble.go` — a PASE hold older than the pairing timeout is no handshake in flight, so a central that abandoned its PASE gives up the BLE place at the next bound. On the fix of R2-P3-dr-net-03 |
| R2-P3-rr-dev-03 | low | fixed `50deec8f`; Ahead `BD-Matter-SubscriptionEndsWithoutReadablePath` (amended) | `bridge/subscribe.go` — a subscription whose subject reads nothing ends at two observations of one ACL and one set of served paths, a reassembly that changes neither notwithstanding. On the fix of R2-P3-dr-dev-01 |
| R2-P3-rr-dev-04 | low | fixed `2f3a58b7`; by_design `BD-Matter-CASE-HandshakeStateBounded` | `bridge/securechannel.go` — a Sigma1 takes the oldest handshake's record only once it began a handshake. PLAUSIBLE, confirmed by the fix's test. On the fix of R2-P3-dr-dev-03 |
| R2-P3-rr-dev-05 | low | fixed `960a7125`; by_design `BD-chip-WiFiRejoinBackoff` (amended) | `cluster/core/network_commissioning_wifi.go` — a ScanNetworks that ends leaves the driver to a rejoin attempt a fail-safe expiry started meanwhile. PLAUSIBLE, confirmed by the fix's test. On the fixes of R2-P3-dr-net-05 and -06 |
| R2-P3-rr-ctl-01 | low | fixed `5fa31e78` (the test) | `client/handshake/server_replay_test.go` — the relay of the repeated-Sigma1 test passes each message once. What the finding says about the module itself is a held-back row (N3-coord-1) |
| R2-P3-rr-ctl-02 | low | held back until its fix merges (owner decision 25) — phase 6 | — |
| R2-P3-rr-ctl-03 | low | fixed `98710b13`; Ahead `BD-chip-Commissioner-AbortDisarmsFailSafe` (amended) | `controller/commissioner/flow.go` `disarmAfterFailure` — the disarm after a failed step is bounded by the caller's context, and once that has ended by the abort bound. On N3-rwpase-1's fix `db1f78d9` |
| R2-P3-rr-ctl-04 | note | fixed `02f96a64` | `CHANGELOG.md` — the [Unreleased] line on a commissioning whose context ends now says that the disarm is owed from the PASE session's establishment and is sent before the flow's first ArmFailSafe too. On N3-rwpase-1's fix `db1f78d9` |
| R2-P3-rr-ctl-05 | note | fixed `90160568`; Ahead `BD-chip-Commissioner-AbortDisarmsFailSafe` (amended) | `controller/commissioner/connection.go` — a losing PASE session is given up beside the next address, and a run without a winner still waits for it. PLAUSIBLE, confirmed by the fix's test. On N3-rwpase-2's fix `b2437ff4` |

## What phases 0 to 2 handed to phase 3

Each role was given the held-back and the open items of its attacker and
reported their status at `8c9088ee`; an item given to several roles has one
row, with the roles that judged it. "TM" is phase 0's threat model, which is
held back as a whole; its item numbers are the roles' references, and a row
that stays held back keeps its text there. A status is the role's and was not
re-measured for this file.

| ID | role | status at the review | where it stands |
| --- | --- | --- | --- |
| R2-P2-rr-dev-08 | pase | holds, reproduced | R2-P3-pase-04, fixed `25b92e35` — held back in phase 2's table; its text is that finding's |
| N-noc-6 | lan-dev, pase | holds, reproduced against a real node | R2-P3-pase-01 and R2-P3-lan-dev-04, fixed `557eb391`; `BD-Matter-PASE-EstablishedSessionGate` withdrawn — held back in phase 2's table |
| N-ctl-1 | ctl-lan | not reproducible: the refusal's send ends with its acknowledgement wait, of which at most 16 are held, or with the 60 s lifetime of the inbound unsecured session; the two existing bound tests of `client/exchange` pass | struck: not reproducible — held back in phase 2's table |
| N-ctr-1 | lan-dev | not reproducible as a second session: a Pake3 after a completed handshake fails and installs nothing; what it did do the role counts as part of R2-P3-lan-dev-01 and -03 | R2-P3-lan-dev-01 `7254068c` and R2-P3-lan-dev-03 `afa2b964`, fixed; not run again after them — held back in phase 2's table |
| N-rw3req-3 | lan-dev, ctl-lan | — | held back until its fix merges (owner decision 25) — phase 6 |
| U-ctr-1 | lan-dev | the provider part holds, reproduced | R2-P3-lan-dev-01, fixed `7254068c`; the resumption records and the TCP transport's counters and duplicate handling stayed unread: phase 6 — held back in phase 2's table |
| U-alloc-1 | ctl-dev, ctl-lan, net | the persisted image holds, reproduced (ctl-dev); the Check-In trial decryption and the PASE refusal path not reproducible — a bounded cost per datagram, the CPU not measured (ctl-lan); the rest not looked at | R2-P3-ctl-dev-02, fixed `20c24d63`; one more part is held back until its fix merges (owner decision 25) — phase 6 |
| U-dr-ctl-1 | ctl-dev, ctl-lan | the responder's `full` and `resume` read whole: hold (both roles); `loadCache` read: a failure makes only that node unavailable; of the two walks one still recurses without a seen set, and a randomised test of 300 seeds found no cycle in the installed tree (ctl-dev) | R2-P3-ctl-dev-01 `d5a9c979`, R2-P3-ctl-lan-02 `9bef9aa7`, R2-P3-ctl-lan-03 and -04 `5ab87724`, fixed; R2-P3-ctl-lan-05, parity; the BLE channel path into `Manager.HandleDatagram` stayed untraced (ble): phase 6 — held back in phase 2's table |
| R2-P1-req2-11 | lan-dev | holds, reproduced with a control leg | R2-P3-lan-dev-07, fixed `44fcec8c` — held back in phase 1's table |
| R2-P1-dr-req-09, N-bridge2-1 (one item) | admin | holds, reproduced, and wider than the row said; the priming-specific path was not run | R2-P3-admin-03, fixed `fc8186bf` (owner decision 53) — held back in phase 1's table |
| N-req-1 | lan-dev | not reproducible: no amplification — an acknowledgement on the unsecured session is no larger than the datagram that earns it, and a secure duplicate is answered at the session's route, not at its source | struck: not reproducible — held back in phase 1's table |
| N-net-1 | lan-dev | part (a) holds, measured | R2-P3-lan-dev-06, fixed `57d75d19` — held back in phase 1's table; one more part is held back until its fix merges (owner decision 25) — phase 6 |
| N-bridge2-3 | admin | — | held back until its fix merges (owner decision 25) — phase 6 |
| N-req3-1 | admin | — | held back until its fix merges (owner decision 25) — phase 6 |
| S-1 (phase 0, a fix note's list) | lan-dev | holds, reproduced for CASE and for PASE | R2-P3-lan-dev-01, fixed `7254068c` (owner decision 51) |
| D-3 (phase 0) | ctl-lan | holds | R2-P3-ctl-lan-09, fixed `1520c5af` |
| R2-P0-bound-E01 | ctl-dev | already fixed in phase 0 (its register row), present at `8c9088ee`; not attacked again beyond reading | closed |
| R2-P0-bound-E05 (TM R5 5a.6) | ble | already fixed in phase 0 (`b06ed79d`): at the bound only the sender's own connection stalls | closed |
| R2-P0-bound-C01, TM R6.3 | net | already fixed in phase 0: the download is refused or cut at 256 MiB; not tested again | closed |
| R2-P0-sec-D01 = R2-P0-store-E06 | net | the document half already fixed: `docs/security.md` names a Wi-Fi node's network credentials among the secrets stored unencrypted | closed; sealing at rest stays phase 5 (owner decision 10) |
| U-core-1 | pase, admin | already fixed in phase 2 (R2-P2-comm-08, owner decision 47), read again: a window arms no fail-safe, the auto-arm checks and arms in one lock hold | closed |
| N-core2-1 | pase | already fixed in phase 2 (R2-P2-comm-09), read again | closed |
| phase 1, the coordinator's two additions | admin | already fixed in phase 2 (R2-P2-comm-04, R2-P2-comm-01), read, not run | closed; what the second fix left was R2-P3-admin-06, fixed `743b9bf3` |
| N-noc-4 | pase | not looked at: the host's own act, kept by the coordinator in phase 2; only the closed-window half was tested | struck in phase 2; the closed-window half is R2-P3-pase-04 |
| phase 2, the coordinator's note on the rework's controller review | ctl-lan | not reproducible, informational: a device that retries CASE under a fresh ephemeral id opens a new inbound session and is not refused by the bound on inbound handshakes | struck: nothing to test |
| TM R1 Q1 | lan-dev | holds, reproduced | R2-P3-lan-dev-01 `7254068c` and R2-P3-lan-dev-03 `afa2b964`, fixed |
| TM R1b-1 | ctl-lan | holds in part, reproduced | R2-P3-ctl-lan-02, fixed `9bef9aa7` |
| TM R1b-2 | ctl-lan | not reproducible: the Check-In part of U-alloc-1 | struck: not reproducible |
| TM R1b-3 | ctl-lan | not reproducible: the outbound ephemeral node id is drawn from `crypto/rand` in the operational range and checked for collision | struck: not reproducible |
| TM R2.1 | pase | the keying by authentication mode matches matter.js (the role's citation, not re-read); the bound on a PASE session's lifetime did not hold | R2-P3-pase-02 `9a5d600f` and R2-P3-pase-03 `7daa7ea3`, fixed; a bridge wired without the node facade was not tested: phase 6 |
| TM R2.2 | pase | not reproducible: PASE is refused after the window closes, by its timer and by `Node.CloseWindow` (the role's test) | struck: not reproducible; hosts on `bridge/` without `node` were not tested: phase 6 |
| TM R2.3 | net | holds, reproduced | R2-P3-net-01 and R2-P3-net-02, fixed `2e5c955f`, `7780fd17` |
| TM R2.4 | net | not reproducible, read, not tested: every command answers Busy while one runs, the driver bounds a join and a scan itself, an expiry clears the busy state. The cluster's connect applies no bound of its own, so a host driver without one holds the command until the fail-safe expires — a property of the host port | struck: not reproducible; since `6f40740b` a command cancels a running rejoin attempt |
| TM R2.5 | net | not reproducible, read: credential lengths are validated, the SSID is logged quoted and an AddNetwork failure by its D-Bus error name only; the repository's `TestCredentialNeverLeaves` holds | struck: not reproducible |
| TM R2.6 | net | not reproducible, read: every debug text the driver returns is a fixed string, an error name or a reason number | struck: not reproducible |
| TM R3.1, R3.2, R3.6 | admin | — | held back until its fix merges (owner decision 25) — phase 6 |
| TM R3.3 | admin | not looked at; found on the way: R2-P3-admin-02 | R2-P3-admin-02, fixed `a014460c`; one more part is held back until its fix merges (owner decision 25) — phase 6 |
| TM R3.4 | admin | the table half holds, measured | R2-P3-admin-04, fixed `22464710`; one more part is held back until its fix merges (owner decision 25) — phase 6 |
| TM R3.5 | net, admin | the second half holds (net); the first not looked at (admin) | R2-P3-net-08, fixed `11a7d3de` (docs); one more part is held back until its fix merges (owner decision 25) — phase 6 |
| TM R3.7 | admin | subscriptions are counted per fabric, so one fabric does not starve another; sessions: holds | R2-P3-admin-05, fixed `d08f0f35`; one more part is held back until its fix merges (owner decision 25) — phase 6 |
| TM R3.8 | admin | not reproducible (the role's probe: after a removal over the wire the device holds no session, subscription, resumption record or ACL of the removed index); the one thing that survived was the fail-safe. Group keys, the kept routes of bindings, persisted subscriptions and ICD registrations were not checked | R2-P3-admin-06, fixed `743b9bf3`; the unchecked parts: phase 6 |
| TM R3b.1 | admin | — | held back until its fix merges (owner decision 25) — phase 6 |
| TM R3b.2, R3b.4 | admin | — | held back until its fix merges (owner decision 25) — phase 6 |
| TM R4 Q3 | ctl-dev | already fixed for one interaction (2 MiB a session); the sum across sessions and reports was not bounded | R2-P3-ctl-dev-01 `d5a9c979` and R2-P3-ctl-dev-02 `20c24d63`, fixed |
| TM R4 Q4 | ctl-dev | — | held back until its fix merges (owner decision 25) — phase 6 |
| TM R4 Q5 | ctl-dev | the commissioning timings a device states are bounded; the session intervals a handshake states are not | R2-P3-ctl-dev-04, struck (owner decision 45) |
| TM R4 Q6 | ctl-lan | holds in part: discovery alone does not replace the host a session proved | R2-P3-ctl-lan-07, fixed `5801514d`; R2-P3-ctl-lan-05, parity |
| TM R4 Q7 | ctl-dev | related: R2-P3-ctl-dev-05; `checkOnboarding` itself not read | phase 5, with R2-P3-ctl-dev-05 |
| TM R5 5a.1 | ble | not looked at: documented as accepted — `docs/security.md`, the passcode row: the budget and the lockout are the bridge's, not a transport's | struck: documented |
| TM R5 5a.2 | ble | holds, reproduced | R2-P3-ble-05, fixed `4d9f424e`, `1dd80820`, `bdd115fe` |
| TM R5 5a.3 | ble | not reproducible: a repeated handshake request on an open session restarts the handshake timer, whose end drops the connection (the role's guard test passes) | struck: not reproducible |
| TM R5 5a.4 | ble | holds, as in matter.js (the role's citation, not re-read); part of R2-P3-ble-05, and R2-P3-ble-03 a variant of it | fixed `59f4fa23`, `1dd80820` |
| TM R5 5a.5 | ble | — | held back until its fix merges (owner decision 25) — phase 6 |
| TM R5 5a.7 | ble | not reproducible, traced (the Linux driver): a refused second central is disconnected and forgotten | struck: not reproducible |
| TM R5 5a.8 | ble | not reproducible: the bridge serves C3 empty, and the read bounds its offset | struck: not reproducible |
| TM R5 5b.1 | ble | holds: reproduced for the scanner, traced for the BlueZ central | R2-P3-ble-02 `d1d42547` and R2-P3-ble-04 `279dc842`, fixed; one part is a held-back row (N3-disc-5) |
| TM R5 5b.2 | ble | holds in part, reproduced; the half about the genuine device's PASE budget is not reproducible | R2-P3-ble-01, fixed `5345b4ec`, `c8e69f85` (owner decision 55) |
| TM R5 5b.3 | ble | not reproducible: renegotiation happens at most once per channel | struck: not reproducible |
| TM R5 5b.4 | ble | — | held back until its fix merges (owner decision 25) — phase 6 |
| TM R6.1 | net | holds for the OTA download, reproduced; for the CRL read only | R2-P3-net-06, fixed `343f8fd6`; one part is a held-back row (N3-net-4) |
| TM R6.2 | net | — | held back until its fix merges (owner decision 25) — phase 5 |
| TM R6.4 | net | — | held back until its fix merges (owner decision 25) — phase 6 |
| TM R6.5 | net | the count: holds, reproduced; the byte handling read and found sound, scan results are not logged | R2-P3-net-04, fixed `b5d2b533` |
| TM R6.6 | net | not looked at: inherent to joining by SSID and credential, not testable in process (the role) | struck: inherent; its consequence for a join that is not committed was R2-P3-net-02 |
| TM R6.8 | ctl-lan | names and records are bounded since phase 2; the staged addresses were not | R2-P3-ctl-lan-01, fixed `48e31f29` |
| TM R6.9 | net | — | held back until its fix merges (owner decision 25) — phase 6 |
| TM R6.10 | net | holds, reproduced | R2-P3-net-07, fixed `343f8fd6` |
| TM R7.5 | net | the net role's half holds (the second test of R2-P3-net-02) | fixed `7780fd17` where the rejoin of the committed network succeeds — a successful join leaves one network configured (`BD-chip-WpaSupplicantDriver`, its seventh departure; inferred from that rule, not run); where it fails, R2-P3-dr-net-01: phase 5 |

## The owner's decisions of this phase

Decisions 51 to 57 were taken on open items of this phase and were one
held-back line of `decisions.md` while their fixes were under way; since
`0bb6a298` they are rows there, with their questions. The table says where
each went.

| # | on | decided | where |
| --- | --- | --- | --- |
| 51 | R2-P3-lan-dev-01 | now, announced: the providers of `bridge` are keyed by initiator and exchange | `7254068c`; the break is in `f85e8cbc`'s CHANGELOG lines |
| 52 | R2-P3-admin-01 | the hold of a chunked write is taken only after the access check, and per fabric for a fabric-scoped attribute; a `by_design` divergence; decision 30 is changed | `5a53e37e`; `BD-chip-ChunkedWritesBounded` |
| 53 | R2-P3-admin-03 | connectedhomeip's rule at the quota, read at the pin and mirrored, and a subscription whose subject has no readable path left ends; back to the owner if the rule frees no place for a legitimate controller | `fc8186bf`, `9199d9ec`, `50deec8f`; N3-acl-4 |
| 54 | R2-P3-ctl-dev-02 | a structural rule and a configurable quota: the image stores only what the device's own structure names or the request asked for, behind it a quota per node with a default that is chosen, not sourced | `20c24d63`, `f59dfc60` |
| 55 | R2-P3-ctl-lan-06 = R2-P3-ble-01, what the first fix left | a capped stagger and rotating places: a ceiling on a candidate's delay, a cap on the attempts that run together, and a new candidate ends the oldest attempt still unanswered; both numbers chosen | `c8e69f85` |
| 56 | the total bound on inbound TCP connections (R2-P3-ctl-lan-03) | configurable, a default per role: 4 on the device, connectedhomeip's number, and 256 on the controller, chosen | `2aed8e2a` |
| 57 | the ServerList half of decision 54 | matter.js's tolerance stays (`ClientStructure.ts:654-659`, the fix note's reading); decision 54 is narrowed to the PartsList rule and the quota | R2-P3-ctl-dev-02's row |

## Noticed by the fix agents, the citation passes and the coordinator, not fixed where it was noticed

No severity: none of these was reviewed or verified as a finding. The ids are
this register's (`N3-<group>-<n>`, the group being the fix note's); phases 1
and 2 used `N-…` for theirs.

| ID | noticed in | what | disposition | where |
| --- | --- | --- | --- | --- |
| N3-gc-1 | — | — | held back until its fix merges (owner decision 25) — phase 4 | — |
| N3-gc-2 | `phase-3-fix-gc.md` | tests that wait with `time.Sleep`, there before this branch | phase 5: test quality | `node/issue87_test.go`, `node/commissioning_blocked_test.go` |
| N3-gc-3 | `phase-3-fix-rwtcp.md`; the coordinator's list | `core.AutoArmOnPaseEstablished` was deprecated by R2-P3-pase-03's fix and named nowhere in `CHANGELOG.md`, so `TestDeprecatedIdentifiersAreAnnounced` failed | fixed `f85e8cbc` | `CHANGELOG.md`, `README.md` |
| N3-acl-1 | `phase-3-fix-acl.md` | the ports page gave Operate as the write privilege of an attribute whose server states none; the dispatcher takes the data model's as the floor | fixed `ea3289d8` | `docs/guides/ports.md` |
| N3-acl-2 | `phase-3-fix-acl.md` | every admission at the quota logged a refusal at warn for the attempt before the eviction | fixed `3264a9dd` | `bridge/subscribe_dispatch.go` |
| N3-acl-3 | `phase-3-fix-acl.md`, `-rwhs.md` | the walk that decides whether a subject reads nothing ran before every report; the rework group read it and found nothing to fix, the diff review found it understated | R2-P3-dr-dev-06, fixed `9199d9ec` | `bridge/subscribe.go` |
| N3-acl-4 | `phase-3-fix-acl.md`; the coordinator's list | connectedhomeip's rule at the quota gives no share per peer inside a fabric: the requesting fabric's oldest subscription goes, whoever holds it; the remedy is the fabric's own, removing the subject's ACL entry | by_design `BD-Matter-TimedAndQuotaDeferred` says so, with the remedy | `im/subscription`, `bridge/subscribe.go` |
| N3-hs-1 | `phase-3-fix-hs.md` | the handshakes this node opens found their replies by the exchange id alone — the initiator's side of R2-P3-lan-dev-01 | fixed `5ad79b19` | `bridge/case_initiator.go` — a CASE handshake this node opens takes only replies addressed to its ephemeral node id |
| N3-hs-2 | `phase-3-fix-hs.md` | an acknowledgement on the unsecured session was matched by its counter alone | fixed `1e4c7994` | `bridge/outbound_reliable.go`, `bridge/receive.go` — it clears only a message of its own handshake |
| N3-hs-3 | `phase-3-fix-hs.md` | the diagnostic event of a completed handshake was recorded for a Sigma3 the adapter refused as well | fixed `3776a519` | `bridge/securechannel.go` |
| N3-hs-4 | `phase-3-fix-hs.md` | the eviction of a CASE responder ended the connection hold of its exchange id on every connection | fixed `805a2ff7` | `bridge/conn.go` — of that initiator's handshake only |
| N3-hs-5 | `phase-3-fix-hs.md`; the coordinator's list | `TestSubscribeAttributesToTheSessionPeerNotTheHeader` timed out once in a full parallel run | struck: not reproduced — 150 runs, 3000 instances and 16 package runs of a race-enabled binary without a failure (`phase-3-fix-rwhs.md`); the cause is not established | `bridge/subscribe_peer_identity_test.go` |
| N3-net-1 | `phase-3-fix-net.md` | `MatterInvoke` checked the busy state and set it in two lock holds, so two commands could pass the check together | phase 6; not read again after `ea6718ce` and `6f40740b` reworked the driver's turn | `cluster/core/network_commissioning_wifi.go` |
| N3-net-2 | — | — | held back until its fix merges (owner decision 25) — phase 4 | — |
| N3-net-3 | `phase-3-fix-net.md` | a network configured in wpa_supplicant that is not the current one is neither removed nor reused by `addNetwork`, so networks can accumulate until wpa_supplicant restarts — unverified on a real supplicant | phase 6 | `wifi/wpasupplicant/driver.go` |
| N3-net-4 | — | — | held back until its fix merges (owner decision 25) — phase 5 | — |
| N3-disc-1 | `phase-3-fix-disc.md` | `Names.Get` replays staged addresses without the overflow eviction, so the cache can stand up to five records over its bound until the next packet | phase 6 | `client/discovery/names.go` |
| N3-disc-2 | — | — | held back until its fix merges (owner decision 25) — phase 6 | — |
| N3-disc-3 | `phase-3-fix-disc.md`; the coordinator's list | a BTP session's send queue had no bound — the optional part of R2-P3-ble-03 | fixed `37b2ac49`; Ahead `BD-Matter-BTP-SendQueueBounded` | `transport/btp/session.go` — at most 255 messages, a further one refused; the number is chosen, neither reference has a limit (the fix note) |
| N3-disc-4 | `phase-3-fix-disc.md` | `withdrawBLE`'s comment no longer matched `bridge.WithdrawBLE` | fixed `93124480` | `node/commissioning.go` |
| N3-disc-5 | — | — | held back until its fix merges (owner decision 25) — phase 6 | — |
| N3-ctl-1 | `phase-3-fix-ctl.md` | the controller's served reads are still assembled whole before they are chunked, where the bridge streams (owner decision 44); bounded now by the path dedup with the node filter | phase 5 — the open part of R2-P3-ctl-dev-03 | `client/interaction/server.go` |
| N3-ctl-2 | `phase-3-fix-ctl.md` | `AllocateSessionID` still closes the least recently used session of any peer once the id space is full; with the cap per peer one device no longer fills it | phase 6 | `client/exchange/manager.go` |
| N3-ctl-3 | — | — | held back until its fix merges (owner decision 25) — phase 6 | — |
| N3-rwhs-1 | `phase-3-fix-rwhs.md`; the coordinator's list | `TestNodeServesPASEReadAndSubscription` failed twice run alone, a PASE that did not answer for 11 s | struck: it passes 4 of 4 after `db1f78d9`; that N3-rwpase-1 was its cause is inferred, not established | `node/node_test.go` |
| N3-rwhs-2 | — | — | held back until its fix merges (owner decision 25) — phase 4 | — |
| N3-rwpase-1 | the coordinator's integration run; `phase-3-fix-rwpase.md` | `TestPairTermsRequired` failed 3 of 3 at `d08f0f35`: with R2-P3-pase-01 fixed, this module's commissioner still left its PASE session behind when it gave up early | fixed `db1f78d9`, corrected by R2-P3-rr-ctl-03 `98710b13`; Ahead `BD-chip-Commissioner-AbortDisarmsFailSafe` (amended) | `controller/commissioner/flow.go`, `commissioner.go` — a commissioning that gives up after PASE expires the fail-safe the PASE session armed, so the device accepts the next PASE at once. |
| N3-rwpase-2 | `phase-3-fix-rwpase.md` | two more exits closed a device's PASE session without disarming it: a session that lost the connection's race, and `EstablishPASE` when `ContinueAfterPASE` refuses | fixed `b2437ff4`, corrected by R2-P3-rr-ctl-05 `90160568` | `controller/commissioner/connection.go`, `commissioner.go` |
| N3-rwpase-3 | `phase-3-fix-rwpase.md` | several matter.js citations of `flow.go` that carry no commit are one line off against `db021da0`; they belong to the older pin the file's header names | phase 4: owner decision 13; not changed here | `controller/commissioner/flow.go` |
| N3-rwrace-1 | — | — | held back until its fix merges (owner decision 25) — phase 6 | — |
| N3-rwrace-2 | — | — | held back until its fix merges (owner decision 25) — phase 6 | — |
| N3-finctl-1 | `phase-3-fix-finctl.md`; the coordinator's runs | one unidentified failure of `go test ./client/handshake/...` in nine runs | fixed `5fa31e78`, `081a7141` | `client/handshake/server_replay_test.go`, `pase_zeroise_test.go` — two tests that failed under repetition: 9 of 200 runs before and 0 of 400 after, 30 of 160 before and 0 of 160 after (the coordinator's measure). The first is R2-P3-rr-ctl-01 |
| N3-finctl-2 | `phase-3-fix-finctl.md` | after a first transmission the socket refused, `sentToAck` stays set, so `HasUnackedMessage` answers true for a message that never left; harmless where it is read today | phase 6 | `client/exchange/exchange.go` |
| N3-finctl-3 | `phase-3-fix-finctl.md` | a losing PASE session is disarmed without a check that it does not reach the winner's device | struck: accepted by the coordinator — a device accepts one PASE at a time | `controller/commissioner/connection.go` |
| N3-findev-1 | — | — | held back until its fix merges (owner decision 25) — phase 6 | — |
| N3-findev-2 | `phase-3-fix-findev.md` | `PerExchangeCaseProvider.Forget` calls no eviction hook, so a completed handshake's Sigma1 record outlives an adapter a host forgets, until the bound of 256 evicts it; traced | phase 6 | `bridge/case_provider.go` |
| N3-findev-3 | `phase-3-fix-findev.md` | `TestSubscriptionEndsWhenItsSubjectReadsNothing` failed once at `6abf136a` beside the new chunked test; not reproduced by the fix agent alone or after `9199d9ec` | fixed `6d6cf533` — the test and the message of N3-coord-5; that it was the same cause is inferred | `fabrictest/subscription_quota_test.go` |
| N3-findev-4 | `phase-3-fix-findev.md` | the new chunked-write test holds its exchange open between the chunks, about one second of wall time | phase 5: test quality | `fabrictest/subscription_quota_test.go` |
| N3-finimg-1 | `phase-3-fix-finimg.md` | `image_cut_at_load` is logged at every load while the stored image is above the quota, also when the cut drops nothing | phase 6 | `controller/client/image_bounds.go` |
| N3-finimg-2 | `phase-3-fix-finimg.md` | `TestStoredImageBeyondQuotaIsCutBackAtLoad` holds with a thin margin under the new charges | phase 5: test quality | `controller/client/image_bounds_internal_test.go` |
| N3-finimg-3 | `phase-3-fix-finimg.md` | the quota check still sums every cluster and walks the PartsLists for each report — at most about 14,600 clusters under the default quota; a running total was the finding's second proposal | phase 6 | `controller/client/image_bounds.go` |
| N3-finimg-4 | the coordinator's list | `CHANGELOG.md` and the entry `BD-Matter-Controller-NodeImageBounded` described the quota's first measure and the first ask rule, from before `f59dfc60` and `0376473d` | fixed `02f96a64`, `97850231` | `CHANGELOG.md` — the Added and the Changed line name the charges, the ask for a path that names only an attribute and the cut that erases only what it drops; `notes/parity/by_design.md` — the row and the entry amended |
| N3-finnet-1 | `phase-3-fix-finnet.md` | a write of InterfaceEnabled false is accepted and stored; connectedhomeip answers InvalidAction when the driver implements no `SetEnabled` (the fix note's citations, to verify) | phase 4: parity | `cluster/core/network_commissioning_wifi.go` |
| N3-finnet-2 | `phase-3-fix-finnet.md` | a ConnectNetwork whose fail-safe expired during its driver call returns without clearing the busy state and relies on the expiry having cleared it; it holds, the coupling is implicit | phase 6 | `cluster/core/network_commissioning_wifi.go` |
| N3-finnet-3 | `phase-3-fix-finnet.md` | a dialled connection records no traffic; only the listener measures it today | phase 6 | `transport/tcp` |
| N3-finnet-4 | — | — | held back until its fix merges (owner decision 25) — phase 6 | — |
| N3-fixrrctl-1 | `phase-3-fix-fixrrctl.md` | after a failed flow the peer is deleted on a context without the caller's cancellation and without a deadline; whether that delete can wait on the network was not checked | phase 6 | `controller/commissioner/commissioner.go:639` |
| N3-fixrrdev-1 | — | — | held back until its fix merges (owner decision 25) — phase 6 | — |
| N3-fixrrdev-2 | `phase-3-fix-fixrrdev.md` | over a connection, an answered Sigma3 of a completed handshake releases the handshake hold of its exchange id on the sender's own connection, as an unbegun Sigma3 did before this branch | phase 6 | `bridge/conn.go` `settleHandshake` |
| N3-fixrrdev-3 | the coordinator's runs; `phase-3-fix-fixrrdev.md` | a peer's failure StatusReport that ends a PASE handshake was never acknowledged, so the peer retransmitted until MRP gave up; there since the module's first commit | fixed `d78b583f` | `bridge/ackpump.go` — a peer's acknowledgement no longer discharges the acknowledgement the bridge owes; `MRPAckAdapter` is deprecated |
| N3-cite-1 | the citation passes | `client/discovery/session_kinds.go` cites matter.js at `862ad75f` and at `db021da0` side by side; the bare range for `registerAttempt` (`:81-147`) is `:81-151` at the pin. More citations at `862ad75f` stand in `client/discovery` | phase 4: owner decision 13; one citation re-pinned by `cc6d4d59`, the rest not changed | `client/discovery` |
| N3-cite-2 | the first citation pass | matter.js answers any other secure-channel message that opens an exchange with a status response error, InvalidAction (`SecureChannelProtocol.ts:152-153`, `:43-49`, the pass's reading); this module's handlers for a handshake that was not begun answer a failure StatusReport, InvalidParameter. What matter.js puts on the wire was not read | phase 4: parity | `bridge/securechannel.go` |
| N3-cite-3 | the second citation pass | three citations of `secure/operational/manager.go` the pass corrected should leave `script/citecheck/drift.json`, which the pass did not regenerate; thirteen further drifted citations of that file are untouched | fixed `58d60b96`; phase 4: the thirteen (owner decision 13) | `script/citecheck/drift.json` — regenerated: the three left it (the file's entries went from 16 to 13), and two citations this branch added entered it, both read as holding by the first pass (`client/handshake/server_test.go`, `client/interaction/server.go`), so the drift count and its ratchet went from 404 to 403. The same commit regenerates the reachability snapshot |
| N3-cite-4 | the second citation pass | the limits page and a comment said the fabric's oldest subscription ends; connectedhomeip's candidate is one over the per-fabric path share first, the oldest after — the module keeps no path pool, which leaves the oldest | fixed `b0b7e8e0` | `docs/guides/limits.md:52` — the row names connectedhomeip's order and that this module keeps no path pool, as the by_design entry does. The comment in `im/subscription/manager.go` states the module's rule and was not changed |
| N3-cite-5 | the second citation pass | two statements of the limits page about the references carry no line and were not verified | phase 4 | `docs/guides/limits.md:106`, `:114` |
| N3-coord-1 | — | — | held back until its fix merges (owner decision 25) — phase 6 | — |
| N3-coord-2 | the coordinator's integration run | `TestClientNodeTcpParity` failed once in a full parallel run, a port picked and bound again; it passed alone 3 of 3 | phase 5: test quality | `controller/client_node_tcp_parity_matterjs_test.go` |
| N3-coord-3 | the coordinator | two tests still used the calls this branch deprecated | fixed `33b18716` | `bridge/subscription_resume_e2e_test.go`, `examples/reference-bridge/wiring_test.go` |
| N3-coord-4 | the coordinator's list | register rows owed for what was fixed after `2155ad38`: the race's two numbers, the BTP send queue, the commissioner's disarm on every failure after PASE, the bound on a BLE central without PASE, the TCP listener's replacement rule, the stake calls | fixed `97850231` | `notes/parity/by_design.md` — two Ahead rows with their entries (`BD-Matter-Controller-CommissioningRaceBounded`, `BD-Matter-BTP-SendQueueBounded`), three more entries (`BD-Matter-CASE-HandshakeStateBounded`, `BD-Matter-BLE-CentralWithoutPASEGivenUp`, `BD-Matter-Controller-ResumedSessionCountedAtRegistration`), and nine entries and four rows amended, `BD-chip-Commissioner-AbortDisarmsFailSafe`, `BD-chip-TCP-InboundConnectionsBounded` and `BD-Matter-CASE-IDExhaustionEvictsPlaceholdersOnly` among them |
| N3-coord-5 | the coordinator's guards on `3b6b250f` | `TestSubscriptionEndsWhenItsSubjectReadsNothing`, a test this branch added with `fc8186bf`, failed in the race run and under load: 77 of 324 runs under the stress recipe with the race detector (the coordinator's measure). One step of a minute on the fake clock fired the keep-alive's timer and, in the same advance, every retransmission timer armed behind it (`clock/clocktest/fake.go:205-212`), so the device gave its subscriber up as unreachable (the coordinator's reading of a failing run's log) | fixed `6d6cf533` | `fabrictest/subscription_quota_test.go` — the quota tests let the clock pass under the quiet protocol (`letPass`, over `Site.Eventually`), and the repaired test's subscriptions ask for a ceiling of five seconds; 0 of 300 runs of the two changed tests failed after (the coordinator's measure). No product code changed |
| N3-coord-6 | the coordinator's guards on `3b6b250f` | `TestDeprecatedIdentifiersAreAnnounced` was red again: `Manager.ReleaseID`, `MRPAckAdapter` and `NewMRPAckAdapter`, deprecated by `2299af59` and `d78b583f`, were in `README.md`'s table and not in `CHANGELOG.md` | fixed `02f96a64` | `CHANGELOG.md` — the two Deprecated lines; the same commit brings the lines for what was fixed after `f85e8cbc`. Green after it is the notes pass's run, not repeated here |

## Unread

What each reviewer said it did not attempt, read or trace, summarised per
unit; all of it is phase 6 (the full pass reads every hand-written package
again). The list is the reviewers', not re-measured here. What a role
attempted without a finding is in the result file (96 entries over the seven
roles) and not repeated.

| Unit | area | not read | phase |
| --- | --- | --- | --- |
| lan-dev | an unauthenticated host on the device's link | group-typed datagrams; the mDNS responder's state for truncated queries; CASE resumption and its record table; the TCP transport's counters and duplicates; secure datagrams under session ids the sender guesses; handshake opcodes inside an established session (needs a session); no run over a real socket for R2-P3-lan-dev-01 — its figures are in-process. More of this unit is a held-back row (U3-lan-dev-1) | 6 |
| pase | a holder of the passcode | an end-to-end run of AddNOC over PASE, CASE on the pending fabric and the fail-safe's expiry; the start-up rollback after a restart; NetworkCommissioning for Wi-Fi and Thread, TimeSynchronization and ICD registration over PASE; timed and chunked interactions over PASE; hosts that wire `bridge/` without the node facade; the Wi-Fi driver; a wire-level reproduction of R2-P3-pase-03, whose timing is inferred | 6 |
| admin | another fabric's administrator | node-target bindings; the writable lists other than Binding and UserLabel; the Thread border router and NetworkCommissioning across fabrics; the OTA requestor's providers across fabrics; ICD registrations across fabrics; group messaging between fabrics — no group message was sent; UpdateNOC and AddNOC over CASE on the wire; a request in flight at its fabric's removal; persisted subscriptions after a restart; the fabric-scoped state of DoorLock, Thermostat, CommissionerControl, TLS, cameras, JointFabric, Messages and TimeSynchronization; the event log across fabrics. More of this unit is a held-back row (U3-admin-1) | 6 |
| ctl-dev | a hostile device against the controller | eviction across peers at a full session id space, traced only; the camera's Push AV and SDP paths, topology and diagnostics; chunked writes and the invoke batcher against hostile responses; the attestation policy, an owner question the role did not raise; the commissioner's store after a late failure. More of this unit is a held-back row (U3-ctl-dev-1) | 6 |
| ctl-lan | an unauthenticated host on the controller's link | the controller's own mDNS responder; the discovery solicitor; the Thread border-router registry; the controller's OTA provider and served IM (need CASE); no race-detector run of the role's tests. More of this unit is a held-back row (U3-ctl-lan-1) | 6 |
| ble | a hostile BLE peer | a bridge-level measurement of R2-P3-ble-03; the ESPHome proxy's decoders; the Linux-tagged BlueZ test, compiled and vetted only; a peripheral that knows the passcode; the controller's BLE channel against the new bounds; connectedhomeip's `SetUpCodePairer`, the reference for R2-P3-ble-01. More of this unit is a held-back row (U3-ble-1) | 6 |
| net | a hostile Wi-Fi, DCL or OTA peer | a retry test for the rejoin at `Node.Start`; tests of the BDX variants of R2-P3-net-03; `thread/otbr` beyond `client.go`'s bounds, `cluster/threadbr`, `thread/brdiscovery`; `controller/softwareupdate` against a node that misreports; `bdx/send.go`, `bdx/sink.go` and the SendInit branch of `bdx/serve.go`; the ACL of the controller's served provider endpoint; a process death between the Wi-Fi list's write and the fabric's commit; `wifi/wpasupplicant` against a real bus; wpa_supplicant's own limit on its BSS table. More of this unit is a held-back row (U3-net-1) | 6 |
| dr-dev | the first wave's diff, device side | the unit's test files but for seven of 42, which were skimmed; `general_commissioning.go` beyond the fail-safe arm, the expiry, `FabricRemoved` and `ArmOnPaseEstablished`; `secure/operational/manager.go` beyond the changed functions; the constructors and setters of `bridge/handlers.go`; the list-append path behind the Binding bound; report generation in `bridge/subscribe.go`; six files by their hunks only; the probe for a privileged attribute served below its privilege ran for two node configurations, not for a Wi-Fi node or the camera clusters; event read privileges and command privileges; the two handshake items then in rework (N3-hs-1, -2); no race-detector run; the docs pages beyond their diffs | 6 |
| dr-ctl | the first wave's diff, controller side | `names.go` before line 268 and the solicitor; `FindInstance` and the continuous kind of `session_kinds.go`, the race against `controller/pair_race.go`; the inbound dispatch of `client/exchange/manager.go`; `client/peer/connection.go` beyond the diff and three functions; `structure.go` from `resolvePartClaims` on; `secure/sigma`'s `ProcessSigma1WithResume`; two test files; the limits and ports rows against the code; connectedhomeip's responder before the first decrypted message | 6 |
| dr-net | the first wave's diff, Wi-Fi, TCP, BLE and DCL | `ble/bluez/central_linux.go` by its diff, not run (Linux only); `wifi/wpasupplicant` against the fake bus only; the scanner beyond the diff; whether a central built on matter.js ever repeats an ack number; `controller/dcl` by its diffs and the touched functions; `ota_requestor*.go`, which the unit's file list did not change; the tests, skimmed | 6 |
| rr-dev | the rework, device side | the tests beyond what a finding needed (its report lists the files); `tryResume` of `secure/sigma/protocol.go` beyond the changed lines; `transport/tcp/doc.go` | 6 |
| rr-ctl | the rework, controller side | three test files, and two more in part; the two give-up tests read whole, their timing under load not run | 6 |
| U3-lan-dev-1 | unit lan-dev | — | held back until its fix merges (owner decision 25) — phase 6 |
| U3-admin-1 | unit admin | — | held back until its fix merges (owner decision 25) — phase 6 |
| U3-ctl-dev-1 | unit ctl-dev | — | held back until its fix merges (owner decision 25) — phase 6 |
| U3-ctl-lan-1 | unit ctl-lan | — | held back until its fix merges (owner decision 25) — phase 6 |
| U3-ble-1 | unit ble | — | held back until its fix merges (owner decision 25) — phase 6 |
| U3-net-1 | unit net | — | held back until its fix merges (owner decision 25) — phase 6 |

## The citation passes

Three passes read citations of matter.js and connectedhomeip in the exported
trees of the pins. The first two read every citation that a wave of the branch
added or changed in a Go file or a docs page — `8c9088ee..dd6e5bf3` and
`dd6e5bf3..c8baf5c3` — and the fix agents' lists of new citations. Their
corrections are the nine commits "the citation says what the lines say"
(`e6b6db9b`, `b4d4f0a7`, `75a8067f`, `2bbad254`, `2a67ab39`; `89a604a9`,
`cc6d4d59`, `0bf52826`, `b08ce185`): comment text and lines of docs pages, 43
lines added and 35 removed in 14 files.

- The first pass, by its own count: 125 rows — 113 hold, 10 gave lines that
  did not hold the claim (six distinct citations), 1 said more than its lines
  (a comment that a request at the quota is never refused for want of room;
  corrected), 1 was not performable (a bare range beside a citation at another
  commit, `862ad75f`; left). No citation was found on which code rests
  falsely. Two of the fix agents' own corrections of line numbers were off at
  the pin and the originals nearer (the Binding table's constant and the
  Binding list's constraint).
- The second pass, by its own count: 44 hold, 12 gave lines that were
  corrected, 1 was wrong in its lines and its wording
  (`secure/operational/manager.go` on when matter.js takes a session id — the
  cited line is another function, and matter.js takes the id for Sigma2,
  `CaseServer.ts:266`, the pass's reading; corrected), 1 stood at the other
  commit and was re-pinned, 1 statement had no citation to read. The verdict
  column of its table reads 44, 10, 2 mixed, 1, 1 and 1 over 59 rows.
- What the first two passes left is N3-cite-1 to -5; two of the five are
  settled since (N3-cite-3 in part, N3-cite-4).
- The third pass, the coordinator's, read the twelve commits after the second
  pass's own (`b08ce185..3b6b250f`): the lines those commits add to a Go file
  that carry a line number of a `.ts`, `.cpp` or `.h` file — four lines, six
  ranges in three files (`bridge/ackpump.go`, `bridge/securechannel.go`,
  `controller/commissioner/flow.go`), each read at `db021da0`, each holds; no
  correction. Its limit, as it states it: a line number that stands on a line
  without one of the three patterns was not found, and a statement about a
  reference without a line number was not read. `3b6b250f`, the last of the
  twelve, is itself the correction of one citation (R2-P3-rr-dev-01).
- The notes pass's three commits (`02f96a64`, `97850231`, `b0b7e8e0`) add 85
  citations on the lines they write in `CHANGELOG.md`, `by_design.md` and the
  limits page. They are their author's reading; the sources name no citation
  pass over them. `6d6cf533` adds none.
- Re-read for this file, in the exported trees: matter.js
  `PeerConnection.ts:703-707`, `CaseServer.ts:211-216`,
  `SessionManager.ts:312-318` and `:699-726`; connectedhomeip
  `CASESession.cpp:2315-2340` and `PairingSession.cpp:62-66` — the lines of
  the three parity rows — and the third pass's six ranges
  (`MessageExchange.ts:501-511`, `:536-542`, `:553-560`,
  `CaseServer.ts:95-98`, `ControllerCommissioningFlow.ts:453-456`,
  `:463-465`). All hold; one range a fix note gives
  (`SessionManager.ts:697-725`) begins two lines above the function.

## Counts

Generated from the rows above, at `58d60b96` (`fixed`, `parity`, `struck`,
`phase N` and held back are read off the head of each disposition).

The 76 findings, by their first disposition:

| disposition | the seven roles | the diff review | the review of the rework | all |
| --- | --- | --- | --- | --- |
| fixed | 39 | 19 | 9 | 67 |
| parity with matter.js, no change | 2 | 1 | 0 | 3 |
| struck | 3 | 0 | 0 | 3 |
| open: phase 5 | 1 | 1 | 0 | 2 |
| held back | 0 | 0 | 1 | 1 |
| all | 45 | 21 | 10 | 76 |

By the severity their rows carry: 7 high, 16 medium, 47 low, 6 note. 5 rows
name another row as the same defect.

Of the 67 fixed: 14 were corrected by a finding of a later review, 25 name an
Ahead row, 21 a `by_design` entry that is new, amended or withdrawn, and 4
leave one part open in a later phase (R2-P3-admin-04, R2-P3-ctl-dev-03,
R2-P3-net-01, R2-P3-net-02).

What phases 0 to 2 handed over: 68 rows — 31 fixed on this branch, 17 struck,
7 closed before this phase, 1 phase 5, 12 held back; 6 of the public rows
carry a part that stays held back.

The 61 noticed items: 22 fixed, 1 a `by_design` entry, 3 struck, 5 phase 4, 5
phase 5, 12 phase 6, 13 held back.

Unread: 12 units, phase 6, and 6 held-back rows.

Rows written in `by_design.md` (`git diff 8c9088ee..58d60b96`, two commits,
`2155ad38` and `97850231`): 12 Ahead rows with their entries, 8 more entries
(`BD-Matter-Controller-ResponderSessionsPerPeer`,
`BD-Matter-Controller-ResumedSessionCountedAtRegistration`,
`BD-Matter-CASE-HandshakeStateBounded`,
`BD-Matter-BLE-CentralWithoutPASEGivenUp`, `BD-chip-BTP-RepeatedAckRefused`,
`BD-chip-BLE-WindowEndDisconnectsCentral`, `BD-chip-WiFiRejoinBackoff`,
`BD-chip-WiFiScanResponseBounded`), 1 entry withdrawn
(`BD-Matter-PASE-EstablishedSessionGate`), and changed text under 8 entries
that were there before — `BD-chip-ChunkedWritesBounded` and
`BD-chip-Commissioner-AbortDisarmsFailSafe`, each with its Ahead row,
`BD-Matter-TimedAndQuotaDeferred`,
`BD-Matter-CASE-IDExhaustionEvictsPlaceholdersOnly`,
`BD-Matter-PeerBusyDeadline`, `BD-Matter-OTARequestor-HTTPS`,
`BD-chip-WiFiNetworkCommissioningServer`, `BD-chip-WpaSupplicantDriver`. Of
the two commits' own counts, `2155ad38`'s six amended entries are seven in the
diff; `97850231`'s — two Ahead rows, five entries, nine entries and four rows
amended — are what its diff has, six of the nine being entries the first
commit wrote. `notes/parity/matter_behaviour_findings.md` did not change.

**Held back: 32 rows** — 1 finding (R2-P3-rr-ctl-02), 12 rows of what phases 0
to 2 handed over, 13 noticed items and 6 unread parts; 3 in phase 4, 2 in
phase 5, 27 in phase 6. 6 more rows are public and carry a part that is held
back. Of the 8 rows phase 2 held back, 7 are public with this branch
(R2-P2-rr-dev-08, N-noc-6, N-ctl-1, N-ctr-1, U-ctr-1, U-dr-ctl-1, and
U-alloc-1 but for two parts) and N-rw3req-3 stays; of the 7 phase 1 still
held, 5 are public (R2-P1-req2-11, R2-P1-dr-req-09, N-bridge2-1, N-req-1, and
N-net-1 but for one part) and N-bridge2-3 and N-req3-1 stay.

## Not placed, not verified

- The mutation sampling (phase 0's mechanical evidence) ran on `940c99b7`,
  phase 2's head, not on this branch. For `cluster/core`'s ACL and OpCreds
  files, `commissioning`, `im` and `endpoint` it finished with the figures
  phase 2's register gives; for `bridge` the summary log has two finished
  blocks — 121 and 151 in the "SURVIVED SURVIVED" column, 200 and 250 killed —
  and a third block of 261 mutants without an exit line when this file was
  written. What the log's labels mean it does not say; the survivors are phase
  5 material.
- Held back by the register editor's judgement, where the coordinator's list
  does not say: N3-gc-1, N3-net-2, N3-net-4, N3-disc-2, N3-disc-5, N3-rwhs-2,
  N3-findev-1, N3-fixrrdev-1; the six unread rows U3-…-1; and, of what phases
  0 to 2 handed over, every row and part that is not closed — the threat
  model's items among them, because the threat model is held back as a whole.
  Public by the same judgement although open: every other noticed row placed
  in a later phase. The coordinator may move either kind.
- Not put to the owner, though a source asks for it: that connectedhomeip's
  rule at the quota gives no share per peer (decision 53's proviso; N3-acl-4 —
  the by_design entry states it, whether the owner was told is not in the
  sources); the default of the image quota after its measure changed (32 MiB
  admits about 2000 bridged plugs, the coordinator's figure); the decision
  R2-P3-ctl-dev-05 needs; and what two held-back rows ask. Chosen by a fix
  agent without a reference line, and recorded as chosen in a row or a docs
  page, not as an owner decision: the 8192 records of the BLE scanner, the 64
  slots of slack in the session id placeholder queue, the Wi-Fi rejoin backoff
  of 5 s doubling to 5 min, 4 CRL downloads at once and 10 s per lookup, the
  255 messages of a BTP send queue, the charges of the image quota (measured
  on one platform, rounded). Decided by the owner: the image quota as such
  (54), the race's two numbers (55), the TCP defaults (56).
- What the verifiers could not perform (36 entries in the two result files: 23
  and 13), in short: no measurement on the wire for the figures of
  R2-P3-lan-dev-01 and -02, which are in-process; the descriptor limit of a
  real deployment, and how matter.js's runtime treats a failed accept — Node's
  own listener, outside the exported pins (R2-P3-lan-dev-05,
  R2-P3-ctl-lan-04); R2-P3-pase-01's path through RevokeCommissioning, read
  only; the width of R2-P3-pase-03's interval on a real node; how long
  matter.js keeps a chunked write's hold (R2-P3-admin-01); that matter.js has
  no subscription quota rests on a search, not a full read (R2-P3-admin-03);
  no run over the wire into a controller with a store (R2-P3-ctl-dev-02); the
  real mDNS socket, real connections from many addresses, the scanner's
  delivery order and a radio's delivery rate (R2-P3-ctl-lan-01, -03, -06,
  R2-P3-ble-01, -02); how connectedhomeip's CASE server treats a Sigma1 it has
  seen (R2-P3-ctl-lan-02); the BDX variants of R2-P3-net-03; wpa_supplicant's
  own behaviour and limits (R2-P3-net-01, -04, R2-P3-dr-net-01); the ledger's
  own rules (R2-P3-net-05); which SDK versions deployed controllers run, and
  the rate on slower storage (R2-P3-dr-dev-01); a run against the SQLite
  store, and the heap at the default quota, which is extrapolated
  (R2-P3-dr-ctl-01, -02); what a real device does while it holds a session
  (R2-P3-dr-ctl-03); the specification's text is not available.
- Not run for this file: any test but `script/docscheck` and
  `script/citecheck`. Red and green of a fix are its fix note's. The guards on
  `3b6b250f` are the coordinator's measure and not in the tree: build, vet,
  lint, the fuzz targets and the benchmark smoke run passed; the tests were
  red for two tests only, each a row (N3-coord-5, N3-coord-6); the race run
  reported no data race; the chip-tool suite's host fallback in a container
  passed (403 subtests, 1 skipped). That suite is not the certification
  families, which hold the NetworkCommissioning legs and run in CI; they were
  not run after the behaviour at a fail-safe expiry changed. Never seen red or
  green: the Linux-only test of R2-P3-ble-04, compiled and vetted on darwin.
- Every statement of a row that is marked unverified, inferred, "not run",
  "not re-read" or "the role's"; a status in "What phases 0 to 2 handed to
  phase 3" is the role's at `8c9088ee`, before the fixes. One of them rests on
  a phase 0 fix that phase 0's register records as reverted; the held-back row
  says which.
- Two failures seen once and not explained are rows, not findings: N3-hs-5 and
  N3-coord-2. N3-rwhs-1's cause is inferred, and so is that N3-findev-3 was
  the failure `6d6cf533` repaired. The rates of N3-coord-5 and of N3-finctl-1
  are the coordinator's measure.
- Phase 0's held-back row "rework-bridge, one item on the unsecured session"
  was not compared with this branch; it stays held back with phase 0's text.
- The map from finding to commit came from a script over the commit subjects
  and the fix notes' tables; the six commits after `3b6b250f` have no fix note
  and were placed from the coordinator's note on them, each statement taken
  into a row checked against the tree: of the commit ids the notes carry, 16
  are the branch's own and 69 are from before the rebase; those were resolved
  in the object store and matched to the branch by identical subject, every
  one with a match. One statement of a fix note about a commit did not hold
  and is not in the rows: `phase-3-fix-rwtcp.md` names `743b9bf3` as the
  commit that deprecated `AutoArmOnPaseEstablished`; it is `7daa7ea3` (read
  off the two diffs). Two rows say what a commit touches instead of the file
  the finding names: R2-P3-ctl-lan-02 and R2-P3-net-01 (its second commit). 4
  of the 100 commits are named nowhere above: 3 notes (`af465dd2`, `175f63c2`,
  `dd6e5bf3`); 1 docs (`6abf136a`). The nine citation commits are named by
  their section only, `2155ad38` by "Counts" only. All of these were placed by
  their subjects, not read one by one.
- Whether the sibling checkouts hold what the exported pins hold was not
  compared; the exported trees carry no `.git`, so that they are the pins is
  taken from how they were made (a verifier says the same).
  `script/citecheck`'s drift artefact was regenerated by `58d60b96`
  (N3-cite-3); what its two versions hold was read off the files, the check
  itself was not run against a clone for this file.
- Which model the diff review's and the rework's reviewers ran on is not in
  their result files.
