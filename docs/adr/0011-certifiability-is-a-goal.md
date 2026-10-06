# ADR 0011 — Certifiability is a goal; certification is not pursued

- **Status**: Accepted
- **Date**: 2026-10-05
- **Supersedes**: the scope statement "no CSA certification" wherever it was
  read as "certification conformance does not matter here"
- **Related**:
  [`../certifiability.md`](../certifiability.md) (the status document this
  decision requires),
  [`../matter-parity-contract.md`](../matter-parity-contract.md),
  [ADR 0007 — chip-tool send/receive matrix](./0007-chiptool-send-receive-matrix.md)

## Context

Until now the module recorded CSA certification as a non-goal, next to the
controller role, Bluetooth and Thread. The certification cases borrowed from
connectedhomeip were described as regression tests only, the PICS file was
written "only as far as the chosen cases need it", and the chip-tool suite ran
one commissioning test and a single `Test_TC_*` case.

That wording conflated two different things:

- **Certification** — submitting a product to an Authorized Test Laboratory,
  with a vendor id, a Device Attestation chain, a Certification Declaration
  and a DCL entry. This belongs to a product and its vendor. A library cannot
  be certified, and this project does not pursue it.
- **Certifiability** — the stack behaving such that a product built on it
  *can* pass. This is a property of the module, and it is the difference
  between a stack a vendor can ship and one they have to re-validate from
  scratch.

Two further facts shaped the decision. The owner cannot rely on manual tests
with physical devices and ecosystems, so whatever is not covered by an
automated test is effectively untested. And matter.js, the gold standard this
module mirrors, already shows the method: it runs whole certification test
families — YAML and Python cases, including the spec-driven device-composition
and conformance checkers — against its own device implementation, with every
exclusion written down and justified (`../matter.js/support/chip-testing/`,
`../matter.js/packages/testing/src/chip/`).

## Decision

1. **Certifiability is a goal of this module.** Certification itself is not
   pursued by this project, and nothing built on the module may be described
   as certified on the strength of anything in this repository.

2. **The certification test suite is the yardstick, not a regression
   sample.** The cases in connectedhomeip
   (`src/app/tests/suites/certification/`, `src/python_testing/`) are run by
   family against the reference DUT. The reference set is the one matter.js
   runs (`support/chip-testing/test/core`, `test/app-*`), extended by the
   families of every cluster server this module ships.

3. **A failing or excluded case is a gap and must be classified.** Exactly
   one of:

   | Class | Meaning | What follows |
   | --- | --- | --- |
   | (a) defect | go-fabric misbehaves or lacks mandatory behaviour | fix per matter.js, or an open finding carrying the TC id |
   | (b) not supported | optional per spec for the device types exposed, and declared so in PICS | the PICS line is the record |
   | (c) harness | the test case or the environment is at fault | cite the evidence, as matter.js does for its own excludes |
   | (d) out of scope | Bluetooth, Thread, Wi-Fi commissioning, controller role | name the scope decision |

   A case matter.js passes and this module fails is class (a) by definition.
   No case is excluded to turn a run green.

4. **PICS are honest and complete.** The PICS set describes what the DUT
   actually exposes and is checked against the running daemon; a PICS line and
   the device may not disagree.

5. **Automated tests replace manual device testing wherever possible.** Three
   layers, each runnable locally and in CI:
   - the CSA commissioner and its harness (chip-tool, the YAML runner, the
     Python `TC_*` cases) against the reference daemon;
   - matter.js's controller as a second, independent controller;
   - unit and in-process tests that port the scenarios matter.js tests in
     `packages/protocol/test` and `packages/node/test`, each citing the
     matter.js test it mirrors.

   The reference daemon offers the out-of-band controls the CSA cases expect —
   a named command pipe and `TestEventTrigger` — behind a flag that is off by
   default.

6. **The status is published and kept true.** [`../certifiability.md`](../certifiability.md)
   lists, per test family, the cases run, passed and excluded with their
   class and reason, the PICS source, the open findings by TC id, and what a
   product owner must supply that this module cannot. Where practical it is
   generated or checked by a test so that it cannot drift from the suite.

## Consequences

- An exclusion needs a written reason before it is merged; "does not pass
  yet" is a finding, not an exclusion.
- A new cluster server is not complete until its certification family runs
  and its PICS are declared.
- The chip-tool suite grows from a commissioning guard into the main
  interoperability gate, and its wall-clock and CI cost grow with it. Families
  are grouped so a pull request runs the fast core and the full set runs
  nightly.
- Certification blockers outside the module — production attestation
  material, the DCL entry, a vendor's own PICS for its product — stay with the
  product. The status document names them so that the boundary is explicit.
- The remaining scope boundaries are unchanged: no controller or commissioner
  role beyond the narrow initiator of ADR 0008, no Bluetooth, no Thread.

## What this does not claim

Passing borrowed certification cases in this repository's CI is evidence of
certifiability, not a certification result. Only an Authorized Test Laboratory
run against a concrete product produces one.
