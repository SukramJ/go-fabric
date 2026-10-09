# ADR 0017 — A generated default cluster server, so that a cluster without matter.js logic costs no hand-written server

- **Status**: Proposed (planning record; phase 0 of
  [`../concept-matter-1.6.1-and-device-layer.md`](../concept-matter-1.6.1-and-device-layer.md))
- **Date**: 2026-10-08
- **Related**:
  [ADR 0013 — generated cluster definitions](./0013-generated-cluster-definitions.md),
  [ADR 0016 — device type validation](./0016-device-type-validation.md),
  [ADR 0018 — the device layer](./0018-device-layer-and-node-facade.md),
  [`../matterjs-comparison.md`](../matterjs-comparison.md) §5,
  `cluster/spec/`, `script/clustergen`

## Context

The goal set on 2026-10-08 is Matter 1.6.1 complete for the home-automation
scope (the concept, §0 and Appendix F). Measured against the snapshot
(`parity/schema.json`, 135 clusters), the module serves 55 clusters; the
scope needs 46 more: 11 that a device type in scope mandates, 35 that one
allows (concept, §A.2).

matter.js does not write 46 servers for them. Its node mounts, for most
clusters, the behaviour `ClusterBehavior.for(Cluster)` generates from the
model, and a `*Server.ts` file that adds nothing:

```ts
// packages/node/src/behaviors/fixed-label/FixedLabelServer.ts
export class FixedLabelServer extends FixedLabelBehavior {}
```

Of the 143 behaviour directories, 48 have a `*Server.ts` over 40 lines
(measured with `wc -l`); of the 46 clusters this module still needs, 30
have none or a trivial one. What makes the trivial server correct is the
machinery under it: `ValidatedElements`
(`packages/node/src/behavior/cluster/ValidatedElements.ts`) derives the
attribute, command and event lists from the model and the active features;
the model's constraint and conformance logic
(`packages/model/src/logic/`) validates every write; the state layer
tracks data versions and emits the change events the subscription engine
reports. A generated behaviour is a full server; a hand-written one only
replaces parts of it.

ADR 0013 brought the first half here: `script/clustergen` emits a
definition package per cluster — ids, typed enums, bitmaps and structs with
TLV codecs, the conformance, access, quality and constraint of every
element — and `cluster/spec` derives lists, globals, privileges, write
checks and payload codecs from it. What it did not bring is the server
itself: since the last migration round (2026-10-09) every application
server, `cluster/measurement`, GenericSwitch, AdministratorCommissioning and
fourteen `cluster/core` servers are built on their definitions, and each
still writes the host port, the read and write dispatch and the reporting
by hand. Writing that by hand 46 more times is the cost this ADR removes.

## Decision

**`cluster/spec` gains a complete default server.** A type (working name
`spec.Server`) built from a generated definition and a feature selection
implements `contract.ClusterServer` and every optional contract interface
the dispatcher consults (`ClusterAttributeLister`, `ClusterCommandLister`,
`ClusterEventLister`, `ClusterDataVersion`, the three privilege interfaces,
`AttributeChangeNotifier`, `FabricScopedReader` where the definition has
fabric-scoped elements), with no cluster-specific code:

- **Reads** answer from a typed state store the definition shapes; an
  attribute the conformance excludes for the selected features is absent,
  as `ValidatedElements` makes it absent.
- **Writes** run the ADR 0013 write checks (writability, constraint,
  conformance, privilege, timed requirement from the generated tables) and
  then land in the state store, bump the data version and notify the
  change, as matter.js's state transaction does.
- **Commands** with no matter.js logic are rejected as
  `UnsupportedCommand` unless the host registers a handler; a cluster whose
  matter.js server implements a command (the 48) keeps a hand-written
  server, as today.
- **The host port** is one interface per server: a value source for
  attributes the device owns and a sink for writes the controller makes,
  typed by the generated state struct. The measurement materializers'
  pattern (`contract.MeasurementMaterializer`) stays for the sensor family.

**The hand-written schema tables stay**, as ADR 0013 decided in 2026-10
after diffing them against generated ones ("What stays as it was"): the
differences are behaviour changes to classify per row, not transcription
defects. A server on the default server answers writability, the timed
requirement and the privileges itself from its definition, as the migrated
servers do (`ValidateWrite`, `MinWritePrivilege`, `MinInvokePrivilege`), so
the dispatcher's tables cover only the servers that are not on it. Whether
a table row is then generated, or retired, is that classification, and it
is not part of this ADR.

**What stays hand-written** is exactly what matter.js hand-writes: the
rules. The ADR 0013 migration list names them per existing server; for a
new cluster the rule is the size of its matter.js `*Server.ts`. A server
with rules is a `spec.Server` wrapped by the rules, not a rewrite of the
default.

**Order.** The default server lands before any of the 46 clusters, with
one existing generated-only server moved onto it first as the proof
(`cluster/filter` is the candidate: generated from the start, no rules).
The ADR 0013 migration list is complete but for the four security clusters
it skips on purpose; those are not candidates for the default server
either.

## Consequences

- A cluster without matter.js logic costs its definition (generated), a
  one-line registration and a host port. FixedLabel, UserLabel, the seven
  concentration clusters, the localization clusters and the diagnostics
  clusters are this case.
- The dispatcher's behaviour is unchanged: the default server implements
  the same contract interfaces a hand-written one does, so
  `endpoint/dispatcher.go`, the IM gates and the ACL checks neither know
  nor care which kind they talk to.
- A transcription defect loses its last hiding place: with the tables
  generated and the server generic, a cluster's observable behaviour comes
  from the snapshot and the rules, and the rules are the only code reviewed
  per cluster.
- Every generated server package keeps a coverage floor; its generated
  test holds the definition against the snapshot and round-trips every
  codec, as ADR 0013 does today; the default server has its own tests once,
  not per cluster.
- Risk, recorded in the concept §D: some matter.js behaviours with no
  `*Server.ts` inherit rules from a family base (`ModeBase`,
  `ConcentrationMeasurement`, `ResourceMonitoring`, `AlarmBase`, `Label`
  directories without a server file). Those bases are read before the
  first family migrates; `cluster/modebase` already carries ModeBase's.
