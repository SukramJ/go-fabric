# Brief AU — the facade's endpoint persistence exported (#117), the re-announce loop owned by the facade (#119), the UniqueID rotation marker dropped (#122)

Owner: Fable. Implementer: Opus. A second Opus agent works on brief AV
(`cluster/levelcontrol`, `cluster/light`, `endpoint/sqlitestore`,
`examples/reference-bridge/fleet_lighting.go`) at the same time: stay
out of those paths; the shared file is `CHANGELOG.md` (the owner
merges). **Never run chip-tool.** Branch `wave27/node` from
`planning/briefs-wave-27`, pushed, **no PR**. Report
`notes/briefs/reports/AU.md`. Read first: `notes/briefs/README.md`, the
three issues (#117, #119, #122 — each carries the owner's assessment
comment with the decision; the issue text is the spec), `node/persist.go`
(`persisted`, `storeLabel` :104, `label` :115, `LoadScenes` :147,
`SaveScenes` :160, `uniqueID` :180), `node/node.go:146-154` (where the
facade wires them), `node/config.go` (`Network` :137-156,
`BasicInformation.UniqueID` :237-245, `Advertiser`), `node/node.go`
Start/Stop (`advertiser` :61, :110-114, :531-567), `mdns/advertiser.go`
(`Advertiser` :26), `mdns/zeroconf.go:46-60`, `:584-640`
(`TriggerReannounce`, `StartReannounceLoop`), `mdns/noop.go`,
`store/settings.go:25-42`, `endpoint/assembler.go:67-70` (`ScenesStore`),
`README.md` §API stability (the deprecation window: a public identifier
is deprecated, not removed). matter.js at **`0ceb202d`**
(`git -C ../matter.js show 0ceb202d:<path> | cat -n`):
`packages/node/src/behaviors/basic-information/BasicInformationServer.ts:89-91`
(uniqueId created once and persisted — the rule the facade keeps),
`packages/protocol/src/mdns/MdnsServer.ts:150-170` (answers re-sent on
a query after a quarter of the TTL; no unsolicited periodic announce —
the loop is this module's own device, say so in the comment).
Guards: `make ci`, `make race`, `make reachability-check` (export
`PATH=$HOME/go/bin:$PATH`; a `make ci` failing only on golangci-lint's
timeout after "0 issues" is rerun as `make lint`; a `node/` live test
failing once with "Could not connect to device" is rerun alone; new
live tests do not call `t.Parallel()`; under 15 GB free:
`find ~/.cache/go-build -type f -mmin +360 -delete`). One commit per §,
reachability in each. Allowed paths: `node/`, `mdns/` (new optional
interfaces and `Enabled()` on `Zeroconf` and `Noop` only — `Advertiser`
itself does not change), `store/settings.go` (the Deprecated mark only),
`examples/reference-bridge` (if it starts a loop or asserts a type —
read; else untouched), `CHANGELOG.md`. Everything else is read, not
changed.

## Owner's decisions

- **D1 (#117) — `node.EndpointPersistence`:** an exported interface
  `endpoint.ScenesStore` + `OnNodeLabelWritten(key endpoint.SourceKey,
  label string)` + `NodeLabel(ctx, key) string`, a constructor
  `node.NewEndpointPersistence(st *store.Store, logger *slog.Logger)
  EndpointPersistence` (usable before `node.New`), `Node.Persistence()`
  returning the facade's, and `node.Config.EndpointPersistence` as an
  optional override the facade's own assembler uses when set. The key
  shapes (`scenes.<key>`, `bridged_node_label.<key>`) and the existing
  matter.js citations stay as they are.
- **D2 (#119) — the re-announce loop:** `mdns.Reannouncer`
  (`StartReannounceLoop(ctx, interval) (cancel func(), trigger
  chan<- struct{})`, `TriggerReannounce(ctx)`) and `mdns.Enabled`
  (`Enabled() bool`) as optional interfaces; `Zeroconf` implements
  both (true), `Noop` implements `Enabled` (false); `mdns.IsEnabled(a
  Advertiser) bool` (true for an advertiser that does not implement
  `Enabled`). `node.Network.ReannounceInterval time.Duration` (0 =
  `node.DefaultReannounceInterval` = 30 min, documented as under half
  the 75-minute mDNSResponder TTL) and `node.Network.DisableReannounce
  bool`; the facade starts the loop on `Start` when the advertiser is a
  `Reannouncer` and stops it on `Stop`; `Node.TriggerReannounce(ctx)`
  for link changes (a no-op on an advertiser that is not a
  `Reannouncer`).
- **D3 (#122) — the marker is dropped:** `persisted.uniqueID` no longer
  reads `store.SettingUniqueIDRotated`; the constant stays with a
  `// Deprecated:` line (no reader; removed after the window); the
  `UniqueID` field comment says a rotated boot pins nothing and a host
  leaves the field empty unless it owns the identity. The behaviour
  stays matter.js's (`BasicInformationServer.ts:89-91`).
- **D4 — no new dependency.**

## §1 — `node.EndpointPersistence` (#117)

D1. Test: a node with `Topology` set and an assembler the test builds
over `node.NewEndpointPersistence` (or `Node.Persistence()`), a
NodeLabel write and a scene table through it, the node restarted over
the same store, both read back; plus the override field used by the
facade's own assembler.

## §2 — the re-announce loop (#119)

D2. Tests: a fake advertiser implementing `Reannouncer` and `Enabled`
observes the loop started on `Start` with the configured interval,
`TriggerReannounce` forwarded, the loop stopped on `Stop`;
`DisableReannounce` starts none; `IsEnabled` on `Noop`, `Zeroconf` and
a plain fake. No chip-tool.

## §3 — the UniqueID marker (#122)

D3. Test: rotation on/off/on over one store (`bootid` salt set and
unset — read how the existing tests do it): the value served after
rotation is switched off is the pre-rotation pinned value, never a
salted one; nothing is written under rotation.

## Report

As AK–AT: per §, guards with HEAD, lines that differ from the files,
decisions, left out, questions, `by_design` candidates (not written).
Close the three issues in the commit messages (`Closes #117` etc. in
the body — the squash merge carries them).
