# go-fabric

`go-fabric` is a pure-Go implementation of the Matter protocol, packaged as
a library a host application embeds. On the **device side** it brings the
TLV codec, the Interaction Model, PASE and CASE session establishment, MRP
over IPv6 UDP and TCP, DNS-SD advertisement, the cluster servers a bridge
needs (including the media clusters, the OTA requestor and provider, and
DiagnosticLogs over BDX), and the endpoint assembler that turns a host's own
device list into a Matter topology. The host keeps its device model; this
module owns the wire format and hands back the port contracts (`contract/`)
a device implements to appear as a bridged endpoint. On the **controller
side** (`controller/`, [ADR 0020](./docs/adr/0020-commissioner-and-controller-role.md))
it is a full commissioner: discovery, attestation (with the CSA Distributed
Compliance Ledger, opt-in), NOC issuance, the client interaction and a typed
image of a remote node, bindings and the group sender —
`examples/reference-controller` drives it from the command line. It is a
semantic port of [matter.js](https://github.com/matter-js/matter.js)
— cluster IDs, revisions, attribute IDs, constraints and wire shape are
mirrored from matter.js HEAD rather than transcribed from the specification,
and the extracted matter.js element model ships embedded so parity is a test,
not a claim.

## Status

As of `v0.8.7` (2026-10-20); the numbers are [the roadmap's baseline](./docs/roadmap.md#baseline-2026-10-17-v060).

| Area | State |
| --- | --- |
| Device side | 118 of 135 clusters served, 79 of 91 device types generated ([comparison](./docs/matterjs-comparison.md), [roadmap](./docs/roadmap.md)) |
| Controller side | full commissioner (BLE, Wi-Fi and Thread network steps, keys behind `crypto.Signer`, the embedded DCL snapshot), controller role stages 1–6b; Joint Fabric deferred ([comparison](./docs/matterjs-comparison.md)) |
| BLE | controller: done with [milestone 1](./docs/roadmap.md#milestone-1-the-commissioner-in-the-field) — `ble/bluez` on Linux, `pair --ble`, proven against a matter.js node over virtual controllers; device: [milestone 7](./docs/roadmap.md#milestone-7-ble-on-the-device-side) |
| Thread | dataset provisioning only, by the commissioner ([milestone 1](./docs/roadmap.md#milestone-1-the-commissioner-in-the-field), phase 12); no Thread radio ([comparison](./docs/matterjs-comparison.md)) |
| ICD | [milestone 5](./docs/roadmap.md#milestone-5-icd-and-the-controller-remainder) |
| Certification | not pursued; certifiability is a goal ([certifiability](./docs/certifiability.md)) |
| Verified ecosystems | Apple Home, Home Assistant; Google Home and Alexa pending ([roadmap](./docs/roadmap.md#baseline-2026-10-17-v060)) |

## Not certified

This is an independent open-source implementation. It is **not certified by
the Connectivity Standards Alliance**, and it is not a project of the
Alliance. No warranty or assurance is made about rights that may be required
to implement it. Using it does not certify anything: it does not assure
compliance with the Matter specification, and it does not convey any right to
describe a device, product or service built with it as Matter compliant,
certified or similar. Certification requires membership in the Alliance and
compliance with Alliance policy; so does any use of Alliance trademarks and
logos. See [`licenses/NOTICE-matter.js.txt`](./licenses/NOTICE-matter.js.txt),
reproduced from matter.js, which states the same terms for the project this
one is ported from.

Certification is not pursued by this project. Certifiability is a goal: the
module is meant to let a product built on it pass certification, and the CSA
certification test families run against the reference daemon to hold it
there. What passes, what does not and why, and what a product owner must
supply that a library cannot is in
[`docs/certifiability.md`](./docs/certifiability.md)
([ADR 0011](./docs/adr/0011-certifiability-is-a-goal.md)).

## Documentation

The site (rendered from `docs/`) is at
<https://sukramj.github.io/go-fabric/>; the API reference is
[pkg.go.dev](https://pkg.go.dev/github.com/SukramJ/go-fabric).

| | |
| --- | --- |
| [`docs/index.md`](./docs/index.md) | Where to start, by role: embedding the device side, driving the controller, evaluating the architecture. |
| [`docs/architecture.md`](./docs/architecture.md) | The two roles, the layers, the request path, the package map and the scope boundaries that are decisions rather than backlog. |
| [`docs/getting-started/`](./docs/getting-started) | A minimal device host and a minimal controller, each a compiling example. |
| [`docs/guides/`](./docs/guides) | Host ports, concurrency, persistence. |
| [`docs/security.md`](./docs/security.md) | The security model of both roles. |
| [`docs/matterjs-comparison.md`](./docs/matterjs-comparison.md) | Feature-by-feature against matter.js, with a verdict on whether each gap is worth closing here. |
| [`docs/certifiability.md`](./docs/certifiability.md) | What the CSA certification families say about this module, family by family. |
| [`docs/matter-parity-contract.md`](./docs/matter-parity-contract.md) | What parity means and which standing guards enforce it. Read before your first change. |
| [`docs/matter-ecosystem-observations.md`](./docs/matter-ecosystem-observations.md) | What Apple Home, Google Home and Alexa were actually observed to do. |
| [`docs/adr/`](./docs/adr) | Architecture decision records. |
| [`docs/history/`](./docs/history) | Earlier planning concepts (German), kept for their reasoning. |
| [`notes/parity/by_design.md`](./notes/parity/by_design.md) | The catalogue of intentional divergences from matter.js. |
| [`notes/parity/matter_behaviour_findings.md`](./notes/parity/matter_behaviour_findings.md) | Known behaviour gaps that are *not* intentional, as scoped fix packages. |

## Examples

[`examples/reference-bridge`](./examples/reference-bridge) is the reference
daemon: a bridge on this module, the binary the chip-tool suite runs
against. [`examples/reference-controller`](./examples/reference-controller)
is the controller role as a command-line tool — pair a node by its QR or
manual code, read, write, invoke, subscribe, provision groups and bindings,
serve an OTA image — with its fabric kept in one SQLite file; its
[README](./examples/reference-controller/README.md) lists every command.

## API stability

The module carries its own SemVer lane, independent of any host that embeds
it.

### What "public API" means here

**Every exported identifier outside `internal/` is public API.** The module
already draws that line for itself: `internal/` holds the two one-way seams
through which the test-support packages reach package internals
(`bridgeseam`, `channelseam`) and the chip-tool suite, and Go makes those
unimportable from outside. Nothing else is private by convention, so nothing
else may be changed as if it were.

Depend freely on `bdx/`, `binding/`, `bootid/`, `bridge/`, `cluster/…`,
`commissioning/`, `contract/`, `controller/…`, `device/`, `diagevent/`,
`eligibility/`, `endpoint/` (including `endpoint/sqlitestore`), `groups/`,
`im/`, `im/subscription`, `mdns/`, `node/`,
`parity/`, `schema/`, `secure/…`, `store/`, `tlv/` and `transport/…`. The
device layer `device/` and the node facade `node/` are public API since
`v0.3.0`; that release freezes nothing, and the `v1.0.0` freeze is still
ahead.

`bridge/bridgetest`, `endpoint/endpointtest`, `controller/ble/bletest`,
`clock/clocktest` and `fabrictest` are public API on the same terms as the
rest, not a lower tier. They exist for the reason
`net/http/httptest` exists — a consumer's tests need to place a bridge in a
state the production API is not shaped to reach — and a consumer's test suite
is as expensive to break as its production code.

Three things outside `internal/` are still not API. `conformance/` is this
module's own suite, shaped for the tests in this repository rather than for a
consumer -- one of its three exported identifiers takes a `*testing.T`, and the
other two exist to serve it. `examples/reference-bridge`,
`examples/reference-controller` and everything under `script/` are
`package main` — runnable, not importable. Copy from them; do not build
against them.

One value-level caveat on `parity/` and `schema/`: their Go surface is
covered by everything below, but the data behind it is a matter.js HEAD
extract. Cluster revisions, attribute IDs and constraint values move when
upstream corrects them. That is a behaviour change, not a signature change,
and it is announced the same way — see the exception under
[Pre-1.0](#pre-10).

### The deprecation window

An identifier on its way out is marked, kept working, and only then removed.

1. **Marked.** Its doc comment gains a paragraph beginning `Deprecated: `,
   naming the replacement and the release the removal becomes permissible in.
   That exact prefix is what `gopls`, `staticcheck` (SA1019) and the Go
   documentation renderer key on, so a consumer sees it in an editor without
   reading a changelog.
2. **Kept.** A deprecated identifier keeps its exact behaviour for the whole
   window. It is not degraded, does not start logging, and does not become a
   no-op. A rename lands the new name first and leaves the old one delegating
   to it.
3. **Removed** no earlier than the **second minor release after the one that
   marked it**. Deprecated in `v0.4.0` means removable in `v0.6.0`, not
   before. Consumers who upgrade minor-by-minor therefore see at least one
   release in which their code still compiles and the warning is already
   visible.

Currently deprecated, each with its replacement — all marked when the time
port `clock` landed (`[Unreleased]` in the changelog), removable in
`v0.10.0` at the earliest:

| Deprecated | Replacement |
| --- | --- |
| `transport/btp.Clock`, `btp.Timer`, `btp.SystemClock`, `btp.Options.Clock` | `btp.Options.TimeSource` (a `clock.Clock`), `clock.System` |
| `cluster/onoff.Clock`, `onoff.Config.Clock` | `onoff.Config.TimeSource` |
| `cluster/core.OtaClock`, `core.OTARequestorConfig.Clock` | `core.OTARequestorConfig.TimeSource` |
| `cluster/core.OTAProviderConfig.Now` | `core.OTAProviderConfig.Clock` |
| `cluster/core.CommissionerControlConfig.Now` | `core.CommissionerControlConfig.Clock` |
| `cluster.Quieter.Now`, `cluster.Quieter.AfterFunc` | `cluster.Quieter.Clock` |
| `cluster/closure.DimensionConfig.Now` | `closure.DimensionConfig.Clock` |
| `cluster/energy.CommodityTariffConfig.Now`, `energy.DeviceEnergyConfig.Now`, `energy.EvseConfig.Now` | the `Clock` field beside each |
| `secure/mattercert.SystemTime` | `clock.System` (the `TimeSource` parameter stays: a `clock.Clock` satisfies it) |
| `controller/ca.Config.Now` | `ca.Config.Clock` |
| `controller/ble.ErrScannerClosed` (marked by brief BH) | none: a discovery on a closed scanner returns the stored devices without an error |
| `cluster/wire.GenericSwitch`, `wire.NewGenericSwitch` (marked by brief BK) | `cluster/switch` (`switchserver.New`), which the MomentarySwitch materialiser builds |

A patch release (`v0.x.y`) never removes or renames a public identifier and
never changes a signature. Deprecations may be *marked* in a patch release;
they are only ever *acted on* in a minor one.

### Pre-1.0

The module is at `v0`; `v0.1.0` is its first tag. Before it, `main` was the
only consumable version and could break in any commit, which is why consumers
tracked a pseudo-version. `main` is still consumable the same way — see
`CONTRIBUTING.md` — but a tagged version is what a consumer should pin.

SemVer permits a `v0` module to break on any minor bump. This module does not use that permission as a default. The window
above applies at `v0` exactly as it would at `v1`: a breaking change is a
minor bump, never a patch, and wherever the old identifier can be kept
working it is kept working for the full window.

`v0.3.0` is the first release to act on the window: it removes the
identifiers `v0.2.0` marked for removal in it, each listed with its
replacement under `### Removed` in [`CHANGELOG.md`](./CHANGELOG.md#030--2026-10-12).

Two changes bypass the window, and both are named as such in their entry:

- **A matter.js parity correction.** matter.js HEAD is the gold standard for
  every cluster ID, revision, attribute ID and wire shape in this module.
  When upstream corrects one, this module follows it in the next release
  without a window. Holding a value stable *because it is stable* would mean
  knowingly shipping a wrong one, and the failure it produces is a silent
  pairing abort in a controller nobody can attach a debugger to.
- **A security fix that cannot be made without an API change.** It ships in
  the next release, marked `### Security`.

What `v0` does *not* excuse: an unannounced break. Every one of these lands
in the changelog before a consumer meets it.

### How a consumer learns about a break

[`CHANGELOG.md`](./CHANGELOG.md) is the single place. It follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), so the headings
already carry the meaning:

- `### Deprecated` — the release that marks an identifier. The entry gives
  the full import path and name, the replacement, and the earliest release
  the removal may happen in.
- `### Removed` — the release that removes it. The entry repeats the
  replacement, so a consumer who skipped the deprecation release still finds
  it here rather than in a diff.
- `### Changed` — behaviour that moved under an unchanged signature. A parity
  correction lands here and says which matter.js element changed.
- `### Security` — a fix that forced an API change out of band.

A release note carries, for the version it names: every entry from those four
headings, and an explicit statement of whether upgrading requires a source
change. "No source change required" is a claim worth making when it is true,
because it is the only sentence most consumers need to read.

The rule behind all of the above: **a break that appears only in a commit
message, a doc comment or a diff has not been announced.** If it is not in
the changelog, it does not ship.

## Licence

Apache License 2.0 — see [`LICENSE`](./LICENSE) and [`NOTICE`](./NOTICE)
([ADR 0023](./docs/adr/0023-apache-2-and-the-cla.md): the same licence as
matter.js and connectedhomeip, with their patent grant; `v0.4.0` and earlier
were released under MIT and stay so). Upstream notices and the dependency
licences are recorded in
[`THIRD-PARTY-NOTICES.md`](./THIRD-PARTY-NOTICES.md). Contributions need
the contributor licence agreement in [`CLA.md`](./CLA.md), agreed once per
account in the first pull request.
