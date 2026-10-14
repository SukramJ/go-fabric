# Brief AT — the reference controller CLI, pairing-code decoders, a persisted controller fabric, the manual test plan for `v0.4.0`

Owner: Fable. Implementer: Opus. Another Opus tranche (wave 24, media)
may still be landing: stay out of `cluster/media`, `cluster/spec/<media>`,
`device/`, `examples/reference-bridge`, `internal/chiptool`,
`docs/certifiability.md`; the shared file is `CHANGELOG.md` (the owner
merges). **Never run `make chiptool-test`, `make chiptool-families` or
any chip-tool binary.** Branch `wave26/controller-cli` from
`planning/briefs-wave-26`, pushed, **no PR**. Report
`notes/briefs/reports/AT.md`. Read first: `notes/briefs/README.md`, the
owner's inventory `read-AT.md` (copied to `notes/briefs/reads/read-AT.md`
on this branch; every repo fact there carries `file:line`),
`controller/client/registry_live_test.go:82-162` (the controller stack's
construction and teardown order — the CLI's `open()`), the three live
tests `node/binding_live_test.go`, `node/groupsend_live_test.go`,
`node/ota_live_test.go` (the API calls the CLI's `bind`, `group`, `ota`
commands make), `secure/setup/setup.go` (`Payload`, `QRCode` :100,
`ManualCode` :179 — encoders only), `controller/ca/ca.go:83-88` (`Store`,
`Stored`, `MemoryStore`), `controller/commissioner/fabric.go:67`
(`NewFabric`, `FabricConfig`), `controller/dcl/certstore.go:135`
(`OpenCertificateStore`), `controller/attestation/validate.go:236`,
`examples/reference-bridge/{main,dcl}.go` (flag style, `slog`, the
sqlite open, the `--dcl` client), `controller/client/sqlstore`,
`cluster/spec/model.go:347-387` (`AttributeByName`, `CommandByName`),
`bridge/attribute_value_reader.go` (how a `spec.Node` is built from a
typed value — the shape the JSON codec produces), report AS.

matter.js at **`0ceb202d`** (`git -C ../matter.js show 0ceb202d:<path> | cat -n`):
`packages/types/src/schema/PairingCodeSchema.ts` (`QrPairingCodeSchema`
:277-405 with `decode` :336-357 — Base38, the 11-byte header, the
optional TLV tail with vendor-specific and `serialNumber` fields :250;
`ManualPairingCodeSchema` :424-496 with the Verhoeff check digit :452,
:470 and the 11- and 21-digit forms; `isValidPasscode` :171,
`INVALID_PASSCODES` :160; `assertValidPayloadIdentity` :217),
`packages/protocol/src/fabric/FabricManager.ts:41-114` (fabrics persisted
as `Fabric.Config[]` under `"fabrics"` with `nextFabricIndex`),
`Fabric.ts` (`Fabric.Config`: what a fabric record holds). chip at the
harness pin is the authority for the tool's argument conventions:
`examples/chip-tool/commands/clusters/{JsonParser.h,ComplexArgument.h,CustomArgument.h}`
and `CustomStringPrefix.h` (`hex:`, `base64:`, `str:` string prefixes,
null, nested JSON for structs and lists, `u:`/`s:`/`f:`/`d:` typed
numbers if present — read what the pin has). Guards: `make ci`,
`make race`, `make reachability-check` (export `PATH=$HOME/go/bin:$PATH`,
`CHIP_ROOT=/home/markus/Dokumente/GitHub/connectedhomeip`; a `make ci`
failing only on golangci-lint's timeout after "0 issues" is rerun as
`make lint`; a `node/` live test failing once with "Could not connect to
device" is rerun alone; new live tests do not call `t.Parallel()`; if
`df -h /` shows under 15 GB free, run
`find ~/.cache/go-build -type f -mmin +360 -delete`). One commit per §,
reachability in each. Allowed paths: `secure/setup` (§1 decoders +
tests), `controller/ca` (§2: a `Store` over `*store.Store` settings or a
file; `Stored` may gain fields), `controller/commissioner` (§2: only if
`NewFabric` needs a "load or mint" path — say so), `cluster/spec`
(§3: `json.go` + tests, nothing else), `examples/reference-controller`
(new), `script/coverfloor/floors.go`, `CHANGELOG.md`, `README.md` (one
paragraph under the examples section naming the controller),
`notes/briefs/reads/read-AT.md` (copy only). Everything else is read,
not changed. `notes/parity/`, `docs/` and the ADRs are the owner's; the
test plan `docs/manual-test-plan-0.4.0.md` is written by the owner from
your README.

Out: a TUI or daemon mode, pairing over BLE/Wi-Fi (Ethernet/IP only,
`discovery.ByLongDiscriminator` as the harness does), a controller
device type on the CLI's own node (it is a bare controller, no
`node.Node` of its own except where `bind`/`group`/`ota` need one — read
how the live tests do it and mirror that), Joint Fabric, CommissionerControl
client, media.

## Owner's decisions

- **D1 — decoders in `secure/setup`:** `DecodeQRCode(s) (Payload, error)`
  and `DecodeManualCode(s) (ManualPayload, error)` as the inverse of the
  encoders, after `PairingCodeSchema.ts`: the `MT:` prefix, Base38, the
  88-bit header, the TLV tail (serial number and vendor elements kept
  as `Payload.Extra`), the Verhoeff digit, the 11- and 21-digit manual
  forms, `isValidPasscode`'s list. Round-trip tests against the existing
  encoders and fixtures from matter.js's own tests
  (`packages/types/test/schema/PairingCodeSchemaTest.ts` at the pin —
  copy the vectors, cite the lines).
- **D2 — the controller fabric persists:** `ca.Store` implementations
  `ca.SettingStore` (a key in `*store.Store`'s settings table, as the
  node keys its bindings) and a `FabricRecord{FabricID, RootNodeID,
  IPK, FabricIndex, Label}` stored beside it, after `FabricManager.ts`'s
  `Fabric.Config` — name which fields of `Fabric.Config` you keep and
  which you drop (no cross-fabric list: one controller fabric per
  database). `NewFabric` loads or mints; a second run of the CLI is the
  same fabric with the same root node id, so every paired node still
  trusts it. Session resumption stays in memory (`peer.MemoryResumptionStore`)
  — a `by_design` candidate, not a change.
- **D3 — one JSON codec over the generated definitions,
  `cluster/spec/json.go`:** `spec.EncodeJSON(field *spec.Field, raw
  json.RawMessage) (spec.Node, error)` and `spec.DecodeJSON(field,
  node) (json.RawMessage, error)` walking `spec.Type` (ints, bools,
  strings, bytes as chip-tool's `hex:`/`base64:` prefixes, enums and
  bitmaps by number or name, structs as objects by field name, lists as
  arrays, nullable as `null`, optional absent); chip-tool's argument
  conventions are the reference (cite `JsonParser.h` and
  `CustomArgument.h` lines). Tests: a round trip for every attribute
  and command field of every committed definition (iterate
  `spec.Lookup` over all; synthesise a value per type), plus the chip-tool
  fixtures you find in `src/app/tests/suites/` YAMLs as literal examples.
- **D4 — the CLI `examples/reference-controller`** (package `main`,
  `flag` only, `slog` to stderr, exit 1 on error, a `--db <file>`
  sqlite database holding the CA, the fabric record, the client
  registry (`sqlstore`) and the DCL trust store; `--dcl prod|testnet|off`
  default `off`, `--attestation accept|reject|ask` default `ask` on a
  terminal and `reject` otherwise; `--log-level`):
  - `pair --code <MT:… or 11/21 digits> | --passcode N --discriminator N
    [--label] [--timeout]` — discovery over DNS-SD by long discriminator,
    PASE, attestation (DCL store when `--dcl` is on, else the module's
    embedded PAAs with the policy above), NOC, the registry's record;
    prints the node id and the device view (`Endpoint.Device()`, each
    endpoint's device type name and clusters).
  - `unpair <node>` — RemoveFabric on the node, the registry's record
    deleted. `nodes` — every record with reachability and the
    conformance verdict. `node <node>` — the full image.
  - `read <node> <ep>/<Cluster>/<Attribute>` (names from the
    definitions or hex ids; wildcards `*` where the IM allows),
    `write <node> <path> <json>`, `invoke <node> <ep>/<Cluster>/<Command>
    [<json>] [--timed ms]`, `subscribe <node> <path…> [--min --max]`
    streaming reports as JSON lines until Ctrl-C, `events <node> …`.
  - `group create <id> --key <hex|random> --nodes <n,n…> --endpoints …`
    (GroupKeyManagement + Groups on each node, the sender's key set),
    `group send <id> <Cluster>/<Command> [<json>]` through
    `groups.Manager.Sender` — mirror `TestLiveGroupSend`.
  - `bind <source node> <ep> --to <target node>/<ep>[/<Cluster>]` and
    `bind … --group <id>` — the Binding write and the ACL entry the
    target needs, as `TestLiveBinding` does.
  - `ota serve --image <file> [--version --vendor --product]
    [--announce <node>]` — a `node.Node` with `Config.OTAProvider`
    from the live test's image source, consent on the console,
    `AnnounceOtaProvider` to the node; runs until Ctrl-C.
  - `README.md` in the package: every command with one worked example,
    the database layout, what leaves the host when `--dcl` is on.
- **D5 — no new dependency; no cgo; the CLI stays under `examples/`**
  (public API terms of `README.md` §API stability do not cover it).

## §1 — the decoders (`secure/setup`)

D1. Tests: matter.js's vectors, round trips, every invalid passcode,
the wrong check digit, a code with a TLV tail.

## §2 — the persisted fabric (`controller/ca`, `controller/commissioner`)

D2. Tests: open-close-open gives the same root certificate, fabric id,
root node id and IPK; a live test in `controller/commissioner` (not
parallel) commissions the harness node, closes the stack, opens it from
the same database and reads over CASE without re-commissioning.

## §3 — the JSON codec (`cluster/spec`)

D3. Floors: keep `cluster/spec`'s.

## §4 — the CLI and its proof (`examples/reference-controller`)

D4. The proof is in-repo: `TestReferenceController` (not parallel)
starts the reference-bridge's node composition in-process (import what
`node/` live tests import — not the `reference-bridge` package) and
drives the CLI's `run(args, stdin, stdout)` through `pair` (manual code
from `setup.ManualCode`), `nodes`, `read`, `write`, `invoke`,
`subscribe` (one report then cancel), `group create`+`send`, `bind`,
`unpair`; `ota serve` is covered by a test that starts it against the
`node/` OTA requestor as `TestLiveOTAProvider` does, if the test fits
in the package without the harness — otherwise say so. Floor
`examples/reference-controller` 70.

## Report

As AK–AS: per §, guards with HEAD, lines that differ from the files
(PairingCodeSchema.ts, FabricManager.ts, chip-tool's parser),
decisions, left out, questions, `by_design` candidates (not written),
and the README's command list verbatim (the owner's test plan quotes
it).
