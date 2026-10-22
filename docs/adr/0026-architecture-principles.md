# ADR 0026 — Architecture principles of the module

- **Status**: Accepted (2026-10-20; owner decision of the same day)
- **Date**: 2026-10-20
- **Related**: [ADR 0025 — design principles of the facade](./0025-facade-design-principles.md),
  [ADR 0018 — the device layer and the node facade](./0018-device-layer-and-node-facade.md),
  [ADR 0020 — the commissioner and controller role](./0020-commissioner-and-controller-role.md),
  [ADR 0013 — generated cluster definitions](./0013-generated-cluster-definitions.md),
  `docs/architecture.md`, `docs/guides/concurrency.md`, `docs/guides/persistence.md`,
  `docs/matter-parity-contract.md`

## Context

ADR 0025 holds the host-facing surface to a standard. The owner set the
same standard for everything behind it on 2026-10-20: "It is not only
about the facade. The whole architecture must be excellent and coherent."
A facade is only as coherent as the layers it stands on; two concurrency
models, a seam that leaks, or generated and hand-written code under
different rules fail that bar even when every guard is green.

What the module has today, measured on `main` at `v0.9.0` with
`go list -deps` reduced to top-level directories: a layered graph with
no upward import between its areas except one — `binding` (device side)
imports `controller/exchange`, `controller/handshake`,
`controller/interaction` and `controller/peer`, the client stack the node
borrows from the controller role since phase 18d. Other edges the first
audit has to judge rather than accept: every cluster server imports `im`
(for the status errors and the fabric filter); `cluster/core` imports
`groups` while `groups` imports `cluster/spec` (a cycle at the directory
level, not at the package level, because `cluster/spec` is a model
package living under a server directory); `controller/client` imports
`device` and `endpoint`; `time.Now()` survives in three non-test places
(`transport/mrp`, `store`, `mdns`) beside the `clock` port that every
other package reads. None of these is wrong by itself; each is a
question the principles below answer, one way or the other, with the
reason written down.

## Decision

Twelve principles for every layer, each checkable; a rule a guard can
hold gets the guard (principle 11).

1. **Layers and direction.** The module is layered, bottom up:
   foundation (`clock`, `tlv`, `schema`, `parity`); transport
   (`transport/*`, `mdns`); security and storage (`secure/*`,
   `commissioning`, `store`, `bdx/*`, `diagevent`, `bootid`); model and
   protocol (`im/*`, `groups`, `cluster/spec/*`, `cluster/wire`,
   `contract`, `eligibility`, and the client machinery both roles share,
   `client/*` — exchanges, the CASE/PASE initiators, the client
   interaction, the peer set, discovery — moved there from `controller/`
   by brief BX4, 2026-10-21); servers (`cluster/*` except `spec` and
   `wire`, `binding`); composition of endpoints (`endpoint/*`,
   `device/*`); the two roles (`bridge`, `controller/*`); the facade
   (`node`); and test support, examples and scripts above everything. A
   package imports only its own layer or a lower one; the archcheck
   ratchet carries no layer exception since BX4 resolved `binding` →
   `controller/*` by the `client/*` move (principle 12). `cluster/spec/*` and
   `cluster/wire` are model packages under the servers' directory;
   archcheck assigns them to the model layer (the exact pattern wins) and
   they stay where they are: a move to `spec/` would rename 119 public
   packages for no host gain (decided at gate 1, 2026-10-20).
2. **One composition root per role.** `bridge` composes the device side
   and `node` is the facade over it; `controller/*` composes the
   controller role. No other package wires ports; a package that needs a
   collaborator receives it (constructor argument, `Config` field, or an
   `Attach…`/`Set…` call documented in the composition root's package
   doc), never reaches for a global.
3. **Generated code has one source and one generator.** The matter.js
   snapshot (`parity/schema.json`) is the source; `schema/`,
   `cluster/spec/*`, `device/*_gen.go` and `contract/devicetype_names_gen.go`
   are its outputs, each with a test that holds the committed file to the
   snapshot. Generated files are regenerated, never edited; a generated
   package is public API under the same rules as a hand-written one; the
   generator's template is reviewed as code.
4. **Time and randomness are injected.** Every package that waits, times
   out, retries or stamps reads the `clock` port; `time.Now()` and
   `time.After` in non-test code are defects (three remain today and are
   findings). Randomness comes from `crypto/rand` directly; a declared
   seam (a function field or parameter) exists only where a test needs
   determinism (changed at gate 1, 2026-10-20: the module reads
   `crypto/rand` in 33 files and has seams where SPAKE2, MRP, the group
   counters and the controller's jitter need them; one seam per package
   bought nothing).
5. **Every goroutine has an owner.** The owner starts it, hands it a
   context or a done channel, waits for it on stop, and stop is
   synchronous and idempotent. No goroutine outlives its owner;
   `fabrictest`'s quiet check (no work in progress once a site settled)
   is the proof, and a timer is a goroutine for this rule.
6. **Locks are few, named and ordered.** One mutex per type with a
   comment naming what it protects; where two types nest, the order is
   written at the outer one; a lock is never held while calling host
   code, a callback, or the network, and never across a `clock` wait
   (`docs/guides/concurrency.md` states this for servers; it holds
   everywhere). A command queue (`op`) that serialises one server's
   commands, writes and ticks and that the host never takes may span that
   server's own host call; a state mutex (`mu`) never does, and a host
   port's call path never waits on a lock the library holds across host
   code (gate 1, brief BX2, 2026-10-21). `make race` is a release gate
   because of this principle.
7. **Persistence is one seam.** `store.Store` (credentials, fabrics, ACL,
   groups, subscriptions, settings), `endpoint.Store` (endpoint numbers)
   and the controller's client store are the three interfaces; SQLite is
   the shipped implementation, and a second implementation (phase 21)
   proves the seam. Four file-based seams stand beside it, each behind
   one path a host configures (named at gate 1, 2026-10-20): the OTA
   download and applied-image directories (`node/ota.go`,
   `cluster/core/ota_requestor_query.go`), the DCL file store
   (`controller/dcl/store.go`), the controller's CA key file
   (`controller/ca/keyfile`) and the attestation chain files
   (`secure/attestation/files.go`). No package opens a file or keeps
   state across a restart by any other path — two residues named at
   gate 2 (2026-10-22) until phase 21 moves them behind a seam: the OTA
   requestor's downloaded image (`cluster/core/ota_requestor_query.go`)
   and the DCL snapshot fetch (`controller/dcl/snapshot/fetch.go`).
8. **Errors are values with one style.** A sentinel or typed error per
   condition, its message prefixed with the package name, wrapped with
   `%w` on the way up; a wire status (an Interaction Model status code) is
   carried by one typed error and mapped to the wire in one place (the
   dispatcher), so a server names the condition and never formats the
   status itself. A server spells its status through `im` (`spec.Errorf`,
   `im.UnsupportedCommandf`): `im` is the model-and-protocol layer below
   the servers, the edge is downward, and no second status type is
   introduced (decided at gate 2, 2026-10-22).
9. **Observability is `slog` with injected loggers.** Every component
   takes a `*slog.Logger` (nil = `slog.Default()`), events are named
   `area.event` in dotted lower case (`ota.provider.apply_version_adjusted`),
   attributes are typed (`slog.Uint64`, never a formatted string of a
   number); nothing prints. A metrics hook and the diagnostic event ring
   are phase 21 and follow the same rule.
10. **Tests are part of the architecture.** A parity test proves a port
    line by line against the matter.js test at the pin; a unit test
    proves a package's contract; `fabrictest` proves behaviour across
    packages without hardware; chip-tool and the families prove the
    device side against the reference tool. A change ships with the test
    that would catch its regression, and a guard that is a ratchet
    (coverage floors, reachability, test parity, citations, archcheck)
    moves only in the commit that explains why.
11. **A structural rule is a guard.** Principle 1 is held by
    `script/archcheck` (layers and allowed direction as data, exceptions
    as a ratchet with a reason each); generated code by the snapshot
    tests; citations by `cite-check`; the public API by `apidiff` (ADR
    0025). A principle without a guard is named as such here and checked
    at the architecture gates until it has one (today: 2, 5, 6, 8, 9).
12. **The two roles share only role-neutral packages.** What both roles
    need (the CASE initiator, exchanges, the client interaction, the peer
    set) lives under a name that says what it is, not under the role
    that first needed it; `binding` and `node` importing `controller/*`
    is the measure of what still has to move. ADR 0020's rule — nothing
    outside `controller/` changes for the controller role until a stage
    lands — stays; this principle is about where shared code lives, not
    about scope.
    The controller's reading of a peer's device types and composition
    (`controller/client/endpoint.go`, `conformance.go`) uses `device.Type`
    and `endpoint.DeviceTypeTree`: role-neutral by content, in `endpoint`
    by history, a downward edge in the layer order; milestone 5 moves the
    validator to a role-neutral package when it touches that reading
    (gate 2, 2026-10-22).

**Gates.** The architecture review at each gate of phase 20 (ADR 0025)
audits the whole tree against these twelve principles, one section per
principle, a finding per violation with its package and line; findings
are fixed in their own wave before the next facade wave. The first
audit's known questions are the edges named in the context above.

## Consequences

- `script/archcheck` is built as a Go test (so `go test ./...` and the
  CI test job run it without a new required check), with
  `script/archcheck/layers.json` as the rule file and an exception list
  that may only shrink.
- The first audit (`notes/briefs/reports/G1.md`, 2026-10-20) judged the
  questions above: the two remaining raw `time.Now` sites are kernel
  socket deadlines and stay; the `im` import of every server is the
  status vocabulary and holds; `cluster/spec` stays; the client stack
  moves to a role-neutral `client/` tree (wave BX4); `controller/client`'s
  imports of `device` and `endpoint` are read in that wave.
- A principle that proves wrong is changed in this ADR with the date and
  the reason, never worked around in a wave.
