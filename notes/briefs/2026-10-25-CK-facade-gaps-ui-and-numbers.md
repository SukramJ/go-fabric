# Brief CK — the facade gaps go-fabric-ui U1 and go-ha2fabric H3 found, endpoint-number reservation, the reference CLI on `controller.New`

Date 2026-10-25. go-fabric `main` after CJ (#267) merged and `v0.12.1`
tagged. Sources: go-fabric-ui `docs/reports/U1.md` §Questions "go-fabric
facade gaps (CK)" (local `/home/markus/Dokumente/GitHub/go-fabric-ui`,
v0.1.1), go-ha2fabric `docs/reports/H3.md` Q1 (local
`/home/markus/Dokumente/GitHub/go-ha2fabric`, v0.2.0), go-fabric
`notes/briefs/reports/CJ.md` §Questions (the CLI), ADR 0025 (facade
principles), ADR 0026 P1/P12, ADR 0018 (device layer), concept v9 B.2
(`docs/concept-v9-fabric-ui.md`). Branch `m5/facade-ui-gaps`, commits
`m5 CK: <step>`, one pull request, no auto-merge, no label. Version:
**`v0.12.2`** if additions only; **`v0.13.0`** if §2 needs a store
migration a host must know about or §4 moves a CLI-visible flag — decide
and say which, with a migration-guide line for a Break. English. Never
add attribution lines. The owner merges.

**Model:** Opus. **Owner paths:** `secure/*`, `commissioning/*`,
`client/handshake`, ACL and the four `cluster/core` servers ADR 0013
skips stay untouched; §3 (window credentials) calls the existing exported
functions of `secure/setup`/`secure/spake2` as the CLI and the bridge do
today (read `bridge.RandomDiscriminator/RandomPasscode/RandomSalt` and
their callers), it does not change them. matter.js only via
`git -C /home/markus/Dokumente/GitHub/matter.js show 862ad75f:<path>`;
every new symbol: doc comment with the matter.js counterpart cited,
fabrictest or unit test, CHANGELOG line, docs entry (`docs/guides/ports.md`,
the controller guide, `docs/guides/advanced-seams.md` where a seam becomes
a facade route — then remove the seam's mention). go-fabric's `CLAUDE.md`
token rules.

## 1 The six U1 gaps (go-fabric-ui reads only the facade)

- **1.1 Runtime observers.** `node.Observers` exist only in `node.Config`.
  Add `func (n *Node) Observe(o Observers) (unsubscribe func())`: every
  hook of `Observers` that is set is called after the config's, in
  registration order, on the same goroutine rules as the config's; a
  returned unsubscribe is idempotent. Test: a handler mounted after
  `Start` sees a commissioning and a fabric removal.
- **1.2 Window snapshot.** `CommissioningWindow().Discriminator()` is 0 for
  the node's own and basic windows. Add `func (n *Node) Window() WindowSnapshot`
  (`Open bool; Kind (Own|Basic|Enhanced); Discriminator uint16; OpenedAt,
  Deadline time.Time; Timeout time.Duration`), the same value the window
  observer receives; read `node/commissioning.go` and the window's state
  machine; cite matter.js `CommissioningServer.ts`'s window state.
- **1.3 Setup payload.** `func (n *Node) SetupPayload() (SetupPayload, error)`
  with `Discriminator, Passcode, VendorID, ProductID, QR, Manual` from the
  configured credentials (what `PairingCodes` encodes); the passcode is in
  it by design (the admin route needs it) and the doc comment says so.
- **1.4 Cluster and device-type names.** `BridgedDevice` (node.Topology)
  gains `Clusters []ClusterInfo{ID uint32; Name string; Revision uint16}`
  and `DeviceTypeRevision uint16`, filled for every endpoint including
  parts and snapshotter endpoints (read how `Added.Spec().Source.MatterClusterServers()`
  is empty for them and fix at the assembler: the topology walks the
  mounted servers, not the declaration). Names and revisions from
  `schema` (generated; `schema.ClusterName(id)`, `schema.ClusterRevision(id)`
  if missing — generated, not hand-written). Keep `contract/` stdlib-only.
- **1.5 Fresh window credentials.** `func NewWindowCredentials(timeout time.Duration) (WindowCredentials, error)`
  in `node`: random discriminator, a passcode that passes the setup-PIN
  rule (cite `PaseClient.ts`/`setup` where the invalid passcodes are
  listed; read `bridge.RandomPasscode` and move the rule behind one
  exported function the bridge calls too), iterations and salt as the
  bridge draws them; the UI and loom's window adapter use it.
- **1.6 Session diagnostics and the clock.** `func (n *Node) SessionDiagnostics() SessionDiagnostics`
  (`Sessions []SessionInfo{ID, FabricIndex, PeerNodeID, LocalNodeID, PASE bool,
  Subscriptions int, LastActivity, LastPeerActivity time.Time}; Occupancy{Live,
  Reserved, Capacity, Free}`), read-only, ages computed on the node's clock;
  and `func (n *Node) Clock() clock.Clock`. Replace the `SessionManager`/
  `SubscriptionManager` aliases' mention in `advanced-seams.md` where this
  covers them.

## 2 Endpoint-number reservation per stable key (H3 Q1; a parity gap)

matter.js reserves an endpoint number per endpoint store: `ServerEndpointStores.assignNumber`
(`packages/node/src/storage/server/ServerEndpointStores.ts:20-22, 109-132`
at `862ad75f`: "Each number is reserved for one store. An installed endpoint
keeps its number", `__nextNumber__` persisted `:13, :37-43`) and a closed
endpoint keeps its reservation (`ServerEndpointInitializer.ts:68`); only
`eraseStoreForEndpoint` (`:64`) frees it. go-fabric's Aggregator gives a
removed stable key's number away and numbers a re-added device anew.
Mirror matter.js: the Aggregator keeps the number of a removed stable key
in the endpoint persistence (`node.NewEndpointPersistence`, `store.Endpoints`
area — read it; add what is missing without an SQL migration if the
existing table carries a free column, else migration v8 with a
migration-guide line) and hands it back on `Add` of the same stable key;
`RetirePartition`/an explicit `Forget(stableKey)` frees it (the
counterpart of `eraseStoreForEndpoint`). Parity test ported from
matter.js's endpoint-number tests (find them: `grep -rl "assignNumber\|nextNumber" packages/node/test`),
and `endpoint_number_conflict_parity_matterjs_test.go` extended. Update
`docs/guides/device.md` (or where numbering is described) and the
comparison row if one exists. The reference bridge needs no change; say
whether its composition moved (chip-tool run only if it did).

## 3 go-fabric-server note

The JSON route (CJ 1.1) means go-fabric-server's `internal/wire`
exception can go; that is S3's work. Nothing to do here beyond a line in
the report.

## 4 The reference CLI on `controller.New` (CJ's question, owner yes)

`examples/reference-controller/cli` keeps its own DNS-SD and BLE scans
because it does not build on `controller.New`. Move it: a
`controller.Config` seam for what the CLI configures by hand (read
`cakey.go`, `pair.go`, `cli.go`: the CA key file, the DCL trust setting,
the BLE adapter) — `controller.Config.CAKey` (or `Credentials`),
`controller.Config.Trust`, `controller.Config.BLE` as the facade already
names them after CJ; then `cmdPair` uses `Controller.Discover` and
`PairOptions.BLE`, and the CLI's `dialBLE` and discovery code go. Flags
and output stay the same (the manual test plan `notes/test-plans/` drives
them); `args_test.go`, `pair_ble_test.go` move onto the facade. If a flag
has to change, it is a Break for the CLI only — say so in CHANGELOG.

## 5 Docs, tests, report

CHANGELOG under [Unreleased]; `docs/guides/ports.md` / controller guide /
`advanced-seams.md` / `testing-in-process.md` as each § says;
`docs/matterjs-comparison.md` rows touched (endpoint numbering). Guards:
`make ci`, `make race` (twice), `make reachability-check`, `make cite-check
MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`, `make apidiff-check`,
`go test ./script/...`, `mkdocs build --strict`, `make stress-flakes`;
chip-tool only if the reference bridge's composition or FeatureMap moved.
Background runs with a summary log; `df -h /` before, `go clean -cache
-testcache` after. Report `notes/briefs/reports/CK.md` (PR body): per §
with commit hashes; guards with HEAD; differences; decisions; left out;
questions with the symbol needed; `by_design` candidates with citations
(you do not write by_design/ADR/findings entries). Copy this brief to
`notes/briefs/2026-10-25-CK-facade-gaps-ui-and-numbers.md` in the first
commit. If a permission is denied, say which and stop that step; never ask
the launcher to perform it. Hand back with the PR number, HEAD, the
version decision and the Questions and by_design sections verbatim.
