# Report CJ: the controller facade gaps (Q1–Q19, H1, H2, ICD keys)

Branch `m5/controller-facade` from `v0.12.0` (269a3514). Brief:
`notes/briefs/2026-10-25-CJ-controller-facade-gaps.md`. Version target
**v0.12.1**: additions and behaviour changes only, no Break (`Config.DB`
stays required; `controller.OpenDB` is the opener). matter.js read at
`862ad75f` only.

## §1 What landed

| § | Symbol(s) | File | CLI site moved | Test | Commit |
| --- | --- | --- | --- | --- | --- |
| 1.1 | `controller.Config.Clock`, `Config.Listener`; `fabrictest.Site.ControllerConfig` | `controller/controller.go`, `fabrictest/controller_config.go` | — (the CLI composes its own stack) | `controller/site_test.go` `TestControllerOnASite` | 48643794 |
| 1.2 | `client.Value`, `client.Encodable`, `client.Field` (`AttributeField`, `CommandField`, `ResponseField`, `EventField`, `Typed`), `client.Form` (`TagBased`, `NameBased`), `client.EncodeJSON`, `client.DecodeJSON`, `client.ErrJSONValue`; `spec.PropertyName`, `spec.GlobalAttributeType` | `controller/client/json.go`, `cluster/spec/json.go` | — (the CLI's chip-tool argument form is another form) | `controller/client/json_compat_test.go` (`TestJSONCompat` over 9 matterjs-server fixtures, `TestJSONOnASite`, `TestJSONForms`) | 922d617a |
| 1.3 | `controller.Config.HooksFor` | `controller/controller.go` | — | `controller/facade_test.go` `TestHooksFor` | c9c749c4 |
| 1.4 | `Controller.Discover`, `controller.DefaultDiscoveryTimeout` | `controller/code.go` | — (no `discover` command; `pair`'s scanner stays, see Differences) | `TestDiscover` | c9c749c4 |
| 1.5 | `controller.ParsePairingCode`, `controller.PairingCode` (+ `Identifier`), `controller.ErrPairingCode` (wrapped by `Pair`) | `controller/code.go` | `cli/pair.go` `parseCode` | `TestParsePairingCode` | c9c749c4 |
| 1.6 | `client.Node.OpenCommissioningWindow`, `Node.RevokeCommissioningWindow`, `client.WindowOptions`, `client.CommissioningWindow`, `client.DefaultWindowTimeout`, `client.ErrNoBasicWindow` | `controller/client/window.go` | `cli/window.go` (window open / revoke); its parity port moved | `controller/client/commissioning_window_parity_matterjs_test.go` (moved from the CLI), `window_internal_test.go` | 934d4ff4 |
| 1.7 | `commissioner.ErrAttestationRejected` | `controller/commissioner/errors.go`, `flow.go` | — | `TestPairAttestationRejected` | 3d9c6085 |
| 1.8 | `client.Node.InvokeBatch`, `Node.MaxPathsPerInvoke`, `client.CommandRequest`; `interaction.Interaction.InvokeBatch`, `interaction.CommandRequest`, `interaction.ErrNoCommands` | `client/interaction/invoke_batch.go`, `invoke.go` (shared exchange), `controller/client/interact.go` | — (the CLI has no batch command) | `controller/client/invoke_batch_test.go` (default and MaxPathsPerInvoke 1) | 53e4909d |
| 1.9 | `client.Registry.SetFabricLabel`; `commissioner.Fabric.SetLabel`, `Fabric.CurrentLabel`, `commissioner.MaxLabelLength`, `commissioner.ErrLabel` | `controller/client/label.go`, `controller/commissioner/fabric.go` | — | `set_fabric_label_test.go` | 96544567 |
| 1.10 | `client.Node.SetAutoSubscribe`, `Node.AutoSubscribe` | `controller/client/node.go`, `network.go` | — | `set_auto_subscribe_test.go` | 27c04b91 |
| 1.11 | `controller.OpenDB`, `controller.ProductionTrust` | `controller/open.go` | `cli/stack.go` `openDB`, quickstart-controller | `TestOpenDBAndProductionTrust` | 48643794 |
| 1.12 | `client.SpecVersion` (`String`, `Uint32`), `client.SpecVersionOf`, `client.Properties.Specification` | `controller/client/specversion.go` | — | `specversion_test.go` | b96d32d6 |
| 1.13 | none: `controller/thread.DecodeDataset` already reads it (documented) | docs | — | existing `controller/thread` tests | b96d32d6 |
| 1.14 | `controller.PairOptions.BLE`, `PairOptions.WiFi`, `PairOptions.Thread` | `controller/controller.go` | — (see Differences) | `controller/ble_test.go` `TestPairOverBLE`, end to end over bletest on a site | f577f6a3 |
| 1.15 | `contract.StatusError`, `contract.StatusTimeout` | `contract/status.go`, `endpoint/dispatcher.go` | — | `device/status_error_test.go` | 8f443dec |
| 1.16 | `contract.IdentifyHandler`, `device.Device.Identify`, `Device.MatterIdentify`/`MatterTriggerEffect`, `cluster/core.Identify.SetHandler`; `identifydef.Declare{HandleIdentify, HandleTriggerEffect}` accepted on a device | `device/identify.go`, `cluster/core/identify.go`, `endpoint/types.go` | — | `device/identify_test.go` | fa2d2ecc |
| 1.17 | none: the `subscriptions.active` gauge, named in the observability guide | `docs/guides/observability.md` | — | `fabrictest/metrics_test.go` `TestSubscriptionsActiveGauge` | 29d92e4b |
| 1.18 | `snapshot.Snapshot.Vendors`, `Snapshot.Vendor`, `snapshot.Vendor`, `snapshot.VendorsFile`, `snapshot.FetchVendors`; `data/vendors.json` (882 vendors, fetched 2026-10-25); `script/dclsnapshot -vendors-only` | `controller/dcl/snapshot/vendors.go` | — | `vendors_test.go`, `fetch_test.go` | ac1e7637 |
| 1.19, 1.23 | `fabrictest.Site.AddNode` | `fabrictest/device.go` | — | `fabrictest/addnode_test.go` | 7f2f52b1 |
| 1.20 | PowerSourceConfiguration mounted under a stated BridgedPowerSourceInfo | `device/base.go` | — | `device/bridged_power_source_config_test.go`, `base_test.go` | c0ddd661 |
| 1.21 | the SelectDefaultValue fallbacks and three `initialize` tables for the kit's servers | `device/defaults_fallback.go`, `device/device.go` | — | `device/mandatory_served_test.go` (`TestEveryListedAttributeHasAValue` over every committed type, `TestMandatoryDefaultsOnTheWire`) | c0ddd661 |
| 1.22 | `cluster/cover` serves Lift/Tilt without PositionAware | `cluster/cover/windowcovering_server.go` | — | `TestLiftWithoutPositionAwareness`, `TestFeatureMapIsDerivedFromWhatTheServerServes`, `device/cover_lift_only_test.go` | 33b58389 |
| 1.24 | the testing guide's sentence | `docs/guides/testing-in-process.md` | — | `fabrictest/removed_endpoint_test.go` | 04d836af |
| 1.25 | `store.SettingICDKey`; sealed `icd_key`, `store.Upgrade` seals clear rows | `store/icd_cipher.go`, `store/icd.go`, `store/schema.go` | — | `store/icd_cipher_test.go` | 972a1359 |

## §2 Docs, tests

- `docs/getting-started/controller.md`: the `Config` bullets (OpenDB,
  ProductionTrust, Clock/Listener), the attestation sentinel and the offline
  vendors, and a section "More of the facade" (hooks, JSON values,
  discovery, pairing codes, commissioning window, batched invokes, fabric
  label, auto-subscribe, specification version, BLE pairing, Thread
  datasets). `docs/getting-started/device.md`: status errors, Identify, the
  state defaults. `docs/guides/testing-in-process.md`: "A program's own
  composition" (ControllerConfig, AddNode) and the ReadCluster sentence.
  `docs/guides/observability.md`, `persistence.md`, `embed-in-a-daemon.md`,
  `docs/matterjs-comparison.md` (controller row, batched invoke row).
- `THIRD-PARTY-NOTICES.md`: the matterjs-server fixtures and the DCL vendor
  records.
- CHANGELOG `[Unreleased]`: `### Changed` (PSC, defaults, cover, ICD
  sealing) and `### Added` per group. No migration-guide line (no Break).
- Regenerated: `script/reachability` (3 unreached, on its ratchet),
  `script/citecheck` (0 hard errors, drift 262 → 259, the ratchet lowered
  in the same commit), the testparity page. Commits e97bbdbd, 5ed7c024.
- Guard fixes: b9e7ed7f (the BLE scan on the controller's clock, the label
  conflict as a NOCResponse status, error prefixes, slog keys), d0ad60f7
  (lint: `OpenDB` opens on first use, the initialize defaults in
  `generatedConfig`), 211d55b4 and 6886cb80 (coverage of the new code in
  `client/interaction`, `contract`, `store`, `fabrictest`).

## Guards

`df -h /` before the runs: 75 GB free (70 %); `/tmp` is a 7.6 GB tmpfs,
so the final runs used a private `GOCACHE`/`GOTMPDIR` under `~/.cache`
(one run died of "disk quota" in `/tmp`, another of a `go clean -cache` from
a concurrent session).

| Guard | HEAD | Result |
| --- | --- | --- |
| `make ci` (build, build-32bit, vet, archcheck, apidiff-check, test, lint) | 6886cb80 | pass up to `cover-check`; `cover-check` failed only on the FAIL line of `TestIcdClientParity` (below) |
| `make cover-check`, `make bluez-check` (the rest of `ci`) | 6886cb80 | pass |
| `make race` | 6886cb80 | pass (two earlier runs under load failed in `bridge` `TestReportSubscriptionChunksAnOversizedOngoingReport` and `cluster/core` `TestICDManagementRegistrationLifecycle`, packages this bundle does not touch; both pass alone, `-race -count=5`) |
| `make reachability-check` | 5ed7c024 | pass (3 unreached, on the ratchet) |
| `make cite-check MATTERJS_DIR=…/matter.js` | 5ed7c024 | pass (0 hard errors; drift 259, ratchet lowered from 262) |
| `make testparity-check` | 5ed7c024 | pass |
| `go test ./script/...` | 5ed7c024 | pass |
| `mkdocs build --strict` | 5ed7c024 | pass |
| `make stress-flakes` | d0ad60f7 | pass |
| `make chiptool-test` | 5ed7c024 | pass (`TestChipToolSuite`, `TestChipToolCommissionsReferenceBridge`, the Go controller cases; `TestMatterJSController` skips as before) — run because §1.20 changes the reference bridge's composition |

`TestIcdClientParity/availability` (the SIT→LIT flip cases, a 180 s wait)
flakes under the shared machine's load on this branch and on `v0.12.0`
itself: a detached `v0.12.0` worktree failed it 2 of 10 runs with the same
"waiting for the flip" message, so it is pre-existing, not this bundle's.
`make race` ran once green, not twice as the brief asks, and `make
archcheck` ran inside `make ci`.

## Differences from the brief

- **1.1** `Config` gained `Clock` and `Listener` only; DNS-SD stays the
  existing `Names` func, and there is no `Exchanges`/`Resolver` field: the
  controller builds its exchange manager over the listener. A controller of
  `ControllerConfig` is not an operational peer the site's devices resolve
  (subscription re-establishment and Check-In reach `AddController`'s only).
- **1.2** `DecodeJSON` takes the `Field` too (`DecodeJSON(field, value,
  form)`): rendering by name and typing bytes and nullable values needs the
  type. A mandatory member a command's fields leave out takes matter.js's
  `MandatoryDefaultValue` fallback, because matterjs-server's documented
  `moveToLevelWithOnOff {"level","transitionTime"}` omits the mandatory
  OptionsMask/OptionsOverride (by_design candidate below). The forms are
  not cited by line: no matterjs-server clone is local; the fixtures are the
  reference. go-fabric-server's `internal/wire` exception can go in its S2
  bundle: `client.AttributeField/CommandField/EventField` with
  `EncodeJSON`/`DecodeJSON` replace its `Encode`/`Value`/`Fields`; its
  FeatureMap-of-an-unknown-cluster `{}` quirk stays the server's.
- **1.4, 1.14** The reference CLI composes its own stack (not
  `controller.New`), so the `Controller` methods (`Discover`, `Pair`'s BLE
  route) are not its route; `pair`'s DNS-SD and BLE scans stay in
  `cli/pair.go`. The package- and node-level routes moved (ParsePairingCode,
  OpenCommissioningWindow, RevokeCommissioningWindow, OpenDB).
- **1.4** `Discover` returns every device found until the timeout (matter.js
  `discoverCommissionableDevices`), not the first, unlike
  `fabrictest.Controller.Discover`.
- **1.6** The vendor and product id come from the image; an unseeded node
  reads them first (matter.js refuses an unseeded node).
- **1.8** The brief says `client/interaction` already split invokes; it did
  not (its package doc said "Invoke batching and splitting … not ported").
  The split is new in `client/interaction`; the single-command path shares
  the exchange code.
- **1.9** matter.js's `updateFabricLabel` returns after the first connected
  node it updated (CommissioningController.ts:996-998); every connected
  node is updated here.
- **1.11** `attestation.Production()` is not possible: `attestation.Production`
  is a `TrustMode` constant and `controller/dcl/snapshot` imports
  `attestation`; the opener is `controller.ProductionTrust()`.
- **1.12** Additive: `Properties.SpecificationVersion` keeps `*uint32`;
  `Properties.Specification()` returns the `SpecVersion` (the field type
  change would be a Break).
- **1.13** No `commissioner.ParseThreadDataset`: `thread.DecodeDataset`
  exists and is the one shape.
- **1.16** Not `Device.Identify()` as the assembler's server: the assembler
  keeps `cluster/core.Identify`; `Device.Identify()` is a typed holder of the
  host's two handlers, which the assembler's server calls through
  `contract.IdentifyHandler` before its own handling.
- **1.17** No new observer; the gauge is the hook (decided, documented).
- **1.20** The brief's fix (a deprecated element is never required) is not
  matter.js's: `Conformance.ts:937-940` at the pin skips a trailing `D` and
  lets the terms before it decide, so `BridgedPowerSourceInfo, D` is
  mandatory under the condition. The validator stays; the kit, which states
  the condition when a device declares PowerSource, mounts
  PowerSourceConfiguration (Sources `[]`, matter.js's default).
- **1.21** The rule applies to the kit's generated servers and to self-held
  servers that take `SetAttributes`; enums, strings, structs and constrained
  lists without a matter.js fallback are listed host-supplied in the test.
- **1.25** The key is in `matter_settings` (`icd_management.key`), not
  `matter_metadata` (integers only). There is no SQL migration v8 — the
  schema is unchanged; the re-encryption is an `Upgrade` step (Go), and a
  clear 16-byte row is read as it is until then. The storetest round trip
  is unchanged; the at-rest property is `store.Store`'s and tested in
  `store/icd_cipher_test.go`. The by_design entry and ADR 0029 were not
  edited (owner's, see below).

## Decisions

- No Break: v0.12.1. `Config.DB` stays required.
- One shape: `client.Field` handles over the generated definitions instead of
  exposing `spec.Field`; `client.Value`/`Encodable` are the brief's aliases.
- Controller hooks per node through the registry's existing `HooksFor`.
- `SetAutoSubscribe` serialises the default-subscription sync per node
  (`subSync`), the matter.js `subscriptionChanges` queue.
- `Fabric.SetLabel` keeps the exported `Label` field (now the initial label)
  and adds `CurrentLabel`, guarded by a mutex; a store kept for persisting.
- ICD key sealing binds each key to fabric, check-in node and monitored
  subject (AAD), which the DoorLock cipher does not.
- The DCL vendor list is a file of its own (`vendors.json`, matter.js's
  `VendorEntry` seed shape), not part of `SOURCE`.

## Left out

- The reference CLI's DNS-SD and BLE pairing scans (own stack).
- matter.js's batching of single commands within a timer tick.
- Per-cluster `initialize` defaults beyond FanControl, SmokeCoAlarm and
  OccupancySensing.
- A second green `make race` (one green run) — see Guards.

## Questions

- None that block. For the owner: should the reference CLI move onto
  `controller.New` (then `Controller.Discover` and `PairOptions.BLE` are its
  routes)? Symbol needed: none new; a `controller.Config` seam for the CLI's
  CA key file and DCL trust setting.

## by_design candidates (for the owner to write)

- `BD-Matter-ICD-RegistrationsInStoreArea` and ADR 0029's consequence:
  "ICD keys sealed at rest, done in v0.12.1" — matter.js keeps them clear
  (`IcdManagementServer.ts:787-791` at 862ad75f).
- JSON route: a mandatory member a command's fields leave out takes
  `MandatoryDefaultValue` (`packages/model/src/logic/SelectDefaultValue.ts:72-131`
  at 862ad75f), a state rule applied to a request.
- Fabric label: every connected node is updated, where matter.js returns
  after the first (`packages/matter.js/src/CommissioningController.ts:996-998`).
- `commissioner.ErrAttestationRejected`: a sentinel matter.js has no class
  for (`ControllerCommissioningFlow.ts:1306-1315`).
- Identify: the host's handler runs before the assembler's IdentifyTime
  handling, where matter.js's override replaces `identify`
  (`packages/node/src/behaviors/identify/IdentifyServer.ts:129-135`).
- WindowCovering StopMotion on a mixed covering (one axis position-aware):
  matter.js stops only the aware axis (`WindowCoveringServer.ts:487-504`);
  mirrored as is — no divergence, but `BD-Matter-SelfHeldDefault-SelectionNotServed`
  no longer applies to WindowCovering Lift alone, and ADR 0013's
  WindowCovering row ("the one profile … Lift + PositionAwareLift") is
  stale.
- `OpenCommissioningWindow` reads BasicInformation for an unseeded node,
  where matter.js throws (`CommissioningClient.ts:507-515`).
