# Report Z — host observers and per-window credentials on the node facade (issue #58)

Branch `wave9/node` from `planning/briefs-wave-9` (f6f221e), pushed, no PR.
matter.js read at the pin `4633bfd7` (`git -C ../matter.js show
4633bfd7:<path>`); line numbers below are the pin's. Every commit carries its
regenerated reachability inventory (unreached unchanged at 5) and a CHANGELOG
entry under `[Unreleased]`. `secure/` and `commissioning/` are untouched.

| Commit | Item |
| --- | --- |
| d73aa27 `node: Config.Observers — fabric, commissioning-complete and window observers chained after the facade's handlers` | 1 |
| 1e1cfdf `node: OpenBasicWindow — the node's own window with per-window credentials, CM=1 under its discriminator, configured credentials back on close` | 2 |
| f909430 `node: Config.SupportedFabrics and DisableTimeSynchronization` | 3 |
| (this file) `notes: report Z` | — |

## Already there (for the host, no code)

- **CM=2 for an administrator's enhanced window** is announced by the facade
  itself under the window's discriminator (`node/commissioning.go`,
  `transition`: CM=2 when `snap.Status == wire.WindowStatusEnhanced &&
  window.HasSuppliedVerifier()`; mirrors `DeviceCommissioner.ts:236-256`
  `#openWindow`, `CommissioningMode.Enhanced` iff `EnhancedWindowOpen`). Issue
  point 3's second half needs no host hook; `OnWindowTransition` reports it
  (`CommissioningMode` 2, `Discriminator` the window's).
- **Concurrent pairings are always on**: `node/security.go`
  `PerExchangePaseProvider` (reaper 30 s / TTL 1 min) for the configured
  passcode, and the same provider type for every window's verifier
  (`installVerifierContext`). No `ConcurrentPairings` field was added (open
  question 1).

## What was built

### 1. Observers (`node.Config.Observers`)

Name: `Observers`, not `Hooks` — matter.js's node events are `Observable`s
that observers attach to (`FabricManager.ts:41-48` `#events`), and these
callbacks only observe: they run after the facade's handler, cannot change
its outcome, and their failures are logged — matter.js
`FabricManager.ts:381-386` `unhandled` ("Log unhandled errors but do not
abort deletion").

`node/observers.go`: `Observers{OnFabricAdded, OnFabricRemoved,
OnFabricUpdated func(FabricInfo) error; OnCommissioningComplete
func(fabricIndex uint8) error; OnWindowTransition func(WindowSnapshot)
error}`; `FabricInfo{FabricIndex, FabricID, NodeID, RootPublicKey,
CompressedFabricID}`; `WindowSnapshot{Open, Own, Administrator
wire.WindowStatusSnapshot, CommissioningMode, Discriminator}`. Each observer
is optional; an error is logged (`node.observer.failed`), a panic is
recovered and logged (`node.observer.panic`); nothing is returned to the
handler. Observers run synchronously on the event's goroutine.

The facade's handlers are unchanged and stay where they were wired; each
hook the facade sets now calls a wrapper that runs the facade's handler
first and the observer second.

**Call order of every observer relative to the facade's own handler:**

| Observer | Wired at | Order |
| --- | --- | --- |
| `OnFabricAdded` | `OpcredsConfig.OnFabricInstalled` → `Node.observedFabricInstalled` | OperationalCredentials AddNOC persists the fabric, re-arms the fail-safe, then calls the hook → **facade `fabricInstalled`** (CASE identity load, `AnnounceFabric` of the operational record) → **observer**. Runs inside AddNOC, before the NOCResponse. Runs even when the facade's handler returned early (identity load failed). `FabricInfo` from the hook's arguments, compressed id from the stored fabric. |
| `OnFabricRemoved` | `OperationalCredentials.SetOnFabricRemoved` → `Node.observedFabricRemoved` | OperationalCredentials removes the fabric from the store (FK cascade: ACL, keys, identity) and runs its own `notifyFabricRemoved`, then the hook → `FabricInfo` captured from the facade's CASE identity (the store no longer has it) → **facade `fabricRemoved`** (BasicInformation Leave, OTA providers, CASE identity forgotten and operational record withdrawn, `EmitFabricRemovedContext`: other sessions told with CloseSession and closed, the invoking session's close deferred until after the response, then `reopenIfDecommissioned`) → **observer** → (OperationalCredentials' own withdraw hook) → NOCResponse → the invoking session closes. Also on the fail-safe rollback of an AddNOC (`cluster/core/operational_credentials.go`, the fail-safe expiry revert). |
| `OnFabricUpdated` | `OperationalCredentials.SetOnFabricUpdated` → `Node.observedFabricUpdated` | UpdateNOC (or its rollback) persists, then the hook → **facade `fabricUpdated`** (CASE identity reload, operational record moved, `CloseFabricSessionsExcept`) → **observer** with the fabric as the store now holds it → NOCResponse. |
| `OnCommissioningComplete` | the closure `buildRoot` gives `GeneralCommissioning.SetOnCommissioningComplete` | GeneralCommissioning disarms the fail-safe, then the hook → **facade body** (`ClearPendingState`, OTA requestor `Commissioned`) → **observer** (`rootDeps.onCommissioningComplete`) → GeneralCommissioning's `onCommissioned` (the window's `EndCommissioning`) → response. So the window is still open when the observer runs; its close follows as an `OnWindowTransition`. |
| `OnWindowTransition` | the window's `SetTransitionHook` | The bridge's `transitionHooks` run the bridge's own attribute reporting first, then the facade's hook: **facade `transition`** (PASE acceptor switched, commissionable record published or withdrawn) → **observer** → (only after a basic window closed on a node without a fabric) `reopenIfDecommissioned`, whose own transition runs facade then observer again. A host therefore sees close, then open. |

Tests (`node/observers_test.go`, `bridgetest.Commissioner`):
`TestObserversOnCommissioning` (events `[added complete]` in that order;
`FabricInfo` matches the commissioner's fabric, node id, compressed id and a
65-byte root key; the operational record is published when `OnFabricAdded`
runs; an error from `OnCommissioningComplete` does not fail the
commissioning), `TestObserverOnFabricRemoved` (when the observer runs:
`Fabrics()` empty, `ListACL` empty, no operational record, own window
reopened; `FabricInfo` is the removed fabric's), `TestObserverOnWindowTransition`
(Start opens the own window, `RevokeWindow` closes it: exactly two calls,
open with CM=1 / D=3840 / record published, close with CM=0 / record gone; a
panicking first call does not stop the second). Checked: moving the
observer before the facade's handler fails the added and removed tests.

### 2. `Node.OpenBasicWindow(ctx, WindowCredentials)`

Name: `OpenBasicWindow` — CM=1, passcode-based, as matter.js's
`openBasicCommissioningWindow`; `OpenWindow` would collide in meaning with
`CommissioningWindow.OpenWindow` (the administrator's enhanced window).

- `WindowCredentials{Discriminator uint16; Passcode uint32; Iterations
  uint32; Salt []byte; Timeout time.Duration}`, validated as `Config.Validate`
  validates the configured secret (12-bit discriminator, valid passcode,
  iterations in `[spake2.IterationsMin, IterationsMax]`, salt
  `spake2.PBKDFMinSaltSize..PBKDFMaxSaltSize`, timeout ≥ 0) →
  `ErrInvalidWindowCredentials`.
- The verifier: `spake2.NewVerifierContext` — the function `commissioning/`
  uses (`commissioning/pase.go:80`). Not reimplemented.
- Installation: `InstallVerifier`'s body became
  `commissioningHost.installVerifierContext(vc, iterations, salt)`;
  `InstallVerifier` (the administrator's 97-byte verifier) and
  `OpenBasicWindow` both call it. One acceptor at a time, per-exchange
  provider with its reaper, restore on close.
- The window is the node's own (`OpenOwnWindow(ctx, Timeout)`; 0 is its
  default, 48 h without a fabric, 15 min with one): AdministratorCommissioning
  reads WindowNotOpen, an administrator's OpenCommissioningWindow replaces it,
  CommissioningComplete and RevokeCommissioning close it. While it is open
  the transition advertises CM=1 with the **window's** discriminator.
  Opened while an administrator's window is open:
  `bridge.ErrAdministratorWindowOpen`, checked before the install so the
  administrator's acceptor is not replaced. A second `OpenBasicWindow`
  replaces the first.
- On close (timeout, CommissioningComplete, revoke, replacement by an
  administrator) the window's acceptor is removed; then, if no window is open
  and the node has no fabric, the node's own window is reopened with the
  configured credentials and discriminator (`reopenIfDecommissioned`, the
  facade's existing rule). A commissioned node publishes no commissionable
  record after it. Window semantics unchanged: one window at a time,
  commissionable only while open.
- matter.js has no per-window basic credentials
  (`AdministratorCommissioningServer.ts:105-121`, "The default passcode is
  used"; `DeviceCommissioner.ts:236-256` advertises `discriminator ??
  commissioningConfig.discriminator`). The doc comment says so.

Tests (`node/basic_window_test.go`): `TestOpenBasicWindowPairsWithItsCredentials`
(random credentials; record D = window's, CM=1; the commissioner holding only
those credentials commissions; afterwards no record, window closed),
`TestOpenBasicWindowTimeoutRestoresConfigured` (uncommissioned node: record
D = window's; after the 300 ms timeout the record is D=3840 CM=1 and the
configured passcode commissions), `TestOpenBasicWindowOnCommissionedNode`
(record while open, none after the timeout; `OnWindowTransition` exactly
open (CM=1, D = window's) then closed), `TestOpenBasicWindowRefusals` (five
invalid credentials refused, record unchanged; administrator window open →
`ErrAdministratorWindowOpen`). Checked: without the discriminator override
three tests fail; without the reopen the timeout test fails.

### 3. Configuration knobs

- `Config.SupportedFabrics uint8`: 0 → 5 (as before); above 254 refused by
  `Validate` (Matter §11.18.4.4 1..254; matter.js "FabricBuilder uses 254
  as max", `OperationalCredentialsServer.ts:85`). matter.js defaults to 254
  (`:84-88`); the facade keeps 5 per the brief.
- `Config.DisableTimeSynchronization bool` — the zero-value-keeps-today form
  (`TimeSynchronization *bool` would need a pointer for the common case).
  Set: no TimeSynchronization server, `RootClusters.TimeSynchronization` nil,
  ServerList without 0x0038, mount order otherwise unchanged.
  GeneralDiagnostics then has no `SetUTCClock`: TimeSnapshot still answers
  with `SystemTimeMs` (the server's monotonic start time) and **PosixTimeMs
  null** — matter.js `GeneralDiagnosticsServer.ts:168-182` sets
  `posixTimeMs` only when `agent.has(TimeSynchronizationBehavior)` and its
  `utcTime` is non-null. That is the fallback: no wall-clock time, not the
  host clock.
- **CASE identity preferences: nothing to configure.** `caseIdentities.load`
  (`node/security.go`) builds each fabric's identity from the stored fabric
  and identity rows that AddNOC / UpdateNOC wrote: NodeID and FabricID come
  from the commissioner's NOC, the key from the CSR the node generated, the
  IPK from AddNOC. The responder resolves the identity from the Sigma1
  destination id; the re-establishment initiator (`caseInitiatorProvider`)
  uses the same per-fabric identity. A host-chosen node id or fabric id would
  contradict the NOC. Skipped, as the brief allows.

Tests (`node/knobs_test.go`, over PASE): `TestSupportedFabrics` (default 5,
configured 16, 255 refused), `TestTimeSynchronizationOptional` (mounted:
0x0038 listed, PosixTimeMs set; disabled: not listed, nil field, TimeSnapshot
answers with SystemTimeMs and null PosixTimeMs).

## Guards

All local, Linux, on this branch:

- `make ci` — green (build, vet, test, lint, cover-check: 173 packages at or
  above their floor; advice only: node 5.0 points above its floor).
- `CGO_ENABLED=1 go test -race -count=1 ./node/... ./bridge/...` — green
  (node 45.3 s, bridge 31.0 s, bridgetest 8.6 s).
- `make reachability-check` — green; unreached 5 (unchanged in every commit).
- `make chiptool-test` — green: `TestChipToolCommissionsReferenceBridge`,
  `TestChipToolSuite` pass; `TestMatterJSController` skips as on main (311
  PASS lines, 0 FAIL, 1 SKIP).
- `make chiptool-families GOFABRIC_CHIP_FAMILIES=CADMIN,OPCREDS,CGEN,IDM` —
  green, 4376.5 s (16:29 → 17:42), `TestChipCertificationFamilies` PASS.
  Cases: CADMIN 9 passed / 12 skipped, OPCREDS 6 / 3, CGEN 3 / 7, IDM 20 /
  13 (10.4 on every endpoint); **0 failed**. Every skip is the table's
  verdict (manual case, commissioner, PICS) as on main.

No test, PICS slice or golden of `internal/chiptool` was edited. The
reference daemon is unchanged: it sets none of the new fields, so it runs
with the defaults (5 fabrics, TimeSynchronization mounted, no observers).

## Test edits

None. Existing tests unchanged; the new tests are in three new files.

## Left out, and why

- **`ConcurrentPairings bool`** — the brief makes it a documented constant
  unless the owner decides otherwise; it is always on (question 1).
- **Firing `Bridge.EmitFabricAdded`** from the facade. The bridge hook stays
  unfired, as before; `Observers.OnFabricAdded` replaces it for a host on the
  facade (question 3).
- **CASE NodeID / FabricID preferences** — nothing a host can choose (item 3).
- **A minimum window duration for `OpenBasicWindow`.** The node's own window
  takes any positive timeout; matter.js's administrator path asserts
  180 s..900 s (`#assertCommissioningWindowRequirements`), which is not the
  path this mirrors. The tests use 300 ms.

## Open questions

1. **ConcurrentPairings**: a documented constant (no field) — confirm, or add
   a field that, when false, would make the provider single-exchange.
2. **Observer signatures** carry no `context.Context` and return `error`
   (logged). The brief gives `OnFabricAdded(FabricInfo)` etc. without either;
   the error return follows "errors are logged not returned". A host doing
   I/O in an observer has no request context — intended?
3. **`Bridge.SetOnFabricAdded` stays unfired under the facade** (and the
   issue lists it). Should the facade call `EmitFabricAdded` in its AddNOC
   handler, as `EmitFabricRemovedContext` already fires the bridge's removed
   hook?
4. **`OnCommissioningComplete` fires on every CommissioningComplete**, also
   for a second admin's; matter.js's `commissioned` event fires only when the
   fabric count crosses zero (`CommissioningServer.ts:157-171`). Keep per
   command, or add a separate "first fabric" observer?
5. **`WindowSnapshot` fields** are not specified by the brief; chosen: open,
   own, the AdministratorCommissioning snapshot, and the published CM and
   discriminator (what the facade advertised after the transition; a failed
   publish leaves the previous values). Adequate for the host's WS event?
6. **Restore after a basic window on an uncommissioned node** reopens the
   node's own window with a fresh default timeout (48 h), not the remainder
   of the window it replaced. Acceptable?
7. **`CommissioningWindow().OpenOwnWindow` while a basic window is open**
   restarts the window but keeps the basic window's credentials and
   discriminator (the basic window ends only on close). Should a host's
   direct own-window open end the basic window first?

## by_design / findings candidates (for the owner)

- by_design: **Per-window basic credentials** (`Node.OpenBasicWindow`) — a
  host extension; matter.js opens basic windows with the node's passcode and
  configured discriminator (`AdministratorCommissioningServer.ts:105-121`,
  `DeviceCommissioner.ts:236-256`).
- by_design: **SupportedFabrics default 5 in the facade** vs matter.js 254
  (`OperationalCredentialsServer.ts:84-88`); `cluster/core` defaults to 254,
  the facade passes 5 when unset.
- by_design (or finding): **`OnCommissioningComplete` per command** vs
  matter.js `commissioned` on the fabric count crossing zero (question 4).
- by_design: **Observers run synchronously before the command's response**
  (fabric events) — as matter.js emits its Observables inline; a blocking
  observer delays the NOCResponse.
