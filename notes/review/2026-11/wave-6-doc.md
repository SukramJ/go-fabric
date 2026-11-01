# Wave 6 — `doc`: the published docs against the code

- **Scope:** the published pages (`docs/*.md` but the unpublished
  `concept-v7/8/9`, `docs/guides/*.md`, `docs/getting-started/*.md`,
  `docs/reference/*.md`), the Decision and Consequences sections of ADR
  0001–0030, `README.md`, `CHANGELOG.md` `[Unreleased]` and 0.15.4–0.15.6,
  the `mkdocs.yml` nav, and the package docs of `node`, `device`,
  `contract`, `bridge`, `endpoint`, `im`, `secure`, `transport`, `mdns`,
  `store`, `controller`, `client`.
- **HEAD:** `769ea3c2`.
- **Not repeated:** W2-api-01 (`SetOnFabricAdded`), W2-api-02 (unread
  `bridge.Config` fields), W2-api-03 (1..65534 vs 2..65534), W2-api-04/05,
  W5-ex-02/03/04/11 (quickstart contradictions, reference-bridge README,
  `fabrictest/doc.go`), W5-api-16 (`node/doc.go` lines).
- **Pages checked:** 31 pages and package docs read (see the last two
  sections for depth), 30 ADR Decision/Consequences sections checked
  mechanically, five of them read.
- **Claims checked:** 3 230 references checked by script (1 752 qualified
  identifiers, 1 464 repository paths and line ranges, Make targets,
  environment variables, CLI flags, absolute GitHub URLs, mkdocs nav and
  anchors), about 250 behavioural claims verified by hand (defaults,
  limits, lifecycle order, log keys, metric names, settings keys, schema
  tables, changelog entries against the release diffs).
- **Guards run:** `mkdocs build --strict` exit 0 (12 INFO lines, see
  W6-doc-19); `go test ./script/docscheck` ok.

Findings: **0 high, 2 medium, 23 low, 2 note.**

---

## W6-doc-01 — medium — "a port's `contract.StatusError` answers its status" does not hold for OnOff's `Sink` or ClosureDimension's handler

- **Page:** `docs/getting-started/device.md:310-319`; `docs/guides/ports.md:15-19`.
- **Claim:** device.md: "To refuse a command … declare the light's switch:
  `onoffdef.Declare{Sink: relay}`. An error from the sink answers FAILURE …
  A hook, a port or a host server that wants another status returns (or
  wraps) a `contract.StatusError{…}`: the controller receives that status".
  ports.md: "A port's error is the answer of the request that called it: a
  `contract.StatusError` answers its status".
- **Code:** `cluster/onoff/server.go:378-379` wraps every sink error as
  `spec.Errorf(im.StatusFailure, "…: %v", err)` with no `spec.CarriesStatus`
  check, so a `StatusError{Status: 0x9C}` from the sink answers FAILURE.
  The same in `cluster/closure/closuredimension_server.go:556-557` and
  `:615-619` (`Handler.SetTarget`, `Step`). v0.15.6 added the check to
  camera, thermo, tls, lock, cover, jointfabric, threadbr and core
  (`CHANGELOG.md:22`), not to these two. `ports.md:256` states FAILURE for
  `onoff.Sink`, so the page contradicts its own rule.
- **Correction:** add the `spec.CarriesStatus` pass-through to OnOff's
  sink and ClosureDimension's handler as v0.15.6 did for the others
  (ADR 0026 principle 8 also asks for `%w`), or, if FAILURE is meant,
  name those ports as exceptions in both pages.
- **Confidence:** confirmed (code path read; not run).

## W6-doc-02 — medium — the "guards that hold this" are another repository's

- **Page:** `docs/matter-ecosystem-observations.md:104-124`.
- **Claim:** five guards (`TestBridgedEndpointClustersConformToTheirDeviceType`,
  `TestEveryMandatoryAttributeIsAnswered`,
  `TestFeatureGatedAttributesAreAnsweredWhenAdvertised`,
  `TestEveryMandatoryCommandIsAccepted`,
  `TestMeasurementClassProjectsOntoAConformantDeviceType`) "read the same
  oracle … which the go-fabric module generates and this repo pins (`make
  sync-matter-schema`). They live under `tests/integration/` and
  `tests/contract/`." Also: the chip-tool suite "is the only place a real
  commissioner is in the loop".
- **Code:** none of the five test functions exists in this module (grep
  over every `_test.go`); there is no `tests/` directory and no
  `sync-matter-schema` target (`Makefile`; the target is
  `generate-matter-schema`). The section is carried over from
  OpenCCU-Loom. This module's guards with the same purpose are
  `TestBridgedEndpointsMeetTheirDeviceTypeConformance`,
  `TestApplicationDeviceTypesMountTheirMandatoryClusterSet`,
  `TestApplicationDeviceTypesMeetTheirDeviceTypeRequirements` and
  `endpointtest.AssertDeviceTypeConformance` (ADR 0016). The module's own
  controller commissions the module's node in-repo (`docs/index.md:32-33`,
  `fabrictest`), so "the only place" is stale too.
- **Correction:** replace the table with this module's guards and the
  real target, or mark the section as the host's and link it.
- **Confidence:** confirmed.

---

## Low — `README.md`

### W6-doc-03 — the status table is the 2026-10-17 baseline under a v0.15.6 heading

- **Page:** `README.md:29-37`.
- **Claim:** "As of `v0.15.6` …": "118 of 135 clusters served, 79 of 91
  device types generated"; Thread "dataset provisioning only, by the
  commissioner"; ICD "milestone 5" (a link, no state).
- **Code:** all 135 served (`docs/matterjs-comparison.md:99`,
  `docs/roadmap.md:312`), 91 device types committed and `excluded` empty
  (`script/devicegen/devicetypes.go:28`, `:139`); a Thread Border Router
  over OTBR and ThreadNetworkDirectory serve on the device side (ADR 0027,
  `cluster/threadbr`, `cluster/netinfra`); ICD shipped with milestone 5
  (`docs/guides/icd.md`, `node/icd.go`).
- **Correction:** state the current counts and states, and keep the
  baseline only as a link.
- **Confidence:** confirmed.

### W6-doc-04 — `internal/` is described as two seams and the chip-tool suite

- **Page:** `README.md:121-123`.
- **Claim:** "`internal/` holds the two one-way seams … (`bridgeseam`,
  `channelseam`) and the chip-tool suite".
- **Code:** `internal/` holds 13 packages: also `deviceseam`, `nodeseam`,
  `surfaceseam`, `udpseam`, `paritytest`, `chipdm`, `clockvar`, `forward`,
  `inflight`, `serialqueue`.
- **Correction:** describe the directory by kind (test seams, test
  support, shared internals) instead of naming two.
- **Confidence:** confirmed.

### W6-doc-05 — the package lists of §API stability are incomplete

- **Page:** `README.md:127-137`.
- **Claim:** the "Depend freely on" list and the test-support list.
- **Code:** `clock/`, `icd/`, `thread/` are missing from the first;
  `surface/surfacetest` and `store/storetest` from the second (both public,
  both named in `docs/architecture.md:166` and
  `docs/guides/persistence.md:79`).
- **Correction:** add them.
- **Confidence:** confirmed.

## Low — `docs/index.md`, `docs/matterjs-comparison.md`, `docs/architecture.md`, `docs/roadmap.md`

### W6-doc-06 — the landing page's numbers and BLE state are stale

- **Page:** `docs/index.md:27`, `:28-31`; `docs/matterjs-comparison.md:144`.
- **Claim:** "118 served clusters and 79 generated device types"; "the
  commissioner's BLE support is decided and in progress"; comparison:
  "79 of the snapshot's 91".
- **Code:** as W6-doc-03; BLE commissioning is done with milestone 1
  (`README.md:35`, `controller/ble`, `ble/bluez`, `ble/esphome`).
- **Correction:** 135, 91, and "the commissioner commissions over BLE".
- **Confidence:** confirmed.

### W6-doc-07 — the `client/` forwarders are said to exist until v0.12.0

- **Page:** `docs/architecture.md:83-84`.
- **Claim:** "the old import paths forward to them until v0.12.0."
- **Code:** `controller/` has no `discovery`, `exchange`, `handshake`,
  `interaction` or `peer` package (removed); the module is at v0.15.6.
- **Correction:** "the old import paths were removed in v0.12.0".
- **Confidence:** confirmed.

### W6-doc-08 — camera and WebRTC clusters are listed as out of scope

- **Page:** `docs/architecture.md:189`.
- **Claim:** "Camera and WebRTC clusters | need a media path in the host"
  in the table "Boundaries that are decisions".
- **Code:** milestone 6 serves them (`cluster/camera`,
  `docs/guides/camera.md`, comparison row `docs/matterjs-comparison.md:132`
  ✅).
- **Correction:** drop the row, or reword it to "the media path itself is
  the host's (`camera.*Port`)".
- **Confidence:** confirmed.

### W6-doc-09 — milestone 7 carries a version already used

- **Page:** `docs/roadmap.md:103`.
- **Claim:** "7 | BLE on the device side | `v0.15.0`".
- **Code:** `v0.15.0`–`v0.15.6` shipped the surface policies (concept v11)
  and fixes (`CHANGELOG.md:13-90`); device-side BLE is not built. The
  roadmap records nothing of the v0.15 line.
- **Correction:** move milestone 7's version and add the v0.15 line's
  status paragraph.
- **Confidence:** confirmed.

## Low — `docs/guides/persistence.md`

### W6-doc-10 — "never opens a database itself"

- **Page:** `docs/guides/persistence.md:3-6`.
- **Claim:** "the host opens the database … the module … never opens a
  database itself."
- **Code:** `node/store_path.go` opens the SQLite file for
  `Config.Store.Path`; the same page says so at `:121-123`.
- **Correction:** "the `store` package never opens one; the node does
  for `Store.Path`".
- **Confidence:** confirmed.

### W6-doc-11 — the fabric cascade omits the ICD registrations

- **Page:** `docs/guides/persistence.md:61-65` (and `store/port.go:23-28`,
  which the page mirrors).
- **Claim:** "`RemoveFabric` removes … its identity, key sets, key map,
  group table, Groupcast groups, ACL, resumption records and default OTA
  providers."
- **Code:** `matter_icd_registrations` cascades too
  (`store/schema.sql:357-365`), `store/memstore/memstore.go:278` deletes
  them, and the table row at `:154` says "removed with the fabric". A host
  backend written from the list keeps a removed fabric's Check-In keys.
- **Correction:** add "ICD registrations" to both lists.
- **Confidence:** confirmed.

### W6-doc-12 — the settings-key table misses seven key families

- **Page:** `docs/guides/persistence.md:166-195`.
- **Claim:** the table of keys, then "Pick a prefix that none of the keys
  above use."
- **Code:** written but not listed: `basic_information.surface_digest`,
  `bridged_surface_digest.<key>` (`node/surface_stability.go`),
  `endpoint_number_reserved.<key>` (`node/reserve.go:35`),
  `timesync.default_ntp`, `timesync.dst_offset`, `timesync.time_zone`,
  `timesync.trusted_time_source` (`cluster/core`). A host choosing its
  prefix from the table can collide.
- **Correction:** add the seven, or derive the table from the code.
- **Confidence:** confirmed.

### W6-doc-13 — a citation past the end of its file

- **Page:** `docs/guides/persistence.md:162`.
- **Claim:** `endpoint/sqlitestore/schema.sql:13-60`.
- **Code:** the file has 58 lines. `TestCitationsResolve` checks
  `.go|md|json|yml|yaml|py|sh` only (`script/docscheck/docscheck_test.go:101`),
  so `.sql` citations are unguarded.
- **Correction:** `:13-58`, and add `sql` to the guard's pattern.
- **Confidence:** confirmed.

## Low — `docs/guides/ports.md`, `docs/getting-started/device.md`, `docs/guides/concurrency.md`

### W6-doc-14 — "the camera, Thread and TLS clusters … have no port"

- **Page:** `docs/guides/ports.md:273-281`.
- **Claim:** "Clusters the generated server serves with no hand-written
  logic — among them the camera, Thread and TLS clusters, as matter.js
  serves them with its generated behaviour stubs — have no port beyond
  their `Declare`".
- **Code:** the same page's table lists `camera.*Port`,
  `threadbr.BorderRouter`, `tls.CertificatePort` (`:239-262`), backed by
  hand-written servers in `cluster/camera`, `cluster/threadbr`,
  `cluster/tls`.
- **Correction:** drop the three examples; name a cluster that really has
  no logic.
- **Confidence:** confirmed.

### W6-doc-15 — `Config.Validate`'s list is incomplete

- **Page:** `docs/getting-started/device.md:164-180`.
- **Claim:** "It checks:" and thirteen items.
- **Code:** `node/config.go:529-596` also refuses an unknown
  `DeviceTypeValidation` mode, invalid `Limits` and `Sessions`,
  `TimeSynchronization` beside `DisableTimeSynchronization`, and an
  invalid `ICD`.
- **Correction:** add the five, or say "among others" and link `Validate`.
- **Confidence:** confirmed.

### W6-doc-16 — `node/node.go` citations drifted by about 100 lines

- **Page:** `docs/getting-started/device.md:30`, `:211`, `:403`, `:471`;
  `docs/guides/concurrency.md:54`, `:177`; `docs/guides/persistence.md:215`;
  `docs/guides/ports.md:284`.
- **Claim:** e.g. `node/node.go:790-792` for `Node.Bridge`,
  `:900-907` for `PairingCodes`, `:835-855` for `SetCommissioningBlocked`,
  `:715-788` / `:723-788` for `Stop`.
- **Code:** `Bridge` is at `node/node.go:936`, `PairingCodes` `:1048`,
  `SetCommissioningBlocked` `:989`, `Stop` `:815-878`; the cited lines are
  other code (`:900` is `countedWork.pause`). The guard checks only that a
  range lies inside the file.
- **Correction:** re-cite by function name, or regenerate the lines.
- **Confidence:** confirmed.

## Low — `docs/guides/embed-in-a-daemon.md`, `docs/matter-ecosystem-observations.md`

### W6-doc-17 — the v0.10.8 caveat still points at `[Unreleased]`

- **Page:** `docs/guides/embed-in-a-daemon.md:114-126`.
- **Claim:** the typed PowerSource declaration "needs the release after
  v0.10.8, which carries this cookbook's facade fix (CHANGELOG,
  Unreleased) … The daemon moves to the typed declaration when a release
  carries both fixes."
- **Code:** both fixes have shipped (the module is at v0.15.6).
- **Correction:** name the release that carries them and drop the
  conditional.
- **Confidence:** plausible (the release number not traced).

### W6-doc-18 — host paths outside `internal/` presented as this repository's

- **Page:** `docs/matter-ecosystem-observations.md:22-26` against `:82-84`.
- **Claim:** "A source path beginning `internal/` is OpenCCU-Loom's …
  Everything else is a path in this repository."
- **Code:** `pkg/interfaces/matter.go`, `docs/user/matter.md` and "ADR
  0049" (`:82-84`) are not in this repository (no such files; the ADRs end
  at 0030).
- **Correction:** prefix these as Loom's or link them.
- **Confidence:** confirmed.

## Low — the site build

### W6-doc-19 — twelve relative links into the unpublished `history/`

- **Page:** `docs/roadmap.md:32`, `:188`; `docs/matterjs-comparison.md:111`,
  `:207`; `docs/adr/0017…:4`, `0018…:4`, `0019…:4`, `0020…:4`, `:14`,
  `0021…:4`, `0022…:4`.
- **Claim:** `[…](./history/concept-…md)`.
- **Code:** `mkdocs.yml` excludes `history/`; the build renders these as
  `../history/…/` links that 404 on the site. Strict mode passes because
  mkdocs reports them at INFO. `roadmap.md:112` shows the working form (an
  absolute GitHub URL).
- **Correction:** use absolute GitHub URLs for links into `history/`.
- **Confidence:** confirmed (rendered HTML inspected).

## Low — package docs

### W6-doc-20 — `endpoint/doc.go` describes the Loom integration

- **Page:** `endpoint/doc.go:16-25`.
- **Claim:** "the bridge core subscribes to the event bus and triggers
  re-assembly"; "Endpoint identity is NOT persisted here … the owner
  implements [Store] over its own table."
- **Code:** no event bus exists in the module (the node reassembles on
  `Aggregator.Add/Remove`, `node/aggregator.go`); the module ships
  `endpoint/sqlitestore` and `endpoint/memstore`.
- **Correction:** describe reassembly by `Bridge.Reassemble` / the
  Aggregator and name the shipped stores.
- **Confidence:** confirmed.

### W6-doc-21 — `im/doc.go`: cross-restart resumption "deferred by design"

- **Page:** `im/doc.go:35-38`.
- **Claim:** "cross-restart resumption is deferred by design".
- **Code:** implemented by ADR 0008 (`bridge/subscription_reestablish.go`,
  `im/subscription/persistence.go`); the by_design entry is retired
  (ADR 0008 header).
- **Correction:** point at ADR 0008 and `im/subscription`.
- **Confidence:** confirmed.

### W6-doc-22 — `mdns/doc.go`: instance names and a foreign ADR

- **Page:** `mdns/doc.go:9-16`; `mdns/service.go:363`.
- **Claim:** operational name "16+16 lowercase hex"; commissionable name
  "a 16-byte random identifier in lowercase hex"; "see ADR 0012 on the
  pairing UI".
- **Code:** both names are upper case (`mdns/service.go:274`, `:520`);
  `InstanceID` is `[8]byte` (`:366`) though its comment says 16-byte; ADR
  0012 here is the ScenesManagement server.
- **Correction:** upper-case hex, 8 bytes (16 hex digits), drop the ADR
  reference.
- **Confidence:** confirmed (wire behaviour is right; only the docs).

### W6-doc-23 — `store/doc.go`: "does not encrypt at rest"; `transport` omits BTP

- **Page:** `store/doc.go:38-39`; `transport/conn.go:8-10`.
- **Claim:** "the module does not encrypt at rest"; "The concrete
  transports live in the sub-packages: transport/udp … and transport/tcp".
- **Code:** `store` seals ICD keys at rest (`store/icd.go:67`,
  `store/icd_cipher.go`, ADR 0029); `transport/btp` is a third transport.
- **Correction:** "operational and fabric keys are stored in clear; ICD
  keys are sealed"; add `transport/btp`.
- **Confidence:** confirmed.

## Low — ADRs (Decision / Consequences as claims about the current code)

### W6-doc-24 — ADR 0016 and ADR 0026 state rules the code has moved past

- **Page:** `docs/adr/0016-device-type-validation.md` Decision ("`strict`
  refuses any new violation"); `docs/adr/0026-architecture-principles.md`
  principle 4 (`:76-78`, "three remain today") and principle 8 ("no second
  status type is introduced").
- **Code:** strict refuses only where an addition may have caused the
  violation (`endpoint/devicetype_validation.go:400-406`, `:505`; the enum
  doc at `:316` still says "any"; `docs/guides/ports.md:178-190` has the
  current rule); `clock/noraw_test.go` now fails on any raw time call;
  `contract.StatusError` (`contract/status.go:22`) is a second, host-facing
  status type, needed because `contract` is stdlib-only.
- **Correction:** amend the two ADRs with dated notes (as ADR 0008's
  header does) and fix the enum doc.
- **Confidence:** confirmed.

### W6-doc-25 — ADR 0001 §5 and ADR 0007 name code that is not in this module

- **Page:** `docs/adr/0001-matter-commissioning-bring-up.md:80-87`;
  `docs/adr/0007-chiptool-send-receive-matrix.md` Decision (`:46-89`).
- **Claim:** "`Endpoint.RootClusterServers` plus `Bridge.AttachRootClusters`
  are the explicit attach surface"; ADR 0007 decides a suite on
  `MockCCU.FireDeviceEvent` and `harness.AwaitProactiveReport` under
  `tests/chiptool/`.
- **Code:** no `RootClusterServers` exists (only `AttachRootClusters`);
  `MockCCU`, the harness and `tests/` are not in this module (the suite is
  `internal/chiptool`). ADR 0007's provenance note says only paths were
  rewritten.
- **Correction:** add a header note to both: superseded by the current
  API / the suite lives in the host.
- **Confidence:** confirmed.

---

## W6-doc-26 — note — the dependency bump after v0.15.6 has no `[Unreleased]` line

`git log v0.15.6..HEAD`: `769ea3c2` raises `modernc.org/sqlite` 1.60.1 →
1.61.0 (and `modernc.org/libc`, `go-humanize`, `go-strftime`) in `go.mod`,
which raises them in every consumer's build list. `[Unreleased]` is empty
(`CHANGELOG.md:11`). Earlier dependency bumps were not recorded either, so
this is a convention question, not a defect: decide whether requirement
bumps get a `### Changed` line.

## W6-doc-27 — note — `ClusterDataVersion` absent is "a constant 1" only on root and Aggregator

`docs/guides/ports.md:41`: "When absent: the IM layer stamps a constant 1".
On a bridged endpoint the dispatcher takes the version from the endpoint's
own state whatever the server implements (`endpoint/dispatcher.go:38-43`,
`:1330-1332`); the fallback to 0 → 1 applies to root and Aggregator
servers only (`:20-28`). Worth a clause.

---

## Checked and sound

- `CHANGELOG.md` 0.15.4, 0.15.5, 0.15.6 against `git diff` of each
  release: every user-visible change has an entry, every named identifier
  exists (`camera.ErrPayloadTooLarge`, `spec.CarriesStatus`/`StatusOf`,
  `bridge.ConnectPeer`/`PeerSession`/`ErrPayloadTooLarge`,
  `camera.PeerConnector`, `AttachPeerConnector`, the `ErrNoPeerConnector`,
  `ErrUnknownSession`, `ErrNoICECandidates` errors,
  `matter.case.initiator.resolving/resolved/resolve_failed`), the four
  `STRESS_FLAKES` tests are in the Makefile; no entry describes a change
  the diff lacks.
- `README.md` §API stability: the deprecation table is empty and no
  `Deprecated:` marker exists outside tests; the removed
  `controller/<name>` forwarders are gone; `make apidiff`/`apidiff-check`
  and their CHANGELOG rule match `script/apidiff`; `conformance/` has three
  exports, one taking `*testing.T`; six direct dependencies (index: five at
  run time plus `x/tools`) match `go.mod`.
- `docs/security.md`: PASE cap 20, 15 min lockout doubling to 4 h, reset
  on a new window, one handshake at a time with 60 s
  (`bridge/securechannel.go:504`, `:520`, `:543-544`, `:632-648`); the
  controller's in-memory resumption (`controller/controller.go:293`); the
  attestation zero policy (consistent with `controller.md:383-385`, Pair
  defaults to Reject at `controller/controller.go:628-629`).
- `docs/guides/concurrency.md`: every cadence and bound (50 ms ACK pump,
  250 ms tick, 1 min / 5 min / 30 s reapers, 1024 dispatch slots, 256
  per-session backlog, 200 ms standalone ACK) and the Stop order.
- `docs/guides/operations.md`, `docs/guides/limits.md` (also held by
  `TestLimitsPage`), `docs/guides/observability.md` (metric names,
  `--metrics-expvar`, 256-event ring), `docs/guides/icd.md` (timing
  defaults, 200 ms fast poll, 5 s LIT threshold, controller API),
  `docs/guides/host-view.md`, `docs/matter-parity-contract.md` (every
  guard file, job and Make target exists).
- `docs/getting-started/device.md` defaults table (`:5540`, 5 fabrics,
  `en-US`, RootNode 0x0016, 500/300/4000 ms, 30 s health, TLS maxima 5),
  stop order, `ErrStopped`, 5 s `Run` budget — apart from W6-doc-01/15/16.
- `docs/getting-started/controller.md` lines 1–669: `OpenDB` pragmas
  (WAL included), `DefaultPairTimeout`, 15 min discovery, 900 s window,
  1000 iterations / 32-byte salt, the nine-part stack.
- `docs/guides/testing-in-process.md` lines 1–305: `DefaultWait` 30 s,
  `DefaultSettle` 10 ms, node id 112233, `fe80::n`, TTLs.
- `mkdocs.yml`: every page in `docs/` but the excluded ones is in the nav,
  every nav target exists; all absolute `github.com/SukramJ/go-fabric`
  URLs and README/roadmap anchors resolve; no Make target, environment
  variable or CLI flag named in the pages is missing (but
  `sync-matter-schema`, W6-doc-02); no other Matter Go project is cited.
- Package docs of `device`, `contract`, `controller` (`controller.go`) and
  `bridge` (not re-read beyond W2). `secure` has no non-test file at its
  root and `client` no Go file, so neither has a package doc to check.
- ADR 0008 Decision (timeout 2 s, `im/subscription/persistence.go:21`; the
  scope note in its header covers `ConnectPeer`), ADR 0029 Decision and
  Consequences.

## Not read

- `docs/guides/camera.md`, `thermostat.md`, `sensing.md`,
  `thread-border-router.md`, `network-infrastructure.md` (but `:60-64`),
  `joint-fabric.md` (but `:44-81`), `ble-proxies.md` (flags only),
  `migrating-to-the-facade.md` (identifier script only; its removed names
  are expected): mechanical checks only.
- `docs/certifiability.md`, `docs/chip-datamodel-crosscheck.md`,
  `docs/reference/device-types.md`, `docs/reference/spec-levels.md`
  (the last three generated): mechanical checks only.
- `docs/getting-started/controller.md:670-749`,
  `docs/guides/advanced-seams.md:140-281`,
  `docs/guides/testing-in-process.md:306-408`, `docs/roadmap.md` and
  `docs/matterjs-comparison.md` beyond the lines cited.
- ADR Decision/Consequences sections other than 0001 §5, 0007, 0008, 0016,
  0026 (principles 1–9) and 0029: paths and identifiers checked by script,
  prose not read.
- `docs/concept-v7/8/9-*.md`: excluded from the site.
