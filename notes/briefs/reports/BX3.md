# BX3 report — gate-1 sweeps (2026-10-20)

Brief `notes/briefs/2026-10-20-BX3-sweeps.md`; branch `m4/bx3` off `main` at `ab4303fd`. Strings, comments, aliases, one
field per config and one dump removal; no behaviour changed except where named below. `make race` not run: nothing here
adds a goroutine, a lock or a shared write (the one new package variable, `tlv.Logger`, is documented as set once before use).

## Scripts

All in `notes/briefs/reports/BX3-scripts/` (and the scratchpad), idempotent, run from the repository root on a merged tree:

| Sweep | Script |
| --- | --- |
| 1 error prefixes | `bx3-prefixes.py` (prints one line per changed message) |
| 2a log names | `bx3-logs.py` |
| 2b loggers | `bx3-loggers.py` |
| 3 mutex comments | `bx3-mutex.sh` (wraps `bx3-mutex.go`, a go/ast pass; dry run by default, `-apply` writes) |
| 4 facade aliases | `bx3-aliases.py` |
| 5 the dump | `bx3-dump.py` |

After BX1/BX2 merge, re-run in this order: `bx3-dump.py`, `bx3-prefixes.py`, `bx3-logs.py`, `bx3-loggers.py`,
`bx3-aliases.py`, `bx3-mutex.sh -apply`, then `gofumpt -l .`, `go build ./...`, `go test ./...`. A script that finds its anchor gone
asserts and says which; a rewritten region is a manual merge. The test edit `node/system_set_wiring_test.go` is part of `bx3-logs.py`.

## 1 Error prefixes (G1-9) — 485 messages

Commit `error prefixes`. 417 `errors.New`/`fmt.Errorf` literals plus 68 further string literals of the old `matter:` prefix
(typed errors, `im.UnsupportedCommandf`, `im.ErrUnsupportedCommand`). By package: `cluster/core` 208, `store` 131 (`matter store:`
to `store:`, most are `fmt.Errorf` wraps), `bridge` 19, `controller/commissioner` 14, `controller/attestation` 14,
`cluster/thermo` 13, `cluster/cover` 13, `cluster/lock` 11, `node` 10, `cluster/closure` 10, `controller/dcl` 9,
`internal/chipdm` 7, `cluster/wire` 7, `im` 4, `cluster/light` 3, `cluster/appliance` 3, `mdns` 2, and one each in
`transport/btp`, `secure/spake2`, `secure/sigma`, `secure/attestation`, `cluster/spec`, `bdx/otaimage`.
Three prefixes were kept behind the package name rather than replaced, because they name a sub-area, not a package:
`node: ota: image carries no payload`, `spake2: hex: odd length`, `chipdm: conformance: …`.

No test asserted a changed message (the full suite passed with no test edit); `docs/` had no occurrence;
`internal/chiptool` reads no `DebugText` (grep), so nothing there changed.

Left as it is, with the reason:

- `contract/` panics `matter: …` (`dataversion.go:59`, `matter.go:412,467,470`): `contract` is not one of the 23 directories and
  is the stdlib-only boundary; the brief names no prefix for it. A decision: `contract:`?
- `spec.Errorf(im.Status…, "doorlock: …")`, `thermostat: …`, `windowcovering: …`, `closuredimension: …`, `microwaveovencontrol: …`,
  `colorcontrol: …`, `temperaturecontrol: …`, `closurecontrol: …` (about 200 literals in `cluster/lock`, `thermo`, `cover`, `closure`,
  `appliance`, `light`): these are status-carrying messages and typed-error texts that the dispatcher maps by status (G1-8), not
  sentinels, and the cluster name is the more useful prefix there. They are the sub-package prefixes the brief lists, but the brief
  scopes the rule to `errors.New`/`fmt.Errorf` sentinels. Where the same prefix started an `errors.New`/`fmt.Errorf` literal it was
  replaced. If the owner wants these too, `bx3-prefixes.py` takes the extra literals in its `matter` pass; say so.
- `fmt.Errorf` literals without a known sub-package prefix (they wrap) were not given one.
- `endpoint/dispatcher_test.go:1478-1479` keep `matter: …` strings: fixtures for the legacy wording heuristic (G1-8), not messages.
- Wire payload: `operational_credentials.go` puts `err.Error()` into `NOCResponse.DebugText` at about a dozen sites; the errors come from
  `store` (shorter now) and `mattercert` (untouched). No chip-tool case reads it.
- Security paths got prefix text only: 92 literals across `cluster/core/{operational_credentials,group_key_management,access_control*,general_commissioning}.go`
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

### 2b loggers — 29 of 44 calls, 8 config fields (commit `loggers`)

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

One test edit comes with this: `transport/udp/listener_test.go` swapped `slog.Default()` after the listener was built; the
listener now keeps the logger it was built with, so the test sets `a.log` instead. (A host that calls `slog.SetDefault` after
building a component of this table keeps the old default in it — the same as the existing constructors that resolve `slog.Default()`
at construction.)

`node.Config` does not yet pass its logger into these configs; that is wiring (not a string) and is listed for a follow-up, so today
nothing changes for a host that sets no `Logger`.

Left, 15 calls, because the type has no config struct to carry the field or the file is a security path:

- `cluster/core/general_diagnostics.go:919,923` (`NewGeneralDiagnostics(bootReason)` and `…With(bootReason, features)` are positional;
  the features struct is not a logger home);
- `mdns/zeroconf.go:637` (`NewZeroconf()` takes nothing; `SubtypeResponder` already takes a logger, `Zeroconf` does not);
- `bridge/handlers.go:746,793` (`CaseAdapter` is built as a bare `&CaseAdapter{}`; the lines log the first 128 bytes of Sigma2 in hex at
  debug, which may deserve a look of its own);
- security paths, strings only: `cluster/core/operational_credentials.go` 6, `general_commissioning.go` 3, `access_control_auxiliary.go:102`
  (its `AccessControl` is in the `access_control.go` path, built with a positional store).

For these a decision is needed: a `SetLogger` like `DiagnosticLogs.SetLogger`, or a config struct. The one print in
`ble/bluez/cmd/reference-controller/main.go:21` stays: `fmt.Fprintln(os.Stderr, …)` in a `main` package is the CLI's own output.

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

## CHANGELOG

`[Unreleased]` `### Changed` (prefixes, log events, `tlv.Logger`, the eight logger fields, the aliases with the signatures that now
spell `node.X`, the mutex comments) and `### Removed` (the dump).

## Guards

The exit lines with HEAD are in the PR description. `make reachability` and `make cite-check` were regenerated and committed: the
unreached count stays at its ratchet 5 (the new exported names are whitelisted), the citation drift keeps its 28 entries and only
their line numbers moved. `make race` was not run (strings, comments, aliases, one write-once variable).

## Questions

1. `contract/` panics still say `matter:`; `contract:`?
2. The ~200 `spec.Errorf`/typed-error status texts with cluster-name prefixes: leave (cluster name is the useful prefix) or rename to the package?
3. The five types without a config (`GeneralDiagnostics`, `Zeroconf`, `CaseAdapter`, `OperationalCredentials`/`GeneralCommissioning`/`AccessControl`):
   `SetLogger` or a config struct?
4. `node.Config.Logger` into the eight new fields: a follow-up.
