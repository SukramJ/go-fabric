# Controller profiles — register

What a specific controller ecosystem tolerates of a node's surface, with
the evidence for every row. Concept v11 §C.6 and ADR 0030 decision 8: a
profile is the device-side twin of `eligibility.Compat` (which records the
device types a controller silently drops, `eligibility/compat.go`). The
confidence levels are those of
[`docs/matter-ecosystem-observations.md`](../../docs/matter-ecosystem-observations.md):

| Level | Meaning |
| --- | --- |
| **Reproduced** | Observed against a real controller during this project's own bring-up, with the evidence named in the row. |
| **Inherited** | Taken from matter.js, which encodes it after their own interop testing. Not independently reproduced here. |
| **Reported** | Third-party report we act on but have not reproduced. |

**Only a Reproduced row removes an element in a shipped profile** (owner
decision 2026-10-30, E.5). Reported and Inherited rows stand here and
appear in the surface report as warnings. A row never states a mechanism
it has not observed; when the mechanism is unknown, it records the
observation and stops.

**The table is machine-read** by `script/profilegen`, which generates
`surface/profiles_gen.go` and whose test holds the generated file to this
table. Column contract: `Profile` is the profile id (`apple-home`,
`google-home`, `alexa`, `smartthings`, `home-assistant`); `Element` is
`cluster <hex id> feature <code>` / `attribute <hex>` / `command <hex>` /
`event <hex>`, or `device-type <hex>`, or `level` for a row that sets the
profile's level; `Effect` is `remove`, `level <level>` or `warn`;
`Confidence` is one of the three levels; `Source` names the evidence
(a hand-test record under `notes/reports/`, a code comment `file:line`, a
matter.js path at the pin, or an issue); `Reason` is one sentence.

## Register

| Profile | Element | Effect | Confidence | Source | Reason |
| --- | --- | --- | --- | --- | --- |
| apple-home | cluster 0x0201 feature MSCH | warn | Reported | openccu-loom 0.94.0 owner report (go-fabric #331): with MSCH, TemperatureSetpointHold and ThermostatRunningState enabled together, Apple Home stopped loading data after a late enable and refused a fresh pairing; which element caused it is not measured | Not reproduced in isolation; the S5 hand test (MSCH alone, Hold alone, RunningState alone) decides whether this becomes a Reproduced `remove`. |

Rows are added only with their evidence. An empty register ships every
profile as a name with no removals.
