# Brief A — the generated default cluster server (ADR 0017, phase 0)

Owner: Fable (review). Implementer: Opus. Branch `feat/spec-server`.

## Goal

A type in `cluster/spec` that turns a generated definition plus a feature
selection into a complete `contract.ClusterServer`, so that a cluster with
no matter.js server logic costs a definition, a registration and a host
port. Proof: `cluster/filter` runs on it with its public API and its tests
unchanged.

## Facts

- matter.js mounts, for most clusters, the behaviour generated from the
  model; the server file adds nothing:
  `packages/node/src/behaviors/fixed-label/FixedLabelServer.ts` is
  `export class FixedLabelServer extends FixedLabelBehavior {}`. What makes
  it a full server is `packages/node/src/behavior/cluster/ValidatedElements.ts`
  (element lists from model and features) and the model's constraint and
  conformance validation on write.
- `cluster/spec.Instance` (`cluster/spec/instance.go`) already answers
  `MatterClusterID`, `MatterAttributes`, `MatterReportable`,
  `MatterAcceptedCommands`, `MatterGeneratedCommands`, `MatterEvents`,
  `ReadGlobal`, `MinReadPrivilege`, `MinWritePrivilege`,
  `MinInvokePrivilege`, `IsTimed`, `EventPriority`, `EnumSupported`, and
  `ValidateWrite` (`write.go:52`). What it lacks is state, dispatch and
  reporting.
- `cluster/filter/filter.go` is the pattern a server on `Instance` has
  today: `newServer` (179–214) builds the `Options` from `Config`, `MatterRead`
  (300–324) answers `ReadGlobal` first, then `Serves`, then the state;
  `MatterWrite` (330–355) runs `ValidateWrite`, then the host writer, then
  `apply`; `apply` (273–295) stores, computes the changed attribute ids,
  bumps the `cluster.DataVersionTracker` and calls `Notify`.
- The contract interfaces the dispatcher consults are in
  `contract/matter.go`: `ClusterServer` (44), `FabricScopedReader` (91),
  `ClusterAttributeLister` (111), `ClusterCommandLister` (124),
  `ClusterDataVersion` (144), `ClusterEventLister` (157), the three
  privilege interfaces (177, 199, 221), `AttributeChangeNotifier` (636),
  `EventReceiver` (702). The event emission pattern is
  `cluster/alarm/smokecoalarm_server.go:644–709`.
- The bridge encodes any value implementing `spec.Encodable`
  (`bridge/application_values.go:110`), so generated struct and list types
  need no bridge change.

## Scope

Must:

1. `cluster/spec/server.go`: `type Server struct{ *Instance; … }` built by
   `NewServer(def *Cluster, opts Options, cfg ServerConfig) (*Server, error)`.
   `ServerConfig` carries an optional `DataVersion *cluster.DataVersionTracker`
   (external tracker, as filter's `Config.DataVersion`), an optional read
   port `Source interface{ MatterAttribute(attrID uint32) (any, bool) }` for
   attributes the device owns, an optional write sink
   `Sink interface{ MatterWriteAttribute(ctx, attrID, value any) error }`,
   and initial values `Initial map[uint32]any`.
2. Reads: `ReadGlobal` → `Serves` → `Source` → stored value → absent
   (`nil, false`) as filter does.
3. Writes: `ValidateWrite` → `Sink` (if set) → store → data-version bump →
   `Notify` for the changed id. A write to an attribute the definition does
   not serve or does not allow is what `ValidateWrite` already answers.
4. `Set(attrID uint32, value any) error` for the host: the device-side
   change (validated against the definition's type and constraint, not its
   access), bump + notify on a change, no-op on an equal value.
5. Commands: `Handle(cmdID uint32, fn func(ctx context.Context, fields any) (any, error))`;
   `MatterInvoke` answers an unregistered or unaccepted command with
   `im.UnsupportedCommandf`, as filter does (`filter.go:358`).
6. Events: `SetMatterEventEmitter` (contract.EventReceiver) and
   `Emit(endpoint uint16, eventID uint32, data any)` with the priority from
   `EventPriority`; emitting an event the selection does not include is an
   error.
7. `cluster/filter` rebuilt on `spec.Server`: same exported API (`Config`,
   `State`, `SetState`, `Resetter`, `LastChangedTimeWriter`, constructors),
   same tests passing unchanged; the rules (`checkProducts`, `check`, the
   ResetCondition logic) stay in `filter`, the dispatch goes.
8. Tests for `spec.Server` alone over two generated definitions with
   different shapes (one with a list attribute, one with a command and an
   event), covering read/write/set/notify/command/event and the refusals.

Should: a second proof with no rules at all — `FixedLabel` (0x0040): add it
to `script/clustergen/clusters.go`, regenerate, and expose
`cluster/core.NewFixedLabel(labels)` as a `spec.Server` with a fixed
`LabelList`. Not mounted in the reference daemon in this PR.

Not in scope: fabric-scoped attributes (`FabricScopedReader`) — refuse a
definition that has one with a clear error; attribute transitions; any
change to `endpoint/`, `bridge/`, `schema/`, or the hand-written schema
tables (they stay, ADR 0013 "What stays as it was").

## Guards

`go test ./cluster/...`, `make cover-check` (new package files need floors
in `script/coverfloor/floors.go`), `make reachability` + check (regenerate
and commit in the same PR), `make lint`, `make ci`. The filter parity test
`cluster/filter/filter_parity_matterjs_test.go` must pass without edits.

## Do not touch

`bridge/`, `endpoint/`, `cluster/modebase/` (brief C owns it),
`cluster/wire/` (brief B), `schema/*.go` tables.
