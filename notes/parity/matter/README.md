# Wire-fixture generators

Node scripts that run *inside a built matter.js checkout* and print the
byte-level output of matter.js's own encoders as JSON. The Go parity tests
compare this module's encoders against those bytes, so a wire-shape drift
fails a test instead of failing a controller.

| Generator | Emits | Consumed by |
| --- | --- | --- |
| `generate-tlv-wire-fixtures.ts` | Low-level TLV primitive wire bytes (uint, bool, string, tags) | [`tlv/testdata/tlv-wire-fixtures.json`](../../../tlv/testdata/tlv-wire-fixtures.json) → `tlv/parity_matterjs_test.go` |
| `generate-im-wire-fixtures.ts` | IM-message-level wire bytes (ReportData, StatusResponse, …) | [`im/testdata/im-wire-fixtures.json`](../../../im/testdata/im-wire-fixtures.json) → `im/wire_fixtures_parity_test.go` |
| `generate-group-fixtures.ts crypto` | Operational group key, group session id, privacy key, multicast address and complete sealed group messages (`FabricGroups`, `KeySets`, `MessagePrivacy`, `GroupSession.encode`) | [`groups/testdata/group-crypto-fixtures.json`](../../../groups/testdata/group-crypto-fixtures.json) → `groups/parity_matterjs_test.go` |
| `generate-group-fixtures.ts wire` | GroupKeyManagement / Groups command payloads (`TlvOfModel(command)`) | [`bridge/testdata/group-wire-fixtures.json`](../../../bridge/testdata/group-wire-fixtures.json) → `bridge/groups_parity_matterjs_test.go` |
| `generate-application-fixtures.ts` | FanControl Step request payloads and the SmokeCoAlarm / PumpConfigurationAndControl / Switch event payloads (`TlvOfModel(element)`) | [`bridge/testdata/application-wire-fixtures.json`](../../../bridge/testdata/application-wire-fixtures.json) → `bridge/application_parity_matterjs_test.go` |
| `generate-colorcontrol-fixtures.ts` | Every ColorControl request payload and the colour attributes cluster/light reads (`TlvOfModel(element)`) | [`bridge/testdata/colorcontrol-wire-fixtures.json`](../../../bridge/testdata/colorcontrol-wire-fixtures.json) → `bridge/colorcontrol_parity_matterjs_test.go` |
| `generate-filter-fixtures.ts` | HepaFilterMonitoring / ActivatedCarbonFilterMonitoring ResetCondition requests and ReplacementProductList values (`TlvOfModel(element)`) | [`bridge/testdata/filter-wire-fixtures.json`](../../../bridge/testdata/filter-wire-fixtures.json) → `bridge/filter_parity_matterjs_test.go` |
| `generate-group-fixtures.ts groupcast` | Groupcast command payloads, the Membership, AccessControl.AuxiliaryAcl / Acl / Extension and GroupKeyManagement GroupTable / GroupKeyMap values (unfiltered read), the GroupcastTesting / AuxiliaryAccessUpdated events (`TlvOfModel(element)`) | [`bridge/testdata/groupcast-wire-fixtures.json`](../../../bridge/testdata/groupcast-wire-fixtures.json) → `bridge/groupcast_parity_matterjs_test.go` |

The `testdata/` copies are the **masters**: the tests read them, nothing reads
a file in this directory. A generator exists to *re-derive* those bytes from a
newer matter.js, not to be the source of truth at rest.

## Regenerating

The scripts import `@matter/model` and friends by bare specifier, so they have
to run from inside the matter.js checkout, after it is built. Do not pipe
straight onto the destination file — a throw would truncate it.

```sh
cd ../matter.js/packages/model && npm run build && cd ../..

export GO_FABRIC=../go-fabric

node "$GO_FABRIC"/notes/parity/matter/generate-tlv-wire-fixtures.ts > /tmp/tlv.json \
    && mv /tmp/tlv.json "$GO_FABRIC"/tlv/testdata/tlv-wire-fixtures.json

node "$GO_FABRIC"/notes/parity/matter/generate-im-wire-fixtures.ts > /tmp/im.json \
    && mv /tmp/im.json "$GO_FABRIC"/im/testdata/im-wire-fixtures.json

node "$GO_FABRIC"/notes/parity/matter/generate-group-fixtures.ts wire > /tmp/gw.json \
    && mv /tmp/gw.json "$GO_FABRIC"/bridge/testdata/group-wire-fixtures.json

node "$GO_FABRIC"/notes/parity/matter/generate-group-fixtures.ts groupcast > /tmp/gcw.json \
    && mv /tmp/gcw.json "$GO_FABRIC"/bridge/testdata/groupcast-wire-fixtures.json

node "$GO_FABRIC"/notes/parity/matter/generate-application-fixtures.ts > /tmp/app.json \
    && mv /tmp/app.json "$GO_FABRIC"/bridge/testdata/application-wire-fixtures.json

node "$GO_FABRIC"/notes/parity/matter/generate-group-fixtures.ts crypto > /tmp/gc.json \
    && mv /tmp/gc.json "$GO_FABRIC"/groups/testdata/group-crypto-fixtures.json

node "$GO_FABRIC"/notes/parity/matter/generate-filter-fixtures.ts > /tmp/f.json \
    && mv /tmp/f.json "$GO_FABRIC"/bridge/testdata/filter-wire-fixtures.json

node "$GO_FABRIC"/notes/parity/matter/generate-colorcontrol-fixtures.ts > /tmp/cc.json \
    && mv /tmp/cc.json "$GO_FABRIC"/bridge/testdata/colorcontrol-wire-fixtures.json
```

Then run `go test ./tlv/... ./im/... ./bridge/... ./groups/...` from this module's root.

**A changed byte is a review decision, not a formatting update.** The whole
point of the fixtures is that this module's output is pinned; a diff here says
either matter.js changed its wire shape (follow it, and say so in the
changelog — see the parity-correction carve-out in
[`README.md`](../../../README.md#pre-10)) or the generator was run against a
checkout that is not HEAD.

## Related

- The cluster/device-type schema snapshot is a different pipeline:
  `parity/schema.json`, refreshed with `make generate-matter-schema`. See
  [`CLAUDE.md`](../../../CLAUDE.md).
- The scenario corpus that drives a *live bridge* (rather than an encoder)
  belongs to the host and stays in OpenCCU-Loom at
  [`notes/parity/matter/scenarios/`](https://github.com/SukramJ/openccu-loom/tree/main/notes/parity/matter/scenarios).
