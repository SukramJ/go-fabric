# Brief AP — the `device/` types as the client view, and the Binding list persisted (ADR 0020 stage 4 close-out; findings from report AO)

Owner: Fable. Implementer: Opus. A second Opus agent may start brief AQ
(phase 8: OTA provider, DiagnosticLogs over BDX — `cluster/core/ota*`,
`cluster/core/diagnostic_logs*`, `bdx/`, `transport/`) while you work;
stay out of those paths. You run `make chiptool-test` once at the end
(the other agent does not). Branch `wave22/clientview` from
`planning/briefs-wave-22`, pushed, **no PR**. Report
`notes/briefs/reports/AP.md`. Read first: `notes/briefs/README.md`,
[ADR 0020](../../docs/adr/0020-commissioner-and-controller-role.md)
(stage-4 row: "`device/` types as the client view"; decision 6),
[ADR 0018](../../docs/adr/0018-device-layer-and-node-facade.md),
reports AK, AL and AO (the owner's decisions are rules), `device/`
(`Type`, `ClusterRequirement`, `ElementRequirement`, one generated
file, `script/devicegen`), `endpoint/devicetype_validation.go` (the
server-side validator: what it checks, what it takes as input),
`controller/client` (`Endpoint`, `Cluster`, `structure.go`'s use of
`schema.DeviceTypeDefinitionOf`), `cluster/core/binding.go`,
`node/root.go` `buildLabels` (the UserLabel persistence pattern:
`store.GetSetting`/`SetSetting` under a per-endpoint key) and where the
node mounts Binding servers (`node/client.go`, the wrapped Snapshotter
of wave 21).

matter.js at **`0ceb202d`** (`git -C ../matter.js show 0ceb202d:<path> | cat -n`):
`packages/model/src/logic/device-types/ResolvedEndpoint.ts` and
`ConditionAssertions.ts` (how matter.js resolves a device type's
requirements against an endpoint — the same logic the Go server-side
validator mirrors), `packages/node/src/node/client/ClientStructure.ts:851-1009`
(the client endpoint takes its type and name from the model —
`this.#node.matter.deviceTypes(dt.deviceType)`; matter.js installs no
generated device type on a client endpoint), `packages/model/src/standard/elements/binding.element.ts`
(the Binding attribute's quality `N`: nonvolatile),
`packages/node/src/behaviors/binding/BindingServer.ts` (its state is
persisted as every nonvolatile behaviour state). Guards: `make ci`,
`make race`, `make reachability-check`, `make chipdm-check`,
`make chiptool-test` once (export `PATH=$HOME/go/bin:$PATH`;
`CHIP_ROOT=/home/markus/Dokumente/GitHub/connectedhomeip` for chipdm
from a worktree; a `make ci` that fails only on golangci-lint's timeout
after "0 issues" is rerun as `make lint`; if a test of `node/` fails with
"Could not connect to device" once, rerun it alone — the package's live
tests are load-sensitive). One commit per §, reachability in each.
Allowed paths: `device/` and `script/devicegen` (§1), `endpoint/`
(§1, the validator's input seam only), `controller/client` (§2),
`cluster/core/binding.go` (+ tests, §3), `node/` (§3), `store/` **only
if** a setting key is not enough (say so in the report before you add a
table — prefer the settings key), `script/coverfloor/floors.go`,
`CHANGELOG.md`. Everything else is read, not changed. `notes/parity/`
and ADRs are the owner's.

Out: the controller-side use of client clusters (a client node image
of a *controller* endpoint), scenes, the `TargetStruct` sentinel
(endpoint 0 as a target — stays in the findings register), group
bindings beyond what wave 21 has.

## Owner's decisions

- **D1 — one validator, two inputs.** `endpoint/devicetype_validation.go`
  validates an assembled server endpoint; the client view needs the same
  judgement of a remote endpoint's image. Give the validator an input
  interface (the endpoint's device types, its server cluster ids with
  FeatureMap and AttributeList/command lists per cluster, the stated
  conditions, its parts) that both the assembled endpoint and
  `client.Endpoint` satisfy; no second implementation.
- **D2 — `client.Endpoint.Device()`** returns the generated `*device.Type`
  of the endpoint's primary device type (what `DeviceType()` reports),
  nil for a type `device/` does not generate (the excluded ones: say
  which in the doc), through a `device.Lookup(id uint32) (*Type, bool)`
  the generator emits (a registry file, like `cluster/spec`'s). The
  image's `DeviceRevision()` stays the peer's; `Device().Revision` is the
  snapshot's.
- **D3 — `client.Endpoint.Conformance()`** returns the validator's report
  for the image: the device type's mandatory server clusters missing,
  present ones whose mandatory features or elements the image lacks
  (judged on the cluster's reported FeatureMap, AttributeList and command
  lists — not on values), the client clusters the type expects
  (informational: the image reports a ClientList through the Descriptor;
  compare it). A node still reading is "unknown", not failing: a cluster
  without a built shape (`Ready()` false) is not judged.
- **D4 — the Binding list is persisted** like UserLabel: `core.NewBinding`
  gains an option (`BindingConfig{Load func() ([]TargetStruct, error),
  Save func([]TargetStruct) error}` or the `buildLabels` shape — read
  it and keep the pattern) and the node mounts each endpoint's Binding
  with a `store` setting key per endpoint (`binding:<endpoint key>`,
  the same key scheme the labels use), restored before the binding
  manager registers the entries (BS:120-126: the manager sees the stored
  entries at start). `RemoveFabric` persists. The findings-register line
  "the Binding attribute is held in memory only" closes (owner).
- **D5 — no new dependency.**

## §1 — `device.Lookup` and the validator seam (`script/devicegen`, `device/`, `endpoint/`)

The generator emits `device/registry_gen.go` with `Lookup` over every
generated type (and the test that every generated file is registered,
as `cluster/spec` has). The validator's input interface (D1), the
assembled endpoint adapted to it, the existing tests unchanged in
outcome. Tests: `Lookup` finds OnOffPlugInUnit and misses an excluded
id; the validator through the interface gives the same verdicts on the
assembled fixtures as before.

## §2 — the client view (`controller/client/endpoint.go`, `conformance.go`)

D2, D3. Tests (scripted, as `structure_internal_test.go`): a plug
endpoint resolves to the OnOffPlugInUnit type; conformance clean when
OnOff carries the mandatory attributes; a missing mandatory cluster
(Identify dropped from ServerList) reported; a cluster whose FeatureMap
lacks a mandated feature reported; an unknown device type gives nil
`Device()` and an "unknown" report; a cluster not yet built is not
judged. The live test of AK (`TestLiveClientNode`) gains two assertions:
the plug's `Device().Name` and a clean conformance.

## §3 — Binding persisted (`cluster/core/binding.go`, `node/`)

D4. Tests: a write persists, a restart restores (unit, with the `node`
test database as the AL live test uses), `RemoveFabric` persists; the
binding manager registers the restored entries at start — extend
`TestLiveBinding`: after the DUT's `Stop` and a new `node.New` on its
database, `Bindings().OnEstablished` sees the entries again without a
new write.

## Report

As AK–AO: per §, guards with HEAD, brief lines that differ from the
files, decisions, left out, questions, `by_design` candidates (not
written); the findings line this closes.
