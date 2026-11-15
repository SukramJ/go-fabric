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
- SetRegulatoryConfig ignores CountryCode (Location stays "XX"). `cluster/core/general_commissioning.go:629` vs `GeneralCommissioningServer.ts:210`. **Still open (2026-10-13):** deferred by batch 1 with its reason (Apple Home sent RemoveFabric after a non-empty Location); `bridge/fields_reader.go` (`decodeSetRegulatoryConfigRequest`) reads the field as octets while a UTF-8 string lands in `el.String`, so CountryCode is always empty, and `general_commissioning.go` (`handleSetRegulatoryConfig`) checks only the length. Fix package: decode the string, store it as Location, and verify against Apple Home in concept v2 phase 5 before it ships.

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
- **Groupcast's membership quota does not follow a raised `MaxGroupsPerFabric`.** matter.js derives `MaxMembershipCount` from `maxGroupsPerFabric × 2` (`GroupcastServer.ts:928` at `862ad75f`), so the Groupcast per-fabric quota `floor(MaxMembershipCount/2)` always equals the group cap. The module's `groupcastMaxMembershipCount = 44` is a constant (`cluster/core/groupcast.go:52`); since brief CC a host can set `node.Config.Limits.MaxGroupsPerFabric` above 22 and is then still held to Groupcast's quota of 22. Route: derive the Groupcast constants from the configured group limit when the Groupcast server is composed (the wave that next touches `groups/`; M5). (Triage 2026-10-29: wave H1, the device IM and write path.)
- **Groupcast has not met a real controller.** The evidence is the in-process
  end-to-end test (`bridge/groupcast_e2e_test.go`) and the wire fixtures
  matter.js produced (`bridge/testdata/groupcast-wire-fixtures.json`).
  Unverified controller-side assumptions: that a controller provisions a
  1.6.1 node through Groupcast JoinGroup (rather than KeySetWrite /
  GroupKeyMap / AddGroup) and sends on FF05::FA for IanaAddr; that it relies
  on `UseAuxiliaryAcl` instead of writing a Group ACL entry; and that
  chip-tool's `groupcast` commands encode the optional fields as matter.js
  does. Fix package: an `internal/chiptool` leg (`groupcast join-group`, a
  multicast `onoff toggle`, `groupcast leave-group`).

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
  case for a restart that re-establishes subscriptions (issue #362).
- **A closed PASE session does not resume the commissionable broadcast on
  the responder.** matter.js's `sessions.deleted` observer restarts the
  commissioning advertisement with "startup" when a PASE session closes
  while the node is in commissioning mode (`DeviceAdvertiser.ts:152-161`,
  `:248-260` at `862ad75f`). The operational manager fires its reannounce
  trigger with fabric index 0 for a PASE victim; on an advertiser with the
  broadcast events (`mdns.Responder`) the bridge starts no event for it
  (`bridge/securechannel.go`, `triggerSessionReannounce`), and only the
  `TriggerReannounce` and per-record fallbacks re-announce the
  commissionable record. Fix: on index 0, start the responder's
  "startup" schedule for the active commissionable service (issue #362).
- **A subscription that ends while the node runs stays persisted.**
  `im/subscription/manager.go` (`Manager.Release`, `finishClose`) keeps the former matter.js line: closed
  on session close = not terminated = re-established after a restart.
  matter.js #4742 drops the stored subscription whenever it ends while the
  node runs; only `closeForShutdown` leaves it for re-establishment
  (`Subscription.ts:21-40`, `ServerSubscription.ts:766-812`,
  SubscriptionsServerTest "drops the stored subscription when the peer closes
  its session while the node keeps running"). Fix: terminate on every close
  except the one `Stop` performs. (Triage 2026-10-29: wave H1, the device IM and write path.)
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
  Crosses `secure/channel` and `bridge`. (Triage 2026-10-29: wave H2, commissioning and security (Fable).)
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
- **A closed PASE session does not restart the commissioning
  advertisement.** The device keeps its commissionable record during PASE
  and publishes nothing when the session closes; matter.js starts a
  commissioning advertisement round on every PASE session that closes
  while the window is open (`protocol/DeviceAdvertiser.ts:131-162`). (Triage 2026-10-29: wave H2, commissioning and security (Fable).)
- **A host-initiated RemoveFabric does not flush the Leave event.**
  `node/node.go:707` `RemoveFabric` closes the fabric's subscriptions
  without a last report; matter.js `Fabric.leave` closes each session
  through `closeSubscriptions(true)` so the Leave event reaches the
  controller (`fabric/Fabric.ts:447-457`; ClientNodeTest "erases node
  after leave event"). (Triage 2026-10-29: wave H1, the device IM and write path.)
- **Data versions are drawn anew at every start.**
  `contract.DataVersionTracker` is the host's, and the module's own
  trackers start random; matter.js loads a persistent cluster's version
  from its store and draws one only when the store has none
  (`behavior/state/managed/Datasource.ts:597`, `:802`), so a controller's
  cache survives a device restart where this side sends every cluster
  again. (Triage 2026-10-29: wave H1, the device IM and write path.)
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
  SetRegulatoryConfig CountryCode item below.)

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
  node option over the store.)

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
- **The OccupancySensing coupling is not built.** matter.js's
  `#setupOccupancyIntegration` follows the Occupancy attribute of an
  OccupancySensing server on the endpoint `localOccupancyMeasurementEndpoint`
  names, preferring it over `externallyMeasuredOccupancy`
  (`ThermostatServer.ts:538-558`); the Go server takes occupancy from the
  host only (`ThermostatServer.SetOccupied`, matter.js's
  `externallyMeasuredOccupancy`, `:559-575`). A host that composes a
  thermostat with an occupancy sensor wires the two itself. (Milestone 4,
  the embedding layer.)

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

- **No generic sanitisation of fabric-scoped data on fabric removal.**
  matter.js strips every fabric-scoped list entry of a removed fabric from
  every behavior's state (`ServerEnvironment.ts:187-195`,
  `limitNodeDataToAllowedFabrics`); Go drops it only in servers that
  implement `MatterFabricRemoved` (`bridge/bridge.go:1317-1322`), and the
  WebRTC Transport Provider (`cluster/camera/provider.go`) keeps a removed
  fabric's `CurrentSessions` (ServerNodeTest.ts:1549-1580 "removes the
  entries of a removed fabric from a fabric-sensitive list" stays missing).
- **A corrupt endpoint allocation counter fails the allocation.**
  `endpoint/sqlitestore` `allocate` refuses a counter below the first part
  number (and a non-integer one fails its scan); matter.js starts over from
  the first number (ServerNodeTest.ts:941-957, "starts over from 1 when the
  next number in storage is corrupt", stays missing).
- **No flushing close of a subscription.** `Manager.Close` drops the
  queued changes; matter.js's close of a subscription sends a report queued
  behind the one in flight first, despite a deferral
  (ServerSubscriptionTest.ts:1123-1195 and :499-555 stay missing).

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
- **Numeric bounds compare as float64.** `cluster/spec`'s constraint check
  (`Instance.satisfies`, `bound_expr.go` `evalBound`) and `Bound.Int`
  (int64) hold a bound beyond 2^53 inexactly: 2^62 + 1 compares equal to
  2^62, and a uint64 bound above int64 cannot be stated. matter.js keeps
  such bounds as bigints (`TlvOfModelTest.ts` "refuses a value beyond the
  computed bound", "refuses the value above it", "states a bound beyond
  the safe integers as a bigint"; `ConstraintTest.ts` "keeps the magnitude
  of a decimal / hexadecimal / negative bound", "judges a value against a
  bound only a bigint states" and its value tests at 2^53 ± 1 and 2^62 ± 1).
  No served attribute states such a bound at the pin. Route: compare integers as integers (uint64/int64 or
  math/big) where the type is a 64-bit integer.

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

- **F-db021da0-1: the device side sends above the peer's MAX_TCP_MESSAGE_SIZE.**
  matter.js refuses a message above the TCP limit of its session — the
  peer's announced MAX_TCP_MESSAGE_SIZE under the channel's own — with
  `MessageTooLargeError` on both roles (`MessageChannel.send`,
  `packages/protocol/src/protocol/MessageChannel.ts:104-112` at `db021da0`,
  matter.js #4793). The client stack does since wave P4
  (`client/exchange` `checkSize`); the bridge sizes its report chunks by the
  peer's announced size without the 64000 fallback or the 1232 floor and
  does not refuse a larger message (`bridge/reply.go:379-400`
  `chunkLimitsFor`), and its BDX exchange sizes blocks by the connection
  alone (`bridge/ota_provider_link.go:558-566` `MaxPayloadSize`). Fix:
  the session's peer cap in both and the refusal before the send. Found in
  the review of wave P4 (#433); left out of that wave's scope.
- **F-db021da0-3: a failed subscription report is retried one MinIntervalFloor later.**
  matter.js, after a report that could not be sent, owes the data again and
  schedules nothing: the attribute filter goes back through
  `#addOutstandingAttributes`, which only fills the set
  (`packages/node/src/node/server/ServerSubscription.ts:346-357` at
  `db021da0`), the event number is set back (`:582-584`), and neither calls
  `#prepareDataUpdate` as the change handler does (`:380-382`); the send loop
  ends unless another update was queued behind the failed one (`:610-612`).
  The retry leaves with whatever starts the next report — a change, an urgent
  event, the send interval — and after three failed updates a subscription
  whose error is a communication error is given up (`:569`, `:585-603`).
  This module asks for a report again:
  `Subscription.RequeueAttributes` marks the paths dirty and
  `Subscription.RequeueEvents` marks the owed events urgent
  (`im/subscription/subscription.go`), so the retry goes out once
  MinIntervalFloor has elapsed since the failed report. Not deliberate: it
  is what the attribute path did before events shared its report, and the
  event half was kept the same so that one report has one rule (review II,
  W2-conc-05). Pinned as it is by the subtest "events owed again after a
  failed report are retried when the floor ends" of
  `im/subscription:TestEventReportParityMatterJS`. After three failed
  updates in a row the subscription ends and the peer's sessions stay, as
  in matter.js (`noteSubscriptionSendError`, `noteReportUndelivered`,
  `bridge/subscribe.go:1018`, `:1135`; review II, R2-P4-imread-04, owner
  decision 60; `bridge:TestUndeliveredReportIsOneFailedUpdateParity`);
  what is left of this row is when the retry leaves. Fix: requeue without asking for a report — the paths and the
  event number are owed and leave with the next pass something else starts —
  and move the subtest onto matter.js's rule. Found in review II, phase 0.
- **F-db021da0-4: what is outstanding when a subscription is activated waits for MinIntervalFloor.**
  matter.js sends what is outstanding at activation at once: `activate`
  calls `#triggerSendUpdate()` when an attribute update or an event number
  is outstanding
  (`packages/node/src/node/server/ServerSubscription.ts:443-445` at
  `db021da0`), and `#triggerSendUpdate` starts `#sendUpdate` directly —
  no floor, no send delay (`:520-529`); an event the initial report already
  carried is not owed again (`:431-437`). `Subscription.EndPriming`
  (`im/subscription/subscription.go`) stamps `lastReport` with the end of
  the priming report and marks the events fired meanwhile urgent, so the
  changes and events of the priming window go out once MinIntervalFloor has
  elapsed since the priming report. Not deliberate: no entry of
  `by_design.md` gives a reason, and the function's comment states the
  difference without one. Pinned as it is by the subtest "an event fired
  while the subscription was priming is reported after the floor" of
  `im/subscription:TestEventReportParityMatterJS`. Fix: when priming ends
  with a dirty path or an owed event, the first report is due at once, and
  the floor counts from that report; move the subtest. Found in review II,
  phase 0 (W2-conc-05).
- **F-db021da0-7: a handshake's failure StatusReport is sent while its previous reply is unacknowledged.**
  Both references send none. matter.js: `PaseServer.cancelPairing` skips
  the report "when a sent message is still unacked"
  (`packages/protocol/src/session/pase/PaseServer.ts:208-210` at
  `db021da0`), `CaseServer` returns before any `sendError` when
  `exchange.considerClosed || exchange.hasUnackedMessage`
  (`packages/protocol/src/session/case/CaseServer.ts:82-86`), and the two
  initiators test the same
  (`packages/protocol/src/session/case/CaseClient.ts:86-90`,
  `packages/protocol/src/session/pase/PaseClient.ts:97-100`);
  `hasUnackedMessage` is `#sentMessageToAck !== undefined`
  (`packages/protocol/src/protocol/MessageExchange.ts:450-452`), and the
  comments give the reason: "the send would only fail with a flow error".
  connectedhomeip, read at `6170af84`: a retransmit-table entry sets its
  exchange's waiting-for-ack flag when it is made and clears it when it is
  destroyed (`src/messaging/ReliableMessageMgr.cpp:53-62`), and it is
  destroyed with the acknowledgement
  (`src/messaging/ReliableMessageContext.cpp:89-95`,
  `ReliableMessageMgr.cpp:340-351`); while the flag is set, a message on the
  exchange that carries no acknowledgement for it is dropped before the
  delegate sees it — "Dropping message without piggyback ack when we are
  waiting for an ack." (`src/messaging/ExchangeContext.cpp:585-595`) — so
  the pairing session is not handed the message that would make it fail;
  and a `SendMessage` in that state fails with `CHIP_ERROR_INCORRECT_STATE`
  in `AddToRetransTable` (`ReliableMessageMgr.cpp:254-256`), before the
  message is sent (`src/messaging/ExchangeMessageDispatch.cpp:54-64`),
  which `SendStatusReport` logs and returns
  (`src/protocols/secure_channel/PairingSession.h:183-189`). Not
  established for connectedhomeip (unverified): whether a failure that does
  not come from a received message — a timeout of the pairing session's
  own — reaches `SendStatusReport` in that state at all. This module drops
  what the handshake left unacknowledged on the exchange and then sends the
  report as a reliable message: `Bridge.sendHandshakeFailureReport`
  (`bridge/securechannel.go`, `abandonHandshakeReplies` first), and the
  handshake this node opened abandons before it sends its report the same
  way (`bridge/case_initiator.go`). The cases coincide for an initiator
  that acknowledges a reply with its next message, which is what makes a
  handshake fail in the ordinary run (a Pake3 that does not confirm, a
  Sigma3 that does not verify); they differ when the handshake fails while
  its last reply is still unacknowledged. Which peers and failures reach
  that state was not exercised (unverified). Not decided: the report was
  made reliable by `e98028e4` (before, it left without the R flag, which
  neither reference does either), and whether to skip it as the references
  do or to keep it under a `by_design` entry is an open question of that
  rework. Fix: skip the report while an entry of the exchange is still
  unacknowledged, with a case for each role beside
  `bridge:TestUnsecuredRetransmitEntriesEndWithTheirHandshake`. Found in
  review II, phase 0 (the rework of R2-P0-rv-A04).
- **F-db021da0-8: a report pass that leaves nothing for the subscriber sends no DataReport.**
  matter.js takes what is outstanding, stamps the time of the last update
  before it sends
  (`packages/node/src/node/server/ServerSubscription.ts:549-551` at
  `db021da0`) and takes the data branch whenever an attribute filter or an
  event number is outstanding (`:909`, `:921-936`), with
  `suppressEmptyReport: onlyWithData` (`:935`); `onlyWithData` is false for
  the pass the send delay or the send interval starts (`:198-200`,
  `:480-484`, the default of `:520`) and true only for a flush (`:757`) and
  for a queued follow-up with no keep-alive owed (`:622`), and the
  messenger leaves a report out only when that flag is set and the report
  is empty (`packages/protocol/src/interaction/InteractionMessenger.ts:794-796`).
  So a pass whose data is all filtered for the subscriber goes out as an
  empty DataReport with `suppressResponse: false` (`:928`) and counts as
  the last update — under a TODO of matter.js's own, "Add correct handling
  for reports that would have data but in the end not send any because of
  filtered out" (`:923-925`). Here the bridge's event reporter sends
  nothing when its access check leaves no event and no attribute for the
  subscriber and says so (`Subscription.NothingSent`,
  `im/subscription/subscription.go`; `reportSubscriptionEvents`,
  `bridge/subscribe.go`), and the engine then sends the keep-alive when the
  pass was the send interval's and otherwise puts the time of the last
  report back (`dueReport`, `im/subscription/engine.go`); an event drain
  that yields no event is no report either (`758fb3ec`). Since `16495f81`
  the keep-alive due in such a pass is sent — before, the pass counted and
  a subscriber heard nothing for two send intervals (review II,
  R2-P0-rv-A03) — so what still differs is the empty DataReport itself and
  the send interval it restarts. The attribute path of this module goes the
  other way: `reportSubscription` sends the report empty when every dirty
  path was filtered (`bridge/subscribe.go`, "An empty report is a
  max-interval keepalive"), as matter.js does. The doc of `NothingSent`
  calls its rule the module's own; no entry of `by_design.md` gives a
  reason, so it stands here. Pinned as it is by
  `im/subscription:TestKeepAliveAndAReportThatSendsNothing` and
  `bridge:TestReportWhoseEventsAreAllFilteredLeavesTheKeepAliveDue`. Fix:
  one rule for both paths — send the filtered pass as an empty DataReport
  and count it, as matter.js, together with the keep-alive entry of the
  `862ad75f` section (`SuppressResponse`), or keep the silence under a
  `by_design` entry; the owner's decision. Found in review II, phase 0.
- **F-db021da0-9: `Identify.Close` during a running identify does not tell the host to stop.**
  matter.js's `IdentifyServer[Symbol.asyncDispose]` calls
  `#stopIdentifying()`
  (`packages/node/src/behaviors/identify/IdentifyServer.ts:116-119` at
  `db021da0`), which, while the identify timer runs, stops it, sets
  `isIdentifying` to false and emits `stopIdentifying` (`:100-106`), the
  event the class doc calls mandatory (`:25`). `Identify.Close`
  (`cluster/core/identify.go`) marks the server closed, closes the
  countdown's `done` and waits for its goroutine; the tick that finds
  `done` closed leaves without counting, and the host's
  `MatterIdentify(ctx, 0)` is called only by the tick that counts to zero
  (`tick`) and by a request that sets 0 (`setIdentifyTime`). A host that
  began identifying on an endpoint that is then removed is never told to
  stop. The comment over `Close` says it mirrors `[Symbol.asyncDispose]`,
  "which stops the identify timer" — half of what the lines do. Read, not
  exercised: whether a test closes a server while it identifies and looks
  at the handler was not searched for (unverified). Fix: `Close` on a
  running countdown reports the stop as the zero tick does, from a
  goroutine of its own that `Close` does not wait for (R2-P0-rv-B04), with
  that test, and `contract/identify.go` says so; whether that call gets a
  context `Close` ends is the open question beside it. Found in review II,
  phase 0 (the rework of R2-P0-rv-B04).
- **F-db021da0-10: a ConnectNetwork whose result outlives its fail-safe
  is still answered with the driver's result.** matter.js has no logic
  here (`packages/node/src/behaviors/network-commissioning/NetworkCommissioningServer.ts:18`
  at `db021da0` is the empty subclass; `BD-chip-WiFiNetworkCommissioningServer`),
  so connectedhomeip decides. Its fail-safe expiry releases the pending
  command handle (`mAsyncCommandHandle.Release()`,
  `src/app/clusters/network-commissioning/NetworkCommissioningCluster.cpp:1002`
  at `6170af84`): the release ends the command's pending work
  (`src/app/CommandHandler.cpp:40-47`) and, with none left, finalizes the
  response message and closes the handler
  (`src/app/CommandHandlerImpl.cpp:461-489`) — nothing was added for the
  ConnectNetwork path at that point. The driver's late result finds the
  handle gone and returns before it builds a ConnectNetworkResponse
  (`NetworkCommissioningCluster.cpp:845-857`, under
  `CHIP_DEVICE_CONFIG_SUPPORTS_CONCURRENT_CONNECTION`). What the finalized
  message carries for a path nothing was added to was not traced
  (unverified). Since `3b77e4e8` this module's late result touches neither
  the reverted networks nor the Breadcrumb, but the command still answers
  a ConnectNetworkResponse with the driver's status, after the expiry
  (`cluster/core/network_commissioning_wifi.go`, `connect`). Fix: decide
  what the invoke answers once its fail-safe has expired — the handler here
  returns only when the driver does, so ending the interaction at the
  expiry needs the expiry to end the handler's wait — and pin it with
  `TestWiFiNetworkCommissioningConnectAfterExpiryIsDropped`. Found in
  review II, phase 1 (the fix of R2-P1-core-06).
- **F-db021da0-11: RemoveFabric answers InvalidFabricIndex for every
  failure.** matter.js answers `InvalidFabricIndex` when the fabric is
  unknown (`packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:412-419`
  at `db021da0`) and awaits `fabric.leave` without a catch (`:425`), so an
  error of the removal leaves the handler; what the interaction answers
  then was not traced (unverified). connectedhomeip sends the NOCResponse
  `kInvalidFabricIndex` for `CHIP_ERROR_NOT_FOUND` only
  (`src/app/clusters/operational-credentials-server/OperationalCredentialsCluster.cpp:869-874`
  at `6170af84`) and returns any other error as the command's status
  (`:876-880`, "We have no idea what happened; just report failure").
  `handleRemoveFabric` maps every error of `RemoveFabric` — a store error
  after the Leave event went out included — to a NOCResponse with
  `InvalidFabricIndex` (`cluster/core/operational_credentials.go`); since
  `7b819233` that store failure is logged at error level, and the Leave
  cannot be taken back. Fix: `InvalidFabricIndex` for the fabric the store
  does not know, a failure status for anything else, with a test over a
  store that fails. Found in review II, phase 1 (the fix of
  R2-P1-core-11).
- **F-db021da0-13: the chunk StatusResponse bound keeps the peer on one
  interval.** matter.js's maximum peer response time sums the peer's
  retransmission schedule and switches it from the active to the idle
  interval once the wait passes the peer's active threshold
  (`maxResponseTimeOf`, `packages/protocol/src/protocol/MRP.ts:198-224` at
  `db021da0`, the switch at `:206-210`; called for the peer at `:102`).
  `chunkStatusResponseTimeout` sums the peer's schedule on the one base
  interval the session lookup resolves for it (`bridge/subscribe.go`);
  since `e931c2d7` it adds this node's own schedule, with that switch, as
  matter.js does (`:103-106`). Not a decision: the session port
  (`SessionRetransmitIntervalResolver`) names one interval, not the
  peer's idle interval, active interval and threshold. Fix: the port
  hands all three and the peer term uses `maxScheduleTime` as the local
  one does; `TestChunkStatusResponseTimeoutSumsBothDirections` gains the
  case. Found in review II, phase 1 (the fix of R2-P1-req2-12).
- **F-db021da0-15: the controller holds reported values to their
  constraints, and fails on a malformed one, where matter.js decodes by
  shape and skips.** matter.js decodes each attribute of a data report
  with `decodeTlv` (`InputChunk.ts:299-303` at `db021da0`), which does
  not validate (`TlvSchema.ts:64-66`): an octet string is read at any
  length (`TlvString.ts:57-71`), a missing mandatory field is left unset
  (`TlvObject.ts:236-250`); an entry that does not decode is logged and
  skipped, and the read goes on (`InputChunk.ts:312-320`). The
  commissioner decodes reported values with the generated decoders, which
  check constraints, and returns `errUnexpectedResponse` for one that
  fails: BasicCommissioningInfo (`controller/commissioner/flow.go:750`),
  the JCM reads of Fabrics, NOCs and TrustedRootCertificates
  (`jcm.go:162`, `:186`, `:217`), and — until `224a9495` — Networks,
  which failed the commissioning of a chip Linux app that reports an
  empty NetworkID (R2-P1-ci-01, fixed for Networks alone, where a value
  of the wrong shape is still an error rather than a skipped entry). How
  matter.js decodes a command response was not read (unverified), and no
  device is known that reports the other values outside their
  constraints. Fix: one decoder for a reported value in the controller —
  `spec.DecodeValue`, which checks no constraint, or a shape-only mode of
  the generated decoders — and the skip-and-continue rule, each with a
  parity case; phase 4. Found by CAMERA-GO-1.1 on a CI runner of pull
  request 525.
- **F-db021da0-17: a DataVersion here is never 0.** A
  `contract.DataVersionTracker` starts at a random value that is not 0,
  `Bump` skips 0 on its way round, and `Current` uses 0 for "not installed
  yet" (`contract/dataversion.go`). matter.js starts a server datasource's
  version at `entropy.randomUint32`, which can be 0
  (`packages/node/src/behavior/state/managed/Datasource.ts:597` at
  `db021da0`), and `advanceVersion` wraps past 0xFFFF_FFFF to 0
  (`:690-699`). A version taken from a store (`:796-805`, `:837`:
  `this.version = store.version`) is only that of a
  `Datasource.ExternallyMutableStore` (`:291-294`), the client-side cache
  of a peer's values (`packages/node/src/storage/client/DatasourceCache.ts:28`,
  checked in `packages/node/src/behavior/internal/ClientBehaviorBacking.ts:81`);
  no file under `packages/node/src/storage/server` names it. So both stacks
  draw a new version at every start and prime whole after a restart, and
  no controller can tell the two apart there. What remains is the zero
  value, one in 2^32. Since owner decision 46 the wire and the filters
  treat 0 as an ordinary version; the tracker's own zero state is what is
  left of the sentinel. Fix: a `by_design` sentence for the never-zero
  tracker (reason: 0 is the tracker's not-installed state), or the tracker
  holds "installed" beside the value and 0 becomes a version. Found in
  review II, phase 2; the restart half was struck in phase 4
  (R2-P4-cert-11): until then this row said matter.js's versions come back
  from its store.
- **F-db021da0-19: the client role's CASE resumption records live in memory.** matter.js persists the initiator's record: `CaseClient` saves it when a session is established (`packages/protocol/src/session/case/CaseClient.ts:379` at `db021da0`) and `SessionManager.saveResumptionRecord` writes it to the resumption store (`packages/protocol/src/session/SessionManager.ts:896-908`). The node's client stack keeps its records in a `peer.MemoryResumptionStore`, the store's resumption table being the responder's — one row per peer, without the session parameters and CATs a client record carries (`node/client.go:113-116`). Who sees it (inferred, not run): a bound peer after the node restarts, which gets a full CASE where a matter.js node would resume. Fix: a client-side resumption table behind the store port — a port extension, placed in phase 5 of review II. Until 2026-11-14 `BD-Matter-NodeClientStack-SecondSocket` pointed at a row here that #137 had removed with the code unchanged. Found in review II, phase 4 (the `by_design` re-read).
- **F-db021da0-20: DoorLock's ten validated commands are held to their bounds only.** matter.js validates a request of setUser, getUser, clearUser, setCredential and the six schedule commands against the model and turns a validation error into INVALID_COMMAND (`Supervision(…).onValidationError`, `packages/node/src/behaviors/door-lock/DoorLockServer.ts:1493-1511` at `db021da0`; the check that calls it, `packages/node/src/node/integration/ProtocolService.ts:660-676`) — enum values and conformance beside the static bounds. The definition's request check holds bounds, lengths, counts and mandatory fields (the DoorLock section of `script/clustergen/relaxed.go:99-115`), so an undefined enum value in SetUser reaches the DoorLock server. For DoorLock's other commands matter.js skips the generic validation (`ProtocolService.ts:343-348`); a missing mandatory field of such a command is INVALID_COMMAND at decode here, and what matter.js's handler is given in that case was the fix's reading, not repeated (unverified). Fix: a request-time model check for the ten commands; the decode half is a decision about the decoder's type level. Found in review II, phase 4 (the fix of owner decision 64).
- **F-db021da0-21: LocalTemperatureCalibration is served and not applied.** matter.js applies the calibration where a measured temperature comes in: `#handleMeasuredTemperatureChange` adds `localTemperatureCalibration * 10` (`packages/node/src/behaviors/thermostat/ThermostatServer.ts:511-514` at `db021da0`), and an application that sets `localTemperature` itself bypasses it, with a warning (`:491-496`). Go's `ThermostatServer.SetLocalTemperature` is that direct setter (`cluster/thermo/thermostat_server.go:699`) and the server has no measured-temperature input, so nothing applies the calibration it serves (`:684`); applying it in the direct setter would depart from matter.js. Fix: a measured-temperature input on the server that calibrates and stores LocalTemperature — a new exported method, one decision with the OccupancySensing coupling of the `862ad75f` section; the API bundle of phase 5. Found in review II, phase 4 (R2-P4-appclim-05).
- **F-db021da0-22: a generated server reports every change of a quieter attribute at once.** matter.js reports a property of quality Q through its quiet event (`packages/node/src/behavior/internal/ServerBehaviorBacking.ts:230-262` at `db021da0`), and PowerSource's BatPercentRemaining, BatTimeRemaining and BatTimeToFullCharge at most every ten seconds (`packages/node/src/behaviors/power-source/PowerSourceServer.ts:25-34`). The measurement projection follows it since review II (R2-P4-nv-05b). The generated `spec.Server` does not: `store` advances the version and notifies for every changed attribute, a quieter one included (`cluster/spec/server.go:463-481`). The fix's probe saw three `Set`s of the kit PowerSource's BatPercentRemaining at one instant as three reports and three versions; the probe was not kept and not repeated. It holds for every Q attribute of every generated kit server. Fix: the quieter in `spec.Server`'s report path, with PowerSource's ten seconds. Found in review II, phase 4.
- **F-db021da0-23: eight of the features matter.js's servers enable by default were not compared.** matter.js lists the features its published servers enable (`INTENTIONALLY_ENABLED`, `packages/node/test/behavior/cluster/ClusterBehaviorFeaturesTest.ts:27-37` at `db021da0`): AccessControl extension, BooleanState changeEvent, GeneralDiagnostics dataModelTest, Groupcast listener, sender and perGroup, Groups groupNames, IcdManagement checkInProtocolSupport, ScenesManagement sceneNames, TimeFormatLocalization calendarFormat and UnitLocalization temperatureUnit. BooleanState's is the kit's default since review II (`device:TestClusterBehaviorFeaturesParityMatterJS`); whether the module's servers select the other eight by default was not checked (unverified). Fix: the port of that test for the eight, each a parity case or a `by_design` entry. Found in review II, phase 4.
- **F-db021da0-27: the OTA provider judges a second QueryImage by the transfer's last block, not by the two sessions' activity.** matter.js cancels a running transfer for a QueryImage on another session only when the transfer's session was last active before the querying one — "likely a reboot" (`packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts:197-201` at `db021da0`) — and answers Busy otherwise (`:234`). The invoke context here carries no last-activity time of the querying session, so the transfer's last block stands in for its session's activity and the querying session counts as active now (`cluster/core/ota_provider.go:468-493`). Who sees it (read, not run): a requestor that queries again on a new session while its transfer on the old one still receives blocks — Busy from matter.js, a cancelled transfer and a new offer here. Fix: the session's last activity in the invoke context. Found in review II, phase 4 (R2-P4-coresys-10).
- **F-db021da0-28: BridgedDeviceBasicInformation serves NodeLabel and SerialNumber whatever the host set.** Both are optional in the model (`packages/model/src/standard/elements/bridged-device-basic-information.element.ts:30`, `:40` at `db021da0`); that matter.js lists an optional attribute only when its state holds a value was the fix's reading of `ValidatedElements.ts:247-279` and was not repeated (unverified). Here NodeLabel is always served, empty when the host gave none (`cluster/core/bridged_device_basic_information.go:245`), and SerialNumber is filled from the UniqueID when the host gave none (`:179-181`) — the pair matter.js warns about, "uniqueId and serialNumber shall not be the same" (`basic-information-validators.ts:42-44`). A VendorID of 0 or above 0xFFF4 and a ProductID of 0 are warnings here (`:281-301`) where matter.js's validator, which the bridged server runs (`BridgedDeviceBasicInformationServer.ts:45`), throws (`basic-information-validators.ts:32-40`). The code gives Apple's HAP mapper as the reason for SerialNumber, as the root's `BD-Matter-BasicInfo-SerialNumberAlwaysServed`; for NodeLabel it gives none. Not decided: follow matter.js, or a `by_design` entry with the owner's reason. Found in review II, phase 4 (R2-P4-coresys-05).
- **F-db021da0-29: a command with the Large Message quality is served over any transport.** matter.js answers a command whose model carries the Large Message quality INVALID_TRANSPORT_TYPE on a session that carries no large messages, after the existence checks and the command's privilege and before the fabric-scope check (`packages/protocol/src/action/server/CommandInvokeResponse.ts:263-290`, the gate at `:281-285`, at `db021da0`), and leaves it out of a wildcard path's targets (`#wildcardTargetOf`, `:367-386`); `largeMessage` is the `supportsLargeMessages` of the exchange's session (`packages/node/src/behavior/context/server/RemoteActorContext.ts:208`). connectedhomeip refuses the same way (`CheckCommandFlags`, `src/app/InteractionModelEngine.cpp:1875-1884` at `6170af84`). matter.js's client requires TCP for such an invoke: the request's `largeMessage`, inferred from the command when the caller left it open, makes TCP the required transport (`packages/protocol/src/action/client/ClientInteraction.ts:716-719`, `:1235`), and a required transport is the only one the peer connects on (`packages/protocol/src/peer/Peer.ts:369-376`). Here the device's invoke path does not ask for the quality — `schema.CommandLargeMessage` (`schema/large_message.go:26`) has one caller outside the tests, the client's inference (`client/interaction/invoke.go:181`) — so such a command runs over UDP and BLE as well, and the bridge's peer sender (`PeerSession.Invoke`, `bridge/ota_provider_link.go:138`) sends one over the session it holds. The gate cannot come alone: the peer sender sends over the bridge's own session registry and cannot open TCP (ADR 0019), so with the gate a camera's signals between two nodes of this module would not arrive (owner decision 76). Fix: the peer path moves onto `client/`, which opens TCP for a large message, with an ADR that supersedes ADR 0019's peer-path decision; the gate follows with it. Placed in phase 5 of review II. Found in review II, phase 4 (R2-P4-imwrite-01).
- **F-db021da0-30: the redaction of another fabric's fabric-sensitive fields is written by hand.** matter.js decides the filtering of a fabric-scoped list in one place for every behavior, from the list's `fabricScoped` and `fabricSensitive` configuration (`createProxy`, `packages/node/src/behavior/state/managed/values/ListManager.ts:609-616` at `db021da0`); where it withholds a single field was not re-read for this row (unverified). Here each server that holds such a list withholds the fields itself. A guard holds the class: `node:TestFabricSensitiveFieldsWithheldFromOtherFabrics` walks every served fabric-scoped list whose entry struct has a fabric-sensitive field of the generated definition (`spec.Field.FabricSensitive`, `cluster/spec/model.go:169-171`) and holds the bytes on the wire to what matter.js serves (`node/fabric_sensitive_parity_matterjs_test.go:30-55`). No wire difference is known. Fix (owner decision 85): the redaction generated from the model, in phase 5 of review II; the guard holds the class until then. Found in review II, phase 4.
- **F-db021da0-31: `Handle` replaces a different matter.js counterpart in each server.** LevelControl's `Handle` names the override of the command's logic method, which keeps the checks and the command method's scene invalidation (`cluster/levelcontrol/typed.go:137-146`; `moveToLevel` invalidates and then calls `moveToLevelLogic`, `packages/node/src/behaviors/level-control/LevelControlServer.ts:268-277` at `db021da0`). ColorControl's names the override of the command method — the gate, the mode switch and the transition included (`cluster/light/colorcontrol_typed.go:160-166`) — and matter.js's invalidation sits in that method (`packages/node/src/behaviors/color-control/ColorControlServer.ts:594-599`), so it goes with the override. OnOff invalidates inside `on()` and `off()` themselves (`packages/node/src/behaviors/on-off/OnOffServer.ts:97-100`, `:119-122`); its `Handle` (`cluster/onoff/server.go:756-764`) was not compared (unverified). Who sees it: a host that replaces a command through `Handle` — whether the current scene ends with the command depends on the server. Fix: one meaning of `Handle` across the kit's servers; the API bundle of phase 5 of review II. Found in review II, phase 4 (R2-P4-dr-app-04).
- **F-db021da0-32: a report reads every concrete path twice.** matter.js registers every request path first and runs the producers of the values afterwards (`packages/protocol/src/action/server/AttributeReadResponse.ts:88-100` at `db021da0`); that `addConcrete` (`:202`) settles a failing path's status without producing a value is the reading of the Go comment (`im/read_expansion.go:133-139`) and was not repeated (unverified). Here the statuses-first order reads each concrete path once up front to learn its status and again at its place, so that its value is as current as the rest of the report (`prepare`, `im/read_expansion.go:128-171`); the Subscribe's pass that asks whether anything matches stops at its first match since review II (`bridge:TestPrimingMatchesStopsAtTheFirstMatch`). No wire difference; the cost is a second read of the server for each concrete path. Fix: a status-only check on the dispatcher, a new exported interface — the API bundle of phase 5 of review II. Found in review II, phase 4 (R2-P4-dr-im-06).
- **F-db021da0-33: GetUser and GetCredentialStatus answer the creator and last-modified fabric of every record as values.** connectedhomeip sets the two fields of GetUserResponse and GetCredentialStatusResponse only for a record created, or modified, through Matter and leaves them null otherwise (`creationSource == DlAssetSource::kMatterIM`, `src/app/clusters/door-lock-server/CodegenIntegration.cpp:570-578`, `:967-974` at `6170af84`). matter.js answers the stored values (`packages/node/src/behaviors/door-lock/DoorLockServer.ts:375-376`, `:598-599` at `db021da0`) and null for an empty slot (`:361-362`, `:587-588`). Here the two are always answered as values (`cluster/lock/users.go:275-276`, `cluster/lock/credentials.go:222-223`), also for a record a host seeded in `DoorLockConfig.Initial`; the records keep no source. Who sees it: a controller reading a host-seeded user or credential — an index, 0 once its fabric is removed (`BD-Matter-DoorLock-CreatorFabricIndexCleared`), where connectedhomeip answers null. Fix: a source per record, a change of the lock's stored shape; phase 5 of review II. Found in review II, phase 4 (the fix of owner decision 81).
- **F-db021da0-34: the session reused for a peer is the one with the highest id, not the one last heard from.** matter.js's `newestSession` takes the live session with the highest `activeTimestamp`, "the session we last heard from the peer on" (`packages/protocol/src/peer/Peer.ts:582-601`, `:596` at `db021da0`). The bridge takes the peer's live session with the highest session id (`existingPeerSession`, `bridge/case_initiator.go:177-204`, over `SessionIDsForPeer`, which orders by id, `secure/operational/manager.go:1853-1867`). Who sees it (read, not run): a node that holds two live sessions with one peer sends on the one with the higher id where matter.js sends on the one the peer last used. Fix: a last-activity accessor on the session registry; phase 5 of review II. Found in review II, phase 4 (R2-P4-dr-sub-07).
- **F-db021da0-35: a group message whose authenticated payload does not parse ends at its first candidate key.** matter.js fails a candidate whose authenticated payload does not parse and tries the next, "As in the CHIP SDK" (`packages/protocol/src/session/GroupSession.ts:435-442` at `db021da0`). `groups.Decode` returns the message of the first candidate that authenticates and that GroupKeyMap allows, without parsing its payload (`groups/decode.go:232-253`); the bridge parses it afterwards and ends the message with an error, without a GroupcastTesting result, when it does not parse (`bridge/group_messaging.go:307-310`). Who sees it (read, not run): only a message a candidate key authenticates and whose payload does not parse — matter.js goes on to the next candidate, this module does not. Fix: the payload parse inside the candidate loop; the cases of SecureSessionTest.ts's "GroupKeyMap check" that stay missing in the test register go with it. Placed in phase 6 of review II. Found in review II, phase 4 (R2-P4-nw-07b).
- **F-db021da0-36: an ongoing report counts as delivered on its acknowledgement, not on the peer's StatusResponse.** matter.js sends every ongoing report, a keep-alive included, with `suppressResponse: false` (`packages/node/src/node/server/ServerSubscription.ts:909-937` at `db021da0`), and `sendDataReport` then waits for the peer's StatusResponse (`waitForSuccess`, `packages/protocol/src/interaction/InteractionMessenger.ts:838-845`, `:221-227`). An error status ends the subscription; any other error is passed on (`StatusResponseError.accept`, `ServerSubscription.ts:938-950`) to the update's catch, which counts a failed update (`:558-603`), and the count starts again only when `#sendUpdateMessage` returns (`:553-557`). Here the update is delivered when the report's last message is acknowledged (`releaseReportCounter`, `bridge/subscribe.go:1347-1365`; `noteReportSent`, `:1187-1203`; `noteUpdateDelivered`, `:1166-1173`); the StatusResponse is read when it comes — an error status ends the subscription (`closeSubscriptionByExchange`, `:1323`) — and is not waited for. Who sees it (read from the lines, not run): a peer that acknowledges a report and never answers it is a failed update in matter.js and a delivered one here; what bounds matter.js's wait was not read (unverified). Not deliberate, and not fixed in phase 4. Fix: the update is delivered with the StatusResponse; the waiting half of the keep-alive status case goes with it (`bridge/subscribe_keepalive_status_parity_matterjs_test.go:23-28`). Placed in phase 6 of review II. Found in review II, phase 4 (the last fix group).
- **F-db021da0-38: a GroupKeyMap write of the held entries in another order is no change.** matter.js judges a write by `isDeepEqual` of the written and the held value (`#noteIfChanged`, `packages/node/src/behavior/state/managed/Datasource.ts:1476-1481` at `db021da0`), which compares two arrays property by property, so the place of an entry counts (`packages/general/src/util/DeepEqual.ts:57-84`). GroupKeyManagement compares the written list with the fabric's mappings per group (`sameGroupKeyMap`, `cluster/core/group_key_management.go:645-660`; "the order of the written entries does not count", `:576-577`): the held entries in another order answer unchanged, store nothing and advance no version. Whether matter.js holds a differently ordered list after such a write depends on how its fabric-scoped list builds the stored array on a REPLACE, which was not traced (unverified); nothing was run on either side. Who sees it: a controller that writes its fabric's GroupKeyMap entries in another order gets no new DataVersion and no report here. Fix: with that trace, either the order is kept and compared or the difference becomes a `by_design` entry. Not placed. Found in review II, phase 4 (the citation pass on owner decision 78).
- **F-db021da0-39: the bridge reports the written attribute whenever its write changed the cluster, also when another attribute moved and not the written one.** matter.js broadcasts the names of the properties a commit changed and no other: `#noteIfChanged` adds a changed name to the change list (`packages/node/src/behavior/state/managed/Datasource.ts:1483-1484` at `db021da0`) and `postCommit` hands that list to `onChange` (`:1319`). A server's verdict here is one per write: `MatterWriteChanged` answers changed when the write changed any attribute of the cluster (`contract/matter.go:204-211`), the record keeps it under the written path (`WriteChanges.record`, `endpoint/write_changes.go:47-58`), and `reportWrittenAttributes` marks every successfully written path that the record does not hold as unchanged (`bridge/receive_dispatch.go:930-946`). A write that leaves its own attribute as it was and moves another therefore marks the written one as well. Read from the lines, not run; no kit server was looked for whose write does that. Who sees it: a subscriber of the written attribute is told of it although its value did not move. Fix: the verdict names the attributes that changed, or the bridge compares the written path's value. Not placed. Found in review II, phase 4 (the citation pass on owner decision 78).
- **F-db021da0-40: a generated server's write veto runs for a write of the held value.** matter.js computes a pre-commit change only for a field whose value differs from the held one — `#computePreCommitChange` returns for an equal value (`packages/node/src/behavior/state/managed/Datasource.ts:1397-1407` at `db021da0`) — so no `$Changing` observer runs for it (from the lines, not run). `spec.Server` calls its veto (`ServerConfig.BeforeWrite`, the typed server's `OnWrite`) before it compares the written value with the held one (`MatterWriteChanged`, `cluster/spec/server.go:337-357`): the veto hears a write of the held value and may refuse it, where the `Sink` and the store are no longer reached (review II, R2-P4-fr-ver-01). The veto also carries constraint checks of some servers — the camera's zone and stream servers by the fix's note, which was not re-read (unverified) — and a veto moved behind the comparison would skip them for a held value. Who sees it: a write of the value a server already holds is refused by a host's veto here and accepted by matter.js. Fix: the veto after the comparison, the checks it carries moved to where a held value still meets them; phase 5 of review II, with the API bundle. `BD-Matter-TypedServer-WriteVeto` describes the veto. Found in review II, phase 4.
- **F-db021da0-41: a GroupKeyMap write that fails part-way has stored some of its mappings and advances no version.** GroupKeyManagement sets the written mappings one by one and then removes the dropped ones (`MatterWriteChanged`, `cluster/core/group_key_management.go:619-635`); a store error at any step answers the write with that error and no change, after the earlier steps were stored, and the version advances only after the last (`:639`). matter.js hands a commit's changes to the store as one step and rolls back on a failure — `BD-Matter-NonVolatileWrite-StoredBeforeApplied`'s reading of `Datasource.ts` and `Tx.ts`, not repeated for this row (unverified). Who sees it (read, not run): after a store fault a fabric's GroupKeyMap holds part of the written list under the DataVersion it had, so a DataVersionFilter with that version still matches. Fix: one store transaction for the write, a method of the store port; phase 5 of review II, with the store API. Found in review II, phase 4 (the survey for R2-P4-fr-ver-01).
- **F-db021da0-42: the Groupcast commands change a fabric's GroupKeyMap outside the mutex that serialises its writes.** In matter.js the Groupcast server assigns GroupKeyManagement's `groupKeyMap` state (`#setGroupKeyMapping`, `#removeGroupKeyMappings`, `packages/node/src/behaviors/groupcast/GroupcastServer.ts:777-792` at `db021da0`), a managed write that takes the datasource into its transaction as a resource (`#startWrite`, `packages/node/src/behavior/state/managed/Datasource.ts:1345-1350`), as a controller's write of the attribute does; how two transactions on one resource wait for each other is `BD-Matter-Invoke-HoldsHostReports`'s reading and was not repeated (unverified). Here a controller's GroupKeyMap write reads the held mappings, compares and stores under `writeMu` (`cluster/core/group_key_management.go:610-640`), while `setGroupKeyMapping` and `removeGroupKeyMapping`, which the Groupcast commands reach, check and store without it (`:1179-1198`). A Groupcast change of a fabric beside a GroupKeyMap write of the same fabric can fall between the write's read and its store: the write's verdict and its removals are then judged against a list that has moved, and each path checks the fabric's group count on its own. Both advance the version, so no change goes unversioned. Read from the lines, not run; whether a Groupcast path reaches the mutators from inside the group table's reload, which the write runs under `writeMu` (`syncGroups`, `:640`), was not read (unverified). Who sees it: two administrators of one fabric that change its group mappings at the same moment. Fix: the mutators under `writeMu`, once the lock order is read; phase 6 of review II. Found in review II, phase 4 (the survey for R2-P4-fr-ver-01).
- **F-db021da0-43: two NodeLabel writes to one bridged device at the same moment are each judged against their own copy of the label.** A bridged endpoint's BridgedDeviceBasicInformation server is built for every dispatch, seeded with the endpoint's label (`endpoint/materialize.go:210-231`, `NodeLabel: ep.NodeLabel()`), and the mutex that makes the comparison, the store and the host's hook one step belongs to that instance (`writeMu`, `cluster/core/bridged_device_basic_information.go:50-55`, `:494-495`). Two writes dispatched at the same moment run on two instances: both can answer changed for one label, and the host's `OnNodeLabelWrite` can hear two labels in another order than the endpoint keeps them. Read from the lines, not run. matter.js keeps one datasource per behavior and takes it into a write's transaction (`packages/node/src/behavior/state/managed/Datasource.ts:1345-1350` at `db021da0`); how its transactions wait for each other was not repeated (unverified, as for F-db021da0-42). Who sees it: two controllers that rename one bridged device at the same moment. Fix: the comparison against the endpoint's label under one lock — the endpoint's, or a server kept across dispatches; phase 6 of review II. Found in review II, phase 4 (R2-P4-fr-ver-01, second round).
- **F-db021da0-44: a hand-off LevelControl whose port is also the endpoint's source notifier is told of a host change on two subscriptions whose order the host decides.** The bridge subscribes the endpoint's source notifier for the paths of the source's own cluster (`bridge/subscribe.go:2091-2120`), and a LevelControl server without the transition engine subscribes to its port's notifier itself once it has a change listener (`reconcileHost`, `cluster/levelcontrol/levelcontrol_server.go:611-635`), handing a notification to its forwarded callbacks before its listeners (`onHost`, `:586-607`). When port and source are one notifier, the bridge's callback and the server's are two callbacks of the host's notifier, in the order the host calls them. By the fix's reading the server's change, when it comes first, advances the version and takes the value into the notification's memory (`quieterChanged`, `bridge/subscribe.go:2472`; `settlerFor`, `:2495`), so the notification after it finds nothing moved and reports nothing; with the bridge's half of the fix alone that showed as an advance without a report in 7 of 60 runs of `bridge:TestHostChangeAdvancesOnceOnAHandOffServer`, in a wiring where the server forwards the notifier. For the wiring of this row nothing was run, and the reading was not repeated (unverified). matter.js has one datasource per behavior, whose commit advances and broadcasts together (`packages/node/src/behavior/state/managed/Datasource.ts:1456-1457`, `:1319` at `db021da0`). Who would see it: a subscriber of CurrentLevel on such a device misses a host change until the next one. Fix: one subscription per host notifier and endpoint, or the settlement told which callback comes last; phase 6 of review II. Found in review II, phase 4 (R2-P4-fr-ver-01, second round).
- **F-db021da0-45: a change a host makes from inside a controller's write is held only when a change notification tells the bridge of it.** While a controller's write is applied, the bridge holds the notifications of the written endpoints' sources and of the servers that forward a port's notifier, and settles them against the write's record (`writeHoldTable`, `bridge/write_hold.go:33-41`; `wireMeasurementNotifier`, `bridge/subscribe.go:2610`, `:2622`; R2-P4-fr-im-01). A root server's change notifier (`wireRootNotifiers`, `:2314-2348`) and a change a server names itself with a host origin (`wireAttributeNotifiers`, `:2380-2413`; `reportServerChange`, `:2423-2446`) are held for a command (`invokeCommits.hold`) and not for a write. A host that changes an attribute of the written cluster from inside the write — a setter or Sink that sets another attribute of its server — therefore advances the version a second time and is reported on its own, where matter.js commits what a write's transaction changed once (`packages/node/src/behavior/state/managed/Datasource.ts:1446-1457` at `db021da0`; that a reactor's change joins the write's transaction there was not read: unverified). Read from the lines, not run; no kit server or host was looked for that does it. Who would see it: a subscriber gets two DataVersions for one write. Fix: the write hold over these two wirings as well. Not placed. Found in review II, phase 4 (R2-P4-fr-im-01, "not held").
- **F-db021da0-46: the hand-off advance of LevelControl and ModeSelect moves a version no controller reads.** A note, not a wire gap. The dispatcher serves a server's own version only on the root and the Aggregator (`ownDataVersion`, `endpoint/dispatcher.go:39-45`); on every other endpoint the served version is the endpoint's (`clusterDataVersionFor`, `:55-63`), and `clusterDataVersion` (`:24-29`) has no caller outside tests. LevelControl and ModeSelect are mounted on neither, so the rule `53c68d85` gave their own trackers — a command handed to the host's port advances when the value it leaves differs from the one the last host notification read (R2-P4-rr-im-01) — and the tests that pin it hold `MatterDataVersion()`, which the wire does not carry. On the wire the bridge's command commit advances the endpoint's version once for the same case (`BD-Matter-Invoke-HoldsHostReports`). Fix: none for the wire; the tests say which version they hold, or the trackers of servers that are never served by them go. Not placed. Found in review II, phase 4 (R2-P4-fr-im-03).
- **F-db021da0-47: the node's persister writes a held value to the store again.** matter.js hands the store only the fields a commit noted as changed (`#computePostCommitChanges`, `packages/node/src/behavior/state/managed/Datasource.ts:1446-1452` at `db021da0`). The node stores a controller's write of a nonvolatile attribute before the server takes it (`writeStored`, `endpoint/dispatcher.go:1076-1085`; `BD-Matter-NonVolatileWrite-StoredBeforeApplied`), and `PersistWrite` sets the setting without comparing it with the stored one (`node/nonvolatile.go:253-256`), so a write the server then answers unchanged has written the held value to the settings store once more. Read from the lines, not run. Who sees it: no controller; the host's store takes a write for a value that did not change. Fix: the persister compares before it writes, or runs after the verdict. Not placed. Found in review II, phase 4 (closing F-db021da0-2).
- **F-db021da0-48: the hand-written list readers accept an array element with a tag.** matter.js's array schema refuses an array whose element carries a tag (`packages/types/src/tlv/TlvArray.ts:77-78` at `db021da0`, "Array element tags should be anonymous."), and connectedhomeip's reader refuses it for every array it reads (`src/lib/core/TLVReader.cpp:873-875` at `6170af84`). Here the tree reader behind the generated decoders refuses it since review II, phase 4 (`spec.ReadContainer`, `cluster/spec/codec.go`, `ErrArrayElementTag`), but `tlv.Decoder.Next` itself reports the element with its tag, and a reader written by hand over the decoder does not look at it: `decodeACLList` takes a tagged structure as an entry (`bridge/attribute_value_reader.go:177-195`). Not deliberate; which hand-written readers of the bridge and of `cluster/wire` read an array this way was not listed. Placed in phase 6 (the full pass); the one place to close it is the decoder, which knows the container it is in.
## Tier 3 — LOW


### T3-P35-1 — a write value of the wrong Go type gets three different statuses (phase 35, 2026-11-04)

- **Where:** FAILURE from `cluster/core/access_control.go:674`, `bridged_device_basic_information.go:459`, `general_commissioning.go:403`, `network_commissioning.go:234`; CONSTRAINT_ERROR from BasicInformation and Binding through the convention `cluster/spec/write.go:126-128`; INVALID_ACTION from `cluster/core/identify.go:453-457`.
- **matter.js at 862ad75f:** the node's write path validates through `ValueValidator`, whose `DatatypeError` extends `ValidateError` with the default `Status.InvalidDataType` (`packages/protocol/src/action/errors.ts:55-72`); the older TLV-schema path answers `ValidationDatatypeMismatchError` → `Status.InvalidAction` (`packages/types/src/common/ValidationError.ts:31-38`). Neither is FAILURE.
- **Unverified:** whether a wire write can reach a Go server with a value of a type the attribute does not hold — the bridge decodes by the attribute's schema, so these may be unit-level paths only. Phase 35's status assertions pin today's answers (`TestBasicInfo_WriteNodeLabelWrongType` and siblings) so a change is visible; the rule — one status, matter.js's INVALID_DATA_TYPE on the node path — is a decision for the next write-path wave.
- **Measured 2026-11-05 (follow-up wave, attempt reverted):** the "wrong Go type" convention of `cluster/spec/write.go:122-128` also carries a wire case — `bridge.undecodedValue` (`bridge/attribute_value_reader.go:108-114`), the value the attribute's decoder refused (a label over its max, a string cut at an IS1), which the server's write check answers CONSTRAINT_ERROR because it is of no attribute type; matter.js answers a value its TlvSchema rejects with CONSTRAINT_ERROR too (`AttributeWriteResponse.ts` #decodeWithSchema, cited there), and the ported cases `bridge:TestNodeLabelWriteBoundsTheLengthBeforeIS1` (TlvString.ts:57-69), `bridge/attribute_value_reader_userlabel_test.go:87` and `cluster/spec:TestValueValidatorParityMatterJS/still_rejects_a_wrong-datatype_value_on_a_client_peer_write` (ValueValidatorTest.ts:478-480) assert CONSTRAINT_ERROR. Switching the convention to INVALID_DATA_TYPE therefore breaks three matter.js-asserted cases and 20 module tests; the write-path wave has to separate the undecoded wire value (CONSTRAINT_ERROR, as matter.js) from a host-side Go value of the wrong type before the statuses can be unified, and read what matter.js's node path answers a TLV element of another type on the wire (the decoder's error, not the validator's) at the pin.
- **Review II, phase 4 (2026-11-14):** since owner decision 64 a controller's written value is held to the attribute's type, nullability and constraint by the cluster's definition in the dispatcher, before any server runs, and refused there with CONSTRAINT_ERROR (`spec.CheckWritten`, `cluster/spec/written.go:100-114`; `writtenHeld`, `endpoint/dispatcher.go:521`); a value the definition did not decode is not judged there. For a cluster with a definition the servers' own statuses above are therefore a host-side matter; that no wire write reaches them any more was not measured (unverified). Of the lines under "Where", `bridged_device_basic_information.go:459` and `cluster/spec/write.go:126-128` hold; the Breadcrumb refusal is `general_commissioning.go:529` and InterfaceEnabled's `network_commissioning.go:252` now; `access_control.go:674` and `identify.go:453-457` are other lines today and were not located again.

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
None changes a shipped server's behaviour today; each entry leaves the
table, and this list, when the snapshot is corrected.

### Device type validation — open departures

What `endpoint.ValidateDeviceTypes`
([ADR 0016](../../docs/adr/0016-device-type-validation.md)) finds in what
the module builds itself (`endpoint/devicetype_module_test.go` pins each).

## Verified sound (no action)

- **A subscription is persisted after its SubscribeResponse, so a Stop in the same instant loses it.** `bridge/subscribe.go:1062-1071` records the subscription once the response is sent; matter.js's `SubscriptionsServer.#addSubscription` fires on `subscriptionAdded`, which the InteractionServer emits after the SubscribeResponse (`SubscriptionsServer.ts:54`, `:133-140`). Same timing; a test waits until it is stored (report S, question 7).

TLV element codes/widths, bool/null/float shapes, string/octet length forms, tag-encode thresholds; message/protocol header field order and presence rules; Sigma KDF salts/infos/nonces, TBE/TBS tag order, destinationId HMAC, RCAC/chain validation, AddNOC rollback + fail-safe-expiry revert, RemoveFabric teardown; SPAKE2 generators/point validation/Ke/transcript, PBKDF bounds; MRP max-transmissions = 5, nonce layout, standalone-ack delay, session-id collision avoidance; timed-invoke gate, keepSubscriptions purge, sendInterval formula, subscription death after 3 failures; EP0 cluster set, full-family PartsList, ServerList, empty-ACL fail-closed, CAT matching; OnOff toggle semantics, level scaling, thermostat limit-clamp + deadband, measurement null semantics, illuminance formula, cover inversion; mDNS operational instance-name + subtypes + TXT formats, multi-fabric boot re-announce, AddNOC-time publish, shutdown withdraw.
