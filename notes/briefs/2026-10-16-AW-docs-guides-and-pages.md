# Brief AW — developer guides and the GitHub Pages site

Owner: Fable. Implementer: Opus. The owner works in parallel on the
cleanup of `docs/` and writes `docs/index.md`, `docs/architecture.md`
and `docs/security.md`; stay out of those three files and out of every
existing file under `docs/` (read them, do not edit). Branch
`docs/guides` from `planning/briefs-docs`, pushed, **no PR**. Report
`notes/briefs/reports/AW.md`. Never run chip-tool.

Audience: developers who embed the module and architects who evaluate
it. English, plain, task-oriented, every claim checkable against the
code you cite (`path:line` in a short "Source" line under each section,
not in prose). Read first: `CLAUDE.md` (§Architecture and §Token use),
`README.md` (§API stability, versioning), `contract/doc.go`,
`bridge/bridge.go` (package doc: every port in call order), `node/doc.go`,
`node/config.go`, `examples/reference-bridge/main.go` (composition root),
`controller/*/doc.go`, `controller/client/doc.go`,
`examples/reference-controller/README.md`, `store/doc.go`,
`store/migrations.go`, `store/settings.go`, `transport/udp/doc.go`,
`im/subscription/doc.go`, `controller/discovery/doc.go`,
`cluster/levelcontrol/native.go` (optional host interfaces), the owner's
inventory `notes/briefs/reads/read-docs.md` (copied from the session;
it lists where each topic lives today).

## The pages (new files only)

1. `docs/getting-started/device.md` — embed the device side: when to use
   `node/` (the facade) and when `bridge/` directly; a minimal host in
   ~60 lines of Go that compiles (put the program under
   `examples/quickstart-device/main.go` with a test that builds it and
   starts/stops the node — the page includes it by reference, not by
   copy); the topology (`Snapshotter`/`node.Config.Topology`), the
   device types (`device/`), commissioning (QR/manual code), persistence
   (`store`), stopping cleanly; what the reference daemon adds.
2. `docs/getting-started/controller.md` — the controller side: the
   stack `examples/reference-controller` builds (cite its `open()`),
   pairing (codes, attestation, DCL opt-in), the registry, reading,
   writing, invoking, subscribing, the device view; a minimal program
   under `examples/quickstart-controller/main.go` with a build test.
3. `docs/guides/ports.md` — every host port in one table: `contract/`
   interfaces, the `node.Config` ports (OTA, OTA provider, Wi-Fi,
   Client, CommissionerControl, EndpointPersistence, attestation),
   `bridge` `Attach…/Set…` ports (from the package doc), the optional
   interfaces (`levelcontrol.NativeMover`/`NativeSwitcher`,
   `mdns.Reannouncer`, …) — what each does, nil behaviour, fail closed or
   silent (the bridge doc says which).
4. `docs/guides/concurrency.md` — the goroutines (listener, per-session
   ordered dispatch, ack pump, subscriptions, reapers, re-announce),
   what a host callback may do (blocking, calling back into the node),
   ordering guarantees, contexts and shutdown.
5. `docs/guides/persistence.md` — the SQLite store: tables per
   migration, the settings keys (`store/settings.go` and the facade's
   `scenes.`, `bridged_node_label.`, `binding.` keys), what survives a
   restart, the controller's database (fabric record, registry,
   `sqlstore`), backup and migration rules.

## GitHub Pages

- `mkdocs.yml` at the repo root: Material theme, `docs_dir: docs`,
  search, `strict: true`; `exclude_docs` for `history/`,
  `concept-v3-*.md`; nav:
  Home (`index.md`), Getting started (device, controller), Architecture
  (`architecture.md`), Guides (ports, concurrency, persistence,
  `security.md`), Reference (ADRs index `adr/README.md` + every ADR,
  `matterjs-comparison.md`, `matter-parity-contract.md`,
  `certifiability.md`, `chip-datamodel-crosscheck.md`,
  `matter-ecosystem-observations.md`), Project (links to
  CONTRIBUTING.md, CLA.md, CHANGELOG.md, LICENSE on GitHub — absolute
  URLs, they live outside `docs/`). `edit_uri` to the repo.
- `.github/workflows/pages.yml`: on pull_request touching `docs/**`,
  `mkdocs.yml` or the workflow → `mkdocs build --strict` (the link
  check); on push to `main` → build and deploy with
  `actions/upload-pages-artifact` + `actions/deploy-pages`, pinned by
  SHA as the other workflows pin; `mkdocs-material` pinned in a
  `docs/requirements.txt` (exact versions). No Go module change.
- Run `mkdocs build --strict` locally in a venv under the worktree's
  `bin/` (not committed) with the owner's three files absent: list
  every warning that only their arrival resolves; fix every other.
- `CLAUDE.md` is not changed; the owner adds the docs rule.

## Guards

`make ci`, `make reachability-check` (the two quickstart programs are
new packages: give them a coverage floor in `script/coverfloor/floors.go`
or an exemption with the reason, as the existing examples have).
Export `PATH=$HOME/go/bin:$PATH`. One commit per page plus one for
Pages. Every new `.go` file carries the Apache-2.0 header.

## Report

Per page: what it covers, sources, open questions (anything the code
does that looks unintended — do not fix, report). The `mkdocs build`
warnings left for the owner's files. Push `docs/guides`; no PR.
