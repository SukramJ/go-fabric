# Brief CB — M4 phase 21b: metrics port, diagnostic ring and slog convention on the facade; the Base device type's optional clusters

Date 2026-10-24. Concept v7 (`docs/concept-v7-operation-and-packaging.md`
A.3, decisions 0.2 and 0.7), the read map
`notes/briefs/reads/read-m4-phase-21-22.md` §1 (observability today:
every logger field, key, fallback, the ring, the counters, with
file:line — a map, verify before acting), ADR 0025 P5/P12, ADR 0026 P9
(the logger rule) and P1, `docs/guides/advanced-seams.md` (the ring
row, `:223` "milestone 4, phase 21"), brief CA's report
`notes/briefs/reports/CA.md` (read Differences and Left out; do not
redo). matter.js pin `862ad75f`, read only via
`git -C $MATTERJS_DIR show 862ad75f:<path>`; cite lines. go-fabric
`main` after CA has merged (one implementing agent at a time — owner
2026-10-23: start when `gh pr list --state open` shows no `m4/phase-21a`
pull request and `git log origin/main --oneline -5` shows CA's squash).
Branch `m4/phase-21b`, commits `m4 CB: <step>`, one pull request, no
auto-merge, no chip-tool label unless §4 moves the reference bridge's
composition (it does not: FixedLabel stays mounted on the same
endpoints). The removal rule of 2026-10-21 applies. Version: a `v0.10.x`
patch. The owner merges.

**Model:** Opus. No security path changes: `client/handshake`,
`secure/*`, `cluster/core/{access_control,operational_credentials,
general_commissioning,group_key_management}.go` are touched only where
a slog key is renamed to the convention (§3) — list every such line in
the report, nothing else there.

## 1 `node.Metrics` (concept 0.2; ADR 0025 P12 — no sixth dependency)

- `contract.Metrics` (stdlib only, so a host's implementation does not
  import `node`): `Counter(name string, delta int64)` and
  `Gauge(name string, value int64)`; no attributes in this wave (a
  per-fabric split is a later decision — say so in the doc comment).
  `node.Metrics = contract.Metrics` alias; `node.Config.Metrics`
  (nil = nothing recorded, no cost beyond a nil check).
- The named set, as constants in `node` (`node.MetricSessionsOpen` …)
  and a table in the guide: `sessions.open` (gauge),
  `subscriptions.active` (gauge), `im.received`, `im.sent` (counters;
  the DeviceLoad counters at `bridge/device_load.go:20-24` feed both the
  hook and keep `Bridge.DeviceLoad`), `pase.failures`, `pase.lockouts`
  (`bridge/bridge.go:518` `paseFailures` and the lockout site —
  read `bridge/securechannel.go` for where the window closes),
  `aggregator.reassemblies` (`node/aggregator.go:111`),
  `mrp.retransmits` (find the retransmit site in `transport/mrp`),
  `panics.recovered` (the `RecoveredPanics` counters,
  `transport/udp/listener.go:96`, `transport/tcp/*`). Every existing
  reader of these counters stays. A name is spec-near, not
  matter.js-internal; the doc comment of each constant says what one
  unit is.
- `node/metrics/expvar` (stdlib `expvar`): `expvarmetrics.New(prefix)`
  implementing the port, publishing each name under
  `prefix + name`; the reference bridge wires it behind a flag
  (`-metrics-expvar` or the existing debug listener if there is one —
  read `examples/reference-bridge/main.go:150-200`) so `/debug/vars`
  shows the set. Tests: a fake port in `fabrictest` (`Site.Metrics()`
  or an option) and one test per name that drives the event (a pairing
  → `sessions.open`; a subscribe → `subscriptions.active`; a failed
  PASE → `pase.failures`), the expvar implementation's round trip.

## 2 The diagnostic ring on the facade

- `node.Config.Diagnostics{Capacity int}` (0 = 256, today's
  `node/node.go:249`); `Node.DiagnosticEvents() []DiagnosticEvent`
  (newest first, as `Ring.Snapshot`); `Node.OnDiagnosticEvent(fn
  func(DiagnosticEvent))` inline and `OnDiagnosticEventAsync` on the
  node's callback rule (one signature, no error; read
  `device.OnChangedAsync` and ADR 0026 for the goroutine ownership —
  the ring's `Record` must not call the host under the ring's lock).
  `diagevent.Ring` gains the observer; `Bridge.DiagnosticEvents` and
  `AttachDiagnosticEvents` stay as the seam. `advanced-seams.md:223`
  and the row's sentence change to "on the facade since v0.10.x";
  `docs/matterjs-comparison.md:141` unchanged (✅ already).
- The reference consumer reads `diagevent.Event` in its REST handler
  (concept v6 `:109`): the alias `node.DiagnosticEvent` is its route;
  the migration guide gets one line (`diagevent` import → `node`).

## 3 The slog convention and its guard (ADR 0026 P9)

- `docs/guides/observability.md` (new page, in `mkdocs.yml` nav next
  to persistence): the key table — `fabric_index`, `node_id`,
  `endpoint`, `cluster`, `attribute`, `session_id`, `exchange_id`,
  `subscription_id`, `group_id`, `peer` (an address), `err` (always
  `slog.Any`), `device` (the host's key, examples only), `reason`,
  `status` — one line each with the type; the message prefixes per
  package (read map §1.4, keep what exists, do not rename messages);
  the metrics table of §1; the ring of §2; how a host sets the logger
  (`Config.Logger`) and what the nil default is (`slog.Default()`,
  ADR 0026 P9).
- Rename to the convention in library code (not `examples/`'s
  `device`): `fabric` → `fabric_index` (6 sites, read map §1.3),
  `session` → `session_id` (10), `exchange` → `exchange_id` (14),
  `endpoint_id` → `endpoint` (3), `subscription` → `subscription_id`
  (6), `id` at `im/subscription/manager.go:342`/`persistence.go:122` →
  `subscription_id`, `group` → `group_id` (2), `error` → `err` (2),
  `peer_node`/`peer_node_id`/`node` (where it is a node id) → `node_id`,
  `addr`/`address`/`dst`/`dest`/`to` (where it is the peer's address) →
  `peer`; `src` stays (the datagram's source, a different thing — say
  so in the table). `slog.String("err", err.Error())` (233 sites) →
  `slog.Any("err", err)` — one scripted replacement, `gofumpt` after.
  `client/*`'s `slog.New(slog.DiscardHandler)` fallbacks (read map
  §1.5: handshake, exchange, interaction, peer) become `slog.Default()`
  like the rest of the module; `fabrictest/site.go:124` keeps discard
  (a test site is quiet by design — say so).
- Guard `script/slogkeys`: an analyzer next to `script/errprefix`
  (same wiring in the Makefile and CI; read how errprefix is run) that
  reports a `slog.<Type>("<key>", …)` call in non-test, non-`examples/`
  code whose key is not in the table (the table is a Go slice in the
  analyzer, the doc page's table is generated from it or checked
  against it — `make docs-check` style, say which), and
  `slog.String("err", …)`. Ratchet none: the tree is clean when the
  wave lands.

## 4 The Base device type's optional clusters (owner decision 0.7)

- matter.js `base.element.ts:46-47` at the pin: FixedLabel (`0x40`)
  and UserLabel (`0x41`) are `conformance: "O"` on every endpoint. The
  kit's typed declaration accepts both on any device type:
  `fixedlabeldef.Declare{Initial: …}` and `userlabeldef.Declare{…}`
  mount the module's servers (`cluster/core/fixed_label.go`,
  `cluster/core/user_label.go`; UserLabel is writable — the server's
  persistence through the store port of CA, read how it stores today).
  The refusal of an undeclared cluster stays (`device/options.go:159`);
  what changes is the device-type conformance check that rejects a
  cluster the type does not list — read where it runs
  (`device/conformance_test.go`, `host_options_test.go:22`) and add the
  Base rule there with the citation, not a per-type exception.
  Binding (`base.element.ts:45`, `Simple & Client`) is out of scope —
  say so.
- `device.FixedLabel` goes (removal rule; migration-guide line
  "`device.FixedLabel(labels…)` → `fixedlabeldef.Declare{Initial:
  fixedlabeldef.State{LabelList: …}}`"); the reference bridge and
  every test move. CHANGELOG `### Changed` names the removed symbol
  (apidiff-check needs the line).
- Tests: a FixedLabel and a UserLabel on an OnOffLight through the
  declaration on `fabrictest` (read → the labels; UserLabel write →
  the host's `OnChanged`), the refusal test rewritten to a cluster
  that is neither required nor Base-optional.

## Guards and report

`make ci` (archcheck, apidiff-check, the new slogkeys), `make race`
(twice: the ring's observer and the metrics hook are new concurrency),
`make reachability` then `-check`, `make cite-check`
(`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`),
`make testparity-check`, `go test ./script/...`, `mkdocs build
--strict`. CHANGELOG `### Added` (metrics port and names, expvar,
ring on the facade, observability guide, Base-optional clusters by
declaration), `### Changed` (slog keys — a host that greps logs reads
the table; `device.FixedLabel` removed; the `client/*` nil logger),
guide rows. Report `notes/briefs/reports/CB.md`: **Metrics** (name →
source counter → site), **Ring**, **slog** (rename table with counts,
the guard), **Base clusters** (where the rule sits, with the citation),
**Guards** with HEAD, **Differences**, **Decisions**, **Left out**,
**Questions**, `by_design` candidates with citations. Copy this brief
to `notes/briefs/2026-10-24-CB-m4-phase-21b-observability.md` in the
first commit. Never add attribution lines to commits or the PR body.
