# Wave 5 review — theme `api`

- **Scope:** the facade as a host reads it. `node/` — `go doc` of the whole package; `doc.go`, `config.go` (whole), `observers.go` (whole), `lifecycle.go` (whole), `credentials.go:14-110`, `commissioning.go:395-492`, `node.go:150-300`, `:440-500`, `:660-1010`, `:1048`, `aggregator.go:30-600`, `inspect.go:14-125`, `sessions.go:1-112`, `diagnostics.go:30-60`, `health.go:20-40`, `client.go:30-50`, `surface.go:26-125`, `ota_provider.go:30-120`, `commissioner_control.go:54-56`, `facade.go:35`. `contract/` — `go doc`, `go list -deps`, `status.go`, `doc.go`, `dataversion.go:37-80`, `metrics.go`, `matter.go:395-480`, `:640-750`, `:880-893`, `devicetype_names_gen.go:1-30`. `device/` — `go doc` (hand-written surface), `doc.go`, `type.go`, `options.go`, `fromspec.go`, `surface.go` (all whole), `device.go:1-330`, `:385-425`, `:555-592`, `:673-690`, `lifecycle.go:100-251`, `identify.go:65-80`, `defaults.go:1-130`, `defaults_internal_test.go:24-45`; generated output spot-checked on `on_off_light_gen.go`, `door_lock_gen.go`, `thermostat_gen.go`, `types_gen.go:1-13`, `script/devicegen/labels.go:15-64`, `devicetypes.go:10-40`, `:122-133`. `eligibility/` (all three files whole). `surface/` — `go doc`, `doc.go:1-97`, `policy.go:123-170`, `:226-320`, `level.go:265-340`.
- **HEAD:** `769ea3c2`.
- **Pins:** matter.js `862ad75f` (read: `packages/node/src/node/ServerNode.ts:100-175`, `Node.ts:114-132`, `:255-275`, `packages/node/src/behavior/system/sessions/SessionsBehavior.ts:50-127`, `packages/node/src/behavior/system/commissioning/CommissioningServer.ts:305-312`, `:470-473`, `packages/protocol/src/protocol/DeviceCommissioner.ts:125-140`, `:315-330`, `packages/node/src/behaviors/bridged-device-basic-information/BridgedDeviceBasicInformationServer.ts:30-40`, `:96-104`). connectedhomeip `6170af84` was not needed: no finding turns on tool or controller behaviour.
- **Method:** slices only. ADR 0018, 0025, 0026, 0030, `README.md` §API stability, `docs/guides/advanced-seams.md`, `docs/guides/migrating-to-the-facade.md`, `docs/guides/operations.md` §Factory reset and `notes/parity/by_design.md` were checked for each candidate; a recorded decision is not listed. W2-api-01…13 (bridge ports, endpoint numbering, `MatterRead` without ctx) are not repeated; where a W5 line extends one, it says so.

No high findings. Five medium, eleven low, two notes.

---

## W5-api-01 — medium — the facade can open a window but not close one; the route it points to hands out the window's wiring

**Where:** `node/node.go:977` (`Node.CommissioningWindow() *CommissioningWindow`), `node/node.go:989-1000` (`SetCommissioningBlocked`), `node/commissioning.go:450-492` (`OpenBasicWindow`), `node/doc.go:59-65`, `bridge/commissioning_window.go:322-326` (`SetTransitionHook`).

**Claim:** The facade's window API is `OpenBasicWindow`, `Window()` and `SetCommissioningBlocked`. Closing a window, or reopening the node's own window with the configured credentials, has no facade route: the doc comments point to `CommissioningWindow().OpenOwnWindow` / `RevokeWindow` on the `bridge.CommissioningWindow` alias, and no page under `docs/guides/` names either. That type also exports the facade's wiring — `SetTransitionHook` *replaces* (`w.onTransition = fn`) the hook `node/commissioning.go:97` installs, which drives `OnWindowTransition` and the last-fabric reopen; `SetFabricCounter`, `SetFailSafeArmer`, `SetPaseSessionCloser`, `SetPaseVerifierInstaller`, `SetClock` likewise. `node/doc.go:62-64` warns about it in prose only. And the seam's `SetBlocked(ctx, nil)` differs from `Node.SetCommissioningBlocked(ctx, nil)`, which also reopens the own window on a decommissioned node — two calls for one decision with different results.

**Reference:** matter.js gives the host the pair on one object: `DeviceCommissioner.allowBasicCommissioning(options)` (`DeviceCommissioner.ts:130`) and `endCommissioning()` ("Close the open commissioning window, whoever opened it", `:315-325`). ADR 0025 principle 3 (no leaked types) and 4 (seams are not the facade).

**Proposed change:** Add `Node.CloseWindow(ctx)` (and `Node.OpenOwnWindow(ctx, timeout)` if the configured-credential window stays a host action); make `Node.CommissioningWindow()` return a read-only facade view or move it to the advanced-seams table with the replace-not-chain warning.

**API break:** adding the methods: no. Narrowing `Node.CommissioningWindow()`'s return type: yes.

**Confidence:** confirmed.

## W5-api-02 — medium — the reachability probe changes `Reachable` without reporting it

**Where:** `device/options.go:40-44` (`Options.Reachable`), `device/lifecycle.go:112-119` (`SetReachable`), `:190-208` (`Update` replaces the probe), `endpoint/materialize.go:197-198` (probe read per dispatch), `bridge/receive.go:557-559`.

**Claim:** A device has two reachability inputs that AND together: the `Options.Reachable` probe (replaceable through `Update`'s `HostStatement.Reachable`) and `SetReachable`. Only `SetReachable` calls `NotifyReachable` (ReachableChanged event, DataVersion bump, dirty mark). A probe that flips, or a probe swapped by `Update`, changes what a Read returns but reaches no subscription and emits no event — the bridge's own doc says "a Read alone does not push the change to an active subscription" (`receive.go:557-559`). `Options.Reachable`'s doc says nothing of this, so a host that wires the probe (the field's obvious use) gets bridged devices whose offline state Apple Home and Google Home never see until they re-read.

**Reference:** matter.js has one input: the `reachable` state, whose change the server reacts to with the event (`BridgedDeviceBasicInformationServer.ts:37` `reactTo(this.events.reachable$Changed, …)`, `:100-102`). ADR 0025 principle 2 ("one lifecycle (add, reachable, rename, …)").

**Proposed change:** Either drop the probe from the facade (`SetReachable` is the one input, as in matter.js) or state on `Options.Reachable` and `HostStatement.Reachable` that a probe change is not reported and the host must call `SetReachable`.

**API break:** removal: yes; doc: no.

**Confidence:** confirmed (the only `NotifyReachable` caller in `device/` is `SetReachable`).

## W5-api-03 — medium — `node.New` does I/O and starts goroutines without a context

**Where:** `node/node.go:164` (`func New(cfg Config) (*Node, error)`), `:182` (`ctx := context.Background()`), `:207` (`openStores`), `:281` (`groups.Load`), `:161-163` (doc: New starts the subscription engine and the PASE/CASE reapers), `node/commissioning.go:362` (`StartReaper(context.Background(), …)`).

**Claim:** `New` opens and migrates the SQLite store (`Store.Path`/`DB`), loads the group state and reads the store under `context.Background()`, so a host cannot bound or cancel construction, and it starts goroutines that only `Stop` ends — a node that is constructed and dropped without `Stop` leaks them. Every other lifecycle call (`Start`, `Stop`, `Run`, `OpenBasicWindow`, `Aggregator.Add`) takes a `ctx`; the constructor is the one that does the slowest work.

**Reference:** matter.js construction is asynchronous and awaited (`ServerNode.create`, `ServerNode.ts:107-127`, `asyncNew`). ADR 0026 principle 5 (every goroutine has an owner, started by an explicit call).

**Proposed change:** `New(ctx, cfg)`, and move the goroutine starts (subscription engine, reapers) into `Start` so that `New` allocates and `Start` runs.

**API break:** yes (signature).

**Confidence:** confirmed for the signature and the `Background` I/O; the goroutine move is a design proposal.

## W5-api-04 — medium — four callback shapes where ADR 0025 asks for one rule

**Where:** `node/observers.go:35-90` (`Observers`: `func(…) error`, no `…Async`), `:226-257` (`Node.Observe`), `node/health.go:35` (`Health.OnSample func(HealthSample)`, "must not block", no unsubscribe), `node/ota_provider.go:38` (`OTAProviderConfig.OnStatusChange`), `node/diagnostics.go:40-53` (`OnDiagnosticEvent` / `…Async`), `node.ICD.OnModeChange` / `…Async`, `device.Device.OnChanged` / `…Async`.

**Claim:** The facade delivers host callbacks in four forms: a config struct of error-returning funcs plus `Observe` (fabric, commissioning, window, surface), single config fields with no unsubscribe and no queued variant (`Health.OnSample`, `OnStatusChange`), and method pairs with an `…Async` variant (diagnostic events, ICD mode, device changes). The `Observers` doc tells a blocking host to "hand it to its own goroutine", while the other half of the facade offers the queued variant ADR 0025 names. Only `Observers` return an error.

**Reference:** ADR 0025 principle 8: "a host that may block chooses the queued variant the facade offers (`…Async`) … The rule is one sentence in the docs and the same for every callback." matter.js: every node event is an `Observable` with `on`/`off` (cited at `observers.go:251-255`).

**Proposed change:** Give `Observers` an async registration (`ObserveAsync`) and move `Health.OnSample` and `OnStatusChange` to `On…`/`On…Async` methods with unsubscribe; drop the `error` return or document why only these hooks carry one.

**API break:** yes for moving the config fields; adding `ObserveAsync`: no.

**Confidence:** confirmed.

## W5-api-05 — medium — the device kit logs through a package global captured at init

**Where:** `device/defaults.go:96-98` (`var Logger = slog.Default()`), `device/device.go:405`, `device/defaults_netinfra.go:36`, `device/defaults_threadbr.go:42`, `:64`; `device/defaults_test.go:167` (`//nolint:paralleltest // swaps the package Logger`).

**Claim:** `device.Logger` is evaluated once at package init and read without synchronisation by every `New`. `node.Config.Logger` never reaches it, so a host that injects its logger into the node still gets the kit's `device.default_server.generated` lines and the Thread servers' logs on the process default captured before `main` ran; replacing the variable races with concurrent device construction, which is why the package's own test cannot run in parallel.

**Reference:** ADR 0026 principle 9: "Every component takes a `*slog.Logger` (nil = `slog.Default()`) … injected loggers".

**Proposed change:** Resolve the logger at use from the node the device is attached to (`deviceseam.Host`) or from `Options`, nil meaning `slog.Default()` at call time; remove the variable.

**API break:** yes (removes `device.Logger`).

**Confidence:** confirmed.

## W5-api-06 — low — after `Stop` the Aggregator keeps accepting changes; a failed `Start` reports as started

**Where:** `node/aggregator.go:580-584` (`setStarted`, never reset), `:482-485` (`attach` binds nothing once closed), `:343-372` (`Partition.Batch` reassembles when `started`), `bridge/bridge.go:954-960`, `:1775`; `node/node.go:728` (`started.Store(true)`), `:748-750`.

**Claim:** `Node.Stop` leaves `Aggregator.started` true, so an `Add`/`Replace`/`Remove` on a stopped node changes the set, binds nothing (the device's `SetReachable`/`Rename` then go nowhere), and returns `node: reassemble: bridge: not started` instead of `node.ErrStopped`, which `Start` returns. Separately, `Start` sets `started` before `attachPartsListProviders`; when that fails it returns the error without rollback, and a retry returns `nil` for a half-started node.

**Reference:** matter.js serialises lifecycle through one mutex and refuses work on a closed node (`Node.ts:114-132`, `:193`); `node/node.go:690-697` promises "after Stop it is ErrStopped".

**Proposed change:** Refuse Aggregator mutations after `Stop` with `ErrStopped`; set `started` only once `Start` has completed, or `Stop` internally on a failed `Start`.

**API break:** no (error value and state only).

**Confidence:** confirmed.

## W5-api-07 — low — lower-layer types on `Node` that the advanced-seams list does not name

**Where:** `node/sessions.go:110` (`SessionParameters() sessionparams.Parameters`), `node/ota_provider.go:104` (`OTAProvider() *mattercore.OTAProvider`), `:119` (`BDX() *bdx.Responder`), `node/commissioner_control.go:56` (`CommissionerControl() *mattercore.CommissionerControl`), `node/client.go:46` (`Client.Names *discovery.Names`).

**Claim:** These return or take types from `secure/sessionparams`, `cluster/core`, `bdx` and `client/discovery` directly, with no alias the facade owns, and `docs/guides/advanced-seams.md` lists none of them (it lists `Peers`, `Bindings`, `Sessions`, `Subscriptions`, `Store`, `Bridge`). `migrating-to-the-facade.md:181` already replaced `sessionparams.Parameters` with `client.SessionParameters` on the controller side, so the node side is now the odd one.

**Reference:** ADR 0025 principle 3 ("A lower package's type reaches a host only through the advanced seams") and principle 4 (seams listed in `advanced-seams.md`).

**Proposed change:** List them in the advanced-seams table, or give them owned aliases (`node.SessionParameters = client.SessionParameters`, `node.OTAProvider`, …) as the root servers already have.

**API break:** no (aliases and docs).

**Confidence:** confirmed.

## W5-api-08 — low — second forms in `device/`

**Where:** `device/options.go:86` (`type Option = Declaration`), `:298` (`WithServer`), `:316` (`HostServer`); `device/type.go:147` (`TypeByID(schema.DeviceTypeID)`), `:168` (`Lookup(uint32)`).

**Claim:** `HostServer(srv)` covers every case `WithServer(srv)` covers (it falls back to `WithServer` for a cluster without a package), so the kit has two calls for "mount the host's server", plus the per-cluster `Declare{HostServer: srv}`. `Option` is a second name for `Declaration`. `TypeByID` and `Lookup` answer the same question from the same registry, differing only in the argument's type. The prefix convention is mixed too: `WithServer`, `WithChangeNotifier`, `WithClientClusters` beside `HostServer`, `Measurement`.

**Reference:** ADR 0025 principle 2 ("A second form for a special case is a defect, not an option"); `migrating-to-the-facade.md:237` records why `WithServer` survived, not why both should.

**Proposed change:** Keep `HostServer` and remove `WithServer`; drop the `Option` alias; keep one of `TypeByID`/`Lookup` (the 32-bit one, which a controller needs).

**API break:** yes (removals; the removal rule of `README.md` §"Suspended until a second consumer" applies).

**Confidence:** confirmed.

## W5-api-09 — low — second forms in `node/`

**Where:** `node/node.go:1048` (`PairingCodes`), `node/inspect.go:42-60` (`SetupPayload`), `node/credentials.go:101` (`PairingCodesFor`); `node/aggregator.go:523` (`IncreaseConfigurationVersion(key)`), `:547` (`IncreaseConfigurationVersions(keys...)`); `node/config.go:367` (`Commissioning.PBKDFIterations int`) vs `node/commissioning.go:421` (`WindowCredentials.Iterations uint32`), `node/credentials.go:79-92` (the clamp between them).

**Claim:** `PairingCodes()` returns a strict subset of `SetupPayload()`. `IncreaseConfigurationVersions` with one key does what `IncreaseConfigurationVersion` does; both take no `ctx`, persist under `context.Background()` (`:530`, `:576`) and fail with an unexported `errors.New("node: no topology yet")` where every sibling method returns a sentinel. The two credential structs name and type the same PBKDF count differently, which is why `Commissioning.WindowCredentials` needs a clamp.

**Reference:** ADR 0025 principles 2 and 7 (sentinel errors with `errors.Is`).

**Proposed change:** Remove `PairingCodes` (and keep `PairingCodesFor`) or fold it into `SetupPayload`; keep the variadic form with a `ctx` and a sentinel (`ErrNotStarted`); align the field to `PBKDFIterations uint32` in both structs.

**API break:** yes.

**Confidence:** confirmed.

## W5-api-10 — low — four opt-in idioms in one `Config`

**Where:** `node/config.go:77-110` (`OTA`, `ICD`, `TLS`, `OTAProvider`, `CommissionerControl`: pointer, nil = off), `node/client.go:36` (`Client.Enabled bool`), `node/config.go:158` + `:166` (`DisableTimeSynchronization bool` beside `TimeSynchronization *…`, excluded by `Validate` at `:600-606`), `:289` (`Network.DisableReannounce`), `node/mdns.go:17` (`MDNS.Disabled`), `node/health.go:20-35` (on when `OnSample` set or `Interval > 0`), `node/config.go:303-325` (`WiFi`, `WiFiCommissioning`: on when a port is set).

**Claim:** A host turns a feature on by a non-nil pointer, by `Enabled: true`, by clearing `Disable…: true`, or by setting an unrelated field. Time synchronisation takes two fields for one tri-state, kept apart only by a `Validate` rule.

**Reference:** ADR 0025 principle 2 ("one shape").

**Proposed change:** Pick one idiom (pointer-nil for optional sub-systems, which most fields already use); fold `DisableTimeSynchronization` into the `TimeSynchronization` field (e.g. a mode).

**API break:** yes.

**Confidence:** confirmed.

## W5-api-11 — low — test and single-user knobs in the public `node.Config`

**Where:** `node/config.go:460-469` (`OtaConfig.Harness`, "TESTING ONLY"), `:274-280` (`Network.WiFiCommissioning`, "TESTING ONLY … the reference daemon's --wifi-sim is the one user"), `:235-249` (`BootID.RotateUniqueIDs`: a per-node field whose effect is process-wide and permanent).

**Claim:** Two `Config` fields are labelled testing-only in their own docs, and `BootID` is a per-instance setting that switches a process global (`enableBootIDRotation`, `node.go:176-177`) for every node in the process, for the life of the process. They are frozen as API with everything else in `Config`.

**Reference:** `README.md` §API stability ("Every exported identifier outside `internal/` is public API"); the module keeps test seams under `internal/` (`bridgeseam`, `channelseam`) or in `…test` packages.

**Proposed change:** Move the harness and Wi-Fi-simulation knobs to `fabrictest` or an `internal/` seam the reference daemon reaches; make boot-id rotation a package-level call (`bootid.EnableRotation`) rather than a node field.

**API break:** yes.

**Confidence:** confirmed.

## W5-api-12 — low — mutable globals in the stdlib-only boundary

**Where:** `contract/dataversion.go:49-51` (`var InitialDataVersion = func() uint32`, "a package-level var for test-side override"), `contract/matter.go:466` (`SetMeasurementMaterializer`).

**Claim:** `InitialDataVersion` is exported and writable for test override, and no file in the module (test or not) assigns it; a host that does changes every server's first DataVersion process-wide and races `DataVersionTracker.Current`. `SetMeasurementMaterializer` exists for one caller, `cluster/measurement/materializers.go`'s init; a host that calls it replaces a built-in projection for every node in the process.

**Reference:** ADR 0026 principle 4 (time and randomness are injected) and `contract/doc.go` ("the property worth preserving").

**Proposed change:** Unexport `InitialDataVersion` (or remove the override); move the built-in materialisers' registration behind an `internal/` seam so `contract` exports only `RegisterMeasurementKind` for hosts.

**API break:** yes.

**Confidence:** confirmed (`grep contract.InitialDataVersion` finds no assignment).

## W5-api-13 — low — `eligibility`: host vocabulary and dead names in the docs; ecosystem claims beside the evidence register

**Where:** `eligibility/eligibility.go:9-21`, `:72-82`; `eligibility/compat.go:66-83`, `:97-148`.

**Claim:** The package doc speaks a host's language ("every DP exposes its verdict", "Custom DPs … Siren tones, Effect light effect dispatch, FixedColorLight palette quantisation"), names interfaces that do not exist (`MatterEndpointSource`, `MatterMeasurementSource`; the contract's are `EndpointSource`, `MeasurementSource`) and links `github.com/SukramJ/go-fabricadapter`, a repository nothing else in the module names. `Compat` hand-codes device type ids (`0x0042`, `0x0043`) where `schema` carries them, and states ecosystem behaviour ("Alexa becomes unresponsive for the entire bridge", the 90-endpoint wall) as "field-verified" with no row in `notes/parity/controller_profiles.md`, the register ADR 0030 decision 8 made the place for what an ecosystem tolerates, with a confidence per row.

**Reference:** `CLAUDE.md` §"Where the host's obligation begins" (host wire knowledge never enters this module); CLAUDE.md workflow 4 (no hand-coded ids); ADR 0030 decision 8.

**Proposed change:** Rewrite the package doc in the module's terms with the real interface names; use `schema` constants; record each `Compat` rule as a register row with its confidence (warnings only, as now).

**API break:** no.

**Confidence:** confirmed for the docs and ids; the register placement is a design proposal.

## W5-api-14 — low — "which extensions exist" has two partial answers

**Where:** `node/surface.go:46-60` (`node.AheadRows`, root servers), `device/surface.go:171-195` (`device.AheadRows`, device servers, with its own copy of the union helper).

**Claim:** A host building the operator setting for `surface.Policies.Extensions` must call both and union them; neither doc says the other exists, and `surface`, where `Extensions` is declared, offers none.

**Reference:** ADR 0030 Consequences ("this module exposes the mechanism and the report"); ADR 0025 principle 2.

**Proposed change:** One `surface.AheadRows()` (or `node.AheadRows()` returning the union) and remove the other.

**API break:** yes (removal of one).

**Confidence:** confirmed.

## W5-api-15 — low — `contract.DeviceTypeName` is one host's label table

**Where:** `contract/matter.go:880-893`, `contract/devicetype_names_gen.go:12-30`, `script/devicegen/labels.go:27-59`.

**Claim:** The function returns humanised labels for 29 device types ("Door Lock", "On/Off Light") and matter.js CamelCase for the rest ("DoorLockController", "PowerSource") — kept because "hosts group, filter and search their exposure rows by it". `schema.DeviceTypeName` returns the matter.js name for the same id, so the module has two name functions that disagree. No file in the module calls `contract.DeviceTypeName`.

**Reference:** ADR 0025 principle 5 (vocabulary) and principle 2.

**Proposed change:** Remove it from `contract` and let the host keep its labels, or label every type one way.

**API break:** yes.

**Confidence:** confirmed.

## W5-api-16 — low — doc comments that contradict the code

**Where:** `node/doc.go:52-53`; `node/doc.go:58`, `node/node.go:158-160`; `node/doc.go:46-48`; `device/type.go:157-161`.

**Claim:** (1) `node/doc.go` says "a nil Config.Advertiser publishes nothing"; `Config.Advertiser` (`config.go:59-63`) and `New`'s doc say nil is the node's own Zeroconf. (2) Both docs advertise the bridge's `SetOnFabricAdded` as a host hook; W2-api-01 found it never fires — these two lines belong in that fix. (3) `node/doc.go` sends a host's operator surface to `Node.Sessions`/`Node.Subscriptions`; `advanced-seams.md:30` leads with `Node.SessionDiagnostics`, the read-only facade view. (4) `Lookup` says CastingVideoClient is on devicegen's excluded list; the list is empty since wave 27-C2 (`script/devicegen/devicetypes.go:122-124`) and `device.CastingVideoClientType` exists.

**Reference:** the code and ADR 0018 addendum 2026-10-29.

**Proposed change:** Correct the four passages.

**API break:** no.

**Confidence:** confirmed.

## W5-api-17 — low — names with more than one meaning or casing

**Where:** `node/facade.go:35` (`type Topology = endpoint.Topology`), `node/config.go:127` (`Config.Topology Snapshotter`), `node/lifecycle.go:145` (`Node.Topology() BridgeTopology`); `node/config.go:77`, `:460` (`OTA *OtaConfig`) vs `OTAProviderConfig`, `OTAApplier`, `OTAImage`; `contract.Origin` vs `surface.Origin`.

**Claim:** In `node`, `Topology` is a type, a config field of a different type, and a method returning a third type. `OtaConfig` is the one mixed-case acronym among a dozen `OTA…` names. `Origin` names the cause of an attribute change in `contract` and the provenance of a surface element in `surface`; `node` uses both.

**Reference:** ADR 0025 principle 5.

**Proposed change:** Rename `Config.Topology` (`Config.Snapshotter`) and `OtaConfig` (`OTAConfig`); consider `surface.Provenance`.

**API break:** yes.

**Confidence:** confirmed.

## W5-api-18 — note — the generated `…Type` variables are the registry and are writable

**Where:** `device/on_off_light_gen.go:23-29` (`var OnOffLightType = typeOnOffLight`, a struct value), `device/types_gen.go:8-13` (`committedTypes = []*Type{&DoorLockType, …}`), `device/type.go:20-21` ("callers must not modify it").

**Claim:** `FromSpec`, `TypeByID` and `Lookup` return pointers into the exported variables, so a host statement like `device.DoorLockType.Revision = 5` or `device.OnOffLightType = device.DimmableLightType` rewrites the registry for every node in the process; only a comment prevents it.

**Reference:** ADR 0025 principle 10 (the same bar inside).

**Proposed change:** Export accessor functions (`device.OnOffLightType()`) over unexported values, or keep the variables but have the registry hold its own copies.

**API break:** yes for accessors.

**Confidence:** confirmed.

## W5-api-19 — note — no session events; subscription counts only as metrics

**Where:** `node/observers.go:35-90`, `node/inspect.go:110-124`, `contract/metrics.go:36-39`.

**Claim:** `Observers` covers fabrics, commissioning, the window and the surface. Session open/close and subscription changes reach a host only as the `sessions.open` / `subscriptions.active` gauges (no peer, no fabric) or by polling `SessionDiagnostics`. A host page that shows which controller is connected must poll.

**Reference:** matter.js `SessionsBehavior.Events` — `opened`, `closed`, `subscriptionsChanged`, `subscriptionAdded` (`SessionsBehavior.ts:123-127`, emitted at `:63`, `:72`, `:82`, `:92`).

**Proposed change:** `Observers.OnSessionOpened` / `OnSessionClosed` / `OnSubscriptionsChanged` carrying a `SessionInfo`.

**API break:** no (additive).

**Confidence:** confirmed (absence).

## W5-api-20 — note — the same policies as a pointer on the device and a value on the node

**Where:** `device/options.go:48-51` (`Options.Surface *surface.Policies`), `node/config.go:143` (`Config.Surface surface.Policies`).

**Claim:** ADR 0030 decision 4 records that each device carries the node's policies and the node compares digests; it does not record why the device's copy is a pointer and the node's a value, so a host writes `Surface: &cfg.Surface` on every device.

**Reference:** ADR 0025 principle 2.

**Proposed change:** One form (value on both, the zero value meaning no policy, as on the node), until the v1.0 move of composition into the node that ADR 0030 defers removes the field.

**API break:** yes.

**Confidence:** confirmed.

---

## Checked and sound

- `contract/` imports the standard library only: `go list -deps ./contract` lists `crypto/internal/entropy/v1.0.0` (stdlib) and nothing of this module.
- `node.Config.Validate` against what is read: every field it requires is consumed — `ProductDescription.VendorID/ProductID` (`root.go:131-133`, `node.go:233-234`, `:261-262`, `:479-480`, `ota.go:231`), `BasicInformation.NodeLabel`, `Commissioning.Discriminator` (bridge, advertisement, `SetupPayload`), the passcode/PBKDF/salt (the verifier), `OTA.Dir`, `TLS.CertificatePort`; `ProductDescription.Name` and `DeviceType` are read by `advertisement` (`node.go:468-474`). No node-side twin of W2-api-02.
- `Start` is idempotent and `Stop` idempotent and safe without `Start`, `ErrStopped` after `Stop`, as `Node.ts:114-132`, `:193`; `Run` stops under a fresh bounded context.
- `Observe`/unsubscribe semantics (idempotent, copy-on-dispatch) and the panic/error containment in `observe`.
- Aggregator number reservation, partitions and `Batch` semantics are documented with their matter.js citations; `Replace` refuses a key change.
- `device.New` refuses a cluster the type does not list, an assembler-mounted cluster, features or initial values beside a `HostServer`, a cluster declared twice, and a declared selection the self-held server does not serve (`ErrSelectionNotServed`; Thermostat without HEAT/COOL, `defaults_internal_test.go:33`). Feature selections are typed (`thermostatdef.Feature`). The revision flows from the generated `Type.Revision` into the surface's `DeviceTypeRef` (`device/surface.go:44`) and from `schema.DeviceTypeRevision` into `node.BridgedDevice` (`lifecycle.go:211-213`).
- Generated shape across OnOffLight, DoorLock, Thermostat: one constructor through `FromSpec`, one typed accessor per mandatory or optional cluster with a known server, nil for an unmounted optional one, assembler clusters without accessors; withheld devices returned with their error as `New` documents.
- `surface.Evaluate`: Refuse > Remove > Warn > Keep, a Remove of a mandatory element kept for every policy including `Custom`; `Digest` covers custom policies by name and `Describe`, in order. A `HostServer`'s and a hand-written server's internal elements are outside the reference and element policies — documented in `surface/doc.go:26-33`, `:51-55`, so not a finding.
- `eligibility.Classify` / `DeriveMatterEligibility` logic; the embedded vendor-name table is by_design (`by_design.md:3149`).
- No factory-reset call: documented (`docs/guides/operations.md` §Factory reset, `BD-Matter-LastFabricReopenWithoutErase`).
- `node.Endpoint` naming `endpoint.Spec` (ADR 0018 decision 4); `Sessions`, `Subscriptions`, `Peers`, `Bindings`, `Store`, `Bridge` as listed seams (`advanced-seams.md`).

## Not read

- `node/`: `security.go`, `icd.go`, `ota.go` (beyond `:120-250` greps), `client.go` body, `persist.go`, `reserve.go`, `routes.go`, `tls.go`, `limits.go`, `mdns.go`, `health.go` body, `metrics.go`, `webrtc_*.go`, `surface.go:125-546`, `surface_stability.go`, `root.go` beyond `:120-160`, `node/controllernode`, `node/metrics`, and every test.
- `device/`: `defaults_*.go` bodies (the factory table beyond `defaults.go:1-130`, `:385-425`), `base.go`, `measurement.go`, `queue.go`, `identify.go` beyond `:65-80`, `device.go:330-385`, `:425-555`, `:592-673`, `:690-731`; 91 of the 94 generated files; `script/devicegen` beyond the label and exclusion lists.
- `surface/`: `profile.go`, `ahead.go`, `change.go`, `element.go`, `hexid.go`, `surface.go`, `level.go` beyond `:265-340`, `surfacetest`.
- `contract/matter.go` beyond the slices named in the scope; `switchevents.go`, `wifi.go`, `identify.go`, `change.go`.
- `examples/`, `fabrictest/` and the bridge facade (other W5 themes).
