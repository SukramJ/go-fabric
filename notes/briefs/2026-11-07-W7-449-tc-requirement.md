# Brief — issue 449: the device's Terms and Conditions requirement as a typed result

Date 2026-11-07. Hand-over from go-fabric-ui (wave U7); owner decisions 2026-11-07 (comment on
the issue): the requirement is read in the read-commissioning-info step (no step of its own), the
typed error is returned right after it, before AddNOC; `Deadline` travels as data. Branch
`feat/449-tc-requirement`, one commit per step, one pull request ("Closes #449"); English; no
attribution lines; commit with `git -c user.name=SukramJ -c user.email=sukramj@icloud.com`.
**Model:** Opus; Fable line-reviews the controller/commissioner citations before the merge.
Pins: matter.js db021da0 (`git -C ../matter.js show db021da0:<path>`), connectedhomeip 6170af84
(`git -C ../connectedhomeip show 6170af84:<path>`). Environment and guard rules as in CLAUDE.md.
Read the issue once (`gh issue view 449`) for the host's wording; its chip lines are at 9fb3392c —
the verified lines at 6170af84 are below, cite those.

## Facts verified at the pins

- chip `src/controller/CHIPDeviceController.cpp` at 6170af84: `kReadCommissioningInfo` reads
  GeneralCommissioning's SupportsConcurrentConnection, Breadcrumb, BasicCommissioningInfo,
  RegulatoryConfig, LocationCapability, IsCommissioningWithoutPower (:2592-2603) — **no TC
  attribute**; `kConfigureTCAcknowledgments` (:3946-3954) is skipped when
  `params.GetTermsAndConditionsAcknowledgement()` has no value, else sends SetTCAcknowledgements
  (:3956-3957); `AutoCommissioner.cpp:486-487` orders the stage. `CommissioningDelegate.h:49`.
- matter.js at db021da0: the flow has no TC step (the module's existing citation
  `ControllerCommissioningFlow.ts:558-567` at 862ad75f — re-read at db021da0 and re-pin it if the
  lines moved); its GeneralCommissioning server serves no TC.
- Module (main f1e9677e): `controller/commissioner/commissioner.go:154-163` `Options.TermsAndConditions`,
  :662-665 `TermsAndConditionsAcknowledgement{Version, Acknowledgements uint16}`;
  `flow.go:710-742` `StepTermsAndConditions` / `configureTermsAndConditions` (skipped without the
  option); the info step is `GetInitialData` 0.1 (`flow.go:168`, :455-556; reads NetworkCommissioning
  FeatureMap/Networks and the OTA requestor's DefaultOtaProviders through `readValues` :392);
  `controller/controller.go:619-620` `PairOptions.Edit` (the seam); TC attribute ids
  `cluster/spec/generalcommissioning/definition_gen.go:35-39` (TCAcceptedVersion 0x5,
  TCMinRequiredVersion 0x6, TCAcknowledgements 0x7, TCAcknowledgementsRequired 0x8,
  TCUpdateDeadline 0x9 nullable); the device side serves TC behind `node.Commissioning.TermsAndConditions`
  (phase 38) and the reference bridge's `--tc-required`; harness family `CGENTC`.

## Scope (one shape)

1. `controller.PairOptions.TermsAndConditions *commissioner.TermsAndConditionsAcknowledgement` as a
   first-class field, applied before `Edit` (the seam stays).
2. `GetInitialData` also reads the root GeneralCommissioning FeatureMap and, when TC is set,
   TCMinRequiredVersion, TCAcknowledgementsRequired, TCUpdateDeadline (one read interaction, more
   paths). Store the requirement on the flow.
3. Right after that step, before ArmFailSafe/AddNOC: when the device requires terms
   (TCAcknowledgementsRequired true, or TCMinRequiredVersion > the option's Version, or no option),
   return `*commissioner.TermsRequiredError{Version, Acknowledgements uint16; Deadline *uint32}`
   wrapping `ErrCommissioning`, naming the requirement. `Acknowledgements`: the bits the device
   requires — read what the device offers (TCAcknowledgements is the accepted set; the required
   bits come from the DCL terms outside both modules — if the device exposes no required-bit set,
   report the version and leave Acknowledgements 0 and say so in the doc comment). Verify against
   the device server (`cluster/core` GeneralCommissioning TC, phase 38) what the module itself
   serves and keep the two consistent.
4. `*commissioner.GeneralCommissioningError{Code}` wrapping `ErrCommissioning` for every refused
   GeneralCommissioning command response (ArmFailSafe, SetRegulatoryConfig, SetTCAcknowledgements,
   CommissioningComplete), with `errors.As` support.
5. Tests: `controller/commissioner` unit tests for the typed errors; a harness case in the
   `CGENTC` family (`internal/chiptool/families_table_test.go:455` area, `gocert` shape): against
   `--tc-required`, the Go controller without the acknowledgement fails with
   `TermsRequiredError{Version: 1, Acknowledgements: 1}` early, and succeeds with
   `TermsAndConditions{1, 1}`. Document in `docs/` (controller guide: the field, the typed errors)
   and CHANGELOG `### Added`.
6. `notes/parity/by_design.md`: `BD-Matter-Commissioner-TermsRequirementRead` in the *Ahead of
   matter.js* register (marker: ControllerCommissioningFlow at db021da0 has no TC step; chip reads
   no TC attribute :2592-2603), with the chip lines above.

## Guards

build, vet, gofumpt, `go test -count=1` + `-race` of `controller/...`, `golangci-lint run --timeout 40m ./...`,
`make cover-check`, `make cite-check`, `make reachability-check`, `make ahead-check`, `make apidiff-check`
(additive), and `GOFABRIC_CHIP_FAMILIES=CGENTC make chiptool-families` under the lock (the device
composition does not move; the family holds the new case). Report in the fixed shape; push; open
the PR; no auto-merge.
