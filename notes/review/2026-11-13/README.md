# Whole-codebase review II, November 2026

Owner decision 2026-11-13. Goal: every weakness found and eliminated, over
every aspect. The review of 2026-11-01 (`../2026-11/`, HEAD `769ea3c2`) ran
package by package along the request path; this one adds the cuts that one
could not see, and reads every hand-written package again.

Pins: matter.js `db021da0` (`parity/schema.json` → `matter.sourceCommit`),
connectedhomeip `6170af84` (`CHIP_TEST_IMAGE_COMMIT`). Baseline HEAD:
`95e03096` (main after #520, #523 and #522). The sibling checkouts were not
at the pins on 2026-11-13 (matter.js `ba725846`, connectedhomeip
`f09331bd`); reviewers read `git archive` exports of the two pins.

## Owner decisions (2026-11-13)

| Decision | Choice |
| --- | --- |
| Cut | **Maximal**: delta, cross-cutting flows, adversarial, and a full second pass over every hand-written package |
| Models | **Opus by default** (owner 2026-11-13, decision 24): every implementation and the code review of a diff. **Fable** only for the security paths' review and verification, every citation and register line against the pin, the verification of an Opus review's high and medium findings, and planning. Inventories by script or Sonnet |
| Fix rhythm | **One fix PR per phase**, bundled — CI runs are long (owner 2026-11-13), so no smaller pull requests than that unless a fix cannot wait; the next phase reads the repaired code; one tag at the end |
| Baseline | main **after #520 merged** |
| Feature work | **continues** during the review; each phase re-reads what landed since the one before as delta, and a convergence run holds for the HEAD it read |
| Plan | committed with the phase 0 pull request |
| chip-tool on the Mac | no native build. Phase 0 tries once, time-boxed: the pinned arm64 chip-tool (`make chiptool-extract`) with avahi and the reference daemon in **one** `linux/arm64` container on the IPv6 bridge network — never the host network — through the host-chip-tool fallback (`internal/chiptool/doc.go:61-64`). CI stays the instance that counts; the families run only there |
| Done | **Every finding down to note** ends fixed, struck with a reason, or as a `by_design` row; one standing guard per defect class; convergence runs until one yields no new high or medium |

On record (assistant, before implementing): the second full pass over the
security paths Fable read on 2026-11-01 has the lowest yield per token, so
it runs last and counts as the first convergence run; Fable on guard-checked
mechanical fixes buys no measurable quality over Opus. The owner decided.

## What is in this directory, and what is not

The plan (this file), the owner's decisions (`decisions.md`), the
dispositions of every finding (`phase-<N>-register.md`) and the reviewers'
and fix agents' working reports. A report that mentions a security item
still open is held back, with that item's row, until the pull request that
fixes it (owner decision 25): of phase 0 these are the threat model, the
verifier's report, the request-path diff review and five fix and rework
notes. The dispositions table says how many items are held back.

## Phases

| Phase | Content |
| --- | --- |
| 0 Basis | Pin the baseline. Register reconciliation: do the fixes of #359–#367 still hold, status of every open low and note of `../2026-11/`. Threat model (`notes/audits/matter-threat-model.md`, 2026-10-24) brought up to ADR 0031, BLE, Wi-Fi, Thread, controller. Mechanical evidence: `make race`, fuzz at a large budget, govulncheck, error-path coverage, mutation sampling on `secure/`, ACL, `im/` (ad hoc, no new dependency). |
| 1 Delta | Package by package, everything since `769ea3c2` no review has read: `controller`, `mdns`, `ble/*`, `wifi/*`, `thread`, device-side BLE commissioning, the nested modules. |
| 2 Flows | End to end across packages: commissioning and fail-safe; fabric life cycle (add, update, remove → sessions, subscriptions, ACL, groups, resumption, store); session and subscription life cycle; persistence and crash consistency; shutdown and goroutine leaks; counters, replay, rollover; every allocation or recursion a wire length drives. |
| 3 Adversarial | One reviewer per attacker role, exploits written as failing tests: unauthenticated LAN, PASE holder, another fabric's administrator, hostile device against the controller, hostile BLE peripheral, hostile Wi-Fi / DCL / OTA peer. |
| 4 Parity | Behaviour of the hand-written servers and the IM against matter.js at the pin; every `by_design` row and Ahead marker re-read; mandatory requirements against `docs/certifiability.md`; the citation drifts on their ratchet. |
| 5 Architecture, tests, docs | The API cut of `node` / `device` / `contract` against the architecture standard; test quality (tests that assert nothing, flakes, missing negative cases, required checks); every behavioural claim in `docs/` against the code; supply chain, workflows, release. |
| 6 Full pass | Every hand-written package again, in the wave order of `../2026-11/README.md`, the packages Opus read first. Counts as convergence run 1. |
| 7 Convergence | Repeat a full-cut run until one yields no new high or medium. |

## Aspects

Security and crypto · access control and fabric scoping · concurrency ·
resource bounds against hostile peers · wire and IM correctness · state
machines and timeouts · persistence · error paths (fail open, noop ports) ·
matter.js and chip parity with the registers · API and architecture · tests
and guards · supply chain and build · docs against code.

## Method per phase

1. **Map.** Script-generated inventory per package (`notes/reads/`), `file:line`
   for every fact. A map, never a citation.
2. **Review.** One Fable reviewer per theme or role, briefed with the map,
   the file list and the pinned sources. Findings into
   `phase-<N>-<theme>.md`: ID, severity (high / medium / low / note),
   `file:line`, the claim, the matter.js or chip citation, the proposed fix.
3. **Verify.** A second, independent Fable re-reads every finding against
   the code and the pin and tries to refute it; struck with a reason, or
   confirmed. Measured and inferred are reported separately.
4. **Fix.** Branch `review2/p<N>`; regression test first (red, then green);
   one standing guard per defect class; guards in the background; one pull
   request per phase. Decisions go to the owner as a wizard. A confirmed
   vulnerability is not committed to the register before its fix merges.

## Finding IDs

`R2-P<N>-<theme>-<nn>`; themes `sec`, `acl`, `conc`, `bound`, `wire`, `fsm`,
`store`, `err`, `par`, `api`, `test`, `sup`, `doc`. Status: open / fixed
(#PR) / struck (reason) / by_design (row).
