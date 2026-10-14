# Report AT — the reference controller CLI, pairing-code decoders, a persisted controller fabric (v0.4.0)

Branch `wave26/controller-cli` from `planning/briefs-wave-26` (789cb4b), pushed, no PR.
matter.js read at `0ceb202d`, connectedhomeip at the harness pin `6170af84`;
every `// Mirrors`/citation comment cites lines read there. PCS below is
`packages/types/src/schema/PairingCodeSchema.ts`. Each commit carries its
regenerated reachability inventory (unreached stays **5**, ratchet untouched).
No chip-tool binary was run.

| Commit | Item |
| --- | --- |
| 6510c83 `feat(setup): QR and manual pairing-code decoders` | §1 |
| 6b751dd `feat(controller): a persisted controller fabric` | §2 |
| 644cef7 `feat(spec): a JSON codec over the generated definitions` | §3 (+ lint fixes of §1/§2 code: G115 notes, named results) |
| 7e6dca5 `feat(examples): the reference controller CLI` | §4 |
| fbf549c `fix(setup): keep a QR tail DecodeExtra refuses as raw bytes` | §1 fix-up (difference 1 below) |
| (this commit) `notes: report AT` | — |

## Per section — what was built, what proves it

- **§1 `secure/setup/decode.go`** (floor 84, measured 97.4).
  `DecodeQRCode` / `DecodeQRCodes` (the `*`-joined multi form) = PCS
  `decodeInternal` :330-345 + the default validation (Schema.ts:18-25):
  version 0 and `isValidPasscode` (:279-286), `assertValidPayloadIdentity`
  (:217-230, vendor ≤ 0xFFF4 per VendorId.ts:21-26); `base38Decode` =
  Base38Schema.ts:44-101 incl. the group-overflow check. The TLV tail is
  `Payload.Extra` (`Raw` + parsed fields); `DecodeExtra` = `decodeTlvData`
  :354-377 (tag ranges :248-275, default fields :122-142, PBKDF
  both-or-neither :238-242, serial number string-or-uint :358-375), vendor
  elements 0x80-0xFF kept as encoded bytes in `Extra.Vendor`.
  `DecodeManualCode` → `ManualPayload{ShortDiscriminator, Passcode,
  VendorID, ProductID, HasVendorProduct}` = :461-493 (non-digits stripped,
  first digit ≥ 8 refused, Verhoeff :470, VID_PID_PRESENT vs length
  :473-482, 11/21 digits). `IsValidPasscode` = :171 (alias of the existing
  `IsValidSetupPIN`, same list :160). `QRCode` now appends `Extra.Raw` and
  refuses > 255 characters (:308-318, `MaxQRCodeLength`).
  Proofs: `decode_test.go` — matter.js vectors from
  `packages/types/test/schema/PairingCodeSchemaTest.ts` with their lines
  (QR :27-49 incl. tail and multi, :168-226 rejects, :137-159 length, TLV
  :230-350, manual :56-81, :396-485), round trips against both encoders,
  every invalid passcode through both decoders, wrong check digit, a code
  with a TLV tail (serial + vendor element); `FuzzDecodeQRCode`,
  `FuzzDecodeManualCode`.
- **§2 `controller/ca/persist.go`, `controller/commissioner/fabric.go`**
  (ca 90.0, commissioner unchanged floor). `ca.FabricRecord` +
  `ca.FabricStore`; `ca.SettingStore{Settings, Key}` is a `ca.Store` and a
  `ca.FabricStore` over `*store.Store`-shaped `ca.Settings` (CA as JSON under
  `Key` with CertificateAuthority.ts:448-474 field names, the record under
  `Key+".fabric"`); `ca.MemoryStore` is a `FabricStore` too.
  `commissioner.FabricConfig.Store`: `NewFabric` loads the record (the load
  half of FabricManager.ts:66-69) or mints and saves — **commissioner
  changed: yes**, the "load or mint" path lives in `NewFabric`
  (`loadFabric`, `buildFabric`, `fabricRecord`, `ErrFabricMismatch`).
  Proofs: `TestSettingStoreOpenCloseOpen` (same root cert/key, ICAC,
  counter), `TestNewFabricLoadsOrMints` (same ids, IPK, key, NOC;
  mismatches), and the live `TestFabricPersistsAcrossRestart` (not
  parallel): commissions the harness node, closes the stack, reopens from
  the same sqlite file, CASE + Read of NodeLabel without re-commissioning.
- **§3 `cluster/spec/json.go`** (floor 97, measured 97.4).
  `EncodeJSON(field, raw)` / `DecodeJSON(field, node)` over `spec.Type`,
  plus `AttributeField`/`CommandField`/`EventField`; a nil field is chip-tool's
  untyped `CustomArgument` form. Rules cite ComplexArgument.h:60-330 and
  CustomArgument.h:32-139, CustomStringPrefix.h:28. Proofs:
  `TestJSONRoundTripEveryDefinition` — all 99 committed definitions, 2468
  values (every attribute, command and event payload, two variants:
  names/max values and numbers/null/ids) through JSON → node → wire →
  JSON → node; `TestJSONChipToolFixtures` (TestBinding.yaml:65-70,
  TestCluster.yaml:734); rejects and forms tests.
- **§4 `examples/reference-controller`** (floor 70 added, measured 83.8).
  `run(args, stdin, stdout)`; files main/stack/pair/interact/group/bind/
  ota/self/paths.go + README.md. Stack order of
  registry_live_test.go:82-162. Proof: `TestReferenceController` (not
  parallel) — in-process node (plug + OnOffLightSwitch on the Aggregator,
  the `node/` live tests' imports), a fake DNS-SD responder injected through
  `hooks.names`, each command a separate `run` on one database: pair by
  `setup.ManualCode` (+ `--label`), nodes, read, write, invoke (+ timed),
  subscribe `--count 1`, events, node, group create + group send (Toggle
  flips the plug), bind `--to` and `--group` (switch's Binding has both
  entries), unpair (node's fabric table empty). `TestReferenceControllerOTA`
  fits in the package: `ota serve --announce --once` serves an image to the
  node's harness-mode OTA requestor (Querying → Downloading → Done, node runs
  version 2), consent "y" on stdin. `TestArguments` covers the argument
  errors.

## Guards

All logged with HEAD before/after (`bin/guard-*.log` in the worktree):

- `make ci` at `7e6dca5` (§4 head) — **green** (lint 0 issues; floors:
  `examples/reference-controller` 83.6–83.8 vs 70, `secure/setup` 97.4 vs 84,
  `cluster/spec` 97.4 vs 97, `controller/ca` 90.0).
- At `fbf549c` (final code head): `make race` **green**; `make
  reachability-check` **green** (unreached 5); `make ci` failed once only on
  `node` `TestSessionAndSubscriptionDiagnostics` ("Commission: commissioner:
  Could not connect to device", issue87_test.go:308) — rerun alone per the
  brief's rule, 3/3 **pass**; lint 0 issues in that run. The only change
  between the green `make ci` and this one is `secure/setup` (fbf549c).

## Lines that differ from the cited files

PairingCodeSchema.ts:

1. `decode` keeps the TLV tail unparsed (:342); `DecodeQRCode` parses it with
   `DecodeExtra` when it decodes and keeps it as `Extra.Raw` only when it
   does not (fbf549c; first version refused such a code — matter.js's own
   vector `MT:YNJV7VSC00CMVH7E4810AK00` has tail `010203`).
2. Unspecified vendor/product: matter.js reports `undefined` (:188-200); Go
   keeps `0` (the zero value is "unspecified").
3. `QRCode` (existing encoder) still allows versions 1-7 and the trivial
   passcodes (matter.js `encode` validates version 0 and `isValidPasscode`,
   :279-286); only the 255-character limit was added. Not tightened: the
   daemon's config validation calls it.
4. `DecodeQRCode` (single) refuses a `*`-joined code; `DecodeQRCodes` is
   matter.js's array.
5. No `validate=false` mode (matter.js `decode(code, false)`).
6. No 21-digit manual encoder (brief: decoders only); the 21-digit form is
   tested with matter.js's vectors.

FabricManager.ts / Fabric.ts:

7. One fabric per store, no `"fabrics"` list, no `nextFabricIndex`. Kept from
   `Fabric.Config`: fabricIndex, fabricId, rootNodeId (= the controller's
   nodeId), rootVendorId, identityProtectionKey, label, keyPair,
   operationalCert, and rootPublicKey as a check against the CA. Dropped:
   rootCert/intermediateCACert (the CA's `Stored` holds them),
   vidVerificationStatement/vvsc, globalId and operationalIdentityProtectionKey
   (derived), nextFabricIndex. D2 listed the record without key and NOC; I
   kept them so a restart is the same operational identity (no NOC re-mint,
   no certificate-counter growth per run); a record without them mints and
   saves them.
8. First-run ids follow FabricAuthority.ts:133-171: random 64-bit fabric id
   (:155), random operational root node id (:134), vendor 0xFFF1 (:47).

chip-tool parser (ComplexArgument.h / CustomArgument.h):

9. Enums by value name and bitmaps as `{"member": true|n}` — extensions (chip
   takes the underlying integer only, :105-133).
10. `base64:` accepted for typed octet strings (chip's typed parser takes
    `str:`/`hex:`/bare hex, :196-249; `base64:` is in CustomStringPrefix.h:28
    but used only for the JSON-arguments envelope, Commands.cpp:612).
11. A struct's FabricIndex (254) may be left out though mandatory: chip's
    encoder omits it for writes (kIsFabricScoped, CustomArgument.h:292-294
    shows the flag); the server sets it.
12. Struct keys match case-insensitively and also by decimal field id (chip's
    generated Setup uses the exact camelCase label; the id form is
    CustomArgument's :134-139). Output keys are camelCase per matter.js's
    camelize.
13. Signed integers are JSON numbers only (as chip :92-103); unsigned also
    `"0x…"`/decimal strings (:60-90). Floats NaN/Inf render as strings.

## Decisions

- **The tool's own node.** `group send` and `ota serve` need a fabric
  member (groups.Manager.Sender, the OTA provider); per the brief's "mirror
  the live tests", the tool runs a `node.Node` in the same database
  (passcode/discriminator/salt/node id in `controller.self`), commissions it
  into its own fabric on first use (attestation accepted: CSA test chain),
  always with the OTA provider endpoint so its layout is stable, advertising
  with Zeroconf. `group create` also gives it the key set as a sender-only
  Groupcast member. It is reached through `peer.AddKnownPeer` with its
  socket; the commissioner's post-commissioning CASE still needs DNS-SD
  (works with real mDNS; the test injects it via `selfHooks.started`).
- **Groups via Groupcast.** `group create` = KeySetWrite + Groupcast
  JoinGroup with the auxiliary ACL flag, exactly TestLiveGroupSend. The
  pre-1.5 path (GroupKeyMap + Groups.AddGroup + a group ACL entry) is not
  implemented (see Questions).
- **bind** reads the fabric-scoped list (Binding, ACL, GroupKeyMap), appends,
  writes the whole list (`DisableListChunking`); a GroupKeyMap entry for the
  group is not added twice (JoinGroup may have written it — the second one
  was refused with CONSTRAINT_ERROR).
- **OTA consent** is asked in `QueryImage` before the offer (or `--yes`),
  not via `mattercore.OTAConsent`: matter.js asks that port only for a
  requestor that cannot consent and discontinues at ApplyUpdate an update
  whose consent it does not hold (OtaSoftwareUpdateProviderServer.ts:243-250,
  :430-437) — with `ConsentRequired` the harness requestor ended Cancelled.
- **DCL only for `pair`**: other commands use the embedded test PAAs and
  never open the ledger store (each open runs an update).
- `--label` writes `BasicInformation.NodeLabel`; `--node-id` added (chip-tool
  takes the node id too). `nodes` reachability = a NodeLabel read within
  `--timeout` (5 s default there). Extra flags: `--listen`, `--node-listen`,
  `--timeout`, `subscribe --count`, `ota serve --once/--yes`, `group create
  --keyset`. Global flags may follow the command.
- `cluster/spec`'s test imports `controller/client` (blank) to link every
  generated definition, as known.go does.
- README.md: there was no examples section; added `## Examples` (one
  paragraph) and named the controller in §API stability's "not API" sentence.

## Left out

The Groups-cluster (pre-1.5) group provisioning; a 21-digit manual encoder;
persistent CASE resumption (in memory, per D2); commissioning-window
opening on paired nodes; BLE/Wi-Fi; TUI/daemon; Joint Fabric;
CommissionerControl client; media (all per brief).

## Questions

1. Should `group create` fall back to GroupKeyMap + Groups.AddGroup + a
   Group-auth ACL entry when a node answers UNSUPPORTED_CLUSTER for Groupcast
   (devices before 1.5)? Not done: no in-repo proof possible today.
2. D2's record omitted the operational key and NOC; I kept them (decision
   above). Keep, or re-mint the NOC each run?
3. `QRCode` still accepts what matter.js's `encode` refuses (difference 3) —
   tighten in a later wave?
4. `cluster/spec` has no enumeration API; the test iterates
   `schema.ClusterNames` + `spec.Lookup`. Worth a `spec.All()`?

## by_design candidates (not written)

Session resumption in memory only (D2); one controller fabric per database;
operational key + NOC kept in the fabric record; enum-by-name / bitmap-object /
`base64:` JSON extensions; FabricIndex omissible on encode; OTA consent asked
at QueryImage; the tool's own node for group send and OTA.

## The README's command list (verbatim)

## Commands

- `pair --code <MT:… | 11 or 21 digits> | --passcode N --discriminator N [--node-id N] [--label L]`
  Finds the device over DNS-SD by its discriminator (long from a QR code or
  `--discriminator`, short from a manual code), runs PASE, checks its
  attestation, installs the NOC, keeps the node in the registry and prints
  its node id and device view (each endpoint's device type and clusters).
  `--label` writes `BasicInformation.NodeLabel`.

  ```sh
  reference-controller pair --code 34970112332 --node-id 0x4321 --label kitchen
  ```

- `unpair <node>` — RemoveFabric on the node, then its record is deleted.

  ```sh
  reference-controller unpair 0x4321
  ```

- `nodes` — every paired node: node id, local id, label, whether it answers
  a read within `--timeout` (5 s here unless given), and the conformance
  verdict of its image.

  ```sh
  reference-controller nodes
  ```

- `node <node>` — reads every attribute and prints the node's full image:
  endpoints, device types, conformance, clusters, attribute values.

  ```sh
  reference-controller node 0x4321
  ```

- `read <node> <ep>/<Cluster>/<Attribute>…` — one JSON line per attribute.

  ```sh
  reference-controller read 0x4321 0/BasicInformation/NodeLabel '*/OnOff/OnOff'
  ```

- `write <node> <ep>/<Cluster>/<Attribute> <json> [--timed ms]`

  ```sh
  reference-controller write 0x4321 0/BasicInformation/NodeLabel '"pantry"'
  ```

- `invoke <node> <ep>/<Cluster>/<Command> [<json>] [--timed ms]` — prints the
  status and the response's fields as a JSON line.

  ```sh
  reference-controller invoke 0x4321 1/LevelControl/MoveToLevel '{"level": 128, "transitionTime": 10, "optionsMask": 0, "optionsOverride": 0}'
  ```

- `subscribe <node> <path>… [--min s] [--max s] [--count n]` — attribute or
  event paths; every report as JSON lines until Ctrl-C (or `--count`
  reports).

  ```sh
  reference-controller subscribe 0x4321 1/OnOff/OnOff --max 30
  ```

- `events <node> <ep>/<Cluster>/<Event>…` — reads events, one JSON line each.

  ```sh
  reference-controller events 0x4321 0/BasicInformation/StartUp
  ```

- `group create <id> --key <hex|random> --nodes <n,n…> --endpoints <e,e…> [--keyset N]`
  — writes the key set (GroupKeyManagement KeySetWrite) on each node and
  joins the group on its endpoints with Groupcast JoinGroup and the
  auxiliary ACL entry; the controller's own node gets the key set as a
  sender-only member. The key set id defaults to the group id.

  ```sh
  reference-controller group create 257 --key random --nodes 0x4321,0x4322 --endpoints 1
  ```

- `group send <id> <Cluster>/<Command> [<json>]` — a group command from the
  controller's own node.

  ```sh
  reference-controller group send 257 OnOff/Toggle
  ```

- `bind <node> <ep> --to <node>/<ep>[/<Cluster>]` — adds the Binding entry on
  the source endpoint and the ACL entry on the target that grants the source
  Operate on the target endpoint (and cluster).
  `bind <node> <ep> --group <id>` — adds a group Binding entry and gives the
  source the group's key set and GroupKeyMap entry.

  ```sh
  reference-controller bind 0x4322 1 --to 0x4321/1/OnOff
  ```

- `ota serve --image <file> [--version N] [--vendor N] [--product N] [--announce <node>] [--once] [--yes]`
  — serves the image from the own node's OTA provider to requestors of its
  vendor and product below its version (defaults from the image header);
  each offer is asked on the console unless `--yes`; `--announce` sends
  AnnounceOtaProvider to a node. Runs until Ctrl-C, or `--once` until one
  update is done.

  ```sh
  reference-controller ota serve --image light-v2.ota --announce 0x4321 --once
  ```

