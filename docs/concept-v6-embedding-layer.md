# Konzept v6: Die Einbettungsschicht (Meilenstein 4)

Stand 2026-10-20, go-fabric `v0.9.0`, matter.js-Pin `862ad75f`. Fortsetzung
von [Konzept v5](./history/concept-v5-behaviour-depth.md). Sprache ist
Deutsch, weil es ein Planungsstand ist; ADRs, `by_design`-Einträge und die
Briefs daraus sind Englisch. Die Read-Map dazu ist
`notes/briefs/reads/read-m4-phase-20.md` (Sonnet, 2026-10-20; eine Karte,
keine Zitierung — jede matter.js-Zeile unten habe ich am Pin gelesen).

Meilenstein 3 hat die 25 matter.js-Server mit Logik portiert und bewiesen.
Meilenstein 4 ist der Anspruch der Roadmap: **voraus in Einbettung und
Betrieb, gleich in der Semantik** (`docs/roadmap.md`, Milestone 4, „Value").
matter.js verlangt von einem Host Behaviors, Agents und Transaktionen; dieses
Modul soll einen Gerätetyp und eine Handvoll Callbacks verlangen. Dieses
Dokument plant **Phase 20** (Teil A) und enthält die Entwürfe der beiden
Nachbarkonzepte (Teil B go-ha-matter-bridge, Teil C Adapter-Modul), die laut
Roadmap das M4-Detail mitformen (`docs/roadmap.md:187-189`) und später in
ihre eigenen Repositories umziehen (`:284-285`). Phasen 21 und 22 bekommen
ihre Teile, wenn Phase 20 abgeschlossen ist.

## 0. Entscheidungen des Owners (2026-10-20)

| # | Frage | Entscheidung |
| --- | --- | --- |
| 0.1 | Form des deklarativen Device-Kits | **Generierte typisierte Accessoren** nach ADR 0018: pro Gerätetyp die Cluster-Accessoren, pro Cluster ein typisierter `State`, `Patch`, `OnChanged` und typisierte Command-Handler, aus dem Snapshot generiert; Werte und Handler in einer Deklaration. |
| 0.2 | apidiff-Guard | **`golang.org/x/exp/cmd/apidiff` als Tool-Abhängigkeit** (`tool`-Direktive in `go.mod`), Vergleich gegen das letzte Tag; CLAUDE.md's Abhängigkeitszahl wird mit korrigiert. |
| 0.3 | Nachbarkonzepte | **Als Teil B/C hier**, aus Roadmap und Read-Map; der Owner korrigiert; Phase 22 verschiebt sie in ihre Repositories. |
| 0.4 | Maßstab „looms Importliste schrumpft auf die Facade" | **Produktcode.** Die 32 Pakete, die looms Nicht-Test-Code importiert; Test-Importe (`bridgetest`, `endpointtest`, `im`, `tlv`, …) bleiben erlaubt. |

Weiter gelten: Fable plant und prüft, Opus implementiert, Sonnet liest
(`CLAUDE.md`, „Which model does which work"); ein PR pro Welle; Auto-Merge
nach meinem Review; matter.js bleibt der Goldstandard der Semantik — in
der Einbettung gibt es keinen Goldstandard, dort gilt die Roadmap-Formel
(„ein Gerätetyp und eine Handvoll Callbacks").

## Teil A: Phase 20 — Facade, Device-Kit, apidiff

### A.1 Ausgangslage

- **Die Facade heute.** `node/` exportiert 61 Bezeichner (`node.Config`,
  `New`, `Start`, `Stop`, `Run`, der Aggregator, die Fenster, die Observer;
  `node/config.go`, `node/node.go`), `device/` 567 (generiert, 80
  Dateien, 79 Konstruktoren), `contract/` 49. Die generierten Gerätetypen
  tragen schon 219 Accessoren (ADR 0018, Entscheidung 3), aber nur drei
  Cluster haben einen typisierten Server (`script/devicegen/servers.go:26-30`:
  OnOff, Binding, EcosystemInformation); jeder andere Accessor liefert
  `*spec.Server` mit `Set(attrID uint32, value any)` und
  `Handle(cmdID uint32, fn CommandHandler)` (`cluster/spec/server.go:295`,
  `:404`; `device/on_off_light_gen.go:90-93`).
- **Loom.** OpenCCU-Loom pinnt `v0.6.0` (`openccu-loom/go.mod:36`) und
  importiert 45 go-fabric-Pakete, 32 im Produktcode (Read-Map §1; die
  Roadmap-Zeile 32 sagt 20 und wird korrigiert). Davon unterhalb der Facade:
  `bridge` (Health-Probe, `Random*`, die Fenster-Fehler), `store` (`New`,
  `Store`, `FabricRecord`), `secure/setup`, `secure/spake2`,
  `secure/operational`, `secure/attestation` (`LoadChainFiles`), `mdns`
  (`NewZeroconf`, `NewSubtypeResponder`, `Diagnose`, `IsEnabled`),
  `im/subscription`, `endpoint` (der eigene Assembler, 74 Verwendungen in
  `matteradapter/assembler.go`), `endpoint/sqlitestore`, `groups`,
  `bootid` (`EnableRotation`, `daemon_matter.go:409`), `diagevent`,
  `eligibility`, `schema`, 14 `cluster/*`-Pakete.
- **Vergleichstabelle.** Drei ◐ in „device model"/„runtime"
  (`docs/matterjs-comparison.md:128-130`): das Behaviour-Idiom (bleibt per
  Roadmap), die generierten Cluster-Server (ADR 0013/0017) und Pluggable
  Storage (Phase 21).
- **Register und Findings.** Zwölf Testdateien tragen `until: M4`
  (`behavior/cluster/ClusterBehaviorFeaturesTest.ts`,
  `behavior/internal/MarkChangedTest.ts`,
  `behavior/state/managed/values/ValuePatcherTest.ts`,
  `behavior/state/validation/conformanceTest.ts`,
  `behaviors/access-control/AccessControlCustomExtensionTest.ts`,
  `endpoint/EndpointNumberConflictTest.ts`,
  `endpoint/server/ProtocolServiceTest.ts`,
  `endpoint/type/SemanticNamespaceTest.ts`,
  `endpoint/validation/{ConstructionValidationTest,RuntimeValidationTest,StandardDeviceTypeCorpusTest}.ts`,
  `endpoints/LargeBridgeTest.ts`); 16 Findings-Einträge sind mit
  „Milestone 4" markiert (Data-Versions beim Start, die ACL-Anwendung in
  einem Schritt, der Extension-Access-Hook, das Basic-Fenster, die
  Feature-Wahl von OccupancySensing und Groupcast, Thermostat-Schedules,
  die Konzentrations-Level-Quelle, Session-Intervalle, der nichtflüchtige
  Event-Store, TagList, die monotone Uhr, die Occupancy-Kopplung, der
  Switch auf der Systemuhr, der TriggerEffect-Hook, ColorControl-Primaries).

### A.2 Die Facade vollständig (20.1)

Ziel nach 0.4: looms Produktcode importiert `node`, `device`, `contract`,
`schema` (das eingebettete Modell), `eligibility` (Ökosystem-Wissen für den
Host, kein Protokoll) und die `cluster/*`-Pakete der Server, die er
anpasst — nichts aus `bridge`, `store`, `secure/*`, `mdns`, `im`,
`endpoint`, `groups`, `bootid`. Jedes heute benutzte Symbol bekommt einen
Weg oder eine Dokumentation als **advanced seam** (eine neue Seite
`docs/guides/advanced-seams.md`: was unterhalb der Facade öffentlich bleibt,
wofür, und was es kostet).

| Loom-Symbol (Read-Map §1) | Weg in Phase 20 |
| --- | --- |
| `bridge.StartHealthProbe`, `HealthSample`, `DefaultProbeInterval` | `node.Config.Health{Interval, OnSample}`; `Node.Health()` liefert die letzte Probe. |
| `bridge.RandomPasscode`, `RandomSalt`, `RandomDiscriminator` | `node.NewCommissioningCredentials()` (Passcode nach den Regeln von `secure/setup`, Salt, Diskriminator) — ein Aufruf, kein `secure/*`-Import. |
| `bridge.ErrCommissioningWindow*`, `ErrAdministratorWindowOpen` | die Fehler in `node` (einer existiert: `node/commissioning.go:213`); loom vergleicht mit `errors.Is`. |
| `bridge.Snapshotter`, `bridge.TCPConfig` (Felder von `node.Config`) | Typ-Aliase `node.Snapshotter`, `node.TCPConfig`; der Host importiert `bridge` nicht mehr für einen Feldtyp. |
| `store.New`, `store.Store`, `store.FabricRecord` (`handlers/matter.go:231`) | `node.Config.Store.Path` öffnet den SQLite-Store; `Node.Fabrics()` liefert `[]node.FabricInfo` mit den Feldern, die loom aus `FabricRecord` liest (Rückgabetyp-Änderung: `v0.10.0`, apidiff meldet sie, Fenster nach README). `store` bleibt advanced seam für einen Host mit eigenem Store (Phase 21). |
| `secure/setup.IsValidSetupPIN`, `MaxDiscriminator`, `Payload`/`QRCode`/`ManualCode`, `DiscoveryOnIP` | `node.Config.Validate()` prüft Passcode und Diskriminator (existiert, `node/config.go:388`); `Node.PairingCodes()` liefert QR und manuellen Code (existiert, `node/node.go:789`). Die Konstanten `node.MaxDiscriminator`, `node.ValidateSetupPIN` für eine Konfigurationsprüfung vor `New`. |
| `secure/spake2.IterationsMin/Max`, `PBKDFMin/MaxSaltSize` | `node.Config.Validate()`; die Grenzen als `node.PBKDF…`-Konstanten. |
| `secure/operational.Manager`, `im/subscription.Manager` (Typen der Accessoren `Sessions()`, `Subscriptions()`) | Interfaces `node.Sessions`, `node.Subscriptions` mit dem, was loom liest (Zähler, Liste); die konkreten Manager bleiben advanced seam über `Node.Bridge()`. |
| `secure/attestation.LoadChainFiles`, `ChainFiles`, `ErrNoChainConfigured` | `node.Attestation{Files: ChainFiles}` — die Facade lädt; `Config.Attestation.Chain` bleibt für einen Host mit eigener Kette. |
| `mdns.NewZeroconf`, `NewSubtypeResponder`, `NewNoop`, `IsEnabled`, `Diagnose` | `node.Config.Advertiser == nil` baut Zeroconf mit Subtype-Responder (heute dokumentiert `config.go:51-53` nur Noop); `node.Config.Network.MDNS{Disabled}`; `Node.DiagnoseMDNS()`. `mdns` bleibt advanced seam für einen eigenen Advertiser. |
| `endpoint.*` (Assembler, Spec, Topology, SourceKey, Record, Store, ErrNotFound, ComposeNodeLabel, ScenesStore), `endpoint/sqlitestore` | Das Device-Kit (A.3) ersetzt looms eigenen Assembler; `device.Options.StableKey` und `Reachable` bekommen Aliase (`device.SourceKey`, `device.AvailabilityProbe`); Endpoint-Persistenz ist `node.NewEndpointPersistence` (existiert, `node/persist.go:81`). `endpoint` bleibt advanced seam („die Matter-nahe Ebene", ADR 0018) für die Migration; looms Umstieg ist looms Arbeit (A.6). |
| `groups.NewManager` (`node.Config.Groups`) | `nil` baut den Manager (Prüfung in der Welle: ob `node.New` das schon tut); das Feld bleibt advanced seam. |
| `bootid.EnableRotation` | `node.Config.BootID.Rotate bool` (Semantik in der Welle lesen: `bootid/`). |
| `diagevent.Event` (REST-Handler) | `node.DiagnosticEvent` als Alias; der Ring selbst ist Phase 21 („diagnostic event ring"). |
| `cluster/core.{BasicInformation, OperationalCredentials, RegulatoryIndoorOutdoor, SoftwareVersionFromString}` | `node.RootClusters` existiert (`node/root.go:58`); `node.Config.Commissioning.Regulatory…` und `node.SoftwareVersionFromString`. |
| `schema.*`, `eligibility.*`, `cluster/<server>` | erlaubt; dokumentiert als Teil der typisierten Oberfläche (A.3). |

Was die Facade nicht wird: ein zweiter Zugang zu `bridge`'s
`Attach…`/`Set…`-Ports. Wer die braucht, nimmt `Node.Bridge()` und steht
in der Seams-Seite.

### A.3 Das deklarative Device-Kit (20.2)

**matter.js' Form.** Ein Gerätetyp ist eine `MutableEndpoint`-Definition
mit Pflicht- und Wahl-Behaviors (`packages/node/src/devices/on-off-light.ts:103-135`;
Aggregator `endpoints/aggregator.ts:116-134`, BridgedNode
`endpoints/bridged-node.ts:118-148`). Der Host setzt Zustand über
`endpoint.set({onOff: {onOff: true}})`, das in einer Transaktion schreibt
(`endpoint/Endpoint.ts:256-262`, `act` `:864-880`), beobachtet ihn über
`events.<cluster>.<attribut>$Changed` (`behavior/state/managed/Datasource.ts:202`;
`Behavior.reactTo` `Behavior.ts:223-230`), überschreibt Commands als
Methoden einer abgeleiteten Behavior-Klasse, und Übergänge laufen in
`Transitions` (`behavior/Transitions.ts:64-80`, `start` `:120`). Der
Validator prüft den Node-Scope in einem Durchgang und weist mit den
Verstößen ab (`node/server/DeviceTypeConformanceService.ts:80-95`).

**Die Go-Form nach 0.1.** Vier Bausteine, in dieser Reihenfolge:

1. **Typisierte Cluster-Schicht, generiert.** `script/clustergen` erzeugt
   pro Cluster-Definition unter `cluster/spec/<cluster>/` zusätzlich:
   `State` (ein Feld je Attribut, nullable als Pointer, Listen als Slices;
   die Typen existieren schon als Enums/Bitmaps/Structs mit TLV-Codecs,
   ADR 0013), `Patch` (jedes Feld Pointer: „setzen oder lassen", wie
   `SupportedBehaviors.StatePatchOf` in `Endpoint.set`), und einen
   typisierten Server `<cluster>def.Server`, der `*spec.Server` einbettet
   und ergänzt: `Set(ctx, Patch) error`, `Get() State`,
   `OnChanged(func(State, changed []uint32)) (unsubscribe func())`
   (über `contract.AttributeChangeNotifier`, wie `device.Device.OnChanged`
   heute, `device/device.go:657`), und je Command
   `Handle<Command>(func(ctx, <Command>Request) (<Command>Response, error))`
   über `spec.Server.Handle`. Die Request-/Response-Structs existieren
   (`cluster/spec/<cluster>`). Für die selbstgehaltenen Server (`onoff`,
   `levelcontrol`, `light`, `thermo`, `lock`, …) erzeugt der Generator
   dieselbe typisierte Hülle über deren Server; die Server-Tabelle von
   `devicegen` (`servers.go:26-30`) wird dadurch überflüssig: jeder
   Accessor liefert `*<cluster>def.Server`.
2. **Eine Deklaration.** Pro Cluster eine Option
   `<cluster>def.Declare{Initial Patch, Features, Handlers …}`, die
   `device.New<Typ>` entgegennimmt; `WithInitial`, `WithFeatures`,
   `WithElements`, `WithServer` bleiben (README-Fenster) und werden zu
   Sonderfällen der Deklaration. Beispiel, so wie der Daemon es schreibt:

   ```go
   lamp := device.NewDimmableLight(device.Options{StableKey: key, Name: name},
       onoffdef.Declare{Initial: onoffdef.Patch{OnOff: ptr(false)},
           OnSet: func(ctx context.Context, on bool) error { return relay.Switch(on) }},
       levelcontroldef.Declare{Transitions: true})
   agg.Add(ctx, lamp)
   lamp.OnOff().OnChanged(func(s onoffdef.State, _ []uint32) { log(s.OnOff) })
   lamp.LevelControl().Set(ctx, levelcontroldef.Patch{CurrentLevel: ptr(uint8(128))})
   ```

   `OnSet` ist die Go-Form des Vetos vor dem Commit (`$Changing`,
   `Datasource.ts:1172-1195` laut Read-Map; die Zeile wird in der Welle
   gelesen): bei den selbstgehaltenen Servern ist das heute der `Sink`
   (`cluster/onoff/server.go:71`), bei `spec.Server` ein neuer
   `BeforeSet`-Hook; ein Fehler antwortet FAILURE und speichert nichts
   (wie `docs/getting-started/device.md`, „Two directions of state").
3. **Übergänge und Kopplungen.** `cluster/transition` ist der Port von
   `Transitions.ts` (ADR 0014); die Deklaration schaltet sie pro
   Endpunkt (`Transitions: true`), neu ist nur die Verdrahtung. Die
   Findings „Occupancy-Kopplung", „TriggerEffect-Hook" und „TagList"
   sind Deklarationsfelder (`Occupancy: sensor`, `OnTriggerEffect`,
   `TagList`), keine eigenen APIs.
4. **Validator und Fehler.** `Aggregator.Add` prüft schon
   (`endpoint.ValidateDeviceTypes`, ADR 0016); das Kit gibt den Verstoß
   als `device.ConformanceError` zurück (Alias des endpoint-Typs), damit
   ein Host `endpoint` nicht importiert. `fabrictest` ist der Testhelfer
   des Hosts (Konzept v4), `endpointtest.AssertDeviceTypeConformance`
   bleibt.

**Beweis.** `examples/reference-bridge` (13 647 Zeilen, `fleet_*.go`)
wird auf der Deklaration neu geschrieben und importiert danach nur
`node`, `device`, `contract`, `schema` und die `<cluster>def`-Pakete;
die chip-tool-Familien und `TestChipCertification…` entscheiden den
Umbau (wie ADR 0018, Entscheidung 6). Die zwölf `until: M4`-Dateien
werden portiert, soweit ihr Gegenstand existiert: die State-Dateien
(`ValuePatcherTest`, `MarkChangedTest`, `conformanceTest`,
`ClusterBehaviorFeaturesTest`) gegen die typisierte Schicht, die
Endpoint-Dateien (`EndpointNumberConflictTest`, `ConstructionValidationTest`,
`RuntimeValidationTest`, `StandardDeviceTypeCorpusTest`, `LargeBridgeTest`,
`SemanticNamespaceTest`) gegen `node.Aggregator` und den Validator;
`ProtocolServiceTest` und `AccessControlCustomExtensionTest` behalten
`until` (Phase 21: Session-Limits; der Extension-Hook ist ein
Sicherheitspfad und mein Lesen).

### A.4 Der apidiff-Guard (20.3)

- `go.mod`: `tool golang.org/x/exp/cmd/apidiff` (Go 1.24+; das Modul ist
  bei `go 1.27.2`). BSD-3 (Go-Projekt), Eintrag in
  `THIRD-PARTY-NOTICES.md`. `CLAUDE.md` sagt „five direct dependencies",
  `go.mod:5-10` hat sechs (`golang.org/x/tools` für den
  Reachability-Guard): der Satz wird zur Regel „jede weitere Modul- oder
  Tool-Abhängigkeit ist eine Entscheidung (Lizenz, Notices)", mit der
  aktuellen Zahl.
- `make apidiff`: ein temporärer Worktree des letzten Tags
  (`git describe --tags --abbrev=0`), `go tool apidiff -w` dort und hier
  für jedes öffentliche Paket (alles außer `internal/`, `examples/`,
  `script/`, `conformance/` — README §„What public API means"), dann der
  Vergleich. Kein kommittiertes Artefakt.
- Die Regel des Guards folgt README §Pre-1.0 (`README.md:178-200`): eine
  inkompatible Änderung ist erlaubt, wenn `CHANGELOG.md` unter
  `[Unreleased]` den Bezeichner unter `### Changed`, `### Removed` oder
  `### Deprecated` nennt (oder der Eintrag eine Paritätskorrektur oder
  einen Sicherheitsfix als solchen benennt); sonst schlägt der Job fehl
  und druckt die Liste. Generierte Pakete (`schema/`, `cluster/spec/*`,
  `device/`) laufen mit: eine Snapshot-Regeneration, die ein exportiertes
  Element entfernt, ist genau die Paritätskorrektur, die angekündigt
  werden muss.
- CI: ein Job `apidiff` in `ci.yml`; der Owner nimmt ihn in die
  Pflicht-Checks auf (die Namen der Pflicht-Checks sind eine harte Regel).
  Der Job läuft nicht auf einem Release-Branch gegen sein eigenes Tag.

### A.5 Vergleichstabelle und Roadmap

Nach Phase 20: Zeile 129 (generierte Cluster-Server) wird ✅ — die
typisierte Schicht ist die Gegenform zu matter.js' Modell-generierten
Cluster-Typen; Zeile 130 (Storage) bleibt ◐ bis Phase 21; Zeile 128 bleibt
per Roadmap. Die Roadmap-Zeile 32 (loom „20 Pakete") wird zu „32 im
Produktcode (`v0.6.0`), Ziel nach Phase 20: die Facade plus die
Cluster-Pakete".

### A.6 Was Phase 20 nicht ist

- Looms Umstieg auf `v0.9.x`/`v0.10.0` und auf das Kit ist looms Arbeit
  (Repository openccu-loom, eigene Wellen); Phase 20 liefert die Form und
  beweist sie am Daemon. Die Messung (0.4) erfolgt nach looms Umstieg.
- Kein zweiter Store, keine Metriken, keine Limits (Phase 21); kein
  Container, kein Cookbook (Phase 22).
- Keine Änderung an `contract/` über Aliase und additive Interfaces
  hinaus; stdlib-only bleibt.

## Teil B: go-ha-matter-bridge (Entwurf)

Arbeitsname; „not started; its concept is written when milestone 4 is due"
(`docs/roadmap.md:33`). Dieser Entwurf zieht mit Phase 22 in das eigene
Repository um; hier steht, was Phase 20 für ihn festlegt.

- **Zweck.** Home-Assistant-Entitäten als Matter-Bridge für Apple Home,
  Google Home und Alexa — die Rolle, die
  [home-assistant-matter-bridge](https://github.com/Nabu-Casa/home-assistant-matter-bridge)
  auf matter.js spielt, als ein statisches Go-Binary auf diesem Modul.
- **Datenquelle.** Die HA-WebSocket-API (Entity-Registry, Geräte-Registry,
  `state_changed`, `call_service`); eine Entität ist eine
  Gerätedeklaration des Kits (A.3): Domäne und Attribute → Gerätetyp und
  Cluster-Patch, `call_service` → Command-Handler.
- **Die Abbildung** HA-Vokabular → Matter-Gerätetyp liegt nicht in der
  Bridge, sondern im Adapter-Modul (Teil C), damit die `go-*2mqtt`-Familie
  dieselbe benutzt.
- **Tests** auf `fabrictest` (In-Process-Controller, Konzept v4): die
  Bridge wird kommissioniert, eine HA-Entität erscheint als Endpunkt, ein
  Controller-Invoke wird zum `call_service`.
- **Was Phase 20 festlegt:** die Deklarationsform (A.3), der
  Facade-Umfang (A.2: keine Imports unterhalb von `node`), die
  Kit-Fehler (ConformanceError), der apidiff-Guard als Versprechen an
  einen Konsumenten außerhalb dieses Repos.

## Teil C: Das Adapter-Modul `go-hamatter` (Entwurf)

Arbeitsname (`docs/roadmap.md:34`): `go-hamqtt`'s Home-Assistant-Vokabular
→ Matter-Gerätetypen; eigenes Modul, damit `contract/` stdlib-only bleibt.
Zieht mit Phase 22 um.

- **Zweck.** Eine Tabelle und ihre Laufzeitform: HA-Komponente (`light`,
  `switch`, `cover`, `climate`, `lock`, `fan`, `sensor`, `binary_sensor`,
  `number`, `select`, …) mit ihren Attributen und Fähigkeiten
  (`supported_color_modes`, `hvac_modes`, `device_class`) → Gerätetyp des
  Kits mit Cluster-Deklaration (A.3), und zurück: Matter-Command →
  HA-Service oder MQTT-Kommando.
- **Konsumenten.** Die `go-*2mqtt`-Daemons (daikin, homeconnect, mtec,
  unifi, zendure) sprechen HA-MQTT-Discovery; mit dem Adapter wird jeder
  mit demselben Vokabular eine Matter-Bridge (Roadmap: „a 2mqtt daemon
  exposes one device type end to end on the kit", Phase 22). Die
  HA-Bridge (Teil B) ist der zweite Konsument.
- **Grenze.** Der Adapter importiert `node`, `device`, `contract`, `schema`,
  `eligibility` und die `<cluster>def`-Pakete — genau die Liste aus A.2;
  er ist damit der erste Konsument, an dem die Messung 0.4 ohne
  Altlast gilt.
- **Eligibility.** Welche Gerätetypen ein Ökosystem annimmt, ist
  `eligibility`'s Wissen (`Classify`, `EcosystemForVendor`); der Adapter
  wählt den Gerätetyp danach, nicht die Bridge.

## Teil D: Wellen

| Welle | Brief | Inhalt | Pfade (disjunkt) | Version |
| --- | --- | --- | --- | --- |
| 20a | BS (Opus) | Facade vollständig (A.2): die Wege der Tabelle, Aliase, `docs/guides/advanced-seams.md`, Roadmap-Zeile 32 | `node/`, `docs/guides/`, `docs/getting-started/device.md` | `v0.9.1` |
| 20b | BT (Opus; Sonnet regeneriert) | Typisierte Cluster-Schicht (A.3.1): `clustergen` erzeugt `State`/`Patch`/`<cluster>def.Server`, `spec.Server.BeforeSet`, die Accessoren von `devicegen` auf die typisierte Form | `script/clustergen`, `cluster/spec/*`, `script/devicegen`, `device/*_gen.go` | `v0.9.2` |
| 20c | BU (Opus) | Deklaration (A.3.2–4), Daemon-Umbau, die Geräte-Seite der Docs; chip-tool-Familien lokal | `device/`, `examples/reference-bridge`, `docs/getting-started/` | `v0.9.3` |
| 20d | BV (Opus) | apidiff-Guard (A.4), `THIRD-PARTY-NOTICES.md`, `CLAUDE.md`, `ci.yml`; die Ports der `until: M4`-Dateien gegen die typisierte Schicht und den Aggregator | `script/apidiff` oder Makefile, `.github/workflows/ci.yml`, `node/*_parity_matterjs_test.go`, `device/*_parity_matterjs_test.go` | `v0.9.4` |

20a und 20b sind disjunkt und laufen parallel (die Harness-Regel: eine
Welle mit chip-tool zur Zeit — 20c). 20c braucht 20b; 20d braucht 20c
(der Guard misst den Umbau). Konfliktflächen: `CHANGELOG.md`,
`script/testparity/register.json`, `script/reachability/`,
`script/coverfloor/floors.go`; löse ich beim Merge. `v0.10.0` schließt
Meilenstein 4 mit Phase 22.

## Teil E: Was sich an Grenzen ändert

- **Öffentliche API.** Die Accessoren der generierten Gerätetypen ändern
  ihren Rückgabetyp (`*spec.Server` → `*<cluster>def.Server`, der
  `*spec.Server` einbettet: Methodenaufrufe bleiben, Variablendeklarationen
  brechen); `Node.Fabrics()` liefert `[]node.FabricInfo`. Beides als
  `### Changed` angekündigt, ab 20b/20a wirksam, Fenster nach README;
  ab 20d meldet apidiff jede weitere.
- **`contract/`** bleibt stdlib-only; neue Interfaces sind additiv
  (`BeforeSet` als optionales Interface des Servers, nicht des Contracts).
- **`go.mod`** bekommt die Tool-Direktive; die Abhängigkeitsregel in
  `CLAUDE.md` wird korrigiert.
- **Pflicht-Checks** wachsen um `apidiff` (Owner).

## Teil F: Risiken

- **Generatorumfang.** Die typisierte Schicht für über hundert
  Cluster-Definitionen ist Generatorarbeit mit einem Test, der jede
  Definition kompiliert (wie `script/clustergen`'s heute); Handarbeit nur
  in `spec.Server` und `device/`. Eine Definition, deren Typen die
  Schicht nicht trägt (Listen von Structs mit fabric-scoped Feldern), wird
  in der Welle benannt, nicht umgangen.
- **Zwei Formen zugleich.** `WithInitial`/`WithServer` und die Deklaration
  koexistieren im Fenster; der Daemon zeigt nur die Deklaration, die
  Docs nennen beide einmal.
- **apidiff und Regeneration.** Jede Snapshot-Regeneration kann
  exportierte Konstanten bewegen; der Guard verlangt dann den
  Paritäts-Eintrag — gewollt, aber eine neue Pflicht bei
  `make generate-matter-schema`.
- **Loom-Messung.** Die Zahl 32 gilt für `v0.6.0`; looms Umstieg ist
  nicht Teil dieses Repos. Die Messung 0.4 ist deshalb ein Ziel von
  Meilenstein 4, nicht ein „Done when" von Phase 20 allein.
- **Sicherheitspfade.** `AccessControlCustomExtensionTest.ts` und die
  ACL-Anwendung in einem Schritt bleiben mein Lesen (Phase 21 oder später).
