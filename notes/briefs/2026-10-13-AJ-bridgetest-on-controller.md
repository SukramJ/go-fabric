# Brief AJ — `bridge/bridgetest.Commissioner` onto `controller/` (ADR 0020, stage 2 close-out)

Owner: Fable. Implementer: Opus (one agent; nothing else runs on the repo
while you do). Branch `wave16/bridgetest` from `planning/briefs-wave-16`,
pushed, **no PR**. Report in `notes/briefs/reports/AJ.md`. Read first:
`notes/briefs/README.md`, [ADR 0020](../../docs/adr/0020-commissioner-and-controller-role.md)
(the stage-2 row: "`bridgetest.Commissioner` is a thin wrapper or gone"),
`README.md` §API stability (every exported identifier of
`bridge/bridgetest` is public API — a consumer's test suite builds on it;
nothing is renamed, removed or given a new signature), the reports AH and
AI (their owner's decisions are rules; AI's last section "What brief AJ
needs" is this brief's seed) and the packages this one builds on:
`controller/exchange` (`NewManager`, `Manager.CloseSession`, `Session`),
`controller/handshake` (`PASE`, `CASE`, `CASEServer`, `Fabric`,
`FabricList`, `MemoryResumptionStore`), `controller/ca` (`New`, `Config`,
`GenerateNOC`, `RootPublicKey`, `Verifier`, `CompressedFabricID`),
`controller/commissioner` (`New`, `Context`, `NewFabric`, `FabricConfig`,
`Commission`, `Options`, `EstablishPASE`, `Result`, `Interaction`),
`controller/peer` (`NewSet`, `Set.Get`, `Peer.NewestSession`),
`controller/interaction` (`New`, `Interaction.Read/ReadWith/Write/Invoke/Subscribe`,
`Raw`, `Report`, `NewSubscriptions`, `SubscribeRequest`, `StatusError`),
`controller/discovery` (`NewNames`, `OperationalQName`), and
`controller/interaction/live_test.go:123-220` — the in-process controller
that commissions a `node.Node` through the module's own commissioner with
the node's operational record injected into a `discovery.Names` through a
fake `mdns.QueryConn`: **that is the shape `bridgetest.Dial` takes**.
matter.js at **`0ceb202d`** is read where a `// Mirrors` comment is
written; this brief ports no new matter.js behaviour — it replaces
`bridgetest`'s own protocol code with the ported one. One commit per item,
reachability in each, CHANGELOG under `[Unreleased]` → Changed (public
test-support behaviour that moves) and Added (the two `interaction.Report`
fields). Guards: `make ci`, `make race`, `make reachability-check`, and
`make chiptool-test` once at the end (export `PATH=$HOME/go/bin:$PATH`
first). You may change `bridge/bridgetest` (everything but
`bridgetest.go`, which stays as it is), `controller/interaction`
(§2 only), the test files named in §5, and `CHANGELOG.md`; `secure/`,
`transport/`, `mdns/`, `bridge/` (outside `bridgetest`), `node/`,
`cluster/`, `im/`, the rest of `controller/` are read, not changed — a
change you need there is a question in the report. `notes/parity/by_design.md`
and the findings register are the owner's (`BD-Matter-Commissioner-TestCA`
describes the CA and transport this brief replaces; the owner rewrites it
from your report).

Out of this brief: `EstablishSubscriptionTarget`, `AckPumpTick`,
`SubscriptionTarget` and the `Err*` of `bridgetest.go` (device-side test
seams into `bridge` through `internal/bridgeseam`; no controller
counterpart — they stay exactly as they are), any new capability of the
commissioner (no batching, no ICD, no TCP), the chip-tool suite (it does
not use `bridgetest`), and the docs (owner).

## §0 What changes and what does not

`bridge/bridgetest` keeps its import path, its package doc's promise and
every exported identifier with its signature and documented behaviour:
`Dial`, `Options` (every field), `Commissioner` and its methods (`Accept`,
`Addr`, `CASE`, `CASEAs`, `Close`, `Commission`, `CompressedFabricID`,
`ControllerIdentity`, `FabricIndex`, `PASE`, `RemoveFabric`,
`ResolveOperational`, `ResolveSigma1Destination`, `RootPublicKey`),
`Session` and its methods (`Close`, `Invoke`, `LocalNodeID`, `NextReport`,
`PeerNodeID`, `Read`, `Subscribe`, `Write`), `Subscription`,
`InvokeResult`, `Report` (`Value`, `Status`), `AttributeData`,
`AttributeStatus`, `StatusError`, `TLV`, `DecodeTLV`, `ErrHandshake`,
`ErrSessionClosed`, the `Default*` constants. What goes: the package's own
UDP socket and read loop (`transport.go`), its PASE and CASE clients and
its CASE responder (`handshake.go`), its certificate authority (`ca.go`),
its hand-written IM encoders and report decoder (`session.go`,
`report.go` `decodeReport`). What replaces them: one `udp.Listener` +
`exchange.Manager`, `ca.CA`, `commissioner.Fabric`, `peer.Set`,
`interaction.Subscriptions`, `commissioner.Commissioner`, a
`handshake.CASEServer`, and `interaction.Interaction` per session. The
current files are the specification of the behaviour the tests expect;
read them before deleting them (`commissioner.go:152-213` Dial,
`:268-289` Commission, `:448-472` CASE/CASEAs/Accept, `:474-487`
RemoveFabric, `:489-525` the advertiser lookups; `handshake.go:273-343`
PASE, `:351-388` identities, `:397-443` caseAs, `:458-510` respondCASE;
`session.go` throughout; `report.go:298-337` Report and merge).

Two behaviours change, both announced under Changed:

- **`Session.Subscribe` on a PASE session is refused** with an error
  wrapping `interaction.ErrUncommissioned`: matter.js's client subscribes
  only to a peer with a peer address (ClientInteraction.ts:991-994, report
  AI §3c), and `bridgetest` now is that client. The two tests that
  subscribed over PASE move to a CASE session (§5).
- **Determinism moves from HKDF labels to a seeded stream.** `Options.Seed`
  still determines every key, certificate and nonce the commissioner mints
  (two `Dial`s with one seed mint the same bytes), but the bytes differ from
  today's: the CA is `controller/ca` (matter.js's certificate shape — root
  id 0, serials from the counter, subject order fabric id, node id, CATs,
  validity −1 y / +10 y around a fixed instant; report AH) over a seeded
  `io.Reader`. No test compares the minted bytes to a fixture (check with
  `grep -rn "Seed" --include=*_test.go` and read each use; a test that
  does is a question).

## §1 `Dial`, `Close`, `Addr` — the controller's parts

`Dial(tb, addr, opts)` validates `opts` as today (`commissioner.go:157-176`),
resolves `addr`, and builds:

- a `udp.Listener` on the loopback address of `addr`'s family, port 0
  (`udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})` /
  `"[::1]:0"`), served by a goroutine into `exchange.NewManager(l,
  exchange.Config{Logger: discard})` — `live_test.go:132-148`. `Addr()` is
  `l.LocalAddr()`. `Close` (idempotent, also `tb.Cleanup`) closes in the
  order `live_test.go:172-180` closes: the subscription registry, the CASE
  server, the commissioner, the peer set, the manager, the listener (stop
  the serve goroutine and wait), the names.
- a seeded randomness: one `io.Reader` whose stream is a function of
  `opts.Seed` (an HKDF-SHA256 expand over the seed is fine — keep a
  `deriveBytes`-like helper; `bridgetest`'s current `deriveBytes`,
  `ca.go:609-616`, is the pattern), and the `func() float64` for MRP jitter
  from the same seed. The unsecured session's ephemeral node id and
  message counter are the manager's own (random, `ExchangeManager.ts:729-734`
  as `exchange.NewManager` draws them) — the reason `BD-Matter-Commissioner-TestCA`
  gives for keeping them random holds.
- `ca.New(ca.Config{Rand: seeded, Now: fixed})` with `Now` returning one
  fixed instant (2026-01-01T00:00:00Z, the instant `ca.go:442` uses), so
  certificates are a function of the seed. `RootPublicKey()` is
  `ca.RootPublicKey()`; `CompressedFabricID()` is
  `ca.CompressedFabricID(root, opts.FabricID)`.
- `commissioner.NewFabric(ca, commissioner.FabricConfig{FabricID:
  opts.FabricID, RootNodeID: opts.ControllerNodeID, VendorID:
  opts.AdminVendorID, IPK: 16 seeded bytes, CATs: opts.CAT, Rand: seeded})`
  — the fabric's own identity is `ControllerNodeID`'s, as today's
  `identity(opts.ControllerNodeID, opts.CAT)`. `FabricIndex` 0 → 1, which
  every session of the fabric then carries (`handshake.CASE` with the
  fabric, the CASE server's `OnSession`).
- `discovery.NewNames(fakeConn, discard)` over a fake `mdns.QueryConn`
  the package owns (the shape of `live_test.go`'s fake: `Send` ignores,
  `Responses` is a channel the package writes DNS answers into, `Close`
  no-op). The names cache is fed before each flow step that needs it
  (§3).
- `peer.NewSet(peer.Context{Exchanges, Names, Fabrics: fabric.Lookup,
  Resumption: &peer.MemoryResumptionStore{}, Logger: discard})`.
- `interaction.NewSubscriptions(interaction.SubscriptionsConfig{Logger:
  discard})`.
- `commissioner.New(commissioner.Context{Exchanges, Peers, Attestation:
  attestation.TestStore(), Subscriptions: reg, Rand: seeded, Logger:
  discard})` — **`Fabrics` nil**: the package registers its own CASE
  responder (§4) so that `Accept` sees the sessions.
- `handshake.NewCASEServer(mgr, handshake.CASEServerConfig{Fabrics:
  handshake.FabricList{fabric.Fabric}, Resumption: nil, OnSession: §4})`,
  `Register()`ed. No resumption store: a resuming Sigma1 gets the full
  handshake, as today (`handshake.go:460-462`).

`Options` fields with no counterpart in the ported flow keep their
documented defaults and are reported, not silently dropped:
`FailSafeSeconds` — the ported flow arms the failsafe with matter.js's
budget (`commissioner` constants), not `Options.FailSafeSeconds`; if no
test in the module sets it (`grep -rn FailSafeSeconds --include=*_test.go`),
document the field as having no effect since this brief and list it in
the report; if one does, ask. `AdminSubject` — `FabricConfig.RootNodeID`
is the CaseAdminSubject; a value other than `ControllerNodeID` cannot be
expressed: same rule (no test → documented, listed; a test → ask).

## §2 `controller/interaction`: two fields on `Report`

`interaction.Report` gains `SubscriptionID uint32` — the report's
subscription id, 0 for a read's report — and `Session *exchange.Session`
— the session the report arrived over (`HandleExchange`'s
`m.x.Session()`, `subscribeOnce`'s and `ReadWith`'s exchange session).
The assembler (`report.go`) copies `SubscriptionID` from the first chunk
(`im.ReportData.SubscriptionID` when `HasSubscription`); the three
callers set `Session`. matter.js keeps both on the chunk's exchange
(`InputChunk` carries the `DataReport` with its `subscriptionId`;
`ClientSubscriptionHandler.ts:108-112` hands the session to
`reportStarted`); cite them in the field comments. A parity-neutral
addition; a test in `controller/interaction` (the scripted ones) checks
both fields.

## §3 `Commission`, `PASE`, `CASE`, `CASEAs`, `RemoveFabric`, `FabricIndex`

- **Addresses.** `commissionableAddr` / `operationalAddr`
  (`commissioner.go:489-525`) stay as the way `Options.Advertiser` is read
  (the device's `mdns.Noop`: the commissionable record with
  `Discriminator` for PASE's port, the operational record of
  `CompressedFabricID`/`NodeID` for CASE's port, on the dialled host;
  without an advertiser the dialled address for both). Before the flow's
  operational CASE step and before every `CASE`/`CASEAs`, the operational
  address is also written into the names cache as `live_test.go:185-202`
  writes it (SRV + TXT `SII=500`/`SAI=300` + A/AAAA under
  `discovery.OperationalQName(compressedFabricID, opts.NodeID)`), because
  the ported flow resolves the node through `discovery.Names`.
- **`Commission(ctx)`** = `com.Commission(ctx, commissioner.Options{Fabric,
  Passcode: opts.Passcode, NodeID: opts.NodeID, Addresses:
  []*net.UDPAddr{commissionable}})` with the attestation policy left zero
  (matter.js's default over the test store — the CSA test chain the node
  serves passes; today's `bridgetest` checked the DAC signature only) and
  the regulatory defaults of the ported flow (country "XX", the device's
  LocationCapability — the same values `commissioner.go:332-342` sends).
  `FabricIndex()` is `Result.FabricIndexOnPeer`. The returned `Session`
  wraps the peer's newest session (`set.Get(res.Address).NewestSession()`)
  — the CASE session CommissioningComplete went over, as today
  (`commissioner.go:251-254`). The default 10 s timeout of
  `withDefaultTimeout` goes: the ported flow has matter.js's budgets
  (`commissioner` constants); a test's own `ctx` deadline bounds it.
  Keep `withDefaultTimeout` for `Accept` and `NextReport` only.
- **`PASE(ctx)`** = `com.EstablishPASE(ctx, commissioner.PASEOptions{Addresses:
  []*net.UDPAddr{commissionable}, Passcode})`, the returned
  `*exchange.Session` wrapped. A failure wraps `ErrHandshake`
  (`fmt.Errorf("%w: %w", ErrHandshake, err)`), as today's handshake errors
  do.
- **`CASE(ctx)`** = `CASEAs(ctx, opts.ControllerNodeID, opts.CAT...)`.
  **`CASEAs(ctx, nodeID, cats...)`** = `handshake.CASE(ctx, mgr,
  operational, handshake.Fabric{Identity: &id, RootPublicKey: root,
  Verifier: ca.Verifier()}, opts.NodeID, handshake.CASEOptions{})` with
  `id := ControllerIdentity(nodeID, cats...)` (`FabricIndex` 1); a new
  session per call, no resumption, as today (`handshake.go:397-443`). The
  session is wrapped and registered for `NextReport` (§4); it does not join
  the peer set. A failure wraps `ErrHandshake`.
- **`ControllerIdentity(nodeID, cats...)`** mints, once per node id, a
  P-256 key from the seeded stream and `ca.GenerateNOC(pub, opts.FabricID,
  nodeID, cats)`, and returns `sigma.Identity{NOC, ICAC: ca.ICACCert(),
  PrivateKey, NodeID, FabricID, CompressedFabricID, IPK: the operational
  IPK (sigma.DeriveOperationalIPK(raw IPK, compressed id), as
  `handshake.go:359-371`)}`. The fabric's own identity (`ControllerNodeID`,
  `opts.CAT`) is the one `commissioner.NewFabric` minted — return that
  one for `ControllerNodeID`, so `CASE` and the commissioning flow present
  the same NOC. `ResolveSigma1Destination` returns the identity of the
  fabric the Sigma1 addresses through `handshake.FabricList` over every
  identity minted so far (`FabricForDestination`); it is public API, keep
  it (a device-side test may use it as a `sigma.IdentityResolver`).
- **`RemoveFabric(ctx, s)`** invokes OperationalCredentials RemoveFabric
  (`0x003E:0x0A`, field 0 = `FabricIndex()`) through `s.Invoke` and
  returns NOCResponse's status (field 0), as `commissioner.go:474-487`.

## §4 `Session`, `Accept`, `NextReport`

`Session` wraps `s *exchange.Session` and `ix := interaction.New(mgr, s,
discard)` (a `Config.Subscriptions`-bound interaction: use
`interaction.Bind(interaction.SessionProvider(mgr, s),
interaction.Config{Subscriptions: reg, Logger: discard})`), plus the
report queue (`reports chan *Report`, 64, as today). The commissioner
keeps `map[*exchange.Session]*Session` so a report can find its queue.

- `PeerNodeID` / `LocalNodeID`: `s.PeerNodeID()` / `s.NodeID()`.
- `Close`: `mgr.CloseSession(ctx, s)` (tells the peer with CloseSession,
  as today's `session.go:62-73`), idempotent; the wrapper then answers
  `ErrSessionClosed` to every later call (`s.IsClosed()`), as today — and
  a session the device closed with CloseSession is closed by the manager
  (its built-in Secure Channel handler), so `IsClosed` covers today's
  `markClosed` path too.
- `Read(ctx, paths...)`: `ix.Read` (fabric-filtered by default, as today)
  → `Report` converted (§4a).
- `Subscribe(ctx, min, max, paths...)`: `ix.Subscribe(ctx,
  interaction.SubscribeRequest{Attributes: paths, ReplaceSubscriptions:
  true, MinIntervalFloor: Interval(min s), MaxIntervalCeiling:
  Interval(max s), Updated: §4b})` — today sends KeepSubscriptions false
  (`session.go:207-213`), hence `ReplaceSubscriptions: true`. The priming
  report reaches `Updated` first (report AI §3c): capture it into
  `Subscription.Priming` (the first `Updated` call during `Subscribe` is
  the priming report; later ones are queued, §4b). `Subscription.ID` and
  `MaxInterval` from the returned `*interaction.Subscription` (`ID()`,
  `MaxInterval()` in seconds). On a PASE session `ix.Subscribe` fails
  with `ErrUncommissioned`; return that error as it is and document it
  on `Subscribe` (§0, Changed).
- `Write(ctx, path, value)`: `ix.Write(ctx, []interaction.WriteRequest{{Path:
  path, Value: encodable(value)}}, interaction.WriteOptions{DisableListChunking:
  true})` — today writes one AttributeDataIB whatever the value
  (`session.go:249-287`), so chunking is off; the one status is returned;
  a `*interaction.StatusError` in place of the WriteResponse becomes
  `*StatusError{Status}`; any other error is returned. `encodable(value)`
  adapts a `func(e *tlv.Encoder, tag tlv.Tag)` to `spec.Encodable`.
- `Invoke(ctx, path, fields)`: `ix.Invoke(ctx, path, encodable(fields) or
  an empty structure, interaction.InvokeOptions{})` → `InvokeResult` from
  `interaction.Result` (read `interaction/invoke.go` for its fields: the
  response command and fields as a node tree → `TLV`, or the status and
  cluster status); a `*interaction.StatusError` → `*StatusError`. Note the
  AH-era single-command invoke stamps revision 12 (owner's decision, AI).
- **§4a `Report` conversion.** `interaction.Report` → `bridgetest.Report`:
  `SubscriptionID` from the new field; each `AttributeData{Path,
  DataVersion, Value}` with `spec.Node` → `TLV` (`TLV{Element: n.El,
  Children: …}` recursively — `report.go:232-283`'s shape); each
  `AttributeStatus{Path, Status}`. `report.go`'s `merge`/`decodeReport`
  and the `more`/`suppress` fields go (the controller merges chunks).
- **§4b `NextReport` and `Accept`.** Every `Updated` callback of every
  subscription the package made pushes the converted report into the queue
  of the `Session` whose `exchange.Session` is `report.Session` (the map
  above); a report whose session the map does not know is dropped with a
  `tb.Logf`. `NextReport(ctx)` reads the queue (default timeout kept).
  `Accept(ctx)`: the CASE server's `OnSession(s, fabricIndex)` wraps `s`
  into a `Session`, registers it in the map and queues it on `accepted`
  (8, as today); `Accept` reads it. A report the restarted node pushes on
  that session under its old subscription id reaches the registry's
  handler (`Register`ed by `commissioner.New` through
  `Context.Subscriptions`), which finds the subscription by (peer address,
  id) — the address the session carries (fabric index 1, the node id) is
  the one `Subscribe` registered it under — and calls its `Updated`, whose
  `report.Session` is the accepted session: `TestSubscriptionSurvivesRestart`
  (`node/commission_test.go:196-262`) is the proof. `ResolveOperational`
  stays as it is (`commissioner.go:237-249`), `Addr()` now the listener's.

## §5 Tests to move, and the import cycle

- `bridge/bridgetest`'s own tests (`commissioner_test.go`,
  `bridgetest_test.go`, `peer_counter_anchor_test.go`) keep passing; adapt
  what reads the removed internals.
- **`controller/peer/helpers_test.go:204-211` and `node_test.go:80-96`
  are `package peer` and import `bridgetest`; `bridgetest` now imports
  `controller/peer` — an import cycle.** The fabric helper builds its
  `handshake.Fabric` from `ca.New` (seeded) + `ca.GenerateNOC` +
  `ca.Verifier()` directly (no `bridgetest`); the node proof that
  commissions a `node.Node` moves to an external test package
  (`package peer_test`, its own file) if it uses exported API only —
  there it may use `bridgetest` or `commissioner` — else it is a question.
  `controller/handshake/scripted_test.go:211-214`,
  `controller/interaction/provider_test.go:85-91`,
  `controller/ca/ca_test.go:224-230` are external test packages: no cycle;
  they keep using `ControllerIdentity` / `RootPublicKey` /
  `CompressedFabricID`.
- `node/node_test.go` `TestNodeServesPASEReadAndSubscription:129-213` and
  `node/issue87_test.go` `TestSessionAndSubscriptionDiagnostics`
  subscribe over PASE: both commission first (`commissioned(t, cfg)`,
  `node/commission_test.go:52-64`) and subscribe over the CASE session;
  the diagnostics test then expects the CASE session (`IsPASE` false,
  the fabric index) — adjust its assertions, keep its intent. The PASE
  reads stay over PASE.
- Every other user of `bridgetest` (`node/*_test.go`,
  `controller/handshake`, `controller/interaction`, `controller/ca`)
  passes unchanged; a test that does not is a finding for the report, not
  a test to weaken.
- Coverage floors: `bridge/bridgetest` shrinks; keep its floor honest
  (`script/coverfloor/floors.go`, the ratchet rule of CLAUDE.md).

## Report

`notes/briefs/reports/AJ.md`: the table commit → item; what each guard
proved (`make chiptool-test` once; the chip-tool suite is untouched by
this brief, so its result is the regression check); every line where the
implementation differs from the brief and why; the `Options` fields left
without effect; what `BD-Matter-Commissioner-TestCA` should now say (what
is still true: test support only, nothing on the device path; what
changed: the CA, the transport with MRP, the seed); open questions;
by_design candidates (not written). The report is also the owner's input
to the ADR 0020 stage-2 row and `docs/matterjs-comparison.md`.
