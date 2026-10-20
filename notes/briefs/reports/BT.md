# Report BT — milestone 4, phase 20b: the typed cluster layer

Branch `m4/phase-20b` from main 7084a60. Implementer: Opus, one agent.
matter.js read at `862ad75f` (git show). The owner's addendum (2026-10-20,
after the wave started) is implemented; the brief copy under
`notes/briefs/` carries it. The first form (a hull embedding
`*spec.Server`, wrapped at mount, commits 194a6af–f4a0995) was generalised
in 5fe40b9 rather than finished. No chip-tool run: the daemon's
composition and FeatureMap do not move — the device mounts its servers
exactly as before, the typed server is a view on access, and the
reference bridge changed by compile fixes only.

## The hull

`script/clustergen/hull.go` renders `cluster/spec/<cluster>/server_gen.go`
for every generated cluster (119 committed packages; all 159 snapshot
clusters generate and vet in `TestEveryClusterGeneratesAndCompiles`).
Example: `cluster/spec/levelcontrol/server_gen.go`, 154 lines, 7 982 bytes.

- `State`: one field per non-global attribute, the definition's Go type,
  nullable as a pointer, lists as slices; a `matter:"0x…"` tag and an
  `// attribute 0x…` comment per field. `Patch`: the same fields as
  pointers (nil leaves), `Null []uint32` where a nullable attribute exists.
- `Server struct{ spec.Wrapper }`: `Wrap(contract.ClusterServer)` (nil
  for nil), `Underlying()`, `Set(Patch) error`,
  `Get() State`, `OnChanged(func(State, []uint32)) func()`,
  `OnWrite(func(ctx, next State, attrID) error) error`,
  `Handle<Command>(func(ctx, <Cmd>Request) (<Resp>, error)) error` per
  request command (`struct{}` for status; the response named as
  `responseOf` names it), `Emit<Event>(endpoint uint16, <Event>Event) error`
  per event (127 emitters across the committed packages).
- Every generated method is one line over `cluster/spec/typed.go`
  (`SetPatch`, `StateOf`, `OnChangedOf`, `OnWriteOf`, `HandleOn`,
  `EmitOn`, `PatchValues`, `HandlerOf`); `cluster/spec/wrapper.go`
  (`Wrapper`) only holds the server. The typed server is not a
  `contract.ClusterServer` (asserted by reflect in `cluster/spec` and
  `cluster/spec/levelcontrol`), so it cannot be mounted; a typed server is
  never wrapped twice for the same reason. The value conversion (width integer → enum/bitmap,
  `spec.List[T]` → `[]T`, generic lists entry by entry, nil → nil pointer /
  null `Nullable`) is tested once (`typed_internal_test.go`).
- The `// Mirrors` comments are in `hull.go` only: `Endpoint.ts:256-262`
  (`set`), `Datasource.ts:202` (`$Changed`), `Datasource.ts:1172-1195` +
  `Participant.ts:16-23` (the pre-commit veto), `on-off-light.ts:48-100`
  (the behavior-method override). `Emit<Event>` carries no citation: the
  brief names no matter.js line for it.

Committed clusters whose `State` lacks a field (named in the State doc):

| cluster | attribute | why |
| --- | --- | --- |
| Binding | Binding (0x0000) | fabric-scoped: `spec.NewServer` refuses to serve it |
| Groupcast | Membership (0x0000) | fabric-scoped |
| EcosystemInformation | DeviceDirectory, LocationDirectory | fabric-scoped |
| Messages | Messages (0x0000) | fabric-scoped |
| ScenesManagement | FabricSceneInfo (0x0002) | fabric-scoped |
| ScenesManagement | DoNotUse (0x0000) | typed `any` |
| IcdManagement | RegisteredClients (0x0003) | fabric-scoped |
| OtaSoftwareUpdateRequestor | DefaultOtaProviders (0x0000) | fabric-scoped |
| DoorLock | SecurityLevel (0x0034) | typed `any` |
| GeneralDiagnostics | DoNotUse (0x0009) | typed `any` |

(The fabric-scoped ones are left out because the generated server cannot
serve them; over a self-held server that can — `core.Binding` — a typed
field could be read through `MatterReadFiltered`. Not done: see Questions.)

## The hook

`spec.ServerConfig.BeforeWrite func(ctx, attrID uint32, value any) error`
and `Server.SetBeforeWrite`. `MatterWrite` calls it after
`ValidateWrite` and `stored()`, **before the Sink** and the store, with the
stored form. Its error is returned as is when it carries an
`im.StatusCodeError` (a cluster status included); otherwise it is wrapped
(`vetoError`, which unwraps to the host's error) and answers FAILURE. The
reason: the dispatcher's `writeErrorStatus` falls back to a string
heuristic (`"constraint"` → CONSTRAINT_ERROR, `"read-only"` →
UNSUPPORTED_WRITE), so a plain host message would otherwise pick its status
by its wording. The Sink's errors keep today's path.

`Datasource.ts:1172-1195` at the pin is `preCommit()`: for each field
changed since the last pre-commit cycle that has an observed
`<field>$Changing` event, it emits `(newValue, oldValue, session)` before
the commit; a throw is a pre-commit failure, which rolls the transaction
back (`Participant.ts:16-23`: "pre-commit failure | preCommit*, rollback").
So it is the pre-commit veto the concept assumes — with one difference:
it fires for every change in the transaction (a host's `set` too), the Go
hook only for a controller's write.

## devicegen

Every accessor returns `<pkg>def.Wrap(d.Server(id))`: 219 accessors in 74
device files over 105 clusters, OnOff (20), Binding (12) and
EcosystemInformation (1) included. `script/devicegen/servers.go`'s
`servers` table is gone (every accessor has one type rule now);
`device/device.go` is unchanged against main apart from a comment — no
mount wrap. The generated device test holds each accessor to
`Underlying() == Device.Server(id)` and to nil exactly when the cluster is
not mounted.

## Compile fixes

The accessor results that were used as the server itself now go through
`Underlying()` asserted to the server's type:
`cluster/onoff/onoffserver_parity_matterjs_test.go`,
`controller/client/client_{event_notification,node,subscribe}_parity_matterjs_test.go`
(`client_structure_offload_parity_matterjs_test.go` uses the typed
`EmitInitialPress` instead), `device/{device,subscriber,unimplemented_commands}_test.go`,
`examples/quickstart-device/main.go` (`light.OnOff().Get().OnOff`),
`examples/reference-bridge/{fleet,fleet_climate,fleet_lighting}.go` and
`conformance_test.go`, `examples/reference-controller/cli/main_test.go`,
`fabrictest/guide_test.go`, and six test files under `node/`
(`attribute_subscription_response`, `binding_integration`, `binding_live`,
`binding_persist`, `command_invoke_response`, `dot_qualified_constraint`).
The `node/` ones are BS's path: test files, one call site each.
`device_test.go` `TestWithServerReplacesTheDefault` now expects the
accessor to wrap the host's server (it expected nil before).

## Guards

Run in the background with a summary log; the machine had 5 cores at a
load of 13–18 (another agent's `go test -race ./node/` and
`go test ./...` ran alongside), which is what the timing failures below
have in common.

| guard | HEAD | exit | note |
| --- | --- | --- | --- |
| `make ci` | 8d14e734 | 2 | build, vet 0; `test` red on a timing test only, a different one per run, each green in isolation (below); `lint` 0 issues; `bluez-check` 0 |
| `make cover-check` | 8d14e734 | 2 | the report carried the flaky FAIL of `controller/interaction`; with that package's isolated rerun (92.7 %) spliced in, `script/coverfloor`: 228 packages at or above their floor, no floor moved |
| `make race` | 20ee8b1f | 2 | `controller/client` and `fabrictest` hit the 10-minute package timeout; no `DATA RACE` in the log |
| `go test -race -timeout=30m ./controller/client/ ./fabrictest/ ./controller/interaction/` | 8d14e734 | 0 | the timed-out packages again |
| `go test ./script/clustergen/... ./script/devicegen/...` | 20ee8b1f | 0 | |
| `make reachability-check` | 8d14e734 | 0 | unreached 5 (the ratchet) |
| `make cite-check` | 20ee8b1f | 0 | 5 new citations (the hull template), no new drift |
| `make testparity-check` | 20ee8b1f | 0 | |

The timing failures across the five full runs, none in a package BT
changed and none using the typed accessors: `controller/interaction`
`TestLiveSustainedSurvivesRestart` (three runs; 13 isolated runs green,
BH's report records a hang of the same test), `node` `TestOtaParityMatterJS`
(once; 5 isolated runs green), `controller/client`
`TestNodeLifecycleSeededPairParity` ("[fe80::1]:5540 is in use", once) and
`TestRegistryDecommission` (once), `secure/operational`
`TestStartReaper_ActiveSessionSurvivesSweeps` (once, the wall-clock test
BH names), `fabrictest` `TestNetworkLossAndDelay` (once, under `-race`).
One earlier run also failed to link for a full `/tmp` (stale `go-build`
directories of killed runs, removed). CI on the pull request is the
clean run.

## Differences to the brief

1. The device does not mount the typed server; accessors wrap on access,
   and the typed server forwards nothing (the addendum's forwarding
   requirement is withdrawn in review). The dispatcher skips a check for
   an *absent* lister (`endpoint/dispatcher.go:407`, `:431`, `:494`,
   `:614-618`, `:781`, `:992`), which no forwarded value can express — a
   mounted wrapper over a server without one would change the wire.
   Wrapping at mount also broke type assertions on mounted servers
   (`fleet.go:738`, `device_test.go:231`). A call that used to reach the
   server through the accessor (`MatterRead`, `MatterAcceptedCommands`, …)
   now goes through `Underlying()` — the compile fixes below.
2. The generated `definition_gen_test.go` gains `TestTypedServer` (every
   hull method once, over a server of an empty definition with the
   cluster's id). The brief said no per-package hull test; without it the
   119 packages fall below their 100 % floors and every hull method is
   unreached for the reachability ratchet. No floor moved.
3. `OnWrite`'s veto runs before the Sink (the brief: "after the check and
   before the store"); a vetoed write never reaches the device's port.
4. `OnWrite` and `Handle<Command>` return an error (`ErrNoWriteVeto`,
   `ErrNoHandlers`) — the addendum's rule for `Set`, applied to every
   operation that needs an optional interface; `spec.BeforeWriteSetter` and
   `ErrNoWriteVeto` are additions of mine for that.
5. The server is held in an embedded `spec.Wrapper` rather than a field
   `srv` of each generated type, so `Underlying()` is written once.
6. LevelControl defines no event at the pin; the hand-written event case
   is `cluster/spec/booleanstate/server_typed_test.go` (StateChange).

## Decisions for the owner

- **No forwarding** (review of #198): `spec.Wrapper` holds the server and
  exposes `Underlying()` only; the typed server is not a
  `contract.ClusterServer` and is never mounted.
- **What the hull can do over which server**: the generated `spec.Server`
  — Set, Handle, Emit, OnWrite all. Self-held servers that embed
  `*spec.Server` (DoorLock, the media servers, Actions, CommissionerControl,
  PowerSourceConfiguration, OtaSoftwareUpdateProvider,
  WiFiNetworkDiagnostics, CommodityMetering, Switch;
  ThermostatUserInterfaceConfiguration with its own `SetAttributes`) — the
  same. `onoff.Server`, `core.Binding`, `core.EcosystemInformation` and the
  other self-held servers (`cluster/levelcontrol`, `light`, `thermo`'s
  Thermostat, `cover`, `fan`, `pump` — whose `Emit(event uint32)` has
  another signature) — Get and OnChanged; Set, Handle, OnWrite and Emit
  refuse with the `Err…` above, as the addendum says (no generic setter
  added).
- **The accessor break for the three mapped servers**: `OnOff()`,
  `Binding()`, `EcosystemInformation()` return `*onoffdef.Server`,
  `*bindingdef.Server`, `*ecosysteminformationdef.Server`; the self-held
  API is `Underlying().(*onoff.Server)` — the compile fixes above show
  what that costs a host (`d.OnOff().Underlying().(*onoff.Server).Set(true)`
  for what was `d.OnOff().Set(true)`). The typed `Set(Patch)` refuses over
  `onoff.Server` (no `SetAttributes`), so the typed form does not yet
  replace the old one for OnOff: the self-held servers need an
  `AttributeSetter` (BU) before the host's code gets shorter, not longer.
- An accessor now answers for a host's server of any type; it answered nil
  for one of another type.
- `Set(Patch)` without a context, as `spec.Server.Set` (the concept's
  example has `Set(ctx, …)`).
- `Patch` for a nullable attribute is `*T` plus `Null []uint32`, so the
  concept's `levelcontroldef.Patch{CurrentLevel: ptr(uint8(128))}` compiles
  as written.
- Tag-driven reflection rather than per-field generated code; a value of
  an unexpected type leaves the field zero rather than failing.
- `HandlerOf` answers INVALID_COMMAND for a payload of another type (as
  `cluster/boolcfg` does) and accepts nil for a fieldless request.
- CHANGELOG names the accessor change a break released as `v0.10.0`; the
  brief's header names this wave `v0.9.2`.

## Left out

The declaration (`<cluster>def.Declare`), transitions, the daemon, docs
(BU/BS); an `AttributeSetter` / `CommandRegistrar` / `EventEmitting` on
the self-held servers (the addendum: not in this wave); typed fields for
fabric-scoped attributes. `docs/adr/0018` (`:154`) and the device pages
still describe the `*spec.Server` / `*onoff.Server` accessor types — docs
are BS's/BU's.

## Questions (answered in review)

1. The self-held servers get `AttributeSetter`, `CommandRegistrar` and
   `EventEmitting` in BU: yes.
2. A typed server is never mountable: now enforced (no
   `contract.ClusterServer`).
3. The veto stays on controller writes only; the coordinator writes the
   `by_design` entry.
4. The release number is the owner's decision.

## by_design candidates

- The write veto fires for a controller's write only; matter.js's
  `$Changing` (`Datasource.ts:1172-1195`) fires for every change of a
  transaction. A host's own change is the host's decision in Go.
- A plain veto error answers FAILURE rather than the dispatcher's text
  heuristic (matter.js's mapping of a thrown plain `Error` not checked in
  this wave — verify before an entry).
