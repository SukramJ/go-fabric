# Brief CL — go-fabric v0.12.3: the findings of go-fabric-server S3 (typed JSON route for every cluster, first-match discovery, JSON escaping, a device from a bare address)

Date 2026-10-25. go-fabric `main` after CK (#270) merged and `v0.12.2`
tagged. Source: go-fabric-server `docs/reports/S3.md` §Questions Q20, Q21,
Q23, Q24 (local `/home/markus/Dokumente/GitHub/go-fabric-server`, tag
`v0.3.0`; Q22 is closed by `ble/bluez/v0.12.1`), ADR 0013 (generated
cluster definitions; its skip list is about *servers*), ADR 0025, the
comparison page's scope rows. Branch `m5/server-findings`, commits
`m5 CL: <step>`, one PR, no auto-merge, no label. Version **`v0.12.3`**:
additions only — the `MarkComplete` removal the owner decided on
2026-10-25 is a Break and rides with milestone 5 phase 25's `v0.13.0`
(brief CM), not here. English. Never add attribution lines. The owner
merges. **Model:** Opus. **Owner paths** untouched (`secure/*`,
`commissioning/*`, `client/handshake`, ACL, the four `cluster/core`
servers ADR 0013 skips: this brief generates *definitions* for three of
their clusters, it does not touch the servers). matter.js only via
`git -C /home/markus/Dokumente/GitHub/matter.js show 862ad75f:<path>`;
doc comments cite it; fabrictest or unit tests; CHANGELOG lines; docs
entries. go-fabric's `CLAUDE.md` token rules.

## 1 Definitions for every cluster of the snapshot (S3 Q20)

> **Amended 2026-10-25 (owner):** the 13 non-core clusters (camera, Thread, Joint Fabric, TLS) go through the generator in the standard form — definition **and** generated server, as matter.js serves them with its generated behaviour stubs (~15 lines each at `862ad75f`); `WebRtcTransportRequestorServer.ts` (240 lines) is ported as a self-held server. Definition-only applies to AccessControl, OperationalCredentials and GroupKeyManagement alone. Comparison rows and ADR 0013 follow; the message to the agent has the details.

`script/clustergen/clusters.go:158-161` leaves out AccessControl,
OperationalCredentials, GroupKeyManagement (hand-written servers), the
camera clusters, Joint Fabric, TLS and Thread (out of scope as *servers*).
The controller's JSON route (`client.EncodeJSON/DecodeJSON`,
`client.AttributeField/CommandField/ResponseField/EventField`) types
only clusters with a `cluster/spec` definition, so `set_acl_entry`,
`get_matter_fabrics`, `remove_matter_fabric` and Thread diagnostics stay
untyped for a JSON consumer. Decision (owner's goal: full 1.6.1 where
matter.js has it): the generator emits a **definition-only** package for
every snapshot cluster that has none — types, attribute/command/event
tables, `StateFrom`, no server, no `Declare` — behind a generator mode
(`definitionOnly` list beside the existing one, documented in ADR 0013
as an amendment: "a definition exists for every cluster; a server only
where the comparison says so"). The hand-written core servers keep
their own code and gain nothing but a parity test that their wire
constants equal the generated definition's (one table test per cluster,
in the generated package's test, reading the server's exported
constants — not the other way round). `script/clustergen` staleness
test covers the new list; `make generate-matter-schema` regenerates.
The JSON route then types every cluster; a test reads an
AccessControl ACL entry through `DecodeJSON` and writes one through
`EncodeJSON` on a fabrictest site (the ACL *server* is the existing one).
Comparison page: the scope rows say "definition yes, server no" where
that is the state.

## 2 First-match discovery (S3 Q21)

`Controller.Discover(ctx, id, timeout)` (`controller/code.go:105`)
returns only when the timeout passes; `Controller.Pair` takes the first
device that answers. Add `DiscoverOptions{First bool}` to a new
`Controller.DiscoverWith(ctx, id, timeout, opts)` (or make `Discover`
variadic in options — pick the shape ADR 0025 prefers and say why) that
returns on the first matching answer, as matter.js's `commissionNode`
stops at the first (`packages/matter.js/src/CommissioningController.ts:462-500`
at `862ad75f`); the reference CLI's `discover --first` flag uses it.
Test on fabrictest with two advertised devices.

## 3 JSON without HTML escaping (S3 Q23)

`controller/client/json.go:171, :323` use `json.Marshal`, which escapes
`<`, `>`, `&`. Use an encoder with `SetEscapeHTML(false)` (and no trailing
newline) so the bytes equal what matterjs-server's JSON writes; a test
with a string attribute containing the three characters; fixtures in
`im/testdata` or `controller/client/testdata` if any JSON golden exists.

## 4 A device from a bare address (S3 Q24)

Document and test that a `client.Device` (`client/discovery/scanner.go:132`)
built from an address alone, without a DNS-SD answer, is a supported
input of `Registry.ForDevice` (`controller/client/addresses.go:151`) and
of `Controller.Pair` through a `PairOptions.Address` (net.UDPAddr) that
skips discovery: matterjs-server dials `ip_addr` first
(`ControllerCommandHandler.ts:2052-2095` in matterjs-server; the matter.js
counterpart is `CommissioningController.ts`'s `knownAddress` path — find
and cite it). fabrictest: pair by address with no advertisement running.

## 5 Docs, tests, report

CHANGELOG under [Unreleased]; controller guide, `docs/guides/ports.md`,
ADR 0013 amendment, comparison rows; guards: `make ci`, `make race`
twice, `make reachability-check`, `make cite-check MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`,
`make apidiff-check`, `go test ./script/...` (clustergen, devicegen),
`mkdocs build --strict`, `make stress-flakes`; `make chipdm-check` is
not needed (snapshot unchanged) — say so; chip-tool only if the
reference bridge's composition moved (it should not). Known load flakes
to rerun alone: TestTypedReadsConsumer, TestIcdClientParity. Background
runs with a summary log; `df -h /` before, `go clean -cache -testcache`
after. Report `notes/briefs/reports/CL.md` (PR body): per § with commit
hashes; guards with HEAD; differences; decisions; left out; questions;
`by_design` candidates with citations. Copy this brief to
`notes/briefs/2026-10-25-CL-server-findings.md` in the first commit.
Hand back with the PR number, HEAD, and the Questions and by_design
sections verbatim. If a permission is denied, say which and stop that
step; never ask the launcher to perform it.
