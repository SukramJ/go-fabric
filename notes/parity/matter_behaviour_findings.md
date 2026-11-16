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
  `CHIP_DEVICE_CONFIG_SUPPORTS_CONCURRENT_CONNECTION`, which
  `src/include/platform/CHIPDeviceConfig.h:337` defines to 1). Nothing is
  sent, at the expiry or afterwards: `HandleConnectNetwork` adds nothing
  (`NetworkCommissioningCluster.cpp:649`, `:682`), so finalizing returns
  `CHIP_ERROR_INCORRECT_STATE` (`src/app/CommandHandlerImpl.cpp:1000`), and
  the response sender, with nothing to send, logs "Failed to send
  InvokeResponseMessage" and sends no Failure StatusResponse either
  (`src/app/CommandResponseSender.cpp:82-103`, `:88-100`, under
  `TODO(#30453)`); the client's invoke runs into its own timeout (read end
  to end, not executed; an invoke batch whose other commands added a
  response, and a build without concurrent connection, were not covered).
  Since `3b77e4e8` this module's late result touches neither the reverted
  networks nor the Breadcrumb, and since `7c5c2c75` the Thread server's
  neither: both servers match connectedhomeip in state. The answer still
  differs: the command answers a ConnectNetworkResponse with the driver's
  status, after the expiry (`cluster/core/network_commissioning_wifi.go`,
  `connect`; `network_commissioning_thread.go`, `OnFailSafeExpired`).
  Answering nothing needs a way for a server to end the interaction
  without a response, an `im` move (review II, phase 6a, R2-P6-fsm-1001). Fix: decide
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
- **F-db021da0-20: whether the request check of DoorLock's ten validated commands refuses exactly what matter.js's validation refuses was not compared.** matter.js validates every command that has a schema against the model (`packages/node/src/node/integration/ProtocolService.ts:351-368`, `:662-671` at `db021da0`); a failure goes to the command's `onValidationError` where its Supervision config has one (`:659-660`, `:673-674`) and is otherwise logged while the command runs (`:675-677`), and the one `onValidationError` at the pin is DoorLock's, which answers INVALID_COMMAND for setUser, getUser, clearUser, setCredential and the six schedule commands (`packages/node/src/behaviors/door-lock/DoorLockServer.ts:1494-1511`). Since review II, phase 5b (owner decision 113, `13f1a70a`) the definition's request check (`spec.Instance.CheckRequest`: types, constraints, enum values, field conformance) holds every request before any handler, refuses DoorLock's ten with INVALID_COMMAND (`Check.Refusal`, `cluster/spec/doorlock/definition_gen.go`) and logs the failures of other commands while they run, as matter.js; DoorLock's other commands are not checked: matter.js skips the TLV-level validation for a cluster with a configured command (`ProtocolService.ts:343-348`) and its model validation only logs for them (`:662-679`). Whether matter.js's validator refuses exactly what `CheckRequest` refuses for the ten — an enum value outside a member-list constraint, reserved bitmap bits — was not compared (unverified). Fix: a parity case per kind of refusal, or a `by_design` line where they differ. Found in review II, phase 4 (the fix of owner decision 64); narrowed in phase 5b.
- **F-db021da0-21: the thermostat does not follow the temperature or the occupancy of a sibling endpoint.** matter.js couples its Thermostat to two sibling clusters, once, when the node goes online (`reactTo(node.lifecycle.online, #nodeOnline)`, `packages/node/src/behaviors/thermostat/ThermostatServer.ts:168`, `:177-182` at `db021da0`; later structure changes are not followed, the TODO at `:178-179`): the endpoint named by the state field `localIndoorTemperatureMeasurementEndpoint` (`:467`, declared `:1618-1622`) feeds LocalTemperature from its TemperatureMeasurement `measuredValue` (`:480-484`), and the one named by `localOccupancyMeasurementEndpoint` (`:539`, declared `:1631-1635`) feeds Occupancy from its OccupancySensing `occupancy` (`:552-553`, `:577-579`); each is taken over the external state unless a RemoteSensing bit selects the external source (`:455`, `:468`, `:538`, `:540`). Since review II, phase 5b the Go server has the external temperature input with its calibration and the LocalTemperatureNotExposed rule — `ThermostatServer.SetMeasuredTemperature`, the mirror of `externalMeasuredIndoorTemperature` (`:497-501` → `:511-527`) — so the calibration this row first named is applied; occupancy comes from the host (`SetOccupied`). Neither sibling coupling is built: a host that composes a thermostat with such a sensor wires the two itself. Fix: the couplings, from an endpoint the host names. Found in review II, phase 4 (R2-P4-appclim-05); narrowed in phase 5b, when the row of the `862ad75f` section on the OccupancySensing coupling joined it.
- **F-db021da0-28: BridgedDeviceBasicInformation serves NodeLabel and SerialNumber whatever the host set.** Both are optional in the model (`packages/model/src/standard/elements/bridged-device-basic-information.element.ts:30`, `:40` at `db021da0`); that matter.js lists an optional attribute only when its state holds a value was the fix's reading of `ValidatedElements.ts:247-279` and was not repeated (unverified). Here NodeLabel is always served, empty when the host gave none (`cluster/core/bridged_device_basic_information.go:245`), and SerialNumber is filled from the UniqueID when the host gave none (`:179-181`) — the pair matter.js warns about, "uniqueId and serialNumber shall not be the same" (`basic-information-validators.ts:42-44`). A VendorID of 0 or above 0xFFF4 and a ProductID of 0 are warnings here (`:281-301`) where matter.js's validator, which the bridged server runs (`BridgedDeviceBasicInformationServer.ts:45`), throws (`basic-information-validators.ts:32-40`). The code gives Apple's HAP mapper as the reason for SerialNumber, as the root's `BD-Matter-BasicInfo-SerialNumberAlwaysServed`; for NodeLabel it gives none. Not decided: follow matter.js, or a `by_design` entry with the owner's reason. Found in review II, phase 4 (R2-P4-coresys-05).
- **F-db021da0-35: a group message whose authenticated payload does not parse ends at its first candidate key.** matter.js fails a candidate whose authenticated payload does not parse and tries the next, "As in the CHIP SDK" (`packages/protocol/src/session/GroupSession.ts:435-442` at `db021da0`). `groups.Decode` returns the message of the first candidate that authenticates and that GroupKeyMap allows, without parsing its payload (`groups/decode.go:232-253`); the bridge parses it afterwards and ends the message with an error, without a GroupcastTesting result, when it does not parse (`bridge/group_messaging.go:307-310`). Who sees it (read, not run): only a message a candidate key authenticates and whose payload does not parse — matter.js goes on to the next candidate, this module does not. Fix: the payload parse inside the candidate loop; the cases of SecureSessionTest.ts's "GroupKeyMap check" that stay missing in the test register go with it. Placed in phase 6 of review II. Found in review II, phase 4 (R2-P4-nw-07b).
- **F-db021da0-38: a GroupKeyMap write of the held entries in another order is no change.** matter.js judges a write by `isDeepEqual` of the written and the held value (`#noteIfChanged`, `packages/node/src/behavior/state/managed/Datasource.ts:1476-1481` at `db021da0`), which compares two arrays property by property, so the place of an entry counts (`packages/general/src/util/DeepEqual.ts:57-84`). GroupKeyManagement compares the written list with the fabric's mappings per group (`sameGroupKeyMap`, `cluster/core/group_key_management.go:645-660`; "the order of the written entries does not count", `:576-577`): the held entries in another order answer unchanged, store nothing and advance no version. Whether matter.js holds a differently ordered list after such a write depends on how its fabric-scoped list builds the stored array on a REPLACE, which was not traced (unverified); nothing was run on either side. Who sees it: a controller that writes its fabric's GroupKeyMap entries in another order gets no new DataVersion and no report here. Fix: with that trace, either the order is kept and compared or the difference becomes a `by_design` entry. Not placed. Found in review II, phase 4 (the citation pass on owner decision 78).
- **F-db021da0-42: the Groupcast commands change a fabric's GroupKeyMap outside the mutex that serialises its writes.** In matter.js the Groupcast server assigns GroupKeyManagement's `groupKeyMap` state (`#setGroupKeyMapping`, `#removeGroupKeyMappings`, `packages/node/src/behaviors/groupcast/GroupcastServer.ts:777-792` at `db021da0`), a managed write that takes the datasource into its transaction as a resource (`#startWrite`, `packages/node/src/behavior/state/managed/Datasource.ts:1345-1350`), as a controller's write of the attribute does; how two transactions on one resource wait for each other is `BD-Matter-Invoke-HoldsHostReports`'s reading and was not repeated (unverified). Here a controller's GroupKeyMap write reads the held mappings, compares and stores under `writeMu` (`cluster/core/group_key_management.go:610-640`), while `setGroupKeyMapping` and `removeGroupKeyMapping`, which the Groupcast commands reach, check and store without it (`:1179-1198`). A Groupcast change of a fabric beside a GroupKeyMap write of the same fabric can fall between the write's read and its store: the write's verdict and its removals are then judged against a list that has moved, and each path checks the fabric's group count on its own. Both advance the version, so no change goes unversioned. Read from the lines, not run; whether a Groupcast path reaches the mutators from inside the group table's reload, which the write runs under `writeMu` (`syncGroups`, `:640`), was not read (unverified). Who sees it: two administrators of one fabric that change its group mappings at the same moment. Fix: the mutators under `writeMu`, once the lock order is read; phase 6 of review II. Found in review II, phase 4 (the survey for R2-P4-fr-ver-01).
- **F-db021da0-43: two NodeLabel writes to one bridged device at the same moment are each judged against their own copy of the label.** A bridged endpoint's BridgedDeviceBasicInformation server is built for every dispatch, seeded with the endpoint's label (`endpoint/materialize.go:210-231`, `NodeLabel: ep.NodeLabel()`), and the mutex that makes the comparison, the store and the host's hook one step belongs to that instance (`writeMu`, `cluster/core/bridged_device_basic_information.go:50-55`, `:494-495`). Two writes dispatched at the same moment run on two instances: both can answer changed for one label, and the host's `OnNodeLabelWrite` can hear two labels in another order than the endpoint keeps them. Read from the lines, not run. matter.js keeps one datasource per behavior and takes it into a write's transaction (`packages/node/src/behavior/state/managed/Datasource.ts:1345-1350` at `db021da0`); how its transactions wait for each other was not repeated (unverified, as for F-db021da0-42). Who sees it: two controllers that rename one bridged device at the same moment. Fix: the comparison against the endpoint's label under one lock — the endpoint's, or a server kept across dispatches; phase 6 of review II. Found in review II, phase 4 (R2-P4-fr-ver-01, second round).
- **F-db021da0-44: a hand-off LevelControl whose port is also the endpoint's source notifier is told of a host change on two subscriptions whose order the host decides.** The bridge subscribes the endpoint's source notifier for the paths of the source's own cluster (`bridge/subscribe.go:2091-2120`), and a LevelControl server without the transition engine subscribes to its port's notifier itself once it has a change listener (`reconcileHost`, `cluster/levelcontrol/levelcontrol_server.go:611-635`), handing a notification to its forwarded callbacks before its listeners (`onHost`, `:586-607`). When port and source are one notifier, the bridge's callback and the server's are two callbacks of the host's notifier, in the order the host calls them. By the fix's reading the server's change, when it comes first, advances the version and takes the value into the notification's memory (`quieterChanged`, `bridge/subscribe.go:2472`; `settlerFor`, `:2495`), so the notification after it finds nothing moved and reports nothing; with the bridge's half of the fix alone that showed as an advance without a report in 7 of 60 runs of `bridge:TestHostChangeAdvancesOnceOnAHandOffServer`, in a wiring where the server forwards the notifier. For the wiring of this row nothing was run, and the reading was not repeated (unverified). matter.js has one datasource per behavior, whose commit advances and broadcasts together (`packages/node/src/behavior/state/managed/Datasource.ts:1456-1457`, `:1319` at `db021da0`). Who would see it: a subscriber of CurrentLevel on such a device misses a host change until the next one. Fix: one subscription per host notifier and endpoint, or the settlement told which callback comes last; phase 6 of review II. Found in review II, phase 4 (R2-P4-fr-ver-01, second round).
- **F-db021da0-46: the hand-off advance of LevelControl and ModeSelect moves a version no controller reads.** A note, not a wire gap. The dispatcher serves a server's own version only on the root and the Aggregator (`ownDataVersion`, `endpoint/dispatcher.go:39-45`); on every other endpoint the served version is the endpoint's (`clusterDataVersionFor`, `:55-63`), and `clusterDataVersion` (`:24-29`) has no caller outside tests. LevelControl and ModeSelect are mounted on neither, so the rule `53c68d85` gave their own trackers — a command handed to the host's port advances when the value it leaves differs from the one the last host notification read (R2-P4-rr-im-01) — and the tests that pin it hold `MatterDataVersion()`, which the wire does not carry. On the wire the bridge's command commit advances the endpoint's version once for the same case (`BD-Matter-Invoke-HoldsHostReports`). Fix: none for the wire; the tests say which version they hold, or the trackers of servers that are never served by them go. Not placed. Found in review II, phase 4 (R2-P4-fr-im-03). Since review II, phase 5b (`c1028573`) `spec.Server`'s `ServerConfig.DataVersion` and `MatterDataVersion` state that the wire carries a server's own version on the root endpoint and the Aggregator only; LevelControl's and ModeSelect's own trackers and the tests that hold their `MatterDataVersion()` say nothing of it yet (`cluster/levelcontrol/levelcontrol_server.go`, `cluster/modeselect/modeselect_server.go`).
- **F-db021da0-47: the node's persister stores a held value the store does not hold yet.** matter.js hands the store only the fields a commit noted as changed against the held value (`#computePostCommitChanges`, `Datasource.ts:1446-1452`; `#noteIfChanged`, `:1476-1481` at `db021da0`). `PersistWrite` compares the written value with the stored one, not with the one the server holds (`node/nonvolatile.go:252-258`), so a write of the value a server holds from its declaration, under a key never stored, is written to the settings store once. Read from the lines, not run. Who sees it: no controller; the host's store takes one write per key for a value that did not change. Narrowed 2026-11-16 (review II, phase 5b, wave F, `1976e84a`): a write of the value the store already holds stores nothing (`node:TestNonVolatileWriteOfTheStoredValueStoresNothing`).
- **F-db021da0-48: the hand-written list readers accept an array element with a tag.** matter.js's array schema refuses an array whose element carries a tag (`packages/types/src/tlv/TlvArray.ts:77-78` at `db021da0`, "Array element tags should be anonymous."), and connectedhomeip's reader refuses it for every array it reads (`src/lib/core/TLVReader.cpp:873-875` at `6170af84`). Here the tree reader behind the generated decoders refuses it since review II, phase 4 (`spec.ReadContainer`, `cluster/spec/codec.go`, `ErrArrayElementTag`), but `tlv.Decoder.Next` itself reports the element with its tag, and a reader written by hand over the decoder does not look at it: `decodeACLList` takes a tagged structure as an entry (`bridge/attribute_value_reader.go:177-195`). Not deliberate; which hand-written readers of the bridge and of `cluster/wire` read an array this way was not listed. Placed in phase 6 (the full pass); the one place to close it is the decoder, which knows the container it is in.
- **F-db021da0-49: a certificate's vendor and product id are read from a string-typed DN value only.** matter.js reads the attribute value's raw bytes as text whatever its DER tag (`packages/protocol/src/certificate/kinds/Certificate.ts:326` at `db021da0`). `internal/attestids` takes the value through `pkix` as a Go string (`Value.(string)`), and `encoding/asn1` decodes a T61String and a BMPString to a string too; so, inferred and not measured, a BMPString `FFF1` is accepted here and malformed in matter.js, and an OCTET STRING holding `FFF1` is malformed here and accepted in matter.js. "A value that is not a string counts as malformed" is this module's own rule. Fix: read the value's bytes as matter.js does, with a parity case per tag. Found in review II, phase 5b (the reference read of wave F); older than the wave.
- **F-db021da0-50: the re-establishment of former subscriptions connects over UDP only.** matter.js prefers TCP for it when the peer's last known operational address was TCP (`packages/node/src/behavior/system/subscriptions/SubscriptionsServer.ts:298-309` at `db021da0`). Here the ADR 0008 initiator resolves and handshakes over UDP (`connectPeer`, `runCASEInitiator`, `bridge/case_initiator.go`). Not deliberate: the order of the steps (ADR 0032, Shape 1). Fix: the re-establishment on the one session path; the single session layer ADR 0032 names as the direction closes it. Found in review II, phase 5b (wave B).
- **F-db021da0-51: a request on a session a node's client stack opened is served with the bridge's gates, not with the rest of the bridge's server.** The node serves its endpoint tree there with the bridge's dispatcher, ACL, privilege, existence, timed and Large Message gates and the same request facts (`clientServerConfig`, `node/client.go`; review II, wave B), but a write there runs in a fresh `im.WriteTransaction` of its own, not under the bridge's write holds, so it is not answered BUSY for an attribute a chunked write on a bridge session holds; a chunked write on such a session is applied chunk by chunk as separate writes (the server's exchange ends after the first chunk, `MoreChunkedMessages` is not read), so a chunked ACL replace leaves the list at its first chunk; none of the bridge's per-session bounds (chunk waits, chunked writes per session) applies, and a read's whole report is built before its first chunk is sent; a SubscribeRequest is answered INVALID_ACTION, a read reports no events, an invoke's before-and-after snapshot of the invoked clusters does not run (a change reaches the subscriptions through the servers' own notifications only), and a CommissioningComplete there does not close the PASE sessions as the bridge does after it (`closePASESessionsOnCommissioningComplete`). The session is named to the clusters by its identity (`core.WithInvokeSession`), never by a bridge id. Not deliberate: the order of the steps (ADR 0032, Shape 1). Fix: the single session layer, or earlier the bridge's write holds and bounds handed to the client stack's server. Found in review II, phase 5b (wave B).
- **F-db021da0-52: a request whose decode fails as a whole is answered differently by the two stacks.** For a reader error that carries no status the bridge answers with the error's status, FAILURE by default, and honours SuppressResponse (`bridge/receive.go`); the client stack's server answers INVALID_ACTION always and sends the status whatever SuppressResponse says (`client/interaction/server.go`). Both refuse the request. Which one matter.js answers was not read (unverified). Fix: one answer for both stacks, matter.js's once read. Found in review II, phase 5b (the reference read of wave B).
- **F-db021da0-53: a pairing hint with any of bits 16 to 22 set is no hint at all to a commissioner built on connectedhomeip.** matter.js models 23 hint bits, the last `powerCycleUntilIndication`, bit 22 (`packages/protocol/src/advertisement/PairingHintBitmap.ts:145` at `db021da0`). connectedhomeip reads PH with `MakeU16FromAsciiDecimal` (`src/lib/dnssd/TxtFields.cpp:195-198` at `6170af84`), which returns 0 for a value that does not fit 16 bits (`:104-108`), and stores that (`:274-275`): a PH above 65535 is read as 0, the whole hint. Whether another connectedhomeip path parses PH was not read (unverified). The module advertises the bits the host sets, as matter.js, and its docs state the consequence (`mdns.PairingHints`, `node.Commissioning.PairingHints`). Who sees it: a chip-based commissioner shown a device whose host set a hint of bit 16 or above, which sees no hint. Fix: none in this module while the two references differ; a host that wants every commissioner to see its hint keeps to bits 0 to 15. Found in review II, phase 5b (the reference read of wave A2).
- **F-db021da0-54: matter.js's set of hints that need a PairingInstruction leaves out bits 20 to 22.** matter.js's bitmap comments say the PairingInstruction SHALL carry the value for bits 20, 21 and 22 (`packages/protocol/src/advertisement/PairingHintBitmap.ts:128-145` at `db021da0`), but its instruction set names eleven bits without them (`PAIRING_HINTS_REQUIRING_INSTRUCTION`, `packages/protocol/src/mdns/MdnsConsts.ts:20-32`). The module mirrors the set (`mdns.PairingHints`): bit 20 with a PairingInstruction is refused, as in matter.js. A gap of matter.js's, mirrored; the row closes when matter.js's set changes, and the mirror follows. Found in review II, phase 5b (the reference read of wave A2).
- **F-db021da0-55: a manufacturer-specific fabric-scoped list on a cluster of the schema snapshot is not scoped by the dispatcher.** For a cluster of the snapshot the generated table is the whole fabric scope (`fabricScopeFor`, `endpoint/dispatcher.go`); a manufacturer-specific attribute a server adds to such a cluster cannot be declared, since `contract.FabricScopeDeclarer` is not asked, and the dispatcher applies no scope to it, as `contract/fabric_scope.go` states. No module server is affected. Fix: ask the declarer for an attribute id in the manufacturer range that the table does not hold, or refuse such an attribute at assembly. Found in review II, phase 5b (wave A).
- **F-db021da0-56: a host's change during a later attribute data of a written cluster gets no advance of its own.** matter.js commits one transaction per attribute data (`packages/protocol/src/action/server/AttributeWriteResponse.ts:433-435` at `db021da0`), and each advances the version once when it noted a change (`packages/node/src/behavior/state/managed/Datasource.ts:1456-1457`); a change in another transaction is another commit and another advance. The bridge's write hold spans the message and the endpoint, and a held path gets no advance of its own when any write of the message changed its cluster (`settleWrite`, `bridge/write_hold.go`): for a WriteRequest `[X.a (changes), X.c (unchanged)]` with a host change of `X.b` during the second, matter.js advances twice and the module once, and the same rule merges a host change made beside the write into the write's advance. Read from the lines, not run; whether a Read can be served between the dispatcher's advance and the settle was not read (unverified). A change-only notifier's mark before the advance is F-db021da0-59. Fix: the hold per attribute data. Found in review II, phase 5b (the reference read of wave C).
- **F-db021da0-57: WindowCovering and FanControl answer one verdict for the cluster, not the attributes their write changed.** matter.js broadcasts the names its commit changed (`packages/node/src/behavior/state/managed/Datasource.ts:1476-1484`, `:1319` at `db021da0`), and since review II, phase 5b a server names them here through `contract.ChangeNamingWriter`. `cluster/cover` already computes the attributes a write changed and `cluster/fan` moves PercentSetting and SpeedSetting with FanMode, but both answer one verdict for the cluster (`MatterWriteChanged`), recorded under the written attribute, so the written attribute is marked when only a sibling moved. No kit write was found that changes a sibling and leaves the written attribute (the wave's read, not a search of every host). Fix: both servers onto `ChangeNamingWriter`. Found in review II, phase 5b (wave C).
- **F-db021da0-58: `fabrictest:TestControllerRecoversFromASilentRestart` failed once under load.** Test quality, not a behaviour gap. Its subscription case failed once ("session … not dropped") in a guard run beside six agents and passed 35 times on a rerun under `-race`. Fix: none known; the row records the failure until it recurs or its cause is found. Found in review II, phase 5b (the guards at `1a3b760f`).
- **F-db021da0-59: a controller's write to a server that reports its changes through its change notifier alone is marked before the dispatcher advances the version.** matter.js advances a datasource's version before it broadcasts the commit's change list (`packages/node/src/behavior/state/managed/Datasource.ts:1456-1457`, `:1319` at `db021da0`). Since review II, phase 5b a server with a report view has a change it names as the controller's write held until the write's report marks it after the advance (`reportServerChange`, `bridge/subscribe.go`; `settleWrite`, `bridge/write_hold.go`; `bridge:TestWrittenAttributeIsNotReportedBeforeTheWriteAdvances`). A server without a report view is wired to advance on its own (`wireAttributeNotifiers`), and a change it names as the controller's write is marked at once and the host hook runs, before the dispatcher advances the endpoint's version after the server returns (`endpoint/dispatcher.go`). Servers that name the controller's write from inside `MatterWrite` and have no report view: Thermostat (`cluster/thermo/thermostat_server.go`), WindowCovering (`cluster/cover/windowcovering_server.go`) and OccupancySensing's HoldTime (`cluster/sensing/occupancy.go`). A report built in that window carries the new value with the cluster's old version, the next one the same value with the new version. Read, not measured; whether a report is built inside the window in production was not read (unverified). Distinct from F-db021da0-56, where a held host change gets no advance of its own. Fix: hold a controller-write change of these servers as a report view's is held, or give them a report view. Found in review II, phase 5b (the reference read of wave C).
- **N4-d4wifi-1: ConnectNetwork on a Wi-Fi interface whose InterfaceEnabled is false joins and leaves the attribute false.** That also keeps the rejoin of the committed network disarmed (`cluster/core/network_commissioning_wifi.go` setInterfaceEnabledLocked / armRejoinLocked). connectedhomeip's cluster reads InterfaceEnabled in neither ScanNetworks nor ConnectNetwork (`src/app/clusters/network-commissioning/NetworkCommissioningCluster.cpp:333-367`, `:635-683`; the attribute only at `:1084-1085`, `:1120-1132` at `6170af84`), and the one driver that models it re-enables the interface before it connects (`src/platform/OpenThread/GenericNetworkCommissioningThreadDriver.cpp:209-213`). Open: whether the Wi-Fi server's ConnectNetwork sets InterfaceEnabled true as that driver does. `wifi/wpasupplicant` does not implement the switch. matter.js's server was not read for it. Found in review II, phase 4; recorded in phase 6a.
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

- **GroupcastTesting keeps one FabricUnderTest for the node.** Any fabric's command replaces or clears it, and removing the fabric under test does not end the test before its timer — as matter.js (`packages/node/src/behaviors/groupcast/GroupcastServer.ts:404-430`, `:522-525` at `db021da0`). Mirrored; no `by_design` entry; clearing on removal would be ahead of matter.js (review II, phase 6a, R2-P6-fsm-1018).
- **The BTP idle timer runs on the central only.** A peripheral session whose central completed the handshake and then sends nothing has no idle timer (`transport/btp/session.go`), as in matter.js, whose "Central Device Idle Timer" (`packages/protocol/src/ble/BtpSessionHandler.ts:48` at `db021da0`) is restarted only for the central role (`:495-500`). A silent central is bounded by `bridge/ble.go` `watchBLEPASE` until a secure session exists, then by the commissioning window and its fail-safe (review II, phase 6a, R2-P6-fsm-0212).

- **A subscription is persisted after its SubscribeResponse, so a Stop in the same instant loses it.** `bridge/subscribe.go:1062-1071` records the subscription once the response is sent; matter.js's `SubscriptionsServer.#addSubscription` fires on `subscriptionAdded`, which the InteractionServer emits after the SubscribeResponse (`SubscriptionsServer.ts:54`, `:133-140`). Same timing; a test waits until it is stored (report S, question 7).

TLV element codes/widths, bool/null/float shapes, string/octet length forms, tag-encode thresholds; message/protocol header field order and presence rules; Sigma KDF salts/infos/nonces, TBE/TBS tag order, destinationId HMAC, RCAC/chain validation, AddNOC rollback + fail-safe-expiry revert, RemoveFabric teardown; SPAKE2 generators/point validation/Ke/transcript, PBKDF bounds; MRP max-transmissions = 5, nonce layout, standalone-ack delay, session-id collision avoidance; timed-invoke gate, keepSubscriptions purge, sendInterval formula, subscription death after 3 failures; EP0 cluster set, full-family PartsList, ServerList, empty-ACL fail-closed, CAT matching; OnOff toggle semantics, level scaling, thermostat limit-clamp + deadband, measurement null semantics, illuminance formula, cover inversion; mDNS operational instance-name + subtypes + TXT formats, multi-fabric boot re-announce, AddNOC-time publish, shutdown withdraw.
