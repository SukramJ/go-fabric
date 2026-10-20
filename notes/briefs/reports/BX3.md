# BX3 report — gate-1 sweeps (2026-10-20)

Brief `notes/briefs/2026-10-20-BX3-sweeps.md`; branch `m4/bx3` off `main` at `ab4303fd`. Strings, comments, aliases, one
field per config and one dump removal; no behaviour changed except where named below. `make race` not run: nothing here
adds a goroutine, a lock or a shared write beyond construction: the one new package variable, `tlv.Logger`, is documented as set once
before use, and the two `SetLogger` methods store into an `atomic.Pointer`, so they may be called at any time.

## Scripts

All in `notes/briefs/reports/BX3-scripts/` (and the scratchpad), idempotent, run from the repository root on a merged tree:

| Sweep | Script |
| --- | --- |
| 1 error prefixes | `bx3-prefixes.py` (prints one line per changed message) |
| 2a log names | `bx3-logs.py` |
| 2b loggers | `bx3-loggers.py`; `bx3-setlogger.py` (`SetLogger` for `GeneralDiagnostics` and `Zeroconf`, the `CaseAdapter` dump removal); `bx3-wire-logger.py` (`node.Config.Logger` and the bridge's logger into the components) |
| 3 mutex comments | `bx3-mutex.sh` (wraps `bx3-mutex.go`, a go/ast pass; dry run by default, `-apply` writes) |
| 4 facade aliases | `bx3-aliases.py` |
| 5 the dump | `bx3-dump.py` |

After BX1/BX2 merge, re-run in this order: `bx3-dump.py`, `bx3-prefixes.py`, `bx3-logs.py`, `bx3-loggers.py`, `bx3-setlogger.py`,
`bx3-wire-logger.py`, `bx3-aliases.py`, `bx3-mutex.sh -apply`, then `gofumpt -l .`, `go build ./...`, `go test ./...`. A script that finds its anchor gone
asserts and says which; a rewritten region is a manual merge. The test edits `node/system_set_wiring_test.go` (in `bx3-logs.py`), `transport/udp/listener_test.go` (in `bx3-loggers.py`) and
`bridge/securechannel_noop_test.go` (the `peekBytes` tests, in `bx3-setlogger.py`) are part of the scripts; `node/logger_wiring_test.go` is new.
`bx3-aliases.py` spells the alias `OTAApplier` for `mattercore.OtaApplier`.

## 1 Error prefixes (G1-9) — 631 messages

Commit `error prefixes`, first pass 485 messages: 417 `errors.New`/`fmt.Errorf` literals plus 68 further string literals of the old `matter:` prefix
(typed errors, `im.UnsupportedCommandf`, `im.ErrUnsupportedCommand`). By package: `cluster/core` 208, `store` 131 (`matter store:`
to `store:`, most are `fmt.Errorf` wraps), `bridge` 19, `controller/commissioner` 14, `controller/attestation` 14,
`cluster/thermo` 13, `cluster/cover` 13, `cluster/lock` 11, `node` 10, `cluster/closure` 10, `controller/dcl` 9,
`internal/chipdm` 7, `cluster/wire` 7, `im` 4, `cluster/light` 3, `cluster/appliance` 3, `mdns` 2, and one each in
`transport/btp`, `secure/spake2`, `secure/sigma`, `secure/attestation`, `cluster/spec`, `bdx/otaimage`.
Three prefixes were kept behind the package name rather than replaced, because they name a sub-area, not a package:
`node: ota: image carries no payload`, `spake2: hex: odd length`, `chipdm: conformance: …`.

No test asserted a changed message (the full suite passed with no test edit); `docs/` had no occurrence;
`internal/chiptool` reads no `DebugText` (grep), so nothing there changed.

Second pass (review decisions 1 and 2, commit `error prefixes, second pass`): 146 more string literals. The four `matter:` panics of
`contract/` are `contract:`; the status texts that `spec.Errorf` and the typed errors build, and the `im.UnsupportedCommandf` texts of
the cluster servers, take the package name instead of the cluster name: `cluster/thermo` 46, `cluster/lock` 42, `cluster/closure` 23,
`cluster/appliance` 16, `cluster/light` 8, `cluster/cover` 7 (`lock:`, `closure:`, `thermo:`, `cover:`, `appliance:`, `light:`). No test,
`docs/` page or `internal/chiptool` expectation named an old text (the one `errors.New("doorlock: invoke failed")` is a test-local
sentinel). The `matter` pass of `bx3-prefixes.py` now handles every sub-prefix of a directory, not only `matter`.

Left as it is, with the reason:

- `fmt.Errorf` literals without a known sub-package prefix (they wrap) were not given one.
- `endpoint/dispatcher_test.go:1478-1479` keep `matter: …` strings: fixtures for the legacy wording heuristic (G1-8), not messages.
- Wire payload: `operational_credentials.go` puts `err.Error()` into `NOCResponse.DebugText` at about a dozen sites; the errors come from
  `store` (shorter now) and `mattercert` (untouched). No chip-tool case reads it.
- Security paths got prefix text only: 92 literals (first pass) across `cluster/core/{operational_credentials,group_key_management,access_control*,general_commissioning}.go`
  and `secure/{attestation,sigma,spake2}`.

## 2 Log names and loggers (G1-18)

### 2a names — 7 events (commit `log event names`)

`im.subscription.added`, `im.subscription.restored`, `endpoint.gc_skipped` (the reason moved to the attribute `reason`),
`endpoint.gc`, `tlv.utf8_trimmed`, `tlv.utf8_truncated` (both through the new `tlv.Logger`, default `slog.Default()`),
and `matter.general_diagnostics.data_model_test_optional` for `general_diagnostics.go:364`. For that last one the matter.js sentence
stays verbatim as the attribute `reason`: two tests count it (`cluster/core/general_diagnostics_features_parity_matterjs_test.go`
counts occurrences in the output, still one), and `node/system_set_wiring_test.go` read it as the record message, so that one test
now reads the event name.

Left: `bridge/subscribe.go:517` and `bridge/receive_dispatch.go:316,324` (`"matter.tx.subscribe."+op`, `"matter.tx."+op+".chunk_rejected"`):
every `op` passed is a lower-case literal (`event_report`, `report`, `read`, `invoke`, `subscribe`, `subscribe.ongoing`), so the
names are dotted lower case at run time. `node/commissioning.go:237` (`h.logger.Warn(event, …)`) and `binding/resolve.go:61`
(`m.log.Info(msg, …)`) take their names from callers' literals; not inspected further. `cluster/cover/windowcovering_server.go:721`
(`clampMsg`) is a constant passed by two callers; unchanged.

### 2b loggers — 32 of 44 calls through a logger, 2 removed, 8 config fields and 2 `SetLogger`

Each type below stores `log` (resolved at construction with `cmp.Or(cfg.Logger, slog.Default())`) and reads it through a nil-safe
`logger()` accessor, so a value built without its constructor still logs (`BridgedDeviceBasicInformation` has none: only its validator logs, from the constructor). One additive field per config, nil is `slog.Default()`:

| Config field | Type | Calls |
| --- | --- | --- |
| `core.Config.Logger` | `BasicInformation` | 7 |
| `core.BridgedConfig.Logger` | `BridgedDeviceBasicInformation` | 2 |
| `thermo.ThermostatConfig.Logger` | `ThermostatServer` (also `presets.go`, `atomic.go`; `closeFabric` and `statusOf` take the logger as a parameter) | 15 |
| `cover.Config.Logger` | `WindowCoveringServer` | 2 |
| `light.ColorControlServerConfig.Logger` | `ColorControlServer` | 1 |
| `udp.Config.Logger` | `Listener` | 1 |
| `mdns.OperationalServiceConfig.Logger`, `mdns.CommissionableServiceConfig.Logger` | `capAdvertisedIntervals` takes the logger | 1 |
| `GeneralDiagnostics.SetLogger` (no config struct) | `GeneralDiagnostics`, `EmitBootReason` | 2 |
| `Zeroconf.SetLogger` (no config struct) | `Zeroconf.StartReannounceLoop` | 1 |

`SetLogger` follows `DiagnosticLogs.SetLogger` (nil is `slog.Default()`) but stores into an `atomic.Pointer[slog.Logger]` read through the
nil-safe `logger()` accessor, so it needs no lock and cannot race with a log call. Commit `SetLogger for GeneralDiagnostics and Zeroconf`.

One test edit comes with this: `transport/udp/listener_test.go` swapped `slog.Default()` after the listener was built; the
listener now keeps the logger it was built with, so the test sets `a.log` instead. (A host that calls `slog.SetDefault` after
building a component of this table keeps the old default in it — the same as the existing constructors that resolve `slog.Default()`
at construction.)

Wiring (review decision 4, commit `node.Config.Logger and the bridge's logger reach the components`, `bx3-wire-logger.py`):
`node.Config.Logger` (`n.logger`) reaches `core.Config.Logger` and `GeneralDiagnostics.SetLogger` (`node/root.go`, through `rootDeps.logger`),
`Zeroconf.SetLogger` (`node/mdns.go`) and the two node-built `udp.Config`s (`node/client.go`, `node/groupsend.go`); the bridge's `b.logger`
reaches its `udp.Config` (`bridge/tcp_listen.go`) and the three `mdns` service configs (`bridge/bridge.go`). New test
`node/logger_wiring_test.go`: a node built with a logger over a recorder sees the `BasicInformation` validator line (an empty VendorName;
the validator logs at debug, not warn) and the GeneralDiagnostics BootReason line there. Not wired: `core.BridgedConfig.Logger`
(`endpoint.ClusterServers` is a free function with no logger in reach), and the thermostat, window-covering and colour-control servers
(`thermo`, `cover`, `light` configs), which only the host builds (`examples/reference-bridge` here) — the host sets their `Logger`.
`bridge/bridgetest` still builds a bare `udp.Config` and `mdns.OperationalServiceConfig` (test support).

Removed (review decision 3): the two debug lines of `bridge/handlers.go` (`matter.tx.sigma2resume.wire`, `matter.tx.sigma2.wire`, the first
128 Sigma2 bytes in hex), the helper `peekBytes` and its three tests (`bridge/securechannel_noop_test.go`); CHANGELOG `### Removed`.
Seen, not touched: `bridge/securechannel.go:1089` logs a payload as hex (BX2's file) — the same kind of line.

Left, 10 calls, the owner's after the merge: the three security-path servers, `cluster/core/operational_credentials.go` 6,
`general_commissioning.go` 3 and `access_control_auxiliary.go:102` (its `AccessControl` is in the `access_control.go` path, built with a
positional store).

The one print in `ble/bluez/cmd/reference-controller/main.go:21` stays: `fmt.Fprintln(os.Stderr, …)` in a `main` package is the CLI's own output.

## 3 Mutex comments (G1-16) — 104 comments

`// <mu> guards <fields>.` above 104 mutex fields in 90 files. The set is derived by a go/ast pass: the fields of the receiver
written anywhere in a method of the type that are also touched between a `Lock` and its `Unlock` (or to the end of the block when the
`Unlock` is deferred), including the fields of same-file callee methods that take no lock themselves. A field that is never
written after construction (`clock`, `logger`, `cfg`, a factory) is not named; the comment says what the lock protects, not
everything the locked region reads. A comment above a field breaks gofmt's alignment block, so some neighbouring lines were
re-aligned (`gofumpt`, 17 realigned lines). The derivation is a heuristic over the Lock sites of one file; every comment is a
statement about the code and the owner should skim a sample (`bridge/outbound_reliable.go:52`, `controller/peer/set.go:89`,
`im/eventlog.go:125` are the long ones).

Not commented, with the reason (the 32 below; BX2's files are the rest of the 149):

- more than seven fields under the lock (the set is "most of the struct", which `// mu guards the fields below` would say better;
  needs a human): `bdx/responder.go:355`, `ble/bluez/central_linux.go:77`, `bridge/bridge.go:355`, `bridge/bridgetest/commissioner.go:150`,
  `bridge/commissioning_window.go:109`, `cluster/core/basic_information.go:42`, `general_diagnostics.go:80`, `groupcast.go:222`,
  `ota_software_update_requestor.go:66`, `cluster/thermo/thermostat_server.go:282`, `controller/ca/ca.go:244`,
  `controller/client/node.go:33`, `controller/exchange/session.go:52`, `controller/peer/connection.go:119`;
- the Lock/Unlock pairs do not share a block (`cluster/core/general_commissioning.go:41`, `cluster/switch/server.go:156`,
  `controller/exchange/exchange.go:166`, `transport/btp/session.go:124`) or the file has no Lock site (`cluster/core/vid_verification.go:59`,
  `node/client.go:214`) or no field is touched under it (`cluster/energy/commoditymetering.go:65`, `controller/ble/channel.go:64`);
- security paths (comment-only change still outside "strings only"): `cluster/core/{access_control,operational_credentials,group_key_management,general_commissioning}.go`,
  `secure/sigma/initiator.go:42`;
- not struct fields of a scanned type: `bootid/bootid.go:34`, `contract/matter.go:375`, `bridge/bridgetest/session.go:127`,
  `bridge/subscription_reestablish.go:130`, `controller/client/interact.go:74`.

BX2's files are skipped (`cluster/core/messages.go`, `cluster/cover/windowcovering_server.go`, `cluster/energy/devicemanagement.go`,
`cluster/appliance/microwave.go`, `cluster/measurement/soil.go`, `bdx/sink.go`, `secure/operational/manager.go`,
`controller/exchange/manager.go`, `transport/tcp/conn.go`, `bridge/securechannel.go`, `cluster/core/identify.go`, `cluster/lock/*`,
`node/ota.go`, `cluster/core/diagnostic_logs.go`, `bridge/bdx_responder.go`, `fabrictest/*`); the script run on a tree where BX2 has
landed leaves any mutex that now has a comment alone.

## 4 Facade aliases (G1-6) — 15 aliases, 30 references

`node/facade.go` gains `OTAImageSource`, `OTAConsent`, `OTAConsentState` (the consent interface's result type, added so a host
can implement `OTAConsent` from `node` alone), `OTAApplyPolicy`, `OTAPeer`, `OTAUpdateStatus`, `OTAApplier`, `WiFiReporter`,
`CommissioningApprover`, `ReverseCommissioner`, `CapabilityMinimaStruct`, `GeneralDiagnosticsFeatures`, `DeviceTypeValidationMode`,
`WindowStatus` (for `wire.WindowStatusSnapshot`) and `AttestationChain` (for `attestation.Chain`). 30 references in `node/*.go`
(`Config`, `OTAProviderConfig`, `OtaConfig`, `WiFi`, `CommissionerControlConfig`, `Attestation`, `WindowSnapshot`)
spell the alias; `attestationMaterial` (unexported) keeps `*attestation.Chain`: gocritic's unnamedResult flags the alias there.
`Aggregator.Remove`, `IncreaseConfigurationVersion(s)` and `EndpointPersistence` spell `device.SourceKey`.
`go doc ./node` shows the aliases in those declarations; what still names `cluster/core` there is the seams (`Node.Bridge`,
`Store`, `Peers`, `Bindings`, `RootClusters`, `CommissionerControl()`, `OTAProvider()`), as the brief says. `docs/guides/advanced-seams.md`:
two rows (`Peers`, `Bindings`) and the new alias names.

Left: the constants a host needs next to these types (`OTAConsentGranted`…, the `OTAUpdateStatus` values, the
`DeviceTypeValidation…` modes) are not re-exported; they would be `const` blocks, not aliases, and a host that implements the ports
still imports `cluster/core` for them. `discovery.Names` (`Client.Names`) and `endpoint.Spec`/`ScenesStore` were not in the brief's list.

## 5 The /tmp dump (G1-17)

`bridge/subscribe_dispatch.go`: `GOFABRIC_DUMP_SUBSCRIBE` / `OPENCCU_LOOM_MATTER_DUMP_SUBSCRIBE`, `dumpSubscribeEnabled()` and the
`os.WriteFile("/tmp/subscribe-init-sub…bin")` are gone. The strict TLV validation that was gated on the same variable
(`chunk_strict_violation`, a log line only) went with the gate: nothing else reads it; it was off outside diagnose mode.
Unused imports `os` and `tlv` removed from that file.

## Stray artefact

`reference-controller` at the repository root was a committed 34 MB binary (since #144). `git rm` and `/reference-controller` in
`.gitignore` next to `/reference-bridge`, one commit `remove the committed reference-controller binary`; CHANGELOG `### Removed`.

## CHANGELOG

`[Unreleased]` `### Changed` (prefixes incl. status texts and `contract`, log events, `tlv.Logger`, the eight logger fields, the two
`SetLogger` and the wiring, the aliases with the signatures that now spell `node.X`, the mutex comments) and `### Removed` (the dump,
the Sigma2 wire dumps, the binary).

## Guards

The exit lines with HEAD are in the PR description. `make reachability` and `make cite-check` were regenerated and committed: the
unreached count stays at its ratchet 5 (the new exported names are whitelisted), the citation drift keeps its 28 entries and only
their line numbers moved. `make race` was not run: no shared write beyond construction was added (`SetLogger` is an atomic pointer store).

## Decisions (owner answers to the review of #202)

1. `contract/` panics: `contract:` (done, second pass).
2. Status texts and typed errors take the package name, not the cluster name (done, second pass; P8 allows one style).
3. `GeneralDiagnostics` and `Zeroconf` get `SetLogger`; the `CaseAdapter` wire dumps are removed; the three security-path servers stay
   (below).
4. `node.Config.Logger` is wired in now (done, with a test), where `node/` and `bridge/` build the configs.
5. The alias is `OTAApplier`; `mattercore.OtaApplier` keeps its name (done; CHANGELOG, advanced-seams table and `bx3-aliases.py`).
6. The committed `reference-controller` binary is removed (done).
7. This report, counts and re-run order updated.

## Left out (the owner's)

- `cluster/core/operational_credentials.go` (6 inline `slog.Default()` calls), `general_commissioning.go` (3) and
  `access_control_auxiliary.go:102` (1): the owner does them after the merge.
- The mutexes of the security paths (`access_control.go`, `operational_credentials.go`, `group_key_management.go`,
  `general_commissioning.go`, `secure/sigma/initiator.go`) and the 22 others listed in section 3 (more than seven guarded fields, irregular or missing Lock sites).
- `core.BridgedConfig.Logger` has no caller that sets it from the node (no logger in reach in `endpoint.ClusterServers`).
- The `OTA*` and `DeviceTypeValidation*` constants next to the new aliases are not re-exported.
