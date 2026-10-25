# Brief CJ — the facade gaps found by go-fabric-server S1 and S2 (Q1–Q19): one bundle

Date 2026-10-25. Source: go-fabric-server's reports `docs/reports/S1.md` and `docs/reports/S2.md`
§Questions (repository `SukramJ/go-fabric-server`, tag `v0.1.0`; local
clone `/home/markus/Dokumente/GitHub/go-fabric-server`), concept v8
(`docs/concept-v8-fabric-server.md` E.2: a gap in the controller facade is a
facade fix in go-fabric, never a workaround in the server), ADR 0020 (the
controller role), ADR 0025 (facade design principles), ADR 0026 P1/P12.
go-fabric `main` after CH (M5 23+24) has merged and `v0.12.0` is tagged.
Branch `m5/controller-facade`, commits `m5 CJ: <step>`, one pull request,
no auto-merge, no label. Version: **`v0.12.1`** (additions only; one
Break if §1.11 moves `Config.DB` — then `v0.13.0` and a migration-guide
line; decide and say which). English. Never add attribution lines. The
owner merges.

**Model:** Opus. **Owner paths:** `secure/*`, `client/handshake`, the four
`cluster/core` servers stay untouched; §1.4 and §1.5 need bytes from
`secure/setup` and `secure/spake2` — the facade functions **call** the
existing exported functions of those packages (read what
`examples/reference-controller/cli` calls today), they do not change them.
Every new facade symbol follows ADR 0025 (spec-near names, one shape,
owned types; no `spec.Node` or `tlv` type leaks except as the owned alias
the brief names), has a doc comment with the matter.js counterpart cited
at `862ad75f` where one exists, a `fabrictest` test, a CHANGELOG line, and
an entry in `docs/guides/ports.md` or the controller guide (read which page
describes the controller facade: grep `controller.New` in `docs/`).
The reference CLI (`examples/reference-controller/cli`) moves onto each new
route in the same commit where it duplicated the logic — the CLI is the
second consumer and must not keep a private copy of what the facade now
offers.

## 1 The gaps, in the order to implement

- **1.1 (Q1-test) A controller on a `fabrictest` site.** `controller.Config`
  gains what a site injects: `Clock clock.Clock`, and a transport seam
  (read `fabrictest/controller.go:106-240` for what `AddController` builds —
  listener, exchange manager, names, resolver; the cleanest route is
  `fabrictest.Site.ControllerConfig(opts) controller.Config` returning a
  config whose `Listener`/`Exchanges`/`Names`/`Resolver`/`Clock` fields are
  the site's, so `controller.New` builds on them instead of its own).
  Test: `controller.New` on a site commissions a site device and reads an
  attribute, on the fake clock, paced by the site.
- **1.2 (Q1) A JSON value route on the controller facade.** `client.Value`
  (owned alias of the decoded tree the facade returns today) with
  `client.EncodeJSON(field, raw json.RawMessage) (Encodable, error)` and
  `client.DecodeJSON(v Value, Form) (json.RawMessage, error)` where `Form`
  is `TagBased` (structs keyed by field id) or `NameBased` (camelCase field
  names from the snapshot), bytes base64, 64-bit integers as plain
  decimals; nullable and lists as the snapshot says. The tables come from
  `schema`/`cluster/spec` inside the facade; the server's `internal/wire`
  exception disappears once this lands (say so in the report so the server
  bundle S2 removes it). Tests: round trips against the fixtures the
  server copied (`go-fabric-server/protocol/testdata/compat/*.json` — copy
  the ones that carry values into `controller/client/testdata/`).
- **1.3 (Q2) Per-node hooks on the controller.** `controller.Config.HooksFor
  func(id string) client.Hooks`, passed to the registry as
  `client.RegistryConfig.HooksFor` already is.
- **1.4 (Q3) Discovery on the controller.** `Controller.Discover(ctx,
  discovery.Identifier, timeout) ([]client.Device, error)` (the function
  `fabrictest.Controller.Discover` has; the CLI's `discover` command moves
  onto it).
- **1.5 (Q4) The pairing-code decoder on the facade.**
  `controller.ParsePairingCode(code string) (PairingCode, error)` with the
  decoded fields (version, vendor, product, commissioning flow,
  discovery capabilities, discriminator, passcode, TLV extras present or
  not) and `controller.ErrPairingCode` as the sentinel `Pair` wraps.
- **1.6 (Q5) The commissioning window from the controller.**
  `client.Node.OpenCommissioningWindow(ctx, timeout time.Duration, opts)
  (client.CommissioningWindow, error)` returning passcode, discriminator,
  QR payload and manual code, vendor and product id — the ECM flow
  (`OpenCommissioningWindow` with a fresh PAKE verifier from
  `secure/spake2`, as the CLI does today; `openBasicCommissioningWindow`
  as an option).
- **1.7 (Q6) Attestation rejection as a sentinel.**
  `commissioner.ErrAttestationRejected`, wrapped where the commissioner
  refuses the device's certificates (read `controller/commissioner` and
  `controller/attestation` for the sites); `Pair` returns it through
  `errors.Is`.
- **1.8 (Q7) Batched invokes.** `client.Node.InvokeBatch(ctx,
  []client.CommandRequest, client.InvokeOptions) ([]client.Result, error)`
  honouring the node's `MaxPathsPerInvoke` (split as matter.js's
  `ClientInteraction` does — `client/interaction` already splits, read
  `client/interaction/interaction.go` doc), and `client.Node.MaxPathsPerInvoke()`.
- **1.9 (Q8) The fabric label at run time.** `Registry.SetFabricLabel(ctx,
  label string) error`: persists and reconciles to every commissioned node
  (`client/label.go` already reconciles; make the entry public).
- **1.10 (Q9) The default subscription per node.** `client.Node.SetAutoSubscribe(ctx,
  on bool) error` (stop/start the sustained subscription; persisted in the
  record).
- **1.11 (Q10, Q11) Trust store and database openers.** `attestation.Production()`
  (the DCL snapshot's PAA set as the production trust) and
  `controller.OpenDB(path string) (*sql.DB, error)` (the module's SQLite
  driver, so a host imports none); if `Config.DB` becomes optional with
  `Config.DBPath`, that is additive — decide against a Break unless
  unavoidable.
- **1.12 (Q12) The decoded specification version.** `client.Properties.SpecificationVersion`
  as `client.SpecVersion{Major, Minor, Patch}` with `String()` ("1.4.0").
- **1.13 (Q13) A Thread dataset reader.** `commissioner.ParseThreadDataset(b
  []byte) (ThreadDataset, error)` with network name, extended PAN id,
  channel, PAN id (the TLV layout of the operational dataset — cite the
  matter.js or chip decoder the module already mirrors for provisioning,
  `commissioner`'s Thread path).
- **1.14 (Q14) BLE on `controller.Pair`.** `controller.PairOptions.BLE
  *ble.Config` (or the existing BLE channel type of ADR 0024's stage) so
  `Pair` commissions over BLE and provisions the stored credentials;
  `fabrictest` has no BLE — the test is the unit test of the option's
  routing plus the existing `ble/` tests; say so.

- **1.15 (Q15, S2) Status-bearing errors on the facade.** A host handler
  (`Server.OnWrite`, `Handle<Command>`) can refuse with a Matter status:
  `contract.StatusError{Status uint8; ClusterStatus *uint8}` (stdlib-only,
  implements the `im` status interfaces through the dispatcher's
  recognition — read `im.StatusCodeError`/`MatterClusterStatusError` and
  where the dispatcher maps a handler error), with `device.Timeout` or the
  IM's `Timeout` status for a late answer; the dispatcher answers the
  controller with that status instead of FAILURE.
- **1.16 (Q16, S2) Identify on a bridged device.** The assembler mounts
  Identify itself and refuses `identifydef.Declare`; a host that wants to
  blink its device needs `Device.Identify() *identifydef.Server` with
  `HandleIdentify`/`HandleTriggerEffect` reaching the host (matter.js
  `IdentifyServer` calls the application's `identify` hook —
  `packages/node/src/behaviors/identify/IdentifyServer.ts` at the pin, cite).
- **1.17 (Q17, S2) A subscription-count observer.**
  `node.Observers.OnSubscriptionsChanged func(active int)` (or the metrics
  gauge `subscriptions.active` is enough and the doc says so — decide, the
  poll in the server goes).
- **1.18 (Q18, S2) The offline vendor table.** `controller/dcl/snapshot`
  exposes `Vendors()` (the embedded DCL snapshot's vendor list) so a vendor
  name resolves without network.
- **1.19 (Q19, S2) A daemon-composed node on a site.**
  `fabrictest.Site.AddNode(cfg node.Config) *Device` (the site injects its
  network, clock and resolver into the config the daemon built, so the
  daemon's own composition is what the test runs) — the device-side twin
  of 1.1.

- **1.20 (H1 Q1) BridgedNode's `PowerSourceConfiguration` under `BridgedPowerSourceInfo, D`.**
  A declared PowerSource on a bridged device makes the validator demand
  PowerSourceConfiguration ("BridgedNode PowerSourceConfiguration
  mandatory") while the kit refuses `powersourceconfigurationdef.Declare`.
  Read matter.js's conformance for `D` (deprecated) in
  `DeviceTypeConformance.ts`/the conformance compiler at the pin: a
  deprecated element is allowed, never required; fix the validator (and
  `device/base.go`'s comment) accordingly, with a test on an OnOffLight
  declaring PowerSource under `fabrictest` that shows zero violations.
- **1.21 (H1 Q2) Defaults for mandatory nullable attributes.** A kit
  server with no value for a mandatory nullable attribute answers a
  wildcard read with `UnsupportedAttribute` (TemperatureMeasurement and
  the other measurement clusters' MeasuredValue, OccupancySensing
  OccupancySensorType, SmokeCoAlarm BatteryAlert, FanControl FanMode).
  matter.js serves null for a nullable attribute without a value and the
  schema default otherwise (`Datasource` initialisation / `ValueManager`
  — cite); the kit's `device.New` defaults (`device/defaults*.go`) gain
  the rule, and the validator (or a new test over every committed type)
  catches a mandatory attribute a wildcard read would not serve.

- **1.22 (H2 Q1) WindowCovering with Lift alone.** `device.New` refuses a
  cover whose FeatureMap names Lift (0x1) without PositionAwareLift
  ("FeatureMap names features this server does not serve: 0x1"). Read
  `WindowCoveringServer.ts` at the pin for which features the server serves
  and how Lift-only (UpOrOpen/DownOrClose/StopMotion, no `CurrentPositionLiftPercent100ths`)
  is handled; the kit serves what matter.js serves, the validator refuses
  what matter.js refuses. If matter.js serves Lift alone, the Go server does
  too; cite the lines. (go-hamapping's `rows/cover.yaml` then keeps its
  feature choice; it must not declare a position feature the entity lacks.)
- **1.23 (H2 Q2) A daemon's own `node.New` on a fabrictest site.** Same
  route as 1.19 (`fabrictest.Site.AddNode(cfg)` taking a `node.Config`);
  H2's end-to-end runs on `fabrictest.Device.Node()` until it exists —
  note it in the report so the daemons move onto it.
- **1.24 (H2 Q7) Docs.** `docs/guides/testing-in-process.md`: one sentence
  that `ReadCluster` on a removed endpoint returns no values and no error.

- **1.25 (owner 2026-10-25, ADR 0029) ICD keys encrypted at rest.** The
  `icd_key` column of `matter_icd_registrations` is stored encrypted as the
  DoorLock credentials are (`cluster/lock/auth.go` `NewCredentialCipher`,
  AES-128-GCM, random nonce, key under a metadata key as `door_lock.credential_key`):
  a metadata key `icd_management.key`, migration v8 re-encrypting existing
  rows, `store/storetest` case, persistence guide table rows, CHANGELOG
  (no Break: the port's signature is unchanged; `ICDRegistration.Key` stays
  clear in memory). Update `BD-Matter-ICD-RegistrationsInStoreArea` and
  ADR 0029's consequence to "done in v0.12.1".

## 2 Docs, tests, report

- Docs: the controller guide page (or `docs/guides/ports.md`) gains a
  section per group (site-hosted controller, JSON values, hooks, discovery,
  pairing codes, commissioning window, batch invokes, labels, auto-subscribe,
  openers, spec version, Thread dataset, BLE); `docs/matterjs-comparison.md`
  rows touched by these (controller role row :40) if the status text
  changes; CHANGELOG `### Added` per group; migration guide only on a Break.
- Tests: one `fabrictest` test per symbol (1.1 enables most of them), the
  CLI's tests on the new routes, `make apidiff-check` names every addition.
- Guards: `make ci`, `make race` twice, `make archcheck`, `make reachability`
  then `-check`, `make cite-report`/`cite-check` (MATTERJS_DIR explicit),
  `make testparity-check`, `go test ./script/...`, `mkdocs build --strict`,
  `make stress-flakes` once. Report `notes/briefs/reports/CJ.md`: per gap
  the symbol, the file, the CLI site that moved, the test; **Guards** with
  HEAD, **Differences**, **Decisions**, **Left out**, **Questions**,
  `by_design` candidates. Copy this brief to
  `notes/briefs/2026-10-25-CJ-controller-facade-gaps.md` in the first commit.
