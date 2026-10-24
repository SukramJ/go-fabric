# ADR 0025 — Design principles of the facade

- **Status**: Accepted (2026-10-20; concept v6, owner decisions of the same day)
- **Date**: 2026-10-20
- **Related**: [ADR 0018 — the device layer and the node facade](./0018-device-layer-and-node-facade.md),
  [ADR 0016 — device type validation](./0016-device-type-validation.md),
  [ADR 0014 — transitions optional per endpoint](./0014-transitions-optional-per-endpoint.md),
  [concept v6](https://github.com/SukramJ/go-fabric/blob/main/docs/history/concept-v6-embedding-layer.md),
  `README.md` §API stability, `docs/guides/concurrency.md`,
  `notes/parity/by_design.md` (`BD-Matter-Node-ObserversSynchronous`)

## Context

Milestone 4 of the roadmap makes a claim about embedding, not about Matter
semantics: ahead of matter.js in embedding and operation, equal in
semantics. The semantics have a gold standard (matter.js at the pin) and
guards that hold it. The embedding has had none: ADR 0018 named the three
packages (`node/`, `device/`, `contract/`) and a target shape, `README.md`
the deprecation window, `CLAUDE.md` the stdlib-only rule for `contract/`,
and the rest lived in reviews. Phase 20 builds the facade out (the routes
for every symbol a host reaches below `node/`, the generated typed cluster
layer, the declaration form of the device kit, the apidiff guard), and the
owner set the standard it is built to on 2026-10-20:

> The facade is the developer's view of go-fabric. Everything that happens
> behind it must work, but the facade is the representation to the
> outside. Convenience is no standard: the internal code is held to the
> same architectural and stylistic bar.

This ADR writes that standard down as principles a review can check, so
that a wave is judged against them before it merges, and an architecture
review at the two gates of phase 20 (after the facade and typed-layer
waves, before the declaration; and before `v0.10.0`) has a list to judge
by. matter.js is not the reference here: its host surface is behaviors,
agents and transactions, which this module deliberately does not ask of a
host (`BD-Matter-Node-ObserversSynchronous` and the behaviour-layer row of
the comparison record the idiom translation).

## Decision

The facade is `node/`, `device/`, `contract/`, the generated typed cluster
packages under `cluster/spec/<cluster>/` as reached through `device/`, and
on the controller side `controller/` (every public surface a host program
reaches; the controller's is reviewed against this ADR in milestone 5, the
second gate of phase 20 already includes it). Twelve principles, each
checkable:

1. **The facade is the product.** A facade change is judged as API design,
   not as plumbing: what a host reads in `go doc ./node ./device
   ./contract` is the deliverable, and the docs start there.
2. **One shape.** One typed hull for every cluster server (`Get`, `Set`,
   `OnChanged`, typed command handlers, typed events), whatever server
   stands behind it; one declaration form for a device, in code and as
   data; one lifecycle (add, reachable, rename, replace, remove); one
   error style. A second form for a special case is a defect, not an
   option.
3. **No leaked types.** Every type in a facade signature belongs to the
   facade: its own type, or an alias the facade owns and documents. A
   lower package's type reaches a host only through the advanced seams.
4. **Advanced seams are not the facade.** What stays public below the
   facade (`bridge`, `endpoint`, `store`, `mdns`, `groups`, `secure/*`,
   `im/subscription`, `diagevent`) is listed in
   `docs/guides/advanced-seams.md` with what it gives, when a host needs
   it and what it costs. It is public API under the same deprecation
   window as the facade (owner, 2026-10-20) — without the product claim:
   the docs lead to the facade first.
5. **Vocabulary.** What the Matter specification names keeps its name
   (cluster, attribute, command, event, fabric, endpoint, device type,
   commissioning window); what the host decides is named from the host's
   side (device, name, reachable, stable key). matter.js's internal terms
   (behavior, agent, transaction, backing) do not appear in the facade
   (owner, 2026-10-20). A host reads the facade without the matter.js
   source; `// Mirrors` comments are for maintainers.
6. **Semantics from matter.js, embedding from these principles.** A facade
   decision never changes what is on the wire; where a convenient facade
   would, the wire wins and the divergence is a `by_design` entry with
   its citation (the parity contract is unchanged by this ADR).
7. **Errors are values.** Sentinel errors compared with `errors.Is`,
   typed errors for structured outcomes (`ConformanceError` and its
   violations), wrapped with the operation that failed; never a string a
   host must parse, never a status code a host must know Matter to read.
8. **Callbacks run inline, with an asynchronous option.** Observers,
   `OnChanged` and handlers run on the event's goroutine, before the
   response, as matter.js emits them and as `docs/guides/concurrency.md`
   documents; a host that may block chooses the queued variant the facade
   offers (`…Async`) rather than the facade guessing (owner, 2026-10-20).
   The rule is one sentence in the docs and the same for every callback.
9. **Every break is announced before it ships.** The apidiff guard
   compares the public API with the last tag and fails unless
   `CHANGELOG.md` `[Unreleased]` names the identifier under `Changed`,
   `Removed` or `Deprecated` (or names the entry as a parity correction or
   a security fix, the two bypasses of `README.md`); the deprecation
   window of `README.md` applies as written.
10. **The same bar inside.** The code behind the facade — the generator
    templates, the hulls, the routes, the mounting — is held to the same
    architectural and stylistic standard. "It was simpler" is not a
    reason for a second form, a leaked type or a shortcut; a clean form
    that needs a small refactor gets the refactor, named in the report.
11. **Provable without hardware.** Every facade feature has a `fabrictest`
    proof (an in-process controller commissions a node of the facade and
    observes it), and a host can test its own device mapping the same
    way; chip-tool and the certification families remain the device-side
    guard, not the facade's only proof.
12. **Few dependencies, one binary.** The module builds with
    `CGO_ENABLED=0` into a static binary; a module or tool dependency is
    a decision (licence, `THIRD-PARTY-NOTICES.md`), never a convenience,
    and `go.mod`'s direct list is the record.

**How the principles are enforced.** Every pull request that touches a
facade package names, in its report's Decisions section, which principle
a non-obvious choice follows. The architecture review at a gate is a
report under `notes/briefs/reports/` with one section per principle and a
finding per violation; findings are fixed in their own wave before the
next facade wave starts. One of the gate's readers is an agent without
prior context that receives only `go doc` of the facade and the docs and
is asked to write a bridge: what it stumbles over is a finding against
principle 1 or 5.

## Consequences

- A review has a list; "well, it works" stops being an argument for a
  facade shape, and "it was simpler" stops being one for an internal one.
- The generated typed layer is held to one hull over every server
  (principle 2), which costs a generalisation of the generator in phase 20
  rather than a special case per self-held server.
- The seams page becomes part of the public documentation, and apidiff
  watches the seams as it watches the facade (principle 4, 9).
- A principle that proves wrong is changed here, with the date and the
  reason, not worked around in a wave.
