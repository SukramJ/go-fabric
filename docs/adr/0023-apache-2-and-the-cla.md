# ADR 0023 — Apache License 2.0 and a contributor licence agreement

- **Status**: Accepted (2026-10-15)
- **Date**: 2026-10-15
- **Related**: [ADR 0011 — certifiability is a goal](./0011-certifiability-is-a-goal.md),
  `THIRD-PARTY-NOTICES.md`, `CLA.md`

## Context

go-fabric was released under MIT (`v0.2.0` to `v0.4.0`). It is a semantic
port of matter.js, which is Apache-2.0, as is connectedhomeip; both chose
Apache for its patent grant (§3), which matters in a protocol standardised
by a consortium with patent pools. The owner weighed restricting commercial
use (PolyForm Noncommercial, or AGPL with dual licensing) on 2026-10-15 and
decided against it: the module's value is its upkeep and parity guards, not
the code, which anyone can obtain from matter.js; the ecosystem proofs and
certifiability (ADR 0011) need users more than control; a restriction would
drop the module from pkg.go.dev's rendered docs and from any Home Assistant
Core dependency list; and the released MIT versions cannot be withdrawn.
What the owner wants to keep is the option to license the whole module
under other terms later, which is lost the moment an outside contribution
arrives without a rights grant.

## Decision

1. **Apache License 2.0** from the next release: `LICENSE` is the Apache
   text, `NOTICE` names the copyright and the matter.js lineage with its
   reproduced NOTICE, every Go file's SPDX header reads `Apache-2.0`, and
   `TestLicenseHeaderOnEveryGoFile` enforces it. Generated files carry the
   same header from their generators.
2. **The released MIT versions stay MIT.** A licence already granted is
   not revoked; `v0.4.0` and earlier remain available under MIT.
3. **A contributor licence agreement** (`CLA.md`): every contribution
   grants the owner a copyright licence that allows sublicensing under any
   terms, a patent licence with a litigation termination clause, and the
   usual representations. Agreement is a line in the first pull request
   (`.github/PULL_REQUEST_TEMPLATE.md`); pull requests without it are not
   merged. Dependabot's changes are mechanical and carry no authorship.
4. **Dependencies stay permissive** (MIT, Apache-2.0, BSD): nothing in
   this decision opens the door to copyleft.

## Consequences

- Apache-2.0 is MIT's superset for consumers: notices travel
  (`NOTICE`, `THIRD-PARTY-NOTICES.md`), modified files need no change
  marks beyond the header, and the patent grant protects users of the Go
  code as it does users of matter.js.
- Relicensing later stays possible for the whole module as long as every
  contribution came under the CLA.
- `CLAUDE.md`, `CONTRIBUTING.md` and `README.md` §Licence name the
  licence and the agreement; the changelog announces the change as a
  consumer-visible one.
