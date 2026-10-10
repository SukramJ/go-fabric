# Brief B — the bridge path onto the generated codecs (ADR 0013 follow-up)

Owner: Fable (review). Implementer: Opus. Branch `refactor/bridge-generated-codecs`.

## Goal

The open item "Bridge path still on hand-written wire types"
(`docs/open-items.md` §3): the values the migrated application servers hand
the bridge are encoded by the generated codecs, not by the hand-written
switch in `bridge/application_values.go`.

## Facts

- `bridge/application_values.go` `encodeApplicationValue` /
  `encodeApplianceValue` hand-encode: `clusterwire.FieldlessEvent`,
  `matteralarm.AlarmSeverityEvent`, `[]string` (PhaseList),
  `[]clusterwire.OperationalStateStruct`, `clusterwire.ErrorStateStruct`,
  `OperationalErrorEvent`, `OperationCompletionEvent`,
  `[]clusterwire.ModeOptionStruct`, `OperationalCommandResponse`,
  `ChangeToModeResponse`. It already falls through to `spec.Encodable`
  (line 110 and 166): a value implementing it encodes through its generated
  codec.
- The generated definitions under `cluster/spec/<cluster>/definition_gen.go`
  carry typed structs with codecs for every one of these shapes; their
  round-trip against matter.js's wire fixtures is held by each package's
  `definition_gen_test.go` (ADR 0013).
- ADR 0013's migration list records where a server already aliases a
  generated type (SmokeCoAlarm enums, OperationalState structs, ModeSelect
  structs, ClosureControl event payloads, GenericSwitch press payloads).
- The fixtures the bridge's encoding is pinned to:
  `bridge/appliance_clusters_wire_test.go`,
  `bridge/application_clusters_wire_test.go`,
  `bridge/appliance_parity_matterjs_test.go`,
  `bridge/application_parity_matterjs_test.go`.

## Scope

Must: for each case in the two functions, make the server return (or alias
to) the generated type so the value takes the `spec.Encodable` path, and
delete the hand-written case. Byte-for-byte: the wire tests above pass
unchanged. Where the generated encoding differs from the hand-written one,
stop and report the difference with both byte strings — do not adapt the
test.

Should: `bridge/reply.go` `defaultAttributeValueWriter` cases that belong
to a migrated application cluster, same rule.

Not in scope, by design — leave as is and do not re-open:
`BD-Matter-LevelControl-LenientDecoders` (LevelControl requests, FanControl
Step), `BD-Matter-ColorControl-LegacyDecoders` (four ColorControl requests),
`BD-Matter-Commissioning-HandDecoders` (GeneralCommissioning, Groups,
ScenesManagement), `BD-Matter-AdminCommissioning-OpenWindowParams`,
`BD-Matter-FixedWidthStructs` (DeviceTypeList, BasicCommissioningInfo), and
everything under `attribute_value_reader.go` (ACL, GroupKeyMap, Extension
lists: security-relevant, owner only). The ModeBase `ChangeToModeRequest`
decoder (`fields_reader.go` case for the four mode clusters) stays in this
PR: brief C owns `cluster/modebase` and makes its `MatterInvoke` accept the
generated request first; removing the bridge case is a follow-up after both
merge.

## Guards

`go test ./bridge/... ./cluster/...`, `make fuzz` targets in `bridge/`
(`fuzz_*_fields_test.go`) still build and run, `make ci`.

## Do not touch

`cluster/spec/server.go` (brief A), `cluster/modebase/` (brief C),
`cluster/filter/` (brief A), the by-design decoders above.
