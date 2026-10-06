# Architecture Decision Records

Decisions that shaped this module and are expensive to re-litigate. An ADR is
written when a choice is architectural, contested, or would otherwise look like
an oversight to the next reader — including choices to *not* do something.

| ADR | Decision | Status |
| --- | --- | --- |
| [0001](./0001-matter-commissioning-bring-up.md) | Matter wire-protocol design rules from chip-tool bring-up | accepted |
| [0002](./0002-im-opcode-dispatch-seam.md) | A testable gate + per-opcode seam in `handleIMOpcode` | accepted |
| [0003](./0003-sigma-resume-extraction.md) | Sigma resumption extraction: already satisfied (finding corrected) | accepted, no code change |
| [0004](./0004-groups-cluster-stays-stub.md) | Groups stays a stub — a deliberate, matter.js-conformant divergence | superseded by 0009 |
| [0005](./0005-bridge-decomposition.md) | Defer the `Bridge` CommissioningSession / IMEngine facade split | accepted, deferred with a plan |
| [0006](./0006-subscribe-dispatch-seam.md) | Cohesive sub-helpers out of `handleSubscribeRequest` | accepted |
| [0007](./0007-chiptool-send-receive-matrix.md) | Hermetic per-type send/receive suite against chip-tool | accepted |
| [0008](./0008-subscription-resumption.md) | Persist server subscriptions and re-establish them after restart, with a CASE initiator scoped to that alone | accepted |
| [0009](./0009-groups-and-group-messaging.md) | A real Groups server and group message reception, with membership as stack state; Groupcast deferred to 0010 | accepted |
| [0010](./0010-groupcast-and-auxiliary-acl.md) | Groupcast (Listener, PerGroup) and the AccessControl Auxiliary ACL on the root, on the ADR 0009 group state; no Sender | accepted |
| [0011](./0011-certifiability-is-a-goal.md) | Certifiability is a goal, certification is not pursued; certification families are the yardstick, every exclusion is classified, PICS are honest, tests replace manual device testing | accepted |
| [0012](./0012-scenesmanagement-server.md) | A real ScenesManagement server on the bridged lights, as matter.js builds it; RemainingCapacity bounded by the shared table as chip does | accepted |
| [0013](./0013-transitions-optional-per-endpoint.md) | Attribute transitions run in the module (`cluster/transition`, matter.js `Transitions.ts`), optional per endpoint; a host whose device ramps natively keeps the hand-off path | accepted |

## Provenance

ADRs 0001–0007 were recorded in
[OpenCCU-Loom](https://github.com/SukramJ/openccu-loom/tree/main/docs/adr) while
the Matter stack still lived there as `internal/north/matter/`, and moved here
with the stack. They were renumbered into this module's own sequence; each file
names the OpenCCU-Loom number it carried. ADR 0008 onwards were written here. Decisions that are about *projecting a
device model onto Matter* — which HomeMatic device becomes which device type,
how many endpoints a physical device gets — stayed there, because that is the
host's decision, not this module's.

## Related

- [`../matter-parity-contract.md`](../matter-parity-contract.md) — what parity
  means and which guards enforce it. Read this before your first change.
- [`../../notes/parity/by_design.md`](../../notes/parity/by_design.md) — the
  divergence catalogue. A non-trivial entry there also gets an ADR here.
