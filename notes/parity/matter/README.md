# Wire-fixture generators

Two Node scripts that run *inside a built matter.js checkout* and print the
byte-level output of matter.js's own encoders as JSON. The Go parity tests
compare this module's encoders against those bytes, so a wire-shape drift
fails a test instead of failing a controller.

| Generator | Emits | Consumed by |
| --- | --- | --- |
| `generate-tlv-wire-fixtures.ts` | Low-level TLV primitive wire bytes (uint, bool, string, tags) | [`tlv/testdata/tlv-wire-fixtures.json`](../../../tlv/testdata/tlv-wire-fixtures.json) → `tlv/parity_matterjs_test.go` |
| `generate-im-wire-fixtures.ts` | IM-message-level wire bytes (ReportData, StatusResponse, …) | [`im/testdata/im-wire-fixtures.json`](../../../im/testdata/im-wire-fixtures.json) → `im/wire_fixtures_parity_test.go` |

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
```

Then run `go test ./tlv/... ./im/...` from this module's root.

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
