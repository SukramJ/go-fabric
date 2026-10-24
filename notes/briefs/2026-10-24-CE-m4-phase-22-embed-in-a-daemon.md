# Brief CE — M4 phase 22: the cookbook "Embed in a daemon" against go-zendure2mqtt, and the operations page

Date 2026-10-24. Concept v7 (`docs/concept-v7-operation-and-packaging.md`
B.3, decisions 0.3 and 0.9; B.2 is **not** done here — decision 0.9 moved
the image, the systemd unit and the release matrix to the planned
`go-fabric-server`), the read map `notes/briefs/reads/read-m4-phase-21-22.md`
§4 (packaging today) and §5 (the daemon: `source.Device`, `source.Reading`,
`model.Report`, the `Source`/`Controller`/`Backend`/`Observer` interfaces,
`internal/source/source.go:24-110`, the coordinator — a map, verify
before acting), `docs/guides/host-view.md` (the embedding pattern as a
whole program; the cookbook is the same pattern inside a real daemon, do
not duplicate the page, link it), ADR 0025 (facade), ADR 0026 P1/P9.
Owner decision 2026-10-24 (wizard): the daemon is **go-zendure2mqtt**,
the work there is a branch and a pull request in the owner's local
checkout `/home/markus/Dokumente/GitHub/go-zendure2mqtt` (module
`github.com/SukramJ/go-zendure2mqtt`, `main` at v0.11.0 or later —
`git -C … fetch && git -C … log origin/main --oneline -1`), the Matter
projection is behind config and **off by default**. matter.js pin
`862ad75f`, read only via `git -C $MATTERJS_DIR show 862ad75f:<path>`;
cite lines. Start when the tag **`v0.10.8`** of go-fabric exists (the
daemon pins a tag, never a commit); go-fabric branch `m4/phase-22-ce`,
commits `m4 CE: <step>`; daemon branch `fabric-projection`, commits in
that repo's own convention (read its `CLAUDE.md`, `CONTRIBUTING.md`,
`Makefile`, commit log — conventional commits there). Two pull requests,
no auto-merge on either, no label here (no composition change in
go-fabric). English in every repo artefact, both repositories. Never add
attribution lines to commits or PR bodies. The owner merges both.

**Model:** Opus. **One agent, sequential:** first the daemon PR to a
state whose commits the guide can cite, then the guide here against
those commits, then the operations page. No security-path change in
go-fabric; no go-fabric code change at all unless §1 proves a gap in the
facade (then: its own commit, citation, CHANGELOG line, and the gap
named under Differences — a cookbook that needs a facade change has found
a facade defect, which is the point of the exercise). **Trademark rule
(owner 2026-10-23):** "Matter" appears only descriptively — in a config
key, a log line, a README sentence — never in a product, package or
binary name; the daemon's new package is `internal/fabric` (not
`internal/matter`), the config block is `MATTER_*` (descriptive, like the
repo topics).

## 1 The daemon side (go-zendure2mqtt, pull request there)

The proof of phase 22: "a 2mqtt daemon exposes one device type end to
end on the kit". One Zendure energy store becomes one bridged endpoint
a Matter controller (Apple Home, Home Assistant) shows with its charge
level and its power.

- **1.1 Dependency and config.** `go get github.com/SukramJ/go-fabric@v0.10.8`
  (the daemon's dependency rules: read its `THIRD-PARTY-NOTICES`/licence
  handling and follow it — go-fabric is Apache-2.0; its transitive
  dependencies are the six of go-fabric's `go.mod`). Config block in
  `internal/config` with that file's conventions (uppercase YAML keys,
  `defaults.go`, `validate.go`, tests): `MATTER_ENABLED` (bool, default
  false — nothing changes for an existing installation), `MATTER_LISTEN`
  (default `:5540`), `MATTER_STORE_PATH` (default next to the daemon's
  own state, the SQLite file go-fabric owns — read `node.Config.Store`
  and `docs/guides/persistence.md`), `MATTER_PASSCODE`,
  `MATTER_DISCRIMINATOR` (defaults: read what `examples/reference-bridge/main.go`
  and `node.Config.Commissioning` give and whether the facade generates
  them when zero — say which in the report), `MATTER_VENDOR_ID` /
  `MATTER_PRODUCT_ID` (the test vendor the reference bridge uses; the
  README sentence says the device is an uncertified test device — the
  wording of go-fabric's README §Not certified, verbatim in spirit),
  `MATTER_LABEL` (NodeLabel, default the daemon's name). Validation
  refuses an enabled block with an unusable listen address or store path
  the way the file refuses other fields.
- **1.2 The projection package `internal/fabric`.** One type, say
  `fabric.Bridge`, built from the config and the daemon's logger
  (`slog`; go-fabric's logger rule: pass the daemon's `*slog.Logger` as
  `node.Config.Logger`, ADR 0026 P9) with:
  - `node.New(cfg)` where `cfg` is a `node.Config` from the block
    (`Network.Listen`, `Commissioning`, `BasicInformation`/
    `ProductDescription` with vendor, product, the daemon's version as
    software version, `Store.Path`, `Logger`); nothing below the facade
    (`bridge`, `store`, `mdns`, `im` are not imported — the guide's
    first claim is that a daemon needs `node`, `device`, `contract`
    and the `<cluster>def` packages only; make the import list a test
    in the daemon, `TestFabricImportsOnlyTheFacade`, the pattern of
    go-fabric's `script/archcheck` in one function).
  - **The device type.** One Zendure device = one `device.ElectricalSensor`
    (`device/electrical_sensor_gen.go`: PowerTopology M,
    ElectricalPowerMeasurement O.a+, ElectricalEnergyMeasurement O.a+;
    matter.js `packages/node/src/endpoints/electrical-sensor.ts` at the
    pin) with `electricalpowermeasurementdef.Declare` (ActivePower from
    the report's home-output / input power in mW — read the report's
    property names in the daemon's `internal/catalog` and the unit the
    cluster encodes, `electrical-power-measurement.element.ts` at the
    pin) and `powersourcedef.Declare` for the battery (BatPercentRemaining
    from the state of charge in half-percent, BatChargeLevel, Status;
    `power-source.element.ts` at the pin, and go-fabric's
    `BD-Matter-PowerSource-OwnEndpointList` for what the kit fills in).
    Read how a device carries a PowerSource today: the measurement class
    already rides a battery as the device's PowerSource
    (`device/measurement.go:28-29`, `endpoint.Spec.PowerSource`,
    `contract.MeasurementBattery`; `device/device.go:407-460`
    `powerSource`, `powerSourceDefaults`, `ownEndpointList`). If that
    route takes a typed `powersourcedef.Declare` on an ElectricalSensor,
    use it. If it does not: matter.js lists PowerSource on the **Bridged
    Node** device type with conformance `BridgedPowerSourceInfo`
    (`bridged-node.element.ts:23` at `862ad75f` — a condition, not plain
    "O", so the Base rule's `Conformance.Text == "O"` filter does not
    cover it), and every endpoint the kit assembles under the Aggregator
    carries Bridged Node. The rule then belongs next to the Base rule in
    `device/base.go`: a declared PowerSource on a bridged device is the
    device stating the `BridgedPowerSourceInfo` condition (read how the
    validator reads stated conditions, `endpoint/devicetype_validation.go`
    `StatedConditions`, and state it there). A go-fabric commit of its
    own with the citation, a test on an OnOffLight declaring PowerSource
    through `fabrictest` (read → BatPercentRemaining, EndpointList names
    its own endpoint), CHANGELOG `### Added`, report Differences. Do not
    mount PowerSource on a second endpoint.
    PowerTopology: `NodeTopology` with no features is the smallest
    conforming choice (read `power-topology.element.ts` and the kit's
    defaults); say what the kit applies when the declaration is empty.
  - **The aggregator batch.** The daemon's `Source.Devices()` at start
    and the first reading per device → `n.Aggregator().Add(ctx, dev)`,
    then `MarkComplete()` (the host-view pattern, `examples/host-view/main.go:66-78`);
    a device that appears later → `Add`; one that vanishes → read
    whether the daemon ever removes devices (its `state` package) and
    mirror that (`Aggregator().Remove` or reachability only).
  - **Readings → attributes.** The daemon's `Handler(Reading)` (its
    `coordinator` fans readings out; attach the bridge as one more
    consumer the way the HA publisher is attached — read
    `cmd/zendure2mqtt/main.go:71-150` and `internal/coordinator`; do not
    fork the reading path). Each reading → `dev.ElectricalPowerMeasurement().Set(ctx, patch)`
    and `dev.PowerSource().Set(ctx, patch)` with only the attributes
    that changed (the kit reports; a controller's subscription sees it).
  - **Reachability.** `source.Observer.DeviceReachable(dev, reachable)` →
    `dev.SetReachable(ctx, reachable)`; `UpstreamUsable(false)` → every
    device unreachable (BridgedDeviceBasicInformation.Reachable; read
    `device.Device.SetReachable`).
  - **Lifecycle.** `Start(ctx)` with the daemon's run group, `Stop` on
    shutdown in the order the daemon stops its publisher; a failing
    `node.New` or `Start` with `MATTER_ENABLED=true` is a startup error
    (the daemon's convention), never a silent fallback.
  - **The pairing code.** `n.PairingCodes()` (QR payload and manual
    code) logged once at start (info) and published where the daemon
    publishes its own status — read `internal/web` (a status page?) and
    the HA discovery (`internal/hass`): the manual code as a diagnostic
    sensor entity and the QR payload as an attribute is the smallest
    useful route; if `internal/web` has a page, a line there. Say which.
  - **Writes.** None in this wave: the energy store's controllable
    properties (`Controller.Write`) stay on MQTT; the Matter endpoint is
    a sensor. Say so in the README and the guide (a later wave adds an
    output-limit control when a device type fits).
- **1.3 Tests in the daemon.** go-fabric's `fabrictest`: a site with
  the bridge under test and an in-process controller reads
  `BatPercentRemaining` after a synthetic reading, sees the change
  through a subscription after a second reading, and sees `Reachable`
  flip on `DeviceReachable(false)` (read `docs/guides/testing-in-process.md`
  for the three calls). The import test of 1.2. The config tests of 1.1.
  The daemon's own guards (`make ci` or whatever its Makefile names;
  read it) green; its CI runs on the PR.
- **1.4 Daemon docs.** README: one section "Matter controllers" (what is
  exposed, how to enable, the pairing code, the uncertified-test-device
  sentence, the link to go-fabric), the config reference rows, CHANGELOG
  entry in its convention. The PR body: what, why, the go-fabric tag,
  how it was tested (the fabrictest cases; a real pairing is the
  owner's manual step — list it as such).

## 2 The guide here: `docs/guides/embed-in-a-daemon.md`

In `mkdocs.yml` nav after "A host's view". The recipe in eight steps
(concept B.3), each step one short section with: what to do, the
facade symbols it uses (link to `host-view.md` where the pattern is
already explained line by line), and **the commit of the daemon PR
that does it** (`SukramJ/go-zendure2mqtt@<sha>` as an absolute GitHub
URL — the strict build rejects relative links; the PR's commits are
public once pushed, so push the daemon branch before writing the
links; after a rebase there the links move — do not rebase after the
guide cites them):

1. the dependency (tag, licence note);
2. `node.Config` from the daemon's config (the block, the defaults, the
   logger, the store path);
3. the device: one daemon entity as a typed declaration (the device
   type choice and why — the decision tree in two sentences: a sensor
   device type for measurements, PowerSource for a battery, Bridged
   Node's optional set);
4. the aggregator batch at the device run (`Add` … `MarkComplete`,
   late devices, vanished devices);
5. readings to attributes (`Set` with a patch; what the kit reports and
   persists; what a subscription sees);
6. lifecycle and reachability (`Start`/`Stop` order, `SetReachable`,
   upstream down);
7. the pairing code into the UI and MQTT;
8. running it (systemd/Docker in one paragraph that points to the
   daemon's own packaging and to `operations.md` — no unit or Dockerfile
   in go-fabric, decision 0.9).

Then "What you did not have to do" (no cluster ids, no TLV, no mDNS,
no session code — the list of packages the import test forbids) and
"What a controller sees" (the endpoint as Apple Home / Home Assistant
show it — described from `fabrictest` reads, not screenshots; a real
pairing is the owner's step and the guide says the proof is the daemon
PR's test plus that pairing).

## 3 The operations page: `docs/guides/operations.md` (decision 0.9)

In `mkdocs.yml` nav after Limits. What a host daemon minds when it
runs a go-fabric node — from the node's view, not a product's: the
files (the store, its path, backup while running — read
`docs/guides/persistence.md` and the SQLite journal mode the store
sets; what is lost without it: fabrics, i.e. the pairing), the ports
(UDP 5540 and TCP when enabled, IPv6 required — the reasons in one
sentence each, cite `node.Config.Network`), mDNS (what the node
advertises, avahi coexistence — read `mdns/` package doc and the
chip-tool harness notes in `internal/chiptool/doc.go` for the avahi
rule), the logs (the observability guide's keys, which events matter
in operation: pairing, fabric added/removed, subscription limits),
health (`node.Health`, what a probe asks), the limits page (one link
and the three values an operator meets first), the diagnostic trace
(`Node.DiagnosticEvents` for a failed pairing), factory reset (what
removes the fabrics and what does not), and upgrading (the schema
snapshot pin, CHANGELOG `Break` lines, the migration guide). Every
claim checkable against code with a file link; no paragraph about
containers or units beyond "the daemon's packaging decides; the node
needs these files, ports and capabilities".

## 4 Guards and report

go-fabric: `make ci`, `mkdocs build --strict`, `go test ./script/...`
(docscheck reads the new pages' citations), `make race` only if §1
produced a go-fabric code change (then once). Daemon: its own `make`
guards and CI. CHANGELOG here: `### Added` (the two guides; a facade
change if any). Report `notes/briefs/reports/CE.md`: **Daemon PR**
(URL, commits per recipe step, config block, device type decision with
citations, test list), **Guide**, **Operations page**, **Guards** with
HEAD per repo, **Differences**, **Decisions**, **Left out** (writes;
anything the facade could not do and how it was worked around),
**Questions**, `by_design` candidates (unlikely; a facade gap is a
finding, name it). Copy this brief to
`notes/briefs/2026-10-24-CE-m4-phase-22-embed-in-a-daemon.md` in the
first go-fabric commit.
