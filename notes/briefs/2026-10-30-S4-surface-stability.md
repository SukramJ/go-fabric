# Brief S4 — surface stability: the persisted surface digest, the announcement, the assembled snapshot

Date 2026-10-30. Concept v11 phase S4 (`docs/concept-v11-surface.md` §C.9,
C.10 item 5, D.9, E.1, F row S4; ADR 0030 decision 6). Builds on S1–S3;
read the reports `notes/briefs/reports/S1.md`, `S2.md`, `S3.md` first.
**Model:** Opus. Branch `s4/surface-stability` from `origin/main` **after
the S3 PR has merged**. One pull request, commits per part, report
`notes/briefs/reports/S4.md`. `CLAUDE.md` and `notes/briefs/README.md`
rules; copy this brief to `notes/briefs/2026-10-30-S4-surface-stability.md`
in the first commit. English. No attribution lines. No edit to
`notes/parity/` — the register row this phase needs is listed in the PR
for the owner (part 5).

## 0 What S4 builds

Each endpoint's effective surface digest is persisted next to its
`ConfigurationVersion`. When the same endpoint (same `StableKey`) comes
back with a different digest — after a restart, a reassembly or a host's
`Replace` — the node **announces** the change the way the standard
provides: it raises the endpoint's `ConfigurationVersion` (and, coupled,
the root's), bumps the Descriptor's and the changed clusters' data
versions, and logs the diff. `Announce` is the only mode (owner decision
E.1). The node's surface report and the committed snapshot now cover the
**assembled** topology, so the clusters the assembler adds (Descriptor,
BridgedDeviceBasicInformation, Identify, Groups, ScenesManagement,
Binding) are part of what the guard watches (S1 report Q1).

## 1 Facts (owner-read at the pins)

- matter.js provides the mechanism and leaves the "when" to the
  application: `BasicInformationServer.ts:35-37` ("`ConfigurationVersion`
  (mandatory since Matter 1.6) is seeded to 1 and constrained to only ever
  increase. When the node's configuration changes in a way clients should
  detect, bump it with `increaseConfigurationVersion`"), the method at
  `:132-144`; `BridgedDeviceBasicInformationServer.ts:49-59` for a
  bridged node, and `:86` raises the bridge's own version in the same
  change (`await basicInformation.increaseConfigurationVersion()`). The
  **only production caller** at the pin is that coupling line; no matter.js
  code raises the version when a behavior's features or elements change
  (`git -C ../matter.js grep -n 'increaseConfigurationVersion(' 862ad75f -- packages`
  lists `BridgedDeviceBasicInformationServer.ts:86` and tests only).
  Raising it on a surface change is therefore **ahead of matter.js**; the
  owner writes the register row (part 5).
- go-fabric already has the coupled raise: `endpoint.Endpoint.IncreaseConfigurationVersion`
  (`endpoint/types.go:575-585`, bumps the BDBI data version),
  `bridge.Bridge.IncreaseConfigurationVersion` (`bridge/receive.go:822`),
  `IncreaseNodeConfigurationVersionWith` (`:863`),
  `node.Aggregator.IncreaseConfigurationVersion(s)` (`node/aggregator.go:504-560`,
  "as one change of the node", mirroring `BasicInformationServer.increaseConfigurationVersion(change)`),
  persistence through `node/persist.go:109-125` (`configVersion`,
  `storeConfigVersions`) and the root's `RestoreConfigurationVersion`
  (`:175`).
- `ConfigurationVersion` conformance at the pin: BasicInformation
  `Rev >= v6` mandatory (`cluster/spec/basicinformation/definition_gen.go:508`),
  BridgedDeviceBasicInformation `[Rev >= v6]` optional (`…/bridgeddevicebasicinformation/definition_gen.go:572`);
  go-fabric serves the bridged one always ("Zero serves 1",
  `endpoint/spec.go:88`).
- The assembled servers of an endpoint: `endpoint.ClusterServers(ep)`
  (`endpoint/materialize.go:62`); the topology `endpoint.Topology`
  (`endpoint/types.go:619`, index 0 the root); `bridge.Bridge.Topology()`
  (`bridge/bridge.go:1933`); reassembly `bridge.Bridge.Reassemble`
  (`:954`).

## 2 The persisted digest — `node`, `endpoint`

Persist the per-endpoint **effective** digest (`surface.Endpoint.Digest()`
of the assembled endpoint, S1 semantics: `Number` excluded) in the node's
persistence next to the ConfigurationVersion (`node/persist.go`: the same
key family under the stable key; read how `configVersion` is keyed and
follow it). Not in `endpoint.Record` / the endpoint store: no store
migration, no host store interface change — say so in the report. The
root endpoint's digest is persisted under its own key (as its
ConfigurationVersion is, `rootConfigVersionKey`).

## 3 The announcement — `node`, `bridge`

On every assembly that produces the topology (`Start`, `Reassemble`,
`Add`/`Replace`/`Batch` paths that reassemble): for each endpoint with a
persisted digest that differs from the new effective digest, the node
runs **one** `IncreaseNodeConfigurationVersionWith` change covering every
such endpoint (the existing coupled raise: bridged versions and the root
version once), bumps the Descriptor data version and the data version of
every cluster whose lists changed (`surface.Diff` tells which), persists
the new digests and versions, and logs one `slog` Info record per
endpoint: `surface changed` with the key, the endpoint number, the old and
new digest (hex, first 12 chars) and the `Diff` as a compact list. A
root-surface change raises the root version alone. First assembly of an
endpoint (no persisted digest) persists without raising. No drift: nothing
happens, and a test proves nothing is written.

Introspection: `surface.Report` gains `Changed []SurfaceChange{Endpoint
string; Before, After [32]byte; Changes []surface.Change; At time.Time}`
for the changes announced in this process (cleared never; bounded to the
last 100). `Node.Surface()` fills it.

The host's own `Aggregator.IncreaseConfigurationVersion(s)` stays and
keeps its semantics; the automatic raise and a host raise in the same
process are two raises (matter.js's increase is "exactly once per call",
`BasicInformationServer.ts:37`).

## 4 The assembled snapshot — `node`, `examples/reference-bridge`, `script/surfacecheck`

`Node.Surface().Effective` reads the **assembled** topology when one
exists (`bridge.Topology()`, `endpoint.ClusterServers` per endpoint
through the S1 `endpoint.ClusterSurface` function; device types from
`descriptorDeviceTypes`, `endpoint/materialize.go:560`), so the
assembler-added clusters and the BridgedNode device type appear; before
assembly it falls back to the S1 device-level view and says so in a
`Report.Assembled bool`. `--dump-surface` performs the assembly on its
in-memory store (no socket, no mDNS — the S1 dump already composes the
node; extend it to run the assembler, read how `node.Start` reaches
`bridge.Start` `node/node.go:674-700` and whether the assembly can run
without the listener; if it cannot, the dump starts the bridge on a closed
loopback listener and stops it — say which). Regenerate
`script/surfacecheck`; the summary states the endpoint and cluster counts
before and after (the diff is the assembler-added clusters). The S1
equality test keeps its device-level assertion and gains the assembled one
(every cluster on the wire is in the report, no exception list).

The S1 "aggregator" key and the root key `""` remain; the BridgedNode
device type (0x0013) appears on every bridged endpoint.

## 5 Register row (owner writes it; list it in the PR)

`BD-Matter-SurfaceChange-ConfigurationVersion` — raising the bridged and
root ConfigurationVersion automatically when an endpoint's effective
surface digest changes. matter.js marker (presence):
`BasicInformationServer.ts:36-37` "bump it with
`increaseConfigurationVersion`" (the application decides when); behaviour
source: this module (concept v11 C.9, ADR 0030 decision 6). The agent does
**not** edit `notes/parity/by_design.md`; it writes this paragraph into the
PR description under "by_design candidates".

## 6 Guards, docs, CHANGELOG

- Tests (`node/surface_stability_test.go`): first assembly persists, no
  raise; restart with the same composition: nothing written (count store
  writes); restart after enabling an extension on one device: that
  endpoint's version +1, the root's +1, Descriptor and the changed
  cluster's data versions bumped, `Report.Changed` has one entry whose
  `Changes` name the FeatureMap and the lists; two endpoints changed in
  one assembly: the root raised once; a root-only change (a node config
  option that changes the root surface) raises the root alone.
- `make ci`, `make race node/... bridge/... endpoint/...`,
  `make reachability`, `make surface` (committed), `make cite-check` with
  an absolute `MATTERJS_DIR`, `make ahead-check`, `make apidiff-check`.
  No chip-tool fixture run unless the assembled snapshot shows a cluster
  the daemon did not serve before (it must not; if it does, stop and ask).
- Docs: `docs/architecture.md` surface section (announcement, one
  sentence); `docs/matter-parity-contract.md` §4 Surface snapshot row:
  "assembled topology"; `docs/guides/` has no page for this — none
  needed.
- `CHANGELOG.md` `[Unreleased]` `### Added`: the automatic
  ConfigurationVersion raise on a surface change, `Report.Changed`,
  `Report.Assembled`, the assembled snapshot. `### Changed`: the surface
  snapshot now covers the assembler-added clusters (no API change).
- `notes/handover.md` one line; report `notes/briefs/reports/S4.md`.

## Not in S4

A mode that re-allocates endpoint numbers (owner decision E.1: not
built); profiles (S5); any store schema migration; any edit to
`notes/parity/`.

## Carry-over from S3 (owner decisions 2026-10-30, PR #336)

- **chipdm level cross-check for 1.6.0:** `internal/chipdm/levels_test.go`
  skips level 1.6.0. `../connectedhomeip/data_model/1.6` exists at the
  harness commit; compare 1.6.0 against it with the same lenient loader
  and classify every difference in `levels_acknowledged.go` (never
  acknowledge to pass). Report the counts.
- **D.9 (BridgedDeviceBasicInformation `[Rev >= v6]`)** is implemented in
  S3 but never fires because the assembler mounts that cluster. Once the
  assembled snapshot (part 4) carries the assembler's clusters, add the
  test that shows it firing at 1.4.1 for a bridged endpoint.
- **Withheld devices** (`surface.ErrWithheld`, `*surface.WithheldError`,
  S3 report decision 4): the device serves nothing, `Add` records it as
  withheld. A withheld endpoint has no served surface — persist no digest
  for it, and when it returns served, treat it as a first assembly (no
  raise), not as drift. State this in the test.
