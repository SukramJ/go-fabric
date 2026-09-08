# by_design.md — intentional divergences from matter.js

This is the divergence catalogue the Go code cites. When a comment in this
module says `see notes/parity/by_design.md <ID>`, the entry is here.

**What belongs here.** A deliberate deviation from
[matter.js](https://github.com/matter-js/matter.js) HEAD — the Matter-side
gold standard this module is a semantic port of (see
[`CLAUDE.md`](../../CLAUDE.md) and
[`docs/matter-parity-contract.md`](../../docs/matter-parity-contract.md)).
Each entry names what matter.js does, what this module does instead, why, what
pins it, and — where one exists — the condition that would retire it.

**What does not.** A defect is not a divergence. Open behaviour gaps live in
[`matter_behaviour_findings.md`](./matter_behaviour_findings.md); closing one
removes it from that list rather than adding it here. And schema values —
cluster IDs, revisions, attribute IDs, constraint defaults — are never a valid
divergence: they come verbatim from the embedded matter.js extract.

**Provenance and stale paths.** These entries were written while the Matter
stack lived inside [OpenCCU-Loom](https://github.com/SukramJ/openccu-loom) as
`internal/north/matter/`. Paths into this module have been rewritten to its
root. Paths that still read `cmd/openccu-loom/…`, `internal/model/…`,
`internal/central/…` or `internal/store/…` are the **reference host's** wiring,
not this module's: they are left as written because the decision they record
was made against that code, and a host embedding this module wires the same
seams under its own names. Divergences that are purely a host-side device
projection stayed in OpenCCU-Loom's own
[`notes/parity/by_design.md`](https://github.com/SukramJ/openccu-loom/blob/main/notes/parity/by_design.md).

Some entries are part-German; that is how they were written and the text is
left verbatim rather than re-translated after the fact.

---

### BD-Matter-ClosureWithoutTagList — the Closure endpoint omits the TAGLIST feature its device type marks mandatory

A garage drive projects as the Closure device type (0x0230) carrying
ClosureControl (0x0104). matter.js's `closure-device.element.ts:16-17`
marks the Descriptor's `TAGLIST` feature conformance `M` for that device
type; our Descriptor advertises `FeatureMap=0` and reports TagList as
unsupported (`cluster/core/descriptor.go:141-151`).

**Rationale.** The Descriptor is shared by every endpoint, and TagList is
already disabled there for a controller-interop reason that predates this
work: Apple's iOS Matter SDK does not ship a `semtag` struct schema, so an
endpoint that reports TagList makes Apple reject the whole Descriptor
cluster and abort the HAP service build with `HAPErrorDomain Code=14`.
Turning the feature on to satisfy the Closure device type would trade a
conformance gap no controller currently checks for a pairing abort that
was observed in practice.

The gap is narrow: TagList carries semantic tags that describe an
endpoint's role within a composed device, and the garage projects as a
single endpoint with nothing to disambiguate. Revisit together with the
Descriptor-wide TagList decision — the two are the same switch, and this
entry exists so that switch is not flipped for the Closure device type
alone.

### BD-Matter-GarageIsClosureNotWindowCovering — the garage drive left the WindowCovering projection

A garage drive used to project as WindowCovering (0x0102) with its door
state encoded as a lift percentage: open = 0, ventilation = 5000,
closed = 10000. That is a faithful reading of the only axis
WindowCovering has, and it loses the thing that matters — the ventilation
stop is a named physical position, not a point on a continuum. As a
percentage it could not be labelled by a controller, could not be
selected other than by dragging a slider to the right region, and could
not be told apart on a read from a door resting halfway.

ClosureControl carries `OpenedForVentilation` as a first-class
`CurrentPositionEnum` value and `MoveToVentilationPosition` as a
`TargetPositionEnum` value, gated behind the `VT` feature. The garage now
projects as the Closure device type with `FeatureMap = PS | VT`.

**Consequence.** The two projections cannot coexist: the Closure device
type lists WindowCovering with conformance `X` — disallowed
(`closure-device.element.ts:20`). A controller that had the drive paired
as a window covering sees the endpoint change device type, which
ecosystems treat as a new accessory. This is a deliberate one-way
migration, taken because the previous projection could not express the
device.

### BD-Matter-TLVPermissive — responder TLV decode is more permissive than chip

connectedhomeip enforces container/tag strictness on every element via the
always-on `VerifyElement` (`src/lib/core/TLVReader.cpp`) and rejects
unordered Sigma/PASE optional fields, trailing bytes after the top-level
container, and missing-mandatory post-decrypt fields. go-fabric's live
`Decoder.Next()` (`tlv/`) enforces these only via the
opt-in `Validate()`, which the Sigma/PASE decoders do not call.

**Rationale (corrected 2026-07):** go-fabric is a pure Matter
**responder** — but the permissive command-field decode path is NOT dead
defensive code: it is load-bearing controller interop. Google Home's
brightness slider omits the mandatory-but-nullable TransitionTime field
ENTIRELY (absent context tag, not TLV Null) in LevelControl MoveToLevel /
MoveToLevelWithOnOff / Step, so the lenient tag-walking decoders
(`bridge/fields_reader.go`
`decodeMoveToLevelRequest`; `cluster/wire/levelcontrol.go`
`DecodeMoveToLevel` / `DecodeStep`) must keep decoding that shape to
`TransitionTime == nil`. matter.js HEAD itself decodes an absent struct
member leniently (`packages/types/src/tlv/TlvObject.ts:205`
`decodeTlvInternalValue` leaves it unset); the strict mandatory-field
rejection (`ValidationMandatoryFieldMissingError`, `TlvObject.ts:258`)
lives only in the separate `validate()` step, invoked at command-invoke
time by `packages/protocol/src/action/server/CommandInvokeResponse.ts:446`
— so stock matter.js HEAD would reject Google Home's field-absent shape at
its invoke validate step, and matter.js-based bridges have to relax exactly
that check for Google Home's LevelControl commands. go-fabric therefore
deliberately diverges from stock matter.js strictness here. The earlier
claim that "real controllers emit spec-valid TLV, so the permissive path is
never reached on the wire" is WRONG for command fields and is withdrawn.

The wire shape is pinned by regression tests:
`TestDecodeMoveToLevelOmitsTransitionTime`,
`TestDecodeStepOmitsTransitionTime` (cluster/wire),
`TestDecodeMoveToLevelRequest_AbsentTransitionTime`,
`TestCommandFieldsReader_MoveToLevelVariants_AbsentTransitionTime`
(bridge). Any future strict-validation refactor MUST keep tolerating
absent LevelControl TransitionTime tags or it silently breaks Google Home
dimming. Sigma/PASE container/tag strictness remains a fuzz/robustness
backlog item (parity audit CHIP L9-01..04), not an interop gap. The crypto
core (HKDF salts/info/nonces, Sigma TLV tags, status codes, control octets)
IS byte-verbatim with connectedhomeip HEAD.

### BD-Matter-mDNSDeviceType — commissionable DT advertises RootNode, not Aggregator

The commissionable `DT` TXT key + `_T<id>` subtype advertise RootNode
(0x0016) rather than the Aggregator application device-type (0x000E) that
connectedhomeip's `Dnssd.cpp` would emit (`cmd/openccu-loom/daemon.go`).

**Rationale:** empirical Apple-pairing observation — recorded here so the
audit does not re-flag it (parity audit 2026-05-30 CHIP-mDNS F2). The wire
diff that motivated it should be attached when next reproduced.

### BD-Matter-OTAProvider-NotExposed — the OTA Software Update Provider cluster is not exposed

The bridge does not expose the OTA Software Update **Provider** cluster
(0x0029). Only the **Requestor** cluster server (0x002A) exists, and it too
is intentionally left unmounted (`cmd/openccu-loom/daemon_matter.go`
buildRootClusters — "OTASoftwareUpdateRequestor is intentionally NOT
mounted").

**Rationale (verified against the gold standard, 2026-07):**
- matter.js **defines** the Provider cluster and an `OtaProvider` device
  type (`../matter.js/packages/model/src/standard/elements/ota-provider.element.ts:13`
  — id 0x14) but **composes it on no endpoint**: neither
  `packages/node/src/endpoints/root.ts` nor
  `packages/node/src/devices/aggregator.ts` lists cluster 0x0029, and
  `packages/node/src/devices/` ships no OTA-provider device. There is no
  gold-standard endpoint composition to mirror.
  home-assistant-matter-bridge does not expose it either.
- The Provider cluster is mandatory on the OtaProvider device type (0x14),
  **not** on the RootNode (0x0016) or Aggregator (0x000E) device types loom
  composes. Mounting 0x0029 on the RootNode makes Apple Home's HAP mapper
  reject the node as schematically inconsistent (same reason 0x002A stays
  unmounted — see the buildRootClusters comment).
- A bridge has **no Matter-OTA consumer**: HomeMatic devices update through
  the CCU, and the daemon updates via its Docker / GoReleaser channel
  (`ota_software_update_requestor.go` header). A Provider that only ever
  answers `QueryImage` with `NotAvailable` would serve no controller.

Implementing an unreachable, never-invoked responder (plus its dead
QueryImageResponse / ApplyUpdateResponse TLV encoders) would add code no
wire path exercises — exactly the silent stub the matter-parity rule
forbids. Re-open only if loom takes on the OtaProvider device-class role on
a dedicated device-type-0x14 endpoint, which is out of scope. The cluster
IDs / command shapes are recorded in the A1 plan for that future work.

### BD-Matter-SubscriptionResumption-Deferred — no cross-restart subscription resumption

A daemon restart drops every Matter subscription; the controller
re-subscribes on its own liveness timeout. The SQLite store
(`store/subscriptions.go`) and its table exist but
are intentionally **not** wired to save-on-subscribe / restore-at-boot in
production.

**Rationale (verified against the gold standard, 2026-07-01):** matter.js
HEAD implements **no** subscription resumption — a repo-wide search of
`../matter.js/packages/protocol/src/interaction/` for resumption /
server-initiated-CASE re-establishment finds nothing. Meaningful
resumption is not just persisting rows: (1) the `subscription.Manager`
generates the `SubscriptionId` internally with no restore-with-id path, so
a restored row would re-arm under a fresh id the controller does not
recognise; and (2) report delivery is **session-bound** —
`bridge/subscribe.go` ships every ReportData through
`b.subTargets.Load(sub.ID)`, a `subTarget{src: <transport session>}`
captured at Subscribe time. After a restart the CASE session is gone, so a
restored subscription has no `subTarget` and the engine tick delivers
nothing. Resuming delivery would require the daemon to **initiate** CASE
back to the controller (operational discovery + a CASE-initiator role),
which loom does not have — the bridge is a pure CASE responder, matching
matter.js. Building a server-initiated-CASE resumption path the gold
standard itself omits would be a large divergence, not parity. Deferred
until matter.js (or a concrete interop need) makes it a parity
requirement. WIP scaffolding for the id-preserving store lives on the
unmerged `wip/a1-subscription-persistence` branch; the corrected scope is
recorded in the A1 implementation plan.

### BD-Matter-ButtonPressCycleFromDiscreteFrames — Switch (0x003B) events from discrete CCU frames, not a position stream

matter.js derives Switch (0x003B) events from a continuous currentPosition
stream plus timers (`SwitchServer.ts`: `debounceTimer`,
`longPressTimer`/`longPressDelay`); the CCU instead delivers discrete,
pre-thresholded semantic frames (PRESS_SHORT, PRESS_LONG, PRESS_CONT repeats,
PRESS_LONG_RELEASE). go-fabric therefore maps frames directly onto the
event sequence via a per-endpoint press-cycle state machine
(`generic.ButtonGroup`) instead of porting the timer machinery: the
long-press threshold is already applied device-side, so `longPressDelay`
collapses into the PRESS_LONG/PRESS_LONG_START mapping, and hold tracking
(`held`/`longEmitted`) replaces `longPressTimer`/`currentIsLongPress`.

Buttons without a PRESS_LONG_RELEASE parameter (HmIP KEY channels) cannot
signal hold end, so each PRESS_LONG completes the full
InitialPress→LongPress→LongRelease cycle immediately; with a release
parameter, device-side repeats (PRESS_CONT ~every 300 ms on BidCos, repeated
PRESS_LONG) are suppressed until the release closes the cycle, and the
release synthesizes any missing InitialPress/LongPress prefix so LongRelease
never arrives unpaired. The sequence contract itself is unchanged from
matter.js (ShortRelease only when no LongPress since the previous
InitialPress; LongRelease only after a LongPress). Guarded by
`TestParityMatterJS_GenericSwitchPressCycleSequences`
(`cluster/wire`) and the ButtonGroup suite
(`internal/model/generic`).

### BD-Matter-ButtonGroupCurrentPositionReporting — Switch.CurrentPosition answered on read, not proactively reported

matter.js's reactive state proactively reports Switch.CurrentPosition
(quality N) changes to subscribers whenever `state.currentPosition` moves.
go-fabric's consolidated button endpoint answers CurrentPosition READs
live (1 while a long-press cycle is held open, 0 idle, via
`wire.GenericSwitchPositionSource`) but does not proactively dirty-mark the
attribute on press flips: press gestures are delivered exclusively through
the §1.13 event pipe.

**Rationale:** the bridge's measurement-notifier wiring scopes a notifier to
its own cluster only when the notifier is itself a cluster server
(`filterPathsByNotifierCluster`); a model-side group notifier would fall back
to dirty-marking EVERY reportable path on the endpoint (Identify, Descriptor,
BridgedDeviceBasicInformation) per press — exactly the bursty multi-cluster
report shape the bridge deliberately avoids for Apple Home. Short presses are
atomic on the CCU wire (press+release in one frame), so there is no
observable position interval to report anyway; only CONT-holds would ever
surface a transient 1.

### BD-Matter-LevelControl-NativeRamp — RemainingTime stays 0 during device-native ramps

A positive MoveToLevel / MoveToLevelWithOnOff TransitionTime is delegated to
the HM device as RAMP_TIME inside one atomic put_paramset ({LEVEL, RAMP_TIME,
ON_TIME=NotUsed} via `Light.TurnOnWith` / `Light.TurnOffWithRamp`); the
device performs the transition natively. matter.js's default
LevelControlServer manages transitions host-side (`Transitions.ts`) and can
tick RemainingTime while stepping, but explicitly sanctions delegating to
native hardware transitions (`LevelControlServer.ts:36-41`,
`createTransitions` override note). The CCU reports no ramp progress, so the
bridge keeps RemainingTime at a constant 0 while a device-side ramp runs.
Null/0 transition times keep the instant SetLevel path, matching
`moveToLevelLogic`'s truthy-only rate derivation
(`LevelControlServer.ts:297-303`) and the `changePerS` contract "0 or
nullish means transition instantly" (`LevelControlServer.ts:459`). Devices
whose channel lacks RAMP_TIME (`LightCapabilities.Transition` unset) always
take the instant path.

> **Rule of thumb (CLAUDE.md):** matter.js HEAD is the gold standard for everything under ``. Cluster IDs / revisions / attribute IDs / constraints / defaults / wire shape are taken verbatim. Any item below is a **deliberate** divergence with a documented reason. Bug-class drift (hand-coded revisions etc.) does **not** belong here — it belongs in a fix.

### Idiomatic translations TypeScript → Go (not real divergence)

| matter.js pattern | Go idiom in go-fabric | Files |
| --- | --- | --- |
| `Behavior.with(BasicInformationServer, ...)` Mixin | Concrete struct `core.BasicInformation` with the same fields + `MatterRead`/`MatterWrite`/`MatterAttributes` methods | `cluster/core/basic_information.go` |
| `Promise<T>` async chain | Goroutine + `context.Context` + return value | every I/O method |
| `@matter/types` `TlvSchema<T>` derived from class fields | `tlv.Encoder` / `tlv.Decoder` with explicit `PutUint16` / `PutUint32` per spec-typed slot | `tlv/encode.go` |
| Decorators (`@validate`, `@quality(...)`) | Comment + manual constraint check at write boundary | `cluster/core/*_information.go::MatterWrite` |
| Behavior-state proxies via Reflect | Direct struct fields under `sync.RWMutex` | every cluster server |

These rewrite TypeScript constructs into Go. Wire output is identical; the surface API differs because Go does not have decorators or Reflect-based mixins.

### Production deviations (require ADR if architectural)

| # | matter.js source | go-fabric deviation | rationale |
|---|---|---|---|
| L4-1 | `packages/model/src/standard/elements/root-node-device.element.ts` — Revision 4 | `cmd/openccu-loom/daemon_matter.go::buildRootClusters` → `{DeviceType: 0x0016, Revision: matterschema.DeviceTypeRevisions[0x0016]}` (= 4, `schema/devicetypes.go:23`) | **Status (2026-06): RESOLVED — no longer a divergence.** Die frühere Apple-Home-Bypass-Hardcodierung auf Revision 3 ist entfernt; die RootNode-Revision wird jetzt aus der generierten Schema-Tabelle (matter.js HEAD = 4) gezogen. Hardcodiertes `Revision: 3` driftete hinter matter.js zurück und löste in Apple's HAP-Mapper `Unable to find HAP service type for deviceType 22` aus. |
| L3-PFAD-1 | `packages/protocol/src/session/case/Fabric.ts` — FabricLabel default `""` | `cluster/core/operational_credentials.go:688` `Label: "openccu-loom"` | Apple Home rejectet leeres Label nach CommissioningComplete mit RemoveFabric. Workaround setzt einen non-empty Label; Commissioner kann via `UpdateFabricLabel` umbenennen. Spec-konform (max 32 printable bytes). |
| L3-PFAD-2 | `packages/node/src/behaviors/basic-information/BasicInformationServer.ts:88` — reactTo emitter für Reachable | `cluster/core/basic_information.go:279` returns `true, true` hardcoded | Root-Endpoint ist der Bridge-Daemon selbst; während daemon läuft ist die Bridge per definitionem reachable. Kein ReachableChanged-Event nötig. |
| L3-PFAD-3 | `BasicInformationServer.ts:110-127` — StartUp/ShutDown/Leave events emittiert | `cluster/core/basic_information.go::EmitStartUp` / `EmitShutDown` / `EmitLeave` (+ `SetMatterEventEmitter`) | **Status (2026-06): RESOLVED — implementiert und verdrahtet.** Die früher als deferred markierten Optional-Events (`conformance: "O"`) sind jetzt vollständig vorhanden: Event-Konstanten (0x0000–0x0002), Payload-Typen (`StartUpEvent`/`ShutDownEvent`/`LeaveEvent`) und Emitter. Produktiv aufgerufen aus `cmd/openccu-loom/daemon_north.go:252` (EmitShutDown), `daemon_matter.go:538` (EmitLeave bei RemoveFabric) und `daemon_matter.go:2607,2632` (EmitStartUp). |
| L3-PFAD-4 | `GeneralCommissioningServer.ts:54` `this.state.breadcrumb = 0` in `initialize()` | `cluster/core/general_commissioning.go:138` — Go struct zero-value | Funktional äquivalent (Go zero = 0); idiomatisch in Go. |
| L3-PFAD-5 | `NetworkCommissioningServer.ts` — ScanNetworks per WiFi/Thread feature | `cluster/core/network_commissioning.go:183` — rejectet mit UnsupportedCommand | Bridge ist Ethernet-only. Per Matter §11.9 ist ScanNetworks nicht mandatory für ETH; UnsupportedCommand ist spec-konform. |
| L3-PFAD-6 | `GeneralCommissioningServer.ts:67-82` — auto-armiert 60 s ArmFailSafe bei jeder neuen PASE-Session | `cluster/core/general_commissioning.go::AutoArmOnPaseEstablished` — implementiert, optionaler Hook | Spec §11.10 stellt ArmFailSafe-Pflicht auf den Commissioner. `AutoArmOnPaseEstablished` ist ein defensives Sicherheitsnetz das der Daemon via PaseAdapter.onEstablished einweben kann; es ist ein No-op wenn der Commissioner bereits explizit ArmFailSafe gerufen hat. Alle bekannten Commissioner (chip-tool, Apple Home, HA) callen ArmFailSafe explizit; der Hook macht das robuster. |
| L4-TagList | `Descriptor` — TAGLIST feature, conformance `"desc"` | `cluster/core/descriptor.go` — leere Liste | Optionales TAGLIST-Feature. Apple Home / chip-tool fragen es im Standard-Pair-Flow nicht ab. Erweiterbar wenn ein Commissioner es benötigt. |
| L5-FullyQualified | `packages/types/src/tlv/TlvCodec.ts::writeTag` — FullyQualified48 schreibt `profile(uint32)+id(uint16)` | `tlv/encode.go::Tag` — `Vendor(uint16)+Profile(uint16)+Number(uint16)` per Matter Core Spec §A.7.3 Table 74 | Spec-konform (matter.js conflated profile+vendor in uint32). Kein Live-Impact: keine IM-Message emittiert FullyQualified-Tags. Decoder akzeptiert beide Shapes. |
| L6-PFAD-1 | `packages/protocol/src/interaction/SubscriptionHandler.ts:284` — per-subscription timer mit `sendInterval = max(0.8 × maxInterval, minIntervalFloor)` → 24–48 s | `im/subscription/subscription.go:133` `sendIntervalLocked()` + `engine.go:28-35` shared 250 ms ticker | go-fabric implementiert die matter.js-Formel `min(maxInterval/2, max(minFloor, 0.8 × maxInterval))` korrekt; der hard-cap 5 s aus früheren Implementierungen ist entfernt. Architektur-Drift: matter.js nutzt per-subscription Timer, go-fabric nutzt einen shared 250 ms Ticker — funktional äquivalent, Go-idiomatischer. |
| L6-PFAD-2 | insertion-order via behavior-layer attribute-map | `endpoint/dispatcher.go:253` — numerisch sortiert | Spec stellt keine Ordering-Garantie; Commissioners dürfen sich nicht auf eine bestimmte Reihenfolge verlassen. |
| L6-PFAD-3 | `IncomingInteractionClientMessenger.readDataReports:881` — sendet `Status.Success` per chunk | `bridge/subscribe.go::handleSubscribeRequest` — burst-sendet ohne intermediate Status | Apple Home akzeptiert Burst (empirisch 2026-05-09); chip-tool akzeptiert beides. Kein wire-impact heute. |
| L6-PFAD-4 | `InteractionServer.ts` — erlaubt Endpoint-Wildcard für bestimmte Cluster | `endpoint/dispatcher.go::Invoke` — rejectet `!path.HasEndpoint` | Kein go-fabric-Cluster benötigt Endpoint-Wildcard-Invoke heute. |
| L7-2 | testet gegen NIST SP 800-38C reference vectors | `secure/aesccm/aesccm_test.go` — round-trip-only | End-to-end via Apple-Pair-Audit validiert (Sessions establish, ACL-Write commits). Symmetrisches Seal+Open-Bug wäre theoretisch maskiert; pragmatisch akzeptables Restrisiko. |
| L8-IPv4+IPv6 | `MdnsService.ts` — advertised auf allen Interfaces | `mdns/zeroconf.go::primaryHostIPs` — IPv4-first + ein routable IPv6, einziges Interface | Apple-Home-friendly (Apple wählt das erste advertisierte Interface); Bridge auf Multi-Homed-Server würde sonst inkonsistente Pfade emittieren. Dokumentiert in Code-Comment. |
| D-9 | matter.js MatterDefinition trackt Matter Core 1.5.1 — kein Cluster 0x0024 (Schedules) | `cluster/wire/schedules.go` existiert; `internal/model/custom/climate/matter.go:190-199` mountet ihn nicht | Schedules Cluster ist pre-publication/draft/dropped. Apple Home's HAP service mapper rejectet Endpoint-Clusters, die nicht in matter.js stehen, mit `HAPErrorDomain Code=24`. Code bleibt für spätere Re-Aktivierung erhalten. |
| chip-tool C6 / F3 Voll-Wildcard | matter.js / chip-tool akzeptiert `(cluster=0xFFFFFFFF, attr=0xFFFFFFFF, ep=0xFFFF)` | `bridge/subscribe.go` + `endpoint/dispatcher.go` emittieren ~3000 Reports problemlos | chip-tool exit=1 bei Voll-Wildcard ohne Bridge-side-Fehler. Vermutet UDP-fragment- oder chip-tool-internes Buffer-Limit gegen unsere Bridge-Größe. Workaround: Cluster-spezifische Wildcards (`(cluster=0x001D, attr=*, ep=*)` oder `(cluster=*, attr=*, ep=0)`) — die laufen sauber. Kein Bridge-Fix möglich; Drift ist commissioner-side. |
| L3-PFAD-7 | `packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts` — `Fabrics`/`NOCs` attributes backed by in-memory fabric state | `cluster/core/operational_credentials.go` — live-reads from SQLite store on every attribute access | Avoids stale-read races when a second commissioner modifies the fabric list concurrently. Functionally equivalent: the spec requires the attribute to reflect the current fabric list; reading from the authoritative store (not a cache) guarantees freshness without an explicit invalidation mechanism. |
| L4-PFAD-1 | `packages/node/src/endpoints/aggregator.ts` — `AggregatorEndpointDefinition.deviceType = 0xe`, device-type filter exposed via mDNS `_T14._sub._matterc._udp.local` | `mdns/service.go::BuildCommissionableService` — `DT` TXT key + `_T<deviceTypeID>` subtype hardcoded to `0x000E` (Aggregator) for the bridge | Apple Home's commissioning flow uses the `DT` hint to render a bridge-type icon in the pairing dialog. The bridged endpoints expose their own device types via Descriptor.DeviceTypeList after pairing; the commissionable record advertises the Aggregator (bridge top-level) type only. |
| L7-PFAD-1 | `packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts` — `windowStatus` derived from internal commissioning state machine | `cluster/wire/admincommissioning.go` + `bridge/commissioning_window.go` — `windowStatus` driven by a separate `Bridge.WindowController` | Decouples cluster read path from commissioning logic. The cluster server reads window state from the controller interface; the controller owns the timer and PASE verifier lifecycle. This is a Go-idiomatic separation of concerns (hexagonal architecture) that produces identical wire output. |
| L5-Apple-Width | `packages/types/src/tlv/TlvCodec.ts::encodeTlv` — SubscriptionID and DataVersion encoded as `TypeUnsignedInt` with magnitude-driven byte width (1/2/4 bytes) | `tlv/encode.go::PutUint32` — always encodes as explicit 4-byte unsigned integer | Apple MTRDevice rejected the magnitude-driven 1-byte encoding of SubscriptionID values below 256, causing Subscribe-Initial to be dropped with no error. The 4-byte explicit form is valid per Matter Core Spec §A.7.1 Table 74 (`UInt32` tag) and accepted by all tested commissioners (Apple Home, chip-tool). Verified via pair-debug session. |
| L3-FQ64-Decoder | `packages/types/src/tlv/TlvCodec.ts:175-176` — `default: throw new NotImplementedError("Unexpected tagControl …")` — FullyQualified64 (TagControl 7) falls to the default throw branch; matter.js never decodes FQ64 tags | `tlv/decode.go::readTag` — `case TagKindFullyQualified8:` reads vendor+profile+uint32 tag number per Matter Core Spec §A.7.3 | go-fabric is more spec-compliant than matter.js for this tag form. FullyQualified64 is a valid Matter TLV tag class and chip's TLVReader.cpp supports it. No live impact: no IM message in a standard bridge exchange uses FQ64 tags. By design: go-fabric mirrors the spec; matter.js omits it. |
| L3-ContainerTypeValidation | `chip TLVWriter.cpp:686-699` — `WriteElementHead` returns `CHIP_ERROR_INVALID_TLV_TAG` when a context-specific tag is used outside Structure/List, or a non-anonymous tag inside Array | `tlv/encode.go::writeControlAndTag` — emits tag bytes unconditionally; no container-type stack | Defensive-coding gap: chip rejects wire-invalid tag/container combinations at write time; go-fabric does not. All struct-building call sites in `bridge/reply.go` and cluster servers are hand-typed and correct, so no active interop breakage exists. A container-type stack will be added if a fuzz regression surfaces the gap; until then the cost-benefit does not justify the overhead. |
| L3-ImplicitProfile-Decode | `packages/types/src/tlv/TlvCodec.ts:170-172` — `case TagControl.ImplicitProfile16: case TagControl.ImplicitProfile32: throw new NotImplementedError(…)` | `tlv/decode.go::readTag` — silently decodes ImplicitProfile tags as raw `Tag{Kind, Number}` without profile resolution | go-fabric acts only as a TLV responder (never as an initiator that would need to generate ImplicitProfile tags); no incoming IM message in a standard commissioning or subscription exchange uses ImplicitProfile tags. Profile resolution (chip's `ImplicitProfileId` pattern) is deferred until a code path that needs it materialises. matter.js throws; go-fabric decodes without error and without resolution — both are non-breaking divergences from chip's conditional resolution. |

| L7-D03 | matter.js `MdnsAdvertisement.ts:153` — MAC-derived hostname `<12hexUC>0000.local`; chip `ServiceNaming.cpp:89` `MakeHostName` from MAC/EUI-64 | `mdns/service.go::defaultHostName` — uses OS hostname (with `.local` stripped); `zeroconf.go:189-194` — falls back to `os.Hostname()` | go-fabric runs as a daemon on Linux/macOS where the OS mDNS responder (avahi / mDNSResponder) already owns the A/AAAA record for the OS hostname. Using the OS hostname lets the OS-registered record resolve the SRV target without a second A/AAAA advertisement from the bridge. A MAC-derived label is the recommended Matter approach but requires the bridge to also register the A/AAAA record itself (outside the scope of the current mDNS layer). `Config.HostName` can be set explicitly when a MAC-derived label is preferred. `MACAddress [6]byte` field can be added in a future iteration. Drift L7-D03 (LOW). |
| BD-Matter-UniqueID-Stable | matter.js `BasicInformationServer.createUniqueId()` — 32-char random persisted with Quality "FN" so the value survives bridge restarts | `cluster/core/basic_information.go::uniqueID` mixes `bootid.Salt()` into a deterministic SHA-256 derivation; `bootid/bootid.go` defaults `rotationEnabled = false`, so `Salt()` returns `[16]byte{}` and the hash collapses to a stable function of vendor/product/nodeLabel/serialNumber | go-fabric produces a stable UniqueID across daemon restarts by default. Rotation is opt-in via `matter.dev_rotate_unique_ids=true` and is intended for the dev/debug workflow where pair-iteration has corrupted Apple's HMHome state. Audit drift L1-D19 (in `audit_runs/2026-05-18_resolution_status.md`) is a **false positive**: it assumes `bootid.Salt()` rotates unconditionally, but the default zeroed salt means the production daemon's UniqueID is stable. The bootid package docstring spells out the contract; no code change required. |
| BD-Matter-SoftwareVersion-StringDerived | matter.js derives SoftwareVersionString from the numeric SoftwareVersion default (`packages/node/src/behaviors/basic-information/BasicInformationServer.ts:71` — `setDefault("softwareVersionString", state.softwareVersion.toString())`) and has no string-to-numeric path; its consumers supply the numeric value directly | go-fabric's authoritative version is the human-readable daemon build string (`build.Version`), so `core.SoftwareVersionFromString` derives the numeric attribute from the string instead, using the stable monotonic encoding `major*1_000_000 + minor*1_000 + patch` (components clamped to 999; semver pre-release/build-metadata suffixes dropped, so "0.32.0-rc.1" → 32000; leading "v" tolerated; non-semver strings such as "dev" and all-zero results floor at 1, never advertising matter.js's development default 0 from `BasicInformationServer.ts:59`) | The matter.js invariant — both attributes describe the same release — is preserved; only the derivation direction differs. When the string is absent, the fallback mirrors matter.js exactly (decimal rendering of the numeric value). Guards: `TestSoftwareVersionFromString`, `TestBasicInfo_SoftwareVersionDerivedFromVersionString`, `TestParityMatterJS_BasicInfoServer_SoftwareVersionStringFallback`. |
| BD-Matter-TimeSync-NoUTCFeature | matter.js `time-synchronization.element.ts:32-35` — `UtcTime` and `Granularity` both carry `conformance: "M"` unconditionally; no `UTC` feature flag exists | `cluster/core/time_synchronization.go::MatterRead` returns `FeatureMap = 0` while exposing UTCTime + Granularity | The matter-exhaustive audit drift L1-D14 ("UTC-Feature-Bit nicht gesetzt obwohl UTCTime exponiert wird") is a **false positive**: it presumes a non-existent UTC feature flag. UTCTime and Granularity are mandatory regardless of any feature flag; the optional TZ / NTPC / NTPS / TSC features gate additional attributes (TimeZone, DefaultNtp, TrustedTimeSource, …) that the bridge intentionally does not implement. `FeatureMap = 0` is therefore correct — the bridge advertises no optional time-sync features, which matches the implemented surface. |
| BD-Matter-Dispatcher-Synthesises-Globals | matter.js's behaviour layer auto-generates the Matter §7.13.2 global attributes (FeatureMap, ClusterRevision, AttributeList, AcceptedCommandList, GeneratedCommandList) on every cluster server | `endpoint/dispatcher.go::attributesFor` seeds the five universal globals at the start of every wildcard-attribute expansion and dedupes them against the per-server `MatterAttributes()` extras (lines 269-300 + 378-405) | The audit drifts L1-D04 / L1-D06 / L1-D12 ("MatterAttributes lister fehlt FeatureMap + ClusterRevision") are **false positives**: per-cluster `MatterAttributes()` enumerations focus on cluster-specific attributes; the dispatcher universally adds the five Matter-1.3 globals (EventList intentionally omitted — Apple iOS Matter SDK schema-mismatch reject). Regression tripwire: `TestRead_WildcardAttribute_ListerWithoutGlobalsStillGetsThem`. The wildcard-expansion contract is centrally enforced; per-cluster servers neither need nor benefit from listing globals again. |
| BD-Matter-EndpointID-Persistent | matter.js `@matter/node` Storage-Layer persists endpoint IDs across restarts so commissioners can re-use cached subscription targets | `endpoint/assembler.go::assignOrReuseID` looks the (source-key → endpoint-id) record up in the `store.EndpointStore` (SQLite) and reuses the persisted ID; a fresh allocation only happens when no record exists for that source key | Audit drift L6-D01 ("Endpoint-IDs werden beim Reassemble potenziell neu vergeben … bootid-basiert") is a **false positive**. Endpoint IDs come from the persistent endpoint store (not from `bootid.Salt()` — that one only feeds the UniqueID hash, see `BD-Matter-UniqueID-Stable`). A reassemble that finds the same source key reuses the same endpoint ID, so Apple's HAP-Mapper does not see a topology shuffle. Cross-restart durability of those store rows additionally depends on the boot-time GC gate documented in `BD-Matter-EndpointGC-ModelCompleteGate` (next row): before that gate landed, boot-time assembly against not-yet-loaded centrals erased every persisted row, and endpoint-number stability held only accidentally via deterministic re-allocation. |
| BD-Matter-EndpointGC-ModelCompleteGate | matter.js has no boot-time "empty model" window: `packages/node/src/storage/server/ServerEndpointStores.ts` pre-allocates every persisted endpoint number at `load()` ("Ensure all known numbers are allocated") and `assignNumber` reuses the stored number; the only release path is `eraseStoreForEndpoint`, invoked from `packages/node/src/node/server/ServerEndpointInitializer.ts::eraseDescendant` on explicit endpoint deletion | go-fabric assembles its topology from asynchronous CCU snapshots (Bridge.Start runs before the readiness-gated device load), so the same "numbers reserved until explicit removal" contract is enforced via `endpoint.Snapshot.ModelComplete`: `Assembler.gcVanished` (endpoint/assembler.go) skips vanished-source GC for any central whose initial southbound bring-up has not completed; `cmd/openccu-loom/daemon_matter.go::wireMatterCentralReadiness` latches per-central readiness from CentralSouthboundReadyEvent and `matterSnapshotter` stamps the flag | Mechanism divergence, behaviour parity: without the gate every daemon boot deleted ALL persisted endpoint-ID rows (boot assembly saw registered centrals with empty ModelRegistries and treated the whole fleet as vanished), so endpoint numbers were only accidentally stable via deterministic re-allocation and any fleet/exposure change across a restart renumbered everything after the change point — violating BD-Matter-EndpointID-Persistent and Matter §9.12.4 endpoint-number preservation. Failure direction is fail-safe: a missed ready event only defers GC of genuinely removed devices to a later model-complete assembly; it never deletes rows on stale information. Pinned by TestParityMatterJS_EndpointNumbersReservedUntilExplicitRemoval, TestAssemble_GCSkipsCentralWithIncompleteModel, TestAssemble_GCOfVanishedStillWorksAfterIncompleteAssembly (endpoint) and TestMatterSnapshotter_StampsModelCompletePerCentral (cmd). |
| BD-Matter-EndpointNumber-Monotonic | matter.js `packages/node/src/storage/server/ServerEndpointStores.ts::assignNumber` allocates from the persisted `#nextNumber` (`NEXT_NUMBER_KEY`) and skips numbers held in `#allocatedNumbers` / `#preAllocatedNumbers`; `eraseStoreForEndpoint` drops a number from those sets but never rewinds the counter, so a released number is not preferentially reissued | `store/endpoints.go::allocateEndpointID` allocates from the `next_endpoint_id` row of `matter_metadata` (migration 035), lifts the counter above every number `matter_endpoints` already holds, skips occupied numbers and wraps only past 65534 | Mechanism divergence, behaviour parity: the counter lives in SQLite instead of an in-process field, and the occupancy skip-set is read per transaction rather than held in memory, because two writers (assembly and the retrying upsert path) share one database. The former allocator returned the smallest unused number, so a number freed by `Assembler.gcVanished` — device unpaired, or exposure revoked — went to the next unrelated source. That is invisible on the wire: the Aggregator's `Descriptor.PartsList` set is identical before and after, so Apple Home / Google Home keep the cached DeviceTypeList and cluster set for that number and render the new device under the removed device's identity. Honest consequence of monotonic allocation: a source key removed and re-added later gets a fresh number. Pinned by `TestParityMatterJS_EndpointNumberNotReissuedAfterRemoval`, `TestParityMatterJS_EndpointNumberFreshAfterReAdd`, `TestParityMatterJS_EndpointNumberSeedsAboveExistingRows`. |
| BD-Matter-Reassemble-Closes-Removed | matter.js packages/protocol/src/interaction/SubscriptionHandler.ts — when a BridgedNode endpoint is removed from the Aggregator the `endpoint.lifecycle.remove()` path causes any subscription targeting that endpoint to be closed by the InteractionServer | `im/subscription/manager.go::CloseEndpoint` terminates only the subscriptions whose paths reference the removed endpoint ID (via `subReferencesEndpoint`) — subscriptions on the other 29 of 30 endpoints stay open | Audit drift L6-D08 ("Reassemble closes all subscriptions; matter.js does partial update") is a **false positive**: it conflates "all subscriptions targeting the removed endpoint" with "all subscriptions in the bridge". `CloseEndpoint(endpointID)` is targeted; the partial-update story holds. matter.js's "partielles Endpoint-Update" applies to attribute reports on existing endpoints, not to endpoint removal. |
| BD-Matter-SetReachable-LockReleased | matter.js BridgedDeviceBasicInformationServer emits ReachableChanged through its async `events.reachableChanged.emit` EventEmitter pattern | `cluster/core/bridged_device_basic_information.go::SetReachable` releases the internal `b.mu` **before** calling `emitter.MatterEmitEvent` (lines 438-455) so the emit path cannot deadlock against a Subscribe-Manager mutex acquired downstream | Audit drift L10-D06 ("ReachableChanged-Event synchron emittiert; Deadlock-Risiko wenn unter Lock") is a **false positive**: the emit runs after the unlock; there is no nesting of `b.mu` and any downstream lock. The synchronous-vs-asynchronous distinction is purely an internal-dispatch shape; observable behaviour is identical. |
| BD-Matter-DN-TXT-Set | matter.js `MdnsBroadcaster.ts::buildCommissionableInstanceData` emits the `DN` TXT key when a device name is supplied | `mdns/service.go::BuildCommissionableService` emits `DN` when `cfg.DeviceName != ""`; `bridge/bridge.go:1156` sets `DeviceName: params.NodeLabel` so the bridge's NodeLabel surfaces as the DN value | Audit drift L7-D03 ("DN TXT-Key fehlt im commissionable Record") is a **false positive**: the DN key is already wired through the bridge's parameter pipe; an empty DeviceName (and therefore an omitted DN) only happens when the bridge is started without a NodeLabel, which is rejected by `Config.Validate()`. |
| BD-Matter-PBKDF2-Documented | matter.js `PasePairing.ts` makes the PBKDF2 iteration count configurable; spec default is 1000 (Matter §3.10) | `example.config.yaml:240` documents `iterations: 1000` with the spec-mandated 1000..100000 range comment | Audit drift L5-D02 ("PBKDF2-Iterationsanzahl nicht in example.config.yaml dokumentiert") is a **false positive**: the annotated value is already present. `cluster/wire/admincommissioning.go:403-404` enforces the spec floor/ceiling at runtime. |
| BD-Matter-CriticalEventBypassMin | matter.js `ServerSubscription.ts:281` — "Urgent events are sent immediately" — events tagged urgent bypass the MinIntervalFloor gate | `im/subscription/subscription.go::drainEventsIfElapsed` (lines 235-258) sets `hasCritical = true` for any event with `im.EventPriorityCritical` and short-circuits past the MinIntervalFloor check | Audit drift L10-D02 ("EventPriorityCritical als Bypass-Kriterium vs. matter.js 'urgent flag'") is functionally equivalent: go-fabric's enum value is the equivalent of matter.js's urgent flag for Matter 1.3 / 1.4. When matter.js introduces additional priority/urgency tiers we will mirror them at the same point; until then the wire behaviour is identical. |
| BD-Matter-RemainingAuditTestGaps | matter.js carries its own cluster-revision regression suite via `@matter/model` and per-behaviour parity tests | go-fabric has parity tests for the actively-pair-tested cluster surface (OnOff, LevelControl on Switch/Light/Siren projections, the BridgedDeviceBasicInformation surface, the Aggregator + Root composition) but not for every device-type-projection cluster | Audit drifts L1-D08 (DoorLock), L1-D09 (WindowCovering), L2-D04 (OnOffPlug→OnOffLight upgrade), L2-D05 (LevelControl OO feature-bit), L7-D06 (commissionable TXT golden test) are **PARITY-TEST-GAPs**, not code drifts. L2-D03 (Switch power-source-attachment) is closed rather than open: the cross-channel attachment it described no longer exists — the Device Library specifies neither electrical measurement cluster for OnOffPlugInUnit (0x010A), so the metering channel now projects its own ElectricalSensor (0x0510) endpoint and the Switch projection carries no measurement clusters at all. The corresponding production paths have been Apple-pair-verified end-to-end; adding parity tests is desirable but does not block release. Tracked as an open behavioural-parity-test gap; new cases are added per `docs/matter-parity-contract.md`. L1-D07 (Thermostat) is superseded: the formerly Apple-pair-verified SystemMode read path surfaced Auto(1) whenever the HM device sat in its factory-default CONTROL_MODE=AUTO, violating SystemModeEnum conformance — Auto has conformance "AUTO" (matter.js `packages/model/src/standard/elements/thermostat-cluster.element.ts:558`) while the projection advertises a HEAT-only FeatureMap. Pair-success never exercised the state-sync echo: a controller writing the read value back received ConstraintError from the projection's own write gate, and RunningMode could yield Auto(1), which is not a ThermostatRunningModeEnum member at all (`element.ts:568-573`). Fixed in `internal/model/custom/climate/matter.go` (`systemModeFromHmMode` / `runningModeFromHmMode` clamp reads to the FeatureMap); pinned by `TestParityMatterJS_SystemModeAutoRequiresAutoFeature` and `TestThermostatSystemModeReadClampsHmAutoToFeatureMap`. See `BD-Matter-Thermostat-NoAutoFeature` (next row). |
| BD-Matter-Thermostat-NoAutoFeature | matter.js Thermostat composition may include the AUTO (AutoMode) feature, which mandates independent dual setpoints plus MinSetpointDeadBand (0x0019, conformance "AUTO" — `packages/model/src/standard/elements/thermostat-cluster.element.ts:100-104`) and requires HEAT+COOL (FeatureMap conformance "AUTO, O.a+", `element.ts:24-25,28`) | `internal/model/custom/climate/matter.go::featureMap` advertises HEAT always and COOL for SupportsCool profiles, but never AUTO — even on cooling-capable hybrids | HM climates are single-setpoint devices: the Matter Cool setpoint aliases the one HM setpoint and the projection implements no MinSetpointDeadBand, so advertising AUTO would be a conformance violation. HM's AUTO (week-program) mode is a single-setpoint schedule, not Matter AutoMode; SystemMode reads surface it as Heat(4), or Cool(3) when HEATING_COOLING==COOLING. ThermostatRunningMode stays feature-gated on AUTO ("TEVT & AUTO, [AUTO]", `element.ts:117-120`) and its value space is ThermostatRunningModeEnum (Off/Cool/Heat — no Auto member, `element.ts:568-573`). Precondition for lighting AUTO up: a true dual-setpoint surface plus MinSetpointDeadBand. Aligned with thermo.ThermostatServer's AUTO-clearing constructor; pinned by `TestParityMatterJS_SystemModeAutoRequiresAutoFeature`. |
| BD-Matter-TouchLastReport-Timing | matter.js sets the subscription's `lastReport` timestamp in the SubscribeResponse handler after the final Initial-Chunk has been pushed | `im/subscription/subscription.go:192-197` calls `TouchLastReport()` immediately after the Initial-Report flush sequence completes from the bridge's perspective | Audit drift L10-D04 is a timing-shape difference, not a correctness drift: the chunked Initial-Report sequence is owned by the bridge, which only signals "done" to the subscription after every chunk has been ack'd. The two stacks therefore land on the same effective `lastReport` instant; the only observable difference would be if a peer raced a Report into the gap before the final ack, which the bridge's ack-walk prevents. |
| BD-Matter-CloseSession-PeerAddrHeuristic | matter.js binds every session to its MessageChannel (`packages/protocol/src/session/Session.ts` `get channel()`), so an outbound CloseSession StatusReport can always be routed | go-fabric's UDP listener is connectionless and sessions do not own a channel object; the bridge resolves the peer address at send time from (1) the last authenticated Secure-Channel datagram per session (`Bridge.sessionPeerAddrs`), (2) the per-subscription routing target, (3) the owed-ack exchange table | A session whose peer address was never observed (no SC datagram, no subscription, no pending reliable exchange) skips the farewell with a debug log — acceptable because the notification is best-effort in matter.js too (try/catch-and-warn around `ExchangeManager.ts:658-666` `#sendCloseSession`) and the dominant controllers (Apple Home, chip-tool) always match at least one source. |
| BD-Matter-CloseSession-FabricTeardownSilent | matter.js emits gracefulClose (and therefore a CloseSession StatusReport) on every non-peer-lost session close, including fabric removal | go-fabric deliberately keeps CloseFabric / CloseFabricExcept / ClosePASESessions notification-free | The daemon defers the operational teardown after RemoveFabric so the NOCResponse can ride out on the closing session, and injecting a CloseSession farewell into that window would race the in-flight IM reply; the peer initiated the fabric operation and drops its sessions per protocol anyway. The graceful farewell is scoped to idle reap, same-peer stale-CASE eviction, on-demand ClosePeer invalidation, and daemon shutdown. |
| BD-Matter-IdleSessionReaper | matter.js keeps established operational sessions indefinitely — reclaim happens only on same-peer replacement, explicit close, or session-id exhaustion (`packages/protocol/src/session/SessionManager.ts` `findOldestInactiveSession`, ~:455-476) | go-fabric additionally runs a periodic idle reaper on the operational session manager (`cmd/openccu-loom/daemon_matter.go` — 60 s sweep, 5-min idle TTL); activity semantics mirror `Session.ts:127` `notifyActivity` exactly (refreshed on every authenticated receive including duplicates, and on every secure send), and reaped sessions ship the graceful CloseSession farewell | Headless long-running bridges accumulate dead CASE sessions from controllers that vanish without CloseSession. The TTL sits far above the subscription publisher heartbeat cadence, which `im/subscription/subscription.go` caps at `maxSendInterval` = 2 min (a deliberate divergence from matter.js, which needs no cap because it never reaps idle sessions; pinned by `TestSendInterval_LongMaxIntervalStillBeatsSessionIdleTimeout`), so live subscriptions are never evicted, and a dead controller stops earning send-side activity once the report retransmit cap reaps its subscription. Never-used sessions (lastActivity unset) stay exempt as commissioning protection. |
| L7-D07 | matter.js `MdnsAdvertiser.ts:213-232` — `DefaultBroadcastSchedule` with exponential back-off starting at 1 s, max 90 s; chip is event-driven (re-advertise on fabric change / reconnect) | `mdns/zeroconf.go::StartReannounceLoop` + `cmd/openccu-loom/daemon_matter.go:226` — fixed 30-min `StartReannounceLoop` | The 30-min cadence keeps Apple's `mDNSResponder` cache warm (TTL=4500 s ≈ 75 min). No controller correctness issue — all known commissioners accept periodic re-announcement. Event-driven re-announce (fabric add/remove, reconnect) plus a backoff burst matching matter.js's `DefaultBroadcastSchedule` is a v1.2 milestone. Drift L7-D07 (LOW by-design for v1.0). |
| L8-D03 | matter.js `AdministratorCommissioningServer.ts:283-290` — 48-h extended window when `FabricCount == 0`; chip `CommissioningWindowManager.cpp:313-325` — `MaxCommissioningTimeout()` extends to 48 h for uncommissioned nodes | `bridge/commissioning_window.go` — `OpenWindowParams.IsUncommissioned` flag enables the 172800 s (48 h) cap via `commissioningWindowMaxSecUncommissioned` | Implemented (C-P2-4): the constant `commissioningWindowMaxSecUncommissioned = 172800` is wired into `OpenWindow`; the daemon must set `IsUncommissioned: fabricStore.Count() == 0` before calling OpenWindow. Default (IsUncommissioned=false) retains the 900-s cap. Wiring the fabric-count query into the commissioning-window open path is a daemon-side follow-up. |
| L9-D9 | AccessControl.MatterReadFiltered + UpdateFabricLabel + UpdateNOC fabric resolution | `cluster/core/operational_credentials.go:252-286, 1058-1065, 1003-1008` | Confirmed correct by audit 2026-05-12 (L9-D9): Bug M + Bug P fixes are wire-correct; all three paths use `im.FabricFilterFromContext` with `currentFabric` fallback. No action required. Drift L9-D9 (LOW, confirmed ✓). |
| BD-Matter-Dispatcher-StringHeuristic | matter.js `InteractionServer.ts` and chip `WriteHandler.cpp` / `CommandHandler.cpp` map typed errors via `StatusCodeError` / `MatterClusterStatusError` interfaces only | `endpoint/dispatcher.go::writeErrorStatus` / `invokeErrorStatus` (lines ~447-509) keep a string-contains fallback for "read-only", "unknown attribute", "constraint", "resource exhausted", "unknown command", "invalid command argument" | The 2026-05-19 chip-audit drift M-DRIFT-02 / L4-D03 is a **defense-in-depth** entry: every production cluster server already returns typed errors that implement `im.StatusCodeError` (verified via `grep -rn 'errors.New(' cluster/` — zero hits in production paths; the matches in `tests/` and `endpoint/*_test.go` are intentional fake-server fixtures). The string heuristic survives so legacy fakes keep working; removing it would only break tests, not production wire behaviour. New cluster code is expected to return typed errors via the `StatusCodeError` pattern. |
| BD-Matter-Groups-Already-Mounted | chip + matter.js mandate Groups (0x0004) + ScenesManagement (0x0062) on every OnOff-mapped device-type (OnOffPlugInUnit 0x010A, OnOffLight 0x0100) | go-fabric mounts both stub servers on `Switch` (`internal/model/custom/switch/matter.go:74-75`), `Light` (both dimmable and non-dimmable branches in `internal/model/custom/light/matter.go:114-115, 120-121`), `Siren` (`internal/model/custom/siren/matter.go:131-132`), and the generic-DP OnOff projection (`internal/model/generic/switch_matter.go::MatterClusterServers`) | Audit drift L2-D01-NEW is a **false positive** for the custom-DP projections. The generic-DP projection — the assembler's Path 1, taken for any channel with a writable STATE and no custom-DP wrapper — was the one exception and mounted OnOff alone while still advertising OnOffPlugInUnit; it now mounts both stubs and advertises the mandatory LT feature with its four attributes and three commands. Climate / Cover / Lock correctly have no Groups attachment (non-OnOff device types). |

| BD-Matter-TimeSync-NotMounted | matter.js `packages/node/src/endpoints/root.ts:215` lists TimeSynchronization (0x0038) as `optional` on RootNode | `cmd/openccu-loom/daemon_matter.go::buildRootClusters` — not mounted by default; operator-mountable via `north.matter.enable_time_sync` (default off, 0.15.0) | TimeSynchronization is optional on a Matter-Bridge; home-assistant-matter-bridge omits it, and Apple's HAP mapper may reject unexpected clusters on the RootNode device-type. The cluster implementation exists (`cluster/core/time_synchronization.go`); since 0.15.0 it is mounted only behind the default-off `north.matter.enable_time_sync` opt-in (operators who need a time-sync surface, re-pair afterwards). Status (2026-06): flag-gated mount available; default remains off. (M-P2-03 documented as by-design.) |
| BD-Matter-Actions-NotMounted | chip bridge-app mounts Actions (0x0025) on the Aggregator endpoint for the TC-BR test plan | `cmd/openccu-loom/daemon_matter.go::buildAggregatorClusters` — Actions intentionally not mounted (only Identify 0x0003 + Descriptor 0x001D are mounted; the SetServerListProvider comment names 0x0025 as the deliberate omission) | go-fabric has no scene/action surface to model via Actions. Identify (0x0003) is already mounted; Actions will be added if bridge-app TC-BR conformance is required. Status (2026-06): still intentionally not mounted (no use-case). (C-P2-1 documented as by-design.) |
| BD-Matter-ARL-NotMounted | Matter 1.4 Access Restriction List (cluster 0x002B) for Managed Aggregator use-case | `cluster/core/access_restriction.go` — `ARLClusterID = 0x002B` constant only; no cluster-server struct, no `New…` constructor, not mounted | go-fabric does not implement the Managed Aggregator use-case. ARL cluster constant is in the skeleton for the integration point. Mount on Root endpoint + wire commands when Managed Aggregator is in scope. Status (2026-06): still skeleton-only; verified no server struct/constructor exists. (C-P2-3 by-design. See C-L1-ARL for the re-activation checklist.) |

Bei jeder Änderung an einer der oben gelisteten Stellen ist zu prüfen, ob die Divergenz noch begründbar ist; ggf. Eintrag aktualisieren oder entfernen.

### L00 Schema Audit — chip-lag items (2026-05-12)

The following entries reflect clusters where go-fabric follows matter.js HEAD (the gold standard) while chip HEAD has advanced further. These are **not** bugs in go-fabric; they are tracked here so re-audits score them ✅ rather than flagging false drift.

- **L00-D04** TemperatureMeasurement ClusterRevision=5 — go-fabric follows matter.js HEAD (`temperature-measurement.element.ts:14` default=5); chip HEAD is at rev 4 (`zzz_generated/app-common/clusters/TemperatureMeasurement/Metadata.h:20`). Reason: chip lags matter.js on this cluster; gold standard is matter.js.
- **L00-D05** RelativeHumidityMeasurement ClusterRevision=4 — go-fabric follows matter.js HEAD (`relative-humidity-measurement.element.ts:14` default=4); chip HEAD is at rev 3 (`zzz_generated/app-common/clusters/RelativeHumidityMeasurement/Metadata.h:20`). Reason: chip lags matter.js; gold standard is matter.js.
- **L00-D06** BooleanState ClusterRevision=2 — go-fabric follows matter.js HEAD (`boolean-state.element.ts:19` default=2); chip HEAD is at rev 3. go-fabric does not implement StateChangeEvent (rev-3 addition) because StateChangeEvent is conformance `"O"` in matter.js; HM push events are handled via DP value changes. Reason: matter.js gold standard at rev 2; omitting optional event is intentional.
- **L00-D07** BasicInformation ClusterRevision=5 — go-fabric follows matter.js HEAD (`basic-information.element.ts:20` default=5); chip HEAD is at rev 6 (adds PowerCycleCount). Reason: chip ahead of matter.js; gold standard is matter.js at rev 5.
- **L00-D08** BridgedDeviceBasicInformation ClusterRevision=5 — go-fabric follows matter.js HEAD (`bridged-device-basic-information.element.ts:20` default=5); chip HEAD is at rev 6 (adds ProductAppearance+ConfigurationVersion mandates). Reason: chip ahead of matter.js; gold standard is matter.js at rev 5.
- **L00-D09** AccessControl ClusterRevision=2 — go-fabric follows matter.js HEAD (`access-control.element.ts:21` default=2); chip HEAD is at rev 3 (adds MNGD feature). Reason: chip ahead of matter.js; gold standard is matter.js at rev 2.
- **L00-D10** NetworkCommissioning ClusterRevision=2 — go-fabric follows matter.js HEAD (`network-commissioning.element.ts:20` default=2); chip HEAD is at rev 3 (multi-interface prioritization). Reason: chip ahead of matter.js; gold standard is matter.js at rev 2.
- **L00-D11** GeneralDiagnostics ClusterRevision=2 — go-fabric follows matter.js HEAD (`general-diagnostics.element.ts:21` default=2); chip HEAD is at rev 3 (PayloadTestRequest command). Reason: chip ahead of matter.js; gold standard is matter.js at rev 2.
- **L00-D12** OnOff Options attribute (0x000F) — ✅ **Resolved**. The spurious attribute has been removed from `internal/model/custom/switch/matter.go`: the constant, the `MatterRead` case, and its entry in `MatterAttributes()` are gone, matching matter.js `on-off.element.ts` and chip `zzz_generated/.../OnOff/AttributeIds.h`. Regression tripwires `TestParityMatterJS_SwitchOnOffNoSpuriousOptions` and `TestParityMatterJS_OnOffServer_OptionsAbsent` assert the absence so a future re-port cannot reintroduce it.
- **L00-D13** IlluminanceMeasurement ClusterRevision=4 — go-fabric follows matter.js HEAD (`illuminance-measurement.element.ts:19` default=4); chip HEAD is at rev 3. Reason: chip lags matter.js; gold standard is matter.js at rev 4.
- **L00-D14** PressureMeasurement ClusterRevision=4 — go-fabric follows matter.js HEAD (`pressure-measurement.element.ts:18` default=4); chip HEAD is at rev 3. Reason: chip lags matter.js; gold standard is matter.js at rev 4.

### L00 Schema Audit — cluster-stub design choices (2026-05-12)

- **L00-BD-Groups** Groups/ScenesManagement as stubs — HM has no group/scene concept; go-fabric's Groups and ScenesManagement cluster servers return empty collections and reject all writes. Presence is mandated by Matter device-type requirements (OnOff Light, Dimmable Light, etc.), not feature preference. matter.js `packages/node/src/behaviors/groups/GroupsServer.ts` / `packages/node/src/behaviors/scenes-management/`. Reason: no Homematic CCU-side primitive to map to; stubs satisfy the device-type conformance requirement without exposing broken functionality.
- **L00-BD-OnOffDefault** OnOff default-false on unobserved state — matter.js `OnOffServer.ts` defaults `onOff` to `false` when the underlying DP has not yet reported; go-fabric mirrors this rather than returning null (which matter.js also documents as not spec-nullable for the OnOff attribute). Reason: spec-aligned default; null would be a schema violation.
- **L00-BD-BoolStateEvent** BooleanState no StateChangeEvent — StateChangeEvent (event id 0x0) has conformance `"O"` in matter.js `packages/model/src/standard/elements/boolean-state.element.ts`; go-fabric omits it because HM push events are handled via DP value changes, not a dedicated state-change event mechanism. Reason: optional event, no HM-side equivalent event source.

### Parity Audit 2026-05-12 — L4 IM Engine + L10 Subscribe Lifecycle

The following items were classified during the 2026-05-12 L4/L10 parity
audit. Code-fixed items have regression tests; by-design items are
documented here; out-of-scope items (bridge/) are deferred.

| ID | Severity | Status | Notes |
|---|---|---|---|
| L4-D04 | MED | By-design (already `L6-PFAD-4`) | Wildcard-endpoint Invoke rejects with `UnsupportedEndpoint`; no cluster requires it today. |
| L4-D05 | MED | **Fixed** (2026-05-12) | `timedDeadlines` now keyed by `struct{sessionID, exchangeID uint16}` (`bridge/bridge.go::timedKey`). Regression test: `TestTimedRequest_SessionScopeIsolation`. |
| L4-D06 | LOW | Fixed (regression test added) | CommandRef absent from response when not in request. Guard via `HasCommandRef` is correct; regression test added to `im_test.go`. |
| L10-D05 | MED | By-design (chip alignment) | `SuppressResponse` not set on ongoing subscription ReportData; follows chip pattern; Apple Home expects IM:StatusResponse. See entry below. |
| L10-D06 | MED | **Fixed** (2026-05-12) | KeepSubscriptions=false PASE path now calls `m.CloseSession(sessionID)` instead of skipping (`bridge/subscribe.go`). Regression test: `TestCloseSession_ClosesMatchingSessionSubscription`. |
| L10-D07 | MED | Fixed (code comment) | `snapshot()` concurrency safety documented in `subscription/manager.go`. No bug; comment added for future readers. |
| L10-D08 | LOW | **Fixed** (2026-05-12) | Send-failure path in `reportSubscription` now calls `m.Close(sub.ID)` so the manager evicts the dead subscription. Regression test: `TestReport_PeerUnreachable_ClosesSubscriptionWithoutMRP`. |

#### L10-D05 detail — SuppressResponse not set on ongoing subscription ReportData (chip alignment)

**matter.js ref:** `packages/protocol/src/interaction/InteractionMessenger.ts:679`
— `suppressResponse: dataReport.moreChunkedMessages ? false : dataReport.suppressResponse`;
`ServerSubscription#sendUpdateMessage` sets `suppressResponse: true` for
non-chunked single-chunk ongoing reports so the subscriber ACKs via MRP
standalone ACK.

**chip ref:** `src/app/ReadHandler.cpp:340`
— `responseExpected = IsType(Subscribe) || aMoreChunks`; for ongoing
non-chunked reports `IsType(Subscribe)=true` forces `responseExpected=true`,
meaning chip always expects an IM:StatusResponse on subscription reports.

**go-fabric:** Follows chip (`SuppressResponse=false` zero value).

**Rationale:** Apple Home uses the chip pattern in practice (ongoing
reports receive IM:StatusResponse, not bare MRP ACK — empirically
verified 2026-05-11 via tcpdump). Switching to matter.js's
`suppressResponse=true` behaviour could break Apple Home's ReadClient
flow. Alignment with chip is safer. Revisit if testing shows a specific
commissioner uses matter.js's path.

**File:** `bridge/subscribe.go:reportSubscription`.

---

### Parity Audit 2026-05-12 — L5 Secure Channel + L6 Bridge + Resumption Store

| ID | Severity | Status | Notes |
|---|---|---|---|
| L5-D5 | LOW | **By-design** | Resumption store has no LRU cap per fabric. See detail below. |
| L5-D6 | LOW | **Fixed** (2026-05-12) | `sigma1Replied` map pruned on TTL-reap via `PerExchangeCaseProvider.SetOnEvict` + `forgetSigma1Replied`. Regression tests: `TestSigma1Replied_PrunedOnSigma3`, `TestPerExchangeCaseProvider_OnEvictPrunesSigma1Replied`. |
| L6-D04 | MED | **Fixed** (2026-05-12) | Subscriptions for removed endpoints reaped after reassemble via `Manager.CloseEndpoint`. Regression tests: `TestReassemble_ReapsSubscriptionsForRemovedEndpoints`, `TestCloseEndpoint_ClosesMatchingSubscriptions`. |

#### L5-D5 detail — Resumption store: SQLite upsert vs. in-memory LRU cap

**matter.js ref:** `packages/protocol/src/session/SessionManager.ts` —
in-memory `ResumptionRecord` map; no hard cap; bounded by fabric/node count.

**chip ref:** `src/protocols/secure_channel/DefaultSessionResumptionStorage.h:kSessionResumptionStorageMaxEntries = 8` per node.

**go-fabric:** `store/resumption.go:UpsertResumption` uses
`ON CONFLICT(fabric_index, peer_node_id) DO UPDATE` — exactly **one row per (fabric, peer) pair**.
The unique constraint on `(fabric_index, peer_node_id)` already enforces a natural cap: each peer
has at most one active resumption record at any time. Adding a per-fabric LRU cap (chip's 8 / matter.js
soft 32) would require a DELETE-after-upsert pass over the SQLite table. For a bridge with a typical
fleet of 3–10 controllers per fabric this would be pure overhead; the SQLite unique index is a more
durable and auditable enforcement mechanism than an in-memory counter.

**Rationale:** The by-design cap is one-per-peer (implicit via the DB unique constraint), which is
stricter than chip's 8-per-node in the single-peer case and weaker in the many-peer case. For a
homematic bridge with a single controller hub this is never observable. If a future production scenario
requires 8+ distinct peers per fabric, a `LIMIT 8 ORDER BY last_used_at` eviction pass can be added
to `UpsertResumption` without a schema change (migration `last_used_at` column is already present).

---

### L01 Cluster Server Audit — by-design items (2026-05-12)

The following L01 entries were reviewed during the 2026-05-12 MED/LOW wave and
classified as by-design divergences. Code-fixed items (L01-D06..D10) have
regression tests in the respective source packages.

**L01-D06** celsiusToInt16 clamp fixed: 32767 is TLV-null sentinel; -32768 is below
absolute-zero floor. Clamped to 32766 / -27315 per chip `kMaxMeasuredValueRange` /
`kMinMeasuredValueRange`. Tests: `TestTemperatureServerSaturatesHigh/Low`.

**L01-D07** PressureServer min/max fixed: `MinMeasuredValue=0`, `MaxMeasuredValue=32766`.
Tests: `TestPressureServerMinMeasuredValueIsZero` / `TestPressureServerMaxMeasuredValueIs32766`.

**L01-D08** ScenesManagement.MatterInvoke fixed: error now contains "no commands" for
correct UnsupportedCommand (0x81) mapping in the bridge dispatcher.
Test: `TestScenesManagementInvokeContainsNoCommands`.

**L01-D09** BooleanState non-nullable default fixed: returns `(false, true)` when
unobserved. StateValue has no quality X per matter.js. Test: `TestBooleanStateServerUnobserved`.

**L01-D10** OnOff MatterWrite nil guard fixed: `Switch.MatterWrite` no-ops on nil value
preventing future panic if a scene controller writes nil.
Test: `TestMatterWriteNilValueIsNoOp`.

---

### L02 DeviceType Audit — by-design items (2026-05-12)

**L02-D01** Siren: BooleanState removed from `Siren.MatterClusterServers()`. BooleanState
(0x0045) is not in the OnOffPlugInUnit (0x010A) mandatory/optional cluster set per
matter.js `packages/node/src/devices/on-off-plug-in-unit.ts:86-105`; mounting it
causes UnsupportedCluster rejection by strict controllers. The sirenBooleanStateServer
type is retained for potential future use. Test: `TestSirenClusterServersIncludeOnOff`
asserts 0x0045 is absent.

**L02-D02** OccupancySensing PIROccupiedToUnoccupiedDelay (0x0010) added: returns
`uint16(0)` (no configurable HM hold-time). Conditionally conformant when PIR feature
is active per matter.js `OccupancySensingServer.ts:17-60`.
Tests: `TestOccupancyServerPIRDelayPresent` / `TestOccupancyServerMatterAttributesIncludesPIRDelay`.

**L02-D03 — Siren device-type semantic approximation (by-design).**
Generic Siren (HmIP-ASIR) maps to OnOffPlugInUnit (0x010A). No better-fit Matter
device type exists in Matter 1.5.1 for a multi-tone siren. Renders as plug/outlet in
Apple Home — accepted until Matter introduces a dedicated Siren/Alarm device type.

---

### L06 Bridge Composition Audit — by-design items (2026-05-12)

**L06-D01 — Aggregator Identify absent (by-design).**
matter.js `aggregator.ts:62` marks Identify optional on EP 1. No Apple/chip-tool
failure mode known. chip mounts it in the reference bridge but chip's static-endpoint
composition is not a gold standard for optional cluster policy.

**L06-D02 — Aggregator Actions absent (roadmap v1.1).**
matter.js marks Actions optional. Enables Matter action automation across bridged
endpoints. No short-term breakage. Deferred to v1.1.

**L06-D03 — mDNS operational record not re-published after topology change (by-design).**
Operational `_matter._tcp` record is fabric-specific (NodeID/CompressedFabricID are
static per fabric). Topology changes do not require a new record; commissioners
reconnect via the same record. No functional gap.

**L06-D04 — Vanished endpoint: stale subscriptions reaped after reassemble. FIXED 2026-05-12.**
`Bridge.reassembleLocked` now calls `Manager.CloseEndpoint(ep.ID)` for every endpoint present
in the previous topology but absent in the new one. The new `CloseEndpoint` method in
`im/subscription/manager.go` closes any subscription whose AttributePaths or EventPaths reference
the removed endpoint. Regression tests: `TestReassemble_ReapsSubscriptionsForRemovedEndpoints`,
`TestCloseEndpoint_ClosesMatchingSubscriptions`, `TestCloseEndpoint_EventPathAlsoMatches`.

### Systematic Parity Run #02 — PFAD-ASYMMETRIEN (2026-05-12)

Aus `notes/parity/audit_runs/2026-05-12_systematic_02.md` §5 — 38 neue
🔄 Einträge, gruppiert nach Plan-Layer. Quelle: 7 parallele Sonnet
Sub-Agents über die 11 Plan-Layer.

**L0-D04 — Descriptor.TagList not advertised when TAGLIST feature absent (by-design).**
TagList (0x0004) conformance is "TAGLIST" per Matter §9.5.4.1 + §9.5.6.5 — only present
when the TAGLIST FeatureMap bit is set. go-fabric advertises FeatureMap=0 (no TAGLIST).
`descriptor.go:113-123` returns `(nil, false)` for TagList: Apple's iOS Matter SDK does
not yet ship the `semtag` struct schema and rejects the whole Descriptor cluster when
TagList appears in wildcard expansion (HAPErrorDomain Code=14). matter.js and chip-tool
tolerate the absence. Re-enable only when TAGLIST FeatureMap bit is set AND Apple ships
semtag schema. matter.js ref: `packages/model/src/standard/elements/descriptor.element.ts`.

**L2-D01 + EventList suppression — EventList (0xFFFA) suppressed on all clusters / all endpoints (by-design).**
All 17 Apple cache-drops for attribute 0xFFFA (EventList) across every cluster on every
endpoint (EP0 root, EP1 aggregator, EP14, EP28, …) are expected and harmless. Both
matter.js HEAD and chip return `StatusUnsupportedAttribute` for 0xFFFA when Apple's
iOS Matter SDK (pre-1.4 schema) reads it; go-fabric does the same via
`endpoint/dispatcher.go:413-424`. Apple iOS 26 does not ship a schema for EventList
(Matter 1.4 global attr); returning `[]uint32{}` caused `MTRErrorDomain Code=12` and
dropped the entire ReportData stream. UnsupportedAttribute is handled gracefully.
Re-enable when Apple iOS ships Matter 1.4 SDK schema. No code change needed.
matter.js ref: `packages/protocol/src/interaction/InteractionServer.ts`.
chip ref: `src/app/clusters/*/` Attribute::kEventList.

**L2-D04 — OnOffLight/OnOffPlugInUnit: Groups + ScenesManagement stubs present; chip bridge-app omits them (by-design).**
matter.js `packages/node/src/devices/on-off-light.ts` and `on-off-plug-in-unit.ts` list
Groups (0x0004) and ScenesManagement (0x0062) as mandatory. go-fabric's
`internal/model/custom/switch/matter.go:74-86` returns them from `MatterClusterServers()`,
matching matter.js. chip's `examples/bridge-app/linux/main.cpp:113-151` minimal sample
omits them. Per §3.A.4 resolution rule: matter.js > chip for schema. Apple pairs cleanly
with stubs present (verified 2026-05-12). No code change needed.

**L6-D03 — Aggregator Descriptor.ServerList composition (resolved).**
matter.js `packages/node/src/endpoints/aggregator.ts:56-62` lists Identify, Actions, and
CommissionerControl as optional. Matter Device Library §13.2 mandates no cluster other
than Descriptor on an Aggregator. go-fabric now mounts Identify + Descriptor on EP1:
chip's reference bridge-app composes the Aggregator with Identify + Descriptor + Actions,
and the Apple-pair empirics confirmed that an Identify-less Aggregator triggers Apple's
HAP-Mapper to fall back to "structural placeholder" treatment and skip PartsList
traversal. ServerList is now derived from the mounted set via `SetServerListProvider`
(`buildAggregatorClusters` in `cmd/openccu-loom/daemon.go`) — no static list to drift
when Actions or another optional cluster lands.

**L0/L1 — Structural Decorator-Patterns (5):**
- matter.js Behavior-Decorator (`Behavior.with(...)` mixin) ↔ Go struct
  + Constructor mit defaults (`NewBasicInformation(...)`). PFAD-ASYMMETRIE
  weil TS-Decorator-Composition kein Go-Pendant hat; Go nutzt Constructor +
  Method-Receiver-Pattern.
- matter.js `FabricScopedReader` interface ↔ Go inline access check via
  `FabricFilterFromContext(...)` (Bug M Pattern).
- matter.js CSR session binding via Promise-chain ↔ Go pointer-pinned
  `pendingCSRSessionID` (L9-D5).
- matter.js DataVersion mechanism (`@matter/protocol` `version` field) ↔
  Go `DataVersionTracker` per cluster.
- matter.js cluster-server lifecycle hooks (`onInitialize`) ↔ Go
  Constructor + Registry-Wire.

**L2/L6 — Bridge composition (5):**
- matter.js Behavior-init via `Node.startup` ↔ Go endpoint-materialize in
  `endpoint/materialize.go::Materialize`.
- matter.js Effects-API reactive state ↔ Go EventBus subscriber.
- matter.js Aggregator hierarchy via `parts: { child: { ... }}` nested ↔
  Go flat slice + `ParentEndpointID` hint (verifyable via Descriptor).
- matter.js auto-persisted UniqueID per endpoint via `@matter/node`
  storage ↔ Go bootid.Salt mixed in.
- chip C++ atomic ExchangeID seeding ↔ Go `atomic.Uint16`.

**L3 — TLV Wire Codec (Systematic Parity Run #02 — 2026-05-12):**

- **L3-D2 ImplicitProfile-tag pass-through:** matter.js throws
  `NotImplementedError` for `ImplicitProfile16`/`ImplicitProfile32` tags
  (TlvCodec.ts:171-172); chip resolves them against `ImplicitProfileId` and
  returns `CHIP_ERROR_UNKNOWN_IMPLICIT_TLV_TAG` when no profile ID is set
  (TLVReader.cpp:872-879). go-fabric's `Decoder.readTag` silently surfaces
  them as `Tag{Kind: TagKindImplicitProfile2/4, Number: n}` without
  resolution. By-design: no commissioner sends ImplicitProfile-tagged TLV to
  a bridge in any known commissioning or interaction flow (verified 60+ pair
  iterations). The comment at `tlv/decode.go:83` records
  the open risk for future code paths that might require chip's
  `ImplicitProfileId` pattern. Wire impact: none (zero byte difference on
  current production paths).

- **L3-D5 Container-type tag validation:** chip's TLVWriter.cpp:WriteElementHead
  rejects context-tagged elements inside an Array container with
  `CHIP_ERROR_INVALID_TLV_TAG`. matter.js enforces the same rule at the schema
  layer (TlvArray/TlvObject). **Implemented in Systematic Run #02:**
  `tlv/encode.go` now maintains a `containerStack
  []ElementType` and panics when a context tag is written inside an Array.
  This is an implemented fix, not a by-design entry; the earlier by-design
  comment in encode.go has been replaced with the implementation.

- **L3-D6 PutUintSized / smallest-fit difference:** matter.js
  `TlvUInt64.encodeTlvInternal` (TlvNumber.ts:112-115) uses smallest-fit width
  for any schema-declared `uint64` field (e.g. `BigInt(1)` → 1 byte via
  `TlvLength.OneByte`). go-fabric's `PutUint64` always emits 8 bytes; chip
  tolerates both widths (TLVReader.cpp:GetValue template). **Implemented
  helper in Run #02:** `PutUintWidth(tag, value, widthBytes)` now allows
  callers to emit any exact fixed width. `PutUint64` continues to emit 8 bytes
  (no wire impact vs Apple Home or chip-tool). Cosmetic difference only.

- **L3-D7 TlvObject.injectField not implemented:** matter.js
  TlvObject.ts:279-294 provides `injectField` for IM-layer DataVersion
  injection. go-fabric handles DataVersion at the IM call site
  (im/read.go, im/subscribe.go) by constructing the full AttributeReport
  struct before encoding. No wire difference; different API shape only.

- **L3-D8 TlvObject.removeField not implemented:** matter.js
  TlvObject.ts:300-313 provides `removeField` for fabric-scope filtering.
  go-fabric filters at the cluster-server level before TLV encoding. No wire
  difference; different API shape only.

- **L3-D9 No TlvSchema.validate layer:** matter.js (TlvSchema.ts abstract base
  + TlvNumber.ts:124-135) runs `validate()` post-decode to enforce
  cluster-defined constraint bounds (min/max). chip enforces via
  EmberAfAttributeType at AttributeValueDecoder level. go-fabric uses Go
  struct validation at the cluster-server boundary (`PutInt16Bounded`,
  `PutUint8Bounded`, etc.) — the Bounded helpers reject nullable sentinels on
  encode. The TLV decoder itself is a raw byte reader with no schema layer.
  Architectural reason: Go's type system and struct validation at the
  cluster-server boundary replace the TypeScript schema-object approach.
  Nullable sentinel protection is explicit-opt-in per attribute; cluster
  servers must use Bounded helpers for nullable attributes. The existing
  `TestObjectParity_ValidationError_NotImplemented` skip in
  object_parity_test.go documents the gap. Risk: a cluster server that uses
  raw `PutInt16` for a nullable int16 attribute could accept the `-32768`
  sentinel as a regular value; this is caught in code review, not at the TLV
  layer.

- **L3-D10 allowProtocolSpecificTags not validated:** matter.js
  TlvObject.ts:71,195 `TlvTaggedList` schema has an `allowProtocolSpecificTags`
  flag; when false it throws on non-context tags inside a List. chip has no
  direct equivalent flag. go-fabric's decoder passes non-context tags through
  without error. No Apple-pair impact: no commissioner sends ImplicitProfile or
  FullyQualified tags inside List elements to a bridge in the current bridge
  surface. The `TestObjectParity_TaggedList_ProtocolSpecificTags_NotImplemented`
  skip documents the gap.

- **L3-D11 Chunked-array API:** go-fabric chunks ReportData at the
  AttributeReport boundary (bridge/reply.go:274-368); matter.js supports
  per-element list-index chunking inside a single AttributeReport
  (TlvArray.ts:156-176). Apple Home tolerates over-budget single-attribute
  chunks empirically. Listed as v1.1 future work in memory note
  `matter_udp_mtu_2048.md`.

- **L3-D13 Nullable bounds restriction:** matter.js TlvNullable.ts:27-44
  arithmetically shrinks the inner schema's max by 1 (unsigned) or min by 1
  (signed) to reserve the null-sentinel slot, preventing the sentinel from
  being a valid value at schema construction time. go-fabric implements the
  equivalent guard at encode time via the `Bounded` helpers
  (`PutUint8Bounded`/`PutInt16Bounded`/etc.) which return
  `ErrUint8NullableSentinel`/`ErrInt16NullableSentinel`/etc. when the sentinel
  value is passed. Callers on nullable attributes must use the Bounded path;
  using raw `PutInt16` / `PutUint8` on a nullable attribute bypasses the
  guard. This is enforced by convention and code review, not by the type
  system. No wire difference when Bounded helpers are used correctly.

**L4 — IM Engine (3):**
- StatusResponseError typing: matter.js exception hierarchy ↔ Go typed
  error interface (`StatusCodeError` aus L4-D03 fix).
- DataVersionFilter cache: chip `mCommissionerSessionId` per-session ↔
  Go per-IM-context `DataVersionTracker`.
- Path-Interpreter delegation: chip `ConcreteAttributePath` static ↔ Go
  `dispatcher.Resolve(...)` runtime resolution.

**L7 — mDNS (3 + 1 from Run #02):**
- Hostname: chip MAC-derived (`{macHex16}.{network}`) ↔ go-fabric OS
  hostname (`<host>.local.`). Beide commissioners akzeptieren beide.
  (L7-D02 entry — confirmed by-design.)
- Subtype-emit: go-fabric emittiert `_T<DeviceTypeID>._sub`
  (verifiziert `mdns/service.go:408`). Kein drift.
- TXT-builder: matter.js incremental builder pattern ↔ Go static
  `mdns/txt.go` constants.
- **L7-D02 (Run #02) Subtype PTR TTL 120 s:** `SubtypeResponder` emits
  PTR TTL=120 s (`mdns/subtype_responder.go:355`).
  chip `src/lib/dnssd/minimal_mdns/records/ResourceRecord.h:35`
  `kDefaultTtl=120` for SRV/PTR — go-fabric matches chip exactly.
  matter.js offloads TTL to the DNS-SD library (no explicit value set
  for subtype PTRs). Audit #02 verdict: ✓ No drift against chip.
  By-design: 120 s is the chip-verified correct PTR TTL for subtype records.

**L8 — Commissioning Window (3):**
- Window-Timer: chip `CHIP_System_Layer` for cross-platform timer ↔ Go
  `time.AfterFunc`.
- PASE-only session lifecycle: chip global `PASESession.*` ↔ Go
  `pase.Manager.Close()`.
- AdminFabricIndex resolution: chip `OnSessionEstablished` callback
  binds index ↔ Go IM context `fabric.Index()`.

**L10 — Subscribe lifecycle (3 + 3 new from agent4 L10 run):**
- Heartbeat mechanism: matter.js system clock + Promise ↔ Go `time.Ticker`
  + goroutine.
- Resubscribe trigger: matter.js Promise-chain in
  `SubscriptionHandler.shouldResubscribe()` ↔ Go goroutine-loop in
  `im/subscription/resubscribe.go`.
- Dirty-marking: matter.js `Set<AttributeKey>` ↔ Go
  `map[paramset.Path]struct{}` with `sync.Mutex`.

**L10-D09 — Randomisation window absent (PFAD-ASYMMETRIE, by-design):**
matter.js `packages/node/src/node/server/ServerSubscription.ts:282` adds
`subscriptionRandomizationWindow * Math.random()` to the heartbeat send
interval to distribute publisher traffic across subscriptions sharing one
JS event loop. chip `src/app/ReadHandler.cpp:769-809` applies NO such
randomisation — it uses the negotiated MaxInterval directly. go-fabric
follows the chip model: subscriptions are created at different wall-clock
offsets so their `lastReport` stamps already diverge organically, providing
natural phase scatter without explicit randomisation. The omission is
unobservable by Apple Home or chip-tool. Code: `im/subscription/subscription.go`
`sendIntervalLocked()` comment.

**L10-D10 — MaxInterval publisher selection semantics (chip alignment, no drift):**
Matter §10.6.3.2 requires the SubscribeResponse MaxInterval ≤ requested
MaxInterval. chip `src/app/ReadHandler.cpp:769-809` (non-ICD path) accepts
the subscriber's MaxInterval and bounds it by `kSubscriptionMaxIntervalPublisherLimit`
(3600 s). matter.js `packages/node/src/node/server/ServerSubscription.ts:269-282`
additionally lifts by `minIntervalFloor`. go-fabric clamps `maxCeil` down to
`cfg.MaxIntervalCeilingSeconds` (default 3600 s) and `minFloor` up to
`cfg.MinIntervalFloorSeconds`; the post-clamp inversion check
(`ErrCadenceInvertedAfterClamp`) rejects inverted cadences — equivalent to
matter.js's lower-bound guarantee. The advertised MaxInterval in SubscribeResponse
satisfies §10.6.3.2. Classified as ✓ (chip-aligned, no code change needed).
Code: `im/subscription/manager.go` `Subscribe()` comment.

**L10-D11 — Shared engine ticker vs. matter.js per-subscription timer (PFAD-ASYMMETRIE, by-design):**
matter.js `packages/node/src/node/server/ServerSubscription.ts:191` gives each
ServerSubscription its own `Time.getTimer(sendInterval, callback)`. chip uses
per-ReadHandler state in the IM engine. go-fabric uses one shared ticker at
`cfg.TickInterval` (default 250 ms) that iterates all subscriptions. A
subscription can fire up to 250 ms early compared to a strict per-timer model —
unobservable by any Matter commissioner because it falls within the spec window
and the MinIntervalFloor gate prevents over-reporting. The shared ticker is the
idiomatic Go approach; per-subscription goroutines would create O(N) timers
unnecessarily. Code: `im/subscription/engine.go` `run()` comment.

### Systematic Parity Run #02 — Closed Drift Items (2026-05-12)

Drifts closed by implementation wave closing audit `2026-05-12_systematic_02`.

**L0-D01 — BasicInformation.ConfigurationVersion omitted when unset (by-design, consistent with matter.js bridge sample).**
matter.js `packages/model/src/standard/elements/basic-information.element.ts:104` specifies
ConfigurationVersion (0x0018) with `default: 1` and `constraint: "min 1"`, but the matter.js
bridge sample (`packages/node/src/devices/root-node.ts`) omits ConfigurationVersion on the Root
endpoint when not explicitly provisioned. chip's bridge-app also omits the attribute. go-fabric
mirrors this bridge-sample behaviour: the attribute is present on the wire only when the caller
supplies `Config.ConfigurationVersion > 0`; when unset, `MatterRead(0x0018)` returns `(nil, false)`.
Apple does not request EP0:0x28:0x0018 in any of the 21 pair syslogs. The spec `default: 1` is
a schema-level declaration for clients building provisioned devices, not a mandate for bridges to
emit the attribute when not provisioned. By-design for v1.0; emit when Apple requests it via PICS
test DT.S.A0018.

**L1-D06 — GeneralDiagnostics.BootReason attribute omitted (by-design).**
chip `src/app/clusters/general-diagnostics-server/` serves BootReason (0x0004)
as a mandatory attribute. matter.js `packages/model/src/standard/elements/
general-diagnostics.element.ts:46` lists BootReason as optional (conformance "O").
go-fabric deliberately omits the attribute and surfaces BootReason exclusively
via the §11.12.8.1 BootReason *event* (emitted on Subscribe-Initial via
`EmitBootReason()`), which Apple parses into `estimated start time forward to ...`.
No Apple cache-drop observed for EP0:0x33:0x0004 across 21 pair syslogs — Apple
does not request the attribute when the event is present. The matter.js spec-sample
`examples/device-bridge-onoff` also omits the attribute. Code path:
`cluster/core/general_diagnostics.go:234-235` (`case
gendiagAttrBootReason: return nil, false`). By-design for v1.1; implement if a
future Matter-certification test (PICS DT.S.A0004) requires it.

**L10-D09 — Randomisation window absent in sendIntervalLocked (by-design).**
matter.js `packages/node/src/node/server/ServerSubscription.ts:282` incorporates
`subscriptionRandomizationWindow * Math.random()` in the heartbeat interval. chip
uses the negotiated max directly (no randomisation). go-fabric mirrors chip:
`im/subscription/subscription.go:129-161` computes from
`MaxIntervalCeiling` without randomisation. In Go, each subscription goroutine is
independent so the TS-runtime jitter concern (multiple subscriptions sharing one
event loop) does not apply. No commissioner correctness impact.

**L10-D11 — Shared engine ticker vs. per-subscription timer (by-design).**
matter.js `packages/node/src/node/server/ServerSubscription.ts:191` owns an
independent `Time.getTimer(sendInterval, callback)` per subscription. chip has
per-ReadHandler state machines. go-fabric uses a shared 250 ms engine ticker in
`im/subscription/engine.go:22-36` polling all subscriptions.
Per-subscription goroutines would create O(N_subscriptions) goroutines with no
functional advantage for a bridge with O(10) concurrent subscribers. Granularity
is 250 ms vs. per-subscription precision — practically irrelevant.

**L4-D07 — Lenient timedFlag=false + pending TimedRequest (by-design).**
chip `src/app/WriteHandler.cpp:669-673` rejects with `TimedRequestMismatch` when a
non-timed write arrives on an exchange with a pending TimedRequest. matter.js is
silent on this combination. go-fabric `bridge/receive.go:613-617`
proceeds leniently (clears deadline, continues). Apple's commissioner issues timed
writes correctly for all operations requiring them; no Apple write path triggers
this branch. Matches matter.js semantics. Lenient path is correct for Apple pairing.

**L5-NEW-D02 — Sigma1 Marshal omits initiator-side `initiatorSessionParams` (PFAD-ASYMMETRIE, by-design).**
matter.js `packages/protocol/src/session/case/CaseClient.ts` (Sigma1 initiator)
encodes its MRP params as tag 5 `initiatorSessionParams` so the responder can
adapt its retransmit budget. chip `src/protocols/secure_channel/CASESession.cpp:860`
does the same. go-fabric `secure/sigma/sigma.go:219-228`
(`Sigma1.Marshal()`) emits fields 1-4 only (no field 5). The receive path
(`sigma.go:270-276`) already drains field 5 from inbound Sigma1 correctly.
The send path is dormant: go-fabric always acts as the CASE *responder* in the
v1.0 bridge role — `Sigma1.Marshal()` is exercised only in test round-trips (via
`Initiator.GenerateSigma1()`), never on a real outbound CASE exchange. Becomes
relevant only if an outbound CASE initiator role is added (v1.2 milestone).
PFAD-ASYMMETRIE: bridge-responder use-case only. No wire impact for v1.0.

**L5-NEW-D04 — PaseServer single-active-exchange guard (IMPLEMENTED, noted for completeness).**
This item was closed by implementation rather than by-design. Recorded here for
audit trail completeness. matter.js `packages/protocol/src/session/pase/PaseServer.ts:70-85`
`onNewExchange` checks `this.#pairingMessenger !== undefined` and drops additional
exchanges while one is in-flight; chip `src/protocols/secure_channel/PASESession.cpp:113`
guards the single-session invariant. Fix applied: `bridge/pase_provider.go`
`Resolve()` now returns nil (SC router drops the datagram) when `len(p.entries) > 0`
and the incoming exchangeID is new. Drift L5-NEW-D04.

**L5-NEW-D05 — PASE 60 s timeout hard-enforcement (IMPLEMENTED, noted for completeness).**
This item was closed by implementation rather than by-design. Recorded here for
audit trail completeness. matter.js `packages/protocol/src/session/pase/PaseServer.ts:37`
starts `PASE_PAIRING_TIMEOUT = Seconds(60)` on `handlePairingRequest`. chip uses
per-step timeouts via `mDelegate`. Fix applied: `bridge/pase_provider.go`
`Resolve()` starts `time.AfterFunc(60*time.Second, ...)` per new exchange that calls
`Forget(exchangeID)` on expiry. The existing TTL reaper provides the soft reap;
the per-exchange timer is the spec-mandated hard cap. Drift L5-NEW-D05.

**L1-D02 — OperationalCredentials.Fabrics Apple cache-miss (root cause L4/L10, by-design at L1).**
Audit `2026-05-12_systematic_02/agent1_L0_L1.md` records an Apple MTRDevice
cache-miss for EP0:0x3E:0x0001 (Fabrics). Root cause analysis: the cache-drop
co-occurs with ALL global attributes for cluster 0x3E in the same Apple syslog
burst, confirming the entire cluster's Subscribe-Initial ReportData block was
not delivered or not accepted by Apple's MTRDevice. The L1 code
(`operational_credentials.go` `MatterReadFiltered` for attr 0x0001, fabric-scoped
read via `im.FabricFilterFromContext`) is correct — it correctly enumerates
fabric entries and returns a fabric-scoped slice. This is a L4/L10 delivery gap
(Subscribe-Initial chunking; IM-revision field present-check) rather than a L1
attribute code defect. Marked dependent on L4-D03 (Subscribe-Initial delivery)
and L10-D02 (IM-revision field guard). Will re-test as part of the L4/L10
implementation wave.

**L9-NEW-7 — GroupKeyManagement.GroupTable always empty (by-design, v1.1).**
Matter §11.2.7.5 `GroupTable` (attr 0x0001) lists all multicast group memberships
for the node. go-fabric v1.0 does not implement group multicast — there are no
multicast group entries to report. chip `src/app/clusters/group-key-mgmt-server/
group-key-mgmt-server.cpp` returns the entries from `GroupDataProvider`; in
go-fabric `GroupKeyManagement.MatterRead` returns `[]GroupEntry{}` for attr 0x0001.
Apple Home does not emit an error for an empty GroupTable during commissioning or
normal operation. Implementation deferred to v1.1 when group multicast is added.
No interop impact for the bridge-only v1.0 use-case.

### Open audit hooks

| Audit | Status | Source of truth |
| --- | --- | --- |
| Cluster ID + revision parity | locked via `parity_matterjs_test.go` in 8 packages (cluster/core, cluster/measurement, cluster/wire, cluster/tlv, model/custom/{climate, cover, light, lock, siren, switch}) | `notes/parity/matter/matter-schema-snapshot.json` |
| TLV wire byte parity | locked via `tlv/parity_matterjs_test.go` (23 fixtures) | `notes/parity/matter/tlv-wire-fixtures.json` |
| Behavior layer (BasicInformationBehavior, BridgedDeviceBasicInformationBehavior, OperationalCredentialsServer, etc.) | open — tracked in `notes/parity/matter_behaviour_findings.md` | `../matter.js/packages/node/src/behaviors/` |
| Bridge composition (Aggregator + BridgedNode + per-device-type endpoints) | open — same | `../matter.js/packages/node/src/devices/aggregator.ts`, `bridged-device.ts` |

---

### C-L1-ARL — Access Restriction List (Matter 1.4) — Phase 2 pending

**Status:** Skeleton present, full implementation deferred to Phase 2.

**Matter reference:** Matter Core Specification 1.4 §11.19 (Access Restriction List cluster, cluster ID 0x002B). Introduced alongside the Matter 1.4 Managed Aggregator use-case, which allows a fabric administrator to restrict which subjects can access specific clusters or endpoints on a bridged node.

**Existing skeleton:**
- Constant declaration: `cluster/core/access_restriction.go` — defines `ARLClusterID uint32 = 0x002B` and documents the integration point.
- The cluster is intentionally **not mounted** on any endpoint (see `BD-Matter-ARL-NotMounted` row above). go-fabric does not currently implement the Managed Aggregator use-case.

**What is missing for a full implementation:**

1. **Cluster server struct** — `BridgedARLCluster` implementing `interfaces.MatterClusterServer` with:
   - Attribute 0x0000 `CommissioningARLEntries` (fabric-scoped list).
   - Attribute 0x0001 `ARLEntries` (fabric-scoped list).
   - Event 0x0000 `AccessRestrictionEntryChanged`.
2. **Command handlers** — `CommitRestrictionEntries` (0x01) and `ReviewFabricRestrictions` (0x02), both fabric-scoped.
3. **Fabric-store integration** — persistence of ARL entries per fabric in `internal/store/sqlite/` (new migration + store type), mirroring the ACL store pattern in `cluster/core/access_control.go`.
4. **Root-endpoint mount** — `cmd/openccu-loom/daemon_matter.go::buildRootClusters` must mount the cluster when `cfg.North.Matter.ManagedAggregator` is enabled (opt-in, default off; the config field does not exist yet).
5. **Parity tests** — `cluster/core/access_restriction_test.go` covering attribute read/write, event emission, and fabric-scoped isolation.

**Why deferred:** The Managed Aggregator use-case has no demand in the current device fleet. ARL enforcement is only meaningful once the bridge exposes commissioner-level fabric administration, which is a multi-week effort touching the fabric store, the IM engine, and the commissioning flow. Implementing a partial ARL that does not enforce restrictions would be worse than not implementing it (false compliance signal to the commissioner).

**Re-activation checklist:**
- [ ] Add `cfg.North.Matter.ManagedAggregator bool` to `internal/config/`.
- [ ] Implement `BridgedARLCluster` in `cluster/core/access_restriction.go`.
- [ ] Add SQLite migration for ARL table under `internal/store/sqlite/migrations/`.
- [ ] Mount on Root endpoint behind the `ManagedAggregator` flag in `daemon_matter.go::buildRootClusters`.
- [ ] Add `TestARL_*` parity tests.
- [ ] Update `BD-Matter-ARL-NotMounted` row to reflect the new mounted state.

---

### BD-Matter-P2-D13 — GeneralCommissioning optional Matter 1.5 attributes not exposed

**Affected attributes:** `IsCommissioningWithoutPower` (id 0x0C), TC-feature attrs (ids 0x05–0x09), NetworkRecovery attrs (ids 0x0A, 0x0B). All carry `conformance: "O"` or are feature-gated in matter.js `packages/model/src/standard/elements/general-commissioning.element.ts`.

**Go path:** `cluster/core/general_commissioning.go`.

**Rationale:** These attributes are optional in Matter 1.3 / 1.4 and are only required when the corresponding features (TC: TermsAndConditions; NetworkRecovery) are advertised via FeatureMap. go-fabric does not implement TC or NetworkRecovery — the FeatureMap reports neither feature. Advertising attributes without the corresponding feature bit is a schema violation; adding stubs without feature-bit support would be worse than omitting them. Implementation is gated on feature-demand; no Apple / chip-tool pair-abort risk in the current build.

---

### BD-Matter-P2-D18 — ScenesManagement stub returns empty / rejects writes

**Go path:** `cluster/wire/scenes_management.go`.

**Rationale:** HomeMatic has no scene concept. ScenesManagement (0x0062) is mounted as a mandatory stub on OnOff device types (per Matter device-type conformance). The stub correctly returns `SceneTableSize=0` and rejects AddScene / RemoveScene / StoreScene / RecallScene with `UnsupportedCommand`. This is the same pattern as the matter.js `ScenesManagementBehavior` when no store backend is wired. Full implementation would require a scene store (new SQLite migration) and a HM-side trigger mapping — out of scope for 0.1.0.

---

### BD-Matter-P2-D19 — Groups stub returns NameSupport=0x80 / rejects writes with UnsupportedCommand

**Go path:** `cluster/wire/groups.go`.

**Rationale:** HomeMatic has no group concept. Groups (0x0004) is mounted as a mandatory stub on OnOff device types. The stub returns `NameSupport=0x80` (bit 7 set, GroupNames mandatory per matter.js `groups.element.ts:31`) and rejects AddGroup / RemoveGroup / AddGroupIfIdentifying with IM StatusCode `UnsupportedCommand` (0x81). The 0x81 code is produced by the bridge dispatcher's string-heuristic (`invokeErrorStatus` in `endpoint/dispatcher.go`) when the error message contains "no commands"; `MatterInvoke` includes that sentinel so Apple Home and Google Home receive the correct status. Apple Home, Google Home, and chip-tool all tolerate a Groups stub that rejects commands — this is the same surface that matter.js's default `GroupsBehavior` exposes when no membership provider is wired. Full implementation requires a group-membership store and coordination with the GroupKeyManagement cluster; deferred to a future release.

---

### BD-Matter-P2-D21 — AdministratorCommissioning 48h uncommissioned-window parity test absent

**Go path:** `cluster/wire/admincommissioning.go` — `commissioningWindowMaxSecUncommissioned = 172800`.

**Rationale:** The constant and the `IsUncommissioned` flag are implemented and wired correctly (see `BD-Matter-48h-UncommissionedWindow` in `by_design.md`). A dedicated parity test asserting the 172800-second (48-hour) constant against matter.js's `AdministratorCommissioning.element.ts` is listed as a follow-up item but does not block commissioning correctness — the external chip-tool validation suite passes on this path (62 PASS / 0 FAIL). Adding the test is a hardening step for the next parity-test sweep.

---

### BD-Matter-P2-D24 — `[]any` wire encoder in reply.go covers empty-list only

**Go path:** `bridge/reply.go` — `case []any:` encoder loop.

**Rationale:** The `[]any` case in the TLV struct encoder is used exclusively for the AccessControl Extension attribute, which is always an empty list (`[]any{}`). The loop body never executes a per-element type dispatch because the list is empty. A full type-dispatch encoder (mirroring matter.js's per-element codec) would be required only if the Extension list ever carries entries — which it never does in the current implementation (Extension is served as an empty-list placeholder, conforming to the `EXTS` feature surface without real content). This is a documented intentional scope boundary; the by-design entry `BD-Matter-AccessControl-Extension-Empty` above covers the Extension-empty-list design choice.

---

### BD-Matter-P1-D8 — ColorControl.Options attribute is read-only

**Go path:** `cluster/light/colorcontrol_server.go` — `case wire.ColorCtrlAttrOptions: return uint8(0), true`.

**Rationale:** matter.js `color-control.element.ts` marks Options (0x000F) as access "RW VO" (view-optional write). In practice Apple Home, Google Home, and chip-tool do not write the Options bitmap on a CT-only bridge — they read it once and cache. The attribute is always 0 (no overrides) which is the correct default for a CT-only profile with no scenes. Implementing a write handler would require persisting the bitmap per device and plumbing it into the command-execution gate; deferred to a future release when a use-case arises.

---

### BD-Matter-P1-D9 — WindowCovering.Mode write is validated but not yet persisted or mapped to HM

**Go path:** `internal/model/custom/cover/matter.go` — `validateWindowCoveringMode` gates all three WindowCovering projections' `MatterWrite`.

**Rationale:** matter.js `window-covering-cluster.element.ts:79` marks Mode (0x0017) as access "RW VM", constraint "max 15". The projection now accepts a Mode write and validates the bitmap range (0..15, rejecting >15), so a conformant controller's write no longer fails outright. What remains deferred: the written Mode bitmap is not persisted (a subsequent read still echoes the default 0), and its MotorDirectionReversed / calibration bits are not mapped onto the HM device's MASTER-paramset fields. Real controllers (Apple Home, chip-tool standard suite) do not write Mode in normal operation; full persistence + HM MASTER-paramset mapping is deferred to a future release.

---

### BD-Matter-CASE-StalePeerEviction — same-peer sessions evicted on CASE establishment

**Go path:** `secure/operational/manager.go` — `collectStalePeerSessionsLocked`, called from `OpenFromSigma` / `OpenFromSigmaWithID`.

**Rationale:** matter.js does NOT evict same-peer sessions when a new CASE session is established — `SessionManager.ts:396` `createSecureSession` retains concurrent `(fabric, peerNode)` sessions and reclaims a session id only on exhaustion (`getNextAvailableSessionId` → `findOldestInactiveSession`, `:455-476`). There is no `removeAllSessionsForNode` in matter.js HEAD. go-fabric deliberately diverges: on every CASE establishment it closes any pre-existing session for the same `(fabricIndex, peerNodeID)`. Apple iOS' Matter daemon caches old session keys across daemon restarts and replays them with counter values far above the bridge's in-memory reception window; without eviction the bridge keeps decrypting against the stale entry, fails authentication, and the commissioner aborts the pair attempt with INVALID_PARAMETER. The eviction is locked by dedicated tests (`TestManager_OpenFromSigma_EvictsStalePeerSession` and the DifferentPeer/DifferentFabric keep-existing counter-tests). Reversing it to match matter.js's retain-concurrent behaviour would reintroduce the Apple pairing abort and cannot be validated without live Apple hardware; kept as a deliberate interop divergence.

---

### BD-Matter-CASE-IDExhaustionEvictsPlaceholdersOnly — id exhaustion reclaims reserved ids, never a live session

**Go path:** `secure/operational/manager.go` — `allocateIDLocked` (exhaustion fallback) + `PlaceholderIDTTL`.

**Rationale:** matter.js never reserves a session id ahead of the session — `getNextAvailableSessionId` (`packages/protocol/src/session/SessionManager.ts:490`) runs at the moment `createSecureSession` builds the session, and on a full id space it closes the oldest inactive session (`findOldestInactiveSession`, `:504`) and reuses its id. go-fabric has to announce the id one round-trip earlier (Sigma2's `responderSessionID`, PBKDFParamResponse's `responderSessionID`), so `AllocateID` stakes a placeholder entry that a never-completed handshake would otherwise leak forever. Two divergences follow. (1) Placeholders carry a TTL (`PlaceholderIDTTL`, 20 min — long enough to outlive a 15-minute commissioning window and the 60 s per-exchange CASE adapter, both windows in which the id can still legitimately be claimed) and the idle reaper hands the slot back when it passes; matter.js needs no equivalent. (2) The exhaustion fallback reclaims the OLDEST PLACEHOLDER only and still returns `ErrSessionExhausted` when none exists, where matter.js would close the oldest live session. Closing a live session here has to run the graceful-close cascade (`notifyGracefulClose` → `closeStaleEntries` → `fireOnSessionClose`), which must not run under the manager lock that `allocateIDLocked` is called with; and tearing down a working Apple Home session to admit an unauthenticated Sigma1 is the wrong trade for a bridge. Pinned by `TestAllocateID_EvictsOldestPlaceholderWhenExhausted` and `TestReapIdle_ReclaimsAbandonedPlaceholderIDs`.

---

### BD-Matter-mDNS-InstanceIDReuse — operational InstanceID reused across commissioning-window opens

**Go path:** `cmd/openccu-loom/daemon_matter.go` (InstanceID minted once at boot) + `cmd/openccu-loom/matter_window_adapter.go` (`OpenCommissioningWindow`).

**Rationale:** matter.js mints a fresh random 8-byte instance name on every commissioning-window (re)open (`MdnsAdvertiser.ts` `createInstanceId` per `getAdvertisement`). go-fabric generates the InstanceID once at daemon boot and reuses it for the process lifetime. Rotating the InstanceID per window was tried and empirically broke Apple Home pairing: a service Apple has begun resolving disappears mid-handshake and Apple silently aborts (the same empirical-Apple-testing pattern as the commissionable `DT` device-type choice). Kept stable as a deliberate interop divergence; a stable instance name is spec-legal.

---

### BD-Matter-mDNS-VPAlwaysAdvertised — VendorID/ProductID not masked after extended announcement

**Go path:** `mdns/service.go` — `BuildCommissionableService` always emits the VP TXT key + vendor PTR subtype.

**Rationale:** matter.js `Advertisement.ts:177-191` stops disclosing VendorID/ProductID (VP TXT + vendor PTR) once a commissioning advertisement has run ≥ 15 min (`STANDARD_COMMISSIONING_TIMEOUT`, "extended announcement", Matter §5.4.2.3.1). go-fabric's mDNS layer builds stateless, declarative `Service` records with no notion of elapsed advertisement duration — the record builders take no time input. Implementing the privacy mask requires plumbing "time since window opened" through the advertiser/window-lifecycle layer (duration-aware record construction). Low-severity privacy hardening for a bridge that only advertises during operator-initiated commissioning windows; deferred to a future release rather than half-implemented.

---

### BD-Matter-PASE-EstablishedSessionGate — new PASE refused only while a handshake is in flight

**Go path:** `bridge/securechannel.go` — `claimPaseInFlight` / `releasePaseInFlight` (the single-active-PASE claim, released on Pake3).

**Rationale:** matter.js `PaseServer.ts:80-81` refuses a new PASE exchange while an established, non-closing PASE session exists (`getPaseSession() !== undefined && !isClosing`), not just while a handshake is mid-flight. The bridge's only gate is the in-flight claim, which `releasePaseInFlight` clears on Pake3 — so a second `PBKDFParamRequest` arriving after the first PASE completes (but before CommissioningComplete) is accepted. A faithful, non-regressive fix needs an `operational.Manager` "active non-closing PASE session" predicate plus a bridge-side gate; a bridge-only heuristic (holding the in-flight claim through Pake3) over-blocks a legitimate retry after a failed post-PASE commissioning inside the window and under-blocks a commission running past the 60 s self-expiry — semantically muddy versus matter.js and a commissioning-flow regression risk that cannot be validated without live commissioner hardware. Defense-in-depth only (`AdministratorCommissioning` already returns `Busy` to a second window-open); deferred to a lane that can validate the manager-predicate approach end-to-end.

---

### BD-Matter-PASE-SessionParametersLegacy — PBKDFParamResponse emits only the legacy MRP triplet

**Go path:** `bridge/handlers.go` + `secure/spake2/wire.go` — PBKDFParamResponse tag-5 (`ResponderMRPParams`).

**Rationale:** matter.js `PaseMessages.ts:23-50` serialises the full 1.3+ `TlvSessionParameters` (idle/active as UInt32, activeThreshold, dataModelRevision, interactionModelRevision, specificationVersion, maxPathsPerInvoke) into the PBKDFParamResponse's responderSessionParams, and types the initiator's idle/active intervals as UInt32. go-fabric's PASE wire layer emits only the legacy 3-field SED/MRP triplet and decodes the initiator's idle/active intervals as uint16 (truncating > 65535 ms). The matter.js-faithful target already exists in-tree as `sigma.SessionParameters` (the CASE Sigma2 path emits the full struct); unifying PASE onto it means reshaping the exported `spake2.MRPParameters` struct, which is consumed by `cmd/openccu-loom/daemon_matter.go`. Practically unreachable in the server-responder role — only a commissioner-initiator's InitiatorMRPParams passes through this decode, and commissioners (phone/hub, non-sleepy) advertise sub-65535 ms intervals. Deferred; recommend unifying PASE onto `sigma.SessionParameters` in a future pass.

---

### BD-Matter-P2-D22 — Wire-encoder parity tests for NOCStruct/FabricDescriptor new fields absent

**Go path:** `bridge/reply.go` NOCStruct + FabricDescriptorStruct encoders.

**Rationale:** The NOCStruct Vvsc field (tag 3) and FabricDescriptorStruct VidVerificationStatement field (tag 6) are both implemented and encoded correctly when non-nil. The parity tests that would assert the wire-byte shape of these new fields against matter.js's TLV codec are listed as a follow-up hardening step. The existing `bridge/scenario_tlv_test.go` covers the happy-path for NOCStruct and FabricDescriptor; extending it with VID-verification field cases is the remaining gap.

---

### BD-Matter-P2-D23 — AccessControl EXTS FeatureMap / ExtensionChanged event

**Go path:** `cluster/core/access_control.go` — FeatureMap = 0x1 (EXTS bit), `accessControlEventExtensionChanged` const in `MatterEvents()`.

**Rationale:** The EXTS feature bit is intentionally advertised because the Extension list attribute is served (as an empty list). matter.js `AccessControlServer.with("Extension")` sets the EXTS feature flag whenever the Extension attribute is present; we do the same. The `AccessControlExtensionChanged` event (0x1) is listed in `MatterEvents()` for EventList completeness — the event would be emitted if Extension entries were ever written. Since Extension is always empty, the event never fires in practice. This is not a drift: the feature advertisement is correct (EXTS=present means "Extension attribute is served"), and advertising an event that fires on write is conformance-correct even when writes are rejected.

---

### BD-chip-P2-D-L4-NEW-1 — ListWriteBegin / ListWriteEnd notifications absent

**chip reference:** `src/app/WriteHandler.cpp:259-330` `DeliverListWriteBegin` / `DeliverListWriteEnd`.

**Go path:** `im/write.go`, `endpoint/dispatcher.go`.

**Rationale:** chip notifies cluster servers at the start and end of a list-attribute write so they can apply transactional semantics (all-or-nothing replace). go-fabric's write dispatcher treats every list write as a whole-list replace — which is exactly how Apple Home and all known Matter 1.x controllers write the ACL and other list attributes (full list in a single `WriteRequest`, no `ListIndex` partial-append). The spec's `ListIndex` partial-append path (`chip-tool accesscontrol write-attr acl append`) is a chip-tool-specific test-suite path that no production controller uses. Adding `ListMutationNotifier` is deferred until a use-case that requires partial-list-append arrives.

---

### BD-chip-P1-D-L8-NEW-2 — OnPASEEstablished does not re-arm FailSafe to 60 s

**chip reference:** `src/app/server/CommissioningWindowManager.cpp:209-251` — `kFailSafeTimeoutPostPaseCompletion = 60s`.

**Go path:** `bridge/pase_provider.go`, `bridge/commissioning_window.go`.

**Rationale:** chip re-arms the FailSafe timer to 60 s at PASE session establishment as a defensive net: if a buggy commissioner opens PASE but then never sends an explicit ArmFailSafe, the 60 s cap bounds how long the pending-commissioning state lingers. go-fabric sets the FailSafe duration to the full `CommissioningTimeoutSeconds` at OpenWindow time (typically 180 s); an explicit ArmFailSafe from the commissioner overwrites it. In practice every correct commissioner (Apple Home, chip-tool) sends ArmFailSafe within seconds of PASE success — the 60 s post-PASE cap only matters for buggy commissioners. Apple pair correctness is unaffected by this difference. Adding a 60 s re-arm on OnPASEEstablished is a defensive-coding improvement, deferred to a future release.

---

### BD-chip-P2-D-L9-NEW-1 — AddNOC rollback is layer-specific, not canonical

**chip reference:** `src/app/clusters/operational-credentials-server/OperationalCredentialsCluster.cpp:539-552` — `needRevert` boolean with single rollback block.

**Go path:** `cluster/core/operational_credentials.go::handleAddNOC` (lines 1265–1330).

**Rationale:** chip uses a single `needRevert` flag that triggers a canonical rollback sequence (FabricTable + GroupDataProvider + AccessControl) on any error after the first persistent write. go-fabric's AddNOC path is a sequential store pipeline with per-step rollback that covers the same stores. The code correctly cleans up on failure but does not use a single extracted helper. Refactoring to a canonical `revertAddNOC` helper is a defensive-coding improvement and is deferred; the current behaviour is functionally correct for all tested paths.

---

### BD-chip-P2-D-L10-NEW-1 — ICDConfigurationData-driven MaxInterval rounding absent

**chip reference:** `src/app/ReadHandler.cpp:783-810`.

**Go path:** `im/subscription/manager.go`.

**Rationale:** chip rounds the negotiated MaxInterval to the nearest multiple of `ICDConfigurationData::GetIdleModeDuration()` when `CHIP_CONFIG_ENABLE_ICD_SERVER=1`. go-fabric is a non-ICD bridge (`ICD` mDNS TXT key is absent; `ICDManagement` cluster advertises `IdleModeDuration=1s` as a bridge-always-active signal). The rounding logic only has effect when the bridge is an ICD server — which it is not and will not be in 0.1.0. When / if ICD capability is activated in a future release, this section must be revisited and the `IdleModeDuration`-based rounding added to `subscription/manager.go::clampMaxInterval`.

### BD-Matter-P2-D13-18 — SetVidVerificationStatement returns InvalidCommand

**matter.js reference:** `node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:486-505` — validates all three optional fields and calls `fabric.update` with the VID verification statement.

**Go path:** `cluster/core/operational_credentials.go::handleSetVidVerificationStatement` (line ~1726) — returns `InvalidCommand` unconditionally.

**Rationale:** VID Verification is an optional Matter 1.3+ feature used by device manufacturers for supply-chain attestation. go-fabric is a bridge for Homematic devices whose attestation chain is not managed through Matter VID verification. Returning `InvalidCommand` is spec-conformant per Matter §11.18.6.2 for devices that do not support VID Verification. Re-enable (with real fabric.update integration) if iPhone Multi-Admin commissioning requires VID verification in practice.

---

### BD-Matter-v13-D-AccessControl-Extension-WriteHandler

**matter.js reference:** `AccessControlServer.ts` Extension write-path stores entries and fires `ExtensionChanged` event.

**Go path:** `cluster/core/access_control.go` — `MatterWrite` returns read-only error for the Extension attribute (0x0001); EXTS feature and event remain advertised.

**Rationale:** AccessControl Extension writes come from commercial Matter controllers that enforce policy (enterprise key management, multi-admin policy stores). No known consumer controller used in go-fabric deployments (Apple Home, Google Home, chip-tool in interop tests) writes Extension entries. Implementing the full Extension store (fabric-scoped TLV blobs, conflict resolution, ExtensionChanged fan-out) is deferred until a concrete use-case arrives. The EXTS feature bit and the ExtensionChanged event remain advertised because the spec requires advertising EXTS when the Extension attribute is served — serving an empty list is conformant.

---

### BD-Matter-v13-D-mDNS-FabricChange-ReAnnounce

**chip reference:** `src/platform/Darwin/MdnsError.cpp` + `MdnsAdvertiser.cpp` — event-driven re-announce on fabric change and reconnect.

**matter.js reference:** `packages/mdns/src/MdnsScanner.ts` — `DefaultBroadcastSchedule` with exponential back-off starting at 1 s, max 90 s.

**Go path:** `mdns/zeroconf.go::StartReannounceLoop` + `cmd/openccu-loom/daemon.go` — 30-min fixed cadence; `bridge.EmitFabricAdded/Removed` does not trigger an immediate re-announce.

**Rationale:** The 30-min cadence keeps Apple's `mDNSResponder` cache warm (TTL ≈ 75 min). The `zeroconf.go` trigger channel is wired and functional; coupling it to fabric-add/remove events is a straightforward one-hour wiring task deferred to v1.1. Controllers that query mDNS immediately after fabric add will see the new record at the next periodic announcement (≤ 30 min). No production controller (Apple Home, Google Home, chip-tool) has been observed to suffer from this in testing. See `notes/parity/by_design.md` entry L7-D07 (Matter/matter.js section) for the cadence divergence.

---

### BD-mDNS-SAT-AlwaysEmitted — SAT key always emitted on commissionable record

**matter.js reference:** `packages/protocol/src/mdns/MdnsBroadcaster.ts::buildCommissionableInstanceData` — matter.js emits SAT conditionally in some code paths.

**Go path:** `mdns/service.go::BuildCommissionableService` — always emits SAT.

**Rationale:** Matter §4.3.1.6 lists SAT as a mandatory TXT key on the commissionable `_matterc._udp` record. The parity test `TestParityMdnsServer_CommissionableTXTSchemaLock` enforces this. Always emitting SAT (even at the spec-default 4000 ms) is the correct, spec-conformant behaviour; a conditional gate would violate §4.3.1.6 and break chip-tool and Apple Home commissioning flows that depend on SAT being present. The audit suggestion to gate it as "only if non-default" would constitute a spec regression and is intentionally not applied.

The same reasoning covers the sibling MRP keys **SII and SAI** on both the operational and commissionable records: matter.js `MdnsAdvertisement.ts:183-197` omits each key when its value equals `SessionIntervals.defaults`, but chip — the certified reference stack — emits SII/SAI/SAT whenever a value is configured (it only skips when the MRP config is entirely unset / ICD-LIT), matching go-fabric's always-emit. Because chip (the interop-tested stack) sides with always-emitting-when-configured over matter.js's default-equality optimisation, the always-emit choice is the more conservative, interop-safe one and is applied uniformly to SII, SAI and SAT.

---

### BD-chip-DiagLogs-NoBDX — DiagnosticLogs cluster does not initiate BDX transfers

**chip reference:** `src/app/clusters/diagnostic-logs-server/DiagnosticLogsServer.cpp` — checks `TransferProtocol == BDX` and starts a BDX session.

**Go path:** `cluster/core/diagnostic_logs.go:67-181` — `decodeRetrieveLogsIntent` extracts only the `intent` field; `RequestedProtocol` is not examined. When `BDX` is requested, the handler falls back to inline log delivery with `LogStatusExhausted`.

**Rationale:** BDX (Bulk Data Exchange) is used by enterprise diagnostic tools and certification test suites. No production Matter controller (Apple Home, Google Home, chip-tool interop) requests BDX for bridge diagnostic logs. Implementing BDX upload requires a full BDX initiator session (separate exchange ID, block ack protocol, connection-oriented flow control) which is a non-trivial effort with no concrete deployment need in 0.1.0. The spec permits `LogStatusDenied` for devices that do not support BDX; returning `LogStatusExhausted` with inline content is a graceful fallback that satisfies chip-tool's `62 PASS` baseline. No Apple-reject risk.

---

### BD-chip-ICD-Attrs-0x3-0x5 — ICDManagement attributes 0x0003–0x0005 not implemented

**chip reference:** `src/app/clusters/icd-management-server/ICDManagementServer.cpp` — implements `RegisteredClients` (0x0003), `ICDCounter` (0x0004), `ClientsSupportedPerFabric` (0x0005).

**Go path:** `cluster/core/icd_management.go:34-100` — implements `IdleModeDuration` (0x0000), `ActiveModeDuration` (0x0001), `ActiveModeThreshold` (0x0002) only. Parity test comment marks this as intentional.

**Rationale:** `RegisteredClients` (0x0003), `ICDCounter` (0x0004), and `ClientsSupportedPerFabric` (0x0005) belong to the ICD client-registration feature (`LITS` / `CIP` feature flags). go-fabric is not an ICD server; the bridge is always-active (mains-powered). The three attributes are only meaningful for battery-operated ICD devices that register wake-up clients. Advertising them without ICD-server semantics would mislead controllers into attempting ICD client registration against a non-ICD bridge. No Apple-reject risk — Apple Home does not attempt ICD client registration on bridges.

---

### BD-Matter-InteractionModelRevision — go-fabric emits interactionModelRevision on every IM response

matter.js HEAD commit `47e7f2f78` (`#3751`, 2026-05-17) marks `interactionModelRevision` (tag 0xFF) as `TlvOptionalField` in 10 IM message schemas. go-fabric emits this field on every response (`im/subscribe.go:37-41`, `MatterInteractionModelRevision = 13`).

This is by design: matter.js' own send-path (`TlvDataReportForSend`, `TlvInvokeResponseForSend`) continues to emit the field. Apple Home fails silently when expected fields are absent. Emitting the field unconditionally matches both the prior spec requirement and the actual matter.js wire output; removing it would risk silent Apple Home pairing regressions. No code change is planned.

---

### BD-Matter-WindowCoveringPercentNonNull — deprecated CurrentPositionLiftPercentage projects 0 instead of null

matter.js `window-covering-cluster.element.ts:47-49` marks the deprecated
`CurrentPositionLiftPercentage` (0x0008) attribute `quality "X N"` (nullable)
with `default: null`; go-fabric
(`cluster/cover/windowcovering_server.go`) always returns
a non-null scaled uint8 derived from the canonical
`CurrentPositionLiftPercent100ths` (0x000E).

By design: the bridge always has a concrete position once a HmIP cover reports
LEVEL, so a null projection would only appear in the brief pre-first-report
window. The deprecated 8-bit attribute is retained purely for legacy
controllers; projecting it from the live 100ths value (rather than tracking a
separate nullable lifecycle) keeps the two position attributes consistent and
avoids a transient null that a legacy controller might mishandle. The modern
100ths attribute carries the correct nullability. (Re-audit 2026-05-31, finding
M2-10.)

### BD-Matter-EventBufferSizing — the event buffer follows matter.js' harvesting model at a tenth of its size

matter.js `packages/protocol/src/events/OccurrenceManager.ts:433` sizes the
event store at `minEventAllowance: 10_000` / `maxEventAllowance: 11_000` with
`minPriorityEventAllowance: { info: 2_000, debug: 2_000 }`. go-fabric
(`im/eventlog.go`) ports the model verbatim — one buffer
across all priorities, harvested down to the minimum once it passes the
maximum, non-critical classes floored so a lower class cannot starve a higher
one, and Critical dropped last — but sizes it at 2 000 / 2 200 with floors of
400 (info) and 200 (debug).

By design. matter.js targets a node whose event store is its own dataset;
go-fabric holds a full CCU device model alongside it, and the buffer exists
to answer out-of-band read-event requests rather than to be an event history.
The scaled numbers keep the retention property the model is there for — a
boot-once Critical event stays readable through any realistic volume of
ordinary traffic — at roughly a tenth of the memory. The behaviour under
harvesting is identical; only the point at which harvesting starts differs.

What this replaced is the reason the model was ported at all: three fixed
per-class FIFOs (critical 64, info 32, debug 16). A per-class cap drops a
Critical record while the other classes sit empty, and one CCU interface flap
on a 36-device central produced enough events to evict the BasicInformation
StartUp and GeneralDiagnostics BootReason events that a controller reads
out-of-band right after CASE.

### BD-Matter-FailSafeDisarmOwnership — ArmFailSafe(0) disarm is not gated on the arming fabric

chip `GeneralCommissioningCluster.cpp:420` gates the whole ArmFailSafe body —
including the `ExpiryLengthSeconds==0` disarm branch — on
`!IsFailSafeArmed() || MatchesFabricIndex(accessingFabricIndex)`, so only the
fabric that armed the failsafe may disarm it. go-fabric
(`cluster/core/general_commissioning.go`) lets any CASE
fabric send ArmFailSafe(0).

By design (low impact): a foreign fabric disarming another fabric's failsafe
window mid-commission is already an abnormal flow that cannot be reached in a
normal single-commissioner pairing, and the disarm now runs the full revert
path (re-audit finding F1) so a stray disarm cleans up rather than corrupts.
The ownership guard is a defence-in-depth refinement, not a correctness fix; it
is tracked here rather than implemented to keep the disarm path simple. (Re-audit
2026-05-31, finding F5.)

### BD-Matter-TimedAndQuotaDeferred — per-attribute timed-interaction enforcement and subscribe-quota eviction are unbuilt

Two IM robustness behaviours from chip are intentionally not yet implemented
because the bridged-cluster surface cannot reach them today:

- **Per-attribute/-command `kTimed` enforcement** (chip
  `WriteHandler.cpp:810-812`): an untimed write/invoke targeting a
  timed-required attribute or command must be rejected with
  NEEDS_TIMED_INTERACTION. **Enforced** for commands via the per-path
  predicate `schema.IsTimedInvoke` (`schema/timed.go`),
  consulted by `anyTimedRequiredInvoke` in
  `bridge/receive_dispatch.go` alongside the
  request-level timed flag before `checkTimedGate`
  (`bridge/receive.go`). `timedInvokePaths` currently
  pins AdministratorCommissioning (every command) and DoorLock's
  LockDoor/UnlockDoor/UnboltDoor (Matter §8.7; matter.js
  `door-lock-cluster.element.ts:230,234,560` mark them `"O T"`) — a DoorLock
  unlock routed through the bridge outside a timed window now yields
  NEEDS_TIMED_INTERACTION instead of succeeding. No currently exposed
  *attribute* (as opposed to command) carries the `T` access quality, so
  per-attribute write enforcement remains unbuilt until one does; extend
  `timedInvokePaths` (and `TestTimedInvokeParity`) the next time a newly
  exposed cluster ships a timed command or attribute.
- **Subscribe per-fabric quota eviction** (chip
  `InteractionModelEngine.cpp:1263`): chip evicts an existing subscription to
  guarantee the per-fabric minimum, returning ResourceExhausted only when truly
  out of resources. go-fabric
  (`bridge/subscribe.go`) caps at the default 16
  per-fabric and falls through; with a 1–10 controller bridge fleet the cap is
  effectively unreachable.

Both remain documented unbuilt extension points, not dormant wiring — there is
no implemented-but-unwired capability behind either. Per-command enforcement
became real work once the bridge mounted a cluster with timed-required
commands (DoorLock); per-attribute enforcement and quota eviction become real
work when the bridge exposes a timed-quality attribute or targets large
controller fleets, respectively.
(Re-audit 2026-05-31, findings F3 / F4.)

---

## Removed (built, tested, never wired)

`cluster/doc.go` cites this table. It records code that existed and was
deleted, so a later reader does not re-add it thinking it was an oversight.
The code itself is in Git history.

| Removed | What it was | Why removed / where the live twin is |
|---|---|---|
| `cluster/core/power_source.go` | PowerSource (0x002F) cluster server, core-package variant | Duplicate of the production `measurement.PowerSourceServer` (mounted via the measurement cluster factory). Schema parity for PowerSource is held by the measurement package's parity tests. Removed in the 2026-07-04 wiring audit, while the stack still lived in OpenCCU-Loom. |
