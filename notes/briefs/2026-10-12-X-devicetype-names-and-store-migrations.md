# Brief X — issues #57 and #62: device-type labels from the schema, versioned store migrations

Owner: Fable. Implementer: Opus. Branch `wave9/store` from
`planning/briefs-wave-9`, pushed, **no PR**. Report in
`notes/briefs/reports/X.md`. Read first: `notes/briefs/README.md`, the
issues `gh issue view 57` and `gh issue view 62` (the owner wrote them
from the openccu-loom side; the facts below were checked against the
code). One commit per item, reachability in each.

## 1. `contract.DeviceTypeName` knows every schema device type (#57)
- Module: `contract/matter.go:860-871` (`DeviceTypeName`, the hand-kept
  `deviceTypeNames` with 31 entries), `:828` `MeasurementClassDeviceType`
  (SoilMoisture → 0x0045); `schema/devicetypes.go:137` (`0x0045:
  "SoilSensor"`, generated), `schema/lookup.go:46` `schema.DeviceTypeName`;
  `contract/devicetype_name_test.go:15`.
- Constraint: `contract/` imports **stdlib only** (CLAUDE.md). It cannot
  import `schema`. So: generate the table. Extend `script/devicegen` (or
  `script/generate_matter_schema.go`, whichever already writes into a
  hand-kept package — say which and why) to emit `contract/devicetype_names_gen.go`
  with every device type of the snapshot, labels as `schema.DeviceTypeName`
  gives them; the 31 hand-kept labels must not change (a diff of the
  generated table against the old map is the review item — list every
  label that would differ and keep the hand-kept spelling where it
  differs, as a generator override table with a comment per entry).
- Test: every value `MeasurementClassDeviceType` can return has a label
  that is not the hex fallback; every id in `schema/devicetypes.go` has a
  label (the same shape as openccu-loom's guard
  `TestW2PkgMatterDeviceTypeNameCoversEveryAdvertisedType`). `go test
  ./script/devicegen/...` fails when the generated file is stale.

## 2. `store.Migrations()` (#62)
- Module: `store/schema.go:13-35` (`schemaDDL` from `store/schema.sql`,
  `Schema()`), `:48` `Apply`, `:72` `Upgrade`, `:85-112`
  `dropGroupKeyMapKeySetForeignKey` (rebuild of `matter_group_key_map`);
  CHANGELOG `[0.3.0]` names the four tables added in v0.3.0
  (`matter_group_table`, `matter_groupcast_groups`,
  `matter_server_subscriptions`, `matter_ota_default_providers`) and the
  rebuild; `[0.2.0]` and earlier name the rest — read the CHANGELOG's
  store entries to cut the versions.
- Must: `type Migration struct { Version string; Up string; Down string }`
  and `func Migrations() []Migration`, ordered; v1 = the DDL as of
  v0.2.0, v2 = the four v0.3.0 tables, v3 = the `matter_group_key_map`
  rebuild (the `Upgrade` step), further steps only if the CHANGELOG
  names them. `Down` may be `DROP TABLE` for additive steps and the
  reverse rebuild for v3. `Apply` and `Upgrade` keep their behaviour and
  signatures; implement them over the list or keep `schema.sql` as the
  flattened form — either way a test applies the list to a fresh
  database and `Apply` to another and compares `sqlite_schema` (name,
  type, normalised sql) so the two cannot drift. A second test pins
  `len(Migrations())` and the last version, the way a host would.
- Also: `func SchemaFingerprint(ctx, db) (string, error)` — hash of the
  normalised `sqlite_schema` after `Apply` — and its value for this
  release in the CHANGELOG entry (the host pins it).

Not in scope: changing any table, the queries, or `Apply`'s idempotence.

## Guards
`make ci`, `make race` (store, contract), `make reachability-check`,
`go test ./script/devicegen/...`, `make chiptool-test` locally (the daemon
uses `store.Apply`). CHANGELOG under `[Unreleased]` (Added: `Migrations`,
`SchemaFingerprint`; Fixed: #57). Report as brief W: what was built, which
guard proves it, what was left out and why, every open question.
