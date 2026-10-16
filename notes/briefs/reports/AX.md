# Report AX — open findings: codec, IM, controller, tooling

Branch `findings/ax` from `planning/briefs-findings` (0c9042a), pushed, no
PR. One commit per item group, each with `make reachability` and the
regenerated inventory (the Makefile-only commit skipped it: nothing it
reads changed). No chip-tool run (none allowed; no composition change
except RootNode's optional Groupcast accessor). Owner paths (`bridge/`,
`node/`, `transport/`, `secure/` but `secure/setup`, the three
`cluster/core` security files), `CHANGELOG.md` and `notes/parity/` are
untouched; where an item needs one of them, the change is described
under "Left for the owner".

| Commit | Item |
| --- | --- |
| 9d55b6a `tlv, im: integer widths and UTF-8 decode follow matter.js` | 1 |
| a410aef `cluster/spec: a mandatory command without a handler fails construction` | 3 |
| b93a06a `cluster/core: the OTA requestor clears queryRunning however a query ends` | 8 |
| 543b61a `secure/setup: the encoders refuse what matter.js's refuse; 21-digit manual code` | 9 |
| 36fd0c7 `controller: the registry record follows every descriptor save; ErrFabricRemoved` | 5 |
| db2e6b5 `controller/client: TestReactionLeave waits for the erased store` | 15 |
| aed55d5 `endpoint/sqlitestore: Migrations and SchemaFingerprint` | 12 |
| 077a4ee `contract, store, mdns, …: module terms in public docs` | 13 |
| ff36ea5 `controller/dcl: the certificate helpers are controller/attestation's` | 10 |
| 50676d0 `Makefile: CHIP_ROOT defaults beside the main working tree, from a worktree too` | 14 |
| 4a1b17f `cluster/core: a Binding target's endpoint 0 and cluster 0 are present` | 7 |
| c4f926e `groups: a group write list-chunks a list value as a unicast write does` | 6 |
| 06a44c5 `clustergen: cross-cluster structs resolve; five client-side definitions` | 11 |
| ceb020c `tlv, cluster/spec/channel: lint (…)` | 1, 11 |
| 6bc2376 `cluster/spec: tests for … (coverage floors)` | 3, 6 |

## Per item

### 1. TLV (437) — done

- **Integer widths.** matter.js has no fixed-width integer encoding: every
  `TlvUInt*`/`TlvInt*` takes its length from the value
  (`packages/types/src/tlv/TlvNumber.ts:247-297` at 0ceb202d,
  `TlvCodec.getUIntTlvLength`/`getIntTlvLength`); chip reads any width of
  the right sign (`src/lib/core/TLVReader.cpp:216-255` at 6170af84). In
  this module's paths the writes beyond the sanctioned family were:
  EventDataIB EventNumber/EpochTimestamp (`PutUintWidth(…, 8)`, now
  `PutUint64` — `TlvEventData.ts:20,22`, chip `EventDataIB.cpp:169-177`),
  SubscribeResponse MaxInterval (`PutUint16`, now smallest fit —
  `TlvSubscribeResponse.ts:18`, chip `SubscribeResponseMessage.cpp:87-90`),
  and the seven `Put*Bounded` writers (no caller outside `tlv`; now
  smallest fit, the nullable schema wraps the same `TlvNumber`).
  SubscriptionId/DataVersion keep L5-Apple-Width. The fixed-width helpers'
  comments no longer claim matter.js. The fixture
  `subscribe_resp_large_subid` now matches matter.js's bytes and is no
  longer a by-design divergence (test that fails before:
  `TestIMWireFixtures*` with `byDesignDivergence: false`).
  The old comment on `SubscribeResponse.MarshalTLV` recorded an empirical
  chip-tool timeout with a narrowed MaxInterval; chip's reader contradicts
  it — see question 1.
- **Invalid UTF-8.** matter.js reads a UTF-8 string with a non-fatal
  `TextDecoder` (`packages/general/src/util/DataReader.ts:66-68`): ill-formed
  bytes become U+FFFD, one per maximal subpart. `tlv/utf8.go` implements the
  WHATWG decoder; `tlv/decode.go` applies it after the IS1 truncation.
  Tests: `tlv/utf8_test.go`.
- Remaining fixed-width writers outside this module's paths:
  `bridge/reply.go` (the generic value writer `:738-750` writes every
  `uint16/uint32/int16/int32/int64` value fixed, beyond
  BD-Matter-FixedWidthStructs), `secure/sessionparams`, `secure/sigma`,
  `cluster/core/operational_credentials.go:1333` — owner's.

### 2. IM invoke (437) — closed-stale

Both are enforced since the extraction (f2f65e6): `im.ValidateInvokeBatch`
refuses more than `DefaultMaxPathsPerInvoke` paths with INVALID_ACTION
(`im/invoke.go:421-424`, matter.js `InteractionServer.ts:950-955`), called
before dispatch at `bridge/receive_dispatch.go:469`; a SuppressResponse
invoke without CommandDataIB sends nothing (`bridge/receive_dispatch.go:537`,
`InvokeResponse.HasCommandData`, `InteractionServer.ts:1043-1074`). Tests:
`im/invoke_batch_test.go`, `im/invoke_parity_test.go`. The register line
dates from the Loom-era doc import (636e70f).

### 3. Mandatory command without a handler (433) — done

`spec.ServerConfig.Handlers` carries the handlers; `spec.NewServer` refuses
a mandatory command without one (`ErrMissingHandler`), matter.js
`ValidatedElements.ts:331-333, :357-370` ("Implementation missing", fatal).
Every production server that registered handlers after construction now
passes them in the configuration (alarmbase, appliance, boolcfg, core
Actions/OTAProvider/CommissionerControl/SoftwareDiagnostics/WiFi, energy,
filter, media (all fourteen, `media.newServer` maps the refusal to
`media.ErrNotImplemented`), onoff). `spectest.CheckServer` checks a handler
per accepted command where the server reports it (`Server.Handles`).

Decision taken (question 2): the device layer's generated default server is
matter.js's generated behavior, whose commands are `Behavior.unimplemented`.
Refusing it would have made every device type with a command cluster
(DimmableLight, DoorLock, EVSE, Thermostat, … — 16 clusters) unbuildable.
`ServerConfig.Unimplemented` mirrors matter.js's non-strict path instead:
the server constructs, an unhandled command stays out of
AcceptedCommandList and its response out of GeneratedCommandList
(`ValidatedElements.ts:106-108, :341-343, :361-368`;
`ServerBehaviorBacking.ts:127-148`) until the host's `Handle` registers it.
Before, the device layer advertised every such command and answered
UNSUPPORTED_COMMAND. Every device type still constructs
(`TestGeneratedConstructorsAndAccessors`, all device types); new tests
`cluster/spec/server_handlers_test.go`,
`device/unimplemented_commands_test.go`.

### 4. Negative-write guards (437) — closed-stale

The guards build the module's only implementations, and the ones the
reference bridge mounts: `thermo.NewThermostatServer`
(`examples/reference-bridge/fleet_climate.go:197`), `cover.NewWindowCoveringServer`
(`fleet_safety.go:273`), `alarm`/`fan`/`pump`/`opstate`/`modebase`
constructors (the fleet files). The line came from the Loom doc import
(2026-09-08), when Loom's daemon mounted other servers; the test has used
the same constructors since a518a68.

### 5. Controller registry (443) — done (1, 3), closed-stale (2)

1. `peer.Observer.Updated`, called after every descriptor save
   (`Peer.ts:190-195, :575-579`); `Registry.bindPeer` follows it as
   `CommissioningClient.ts:711` does. Test
   `TestRegistryRecordFollowsDescriptorSave` (fails before: a known-peer
   merge saves without a session).
2. Closed-stale: matter.js's scanner states no TTL either —
   `buildCommissionableDevice` (`CommissionableMdnsScanner.ts:470-496`) and
   the addresses `{ ip, port }` (`IpService.ts:316`) carry none, so
   `expirationOf` falls to `DEFAULT_TTL` there too (`Peers.ts:959-966`).
   The record's comment now says so.
3. `client.ErrFabricRemoved` (`PeerCommunicationError.ts:86-93`, raised by
   `Fabric.ts:461-465`) confirms a RemoveFabric
   (`CommissioningClient.ts:405-417`). No Go layer deletes the controller's
   fabric; a host that does closes the peers with it.

### 6. Group sender (445) — done (2), by_design candidates (1, 3)

Group writes list-chunk as unicast writes do: `ClientGroupInteraction.write`
→ `ClientInteraction.write` → `Write()` with `chunkLists = true`
(`ClientGroupInteraction.ts:44-68`, `Write.ts:59, :149-171`), ACL and
Extension excepted (`:18-21`). The chunking pieces moved to where both
writers reach them without `groups` importing `controller/`:
`spec.ListElements`, `spec.Node.EncodeTLV`, `im.ListChunkedByDefault`
(`interaction.ListChunked` delegates). `RequestOptions.DisableListChunking`
mirrors the option. Test `groups/request_chunk_test.go`.

### 7. Binding target (447) — done in this module's paths; owner follow-ups

`core.TargetStruct` gains `EndpointPresent`/`ClusterPresent` (set for a
zero value only); `HasEndpoint`/`HasCluster`, `Key` and the binding manager
follow. Cluster 0 (Identify) had the same sentinel problem. Test
`cluster/core/binding_endpoint_zero_test.go`. Still writing the sentinel:
the read encoder `bridge/reply.go:1046-1051` (`if t.Endpoint != 0`, `if
t.Cluster != 0` → `t.HasEndpoint()`, `t.HasCluster()`) and the persisted form
`node/persist.go:336-344, :366` (`storedBinding` needs the two flags) —
owner's paths.

### 8. OTA (435, 449, 518)

- `queryRunning` — done: `runUpdateQuery` clears it in a deferred function
  (`OtaSoftwareUpdateRequestorServer.ts:576-581`). Test
  `TestOTARequestor_QueryRunningClearedOnPanic` (fails before).
- ACL on every fabric — **not done, owner path** (`node/ota_provider.go:137-177`):
  replace `own := fabrics[0].FabricIndex` by a loop over `fabrics`, the
  same check-and-add per fabric index. by_design entry below.
- Consent reset, `userConsentNeeded`, TC-SU-2.7 — by_design candidates below.

### 9. setup codes (451) — done

`QRCode` refuses a version other than 0 and the INVALID_PASSCODES;
`ManualCode` the trivial passcodes (`PairingCodeSchema.ts:279-286,
:429-430, :160-162`). The identity rule (`assertValidPayloadIdentity`) is
**decode-only** in matter.js (`validateDecoded`, `:288-293`, `:456-459`:
"bind a code being read but not one a caller may write"), so the encoder
does not apply it — the brief named it among the encoder's refusals.
`ManualCodeLong` writes the 21-digit form (`:440-454`). Tests
`secure/setup/encode_parity_matterjs_test.go`. Not added: a decode mode
that skips validation (`Schema.decode(…, validate=false)`, `Schema.ts:18-24`)
— not in the brief's decision.

### 10. DCL (455) — done (3), by_design candidates (1, 2, 4)

`controller/attestation` exports `MatterIDs`, `P256Key`, `VerifySignedBy`;
`controller/dcl` drops its copies.

### 11. Codegen (441, 450) — done

- Cross-cluster structs: `clustergen` declares `ContentLauncher.AdditionalInfoStruct`
  in Channel under its name, with Name/Value (`channel.element.ts`
  ExternalIdList). The two empty entry types stay as aliases
  (`cluster/spec/channel/entry_aliases.go`). Only structs resolve; the other
  dotted references stay as they were (enums/ids as integers:
  `MediaPlayback.CharacteristicEnum`, the WebRTC/camera/TLS/Zone ids; the
  `RootNode.*`/`Base.*` conformance names are not types).
- New definitions (linked into `controller/client/known.go`): Groupcast,
  WaterTankLevelMonitoring, AmbientContextSensing, WiFiNetworkManagement,
  Chime. devicegen regenerated: RootNode's optional Groupcast gains its
  definition and a `Groupcast()` accessor (Groupcast has a fabric-scoped
  attribute, so a host opting it in without `WithServer` still gets
  `ErrNoDefaultServer`).
- Stays without a definition (16): AccessControl, OperationalCredentials,
  GroupKeyManagement (hand-written core servers, ADR 0013); ZoneManagement,
  CameraAvStreamManagement, CameraAvSettingsUserLevelManagement,
  WebRtcTransportProvider, WebRtcTransportRequestor, PushAvStreamTransport
  (camera); JointFabricDatastore, JointFabricAdministrator; TlsCertificateManagement,
  TlsClientManagement; ThreadNetworkDiagnostics, ThreadBorderRouterManagement,
  ThreadNetworkDirectory. Chime was treated as not a camera cluster.

### 12. Store hygiene (454) — done (sqlitestore), prepared (diagnostics)

- `endpoint/sqlitestore.Migrations()` (v1 = the DDL since v0.1.0) and
  `SchemaFingerprint`, `TestMigrationsMatchApply`.
- `matter_diagnostics`: no reader in this module nor in any local checkout
  (openccu-loom included). Not marked: README §API stability makes a
  deprecated identifier keep its exact behaviour for the window (so the
  table cannot go before the API does), and `TestDeprecationMarkersMatchThePolicy`
  fails unless `CHANGELOG.md` names it — the owner's file. Ready to apply
  together with the CHANGELOG line below: a `Deprecated:` paragraph on
  `DiagnosticsRecord`, `LoadDiagnostics`, `SaveDiagnostics` ("no caller in
  the module; removal permissible in v0.8.0"), and the drop of the table as
  a new migration step in the release that removes them.

### 13. Wording (454) — done in this module's paths

`contract` (Custom DPs, CCU, the REST paths, SPA), `store` (v1.1/v1.2, the
host's config key, the stale migration name) and the inert
`loom:reachable:reason` lines (the analyzer reads `fabric:reachable:` —
`script/reachability/main.go:115`) in contract, diagevent, eligibility,
endpoint, mdns, schema and the schema generator's template. No log key
changed. Owner's: `bridge/bridge.go:7` ("matter.enabled"), `:1748`,
`:1875`, `:1990` (the `north.matter.mdns_advertise` hint — a log value),
`bridge/subscribe_dispatch.go:401-425` (`OPENCCU_LOOM_MATTER_DUMP_SUBSCRIBE`
env var), `secure/attestation/cd.go:49`, `testpaa.go:20`, the four
`loom:reachable` lines in `bridge/` and `secure/`.

### 14. Tooling (447) — done

`CHIP_ROOT` defaults beside the main working tree via `git rev-parse
--git-common-dir`; outside git it stays `../connectedhomeip`; CI passes its
own. `make -n chipdm-check` in this worktree now names
`/home/markus/Dokumente/GitHub/connectedhomeip` (before:
`…/.claude/worktrees/connectedhomeip`).

### 15. Flakes (452, 453)

- `TestReactionLeave` waits on `NodeIDs` — done.
- `TestStartReaper_ActiveSessionSurvivesSweeps` — **not done, owner path**
  (`secure/operational/manager_idle_reaper_test.go`).

## Guards

`PATH=$HOME/go/bin:$PATH`; logs and the summary in `bin/` of the worktree
(`bin/guards-summary.log`). The machine ran brief AY's chip-tool families
in parallel (load 8–13).

| Guard | HEAD | Result |
| --- | --- | --- |
| `make ci` | ceb020c | build, vet, test pass; lint "0 issues" then the 5 min timeout (exit 2) — rerun `make lint` alone: exit 0; `cover-check` then: two floors (cluster/spec 95.2 < 97, spectest 94.2 < 95) |
| `make cover-check` | 6bc2376 | exit 0 (tests added for the new code) |
| `make race` | ceb020c | exit 2: `bridge` `TestReportSubscriptionChunksAnOversizedOngoingReport` (`subscribe_report_chunking_test.go:281`, 14.6 s under load); rerun alone: ok (1.5 s) — a timing flake, not in this brief's paths |
| `go test -race` on every changed package tree | 6bc2376 | exit 0 |
| `make reachability-check` | ceb020c, 6bc2376 | exit 0 |
| `make chipdm-check` (definitions added) | ceb020c | exit 0 |

No chip-tool run (forbidden in this brief).

## Left for the owner (owner paths)

1. `bridge/reply.go:1046-1051`: encode Binding Endpoint/Cluster by
   `HasEndpoint()`/`HasCluster()`; `node/persist.go` `storedBinding` keeps
   the two presence flags (item 7).
2. `node/ota_provider.go:137-177`: the provider ACL entry on every fabric
   (item 8 decision).
3. `secure/operational/manager_idle_reaper_test.go:98`: fake clock (item 15).
4. `bridge/reply.go:738-750`: the generic writer's fixed widths (item 1).
5. The wording lines listed under item 13.
6. The diagnostics deprecation with its CHANGELOG line (item 12).

## CHANGELOG lines (for `[Unreleased]`)

- Changed: `tlv` — the `Put*Bounded` writers encode at the smallest fitting
  width, as matter.js; EventDataIB EventNumber/EpochTimestamp and
  SubscribeResponse MaxInterval likewise (SubscriptionId and DataVersion
  keep 4 bytes).
- Changed: `tlv` — a UTF-8 string element with ill-formed bytes decodes with
  U+FFFD per maximal subpart (matter.js `TextDecoder`) instead of keeping
  the bytes.
- Added: `spec.ServerConfig.Handlers`, `ServerConfig.Unimplemented`,
  `spec.ErrMissingHandler`, `Server.Handles`.
- Changed (breaking for a host that registers a mandatory command's handler
  with `Handle` after `spec.NewServer`): construction refuses a mandatory
  command without a handler; pass it in `ServerConfig.Handlers`.
- Changed: a device-layer default server lists a command in
  AcceptedCommandList only once the host registered its handler (before:
  advertised, UNSUPPORTED_COMMAND).
- Added: `peer.Observer.Updated`; the client registry's record follows
  every descriptor save. Added: `client.ErrFabricRemoved`.
- Changed: `setup.QRCode` refuses a version other than 0 and the trivial
  passcodes; `setup.ManualCode` refuses the trivial passcodes. Added:
  `setup.ManualCodeLong` (21 digits).
- Added: `groups.RequestOptions.DisableListChunking`; a group write
  list-chunks a list value. Added: `spec.ListElements`, `spec.Node.EncodeTLV`,
  `im.ListChunkedByDefault`.
- Added: `core.TargetStruct.EndpointPresent`, `ClusterPresent` — a Binding
  target may name endpoint 0 or cluster 0.
- Added: `attestation.MatterIDs`, `P256Key`, `VerifySignedBy`.
- Added: `sqlitestore.Migrations`, `sqlitestore.Migration`,
  `sqlitestore.SchemaFingerprint`.
- Added: generated definitions for Groupcast, WaterTankLevelMonitoring,
  AmbientContextSensing, WiFiNetworkManagement and Chime; Channel's
  ExternalIdList entries are `AdditionalInfoStruct` (the old entry names
  are aliases); `device.RootNode.Groupcast()`.
- (with the owner's change) Deprecated: `store.DiagnosticsRecord`,
  `store.LoadDiagnostics`, `store.SaveDiagnostics` — no caller; removal
  permissible in v0.8.0.

## by_design candidates

- **BD-Matter-GroupKeyEpochStartTimeUnits** — `groups/sender.go:132-170`
  mirrors `KeySets.ts:83, :126-133` at 0ceb202d, which reads an
  `epochStartTime` (Matter epoch, µs since 2000) through
  `Timestamp.fromMicroseconds` and compares it with `Time.nowMs` (Unix ms);
  a key started 2000–2030 in Matter time reads as started. Mirrored:
  controllers write current Matter-epoch values, "started" either way.
- **BD-Matter-GroupSendFromEphemeralPort** — already catalogued
  (`notes/parity/by_design.md:1431`); the register line 445 (3) closes
  against it.
- **BD-Matter-OtaProviderAclPerFabric** (divergence, owner's decision) —
  matter.js adds the provider's Operate/CASE ACL entry on
  `fabricAuthority.fabrics[0]` only
  (`OtaSoftwareUpdateProviderServer.ts:116-158`, `:118`), so a requestor on
  a second fabric cannot query; the node adds it on every fabric a
  requestor can query from. (Code change pending, owner path.)
- **BD-Matter-OtaConsentReset** — `directConsentObtained` defaults to false
  on every state change (`OtaSoftwareUpdateProviderServer.ts:570-574,
  :598`), so consent granted before the download is lost at apply; mirrored.
- **BD-Matter-OtaUserConsentNeeded** — `userConsentNeeded` is set from
  `consentRequired` (`:399`) also after the provider obtained consent;
  mirrored.
- **BD-Matter-TC-SU-2.7** — a failed provider connection is "no update"
  and resets with reason Success (`OtaSoftwareUpdateRequestorServer.ts:630-641`),
  where TC_SU_2_7.py:257 expects Failure; the BDX failure's OtaDownloadError
  carries no byte count and the BDX code or UpdateState.Unknown as its code
  (`:993-1002`), is emitted as DownloadError and returns to Idle at once with
  Failure (`:924-930`), where the case expects bytes/percent > 0, null
  platform code and ≥300 s. (The register's `:915`, `:982`, `:1209-1215`
  no longer point at these lines at 0ceb202d.) Followed matter.js (the brief's decision); the case is
  manual at the pin, exclude it as class (a) once automated.
- **BD-Matter-DclExpiredCrlRevokes** — `DclCertificateService.ts:1849-1874`
  parses `nextUpdate` and never reads it; an expired CRL still revokes;
  mirrored and pinned by a test.
- **BD-Matter-DclNotPorted** — out-of-band revocation sets and seed data,
  the on-demand PAA fetch by subject key id, the compliance endpoint, the
  vendor-info service, OTA image storage, ML-DSA certificates: not ported
  (register 455 (2)).
- **BD-Matter-DclContextlessSeams** — `PAAStore`/`Revocations` take no
  context; on-demand CRL fetches are bounded by the client timeout only.
  Kept: adding a context is an API break before the freeze.

## Questions

1. The removed comment on `SubscribeResponse.MarshalTLV` recorded a
   chip-tool liveness timeout ("Session 14 §19.4") with a one-byte
   MaxInterval. chip's `SubscribeResponseMessage::Parser::GetMaxInterval`
   reads through `TLVReader::Get(uint16_t&)`, which takes any width; the
   change follows matter.js and chip's reader. A `make chiptool-test` run
   (subscription cases) would confirm — not run here.
2. Item 3's device-layer stub (`ServerConfig.Unimplemented`) is a decision
   beyond the brief's text (which expected every mounted server to construct
   unchanged). Keep, or have the device layer refuse command clusters with
   `ErrNoDefaultServer` instead?
3. Channel's old entry type names are kept as plain aliases, without a
   `Deprecated:` marker (the marker needs the CHANGELOG line): mark them
   with the release notes, or keep them?
