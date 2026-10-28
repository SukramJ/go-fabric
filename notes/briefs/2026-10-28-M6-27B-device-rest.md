# Brief 27-B — the device remainder: reported session parameters, the BDX MRP delay, the Thermostat events, the composite device types, TimeSynchronization after chip, the AirQuality value check, a node adapter for `CommissionerReach` (milestone 6, concept v10)

Date 2026-10-28. Concept v10 wave 27-B plus the owner decisions of
2026-10-27 (0.8: TimeSynchronization TZ/NTPC/NTPS/TSC after chip; the
AirQuality host-value check) and 2026-10-28 (26-D Q2: the node adapter).
Read map: `/home/markus/.cache/go-fabric-scratch/read-27b.md` (a map;
verify every line before citing; the time-sync status lines there are
approximate). **Model:** Opus. Branch `m6/27b-device-rest` from
origin/main after 26-D (#319) has merged — check `gh pr view 319 --json
state`; if still open, branch from `origin/m6/26d-joint-fabric` and
rebase later. One pull request, commits per part, one report
`notes/briefs/reports/27B.md`. go-fabric `CLAUDE.md` rules (matter.js via
`git -C /home/markus/Dokumente/GitHub/matter.js show 862ad75f:<path>`,
chip via `git -C /home/markus/Dokumente/GitHub/connectedhomeip show
6170af84:<path>`; citations; no attribution lines; English; token rules;
`secure/` is a Fable path — the node adapter's signing goes through
existing exported seams, list any `secure/` file touched). Copy this
brief to `notes/briefs/` in the first commit. Disjoint from 27-A
(`client/interaction` batching, `client/discovery`, `controller/dcl`,
`controller/commissioner` rotation, `controller/attestation` PQC): your
`client/`-side work is the node adapter only, new files.

## 1 Reported session parameters — `node`

matter.js feeds the node's intervals and maxPathsPerInvoke into the
session manager (`ServerNetworkRuntime.ts:354-362`; `SessionParameters.ts`
:17-39; `NodeSession.ts:136, :175, :484-491`). Go reports them
(`bridge.DefaultSessionParameters`, `node/security.go:339-352`) and has
`node.Config.MaxPathsPerInvoke` (`node/config.go:156-166`) but no option
for the idle, active and active-threshold intervals. Add `node.Config.
Sessions *node.SessionIntervals{Idle, Active, ActiveThreshold}` (nil =
the defaults 500 ms / 300 ms / 4000 ms the bridge sets) with validation
as matter.js validates them (read the bounds there), served in the
responder's parameters and the ICD-coupled values where ICD overrides
them (read `node/icd*.go`); an accessor on the node for what it reports.
Tests on `node/issue89_internal_test.go`'s pattern.

## 2 The device-side BDX MRP delay — `bdx`, `node`

matter.js sets `exchange.peerAdditionalMrpDelay` on an inbound BDX init
(`BdxProtocol.ts:139-142`) from `LimitsConfig.bdxAdditionalMrpDelay`
(`NetworkServer.ts:153`); the own profile sets the local delay
(`ServerNetworkRuntime.ts:406`). Go's exchange layer has
`PeerAdditionalMrpDelay` and the local delay (`client/exchange/exchange.go:80,
:189, :271, :588-596`) but the device-side BDX responder (`bdx/`,
`node.Node.bdx`) and `node.Config` have no hook. Add `node.Config.Limits`
(or the existing `Limits`, `node/limits.go:24`) field `BDXAdditionalMrpDelay`
and apply it to the exchange of every inbound BDX session the responder
accepts, as matter.js does; a `fabrictest` test shows the retransmission
timing with and without it. Mirror matter.js's validation of the value.

## 3 The Thermostat events — `cluster/thermo`

The model's eight events (`thermostat-cluster.element.ts:287-333`, TEVT
conformance) are defined in Go (`cluster/spec/thermostat/definition_gen.go:
105-114`, `FeatureEvents` :132) and the server emits only
ActiveScheduleChange (`schedules.go:313-314`) and does not serve TEVT
(`thermostat_server.go:86`). matter.js's `ThermostatServer.ts` emits none
of them (the only `events.` emit is `calibratedTemperature$Changed`
:525), so chip is the authority (`ThermostatClusterEvents.cpp`, 244
lines: every `Generate*` gated on `Feature::kEvents` :33-36, :59-61,
:83-86, :109-112, …; call sites `ThermostatClusterBase.cpp:161, :182,
:249, :286`, `ThermostatClusterCoolingSetpoints.cpp:247-292` and the
heating file). Serve TEVT as a feature selection; emit the eight events
where chip emits them, with chip's payloads and the TEVT gate; the
existing BD-chip-Thermostat-Schedules entry gains the events (propose the
paragraph). Tests: the event cases of chip's thermostat tests
(`tests/TestThermostatCluster.cpp`, `…Setpoints.cpp`, `…Sensors.cpp` —
grep `Event` in them, port those cases) plus rules tests per event.

## 4 The composite device types — `script/devicegen`, `device`

`devicetypes.go:138-140` excludes VideoDoorbell (0x143: Camera +
Doorbell), FloodlightCamera (0x144: OnOffLight + Camera) and
CastingVideoClient (owner decision 2026-10-11, reopened by concept v10
0.5: the two composites come in; CastingVideoClient stays excluded).
matter.js's files are shells (`video-doorbell.ts:28-33`,
`floodlight-camera.ts:31-36`: no behaviours of their own; the required
device types carry them). Teach devicegen a composite: a device type
whose requirements are device types (`parity/schema.json` `requirements`
with `deviceType` entries) generates a `device.NewVideoDoorbell` /
`NewFloodlightCamera` that composes the required types' clusters on one
endpoint (one endpoint, two device types in the DeviceTypeList — read how
matter.js's `StandardDeviceTypeCorpusTest.ts` treats them and how the
kit's `Declare` handles two types; the owner's earlier rule "one device
type per endpoint" in the kit is amended by this wave: say exactly what
changes, in the report and the ADR 0018 addendum). devicegen tests, kit
defaults tests, the device-type validation (`node.Config.DeviceTypeValidation`)
accepts the composition; the daemon's reference bridge gains neither
unless trivial.

## 5 TimeSynchronization TZ / NTPC / NTPS / TSC — `cluster/core/time_synchronization.go`

matter.js's `TimeSynchronizationServer.ts:14` is the shell; chip's
`TimeSynchronizationCluster.cpp` (1275 lines) is the authority: the
dispatch :1044-1059, `HandleSetTrustedTimeSource` :1138-1157 (null clears
and emits MissingTrustedTimeSource), `HandleSetTimeZone` :1161-1213
(ResourceExhausted / InvalidCommand / ConstraintError, TimeZoneStatus
event, the TimeZoneDatabase answer), `HandleSetDSTOffset` :1216-1243
(DSTStatus, DSTTableEmpty), `HandleSetDefaultNTP` :1246-1275
(ConstraintError on a bad domain/ip, InvalidCommand per NTPC rules,
Failure on store); the attribute gating :104-159, :221-237; persistence
through `TimeSyncDataProvider` (:58-75). Go serves UTCTime and
Granularity with FeatureMap 0 (`time_synchronization.go`, 179 lines;
`BD-Matter-TimeSync-NoUTCFeature`, by_design :636; findings register
:456-464). Implement the four features as feature selections after chip,
each behind what the host must supply (TZ: the time-zone table and the
DST table, LocalTime derived; NTPC: the default NTP as a host setting;
NTPS: the server flag; TSC: the trusted time source with its event),
persisted through the module's store pattern; UTC remains the node's
clock. `TestSelfHeldDefaults`, the chip test cases
(`tests/TestTimeSynchronizationCluster.cpp` — read; port by meaning), the
by_design entry replaces NoUTCFeature's "intentionally not implemented"
sentence (propose it); the comparison row. Run `make chiptool-test` at
the end: the root node's composition does not change by default (the
features stay off unless configured) — if the daemon turns any on, say so.

## 6 The AirQuality host-value check — `cluster/measurement`

chip's `AirQualityCluster.cpp:63-90` refuses a value whose feature is off
with ConstraintError (Fair/Moderate/VeryPoor/ExtremelyPoor :70-79,
Unknown accepted :81, InvalidValue :86); matter.js's server is the shell.
Go's `NewAirQualityServer` (`measurement.go:1161`) stores the host value
(findings register :465-470). Check the host value against the feature
map as chip does: an unsupported level is refused at the setter (the host
learns it) and never served; the findings row closes; port chip's
`tests/TestAirQualityCluster.cpp` cases (12).

## 7 A node adapter for `CommissionerReach` — `node`, `client`

Wave 26-D's administrator reads the announcing commissioner over a host
port (`jfadef.CommissionerReach`: ReadAdministratorFabricIndex,
ReadFabrics, ReadTrustedRootCertificates, ReadNOCs, SignVIDVerification —
`cluster/spec/jointfabricadministrator/server_gen.go:208-225` after
#319). Give the node an adapter over its client stack so the kit works
without host code: the node resolves the invoking session's peer (the
administrator must hand the invoking fabric index and peer node id to the
port — extend the port's methods with a `jfadef.Peer{FabricIndex,
NodeID}` argument or carry it in `ctx` through a typed key; prefer the
explicit argument and say so), opens a CASE session through `node.Client`
/ `peer.Set` / `interaction.ForPeer` (`node/client.go:69-170`,
`client/interaction/provider.go:176`) and performs the reads and the
SignVIDVerification invoke; the node mounts the adapter as the kit
server's `CommissionerReach` default when the declaration has none. A
`fabrictest` test: the 26-D end-to-end runs with the node adapter instead
of the test's hand-written reach. Nothing under `secure/` changes.

## 8 Docs, register, release

- Docs: node facade page (session intervals, BDX delay, the adapter),
  thermostat guide (events), device-kit guide (composites), the
  time-synchronization section, comparison rows, findings rows closed.
- CHANGELOG `[Unreleased]` Added/Changed (a port signature change in §7 is
  a break: Changed + migration guide; apidiff names it). Coverage floors.
- Guards: `make ci`, `make race` twice, `go test ./script/...`,
  `make ahead-check`, `make reachability` + `cite-report`
  (`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`) with snapshots
  committed, `mkdocs build --strict`, `make chiptool-test` (§5); one
  bundle at the end under `flock /home/markus/.cache/go-fabric-scratch/guards.lock`.
  If the memory reaper kills `make`, report and do not restart. Hand back
  once: PR number, HEAD, guards, per part the ported/left-out test counts,
  differences, Questions verbatim, the by_design proposals.
