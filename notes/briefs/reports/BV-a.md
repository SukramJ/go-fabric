# Report BV-a — the controller's typed surface, owned aliases, `controller.New`, apidiff

Branch `m4/phase-20d` from `origin/main`. It started at `v0.10.2` (bd168c7b), the paused WIP commit c2203e4c, and was rebased on 5f71dba2 (#235/#237, docs only). Brief `notes/briefs/2026-10-22-BV-m4-phase-20d.md`, with its Update of 2026-10-23 and the owner's resume order of 2026-10-23. matter.js pin `862ad75f`. The daemon's composition and FeatureMap do not move, so there was no chip-tool run. No API break: apidiff reports no incompatible change against `v0.10.2`, so this ships as **v0.10.3**.

## §1 Values and typed reads (G2-8)

- `cluster/spec/value.go`: `spec.Value[T]` is an `Encodable` (the zero value is null), with:
  - `Uint` and `Int`, which keep a generated enum or bitmap type;
  - `Uint8` … `Int64`, `Bool`, `Float32`, `Float64`, `String`, `Bytes`;
  - `Null[T]()` for a nullable attribute's null.
  - matter.js encodes a written value by the attribute's model (`packages/protocol/src/action/request/Write.ts:149-185`, `TlvOfModel`). A Go value carries its type, so the constructor names it.
- `cluster/spec/peer_state.go`:
  - `spec.Values`, the interface a source of a peer's values implements, with `spec.AttributeValues` as a map implementation.
  - `spec.DecodeAttribute`, moved out of `client.Cluster.Decoded`. It cites `InputChunk.ts:291-302` and `:58-74`.
  - `spec.StateFrom[S](def, values) (S, absent)`, which runs over the generated `State.FromAttributeValues`. That is the one generated path the device's `Get` reads through as well (ADR 0026 P3). The typing cites `PeerBehavior.ts:42-63` (a known shape is the server's cluster behavior) and `ReadResult.ts:49-55`.
- Generated, by `script/clustergen`, in every package:
  - `<cluster>def.StateFrom(values)`.
  - A per-package test that sends a sample of every State attribute through the wire codec and holds the result to the device-side State. It uses `spectest.WireValues` and `spectest.EqualState`, the latter treating a nil slice as equal to an empty one.
  - That test found that nullable struct attributes had no `Attribute.Decode`, so they came back as `map[uint32]any`. The generator now writes one, with TLV null as nil. The only consumer of `Attribute.Decode` on the device's write path is three named attributes in `bridge/attribute_value_reader.go`, none of them nullable.
- `client.Report.Values(endpoint, cluster)`. A nil `*client.Cluster` reads as empty values.
- `Cluster.Decoded` stays as the untyped form, because named and unknown clusters still need it. Its doc now names the widening (a uint8 as `uint64`) and points to `StateFrom`. Changed line in the CHANGELOG; migration-guide section "The controller side in v0.10.3".
- Tests:
  - `controller/client/typed_reads_consumer_test.go`: the gate-2 consumer's controller program on the typed surface. It subscribes, reads, writes `spec.Int16(2250)` and reads back as `State`, without importing `tlv`, `im` or `interaction`.
  - `fabrictest/typed_read_test.go`: over the wire, `StateFrom(ReadCluster(…))` equals the device's `Get()` for OnOff, LevelControl and Thermostat. Every absent attribute is one the peer's AttributeList does not list. Also BridgedDeviceBasicInformation and a scalar write.

## The 10 s teardown (resume order step 1)

Cause: the device's `absorbStatusResponse` left the ack of a controller's StatusResponse to the ack pump's grace window (`bridge/receive_dispatch.go`). That StatusResponse is the last message of a device-initiated report exchange, so it has nothing to ride on. On a fake clock that stands still after the report, the ack never left. The controller's StatusResponse stayed in `exchange.awaitAck`, and `Subscriptions.Close` waited out `fabrictest`'s 10 s `closeTimeout`. A debug trace showed the report on exchange 2 answered and never acked.

Fix: a StatusResponse that no chunk loop waits for, arriving on an exchange the device initiated, is now acked at once. This mirrors matter.js `MessageExchange.ts:1117-1124` (close sends the standalone ack of the last received message immediately). `signalStatusResponseRX` returns whether a waiter took the status. Pinned by `bridge/report_final_ack_test.go`: no pump runs there, so without the fix there is no ack (checked: it fails on the old code). The consumer test went from 10.2 s to 0.2 s. Fixed line in the CHANGELOG.

## §2 Owned surface (G2-8)

- `controller/client/surface.go` defines these aliases, each documented with the fields a host sets:
  - Paths: `AttributePath`, `CommandPath`, `EventPath`, `DataVersionFilter`, `EventMinimumNumber`.
  - Read and subscribe: `ReadPaths`, `SubscribeRequest`, `Subscription`, `Report`, `AttributeData`, `AttributeStatus`, `EventData`, `EventStatus`.
  - Write and invoke: `WriteRequest`, `WriteOptions`, `WriteStatus`, `WriteStatuses`, `InvokeOptions`, `Result`, `StatusError`.
  - Peers and discovery: `PeerAddress`, `Device`, `SupportedTransports`, `Timing`, `SessionParameters`, `DeviceTypeViolation`.
  - Helpers: `AttributeAt`, `ClusterAt`, `CommandAt`, `EventAt`, `Interval`.
  - The package's own signatures are rewritten on these names, with no type change.
- `fabrictest/surface.go` has the same aliases, plus `Interaction`, `Peer`, `Session`, `Message`, and `Privilege` with its five constants. Its signatures use them too, and there is a new helper, `ReadCluster`.
- `docs/guides/advanced-seams.md` has four new rows: the controller program, its reads and writes, the device of a test, and a pair's transport.
- `controller.New(ctx, controller.Config)` in `controller/controller.go` is the role's composition root (ADR 0026 P2). It is a wiring, not a design: the quickstart's `open` moved over unchanged.
  - `Attestation` is required (`ErrConfiguration`).
  - `ManualStart` false starts the nodes, following matter.js `autoConnect` (`CommissioningController.ts:864-866`).
  - `Pair(ctx, code, PairOptions)` follows `commissionNode` (`:462-500`). Its attestation policy defaults to `Reject()`. A context without a deadline is bounded by `DefaultPairTimeout`; no answer is `ErrNoDevice`.
  - Also `Nodes`, `Close`, and the seams `Registry`, `Commissioner` and `Fabric`.
  - Tests: `controller/controller_test.go`. The quickstart controller, its test, and `docs/getting-started/controller.md` (stack, program, pairing, typed reads, reads and writes) are now on it.

## §3 apidiff (P9, decision 0.2)

- `go get -tool golang.org/x/exp/cmd/apidiff` adds `tool golang.org/x/exp/cmd/apidiff` and `golang.org/x/exp v0.0.0-20261014143641-f0251eb4aa3e`. It also raised `golang.org/x/tools` from 0.51.0 to 0.52.0, plus x/mod and x/sync, which x/exp requires.
- `script/apidiff` does the comparison:
  - It extracts the last tag with `git archive`, so it creates no worktree, then runs apidiff in module mode, `-w` on both trees and `-incompatible`.
  - `examples/`, `script/` and `conformance/` are left out; apidiff itself skips `internal/`.
  - On the tag itself it says there is nothing to compare.
  - `-check` fails when an incompatible symbol is not named, as a word, in the Changed, Removed or Deprecated section of `[Unreleased]`. That matching is unit-tested.
- `make apidiff` and `make apidiff-check` are new, and `apidiff-check` is part of `make ci`.
- CI job `apidiff` (fetch-depth 0, tags). After the edit `ci.yml` parses with 13 jobs, each with steps.
- README "How a consumer learns about a break" names the guard; the paragraph about the suspended window stays.
- `THIRD-PARTY-NOTICES.md` has a "Developer tools" section listing x/tools, x/exp, x/mod and x/sync with their BSD-3 copyright lines.
- Coverage floors: `controller` 72 and `script/apidiff` 35, each with a reason.

## Guards

GUARDS_TABLE

## Differences from the brief

- `fabrictest.Pair.Read(dev).<Cluster>()` is `fabrictest.ReadCluster(ctx, pair, ep, <cluster>def.ClusterID)` plus `<cluster>def.StateFrom`. Per-cluster methods would make `fabrictest` import all 159 def packages. `client.Node.ReadTyped[T]` and `Cluster.State()` are the same `StateFrom` over `ep.Cluster(id)`, because Go methods cannot be generic.
- "A `fabrictest` test per generated package" runs as a wire-codec test inside each generated package. One over-the-wire `fabrictest` test covers three clusters plus BridgedDeviceBasicInformation. A site per package would link the whole node stack 159 times.
- `client.ReadRequest` keeps its embedded field, named `ReadRequest`. Its type is now named `client.ReadPaths`, and its fields are promoted (`req.Attributes = …`). That avoids a break.
- `clock.Clock` (`Options.Clock` and `Wall`) stays unaliased, as gate 2 decided for `node` (foundation vocabulary).
- `discovery.Identifier` (`fabrictest.Controller.Discover`) keeps its type. `controller.Pair` takes a pairing code instead.
- Not in the brief: the teardown defect, the nullable-struct decoder, and the x/tools bump.

## Decisions

- No break, so v0.10.3. `client.ErrUntyped` is now the value `spec.ErrUntyped` (`errors.Is` unchanged).
- `controller.Pair`'s zero `OnAttestationFailure` rejects. The policy's own zero value proceeds (matter.js compatibility); the root does not inherit that.
- The ack fix applies only where no chunk loop waits. Between chunks, the next chunk still piggybacks the ack, as chip requires.

## Left out

- A successful `controller.Pair` end to end (DNS-SD into a site from a real socket). Its parts are covered: commissioning in `controller/client`, discovery in `client/discovery`.
- BV-b, which waits for the owner's go.

## Questions

- CLAUDE.md's "Five direct dependencies" sentence. Concept A.4 asks for the rule "every further module or tool dependency is a decision", with the current count. That count is now six direct modules plus one tool (x/exp). I left the sentence for the owner, as instructed.
- Should `apidiff` become a required check? The concept says that is the owner's decision.

## `by_design` candidates

- `controller.Pair` rejects on attestation failure by default, while matter.js's zero policy proceeds.
- `spec.Value[T]` names the width at the call site, because matter.js encodes the written value by the model.
