# Brief CK-2 — `<cluster>def.Host`: the kit servers' host methods through the facade (go-fabric issue #293)

Date 2026-10-27. Owner decision 2026-10-27 (shape A): every kit server
with host-facing methods the typed layer does not reach gets a generated
`Host` interface in its `<cluster>def` package; the kit server implements
it; the typed server's `Host()` returns it; `Underlying()` is for seams
only. Inventory: `/home/markus/.cache/go-fabric-scratch/read-kit-host-methods.md`
(a map — 40 servers, 226 methods in four heuristic columns: Q getter, S
inbound setter, V validated change, O other; verify every method against
the source before it enters an interface). **Model:** Opus. Branch
`ck2/host-interfaces` from main (after CK-1 merged; if it has not, from
`origin/ck1/progress-and-update-observer` and rebase). One pull request,
one report `notes/briefs/reports/CK2.md`. go-fabric `CLAUDE.md` rules;
the owner's facade rules (`docs/adr/0026-*.md`: one shape, seams are not
the facade). Copy this brief to `notes/briefs/` in the first commit.
Disjoint from the running waves: touch nothing under `cluster/camera`,
`thread/`, `cluster/threadbr` beyond what the generator makes you touch
(the generated `Host` of a cluster another wave owns is generated, not
hand-edited — list it in the report).

## 1 The rule: what goes into `Host`

- **Not** in `Host`: what the typed layer already reaches — attribute
  setters and getters that `Set(Patch)` / `Get()` / `State` cover (the S
  and Q columns where the method is a plain attribute store or read: check
  each against `SetAttributes` of the server; an inbound setter that only
  stores an attribute and reports is covered by `Set`), the
  `contract.*` interface methods, constructors, and `Handle`/`Emit`/
  `SetBeforeWrite` (reached by `Handle<Command>` / `Emit<Event>` /
  `OnWrite`).
- **In `Host`**: the V column (validated host state changes with their
  own rules — `SetPresets`, `SetSchedules`, `SetActivePresetHandle`,
  `SetNetworkCredentials`, `ClearNetworkCredentials`, `ReloadNetworks`,
  `UpdateTariff`, `SetCurrentPrice` …), the O column (state machines the
  host drives — the EVSE's `Start/Stop/…`, DeviceEnergyManagement's,
  WaterHeaterManagement's boost, the lock's credentials …), and an S/Q
  method whose semantics differ from a plain attribute (a setter that
  reconciles, a getter that computes). Decide per method, write the
  decision into the report's table (method | column | in Host? | why).
- One interface per cluster, named `Host`, in the `<cluster>def` package,
  declared in `script/clustergen` next to the `ports` map (a `hosts` map:
  cluster → method signatures with their doc lines, like `portDecl`), so
  it regenerates with the rest. The signatures use the `<cluster>def`
  types (`Preset`, `Schedule`, …) — where the kit server uses its own type
  alias, keep the alias pointing at the def type (as `thermo.Schedule =
  tdef.ScheduleStruct` does); a method whose argument type lives only in
  the kit package (e.g. `energy.Tariff`) moves the type to the def package
  or takes the def type — say which, per cluster, in the report.
- `<cluster>def.Server.Host() Host`: the kit's server when the device
  uses it, nil when the host supplied its own server (document the nil).
  The kit server gains `var _ <cluster>def.Host = (*Server)(nil)`
  compile checks. Typed-server generation: `script/clustergen`'s typed
  template, so every cluster with a `hosts` entry gets `Host()`.

## 2 Consumers and docs

- The reference bridge and the examples use `Host()` where they
  type-assert today (inventory §"Consumers": `examples/reference-bridge/
  fleet_evse_test.go:207`, `examples/reference-controller/cli/main_test.go:420`
  if a kit server), the guides (`docs/guides/thermostat.md:136-150`,
  `docs/guides/network-infrastructure.md:17-21`, the EVSE text in
  `docs/guides/advanced-seams.md:53-55`) name `Host()` instead of
  `Underlying()`, the ports guide gains a "Host" section, and
  `docs/guides/advanced-seams.md` gets the one paragraph on `Underlying()`
  (a seam for a host that mounts its own server type or needs the
  `contract.ClusterServer` itself). `docs/guides/migrating-to-the-facade.md`:
  "Changed in <next version>" — `Underlying()` stays, so this is additive;
  no break unless a type moves (then apidiff names it and CHANGELOG Changed
  announces it).
- Tests: a generator test that every kit server in `device`'s defaults
  with host methods has a `hosts` entry (the inventory's 40 as the first
  list; a server with none is listed as such so the test does not nag),
  and per cluster a test that `Host()` is the kit's server and nil for a
  host-supplied one; openccu-loom's and go-ha2fabric's current uses stay
  valid (they do not type-assert `Underlying()`).

## 3 Release

CHANGELOG `[Unreleased]` Added (the `Host` interfaces and `Host()`), the
migration guide line; guards `make ci`, `make race` twice, `go test
./script/...`, `make apidiff-check`, `make reachability` + `cite-report`
(`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`) with snapshots
committed, `mkdocs build --strict`; one bundle at the end (if the memory
reaper kills `make`, report it and do not restart it on your own). Hand
back once: PR number, HEAD, guards with results, the per-method decision
table's summary (how many in Host, how many covered by Set/Get), the type
moves, differences, Questions verbatim; "Closes #293" in the PR body.

## Guard serialisation (owner, 2026-10-27)

The host has five cores and other agents run bundles too: run every guard bundle under `flock /home/markus/.cache/go-fabric-scratch/guards.lock` (one bundle at a time on the machine; wait for the lock, do not skip it), e.g. `flock /home/markus/.cache/go-fabric-scratch/guards.lock bash -c "make ci && make race && make race"`.
