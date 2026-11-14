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
- **A request field outside its constraint is answered by the handler, not refused on the wire.** matter.js's request TLV schema refuses it before the behavior runs (`CommandInvokeResponse.ts:484-487` at `862ad75f`, CONSTRAINT_ERROR); the Go generic server passes it to the handler, and the OnOff, DoorLock, EVSE, WaterHeater and CommodityPrice ports pin the handler's status. Route: a request-constraint check in `spec.Server`'s invoke path, with those ports' expectations re-read against the pin; brief CT report, question 2. (Triage 2026-10-29: wave H1, the device IM and write path.)
- **A command response is held to field presence only.** matter.js validates the whole response payload (`ProtocolService.ts:725-740` at `862ad75f`; DoorLock switches the constraint off per response, `DoorLockServer.ts:704-707`); the Go server checks which fields a response carries, not their enum values or nested structs. Route: with the request check above. (Triage 2026-10-29: wave H1, the device IM and write path.)
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

- **Keep-alive reports suppress the response.** An empty max-interval
  DataReport goes out with `SuppressResponse = true`
  (`bridge/subscribe.go`, `reportSubscription`); matter.js now sends it with
  `suppressResponse: false` so the peer's INVALID_SUBSCRIPTION (Core § 8.5)
  is read on every transport (`ServerSubscription.ts:913-914` at `862ad75f`,
  #4766; ServerSubscriptionTest "keep-alive status"). Fix: send
  `SuppressResponse = false`, wait for the status, end the subscription on
  INVALID_SUBSCRIPTION as `closeSubscriptionByExchange` already does for data
  reports. Re-read at `db021da0` (review II, phase 0): the keep-alive is the
  branch at `packages/node/src/node/server/ServerSubscription.ts:909-920`,
  `suppressResponse: false` at `:914` under the comment "Requested so the
  peer's INVALID_SUBSCRIPTION answer (Core § 8.5) is read on every
  transport". The comment over the Go code (`bridge/subscribe.go:556-559`)
  still says matter.js ships empty DataReports with SuppressResponse=true
  and cites `ServerSubscription.ts:782`; at the pin that line is
  `await this.#cancel(flushViaSession, currentExchange);`. The citation is
  stale and goes with the fix. (Triage 2026-10-29: wave H1, the device IM
  and write path.)
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
- **F-db021da0-2: an unchanged controller write is still reported.**
  matter.js reports, persists and advances a DataVersion only when a value
  changed: `#noteIfChanged` returns early for an equal value
  (`packages/node/src/behavior/state/managed/Datasource.ts:1477-1495` at
  `db021da0`) and `#computePostCommitChanges` advances the version only
  when something changed (`:1456-1466`). Since #454 the root
  BasicInformation's own write path does the same for NodeLabel and
  Location, but the bridge's write path marks every written attribute of
  every cluster dirty regardless of the server's verdict
  (`bridge/receive_dispatch.go:726-739` `reportWrittenAttributes`), so a
  subscriber receives a report item with the same value and the same
  DataVersion where matter.js sends nothing. Fix: the dispatcher reports a
  written attribute only when the server says it changed (the generated
  servers' `MatterWrite` would have to return that verdict, or the
  dispatcher compares the encoded value before and after the write). Found in the review
  of #454 (issue 414); a bridge-wide change, left out of that PR.
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
  `im/subscription:TestEventReportParityMatterJS`. What this module does
  after several failed reports in a row was not read for this entry
  (unverified). Fix: requeue without asking for a report — the paths and the
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
- **F-db021da0-12: a SubscribeRequest with KeepSubscriptions=false on a
  session without a fabric tears down that session's subscriptions.**
  matter.js runs the teardown only for a session with a fabric
  (`if (fabric !== undefined && !keepSubscriptions)`,
  `packages/node/src/node/server/InteractionServer.ts:647` at `db021da0`;
  the walk over the peer's sessions, `:649-659`); on a PASE session it
  cancels nothing. `registerSubscription` closes the subscriptions of the
  same session there (`m.CloseSession`, `bridge/subscribe_dispatch.go`),
  one that is still priming included (noticed by the fix of
  R2-P1-req2-01). Until `5b12fb30` the comment gave
  `InteractionServer.ts:549-566` as matching "on same-session for
  pre-fabric (PASE) subscriptions"; the pin has no such branch.
  connectedhomeip was not read for a PASE session (unverified). Fix: no
  teardown without a fabric, with the parity case in
  `im/subscription/handler_parity_test.go` — or a `by_design` row if
  connectedhomeip's handling is the reason to keep it. Found in review II,
  phase 1 (the citation pass).
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
- **F-db021da0-14: a refused SubscribeRequest leaves the peer's
  subscriptions standing where matter.js has cleared them.** matter.js
  checks the path count (`InteractionServer.ts:635-641` at `db021da0`),
  then runs the KeepSubscriptions=false teardown (`:647-665`), then
  refuses a request that names no path (`:667-672`) and, further down,
  one with an illegal path (`validateReadPaths`, `:699`; the checks
  between `:672` and `:699` were not read). `handleSubscribeRequest`
  validates the paths and refuses the empty request first
  (`bridge/subscribe.go`, `im.ValidateReadPaths`, then the "empty"
  refusal) and tears down only when it registers the subscription
  (`registerSubscription`). So a KeepSubscriptions=false request refused
  for an illegal path or for naming none has cancelled the peer's
  subscriptions in matter.js and none here. Read, not exercised
  (unverified by a test on either side); which of the two a controller
  relies on is not known. Fix: the teardown before the two refusals, as
  the lines have it, with a parity case — or a `by_design` row. Found in
  review II, phase 1 (the citation pass).

## Tier 3 — LOW


### T3-P35-1 — a write value of the wrong Go type gets three different statuses (phase 35, 2026-11-04)

- **Where:** FAILURE from `cluster/core/access_control.go:674`, `bridged_device_basic_information.go:459`, `general_commissioning.go:403`, `network_commissioning.go:234`; CONSTRAINT_ERROR from BasicInformation and Binding through the convention `cluster/spec/write.go:126-128`; INVALID_ACTION from `cluster/core/identify.go:453-457`.
- **matter.js at 862ad75f:** the node's write path validates through `ValueValidator`, whose `DatatypeError` extends `ValidateError` with the default `Status.InvalidDataType` (`packages/protocol/src/action/errors.ts:55-72`); the older TLV-schema path answers `ValidationDatatypeMismatchError` → `Status.InvalidAction` (`packages/types/src/common/ValidationError.ts:31-38`). Neither is FAILURE.
- **Unverified:** whether a wire write can reach a Go server with a value of a type the attribute does not hold — the bridge decodes by the attribute's schema, so these may be unit-level paths only. Phase 35's status assertions pin today's answers (`TestBasicInfo_WriteNodeLabelWrongType` and siblings) so a change is visible; the rule — one status, matter.js's INVALID_DATA_TYPE on the node path — is a decision for the next write-path wave.
- **Measured 2026-11-05 (follow-up wave, attempt reverted):** the "wrong Go type" convention of `cluster/spec/write.go:122-128` also carries a wire case — `bridge.undecodedValue` (`bridge/attribute_value_reader.go:108-114`), the value the attribute's decoder refused (a label over its max, a string cut at an IS1), which the server's write check answers CONSTRAINT_ERROR because it is of no attribute type; matter.js answers a value its TlvSchema rejects with CONSTRAINT_ERROR too (`AttributeWriteResponse.ts` #decodeWithSchema, cited there), and the ported cases `bridge:TestNodeLabelWriteBoundsTheLengthBeforeIS1` (TlvString.ts:57-69), `bridge/attribute_value_reader_userlabel_test.go:87` and `cluster/spec:TestValueValidatorParityMatterJS/still_rejects_a_wrong-datatype_value_on_a_client_peer_write` (ValueValidatorTest.ts:478-480) assert CONSTRAINT_ERROR. Switching the convention to INVALID_DATA_TYPE therefore breaks three matter.js-asserted cases and 20 module tests; the write-path wave has to separate the undecoded wire value (CONSTRAINT_ERROR, as matter.js) from a host-side Go value of the wrong type before the statuses can be unified, and read what matter.js's node path answers a TLV element of another type on the wire (the decoder's error, not the validator's) at the pin.

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
