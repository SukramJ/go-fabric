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
  Crosses `secure/channel` and `bridge`. (Triage 2026-10-29: wave H2, commissioning and security (Fable).) **Review II, phase 6a (decision 127, at `16a8f6e5`):** still true as far as read — matter.js `packages/protocol/src/common/FailsafeContext.ts:77-86` at `db021da0` (`session.closedByPeer` → `#failSafeExpired`); here the PASE close hook is wired to the re-announce only (`bridge/securechannel.go`), and `ExpireFailSafeArmedOverPASE` has one caller, the revoke (`bridge/commissioning_window.go`; no other caller found by grep, inferred). **Pin lift b25ed960, unit P3-C:** since a fail-safe's end counts as a failed commissioning attempt, this also delays and merges the count — matter.js counts an abandoned attempt at the peer's close (`FailsafeContext.ts:77-86`, `DeviceCommissioner.ts:167-172` at `db91886c`); here it counts when the fail-safe runs out, and PASE sessions established before that extend the same context and count once together. Undercount only; the path needs the passcode.
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

- **F-db021da0-28: BridgedDeviceBasicInformation serves NodeLabel and SerialNumber whatever the host set.** Both are optional in the model (`packages/model/src/standard/elements/bridged-device-basic-information.element.ts:30`, `:40` at `db91886c`); that matter.js lists an optional attribute only when its state holds a value was the fix's reading of `ValidatedElements.ts:247-279` and was not repeated (unverified). Here NodeLabel is always served, empty when the host gave none (`cluster/core/bridged_device_basic_information.go:245`), and SerialNumber is filled from the UniqueID when the host gave none (`:179-181`) — the pair matter.js warns about, "uniqueId and serialNumber shall not be the same" (`basic-information-validators.ts:42-44`). A VendorID of 0 or above 0xFFF4 and a ProductID of 0 are warnings here (`:281-301`) where matter.js's validator, which the bridged server runs (`BridgedDeviceBasicInformationServer.ts:45`), throws (`basic-information-validators.ts:32-40`). The code gives Apple's HAP mapper as the reason for SerialNumber, as the root's `BD-Matter-BasicInfo-SerialNumberAlwaysServed`; for NodeLabel it gives none. Not decided: follow matter.js, or a `by_design` entry with the owner's reason. Found in review II, phase 4 (R2-P4-coresys-05). **Review II, phase 6a (owner decisions 143 and 166):** the NodeLabel and SerialNumber half is closed — the bridged server lists an optional attribute only when its state holds a value, as matter.js (`packages/node/src/behavior/cluster/ValidatedElements.ts:252`, skip `:270-275`, add `:277-278` at `db91886c`), and backfills nothing but UniqueID (`BridgedDeviceBasicInformationServer.ts:39-43`) (`09c32c85`); the root serves SerialNumber only when stated (`5e97e2a1`). The VendorID / ProductID half was not part of the decision and stays open.
- **F-db021da0-44: a hand-off LevelControl whose port is also the endpoint's source notifier is told of a host change on two subscriptions whose order the host decides.** The bridge subscribes the endpoint's source notifier for the paths of the source's own cluster (`bridge/subscribe.go:2091-2120`), and a LevelControl server without the transition engine subscribes to its port's notifier itself once it has a change listener (`reconcileHost`, `cluster/levelcontrol/levelcontrol_server.go:611-635`), handing a notification to its forwarded callbacks before its listeners (`onHost`, `:586-607`). When port and source are one notifier, the bridge's callback and the server's are two callbacks of the host's notifier, in the order the host calls them. By the fix's reading the server's change, when it comes first, advances the version and takes the value into the notification's memory (`quieterChanged`, `bridge/subscribe.go:2472`; `settlerFor`, `:2495`), so the notification after it finds nothing moved and reports nothing; with the bridge's half of the fix alone that showed as an advance without a report in 7 of 60 runs of `bridge:TestHostChangeAdvancesOnceOnAHandOffServer`, in a wiring where the server forwards the notifier. For the wiring of this row nothing was run, and the reading was not repeated (unverified). matter.js has one datasource per behavior, whose commit advances and broadcasts together (`packages/node/src/behavior/state/managed/Datasource.ts:1456-1457`, `:1319` at `db021da0`). Who would see it: a subscriber of CurrentLevel on such a device misses a host change until the next one. Fix: one subscription per host notifier and endpoint, or the settlement told which callback comes last; phase 6 of review II. Found in review II, phase 4 (R2-P4-fr-ver-01, second round). **Review II, phase 6b (decision 209):** not reproduced — a run of exactly this wiring (a fifth sub-test of `bridge:TestHostChangeAdvancesOnceOnAHandOffServer`, the port's own notifier as the endpoint source's) passed 300 runs, 100 under `-race`: one advance, one report, and a notification that moved nothing advances nothing. Both callback orders are inferred from Go's random map iteration over the runs (the test notifier keeps its callbacks in a map, `bridge/write_notifier_version_test.go:29-57`), not instrumented; that both callbacks were registered when it fired was not asserted. The row stays as "not reproduced".
- **F-db021da0-46: the hand-off advance of LevelControl and ModeSelect moves a version no controller reads.** A note, not a wire gap. The dispatcher serves a server's own version only on the root and the Aggregator (`ownDataVersion`, `endpoint/dispatcher.go:39-45`); on every other endpoint the served version is the endpoint's (`clusterDataVersionFor`, `:55-63`), and `clusterDataVersion` (`:24-29`) has no caller outside tests. LevelControl and ModeSelect are mounted on neither, so the rule `53c68d85` gave their own trackers — a command handed to the host's port advances when the value it leaves differs from the one the last host notification read (R2-P4-rr-im-01) — and the tests that pin it hold `MatterDataVersion()`, which the wire does not carry. On the wire the bridge's command commit advances the endpoint's version once for the same case (`BD-Matter-Invoke-HoldsHostReports`). Fix: none for the wire; the tests say which version they hold, or the trackers of servers that are never served by them go. Not placed. Found in review II, phase 4 (R2-P4-fr-im-03). Since review II, phase 5b (`c1028573`) `spec.Server`'s `ServerConfig.DataVersion` and `MatterDataVersion` state that the wire carries a server's own version on the root endpoint and the Aggregator only; LevelControl's and ModeSelect's own trackers and the tests that hold their `MatterDataVersion()` say nothing of it yet (`cluster/levelcontrol/levelcontrol_server.go`, `cluster/modeselect/modeselect_server.go`). Review II, phase 6a (owner decision 165): every host-owned tracker is `Deprecated:` — LevelControl's, ModeSelect's and modebase's `Config.DataVersion`, `HeldConfig.DataVersion`, `MatterDataVersion`, `spec.ServerConfig.DataVersion` and the server `Config.DataVersion` fields that pass one in; the row closes when they are removed after the deprecation window.
- **F-db021da0-50: the re-establishment of former subscriptions connects over UDP only.** matter.js prefers TCP for it when the peer's last known operational address was TCP (`packages/node/src/behavior/system/subscriptions/SubscriptionsServer.ts:298-309` at `db91886c`). Here the ADR 0008 initiator resolves and handshakes over UDP (`connectPeer`, `runCASEInitiator`, `bridge/case_initiator.go`). Not deliberate: the order of the steps (ADR 0032, Shape 1). Fix: the re-establishment on the one session path; the single session layer ADR 0032 names as the direction closes it. Found in review II, phase 5b (wave B). Review II, phase 6b (decision 209): connectedhomeip re-establishes over MRP as well (`SubscriptionResumptionSessionEstablisher.cpp:83`, `CASESessionManager.h:87` at `6170af84`); stays open with ADR 0032's direction.
- **F-db021da0-51: a request on a session a node's client stack opened is served with the bridge's gates, not with the rest of the bridge's server.** The node serves its endpoint tree there with the bridge's dispatcher, ACL, privilege, existence, timed and Large Message gates and the same request facts (`clientServerConfig`, `node/client.go`; review II, wave B), but a write there runs in a fresh `im.WriteTransaction` of its own, not under the bridge's write holds, so it is not answered BUSY for an attribute a chunked write on a bridge session holds; a chunked write on such a session is applied chunk by chunk as separate writes (the server's exchange ends after the first chunk, `MoreChunkedMessages` is not read), so a chunked ACL replace leaves the list at its first chunk; none of the bridge's per-session bounds (chunk waits, chunked writes per session) applies, and a read's whole report is built before its first chunk is sent; a SubscribeRequest is answered INVALID_ACTION, a read reports no events, an invoke's before-and-after snapshot of the invoked clusters does not run (a change reaches the subscriptions through the servers' own notifications only), and a CommissioningComplete there does not close the PASE sessions as the bridge does after it (`closePASESessionsOnCommissioningComplete`). The session is named to the clusters by its identity (`core.WithInvokeSession`), never by a bridge id. Not deliberate: the order of the steps (ADR 0032, Shape 1). Fix: the single session layer, or earlier the bridge's write holds and bounds handed to the client stack's server. Found in review II, phase 5b (wave B). **Review II, phase 6b (owner decision 242), split:** the chunked-write part is fixed: on a session a node's client stack opened, a WriteRequest with MoreChunkedMessages is refused whole with FAILURE before anything is applied, so a chunked ACL replace leaves the list as it was; it fails closed until ADR 0032's single session layer, which matter.js does not need: a request goes out on the peer's newest session (`packages/protocol/src/peer/PeerExchangeProvider.ts:103-115`, `packages/protocol/src/peer/Peer.ts:440-448` at `db021da0`), and one server serves every session (ADR 0032's reading; no line read for it here, unverified). The rest — the bridge's write holds (no BUSY), its per-session bounds, no subscription, no events in a read, no invoke snapshot, no PASE close after CommissioningComplete — stays with ADR 0032's open point. The clause "a read's whole report is built before its first chunk is sent" is stale: the read pulls its reports through the chunker chunk by chunk. Pinned by `client/interaction:TestServeChunkedWriteRefusedWhole`.
- **F-db021da0-53: a pairing hint with any of bits 16 to 22 set is no hint at all to a commissioner built on connectedhomeip.** matter.js models 23 hint bits, the last `powerCycleUntilIndication`, bit 22 (`packages/protocol/src/advertisement/PairingHintBitmap.ts:145` at `db91886c`). connectedhomeip reads PH with `MakeU16FromAsciiDecimal` (`src/lib/dnssd/TxtFields.cpp:195-198` at `6170af84`), which returns 0 for a value that does not fit 16 bits (`:104-108`), and stores that (`:274-275`): a PH above 65535 is read as 0, the whole hint. The DNS-SD reader is the only parser of the TXT key; the value has a second carrier, the UDC message's TLV field (`src/protocols/user_directed_commissioning/UserDirectedCommissioningServer.cpp:302`, holder `UserDirectedCommissioning.h:132`, 16 bits) (review II, phase 6b, R2-P6-doc-2907). The module advertises the bits the host sets, as matter.js, and its docs state the consequence (`mdns.PairingHints`, `node.Commissioning.PairingHints`). Who sees it: a chip-based commissioner shown a device whose host set a hint of bit 16 or above, which sees no hint. Fix: none in this module while the two references differ; a host that wants every commissioner to see its hint keeps to bits 0 to 15. Found in review II, phase 5b (the reference read of wave A2).
- **F-db021da0-54: matter.js's set of hints that need a PairingInstruction leaves out bits 20 to 22.** matter.js's bitmap comments say the PairingInstruction SHALL carry the value for bits 20, 21 and 22 (`packages/protocol/src/advertisement/PairingHintBitmap.ts:128-145` at `db91886c`), but its instruction set names eleven bits without them (`PAIRING_HINTS_REQUIRING_INSTRUCTION`, `packages/protocol/src/mdns/MdnsConsts.ts:20-32`). The module mirrors the set (`mdns.PairingHints`): bit 20 with a PairingInstruction is refused, as in matter.js. A gap of matter.js's, mirrored; the row closes when matter.js's set changes, and the mirror follows. Found in review II, phase 5b (the reference read of wave A2).
- **R2-P6-par-1360: a field conformance that compares a field with the null literal never conforms.** `performComparison` is conformant only when both sides are non-null (`packages/node/src/behavior/state/validation/conformance-util.ts:183-209` at `db91886c`), and this module's comparison is the same (`cluster/spec/field_conformance.go`). At the pin this hits WebRtcTransportProvider ProvideOffer's five fields (`MetadataEnabled` disallowed even with a null session id); the device only logs the misfit, because ProvideOffer states no Refusal. A gap of matter.js's, mirrored; the row closes when matter.js compares with null, and the mirror follows. Found in review II, phase 6a. Review II, phase 6b (decision 209): reach at b5ffb4c9: one definition (WebRTCTransportProvider), no refusing command — a log line only; connectedhomeip's `HandleProvideOffer` has no generic conformance evaluation and no check of MetadataEnabled (`src/app/clusters/webrtc-transport-provider-server/WebRTCTransportProviderCluster.cpp:844-1104` at `6170af84`), so chip accepts the offer, as this module does.
- **R2-P6-fsm-1412 (parked loop): a mode switch or a MoveToHueAndSaturation over a colour loop stops its transition but leaves ColorLoopActive 1.** As matter.js (`stopAllColorMovement`, `packages/node/src/behaviors/color-control/ColorControlServer.ts:843-848` at `db91886c`, no `colorLoopActive`) and connectedhomeip, which leaves it 1 by design: a command's hue transition parks the loop, "ColorLoopActive stays 1 … only ColorLoopSet re-engages it" (`src/app/clusters/color-control-server/ColorControlCluster.cpp:698-701` at `6170af84`), "that attribute stays 1 while the loop is dormant" (`:972-973`), a MoveToColor or MoveToColorTemperature "can leave the loop active-but-dormant" (`:1717-1718`). Recorded as both references' behaviour. The Time-0 half follows connectedhomeip since review II, phase 6b (`BD-chip-ColorControl-LoopTimeZero`). Found in review II, phase 6a.
- **R2-P6-err-1404 (follow-on): a scene recall of an active colour loop no longer invalidates the scene in passing.** `MatterApplySceneValues` calls `colorLoopSet` directly (`cluster/light/colorcontrol_scenes.go`), whose invalidation mark only `MatterInvoke` reads; matter.js invalidates there (`packages/node/src/behaviors/color-control/ColorControlServer.ts:1869` → `:1145` at `db91886c`). The net state is equal — core activates the scene after applying (`cluster/core/scenes_management.go`) — only the intermediate invalid state is gone. Inferred from the read, not run. Found in review II, phase 6a.
- **R2-P6-par-1466 (follow-on): ElectricalPowerMeasurement's Accuracy entries are a stub.** One entry per served measurement type (Voltage, ActiveCurrent, ActivePower, Frequency; three without Frequency for DC), as the model's text asks (`packages/model/src/standard/resources/electrical-power-measurement.resource.ts:57-59` at `db91886c`), but each states the attribute's constraint as its Min/MaxMeasuredValue and FixedMax 1 (`cluster/measurement/measurement.go`): no source carries a meter's accuracy — the contract has none, matter.js's server is empty and connectedhomeip's values were not taken. Fix: a host-supplied accuracy, a contract capability. Found in review II, phase 6a.
- **R2-P6-sec-1513: the door lock's credential data are sealed without associated data, under a key kept in the same store.** As matter.js (`packages/general/src/crypto/Cipher.ts:34-49`, `packages/node/src/behaviors/door-lock/DoorLockServer.ts:153-155`, `:1569-1573` at `db021da0`); the module's store-wide sealing is `BD-Matter-SealedAtRest`. Reaching it needs store access; no network party gains. A gap of matter.js's, mirrored. Found in review II, phase 6a. Review II, phase 6b (decision 209): connectedhomeip at `6170af84` does not seal the data at all (no `encrypt` or `AES` under `src/app/clusters/door-lock-server`, grep).
- **R2-P6-err-1560: a host preset setter between the candidate check and the apply can still make the apply refuse after the presets were stored.** `checkCandidate` and `applySet` do not run under one hold of `mu` (`cluster/thermo/thermostat_typed.go`). Closing it needs the preset setters under the same hold, which the `op` / `persist` order forbids for the inbound path. This module's own; no reference sentence. Found in review II, phase 6a. Review II, close-out C3 (decision 518): kept as this registered residue. A host-path lock taken by every inbound setter that moves setpoint state and held by SetAttributes from the check to the apply would deadlock: SetAttributes holds it while SetPresets runs `reevaluateOnPresetsChange`, which can follow a suggestion into the Sink, and the Sink may call the inbound setters, which wait for the same lock (`cluster/thermo/thermostat_server.go`). Applying the limits and setpoints before the presets changes the call's meaning, and rolling the stored presets back is itself a save that can fail.
- **R2-P6-store-1501: the wait on the preset store takes no context.** A `SavePresets` that never returns blocks every inbound setpoint setter, which did not wait on the store before; stated on the exported `thermo.PresetStore` doc. This module's own. Found in review II, phase 6a.
- **F56-n: a host change that arrives outside an attribute data of a write — between two, or after the last — gets no advance of its own when the write changed its cluster.** The bridge's hold table stamps such a change with no attribute data and settles it against the whole record (`bridge/write_hold.go` `open`, `settleWrite`; measured by run at `7eea3b49`'s code, Fable's read of the F-db021da0-56 rework: no advance after an attribute data that changed the cluster, none before a later one that does); matter.js would give a change after a commit its own commit and advance (inferred from `packages/node/src/behavior/state/managed/Datasource.ts:1456-1457` at `db021da0`). Found in review II, phase 6a.
- **R2-P6-par-0444: a fabric-scoped event that is not fabric-sensitive is not filtered by a fabric-filtered read.** matter.js filters a fabric-sensitive event to the accessing fabric whatever the read's FabricFiltered flag (`packages/protocol/src/action/server/EventReadResponse.ts:344-352` at `db91886c`), and a fabric-scoped event that is not fabric-sensitive only when the read is fabric-filtered (`:353-364`); the module has the first rule only (`im/read.go`). No effect at this pin — no such event in the snapshot; held by `im:TestEveryFabricScopedEventIsFabricSensitive`, which fails when one appears. Found in review II, phase 6a.
- **R2-P6-sec-1103 (follow-on, decision 152): a requester a `contract.ReadAccessDeclarer` refuses is answered UNSUPPORTED_ACCESS for an attribute the server does not list, before its existence is known.** matter.js answers UNSUPPORTED_ATTRIBUTE before the attribute's read-level check (`packages/protocol/src/action/server/AttributeReadResponse.ts:263-266` before `:272` at `db021da0`). Fail-closed; the capability is the module's own (`BD-Matter-ReadAccessCapability`). Found in review II, phase 6a.
- **R2-P6-acl-0402 (follow-on): the accessing-fabric stamp of a written fabric-scoped entry finds the field by its Go name `FabricIndex`.** A host struct that names field 254 otherwise passes unstamped (`endpoint/dispatcher.go` `WriteAuthorized`), while `contract/matter.go`'s `MatterWrite` doc states the condition since review II, phase 6a. matter.js injects the accessing fabric into every decoded write (`packages/protocol/src/action/server/AttributeWriteResponse.ts:466-473`, "We always inject the current fabricIndex for writes", `:471` at `db021da0`). No in-tree server is exposed. Found in review II, phase 6a.
- **R2-P6-wire-0442 (follow-on): a write to a cluster without a definition hands the server the bridge's undecoded value.** `writtenHeld` answers Success without a definition (`endpoint/dispatcher.go`), so a host server without one receives the bridge-private `undecodedValue`; the module does not refuse it. Found in review II, phase 6a.
- **Review II, phase 6a (stake, decision 157): the sealed store's v11 baseline is a hand list.** `store` `TestSecretsAddedAfterV11ShipAnUpgradeStep` holds `sealedAtBaseline` equal to the sealed lists at `fcfbcb3c`; that it is exactly v11's rests on v11 being unreleased with all of them (the canary `f4de428e`, the RI key `425207e1`, the same day), not on a tag. The guard proves an upgrade step by the name of a package test, not by what it does; adding a secret together with its baseline line passes — only review stops that.
- **R2-P6-fsm-2109: the controller takes a granted MaxInterval unchecked — not performable.** Missing input: the peer's IdleModeDuration, which the SubscribeResponse does not carry. Both references accept the granted value: matter.js decodes the response straight into its subscription (`packages/protocol/src/action/client/ClientInteraction.ts:1099`, `:1101-1109` at `db021da0`; the timeout `subscription/PeerSubscription.ts:38-39`), and connectedhomeip's client takes `GetMaxInterval(&mMaxInterval)` unchecked (`src/app/ReadClient.cpp:1122` at `6170af84`) and uses it for the liveness timeout (`:1032`). connectedhomeip's publisher may grant more than the ceiling by design: `src/app/ReadHandler.cpp:780` `maxIntervalCeiling = std::max(GetPublisherSelectedIntervalLimit(), mMaxInterval)`, the limit being 3600 s (`ReadHandler.h:58`) or, on an ICD, `max(IdleModeDuration, 3600)` (`ReadHandler.cpp:42-50`). A client clamp or refusal at `max(ceiling, 3600 s)` would refuse those devices (inferred). Owner decision 211: register only. Found in review II, phase 6b.
- **R2-P6-par-2156: the status budget of a write chunk with a wildcard-endpoint path — not performable.** Missing input: the device's endpoint count, which is the number of statuses such a write is answered with. matter.js budgets one status per AttributeDataIB by the request path (`maxStatusSizeOf(path)`, `packages/protocol/src/interaction/InteractionMessenger.ts:103-105`, `:1344-1346`, `:1376-1381` at `db021da0`); connectedhomeip reserves for the MIC and the request's TLV overhead only (`src/app/WriteClient.cpp:183-204` at `6170af84`). The budget of `client/interaction`'s write chunking holds for concrete paths only. Owner decision 212: register only. Found in review II, phase 6b.
- **R2-P6-fsm-1915: one OTA update at a time, and any progress of its transfer keeps the slot.** Any change of the BDX transfer counter counts as progress (`isTransferring` and the queue loop, `controller/softwareupdate/manager.go`); a commissioned node that negotiates a block size of 1 and queries a block every few minutes keeps the single slot. Not changed: an overall deadline or a minimum rate would be a decision. This module's own; whether matter.js behaves the same was not read (unverified). Found in review II, phase 6b.
- **N-fabric-2 (stored-subscriptions half): whether a removed fabric's stored server subscriptions are swept is not performable here.** The reconcile (`node/fabric_state.go`) does not sweep `matter_server_subscriptions`; whether a row of a gone fabric is loaded or re-established is decided by the bridge's load and re-establish path (`bridge/subscription_persistence.go`), the input this check did not have. Found in review II, phase 2; recorded in phase 6b.
- **Descriptor TagList: a host that enables it can serve a manufacturer tag without a label.** `EnableTagList` and `AddTags` (`cluster/core/descriptor.go`, off by default) take a tag with an `MfgCode` and no `Label` and do not refuse it. CHIP's data model makes Label mandatory when MfgCode is not null (SemanticTagStruct field 3), and matter.js relaxes it to O ("TODO we do not support MfgCode != null", `support/models/src/local/semtag.ts:9-22` at `db91886c`) — the class (ii) entry `semtag.Label` below. Whether the served struct is held to the generated definition's conformance anywhere else was not read. Fix: the module refuses such a tag. Found in review II, phase 6b (R2-P6-doc-2817, D8(c)).
- **R2-P6-doc-3084 (OTA requestor image): the OTA requestor keeps its downloaded image in a file, outside the persistence seam.** `cluster/core/ota_requestor_query.go` stats, opens and removes the image file directly (`os.Stat`, `os.Open`, `os.Remove`). ADR 0026 principle 7 named it one of two residues that phase 21 would move behind a seam; phase 21 landed (`store.Port`, `store/memstore`) and this one stayed. A host on a non-file store still needs a file system for the image. Fix: phase 7 (owner decision 249). Found in review II, phase 6b.
- **R2-P6-doc-3084 (DCL snapshot fetch): the DCL snapshot is written to files, outside the persistence seam.** `controller/dcl/snapshot/fetch.go` writes with `os.MkdirAll` and `os.WriteFile`, the second residue ADR 0026 principle 7 names. Fix: phase 7 (owner decision 249). Found in review II, phase 6b.
- **Commissioning window: the window reads as open before its PASE acceptor is attached.** Low. The window is open — `IsOpen()` true, `paseWindowClosed()` false — before the transition hook has run, because the hooks fire after `w.mu` is released (`transitionHooks`, `bridge/commissioning_window.go`), and `renewSalt` stands before `attachPase(h.baseline)` (`node/commissioning.go`). A PBKDFParamRequest that arrives in between reaches the PASE branch, and `resolvePaseHandler` falls to the singleton with `provider == nil` — the fail-closed noop, which answers nothing; `claimPase` runs before the handler is resolved, so the noop exchange holds the single-PASE claim (read, not instrumented). Measured on a scratch copy with the verifier derivation moved into the hook: the commissioner gave up. The gap (open → acceptor attached is not one step) existed before decision 251; it is now a `rand.Read` wide. In production a commissioner learns of the window from the record or the BLE advertisement, both sent after `attachPase` in the same hook, so only a blind PBKDFParamRequest can land in it — low. Fix: `attachPase` before anything else in each open branch (or the acceptor attached before `open` is visible). Found in review II, phase 6b (Fable read of wave 3, round 2).
- **R2-P6-sec-2412 (follow-on, client stack): a handshake of an updated fabric that completes after the sweep's context ended is adopted and not closed.** On the client stack the sweep waits for a connection process only as long as its own context lives (`client/peer/connection.go:392-405`); a handshake of the fabric that completes after that is adopted (`:1463-1466`, `:1496-1505`) and not closed (`node/client.go:401-421`). Read, not run. The context is the UpdateNOC command's, or the owed-event delivery's at start; the window is the time the process needs to unwind after its cancel (inferred). Fix: wait on the process's end alone, or walk the sessions again after the wait. Found in review II, phase 6b.
- **R2-P6-sec-2401 (limit half): the node's own ACL entry can take a fabric's list past AccessControlEntriesPerFabric.** As matter.js: `packages/node/src/behaviors/web-rtc-transport-requestor/WebRtcTransportRequestorServer.ts:81-91` builds `[...acl, entry]` and sets it; the validation wired to `acl$Changing` (`packages/node/src/behaviors/access-control/AccessControlServer.ts:72`) returns at `:154-156` (`if (!hasRemoteActor(context)) return;`) before the limit check `:178-181` (at `db021da0`), so a local change is not held to the limit (inferred: that the node's own `setStateOf` carries no remote actor was not read), and a later controller write of a list longer than the limit is refused at `:179-180`. connectedhomeip refuses: `AccessControl::CreateEntry` checks `(count + 1) <= maxCount` (`src/access/AccessControl.cpp:252-257` at `6170af84`). The node's own Operate entry (WebRTC requestor, OTA provider) is appended the same way (`node/local_acl.go`); an administrator whose list is then full must drop an entry before any whole-list write succeeds (ResourceExhausted). Kept as matter.js (owner decision 228). Candidate for a later unit (*Ahead*): serve AccessControlEntriesPerFabric with headroom for the node's own entries, so a controller's four are never used up — a separate decision. Found in review II, phase 6b.
- **R2-C2-store-1204: the OTA requestor keeps UpdateInProgressDetails.Location of a removed fabric.** `fabricRemoved` drops the removed fabric's defaults, active providers, selection and deferred announcements, not the update in progress: at the next start NotifyUpdateApplied and the Busy-retry query go to whichever node then holds that (fabric index, node id) (`cluster/core/ota_software_update_requestor.go`, `cluster/core/ota_requestor_query.go`). As matter.js: it keeps `updateInProgressDetails` until the next start and then uses its `location` for NotifyUpdateApplied and the Busy-retry query, and the file has no fabric-removal handling (`packages/node/src/behaviors/ota-software-update-requestor/OtaSoftwareUpdateRequestorServer.ts:241-244`, `:255-278`, `:292-302`, `:1246`, `:1278` at `db91886c`). A gap of matter.js's, mirrored; the row closes when matter.js drops the location with its fabric, and the mirror follows. Found in review II, full-cut run 2 (unit coresys); recorded in close-out C3.
- **R2-C2-wire-0551 (rest, decision 543): `client/interaction` returns a command response without its mandated fields as Success.** `im.UnmarshalInvokeResponseTLV` discards such a response when the fields reader refuses the empty structure with a typed `StatusCodeError` (decision 535), but `client/interaction`'s reader `readFields` (`client/interaction/invoke.go:356-361`) never refuses `{}`, so `Invoke` / `InvokeBatch` still answer Success without fields. matter.js decides by the response schema (`responseSchema !== TlvVoid`) and reports NoCommandResponse (`packages/protocol/src/action/client/ClientInteraction.ts:571-576`, `:636-664` at `b25ed960`). The Go trigger is also narrower (a typed reject of `{}` versus any non-void schema). Run 3, unit cpeer.

## matter.js pin b25ed960 — open items (pin lift 2026-11-19)

The gaps of the lift db021da0 → b25ed960 (`pin-lifts.md`), each with the
wave unit that closes it (decision 287: every one before the release). The
rows the lift caught up in the *Ahead of matter.js* register are reconciled
by their units too and tracked there, not here. matter.js lines are at
b25ed960; connectedhomeip was read only where an entry names it at 6170af84 (otherwise unverified). Closed entries move to `matter_behaviour_findings_closed.md`, same section. Wave P6 (2026-11-22) closed the rest; the rows left are carried past the lift with their reasons (decisions 505, 509).

- **F-b25ed960-32: `Relaxed` stands for two matter.js switches.** matter.js turns a field's validation off as a whole (`validate = false`, `packages/node/src/behavior/state/validation/conformance.ts:36-40` at `db91886c`) or only its constraint (`constraint = false`, `constraint.ts:91-95`, the enum and the conformance staying); `spec.Field.Relaxed` drops the enum-member check either way (`cluster/spec/field_conformance.go` `fieldValidator`). Every `constraint = false` field at the pin is an integer (`DoorLockServer.ts:1520-1526`), so nothing differs today. Latent, low: a `constraint = false` enum field would need its own relaxation kind; one sentence in `script/clustergen/relaxed.go`'s header when it is next touched. Carried past the lift (decision 505): doc-only, nothing differs at the pin.
- **F-b25ed960-34: a session closed after the Success's first transmission is still handed over and its record saved.** matter.js the same: an established session that is closing completes (`NodeSession.ts:242-244` at `db91886c`) and the responder saves the record (`CaseServer.ts:337-357`). Here `completeEstablishment` (`client/exchange/session.go`) and `handshake.CASEServer` follow it. Consequence on the controller's UpdateNOC sweep (`node/client.go`, the sessions closed, then the peer's record deleted): a responder handshake in its ack wait can save a record after the deletion; `node`'s client stack serves no `handshake.CASEServer`: it builds `peer.NewSet` over its resumption store and registers no responder (`node/client.go:133-147`); the only `NewCASEServer` callers are `controller/commissioner/commissioner.go:285` and `bridge/bridgetest/commissioner.go:271` (unit P2-J's read, 2026-11-20), so node's UpdateNOC sweep (`node/client.go:392-437`) is unaffected. Whether the controller's commissioner has a comparable sweep was not read (unverified). Low, a gap of matter.js's, mirrored; the row closes when matter.js refuses it or the commissioner is shown unaffected. Carried past the lift (decision 505): a gap of matter.js's own, mirrored; nothing to close before matter.js moves.
- **F-b25ed960-45: `client/peer:TestPeerAddressMonitorCoalescing` — watch.** Failed once under the full `make race` (`VerifyReachability did not return`, `monitor_test.go:62`, 10 s wall clock; pin-lift wave P2); not reproduced in 3000 runs alone and 270 loaded package runs (unit P2b-L). Hypothesis, unverified: `waitVerifyCallers` counts `verify(` frames process-wide, so a stale caller of an earlier test still unwinding would let it return before the second caller joined, which would then start a second run whose probe nobody answers. No fix without a reproduction; reopen with the next failure's log. Carried past the lift (decision 505): watch, no reproduction.
- **F-b25ed960-48: node `TestPeerPathWithoutTCPRefusesTheSignal` times out under `-race` (phase-7 register `notes/review/2026-11-13/phase-7-register.md:156-157`: "at a9658f58 too, not traced"), not reproduced — watch.** P3b-R audit: 0/3 `-v`, 0/120 beside a full `./node/` race run, 2 full runs green; P3b-R fix at `c4ed9574`: 0/150 (`GOTRACEBACK=all timeout 900 go test -race -count=50 -cpu 1,2,8 -timeout 120s -run '^TestPeerPathWithoutTCPRefusesTheSignal$' ./node/`, under concurrent race load). No blocking call found on the test's path (`peer_path_test.go:47-124`, `:162-176` at `4a3a152c`; waits bounded by `parityCtx`, 90 s). Test quality, no matter.js behaviour in it. Whoever sees it again: rerun that command with `-v` and keep the goroutine dump; no fix without it (owner decision O-12, decision 397). Carried past the lift (decision 505): watch, no reproduction.
- **F-b25ed960-C7: bridge `TestReestablishStopsWithTheBridge` timed out under `-race` in CI — not reproduced, watch.** PR #568's `test (linux, race)` job (run 44965916729): `subscription_reestablish_order_test.go:291: timed out waiting for A and B persisted` after the 5 s `waitFor`. The wave did not touch the path (`git diff b79d38fd -- bridge/subscription*.go` empty); locally 30/30 green under `-race`. A wall-clock wait on a loaded runner, as -48; cause not traced. Carried past the lift (decision 505): watch, no reproduction.

- **F-P8-694: host value whose encoder fails or panics — #625.** A host value whose `EncodeTLV` leaves a container open, or any host panic while a report is built, ended the process in an ongoing subscription report: the report goroutines of `im/subscription` (`engine.go` tick, `manager.go` flush) had no recover (red runs of `bridge` `TestHostEncoderPanicInOngoingReportKeepsTheNodeW2B10` and the two `im/subscription` W2B10 tests before the fix). Backstop since phase 8 wave 2 (decision 694): the panic is recovered in both goroutines, logged ERROR `im.subscription.report_panic`, the report is dropped with everything in it, the subscription kept. matter.js at db91886c has its catch-all at the same place and does not close the subscription either (`ServerSubscription.ts:530-532`; its flush goes through it, `:777-780`), but it requeues an ongoing update for its first two failures and drops it from the third (`:584-585`, `:606`); the backstop drops at once — to be settled with the rest. Still open: a Read and a Subscribe's priming report of such a value get no answer (measured once over UDP by the phase-8 B2b agent, no reply within 3 s; not re-checked; matter.js answers the whole Read with Failure, `packages/protocol/src/interaction/InteractionMessenger.ts:425-462`); the engine's event-source read on the tick goroutine has no backstop (`engine.go:168`, inferred from the code). Target: a per-path status with the rest of the report sent, as connectedhomeip `src/app/reporting/Engine.cpp:460-503` at 6170af84 (Option A).

## matter.js pin db91886c — open items (pin lift 2026-11-27)

The gaps of the lift b25ed960 → db91886c (`pin-lifts.md`), each with the
unit of review II's phase 9 that closes it (decision 737: every one before
the release). The two rows the lift caught up in the *Ahead of matter.js*
register (`BD-chip-Thermostat-Schedules`, P1-F;
`BD-chip-Commissioner-AbortDisarmsFailSafe`, P1-D) are tracked there and in
`script/aheadcheck/caught-up.txt`; the reconcile's differences of the
second are F-db91886c-5. matter.js lines are at db91886c, Go lines at
c26625dfa; connectedhomeip at 6170af84 where an entry names it. Closed
entries move to `matter_behaviour_findings_closed.md`, same section.

- **F-db91886c-1: a request without SuppressResponse or TimedRequest is refused (#5047).** Medium, interop. matter.js decodes Invoke's `suppressResponse` and `timedRequest` and Write's `timedRequest` as optional (`packages/types/src/protocol/messages/TlvInvokeRequest.ts:23`, `:30`; `TlvWriteRequest.ts:29`; Write's `suppressResponse` `:22` was already optional), a missing TimedRequest counts as false (`packages/node/src/node/server/InteractionServer.ts:399`, `:976`), and a group Write without SuppressResponse counts as true, before the MoreChunkedMessages and group checks (`:410-413`); the new decode fixture is a group Invoke "as the CHIP SDK sends it" with InteractionModelRevision 14 (`packages/types/test/protocol/TlvInvokeRequestTest.ts:44-53`). Here `im/invoke.go:245-249` and `im/write.go:102-103` refuse the missing fields (`missingField`), `im.WriteRequest.SuppressResponse` is a plain `bool` (`im/write.go:37`) so the group rule cannot see absence, and `im/group.go:246` refuses a group write without it; the bridge decides nothing (`bridge/group_messaging.go:350-353`). chip-tool at 6170af84 always writes both flags (`src/app/CommandSender.cpp:113`, `src/app/WriteClient.cpp:207-208`), so no chip-tool leg reaches this path; which CHIP commit omits them is unverified. Closes in P1-A.
- **F-db91886c-2: the client always sends a false TimedRequest and Invoke's false SuppressResponse (#5069, #5047's sender half).** Low, latent: matter.js omits a false flag only with `omitFalseFlags` or when its own and a unicast peer's InteractionModelRevision are both ≥ 14, a group request keeping both (`packages/protocol/src/interaction/InteractionMessenger.ts:1569`, `:1587-1611`, rule `:1594-1598`), and its own revision is 12 (`packages/model/src/common/Specification.ts:127`), so only the option changes the wire today. The option is `RequestFlagsOptions.omitFalseFlags` (`packages/protocol/src/action/client/RequestFlagsOptions.ts:10-22`), carried by `Invoke()` and `Write()` (`Invoke.ts:110`, `:157`; `Write.ts:70`, `:91`), applied before a Write is chunked (`InteractionMessenger.ts:1330-1331`) and a key of `isBatchable` (`ClientInteraction.ts:223`). Here the encoders write both Invoke flags (`im/invoke_client.go:31-32`) and Write's TimedRequest (`im/client.go:126-129`) always; `client/interaction` has no option (`invoke.go:274-276`, `write.go:149`, `invoke_batcher.go:66-73`); the group sender is `groups/request.go:53`, `:124`, outside the unit's paths. Decision 733: mirrored now. Closes in P1-B (the encoder switch in P1-A).
- **F-db91886c-3: a missing report chunk leaves the subscription open (#5046).** Medium: a late chunk is processed as a new report of the still-known subscription. matter.js records the interruption `"chunk-missing"` when the wait for a continuation chunk ends in `PeerMessageMissingError` (`packages/protocol/src/interaction/InteractionMessenger.ts:991`, `:1026-1029`, `#nextDataReport` `:1064-1073`), the handler swallows it as it does `"chunk-unacknowledged"` (`packages/protocol/src/action/client/subscription/ClientSubscriptionHandler.ts:141-145`) and hands every interruption to the subscription (`:151-158`), which closes for `chunk-missing` whatever its priming (`PeerSubscription.ts:92-126`, `:104`, `:125`). Here the timeout returns unrecorded from `client/interaction/report.go:736-738`, `handler.go:145` swallows only an unacknowledged chunk status, and `subscriptions.go:422-441` knows "chunk" and "final" (`interaction.go:346-361`); the late chunk then opens a new exchange (`handler.go:134-140`). Closes in P1-B.
- **F-db91886c-4: the BUSY hold differs from matter.js's in value, jitter, handler and re-check (#5050).** Severity not stated by the fact note (unclassified). matter.js holds every new Sigma1 of a connection process after a BUSY from any address (`packages/protocol/src/peer/PeerConnection.ts:176`): the hold is the raw minimum wait (`holdNewSigma1WhilePeerBusy` `:536-545`), recorded before `attempt.fail` and so before the `handleError` hook (`:683`, `:686`), each waiter draws its own jitter once (`waitWhilePeerBusy` `:523-534`), and the wait is re-checked after the socket opens (`:558`, `:580`). Here the hold is the final delay including jitter or the handler's replacement, recorded after the handler (`client/peer/connection.go:1643-1663`), `waitOutBusy` sleeps without jitter and only before the attempt (`:1482-1494`, `:1388`). Decision 734 adopts the four; the proven-address restriction (`connection.go:1657`) and the hold's life on the Peer (`client/peer/peer.go:67-69`, `:437`) stay as the rewritten `BD-Matter-PeerBusyDeadline`. Closes in P1-C.
- **F-db91886c-5: the cancel after PASE differs from matter.js's (#5049).** Severity not stated by the fact note (unclassified). matter.js cuts the cancel off when CommissioningComplete starts (`packages/protocol/src/peer/ControllerCommissioningFlow.ts:2176`, `#requestAbort` `:557-559`) and sends that command with `network: "unlimited"` (`:2189`); after a cancel the fail-safe expiry waits at most 2 s (`CANCELED_EXPIRY_WAIT` `:323`, `:1166-1173`) and the flow rejects with the cancel reason, also when the cancel came during the cleanup of an ordinary failure (`:479-487`); each step checks the cancel at entry (`:413`); `Stop` breaks before the time-limit check and the re-arm (`:425-427`); the attestation's DCL wait ends with the cancel (`:1375-1377`); the winner of the PASE race gets a cancel that comes after the win (`packages/node/src/behavior/system/controller/discovery/ParallelPaseDiscovery.ts:102`, `:178-185`). Here CommissioningComplete and the steps after it stay cancelable (`controller/commissioner/flow.go:1512-1513`, `:375-381`, `:1531`) without the unlimited send, the disarm wait is 10 s (`abortDisarmTimeout`, `flow.go:48-55`), a cancel during a failure's cleanup aborts the send at once and the step's error is returned (`:1060-1067`, `:338-344`), no step checks the ctx at entry (`:306-320`), `afterStep` runs before the stop break (`:321-327`, `:403-421`), `attestation.Validate` takes no context (`flow.go:1252`), and `client/discovery` drops an interruption after the win (`session_kinds.go:602-610`; `controller.Pair` reaches the winner through its own contexts, `controller/pair_race.go:93-95`). Decision 736. Closes in P1-D, with `BD-chip-Commissioner-AbortDisarmsFailSafe`.
- **F-db91886c-6: an event is emitted before a delayed one, and before its cluster has a behavior (#5030).** Low. matter.js delays an event also while delayed events wait, keeping the device's order (`packages/node/src/node/client/ClientStructure.ts:639`), and when its cluster has no behavior yet (`:643`); the conditions are `:637-646` of `#emitEvent` (`:626-651`). Here `controller/client/structure.go:401` checks the other four only; the analogue of the behavior is `Cluster.current` (`controller/client/cluster.go:45-50`). Closes in P1-E.
- **F-db91886c-7: the served WebRTC requestor refuses a signal that precedes its session's registration (#5045).** Low. matter.js lets the application hold a peer's unknown signals (`packages/node/src/behaviors/web-rtc-transport-requestor/WebRtcTransportRequestorServer.ts:102-116`): such a signal waits at most 5 s from its arrival for the registration (`:24`, `#awaitRegistration` `:362-389`), ends with the hold, the node going offline or the behavior closing (`:124`, `:131-141`), and a handler holds no lock while it waits (`lockOnInvoke = false`, `:57`). Here `findSessionStrict` refuses at once (`cluster/camera/requestor.go:365-383`). Decision 735: the hold on the served requestor; the controller keeps registering at the response's arrival (`controller/camera/provider_sessions.go:621-663`), recorded as a divergence. Closes in P1-G.
- **Push transport motion and blind period (P1-H).** Not a gap of this range: the certifiability finding "The reference daemon's push transport augments a clip on every motion; chip's once per elapsed duration" (section *Certification harness — open certifiability findings*) is that entry; it closes in P1-H and leaves the register with the change.

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
- **The reference daemon's camera simulation holds a solicited session's
  streams until EndSession; chip releases them at a 30 s connection
  timer.** `examples/reference-bridge/camera.go` `SolicitOffer` acquires the
  session's streams (#596 C) and keeps them until EndSession or the
  session's removal. chip's camera app starts a 30 s connection timer for
  the solicited session
  (`examples/camera-app/linux/src/clusters/webrtc-provider/webrtc-provider-manager.cpp:306`,
  `kConnectionTimeoutSeconds = 30` `:40` at 6170af84) whose expiry is
  `CleanupSession` (`:1408-1420`), which releases the streams (`:1032`)
  and calls no `RemoveSession`; whether the CurrentSessions entry then
  leaves through the peer connection's Closed callback (`:1041` → `:1183`)
  is inferred, not read (`BD-chip-Camera-EndRemovesSessionWhenUnreachable`). No
  Python case at the pin waits 30 s and then deallocates without
  EndSession: TC_WEBRTCP_2_18, 2_19 and 2_24 end the session first. Found
  by the Fable diff read of #596 C (phase 8, wave 1). Route: model the timer
  in the next camera-simulation change that a case needs.

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
- **GroupcastTesting keeps one FabricUnderTest for the node.** Any fabric's command replaces or clears it, and removing the fabric under test does not end the test before its timer — as matter.js (`packages/node/src/behaviors/groupcast/GroupcastServer.ts:404-430`, `:522-525` at `db91886c`). Mirrored; no `by_design` entry; clearing on removal would be ahead of matter.js (review II, phase 6a, R2-P6-fsm-1018).
- **The BTP idle timer runs on the central only.** A peripheral session whose central completed the handshake and then sends nothing has no idle timer (`transport/btp/session.go`), as in matter.js, whose "Central Device Idle Timer" (`packages/protocol/src/ble/BtpSessionHandler.ts:48` at `db91886c`) is restarted only for the central role (`:495-500`). A silent central is bounded by `bridge/ble.go` `watchBLEPASE` until a secure session exists, then by the commissioning window and its fail-safe (review II, phase 6a, R2-P6-fsm-0212).
- **Bitmap bit conformance is judged for a host value only.** A controller's write is not held to it (`cluster/spec/write.go`), as matter.js enforces bit conformance only for a local actor (`packages/node/src/behavior/state/validation/ValueValidator.ts:286-300`, the gate `hasLocalActor` at `:295`, at `db021da0`). Mirrored; no `by_design` entry (review II, phase 6a, R2-P6-par-1305).
- **A repeated context tag in a decoded payload: the last one wins; a NaN float decodes.** Both are matter.js's shape: a repeat throws only with `preserveDataOrdering` (`packages/types/src/tlv/TlvObject.ts:229-232` at `db91886c`), and `TlvNumber.ts` has no NaN check (`cluster/spec/codec.go` `DecodeFields`, `DecodeFloat64`; review II, phase 6a, R2-P6-wire-1313).
- **The controller's untyped JSON keys parse with a base prefix, and a repeated tag is encoded twice.** As chip-tool: `std::stoul(id, nullptr, 0)` and no repeated-tag check (`examples/chip-tool/commands/clusters/CustomArgument.h:133-138` at `6170af84`; `cluster/spec/json.go` `encodeAny`; review II, phase 6a, R2-P6-wire-1365).
- **A decoded TLV payload's container nesting has no depth bound.** `cluster/spec/codec.go` `ReadContainer` recurses per level and `tlv/` has no depth limit; neither reference bounds it — no depth in connectedhomeip's `TLVReader.h/.cpp`, `TLVCommon.h` at `6170af84`, nor in matter.js's `TlvAny.ts`, `TlvCodec.ts`, `TlvObject.ts`, `TlvArray.ts` at `db021da0` (grep). The message size bounds it (review II, phase 6a, R2-P6-bound-1312).
- **An emitted event's payload is not validated before it is recorded.** `cluster/spec/server.go` `EmitEvent`, as matter.js: `OnlineEvent`'s trigger hands the payload as given to `occurrenceManager.add` (`packages/node/src/behavior/Events.ts:203-208` at `db91886c`) and broadcasts it (`:219-228`); the only check is the TLV encoder's mandatory-field check at report time (`packages/types/src/tlv/TlvObject.ts:128-134`). A payload check would be ahead of matter.js (review II, phase 6a, R2-P6-par-1315).
- **RvcRunMode refuses a mode with two or more of Idle, Cleaning and Mapping.** `cluster/modebase` `checkRvcRun` (`n > 1`), as matter.js `packages/node/src/behaviors/rvc-run-mode/RvcRunModeServer.ts:39-46` (`exclusiveModeCounter > 1`, `:45` at `db91886c`); the error text "…together in one mode" is matter.js's own (`:49`). No defect (review II, phase 6a, R2-P6-par-1469).
- **Door lock schedule commands do not compare the accessing fabric with the user's creator.** As matter.js (`packages/node/src/behaviors/door-lock/DoorLockServer.ts:653-692` set, `:734-745` clear, `#requireValidUser` `:1365-1373` at `db021da0`) and connectedhomeip's set handler, which checks that the user exists only (`src/app/clusters/door-lock-server/CodegenIntegration.cpp:1051-1135`, `userExists` `:1091` at `6170af84`) (review II, phase 6a, R2-P6-acl-1566).
- **The reference daemon's DEM test-event forecasts state values chip's trigger file does not.** The adjustment fields on slots other than 0 (ForecastAdjustment's 20 / 2000 / 120 / 240; chip sets them on slot 0 only, `examples/energy-management/energy-management-triggers/DEMTestEventTriggers.cpp:267-270` at `6170af84`), and on the two "clear" triggers `EarliestStartTime` null with `LatestEndTime = EndTime` under STA where chip clears both (`:218-219`, `:252-253`). Conforming, the daemon's own values (`examples/reference-bridge/fleet_evse.go` `demoDem.conform`); TC_DEM_2_5 reads `slots[0]` only; the other DEM Python cases were not read (review II, phase 6a, the 1304 conformance fix).
- **AwaitNextAction repeats without a bound.** As matter.js (`packages/node/src/behaviors/ota-software-update-requestor/OtaSoftwareUpdateRequestorServer.ts:801-814` at `db91886c`) (review II, phase 6a, R2-P6-fsm-1210).
- **A SimpleAnnouncement schedules without a provider.** It resets the retry count and keeps a running timer, as matter.js: the announcement schedules with no provider (`packages/node/src/behaviors/ota-software-update-requestor/OtaSoftwareUpdateRequestorServer.ts:413-416` at `db91886c`), which resets the retry count (`:491-493`) and keeps a running timer (`:495-499`) (review II, phase 6a, R2-P6-fsm-1211).
- **Any fabric's AnnounceOTAProvider chooses the next provider the node-wide requestor queries.** Mirrored, both references: matter.js builds the provider from the accessing fabric (`packages/node/src/behaviors/ota-software-update-requestor/OtaSoftwareUpdateRequestorServer.ts:384-391` at `db91886c`), adds it to the node-wide list with `previousQueryTimestamp: null` (`:423-438`, `:430`); a non-simple announcement schedules with it (`:408-412`) → `selectedProviderLocation` (`:486-487`) → used once (`:527-531`), else the oldest timestamp across all fabrics (`:535-540`); access "F A" (`ota-software-update-requestor.element.ts:65`). connectedhomeip takes the location from the accessing fabric (`src/app/clusters/ota-requestor/DefaultOTARequestor.cpp:332-334`) and hands it to the driver (`:347`), which, when idle, queries the announced provider (`DefaultOTARequestorDriver.cpp:323-329`) at `6170af84`. Preferring the fabric's own default would be ahead of both (review II, phase 6a, TM R3.5).
- **UnitLocalization refuses a duplicate SupportedTemperatureUnits entry at construction.** The model's text requires unique entries (`packages/model/src/standard/resources/unit-localization.resource.ts:43-45` at `db021da0`); neither matter.js's constraint (`unit-localization.element.ts:30-31`) nor connectedhomeip (`UnitLocalizationCluster.cpp:52-66` at `6170af84`) checks it. Host configuration only, ahead of both (review II, phase 6a, R2-P6-err-1268).
- **A secure session's exchanges end with it.** The bridge's BDX, OTA and Invoke exchanges are keyed by (session id, exchange id) and end when their session closes; they never pass to a later session given the same id, and a session keeps at most 30 exchanges, the least recently active ending — as matter.js, which keys its exchanges by the `Session` object (`packages/protocol/src/protocol/ExchangeManager.ts:569-576` at `db021da0`), so a later session finds none of an earlier one's, closes a force-closed session's exchanges (`packages/protocol/src/session/Session.ts:336-342`), and keeps at most 30 non-closed exchanges per secure session (`ExchangeManager.ts:63`, `:578-595`) (review II, phase 6a, R2-P6-sec-0302, R2-P6-bound-0303). One message per exchange awaits its acknowledgement, as matter.js: a reply or report of an interaction is refused while an earlier message of the exchange awaits its ack (`TrackOnExchange`, `f94b30a3`), as matter.js's `#send` throws "has not been acked yet, cannot send a new message" (`packages/protocol/src/protocol/MessageExchange.ts:701-705`) and a BDX message waits until its predecessor on the exchange is acknowledged, as matter.js's send does not return before its acknowledgement (`:786`, `:822-830`; review II, phase 7, decision 276). On every exchange — interaction, BDX and the unsecured session's PASE and CASE handshakes (decision 277) — the receive side refuses a message that carries no ack while one is owed (`:521-525`) and one whose ack names another counter than the exchange's outstanding message, a standalone ack excepted, which is ignored (`:526-533`, `:528-529`); a refused message is neither acknowledged nor processed (`:553-562`; `ExchangeManager.ts:435-437`, `:538-549`). An acknowledgement clears the outstanding message of its own exchange only (`MessageExchange.ts:521-542`; decision 278).
- **A null written to a non-nullable scalar attribute ends the write interaction with FAILURE.** As matter.js: the write decodes with the attribute's `TlvOfModel` schema, which wraps `TlvNullable` only for a nullable model (`packages/types/src/tlv/TlvOfModel.ts:234-236`, `:242-244`), before the value is assigned (`packages/protocol/src/action/server/AttributeWriteResponse.ts:429`, `:433-434` at `db021da0`); `packages/types/src/tlv/TlvNumber.ts:124-131` throws `UnexpectedDataError` for a TLV null (likewise `TlvBoolean.ts:22-23`, `TlvString.ts:57-58`), which is no `StatusResponseError`: the write's catch rethrows it (`AttributeWriteResponse.ts:452-460`, `:459`) and it ends as StatusResponse FAILURE (`packages/protocol/src/interaction/InteractionMessenger.ts:431`, `:458-460`) — read from the lines, not run. Here through decision 161's `im.UndecodableValue` (`bridge/attribute_value_reader.go`, `126d4566`) (review II, phase 6a, R4-1, owner decision 170).

- **A subscription is persisted after its SubscribeResponse, so a Stop in the same instant loses it.** `bridge/subscribe.go:1062-1071` records the subscription once the response is sent; matter.js's `SubscriptionsServer.#addSubscription` fires on `subscriptionAdded`, which the InteractionServer emits after the SubscribeResponse (`SubscriptionsServer.ts:54`, `:133-140`). Same timing; a test waits until it is stored (report S, question 7).

TLV element codes/widths, bool/null/float shapes, string/octet length forms, tag-encode thresholds; message/protocol header field order and presence rules; Sigma KDF salts/infos/nonces, TBE/TBS tag order, destinationId HMAC, RCAC/chain validation, AddNOC rollback + fail-safe-expiry revert, RemoveFabric teardown; SPAKE2 generators/point validation/Ke/transcript, PBKDF bounds; MRP max-transmissions = 5, nonce layout, standalone-ack delay, session-id collision avoidance; timed-invoke gate, keepSubscriptions purge, sendInterval formula, subscription death after 3 failures; EP0 cluster set, full-family PartsList, ServerList, empty-ACL fail-closed, CAT matching; OnOff toggle semantics, level scaling, thermostat limit-clamp + deadband, measurement null semantics, illuminance formula, cover inversion; mDNS operational instance-name + subtypes + TXT formats, multi-fabric boot re-announce, AddNOC-time publish, shutdown withdraw.
