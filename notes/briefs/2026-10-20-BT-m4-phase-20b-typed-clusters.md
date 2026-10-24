# Brief BT — milestone 4, phase 20b: the typed cluster layer

Date 2026-10-20. Roadmap milestone 4 (`docs/roadmap.md`, phase 20) after
[concept v6](../../docs/history/concept-v6-embedding-layer.md) Teil A.3, step 1
(owner decision 0.1: generated typed accessors) and the phase-20 read map
(`notes/briefs/reads/read-m4-phase-20.md` §4, §5). go-fabric `v0.9.0`,
matter.js pin `862ad75f` (read with `git -C $MATTERJS_DIR show 862ad75f:<path>`).
Second wave of phase 20 (`v0.9.2`), parallel to brief BS (BS owns `node/`,
`docs/`, `examples/reference-bridge/main.go`, `device/options.go`; this
brief owns `script/clustergen`, `cluster/spec/*`, `script/devicegen`,
`device/*_gen.go`, `device/device.go`, and compile fixes in
`examples/reference-bridge/fleet*.go`). Brief BU (the declaration, the
daemon rewrite) builds on this wave.

**Model:** Opus implements; Sonnet may regenerate (`go run ./script/clustergen`,
`go run ./script/devicegen`, `gofumpt`). Generated files are regenerated,
never patched. No security path.

## What matter.js has (the counterpart, read at the pin)

A behavior's state is a typed class (`State` of a `ClusterBehavior`,
`packages/node/src/behavior/cluster/ClusterBehavior.ts:37-93`), a host sets
it as a patch (`endpoint.set({onOff: {onOff: true}})`,
`packages/node/src/endpoint/Endpoint.ts:256-262`), observes it per
attribute (`events.<cluster>.<attribute>$Changed`,
`behavior/state/managed/Datasource.ts:202`) and overrides a command as a
typed method of the behavior class (`on-off-light.ts:48-100` for the
requirement shape). The Go form is a generated typed hull over
`spec.Server` (`cluster/spec/server.go:109`: `Set(attrID, any)` `:295`,
`SetAttributes` `:302`, `Value` `:261`, `Handle(cmdID, CommandHandler)`
`:404`, `MatterWrite` `:276`), generated beside the cluster definition
(`cluster/spec/<cluster>/definition_gen.go`, which already holds the
typed enums, bitmaps, structs and the command request/response structs
with TLV codecs — `cluster/spec/onoff/definition_gen.go:50-230`).

## Scope

1. **`script/clustergen` emits `cluster/spec/<cluster>/server_gen.go`** for
   every cluster it generates (`script/clustergen/clusters.go` lists them;
   `gen.go:111` `generate`, `:490` `source`, `:409` `goType`):
   - `type State struct` — one field per attribute of the definition
     (global attributes excluded), the field's Go type as the definition
     uses it (`fieldInfo.goType`), nullable attributes as pointers, lists
     as slices; a `// attribute 0x…` comment per field.
   - `type Patch struct` — the same fields, every one a pointer (nil =
     leave), lists as pointers to slices.
   - `type Server struct { *spec.Server }` with
     `func Wrap(s *spec.Server) *Server`,
     `func (s *Server) Set(p Patch) error` (non-nil fields →
     `spec.Server.SetAttributes`, all-or-nothing as it is),
     `func (s *Server) Get() State` (each field from `Value(attrID)`,
     typed; an attribute the instance does not serve stays zero),
     `func (s *Server) OnChanged(fn func(State, changed []uint32)) (unsubscribe func())`
     (over `spec.Server`'s `contract.AttributeChangeNotifier`, as
     `device.Device.OnChanged` does, `device/device.go:657-670`), and per
     accepted request command
     `func (s *Server) Handle<Command>(fn func(ctx context.Context, req <Command>Request) (<Response>, error))`
     — the response type is the command's response struct, or `struct{}`
     for a command without one (how the definition names responses:
     `spec.Cluster.responseOf`, `cluster/spec/instance.go:339`); the
     generated adapter asserts the `fields any` of `spec.CommandHandler`
     to the request type.
   - `server_gen_test.go` per package is not generated; the generator's
     own tests cover the shape: extend `TestEveryClusterGeneratesAndCompiles`
     (`script/clustergen/compile_test.go:118`) so the hull compiles for
     every cluster, and `TestGenerateShapes` (`gen_errors_test.go:65`)
     for a hull with a nullable, a list, a struct and a command with and
     without a response. One hand-written test on a real cluster
     (`cluster/spec/levelcontrol/server_typed_test.go`): `Set` with two
     fields, `Get` after it, `OnChanged` reports both ids, `HandleMoveToLevel`
     receives the decoded request and its answer is what `MatterInvoke`
     returns, a `Set` that fails the constraint stores nothing.
   - A definition whose attribute types the hull cannot express (name the
     reason: a fabric-scoped struct list, a type the generator maps to
     `any`) gets a `State` without that field and a line in the report;
     never a hand edit.
2. **`spec.Server` gains the pre-commit hook**: `ServerConfig.BeforeWrite
   func(ctx context.Context, attrID uint32, value any) error` (and
   `SetBeforeWrite` for after construction), called in `MatterWrite`
   (`server.go:276`) after the check and before the store; an error is
   returned to the write (status FAILURE unless the error carries a
   status, as the existing error paths do — read how `MatterWrite` maps
   errors today). The typed hull exposes it as
   `OnWrite(fn func(ctx, State /*the patch applied*/, attrID uint32) error)`
   — keep it simple: the hull passes attrID and the typed new value; the
   host vetoes. This is the Go form of matter.js's pre-commit `$Changing`
   (`Datasource.ts:1172-1195` — read it; if it is not the pre-commit
   veto the concept assumes, say so in the report and cite what it is).
3. **`script/devicegen` returns the hull**: for every cluster whose server
   is `*spec.Server` today the accessor becomes
   `func (d *T) LevelControl() *levelcontroldef.Server`
   (`script/devicegen/servers.go:20-30` is the table: `specServer` →
   hull type `<pkg>def.Server`; the three mapped servers OnOff, Binding,
   EcosystemInformation keep their accessors unchanged — the self-held
   servers' typed form is BU's question). `device.New…` wraps the default
   `*spec.Server` in the hull at mount (`device/device.go`, the mounting
   path) and also a bare `*spec.Server` a host passes through
   `WithServer`, so the accessor's `serverAs` finds the hull; the hull
   implements `contract.ClusterServer` by embedding (check every optional
   interface the dispatcher asserts on servers — `AttributeChangeNotifier`,
   `FabricScopedReader`, `TimedCommander`, … — passes through the embedded
   pointer; a method set that embedding does not forward is a bug to fix
   in the hull, with a test).
4. **Compile fixes** in `examples/reference-bridge/fleet*.go` and tests
   where a `*spec.Server` variable took an accessor's result; no other
   change there (BU rewrites the daemon).

## How a port is built

- Commits: `m4/phase-20b BT: <step>`; the generator change and the
  regenerated files in separate commits (the generator with its tests
  first, then `go run ./script/clustergen && go run ./script/devicegen &&
  gofumpt -w .` as one commit "regenerate").
- `// Mirrors` comments on the hull's template (in the generator's source
  template, once): `Endpoint.set` for `Set`, `$Changed` for `OnChanged`,
  the behavior-method override for `Handle<Command>`, with the lines
  above; the generated files carry the generator's header.
- CHANGELOG `[Unreleased]`: `### Added` (the typed layer, the hook),
  `### Changed` (the accessor return types: `*spec.Server` →
  `*<cluster>def.Server`, which embeds it — method calls unchanged,
  variable declarations break; named as the `v0.10.0` break under the
  README window).
- Guards: `make ci`, `make race`, `make cover-check` (new generated
  packages count: adjust `script/coverfloor/floors.go` only with the
  reason), `go test ./script/clustergen/... ./script/devicegen/...`,
  `make reachability` then `-check` (regenerate and commit the
  inventory), `make cite-check`, `make testparity-check`. No chip-tool
  run: the daemon's composition and FeatureMap do not move (the hull is
  transparent) — say so in the report.
- Branch `m4/phase-20b`, one pull request, no `needs-chiptool` label, no
  auto-merge. Copy this brief to
  `notes/briefs/2026-10-20-BT-m4-phase-20b-typed-clusters.md` in the
  first commit.

## Report shape (`notes/briefs/reports/BT.md`)

**The hull** (what is generated, with one example file named and its
size; the clusters whose `State` lacks a field and why); **The hook**
(what `MatterWrite` does with the error; what `Datasource.ts:1172-1195`
is); **devicegen** (which accessors changed, the mounting wrap);
**Compile fixes** (files); **Guards** with HEAD and exit lines;
**Differences** to this brief; **Decisions** the owner should check
(among them: `Set` without a context, as `spec.Server.Set` — the concept's
example shows one); **Left out**; **Questions** (the self-held servers'
typed form for BU); `by_design` candidates.

## Out of scope

The declaration option (`<cluster>def.Declare`), transitions wiring, the
daemon rewrite, the docs page (BU); the facade (BS); apidiff and the
`until: M4` test ports (BV); the self-held servers (`cluster/onoff`,
`levelcontrol`, `light`, `thermo`, `lock`, …) and `contract/`; the
behaviour of any cluster — the hull adds no semantics.

## Addendum (owner, 2026-10-20, after the wave started)

Two extensions of the scope, decided after the review of the concept;
they replace the narrower statements above where they conflict.

1. **The hull wraps every `contract.ClusterServer`, not only
   `*spec.Server`.** `type Server struct { srv contract.ClusterServer }`
   with `Wrap(srv contract.ClusterServer) *Server` and
   `Underlying() contract.ClusterServer`; `Get` reads through
   `contract.ClusterServer.MatterRead` (typed per attribute, as before);
   `OnChanged` through `contract.AttributeChangeNotifier` or
   `contract.ChangeNotifier` (exactly `device.Device.OnChanged`'s switch);
   `Set` through an optional interface `spec.AttributeSetter`
   (`SetAttributes(map[uint32]any) error`, which `*spec.Server` implements
   today) and returns `spec.ErrNotSettable` (new, exported) for a server
   without it; `Handle<Command>` through an optional interface
   `spec.CommandRegistrar` (`Handle(cmdID uint32, fn CommandHandler)`,
   which `*spec.Server` implements) and returns `spec.ErrNoHandlers`
   otherwise. The hull implements `contract.ClusterServer` and every
   optional interface of the dispatcher by forwarding to `srv` (list them
   from `endpoint/dispatcher.go` and `endpoint/materialize.go`; a
   forwarding test asserts each one on a hull over a fake that implements
   it). Consequence for devicegen: **every** accessor returns the hull,
   the three mapped servers included (`OnOff() *onoffdef.Server`;
   `onoffdef.Server.Underlying()` gives the `*onoff.Server` back for a
   host that needs its own API). This is a `### Changed` for those three
   accessors too. The self-held servers' `Set` stays theirs (they do not
   implement `AttributeSetter` unless they already have a generic setter —
   do not add one in this wave; the report lists which self-held servers
   the hull can `Set` and which not).
2. **Typed events.** For every event of the definition the hull gets
   `Emit<Event>(endpoint uint16, data <Event>Event) error` over the
   server's event emitter path: `*spec.Server.Emit` (`server.go:488`) for
   spec servers; for another server through an optional interface
   `spec.EventEmitting` (`Emit(endpoint uint16, eventID uint32, data any) error`)
   — check whether the self-held servers expose such a method already
   (grep `func (s \*Server) Emit`); if not, `spec.ErrNoEvents`. The event
   data structs exist in the definition packages (the `<Name>Event`
   structs with codecs — verify the naming in one package and use what
   the generator emits).

The report's **Decisions** section names the hull's forwarding list and
the accessor break for the three mapped servers; the hand-written test
on levelcontrol gains an event case; `TestGenerateShapes` gains an event.
