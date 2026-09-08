# Architecture Decision Records

Decisions that shaped this module and are expensive to re-litigate. An ADR is
written when a choice is architectural, contested, or would otherwise look like
an oversight to the next reader — including choices to *not* do something.

| ADR | Decision | Status |
| --- | --- | --- |
| [0001](./0001-matter-commissioning-bring-up.md) | Matter wire-protocol design rules from chip-tool bring-up | accepted |
| [0002](./0002-im-opcode-dispatch-seam.md) | A testable gate + per-opcode seam in `handleIMOpcode` | accepted |
| [0003](./0003-sigma-resume-extraction.md) | Sigma resumption extraction: already satisfied (finding corrected) | accepted, no code change |
| [0004](./0004-groups-cluster-stays-stub.md) | Groups stays a stub — a deliberate, matter.js-conformant divergence | rejected (the proposal, not the stub) |
| [0005](./0005-bridge-decomposition.md) | Defer the `Bridge` CommissioningSession / IMEngine facade split | accepted, deferred with a plan |
| [0006](./0006-subscribe-dispatch-seam.md) | Cohesive sub-helpers out of `handleSubscribeRequest` | accepted |
| [0007](./0007-chiptool-send-receive-matrix.md) | Hermetic per-type send/receive suite against chip-tool | accepted |

## Provenance

All seven were recorded in
[OpenCCU-Loom](https://github.com/SukramJ/openccu-loom/tree/main/docs/adr) while
the Matter stack still lived there as `internal/north/matter/`, and moved here
with the stack. They were renumbered into this module's own sequence; each file
names the OpenCCU-Loom number it carried. Decisions that are about *projecting a
device model onto Matter* — which HomeMatic device becomes which device type,
how many endpoints a physical device gets — stayed there, because that is the
host's decision, not this module's.

## Related

- [`../matter-parity-contract.md`](../matter-parity-contract.md) — what parity
  means and which guards enforce it. Read this before your first change.
- [`../../notes/parity/by_design.md`](../../notes/parity/by_design.md) — the
  divergence catalogue. A non-trivial entry there also gets an ADR here.
