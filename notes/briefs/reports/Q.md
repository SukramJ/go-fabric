# Report Q — `cluster/onoff.Server` and the node facade `node/`

Branch `wave6/node` on `planning/briefs-wave-6` (8594e59). No PR. `make
reachability` after every commit that changed Go code, inventory in the same
commit (unreached stays 5).

| Commit | Item |
| --- | --- |
| bcb18b5 | Part 1: `cluster/onoff.Server`, table tests, parity test, CHANGELOG |
| 17928c9 | Part 2: `node/` (Config, New / Start / Stop / Run, Aggregator, RootClusters), tests, coverage floor, CHANGELOG |
| 7e4a688 | Part 2: Aggregator doc — add known endpoints before Start |

matter.js read at `4633bfd7` (HEAD). `OnOffServer.ts` is 345 lines, as the
brief says.

## What was built

### Part 1 — `cluster/onoff.Server` (`cluster/onoff/server.go`)

- `onoff.New(Config)` with `Config{Features, DataVersion, Sink, Initial,
  BootReason, Clock}`, on `spec.Server` and the generated definition, as
  `boolcfg` / `filter` are. Attribute, command and feature lists, globals,
  privileges and write checks come from the definition. `FeatureLT`,
  `FeatureDF` and `FeatureOFFONLY` are new `Feature`-typed constants. The
  existing `FeatureLighting` is a `uint32` and is kept as it was (public
  API). `StartUpOff`, `StartUpOn` and `StartUpToggle` are also new.
- The rules transcribed from the brief, with the line cited in code:
  - StartUpOnOff at construction, skipped after SoftwareUpdateCompleted
    (:34-50).
  - Held-timer stop on a write of 0 / 0xFFFF; a write never starts a
    timer (:52-58, :71-84).
  - `on` (:97-112), `off` (:119-139), `toggle` (:146-152),
    `offWithEffect` (:158-169), `onWithRecallGlobalScene` (:171-191) and
    `onWithTimedOff` (:198-225).
  - The two 100 ms countdowns with the 0xFFFF hold (:239-253, :312-324).
- The timers have no goroutine of their own. Each is a `Clock.AfterFunc`
  that re-arms after each tick, and a generation counter discards a stale
  callback. Every operation (command, write, tick, `Set`, `MatterQuiesce`)
  runs under one lock, as matter.js runs the timer callbacks with
  `lock: true` (:233, :261). Changes are stored together only on success,
  as a matter.js command transaction commits.
- `Sink.SetOnOff(ctx, bool) error` is called from `on` / `off`. This is the
  Go form of overriding on() / off(), which matter.js says is enough
  (:142-145). A refusal returns FAILURE, and nothing the command would
  have changed is stored.
- `Set(on)` stores and reports the value. It does not call the sink or
  move a timer. `OnOff()` reads the value.
- `MatterQuiesce` stops both timers (asyncDispose, :90-95).
- Scenes: ScenesManagement already recalls OnOff through the On / Off
  commands (`cluster/core/scenes_attributes.go` `applyValues`). The rest
  of matter.js's scene code is not ported here — see "Left out".

### Part 2 — `node/`

- **`config.go`.** `Config{Network, Commissioning, ProductDescription,
  BasicInformation, Store, Advertiser, DeviceTypeValidation, Attestation,
  OTA *OtaConfig, FixedLabels, AggregatorServers, Logger}`:
  - The BasicInformation fields are the ones `buildRootClusters` sets
    (wiring.go:105-116), plus ProductLabel.
  - `Store{DB *sql.DB; Credentials *store.Store; Endpoints
    endpoint.Store}`. With `DB`, both schemas are applied; the other two
    fields override the stores built from it.
  - `Validate()` applies the daemon's checks (main.go:113-131): the
    12-bit discriminator, the passcode range and trivially-guessable list,
    and the PBKDF iteration range. It also checks the salt length, the
    identity the bridge and assembler need, the store, and the OTA
    directory.
- **`node.go`.** `New` follows main.go:135-426 in the order of the
  `bridge/bridge.go` package doc:
  1. Stores, group state, assembler, `bridge.New`, device-type validation.
  2. ACL lister, group messaging, auxiliary ACL, diagnostic events.
  3. Root servers and `AttachRootClusters`, then the Aggregator servers.
  4. Security wiring: session lookup, ACK tracker, subscription manager,
     session registry, CASE provider, subscription store, operational
     resolver, CASE initiator.
  5. Commissioning window, fabric hooks, event-number persistence.

  `Start` runs main.go:428-501:
  1. Bridge start and the DiagnosticLogs provider.
  2. StartUp, then BootReason.
  3. The operational-time keeper and both PartsList providers.
  4. Stored fabrics' CASE identities, then
     `ReestablishFormerSubscriptions`.
  5. OTA start, `AnnounceFabric`, and the node's own window when there is
     no fabric.

  `Stop` follows the daemon's shutdown order. Start and Stop are
  idempotent. A stopped node returns `ErrStopped`. `Run` = Start, wait,
  Stop. Accessors: `Bridge()`, `Root()`, `Aggregator()`,
  `CommissioningWindow()`, `Fabrics(ctx)`, `Store()`, `PairingCodes()`.
- **`aggregator.go`.** The `node.Endpoint interface{ Spec() endpoint.Spec }`
  seam, so `device/` is not imported.
  - `Add`, `Remove`, `Endpoints` and `IncreaseConfigurationVersion`.
  - One snapshot, scope `"node"`, `ModelComplete: true`. Before Start,
    Add and Remove only change the set; after Start, each one runs
    `bridge.Reassemble`.
  - Persisted NodeLabel and ConfigurationVersion are restored as the
    daemon's fleet does (fleet.go:1625-1647).
- **Ported from the daemon, not moved.** `root.go`, `security.go`,
  `commissioning.go`, `persist.go` and `ota.go` are copies adapted from
  `examples/reference-bridge` (wiring.go, commissioning.go, persist.go,
  root_optional.go, ota.go). The daemon is untouched and its tests pass.
  The settings keys are the daemon's, so a daemon database keeps its state
  under the facade.

## Rule → test

| Rule | Test |
| --- | --- |
| StartUpOnOff On / Off / Toggle / null, skipped after SW update, ignored without LT | `cluster/onoff` `TestStartUpOnOff` |
| on / off / toggle / offWithEffect / onWithRecallGlobalScene / onWithTimedOff, with timers armed or not | `TestCommands` (23 rows) |
| timed-on countdown → off, OffWaitTime 0, sink called | `TestTimedOnCountdown` |
| delayed-off countdown | `TestDelayedOffCountdown` |
| write of 0 / 0xFFFF stops the running timer; another value is picked up; writes never start one | `TestHeldTimers` |
| tick at 0xFFFF stops; stale callback ignored | `TestTickHoldsAt0xFFFF`, `TestStaleTickDoesNothing` (internal) |
| sink refusal → FAILURE, nothing stored; refused countdown off | `TestSinkRefusal`, `TestSinkRefusesTimedOff` |
| OFFONLY / non-LT command gating, LT\|OFFONLY refused | `TestFeatureGatedCommands` |
| Quiesce | `TestQuiesce` |
| host Set | `TestHostSet` |
| conformance of every feature selection; snapshot facts | `TestParityMatterJS_OnOffServers`, `TestParityMatterJS_OnOffSnapshot` |
| matter.js sequence (timed on → countdown → guard period → lowered OffWaitTime) | `TestParityMatterJS_OnOffServerBehaviour` |
| PASE answered over UDP; read of root and bridged attributes through the dispatcher; subscription with an id; report after `Aggregator.Add` on a running node | `node` `TestNodeServesPASEReadAndSubscription` (real UDP and a real PASE handshake, `client_test.go`) |
| Start / Stop idempotent, no restart | `TestStartStopIdempotent`, `TestStopWithoutStart` |
| Run stops with its context | `TestRunStopsWithTheContext` |
| Add before and after Start; Remove; number kept across restart; re-add gets a fresh number | `TestAggregatorAcrossRestart` |
| Aggregator refusals | `TestAggregatorRefusals` |
| persisted NodeLabel served; ConfigurationVersion raised, persisted, served after restart | `TestPersistedLabelAndConfigurationVersion` |
| Validate | `TestConfigValidate` |
| OTA mounted | `TestOTARequestorMounted` |
| reboot count, operational time, settings keys, ring logs | `persist_internal_test.go` |

Mutation checks:
- Replacing `max(req.OnTime, …)` fails two `TestCommands` rows.
- Removing `AttachSubscriptionManager` fails the end-to-end test
  ("SubscribeResponse without a SubscriptionID").
- Removing `AttachACLLister` is **not** detected. PASE runs without ACL
  checks; see open question 6.

## Guards run (Linux, local)

- `go test ./node/... ./cluster/onoff/... ./bridge/... ./examples/...` —
  green.
- `make race` scope: `go test -race ./node/ ./cluster/onoff/` — green.
  `node/` also passed `-race -count=5`.
- `make lint` — 0 issues. gofumpt v0.12.0 `-l .` prints nothing.
- `make cover-check` — green: 171 packages at or above their floors.
  `cluster/onoff` is at 100 % (floor 100). `node/` is at 56.9 %, with a
  new floor of 56 and a `why` (see open question 6).
- `make reachability` / `reachability-check` — clean, unreached 5.
- `go test ./internal/chiptool/ -run 'TestWorkflow|TestFamil'` — green.
- The chip-tool suite and the certification families were not run. The
  daemon is unchanged; brief R runs them.

## Left out, and why

- **Scenes logic of OnOffServer.ts.** Three pieces are not ported:
  - Scene invalidation when a command changes OnOff (:98-100, :114-117,
    :120-122, `makeAllFabricSceneInfoEntriesInvalid`).
  - Global-scene store on OffWithEffect and recall on
    OnWithRecallGlobalScene (:159-165, :175-184).
  - The transition-delayed scene apply (:268-310). ScenesManagement's
    `applyValues` invokes On / Off at once.

  The brief allowed leaving the apply to ScenesManagement's extension
  points. The other two need a hook from `onoff.Server` into the
  endpoint's ScenesManagement server, which does not exist yet.
- **Moving the daemon helpers.** They are copied, not moved, because the
  brief keeps the daemon for brief R. About 900 lines are duplicated until
  R deletes the daemon's copies.
- **Behavioural assertion of the CASE-side ports.** ACL lister, CASE
  provider, subscription store, operational resolver, CASE initiator,
  group messaging and auxiliary ACL are wired but not proven over the
  wire. That needs a commissioned fabric (AddNOC, CSR, NOC chain), which
  the test commissioner does not build.

## Open questions for the owner

1. **StartUpOnOff under an Aggregator.** `OnOffServer.ts:37` skips
   StartUpOnOff when `this.endpoint.ownerOfType(AggregatorEndpoint)`, so
   matter.js never applies it to a bridged light. The brief's summary of
   :33-68 leaves out this condition, and I did not add it, because a Go
   surface for it (a config flag, or `node` / `device` telling the
   server) would be my own invention. As built, a bridged `onoff.Server`
   with LT and a non-null StartUpOnOff applies it at construction, where
   matter.js would not. How should the server learn that it sits under the
   Aggregator?
2. **Remove and re-add.** The brief says `Remove` "persists the id for a
   re-add (`BD-Matter-EndpointID-Persistent`)". With `ModelComplete: true`,
   the assembler GC releases the row on Remove (as matter.js
   `eraseDescendant` does). The allocator never reissues a released number
   (`BD-Matter-EndpointNumber-Monotonic`), so a re-add gets a fresh number.
   What persists is the number of an endpoint kept across a restart. The
   test asserts the actual behaviour. Is that the intended reading, or
   should Remove keep the row?
3. **`ModelComplete: true` from the first assembly.** A host that adds a
   known endpoint only after Start loses its persisted number. I
   documented "add before Start". Should the Aggregator have a readiness
   switch, like openccu-loom's per-central gate
   (`BD-Matter-EndpointGC-ModelCompleteGate`)?
4. **Initial LT values.** The model gives GlobalSceneControl, OnTime,
   OffWaitTime and StartUpOnOff no default. matter.js leaves them unset and
   reads unset as falsy (`?? 0`, `?? null`, :39, :105, :132). The server
   starts at false / 0 / 0 / null unless `Config.Initial` says otherwise.
   Confirm, or name the defaults (brief P's device-type alterations may
   override them).
5. **Defaults the brief did not give.** Each of these needs confirmation
   or a replacement:
   - `ProductDescription.Name` defaults to NodeLabel, and `DeviceType` to
     RootNode 0x0016 (the daemon's advert, main.go:285, :290).
   - Root localization is en-US only. The daemon also lists de-DE as its
     own choice.
   - The root FixedLabel list comes from `Config.FixedLabels`. The daemon
     hard-codes `role` / `example`.
   - `Config.AggregatorServers` carries the daemon's Actions.
   - Salt and PBKDF iterations have no defaults; Validate requires both.
   - `Fabrics` takes a `ctx`.
   - `Stop` closes the advertiser.
   - A stopped node cannot be restarted (`ErrStopped`). I have not
     checked whether matter.js's Node can start again after `stop`
     (Node.ts:188); the brief did not say.
6. **Coverage and proof of the CASE-side ports.** Should a Go test
   commissioner that does AddNOC become shared test support (for example
   in `bridge/bridgetest`)? It would make the ACL and CASE ports provable
   through the facade and raise `node/`'s floor.
7. **BootReason for bridged OnOff servers.** `onoff.Config.BootReason` is
   optional. The node has `Root().GeneralDiagnostics`, but nothing passes
   its boot reason to the servers a host builds. Brief P's defaults may
   want `node` to supply it.

## by_design / findings candidates for the owner

- by_design — **`onoff.Sink` as the Go form of a matter.js on() / off()
  override**, with a transactional commit: a refused change stores nothing.
  matter.js gets the same through its transaction rollback.
- by_design — **goroutine-free countdowns on a `Clock` seam** and the
  generation counter for stale callbacks. These are Go mechanics, not
  semantics; matter.js uses `Time.getPeriodicTimer`.
- by_design — **a refused countdown off.** The timer stops and the state
  stays (matter.js stops the timer before `off()`, :248, then rolls the
  transaction back).
- findings — **StartUpOnOff applied under the Aggregator** (open question
  1, OnOffServer.ts:37).
- findings — **OnOff scene invalidation, global-scene store / recall and
  the transition-delayed apply are not ported** (OnOffServer.ts:98-100,
  :120-122, :159-165, :175-184, :268-310).
- findings — **`node.Aggregator` Remove + re-add yields a fresh number**
  (open question 2), if the owner reads the brief as requiring the old one.
