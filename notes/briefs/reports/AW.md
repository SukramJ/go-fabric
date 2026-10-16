# Report AW — developer guides and the GitHub Pages site

Branch `docs/guides` from `planning/briefs-docs` (60f8cab), pushed, no PR.
Only new files under `docs/`; no existing file under `docs/` touched, no
`CLAUDE.md` change. Every page carries a "Source:" line per section with
`path:line`. Two commits add Go code (the quickstarts); each carries its
coverage floor and the regenerated reachability inventory (unreached
unchanged at 5).

| Commit | Item |
| --- | --- |
| 0511ef5 `docs: getting started on the device side; examples/quickstart-device` | page 1 |
| 6ab4c7a `docs: getting started on the controller side; examples/quickstart-controller` | page 2 |
| 1febf17 `docs: guides/ports.md` | page 3 |
| c7a1f29 `docs: guides/concurrency.md` | page 4 |
| a217196 `docs: guides/persistence.md` | page 5 |
| 17a9059 `docs: the GitHub Pages site` | Pages |
| (this commit) `notes: report AW` | — |

## Per page

### 1. `docs/getting-started/device.md`

Covers: `node/` vs `bridge/` (and `Config.Topology` in between), the
program, `Config.Validate`'s checks and the defaults table, the device
layer (`device.New<Type>`, `Options`, the `With…` options, `OnChanged`,
OnOff `Sink`/`Set` for the two directions), the Aggregator (`Add`/`Remove`
before and after `Start`, `MarkComplete` and endpoint-number GC),
commissioning (`PairingCodes`, own window, `OpenBasicWindow`,
`SetCommissioningBlocked`), persistence (driver, DSN pragmas), stopping
(`Run`, `Stop` order, `ErrStopped`, `Stop` after `New`), what the reference
daemon adds.

Program: `examples/quickstart-device/main.go` (108 lines with header and
imports; one OnOffLight, Zeroconf with the subtype responder). Included in
the page by `--8<--` (pymdownx.snippets), not copied. Test: builds the
node on loopback with `mdns.NewNoop`, starts, finds the light and the
pairing codes, stops, rebuilds on the same database (endpoint number
holds); a second test runs `Node.Run` and cancels it. Floor 20 (measured
24.4; `main`/`run` bind :5540 and the mDNS groups), with the reason.

Sources: `node/doc.go`, `node/config.go`, `node/node.go`,
`node/aggregator.go`, `node/commissioning.go`, `device/doc.go`,
`device/options.go`, `device/device.go`, `cluster/onoff/server.go`,
`examples/reference-bridge/main.go`, `persist.go`, `bridge/bridge.go`.

### 2. `docs/getting-started/controller.md`

Covers: the stack in `openStack`'s order (`examples/reference-controller/
stack.go:119-203`; the brief's "open()" is `openStack`), the program, the
fabric (CA and fabric record in the settings table, `ErrFabricMismatch`),
pairing (codes, scanner, `ForDevice`, `Commission`, `CommissionOptions`,
`Decommission`), attestation (test store, nil store, DCL opt-in, the four
policy forms — the zero value proceeds), the registry and the device view,
read/write/invoke/subscribe, what is not there yet.

Program: `examples/quickstart-controller/main.go` (pair by code, read
NodeLabel, toggle OnOff on endpoint 1, subscribe until Ctrl-C). Test:
opens the stack twice on one database with a silent DNS-SD conn (same
fabric id and root node id), `run` without a node, a malformed code, a
valid code on a silent network. Floor 55 (measured 59.3–60.2).

Sources: `controller/*/doc.go`, `controller/client/{registry,fabricops,
interact,endpoint,cluster}.go`, `controller/commissioner/{commissioner,
fabric}.go`, `controller/attestation/{policy,store}.go`,
`controller/ca/persist.go`, `controller/interaction/{write,invoke,
subscribe,handler}.go`, `examples/reference-controller/{stack,pair,
interact}.go`, its README.

### 3. `docs/guides/ports.md`

Covers: the `contract/` interfaces in three tables (endpoints and servers,
change reporting, measurements) with the absent behaviour of each; the
`node.Config` ports with nil defaults; the ports left to `Root()` /
`Bridge()`; every `bridge` `Attach…`/`Set…` port in the package doc's
groups and order with fail-closed/silent; the optional interfaces
(`levelcontrol.NativeRamp`/`NativeMover`/`NativeSwitcher`,
`mdns.Reannouncer`/`Enabled`, `onoff.Sink`) and the root servers' host
ports (`WiFiReporter`, `OtaApplier`, the three OTA provider ports,
CommissionerControl's two, `ActionsHost`, `LogProvider`).

Sources: `contract/matter.go`, `contract/switchevents.go`,
`node/{config,client,ota_provider,commissioner_control,observers,persist,
root,reannounce}.go`, `bridge/bridge.go:29-177`, `bridge/securechannel.go`,
`bridge/ackpump.go`, `cluster/levelcontrol/native.go`, `mdns/reannounce.go`,
`cluster/core/*`.

### 4. `docs/guides/concurrency.md`

Covers: a table of the goroutines (read loop, handlers, TCP, ACK pump
50 ms, engine tick 250 ms and per-subscription reports, session reaper
5 min/1 min, PASE/CASE handler reapers 30 s/1 min, operational-time keeper,
re-announce loop, BDX, binding manager, client socket), per-session ordered
dispatch and what is not ordered (unsecured, group; 1024 unordered slots;
256 per session), what each kind of host callback may do (servers, Sink,
Observers, OnChanged, controller `Updated`, discovery's loop), the
last-writer-wins reassembly behind `Add`/`Remove` (advice: serialise
them), contexts and shutdown (what `Stop` waits for and what it does not).

Sources: `transport/udp/{listener,ordered}.go`, `transport/tcp/doc.go`,
`bridge/{bridge,session_order,ackpump,receive,securechannel,
bdx_responder}.go`, `im/subscription/{manager,engine}.go`,
`secure/operational/manager.go`, `node/{security,node,observers,
reannounce,client,aggregator}.go`, `endpoint/dispatcher.go`,
`controller/interaction/{handler,subscribe}.go`, `controller/discovery/
doc.go`.

### 5. `docs/guides/persistence.md`

Covers: opening the database (pragmas and why), the `store` tables by
migration step and release, the `sqlitestore` tables, every settings key
written by the facade, `store` and `cluster/core` (and the metadata
counters), what survives a restart and what does not, the controller's
database, `Apply`/`Migrations`/`SchemaFingerprint` per package, backup
rules (private keys, consistency, one running copy).

Sources: `store/{doc,schema,migrations,settings,metadata,ota}.go`,
`store/schema.sql`, `store/migrations/*.sql`, `endpoint/sqlitestore/`,
`controller/client/sqlstore/`, `controller/ca/persist.go`,
`node/{persist,root,node,security,client}.go`, `cluster/core/
{access_control,vid_verification}.go`, the reference daemon's and
controller's `persist.go`/`stack.go`/README.

## Pages

- `mkdocs.yml`: Material, `docs_dir: docs`, search, `strict: true`, plus
  `validation.links.anchors: warn` (a missing heading fails like a missing
  page; no current anchor finding). `exclude_docs`: `history/`,
  `concept-v3-*.md`, `requirements.txt`. Nav as the brief; Reference lists
  `adr/README.md` and the 24 ADRs by their titles; Project links are
  absolute GitHub URLs. `edit_uri: edit/main/docs/`.
- `pymdownx.snippets` (`base_path: ["."]`, `check_paths: true`) includes the
  quickstarts; a moved file fails the build. On GitHub the `--8<--` line
  renders as text — the page says where the program lives.
- `script/mkdocs/repo_links.py` (hook, new): the existing docs link
  `../CLAUDE.md`, `../notes/parity/by_design.md`, `../cluster/core/groups.go`
  and the like — 34 strict warnings with no hook. It rewrites a link that
  leaves `docs/` to its GitHub `blob`/`tree` URL when the target exists in
  the repository; a missing target stays a warning. This was the only way to
  fix them without editing existing files.
- `.github/workflows/pages.yml`: PR on `docs/**`, `mkdocs.yml`,
  `script/mkdocs/**`, the two quickstart `main.go` and the workflow →
  `mkdocs build --strict`; push to `main` (same paths) or dispatch → build,
  `upload-pages-artifact`, `deploy-pages` (`pages: write`, `id-token: write`
  on the deploy job only). Pinned by SHA: checkout v7.0.1 (as ci.yml),
  setup-python v7.0.0 `5fda3b95…`, upload-pages-artifact v5.0.0
  `fc324d35…`, deploy-pages v5.0.1 `368f8252…`.
- `docs/requirements.txt`: `mkdocs==1.6.1`, `mkdocs-material==9.7.7`
  (2026-07-17), `pymdown-extensions==12.1` (2026-09-23; 12.2/12.3 are under
  two weeks old).
- The repository's Pages source must be set to "GitHub Actions" once
  (Settings → Pages); the workflow cannot do that.

### `mkdocs build --strict` (venv under `bin/`, owner's files absent)

Resolved by the owner's three files:

    WARNING - A reference to 'index.md' is included in the 'nav' configuration, which is not found in the documentation files.
    WARNING - A reference to 'architecture.md' is included in the 'nav' configuration, which is not found in the documentation files.
    WARNING - A reference to 'security.md' is included in the 'nav' configuration, which is not found in the documentation files.

Not resolved by them (an existing file; the brief keeps me out of it):

    WARNING - Doc file 'adr/0022-media-and-content-clusters.md' contains a link './0017-standard-server.md', but the target 'adr/0017-standard-server.md' is not found among documentation files.

With stub `index.md`/`architecture.md`/`security.md` the build reports
that one warning and nothing else. INFO only (strict passes them): the five
files not in the nav (`concept-matter-1.6.1-and-device-layer.md`,
`concept-v2-scope-extension.md`, `feature-scope.md`,
`manual-test-plan-0.4.0.md`, `open-items.md` — built and published unless
they move to `history/`), links from `adr/0024` and `concept-v2` to the
excluded concept v3, and `matter-parity-contract.md`'s `./adr/` link
(MkDocs suggests `adr/README.md`). The owner's pages may link to the five
guides as `getting-started/device.md`, `getting-started/controller.md`,
`guides/ports.md`, `guides/concurrency.md`, `guides/persistence.md`.

## Guards

At HEAD 17a9059 (before the report commit), `PATH=$HOME/go/bin:$PATH`:

| Guard | Result |
| --- | --- |
| `make ci` (build, vet, test, lint, cover-check) | exit 0 |
| `make reachability-check` | exit 0 (unreached 5, unchanged) |
| `go test -race` on both quickstarts | ok |
| `mkdocs build --strict` | 4 warnings, listed above |

No chip-tool run (no change to the daemon's composition).

## Open questions (code that looks unintended — reported, not fixed)

1. **A committed controller database.**
   `examples/reference-controller/reference-controller.db` (147 KB) has been
   tracked since #113 (b0bd740). The README next to it says the file holds
   the CA root key and the fabric's operational key. If it was a test run's
   leftover, it should go and be ignored; the keys in history are public
   either way.
2. **Ordered dispatch bypasses the dispatch bound.** `Listener.Serve`
   bounds unordered handlers at 1024 (`transport/udp/listener.go:35-44`,
   `:196-210`), but a datagram with a non-zero unicast session id is queued
   by that id before any session lookup or decryption
   (`bridge/session_order.go:36-50`, `transport/udp/ordered.go:56-110`):
   each distinct id gets a drain goroutine and a handler goroutine and up to
   256 queued datagrams, outside the semaphore. Forged ids (up to 65 535)
   are not bounded by it. Probably harmless while handlers drop unknown
   sessions fast; worth a decision.
3. **Concurrent `Aggregator.Add`/`Remove`.** Each reassembles on its own
   goroutine; `reassembleLocked` is last-writer-wins regardless of snapshot
   age (`bridge/bridge.go:886-892`, `node/aggregator.go:70-112`), so a
   topology can miss a just-added device until the next reassembly. With
   `MarkComplete` set, the older model-complete snapshot may also
   garbage-collect the newer device's endpoint number (not traced through
   the assembler). The guide tells hosts to serialise the calls; the
   facade could do it itself.
4. **`Stop` does not wait for in-flight handlers.** `bridge.Stop` waits for
   the read loop, TCP loop and ACK pump; datagram handler goroutines are
   not tracked (`transport/udp/listener.go:147-212`), so a host server can
   still be running when `Node.Stop` returns (and the host closes its
   database). Documented in the concurrency guide as is.
5. **Own window when fabrics exist but fail to load.** `Node.Start` opens
   the node's own commissioning window when `len(loaded) == 0`, where
   `loaded` excludes a fabric whose CASE identity failed to load
   (`node/node.go:500-534`). A commissioned node whose identities all fail
   to load advertises itself as commissionable. The device page says "loads
   no fabric" to stay true to the code.
6. **The attestation policy's zero value proceeds**
   (`controller/attestation/policy.go:42-61`), as matter.js does "for
   backward compatibility". The quickstart and the page set it explicitly;
   is a `by_design` entry wanted, or should the Go zero value reject?
7. **Host-specific wording in public docs and logs**: `bridge/bridge.go:4-7`
   ("the daemon's bootstrap … when matter.enabled is set in config"); the
   log hint `set north.matter.mdns_advertise=zeroconf`
   (`bridge/bridge.go:1986`, a Loom config key, printed by every
   `mdns.NewNoop` node); `store.SettingUniqueIDRotated`'s doc
   (`north.matter.dev_rotate_unique_ids`); `store/doc.go:38-40` ("for v1.1;
   an at-rest encryption ADR is expected for v1.2" — not this module's
   versions); `contract/matter.go` ("Custom DPs", "v1.1 Matter bridge",
   "HAP service rebuild"); `im/subscription/doc.go` ("v1.1 … subscriptions
   live in RAM", contradicted by its next paragraph).
8. **`controller/commissioner/doc.go:15-18`** lists "the DCL" as out of
   scope; `controller/dcl` exists (stage 6) and feeds the commissioner's
   trust store.
9. **README §API stability** ("Depend freely on …") lists neither
   `controller/…` nor `binding/`; their API status is unstated.
10. **`matter_diagnostics`** and `store.LoadDiagnostics`/`SaveDiagnostics`
    are used by no module code; the facade keeps RebootCount and operational
    time under `gendiag.*` settings keys.
11. **`endpoint/sqlitestore`** has `Schema`/`Apply` but no `Migrations` or
    `SchemaFingerprint`, unlike `store` and `controller/client/sqlstore`
    (issue #62's shape).
12. **`bridge.AttachAckHandler`** is not in the bridge package doc's port
    list (`AttachAckTracker` wraps it); the ports page names it under
    `AttachAckTracker`.

## Decisions taken

- The quickstarts are `package main` under `examples/` (not API, as the
  reference programs), with `run`/`newNode`/`open` split out so the tests
  drive them without binding :5540 or the mDNS groups.
- No CHANGELOG entry: the brief is silent and the owner edits in parallel;
  the examples and the site are user-visible, so one may be wanted at merge.
- Absent-behaviour claims for `contract/` interfaces are taken from the
  interfaces' own docs (cited), not re-traced through every caller.
