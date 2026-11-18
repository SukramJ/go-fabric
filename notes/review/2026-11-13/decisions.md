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

## 2026-11-13/14, phase 1

| # | Subject | Decision |
| --- | --- | --- |
| 28 | Phase 1 runs as a workflow of ten review units with batched verification (about 16 agents) | **Approved at that size**; a second pass re-reads the request-path files the first pass left unread (`node/`, the `bridge` and `im` leftovers), the other unread files wait for phase 6 |
| 29 | How the phase 1 findings are fixed | **One bundled pull request**, started at once; the second pass's findings join it |
| 30 | R2-P1-req-03, the hold of a chunked write is taken before the ACL check, as matter.js does | **matter.js's order stays; the holds are bounded per session** (an Ahead row) |
| 31 | R2-P1-req-07, no timer runs between accepting a BLE connection and the BTP handshake | **A timer from the accept** (ahead of matter.js; an Ahead row) |
| 32 | R2-P1-net-03, mDNS queries from a source port other than 5353 | **As matter.js: no special case**; a `by_design` row records the parity |
| 33 | R2-P1-sec-02, vendor and product ids in an attestation certificate are parsed more leniently than matter.js does | **Lifted to matter.js's rule**, with its tests ported |

Not re-asked: TC-WASHERCTRL-2.2 keeps the owner's 2026-11-02 decision (no
`by_design` row; struck). Applied from CLAUDE.md without a question: FIXED
texts leave the live findings register; `by_design` rows for confirmed
parity; Ahead rows for bounds matter.js lacks and chip has.

## 2026-11-14, phase 2

| # | Question | Decision |
| --- | --- | --- |
| 34 | The cut of phase 2's review | **Seven flows, ten agents**: one reviewer per flow of the plan — Fable for commissioning and fail-safe (the pending-NOC marker read as a whole), the fabric life cycle, sessions and subscriptions, counters and replay; Opus for persistence and crash consistency, shutdown and goroutine leaks, wire-driven allocations — and three Fable verifiers for every finding of high or medium severity |
| 35 | The data volume of the Mac holds 44 GiB free after the build cache was emptied (local Time Machine snapshots keep the blocks) | **Left to expire**; the fix round starts only when the space is free; a job with many distinct builds keeps its cache on a RAM disk |
| 36 | The pending-NOC marker: nine states the two settings keys and the memory fields cannot represent or handle wrongly | **The fabric row carries the state**: a pending mark on the row, an UpdateNOC's new credentials in shadow columns, CommissioningComplete one update, start-up deletes pending rows and runs the removal consequences; the store schema and port change now, announced |
| 37 | What a fabric's removal cleans up is a hand-kept list of calls in the node | **One removal contract for every server**, called on root and Aggregator servers too; a reconcile against the fabric table at start and when an endpoint joins; a guard that fails when a server with a fabric-scoped attribute in the schema has no removal path |
| 38 | R2-P0-store-B06, the mechanism (decision 15 set the duty) | **One persister at the node**, keyed by the generated quality flag: restore before the topology is served, store in the write's commit path so that a store error answers FAILURE; a stored value overrides the declared Initial; host state behind a port stays the host's |
| 39 | One logical change spread over several settings writes | **`SetSettings` in the settings port, now**: several keys in one transaction, with a contract test; one announced break together with decision 36 |
| 40 | Start after Stop on a Bridge | **Single-run, as Node**: Start after Stop returns an error |
| 41 | Controller methods after and during Close | **A closed error everywhere**: one gate, every public method |
| 42 | RevokeCommissioning expires any armed fail-safe, another fabric's over CASE included; matter.js does the same at the pin (`AdministratorCommissioningServer.ts:143-148`) under a comment that says "PASE-held" | **Only the fail-safe the PASE session holds**, a `by_design` divergence — after chip's call path is read at the pin; if chip does otherwise, back to the owner |
| 43 | The idle session reaper closes a session whose subscription keeps a MinIntervalFloor of 300 s or more | **The reaper spares a session with a live subscription**, through a seam from the session manager to the subscription manager |
| 44 | R2-P2-alloc-04: repeated or overlapping attribute paths each re-expanded to the whole node and the report was materialised whole; matter.js generates read responses chunk by chunk (`AttributeReadResponse.ts:64`, `:98-105` at `db021da0`) | **Generated chunk by chunk, as matter.js**; `BD-Matter-Read-RepeatedPathsDropped` rewritten |
| 45 | R2-P2-alloc-02, -03, -05: the controller lacked the bounds the device side has against unauthenticated or hostile input (CASE responder, DNS-SD cache, report chunk assembly) | **The device side's policy on the controller** (bounded tables, the oldest evicted, time bounds; an Ahead row each) **and a clamp** on the MRP intervals an unauthenticated peer states |
| 46 | W2-wire-10, the DataVersion sentinel | **A version for every cluster instance**, root and Aggregator included, held by the endpoint unless the server has its own; the result carries "has a version" instead of the sentinel; the contract's text changes |
| 47 | R2-P2-comm-08: opening a commissioning window arms the fail-safe for the window's whole duration, a divergence no `by_design` row records; an administrator gets Busy on ArmFailSafe while a window is open | **Removed, as matter.js**: opening a window arms nothing; the fail-safe comes with the commissioner's ArmFailSafe |
| 48 | R2-P2-alloc-05: the bound on the report chunks the controller assembles per session — 2 MiB of wire bytes, chosen by the fix agent without a source | **Configurable, default 2 MiB**: `controller.Config.MaxReportBytesPerSession` |
| 49 | R2-P2-sess-03, the second half: matter.js ends the sessions at shutdown without a CloseSession, under a TODO of its own that questions it (`SessionManager.ts:1110-1112` at `db021da0`); with a silent close the module's own controller fails six restart tests after about 49 s | **CloseSession stays, a `by_design` divergence; the controller is hardened** to drop a session whose peer went away without a farewell and to establish CASE again |

## 2026-11-14, phase 3

| # | Question | Decision |
| --- | --- | --- |
| 50 | The cut of phase 3's review | **Seven attacker roles, ten agents, Fable**: the plan's six — an unauthenticated host on the device's link, a holder of the passcode, another fabric's administrator, a hostile device against the controller, a hostile BLE peer, a hostile Wi-Fi, DCL or OTA peer — and a seventh, an unauthenticated host on the controller's link; seven reviewers and three verifiers on Fable; a unit whose agent the provider's safeguards stop goes to Opus, and the report says so |
| 51 | R2-P3-lan-dev-01: a handshake's state on the device is found by the 16-bit exchange id alone; matter.js ties exchanges to the unsecured session of their initiator. The fix changes the keys of the exported provider interfaces in `bridge` | **Now, announced**: the providers are keyed by initiator and exchange |
| 52 | R2-P3-admin-01: a chunked write takes the node-wide hold of its attribute before the access check (matter.js's order, kept by decision 30); shown now: any holder of a NOC of any fabric can keep writes of every fabric Busy, the ACL write that would remove it included | **The hold is taken only after the access check, and per fabric for a fabric-scoped attribute**; a `by_design` divergence; decision 30 is changed |
| 53 | R2-P3-admin-03 (R2-P1-dr-req-09): the subscription quota counts per fabric only; one subject can hold all sixteen places and keeps them after its ACL entry is removed; matter.js has no quota | **connectedhomeip's rule, read at the pin and mirrored, and a subscription whose subject has no readable path left ends**; if chip's rule frees no place for a legitimate controller, back to the owner |
| 54 | R2-P3-ctl-dev-02: the controller's node image stores every path a device reports, with no quota per node | **A structural rule and a configurable quota**: only what the device's own structure names or the request asked for is stored; behind it a byte quota per node, a field of the controller's configuration with a default (chosen, not sourced) |
| 55 | R2-P3-ctl-lan-06 = R2-P3-ble-01, the rest: candidates in the commissioning race that never answer PASE still take one stagger slot each (matter.js has the same stagger) | **A capped stagger and rotating places**: a candidate's delay has a ceiling whatever stands before it, at most N attempts run at once, and a new candidate ends the oldest attempt still unanswered; both numbers chosen, an Ahead row says so |
| 56 | The total bound on inbound TCP connections (4, connectedhomeip's number) is too tight for a controller many devices connect to | **Configurable, a default per role**: a field of the TCP configuration; 4 on the device, 256 on the controller (chosen) |
| 57 | Decision 54's ServerList half breaks matter.js's tolerance of a reported cluster its ServerList does not name yet (`ClientStructure.ts:654-659`) | **matter.js's tolerance stays**: the PartsList rule and the quota are the fix; decision 54 is narrowed accordingly |

## 2026-11-14, phase 4

| # | Question | Decision |
| --- | --- | --- |
| 58 | The cut of phase 4's review | The owner, 2026-11-14: phase 4 starts once phase 3's pull request has merged, without a further question, with at most ten agents to a workflow and the cut and its cost stated first. **The coordinator's cut: seven reviewers and three verifiers, Fable** — two on the Interaction Model (Read, Subscribe, reports and events; Write, Invoke, Timed, groups and the status codes), two on the core servers (those that hold credentials and access; the other system servers), two on the application servers (lighting, level, scenes, switch; thermostat, lock, covering and the rest), one on certifiability, the open parity findings and the rows earlier phases placed in phase 4; each compares behaviour with matter.js at `db021da0`. The re-read of every `by_design` entry is a second workflow of three agents; the drifted citations and those at the older pin are worked off in the fix wave |
| 59 | The default of `controller.Config.MaxImageBytesPerNode` (decision 54 left it chosen, 32 MiB, after its measure changed) | **32 MiB stays**: sixteen times the report bound of a session; by the fix's measurement room for about two thousand bridged plugs; a host lowers it |
| 60 | R2-P4-imread-04: a subscription report that runs out of retransmissions counts as peer loss — the subscription ends at the first and every session of the peer is closed; matter.js at the pin does not (`InteractionServer.ts:96-97`, `suppressPeerLoss`) | **As matter.js at the pin**: one failed update, the subscription ends at the third, the sessions stay |
| 61 | R2-P4-imread-05: a Reassemble that removes an endpoint ends every subscription that names it, recorded as matter.js's behaviour, which it is not | **As matter.js: the subscription lives on**, its paths on the removed endpoint select nothing; the entry `BD-Matter-Reassemble-Closes-Removed` goes |
| 62 | R2-P4-imread-06, -10, -12 and F-db021da0-16: the order of reported attributes differs from matter.js in three ways | **matter.js's order everywhere** — statuses first, the request's path order, the model's member order inside a cluster — for Read, priming and change reports alike; the schema extract is extended to carry the member order first |
| 63 | R2-P4-imwrite-09: a group write that appends to a list is applied, as connectedhomeip applies it; matter.js at the pin drops every group ADD | **connectedhomeip's behaviour stays, a `by_design` entry**: matter.js's own client chunks a list write into REPLACE and ADDs, so mirroring its server would leave a group-written list empty |
| 64 | The class behind R2-P4-coresec-01, -04, -07 and R2-P4-coresys-01: the hand-written servers are not held to the model's field constraints and enums, which matter.js checks before a behavior runs | **One check in the dispatch path**, of request and write fields against the generated definitions, in front of the hand-written servers too; a server that relaxes a constraint as matter.js does says so |
| 65 | Where matter.js and connectedhomeip disagree on a commissioning command and the module follows connectedhomeip without a record (the status of a label too long, the order of AddNOC's checks, a new key per CSRRequest) | **matter.js, exceptions one by one**: connectedhomeip's behaviour stays only where a certification case fails otherwise, each with its `by_design` entry |
| 66 | R2-P4-applight-03 and -05: two behaviours of matter.js at the pin that look unintended (a hue at the wheel's bound does not move; FabricSceneInfo is empty for a new fabric until its first scene or a restart) | **The module's behaviour stays, a `by_design` entry each**; the comments that gave matter.js as the reason are corrected |
| 67 | R2-P4-appclim-01: a lock command's PINCode is dropped before authorization on a lock without COTA; matter.js authorizes with it whatever the features | **As matter.js: a PIN that is sent is checked** |
| 68 | R2-P4-cert-01: matter.js automates 120 certification cases at the pin; the Go-TH leg and the page account for 68 | **Classify now, port in this phase**: the fix pull request classifies all 52 and adds a guard that fails on a case neither run nor classified; the ports are a pull request of their own in phase 4 |
| 69 | TC-PAVST-2.3, 2.4, 2.6, 2.11 and TC-WASHERCTRL-2.2 fail because the model's constraint answers before the server's logic; the page calls it the harness | **connectedhomeip's answer, the cases pass**: the servers concerned relax the generic check on exactly these fields and say so in their Ahead rows |
| 70 | R2-P4-cert-02: a Pake1 or Pake3 without a PBKDFParamRequest, and a Sigma3 or Sigma2_Resume without a Sigma1, is answered with a StatusReport and, for PASE, counted as a failed attempt; both references acknowledge and do no more | **As the references: acknowledged, not answered, not counted** |
| 71 | R2-P4-coresys-02: the node facade cannot state a BootReason; every start reports PowerOnReboot | **Now: `node.Config.BootReason`**, zero value Unspecified as matter.js; the OTA apply path states SoftwareUpdateCompleted |
| 72 | `BD-Matter-mDNSDeviceType`: the commissionable advertisement names RootNode when the host sets no device type; the entry compared with connectedhomeip only; matter.js infers the first application device type below RootNode, Aggregator or BridgedNode (`ProductDescriptionServer.ts:93-126`) | **As matter.js: inferred** when the host sets none; a type the host sets wins |
| 73 | `BD-Matter-WindowCoveringPercentNonNull`: the position attributes never read null here; in matter.js both start null | **An unknown position reads null**, as matter.js, until the host reports one; additive in the server's configuration |
| 74 | `BD-Matter-OTAProvider-ACLEveryFabric`: the node writes the provider's ACL entry into every fabric's ACL; matter.js writes it only into the fabric its own authority controls (`OtaSoftwareUpdateProviderServer.ts:117-124`, `FabricAuthority.ts:112-128`) | **As matter.js, the rest a host option**: automatically only on a fabric the node's own controller administers; a host switches it on for another fabric explicitly — a `node.Node` has no controller of its own, so it writes the entry into no fabric unasked; `node.OTAProviderConfig.GrantFabric` is the host's switch |
| 75 | `BD-Matter-SessionRolloverClose` and R2-P4-cert-05: the device closes a session at its counter's rollover at once; matter.js waits for the exchanges in flight | **A deliberate divergence, recorded with its reason**: the case arises after about 4.29 thousand million messages on one session; the device keeps no account of its active exchanges and gets none for this |
| 76 | R2-P4-imwrite-01: a command with the Large Message quality needs a transport that carries large messages, as matter.js; the bridge's peer sender cannot open TCP (it sends over the bridge's own session registry, ADR 0019), so with the gate a camera's signals between two nodes of this module no longer arrive | **The peer path moves onto `client/` in phase 5**, with an ADR that supersedes ADR 0019's peer-path decision; the gate and the peer sender's rule are not part of phase 4's pull request, the finding stays open until then |
| 77 | An ArmFailSafe that arrives while an expired fail-safe is rolled back: matter.js answers SUCCESS and arms nothing, connectedhomeip BusyWithOtherAdmin, this module waits for the rollback and arms fresh | **Wait and arm fresh, a recorded divergence** with both references' lines — `BD-Matter-ArmDuringRollbackWaits` |
| 78 | R2-P4-imwrite-12 and F-db021da0-18: on a bridged endpoint the DataVersion advances after every successful write, changed or not; matter.js advances only for a change | **An optional capability in `contract/`**: a server that implements it says whether a write changed anything, every kit server does, a host's server without it behaves as today — `contract.ChangeReportingWriter` (`MatterWriteChanged`); on the branch every kit server implements it but OperationalCredentials, which refuses every write, and two guards hold that: `cluster:TestEveryMatterWriteSaysWhetherItChanged` over the source and the served-cluster walk of `node:TestServedClusterRevisionAndFeatureMap` |
| 79 | R2-P4-coresec-08: matter.js gives a fabric whose ACL holds no entry an Administer entry for its original administrator at start | **Not mirrored, a `by_design` entry**: the module grants no privilege unasked — `BD-Matter-ACL-NoAdministerEntryAtStart` |
| 80 | R2-P4-cert-04: a write of InterfaceEnabled to the Wi-Fi NetworkCommissioning server is stored without effect | **connectedhomeip's rule, an optional driver capability**: `false` is INVALID_ACTION unless the driver can switch the interface — `contract.WiFiInterfaceSwitcher`; `true` is accepted as it is |
| 81 | `BD-Matter-DoorLock-CreatorFabricIndexKept`: matter.js keeps a removed fabric's index in a user's and a credential's creator fields, connectedhomeip clears them | **Cleared, as connectedhomeip**: the creator and the last-modified field, nothing removed; `BD-Matter-DoorLock-CreatorFabricIndexCleared` replaces the entry |
| 82 | The certifiability page files cases matter.js automates and this module has not ported yet under "harness" | **A fifth status, "not ported yet"**, by a dated amendment of ADR 0011; it also holds a case whose fix is in and whose run is pending |
| 83 | The rest of decision 62: a server's or device type's own reordering of attributes, and the order of endpoints and clusters in a wildcard (insertion order in matter.js, ascending ids here) | **Both recorded as divergences**: neither is a property of the model — `BD-Matter-AttributeOrder-ModelOrder` |
| 84 | `node/webrtc_requestor_acl.go` writes an ACL entry into the store's first fabric, on the reading decision 74 corrected for the OTA provider | **As decision 74**: no entry unasked, the host grants a fabric explicitly — `node.Config.WebRTCRequestor.GrantFabric` |
| 85 | The redaction of another fabric's fabric-sensitive fields is written by hand in three servers and held by a guard | **Generated from the model, in phase 5**; the guard holds the class until then |
| 86 | The certification ports of decision 68 need a Go-TH leg that runs where they are written; on the Mac it runs in a container only, and the Docker engine was down | **Docker first, then the ports with local runs**: every port passes in a container on the user-defined network before it is reported ported; the pipeline confirms |
| 87 | Who coordinates a phase, and on which model | **Opus, in a fresh session per phase**; Fable works as a targeted agent for the phase plan, the decision briefs behind the wizard and the verdict where matter.js, connectedhomeip and the specification contradict each other (`CLAUDE.md`, "Which model does which work") |
| 88 | Who writes the parity registers (`by_design.md`, the findings register, an ADR) | **Fable reads both sides at the pin and gives the verdict as a fact note, Opus writes the entry, Fable reads the text against its note**: every reference statement still comes from Fable |
| 89 | Who reviews | **Opus the code — correctness, concurrency, error paths, tests; Fable, bounded, the behaviour against the references, the security paths and every finding of high or medium severity** — for a review phase's own units as for a wave's pull request |
| 90 | How citations are checked | **A script and Opus check every range; Fable reads every citation that is the reason for behaviour, every doubtful one and a sample of the rest** |

## 2026-11-15, phase 5

Wizard round 1 at the start of phase 5; the briefs and the phase plan were written by Fable, every option taken was the recommended one.

| # | Question | Decision |
| --- | --- | --- |
| 91 | Phase 5's cut and budget | **Variant A, two halves, two PRs** (≈24–27 M): 5a tests, docs, supply chain, daemon; 5b API cut, port extensions, peer path, generated redaction, wave H. Deferred to phase 6: bridge mutation survivors, ADR/history claim checks, 20–30 low/note carried rows; API reviewers read the exported surface only; one rework round |
| 92 | Phases for the ten rows without one | **Phase 5** for F-db021da0-20, -22, -23, -27, -39, -45, -46, -47; phase 6 for -28 and -38 |
| 93 | Commissioning Proxy cluster (0x0455) | **A roadmap note tied to the Matter 1.7 lift**; no work in Review II |
| 94 | The two TCP payload differences (BD-Matter-TCP-ExchangePayloadSize) | **Both closed as matter.js** (Session.ts:237-239, MessageChannel.ts:86-92 at db021da0); the entry is retired |
| 95 | Reports overlap (BD-Matter-Subscription-ReportsOverlap, F-db021da0-36) | **Adopt matter.js's hold in phase 5 (wave H)**: one report in flight per subscription, delivered = StatusResponse; F-36 pulled forward from phase 6 |
| 96 | TC-DD-3.12 / 3.13 | **A public `node` option** (an uncommissioned node does not open its window at start; the host opens it) **+ `--custom-flow` and a pipe command in the daemon**; both cases re-ported at the pin; the option's form is the API review's (apinode), the DD part after wave A2 |
| 97 | PBKDFParamRequest with no window open | **StatusReport Failure/InvalidParam as matter.js** (SecureChannelProtocol.ts:129-137) for the state "window closed" only; an unattached PASE port stays the fail-closed noop |

Wizard at the end of phase 5a (CI and release), every option taken was the recommended one.

| # | Question | Decision |
| --- | --- | --- |
| 98 | zizmor's dangerous-triggers finding on `dependabot-auto-merge.yml`'s `pull_request_target` | **An inline exception with its reason**: the workflow checks out and runs nothing of the pull request and acts only on a Dependabot pull request |
| 99 | Are `test (linux/386)` and `workflow lint` required checks | **Both, after their first green run**: the owner adds them to branch protection, `required.txt` follows |
| 100 | `wifi/wpasupplicant` does not build against the go-fabric version it requires (v0.22.1) | **Its require is raised with the next release**, held from now on by the nested-release check |
| 101 | Are tags or releases signed or attested | **No**: a library without binaries, its integrity held by the Go checksum database; the provenance check (a tag on main with green required checks) stays |
| 102 | Dependabot auto-merges a v0 module's minor update | **No blanket change**: the required checks gate it |
| 103 | The nested-release check's convention | **`release/nested-*` stays, and the check is strict on any release pull request that changes a nested `go.mod`** |
| 104 | The 32-bit test run's packages | **`controller/` and `mdns/` added after the first green run** |

Wizard rounds during phase 5b (2026-11-15); 116a refines 116 after a read of the references.

| # | Question | Decision |
| --- | --- | --- |
| 105 | White-label OTA identity (R2-P3-ctl-dev-05) | **Retain the attested vendor/product pair now, log a mismatch only** (new Record field + store key, additive; nodes commissioned before carry "unknown"; port wave of 5b, ahead of matter.js → register row); **the refusal rule (BasicInformation ∉ the CD's product list) only after a Fable read of matter.js and chip at the pin** |
| 106 | Branch protection (decision 99) | **`test (linux/386)` and `workflow lint` are required on main** (owner, 2026-11-15; the protection itself was found without them on 2026-11-16 and set then, on the owner's choice, through the API — 22 contexts measured after): refresh `required.txt` (script/requiredchecks) and move their rows out of pending in PR 5b |
| 107 | Store port growth (Q-port-1; R2-P5-port-03/-04/-05) | **Methods on the area interfaces, one announced break** (owner's explicit word for a break outside the 5b rule): storetest holds each; CHANGELOG + migration line as decisions 36/39 |
| 108 | Sealing secrets at rest (decision 10, N-store-2) | **Host-supplied `store.Sealer`, key outside the database, opt-in, AAD per secret column, upgrade step seals plaintext rows; built in 5b as its own wave after F**; "ahead of matter.js" only after a Fable read of matter.js's storage |
| 109 | Owed fabric hooks (N-findev-3) | **Durable owed-event marks in the store**, written with RollbackFabric in one transaction together with the resumption removal; Start calls the hooks, then acknowledges (at least once) |
| 110 | Redaction placement (decision 85, F-db021da0-30) | **Shape A: one generated scope table in schema/, enforced behind `readOne` for every server**; the nine hand redactions go, `Redacted` fields `Deprecated:` and still honoured until removed |
| 111 | Fabric-scoped entry without a FabricIndex | **Refuse: FAILURE for the path, as connectedhomeip** (AttributeValueEncoder.h:97-108); by_design row against ListManager.ts:475-482 |
| 112 | Meaning of `Handle` (F-db021da0-31, apispec-05/-06) | **The command method everywhere**: no gate, no scene invalidation, no transition by the server; the answer checked everywhere; reverses 82065176 for LevelControl **after** a Fable read of ScenesManagementServer.ts:841-854 at the pin |
| 113 | Hand-written spec definitions (apispec Q5, apispec-08, F-db021da0-20 rest) | **Supported, with a check**: `Instance.CheckRequest` holds requests to the definition's constraints; a stdlib-only port in contract/ reports a definition's fabric scope to the dispatcher table (the snapshot wins for standard clusters) |
| 114 | BLE peripheral port (N-req-2, apidev-05) | **A new port (Advertise, StopAdvertising, RegisterService), the old `BLEPeripheral` and `node.Config.BLE` `Deprecated:`**; the bridge registers before advertising, StartBLE reports a refusal (Fable's recommendation against the unit's optional extension) |
| 115 | Peer path shape (decision 76, ADR 0032; peer-01/-03, imwrite-01) | **Shape 1: establish on client/, reuse where the session lives (incl. peer-opened), composed in node; TCP for Large Message, newest session by activity**; ADR 0032 names one session layer for both roles as the direction, decided after Review II |
| 116 | Destinations of a DCL/OTA download | **Ledger URLs public only**: a URL the ledger names (OTA record, CRL point) is fetched over https and refused at dial time when it resolves to loopback/private/link-local/unspecified/multicast, first URL and every redirect hop; a host-supplied URL keeps http and local destinations as matter.js; a switch opens private destinations for labs; a host `HTTPClient` is the host's policy. Divergence → by_design row |
| 117 | Ledger record without checksum | **Refused** (prod and test ledger) before the download, warn line; an `Images` option admits it and logs each acceptance; local images unaffected; stored ledger images without checksum not served unless the option is set. Divergence → by_design row |
| 116a | Decision 116 refined (owner, after a Fable read of the CRL point's scheme at the pins) | **CRL distribution points: http or https** (the schema allows both, device-attestation-revocation.ts:98-101; matter.js and chip fetch http); the destination guard applies to both; OTA downloads named by the ledger stay https-only. **Under an environment proxy**: the proxy stays usable; destination URLs whose host is an IP of the five refused classes or `localhost` are refused before hand-over and on every redirect; host names behind a proxy are the proxy's to resolve — documented limit |
| 118 | The controller's secrets at rest (review of the phase-5b diff) | **`controller.Config.Sealer` takes the same `store.Sealer` as `node.Store.Sealer`** (opt-in, off by default): the CA and the controller's fabric keys, the CASE resumption records, the group key sets and the registry's kept onboarding codes and ICD keys are sealed; a clear database is sealed on open, as a node's |
| 119 | The "attested" vendor/product pair the commissioner records | **Recorded only when the DAC chain was verified against the trust store and no error finding was accepted**; otherwise the pair stays empty and one log line names the reason |

## 2026-11-16, phase 6

Wizard round 1 at the start of phase 6; the plan and the options were written
by Fable. 120 and 122 depart from the recommended option, the others take it.

| # | Question | Decision |
| --- | --- | --- |
| 120 | The cut of phase 6 | **Two halves**, each a fresh coordinator session and its own pull request: **6a = W1–W3** (the request path and the cluster servers), **6b = W4–W6** (client, controller, facade, guards). The recommended three parts were not taken. 6a runs two review workflows of at most eight agents at one HEAD, then one verify |
| 121 | Who reads in the full pass | **Decision 89 as it stands**: Fable reads the eight security-path units in full and the rest through three reference overlays; Opus reads the other sixteen units in full; Fable confirms every high and medium |
| 122 | What phase 6 fixes | **Everything confirmed**, as in phases 0–5 (not the recommended high/medium and local lows). A low that needs an exported API move, a new port or a schema change goes to the half's second wizard round before it is fixed |
| 123 | Decision 13, never carried out | **A unit `cite` in 6b**: a script and Opus check every range at `db021da0`, Fable reads every citation that is the reason for behaviour (decision 90), and decision 13's guard ratchets the citations at another commit towards zero |
| 124 | The sealed store before its first release | **All three in 6a**, one fix unit after the security unit's read: the journal hardening (5b-seal F4), a persistent sealed-database marker, `store.Reseal` (N5b-seal-1) |
| 125 | The next release | **After 6b merges, before phase 7**; the nested modules and examples onto `GATTPeripheral` (decision 114) and the `wifi/wpasupplicant` require (decision 100) go into that release's pull request, not into phase 6 |
| 126 | Held-back rows #534 fixed | **Nine now**, in a notes pull request at the start of 6a; **the three of the disclosure class** (R2-P5-redact-01, N4-frsec-1, DR5b-F-01) with the release that carries their fix |
| 127 | The parity backlog | **Placed rows worked** (fixed or recorded as a divergence); every other open entry of `matter_behaviour_findings.md` gets one verdict line from the Fable overlay of its package (true at the pin, stale, or a `by_design` candidate), no fix |
| 128 | Decision 17 larger than decided (N5b-afacade-1) | **Confirmed as built**: `device.New` refuses what matter.js's init validation refuses; the CHANGELOG names every type |

Wizard round 2 of 6a (2026-11-16), after the review's verify; the briefs and
options were written by Fable. 137, 143, 146 and 150 depart from the
recommended option, the others take it.

| # | Question | Decision |
| --- | --- | --- |
| 129 | Fabric isolation where connectedhomeip has none (R2-P6-sec-1201, -1103, -1106) | **Fence all three, recorded as ahead of chip**: Messages refuses an id another fabric holds; the Joint Fabric datastore answers UNSUPPORTED_ACCESS to any fabric but the recorded anchor fabric; a RefreshNode never replaces a held key set |
| 130 | The mark of a sealed database | **A sealed canary** in a reserved `matter_settings` row: the mark, the wrong-key check and Reseal's handle; not tamper protection, and the doc says so |
| 131 | Re-sealing under a new key | **`store.Reseal(ctx, db, old, new Sealer)`**, one transaction, the canary last, then the scrub with its owed mark; no way back to a clear database |
| 132 | `NewSealed` when the scrub cannot finish (R2-P6-err-0108) | **The store opens sealed and strict, without an error**; the owed scrub can be queried, one warning line is logged and the scrub is retried at the next open |
| 133 | Group reception state (decision 22) | **A bound per operational key** that refuses a sender it does not hold yet and evicts nobody; an *Ahead of matter.js* row |
| 134 | Bounds that neither reference has (R2-P6-bound-0106, -0107, -1008, -1205, -1206, -0502, -0203) | **Per fabric (per peer for TCP)**, each an *Ahead of matter.js* row |
| 135 | A database without `foreign_keys` (R2-P6-sec-0601) | **Both**: explicit deletes in RemoveFabric and RollbackFabric, and `Apply` refuses `foreign_keys = 0` (an announced break) |
| 136 | Package `im` without an ACL checker (R2-P6-sec-0560) | **Deny**; no ACL only through `endpoint.UnenforcedACL` |
| 137 | The Rotating Device Identifier (R2-P6-sec-0207) | **The full feature, as connectedhomeip**: a random persisted unique id and a persisted lifetime counter (two store settings, a node option); an *Ahead of matter.js* row. Not the recommended option |
| 138 | H7, the single reporter hook | **Shape A**: `subscription.ReportSender` through `Manager.SetReportSender`; the old hooks and accessors `Deprecated:` |
| 139 | Package `commissioning` | **`Deprecated:` now**, removed after the deprecation window |
| 140 | Three exported surfaces without a production caller (N-ctr-5, R2-P6-sec-1006, R2-P6-api-1261, -par-1262) | **All three `Deprecated:`**; the fallbacks answer UNSUPPORTED_ACCESS; the five wire ids absent from the snapshot go with them |
| 141 | Host hardware knowledge in `cluster/measurement` (R2-P6-doc-1465) | **Onto `contract/` as additive fields**, today's values the defaults |
| 142 | Deep copies of generated state (R2-P6-err-1460) | **`clustergen` emits `Clone`**; every stateful server uses it |
| 143 | F-db021da0-28 with R2-P5-apidev-02 | **Both as matter.js**: NodeLabel and SerialNumber are served only when the host stated them; no SerialNumber backfill, `BD-Matter-BasicInfo-SerialNumberAlwaysServed` goes; a fixture run before the pull request. Not the recommended option |
| 144 | F-db021da0-4, the floor after priming | **Keep the floor as connectedhomeip**, `BD-chip-SubscriptionFloorAfterPriming`, no code |
| 145 | F-db021da0-8, a pass whose data is all filtered | **One rule, the event path's**; an *Ahead of matter.js* row; it comes back if connectedhomeip sends the empty report (superseded by 169) |
| 146 | F-db021da0-17 and -56 | **Fix both**: the tracker holds "installed" beside the value; one advance per attribute data. Not the recommended option |
| 147 | F-db021da0-21, the Thermostat's sibling coupling | **The host wiring stays**, recorded as a divergence |
| 148 | F-db021da0-38, the order of GroupKeyMap | **The written order, as matter.js**; the store carries a position (schema v11) |
| 149 | R2-P6-par-1413, MoveColorTemperature Stop | **Stop as connectedhomeip**; a `by_design` row and an upstream note |
| 150 | Behaviour mirrored from connectedhomeip that breaks a module guarantee (R2-P6-fsm-1581, -1582, -1615, -err-1002, -sup-1009, -fsm-1108, -sec-1602, -fsm-1603, -1604) | **Mirror all nine, recorded** in the `BD-chip-…-RulesInServer` rows; port docs that over-promise are corrected. Not the recommended option |
| 151 | The citation defects | **To unit `cite` in 6b** as its ready list; only `bridge/conn.go`'s seven blocks now |

The coordinator's reservations, stated to the owner: 143 — the backfill had an
ecosystem reason on record, so a reference read checked it before the fix (it
did not hold); 150 — mirroring 1581/1582 and 1615/1009 keeps behaviour a
controller can observe as wrong, consistent with the order of authority since
connectedhomeip is the authority there.

Wizard after 6a's first fix wave (2026-11-16); every option taken was the
recommended one.

| # | Question | Decision |
| --- | --- | --- |
| 152 | R2-P6-sec-1103, the read half | **A contract read-access capability** the dispatcher asks before a read or subscribe, answering UNSUPPORTED_ACCESS; the interim empty-values fence stays until it lands |
| 153 | N3-findev-1 | **`Manager.OpenFromSigmaWithStake` and `OpenFromSigmaAsInitiatorWithStake`** with `ErrStakeLost`; the `…WithID` pair `Deprecated:`; `node/security.go` switches |
| 154 | R2-P6-api-1307 / DR5b-c2-06 | **The eight `spec.Hooks` servers move to `InvokeHeld`**; `Hooks` `Deprecated:` |
| 155 | N4-coresec-1 | **The fabric label is counted in bytes** (connectedhomeip: the command's size check and the fabric table's 32-byte label); matter.js's UTF-16 count is recorded as a `by_design` row |
| 156 | The defects of the deprecated package `commissioning` (R2-P6-sec-0104, -par-0105, -api-0114) | **Documented only**: the deprecated package keeps its behaviour |
| 157 | A secret table added to a sealed store later | **Strict**; a standing test requires an upgrade step for every new sealed setting or secret table |
| 158 | R2-P6-store-1010 | **Binding and ICD Management take the fabric-existence source AccessControl uses** (one port, filled by the node), fail-closed without it |

Wizard during 6a's second fix wave (2026-11-16); every option taken was the
recommended one.

| # | Question | Decision |
| --- | --- | --- |
| 159 | `OpenFromPaseWithID` installs without a stake | **`OpenFromPaseWithStake`** with `ErrStakeLost` as CASE; `OpenFromPaseWithID` `Deprecated:`; `node/security.go` switches |
| 160 | `AllocateID` | **`Deprecated:`**; a test-support helper installs under a chosen id; the bridge's test fixtures switch |
| 161 | R2-P6-wire-0442 / -1703, the status of a malformed write value | **As matter.js**: an exported `im` marker for an undecodable value; the write interaction ends with StatusResponse FAILURE, earlier paths committed |
| 162 | R2-P6-bound-0405, the read order | **`im.LookupAuthority(ctx)`** over the existing `im.WithAuthority` stamp; `im` stamps it in ReadReports when the ACL is active; the dispatcher authorises before it reads |
| 163 | R2-P6-doc-1465, the ElectricalReadings half | **An optional capability `contract.ElectricalCurrentSource`**; absent means AC as today, DC sets PowerMode DC |
| 164 | R2-P6-conc-1407, the step half | **An additive `transition.Transition.TargetFrom`**, the twin of `RateFrom` |
| 165 | F-db021da0-46 / R2-P6-par-1405, exported trackers | **`Deprecated:` all**: the LevelControl, ModeSelect and modebase DataVersion trackers and `spec.ServerConfig.DataVersion` (the wire carries the endpoint's version); removed after the window |
| 166 | The root BasicInformation SerialNumber backfill (the rest of 143) | **As matter.js**: the root serves SerialNumber only when the host stated it; `BD-Matter-BasicInfo-SerialNumberAlwaysServed` goes, and the comment's connectedhomeip claim with it |
| 167 | R2-P6-bound-1206 | **`OTAProvider` implements `contract.FabricScopedState`** (two additive methods); the node drops a removed fabric's entries |
| 168 | R2-P6-wire-1209 | **Both paths lenient**: the `cluster/wire` raw decoders accept an absent LevelControl TransitionTime again, as the bridge path; `BD-Matter-LevelControl-LenientDecoders` stays, corrected with the reference facts |

Wizard during 6a's third fix wave (2026-11-16/17); every option taken was the
recommended one. 182 supersedes 181 after a reference read found its premise
false; 169 supersedes 145.

| # | Question | Decision |
| --- | --- | --- |
| 169 | F-db021da0-8, the all-filtered pass, again (connectedhomeip sends the empty ReportData and restarts both intervals) | **One rule as both references**: the empty ReportData on the attribute and the event path, the pass counts; no *Ahead* row; supersedes 145 |
| 170 | TLV null written to a non-nullable scalar | **As matter.js**: handled as an undecodable value through decision 161's mechanism; the interaction ends with FAILURE |
| 171 | R2-P6-api-0611, SetDeviceTypeValidation after Start | **Doc only**: the mode is a construction-time setting; no exported SetMode |
| 172 | N-ctr-5, the rest | **`SubscriptionTarget`, `ErrNoSubscriptionID`, `ErrNoPeer`, `ErrPeerCounterNotAnchored`, `ErrNotEstablished` `Deprecated:`** with `EstablishSubscriptionTarget`; `ErrNoBridge` stays |
| 173 | F-db021da0-7, the unacknowledged handshake reply after a failure | **As both references**: the reply keeps retransmitting until it is acknowledged or MRP gives up; no `by_design` row |
| 174 | `bridge.ErrAckWaitTimeout` without a producer | **`Deprecated:`**, removable in the next minor release |
| 175 | R2-P6-bound-0502, the source of the cap | **An additive `(*subscription.Manager).MaxSubscriptionsPerFabric()`**; the bridge's per-fabric cap of persisted rows reads it |
| 176 | R2-P6-wire-0503, the bridge half | **As matter.js**: the status wait carries "undecodable" — on an ongoing report a failed send that is retried, on priming FAILURE and close |
| 177 | The deprecated `closure.ControlServer.Hooks` and `modebase.Server.Hooks` handlers no longer consulted | **Honoured for the deprecation window**: a handler registered through the deprecated field is called when no new `Handle` is set; the CHANGELOG line moves to Deprecated |
| 178 | H7: a subscription manager without a report sender | **Refuse `Subscribe` and `Restore` with `ErrNoReportSender`** (the bridge answers FAILURE); an announced change |
| 179 | H7: an exported test sender | **No** |
| 180 | Where 6a's Linux runs go | **`make chiptool-container` locally** after the third wave's integration; the families and the fixture run in CI on the pushed branch before the pull request |
| 181 | R2-P6-fsm-1001, the bridge half: a wildcard invoke whose every target answers `im.NoResponse` | **Silent by one rule**, with an additive `LeftOut()` — taken on a premise about connectedhomeip that a reference read then found false; superseded by 182 |
| 182 | 181 put again (connectedhomeip refuses an invoke whose path names no endpoint before any handler; matter.js expands and always answers) | **The wildcard follows matter.js**: the InvokeResponse is always sent and a target that returned `im.NoResponse` is left out of it; silent only on a concrete path, as connectedhomeip; no `LeftOut()`; a `by_design` row for the omission. Supersedes 181 |

Wizard before 6a's pull request (2026-11-17), after the fixture run; both
options taken were the recommended ones.

| # | Question | Decision |
| --- | --- | --- |
| 183 | TC-PAVST-2.8 step 4: a FindTransport (quality L) sent over a UDP session meets the large-message gate (INVALID_TRANSPORT_TYPE, as matter.js); connectedhomeip carries the same gate, inert for its code-driven clusters through a generator gap, so the case expects NOT_FOUND | **The gate stays**, one rule as matter.js; the case's step is recorded as a divergence (`by_design`, the certifiability page, the harness's record of the case) and reported upstream |
| 184 | R2-P6-bound-1312 put again: connectedhomeip caps its recursive `TLV::Utilities::Iterate` at 10 levels, not its reader | **No depth cap**, as decided; the register's wording corrected |

Wizard round 1 of 6b (2026-11-17), from the coordinator's phase plan for 6b;
every option taken was the recommended one.

| # | Question | Decision |
| --- | --- | --- |
| 185 | Completion of a full read in 6b (6a left 25–57 % of a unit unread on the first pass) | **A completion stage inside each review workflow**: after each code unit's review a script reads its unread-range table and a completion agent reads exactly the unread slices; up to 13 (review A) and 14 (review B) agents a workflow, the owner's word under decision 20 |
| 186 | 6b's budget (≈ 34 M calibrated on 6a, range 28–40, against ≈ 22 M planned; the overrun is the fix side) | **Decision 122 holds for 6b** (everything confirmed is fixed); a checkpoint after the verify with measured counts — a fix side estimated above 20 M goes to the owner before the fix waves |
| 187 | When the matter.js pin is lifted (the weekly marker check red since 2026-11-16: nine markers absent at matter.js origin/main `8addfb15`) | **After phase 7**, as its own pin-lift pull request with the nine register rows reconciled; phases 6 and 7 stay one convergence run at `db021da0`; the weekly job stays red until then by design |

Wizard round 2 of 6b (2026-11-17), after the review's verify; the briefs and
options were written by Fable, step n is decision 188 + n. 207 and 209 depart
from the recommended option, the others take it.

| # | Question | Decision |
| --- | --- | --- |
| 188 | Step 0, the fix scope (the checkpoint of 186; fix side ≈ 27 M, range 22–34) | **Everything confirmed** (decision 122), in four waves |
| 189 | Peer-opened exchanges on an unsecured session this node opened (R2-P6-sec-1801, -bound-1802) | **Refuse**: Sigma1, PBKDFParamRequest and Check-In open an exchange only on an inbound unsecured session; an *Ahead of matter.js* row |
| 190 | Joint commissioning without `TrustAnchors` (R2-P6-sec-1850) | **Fail closed** (VENDOR_ID_VERIFICATION_FAILED); a self-anchored lookup is a named host opt-in; `### Changed` |
| 191 | A stored CA or fabric record a constructor would replace (R2-P6-store-1851 … -1853, R2-P6-api-1859) | **Never replace**: `ErrConfig`; `CreateFabric` errors on a held fabric; an *Ahead* row |
| 192 | What "attested" means (R2-P6-sec-1803, decision 105's rule) | **The pair is withheld under test trust** (a production PAA and a CD signer that is not a test one), no rule at QueryImage |
| 193 | connectedhomeip's stricter attestation checks (R2-P6-par-1804, -1805) | **Error findings under the policy**, the pair withheld; two *Ahead* rows; `Context` gets a clock |
| 194 | The controller registry's seal mark (R2-P6-store-2304, N6a-seal-1) | **Its own sealed canary**, sqlstore schema v3, in the shape of decision 130 |
| 195 | Reseal for a controller database (R2-P6-store-2302) | **`controller.Reseal(ctx, db, old, next)`**: one transaction, both canaries last, one scrub; `sqlstore.Reseal` exported beside it |
| 196 | The radio kits' defaults towards the LAN (R2-P6-sec-2001, -2028) | **Both strict**: ESPHome keyed unless `DiscoverOptions.AllowPlaintext`; the OTBR default client refuses redirects |
| 197 | `OperationalDataset.Redact` (R2-P6-sec-2022) | **The copy carries no `Raw`**; a `by_design` row |
| 198 | R2-P6-sec-3320, the device's resumption record and its resumed session | **Keep the single-use record and record it**; close the installed session when its establishment does not finish; correct the two comments; trace the give-up path first |
| 199 | N3-coord-1, the unsecured messages of one handshake initiator | **An order key for session 0 by the initiator's node id**, where both references apply the counter check in arrival order |
| 200 | The device lifecycle methods' result (R2-P5-apidev-16, R2-P6-api-2503) | **`Rename`, `Update` and `SetReachable` return `error`**, in place, `### Changed`; the four hosts grepped for `.Update(` first |
| 201 | A configuration string beyond its constraint (R2-P6-api-2407, -wire-2502) | **Refuse it in `node.Config.Validate` and `device.New`**; the assembler's VendorName truncation follows |
| 202 | Additive API the fixes need (R2-P6-bound-1908, -api-2450, -par-3209, -api-2312, -par-3351) | **All five** |
| 203 | Decision 17's refusal set (N5b-afacade-1) | **Confirmed as built**: 21 types, 36 rows; a test pins the count |
| 204 | A part that already has an owner (R2-P6-fsm-2501) | **Move it, as matter.js** (`Endpoint.ts:706-725`); refined by 225 |
| 205 | A Sigma1 refused for the one-handshake bound (R2-P6-par-2902) | **Busy with a minimum wait, as connectedhomeip** (`CASEServer.cpp:85-122`); the chip-tool suite before the pull request |
| 206 | The status after a refused store write (R2-P6-par-2903) | **Keep the per-path FAILURE**, its scope recorded in `BD-Matter-NonVolatileWrite-StoredBeforeApplied` — a divergence by choice, the consistent reading of the order of authority |
| 207 | The 23 rows that are the reference's own behaviour ("mirrors") | **Fix every one ahead of the reference**, 23 *Ahead of matter.js* rows. Not the recommended option |
| 208 | The 575 `same` citations nobody read (decision 13) | **Read each in the fix**, a reference read of the doubtful ones and a sample, then relabel; the ratchet to the tool's count |
| 209 | The 6a rows still open at the head (≈ 45) | **All now**, a reference read for each. Not the recommended option |

Coordinator's reservations (the owner decides, the reasons are on record): 207
— an *Ahead* row is a standing duty at every pin lift (a marker re-read,
reconciliation when matter.js catches up); 20 of the 23 rows are behaviour the
reference has and none of the unbounded-growth class (decision 45).
Implementing them needs, per row, the authority the behaviour then follows, a
reference fact note per row before the code, and 23 markers in the register;
where a row turns out to be one consistent rule shared by both references with
no defect, the fix unit brings it back as a question. 209 — those files are
6a's cut and were not reviewed at this head in 6b; each fix therefore gets a
read of its surroundings, and the held-back rows stay held until their fix
merges (decisions 25, 126).

Wizard round 3 of 6b (2026-11-17), on the decision-207 rows a reference fact
note returned as a question or an interop risk; every option taken was the
recommended one.

| # | Question | Decision |
| --- | --- | --- |
| 210 | R2-P6-wire-2153, an invoke response's path not matched | **The response path into `Result`** (additive; both references hand it to the caller, connectedhomeip `CommandSender.cpp:509-513`); no discard rule (interop risk); a gap behind the references, not an *Ahead* row |
| 211 | R2-P6-fsm-2109, the granted MaxInterval unchecked | **Register only**: not performable, the missing input is the peer's IdleModeDuration; connectedhomeip leaves it unchecked too (`ReadClient.cpp:1122`) |
| 212 | R2-P6-par-2156, the status budget of a wildcard-endpoint write chunk | **Register only**: not performable, the missing input is the device's endpoint count; connectedhomeip budgets none (`WriteClient.cpp:183-204`) |
| 213 | R2-P6-err-2106, the doc of `ErrNotConnectable`'s class | **The doc corrected**, the behaviour matter.js's, no *Ahead* row |

Wizard round 4 of 6b (2026-11-17), on the first fix wave's questions. 216
departs from the recommended option, the others take it.

| # | Question | Decision |
| --- | --- | --- |
| 214 | R2-P6-sec-1803, the test-trust facts of decision 192 | **Two additive bools `attestation.Result.TestPAA` and `.TestCDSigner`**, set by `Validate`; the findings list unchanged |
| 215 | R2-P6-fsm-1860, joint-commissioning consent without a context | **`Consent func(ctx, TrustInfo) (bool, error)` in place**: the fail-safe extended before the call, the context ends the wait; `### Changed` (no host uses it) |
| 216 | R2-P6-doc-1857 (b), the signature length at decode | **A reference read of both references first**; the decoder then follows them. Not the recommended option |
| 217 | R2-P6-err-1912, `QueryUpdates` and `CheckForUpdates` always nil | **Return the per-product failures** (`errors.Join`); `### Changed` |
| 218 | 5b-dcl R8, a host outside ASCII handed to a proxy | **One rule for every proxy hand-over**: no proxy is handed a host outside ASCII |
| 219 | R2-P6-bound-1914, the OTA manager forgets a removed node late | **An additive Manager method**, wired to `client.Hooks.Deleted` by the `ctl` unit in wave 2 |
| 220 | R2-P6-bound-2102 follow-up: `DefaultMaxReportBytesPerSession` now counts memory | **16 MiB** (≈ 120 000 scalar attributes; was 2 MiB of payload, ≈ 80 000); `### Changed` |
| 221 | R2-P6-bound-2104, how far the binding manager releases a peer it created | **Disconnect only**; the peer stays in the set, nothing another holder may use is closed |

Coordinator, no owner step (the order of authority): R2-P6-doc-1857 (b) —
matter.js models it (no length at decode, judged downstream), so the hand
decoder stays and its 64-byte cap is lifted; the generated types are not used
for the two signatures.

Wizard during 6b's second fix wave (2026-11-17/18); every option taken was the
recommended one. 225 refines 204 and 237 refines 222 after reference reads.

| # | Question | Decision |
| --- | --- | --- |
| 222 | R2-P6-bound-2362, two nodes claim one extended address or RLOC16 in the topology | **A collision resolves to neither node and is logged once**, no new cap; a divergence paragraph from matterjs-server `0bd06f22` in `BD-matterjs-server-NetworkTopology` |
| 223 | R2-P6-bound-2357, the set of warned unknown events | **256 per node**, a named constant, one "further unknown events are not logged" line; an *Ahead* row |
| 224 | R2-P6-api-2751, `cluster/lock` declares `StateSource` a second time by hand | **An alias of the generated type**, `### Changed` (one declaration, as every kit port; no host imports `cluster/lock`) |
| 225 | R2-P6-fsm-2501 again, after a reference read: matter.js moves an endpoint whose construction has started onto an owner whose parts are ready only by throwing, which leaves it without an owner (`Parts.ts:64-70`, `Construction.ts:284-287`, `Endpoint.ts:706-725`) | **Refuse the move of a part bound to a running node** (a clean refusal, the part stays with its owner); before start it moves, as matter.js; the two Aggregator refusals stay; `BD-Matter-Device-PartBoundWithItsOwner` with the liveness qualification |
| 226 | R2-P5-apidev-16 rest: the second label write in `Endpoint.SetNodeLabel` is dropped and unlocked | **`SetNodeLabel(label) error`, locked, the bridge route returns it**; `### Changed`; unit `fx-open` in wave 3 |
| 227 | R2-P6-par-2713 (replaces the gen unit's second question — its basis "no id differs by direction" is false: Thermostat command 2 is a response at the pin and a request at v0.13.0, measured by a real level-generation run) | **The level extractor writes request and response ids separately; the six levels re-extracted** (an own commit, its diff read) |
| 228 | R2-P6-sec-2401, the limit half: the node's own Operate entry at a full per-fabric ACL | **As matter.js** (it appends, `AccessControlServer.ts:154-156` before `:178-181`), the consequence in the findings register; headroom is a later, separate decision |
| 229 | R2-P6-api-2409, the bound of the mDNS host-name override | **40 bytes, as connectedhomeip** (`src/lib/dnssd/Constants.h:32-36`) |
| 230 | N4-coresys-1, BootReason SoftwareUpdateCompleted on a start into the recorded version | **An *Ahead* row**, marker `OtaSoftwareUpdateRequestorServer.ts:153-155`, source connectedhomeip `ESP32/DiagnosticDataProviderImpl.cpp:192-201` |
| 231 | R2-P6-sec-3320, the rest: the record was single-use only from the upsert (1 to 6 of 16 concurrent copies resumed, measured) | **The responder claims the resumption id at lookup**, so the record is single-use from acceptance |
| 232 | R2-P6-sec-3320: an initiator's failure StatusReport on Sigma2_Resume | **Keep the installed session, as matter.js** (`CaseServer.ts:211`, `:216`; connectedhomeip discards it, `CASESession.cpp:2583-2593`); the report is not authenticated |
| 233 | R2-P6-sec-2412, the initiator role | **Abort in both roles, as connectedhomeip** (`CASESession.cpp:476`, `:529`, `:423-433`), after a reference read of matter.js's CaseClient |
| 234 | R2-P6-err-3112 / W6-test-17, a value of the wrong Go type on an attribute write | **INVALID_DATA_TYPE per path** (matter.js's validation, `action/errors.ts:55-57`); commands keep INVALID_COMMAND |
| 235 | Decision 190 on the device side: `device.NewJointFabricAdministrator` cannot take a trust-anchor lookup | **An additive port field in the generator** (`script/clustergen/ports_jointfabric.go`); unit `fx-open` in wave 3 |
| 236 | R2-P6-sec-2361, `controller.PingAddress` pings any address | **Refuse an address that is not in the node's record** (`ErrConfig`), `### Changed`; both hosts that use it pass `Record().Addresses` |
| 237 | R2-P6-bound-2362 again (an RLOC16 is unique only within one Thread network, matterjs-server `topology-utils.ts:422-451`) | **An RLOC16 collision counts per ExtendedPanId**; extended addresses are global |

Coordinator, no owner step: R2-P6-bound-2203, -2210 and -2362 get no *Ahead of
matter.js* row (the marker check reads matter.js markers only) — divergence
paragraphs in the `BD-matterjs-server-*` entries instead. Step 17's wording
carries connectedhomeip's 16-bit cap and the watchdog condition.

Coordinator, no owner step (the ctl unit's first-round questions):
R2-P6-bound-2362's "logged once" → an optional `topology.Config.Logger`
(additive, the module's `Config.Logger` pattern), implied by 222;
R2-P6-err-2307's residual (a cluster replaced under the same id during a
flush) → closed by marking the replacement dirty (122), later revoked when a
reference read measured the residual not real; N3-rwrace-2's BLE-connect half
→ `fx-open`.

Wizard during 6b's third fix wave (2026-11-18); the fact notes behind it were
written by Fable. 243 departs from the recommended option, the others take it.

| # | Question | Decision |
| --- | --- | --- |
| 238 | R2-P6-sec-2616 (a decision-207 row, doc only) | **The doc sentence** on `ApplyUpdate` and in the README's OTA part, citing matter.js `OtaSoftwareUpdateRequestorServer.ts:1121`; no ledger-checksum behaviour |
| 239 | R2-P6-par-3102 (a decision-207 row, a label) | **No code**; the 3101 (b) case asserts that the old session is neither lost nor closed and that `NewestSession` is the device's; one register sentence (`SessionManager.ts:703-708`) |
| 240 | U3-ctl-lan-1, the eviction half | **Evict the records of names no resolution observes first** (a second order list); a per-name bound is a later decision |
| 241 | The exported additions of F-db021da0-13, -43 and -57 | **All three allowed**: an optional peer-schedule interface, an `OperationalSessionLookup` method and an `operational.Entry` accessor; a `mattercore.BridgedConfig` field; `MatterWriteCommit` on the cover and fan servers; `### Added` each |
| 242 | F-db021da0-51 | **Split**: a chunked write on a client-stack session is refused whole (fails closed) until ADR 0032's session layer — unit `fxo-ctl`; the rest stays with ADR 0032 |
| 243 | R2-P6-sec-3320 (the dev6a unit's R7): the responder's claim is not released when the session is not installed | **An exported field on `sigma.Sigma1ProcessResult`**, which `CaseAdapter.ProcessSigma1` calls when the session is not installed; `### Added`. Not the recommended option |
| 244 | `handshakeStake.resolved` on a refused Sigma1 | **Commit only at acceptance**: an optional capability on the resolver (an unexported interface), which sigma calls at its commit points; no exported move |
| 245 | TM R3.7, the chunk waits a fabric holds | **A new exported bridge setter for the fabric count**, wired by `node`; each fabric's share of the chunk waits applies from half the dispatch budget |
| 246 | F-db021da0-55 | **Export a MEI helper from `cluster/spec`**, used by the dispatcher; `### Added` |
| 247 | R2-P3-rr-ctl-02 (held back, decision 25) | **Decided, no code**; the decision's text is held back with the row (decision 25) |
| 248 | `camera.ErrReportsInUse` | **An additive sentinel that also matches `ErrInvalidArgument`** (`errors.Is`); `### Added` |
| 249 | R2-P6-doc-3084, ADR 0026's principle 7 | **Two findings-register rows** (the OTA requestor's image, the DCL snapshot fetch), fixed in phase 7; a dated note in the ADR points to them |

Coordinator's reservation: 243 — the owner chose the precise fix over
recording; it adds one exported field to `secure/sigma` for a cost of one
extra full CASE per occurrence. No objection on architecture: it is the
precise cut, and the field is documented as the responder's own claim release.

Coordinator, no owner step: R2-P6-test-3110 → recorded as covered by the
`client/exchange` rollover tests (no production seam for one test).
F-db021da0-20's three kinds (a non-member enum, a member outside the field's
list, an undefined bitmap bit) → follow matter.js after a reference item on
`spec.CheckRequest`. R2-P6-fsm-1593's `SetSupportedMaps` and a failed identity
reload → reference items first. R2-P6-wire-2654 → a clause in the example's
existing entry, no new *Ahead* row. The random-per-start salt, the node's
per-window salt and three response-command differences of the guards unit →
reference items. F-db021da0-43 lands as the field 241 allows.

At the end of 6b's third fix wave and during the fourth (2026-11-18); every
option taken was the recommended one.

| # | Question | Decision |
| --- | --- | --- |
| 250 | A failed identity reload after UpdateNOC keeps answering CASE with the old identity | **Withdraw**: the error arm forgets the identity and withdraws the old instance; the stored fabric stays (connectedhomeip deletes it, `FabricTable.cpp:2064-2070`, not followed) |
| 251 | One `Commissioning.Salt` for every basic window | **As matter.js, a fresh salt per window** (`DeviceCommissioner.ts:130-137`, `:214`); a new row for `node/`; `### Changed` |
| 252 | The guard on SQLite DSNs: the examples' `--seal-key file:<path>` sites (8) | **Extend `keyReferences`** with exact counts (the reference bridge's tests 3, the reference controller's 5), the guard's counted mechanism, as for `--ca-key` |
| 253 | CLAUDE.md:26 | **`make ci-net` names its three guards** |
| 254 | Wave 3's fixture run: the helper branch was never pushed | **The coordinator pushes the helper branch and dispatches `chiptool.yml` with `update_fixtures`** (run 44252157107: all green, the artifacts byte-identical, no fixture commit) |
| 255 | The citation sample: the ctl unit's relabels 3 of 10 off (the others 0 of 31) | **A reference read of all 86 of ctl's reason-for-behaviour citations** (70 read, 23 corrected, boundaries only) |
| 256 | The verify's "confirmed" ranges were overlap-checked (7 of 12 off at ctl) | **A boundary read of the 111 verify-sourced citations of the cli, sec, node and clu units** (cli 10 of 39, sec 5 of 25, node 5 of 18, clu 1 of 9 off) |
| 257 | `DAV:n` short citations at `0ceb202d` under a `db021da0` label | **Re-measured in 6b, and the citation check learns the DAV alias** (46 sites in seven files and a package doc) |

Coordinator, no owner step (the reference items of wave 3): F-db021da0-20 is a
test fix only (`spec.CheckRequest` already refuses the three kinds; the
skipped test asserted the decoder). R2-P6-fsm-1593's `SetSupportedMaps` drops
the areas of a removed map, as connectedhomeip; the refusal goes. The three
response-command differences are classified by the guards unit. Two items need
nothing beyond register lines. The class (ii) reason texts are reworded;
"go-fabric serves no Descriptor TagList" is false at `b5ffb4c9`, so a new
findings row.
