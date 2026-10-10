# Brief E — labels, localization and diagnostics on the root node (phase 2a)

Owner: Fable. Implementer: Opus. Branch `wave2/root-clusters`, pushed, **no PR**.

## Facts

- Clusters, all optional on RootNode or on any endpoint, all on
  `spec.Server` (PR #40): FixedLabel (0x0040, exists: `core.NewFixedLabel`),
  UserLabel (0x0041), LocalizationConfiguration (0x002B),
  TimeFormatLocalization (0x002C), UnitLocalization (0x002D),
  SoftwareDiagnostics (0x0034), EthernetNetworkDiagnostics (0x0037).
  Verify every id in `parity/schema.json`.
- matter.js rules (transcribe, cite path):
  - `user-label/UserLabelServer.ts`: a LabelList write longer than
    `maxLabels` (default 255) is refused with RESOURCE_EXHAUSTED.
  - `localization-configuration/LocalizationConfigurationServer.ts`:
    ActiveLocale defaults to the detected locale, SupportedLocales to
    `[ActiveLocale]` when unset. No membership check in matter.js.
  - `time-format-localization/TimeFormatLocalizationServer.ts`: feature
    CalendarFormat; HourFormat, ActiveCalendarType detected when unset,
    SupportedCalendarTypes defaults to `[ActiveCalendarType]`.
  - `unit-localization/UnitLocalizationServer.ts`: feature TemperatureUnit;
    TemperatureUnit defaults to Celsius, SupportedTemperatureUnits to
    `[Celsius, Fahrenheit]` when empty.
  - SoftwareDiagnostics, EthernetNetworkDiagnostics: empty subclasses.
- Rules matter.js does not model but the certification cases test (a
  written ActiveLocale not in SupportedLocales, an ActiveCalendarType not in
  SupportedCalendarTypes, a TemperatureUnit not supported): read
  connectedhomeip at the harness pin,
  `src/app/clusters/localization-configuration-server/`,
  `time-format-localization-server/`, `unit-localization-server/`,
  `user-label-server/`, `software-diagnostics-server/`,
  `ethernet-network-diagnostics-server/`; transcribe and cite file:line.
- The "detected" defaults are the host's in Go: the server takes them in
  its config; the reference daemon passes `de-DE`/`en-US` as it sees fit,
  24-hour, Gregorian, Celsius. The daemon's root clusters are assembled in
  `examples/reference-bridge/wiring.go` (BasicInformation at ~95,
  GeneralDiagnostics ~111, TimeSynchronization ~235, DiagnosticLogs ~239).
- SoftwareDiagnostics values from the Go runtime (`runtime.MemStats`:
  CurrentHeapFree, CurrentHeapUsed, CurrentHeapHighWatermark; feature
  WTRMRK and ResetWatermarks only if the host provides it; ThreadMetrics
  optional and may stay out). EthernetNetworkDiagnostics without PKTCNT /
  ERRCNT features: PHYRate, FullDuplex, CarrierDetect, TimeSinceReset as
  the host reports them, nullable where the host cannot.
- Families: FLABEL (1), ULABEL (4), LCFG (1), LTIME (1), LUNIT (1), DGSW (4),
  DGETH (3); declared in `internal/chiptool/families_table_test.go`
  (device type 0 = root for these).

## Scope

Must:
1. Definitions via clustergen for the six new clusters.
2. Servers in `cluster/core` (one file each) on `spec.Server`, with the matter.js defaults, the chip-cited write rules, UserLabel's RESOURCE_EXHAUSTED, a host port for the diagnostics values; parity tests per cluster (ids, revision, lists against the snapshot; the negative-write cases).
3. Reference daemon root: mount FixedLabel (two labels), UserLabel (persisted through a small host store, as NodeLabel is), the three localization clusters, SoftwareDiagnostics, EthernetNetworkDiagnostics.
4. Family rows; CHANGELOG; floors; reachability.

Not in scope: PICS/golden (owner). Do not touch `cluster/measurement/`, `cluster/thermo/`, `cluster/modebase/`, `examples/reference-bridge/fleet_*.go`.

## Guards
As brief D. `TestCertifiabilityDocument` red until fixtures land.
