# Matter behaviour-parity findings register

!!! info "Who this page is for"
    Contributors and AI agents implementing Matter-side fixes. This is a
    **work backlog**, not an audit-trail report: each entry is a scoped,
    independently testable fix package against matter.js HEAD. It complements
    the [behavioural-parity contract](../../docs/matter-parity-contract.md) (which
    defines *what* parity means) and [`by_design.md`](./by_design.md) (the
    catalogue of *intentional* divergences). Nothing here is a deliberate
    divergence — every item is a fix, and closing one removes it from this list.

**Method.** Code-against-code review of `**` (this module) against the
matter.js HEAD checkout at `../matter.js` (Go file:line vs matter.js file:line).
Comments and prior parity docs were deliberately ignored; only running code was
trusted. Scenario scoped to this deployment: a pure Matter **bridge**
(server/responder only) — RootNode + Aggregator + bridged HomeMatic endpoints,
Ethernet/IP only, commissioned and controlled concurrently by Apple Home,
Google Home, Alexa and chip-tool.

**Confidence.** The five CRITICAL items were re-verified in code by hand
(C1, C3, C4, the C5 chain, App-C0 — all confirmed). HIGH/MEDIUM items come from
single-pass subsystem review: before implementing one, read the cited matter.js
source yourself (that is the contract rule anyway) and confirm the Go side still
matches these line references.

**Closed entries.** Since 2026-11-13 (review II, W6-reg-10) this register holds
open entries only. Every closed entry — the Tier 0 and Tier 1 findings, the
fixed Tier 2 bullets with their batch notes, three closed bullets of the pin
and phase-33 sections and the two "Closed in …" appendices — stands verbatim in
[`matter_behaviour_findings_closed.md`](./matter_behaviour_findings_closed.md),
and closing an entry moves its text there.

**Triage 2026-10-29 (owner decision on report 27-C Q1).** Every open
bullet below carries a `Triage 2026-10-29` tag: the post-pause waves H1
(the device IM and write path), H2 (commissioning and security, Fable's
paths), H3 (node options and the embedding layer) and C1 (the controller
remainder two), or "owner" (a hand test or re-audit), "when a consumer
asks", or "harness note". The waves are planned when the owner ends the
pause after `v0.14.0`; the tag replaces the milestone tag the raising wave
gave. The camera persistence bullet left the register for
`BD-Matter-CameraPersistenceIsTheHosts` in `by_design.md`.

**Milestone 6 sweep (wave 27-C, 2026-10-28).** 61 open items before the
sweep, 57 after. Closed and removed: the discovery errors and abort (wave
27-A, `client/discovery` sessions), the controller-side software-update
manager (milestone 5 phase 25, brief CM, `controller/softwareupdate`), the
node's session intervals (wave 27-B, `node.Config.Sessions`) and the
reported session parameters on the device (#326, `node.SessionInfo`). Kept
with a decision: the camera persistence (owner decision Q5 of brief 26-A1),
SetRegulatoryConfig's CountryCode (owner decision, an Apple Home check
first), ColorControl's primaries (brief BR decision 19.4), the Wi-Fi
fail-safe binding (before milestone 7, report BA). Every other open item
still carries the milestone tag of the wave that raised it; that milestone
has passed without it, and its new milestone is an owner decision (report
27-C, Questions).

**How to use an entry.** Each finding gives (1) the Go defect with file:line,
(2) the matter.js reference to mirror, (3) the failure it produces in the bridge
scenario, (4) a fix package a mid-level implementation agent can execute,
including the test to add. Cite the matter.js `path:line` in the Go code you
write, per the contract.

---

## Tier 2 — MEDIUM

**Core clusters**
- SetRegulatoryConfig ignores CountryCode (Location stays "XX"). `cluster/core/general_commissioning.go:629` vs `GeneralCommissioningServer.ts:210`. **Still open (2026-10-13):** deferred by batch 1 with its reason (Apple Home sent RemoveFabric after a non-empty Location); `bridge/fields_reader.go` (`decodeSetRegulatoryConfigRequest`) reads the field as octets while a UTF-8 string lands in `el.String`, so CountryCode is always empty, and `general_commissioning.go` (`handleSetRegulatoryConfig`) checks only the length. Fix package: decode the string, store it as Location, and verify against Apple Home in concept v2 phase 5 before it ships. **Review II, phase 6a (decision 127):** still true — owner decision pending (the revert's reason is Apple Home, outside matter.js); `bridge/fields_reader.go` still reads `el.Octets` for the UTF-8 field, and `handleSetRegulatoryConfig` is `cluster/core/general_commissioning.go:1133-1173` at `1a0d8ba9` (R2-P6-doc-1019). The matter.js lines of the row were not re-read at the pin (unverified).

---

## Matter 1.6.1 pin — open items

Raised when `parity/schema.json` moved from matter.js `f07365a8` (Matter 1.6.0)
to `85cf6647` (Matter 1.6.1). The schema-level numbers (cluster and
device-type revisions, SpecificationVersion, DataModelRevision, IM revision,
Thermostat access strings) were followed in the same change; the items below
are the behaviour the new pin implies and this module does not have yet.

The item this section opened with — Groupcast (0x0065) on the root with the
AccessControl Auxiliary ACL, which the 1.6.1 device library requires for the
light and plug types (RootNode `GroupcastListenerCond`) — is built:
[ADR 0010](../../docs/adr/0010-groupcast-and-auxiliary-acl.md), with Listener
PerGroup and, since wave 20 (report AN), Sender — FeatureMap 0x07, the
group sender in `groups/`. What remains:

- **Field conformance does not hold the host's own values.** matter.js validates every commit, the host's included (`ValueValidator.ts:392-467`, `conformance.ts:19-60` at `862ad75f`). Since brief CT `spec.Instance.CheckConformance` holds a controller's write of a struct attribute on a generic server; the host's `Initial`/`Set` and the checks self-held servers call are not held, because the reference bridge's CHIP-mirrored tariff thresholds (both `O.b`) and DEM forecasts fail the check. Route: correct the samples (a chip-tool families run of SETRF and DEM decides what they publish), then hold the host path; brief CT report, question 1. (Triage 2026-10-29: wave H1, the device IM and write path.)
- **A command response is held to field presence only.** matter.js validates the whole response payload (`ProtocolService.ts:725-740` at `862ad75f`; DoorLock switches the constraint off per response, `DoorLockServer.ts:704-707`); the Go server checks which fields a response carries, not their enum values or nested structs. Route: a response check of its own — the request check closed without it (review II, phase 4, owner decision 64), and matter.js's response relaxations (Groups' AddGroupResponse, ViewGroupResponse and RemoveGroupResponse, `GroupsServer.ts:43-49` at `db021da0`) are not in the relaxation table. (Triage 2026-10-29: wave H1, the device IM and write path.)
- **Groupcast's membership quota does not follow a raised `MaxGroupsPerFabric`.** At `db021da0` matter.js states `MaxMembershipCount` as a constant, `override maxMembershipCount = 44;` (`packages/node/src/behaviors/groupcast/GroupcastServer.ts:947`; the comment `:944` "Set to 2 * GKM.maxGroupsPerFabric (44)"; `GroupKeyManagementServer.ts:614` `maxGroupsPerFabric = 22`) — no derivation (review II, phase 6a, R2-P6-doc-1707: the row's "derives … from `maxGroupsPerFabric × 2`" was wrong for this pin). What remains is the module's own coherence with a raised limit. The module's `groupcastMaxMembershipCount = 44` is a constant (`cluster/core/groupcast.go:52`); since brief CC a host can set `node.Config.Limits.MaxGroupsPerFabric` above 22 and is then still held to Groupcast's quota of 22. Route: derive the Groupcast constants from the configured group limit when the Groupcast server is composed (the wave that next touches `groups/`; M5). (Triage 2026-10-29: wave H1, the device IM and write path.)
- **Groupcast has not met a real controller.** The evidence is the in-process
  end-to-end test (`bridge/groupcast_e2e_test.go`) and the wire fixtures
  matter.js produced (`bridge/testdata/groupcast-wire-fixtures.json`).
  Unverified controller-side assumptions: that a controller provisions a
  1.6.1 node through Groupcast JoinGroup (rather than KeySetWrite /
  GroupKeyMap / AddGroup) and sends on FF05::FA for IanaAddr; that it relies
  on `UseAuxiliaryAcl` instead of writing a Group ACL entry; and that
  chip-tool's `groupcast` commands encode the optional fields as matter.js
  does. Fix package: an `internal/chiptool` leg (`groupcast join-group`, a
  multicast `onoff toggle`, `groupcast leave-group`). **Review II, phase 6a (decision 127, at `16a8f6e5`):** still true — an evidence gap, not a code row; no `internal/chiptool` groupcast leg was looked for (unverified).

## matter.js pin 862ad75f — open items (pin lift 2026-10-18)

What the lift from `0ceb202d` to `862ad75f` showed (`pin-lifts.md`, first
section): upstream fixes this module lacks. Each closes with the wave that
ports the matter.js test named, or earlier when a consumer needs it.

Device side:. (Triage 2026-10-29: owner's hand test (manual test list C5), no wave.)

- **A "startup" advertisement is not turned into "retransmit" when the
  fabric holds a subscription.** matter.js's `#advertiseFabric` converts a
  "startup" event into "retransmit" — one broadcast instead of the full
  schedule — when any session of the fabric holds a subscription, and
  advertises nothing while the advertiser closes
  (`DeviceAdvertiser.ts:356-380` at `862ad75f`). `bridge.Bridge.AnnounceFabric`
  publishes the operational service, and the responder starts the full
  "startup" schedule on every publish (`mdns/responder.go`, `Publish`);
  `mdns.Advertiser.Publish` carries no event. Fix: an event on the publish
  (or a `Publish` variant the bridge calls with "retransmit" when the
  subscription manager holds a subscription of the fabric), with a parity
  case for a restart that re-establishes subscriptions (issue #362). **Review II, phase 6a (decision 127, at `16a8f6e5`):** still true as far as read — `Bridge.AnnounceFabric` publishes, and the responder has the retransmit event (`mdns/responder.go`) but the publish carries none. `DeviceAdvertiser.ts:356-380` was cited at `862ad75f` and not re-read at the pin (unverified).
- **A subscription that ends while the node runs stays persisted.**
  `im/subscription/manager.go` (`Manager.Release`, `finishClose`) keeps the former matter.js line: closed
  on session close = not terminated = re-established after a restart.
  matter.js #4742 drops the stored subscription whenever it ends while the
  node runs; only `closeForShutdown` leaves it for re-establishment
  (`Subscription.ts:21-40`, `ServerSubscription.ts:766-812`,
  SubscriptionsServerTest "drops the stored subscription when the peer closes
  its session while the node keeps running"). Fix: terminate on every close
  except the one `Stop` performs. (Triage 2026-10-29: wave H1, the device IM and write path.) **Review II, phase 6a (decision 127, at `16a8f6e5`):** still true — `Manager.CloseSession` closes without terminating (`im/subscription/manager.go`), called for a session that went away (`node/security.go`). matter.js at the pin: `close()` is `#close(true, …)` and marks terminated, only `closeForShutdown` does not (`packages/node/src/node/server/ServerSubscription.ts:768-774`, `:800-809` at `db021da0`); the store's drop on termination (`SubscriptionsServer.ts`) was not re-read (unverified).
- **Utility component on the composing endpoint is not a candidate.**
  `endpoint/devicetype_conformance.go:842-850` `candidatesOf` iterates the
  composition scope; matter.js #4745 prepends the composing endpoint when it
  lists the utility component itself (`DeviceTypeConformance.ts:596-614`,
  Core § 9.2.1; Heat Pump and Solar Power examples). A composed product that
  carries its Power Source on the composing endpoint is refused here. (Triage 2026-10-29: wave H3, node options and the embedding layer.)


Controller side:. (Triage 2026-10-29: wave H3, node options and the embedding layer.)

- **Known answers are sent with their original TTL.**
  `client/discovery/solicitor.go:368-375`; matter.js #4756 sends the
  remaining TTL rounded up and omits a record past half its TTL
  (`DnssdSolicitor.ts:280-291`, RFC 6762 § 7.1). (Triage 2026-10-29: wave C1, the controller remainder two.)
- **A status the peer sent is not told from a local one.**
  `client/interaction/interaction.go:56` `StatusError`; matter.js #4750
  `ResultStatus.received` (`ResultStatus.ts:1-35`). Low. (Milestone 5.)

Found by the phase-16 port of `packages/protocol/test` (brief BH, PR #164,
2026-10-18). Each closes with the fix named, or with the milestone in
brackets; the feature-sized ones carry an `until` in the test register.

Device side:. (Triage 2026-10-29: wave C1, the controller remainder two.)

- **An empty ICAC is dropped from the CASE signed data.**
  `secure/sigma/sigma.go:811-822` `signedDataBytes` omits tag 2 when the
  ICAC is empty; matter.js re-encodes the field as received
  (`session/case/CaseServer.ts:294-299`, `TlvSignedData.encode` with
  `responderIcac`), so a peer that sends an empty ICAC (`300200`) signs a
  TBS this side does not rebuild. (Triage 2026-10-29: wave H2, commissioning and security (Fable).)
- **The group sender keeps its node id after UpdateNOC.**
  `groups/sender.go:208,225` captures the fabric's node id when the sender
  is created; matter.js reads it per message from the current fabric
  (`session/GroupSession.ts:184-189`, `:204-206`). (Triage 2026-10-29: wave H2, commissioning and security (Fable).)
- **A PASE session closed by the peer does not expire the fail-safe.**
  `ExpireFailSafe` is reached from RevokeCommissioning only
  (`bridge/commissioning_window.go:724-727`); matter.js expires the
  fail-safe when the PASE session that armed it is closed by the peer
  before CommissioningComplete (`common/FailsafeContext.ts:77-86`).
  Crosses `secure/channel` and `bridge`. (Triage 2026-10-29: wave H2, commissioning and security (Fable).) **Review II, phase 6a (decision 127, at `16a8f6e5`):** still true as far as read — matter.js `packages/protocol/src/common/FailsafeContext.ts:77-86` at `db021da0` (`session.closedByPeer` → `#failSafeExpired`); here the PASE close hook is wired to the re-announce only (`bridge/securechannel.go`), and `ExpireFailSafeArmedOverPASE` has one caller, the revoke (`bridge/commissioning_window.go`; no other caller found by grep, inferred).
- **Session parameters: a local MaxPathsPerInvoke of 0 is not refused and
  an oversized wire value is masked.** `secure/sessionparams/sessionparams.go:182`
  keeps the low 16 bits; matter.js refuses the local value
  (`session/SessionManager.ts:368-374`) and takes the fallback for anything
  a uint16 cannot carry (`session/SessionParameters.ts:65-80`). Low. (Triage 2026-10-29: wave H2, commissioning and security (Fable).)
- **The device CSR subject is CN=CSR.**
  `cluster/core/operational_credentials.go:1481-1483` uses commonName,
  `commissioning/csr.go:84` an empty subject; matter.js uses
  organizationName "CSR" (`certificate/kinds/Certificate.ts:264-267`). A
  NOCSR wire difference every controller so far accepted. Low.

Controller side:. (Triage 2026-10-29: wave H2, commissioning and security (Fable).)

- **A whitespace-only `issuer_name` in an installed revocation set matches
  every issuer.** `controller/dcl/revocation.go:688-701` decodes it to no
  issuer and `:166-173` then matches any; matter.js keeps the empty hex
  string, which matches nothing (`dcl/DclCertificateService.ts:97-109`,
  `:125-127`). Revokes more, never less. Low. (Triage 2026-10-29: wave C1, the controller remainder two.)
- **The close cause of a PASE session that lost the race.**
  `controller/commissioner/connection.go:252` `launch`; matter.js closes it
  with the composed signal's reason (`peer/CommissioningConnection.ts:140-151`).
  Low, no deterministic test. (Triage 2026-10-29: wave C1, the controller remainder two.)
- **PQC Phase 1 on the device side.** A device's own ML-DSA attestation
  chain files are refused by the 600-byte limit (`secure/attestation/files.go`;
  matter.js allows 10 240 bytes for a PQC chain, `certificate/kinds/common.ts:19`).
  The controller side is ported behind the forward-feature switch (wave
  27-A, `controller/attestation/pqc.go`). (Milestone 6.)

Found by the phase-16 port of `packages/node/test/node` (brief BI, PR #168,
2026-10-19). Line numbers at `862ad75f`.

Device side:. (Triage 2026-10-29: wave H2, commissioning and security (Fable), once matter.js ships the forward feature for the device side.)

- **A write path without cluster or attribute is answered per path.**
  `im/write.go:292-298` appends an AttributeStatus of INVALID_ACTION for
  the path and goes on; matter.js and chip refuse the whole interaction
  with a StatusResponse of INVALID_ACTION
  (`action/server/AttributeWriteResponse.ts:278-285`, `:305-312`; chip
  `WriteHandler.cpp` `GetConcreteAttributePath` → `SuccessOrExit`). (Triage 2026-10-29: wave H1, the device IM and write path.)
- **SetRegulatoryConfig's CountryCode never reaches Location.**
  `bridge/fields_reader.go` (`decodeSetRegulatoryConfigRequest`) drops the field on decode, a deliberate
  revert after an Apple Home regression; matter.js writes it to
  BasicInformation.Location (`GeneralCommissioningServer.ts:146-170`,
  `:206-208`). Open: the revert's reason is outside matter.js and needs a
  re-audit with Apple Home before it moves. (Owner decision.)

Controller side:


Found by the phase-18b security and commissioning port (brief BO, PR #184, 2026-10-19).
Line numbers at `862ad75f`. (Triage 2026-10-29: owner: a re-audit with Apple Home before it moves (manual test list), no wave.)

- **The effective ACL is applied entry by entry.** matter.js applies a
  written ACL to the manager when the interaction ends
  (`AccessControlServer.ts:545-560` `#handleInteractionBegin`/`End`,
  `:598-602` `#applyDelayedAclUpdateFor`); a chunked Go write is in force
  per entry, so a concurrent request can meet a partly written list. One
  `AccessControlAuxiliaryTest.ts` case is `missing` for this. The change
  is the write path's, not `cluster/core/access_control.go`'s alone: the
  bridge's write dispatch would collect a CASE session's ACL list items per
  fabric for the interaction (matter.js `#prepareAclUpdateFor` at
  interaction begin, `#delayAclUpdateFor` per write), the access check
  keep reading the list in force, and the interaction's end store the
  collected list as one replacement (`#applyDelayedAclUpdateFor`) —
  more than a few dozen lines in `im/`, `bridge/` and the ACL server,
  so brief BR left it. (Triage 2026-10-29: wave H2, commissioning and security (Fable).)
- **No extension access-check hook.** `extensionEntryAccessCheck`
  (`AccessControlServer.ts:441-450`) is an overridable check per entry; Go
  has none. (Triage 2026-10-29: wave H2, commissioning and security (Fable).)

- **FailSafeExpiryLengthSeconds is 900 where matter.js seeds 60.**
  `GeneralCommissioningServer.ts:39-41` seeds 60 s (900 s for
  MaxCumulativeFailsafeSeconds, `:44-46`); `NewGeneralCommissioning` floors
  `FailSafeMaxSeconds` at 900 (`cluster/core/general_commissioning.go:226-232`)
  and refuses a longer ArmFailSafe with ValueOutsideRange (`:594-599`), a
  check matter.js has not. (Milestone 3, phase 19; decide together with the
  SetRegulatoryConfig CountryCode item below.) Review II, phase 6a
  (R2-P6-doc-1019): still true; the floor is
  `cluster/core/general_commissioning.go:327-334` at `1a0d8ba9`.

Found by the phase-19 port (brief BR, 2026-10-20). Line numbers at
`862ad75f`. (Triage 2026-10-29: wave H2, commissioning and security (Fable).)

- **The Groupcast server has no Listener-only configuration.** The module
  builds Groupcast with Listener, Sender and PerGroup
  (`GroupcastServer.ts:81-83` as RootEndpoint installs it); a node without
  the Sender feature (`ListenerOnlyRootEndpoint`) cannot be configured, so
  one `GroupcastServerTest.ts` loop case is `missing`. (Triage 2026-10-29: wave H3, node options and the embedding layer.)
- **The concentration servers serve NumericMeasurement only.** Every
  `cluster/measurement` concentration server is bound to the MEA
  selection (`concentration.go` `concOptions`), and its host port
  carries a number (`contract.FloatMeasurementSource`), so the
  LevelIndication feature and its LevelValue cannot be served; two
  `ConcentrationMeasurementServerTest.ts` cases are `missing` for this. (Triage 2026-10-29: wave H3, node options and the embedding layer.)
- **The controller chooses no TCP session.** `PeerExchangeProvider`'s
  selection of a TCP session among a peer's sessions (skipping closing
  ones) has no Go counterpart: `client/peer` opens UDP sessions. Four
  `TcpSessionBindingTest.ts` cases are `missing` for this. (Register
  candidate, with a controller TCP transport.). (Triage 2026-10-29: wave C1, the controller remainder two, with the controller's TCP transport.)
- **The event store is volatile only.** matter.js keeps the events of a
  former run with `events: { nonvolatile: true }`
  (`EventsBehaviorTest.ts:45-54`); the Go node's event log lives in
  memory and persists only the event-number ceiling. One
  `EventsBehaviorTest.ts` case is `missing` for this. (Milestone 4, a
  node option over the store.) **Review II, phase 6a (decision 127, at `16a8f6e5`):** still true — `im/eventlog.go` persists only the number's ceiling, not the records; matter.js has `NonvolatileEventStore.ts` at the pin (file present; not read).

Found by the phase-18d Binding port (brief BQ, PR #189, 2026-10-20).
Line numbers at `862ad75f`. (Triage 2026-10-29: wave H3, node options and the embedding layer.)

- **A group write leaves no log line.** matter.js logs every write at
  info (`ClientInteraction.ts:317`); `groups.Sender.Write` logs nothing,
  where `Invoke` does since BQ (`matter.groups.invoke`). No case asks for
  it. (Milestone 3, phase 19, with the next `groups/` fix.)

Found by the phase-18a system-set port (brief BN, PR #182, 2026-10-19).
Line numbers at `862ad75f`. (Triage 2026-10-29: wave H1, the device IM and write path.)

- **The groups manager holds no key set 0.** `KeySetsTest.ts` reads the
  only key of the default key set and the second-newest key of the IPK
  key set (§4.14.2.6); `groups/manager.go` loads key sets from 1 and the
  CASE IPK lives in `secure/`, so the two cases stay `missing`.
  (Milestone 3, wave 18b, the security path.)

Found by the phase-17d DoorLock port (brief BM, PR #176, 2026-10-19).
Line numbers at `862ad75f`. (Triage 2026-10-29: wave H2, commissioning and security (Fable).)

- **`clock.Clock` has no monotonic reading.** matter.js measures the
  wrong-code lockout on `Time.nowUs` (`DoorLockServer.ts:1184`, `:1192-1195`)
  and its tests step the wall clock apart from the timers
  (`DoorLockServerTest.ts:1366` "measures the lockout on the monotonic
  clock", register `missing`); `clocktest.Fake` has one reading, so a
  wall-clock step moves a deadline kept as `time.Time` too, and the case
  has no in-process port. Under `clock.System` the comparison is monotonic.
  The phase-18a port (brief BN, PR #182) hit the same wall: "keeps the
  operational time counted across a backward wall-clock step"
  (`GeneralDiagnosticsOperationalHoursTest.ts:27-40`) steps the wall clock
  alone and stays `missing`; its UpTime twin runs on a package seam
  (`core.UpTimeFrom`) instead of the clock.
  Fix: a monotonic reading on `clock.Clock` and `clocktest.Fake`. (Triage 2026-10-29: wave H3, node options and the embedding layer.)
- **An ExpiringUser provisioned without ExpiringUserTimeout logs no
  warning.** `#warnIfExpiryUnbounded` (`DoorLockServer.ts:1233-1240`, run
  by SetUser Add and Modify, `:291`, `:328`, `:480`) warns that the user's PIN is
  denied until the attribute is set; `cluster/lock/users.go`
  `setUserLocked` denies as matter.js does (`authorizeUserLocked`) but
  logs nothing. (Milestone 3, next lock change.)

Found by the phase-17c Thermostat port (brief BL, PR #175, 2026-10-19).
Line numbers at `862ad75f`. (Triage 2026-10-29: wave H3, node options and the embedding layer.)

- **A staged Presets write bumps the DataVersion and is re-reported.** A
  controller's write of Presets inside an atomic write is staged in the
  write's state and answers SUCCESS (`cluster/thermo/atomic.go`
  `writeAtomicAttribute`); the dispatcher then treats it as a stored
  write, bumps the Thermostat's DataVersion and announces Presets, though
  the stored presets did not change. In matter.js the setter hands the
  value to the `AtomicWriteHandler` (`ThermostatServer.ts:1702-1716`,
  `AtomicWriteHandler.ts:243-266`) and the write's transaction has nothing
  to commit, so neither the version nor a report moves until CommitWrite.
  Fix: a cluster server's write reports "staged, not stored" and the
  dispatcher skips the bump. (Triage 2026-10-29: wave H1, the device IM and write path.)

Found by the phase-17b Switch server (brief BK, PR #171, 2026-10-19). (Triage 2026-10-29: wave H3, node options and the embedding layer.)

- **The materialised Switch server runs on the system clock.**
  `contract.MeasurementContext` carries `EndpointID` only
  (`contract/matter.go:332-338`), so the MomentarySwitch materialiser
  (`cluster/measurement/materializers.go` `switchConfig`) builds
  `cluster/switch` on `clock.System` even under `fabrictest`; matter.js's
  behaviors take `Time` from the node's environment. A bridged button's
  long-press and multi-press timers cannot be driven by a fake clock in an
  in-process test. Fix: the context carries the node's clock (an additive
  field). (Triage 2026-10-29: wave H3, node options and the embedding layer.)
- **TC-SWTCH-2.6 is not covered.** It needs ActionSwitch, which excludes
  MomentarySwitchRelease (`switch.element.ts:25-27` at `862ad75f`: MSR
  "[MS & !AS]", MSM "AS, [MS & MSR]"); the daemon's one button serves MSR.
  A second switch endpoint (MS, AS, MSL, MSM, matter.js's all-clusters
  `ep3`) would move the composition and the IDM goldens. Harness note, not
  a parity gap; `internal/chiptool/families_table_test.go` classifies it.
  (With the next CHIP image lift, if at all.)

Found by the phase-17a port of the OnOff, LevelControl, ColorControl,
Identify, WindowCovering and ServiceArea tests (brief BJ, PR #170,
2026-10-19). Line numbers at `862ad75f`. (Triage 2026-10-29: harness note, the next CHIP image lift (not a parity gap).)

- **ColorControl `MatterQuiesce` leaves the quieter's pending-report
  timers.** `cluster/light/colorcontrol_transitions.go` stops the engine
  only; the LevelControl fix of PR #170 (`MatterQuiesce` drops the held
  report with its timer, `LevelControlServer.ts:765-770`,
  `Observable.ts:1276-1280`) applies to the colour attributes' quieters
  too. No matter.js test checks it. (Milestone 3, with brief BL's
  in-wave fixes or the next lights wave.)

Found by the phase-31 port of `packages/node/test` (brief phase 31,
2026-11-03). Line numbers at `862ad75f`.

- **A corrupt endpoint allocation counter fails the allocation.**
  `endpoint/sqlitestore` `allocate` refuses a counter below the first part
  number (and a non-integer one fails its scan); matter.js starts over from
  the first number (ServerNodeTest.ts:941-957, "starts over from 1 when the
  next number in storage is corrupt", stays missing).
- **No flushing close of a subscription.** `Manager.Close` drops the
  queued changes; matter.js's close of a subscription sends a report queued
  behind the one in flight first, despite a deferral
  (ServerSubscriptionTest.ts:1123-1195 and :499-555 stay missing). **Review II, phase 6a (decision 127, at `16a8f6e5`):** narrowed — the flush exists for a leaving fabric and a session rollover (`Manager.FlushFabric`, `FlushSession`, `im/subscription/manager.go`). What remains: the two `ServerSubscriptionTest.ts` cases the row names; `script/testparity/register.json` holds one `partial` entry for the whole file, so the two could not be told apart (not performable from the register as it is). How the flush differs is R2-P6-par-0702.

Found by the phase-32 port of `packages/node/test/behaviors/ota/OtaTest.ts`
(brief phase 32, 2026-11-04). Line numbers at `862ad75f`.

- **The reboot armer's two OtaTest.ts cases are not ported yet.** matter.js
  arms `RebootResubscribeArmer` at Applying and keys on the first session
  the device opens after its restart (`RebootResubscribeArmer.ts:117-163`):
  the NotifyUpdateApplied session to the provider or the device's
  re-established subscription; then it closes the older sessions
  (Mechanism A) and forces a re-subscription when no report arrives within
  30 s (Mechanism B). Since phase 37 both sessions reach
  `controller.sessionAdded`: the provider is the controller itself and the
  controller publishes its operational record (issue 410), so a device
  resolves it to re-establish a persisted subscription. What stays open is
  the port of OtaTest.ts:706-813 ("closes older sessions and does not
  re-subscribe a device that feeds its subscription") and :815-890 ("force
  re-subscribes (Mechanism B) when the subscription is not fed within the
  grace window") onto `fabrictest`, with the site answering the
  controller's operational record. Route: phase 37 wave D.

## Application cluster servers — open items. (Triage 2026-10-29: wave H1, the device IM and write path.)

- **Wi-Fi NetworkCommissioning: the fail-safe check is not bound to the
  accessing fabric.** `core.WiFiNetworkCommissioning` gates its commands
  on `GeneralCommissioning.FailSafeArmed()` (any fabric), chip on
  `failSafeContext.IsFailSafeArmed(fabricIndex)`
  (`NetworkCommissioningCluster.cpp:157` at the harness pin). Fix: carry
  the accessing fabric into the invoke and expose `FailSafeArmedBy(fabric)`
  on `GeneralCommissioning` (it already keeps `failSafeFabricIndex`,
  `general_commissioning.go:50,533`). TESTING ONLY behind `--wifi-sim`
  today; must land before milestone 7 makes the server real. (Phase 12,
  report BA.)

---. (Triage 2026-10-29: wave H2, commissioning and security (Fable), before milestone 7 makes the Wi-Fi server real.)

## Phase 33 — the general, model and types test roots (matter.js 862ad75f)

Cases of the six phase-33 register roots (`notes/parity/matterjs_tests.md`)
the Go tests do not cover and no `by_design.md` entry explains. Each closes
with a port of the named case.

- **The TLV encoder writes an IS1 into a character string.** matter.js's
  `TlvString.validate` refuses a character string containing Information
  Separator 1 (0x1F) (`packages/types/src/tlv/TlvString.ts:78-81` at
  `862ad75f`); `tlv.Encoder.PutUTF8` writes it, and a peer reading the
  string cuts it there. Case:
  `TlvStringTest.ts` "throws an error if a character string contains
  Information Separator 1 (0x1F)". Route: refuse (or cut) the string in
  the encoder's checked path and decide what a host value with an IS1
  becomes.
- **A write keeps the FabricIndex its caller supplies.** matter.js encodes
  a fabric-scoped struct for a write interaction without its FabricIndex,
  even when the value carries one (`TlvObjectTest.ts` "strips fabricIndex
  for write interaction even when caller provided a value",
  `forWriteInteraction`); `spec.EncodeJSON` writes it when present. The server overwrites it with the accessing
  fabric, so the wire difference is the field's bytes. Route: strip field
  254 on the controller's write encoding.
- **Defaults matter.js derives without a stated one.** matter.js's
  `DefaultValue` gives a list whose type permits no entries the empty list,
  builds a bitmap's default from its members' defaults (bit placement
  across bit 31, a member without an upper bound, the first member to claim
  a bit), and scales a unit default by the type a declared type derives
  from (`packages/model/test/logic/DefaultValueTest.ts` at `862ad75f`,
  nine cases). `script/clustergen` writes only the default the snapshot
  states (`defaultLiteral`), scaling a unit by the declared type name
  (`encodedUnit`). Route: derive these in the generator, or have the
  extractor write matter.js's computed default into `parity/schema.json`.

- **No copyHealth for re-resolved addresses.** matter.js's
  `ServerAddressSet.copyHealth` carries an address's health markers onto an
  equal address of a new set (`ServerAddressSetTest.ts` "copies health
  markers onto equal targets"); `client/discovery` keeps health on
  `Address` (`HealthyAt`, `UnhealthyAt`) and has no copy step.

## matter.js pin db021da0 — open items (pin lift 2026-11-06)

- **F-db021da0-28: BridgedDeviceBasicInformation serves NodeLabel and SerialNumber whatever the host set.** Both are optional in the model (`packages/model/src/standard/elements/bridged-device-basic-information.element.ts:30`, `:40` at `b25ed960`); that matter.js lists an optional attribute only when its state holds a value was the fix's reading of `ValidatedElements.ts:247-279` and was not repeated (unverified). Here NodeLabel is always served, empty when the host gave none (`cluster/core/bridged_device_basic_information.go:245`), and SerialNumber is filled from the UniqueID when the host gave none (`:179-181`) — the pair matter.js warns about, "uniqueId and serialNumber shall not be the same" (`basic-information-validators.ts:42-44`). A VendorID of 0 or above 0xFFF4 and a ProductID of 0 are warnings here (`:281-301`) where matter.js's validator, which the bridged server runs (`BridgedDeviceBasicInformationServer.ts:45`), throws (`basic-information-validators.ts:32-40`). The code gives Apple's HAP mapper as the reason for SerialNumber, as the root's `BD-Matter-BasicInfo-SerialNumberAlwaysServed`; for NodeLabel it gives none. Not decided: follow matter.js, or a `by_design` entry with the owner's reason. Found in review II, phase 4 (R2-P4-coresys-05). **Review II, phase 6a (owner decisions 143 and 166):** the NodeLabel and SerialNumber half is closed — the bridged server lists an optional attribute only when its state holds a value, as matter.js (`packages/node/src/behavior/cluster/ValidatedElements.ts:252`, skip `:270-275`, add `:277-278` at `b25ed960`), and backfills nothing but UniqueID (`BridgedDeviceBasicInformationServer.ts:39-43`) (`09c32c85`); the root serves SerialNumber only when stated (`5e97e2a1`). The VendorID / ProductID half was not part of the decision and stays open.
- **F-db021da0-44: a hand-off LevelControl whose port is also the endpoint's source notifier is told of a host change on two subscriptions whose order the host decides.** The bridge subscribes the endpoint's source notifier for the paths of the source's own cluster (`bridge/subscribe.go:2091-2120`), and a LevelControl server without the transition engine subscribes to its port's notifier itself once it has a change listener (`reconcileHost`, `cluster/levelcontrol/levelcontrol_server.go:611-635`), handing a notification to its forwarded callbacks before its listeners (`onHost`, `:586-607`). When port and source are one notifier, the bridge's callback and the server's are two callbacks of the host's notifier, in the order the host calls them. By the fix's reading the server's change, when it comes first, advances the version and takes the value into the notification's memory (`quieterChanged`, `bridge/subscribe.go:2472`; `settlerFor`, `:2495`), so the notification after it finds nothing moved and reports nothing; with the bridge's half of the fix alone that showed as an advance without a report in 7 of 60 runs of `bridge:TestHostChangeAdvancesOnceOnAHandOffServer`, in a wiring where the server forwards the notifier. For the wiring of this row nothing was run, and the reading was not repeated (unverified). matter.js has one datasource per behavior, whose commit advances and broadcasts together (`packages/node/src/behavior/state/managed/Datasource.ts:1456-1457`, `:1319` at `db021da0`). Who would see it: a subscriber of CurrentLevel on such a device misses a host change until the next one. Fix: one subscription per host notifier and endpoint, or the settlement told which callback comes last; phase 6 of review II. Found in review II, phase 4 (R2-P4-fr-ver-01, second round). **Review II, phase 6b (decision 209):** not reproduced — a run of exactly this wiring (a fifth sub-test of `bridge:TestHostChangeAdvancesOnceOnAHandOffServer`, the port's own notifier as the endpoint source's) passed 300 runs, 100 under `-race`: one advance, one report, and a notification that moved nothing advances nothing. Both callback orders are inferred from Go's random map iteration over the runs (the test notifier keeps its callbacks in a map, `bridge/write_notifier_version_test.go:29-57`), not instrumented; that both callbacks were registered when it fired was not asserted. The row stays as "not reproduced".
- **F-db021da0-46: the hand-off advance of LevelControl and ModeSelect moves a version no controller reads.** A note, not a wire gap. The dispatcher serves a server's own version only on the root and the Aggregator (`ownDataVersion`, `endpoint/dispatcher.go:39-45`); on every other endpoint the served version is the endpoint's (`clusterDataVersionFor`, `:55-63`), and `clusterDataVersion` (`:24-29`) has no caller outside tests. LevelControl and ModeSelect are mounted on neither, so the rule `53c68d85` gave their own trackers — a command handed to the host's port advances when the value it leaves differs from the one the last host notification read (R2-P4-rr-im-01) — and the tests that pin it hold `MatterDataVersion()`, which the wire does not carry. On the wire the bridge's command commit advances the endpoint's version once for the same case (`BD-Matter-Invoke-HoldsHostReports`). Fix: none for the wire; the tests say which version they hold, or the trackers of servers that are never served by them go. Not placed. Found in review II, phase 4 (R2-P4-fr-im-03). Since review II, phase 5b (`c1028573`) `spec.Server`'s `ServerConfig.DataVersion` and `MatterDataVersion` state that the wire carries a server's own version on the root endpoint and the Aggregator only; LevelControl's and ModeSelect's own trackers and the tests that hold their `MatterDataVersion()` say nothing of it yet (`cluster/levelcontrol/levelcontrol_server.go`, `cluster/modeselect/modeselect_server.go`). Review II, phase 6a (owner decision 165): every host-owned tracker is `Deprecated:` — LevelControl's, ModeSelect's and modebase's `Config.DataVersion`, `HeldConfig.DataVersion`, `MatterDataVersion`, `spec.ServerConfig.DataVersion` and the server `Config.DataVersion` fields that pass one in; the row closes when they are removed after the deprecation window.
- **F-db021da0-50: the re-establishment of former subscriptions connects over UDP only.** matter.js prefers TCP for it when the peer's last known operational address was TCP (`packages/node/src/behavior/system/subscriptions/SubscriptionsServer.ts:298-309` at `b25ed960`). Here the ADR 0008 initiator resolves and handshakes over UDP (`connectPeer`, `runCASEInitiator`, `bridge/case_initiator.go`). Not deliberate: the order of the steps (ADR 0032, Shape 1). Fix: the re-establishment on the one session path; the single session layer ADR 0032 names as the direction closes it. Found in review II, phase 5b (wave B). Review II, phase 6b (decision 209): connectedhomeip re-establishes over MRP as well (`SubscriptionResumptionSessionEstablisher.cpp:83`, `CASESessionManager.h:87` at `6170af84`); stays open with ADR 0032's direction.
- **F-db021da0-51: a request on a session a node's client stack opened is served with the bridge's gates, not with the rest of the bridge's server.** The node serves its endpoint tree there with the bridge's dispatcher, ACL, privilege, existence, timed and Large Message gates and the same request facts (`clientServerConfig`, `node/client.go`; review II, wave B), but a write there runs in a fresh `im.WriteTransaction` of its own, not under the bridge's write holds, so it is not answered BUSY for an attribute a chunked write on a bridge session holds; a chunked write on such a session is applied chunk by chunk as separate writes (the server's exchange ends after the first chunk, `MoreChunkedMessages` is not read), so a chunked ACL replace leaves the list at its first chunk; none of the bridge's per-session bounds (chunk waits, chunked writes per session) applies, and a read's whole report is built before its first chunk is sent; a SubscribeRequest is answered INVALID_ACTION, a read reports no events, an invoke's before-and-after snapshot of the invoked clusters does not run (a change reaches the subscriptions through the servers' own notifications only), and a CommissioningComplete there does not close the PASE sessions as the bridge does after it (`closePASESessionsOnCommissioningComplete`). The session is named to the clusters by its identity (`core.WithInvokeSession`), never by a bridge id. Not deliberate: the order of the steps (ADR 0032, Shape 1). Fix: the single session layer, or earlier the bridge's write holds and bounds handed to the client stack's server. Found in review II, phase 5b (wave B). **Review II, phase 6b (owner decision 242), split:** the chunked-write part is fixed: on a session a node's client stack opened, a WriteRequest with MoreChunkedMessages is refused whole with FAILURE before anything is applied, so a chunked ACL replace leaves the list as it was; it fails closed until ADR 0032's single session layer, which matter.js does not need: a request goes out on the peer's newest session (`packages/protocol/src/peer/PeerExchangeProvider.ts:103-115`, `packages/protocol/src/peer/Peer.ts:440-448` at `db021da0`), and one server serves every session (ADR 0032's reading; no line read for it here, unverified). The rest — the bridge's write holds (no BUSY), its per-session bounds, no subscription, no events in a read, no invoke snapshot, no PASE close after CommissioningComplete — stays with ADR 0032's open point. The clause "a read's whole report is built before its first chunk is sent" is stale: the read pulls its reports through the chunker chunk by chunk. Pinned by `client/interaction:TestServeChunkedWriteRefusedWhole`.
- **F-db021da0-53: a pairing hint with any of bits 16 to 22 set is no hint at all to a commissioner built on connectedhomeip.** matter.js models 23 hint bits, the last `powerCycleUntilIndication`, bit 22 (`packages/protocol/src/advertisement/PairingHintBitmap.ts:145` at `b25ed960`). connectedhomeip reads PH with `MakeU16FromAsciiDecimal` (`src/lib/dnssd/TxtFields.cpp:195-198` at `6170af84`), which returns 0 for a value that does not fit 16 bits (`:104-108`), and stores that (`:274-275`): a PH above 65535 is read as 0, the whole hint. The DNS-SD reader is the only parser of the TXT key; the value has a second carrier, the UDC message's TLV field (`src/protocols/user_directed_commissioning/UserDirectedCommissioningServer.cpp:302`, holder `UserDirectedCommissioning.h:132`, 16 bits) (review II, phase 6b, R2-P6-doc-2907). The module advertises the bits the host sets, as matter.js, and its docs state the consequence (`mdns.PairingHints`, `node.Commissioning.PairingHints`). Who sees it: a chip-based commissioner shown a device whose host set a hint of bit 16 or above, which sees no hint. Fix: none in this module while the two references differ; a host that wants every commissioner to see its hint keeps to bits 0 to 15. Found in review II, phase 5b (the reference read of wave A2).
- **F-db021da0-54: matter.js's set of hints that need a PairingInstruction leaves out bits 20 to 22.** matter.js's bitmap comments say the PairingInstruction SHALL carry the value for bits 20, 21 and 22 (`packages/protocol/src/advertisement/PairingHintBitmap.ts:128-145` at `b25ed960`), but its instruction set names eleven bits without them (`PAIRING_HINTS_REQUIRING_INSTRUCTION`, `packages/protocol/src/mdns/MdnsConsts.ts:20-32`). The module mirrors the set (`mdns.PairingHints`): bit 20 with a PairingInstruction is refused, as in matter.js. A gap of matter.js's, mirrored; the row closes when matter.js's set changes, and the mirror follows. Found in review II, phase 5b (the reference read of wave A2).
- **R2-P6-par-1360: a field conformance that compares a field with the null literal never conforms.** `performComparison` is conformant only when both sides are non-null (`packages/node/src/behavior/state/validation/conformance-util.ts:183-209` at `b25ed960`), and this module's comparison is the same (`cluster/spec/field_conformance.go`). At the pin this hits WebRtcTransportProvider ProvideOffer's five fields (`MetadataEnabled` disallowed even with a null session id); the device only logs the misfit, because ProvideOffer states no Refusal. A gap of matter.js's, mirrored; the row closes when matter.js compares with null, and the mirror follows. Found in review II, phase 6a. Review II, phase 6b (decision 209): reach at b5ffb4c9: one definition (WebRTCTransportProvider), no refusing command — a log line only; connectedhomeip's `HandleProvideOffer` has no generic conformance evaluation and no check of MetadataEnabled (`src/app/clusters/webrtc-transport-provider-server/WebRTCTransportProviderCluster.cpp:844-1104` at `6170af84`), so chip accepts the offer, as this module does.
- **R2-P6-fsm-1412 (parked loop): a mode switch or a MoveToHueAndSaturation over a colour loop stops its transition but leaves ColorLoopActive 1.** As matter.js (`stopAllColorMovement`, `packages/node/src/behaviors/color-control/ColorControlServer.ts:843-848` at `b25ed960`, no `colorLoopActive`) and connectedhomeip, which leaves it 1 by design: a command's hue transition parks the loop, "ColorLoopActive stays 1 … only ColorLoopSet re-engages it" (`src/app/clusters/color-control-server/ColorControlCluster.cpp:698-701` at `6170af84`), "that attribute stays 1 while the loop is dormant" (`:972-973`), a MoveToColor or MoveToColorTemperature "can leave the loop active-but-dormant" (`:1717-1718`). Recorded as both references' behaviour. The Time-0 half follows connectedhomeip since review II, phase 6b (`BD-chip-ColorControl-LoopTimeZero`). Found in review II, phase 6a.
- **R2-P6-err-1404 (follow-on): a scene recall of an active colour loop no longer invalidates the scene in passing.** `MatterApplySceneValues` calls `colorLoopSet` directly (`cluster/light/colorcontrol_scenes.go`), whose invalidation mark only `MatterInvoke` reads; matter.js invalidates there (`packages/node/src/behaviors/color-control/ColorControlServer.ts:1869` → `:1145` at `b25ed960`). The net state is equal — core activates the scene after applying (`cluster/core/scenes_management.go`) — only the intermediate invalid state is gone. Inferred from the read, not run. Found in review II, phase 6a.
- **R2-P6-par-1466 (follow-on): ElectricalPowerMeasurement's Accuracy entries are a stub.** One entry per served measurement type (Voltage, ActiveCurrent, ActivePower, Frequency; three without Frequency for DC), as the model's text asks (`packages/model/src/standard/resources/electrical-power-measurement.resource.ts:57-59` at `b25ed960`), but each states the attribute's constraint as its Min/MaxMeasuredValue and FixedMax 1 (`cluster/measurement/measurement.go`): no source carries a meter's accuracy — the contract has none, matter.js's server is empty and connectedhomeip's values were not taken. Fix: a host-supplied accuracy, a contract capability. Found in review II, phase 6a.
- **R2-P6-sec-1513: the door lock's credential data are sealed without associated data, under a key kept in the same store.** As matter.js (`packages/general/src/crypto/Cipher.ts:34-49`, `packages/node/src/behaviors/door-lock/DoorLockServer.ts:153-155`, `:1569-1573` at `db021da0`); the module's store-wide sealing is `BD-Matter-SealedAtRest`. Reaching it needs store access; no network party gains. A gap of matter.js's, mirrored. Found in review II, phase 6a. Review II, phase 6b (decision 209): connectedhomeip at `6170af84` does not seal the data at all (no `encrypt` or `AES` under `src/app/clusters/door-lock-server`, grep).
- **R2-P6-err-1560: a host preset setter between the candidate check and the apply can still make the apply refuse after the presets were stored.** `checkCandidate` and `applySet` do not run under one hold of `mu` (`cluster/thermo/thermostat_typed.go`). Closing it needs the preset setters under the same hold, which the `op` / `persist` order forbids for the inbound path. This module's own; no reference sentence. Found in review II, phase 6a.
- **R2-P6-store-1501: the wait on the preset store takes no context.** A `SavePresets` that never returns blocks every inbound setpoint setter, which did not wait on the store before; stated on the exported `thermo.PresetStore` doc. This module's own. Found in review II, phase 6a.
- **R2-P6-conc-1606 (follow-on): a command in flight across `MatterQuiesce` arms its timer after the quiesce.** PushTransport (`commitAllocation` → `scheduleLocked`) and DeviceEnergyManagement (`armPowerAdjustLocked` after the manager hook released `mu`) arm a timer the quiesce no longer stops, as EvseServer documents for its own (the next EnableCharging arms the timer again). If the bridge does not drain commands before quiescing, that timer reaches the port after the stop. This module's own; no reference sentence. Found in review II, phase 6a.
- **F56-n: a host change that arrives outside an attribute data of a write — between two, or after the last — gets no advance of its own when the write changed its cluster.** The bridge's hold table stamps such a change with no attribute data and settles it against the whole record (`bridge/write_hold.go` `open`, `settleWrite`; measured by run at `7eea3b49`'s code, Fable's read of the F-db021da0-56 rework: no advance after an attribute data that changed the cluster, none before a later one that does); matter.js would give a change after a commit its own commit and advance (inferred from `packages/node/src/behavior/state/managed/Datasource.ts:1456-1457` at `db021da0`). Found in review II, phase 6a.
- **R2-P7-V3-giveup: after this node gives up on a message, the receive rules no longer hold on its exchange.** matter.js keeps `#sentMessageToAck` when the retransmissions end (`packages/protocol/src/protocol/MessageExchange.ts:973-979` at `db021da0` clear the callbacks and the owed ack, `#receivedMessageToAck`, `:975`; it stays set after an aborted ack wait too, `:831-837`), so the exchange goes on refusing a message without the acknowledgement or with another counter (`:521-534`) until it closes. The tracker drops the entry at the give-up (`outboundReliableTracker.Tick`, `bridge/outbound_reliable.go`), and the exchange's next message is taken. For a BDX exchange the give-up ends the exchange (`bridge/ackpump.go`, `handleInitiatedPeerLoss` / `failRespondedExchange`), and a send waiting on the given-up message, or beginning after it, ends unsent (`waitExchangeFree`, `initiatedSecureExchange.Send`, decision 283), so no difference shows there; for an interaction exchange or a handshake a difference can show between the give-up and the close of matter.js's exchange (how long that is per owner was not read; unverified). Found in review II, phase 7 (Fable's read for decision 276, Unresolved 3).
- **R2-P6-par-0444: a fabric-scoped event that is not fabric-sensitive is not filtered by a fabric-filtered read.** matter.js filters a fabric-sensitive event to the accessing fabric whatever the read's FabricFiltered flag (`packages/protocol/src/action/server/EventReadResponse.ts:344-352` at `b25ed960`), and a fabric-scoped event that is not fabric-sensitive only when the read is fabric-filtered (`:353-364`); the module has the first rule only (`im/read.go`). No effect at this pin — no such event in the snapshot; held by `im:TestEveryFabricScopedEventIsFabricSensitive`, which fails when one appears. Found in review II, phase 6a.
- **R2-P6-sec-1103 (follow-on, decision 152): a requester a `contract.ReadAccessDeclarer` refuses is answered UNSUPPORTED_ACCESS for an attribute the server does not list, before its existence is known.** matter.js answers UNSUPPORTED_ATTRIBUTE before the attribute's read-level check (`packages/protocol/src/action/server/AttributeReadResponse.ts:263-266` before `:272` at `db021da0`). Fail-closed; the capability is the module's own (`BD-Matter-ReadAccessCapability`). Found in review II, phase 6a.
- **R2-P6-acl-0402 (follow-on): the accessing-fabric stamp of a written fabric-scoped entry finds the field by its Go name `FabricIndex`.** A host struct that names field 254 otherwise passes unstamped (`endpoint/dispatcher.go` `WriteAuthorized`), while `contract/matter.go`'s `MatterWrite` doc states the condition since review II, phase 6a. matter.js injects the accessing fabric into every decoded write (`packages/protocol/src/action/server/AttributeWriteResponse.ts:466-473`, "We always inject the current fabricIndex for writes", `:471` at `db021da0`). No in-tree server is exposed. Found in review II, phase 6a.
- **R2-P6-wire-0442 (follow-on): a write to a cluster without a definition hands the server the bridge's undecoded value.** `writtenHeld` answers Success without a definition (`endpoint/dispatcher.go`), so a host server without one receives the bridge-private `undecodedValue`; the module does not refuse it. Found in review II, phase 6a.
- **R2-P6-bound-0405 (follow-on): a wildcard read with WildcardPathFlags reads ConfigurationVersion under the subject's read check.** Since decision 162 a subject without View on BasicInformation at endpoint 0 keeps its wildcard flags where the version would have dropped them (`im/wildcard_path_flags.go`) — fewer results, never more. matter.js reads it with `readState(this.session)` (`packages/protocol/src/action/server/AttributeReadResponse.ts:137-144` at `db021da0`); whether that checks access was not read. Found in review II, phase 6a.
- **Review II, phase 6a (stake, decision 157): the sealed store's v11 baseline is a hand list.** `store` `TestSecretsAddedAfterV11ShipAnUpgradeStep` holds `sealedAtBaseline` equal to the sealed lists at `fcfbcb3c`; that it is exactly v11's rests on v11 being unreleased with all of them (the canary `f4de428e`, the RI key `425207e1`, the same day), not on a tag. The guard proves an upgrade step by the name of a package test, not by what it does; adding a secret together with its baseline line passes — only review stops that.
- **R2-P6-fsm-2109: the controller takes a granted MaxInterval unchecked — not performable.** Missing input: the peer's IdleModeDuration, which the SubscribeResponse does not carry. Both references accept the granted value: matter.js decodes the response straight into its subscription (`packages/protocol/src/action/client/ClientInteraction.ts:1099`, `:1101-1109` at `db021da0`; the timeout `subscription/PeerSubscription.ts:38-39`), and connectedhomeip's client takes `GetMaxInterval(&mMaxInterval)` unchecked (`src/app/ReadClient.cpp:1122` at `6170af84`) and uses it for the liveness timeout (`:1032`). connectedhomeip's publisher may grant more than the ceiling by design: `src/app/ReadHandler.cpp:780` `maxIntervalCeiling = std::max(GetPublisherSelectedIntervalLimit(), mMaxInterval)`, the limit being 3600 s (`ReadHandler.h:58`) or, on an ICD, `max(IdleModeDuration, 3600)` (`ReadHandler.cpp:42-50`). A client clamp or refusal at `max(ceiling, 3600 s)` would refuse those devices (inferred). Owner decision 211: register only. Found in review II, phase 6b.
- **R2-P6-par-2156: the status budget of a write chunk with a wildcard-endpoint path — not performable.** Missing input: the device's endpoint count, which is the number of statuses such a write is answered with. matter.js budgets one status per AttributeDataIB by the request path (`maxStatusSizeOf(path)`, `packages/protocol/src/interaction/InteractionMessenger.ts:103-105`, `:1344-1346`, `:1376-1381` at `db021da0`); connectedhomeip reserves for the MIC and the request's TLV overhead only (`src/app/WriteClient.cpp:183-204` at `6170af84`). The budget of `client/interaction`'s write chunking holds for concrete paths only. Owner decision 212: register only. Found in review II, phase 6b.
- **R2-P6-fsm-1915: one OTA update at a time, and any progress of its transfer keeps the slot.** Any change of the BDX transfer counter counts as progress (`isTransferring` and the queue loop, `controller/softwareupdate/manager.go`); a commissioned node that negotiates a block size of 1 and queries a block every few minutes keeps the single slot. Not changed: an overall deadline or a minimum rate would be a decision. This module's own; whether matter.js behaves the same was not read (unverified). Found in review II, phase 6b.
- **N-fabric-2 (stored-subscriptions half): whether a removed fabric's stored server subscriptions are swept is not performable here.** The reconcile (`node/fabric_state.go`) does not sweep `matter_server_subscriptions`; whether a row of a gone fabric is loaded or re-established is decided by the bridge's load and re-establish path (`bridge/subscription_persistence.go`), the input this check did not have. Found in review II, phase 2; recorded in phase 6b.
- **Descriptor TagList: a host that enables it can serve a manufacturer tag without a label.** `EnableTagList` and `AddTags` (`cluster/core/descriptor.go`, off by default) take a tag with an `MfgCode` and no `Label` and do not refuse it. CHIP's data model makes Label mandatory when MfgCode is not null (SemanticTagStruct field 3), and matter.js relaxes it to O ("TODO we do not support MfgCode != null", `support/models/src/local/semtag.ts:9-22` at `b25ed960`) — the class (ii) entry `semtag.Label` below. Whether the served struct is held to the generated definition's conformance anywhere else was not read. Fix: the module refuses such a tag. Found in review II, phase 6b (R2-P6-doc-2817, D8(c)).
- **R2-P6-doc-3084 (OTA requestor image): the OTA requestor keeps its downloaded image in a file, outside the persistence seam.** `cluster/core/ota_requestor_query.go` stats, opens and removes the image file directly (`os.Stat`, `os.Open`, `os.Remove`). ADR 0026 principle 7 named it one of two residues that phase 21 would move behind a seam; phase 21 landed (`store.Port`, `store/memstore`) and this one stayed. A host on a non-file store still needs a file system for the image. Fix: phase 7 (owner decision 249). Found in review II, phase 6b.
- **R2-P6-doc-3084 (DCL snapshot fetch): the DCL snapshot is written to files, outside the persistence seam.** `controller/dcl/snapshot/fetch.go` writes with `os.MkdirAll` and `os.WriteFile`, the second residue ADR 0026 principle 7 names. Fix: phase 7 (owner decision 249). Found in review II, phase 6b.
- **Commissioning window: the window reads as open before its PASE acceptor is attached.** Low. The window is open — `IsOpen()` true, `paseWindowClosed()` false — before the transition hook has run, because the hooks fire after `w.mu` is released (`transitionHooks`, `bridge/commissioning_window.go`), and `renewSalt` stands before `attachPase(h.baseline)` (`node/commissioning.go`). A PBKDFParamRequest that arrives in between reaches the PASE branch, and `resolvePaseHandler` falls to the singleton with `provider == nil` — the fail-closed noop, which answers nothing; `claimPase` runs before the handler is resolved, so the noop exchange holds the single-PASE claim (read, not instrumented). Measured on a scratch copy with the verifier derivation moved into the hook: the commissioner gave up. The gap (open → acceptor attached is not one step) existed before decision 251; it is now a `rand.Read` wide. In production a commissioner learns of the window from the record or the BLE advertisement, both sent after `attachPase` in the same hook, so only a blind PBKDFParamRequest can land in it — low. Fix: `attachPase` before anything else in each open branch (or the acceptor attached before `open` is visible). Found in review II, phase 6b (Fable read of wave 3, round 2).
- **R2-P6-sec-2412 (follow-on, client stack): a handshake of an updated fabric that completes after the sweep's context ended is adopted and not closed.** On the client stack the sweep waits for a connection process only as long as its own context lives (`client/peer/connection.go:392-405`); a handshake of the fabric that completes after that is adopted (`:1463-1466`, `:1496-1505`) and not closed (`node/client.go:401-421`). Read, not run. The context is the UpdateNOC command's, or the owed-event delivery's at start; the window is the time the process needs to unwind after its cancel (inferred). Fix: wait on the process's end alone, or walk the sessions again after the wait. Found in review II, phase 6b.
- **R2-P6-sec-2401 (limit half): the node's own ACL entry can take a fabric's list past AccessControlEntriesPerFabric.** As matter.js: `packages/node/src/behaviors/web-rtc-transport-requestor/WebRtcTransportRequestorServer.ts:81-91` builds `[...acl, entry]` and sets it; the validation wired to `acl$Changing` (`packages/node/src/behaviors/access-control/AccessControlServer.ts:72`) returns at `:154-156` (`if (!hasRemoteActor(context)) return;`) before the limit check `:178-181` (at `db021da0`), so a local change is not held to the limit (inferred: that the node's own `setStateOf` carries no remote actor was not read), and a later controller write of a list longer than the limit is refused at `:179-180`. connectedhomeip refuses: `AccessControl::CreateEntry` checks `(count + 1) <= maxCount` (`src/access/AccessControl.cpp:252-257` at `6170af84`). The node's own Operate entry (WebRTC requestor, OTA provider) is appended the same way (`node/local_acl.go`); an administrator whose list is then full must drop an entry before any whole-list write succeeds (ResourceExhausted). Kept as matter.js (owner decision 228). Candidate for a later unit (*Ahead*): serve AccessControlEntriesPerFabric with headroom for the node's own entries, so a controller's four are never used up — a separate decision. Found in review II, phase 6b.

## matter.js pin b25ed960 — open items (pin lift 2026-11-19)

The gaps of the lift db021da0 → b25ed960 (`pin-lifts.md`), each with the
wave unit that closes it (decision 287: every one before the release). The
rows the lift caught up in the *Ahead of matter.js* register are reconciled
by their units too and tracked there, not here. matter.js lines are at
b25ed960; connectedhomeip was read only where an entry names it at 6170af84 (otherwise unverified). Closed entries move to `matter_behaviour_findings_closed.md`, same section.

- **F-b25ed960-1: a subscription stays on the session it was made on.** matter.js moves the subscriptions of older established CASE sessions with the same peer and equal CATs onto a newly established one, requeues a failed update on the session it left without counting it, and evicts per peer at establishment over established sessions only (`SessionManager.ts:757-808`, `ServerSubscription.ts:569-577`, `:819-834` at `b25ed960`; #4882). Here a subscription is bound to its session for life (`im/subscription/manager.go:652-698`), every failed update counts (`bridge/subscribe.go:1194-1213`) and the eviction runs when a session is installed (`secure/operational/manager.go:1636-1687`). Medium. Unit P2-B (decision 298).
- **F-b25ed960-3: string lengths count UTF-16 code units.** matter.js counts UTF-8 bytes in constraint validation, TLV string bounds, fabric label, group and scene name (`packages/node/src/behavior/state/validation/constraint.ts:203-213`, `TlvString.ts:69-100` at `b25ed960`; #4995), and shortens stored strings at load. This module counts UTF-16 units after matter.js at db021da0 (`cluster/spec/write.go:367-375`, `cluster/core/groups.go:272-281`, `cluster/core/scenes_management.go:1066-1076`, `tlv/utf8.go:72-82`); fabric label and interface name count bytes already. Which persisted or host-supplied strings exceed their byte bound: not enumerated. Medium. Unit P2-A (decision 294).
- **F-b25ed960-4: a fail-safe that ends without commissioning is not a failed commissioning attempt.** matter.js closes the window after 20 failed attempts in it, a fail-safe armed during the window that ends without commissioning counting as one, and RevokeCommissioning reads the window state at receipt (`DeviceCommissioner.ts:44`, `:160-199` at `b25ed960`; #5016). Here only PASE failures count (`recordPaseFailure`, `bridge/securechannel.go:1176-1231`). The timed PASE lockout stays ahead of matter.js. Medium. Unit P3-C (decision 299).
- **F-b25ed960-7: a BLE channel lost during PASE does not end the handshake at once.** matter.js force-closes the unsecured session when the channel closes while PASE runs and fails an attempt whose channel closed before PASE returned (`packages/protocol/src/peer/ControllerCommissioner.ts:482-551` at `b25ed960`; #4908, #4922). Here the watch on `ch.Closed()` starts after PASE returned (`controller/commissioner/commissioner.go:459-478`), so a pending read waits for the PASE timeout. Low. Unit P3-D.
- **F-b25ed960-8: an unacknowledged status to a report does not replace the subscription.** matter.js's controller closes a subscription whose report status the peer did not acknowledge — on a chunk always, on the final report when the replacement primes compactly (`PeerSubscription.ts:60-120` at `b25ed960`; #4943). Here a failed final status is only logged (`client/interaction/report.go:762-774`). Low. Unit P2-E.
- **F-b25ed960-9: the subscription timeout timer outlives the last subscription.** matter.js stops it when the last peer subscription is deleted (`ClientSubscriptions.ts:172-187` at `b25ed960`; #4947); `client/interaction/subscriptions.go:200-211` leaves one pending one-shot timer. Low. Unit P2-E.
- **F-b25ed960-10: no model-driven redaction of secret fields in logs.** matter.js logs the values of fields its model marks `isSecret` as "(redacted)" (`SecretRedaction.ts:13-35` at `b25ed960`; #4931). This module logs no command fields or written values and redacts its own API secret types (`controller/client/redact.go:14-60`), but two debug lines print read values (`bridge/receive_dispatch.go:405-417`, `bridge/subscribe_dispatch.go:271-287`); whether a served read value carries a secret field: unverified. Low. Unit P4-B (decision 304).
- **F-b25ed960-14: the controller places a peer's endpoints by stored claims of the root's PartsList.** matter.js composes the root full-family whatever its DeviceTypeList and places every endpoint named by the root's PartsList or by that of an endpoint on the node, deriving the owner on every apply (`packages/node/src/node/client/ClientStructure.ts:1408-1581`, `:1715-1740` at `b25ed960`; #4921, #4923). Here a root with an empty DeviceTypeList gets no composition and only the root's list counts (`controller/client/structure.go:589-600`, `:641-683`, `:1000-1006`), so a bridge naming its devices only in the aggregator's PartsList shows none. Medium. Unit P2-C (decision 297).
- **F-b25ed960-17: the commissioner's fail-safe expiry after a failed step is not matter.js's.** matter.js skips the disarm on a commissioning-channel drop, awaits a re-arm in flight, checks the ArmFailSafeResponse and sends only on an existing session (`packages/protocol/src/peer/ControllerCommissioningFlow.ts:995-1003`, `:1097-1130` at `b25ed960`; #4928). Here `controller/commissioner/flow.go:864-887`, `:976-1000` sends unless the session is closed, does not wait, does not check the response, and the request types have no existing-session option. Low. Unit P3-D (decision 301).
- **F-b25ed960-18: a CD vendor or product mismatch fails attestation hard.** matter.js reports it as an error-level finding the policy decides, also for unreported values (DAV:580-596 at `b25ed960`; #4939); `controller/attestation/validate.go:357-364` fails and compares an unreported VendorID or ProductID as 0 (`:52`). Low. Unit P2-D (decision 295).
- **F-b25ed960-19: a hard attestation failure drops the findings before it.** matter.js hands the policy the preceding findings and the failure last (DAV:283-293 at `b25ed960`; #4940); `attestation.Error` carries none (`controller/attestation/check.go:85-95`). Low. Unit P2-D.
- **F-b25ed960-20: the controller cannot rotate a node's operational certificate.** matter.js's controller replaces a commissioned node's NOC — CSRRequest for UpdateNOC, UpdateNOC, CommissioningComplete on a new CASE session (`packages/protocol/src/peer/OperationalCertificateUpdate.ts:158-243` at `b25ed960`; #4874); this module has no controller-side UpdateNOC (grep). A missing feature, not a defect; TC-OPCREDS-3.3 step 11 needs it. Medium. Unit P5-B (decision 293).
- **F-b25ed960-21: the OTA provider and manager follow matter.js's db021da0 flow.** matter.js keeps update attempts in the manager, with a query admission table, an apply decision table, one consent per peer with 24 h failed-update retention, opt-in delegation, a candidate order, linked phases and the end of undeliverable updates (`packages/node/src/behavior/system/software-update/OtaUpdateAttempts.ts:23-181` at `b25ed960`; #4826); `cluster/core/ota_provider.go` and `controller/softwareupdate/manager.go` differ in 14 behaviours. Medium. Units P4-C, then P5-A (decision 292).
- **F-b25ed960-23: the device-side CASE responder has no establishing state.** matter.js creates the responder session establishing, completes it at the first transmission of its Success (a resumed one at the initiator's Success, `CaseServer.ts:204-206`), keeps it out of the established set (`sessions`) and the per-peer eviction until then, while `sessionsFor` lists it (`SessionManager.ts:702-705`), and refuses to complete one closed meanwhile (`packages/protocol/src/session/case/CaseServer.ts:314-357`, `:367-376`, `SessionManager.ts:757-808` at `b25ed960`; #4882); decision 311 gave the controller's responder that state in P1-A. The device side's responder (`bridge/handlers.go:919-980` → `node/security.go:1224` → `secure/operational`) installs the session before its Success and evicts at the install; whether `NewestSessionForPeer` can return it before the Success was not read (unverified). SessionViewsTest.ts's ten device-side rows wait on it. Medium: a picker may route a request onto a session the initiator has not confirmed. Own P2 unit with a Fable pre-read of `secure/operational` (decision 312).
- **F-b25ed960-24: `client.Node.PeerAddress()` of a node that failed to load reports none.** matter.js's `ClientNode.peerAddress` answers from storage for a node whose commissioning state did not load (`packages/node/src/node/ClientNode.ts:388-421`, the storage branch `:408-418` at `b25ed960`). Here only `Registry.GetByAddress` and `ForAddress` use the stored address (`controller/client/registry.go` storedPeerAddress, P1-D), and `PeerAddress()` is also the commissioned predicate (`IsCommissioned`, `Commissioned`), which must keep excluding such a node as matter.js's `lifecycle.isCommissioned` does — shown for the orphaned-fabric case only (`PeersLoadFailureTest.ts:123-125`, `NodeLifecycle.ts:23`; other load failures unverified). A consequence of P1-D (inferred from the lines, characterized by fix-up X1's `controller/client:TestCompleteCommissioningOnNodeThatFailedToLoad`): `CompleteCommissioning` on such a node's address no longer creates a second node; it fails with the load error and runs `deleteNode`, which erases the stored node (`controller/client/complete.go`, `registry.go`). matter.js has the same shape (`Peers.ts:269-272`, `:278`, `:348-359`); whether its `node.start` and `node.delete()` behave so on a node whose construction failed is unverified. The other `GetByAddress` callers now receive an unavailable node where they got nil — `controller/camera/client_io.go`, `controller/diagnostics.go`, `controller/ota_provider.go`, `controller/softwareupdate/manager.go`, `examples/reference-controller/cli/stack.go`; go-fabric-ui `fabricui/gofabric/gofabric.go:252` (792d15b), go-fabric-server `controllerapi/cmd_node.go:35` (d61a545); none in openccu-loom (bef849d2) or go-ha2fabric (ec7deea) — not walked one by one. Low: the public node keeps one answer for its address and its commissioned state until the callers are audited. Own P2 unit: audit the PeerAddress and IsCommissioned callers, then mirror `ClientNode.peerAddress` (decision 314).
- **F-b25ed960-25: TimeFormatLocalization has no unset calendar or hour format and always serves CalendarFormat.** matter.js's server is `.with("CalendarFormat")` (`packages/node/src/behaviors/time-format-localization/TimeFormatLocalizationServer.ts:16` at `b25ed960`) and returns after the hour format without the feature (`:21-23`; `TimeFormatLocalizationServerTest.ts:74-78`); an unset hour format takes the detected one (`:18-20`), an unset or unsupported calendar the detected one if listed, else the list's first (`:34-41`; cases `:117-123`, `:160-168`). Here `NewTimeFormatLocalization` fixes `FeatureCalendarFormat` (`cluster/core/time_format_localization.go:89`), and the enum zero values Buddhist and 12Hr (`cluster/spec/timeformatlocalization/definition_gen.go:49`, `:90`) stand for configured, so decision 289's "nothing configured starts as matter.js" cannot be stated for the calendar. Low: a host that leaves the calendar unset gets Buddhist. Own P2 unit with matter.js's tests: `TimeFormatLocalizationConfig` gains an unset calendar (a pointer) and a CalendarFormat opt-out (decision 315); three register cases wait on it.
- **F-b25ed960-26: ThermostatSuggestions and the manual override are not stored.** matter.js stores both — the attribute's quality N (`packages/model/src/standard/elements/thermostat-cluster.element.ts:271-272` at `b25ed960`) and `overriddenSuggestionId` (`ThermostatServer.ts:163`, "Stored, so the override stays in effect after a restart", `:3178-3183`) — and handles them at start: entries naming no preset dropped, MaxThermostatSuggestions raised to cover the stored entries, the id counter seeded from the stored ids (`:2273-2287`). The Go server keeps both in memory; its only store seam is `PresetStore` (`cluster/spec/thermostat/server_gen.go`, `LoadPresets`/`SavePresets`), so a restart forgets the controller's suggestions and the override. Medium. Unit P2-thermo-suggestions (decision 316: a store seam through a new `ThermostatConfig` field, surviving a restart); ten register cases wait on it.
- **F-b25ed960-27: the application cannot write ThermostatSuggestions.** matter.js's application sets the attribute and the server re-evaluates (ThermostatSuggestionsTest.ts "re-evaluates when the application writes ThermostatSuggestions", "follows when the transaction that adds the entry moves the setpoint to keep a limit" at `b25ed960`; the mechanism behind the cases was not read, unverified). Here `SetAttributes` refuses the attribute (`spec.ErrNotHeld`, P1-E's report). Low. Unit P2-thermo-suggestions (decision 317: `SetAttributes` accepts the list with matter.js's checks).
- **F-b25ed960-28: the application cannot activate a preset with its setpoints.** matter.js's application calls `setActivePresetRequest`, the command's path, which moves the setpoints and keeps the preset (ThermostatSuggestionsTest.ts "keeps a preset the application activates" at `b25ed960`; the path not re-read, unverified). A Go host has `SetActivePresetHandle`, which sets the handle only (P1-E's report). Low. Unit P2-thermo-suggestions (decision 318: a host method that activates a preset on SetActivePresetRequest's path).
- **F-b25ed960-29: the request gate does not reach every server.** matter.js binds every command of a cluster to `invokeCommand` (`packages/node/src/node/integration/ProtocolService.ts:347-350` at `b25ed960`) and runs the gate there for every command with a model (`:671-705`, `:359-376`), reached from the one wire path (`CommandInvokeResponse.ts:311-318`, `:350-360`). Here `admitRequest` runs only from spec.Server, InvokeHeld and Hooks (`cluster/spec/request_check.go`): the servers whose host is a `spec.HostHandlers` (Thermostat, WindowCovering, FanControl, LevelControl, ColorControl) and the hand-written `cluster/core` servers do not refuse an undefined enum value with CONSTRAINT_ERROR; ThermostatBehaviorTest.ts `:527-529` stays skipped (`cluster/thermo/thermostat_request_check_parity_matterjs_test.go`). Medium: the same request gets two answers depending on the server. Own P2 unit: one central gate where the bridge decodes by definition, the per-server gate dropped (decision 319, with a Fable pre-read of the command validation).
- **F-b25ed960-30: a handler's absent response is not judged.** matter.js validates a response model against `{}` when the handler returns nothing (`ProtocolService.ts:788`, `:718`, `:720-726`, `:624-625` at `b25ed960`) and answers FAILURE when a mandatory field is missing (`:727-733`); with every field optional the model passes and `CommandInvokeResponse.ts:492` encodes undefined (its answer unverified). Here `checkResponse` returns early on a nil response (`cluster/spec/field_conformance.go`). Low. With the P2 request-gate unit (decision 319), after a grep of the four consumers for handlers that return nil (coordinator decision C-13; Fable recommends adopting).
- **F-b25ed960-31: a request without fields, for a command without a Refusal, runs the handler.** matter.js turns absent CommandFields into undefined (`CommandInvokeResponse.ts:550-552` at `b25ed960`, "The validation will fail if the schema expected data") and validates it against the request TLV (`:485-487`), which throws a TypeError for a command with fields (`TlvObject.ts:253-256`; measured by Fable on the checkout's dist), answered FAILURE (`:512`, `:518-520`); no matter.js test covers it. connectedhomeip decodes an empty struct instead (`src/app/CommandHandlerImpl.cpp:620-628`, `:497-500` at `6170af84`; the status of the failed decode unverified). Here the gate judges a nil request as `{}`, warns and runs the handler with nil fields (`cluster/spec/request_check.go` `requestOf`, `admitRequest`). Low. With the P2 request-gate unit (C-13): absent fields as `{}` at the decode point, chip's rule and matter.js's stated intent, a missing mandatory field then the decoder's INVALID_COMMAND — an owner decision with a `by_design` row.
- **F-b25ed960-32: `Relaxed` stands for two matter.js switches.** matter.js turns a field's validation off as a whole (`validate = false`, `packages/node/src/behavior/state/validation/conformance.ts:36-40` at `b25ed960`) or only its constraint (`constraint = false`, `constraint.ts:91-95`, the enum and the conformance staying); `spec.Field.Relaxed` drops the enum-member check either way (`cluster/spec/field_conformance.go` `fieldValidator`). Every `constraint = false` field at the pin is an integer (`DoorLockServer.ts:1520-1526`), so nothing differs today. Latent, low: a `constraint = false` enum field would need its own relaxation kind; one sentence in `script/clustergen/relaxed.go`'s header when it is next touched.
- **F-b25ed960-33: the decoder refuses an enum value above its wire width where matter.js's handler answers.** matter.js's TLV layer holds an enum to uint32 only (`packages/types/src/tlv/TlvNumber.ts:303`, `TlvOfModel.ts:221-224` at `b25ed960`), so with `validate = false` SetRegulatoryConfig's NewRegulatoryConfig 0x100 reaches the handler, which answers ValueOutsideRange in the response (`GeneralCommissioningServer.ts:288-289`, `:199-206`), and Identify's TriggerEffect runs (SUCCESS, `IdentifyServer.ts:134-139`). Here the definition's decoder holds the enum's wire width (`bridge/fields_reader_range_test.go` expects a refusal for 0x100; the status not read, unverified). connectedhomeip's answer unverified. Low, a pre-existing kind newly reachable after P1-B: a finding or a `by_design` row once chip is read.
- **F-b25ed960-34: a session closed after the Success's first transmission is still handed over and its record saved.** matter.js the same: an established session that is closing completes (`NodeSession.ts:242-244` at `b25ed960`) and the responder saves the record (`CaseServer.ts:337-357`). Here `completeEstablishment` (`client/exchange/session.go`) and `handshake.CASEServer` follow it. Consequence on the controller's UpdateNOC sweep (`node/client.go`, the sessions closed, then the peer's record deleted): a responder handshake in its ack wait can save a record after the deletion; whether `node`'s client stack serves `handshake.CASEServer` with that resumption store was not read (unverified). Low, a gap of matter.js's, mirrored; the row closes when matter.js refuses it or the sweep is shown unaffected.
- **F-b25ed960-35: the controller's responder hands its session to the peer set later than matter.js.** matter.js adds the session to `sessions` on its `established` event, at the Success's first transmission (`SessionManager.ts:1274-1280`, `CaseServer.ts:331` at `b25ed960`), and PeerSet adopts it from there (`PeerSet.ts:104-110`, from the pre-read, not re-read at the pin). Here `OnSession` runs after `SendNotifyingTransmission` returned — after the ack or the MRP give-up (`client/handshake/server.go`) —, so in between the session counts for the per-peer cap but `Peer.NewestSession` does not see it. Low, the safe direction; record as a `by_design` sentence or mirror with the device side's unit (decision 312).

## Tier 3 — LOW


### T3-P35-1 — a write value of the wrong Go type gets three different statuses (phase 35, 2026-11-04)

- **Where:** FAILURE from `cluster/core/access_control.go:674`, `bridged_device_basic_information.go:459`, `general_commissioning.go:403`, `network_commissioning.go:234`; CONSTRAINT_ERROR from BasicInformation and Binding through the convention `cluster/spec/write.go:126-128`; INVALID_ACTION from `cluster/core/identify.go:453-457`.
- **matter.js at 862ad75f:** the node's write path validates through `ValueValidator`, whose `DatatypeError` extends `ValidateError` with the default `Status.InvalidDataType` (`packages/protocol/src/action/errors.ts:55-72`); the older TLV-schema path answers `ValidationDatatypeMismatchError` → `Status.InvalidAction` (`packages/types/src/common/ValidationError.ts:31-38`). Neither is FAILURE.
- **Unverified:** whether a wire write can reach a Go server with a value of a type the attribute does not hold — the bridge decodes by the attribute's schema, so these may be unit-level paths only. Phase 35's status assertions pin today's answers (`TestBasicInfo_WriteNodeLabelWrongType` and siblings) so a change is visible; the rule — one status, matter.js's INVALID_DATA_TYPE on the node path — is a decision for the next write-path wave.
- **Measured 2026-11-05 (follow-up wave, attempt reverted):** the "wrong Go type" convention of `cluster/spec/write.go:122-128` also carries a wire case — `bridge.undecodedValue` (`bridge/attribute_value_reader.go:108-114`), the value the attribute's decoder refused (a label over its max, a string cut at an IS1), which the server's write check answers CONSTRAINT_ERROR because it is of no attribute type; matter.js answers a value its TlvSchema rejects with CONSTRAINT_ERROR too (`AttributeWriteResponse.ts` #decodeWithSchema, cited there), and the ported cases `bridge:TestNodeLabelWriteBoundsTheLengthBeforeIS1` (TlvString.ts:57-69), `bridge/attribute_value_reader_userlabel_test.go:87` and `cluster/spec:TestValueValidatorParityMatterJS/still_rejects_a_wrong-datatype_value_on_a_client_peer_write` (ValueValidatorTest.ts:478-480) assert CONSTRAINT_ERROR. Switching the convention to INVALID_DATA_TYPE therefore breaks three matter.js-asserted cases and 20 module tests; the write-path wave has to separate the undecoded wire value (CONSTRAINT_ERROR, as matter.js) from a host-side Go value of the wrong type before the statuses can be unified, and read what matter.js's node path answers a TLV element of another type on the wire (the decoder's error, not the validator's) at the pin.
- **Review II, phase 4 (2026-11-14):** since owner decision 64 a controller's written value is held to the attribute's type, nullability and constraint by the cluster's definition in the dispatcher, before any server runs, and refused there with CONSTRAINT_ERROR (`spec.CheckWritten`, `cluster/spec/written.go:100-114`; `writtenHeld`, `endpoint/dispatcher.go:521`); a value the definition did not decode is not judged there. For a cluster with a definition the servers' own statuses above are therefore a host-side matter; that no wire write reaches them any more was not measured (unverified). Of the lines under "Where", `bridged_device_basic_information.go:459` and `cluster/spec/write.go:126-128` hold; the Breadcrumb refusal is `general_commissioning.go:529` and InterfaceEnabled's `network_commissioning.go:252` now; `access_control.go:674` and `identify.go:453-457` are other lines today and were not located again.
- **Review II, phase 6b (owner decision 234, R2-P6-err-3112):** GeneralCommissioning (Breadcrumb) and GroupKeyManagement (GroupKeyMap) answer an attribute write's value of the wrong Go type with INVALID_DATA_TYPE for the path, the status matter.js's write validation gives a value of the wrong datatype (`packages/protocol/src/action/errors.ts:55-57` at `db021da0`); a command payload of the wrong Go type keeps INVALID_COMMAND. Whether a wrong TLV type from the wire reaches these servers as a wrong Go type was not traced (unverified). The other sites under "Where" are unchanged.

---

## Certification harness — open certifiability findings

- **The reference daemon's push transport augments a clip on every motion;
  chip's once per elapsed duration.** `examples/reference-bridge/camera.go`
  `motionTriggered` extends a recording transport's clip on each motion
  within the clip, where chip's `PushAVTransport` augments only when it has
  not yet augmented or the clip has run past the previous detected
  duration (`examples/camera-app/linux/src/pushav-transport/pushav-transport.cpp:375-387`
  at 6170af84, `mHasAugmented`, `mPreviousMotionDetectedDurationS`): a third
  motion soon after the second extends the clip here and not in chip. And
  at the instant a recording ends, chip is already in the blind period
  (`InBlindPeriod`, `:330-342`, `elapsedSeconds >= 0`) where the daemon is
  not until the next instant (`now.After`). Found by the #404 unit tests
  (2026-11-05), which assert only the agreeing cases. Route: align
  `motionTriggered` and the blind-period start with chip in the next
  camera-simulation change; the app-camera family does not reach either.

Class (a) gaps the CHIP certification families
(`internal/chiptool`, [ADR 0011](../../docs/adr/0011-certifiability-is-a-goal.md),
[`docs/certifiability.md`](../../docs/certifiability.md)) still report. Each
is excluded in `internal/chiptool/families_table_test.go` with class (a) and
a pointer here; fixing one means removing that exclusion, re-running the
family, and deleting the entry.

The group-messaging cases are not findings: they are skipped on a host
whose LAN interface has no IPv6, with the command that enables it, and run
wherever it has. TC-SC-4.1 and TC-SC-4.3 were wrongly listed here as such an
environment matter; they failed on an IPv6-capable runner because the node
did not answer an AAAA query for its own host name, and run unconditionally
since that was fixed.

### TC-TLSCERT-2.2, TC-TLSCERT-2.9 — the TLS certificate lists over a transport without large payloads

`cluster/tls` serves ProvisionedRootCertificates and
ProvisionedClientCertificates with the certificates on every transport
(`BD-chip-TlsCertificateManagement-RulesInServer`, owner decision
2026-10-28: matter.js's generated behaviour). connectedhomeip encodes the
ids alone when the read did not come over a large-payload transport
(`TLSCertificateManagementCluster.cpp:108`, `:136-166` at `6170af84`), and
both cases read over MRP expecting no certificate (`TC_TLSCERT_2_2.py`
step 5, `TC_TLSCERT_2_9.py:174-181`); in 2.9 two client certificates with
their intermediates exceed one message, and the read answers
RESOURCE_EXHAUSTED. Closing it needs the read path to know the session's
transport (no such flag reaches a cluster server today) and an owner
decision to depart from rule 1 here. Phase 34.

### TC-IDM-10.5 — a device type's revision term is judged against revision 1

connectedhomeip's device-type conformance check (`TC_IDM_10_5`,
`matter/testing/device_conformance_tests.py:404-414` at `6170af84`)
evaluates each cluster requirement with `EMPTY_CLUSTER_GLOBAL_ATTRIBUTES`,
whose `cluster_revision` is 1 (`conformance.py:139-147`), and the
`revision` term's "current" reads that field (`:277-294`). The Casting
Video Player's Messages conformance is `[Rev >= v2]`
(`data_model/1.6.1/device_types/CastingVideoPlayer.xml`; matter.js
`casting-video-player.element.ts`, the generated `device` requirement
"[Rev >= v2]"), so the case reports Messages disallowed on a revision-2
player — CI run 41967709504 on PR #403 — though the device type is at
revision 2 and chip's own tv-app serves Messages on a revision-3 player.
A TH limitation, not a gap of the module: the daemon's Messages moved
behind `--media-messages` (the MESS family's variant) so the default
composition passes the case. Route: re-read at the next harness pin
lift; when the check reads the endpoint's declared revision, Messages
returns to the default player. Phase 34.

### CHIP data model cross-check — class (ii)

Differences where connectedhomeip's data model (the model the Python
certification cases judge a device by) is right and the snapshot is wrong
(`internal/chipdm/acknowledged.go`, class `ii`;
[`docs/chip-datamodel-crosscheck.md`](../../docs/chip-datamodel-crosscheck.md)).
A bullet names the behaviour that depends on its entry or says that none does; where it says neither, the entry's reason in the table does. Each entry leaves the table, and this list, when the snapshot is corrected.

- `GroupKeyManagement.GroupKeySetStruct.GroupKeyMulticastPolicy` (conformance, CHIP `d`, ours `o`) — see `internal/chipdm/acknowledged.go`.
- `semtag.Label` (conformance, CHIP `mfgcode!=null,o`, ours `o`; behaviour depends on it where a host enables the Descriptor TagList and lists a manufacturer tag without a label) — see `internal/chipdm/acknowledged.go`.
- `*.cluster.0x0201.response.0x01` (level cross-check, presence at levels 1.4, 1.4.1, 1.4.2: CHIP absent, ours present; Thermostat GetRelayStatusLogResponse without its request in matter.js's model at v0.13.0, v0.14.0 and v0.16.0, dropped at v0.17.0; nothing served depends on it) — see `internal/chipdm/levels_acknowledged.go`.

### Device type validation — open departures

What `endpoint.ValidateDeviceTypes`
([ADR 0016](../../docs/adr/0016-device-type-validation.md)) finds in what
the module builds itself (`endpoint/devicetype_module_test.go` pins each).

## Verified sound (no action)

- **A peer that establishes a new CASE session does not end the controller's older sessions with it.** They go by the node layer (StartUp, ShutDown, an armed peer) or by failing traffic. As matter.js: `#evictExcessSessionsFor` counts only the sessions the peer established against `MAX_SESSIONS_PER_PEER` = 5 and closes the least recently used of those (`packages/protocol/src/session/SessionManager.ts:697-725`, `:703-708` at `db021da0`), so a new CASE session of the peer closes an older one only as the sixth; connectedhomeip evicts only when the session table is full (`src/transport/SecureSessionTable.cpp:73-80` at `6170af84`). Held by `fabrictest:TestControllerRecoversFromASilentRestart/subscription/device_resumes` (the session from before the restart is neither lost nor closed) (review II, R2-P6-par-3102, owner decision 239).
- **GroupcastTesting keeps one FabricUnderTest for the node.** Any fabric's command replaces or clears it, and removing the fabric under test does not end the test before its timer — as matter.js (`packages/node/src/behaviors/groupcast/GroupcastServer.ts:404-430`, `:522-525` at `b25ed960`). Mirrored; no `by_design` entry; clearing on removal would be ahead of matter.js (review II, phase 6a, R2-P6-fsm-1018).
- **The BTP idle timer runs on the central only.** A peripheral session whose central completed the handshake and then sends nothing has no idle timer (`transport/btp/session.go`), as in matter.js, whose "Central Device Idle Timer" (`packages/protocol/src/ble/BtpSessionHandler.ts:48` at `b25ed960`) is restarted only for the central role (`:495-500`). A silent central is bounded by `bridge/ble.go` `watchBLEPASE` until a secure session exists, then by the commissioning window and its fail-safe (review II, phase 6a, R2-P6-fsm-0212).
- **Bitmap bit conformance is judged for a host value only.** A controller's write is not held to it (`cluster/spec/write.go`), as matter.js enforces bit conformance only for a local actor (`packages/node/src/behavior/state/validation/ValueValidator.ts:286-300`, the gate `hasLocalActor` at `:295`, at `db021da0`). Mirrored; no `by_design` entry (review II, phase 6a, R2-P6-par-1305).
- **A repeated context tag in a decoded payload: the last one wins; a NaN float decodes.** Both are matter.js's shape: a repeat throws only with `preserveDataOrdering` (`packages/types/src/tlv/TlvObject.ts:229-232` at `b25ed960`), and `TlvNumber.ts` has no NaN check (`cluster/spec/codec.go` `DecodeFields`, `DecodeFloat64`; review II, phase 6a, R2-P6-wire-1313).
- **The controller's untyped JSON keys parse with a base prefix, and a repeated tag is encoded twice.** As chip-tool: `std::stoul(id, nullptr, 0)` and no repeated-tag check (`examples/chip-tool/commands/clusters/CustomArgument.h:133-138` at `6170af84`; `cluster/spec/json.go` `encodeAny`; review II, phase 6a, R2-P6-wire-1365).
- **A decoded TLV payload's container nesting has no depth bound.** `cluster/spec/codec.go` `ReadContainer` recurses per level and `tlv/` has no depth limit; neither reference bounds it — no depth in connectedhomeip's `TLVReader.h/.cpp`, `TLVCommon.h` at `6170af84`, nor in matter.js's `TlvAny.ts`, `TlvCodec.ts`, `TlvObject.ts`, `TlvArray.ts` at `db021da0` (grep). The message size bounds it (review II, phase 6a, R2-P6-bound-1312).
- **An emitted event's payload is not validated before it is recorded.** `cluster/spec/server.go` `EmitEvent`, as matter.js: `OnlineEvent`'s trigger hands the payload as given to `occurrenceManager.add` (`packages/node/src/behavior/Events.ts:203-208` at `b25ed960`) and broadcasts it (`:219-228`); the only check is the TLV encoder's mandatory-field check at report time (`packages/types/src/tlv/TlvObject.ts:128-134`). A payload check would be ahead of matter.js (review II, phase 6a, R2-P6-par-1315).
- **RvcRunMode refuses a mode with two or more of Idle, Cleaning and Mapping.** `cluster/modebase` `checkRvcRun` (`n > 1`), as matter.js `packages/node/src/behaviors/rvc-run-mode/RvcRunModeServer.ts:39-46` (`exclusiveModeCounter > 1`, `:45` at `b25ed960`); the error text "…together in one mode" is matter.js's own (`:49`). No defect (review II, phase 6a, R2-P6-par-1469).
- **Door lock schedule commands do not compare the accessing fabric with the user's creator.** As matter.js (`packages/node/src/behaviors/door-lock/DoorLockServer.ts:653-692` set, `:734-745` clear, `#requireValidUser` `:1365-1373` at `db021da0`) and connectedhomeip's set handler, which checks that the user exists only (`src/app/clusters/door-lock-server/CodegenIntegration.cpp:1051-1135`, `userExists` `:1091` at `6170af84`) (review II, phase 6a, R2-P6-acl-1566).
- **The reference daemon's DEM test-event forecasts state values chip's trigger file does not.** The adjustment fields on slots other than 0 (ForecastAdjustment's 20 / 2000 / 120 / 240; chip sets them on slot 0 only, `examples/energy-management/energy-management-triggers/DEMTestEventTriggers.cpp:267-270` at `6170af84`), and on the two "clear" triggers `EarliestStartTime` null with `LatestEndTime = EndTime` under STA where chip clears both (`:218-219`, `:252-253`). Conforming, the daemon's own values (`examples/reference-bridge/fleet_evse.go` `demoDem.conform`); TC_DEM_2_5 reads `slots[0]` only; the other DEM Python cases were not read (review II, phase 6a, the 1304 conformance fix).
- **AwaitNextAction repeats without a bound.** As matter.js (`packages/node/src/behaviors/ota-software-update-requestor/OtaSoftwareUpdateRequestorServer.ts:801-814` at `b25ed960`) (review II, phase 6a, R2-P6-fsm-1210).
- **A SimpleAnnouncement schedules without a provider.** It resets the retry count and keeps a running timer, as matter.js: the announcement schedules with no provider (`packages/node/src/behaviors/ota-software-update-requestor/OtaSoftwareUpdateRequestorServer.ts:413-416` at `b25ed960`), which resets the retry count (`:491-493`) and keeps a running timer (`:495-499`) (review II, phase 6a, R2-P6-fsm-1211).
- **Any fabric's AnnounceOTAProvider chooses the next provider the node-wide requestor queries.** Mirrored, both references: matter.js builds the provider from the accessing fabric (`packages/node/src/behaviors/ota-software-update-requestor/OtaSoftwareUpdateRequestorServer.ts:384-391` at `b25ed960`), adds it to the node-wide list with `previousQueryTimestamp: null` (`:423-438`, `:430`); a non-simple announcement schedules with it (`:408-412`) → `selectedProviderLocation` (`:486-487`) → used once (`:527-531`), else the oldest timestamp across all fabrics (`:535-540`); access "F A" (`ota-software-update-requestor.element.ts:65`). connectedhomeip takes the location from the accessing fabric (`src/app/clusters/ota-requestor/DefaultOTARequestor.cpp:332-334`) and hands it to the driver (`:347`), which, when idle, queries the announced provider (`DefaultOTARequestorDriver.cpp:323-329`) at `6170af84`. Preferring the fabric's own default would be ahead of both (review II, phase 6a, TM R3.5).
- **UnitLocalization refuses a duplicate SupportedTemperatureUnits entry at construction.** The model's text requires unique entries (`packages/model/src/standard/resources/unit-localization.resource.ts:43-45` at `db021da0`); neither matter.js's constraint (`unit-localization.element.ts:30-31`) nor connectedhomeip (`UnitLocalizationCluster.cpp:52-66` at `6170af84`) checks it. Host configuration only, ahead of both (review II, phase 6a, R2-P6-err-1268).
- **A secure session's exchanges end with it.** The bridge's BDX, OTA and Invoke exchanges are keyed by (session id, exchange id) and end when their session closes; they never pass to a later session given the same id, and a session keeps at most 30 exchanges, the least recently active ending — as matter.js, which keys its exchanges by the `Session` object (`packages/protocol/src/protocol/ExchangeManager.ts:569-576` at `db021da0`), so a later session finds none of an earlier one's, closes a force-closed session's exchanges (`packages/protocol/src/session/Session.ts:336-342`), and keeps at most 30 non-closed exchanges per secure session (`ExchangeManager.ts:63`, `:578-595`) (review II, phase 6a, R2-P6-sec-0302, R2-P6-bound-0303). One message per exchange awaits its acknowledgement, as matter.js: a reply or report of an interaction is refused while an earlier message of the exchange awaits its ack (`TrackOnExchange`, `f94b30a3`), as matter.js's `#send` throws "has not been acked yet, cannot send a new message" (`packages/protocol/src/protocol/MessageExchange.ts:701-705`) and a BDX message waits until its predecessor on the exchange is acknowledged, as matter.js's send does not return before its acknowledgement (`:786`, `:822-830`; review II, phase 7, decision 276). On every exchange — interaction, BDX and the unsecured session's PASE and CASE handshakes (decision 277) — the receive side refuses a message that carries no ack while one is owed (`:521-525`) and one whose ack names another counter than the exchange's outstanding message, a standalone ack excepted, which is ignored (`:526-533`, `:528-529`); a refused message is neither acknowledged nor processed (`:553-562`; `ExchangeManager.ts:435-437`, `:538-549`). An acknowledgement clears the outstanding message of its own exchange only (`MessageExchange.ts:521-542`; decision 278).
- **A null written to a non-nullable scalar attribute ends the write interaction with FAILURE.** As matter.js: the write decodes with the attribute's `TlvOfModel` schema, which wraps `TlvNullable` only for a nullable model (`packages/types/src/tlv/TlvOfModel.ts:234-236`, `:242-244`), before the value is assigned (`packages/protocol/src/action/server/AttributeWriteResponse.ts:429`, `:433-434` at `db021da0`); `packages/types/src/tlv/TlvNumber.ts:124-131` throws `UnexpectedDataError` for a TLV null (likewise `TlvBoolean.ts:22-23`, `TlvString.ts:57-58`), which is no `StatusResponseError`: the write's catch rethrows it (`AttributeWriteResponse.ts:452-460`, `:459`) and it ends as StatusResponse FAILURE (`packages/protocol/src/interaction/InteractionMessenger.ts:431`, `:458-460`) — read from the lines, not run. Here through decision 161's `im.UndecodableValue` (`bridge/attribute_value_reader.go`, `126d4566`) (review II, phase 6a, R4-1, owner decision 170).

- **A subscription is persisted after its SubscribeResponse, so a Stop in the same instant loses it.** `bridge/subscribe.go:1062-1071` records the subscription once the response is sent; matter.js's `SubscriptionsServer.#addSubscription` fires on `subscriptionAdded`, which the InteractionServer emits after the SubscribeResponse (`SubscriptionsServer.ts:54`, `:133-140`). Same timing; a test waits until it is stored (report S, question 7).

TLV element codes/widths, bool/null/float shapes, string/octet length forms, tag-encode thresholds; message/protocol header field order and presence rules; Sigma KDF salts/infos/nonces, TBE/TBS tag order, destinationId HMAC, RCAC/chain validation, AddNOC rollback + fail-safe-expiry revert, RemoveFabric teardown; SPAKE2 generators/point validation/Ke/transcript, PBKDF bounds; MRP max-transmissions = 5, nonce layout, standalone-ack delay, session-id collision avoidance; timed-invoke gate, keepSubscriptions purge, sendInterval formula, subscription death after 3 failures; EP0 cluster set, full-family PartsList, ServerList, empty-ACL fail-closed, CAT matching; OnOff toggle semantics, level scaling, thermostat limit-clamp + deadband, measurement null semantics, illuminance formula, cover inversion; mDNS operational instance-name + subtypes + TXT formats, multi-fabric boot re-announce, AddNOC-time publish, shutdown withdraw.
