# Report CL — go-fabric v0.12.3: the findings of go-fabric-server S3

Brief: `notes/briefs/2026-10-25-CL-server-findings.md` (80eff0c6), widened by
the owner decisions of 2026-10-25 relayed during the wave (see Decisions).
Branch `m5/server-findings` from `v0.12.2` (816557e7). Additions only.

## §1 A definition for every snapshot cluster (S3 Q20) — 11bf32ef, d6622bde

- `script/clustergen/clusters.go`: `committed` gains the 13 camera, Thread,
  Joint Fabric and TLS clusters in the standard form (definition, generated
  `spec.Server`, `Declare`) — matter.js's servers for them are the generated
  behaviour stubs at 862ad75f. New list `definitionOnly`: AccessControl,
  OperationalCredentials, GroupKeyManagement.
- Generator mode: a definition-only package gets `definition_gen.go`,
  `state_gen.go` (State, Patch, StateFrom; no Server, Wrap, Declare) and a
  generated `TestTypedState`. `TestEverySnapshotClusterIsCommitted` fails
  for a snapshot cluster in neither list; `TestCommittedPackagesAreCurrent`
  and `TestEveryClusterGeneratesAndCompiles` cover both forms.
- Core-server parity: a hand-written `core_parity_test.go` in each of the
  three packages runs `spectest.CheckHandWrittenServer` (new: CheckServer
  with the server's own FeatureMap checked against the definition, writes
  through the dispatcher's schema gate, globals admitted in the server's
  AttributeList) and compares the exported constants
  (`core.AccessControlChangeType*`, `AccessControlAuxiliaryType*`,
  `core.NOCStatus*`). The `cluster/core` servers are untouched.
- Safety for a binary that links the new definitions (controller/client
  links every definition): `bridge/fields_definition.go` never decodes
  0x001F / 0x003E / 0x003F requests through a definition
  (`handWrittenServers`, test `TestDecodeByDefinitionSkipsHandWrittenServers`);
  `device.WithServer` refuses only a cluster with a generated `Declare`
  (`hostServers`), so the hand-written core servers still mount.
- `script/devicegen` takes only a package with `server_gen.go` as a typed
  package; its `excluded` list loses the nine device types the owner named,
  now generated (Camera, SnapshotCamera, AudioDoorbell, Intercom, Chime,
  CameraController, ThreadBorderRouter, NetworkInfrastructureManager,
  JointFabricAdministrator). `RootNode` gains `ThreadNetworkDiagnostics`,
  `TlsCertificateManagement`, `TlsClientManagement` accessors.
- `controller/client/known.go` links all 135 definitions (it also lacked
  commissionercontrol and wifinetworkdiagnostics). `TestJSONAccessControlOnASite`
  reads the ACL through `DecodeJSON` and writes a second entry through
  `EncodeJSON` on a fabrictest site.
- Tests whose "no definition" examples were 0x0035 / 0x001F / device type
  0x0142 moved to the new state (`cluster_internal_test.go`,
  `conformance_internal_test.go`, `device/registry_test.go`); new fixtures
  for AudioDoorbell (momentary switch) and Intercom (GenericSwitch part);
  `hostSupplied` gains the new clusters' attributes without a default.
- Coverage floors: the 16 new packages at 100.

## §2 First-match discovery (S3 Q21) — f0b80e94

`Controller.DiscoverWith(ctx, id, timeout, DiscoverOptions{First bool})`;
`Discover` delegates with no options. First returns the first device with
an address and cancels the search; none is an empty result, no error.
Cites `Peers.ts:189-194` and `InstanceDiscovery.ts:25-31` at 862ad75f (the
first-match logic), besides the brief's `CommissioningController.ts:462-500`.
`TestDiscoverFirst`: two site devices with one discriminator; Discover
reports both, First one, before the timeout on the site clock. The
reference CLI gains `discover [--code | --discriminator] [--first]`
(the command did not exist; README entry, CLI test).

## §3 JSON without HTML escaping (S3 Q23) — a789e187

`controller/client/json.go` marshals through an encoder with
`SetEscapeHTML(false)`, trailing newline trimmed, at both former
`json.Marshal` sites. Test in `TestJSONForms` (typed NodeLabel and untyped,
both forms, byte-exact). No JSON golden contained an escape.

## §4 A device from a bare address (S3 Q24) — ce4c5736

`PairOptions.Address *net.UDPAddr` skips discovery; `controller.DeviceAt(addr)`
builds the `client.Device` (address only, IPv4-mapped unmapped, zone kept);
`Registry.ForDevice` documented for it. Cites `MatterController.ts:539-566`
(knownAddress → `forDescriptor({addresses})`), `:545-549`,
`CommissioningController.ts:289`, `RemoteDescriptor.ts:37-50`. BLE with
Address is `ErrConfiguration`. `TestPairByAddress`: the device's DNS-SD
records withdrawn, DiscoverWith finds nothing, Pair by address commissions.

## §5 Docs — 681e81a6

CHANGELOG `[Unreleased]` (Added/Changed), `docs/getting-started/controller.md`
(JSON, DiscoverWith, pairing by address), `docs/guides/ports.md` (generated-only
clusters, definition-only ones), ADR 0013 amendment, comparison rows
(Joint Fabric, Thread border-router client, network diagnostics, the
camera/TLS/JF/Thread row, Non-goals item 2), ADR 0027 status and nav title.

## Guards (HEAD d6622bde)

| Guard | Result |
| --- | --- |
| `make ci` | exit 2 at `make test`: TestIcdClientParity (known load flake), passes alone; lint, cover-check not reached in that run |
| `make race` ×2 | exit 0, exit 0 |
| `make apidiff-check` | exit 0: no incompatible change against v0.12.2 |
| `mkdocs build --strict` | exit 0 (at 681e81a6) |
| `go test ./script/clustergen ./script/devicegen` | ok (before the lint commit) |
| `make reachability-check`, `make cite-check`, `go test ./script/...`, `make stress-flakes`, `make lint`, `make cover-check` | **not run**: the guard batch was stopped by Claude Code for low system memory during `reachability-check`; not restarted per the stop notice. The first batch's lint found two issues, fixed in d6622bde |
| `make chipdm-check` | not needed: the snapshot is unchanged |
| chip-tool | not run: the reference bridge's mounted clusters and FeatureMaps did not move (RootNode only gained accessors for optional clusters it does not mount) |

## Differences from the brief

- §1 widened by the owner: 13 clusters in the standard form instead of
  definition-only; definition-only for the three core clusters only.
- The core parity test is hand-written beside the generated files (one
  `core_parity_test.go` per package), not generated: it imports
  `cluster/core` and the store, which the generator has no business knowing.
- §2: the CLI had no `discover` command; added.
- `make generate-matter-schema` not run as a whole (it re-extracts the
  snapshot from the matter.js checkout); its Go half ran
  (`generate_matter_schema.go` — no change but gofumpt formatting, reverted —,
  `clustergen`, `devicegen`).

## Decisions

- Owner 2026-10-25: camera, Thread, Joint Fabric, TLS clusters in scope as
  generated servers; the three core clusters definition-only.
- Owner 2026-10-25: the nine device types leave devicegen's `excluded`.
- Owner 2026-10-25: ADR 0027 accepted (status and nav only).
- Owner 2026-10-25: comparison Thread border-router client row "planned:
  milestone 6 phase 26a, ADR 0027"; Non-goals item 2 "until milestone 7
  (`v0.15.0`)".
- `DiscoverWith` with an options struct rather than a variadic `Discover`
  (ADR 0025 principle 2: one shape — the facade's options are structs,
  `PairOptions`, `CommissionOptions`; a variadic signature change is also
  an apidiff break).
- First requires an address, as `findCommissionable` (Pair) does: the
  result is meant to be dialled.
- Bridge and device guards for the definition-only clusters (above): a
  linked definition must not change the device side's behaviour.
- `controller.DeviceAt` exported so a host building a `client.Device` from
  an address has one checked constructor (principle 7: errors as values).

## Left out

- `WebRtcTransportRequestorServer.ts` (:1-240) not ported: session tracking
  needs the invoking peer's fabric and node in the handler (available via
  `im.LookupFabricFilter` / `im.SubjectFromContext`), a new self-held
  server with host events, and its Operate ACL entry on the first fabric
  writes the ACL (owner path). Too large for this wave; the comparison row
  names the gap. Proposed findings-register entry (for Fable to write):
  "WebRtcTransportRequestor: the generated server runs the host's
  Offer/Answer/ICECandidates/End handlers only; matter.js's
  WebRtcTransportRequestorServer.ts (862ad75f) tracks sessions by (id,
  fabricIndex, peerNodeId), answers NotFound for an untracked session,
  removes a session on End, emits offer/answer/iceCandidates/end/refused,
  and installs an Operate CASE ACL entry for the cluster on the first
  fabric."
- `cluster/spec/json.go`'s chip-tool JSON form still uses `json.Marshal`
  for strings (out of §3's scope, the route the brief names is
  `controller/client`).
- A `pair --address` CLI flag (not asked).

## Questions

- Q1: VideoDoorbell, FloodlightCamera (camera / media by their mandatory
  Camera component) and CastingVideoClient (media client clusters) stay in
  devicegen's `excluded` with their old reason; the camera clusters now
  have servers, so the first two could be generated. Symbol needed: their
  move from `excluded` to `committed` in `script/devicegen/devicetypes.go`.
- Q2: the remaining guards (`reachability-check`, `cite-check`,
  `go test ./script/...`, `stress-flakes`, `lint`, `cover-check`) were
  not run because the session was reaped for memory; the PR's CI runs
  them. A reachability ratchet move from the 16 new packages, if any,
  needs `script/reachability/inventory.json` regenerated.

## by_design candidates

- WebRtcTransportRequestor served without matter.js's session logic
  (`packages/node/src/behaviors/web-rtc-transport-requestor/WebRtcTransportRequestorServer.ts:42-187`
  at 862ad75f) — a gap rather than a design, proposed for the findings
  register above, not `by_design`.
- `DiscoverOptions.First` returns an empty result where matter.js's
  `InstanceDiscovery.onComplete` throws `DeviceNotDiscoveredError`
  (`InstanceDiscovery.ts:41-44` at 862ad75f): the facade keeps one
  outcome shape with `Discover`; `Pair` answers `ErrNoDevice`.
- `PairOptions.Address` does not fall back to discovery when the address
  fails, where `MatterController.connectPaseChannel` tries the known
  address first and then discovers (`MatterController.ts:620-647` at
  862ad75f); `commission` (`:539-566`) does not fall back, and Pair
  mirrors that path.
