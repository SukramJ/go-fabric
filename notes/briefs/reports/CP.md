# Report CP — the `largeMessage` ("L") quality in the snapshot, `LargeMessage` inferred on the client

Brief: `notes/briefs/2026-10-26-CP-large-message-quality.md`. Branch
`schema/large-message-quality`, rebased onto `origin/main` before the pull
request. matter.js checkout at `862ad75f4d3c7ab528b8a3954168146082cad8a6`
(the pin), `packages/model/dist/` present (not rebuilt).

## Per step

### 1. Extractor — `d19fbbdc`

`script/extract-from-matter-js.ts`: the resolved layer of each command and
each event gains `quality` through `qualityOut(m.effectiveQuality)`, beside
`access`. The raw layer is unchanged. Snapshot refreshed with
`make generate-matter-schema` at the same pin.

Snapshot diff (`git diff --stat`):

```
 parity/schema.json               | 117 +++++++++++++++++++++++++++++++++++++++
 schema/schema_provenance_gen.go  |   2 +-
 script/extract-from-matter-js.ts |   6 ++
```

The 117 lines are 39 × `"quality": { "largeMessage": true },` and nothing
else; `grep -c largeMessage parity/schema.json` = **39** (25 request
commands, 14 responses). No event and no attribute carries `L` at the pin.
clustergen and devicegen emitted no change.

### 2. Generator / `schema/` — `b24e6b90`

`script/generate_matter_schema.go` writes `schema/large_message_gen.go`
(`largeMessageCommands`, request commands only — the client probes the
command it sends); `schema.CommandLargeMessage(clusterID, commandID)` in
`schema/large_message.go`. Tests: `TestCommandLargeMessage` (pinned cases)
and `TestCommandLargeMessageMatchesSnapshot` (the generated map against the
snapshot, as `TestTimedInvokeMatchesSnapshot`). No attribute carries `L`, so
no `AttributeLargeMessage`.

### 3. Client — `df203e75`

`InvokeOptions.inferLargeMessage` (`client/interaction/invoke.go`), applied
in `Interaction.Invoke` and in `InvokeBatch` before the split, so every part
carries it; an explicit value (false included) stands; the options are a
value copy and the caller's pointer is never written. Cites
`ClientInteraction.ts:111-127` and `:716-719` (lines verified at the pin).
The facade needs nothing: `controller/client.InvokeOptions` is an alias of
`interaction.InvokeOptions`, and `Node.Invoke` / `Node.InvokeBatch`
delegate.

### 4. Tests — `cc2ecac0`

- `TestClientInteractionInvokeLargeMessageInference` — "does not mutate the
  caller's request when inferring largeMessage" (ClientInteractionInvokeTest.ts:633)
  on the scripted rig: ProvideOffer, one InvokeRequest sent, the caller's
  `LargeMessage` stays nil.
- `TestInferLargeMessageParity` — five cases of InvokeLargeMessageTest.ts
  (concrete L command, command without L, any one command of several, an
  unresolved path, explicit false preserved) plus the positive cases on the
  recording provider: an undecided CaptureSnapshot invoke requires TCP as
  `LargeMessage: true` does, an explicit false requires none, a split
  InvokeBatch with one L command requires TCP on both parts, a toggle
  requires none.
- Register: ClientInteractionInvokeTest.ts 14 → 15 cases ported, the
  largeMessage case removed from `missing`; InvokeLargeMessageTest.ts five
  cases off `missing`, reason rewritten. `notes/parity/matterjs_tests.md`
  re-rendered.

### 5. Notes and docs — `8a789354`, `75f2bb4f`

Findings entry "`largeMessage` is not inferred" removed; CHANGELOG
`[Unreleased]` Added (quality, `schema.CommandLargeMessage`) and Changed
(the inference; `SchemaSnapshotSHA256` moved); the controller guide's TCP
paragraph names the inference. `75f2bb4f` commits the regenerated
reachability snapshot, the citation artefacts (one line moved, drift count
unchanged), and the CHIP cross-check page.

## Guards (HEAD `75f2bb4f`, before the rebase)

| Guard | Result |
| --- | --- |
| `go test ./schema/... ./script/...` | ok |
| `make reachability` / `reachability-check` | ok (snapshot committed) |
| `make cite-check` | ok after committing the artefacts |
| `make testparity-check` | ok |
| `make chipdm-check` | ok after `make chipdm-report` (see below) |
| `make apidiff-check` | ok after the `SchemaSnapshotSHA256` CHANGELOG line |
| `go test -race ./client/...` | ok |
| `mkdocs build --strict` | ok |
| `make ci` | build, vet, test ok; golangci-lint hit its 5 min timeout once ("0 issues" for the run before it); lint and cover-check rerun separately — see the PR's CI |

## Differences from the brief

- More commands carry `L` at the pin than the brief lists: besides
  CameraAvStreamManagement, CommodityPrice and PushAvStreamTransport also
  TlsCertificateManagement, TlsClientManagement, WebRtcTransportProvider
  and WebRtcTransportRequestor (39 in all).
- The extractor emits the quality on events too (none carries one at the
  pin), so a later `L` or other event quality is not lost the same way.
- `make chipdm-check` is affected: command quality **is** a cross-check
  dimension (`internal/chipdm/compare.go` `quality`). With the quality
  absent, 39 command qualities were "not compared"; now they are compared
  and all 39 agree with CHIP (0 not compared). The generated block of
  `docs/chip-datamodel-crosscheck.md` moved (hash, count 39 → 0), and the
  `notCarried` notes for "command quality" / "event quality" in
  `internal/chipdm/compare.go` were reworded — a path the brief does not
  name.
- Five InvokeLargeMessageTest.ts cases ported beyond the one the brief
  names, since the inference made them portable at no cost.
- `SchemaSnapshotSHA256` is an apidiff value change; announced under
  Changed.

## Decisions

- `CommandLargeMessage` covers request commands only: matter.js probes the
  request command's schema (`Invoke.commandOf(cmd).schema`), and request and
  response ids of one cluster can coincide.
- The inference is a value-receiver method on `InvokeOptions`, applied once
  per call before the split.

## Left out

- InvokeLargeMessageTest.ts: the Quality DSL and CommandModel cases (matter.js
  model classes), the ClientCommandMethod pattern, legacy request, wildcard
  command, non-MalformedRequestError rethrow (no Go form), "does not batch L
  commands" (no automatic batcher).
- Raw-layer `quality` text for commands (only the effective layer is read).

## Questions

None.

## by_design candidates

None. The Go form differs from matter.js only where a Go invoke has no
legacy or wildcard command request (InvokeLargeMessageTest.ts:166-201 at
862ad75f); every Go invoke names a concrete path, and a path the snapshot
does not model infers nothing, which matches matter.js's swallowed
MalformedRequestError (`ClientInteraction.ts:121-124`).
