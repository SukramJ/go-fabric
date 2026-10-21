# Report BZ — the deprecated surface removed (owner decision 2026-10-21)

Branch `m4/bz` from main `3cdb91ab` (after #209 merged). Implementer: Opus,
one agent. Commits: the brief copy with the forwarders (`768ebd52`), the
`cluster/wire` requests and the switch wrapper (`97d84154`), the clock forms
(`87963548`), `secure/mattercert` (`470fc6ae`, deletion only), the bridge's
PASE triplet API (`9d529623`), `secure/spake2` (`f2612448`, deletion only),
the remaining items (`00b8ccef`), `device.WithOptional` (`21f81fde`), README /
migration guide / CHANGELOG (`3416d1eb`), the orphaned decoder errors and the
regenerated snapshots (`7be42a80`), this report. Nothing else in `secure/` or
`commissioning/` moved. No chip-tool label: no daemon composition or
FeatureMap changed (the reference bridge's energy devices read the same clock
through a `clock.Clock` adapter).

## Removed

Every row has its line in `docs/guides/migrating-to-the-facade.md`, section
[Removed in v0.10.0: the deprecated surface](../../docs/guides/migrating-to-the-facade.md#removed-in-v0100-the-deprecated-surface)
(anchor in the last column), the WithOptional row in the section before it.

| Symbol | Replacement | Guide |
| --- | --- | --- |
| `controller/discovery`, `exchange`, `handshake`, `interaction`, `peer` (forwarders), `script/bx4-forward` | `client/<name>` | `#controller-forwarding-packages-brief-bx4` |
| `wire.MoveToHueRequest`, `MoveHueRequest`, `StepHueRequest`, `MoveToSaturationRequest`, `MoveSaturationRequest`, `StepSaturationRequest`, `MoveToHueAndSaturationRequest`, `MoveToColorTemperatureRequest` | `cluster/spec/colorcontrol.<same>` | `#clusterwire-request-types-brief-bx1-and-the-switch-wrapper-brief-bk` |
| `wire.GoToLiftPercentageRequest`, `GoToTiltPercentageRequest` | `cluster/spec/windowcovering.<same>` | same |
| `wire.SetpointRaiseLowerRequest` | `cluster/spec/thermostat.SetpointRaiseLowerRequest` | same |
| `wire.MoveToRequest` | `cluster/spec/closurecontrol.MoveToRequest` | same |
| `wire.ChangeToModeRequest` | `cluster/spec/<mode>.ChangeToModeRequest` | same |
| the twelve `wire.Decode…` of those requests; `wire.ErrColorControlMalformed`, `wire.ErrThermostatMalformed` (their errors, orphaned) | the bridge decodes through the generated definition | same |
| `wire.GenericSwitch`, `wire.NewGenericSwitch` | `cluster/switch` (`switchserver.New`) | same |
| `btp.Clock`, `btp.Timer`, `btp.SystemClock`, `btp.Options.Clock` | `btp.Options.TimeSource` | `#clock-forms` |
| `onoff.Clock`, `onoff.Config.Clock` | `onoff.Config.TimeSource` | same |
| `core.OtaClock`, `core.OTARequestorConfig.Clock` | `OTARequestorConfig.TimeSource` | same |
| `Now` of `core.OTAProviderConfig`, `core.CommissionerControlConfig`, `closure.DimensionConfig`, `energy.CommodityTariffConfig`, `energy.DeviceEnergyConfig`, `energy.EvseConfig`, `ca.Config` | the `Clock` field beside each | same |
| `cluster.Quieter.Now`, `Quieter.AfterFunc` | `Quieter.Clock` | same |
| `mattercert.SystemTime` | `clock.System` | same |
| `spake2.MRPParameters`, `PBKDFParamRequest.InitiatorMRPParams`, `PBKDFParamResponse.ResponderMRPParams` | `sessionparams.Parameters`, `InitiatorSessionParams`, `ResponderSessionParams` | `#session-parameters` |
| `bridge.PaseAdapter.SetResponderMRPParams`, `PeerMRPParams` | `SetResponderSessionParams`, `PeerSessionParams` | same |
| `node.Node.Fabrics` | `Node.FabricInfos` | `#others` |
| `ble.ErrScannerClosed` | none needed (nothing returned it) | same |
| `store.DiagnosticsRecord`, `Store.LoadDiagnostics`, `SaveDiagnostics` | the facade's settings rows | same |
| `mrp.Retransmitter`, `NewRetransmitter`, `SendFunc`, `TickResult` | none in `mrp` (the bridge's tracker; `BackoffDuration`) | same |
| `modeselect.ModeOptionField…`, `SemanticTagField…` | the generated codec | same |
| `device.WithOptional` for a cluster with a generated package | `<cluster>def.Declare{}` | `#removed-in-v0100-the-declaration` |

`script/archcheck/layers.json` has no exception left (`cluster/wire →
cluster/switch` is gone with the wrapper). README §API stability: the table
holds one row (Kept below) and the suspension paragraph; `docs/architecture.md`
says the same. CHANGELOG `[Unreleased]`: six `### Removed` lines, one
`### Changed` line for the policy; the Unreleased `### Deprecated` section
(BX1, BX4) is dropped — those marks were never released.

## Kept

| Symbol | Why |
| --- | --- |
| `store.SettingUniqueIDRotated` | no replacement: the node reads it once as the issue-#122 migration (`node/persist.go` `uniqueID`) and clears it; removing the key removes the migration. Question 1 |
| the `matter_diagnostics` table | schema, not API: created and seeded, read by nothing; dropping it is a migration. Question 2 |
| `device.WithOptional` for a cluster without a generated package | the addition (a): kept beside the narrowed `WithServer`. See Differences 4 |
| `wire.MoveToLevelRequest`, `MoveRequest`, `StepRequest`, `StopRequest`, `wire.FanStepRequest`, the FanControl port forms | BU-b's (the self-held servers still use them); not marked |
| `wire.GoToLiftValueRequest`, `GoToTiltValueRequest`, their decoders; `wire.GenericSwitchSource` and its capabilities, the `wire.Switch…Event` aliases | not deprecated; the materialiser reads the sources |

## Guards

| Guard | HEAD | exit |
| --- | --- | --- |
| `make ci` (build, vet, archcheck — no exception left —, test, lint, cover-check, bluez-check) | `fb6ed4db` | 0 (the run at `7be42a80` stopped at lint, 2: a test variable `clock` shadowing the package in `secure/mattercert/verify_test.go`, fixed in `fb6ed4db`) |
| `make race` | `7be42a80` | 0 |
| `make reachability-check` | `7be42a80` | 0 (unreached 5, ratchet unchanged) |
| `make cite-check` (`MATTERJS_DIR=../matter.js`) | `7be42a80` | 0 (drift snapshot regenerated in `7be42a80`) |
| `make testparity-check` | `7be42a80` | 0 (no register entry named a removed test) |

`fb6ed4db` changes one test variable name after the `7be42a80` run; race,
reachability, cite and testparity were not rerun for it. Coverage floors
held without an edit.

`mkdocs build --strict`: exit 0.

## Differences

1. **Raw TLV refused.** ClosureControl's MoveTo and Thermostat's
   SetpointRaiseLower took a `[]byte` payload as a deprecated in-process
   form (their doc comments said so); it went with the decoders. The
   payload-shape tests now list the raw TLV under the refusals.
2. **Removed beyond the markers:** `mrp.SendFunc`, `mrp.TickResult` (the
   Retransmitter's only), `wire.ErrColorControlMalformed`,
   `wire.ErrThermostatMalformed` (only the removed decoders returned them;
   reachability counted them unreached), and unexported constants the lint
   found orphaned. Each has its line in the guide.
3. **Internal seams.** onoff's and the OTA requestor's timer interfaces
   became unexported (`timerClock`, `otaClock`) behind the same
   `portClock` adapter; btp keeps a `clock.Clock` directly. Resolution is
   `clock.Or(cfg.TimeSource|Clock)` everywhere.
4. **`WithOptional` narrowed** (refuses a cluster `spec.Lookup` knows, naming
   `<cluster>def.Declare{}`; test `TestWithOptionalTakesOnlyAClusterWithoutAPackage`).
   What it still accepts is a device type's optional cluster without a
   definition — RootNode's 0x0035, 0x0801, 0x0802 and
   SecondaryNetworkInterface's 0x0035 — where `WithServer` alone already
   mounts the host's server. Device tests that walk clusters by id use an
   internal `withOptional` (the untyped form, like `withFeatures`). Question 3.
5. **Tests that existed only for a deprecated form** were removed (the wire
   decoder and wrapper tests, `TestQuieterDeprecatedNowAndAfterFunc`,
   `TestDeprecatedSystemClockFires`, the ca `Now` case,
   `TestPaseAdapterLegacyTripletSettersStillWork`, the Retransmitter tests,
   the diagnostics tests). Wire-shape tests moved to the replacement
   (spake2 round trips and the uint32 width on `ResponderSessionParams`;
   `TestEmptySessionParamsOmitTag5` replaces the triplet-equality test;
   `TestMRPBackoffExponent_MaxZeroOnFirstRetransmit` on `BackoffDuration`).
6. **Reachability.** `wire.GenericSwitchPositionSource` was reached only
   through the wrapper's test; the measurement test's `stated` helper now
   takes it (unreached stays 5, ratchet unchanged).
7. The README table is not empty: `store.SettingUniqueIDRotated` is still
   deprecated and was never listed; it is now.

## Questions

1. `store.SettingUniqueIDRotated`: end the issue-#122 migration (remove the
   key and the read; a store still flagged would then serve its salted id)
   or keep it until loom's stores have booted once on v0.5.0 or later (the release that added the migration)?
2. `matter_diagnostics`: drop the table with a schema migration, or keep it?
3. `WithOptional`: with the narrowing its accepted uses are redundant with
   `WithServer`. Remove it outright?

## by_design candidates

None. `by_design.md` had two sentences on the removed forms
(`wire.GenericSwitch`, `device.WithOptional` setting `Spec.OptIn`); both
updated in place.
