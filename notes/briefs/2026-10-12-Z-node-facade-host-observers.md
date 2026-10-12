# Brief Z — issue #58: host observers and per-window credentials on the node facade

Owner: Fable. Implementer: Opus. Branch `wave9/node` from
`planning/briefs-wave-9`, pushed, **no PR**. Report in
`notes/briefs/reports/Z.md`. Read first: `notes/briefs/README.md`, `gh
issue view 58`, ADR 0018, `node/doc.go`, `node/node.go:79-90` (which
bridge hooks the facade leaves to the host). Facts checked against the
code on 2026-10-12; two of the issue's points are already there (below).
matter.js at `4633bfd7` via `git -C ../matter.js show 4633bfd7:<path>`.
One commit per item, reachability in each. `secure/` and
`commissioning/` are not touched; what this brief needs from them is
already exported.

## Already there (tell the host in the report, no code)
- CM=2 for an enhanced window with a supplied verifier is announced by the
  facade itself with the window's discriminator:
  `node/commissioning.go:60-80` (`SetTransitionHook`: CM=1 on the own
  window, CM=2 when `snap.Status == wire.WindowStatusEnhanced &&
  window.HasSuppliedVerifier()`). Issue point 3's second half is done; the
  host needs no hook for it. Mirrors `DeviceCommissioner.ts:236-256`
  `#openWindow` (`CommissioningMode.Enhanced` iff `EnhancedWindowOpen`).
- Concurrent pairings are always on: `node/security.go:304-312`
  (`PerExchangePaseProvider`, reaper 30 s / 1 min). Issue point 4's
  `ConcurrentPairings bool` becomes a documented constant, not a field —
  unless the owner says otherwise (open question for the report).

## 1. Observers that run next to the facade's wiring
- Module: `node/node.go:197-198` (facade sets `SetOnFabricRemoved`,
  `SetOnFabricUpdated` on `OperationalCredentials`), `node/root.go:177`
  (`SetOnCommissioningComplete`), `node/commissioning.go:60`
  (`SetTransitionHook`), `bridge/bridge.go:1146` (`SetOnFabricAdded`, not
  wired by the facade), `cluster/core/operational_credentials.go`
  (`OpcredsConfig.OnFabricInstalled` carries fabric index, fabric id,
  node id, root public key — find the exact type).
- matter.js: `FabricManager.ts:42-107` (`events.added / deleted / replaced
  / leaving / deleting`, `:209`, `:253`, `:377`),
  `CommissioningServer.ts:157-171` (`commissioned` emitted on the
  lifecycle when the fabric count crosses zero).
- Must: `node.Config.Observers` (or `Hooks` — pick one name, say why)
  with `OnFabricAdded(FabricInfo)`, `OnFabricRemoved(FabricInfo)`,
  `OnFabricUpdated(FabricInfo)`, `OnCommissioningComplete(fabricIndex)`,
  `OnWindowTransition(WindowSnapshot)` — each optional, each **chained
  after** the facade's own handler (the facade's wiring never moves; the
  observer runs after it, errors are logged not returned). `FabricInfo`
  = index, fabric id, node id, root public key, compressed id. Tests in
  `node/` with `bridgetest.Commissioner`: commission → `OnFabricAdded`
  and `OnCommissioningComplete`; `RemoveFabric` → `OnFabricRemoved` after
  the facade's own cleanup (ACL gone when the observer runs); window open
  / close → `OnWindowTransition` twice.

## 2. A basic window with per-window credentials
- Module: `node/commissioning.go:144-` (`commissioningHost.InstallVerifier`
  for the enhanced window), `bridge/commissioning_window.go:63`
  (`PaseVerifierInstaller`), `node/config.go:95-103` (`Commissioning`:
  `Passcode`, `Discriminator`, `PBKDFIterations`, `Salt`),
  `node/node.go:236-261` (CM=1 advertisement with the configured
  discriminator).
- matter.js: `AdministratorCommissioningServer.ts:105-121`
  `openBasicCommissioningWindow` (the node's own passcode) — matter.js has
  **no** per-window basic credentials; the ephemeral-window pattern is a
  host extension (by_design candidate, list it). The facade therefore
  exposes it as its own API, not as a cluster path.
- Must: `Node.OpenWindow(ctx, WindowCredentials{Discriminator uint16;
  Passcode uint32; Iterations uint32; Salt []byte; Timeout time.Duration})`
  (or `OpenBasicWindow` — one name) that computes the verifier
  (`secure/spake2` has the function `commissioning/` uses — name it, do not
  reimplement), installs it through the same path `InstallVerifier` uses,
  advertises **CM=1 with the window's discriminator** (not the configured
  one), and on close restores the configured advertisement; the window
  obeys the facade's semantics (one window at a time, commissionable only
  while open — `node/commissioning.go:29-35`). Test: open with random
  credentials, `bridgetest.Commissioner` pairs with them, the mDNS
  record carries the window's discriminator, after the timeout the
  record is the configured one.

## 3. Configuration knobs
- Module: `node/root.go:146` (`SupportedFabrics: 5`), `:204-235`
  (TimeSynchronization always built and mounted), `node/security.go:351-`
  (`caseInitiatorProvider`, `caseIdentities`), `node/config.go`.
- Must: `Config.SupportedFabrics uint8` (0 → 5 as today),
  `Config.TimeSynchronization *bool` or `DisableTimeSynchronization bool`
  (pick the zero-value-keeps-today form; say which), and the CASE
  identity preferences the issue names — read `caseIdentities` first and
  report what a host could set (the per-fabric NodeID comes from the NOC;
  if there is nothing a host can legitimately choose, say so and skip it
  — that is an acceptable outcome, not a gap).
- Tests: `SupportedFabrics` read on OperationalCredentials; without
  TimeSynchronization the root's ServerList lacks 0x0038 and
  GeneralDiagnostics still has a clock (`SetUTCClock` fallback — say what
  it falls back to).

Not in scope: window semantics, `secure/`, `commissioning/`, the bridge's
hooks' own signatures, by_design / findings (list for the owner).

## Guards
`make ci`, `make race` (node, bridge), `make reachability-check`, `make
chiptool-test` locally, `make chiptool-families
GOFABRIC_CHIP_FAMILIES=CADMIN,OPCREDS,CGEN,IDM` locally. CHANGELOG under
`[Unreleased]`. Report as brief W; name every observer's exact call order
relative to the facade's own handler.
