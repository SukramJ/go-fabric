# go-fabric

`go-fabric` is a pure-Go implementation of the device side of the Matter
protocol, packaged as a library a host application embeds: TLV codec,
Interaction Model, PASE and CASE session establishment, MRP over IPv6 UDP,
DNS-SD advertisement, the cluster servers a bridge needs, and the endpoint
assembler that turns a host's own device list into a Matter topology. The
host keeps its device model; this module owns the wire format and hands back
the port contracts (`contract/`) a device implements to appear as a bridged
endpoint. It is a semantic port of [matter.js](https://github.com/matter-js/matter.js)
— cluster IDs, revisions, attribute IDs, constraints and wire shape are
mirrored from matter.js HEAD rather than transcribed from the specification,
and the extracted matter.js element model ships embedded so parity is a test,
not a claim.

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

## Documentation

| | |
| --- | --- |
| [`docs/feature-scope.md`](./docs/feature-scope.md) | What this module does today, package by package — and the scope boundaries that are decisions rather than backlog. |
| [`docs/matterjs-comparison.md`](./docs/matterjs-comparison.md) | Feature-by-feature against matter.js, with a verdict on whether each gap is worth closing here. |
| [`docs/matter-parity-contract.md`](./docs/matter-parity-contract.md) | What parity means and which standing guards enforce it. Read before your first change. |
| [`docs/matter-ecosystem-observations.md`](./docs/matter-ecosystem-observations.md) | What Apple Home, Google Home and Alexa were actually observed to do. |
| [`docs/adr/`](./docs/adr) | Architecture decision records. |
| [`notes/parity/by_design.md`](./notes/parity/by_design.md) | The catalogue of intentional divergences from matter.js. |
| [`notes/parity/matter_behaviour_findings.md`](./notes/parity/matter_behaviour_findings.md) | Known behaviour gaps that are *not* intentional, as scoped fix packages. |
| [`notes/reference/matter-conformance.md`](./notes/reference/matter-conformance.md) | Which tests run, when, and what gates on them. |
| [`notes/audits/matter-threat-model.md`](./notes/audits/matter-threat-model.md) | Threat model for the responder role. |

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

Depend freely on `bootid/`, `bridge/`, `cluster/…`, `commissioning/`,
`contract/`, `diagevent/`, `eligibility/`, `endpoint/` (including
`endpoint/sqlitestore`), `im/`, `im/subscription`, `mdns/`, `parity/`,
`schema/`, `secure/…`, `store/`, `tlv/` and `transport/…`.

`bridge/bridgetest` and `endpoint/endpointtest` are public API on the same
terms as the rest, not a lower tier. They exist for the reason
`net/http/httptest` exists — a consumer's tests need to place a bridge in a
state the production API is not shaped to reach — and a consumer's test suite
is as expensive to break as its production code.

Three things outside `internal/` are still not API. `conformance/` is this
module's own suite, shaped for the tests in this repository rather than for a
consumer -- one of its three exported identifiers takes a `*testing.T`, and the
other two exist to serve it. `examples/reference-bridge` and everything
under `script/` are `package main` — runnable, not importable. Copy from
them; do not build against them.

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

A patch release (`v0.x.y`) never removes or renames a public identifier and
never changes a signature. Deprecations may be *marked* in a patch release;
they are only ever *acted on* in a minor one.

### Pre-1.0

The module is at `v0` and **nothing is tagged yet**, so this section is
honest about two different states.

Until `v0.1.0` is tagged, `main` is the only consumable version and it can
break in any commit. Consumers track a pseudo-version; `CONTRIBUTING.md` says
to treat `main` accordingly, and that stands.

From the first tag onwards, SemVer permits a `v0` module to break on any
minor bump. This module does not use that permission as a default. The window
above applies at `v0` exactly as it would at `v1`: a breaking change is a
minor bump, never a patch, and wherever the old identifier can be kept
working it is kept working for the full window.

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

MIT — see [`LICENSE`](./LICENSE). Upstream notices and the dependency
licences are recorded in
[`THIRD-PARTY-NOTICES.md`](./THIRD-PARTY-NOTICES.md).
