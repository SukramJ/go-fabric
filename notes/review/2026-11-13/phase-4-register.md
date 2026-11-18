# Phase 4 — the dispositions

The commit ids below are those of the branch of this phase's pull request —
"the phase 4 pull request" where a number would stand (`review2/p4`,
`9abb2e9e..308a8183`, 256 commits). If that pull request merges squashed, as
phase 3's did, its single commits stay retrievable through the pull request's
reference. Items that are still open after this branch and describe a weakness
a party on the network could use are held back with their text until the pull
request that fixes them (owner decision 25); the table carries their id and
phase only, and "Counts" says how many. A parity gap that is no such weakness
is public when open. What this branch fixed is public as what the code does
now.

Phase 4 was the parity review: seven reviewers each compared one area's
behaviour with matter.js at `db021da0`, and with connectedhomeip at `6170af84`
where matter.js leaves a behaviour open (owner decision 58) — Read, Subscribe,
reports and events (`imread`); Write, Invoke, Timed, groups and the status
codes (`imwrite`); the core servers that hold credentials and access
(`coresec`); the other system servers (`coresys`); lighting, level, scenes and
switch (`applight`); thermostat, lock, covering and the rest (`appclim`);
certifiability, the open parity findings and what earlier phases had placed
here (`cert`). The review ran as seven units and three verifiers on the
session's model; the fixes were written by Opus agents, and the reads of the
security paths, the citation passes and the registers by the session's model,
as owner decision 24 sets (the coordinator's statement; the result files do
not name a model).

One row per finding of five reads — the seven units (81 findings), the diff
review of the first two fix waves (26), the two reads of what the fix authors
had noticed (24), the review of the rework (11) and the final review with the
last guards (17) — one per item of earlier phases a unit was given and judged,
one per owner decision of this phase, one per item the fix agents, the
reviews, the citation passes and the coordinator noticed and did not fix where
it was noticed, and one per group of units for what its reviewers did not
read. Sources: the two result files of the review workflow, the reports of the
two verifying reads, of the rework's review and of the final review, the 40
fix notes (`phase-4-fix-*.md`), the reports of the `by_design` re-read and of
five citation passes, the coordinator's open list, `decisions.md` (owner
decisions 58 to 85), the two parity registers at `308a8183`, and `git log
9abb2e9e..308a8183`. The notes are not part of this change; one that mentions
an item held back follows with the pull request that fixes that item (owner
decision 25). The guards of the branch are in the pull request's body, not
here.

How a row was placed — by rule:

- **fixed `<commit>`** — the commit is in `9abb2e9e..308a8183`, and its
  subject names the ID or the coordinator's list gives it. The fix notes carry
  the ids the commits had before they were rebased onto the branch; the
  branch's commit was found by the ID in its subject. What a fixed row says
  the code does now is the commit's own subject, written out — the subjects of
  this branch state what holds, not what was wrong — and was not checked
  against the code for this file. For every finding of high or medium
  severity, the reviewer's or the verifier's, the files the commit touches
  were read off `git show --stat`: each row's first commit touches the file
  the finding names. "corrected by" names the finding of a later read that
  found the fix wanting and the commit that repaired it; the pairing is the
  register editor's, by the finding a later one names or by the mechanism it
  names and the commit that brought it.
- **parity: a recorded divergence, by_design `<row>`** — the module keeps a
  behaviour that differs from the reference, without a change of behaviour on
  this branch, and the entry that records it is in `notes/parity/by_design.md`
  at `308a8183`: decided by the owner where a decision is named, written by
  the notes pass from the fix note's text otherwise. A `by_design` entry named
  after another disposition is in the file at `308a8183` as well.
- **parity, no change** — the behaviour is matter.js's at the pin; the lines
  were re-read for this file.
- **struck: `<reason>`** — the reason is the fix note's, a later reviewer's or
  the coordinator's.
- **open: `<findings row>`** — the item is a row of
  `notes/parity/matter_behaviour_findings.md` at `308a8183`. Where a row this
  phase wrote names its phase, the disposition is that phase; "open" is for
  the row that names none, and for the older row a unit read and the branch
  left.
- **phase N: `<what>`** — placed by the plan's cut (`README.md`: 4 parity, 5
  architecture, tests, docs and the API bundle, 6 the full pass). The
  placement is the findings row's or the coordinator's where one of them names
  it, the register editor's otherwise, and then the row says so; the
  coordinator may move it. "phase 4: the certification ports" is this phase's
  second pull request (owner decision 68); "phase 4: not done" is work of this
  phase that the branch does not hold.
- **held back** — the coordinator's list marks the item so, or the register
  editor judged that the open item tells a party on the network what it can
  use; "Not placed, not verified" names the rows of the second kind.

Severity is the verifier's where a finding was verified — every high and
medium of the units' review (15) and of the diff review (4) — with the
reviewer's in brackets where the verifier lowered it (2 and 2); a low or note
carries the reviewer's severity and was not verified before its fix. The later
reads had one reader per unit and no verifier; "PLAUSIBLE" in a row is that
reader's own word. "Re-read" after a reference's lines means read for this
file in the exported tree of the pin (matter.js `db021da0`, connectedhomeip
`6170af84`); a reference statement without it is its source's — a commit
subject's among them.

## The findings

### The seven units

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P4-imread-01 | high | fixed `980cd668`, corrected by R2-P4-dr-im-07 `9d2acfb1` (the comments) | `im/read.go` — a fabric-sensitive event record reaches only its own fabric, on a PASE session too. The phase's first fabric-data finding; the others are R2-P4-nv-01, -01b and -01c and R2-P4-rr-sec-01 |
| R2-P4-imread-02 | medium | fixed `ee9d4cfa` | `endpoint/dispatcher.go` — a status answers a fully concrete read path only |
| R2-P4-imread-03 | medium | fixed `ee9d4cfa` | `endpoint/dispatcher.go` — a status answers a fully concrete read path only |
| R2-P4-imread-04 | medium | fixed `d169f125`, corrected by R2-P4-dr-sub-02 `4b5fbf7f`, R2-P4-dr-sub-04 `d4cccefc` and R2-P4-nv-07 `80466d07` | `bridge/ackpump.go` — a subscription report the peer never acknowledges is one failed update — requeued, counted, the subscription ends at the third, the peer's sessions stay. Owner decision 60 |
| R2-P4-imread-05 | medium | fixed `2be53983` | `im/subscription/manager.go` — a Reassemble that removes an endpoint ends no subscription; the paths naming it select nothing, Manager.CloseEndpoint is deprecated. Owner decision 61; `BD-Matter-Reassemble-Closes-Removed` is retired (`c9e4fb7c`) |
| R2-P4-imread-06 | low | fixed `936b0885`, corrected by R2-P4-dr-im-06 `d50f4058` | `im/read_expansion.go` — the statuses of a read's or priming report's concrete paths precede every value. Owner decision 62. What the fix left: `F-db021da0-32`, phase 5 |
| R2-P4-imread-07 | low | fixed `959f13f4` | `im/read.go` — an event path's node is evaluated as an attribute path's — a concrete path naming another node answers UNSUPPORTED_NODE, a path naming another node selects no event, establishes no subscription and matches no later event |
| R2-P4-imread-08 | low | fixed `78f195c8`, `784648bd` | `bridge/subscribe_dispatch.go` — an event path naming an event the cluster lacks establishes no subscription; an event path naming no event exists only on a cluster that has an event, so a subscription whose only event path is a wildcard on clusters without events is refused INVALID_ACTION |
| R2-P4-imread-09 | low | fixed `9ddef1ac` | `bridge/subscribe.go` — a subscription change report carries values only |
| R2-P4-imread-10 | low | fixed `9d8c4603` | `im/subscription/subscription.go` — a change report lists its paths in the priming report's order; one function orders a cluster's attributes. Owner decision 62 |
| R2-P4-imread-11 | low | fixed `bcfdfe61` | `im/subscription/manager.go` — a subscription is granted a MaxInterval of at least 2 s and reports no faster |
| R2-P4-imread-12 | low | fixed `c2ecdc6b`, `dc11f3d5`, corrected by R2-P4-dr-im-03 `be3a0843`; by_design `BD-Matter-AttributeOrder-ModelOrder` for what owner decision 83 leaves | `endpoint/dispatcher.go` — the snapshot carries each cluster's attribute order as matter.js serves it — attributeOrder from matter.js's Scope.membersOf, schema.AttributeOrder; a cluster's attributes are reported in matter.js's order — Read, priming and change reports alike. Owner decision 62; the snapshot's hash moved with the extract. `F-db021da0-16` is closed with it. Reported PLAUSIBLE |
| R2-P4-imread-13 | note | fixed `736fec1c` (after the tip this register counts from) | `notes/parity/by_design.md` — `L10-D05` and its detail say that a subscription's report, with data or as a keep-alive, asks for the peer's StatusResponse in matter.js and connectedhomeip as here: no divergence; what still differs is when an update counts as delivered (F-db021da0-36) |
| R2-P4-imwrite-01 | medium (high) | phase 5: `F-db021da0-29`, with the peer path's move onto `client/` (owner decision 76) | `im/invoke.go`, `bridge/ota_provider_link.go` — the gate came with `9f15e9e8` and `46c950f4` and left with `9d47f6f7`: a command with the Large Message quality runs over the transport its request arrived on, and this node sends one over the session it holds. With the gate a camera's signals between two nodes of this module no longer arrived, because the bridge's peer sender opens no TCP connection. A parity gap, public; `9fe486d9` bounds an exchange over TCP by what the peer stated |
| R2-P4-imwrite-02 | low (medium) | fixed `3f12d831`, `4a61bb83` | `im/write.go` — a write path naming another node is not written — UNSUPPORTED_NODE on a concrete path, dropped on a wildcard or group path; a write that ends at a data element is answered with its StatusResponse on the client's own server too, and a group write's path naming another node writes nothing |
| R2-P4-imwrite-03 | low | fixed `0127adc2` | `im/invoke.go` — an invoke at a path that does not exist answers the existence status before the fabric-scope and timed gates |
| R2-P4-imwrite-04 | low | fixed `f8ff4777` | `im/write.go` — a Write or Invoke request without a mandatory field is refused with INVALID_ACTION |
| R2-P4-imwrite-05 | low | fixed `65a6da92`, corrected by R2-P4-dr-im-05 `4150b75c` | `im/write.go` — a write path with a numeric ListIndex ends the interaction with INVALID_ACTION where its value would be written |
| R2-P4-imwrite-06 | low | fixed `9beab127` | `bridge/receive_dispatch.go` — an expired timed window answers TIMEOUT whatever the request's Timed flag, a chunked write in a running window and a second TimedRequest on it INVALID_ACTION. The row shares its commit with R2-P4-imwrite-07 and -16 |
| R2-P4-imwrite-07 | low | fixed `9beab127` | `bridge/receive.go` — an expired timed window answers TIMEOUT whatever the request's Timed flag, a chunked write in a running window and a second TimedRequest on it INVALID_ACTION |
| R2-P4-imwrite-08 | low | fixed `3960baf8` | `im/invoke.go` — a wildcard-endpoint command whose fields do not decode answers the decode status once per endpoint it targets |
| R2-P4-imwrite-09 | low | parity: a recorded divergence, by_design `BD-chip-GroupWrite-ListAppendApplied` (owner decision 63) | `im/group.go` — a group write that appends to a list is applied, as connectedhomeip applies it; `38655f97` corrects the comment. Corrected by R2-P4-dr-im-01 `19e10ab7`: the append reaches every member endpoint |
| R2-P4-imwrite-10 | low | fixed `2e490df6` | `endpoint/dispatcher.go` — an ADD to an attribute that is not a list answers INVALID_ACTION |
| R2-P4-imwrite-11 | low | fixed `9d16fe1e` | `bridge/receive_dispatch.go` — a chunk that continues a chunked write is not judged as a new request — its Timed flag is not looked at, a SuppressResponse on it ends the interaction unapplied |
| R2-P4-imwrite-12 | low | fixed `fd143e04` (owner decision 78), corrected by R2-P4-fr-ver-01 `18910901`, R2-P4-fr-im-01 `4f789f0d` and their rounds | `endpoint/dispatcher.go`, `contract/matter.go` — a server says whether a controller's write changed anything (contract.ChangeReportingWriter); a write that changed nothing advances no DataVersion and is reported to no subscriber, every kit server gives the verdict, a host server without it advances as before. `F-db021da0-18` and `F-db021da0-2` are closed with it (`06368f7d`). What the verdict and its wiring leave open: `F-db021da0-38` to `-47` |
| R2-P4-imwrite-13 | low | parity: a recorded divergence, by_design `BD-Matter-Invoke-OversizedResponseResourceExhausted` | `bridge/receive_dispatch.go` — an invoke response too large for one message is answered RESOURCE_EXHAUSTED after the command ran; `b7ecbfca` says in the comment that the answer is this module's own |
| R2-P4-imwrite-14 | low | fixed `daa4e3b8`, corrected by R2-P4-dr-im-04 `210d61b8` (the test) | `bridge/receive.go` — an IM request that is not an initiator message, on an exchange this node did not open, is acknowledged and not served |
| R2-P4-imwrite-15 | low | fixed `36afda9d` | `bridge/receive_dispatch.go` — a write path with WildcardPathFlags ends the interaction when it is reached, the elements before it written |
| R2-P4-imwrite-16 | note | fixed `9beab127` | `bridge/receive.go`, `im/timed.go` — comments only: the two say TIMED_REQUEST_MISMATCH, which the code answered before; the commit is R2-P4-imwrite-07's |
| R2-P4-imwrite-17 | note | fixed `b7ecbfca` | `im/write.go` — the provenance comments of the write and invoke paths name the lines that hold at the pins; an oversized invoke response is this module's own answer |
| R2-P4-coresec-01 | high | fixed `c770e982`, `84d092cc`, corrected by R2-P4-dr-core-02 `14891fcb` | `cluster/core/group_key_management.go` — a GroupKeyMap entry cannot map key set 0, the IPK — ConstraintError; a controller's written value is held to the cluster's definition in the dispatcher before any server runs — the hand-written ACL and GroupKeyMap forms through a declared field map — and a deprecated enum value stays writable, as matter.js compiles "D". Owner decision 64: the dispatcher's check is the one that holds the class |
| R2-P4-coresec-02 | medium | fixed `64d45880` | `cluster/core/operational_credentials.go` — UpdateFabricLabel answers LabelConflict for the empty label another fabric carries |
| R2-P4-coresec-03 | medium | fixed `38569584` | `node/config.go` — GeneralCommissioning LocationCapability defaults to IndoorOutdoor, as matter.js, and Commissioning.IndoorOnly states Indoor. An announced break: the zero value of `LocationCapability` reads IndoorOutdoor |
| R2-P4-coresec-04 | low | fixed `621a1614`, `3f3b294c`, `2561b260` | `cluster/core/operational_credentials.go` — AddNOC checks in matter.js's order — root, CSR, for-UpdateNOC, TableFull, admin subject, key, chain — and refuses an over-long NOC or ICAC with ConstraintError; AddNOC's IPK, NOC and ICAC lengths are judged before the AdminVendorId and fail-safe checks, as matter.js validates the request before the handler. Owner decisions 64 and 65. `ccf200d4` moves the reference bridge's test onto the new order. The same order on UpdateNOC and the CAT rule: R2-P4-nv-08, R2-P4-nw-07a |
| R2-P4-coresec-05 | low | fixed `be99093b` | `cluster/core/operational_credentials.go` — TrustedRootCertificates lists a pending root after the installed fabrics' roots |
| R2-P4-coresec-06 | low | fixed `de142c67`, corrected by R2-P4-dr-core-04 `7798e358` | `cluster/core/operational_credentials.go` — the operational key pair belongs to the fail-safe context — every CSRRequest in it signs over the same key. Owner decision 65 |
| R2-P4-coresec-07 | low | fixed `699fa852` | `cluster/core/operational_credentials.go` — UpdateFabricLabel answers a label over 32 bytes with ConstraintError, as matter.js's request validation. Owner decision 65 |
| R2-P4-coresec-08 | low | parity: a recorded divergence, by_design `BD-Matter-ACL-NoAdministerEntryAtStart` (owner decision 79) | `cluster/core/access_control.go` — the module grants no privilege unasked; `F-db021da0-26` is closed with the entry |
| R2-P4-coresec-09 | low | fixed `e334abc5` | `cluster/core/access_control_auxiliary.go` — AuxiliaryAcl splits an entry by the configured subject and target limits |
| R2-P4-coresec-10 | low | fixed `54d17cd0` | `cluster/core/groupcast.go` — a GroupcastTesting event names the group's own address only for a group with endpoints in the group table. The rejection report as a whole: R2-P4-nw-07b |
| R2-P4-coresec-11 | note | parity: a recorded divergence, by_design `BD-Matter-ACLRemovedEventEntry` | `cluster/core/access_control.go` — a Removed event carries the writing fabric's own removed entry, where matter.js at the pin reads the unfiltered list |
| R2-P4-coresys-01 | medium | fixed `da0d710a` | `cluster/core/icd_management.go` — ICDManagement RegisterClient with a ClientType its enum does not define is refused with CONSTRAINT_ERROR and registers nothing. Owner decision 64 |
| R2-P4-coresys-02 | medium | fixed `aab78378` | `node/node.go` — Config.BootReason states the reported BootReason, Unspecified by default, and a start into the update the OTA requestor recorded is SoftwareUpdateCompleted. Owner decision 71 |
| R2-P4-coresys-03 | low | fixed `1b360e36` | `cluster/core/general_diagnostics.go` — PayloadTestRequest answers RESOURCE_EXHAUSTED when its encoded response exceeds the payload one message of the request's exchange carries — 1178 bytes over UDP, the connection's and the peer's bound over TCP — and 1100 bytes without that bound |
| R2-P4-coresys-04 | low | fixed `628f7ec7` | `cluster/core/basic_information.go` — BasicInformation defaults HardwareVersionString to the decimal HardwareVersion and serves ProductLabel as the ProductName |
| R2-P4-coresys-05 | low | open: `F-db021da0-28`, which names no phase | `cluster/core/bridged_device_basic_information.go` — NodeLabel and SerialNumber are served whatever the host set; no interop reason is in the code, and the owner was not asked |
| R2-P4-coresys-06 | low | fixed `35aa862a` (as R2-P4-nv-05b) | `cluster/measurement/measurement.go`, `bridge/subscribe.go` — the measurement projection reports its quieter attributes through matter.js's throttle, ten seconds for PowerSource's battery estimates. The generated server's path stays: `F-db021da0-22`, which names no phase. Reported PLAUSIBLE |
| R2-P4-coresys-07 | low | fixed `660c51db` | `cluster/measurement/measurement.go` — the battery PowerSource's Description is "Battery power", matter.js's default |
| R2-P4-coresys-08 | low | fixed `6fe13fae` | `cluster/core/icd_management.go` — ICDManagement serves UserActiveModeTriggerInstruction only when the host gave one |
| R2-P4-coresys-09 | note | fixed `c9e4fb7c` | `notes/parity/by_design.md` — `BD-Matter-ICD-ZeroBackoffIsIdleDuration` says the schema default is 1 and that the facade's zero means IdleModeDuration, a convenience of the facade |
| R2-P4-coresys-10 | note | open: `F-db021da0-27`, which names no phase | `cluster/core/ota_provider.go` — a second QueryImage is judged by the transfer's last block, not by the two sessions' activity; the invoke context carries no last-activity time |
| R2-P4-coresys-11 | note | fixed `dccb0246` | `cluster/core/basic_information.go` — the BasicInformation comment states that a LocalConfigDisabled write is refused |
| R2-P4-applight-01 | low | fixed `390a0893`, corrected by R2-P4-dr-app-04 `82065176` | `endpoint/types.go` — the server owns its scene port — a command the ExecuteIfOff gate refuses keeps the scene valid, and a scene recalls only a level within MinLevel..MaxLevel |
| R2-P4-applight-02 | low | fixed `8a074a98` | `cluster/levelcontrol/transitions.go` — a rated Move at a null CurrentLevel succeeds on the engine path as on the native one |
| R2-P4-applight-03 | low | parity: a recorded divergence, by_design `BD-Matter-Transitions-CyclicAtBound` (owner decision 66) | `cluster/transition/transition.go` — the module's behaviour stays; `bd0b08d8` corrects the comment that gave matter.js as the reason |
| R2-P4-applight-04 | low | fixed `e270bffe`, `e671095b`, `68bda7ba`, corrected by R2-P4-dr-app-01 `a7cf8bf8` | `cluster/levelcontrol/levelcontrol_server.go` — the DataVersion advances only when a command or a write changed an attribute; ChangeToMode advances the DataVersion only when CurrentMode changed; Step advances the DataVersion only when the fan changed. The DataVersion on a controller's unchanged write: owner decision 78, R2-P4-imwrite-12 |
| R2-P4-applight-05 | low | parity: a recorded divergence, by_design `BD-Matter-Scenes-FabricSceneInfoForEveryFabric` (owner decision 66) | `cluster/core/scenes_management.go` — the module's behaviour stays; `bd0b08d8` corrects the comment |
| R2-P4-applight-06 | low | fixed `7f5cf01e` | `cluster/core/scenes_management.go` — a scene command that arrives as a group message with GroupID 0 is refused with INVALID_COMMAND |
| R2-P4-applight-07 | low | fixed `390a0893` | `cluster/core/scenes_attributes.go` — the server owns its scene port — a command the ExecuteIfOff gate refuses keeps the scene valid, and a scene recalls only a level within MinLevel..MaxLevel |
| R2-P4-applight-08 | low | fixed `fb5cd440`, `b2eb4a05`, corrected by R2-P4-dr-app-05 `fcf4c0e9` (the configuration's doc) | `cluster/light/colorcontrol_server.go` — StartUpColorTemperatureMireds is applied at start-up unless the server is bridged or the node booted from a software update; the kit's ColorControl server is marked Bridged, as its OnOff and LevelControl servers are |
| R2-P4-applight-09 | note | fixed `c9e4fb7c` | `notes/parity/by_design.md` — the two sentences of `BD-chip-Harness-ModelOverCase` on the XY to HS conversion and on the FanMode of a speed mode say what the code does |
| R2-P4-applight-10 | note | fixed `0481d0e6` | `notes/parity/by_design.md`, `cluster/fan/fancontrol_server.go` — `BD-Matter-FanControlCouplingInServer` and the package doc name `FanControlCluster.cpp` with the lines read; "recalled rather than read" is gone from both (a search at `308a8183`) |
| R2-P4-appclim-01 | medium | fixed `1cf1aa2e` | `cluster/lock/doorlock_server.go` — a lock command's PINCode is authorized whatever the features. Owner decision 67 |
| R2-P4-appclim-02 | medium | fixed `8cd49467`, corrected by R2-P4-dr-app-07 `e30a7686` | `cluster/measurement/measurement.go` — an unobserved OccupancySensing source reads Occupancy 0, and no projection reads null where the element has no quality X |
| R2-P4-appclim-03 | low | fixed `9e02f72c` | `cluster/thermo/thermostat_server.go` — a limit write whose reconcile moves the setpoint in effect ends the active preset |
| R2-P4-appclim-04 | low | fixed `e4e05d94` (owner decision 73) | `cluster/cover/windowcovering_server.go` — an unknown position reads null on both position attributes of its axis and on its target until the host reports one. `1f16974b` repairs its test. `BD-Matter-WindowCoveringPercentNonNull` is retired; `BD-Matter-WindowCovering-UnknownPositionTravelStopped` records what a simulated travel does then |
| R2-P4-appclim-05 | low | phase 5: `F-db021da0-21`, with the API bundle | `cluster/thermo/thermostat_server.go` — LocalTemperatureCalibration is served and not applied; matter.js applies it on its measured-temperature input, which needs a new exported setter here |
| R2-P4-appclim-06 | low | fixed `4e4153dc` (as R2-P4-nv-05a) | `cluster/measurement/measurement.go` — the projected ElectricalEnergyMeasurement lists CumulativeEnergyMeasured and the event is emitted when the cumulative energy changes. by_design `BD-chip-ElectricalEnergy-CumulativeEventOnChange` |
| R2-P4-appclim-07 | note | fixed `80a87309` | `notes/parity/matter_behaviour_findings.md` — the row on request fields outside their constraint is closed with owner decision 64 (`27cf0e19`, `2561b260`, `84d092cc`); what matter.js checks for DoorLock beyond bounds is `F-db021da0-20`, which names no phase |
| R2-P4-cert-01 | medium | fixed `7a83b14b`, `2a23725c`, corrected by R2-P4-dr-app-03 `b1e2e556`; phase 4: the certification ports | `docs/certifiability.md`, `internal/chiptool/` — every matter.js certification case at the snapshot's pin is run by the Go-TH leg or classified with what a port needs, and TestCertifiabilityDocument holds the list to the pin. Owner decisions 68 and 82: 44 cases have the status "not ported yet", and their ports are a second pull request of this phase — open: phase 4, the certification ports |
| R2-P4-cert-02 | low | fixed `0bfb32ef` | `bridge/securechannel.go` — a Pake1, Pake3, Sigma3 or Sigma2_Resume no handshake began is acknowledged and no more. Owner decision 70. N3-cite-2 of phase 3 |
| R2-P4-cert-03 | low | fixed `b77161e8` | `cluster/core/network_commissioning.go` — an Ethernet InterfaceEnabled write that changes the value advances the DataVersion and reports Networks |
| R2-P4-cert-04 | low | fixed `6aace1cd`, `c1a235c7` | `cluster/core/network_commissioning_wifi.go` — a write of the Wi-Fi InterfaceEnabled switches the interface through a driver that implements contract.WiFiInterfaceSwitcher, and without one false answers INVALID_ACTION and the attribute stays true; the Wi-Fi driver the node hands the cluster forwards contract.WiFiInterfaceSwitcher to the host's driver, and answers errors.ErrUnsupported without it. Owner decision 80; by_design `BD-chip-WiFiInterfaceEnabled-DriverSwitch`. N3-finnet-1 of phase 3. `F-db021da0-25` is closed with it |
| R2-P4-cert-05 | low | parity: a recorded divergence, by_design `BD-Matter-SessionRolloverClose` (owner decision 75) | `bridge/reply.go` — the device closes a session at its counter's rollover at once; the entry gives the reason. N-finctl-1 and N-finctl-2 of phase 2 |
| R2-P4-cert-06 | note | parity: a recorded divergence, by_design `BD-Matter-FactoryResetEndsTheNode` (a paragraph added, `c9e4fb7c`) | `node/factory_reset.go` — the fabrics leave while the node is still online, after connectedhomeip. N-shut-2 of phase 2 |
| R2-P4-cert-07 | note | fixed `378a5ae5`, `107ce30f`, `ad1a270e`, corrected by R2-P4-dr-app-06 `8419953a` and R2-P4-dr-app-08 `ae232fe8` (the test) | `docs/certifiability.md` — PushAvStreamTransport judges empty VideoStreams/AudioStreams lists and MotionSensitivity itself, as connectedhomeip does, so TC-PAVST-2.3 step 25, 2.4 step 5, 2.6 step 8 and 2.11 step 21a get chip's answers; a NumberOfRinses write SupportedRinses does not list is INVALID_IN_STATE whether or not the enum defines it, as connectedhomeip answers TC-WASHERCTRL-2.2 step 6. Owner decision 69. `378a5ae5` had filed the five cases as class (a) first. They have the status "not ported yet" until a run (owner decision 82, `2a23725c`) |
| R2-P4-cert-08 | note | fixed `51ce8945` | `docs/certifiability.md` — the certifiability page says the SU cases pass against the daemon's --ota-harness applier, and that a product's applier and restart are the product's |
| R2-P4-cert-09 | note | fixed `3d71eaa3` | `docs/guides/limits.md` — the limits page names the commit its matter.js references were read at and the snapshot's pin apart. S-22 of phase 0 |
| R2-P4-cert-10 | note | fixed `68ef6703` | `docs/certifiability.md` — the certifiability page names the snapshot's pin db021da0 and calls no composition certified |
| R2-P4-cert-11 | note | fixed `80a87309` | `notes/parity/matter_behaviour_findings.md` — `F-db021da0-17` no longer says a DataVersion may survive a restart in matter.js; the row keeps the zero state. D-ctr-6 and N-ctr-3 of phase 2 |
| R2-P4-cert-12 | note | parity: a recorded divergence, by_design `BD-Matter-Invoke-HoldsHostReports` | `bridge/receive_dispatch.go` — while a command runs, the change reports of its cluster are held and the version advances once. N-rw3req-1 of phase 2. Reported PLAUSIBLE; the part that was plausible is settled by reading, not by a run |

### The diff review of waves 1 and 2

Four reviewers read the first two fix waves as a diff (`9abb2e9e..1f16974b`):
`dr-im`, `dr-sub`, `dr-core`, `dr-app`. Its four findings of medium severity
were verified; none was of high severity, one was refuted by its fix agent.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P4-dr-im-01 | medium | fixed `19e10ab7` | `endpoint/dispatcher.go` — a list ADD resolved to several endpoints appends the request's own element on every one — a group write chunked into a REPLACE and ADDs ends equal on every member endpoint, as connectedhomeip's WriteHandler.cpp:555-559 hands each mapped endpoint the request's value |
| R2-P4-dr-im-02 | low | fixed `210d61b8` | `bridge/receive.go` — a reliable Write or Invoke whose SuppressResponse leaves it without a reply is acknowledged when it ends, as matter.js's exchange close sends the owed standalone ack (MessageExchange.ts:1150-1156), and the ignored non-initiator request's acknowledgement is asserted |
| R2-P4-dr-im-03 | low | fixed `be3a0843` | `endpoint/dispatcher.go` — a wildcard read of a cluster without attributes of its own lists the globals in the model's order, as its priming and change reports do and as matter.js's ProtocolService.ts:449-522 builds every cluster's list |
| R2-P4-dr-im-04 | low | fixed `210d61b8` | `bridge/imwrite_parity_matterjs_test.go` — a reliable Write or Invoke whose SuppressResponse leaves it without a reply is acknowledged when it ends, as matter.js's exchange close sends the owed standalone ack (MessageExchange.ts:1150-1156), and the ignored non-initiator request's acknowledgement is asserted |
| R2-P4-dr-im-05 | note | fixed `4150b75c` | `im/write.go` — a write path with a numeric ListIndex to an attribute with the timed quality outside a timed interaction answers NEEDS_TIMED_INTERACTION on a concrete path and is skipped on a wildcard one before the index ends the interaction, as matter.js checks timed before writeValue (AttributeWriteResponse.ts:243-247, :343-345, :419-424) |
| R2-P4-dr-im-06 | note | fixed `d50f4058` | `im/read_expansion.go` — whether a Subscribe matches anything is decided by reading its paths in turn up to the first readable attribute, without reading every concrete path up front for the order of the priming report's statuses. What stays, the second read in the report pass: `F-db021da0-32`, phase 5 |
| R2-P4-dr-im-07 | note | fixed `9d2acfb1` | `bridge/subscribe.go` — the event-authorization comments state that a PASE session or a dispatcher without an ACL checker passes the ACL gate and that the fabric-sensitive drop applies to every session |
| R2-P4-dr-sub-01 | low (medium) | fixed `565033a8`, corrected by R2-P4-rr-im-04 `e8012054` | `bridge/subscribe.go` — the throttle of a projected quieter path outlives a re-wiring that serves the path again — a held change is reported when its interval is up and the interval runs on — and a torn-down wiring reports nothing |
| R2-P4-dr-sub-02 | medium | fixed `4b5fbf7f`, `dc15666b`, corrected by R2-P4-rr-im-02 `927d30b0`; by_design `BD-Matter-Subscription-ReportsOverlap` | `bridge/subscribe.go` — reports sent while an earlier one awaits its acknowledgement belong to that update, so their failures in one retransmission window count once and three failed updates take three windows in a row, as matter.js serialises its updates; the log line of a failure not counted again carries only keys of the observability table |
| R2-P4-dr-sub-03 | low | fixed `5d707c2f`, corrected by R2-P4-fr-im-02 `8ff6c2d0` | `bridge/subscribe.go` — a change of a projected quieter attribute advances its cluster's DataVersion at the change, and only the report waits for the throttle, as matter.js advances the version at the commit |
| R2-P4-dr-sub-04 | low | fixed `d4cccefc` | `bridge/subscribe.go` — a subscription's count of failed updates changes under one lock, so a delivered update taken at the moment a failed one is counted is never overwritten. Reported PLAUSIBLE |
| R2-P4-dr-sub-05 | low | fixed `565033a8` | `bridge/subscribe.go` — the throttle of a projected quieter path outlives a re-wiring that serves the path again — a held change is reported when its interval is up and the interval runs on — and a torn-down wiring reports nothing |
| R2-P4-dr-sub-06 | low | fixed `9ccee2ec` | `bridge/case_sigma3_after_completion_test.go` — the Sigma3-after-completion tests fail when a reply waiting for its acknowledgement is taken away, and hold the success report in retransmission across a Sigma3 that is not the completing one |
| R2-P4-dr-sub-07 | note | fixed `1159629b` | `bridge/ota_provider_link.go` — largeMessageLink's comment names its pick, the live TCP-capable session of the highest id, and how it differs from matter.js's newestSession. A comment; the pick itself is `F-db021da0-34`, phase 5. Reported PLAUSIBLE |
| R2-P4-dr-core-01 | low | fixed `d9b694c5` | `node/ota_provider.go` — GrantFabric's documentation says a withdrawn grant removes no ACL entry — what a granting start or an earlier version wrote stays until the fabric's administrator removes it — and a test holds it. No entry is removed and none is logged at start: the node cannot tell an entry it wrote from one an administrator wrote (N4-rwcore-1) |
| R2-P4-dr-core-02 | low | fixed `14891fcb`, corrected by R2-P4-rr-sec-07 `2d5bb41e` (the doc comment); by_design `BD-Matter-GKM-StoredKeySet0Removed` | `cluster/core/group_key_management.go` — a stored GroupKeyMap entry that maps a group to key set 0 is removed, and logged, when GroupKeyManagement is constructed — it is not served and no list append of its fabric is judged with it |
| R2-P4-dr-core-03 | low | fixed `ccf76cee` | `node/commissionable_device_type.go` — an endpoint whose device type the schema snapshot does not hold is of the simple class, and the commissionable record carries its type, as matter.js's EndpointType defaults the class to Simple |
| R2-P4-dr-core-04 | note | fixed `7798e358`, corrected by R2-P4-rr-sec-06 `fc4c020d` | `cluster/core/operational_credentials.go` — CSRRequest draws the fail-safe's key pair and records its CSR under nocMu, one with the NOC commands, the commit and the rollback — a fail-safe that ends during the request leaves no key pair behind. Reported PLAUSIBLE |
| R2-P4-dr-app-01 | low (medium) | fixed `a7cf8bf8` | `cluster/levelcontrol/transitions.go` — under the engine each report of RemainingTime advances the server's DataVersion once, as matter.js's quiet observer advances it for the dynamic remainingTime |
| R2-P4-dr-app-02 | low | struck: refuted — matter.js's test node leaves the status too | `cluster/cover/windowcovering_server.go` — `TestWindowCoveringServer.ts:104-110` returns before it clears the move (re-read) |
| R2-P4-dr-app-03 | low | fixed `b1e2e556` | `internal/chiptool/matterjs_cert_cases_test.go` — with the Go-TH registry compiled in, a matter.js case counts as run only while a registered port runs it, the golden goCert names exactly the registered ports, and a golden rewrite drops an entry no port has |
| R2-P4-dr-app-04 | low | fixed `82065176` | `cluster/levelcontrol/scenes.go` — a command the host replaced through Handle ends the current scene when its handler succeeds, so the server's claim to invalidate holds for it as matter.js's command method invalidates before the logic a host overrides. What `Handle` replaces in each server: `F-db021da0-31`, phase 5 |
| R2-P4-dr-app-05 | low | fixed `fcf4c0e9` | `cluster/light/colorcontrol_server.go` — the ColorControl configuration says the colour temperature the server is constructed with reaches no writer, as matter.js's initializeColorTemperature writes state only, and the host applies it |
| R2-P4-dr-app-06 | low | fixed `8419953a` | `cluster/camera/pushtransport.go` — PushAvStreamTransport stores an empty VideoStreams or AudioStreams list absent, in the allocate response, CurrentConnections and after a modify, as connectedhomeip's TransportOptionsStorage does. Reported PLAUSIBLE |
| R2-P4-dr-app-07 | low | fixed `e30a7686` | `cluster/measurement/measurement.go` — OccupancyChanged follows the readings an occupancy source takes — an unobserved source, which reads 0 on the wire, emits no event and is not remembered |
| R2-P4-dr-app-08 | note | fixed `ae232fe8` | `cluster/appliance/appliance_server_test.go` — the LaundryWasherControls rinse test stores a supported Extra and refuses an unsupported None, so it tells a check that reads the written value from one that falls back to None |

### The two reads of what the fix authors had noticed

What the fix agents of the first waves had noticed beside their findings and
not fixed was read a second time by a verifier, in two batches (`nv-…`,
`nw-…`), before any of it was fixed. The severity is that reader's; a row was
not verified again by another. R2-P4-nv-01d, the other fabric-scoped lists,
came out without a gap and is named in R2-P4-nv-01's row.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P4-nv-01 | high | fixed `8b315ba5`, `e0ac574d` | `cluster/core/messages.go` — an entry of another fabric in Messages is served without its fabric-sensitive fields on an unfiltered read; every fabric-scoped list a node serves is held to serving another fabric's entries without their fabric-sensitive fields. The guard is `node:TestFabricSensitiveFieldsWithheldFromOtherFabrics`. The verifier checked the other fabric-scoped lists and found no gap (R2-P4-nv-01d). The redaction generated from the model: owner decision 85, `F-db021da0-30`, phase 5 |
| R2-P4-nv-01b | high | fixed `75a2a037` | `cluster/core/scenes_management.go` — an entry of another fabric in FabricSceneInfo is served without CurrentScene, CurrentGroup and SceneValid on an unfiltered read |
| R2-P4-nv-01c | low | fixed `926f6154` | `cluster/core/icd_management.go` — an entry of another fabric in RegisteredClients is served as its FabricIndex alone on an unfiltered read |
| R2-P4-nv-02 | medium | fixed `6d02367a` | `bridge/subscribe_dispatch.go` — the priming report answers a concrete attribute path the subject may not read with the access check's status, ahead of the values, as a Read does |
| R2-P4-nv-03 | low | fixed `aab77177`, corrected by R2-P4-dr-sub-06 `9ccee2ec` (the tests) | `bridge/securechannel.go` — a Sigma3 of a completed CASE handshake that is not the completing one is acknowledged and not answered |
| R2-P4-nv-04a | note | fixed `93971953` | `cluster/core/basic_information.go` — an empty NodeLabel is the ProductName, as matter.js defaults it; the node starts without one. The verifier refuted the item as worded and left a stricter contract unrecorded; the fix follows matter.js's default |
| R2-P4-nv-04b | low | fixed `6027d504` | `cluster/core/network_commissioning_wifi.go` — the Wi-Fi NetworkCommissioning DataVersion advances only when InterfaceEnabled changes |
| R2-P4-nv-05a | medium | fixed `4e4153dc` | `cluster/measurement/measurement.go` — the projected ElectricalEnergyMeasurement lists CumulativeEnergyMeasured and the event is emitted when the cumulative energy changes. R2-P4-appclim-06; by_design `BD-chip-ElectricalEnergy-CumulativeEventOnChange` |
| R2-P4-nv-05b | low | fixed `35aa862a`, corrected by R2-P4-dr-sub-01 and -05 `565033a8` and R2-P4-dr-sub-03 `5d707c2f` | `bridge/subscribe.go` — the measurement projection reports its quieter attributes through matter.js's throttle, ten seconds for PowerSource's battery estimates. R2-P4-coresys-06 for the projection; the generated server is `F-db021da0-22` |
| R2-P4-nv-06a | note | fixed `5e59fc38` | `cluster/levelcontrol/transitions.go` — under the engine every change of CurrentLevel advances the server's DataVersion once, a transition's step included |
| R2-P4-nv-06b | low | fixed `a1ca7982` | `cluster/light/colorcontrol_server.go` — the initial ColorTemperatureMireds is cropped to the server's working range, an unset physical maximum read as 0xFEFF |
| R2-P4-nv-06c | low | fixed `f73fe788` | `cluster/core/scenes_attributes.go` — a scene recalls a host's own LevelControl server through MoveToLevel, beside the OnOff and ColorControl command fallbacks |
| R2-P4-nv-07 | low | fixed `80466d07` | `bridge/subscribe.go` — a report whose message is abandoned before its record is stored is a failed update |
| R2-P4-nv-08 | low | fixed `0962c742` | `cluster/core/operational_credentials.go` — UpdateNOC compares the NOC's public key before it verifies the chain, in matter.js's order as AddNOC |
| R2-P4-nw-01 | low | fixed `0a3c5060`, corrected by R2-P4-rr-sec-02 `68afafd9` and R2-P4-rr-sec-04 `6b9ff82e` | `bridge/handlers.go` — the router answers for the singleton CASE handler as for a provider — a completed handshake keeps its record, a Sigma3 or Sigma2_Resume needs a begun handshake, and after establishment only the completing Sigma3 over its route gets the success report again. Since R2-P4-rr-sec-02 a completed record ends with its report, and since R2-P4-fr-sec-02 every record with its handshake |
| R2-P4-nw-02 | note | fixed `0a3c5060` | `bridge/securechannel.go` — the router answers for the singleton CASE handler as for a provider — a completed handshake keeps its record, a Sigma3 or Sigma2_Resume needs a begun handshake, and after establishment only the completing Sigma3 over its route gets the success report again. The record's end: R2-P4-rr-sec-02, R2-P4-fr-sec-02 |
| R2-P4-nw-03 | low | fixed `46f3ea54` | `node/served.go` — ReadAttribute's doc says a fabric-scoped list holds every fabric's entries whole, as matter.js's local actor reads them. The docs page's sentence went with `2979a95b` |
| R2-P4-nw-04 | medium | fixed `5a55de2b`, corrected by R2-P4-rr-sec-01 `0ce59bba` | `cluster/core/access_control.go` — a read with no request behind it sees every fabric's AccessControl Acl and Extension and GroupKeyManagement GroupKeyMap and GroupTable entries whole, as matter.js's local actor; SetCurrentFabric scopes no read. An exported server's unfiltered read changes its return type (CHANGELOG, Changed) |
| R2-P4-nw-05 | low | fixed `1418150e`, corrected by R2-P4-rr-im-03 `51d5b5be` (the contract's doc) | `endpoint/dispatcher.go` — a server without an event lister lists no event — an event path on it, wildcard or named, selects nothing — and PowerSource and ElectricalPowerMeasurement list the none they emit; EventReadResponse.ts's per-event loop is cited at :277-284 |
| R2-P4-nw-06a | low | fixed `2e966300` | `cluster/levelcontrol/typed.go` — a host set advances the DataVersion once however many attributes it changed and not at all when it changed none; an attribute set to its present value is not reported |
| R2-P4-nw-06b | note | fixed `0fefa40f`, corrected by R2-P4-rr-im-01 `53c68d85` | `cluster/modeselect/modeselect_server.go` — a command handed to the host's port advances the DataVersion once, when the host's notifier reports the change while it runs as much as when it does not |
| R2-P4-nw-06c | low | fixed `22b3fc56` | `cluster/fan/fancontrol_server.go` — a FanControl write that leaves every attribute as it was leaves the DataVersion, as Step does |
| R2-P4-nw-07a | low | fixed `64fdd896` | `cluster/core/operational_credentials.go` — AddNOC and UpdateNOC judge the NOC's CASE Authenticated Tags between the decode and the public key, so a bad CAT list is InvalidNOC whatever key the NOC certifies, as matter.js |
| R2-P4-nw-07b | low | fixed `384aa5b7` | `groups/decode.go` — a group message whose key its group is not mapped to fails as a candidate at decode, and every rejected message is reported on the fabric under test with the candidate-fabric result and the owning fabric's address, as matter.js db021da0. `7343faf0` takes the nineteen ported matter.js cases out of the test register's missing list (939 to 920). `groups.NoKeyError` is deprecated. What stays: `F-db021da0-35`, phase 6 |

### The review of the rework

One reviewer per unit read wave 3 and the rework after the diff review as a
whole (`1f16974b..dc15666b`): `rr-sec` the security paths, `rr-im` delivery,
the request path and the servers.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P4-rr-sec-01 | medium | fixed `0ce59bba`; by_design `BD-Matter-FailedReadAnswersItsPath` | `endpoint/dispatcher.go` — a request's read of a fabric-scoped attribute whose scoped read fails answers FAILURE, never the request-free MatterRead. The fabric-data finding of the rework: it needed a store fault, and came with R2-P4-nw-04's fix |
| R2-P4-rr-sec-02 | low | fixed `68afafd9`, corrected by R2-P4-fr-sec-02 and -05 `db6b4648` | `bridge/securechannel.go` — the record of a CASE handshake the singleton handler completed, and its unsecured window, end when the success report is acknowledged or given up, and AttachCaseHandler ends the handshakes the replaced handler began |
| R2-P4-rr-sec-03 | low | fixed `74ae153f` | `bridge/securechannel.go` — a Sigma2_Resume on a completed CASE handshake's key is acknowledged and not answered, and the pending success report stays in retransmission |
| R2-P4-rr-sec-04 | low | fixed `6b9ff82e`, corrected by the last fix group `2645b2b8`; by_design `BD-Matter-CASE-Sigma1OverCompletedAcknowledgedOnly` | `bridge/securechannel.go` — a Sigma1 that begins no handshake leaves a completed CASE handshake's record and its pending success report as they were, through a provider and through the singleton handler. `2645b2b8`: such a Sigma1 is acknowledged and not answered, the handler's failure report not sent |
| R2-P4-rr-sec-05 | note | parity, no change | `cluster/core/groupcast.go` — the destination a rejection report names is derived as matter.js derives it at the pin (`GroupcastServer.ts:478-487`, `:514-520`, re-read) |
| R2-P4-rr-sec-06 | note | fixed `fc4c020d` | `cluster/core/operational_credentials.go` — CSRRequest asks the host's attestation provider before it takes the NOC lock, and answers its error after the fail-safe checks |
| R2-P4-rr-sec-07 | note | fixed `2d5bb41e` | `cluster/core/group_key_management.go` — the doc comment of the key-set-0 mapping removal names the fabrics it covers, none for a store that cannot list fabrics without a Groups manager |
| R2-P4-rr-im-01 | low | fixed `53c68d85` | `cluster/levelcontrol/levelcontrol_server.go` — a hand-off command advances the DataVersion when the value it leaves differs from the one the last host notification read — a notification that fired before the move, or while the command read its starting value, no longer takes the command's advance |
| R2-P4-rr-im-02 | low | fixed `927d30b0` | `bridge/subscribe.go` — a subscription's report numbers and failure count end with its routing — a report begun after the release gets no number, and a failure beside the one that reaps the subscription is not counted |
| R2-P4-rr-im-03 | note | fixed `51d5b5be` | `endpoint/dispatcher.go` — ClusterEventLister's doc states that the list gates EventList and the existence of event paths only, and that a server emits only events it lists, as the generated servers and matter.js's derived events do |
| R2-P4-rr-im-04 | note | fixed `e8012054`, corrected by R2-P4-fr-im-04 `408f1db9` | `bridge/subscribe.go` — a quiet path taken up by a re-wiring is compared with its last value at once — a change in the gap advances the version and goes to the throttle — and the quietPath doc cites what ServerBehaviorBacking shows |

### The final review and the last guards

Three reviewers read what came after the rework (`dc15666b..fd143e04`):
`fr-sec` the security paths, `fr-im` delivery and the servers, `fr-ver` owner
decision 78's commit. R2-P4-fr-ver-05 and R2-P4-fuzz-01 are the two findings
the last notes and the last guard run produced; R2-P4-cite-01 is the one the
first citation pass opened. The coordinator read the diffs of the fixes on the
CASE responder and on the two access lists before they were merged (the
coordinator's list).

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P4-fr-sec-01 | low | fixed `2f9d4644` | `secure/sigma/protocol.go` — a finished responder takes the same Sigma1 again as any other and answers it with a fresh Sigma2. The comments on the responder's replay handling say what matter.js has at the pin (`27cfea63`, `060b21bb`, `b4806091`, `e5d81bdc`) |
| R2-P4-fr-sec-02 | low | fixed `db6b4648`, `a9f020ca` | `bridge/securechannel.go` — every Sigma1 record of the singleton CASE handler ends with its handshake — a resumption's at its acknowledgement, a failed Sigma3's at the handler's error, an unfinished one at the responder's wait for Sigma3, a completed one at its report's acknowledgement on any exchange; the singleton CASE handler's record of a handshake whose reply was given up ends at the give-up, in the retransmission sweep that gave it up. `a9f020ca` ends the record at the give-up itself. The wait for Sigma3 is derived as matter.js derives it (the limits page, `cd1e7dcf`) |
| R2-P4-fr-sec-03 | low | fixed `6cac8245`, `ec13b650` | `bridge/securechannel.go` — a handshake in progress keeps its record, its Sigma2 and its adapter through a Sigma1 that begins nothing and through any Sigma2_Resume under its key. Second round, `ec13b650`: the responder judges a Sigma1 on locals and commits only one it accepts, and a finished responder accepts again only the Sigma3 that finished it. by_design `BD-Matter-CASE-Sigma1RepeatedWhileInProgress` and, for the completed case, `BD-Matter-CASE-Sigma1OverCompletedAcknowledgedOnly` |
| R2-P4-fr-sec-04 | low | fixed `2bf4cca1` | `cluster/core/operational_credentials.go` — the unfiltered NOCs read fails when a fabric's identity cannot be read, and leaves out a fabric whose AddNOC has not stored its identity yet |
| R2-P4-fr-sec-05 | low | fixed `db6b4648` | `bridge/securechannel.go` — every Sigma1 record of the singleton CASE handler ends with its handshake — a resumption's at its acknowledgement, a failed Sigma3's at the handler's error, an unfinished one at the responder's wait for Sigma3, a completed one at its report's acknowledgement on any exchange |
| R2-P4-fr-im-01 | low | fixed `4f789f0d` | `bridge/subscribe.go` — a controller's write beside a host's change notifier advances each changed cluster once and is reported once — the notifications of the written endpoints are held while the write is applied and settled against its record, and the written values enter the notifier's memory. What is not held: `F-db021da0-45` |
| R2-P4-fr-im-02 | low | fixed `8ff6c2d0` | `bridge/subscribe.go` — a quieter attribute a server changes while a command runs advances its cluster's DataVersion when the command ends, whether or not its report went out, and a re-wiring before the report loses no advance. Reported PLAUSIBLE; the fix note says it was reproduced before the fix |
| R2-P4-fr-im-03 | note | open: `F-db021da0-46`, a note without a code change; the row names no phase | `cluster/levelcontrol/levelcontrol_server.go`, `cluster/modeselect/modeselect_server.go` — the hand-off advance of `53c68d85` moves a version no controller reads |
| R2-P4-fr-im-04 | note | fixed `408f1db9` | `bridge/subscribe.go` — the quieter paths of one cluster a re-wiring takes up changed advance the cluster's DataVersion once |
| R2-P4-fr-im-05 | note | parity: a recorded divergence, by_design `BD-Matter-BooleanState-EventFollowsReadings` | `bridge/subscribe.go` — StateChange follows the readings of a projected source, where matter.js emits when the attribute changes |
| R2-P4-fr-ver-01 | medium | fixed `18910901`, `4abdb0e8`, `e98696c3`, `572f90e1`, `98c5d0bb`, `a4bba74e`, `973d655d`, corrected by R2-P4-fr-ver-05 `99e14e30` | `cluster/core/access_control.go` — an AccessControl or GroupKeyManagement write that answers unchanged has stored nothing; the read of the held list, the comparison and the store are one step under a write mutex, so a concurrent write is never undone under an unchanged verdict and two GroupKeyMap writes never merge; a controller's write is judged by the written value against the one the host holds, before the host is called — the held value reaches no setter and answers unchanged, another reaches the setter and answers changed whatever the host's state reads afterwards; the ports say so. Owner decision 78's verdict. The survey for it left `F-db021da0-41` (phase 5) and `F-db021da0-42` to `-44` (phase 6) |
| R2-P4-fr-ver-02 | low | fixed `ac80d790`, `ddfde342` | `endpoint/write_unchanged_parity_matterjs_test.go` — a test over the wire write path holds that a write of the held value advances and reports nothing and a new value advances and reports once; every hand-written writable kit server's verdict is held by one table through the dispatcher — a new value answers changed and advances the served DataVersion once, the same value again answers unchanged — and every other type that declares MatterWriteChanged is exempt only as a delegate to its spec.Server or as a server that stores no write, its method body held to that reason |
| R2-P4-fr-ver-03 | note | fixed `b35f77b6` | `cluster/write_changed_guard_test.go` — the MatterWriteChanged source guard keys a type by its directory, so a generated spec/<name>.Server never stands in for the hand-written package of the same name |
| R2-P4-fr-ver-04 | note | fixed `4abdb0e8` | `cluster/fan/fancontrol_server.go` — a controller's write is judged by the written value against the one the host holds, before the host is called — the held value reaches no setter and answers unchanged, another reaches the setter and answers changed whatever the host's state reads afterwards; the ports say so. The contract's doc says it (`94620ceb`) |
| R2-P4-fr-ver-05 | low | fixed `99e14e30` | `cluster/core/access_control.go` — a fabric's removal clears its AccessControl Extension under the write mutex a write of the Extension holds, so the entry's removal from memory and from its setting is one step against such a write. Found while the last notes were written (the coordinator's list) |
| R2-P4-fuzz-01 | medium | fixed `08210864` | `cluster/spec/codec.go` — an array with a tagged element is refused when command fields or a written value are read as a tree — INVALID_COMMAND for the command, the server's write check for the value — as matter.js's array schema refuses it (TlvArray.ts:77-78) and connectedhomeip's reader (TLVReader.cpp:873-875); the tree's encoder writes an array's elements anonymous, so command fields held to their definition and read again by the hand-written reader are always answered (found by FuzzGroupcastFieldsReader; review II, phase 4). Found by `FuzzGroupcastFieldsReader` in the last guard run: a request was left unanswered by a recovered panic, on a path this branch's `2561b260` had introduced; never released. The run's other failure was a test constant on 32-bit targets (`e6340570`). What stays: `F-db021da0-48`, phase 6 |
| R2-P4-cite-01 | low | struck: the code had matter.js's order already | `bridge/subscribe.go`, `im/read.go` — a Subscribe counts its paths before it validates them; `eb988f9e` holds it by a test and corrects the doc that said otherwise (`InteractionServer.ts:635-641`, `:699`, re-read) |

## What earlier phases handed to phase 4

Each unit was given the open findings rows, the recorded divergences and the
rows of earlier registers that fall into its area, and reported their status
at `9abb2e9e`; an item given to several units has one row. The status is the
unit's word for it — open as recorded, holds as recorded, no longer true, not
looked at — and was not re-measured for this file; "where it stands" is at
`308a8183`. Three rows were held back in phase 3's table and are public here,
each struck.

| ID | unit | status at the review | where it stands |
| --- | --- | --- | --- |
| F-db021da0-16 | imread, cert | imread, cert: open as recorded | fixed `936b0885`, `9d8c4603`, `c2ecdc6b`, `dc11f3d5` (owner decision 62; R2-P4-imread-06, -10, -12); the row is closed (`80a87309`), and what owner decision 83 leaves is `BD-Matter-AttributeOrder-ModelOrder`. N-read-1 of phase 2 |
| F-db021da0-17 | imread, cert | imread: not looked at; cert: no longer true | fixed `80a87309` for the restart half, which is struck (R2-P4-cert-11); the zero state stays an open findings row that names no phase. D-ctr-6 and N-ctr-3 of phase 2 |
| owner decision 53 (`BD-Matter-SubscriptionEndsWithoutReadablePath`, the subscription quota) | imread | imread: open as recorded | parity: a recorded divergence, present as its `by_design` entry records it; no change; not derived again against connectedhomeip |
| the findings row "Keep-alive reports suppress the response" | imread | imread: open as recorded | fixed `8e35331f`: the keep-alive asks for a StatusResponse, and an error status ends the subscription; the row is closed (`a388e28f`). What matter.js counts as delivered is `F-db021da0-36`, phase 6 |
| `L10-D05` detail (phase 0's register) | imread | imread: open as recorded | fixed `736fec1c` — R2-P4-imread-13 |
| F-db021da0-3 | imread, cert | imread, cert: open as recorded | open: the findings row stands. `d169f125` mirrors what follows several failed reports, which the row says was not read; the row's text did not change on the branch (the fix note and the coordinator's list ask for it to be read again) |
| F-db021da0-4 | imread, cert | imread, cert: open as recorded | open: the findings row stands, not changed on this branch |
| F-db021da0-8 | imread, cert | imread, cert: open as recorded | open: the findings row stands, not changed on this branch |
| F-db021da0-12 | imread, cert | imread, cert: open as recorded | fixed `0591dff3`; the row is closed (`80a87309`). connectedhomeip refuses a Subscribe on a session without a fabric; matter.js and now this module admit it — by the order of authority, matter.js's |
| F-db021da0-13 | imread, cert | imread: not looked at; cert: open as recorded | open: the findings row stands, not changed on this branch |
| `BD-Matter-Read-RepeatedPathsDropped` | imread | imread: open as recorded | parity: a recorded divergence, present as its `by_design` entry records it; no change |
| S-24 (phase 0: comments that speak of "v1.1") | imread, cert | imread, cert: open as recorded | phase 5: not done — the comments stand in `im/read.go`, `bridge/receive.go` and `bridge/reply.go` (a search at `308a8183`); with the docs, the register editor's placement |
| `BD-Matter-CriticalEventBypassMin` (resolved) | imread | imread: no longer true | struck: as the entry's status line says, no divergence |
| T3-P35-1 | imwrite | imwrite: open as recorded | open: the findings row stands; the unit read what its last paragraph asks for, and the row did not change |
| the unchanged write of #454 | imwrite | imwrite: holds as recorded | fixed `fd143e04` for every kit server (owner decision 78); it held for the root's NodeLabel and Location before |
| F-db021da0-2 | imwrite, applight, cert | imwrite, cert: open as recorded; applight: not looked at | fixed `fd143e04` (owner decision 78; R2-P4-imwrite-12); the row is closed (`06368f7d`). What the persister still does is `F-db021da0-47` |
| the findings row on a write path without cluster or attribute | imwrite | imwrite: open as recorded | open: the findings row stands, not changed on this branch |
| owner decisions 30 and 52 (`BD-chip-ChunkedWritesBounded`) | imwrite | imwrite: holds as recorded | parity: a recorded divergence, present as its `by_design` entry records it; no change |
| `BD-Matter-ChunkedWriteAuthorizedOnce` | imwrite | imwrite: holds as recorded | parity: a recorded divergence, present as its `by_design` entry records it; no change |
| `BD-Matter-WildcardFabricScopedWriteWithoutFabric` | imwrite | imwrite: holds as recorded | parity: a recorded divergence, present as its `by_design` entry records it; no change |
| `BD-chip-TimedRequestsBounded` | imwrite | imwrite: open as recorded | fixed `9beab127` (R2-P4-imwrite-06): a second TimedRequest in a running window is refused, so the difference the entry did not state is gone |
| W2-wire-04 | imwrite | imwrite: open as recorded | fixed `f8ff4777` (R2-P4-imwrite-04) for the Write and Invoke messages, which were outside its scope |
| W3-clugen-05 | imwrite | imwrite: holds as recorded | closed before this phase; measured again by the unit |
| N3-gc-1 | coresec, coresys, cert | coresec, cert: no longer true; coresys: not looked at | struck: not a parity difference — matter.js re-arms an armed fail-safe for a new PASE session with the expiry given, whatever remained (`GeneralCommissioningServer.ts:55-66`, `:89-93`, `FailsafeTimer.ts:48-73`, re-read). Held back in phase 3's table |
| N-cite-1 | coresec, cert | coresec: holds as recorded; cert: open as recorded | struck: `BD-Matter-PendingNOCMarker` says it already (coresec); what a matter.js node then does with a stored, uncommitted fabric was not traced (cert) |
| N-cite-2 | coresec, cert | coresec, cert: open as recorded | phase 6: the pass's reading of connectedhomeip holds (both units); the comments that say it "does the same" were not changed — `node/node.go:893` still does (a search at `308a8183`). The register editor's placement |
| F-db021da0-11 | coresec, cert | coresec: not looked at; cert: open as recorded | open: the findings row stands; the half it marks unverified is read (cert: both references answer InvalidFabricIndex for the unknown fabric only) |
| N3-net-2 | coresec, coresys, cert | coresec: not looked at; coresys: open as recorded; cert: no longer true | struck: nothing to bound — the Thread server's ScanNetworks builds at most one result, its active dataset's network (`cluster/core/network_commissioning_thread.go:425-457`, read for this file; cert's reading, where coresys took the row as it stood). Held back in phase 3's table |
| N3-rwhs-2 | coresec, cert | coresec: not looked at; cert: no longer true | struck: the same as both references — matter.js checks the sequence and the acknowledgement, not the window (`BtpSessionHandler.ts:274-278`, `:289-293`), connectedhomeip only decrements its counter (`BLEEndPoint.cpp:1199-1201`; all re-read). Held back in phase 3's table |
| S-4 (phase 0) | coresys | coresys: open as recorded | fixed `b97ba312`; by_design `BD-Matter-NetworkInterfaces-RootNetworkType` for the type a Wi-Fi root serves, after the second citation pass found the comment's reason false (`5d920231`) |
| S-3 (phase 0) | coresys | coresys: open as recorded | phase 5: as phase 0's register placed it — the interface name is not cut to 32 characters (`cluster/core/general_diagnostics.go:1187` at `308a8183`) |
| N-flake-1 | coresys, cert | coresys: open as recorded; cert: no longer true | phase 6: not changed, and the two units disagree — coresys reads matter.js as reporting a host's change of SoftwareVersion to a running subscription, cert as having no such change at run time. Not decided by a source; the register editor's placement |
| N3-finnet-1 | coresys, cert | coresys, cert: open as recorded | fixed `6aace1cd`, `c1a235c7` (R2-P4-cert-04, owner decision 80) |
| F-db021da0-9 | coresys, cert | coresys, cert: open as recorded | open: the findings row stands; the comment of `Identify.Close` names it since the second citation pass (`7dad4f6e`, the pass's report) |
| R2-P0-par-D01, W6-reg-12, D-sess-5, D-ctr-5 (owner decision 13) | coresys, appclim, cert | coresys, appclim: open as recorded; cert: not looked at | phase 6: five citation passes read what the branch added or changed, and the last one the drifted rows that were new; `script/citecheck` counts 406 drifted citations at `308a8183` (403 at the base). The re-read of the drift list as a whole was not done |
| W3-club1-03 | applight | applight: holds as recorded | closed before this phase; the same defect in LevelControl is R2-P4-applight-01, fixed `390a0893` |
| W3-clua2-02 | applight | applight: holds as recorded | closed before this phase; ColorControl's counterpart is R2-P4-applight-08, fixed `fb5cd440` |
| W3-clucore-14 | applight | applight: holds as recorded | closed before this phase; the comment its fix added is R2-P4-applight-05 |
| N-cite-3 | applight, cert | applight, cert: open as recorded | fixed in part by the citation passes: `Datasource.ts:1473-1476` stood on 19 Go lines and stands on 5 at `308a8183` (a search); phase 6 for those, owner decision 13 |
| the findings row on ColorControl's `MatterQuiesce` | applight | applight: open as recorded | open: the findings row stands, not changed on this branch |
| the findings row on the materialised Switch server's clock | applight | applight: not looked at | open: the findings row stands, not changed on this branch; not looked at |
| the findings row on an ExpiringUser without a timeout | appclim | appclim: open as recorded | open: the findings row stands, not changed on this branch |
| the findings row on the thermostat's OccupancySensing coupling | appclim | appclim: open as recorded | open: the findings row stands, not changed on this branch. The calibration of the same class is `F-db021da0-21` |
| the findings row on request fields outside their constraint | appclim | appclim: open as recorded | fixed `27cf0e19`, `2561b260`, `84d092cc` (owner decision 64); the row is closed (`80a87309`), and DoorLock's part is `F-db021da0-20` (R2-P4-appclim-07) |
| the findings row on a staged Presets write | appclim | appclim: not looked at | open: the findings row stands, not changed on this branch; not looked at, and not compared with owner decision 78's rule |
| F-db021da0-1 | cert | cert: open as recorded | fixed `9fe486d9` for an exchange over TCP, which carries at most what the peer stated; the row stands for the rest, and `BD-Matter-TCP-ExchangePayloadSize` says what is left — phase 5, not decided |
| F-db021da0-7 | cert | cert: open as recorded | open: the findings row stands, not changed on this branch |
| F-db021da0-10 | cert | cert: open as recorded | open: the findings row stands, not changed on this branch |
| F-db021da0-15 | cert | cert: open as recorded | open: the findings row stands, not changed on this branch. Phase 1's register placed it in phase 4; it was read and not taken up |
| the findings row TC-TLSCERT-2.2, 2.9 | cert | cert: open as recorded | open: the findings row stands; it needs the owner's decision it names, and the transport stamp that would serve it left with R2-P4-imwrite-01's gate |
| the findings row on the push transport's motion augment | cert | cert: not looked at | phase 6: not performable — the camera app is not exported at the connectedhomeip pin |
| the findings row TC-IDM-10.5 | cert | cert: not looked at | phase 6: not looked at in phase 4 — the full pass, the register editor's placement |
| the findings rows chipdm class (ii) and the device type validation departures | cert | cert: not looked at | phase 6: not looked at in phase 4 — the full pass, the register editor's placement |
| R2-P1-rw-07 | cert | cert: no longer true | struck: not a parity difference — in matter.js a subscription joins its session's set only when it is activated, after its priming report (`ServerSubscription.ts:411-421`, re-read; the rest is the unit's reading) |
| N-shut-2 | cert | cert: open as recorded | parity: a recorded divergence, by_design `BD-Matter-FactoryResetEndsTheNode` — R2-P4-cert-06 |
| N-rw3ctl-2 | cert | cert: open as recorded | phase 4: not done — `BD-Matter-Controller-UnsecuredSessionOneHandshake` still says connectedhomeip was not read (the entry's line at `308a8183`); the unit read both references' initiators and found the row's text alone to change |
| N-rw3req-1 | cert | cert: open as recorded | parity: a recorded divergence, by_design `BD-Matter-Invoke-HoldsHostReports` — R2-P4-cert-12 |
| N-finctl-1 | cert | cert: open as recorded | parity: a recorded divergence, by_design `BD-Matter-SessionRolloverClose` (owner decision 75) — R2-P4-cert-05 |
| N-finctl-2 | cert | cert: open as recorded | parity: a recorded divergence, by_design `BD-Matter-SessionRolloverClose` (owner decision 75) — the controller's half of R2-P4-cert-05 |
| N-reg-1 | cert | cert: open as recorded | fixed `011fd6e7`: the older range left `client/exchange` (a search for it at `308a8183`) |
| N3-cite-2 | cert | cert: open as recorded | fixed `0bfb32ef` (R2-P4-cert-02, owner decision 70) |
| N3-cite-1 | cert | cert: open as recorded | phase 6: owner decision 13 — citations at `862ad75f` stand in 12 files of `client/discovery` at `308a8183` (a search) |
| S-7 (phase 0) | cert | cert: open as recorded | phase 6: the unit found three of the citations off at the pin; whether the first citation pass, which read `endpoint/` (`2a8e9aa7`), corrected them was not checked for this file |
| S-11 (phase 0) | cert | cert: open as recorded | phase 5: not changed — `script/extract-levels-from-matter-js.ts` has no commit on the branch; the register editor's placement |
| S-22 (phase 0) | cert | cert: open as recorded | fixed `3d71eaa3` (R2-P4-cert-09); the third citation pass moved the page's intro onto `db021da0` |
| W2-wire-09, the wire fixtures | cert | cert: open as recorded | phase 6: not performable here — regenerating them at the pin needs a build of the pin, which the export does not have |
| R2-P0-par-D02 | cert | cert: not looked at | phase 6: not looked at in phase 4 — the full pass, the register editor's placement |
| S-5 (phase 0) | cert | cert: not looked at | phase 6: not looked at in phase 4 — the full pass, the register editor's placement |
| S-12 (phase 0) | cert | cert: not looked at | phase 6: not looked at in phase 4 — the full pass, the register editor's placement |
| D-1 | cert | cert: not looked at | phase 6: not looked at in phase 4 — the full pass, the register editor's placement |
| R2-P0-rv-C11 | cert | cert: not looked at | phase 6: not looked at in phase 4 — the full pass, the register editor's placement |
| N-core2-2 | cert | cert: not looked at | phase 6: owner decision 13 — the citation passes of this phase read what the branch added or changed and the drifted rows that were new; this item was not among them (`controller/dcl/cert.go` has no commit on the branch) |
| N-reg-2 | cert | cert: not looked at | phase 6: not performable — matterjs-server and `examples/jf-admin-app` are not exported at the pins |
| N-reg-3 | cert | cert: not looked at | phase 6: owner decision 13 — the citation passes of this phase read what the branch added or changed and the drifted rows that were new; this item was not among them (`bridge/exchange_routing.go` has no commit on the branch) |
| N-sess-3 | cert | cert: not looked at | phase 6: owner decision 13 — the citation passes of this phase read what the branch added or changed and the drifted rows that were new; this item was not among them (`bridge/conn.go` has no commit on the branch) |
| N-cite-4 | cert | cert: not looked at | phase 6: not performable — matterjs-server is not exported |
| N3-rwpase-3 | cert | cert: not looked at | phase 6: owner decision 13 — the citation passes of this phase read what the branch added or changed and the drifted rows that were new; this item was not among them (`controller/commissioner/flow.go` has no commit on the branch) |
| N3-cite-3 | cert | cert: not looked at | phase 6: owner decision 13 — the second citation pass touched `secure/operational/manager.go` (`8c90e400`); whether the thirteen left the drift list was not checked for this file |
| N3-cite-5 | cert | cert: not looked at | phase 6: the third citation pass read the limits page's 74 matter.js references and corrected 8; whether the two statements got their lines was not checked for this file, and the page's 13 connectedhomeip citations were not read |

## The owner's decisions of this phase

Decisions 58 to 85 are rows of `decisions.md` (`f04f07cb`, `d85d5f52`,
`f51254d4`; 58 and 59 came first, each with its own commit). The table says
where each went; "a recorded divergence" marks the ones that end in a
`by_design` entry without a change of behaviour.

| # | on | decided | where |
| --- | --- | --- | --- |
| 58 | the cut of the phase's review | seven reviewers and three verifiers; the re-read of every `by_design` entry a second workflow; the drifted citations worked off in the fix wave | `0f25ce3a`; the review ran as cut, and the re-read is five parts ("The re-read of `by_design` and the citation passes") |
| 59 | the default of the controller's image quota | 32 MiB stays | `00bed4af`; no code change — the decision confirms a number phase 3 left chosen |
| 60 | R2-P4-imread-04 | as matter.js at the pin: one failed update, the subscription ends at the third, the sessions stay | `d169f125`; `4b5fbf7f` for what counts as one update |
| 61 | R2-P4-imread-05 | as matter.js: the subscription lives on | `2be53983`; `BD-Matter-Reassemble-Closes-Removed` retired |
| 62 | R2-P4-imread-06, -10, -12 and `F-db021da0-16` | matter.js's order everywhere; the schema extract carries the member order | `936b0885`, `9d8c4603`, `c2ecdc6b`, `dc11f3d5`; `d5726c95` for the command lists |
| 63 | R2-P4-imwrite-09 | connectedhomeip's behaviour stays, a `by_design` entry | `BD-chip-GroupWrite-ListAppendApplied`; no code change but the comment (`38655f97`) — a recorded divergence |
| 64 | the class behind R2-P4-coresec-01, -04, -07 and R2-P4-coresys-01 | one check in the dispatch path, against the generated definitions; a server that relaxes a constraint says so | `27cf0e19`, `2561b260`, `84d092cc`; ADR 0013's amendment `98b0d629`; `BD-Matter-RequestCheck-RelaxationTable`. What it does not reach: `F-db021da0-20` |
| 65 | three commissioning commands on which the references disagree | matter.js, exceptions one by one | `699fa852`, `621a1614`, `de142c67` — all three follow matter.js; no exception was needed (the fix note: no certification case asserts connectedhomeip's behaviour for them; no container run confirmed it, see "Not placed, not verified") |
| 66 | R2-P4-applight-03 and -05 | the module's behaviour stays, a `by_design` entry each | `BD-Matter-Transitions-CyclicAtBound`, `BD-Matter-Scenes-FabricSceneInfoForEveryFabric`; the comments `bd0b08d8` — a recorded divergence |
| 67 | R2-P4-appclim-01 | as matter.js: a PIN that is sent is checked | `1cf1aa2e` |
| 68 | R2-P4-cert-01 | classify now, port in this phase | `7a83b14b`; the ports are open: phase 4, the certification ports |
| 69 | five certification cases the model's constraint failed | connectedhomeip's answer, the cases pass | `107ce30f`, `ad1a270e`, `fbf69e7d`; passing by unit test, not by a run of the cases |
| 70 | R2-P4-cert-02 | as the references: acknowledged, not answered, not counted | `0bfb32ef` |
| 71 | R2-P4-coresys-02 | `node.Config.BootReason` now | `aab78378` |
| 72 | `BD-Matter-mDNSDeviceType` | as matter.js: inferred when the host sets none | `2ff50e80`, `ccf76cee`; the entry retired; without an application endpoint the node still advertises RootNode — `BD-Matter-CommissionableDeviceType-RootNodeWithoutApplicationEndpoint` |
| 73 | `BD-Matter-WindowCoveringPercentNonNull` | an unknown position reads null | `e4e05d94`; the entry retired |
| 74 | `BD-Matter-OTAProvider-ACLEveryFabric` | as matter.js, the rest a host option | `a61f85c7`, `8e248dd9`, `d9b694c5`; `BD-Matter-OTAProvider-ACLGrantedFabrics`; a break (`node.OTAProviderConfig.GrantFabric`) |
| 75 | `BD-Matter-SessionRolloverClose` and R2-P4-cert-05 | a deliberate divergence, recorded with its reason | the entry (`c9e4fb7c`); no code change — a recorded divergence |
| 76 | R2-P4-imwrite-01 | the peer path moves onto `client/` in phase 5; the gate is not part of this pull request | `9d47f6f7`; `F-db021da0-29`, open, phase 5 |
| 77 | an ArmFailSafe during the rollback of an expired fail-safe | wait and arm fresh, a recorded divergence | `BD-Matter-ArmDuringRollbackWaits`; no code change; `F-db021da0-24` closed (`a388e28f`) — a recorded divergence |
| 78 | R2-P4-imwrite-12 and `F-db021da0-18` | an optional capability in `contract/` | `fd143e04` and the final round (R2-P4-fr-ver-01 to -05, R2-P4-fr-im-01); the row's own sentence `f51254d4` |
| 79 | R2-P4-coresec-08 | not mirrored, a `by_design` entry | `BD-Matter-ACL-NoAdministerEntryAtStart`; no code change; `F-db021da0-26` closed — a recorded divergence |
| 80 | R2-P4-cert-04 | connectedhomeip's rule, an optional driver capability | `6aace1cd`, `c1a235c7`; `BD-chip-WiFiInterfaceEnabled-DriverSwitch`; `F-db021da0-25` closed |
| 81 | `BD-Matter-DoorLock-CreatorFabricIndexKept` | cleared, as connectedhomeip | `40431297`, `31e185ca`; `BD-Matter-DoorLock-CreatorFabricIndexCleared` replaces the entry. What the two responses answer: `F-db021da0-33`, phase 5 |
| 82 | cases matter.js automates that are filed under "harness" | a fifth status, "not ported yet" | `2a23725c`; ADR 0011's amendment `715e5949` |
| 83 | the rest of decision 62 | both recorded as divergences | `BD-Matter-AttributeOrder-ModelOrder`; no code change — a recorded divergence |
| 84 | `node/webrtc_requestor_acl.go` | as decision 74: no entry unasked | `868b4969`, `838d7f1d`; `BD-Matter-WebRtcRequestor-ACLGrantedFabrics`; a break (`node.Config.WebRTCRequestor.GrantFabric`) |
| 85 | the hand-written redaction of fabric-sensitive fields | generated from the model, in phase 5 | `F-db021da0-30`, phase 5; the guard of R2-P4-nv-01 (`e0ac574d`) holds the class until then. One more part is held back, with N4-frsec-1 |

## Noticed by the fix agents, the reviews, the citation passes and the coordinator, not fixed where it was noticed

No severity: none of these was reviewed or verified as a finding. What the two
verifying reads took up is in the findings above and not repeated here. The
ids are this register's (`N4-<group>-<n>`, the group being the fix note's).

| ID | noticed in | what | disposition | where |
| --- | --- | --- | --- | --- |
| N4-cert-1 | `phase-4-fix-cert.md` | the certifiability page linked a file outside `docs/` relatively, and its sentence "No class (a) case is left" read as a claim about the whole page | fixed `2979a95b` | `docs/certifiability.md` |
| N4-constraint-1 | `phase-4-fix-constraint.md`; the coordinator's list | matter.js checks DoorLock's ten validated commands against the model beyond their bounds — enum values and conformance — and hands a request without a mandatory field to the handler; here the definition holds bounds, and a missing mandatory field is INVALID_COMMAND at decode | open: `F-db021da0-20`, which names no phase | `cluster/lock`, `cluster/spec`; whether the row carries the second half was not compared |
| N4-constraint-2 | `phase-4-fix-constraint.md` | the response relaxations of matter.js (Groups, DoorLock, Scenes) are not in the relaxation table | struck: decoding a response is the client's; the table holds what the device checks | `script/clustergen/relaxed.go` |
| N4-constraint-3 | `phase-4-fix-constraint.md`; the coordinator's list | the bridge's hand decoder drops SetRegulatoryConfig's CountryCode on purpose, so the server's check of it never sees a value from the wire | parity: a recorded divergence, by_design `BD-Matter-Commissioning-HandDecoders` (a sentence added, `c9e4fb7c`) | `bridge/fields_reader.go`, `cluster/core/general_commissioning.go` |
| N4-constraint-4 | `phase-4-fix-constraint.md`; `phase-4-fix-dec2a.md`; `phase-4-fix-dec2b.md`; the coordinator's list | the reference bridge's test of the pending credential state failed since R2-P4-coresec-04's order of AddNOC's checks; the coordinator's integration runs had left `./examples/...` out. A full run at `0cbdf414` had two more failures, the chip cross-check's hash block and a stale skip row of the test register | fixed `ccf200d4`, `8e248dd9` | `examples/reference-bridge`, `script/testparity`, `docs/chip-datamodel-crosscheck.md` |
| N4-fixex-1 | the coordinator's list | the repaired test is red only when both paths that clear the pending state are unwired: each backs the other up, so unwiring one alone leaves it green | phase 5: test quality — each path needs an observable of its own | `examples/reference-bridge/wiring_test.go`, `node/root.go` |
| N4-coresec-1 | `phase-4-fix-coresec.md` | matter.js counts a string's length in UTF-16 code units, this module and connectedhomeip in bytes: a fabric label of 17 to 32 characters with multi-byte UTF-8 is refused here and accepted there. What the specification says was not read | phase 6: the register editor's placement; no source places it | `cluster/core/operational_credentials.go` |
| N4-coresec-2 | the coordinator's list | a CSR build that fails leaves the fail-safe's key pair without a CSR session — a random-source failure only | phase 6: the register editor's placement | `cluster/core/operational_credentials.go` |
| N4-coresys-1 | the coordinator's list | the requestor's record of an applied update overrides a BootReason the host set — left "to confirm" in the coordinator's list | phase 6: not confirmed by a source read for this file | `node/` |
| N4-d4cert-1 | `phase-4-fix-d4cert.md` | the certifiability guard files a controller-role case of the host-skipped list as class (c) whatever its own class says; latent — no such case is in the golden file today | phase 4: the certification ports, the register editor's placement | `internal/chiptool/certifiability_doc_test.go` |
| N4-d4gate-1 | `phase-4-fix-d4gate.md`; the coordinator's list | what `9fe486d9` leaves of the TCP payload bound: no cap for a peer that stated no MaxTcpMessageSize, where matter.js falls back to 64000; and a floor at the UDP bound, where matter.js takes the smaller of the two | parity: a recorded divergence, by_design `BD-Matter-TCP-ExchangePayloadSize` (rewritten, `ac5c41cc`); phase 5: "not decided" | `bridge/ota_provider_link.go` |
| N4-d4gate-2 | `phase-4-fix-d4gate.md` | `TestBleScannerDeduplicationAndAging` failed once under load and passed five times alone | phase 5: test quality; not one of the six tests of `F-db021da0-37` | `controller/ble/scanner_parity_matterjs_test.go` |
| N4-d4lock-1 | `phase-4-fix-d4lock.md` | GetUser and GetCredentialStatus answer the creator and last-modified fabric as values; connectedhomeip answers null unless the record came through Matter | phase 5: `F-db021da0-33` | `cluster/lock/users.go`, `cluster/lock/credentials.go` |
| N4-d4ver-1 | the coordinator's list | the write's verdict travels in `endpoint.WriteChanges`, not in `im.WriteResult` | phase 5: the API bundle, a question | `endpoint/write_changes.go` |
| N4-d4ver-2 | the fourth citation pass; the coordinator's list | three differences of owner decision 78's rule from matter.js that no row stated: a GroupKeyMap written in another order is no change here; the bridge reports the written path when the verdict is "changed" although another attribute moved; state that is no attribute | open: `F-db021da0-38`, `F-db021da0-39`, which name no phase; the third has no row | `cluster/core/group_key_management.go`, `bridge/receive_dispatch.go` |
| N4-d4wifi-1 | `phase-4-fix-d4wifi.md`; the coordinator's list | ScanNetworks and ConnectNetwork still reach the driver while InterfaceEnabled is false; whether connectedhomeip refuses them was not read. `wifi/wpasupplicant` does not implement the new switch | phase 6: no row was written (the coordinator's list); the final review did not read it either | `cluster/core/network_commissioning_wifi.go`, `wifi/wpasupplicant` |
| N4-dec2a-1 | `phase-4-fix-dec2a.md`; the coordinator's list | owner decision 72 with no application endpoint: matter.js does not advertise as commissionable at all; the node advertises RootNode | parity: a recorded divergence, by_design `BD-Matter-CommissionableDeviceType-RootNodeWithoutApplicationEndpoint` | `node/commissionable_device_type.go`; the fix agent's choice, recorded — the owner was not asked |
| N4-dec2a-2 | `phase-4-fix-dec2a.md`; the coordinator's list | owner decision 73 and a simulated travel: a MoveStep with an unknown position reports Stopped, where matter.js's test node shows Opening or Closing without moving | parity: a recorded divergence, by_design `BD-Matter-WindowCovering-UnknownPositionTravelStopped` | `cluster/cover/windowcovering_server.go`. R2-P4-dr-app-02 asked for the opposite and is struck |
| N4-dec2b-1 | `phase-4-fix-dec2b.md`; the coordinator's list | a generated server reports every change of a quieter attribute at once — measured for the kit's PowerSource; and eight of the features matter.js's servers enable by default were not compared with the kit's | open: `F-db021da0-22`, `F-db021da0-23`, which name no phase | `cluster/spec/server.go`, `device/` |
| N4-dec2b-2 | `phase-4-fix-dec2b.md`; the coordinator's list | the typed `Declare{Features: 0}` means "the default", so with ChangeEvent in BooleanState's default (`5b448fa5`) a host cannot select no feature through it; matter.js can | phase 5: the API bundle, the register editor's placement | `device/` |
| N4-miscb-1 | `phase-4-fix-miscb.md` | an electrical-sensor endpoint was wired twice for its power and energy clusters, so one change advanced the DataVersion twice; Voltage, ActiveCurrent and Frequency were served and never reported on change | fixed `0cbdf414`, `88b289c0` | `bridge/subscribe.go`, `cluster/measurement/measurement.go` |
| N4-miscb-2 | the coordinator's list | how long the tracker remembers an abandoned message — as long again as it was pending (R2-P4-nv-07) — is chosen, not sourced | phase 5: the limits page, which has no row for it at `308a8183` (a search); the register editor's placement | `bridge/outbound_reliable.go`, `docs/guides/limits.md` |
| N4-f1-1 | `phase-4-fix-f1.md` | a hand-off LevelControl over a port that is a ChangeNotifier advanced twice for one change of CurrentLevel by the host (measured); the ports page said a ChangeNotifier's fire marks every reportable attribute dirty | fixed `973d655d`, `cd1e7dcf` | `cluster/levelcontrol`, `bridge/subscribe.go`, `docs/guides/ports.md` |
| N4-f1-2 | `phase-4-fix-f1.md` | the changes of a root server's notifier, and a host's own attribute changes, during a controller's write are not held | open: `F-db021da0-45`, which names no phase | `bridge/write_hold.go` |
| N4-f2-1 | `phase-4-fix-f2.md` | a generated server's Sink and the bridged NodeLabel hook heard a write of the held value | fixed `572f90e1`, `98c5d0bb` | `cluster/spec/server.go`, `cluster/core/bridged_device_basic_information.go` |
| N4-f2-2 | `phase-4-fix-f2.md`; the coordinator's list | what the survey for R2-P4-fr-ver-01 left: a GroupKeyMap write that fails part-way has stored some mappings and advances no version; a generated server's write veto runs for the held value | phase 5: `F-db021da0-41` (the store API), `F-db021da0-40` (the API bundle) | `cluster/core/group_key_management.go`, `cluster/spec/server.go` |
| N4-f2-3 | `phase-4-fix-f2.md`; the coordinator's list | concurrency left by the same survey: the Groupcast commands change a GroupKeyMap outside the write mutex; two NodeLabel writes to one bridged device are each judged against their own copy; a hand-off LevelControl that is also its endpoint's source notifier hears a host change on two subscriptions (read, not measured) | phase 6: `F-db021da0-42`, `-43`, `-44` | `cluster/core`, `endpoint/materialize.go`, `cluster/levelcontrol` |
| N4-f3-1 | `phase-4-fix-f3.md`; the coordinator's list | a finished responder accepted any Sigma3 bytes although its doc said the same bytes only; a gate's comment cited a function and a field matter.js does not have | fixed `ec13b650`, `060b21bb` | `secure/sigma/protocol.go`, `bridge/bridge.go` |
| N4-f3-2 | — | — | held back until its fix merges (owner decision 25) — phase 6 | — |
| N4-frsec-1 | the final review (`fr-sec`); the coordinator's list | a server of the host's own that serves a fabric-scoped attribute and does not implement `contract.FabricScopedReader` is still answered through its plain `MatterRead` when a request reads it (`endpoint/dispatcher.go:910`), so what another fabric's reader gets is whatever that server returns; `contract/matter.go:95-97` calls the missing interface the safe default. No server of the module is affected | phase 5: with owner decision 85 — a check when the endpoint is assembled; fixed by #534 (`phase-5b-register.md`), public since 2026-11-18 with the release that carries the fix (owner decision 126) | `endpoint/dispatcher.go`, `contract/matter.go` |
| N4-notesE-1 | — | — | held back until its fix merges (owner decision 25) — phase 6 | — |
| N4-notesE-2 | the coordinator's list | through a provider an abandoned CASE handshake ends at the adapter's one-minute lifetime, where matter.js ends it at the wait for Sigma3 | phase 6: parity; `BD-Matter-CaseHold-BoundedByEvictionOnly` is the entry it belongs to | `bridge/handlers.go` |
| N4-rrim-1 | — | — | held back until its fix merges (owner decision 25) — phase 6 | — |
| N4-im2-1 | `phase-4-fix-im2.md`; `phase-4-fix-write.md`; `phase-4-fix-tcpout.md`; the coordinator's list | why the Large Message gate left: the bridge initiates CASE over UDP only although the T key advertises outgoing TCP; its peer sender sends over sessions of the bridge's own registry (ADR 0019); the operational resolver reads no TXT record, so it learns neither T nor the peer's intervals | phase 5: `F-db021da0-29`, owner decision 76 — an ADR that supersedes ADR 0019's peer-path decision | `bridge/case_initiator.go`, `bridge/ota_provider_link.go`, `mdns/operational_resolver.go` |
| N4-im2-2 | `phase-4-fix-im2.md` | a subscription persisted before R2-P4-imread-07's fix is restored with its event paths as they were, a path naming another node included, and such a path keeps matching until the subscription is replaced | phase 6: the register editor's placement | `im/subscription/persistence.go:121` |
| N4-l1-1 | `phase-4-fix-l1.md`; `phase-4-fix-rwcore.md` | matter.js refuses a CSR over a key pair a fabric already holds; the module has no such check | struck: not reachable by a commissioner in either — the pair is drawn once per fail-safe (the fix note's reading of `FailsafeContext.ts` and `Fabric.ts`, not re-read) | `cluster/core/operational_credentials.go` |
| N4-l1-2 | `phase-4-fix-l1.md`; the coordinator's list | matter.js counts an ongoing report as delivered on the peer's StatusResponse, this module on the acknowledgement | phase 6: `F-db021da0-36` | `bridge/subscribe.go` |
| N4-l1-3 | `phase-4-fix-l1.md` | a condition in the OpenCommissioningWindow decoder tests what an earlier refusal already guarantees (a dead half, harmless) | phase 6: the register editor's placement | `cluster/wire/admincommissioning.go:464` |
| N4-order-1 | `phase-4-fix-order.md`; the coordinator's list | AcceptedCommandList and GeneratedCommandList were served in each server's own order, where matter.js sorts them | fixed `d5726c95` | `endpoint/dispatcher.go` |
| N4-read-1 | `phase-4-fix-read.md` | the dispatcher's comment on EventList says a status is emitted for it; that holds for a concrete read, and a wildcard read now reports nothing for it | phase 6: a comment, the register editor's placement; not checked whether a citation pass corrected it | `endpoint/dispatcher.go` `synthesizeGlobalRead` |
| N4-rwapp-1 | `phase-4-fix-rwapp.md` | the bridge's BooleanState events had the shape R2-P4-dr-app-07 fixed for occupancy: a source that loses its reading while true emitted a change to false | fixed `7e7c34ce`; by_design `BD-Matter-BooleanState-EventFollowsReadings` (R2-P4-fr-im-05) | `bridge/subscribe.go` |
| N4-rwcore-1 | `phase-4-fix-rwcore.md`; the coordinator's list | an ACL entry the node wrote for its OTA provider under an earlier version, or under a grant since withdrawn, stays; a marker that lets the node remove only an entry it can prove it wrote was not built | phase 5: a question for the owner; `d9b694c5` documents what stays | `node/ota_provider.go` |
| N4-rwim-1 | `phase-4-fix-rwim.md` | a reliable request that is not an initiator message was acknowledged only after the pump's grace window | fixed `c236e540` | `bridge/receive.go` |
| N4-rwsub-1 | `phase-4-fix-rwsub.md`; the review of the rework | ColorControl, OperationalState and ServiceArea advanced the DataVersion of their quieter attributes at the report, the reading R2-P4-dr-sub-03 corrected for the projection | fixed `9c930548` | `bridge/subscribe.go`, `cluster/opstate`, `cluster/servicearea` |
| N4-rwsub-2 | `phase-4-fix-rwsub.md` | two re-wirings at once, one overtaken, can drop a parked quiet path the winner would have taken up, and its held change with it; the same window exists for every listener of an overtaken wiring | phase 6: the register editor's placement | `bridge/subscribe.go` |
| N4-rwsub-3 | — | — | held back until its fix merges (owner decision 25) — phase 6 | — |
| N4-rwsub-4 | `phase-4-fix-rwsub.md` | `SessionIDsForPeer`'s doc names a function for evicting a peer's earlier sessions that no longer exists | phase 6: the name still stands once in `secure/` at `308a8183` (a search) | `secure/operational/manager.go` |
| N4-sub-1 | `phase-4-fix-sub.md`; the coordinator's list | connectedhomeip refuses a Subscribe on a PASE session with UnsupportedAccess; matter.js and this module admit it | struck: by the order of authority matter.js decides | `bridge/subscribe_dispatch.go` |
| N4-tcpout-1 | `phase-4-fix-tcpout.md`; the citation passes | four comment sites cite `webrtc-provider-manager.cpp` of connectedhomeip's camera app, which the sparse pin does not hold | phase 6: not performable here; the fix note names lines under `src/` that could stand in, not verified | `bridge/ota_provider_link.go`, `node/webrtc_provider_peers.go` |
| N4-w3bridge-1 | `phase-4-fix-w3bridge.md`; the coordinator's list | a host's server that emits events must now implement `contract.ClusterEventLister`, and the ports page and the contract's doc said only that its EventList is empty | fixed `51d5b5be`, `ecf0a65f` | `contract/matter.go`, `docs/guides/ports.md` — the page names "no event path" at `308a8183` (a search); announced as a break (`987e419d`) |
| N4-w3core-1 | `phase-4-fix-w3core.md` | with a store that cannot list fabrics, an unfiltered read of the ACL Extension leaves out the other fabrics' entries; no store of the module lacks the listing | phase 6: the register editor's placement | `cluster/core/access_control.go` |
| N4-w3gc-1 | `phase-4-fix-w3gc.md` | a branch of the dispatcher's group path can no longer be taken since the mapping is judged at decode | phase 6: the register editor's placement | `endpoint/dispatcher.go` |
| N4-w3ver-1 | `phase-4-fix-w3ver.md`; the coordinator's list | outside commands, a hand-off port that reports through its notifier while a setter of its server runs got two advances for one change (read, not measured) | phase 6: not compared after `fd143e04`, `4f789f0d` and `973d655d`, which changed these paths | `cluster/levelcontrol`, `cluster/fan` |
| N4-write-1 | `phase-4-fix-write.md` | the parity contract page does not list the parity tests the write and invoke fixes added | phase 5: docs — the page names none of the three files at `308a8183` (a search) | `docs/matter-parity-contract.md` |
| N4-x2-1 | `phase-4-fix-x2.md` | a fabric-filtered NOCs read left out the requester's own entry when its identity could not be read, and answered SUCCESS | fixed `e00867c8`; the unfiltered read is R2-P4-fr-sec-04 | `cluster/core/operational_credentials.go` |
| N4-x3-1 | `phase-4-fix-x3.md`; the coordinator's list | a quieter attribute's report that a command's commit held is marked at the commit's end at once and passes the throttle; there before this branch | phase 6: no row was written (the coordinator's list) | `bridge/subscribe.go`, `bridge/receive_dispatch.go` |
| N4-bdnotes-1 | the re-read of `by_design`; the coordinator's list | the client role's CASE resumption records live in memory, where matter.js persists the initiator's | phase 5: `F-db021da0-19`, the port extensions | `client/peer` |
| N4-bdnotes-2 | the re-read of `by_design`; the coordinator's list | `BD-Matter-GroupReceptionUnbounded`: the design owner decision 22 asked for was never done | phase 6: the entry says so (read at `308a8183`) | `groups/` |
| N4-notes-1 | the coordinator's list | `docs/matterjs-comparison.md` has 20 relative links that leave `docs/`, as at the base | phase 5: docs — counted at `308a8183`; how the strict build passes with them was not looked at | `docs/matterjs-comparison.md` |
| N4-notes-2 | this file | `L10-D06`, the row beside `L10-D05`, records as a fix that a KeepSubscriptions=false request on a PASE session closes the session's subscriptions — what `0591dff3` removed | fixed `736fec1c`: the row says its fix of 2026-05-12 was taken back with F-db021da0-12 — seen while reading for R2-P4-imread-13, by the register editor | `notes/parity/by_design.md` |
| N4-coord-1 | the coordinator's list | tests that fail only under load, each seen once or a few times in the phase's runs and none changed by it | phase 5: `F-db021da0-37`, six tests | `bridge`, `controller`, `cluster/core` |

## Unread

What each reviewer said it did not compare, read or run, summarised per unit;
all of it is phase 6 (the full pass reads every hand-written package again).
The lists are the reviewers', not re-measured here: 102 entries over the seven
units, 40 over the diff review, and the "Not reviewed" sections of the five
later reports.

| Unit | area | not read | phase |
| --- | --- | --- | --- |
| imread | Read, Subscribe, reports and events | the fabric filter of fabric-scoped lists server by server; UnsupportedRead; the access restriction list; chunk boundaries in bytes; a read path with a ListIndex; the event log under pressure and its numbering across restarts; changes during a priming report; the ICD condition of the idle interval; which closes count as terminated for persistence; the allocation of subscription ids; `bridge/subscribe.go` from the notifier wiring on, and the wire decoders. Nothing was run against matter.js | 6 |
| imwrite | Write, Invoke, Timed, groups, the status codes | what a write does to a value cluster by cluster; the statuses of the generated and hand-written request decoders; null written to a list; the invoke chunk budget and TCP sessions; the ACL algorithm itself; the injection of the fabric index; transactions beyond one commit per attribute; the wire bytes of the responses; connectedhomeip beyond the lines cited. Two of its findings were not run on the Go side | 6 |
| coresec | the servers that hold credentials and access | the Busy answer for a parallel ACL write; the redaction rules of ACL, Extension and AuxiliaryAcl; the fail-safe's rollback step by step; the sessions closed on UpdateNOC and RemoveFabric; the certificate validation rules; the terms-and-conditions feature; the window's own life cycle; the start-up restore and the removal of group keys; KeySetWrite at its edges; Groupcast's membership, quota and events. connectedhomeip was read only where a finding needed it | 6 |
| coresys | the other system servers | Binding's manager and its write checks; the fault ports and events of GeneralDiagnostics and its interface watcher; the element order of the Descriptor's lists; the ICD's DNS-SD record and Check-In codec; the OTA requestor's image reader and the provider's start; TimeSynchronization, the NetworkCommissioning servers as a whole and the diagnostics clusters; the device kit's path for quieter attributes | 6 |
| applight | lighting, level, scenes, switch | the colour conversions' arithmetic; ColorControl's device-origin paths; LevelControl's native switcher case by case and its hand-off path; request-field validation by the definitions; ScenesManagement's bounds, decoders, persisted format and a partial CopyScene; the Switch gesture input; matter.js's own tests of these behaviors; FanControl against connectedhomeip beyond the cited ranges; OnOff's DeadFrontBehavior and OffOnly | 6 |
| appclim | thermostat, lock, covering and the rest | the thermostat's schedules, suggestions and events against connectedhomeip, and two of its files; DoorLock's event payloads, credential cipher, persistence and timers; WindowCovering's Mover path and its simulated travel; OperationalState's rules line by line; SmokeCoAlarm's defaults; BooleanStateConfiguration and the camera clusters; the occupancy hold timer; `cluster/energy` | 6 |
| cert | certifiability, the open findings, the rows handed over | the 403 drifted citations; eleven rows it did not look at and three it could not perform ("What earlier phases handed to phase 4"); what the Go side of four findings rows does, taken from comments and pinning tests; the certifiability page beyond a sample — its 564 pass claims case by case; no run of `TestCertifiabilityDocument` or the chip-tool suites | 6 |
| dr-im, dr-sub, dr-core, dr-app | the diff review of waves 1 and 2 | 40 entries over the four units: the hand decoders one by one and the files that left the bridge; the client stack's chunked invoke and timed paths; most of `bridge/receive_dispatch.go` and `bridge/receive.go` beyond the diff; `bridge/subscribe.go` and `bridge/securechannel.go` outside the ranges named; the BDX and OTA parts of the peer link; the persistence of a subscription that outlives its endpoint; reports over TCP; lock orders; the bridge side of the core units' changes; the six lists whose attribute access is fabric-sensitive, outside the guard; generated and golden files, sampled; tests skimmed. No race run and no stress run | 6 |
| rr-sec, rr-im | the review of the rework | most test files of the diff; the fail-safe's own lock order; a Sigma2_Resume and a foreign Sigma3 during a handshake in progress and the TCP routes — read, not run; the subscription engine's send timing, on which the numbering argument rests; whether an appended list aliases the server's slice; no race run; connectedhomeip not consulted | 6 |
| fr-sec, fr-im, fr-ver | the final review | test files beyond those named; the docs pages the commits touch but for one row of the limits page; `d4wifi` between the driver's switch and the stored state, and Scan and Connect while disabled; `d4lock`'s lock order against the removal fan-out; TCP routes read, not run; whether the kit's transition engine settles inside a command; whether the reference daemon's sources are synchronous; `im/write.go` beyond the call sites; no race run, no chip-tool run. More of this unit is two held-back rows (U4-fr-sec-1, U4-fr-sec-2) | 6 |
| U4-fr-sec-1 | unit fr-sec | — | held back until its fix merges (owner decision 25) — phase 6 |
| U4-fr-sec-2 | unit fr-sec | — | held back until its fix merges (owner decision 25) — phase 6 |

## The re-read of `by_design` and the citation passes

Owner decision 58 made the re-read of every `by_design` entry a workflow of
its own. Five parts of 76 entries each read all 380 entries that stood at the
base, with the Ahead rows that name them, against the exported trees of the
pins; their corrections are the 14 commits "by_design says what the lines say"
(`0481d0e6` to `b42380f0`). By the parts' own counts: about 150 cited ranges
corrected (27, 44, 40, 33 and 7, the Go ranges among them), the reference
ranges re-pinned, 30 statements and 3 numbers corrected, 5 statements marked
unverified. A notes pass after them (`964c625e`, `8826a074`) rewrote the
entries whose text the code had left behind, retired two and marked what could
not be re-read. What the re-read asked the owner is decisions 72 to 75, 77 and
81; what it placed without a question is in the rows above (N4-bdnotes-1, -2;
`F-db021da0-19`). Not read by it: the tables of the file that stand under no
`BD-` heading — the audit tables of the older layers, `L10-D05` among them
(R2-P4-imread-13).

Five citation passes read citations of matter.js and connectedhomeip in the
exported trees; the fifth came with the last fix round. Their corrections are
the 24 commits "the citation says what the lines say": comment and
documentation text, 529 lines added and 430 removed in 134 files (`git show
--numstat`).

- The first three passes read, by directory, what `9abb2e9e..dc15666b` added
  or changed, the fix notes' lists of new citations, and the citations the
  review units had found failing (89 of the 589 the seven units checked, 3 of
  the 194 of the diff review, by the result files). By their own counts: the
  first (`bridge`, `im`, `endpoint`, `client`, `groups`) read 491 citations in
  216 comment paragraphs and corrected about 125 ranges in 54 files and 12
  statements, added lines to 4 statements and marked 2 unverified; the second
  (`cluster/core`, `cluster/spec`, the scripts, `node`, `secure`, `mdns`) read
  364 line references and the 52 case lines of the certification table and
  corrected 60 ranges and 3 statements; the third (the other cluster packages,
  `device`, `docs`) read about 390 places, 74 of them on the limits page, and
  corrected 76 ranges and 17 statements.
- The fourth pass read the Go lines the commits after the rework added, the
  ten later fix notes' lists and the CHANGELOG and docs lines of the notes
  pass — about 280 citations: 9 ranges and 6 statements corrected, 5 citations
  added, 1 statement marked unverified; `705e259d` keeps a guard test's
  fixture from being read as a citation. The fifth read the lines of the final
  fix round (139 line references in 33 files) and the 44 rows that were new in
  the drift list, of which 41 hold and 3 were corrected; 10 places and 2
  statements corrected in all. `script/citecheck`'s drift ratchet went from
  403 to 406 and its register ratchet from 20 to 16 (`0c894434`, regenerated
  again by `308a8183`).
- Claims that gave a reference as the reason for behaviour and did not hold,
  and what became of each. (1) The keep-alive that suppresses its response,
  with a line of matter.js that is another statement at the pin: matter.js
  asks for the response; fixed `8e35331f`, an open findings row since the pin
  lift. (2) The 48 hours an administrator's OpenCommissioningWindow may ask
  for on an uncommissioned node, given as matter.js's cap: matter.js's 48
  hours are the node's own window; the behaviour is connectedhomeip's, the
  comment says so now (`5d920231`) and `L8-D03` records it. (3) The order of a
  Subscribe's path checks: the comment was wrong and the code right —
  R2-P4-cite-01, `eb988f9e`. (4) The send interval's clamp to the subscriber's
  floor, written into matter.js's formula: the clamp is this module's own; the
  header is corrected (`5eca877f`) and the difference is
  `BD-Matter-SendIntervalNotBelowSubscriberFloor`. (5) That NetworkInterfaces
  carries the root's network type because Node.js sets no platform type: it
  sets Ethernet (`NodeJsNetwork.ts:195-202`, the second pass's reading); the
  comment is corrected (`5d920231`) and the difference is
  `BD-Matter-NetworkInterfaces-RootNetworkType`. (6) Two sentences on the CASE
  responder — that a message under a handshake in progress never reaches
  matter.js's CASE server, and when matter.js lets go of a resumed handshake's
  session — held in part only; corrected by the coordinator in `e5d81bdc`,
  with a third on what a duplicate triggers. The third and the fourth pass
  found no such claim standing.
- Marked unverified and left: why a quieter attribute's throttle outlives a
  re-wiring (no line read says when matter.js re-creates a behavior's
  backing); what a rollback leaves of the scene monitor that an invalidation
  clears; the camera app's half of a sentence of the camera guide.
- What the passes could not read, as they state it:
  `webrtc-provider-manager.cpp` of connectedhomeip's camera app, which the
  sparse export does not hold (N4-tcpout-1); citations pinned at another
  commit (`862ad75f`, `0ceb202d`, `645b267a`), which cannot be read at their
  commit in the exports and were left; long ranges read at their boundaries
  only; the 13 connectedhomeip citations of the limits page; statements about
  connectedhomeip without a line; and every citation on a line the branch did
  not touch and no list names — `script/citecheck` counts 9848 citations in
  the module at `308a8183`.
- Re-read for this file, in the exported trees: matter.js
  `GeneralCommissioningServer.ts:55-66` and `:89-93`,
  `FailsafeTimer.ts:48-73`, `BtpSessionHandler.ts:274-278` and `:289-293`,
  `InteractionServer.ts:635-641`, `:647-651` and `:699`,
  `ServerSubscription.ts:411-421`, `GroupcastServer.ts:478-487` and
  `:514-520`, `TlvArray.ts:77-78`, `TestWindowCoveringServer.ts:104-110` and
  `:119`; connectedhomeip `BLEEndPoint.cpp:1199-1201`, `TLVReader.cpp:873-875`
  — the lines of the parity row, of the struck rows that rest on a reference,
  and of R2-P4-fuzz-01's fix. All hold what the rows say of them.

## Counts

Generated from the rows above, at `308a8183`: a row is counted by the head of
its disposition — `fixed`, `parity`, `struck`, `open:`, `phase N` — or as held
back. A fixed row that names a `by_design` entry or leaves a part open is
counted as fixed; the paragraph below the table says how many do.

The 159 findings, by their first disposition:

| disposition | units | diff review | noticed, second read | rework | final | all |
| --- | --- | --- | --- | --- | --- | --- |
| fixed | 68 | 25 | 24 | 10 | 14 | 141 |
| a recorded divergence, no code change | 9 | 0 | 0 | 0 | 1 | 10 |
| parity with matter.js, no change | 0 | 0 | 0 | 1 | 0 | 1 |
| struck | 0 | 1 | 0 | 0 | 1 | 2 |
| open: a findings row that names no phase | 2 | 0 | 0 | 0 | 1 | 3 |
| open: phase 5 | 2 | 0 | 0 | 0 | 0 | 2 |
| all | 81 | 26 | 24 | 11 | 17 | 159 |

By the severity their rows carry: 4 high, 20 medium, 98 low, 37 note.

Of the 141 fixed: 30 were corrected by a finding of a later read, 9 name a
`by_design` entry beside the fix, 16 name a findings row — one the fix closes
or one for what it leaves — and 1 leaves a part open in its disposition
(R2-P4-cert-01).

What earlier phases handed over: 79 rows for the 103 entries of the units'
lists — 17 fixed, 9 a recorded divergence, no code change, 6 struck, 4 closed
before this phase, 17 open: a findings row, 1 open: this phase's notes, not
done, 3 open: phase 5, 22 open: phase 6.

The 61 noticed items: 13 fixed, 4 a recorded divergence, no code change, 3
struck, 4 open: a findings row, 1 open: phase 4, the certification ports, 14
open: phase 5, 18 open: phase 6, 4 held back (N4-frsec-1, held back until the
release that carries its fix, is counted with phase 5).

Unread: 10 rows for the 16 review units, phase 6, and 2 held-back rows.

The owner's decisions: 28 rows (58 to 85); 6 end in a recorded divergence
without a code change (63, 66, 75, 77, 79, 83), and 59 confirms a number.

Written in `by_design.md` (`git diff 9abb2e9e..308a8183`, measured by the
headings; 20 commits touch the file, 14 of them the re-read): 27 entries are
new — `BD-Matter-ACL-NoAdministerEntryAtStart`,
`BD-Matter-ACLRemovedEventEntry`, `BD-Matter-ArmDuringRollbackWaits`,
`BD-Matter-AttributeOrder-ModelOrder`,
`BD-Matter-BooleanState-EventFollowsReadings`,
`BD-Matter-CASE-EvictedSigma2HoldsSingletonRecord`,
`BD-Matter-CASE-Sigma1OverCompletedAcknowledgedOnly`,
`BD-Matter-CASE-Sigma1RepeatedWhileInProgress`,
`BD-Matter-CommissionableDeviceType-RootNodeWithoutApplicationEndpoint`,
`BD-Matter-DoorLock-CreatorFabricIndexCleared`,
`BD-Matter-FailedReadAnswersItsPath`, `BD-Matter-GKM-StoredKeySet0Removed`,
`BD-Matter-Invoke-HoldsHostReports`,
`BD-Matter-Invoke-OversizedResponseResourceExhausted`,
`BD-Matter-LevelControl-HandOffSceneInvalidation`,
`BD-Matter-NetworkInterfaces-RootNetworkType`,
`BD-Matter-OTAProvider-ACLGrantedFabrics`,
`BD-Matter-RequestCheck-RelaxationTable`,
`BD-Matter-Scenes-FabricSceneInfoForEveryFabric`,
`BD-Matter-SendIntervalNotBelowSubscriberFloor`,
`BD-Matter-Subscription-ReportsOverlap`,
`BD-Matter-Transitions-CyclicAtBound`,
`BD-Matter-WebRtcRequestor-ACLGrantedFabrics`,
`BD-Matter-WindowCovering-UnknownPositionTravelStopped`,
`BD-chip-ElectricalEnergy-CumulativeEventOnChange`,
`BD-chip-GroupWrite-ListAppendApplied`,
`BD-chip-WiFiInterfaceEnabled-DriverSwitch`. 6 entries left their heading for
a row of the table of retired entries
(`BD-Matter-Controller-InboundWithoutSourceNodeIDDropped`,
`BD-Matter-DoorLock-CreatorFabricIndexKept`, `BD-Matter-Mdns-ReannounceLoop`,
`BD-Matter-OTAProvider-ACLEveryFabric`,
`BD-Matter-WindowCoveringPercentNonNull`, `BD-Matter-mDNSDeviceType`). The
Ahead register gained `BD-Matter-Thermostat-Hold-ReachesHost` and lost the row
of `BD-matterjs-mdns-interface-allowlist`, whose entry stays. How many entries
were corrected is the passes' own count ("The re-read of `by_design` and the
citation passes") and was not measured.

In `matter_behaviour_findings.md` (5 commits): the rows `F-db021da0-18` to
`-48` were written on this branch, 31 of them; 4 were closed on it again
(`-18`, `-24`, `-25`, `-26`), so 27 stand at `308a8183`. Closed besides:
`F-db021da0-2`, `-12`, `-16`, the keep-alive row and the row on request fields
outside their constraint — a closed row leaves the file. Of the 27, 17 name
their phase — phase 5 for `-19`, `-21`, `-29` to `-34`, `-37`, `-40`, `-41`,
phase 6 for `-35`, `-36`, `-42` to `-44`, `-48` — and 10 name none (`-20`,
`-22`, `-23`, `-27`, `-28`, `-38`, `-39`, `-45`, `-46`, `-47`); read off each
row by a search for the phase, asserted by the generator.

**Held back: 6 rows** — no finding, 4 noticed items (N4-f3-2, N4-notesE-1,
N4-rrim-1, N4-rwsub-3) and 2 unread parts (U4-fr-sec-1, U4-fr-sec-2), all in
phase 6. N4-frsec-1, held back until the release that carries its fix, is in
the table since 2026-11-18 (owner decision 126), at its state of phase 4. Of the 32 rows phase 3 held back, 3
are public with this file, each struck (N3-gc-1, N3-net-2, N3-rwhs-2), and 29
stay in phase 3's file as they were, with the 6 parts of public rows it leaves
out.

## Not placed, not verified

- Done after the tip this register counts from, by `736fec1c`: R2-P4-imread-13
  (`L10-D05`, which stood under an audit table the re-read of the `BD-`
  entries did not cover) and the row beside it, `L10-D06` (N4-notes-2). Not
  done: `BD-Matter-Controller-UnsecuredSessionOneHandshake` still says
  connectedhomeip was not read, which N-rw3ctl-2 of phase 2 and this phase's
  unit asked to be corrected; it is this phase's notes and goes with phase
  6's.
- Owner decision 13 gave phase 4 the re-read of every citation at the older
  pin and a guard that ratchets citations at another commit to zero; decision
  58 put it into the fix wave. Measured at `308a8183`: 925 Go lines carry `at
  862ad75f` (a search), the drift list holds 406 rows against 403 at the base,
  and `script/citecheck`'s tests hold two ratchets, drift and registers, none
  for citations at another commit (a search of its test files). The passes
  read what the branch touched. The rows that rest on the decision are placed
  in phase 6 by the register editor; the decision itself is not carried out
  and no source says where it goes.
- Ten findings rows written in this phase name no phase ("Counts"); the rows
  of this file that end in one of them say "open" and nothing more. The older
  rows a unit read and the branch left — `F-db021da0-1`, `-3`, `-4`, `-7` to
  `-11`, `-13`, `-15`, `-17` and T3-P35-1 — stand as they were; whether each
  names a phase was read for `-17` only, which names none. `F-db021da0-15` was
  placed in phase 4 by phase 1's register and not taken up, and
  `F-db021da0-3`'s text was to be read again after R2-P4-imread-04's fix and
  did not change.
- Placed in phase 6 by the register editor because phase 4 did not look at
  them (the cert unit's list): R2-P0-par-D02, S-5, S-12, D-1, R2-P0-rv-C11,
  the two findings rows of the certification harness, and the citation rows
  N-core2-2, N-reg-3, N-sess-3, N3-rwpase-3, N3-cite-3, N3-cite-5; not
  performable with the exports: N-reg-2, N-cite-4, W2-wire-09 and the push
  transport's row. N-flake-1 is read one way by one unit and the other way by
  another, and nothing decides between them.
- Held back by the coordinator's list: N4-frsec-1, N4-f3-2, N4-notesE-1,
  N4-rrim-1, U4-fr-sec-1, U4-fr-sec-2. Held back by the register editor's
  judgement: N4-rwsub-3. Made public by the register editor although phase 3
  held them back, because each is struck: N3-gc-1, N3-net-2, N3-rwhs-2 — as
  phase 3 did with a row phase 2 had held back. Public by the same judgement
  although it speaks of what a fabric is told: R2-P4-rr-sec-05, which is
  matter.js's behaviour at the pin. Phase 3's other 29 held-back rows and 6
  parts were not compared with this branch; the generator checked that no five
  consecutive words of their texts, or of this phase's, stand in this file.
  The coordinator may move any of these.
- Not put to the owner, though a row rests on a choice: that the node
  advertises RootNode without an application endpoint (N4-dec2a-1, the fix
  agent's choice, recorded); the reason BridgedDeviceBasicInformation serves
  two optional attributes always (R2-P4-coresys-05); the marker for an ACL
  entry the node wrote (N4-rwcore-1); the design of overlapping reports
  (`BD-Matter-Subscription-ReportsOverlap`, "not decided", phase 5 by the
  coordinator's list). Chosen without a reference line: how long the tracker
  remembers an abandoned message (N4-miscb-2); the module's own bound for an
  evicted Sigma2 (`BD-Matter-CASE-EvictedSigma2HoldsSingletonRecord`). The
  wait for Sigma3 is derived as matter.js derives it; the limits page's figure
  for it is computed, the bridge's function was not run (the coordinator's
  list).
- What the verifiers could not perform (14 entries in the units' result file,
  9 in the diff review's), in short: matter.js was not executed at the pin —
  every statement about it is from reading; no end-to-end run of a bridge for
  R2-P4-imread-01, -04 and -05, whose verdicts rest on unit-level
  measurements; the group half of R2-P4-imwrite-02 not measured again; the
  specification's text, not available; no chip-tool run — whether a
  certification case asserts connectedhomeip's behaviour where owner decision
  65 follows matter.js was read from the cases, not run; what a controller
  does with a null Occupancy; that the exported trees are the pins is taken
  from how they were made, the exports carrying no `.git`.
- Not run for this file: any test but `script/docscheck` and
  `script/citecheck`. Red and green of a fix are its fix note's. By the
  coordinator's list: build, vet and the tests, with and without the race
  detector, passed at `973d655d` (283 packages twice, no failure, no data
  race); the last guard run there failed twice, both fixed since
  (R2-P4-fuzz-01, and a test constant, `e6340570`); the final guard set at
  `308a8183` was started and its result is not in the sources. The chip-tool
  suite in its container was not performable at the end of the phase — the
  container engine did not answer after a full disk — so the pull request's CI
  is the first run of it over owner decisions 65, 72, 74, 78 and 84. The
  certification families were not run: the five cases of owner decision 69
  pass by unit test.
- Every statement of a row that is marked unverified, inferred, "not run",
  "not checked for this file" or a unit's; a status in "What earlier phases
  handed to phase 4" is the unit's at `9abb2e9e`, before the fixes. What a
  fixed row says the code does is its commit's subject.
- Failures seen once and not explained are rows, not findings: the six tests
  of `F-db021da0-37` (the cause of the sixth is not traced) and N4-d4gate-2.
- The map from finding to commit came from a script over the commit subjects;
  where a subject names no ID the commit was taken from the coordinator's list
  (the final rounds) or from the subject's own reference to a decision or an
  entry. The fix notes' commit ids are from before the rebase and were not
  resolved one by one. Of the 42 commits whose `--stat` was read, each touches
  the file its finding names or what its subject says instead — tests, docs,
  the other servers of a finding that names several. 42 of the 256 commits are
  named by no row and no section: 12 of the `by_design` re-read; 18 of the
  citation passes, which their section names by their first and last or by
  their count only; and 3 notes (`b8ce431d`, `e2e6a48a`, `51574cd6`); 1 docs
  (`1ca92b3b`); 6 CHANGELOG (`97d7c6e3`, `59685f95`, `c89633ef`, `113247fd`,
  `5b324209`, `02266913`); 2 test register (`760f45e3`, `c44771d4`). They were
  placed by their subjects, not read one by one.
- Whether the sibling checkouts hold what the exported pins hold was not
  compared. Two reviews ran after the result files were written and have
  reports, not result files: the rework's and the final one; their severities
  and verdicts are their reports'.
