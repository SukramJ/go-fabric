# Phase 0 — fixes, second wave, cluster/ and its generator

- Brief: the coordinator's (items 1–9). Baseline `322a1164`; worktree `.claude/worktrees/fix2-cluster`, branch `review2/p0-fix2-cluster`, not pushed.
- Pins read: matter.js `db021da0`, connectedhomeip `6170af84` (the scratchpad exports under `scratchpad/pin/`). Every new citation was read there by line.
- MEASURED = a command's output or a line read; anything else is marked.
- Throwaway tests and overlays: `scratchpad/fix2-cluster/` (session scratchpad), none committed.

## 1. Per item

### W3-clucore-06 — commit `20ee8183`

- Files: `cluster/core/general_diagnostics.go`, `general_diagnostics_network_interfaces_parity_matterjs_test.go`, `general_diagnostics_test.go`, `general_diagnostics_internal_test.go`.
- Read at the pin: `GeneralDiagnosticsServer.ts:505-528` (filter `mac !== "00:00:00:00:00:00"` `:507`, sort operational first `:508-517`, `.slice(0, 8)` `:518`, IPv4 `slice(0, 4)` `:525`, IPv6 `slice(0, 8)` `:526`); `NodeJsNetwork.ts:193-194` (an interface without addresses and an internal one are skipped); model `general-diagnostics.element.ts:27` (`max 8`), `:191` (`max 4`), `:195` (`max 8`). matter.js adds no entry of its own.
- Now: `selectNetworkInterfaces` (unassigned MAC dropped, stable sort operational first, cut at 8, empty list when nothing qualifies) over the candidates; the test seam `g.interfaces` feeds the same selection. `syntheticEthernetIface` and `maxIfacesForApple` are gone, with the three Apple comments that justified them (`:30-38` struct doc, `:524-531` MatterRead, `:1083-1088` constants, `:1104-1108` hwadr, `:1146-1148` fallback).
- Red → green: `TestGeneralDiagnosticsNetworkInterfacesSelection`. Red with the baseline's semantics put into the selection function by overlay (`scratchpad/fix2-cluster/gd_baseline.go`): "NetworkInterfaces [eth0 eth1 eth2 eth3], want [eth0 eth1 eth2 eth3 eth4]", "none: NetworkInterfaces [eth0], want an empty list", "NetworkInterfaces [tun0 eth0], want [eth0]", "[down0 up0 down1 up1], want [up0 up1 down0 down1]". Green at the commit. `TestGenDiag_ReadNetworkInterfacesNonEmpty` asserted a non-empty list on the test host — it held only through the synthetic entry; it is now `TestGenDiag_ReadNetworkInterfaces` (≤ 8, named entries). `TestSyntheticEthernetIface` deleted with the function.
- CHANGELOG (**Changed**): GeneralDiagnostics `NetworkInterfaces` lists up to 8 interfaces (was 4), the operational ones first, skips the unassigned MAC `00:00:00:00:00:00`, and is an empty list when no interface qualifies — the synthetic `eth0` entry with an all-zero MAC is gone (matter.js `GeneralDiagnosticsServer.ts:505-518`).
- by_design: **no row exists** (MEASURED: `grep -n "eth0\|maxIfaces\|InterfaceCap" notes/parity/by_design.md` → no NetworkInterfaces row), so nothing to delete; the proposed "BD-Matter-GeneralDiagnostics-InterfaceCap" row of `phase-0-reg-w3.md:106` is not needed. Stale after this commit, not mine to edit: `notes/review/2026-11/wave-3-clu-core.md` (the open row).
- For the coordinator, removed with the comments and recorded nowhere else: the code stated, without a source, that Apple Home "tears the fabric down via RemoveFabric ~5 s after Subscribe-Initial" on an **empty** NetworkInterfaces list ("the prior stub value"). After this commit a host without a qualifying interface serves the empty list, as matter.js does. Whether the observation was real is unverified; if it was, the host that hits it is one without any non-loopback interface carrying a MAC and an address.
- Left as it was (differences from matter.js that the decision did not name): `IsOperational` is the OS up flag (matter.js: the mDNS interface restriction, `:498-503`, true for every interface without one); `InterfaceType` is guessed from the interface name (matter.js: the configured type, else from the NetworkCommissioning feature, `:480-494`, `:527`); the name is not cut to 32 characters (`:520`). Each is a candidate for the findings register.

### R2-P0-acl-B01 — commit `0e276ad4`

- Files: `cluster/core/group_key_management.go` (`writeFabric`), `group_key_management_parity_matterjs_test.go`.
- Read at the pins: chip `src/app/WriteHandler.cpp:805-811` at `6170af84`; matter.js `packages/protocol/src/action/server/AccessControl.ts:358-365` at `db021da0` (`authorizeWrite`, "No accessing fabric", `Status.UnsupportedAccess`).
- Now: `request && fabric == 0` → UNSUPPORTED_ACCESS, nothing stored; a local write (no request context) uses the `SetCurrentFabric` value and is refused without one. Difference from AccessControl, deliberate: AccessControl's local write falls back to the entries' own FabricIndex; GroupKeyMap does not, because this server already treats an entry's FabricIndex as raw input and never as a scope (`group_key_management.go`, the comment above `validateGroupKeyMap`).
- Red → green: `TestGroupKeyMapWriteWithoutAccessingFabricIsUnsupportedAccess`. Red at `20ee8183`: "GroupKeyMap write [] without accessing fabric: status Success, want UnsupportedAccess" (three lists), "1 mappings stored, want 0" — the mapping landed under the legacy hook's fabric 1, i.e. the hook stood in for the request's fabric — and "local GroupKeyMap write without a fabric: status Success". Green at the commit; `go test ./cluster/core/...` ok.
- The other fabric-scoped writable lists, MEASURED from `parity/schema.json` (effective access `rw` contains W and `fabric: F`): exactly six — Binding.Binding, AccessControl.Acl, AccessControl.Extension, OtaSoftwareUpdateRequestor.DefaultOtaProviders, GroupKeyManagement.GroupKeyMap, GroupKeyManagement.GroupcastAdoption. Read: Binding refuses (`binding.go:343-345`), ACL and Extension refuse (`access_control.go:646-660`), DefaultOtaProviders refuses with or without a request context (`ota_software_update_requestor.go:532-535`), GroupcastAdoption has no hand-written reference in `cluster/` (grep: generated files only) and `MatterWrite` answers UNSUPPORTED_WRITE for every attribute but GroupKeyMap. No further gap.
- Left, belongs to R2-P0-api-B03 (phase 5): `GroupKeyManagement.MatterInvoke` still falls back to the legacy hook when the request has no fabric (`group_key_management.go`, MatterInvoke head); the hook has no production caller.
- CHANGELOG (**Fixed**): a GroupKeyMap write over a session without a fabric answers UNSUPPORTED_ACCESS instead of being stored under fabric 0; `GroupKeyManagement.SetCurrentFabric` now serves a local write only.
- by_design: none.

### R2-P0-err-B02 — dropped on the coordinator's instruction (done elsewhere, `39f7fe0b`)

- I had implemented it as `a0e07e78` (nil port = disarmed, 49 test constructors routed through an armed rig). On the coordinator's mid-task message the commit was removed with `git rebase --onto a0e07e78^ a0e07e78`; the two later commits were re-created as `e81a1c98` and `4f695eea`. MEASURED after the rebase: `git diff --stat 322a1164 HEAD` over `operational_credentials.go`, `testhelper_test.go`, `operational_credentials*_test.go`, `vid_verification_test.go`, `fabric*_test.go`, `failsafe*_test.go` is empty; `go test ./cluster/core/...` ok.
- What I measured before dropping, for whoever writes the CHANGELOG line of `39f7fe0b`: the only non-test constructor of `core.NewOperationalCredentials` is `node/root.go` (`FailSafe: generalCom.FailSafeFabric`, `:196`); with nil = disarmed, `./bridge ./endpoint ./cluster/spec/operationalcredentials ./node ./fabrictest ./examples/reference-bridge` were all `ok` unchanged. So a host on the node facade sees no change; a host that composes `cluster/core` itself without `OpcredsConfig.FailSafe` / `SetFailSafe` gets FAILSAFE_REQUIRED from CSRRequest, AddNOC, UpdateNOC and AddTrustedRootCertificate where they ran before.

### R2-P0-bound-B04 — commit `e81a1c98`

- Files: `cluster/measurement/measurement.go` (`scaled`, `boundedInt64`; `clampToInt64` and `wattsToMilliWatts` are gone), `reading_bounds_parity_matterjs_test.go`.
- Read at the pin: `electrical-power-measurement.element.ts:54-55` (Voltage), `:58-59` (ActiveCurrent), `:70-71` (ActivePower) — `-2^62 to 2^62`; `:94-95` (Frequency) — `0 to 1000000`.
- Now: NaN → null for Voltage, ActiveCurrent, Frequency (ActivePower had it, W3-club1-07). Wider than the finding, and said here: every one of the four readings is held inside its attribute's constraint, so ±Inf and an out-of-range reading read as the bound — the rule W3-club1-08 gave Pressure and CO2. Before, they saturated at int64's range, outside `±2^62`, a negative frequency was served, and a reading of exactly 2^63 reached `int64(v)` (the `v > math.MaxInt64` comparison is false for it), the same implementation-defined conversion as NaN.
- Red → green: `TestElectricalPowerReadingBoundsParity`. Red at `0e276ad4` (19 rows): "Voltage NaN: read 0 (int64), true; want <nil>" (arm64's `int64(NaN)`), "Voltage +Inf: read 9223372036854775807 … want 4611686018427387904", "Frequency below 0: read -1000 … want 0", "Frequency above 1000000 mHz: read 1000500". Green at the commit. The test also holds the four constraint texts against the generated definition, so a pin lift that moves one fails.
- CHANGELOG (**Fixed**): ElectricalPowerMeasurement reads Voltage, ActiveCurrent and Frequency as null for a NaN reading; ActivePower, Voltage and ActiveCurrent are held to `±2^62` and Frequency to `0…1000000` mHz instead of saturating at int64's range.
- by_design: none. Not touched, same shape: `whToMilliWattHours` (`measurement.go`, ElectricalEnergy) still saturates at int64 with the 2^63 edge — see "New defects".

### R2-P0-err-D01 — commit `4f695eea`

- Files: `cluster/appliance/microwave.go` (the finding), and the same defect in `cluster/alarmbase/alarmbase.go`, `cluster/boolcfg/boolcfg.go`, `cluster/core/messages.go`, `cluster/energy/devicemanagement.go`, `cluster/jointfabric/administrator.go`; one test per package.
- The shape is W6-doc-01's (`cluster/onoff/server.go:379`, `closuredimension_server.go:568-573`): `spec.CarriesStatus(err)` → the error as is, FAILURE otherwise. No pinned reference: the contract is the module's own, `docs/guides/ports.md:15-19`.
- Red → green, each MEASURED red before the fix with `contract.StatusError{Status: BUSY}` and `spec.StatusOf`:
  - Microwave `TestMicrowaveHostStatusPassesThrough` (value and pointer; SetCookingParameters, AddMoreTime): "answered SetCookingParameters: matter status 0x9C: door open, want BUSY".
  - AlarmBase `TestDelegateStatusPassesThrough` (Reset, ModifyEnabledAlarms).
  - BooleanStateConfiguration `TestDelegateStatusPassesThrough` (CurrentSensitivityLevel write, SuppressAlarm, EnableDisableAlarm; five call sites).
  - Messages `TestMessagesRefusals` (PresentMessages, CancelMessages) — `messagesStatus` had the microwave's exact shape (`errors.As(im.StatusCodeError)` only).
  - DeviceEnergyManagement `TestDEMPowerAdjustLifecycle` (PowerAdjustRequest; all eight `demFailure("…: %v", err)` sites now go through `demRefusal`).
  - JointFabricAdministrator `TestICACCSRRequestStatuses` (`IcacCsr`).
  All green at the commit; `./script/errprefix/` ok (the microwave text gained its `appliance: ` prefix).
- Two port docs said the opposite of `ports.md` and were changed with the code: `alarmbase.Delegate` ("the server answers FAILURE") and `energy.DemManager` ("fails the command as the delegate fails it (FAILURE)"). Both now say: the status the error names, FAILURE for any other. If the coordinator reads those two docs as the decision and `ports.md` as the general rule that did not apply, the two packages' hunks revert cleanly on their own.
- How the survey was made (so its limit is known): (1) every non-test, non-generated line of `cluster/` with `StatusFailure` and `err` on it, 40 lines, each read; (2) a scripted pass over calls on a field named handler / delegate / sink / port / host / presenter / driver / provider / manager / hooks / setter … whose `err != nil` branch does not return the error, wrap it with `%w`, or call `CarriesStatus`. A port reached through a field with another name is not covered.
- Checked and not affected: `cluster/media/media.go:105-114` (`commandStatus` looks for `im.StatusCodeError` only, but wraps in a `StatusError` with `Unwrap`, and the dispatcher reads a host status in the chain first, `endpoint/dispatcher.go:863`); `cluster/thermo/temperaturecontrol.go:311-317` (`%w`, and INVALID_IN_STATE only for the documented `ErrTemperatureRefused`); `closurecontrol_server.go:592-594` (`%w`); the `host.Invoke` / `hooks.Invoke` servers (cover, fan, modeselect, valve: the error is returned as is); camera, tls, thermostat, lock, cover (`CarriesStatus` already). Store ports (`netinfra` Thread directory, OTA providers, ICD registrations, TLS tables, TimeSynchronization, DoorLock records) answer FAILURE for a failed save — listed, not changed: a persistence failure, not a host's refusal.
- CHANGELOG (**Fixed**): a host port's `contract.StatusError` (or `im.StatusCodeError`) answers its own status instead of FAILURE in MicrowaveOvenControl (`SetCookingParameters`, `AddMoreTime`), the alarm clusters' `Delegate`, BooleanStateConfiguration's `Delegate`, Messages' presenter, DeviceEnergyManagement's `DemManager` and JointFabricAdministrator's `IcacCsr`.
- by_design: none.

### R2-P0-par-B05 — commit `1d58062a`

- Files: `script/clustergen/gen.go` (`checkBoundExpr`, `checkBoundNode`, `dottedName`, the lists `boundOperators` / `boundFunctions` / `boundUnits`; `confError` renamed `renderError`, it now carries both refusals), `gen_test.go`, `gen_errors_test.go`, `bound_runtime_test.go` (new).
- Now: `boundLiteral` renders as an expression only a number, a celsius/percent value the carrying type does not scale (the case matter.js defines as "admits every value", `EncodedConstraint.ts:56-61`, already cited there and held by the existing test row), a named reference, a dotted reference of two names, `+ - * / ^` with both operands, and `maxOf` / `minOf` with at least one argument — recursively. Anything else fails `generate` with "constraint bound …: unknown bound node type …".
- "One shared list" without new public API: the list lives in the generator, and `TestBoundNodeTypesAreComputed` holds it against the runtime through `spec.New` + `ValidateWrite` — for every accepted node type a `max` bound stated with it must refuse the value above what it computes. The same test asserts its own premise: the unknown node `%` admits 255 at runtime and is refused by the generator.
- Red → green: `TestLiterals` at `4f695eea` with the new rows: 16 lines "boundLiteral({"type":"bogus"}) did not fail" … "boundLiteral({) did not fail". Green at the commit, with `TestGenerateRejects` (two generation-level rows) and the guard.
- One existing assertion changed: `boundLiteral({"type":"-","lhs":1})` was expected to render an `Expr` — a binary operator without its right operand, which the runtime parses to the opaque node. It is now one of the refused shapes.
- Regeneration: `go run ./script/clustergen` at the commit changes no file (MEASURED: `git status --short` lists only the three edited generator files). Snapshot-wide count of bound node types (my own walk over `parity/schema.json`, not exhaustive by construction — the generator run is the measurement): reference 334, `^` 40, `-` 28, `+` 22, maxOf 6, `.` 6, minOf 4, percent 3, `*` 3, celsius 1, `/` 1; none refused.
- CHANGELOG: none (no output change). by_design: none.

### R2-P0-conc-B07 — real; commit `c3c95b52`

- Files: `cluster/core/identify.go` (`op`, `setIdentifyTime`, `tick`), `identify_handler_parity_matterjs_test.go`.
- Settled: **real**, but not a Go data race — every shared word is an atomic, so `-race` reports nothing; it is an ordering defect. MEASURED with a handler whose handling of the new IdentifyTime spans the countdown's last tick (`tickingIdentify` advances the fake clock from inside `MatterIdentify(10)`): at `1d58062a`, `go test -race -count=3`, both paths, 6 of 6 runs — "the host saw [1 10 0] and IdentifyTime is 10; want [1 10] and 9: the countdown's stop overtook the write" (and "… the command"). The host is stopped while IdentifyTime counts from 10; no `DATA RACE` line.
- Read at the pin: `IdentifyServer.ts:54` (the tick is `this.callback(this.#identifyTick, { lock: true })`), `:58` (the reactor on `identifyTime$Changed`), `:108-114`, `:121-127` at `db021da0`.
- Fix: one mutex `op`. A request holds it from the host's handler to the stored value; the tick holds it from the decrement to the host's stop. The zero edge is reported to listeners after `op` is released (it was before the host's stop), so a change listener may call back into the server. Second interleaving closed by the same lock, read not exercised: the tick read 0, a request stored a new time and found `timerActive` still set, the tick then cleared it and ended — IdentifyTime stuck without a countdown.
- Green: `go test -race -count=5 ./cluster/core/ -run Identify` ok; `./cluster/core/...`, `./device/...`, `./endpoint/` ok.
- Consequence to state somewhere a host reads: `contract.IdentifyHandler.MatterIdentify` now runs with the server's queue held — a handler that sets IdentifyTime on the same server from inside the call (a write or the Identify command) deadlocks. Before, it recursed. `contract/identify.go` is outside this agent's paths: **the coordinator adds the sentence to the interface doc**. In-repo implementer checked: `device/identify.go:79` forwards to the host's function and does not call back.
- CHANGELOG (**Fixed**): Identify no longer tells the host to stop while a new IdentifyTime is being set: the countdown's tick and a request are serialised. `IdentifyHandler.MatterIdentify` must not set IdentifyTime on the same server from inside the call.
- by_design: none. The moved comment in `tick` keeps its old-pin citation (`:100-114, :121-127 at 862ad75f`) unchanged — decision 13's phase 4 re-reads it.

## 2. Analysis — W3-clugen-05 (read-only table for every cluster). Not implemented.

### What the references do (read at the pins)

| step | matter.js `AttributeWriteResponse.ts` at `db021da0` (`#writeConcrete`) | chip `src/app/WriteHandler.cpp` at `6170af84` (`CheckWriteAllowed`) |
| --- | --- | --- |
| 1 | View-privilege pass, denial → its status (`:203-219`) | `CheckWriteAccess(…, kView)` (`:777-778`) |
| 2 | UNSUPPORTED_ENDPOINT / _CLUSTER / _ATTRIBUTE (`:221-229`) | `finder.Find` / `ValidateClusterPath` (`:780-788`) |
| 3 | **`!limits.writable` → UNSUPPORTED_WRITE** (`:231-234`) | **no write privilege → UnsupportedWrite** (`:790-791`) |
| 4 | write-privilege pass (`:236-241`) | `CheckWriteAccess(…, *GetWritePrivilege())` (`:793-795`) |
| 5 | NEEDS_TIMED_INTERACTION (`:244-247`), fabric-scoped without a fabric → UNSUPPORTED_ACCESS (`:248-251`) | timed (`:801-802`), fabric-scoped list (`:808-812`) |
| 6 | DATA_VERSION_MISMATCH (`:254-257`) | DataVersion (`:818-826`) |

Both agree: a read-only attribute answers UNSUPPORTED_WRITE to anyone who may *view* the cluster, before the write privilege and before the DataVersion. A wildcard path skips it without a status in matter.js (`#writeAttributeForWildcard`, `:331-333`).

### What this module answers today — MEASURED

Throwaway test `scratchpad/fix2-cluster/zz_ro_probe_test.go` (overlay into `endpoint/`), `im.HandleWriteRequest` over a `TopologyDispatcher` with an ACL of one CASE entry; INSIDE = OnOff.OnOff (0x0006:0x0000, in the hand table), OUTSIDE = AirQuality.AirQuality (0x005B:0x0000, `schema.AttributeWritable` → known=false) on a generated `spec.Server`, and HepaFilterMonitoring.Condition (0x0071:0x0000) on a server that does not validate its own writes.

| subject, path | references | INSIDE the table | OUTSIDE, generated server | OUTSIDE, server that does not refuse |
| --- | --- | --- | --- | --- |
| Operate, concrete | UNSUPPORTED_WRITE | UNSUPPORTED_WRITE | UNSUPPORTED_WRITE (the server's own) | **Success — the write reached the server** |
| Operate, concrete, stale DataVersion | UNSUPPORTED_WRITE | UNSUPPORTED_WRITE | **DATA_VERSION_MISMATCH (0x92)** | Success |
| Operate, wildcard endpoint | no status | no status | **UNSUPPORTED_WRITE status entry** | Success |
| **View**, concrete (± version) | UNSUPPORTED_WRITE | **UNSUPPORTED_ACCESS (0x7E)** | **UNSUPPORTED_ACCESS** | **UNSUPPORTED_ACCESS** |
| View, wildcard endpoint | no status | no status | no status | no status |

### Corrections to the first wave's reading (`phase-0-fix-w3.md` §2)

- (b) DataVersion and (c) wildcard: confirmed by measurement, for the clusters outside the table.
- (a) "UNSUPPORTED_ACCESS instead of UNSUPPORTED_WRITE for the 94 clusters outside the table": the difference is real but **not a property of the table — it is the same inside it** (row 4). For a concrete path the ACL decision is made in `im/write.go` (the `aclActive && w.Path.HasEndpoint` block, before `d.Write`), with the privilege `TopologyDispatcher.MinWritePrivilege` returns (`endpoint/dispatcher.go`, MinWritePrivilege): `schema.AttributeWritePrivilege`, which is Operate for an attribute without a write privilege. The dispatcher gate the first wave cited (`:339-345`) is the wildcard-endpoint authorizer only. A View subject therefore gets UNSUPPORTED_ACCESS for every existing read-only attribute, in all 135 clusters. Generating the table does not change this; the fix is one more rule in `MinWritePrivilege` — a read-only attribute needs View, as an absent one already does — and that rule needs the table to know every cluster. `endpoint/` and `im/` are the other agent's.
- New, not in the first wave: a hand-written or host server outside the table that does not refuse a read-only attribute itself **stores the write** (column 5). MEASURED with a recording server; which shipped servers outside the table accept such a write was not enumerated.

### What a generated table for all clusters changes

- Table, MEASURED from `parity/schema.json`: 41 clusters / 415 entries → 126 clusters / 948 entries (the 9 remaining clusters have no read-only attribute below the global range; with the globals the first wave's 94 / 693).
- Files: the table moves from `schema/writable.go` (hand-written) into the generated `schema/attribute_access_gen.go` beside `attributeWritePrivileges` — one generator (`script/generate_matter_schema.go`), one generated file, one hand-written file shrinking; `TestReadOnlyAttributeParity` / `readOnlyDrift` become a generated-equals-snapshot check. No file under `cluster/spec/` moves (the definitions carry the access already).
- Behaviour: rows 2 and 3 and column 5 of the table above move to the references' answer for the 85 clusters; row 4 does not (see above).
- Tests that move: **none** — MEASURED. The full table was put in place of `schema/writable.go` by overlay (`scratchpad/fix2-cluster/writable_full.go`, 126 clusters / 948 entries, generated from the snapshot by a throwaway script) and `go test -count=1 -p 4 -overlay … ./schema/... ./endpoint/... ./im/... ./cluster/... ./device/... ./bridge/... ./node/... ./fabrictest/... ./conformance/... ./examples/reference-bridge/...` ran at `c3c95b52`: 185 packages `ok`, 0 `FAIL`, exit 0 (`scratchpad/fix2-cluster/full-table-run.log`). That the overlay took effect is shown by the probe run against it: `schema.AttributeWritable(0x005B, 0x0000)` → known=true, and the three Operate rows answer UNSUPPORTED_WRITE / UNSUPPORTED_WRITE / no status for both OUTSIDE servers, with no write reaching the recording server; the View row stays UNSUPPORTED_ACCESS. So the change is covered by no existing test in either direction: it needs its own table test, and `spectest.CheckHandWrittenServer` (which emulates the gate with `schema.AttributeWritable`, `cluster/spec/spectest/spectest.go:446-463`) keeps passing because every hand-written server already refuses on its own or is inside the table. Not run: `controller/`, `client/`, the chip-tool suite.

### Options for the coordinator

1. **Generate the table and add the View rule** (recommended; the references' order in every row). Two agents' paths: `schema/` + its generator, and `endpoint/dispatcher.go` MinWritePrivilege with a table test over inside/outside × Operate/View × version × wildcard.
2. Generate the table only: closes rows 2, 3 and column 5; row 4 stays as a findings-register entry.
3. Keep the hand table: then rows 2–4 and column 5 are four findings-register entries, not one.

## 3. Analysis — N-1 (40 kit-default servers list a mandatory attribute that answers no read). Not implemented.

### What a read answers today — MEASURED

Throwaway test `scratchpad/fix2-cluster/zz_n1_probe_test.go` (overlay into `node/`): `device.New(type, defaults)` mounted on a `TopologyDispatcher`, six of the 40 (ModeSelect 0x0050:0, Fan 0x0202:1, AirQualitySensor 0x005B:0, LaundryWasher 0x0060:4, Chime 0x0556:0, WaterHeater 0x0094:5), identical for all six:

- concrete read → **UNSUPPORTED_ATTRIBUTE**;
- the cluster's AttributeList (0xFFFB) **lists the attribute**;
- the dispatcher's wildcard read of the cluster returns an entry for it with status UNSUPPORTED_ATTRIBUTE (what the report writer does with an error entry of a wildcard path was not exercised).
- With the value declared (`airqualitydef.Declare{Initial: Patch{AirQuality: &v}}`) the read answers it: MEASURED for AirQuality — a host that declares the values closes the gap; the defect is the kit building the type without them.

### What matter.js does at `db021da0`

- A cluster behaviour's state default is `SelectDefaultValue` (`packages/node/src/behavior/internal/ClusterDerivation.ts:143-144`): the model's default, else null when nullable, else 0 / false / `{}` / `[]` for integer-float / boolean / bitmap-object / array, and **nothing for enum, string and bytes** (`packages/model/src/logic/SelectDefaultValue.ts:142-170`; the doc at `:16-20` says so).
- After the behaviour's initializer the state "must now conform to the schema": `this.datasource.validate(context, behavior.state)` (`packages/node/src/behavior/internal/ServerBehaviorBacking.ts:44-63`), and a mandatory member without a value throws `ConformanceError(…, "Matter requires you to set this attribute")` (`packages/node/src/behavior/state/validation/conformance-compiler.ts:717-721`).
- None of the 16 matter.js servers of these clusters sets one of the 30 attributes in its own code (grep over `packages/node/src/behaviors/<cluster>/*Server.ts`; the two OperationalState servers assign `operationalError` only in a change handler, `OperationalStateServer.ts:74`).
- So: **the behaviour does not initialize and the application is told to set the attribute**; matter.js never serves an AttributeList entry without a value. INFERRED, not read: that the failed behaviour fails the endpoint's construction as a whole, and that the `[]` / `{}` placeholders of the list- and struct-typed ones fail the same validation on their count constraint / mandatory fields.

### The 40, by cluster and attribute (30 pairs) — from `parity/schema.json`

No pair has a model default; none is nullable. "matter.js state default" is the `SelectDefaultValue` result for the metatype.

| cluster | attribute | type | conformance | quality | constraint | matter.js state default | kit device types |
| --- | --- | --- | --- | --- | --- | --- | --- |
| ModeSelect 0x0050 | 0x0000 Description | string | M | F | max 64 | none → refuses | ModeSelect |
| RvcRunMode 0x0054 | 0x0000 SupportedModes | list | M | F | 2 to 255 | `[]` | RoboticVacuumCleaner |
| AirQuality 0x005B | 0x0000 AirQuality | enum | M | — | desc | none → refuses | AirQualitySensor |
| OperationalState 0x0060 | 0x0004 OperationalState | enum | M | — | — | none → refuses | LaundryWasher, LaundryDryer, Dishwasher |
| OperationalState 0x0060 | 0x0005 OperationalError | struct | M | — | desc | `{}` | LaundryWasher, LaundryDryer, Dishwasher |
| RvcOperationalState 0x0061 | 0x0004 OperationalState | enum | M | — | — | none → refuses | RoboticVacuumCleaner |
| RvcOperationalState 0x0061 | 0x0005 OperationalError | struct | M | — | desc | `{}` | RoboticVacuumCleaner |
| WaterHeaterManagement 0x0094 | 0x0005 BoostState | enum | M | — | — | none → refuses | WaterHeater |
| EnergyEvseMode 0x009D | 0x0000 SupportedModes | list | M | F | 2 to 255 | `[]` | EnergyEvse |
| WaterHeaterMode 0x009E | 0x0000 SupportedModes | list | M | F | 2 to 255 | `[]` | WaterHeater |
| FanControl 0x0202 | 0x0001 FanModeSequence | enum | M | F | — | none → refuses | Fan, AirPurifier, ExtractorHood |
| SoilMeasurement 0x0430 | 0x0000 SoilMoistureMeasurementLimits | struct | M | F | — | `{}` | SoilSensor |
| ThreadBorderRouterManagement 0x0452 | 0x0000 BorderRouterName | string | M | — | 1 to 63 | none → refuses | ThreadBorderRouter, NetworkInfrastructureManager |
| ThreadBorderRouterManagement 0x0452 | 0x0001 BorderAgentId | octstr | M | — | 16 | none → refuses | ThreadBorderRouter, NetworkInfrastructureManager |
| MediaPlayback 0x0506 | 0x0000 CurrentState | enum | M | — | desc | none → refuses | BasicVideoPlayer, CastingVideoPlayer |
| ApplicationBasic 0x050D | 0x0002 ApplicationName | string | M | F | max 256 | none → refuses | ContentApp |
| ApplicationBasic 0x050D | 0x0004 Application | struct | M | F | desc | `{}` | ContentApp |
| ApplicationBasic 0x050D | 0x0005 Status | enum | M | — | desc | none → refuses | ContentApp |
| ApplicationBasic 0x050D | 0x0006 ApplicationVersion | string | M | F | max 32 | none → refuses | ContentApp |
| CameraAvStreamManagement 0x0551 | 0x0002 VideoSensorParams | struct | VDO | F | — | `{}` | Camera |
| CameraAvStreamManagement 0x0551 | 0x0004 MinViewportResolution | struct | VDO | F | — | `{}` | Camera |
| CameraAvStreamManagement 0x0551 | 0x0007 MicrophoneCapabilities | struct | ADO | F | — | `{}` | Camera, Intercom |
| CameraAvStreamManagement 0x0551 | 0x0018 Viewport | struct | VDO | N | — | `{}` | Camera |
| Chime 0x0556 | 0x0000 InstalledChimeSounds | list | M | — | 1 to 255 | `[]` | Chime |
| JointFabricDatastore 0x0752 | 0x0000 AnchorRootCa | octstr | M | N | max 400 | none → refuses | JointFabricAdministrator |
| JointFabricDatastore 0x0752 | 0x0003 FriendlyName | string | M | N | max 32 | none → refuses | JointFabricAdministrator |
| JointFabricDatastore 0x0752 | 0x0004 GroupKeySetList | list | M | N | 1 to 254 | `[]` | JointFabricAdministrator |
| JointFabricDatastore 0x0752 | 0x0005 GroupList | list | M | N | 1 to 254 | `[]` | JointFabricAdministrator |
| JointFabricDatastore 0x0752 | 0x0007 AdminList | list | M | N | 1 to 32 | `[]` | JointFabricAdministrator |
| JointFabricDatastore 0x0752 | 0x0008 Status | struct | M | N | — | `{}` | JointFabricAdministrator |

By kind: 7 enum, 5 string, 2 octstr (14 that matter.js gives no value at all), 7 list (every one with a minimum count the empty list misses), 9 struct. 14 of the 30 are quality F — a fact of the product (the supported modes, the fan's mode sequence, the application's name), which the module has no business inventing ("rich model, dumb bridge").

### Options for the owner

1. **`device.New` refuses, as matter.js does** (recommended). A kit type whose mandatory attribute has neither a model default nor a declared value is refused with an error naming the attribute and its `Declare` field — the shape `refusedDefaults` already has for a feature selection (`device/conformance_test.go:31-63`). It is matter.js's behaviour ("Matter requires you to set this attribute"), it keeps product facts on the host's side, and it turns a silent non-conformance into a construction error. Cost: 19 kit types stop building with bare defaults — a break for a host that relies on that; today such a host publishes an endpoint whose AttributeList names an attribute that answers UNSUPPORTED_ATTRIBUTE. The node guard's pin list empties.
2. The generated server serves a fallback (the specification's fallback column, matter.js `MandatoryDefaultValue`, `SelectDefaultValue.ts:66-138`): closes strings, octet strings and lists on the wire, but the 7 enums have no fallback there either ("manufacturer-specific … stays undefined"), the 7 lists would violate their own minimum count, and 14 values would be invented product facts. Not recommended: further from matter.js than today, not closer.
3. Split: refuse for quality-F and enum attributes, a kit default only where the device type's own semantics give one (OperationalError = NoError is the one candidate I see; matter.js's server writes exactly that value itself in `OperationalStateServer.ts:74`). Smaller break, two rules instead of one.
4. Leave it pinned in `unansweredWithDefaults` and record it in the findings register. Not an architecture, a deferral.

Best architecture in my judgement: 1, with the error text naming the `Declare` field so the fix is one line in the host.

## 4. Not implemented

- **R2-P0-err-B02** — dropped on the coordinator's instruction; done elsewhere (`39f7fe0b`). My commit `a0e07e78` was removed from the branch by rebase; see §1.
- **W3-clugen-05** and **N-1** — analysis only, as briefed (§2, §3).
- Nothing was refuted. R2-P0-conc-B07 turned out real (an ordering defect, not a data race).

## 5. Guards

HEAD `c3c95b52` for every row; log `scratchpad/fix2-cluster/final-guards.log`.

| command | result |
| --- | --- |
| `gofumpt -l cluster script/clustergen` | no output |
| `go vet` on `./cluster/core/... ./cluster/measurement/... ./cluster/appliance/... ./cluster/alarmbase/... ./cluster/boolcfg/... ./cluster/energy/... ./cluster/jointfabric/... ./script/clustergen/...` | ok |
| `go test -count=1 -p 4 ./script/errprefix/ ./schema/... ./node/... ./script/clustergen/...` | all `ok` (node 96.5 s) |
| `go test -count=1 -p 4` on the seven touched cluster packages, `./device/... ./endpoint/... .` (root: licence header) | all `ok` |
| `go test -count=1 -p 4 ./bridge/... ./fabrictest/... ./examples/reference-bridge/...` (consumers) | all `ok` |
| `CGO_ENABLED=1 go test -race -count=1 -p 4 ./cluster/core/ ./cluster/measurement/ ./cluster/energy/` | all `ok`, no `DATA RACE` |
| `go run ./script/clustergen` at `1d58062a` | no generated file changed |

Not run: `make` targets, the full `make race`, `cite-check`, `reachability-check`, the API-diff guard, `mkdocs build --strict`, chip-tool, `controller/` and `client/`. Expected to move, for the coordinator: `script/citecheck/summary.md` (new citations at `db021da0` / `6170af84` in `general_diagnostics.go`, `group_key_management.go`, `measurement.go`, `identify.go` and four test files); `script/reachability/inventory.json` (functions removed: `syntheticEthernetIface`, `clampToInt64`, `wattsToMilliWatts`; added: `selectNetworkInterfaces`, `boundedInt64`, `writeFabric`, `setIdentifyTime`, `tick`, the five refusal helpers). No exported identifier added or removed (the API diff should be empty).

## 6. New defects noticed (none fixed here)

1. **A View subject gets UNSUPPORTED_ACCESS, not UNSUPPORTED_WRITE, for a read-only attribute — in every cluster, the hand table's included.** MEASURED (§2, row 4). `endpoint/dispatcher.go` MinWritePrivilege returns Operate for an attribute that has no write privilege; `im/write.go` checks it before the dispatcher's read-only gate. References: matter.js `AttributeWriteResponse.ts:231-241`, chip `WriteHandler.cpp:790-795`. Other agent's paths.
2. **A server outside the hand table that does not refuse stores a write to a read-only attribute.** MEASURED with a recording server (§2, column 5). Which shipped servers do was not enumerated.
3. `cluster/measurement/measurement.go` `whToMilliWattHours` (ElectricalEnergy): still saturates at int64's range, and a reading of exactly 2^63 mWh reaches `int64(v)`. Read, not exercised; the Energy field's constraint was not looked up.
4. GeneralDiagnostics, differences from matter.js the decision did not name (read at the pin): `IsOperational` is the OS up flag (`GeneralDiagnosticsServer.ts:498-503, :521`: the mDNS interface restriction); `InterfaceType` is guessed from the interface name (`:480-494, :527`: configured, else from NetworkCommissioning's feature); the name is not cut to 32 characters (`:520`).
5. `cluster/core/general_diagnostics.go` still carries Apple statements without a source outside the NetworkInterfaces code (the attribute-list comments around the RebootCount / BootReason block and `MatterAttributes`), and old-pin citations (`862ad75f`).
6. `docs/guides/ports.md:284` says of `onoff.Sink` "an error answers FAILURE"; since W6-doc-01 a status-carrying error answers its status (`cluster/onoff/server.go:379-382`). Read, not edited.
7. `script/clustergen/gen.go` `intOf`: a bound stated as JSON `null` unmarshals into 0 and renders `&spec.Bound{Int: 0}`. Read, not exercised; whether the snapshot can carry a null bound was not checked (none does at the pin: the generator's output did not move).
8. The dispatcher's comments cite `AttributeWriteResponse.ts:229-231` and `:329-331` without a pin; at `db021da0` the lines are `:231-234` and `:331-333` (decision 13, phase 4).
9. `GroupKeyManagement.MatterInvoke` still lets the legacy `SetCurrentFabric` value stand in for a request without a fabric (R2-P0-api-B03's scope).

## 7. Open questions for the coordinator

1. R2-P0-err-D01: I applied `ports.md`'s rule to `alarmbase.Delegate` and `energy.DemManager`, whose own docs said "FAILURE" — and changed those two docs. Keep, or revert the two packages' hunks?
2. R2-P0-bound-B04: the fix holds all four readings to the model's constraints (wider than "NaN → null"). Keep?
3. R2-P0-conc-B07: `contract/identify.go` needs the sentence that `MatterIdentify` runs with the server's queue held (outside my paths).
4. W3-clucore-06: the removed comment recorded an unsourced observation that Apple Home removes the fabric on an *empty* NetworkInterfaces list. Decision 4 serves the empty list; if the observation is to be kept anywhere, it is the coordinator's to place (`docs/matter-ecosystem-observations.md`?) — as unverified.
5. W3-clugen-05: options in §2 (recommended: generate the table and add the View rule).
6. N-1: options in §3 (recommended: `device.New` refuses, as matter.js does) — the owner's.
