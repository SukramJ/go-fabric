# Report AU — the facade's endpoint persistence (#117), the re-announce loop (#119), the UniqueID marker dropped (#122)

Branch `wave27/node` from `planning/briefs-wave-27` (59c50e1), pushed, no PR.
matter.js read at `0ceb202d`; every citation added cites lines read there.
Each § commit carries its regenerated reachability inventory (unreached
stays **5**, ratchet untouched). No chip-tool binary was run.

| Commit | Item |
| --- | --- |
| d929216 `feat(node): export the endpoint-level persistence` | §1, Closes #117 |
| 11cdf4d `feat(node,mdns): the facade owns the re-announce loop` | §2, Closes #119 |
| 4e5ea9e `fix(node): drop the UniqueID rotation marker read` | §3, Closes #122 |
| 451e476 `notes: report AU` | — |
| 5ebfe61 `fix(node): the UniqueID rotation marker read as a one-time migration` | §3 fix-up (owner Q1) |
| 75fe8a5 `fix(mdns): Zeroconf runs one re-announce loop at a time` | §2 fix-up (owner Q2) |
| (this commit) `notes: report AU — fix-ups` | — |

## Per section — what was built, what proves it

- **§1 `node/persist.go`, `node/node.go`, `node/config.go`,
  `node/aggregator.go`.** D1 as written: `node.EndpointPersistence`
  (`endpoint.ScenesStore` + `OnNodeLabelWritten(key, label)` +
  `NodeLabel(ctx, key) string`); `persisted`'s `storeLabel`/`label` renamed
  to those two methods (keys `scenes.<key>`, `bridged_node_label.<key>` and
  the existing comments unchanged). `node.NewEndpointPersistence(st,
  logger)` returns `persisted` (nil logger = `slog.Default()`; `st` must
  carry `store.Apply`'s schema — documented). `Config.EndpointPersistence`
  replaces it in the facade's assembler (`OnNodeLabelWritten`, `Scenes`)
  **and** in the Aggregator's snapshot label restore (`aggregator.go:191`),
  so the override is consistent for both directions. `Node.Persistence()`
  returns what the facade uses (the override when set).
  Proofs (`node/persistence_test.go`): `TestEndpointPersistenceForHostTopology`
  — a Topology node whose host assembler is built over
  `NewEndpointPersistence` before `node.New`; the bridged NodeLabel is
  written over a PASE IM Write (0x0039/0x0005), a scene table saved through
  the persistence; a new node over the same file restores the label into
  the served topology and both are read back through the host's persistence
  and through `Node.Persistence()`, keys asserted in the settings table.
  `TestEndpointPersistenceOverride` — a fake override: its label restored
  by the facade's Aggregator, a controller's write reaches it, the store is
  not written, `Persistence()` is the override.
- **§2 `mdns/reannounce.go` (new), `node/reannounce.go` (new),
  `node/config.go`, `node/node.go`.** D2 as written: `mdns.Reannouncer`
  (`StartReannounceLoop`, `TriggerReannounce` — `Zeroconf`'s existing
  signatures), `mdns.Enabled` (`Zeroconf` true, `Noop` false, both methods
  in the new file so `zeroconf.go`/`advertiser.go` stay untouched),
  `mdns.IsEnabled` (true for an advertiser without `Enabled`, false for
  nil). `Network.ReannounceInterval` (0 = `DefaultReannounceInterval` = 30
  min, comment: under half the 75-minute mDNSResponder TTL),
  `Network.DisableReannounce`; `Validate` refuses a negative interval.
  `Start` starts the loop on `n.life` after `startGroupSender`, `Stop` ends
  it once, before `br.Stop` and the advertiser's `Close`.
  `Node.TriggerReannounce(ctx)` forwards to the advertiser's method (no-op
  on a non-Reannouncer). Comments on `mdns.Reannouncer` and
  `startReannounce` say the loop is this module's own device, citing
  `MdnsServer.ts:158-164` (the TTL/4 multicast re-send on a query).
  Proofs: `mdns/reannounce_test.go` `TestIsEnabled` (Zeroconf, Noop, a plain
  fake, nil; Zeroconf is a Reannouncer, Noop is not);
  `node/reannounce_test.go` `TestReannounceLoop` — a fake Reannouncer+Enabled
  advertiser: no loop after `New`, one loop at the configured interval /
  the default after `Start` (twice), trigger forwarded, cancelled exactly
  once by `Stop` (twice); `DisableReannounce` starts none; a Noop node
  starts/triggers/stops; negative interval refused.
  `examples/reference-bridge`: read — it starts no loop and asserts no
  concrete type (`main.go:174-198`), so untouched (it now gets the loop
  from the facade with `--advertise`).
- **§3 `node/persist.go`, `node/config.go`, `store/settings.go`.** D3:
  `persisted.uniqueID` no longer reads or clears
  `store.SettingUniqueIDRotated`; under rotation it derives and touches no
  key; otherwise stored-or-derive-and-pin (`BasicInformationServer.ts:89-91`,
  comment kept). The constant has a `// Deprecated:` paragraph.
  `BasicInformation.UniqueID`'s comment adds the rotated-boot rule and
  "leave empty unless the host owns the identity". A `salt func()
  [16]byte` field on `persisted` (nil = `bootid.Salt`) is the test seam:
  `bootid` has no exported way to switch rotation off, and its state is
  process-global (`bootid_test.go:14-42` resets it only from inside the
  package). Proofs: `TestUniqueIDRotationOnOffOn` (internal) — fresh store
  on (salted, nothing pinned) / off (pin) / on / off / on / off: every
  rotated boot serves its salted value and leaves the pin, every other
  boot serves the pin, the marker is never written. `TestUniqueIDPinned`'s
  marker segment rewritten: a stored value with marker "1" is now served
  as pinned and the marker left alone.

## Owner's answers (2026-10-15) — fix-ups

- **Q1 → 5ebfe61.** `persisted.uniqueID` reads `store.SettingUniqueIDRotated`
  again as a one-time migration until the key is removed: "1" ⇒ re-derive,
  pin, clear; no flag ⇒ serve the stored value; rotation on ⇒ nothing read
  or written. The facade never writes the flag. Deprecated line: "read for
  the migration only; never written". `salt` seam kept;
  `TestUniqueIDRotationOnOffOn` gains the migration case (a rotated boot
  leaves flagged value and flag alone, the next unrotated boot migrates);
  `TestUniqueIDPinned`'s segment is back to the migration assertions.
  CHANGELOG: no manual clearing needed.
- **Q2 → 75fe8a5.** `Zeroconf.StartReannounceLoop` keeps the running
  loop's trigger in `loopTrigger`; a second call while it runs logs
  `mdns.reannounce.loop_already_running` at Info (`slog.Default()` — Zeroconf
  has no logger) and returns a no-op cancel plus that trigger. cancel is
  `sync.OnceFunc` (was `close(stopCh)`, which panicked on a second call) and
  releases the slot synchronously; the goroutine releases it too when its
  ctx ends. `TestZeroconfOneReannounceLoop` (no-op second cancel, trigger
  shared, double cancel, new loop after cancel and after ctx end); the
  existing loop tests pass unchanged. CHANGELOG line keeps the advice and
  names the guard.
- Q3: no change (owner records `TestReactionLeave`). Floor: untouched.

## Guards

All at `4e5ea9e` (HEAD before = after for every run; summary
`bin/guards-summary.log`, per-guard logs `bin/guard-*.log` in the worktree):

- `make ci` — build, vet, test **green**; failed only on golangci-lint's
  timeout after "0 issues" (brief rule) → `make lint` rerun **green**
  (0 issues). `make cover-check` (the step `ci` did not reach) failed once
  on `node` `TestSessionAndSubscriptionDiagnostics` ("Could not connect to
  device", issue87_test.go:324) → rerun alone `-count=3` **pass**, then
  `make cover-check` **green** (node 83.2 vs floor 72, mdns 92.9 vs 91;
  the tool advises raising node's floor to 83 — `script/coverfloor` is
  outside this brief's paths, left to the owner).
- `make race` — failed once on `controller/client` `TestReactionLeave`
  (reactions_test.go:139 "stored [peer1]", a wait on the registry before
  the store deletion lands; package untouched by this brief) → rerun
  `-race -count=5` **pass**. Every other package green under race,
  including `node` and `mdns`.
- `make reachability-check` — **green** (unreached 5).

### Guards after the fix-ups

At `75fe8a5` (HEAD before = after; `bin/guards-summary3.log`): `make ci`
**green** (lint and cover-check included, no rerun needed), `make race`
**green**, `make reachability-check` **green**. An earlier run at the
pre-amend head failed `TestDeprecationMarkersMatchThePolicy` on a test
comment line starting with "deprecated" (issue87_test.go:122); reworded
in the amended §3 fix-up.

## Lines that differ from the cited files

- `node/aggregator.go:191` read `a.n.persisted.label`; it now reads
  `a.n.endpoints.NodeLabel` (the override, if set). D1 names the
  assembler only; the Aggregator restore follows so a label the override
  stores is the label restored.
- `node/persist.go` `uniqueID` (was :180-205): after the Q1 fix-up the
  logic equals the pre-wave one except under rotation (still no store
  access) and the salt source (`p.salt`, nil = `bootid.Salt`); the comment
  now says the flag is read as a migration and never written.
- `mdns/zeroconf.go` `StartReannounceLoop` (was :584-617, Q2 fix-up): the
  single-loop guard, `sync.OnceFunc` cancel, `loopTrigger` field; the
  "Idempotent — calling twice silently replaces the prior loop" doc line
  (which did not match the code: it started a second loop) is replaced.
- `node/node.go` Stop doc comment lists the re-announce loop in the
  shutdown order.

## Decisions

- `Node.Persistence()` returns the override when set, not always the
  facade's own — "the one the assembler uses" is what a Topology host needs.
- `Node.TriggerReannounce` goes through `Reannouncer.TriggerReannounce`, not
  the loop's trigger channel (kept unused): it works with the loop off and
  matches the brief's "forwarded".
- The loop runs under the node's lifetime context (`n.life`), so a
  forgotten Stop path still ends it; Stop cancels it explicitly before the
  advertiser closes.
- `mdns.IsEnabled(nil)` is false (nil is no advertiser).
- `Network.ReannounceInterval < 0` is a `Validate` error (ErrInvalidConfig).
- The deprecated constant is used in two tests with `//nolint:staticcheck`
  to show it is ignored / not written.
- CHANGELOG: `### Added` (§1, §2), a `### Changed` line (§3) and a
  `### Deprecated` section under `[Unreleased]`.

## Left out

- `mdns/zeroconf.go:46-60` still says "run StartReannounceLoop" — true for
  a non-facade user; the brief allows only the new interfaces there.
- No chip-tool run (brief), no schema/parity regeneration (nothing
  matter.js-derived moved).

## Questions

0. `TestReactionLeave` (controller/client) looks timing-flaky under
   `-race` load: the test asserts the store empty after `WaitRegistry`;
   worth a findings note or a `waitUntil` on `NodeIDs` (not this brief's
   path).

1. *(answered: migrate once — 5ebfe61)* **#122 behaviour change for existing databases:** a value a host wrote
   under rotation with the marker "1" (OpenCCU-Loom's copy,
   `daemon_matter.go:626-684`) is now served as the pinned UniqueID on the
   next non-rotated boot instead of being re-derived. The CHANGELOG tells
   such a host to clear `store.SettingUniqueID` once. Should the facade
   instead do a one-time migration (marker "1" ⇒ re-derive, pin, clear) for
   the window, i.e. keep the old reader until the constant goes?
2. *(answered: guarded in `mdns` — 75fe8a5)* A host that already calls `Zeroconf.StartReannounceLoop` itself (Loom,
   `daemon_matter.go:525`) runs two loops after upgrading unless it drops
   the call or sets `DisableReannounce` — stated in the CHANGELOG; no
   guard against it in the library.

## by_design candidates (not written)

- The re-announce loop: matter.js sends no unsolicited periodic announce
  (`MdnsServer.ts:158-164` re-sends on a query after TTL/4); go-fabric's
  facade re-publishes every 30 min because the upstream zeroconf library
  announces once per Register only.
- `bootid` UniqueID rotation (a dev knob with no matter.js counterpart;
  under it nothing is pinned) — if not already catalogued.
