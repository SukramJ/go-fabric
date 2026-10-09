# Konzept: Matter 1.6.1 vollständig, und die Geräteschicht `device/`

Stand 2026-10-08, go-fabric `v0.2.0`. Ergebnis des Planungs-Wizards vom
selben Tag. Sprache dieses Dokuments ist Deutsch, weil es ein Planungsstand
ist; die ADRs, die daraus hervorgehen, sind wie der Rest des Repos Englisch.

Dieses Dokument hat drei Teile:

- **Teil A** definiert, was „Matter 1.6.1 vollständig“ für go-fabric heißt,
  misst die Lücke gegen den heutigen Stand und ordnet sie in Phasen.
- **Teil B** ist das Konzept der standardisierten Bridge-API: das Paket
  `device/` in go-fabric, der Port von matter.js `packages/node/src/devices`
  und `packages/node/src/endpoints`, mit einem Node-Fassadenpaket.
- **Teil C** beschreibt den einen neuen Daemon außerhalb von go-fabric, der
  HA- und zigbee2mqtt-Quellen auf `device/` projiziert, und was in
  openccu-loom passiert.

Jede Zahl trägt ihre Quelle. Was aus dem Schema oder dem Repo gemessen wurde,
steht als gemessen; was eingeschätzt ist, steht als Einschätzung.

---

## 0. Entscheidungen aus dem Wizard

| Frage | Entscheidung |
| --- | --- |
| Cluster-Scope | Alle Home-Automation-Cluster: Energie, Appliances, Labels, Lokalisierung, Diagnostik, Sensorik. Ohne Kamera, WebRTC, TLS, Media/Content, Joint Fabric, Thread/Netzwerk-Infrastruktur. |
| Transport | TCP und BDX aufnehmen. |
| Scope-Grenzen | Bleiben: keine Controller-Rolle, kein BLE, kein Thread. |
| OTA-Requestor, Binding, ICD | Keine davon; bleiben klassifiziert, nicht montiert. |
| Konsumenten der API | Go-Hosts in-process (openccu-loom, go-*2mqtt) und Home Assistant direkt über die WebSocket-API. |
| Abstraktionsebene | Semantisches Gerätemodell; Projektion auf Gerätetyp, Cluster, Feature aus dem Schema. |
| Ort der Geräteschicht | In go-fabric als Paket `device/`. Kein neues Projekt. |
| Ort der Quell-Adapter | Genau ein neues Repo für alle Adapter und den Daemon (Arbeitstitel `go-matter-bridge`). |
| HA-Direktanbindung | Form später entscheiden; das Konzept nennt die Option. |
| Konfiguration | Code-first mit Go-Typen; deklarative Datei optional im Daemon. |
| Reihenfolge | Vollständigkeit zuerst, Bridge-API danach. |
| Echte Controller | Apple Home und Amazon Alexa stehen zum Testen zur Verfügung. |

Eine Spannung aus diesen Entscheidungen muss benannt werden: **TCP und BDX
ohne OTA-Requestor** haben genau einen Konsumenten, den
DiagnosticLogs-Transfer (`BD-chip-DiagLogs-NoBDX`). Das ist ein legitimer
Grund, weil DiagnosticLogs auf dem RootNode optional, aber von Testlaboren
regelmäßig geprüft wird. Der zweite Konsument, OTA, ist abgewählt. Teil A
ordnet TCP+BDX deshalb als letzte Protokollphase ein, nicht als erste. Wenn
sich das Verhältnis ändert, ist das die Stelle, an der die Reihenfolge kippt.

---

## Teil A: Was „Matter 1.6.1 vollständig“ heißt

### A.1 Definition

go-fabric ist vollständig für Matter 1.6.1, wenn alle vier Sätze gelten:

1. **Protokoll.** Jede Responder-Fähigkeit, die matter.js's `ServerNode`
   hat und die nicht durch eine Scope-Grenze ausgeschlossen ist, ist
   portiert: IM-Rest (Timed-Write pro Attribut, Subscription-Quota),
   Commissioning-Rest (node-eigenes Fenster), Transport (TCP, BDX),
   Sitzungsparameter auf dem PASE-Pfad.
2. **Cluster.** Jeder Cluster, den ein Gerätetyp im Scope als Server verlangt
   oder erlaubt, hat einen Server in `cluster/`.
3. **Gerätetypen.** Jeder Gerätetyp im Scope ist über `device/` montierbar,
   besteht `endpoint.ValidateDeviceTypes` (ADR 0016) und ist im
   Referenz-Daemon mindestens einmal vertreten, damit die CSA-Familien ihn
   erreichen.
4. **Nachweis.** Für jede CSA-Familie im Scope ist jeder Fall entweder
   bestanden oder eine Klasse-(c)-Lücke mit zitiertem Beleg. Klasse (b)
   „nicht unterstützt“ bleibt nur für Fälle, deren Cluster oder Feature
   außerhalb des Scopes liegt. Zusätzlich: jede Release-Kandidatin wurde
   gegen Apple Home und Alexa gepaart, mit Protokoll in
   `docs/matter-ecosystem-observations.md`.

Nicht Teil der Definition, weil Scope-Entscheidung: Controller-Rolle, BLE,
Thread, OTA-Requestor und -Provider, Binding-Client, ICD, Kamera, WebRTC,
TLS, Media/Content, Joint Fabric, Netzwerk-Infrastruktur. Anhang F listet
jeden ausgeschlossenen Punkt mit Grund und Quelle. Diese Cluster
bleiben als Schema vorhanden und generierbar (`script/clustergen` erzeugt
jeden Cluster des Snapshots), sie bekommen nur keinen Server mit Regeln.

### A.2 Gemessene Lücke

**Methode.** Die bedienten Cluster-IDs wurden aus den
`MatterClusterID()`-Implementierungen und den Konstanten unter `cluster/`
gelesen; die Gerätetyp-Anforderungen aus `parity/schema.json`
(`deviceTypes[].requirements`, Element `serverCluster`). Der Vergleich lief
als Skript über den Snapshot; die Ergebnisse unten sind dessen Ausgabe.

| Messgröße | Wert | Quelle |
| --- | --- | --- |
| Cluster im Snapshot | 135 | `parity/schema.json` |
| Gerätetypen im Snapshot | 91 | `parity/schema.json` |
| Cluster mit Server in go-fabric | 55 | `cluster/` (39 per `MatterClusterID`, 16 über `cluster/spec`-Instanzen und `cluster/measurement`) |
| Gerätetypen im Scope, deren Pflichtcluster alle vorhanden sind | 60 von 72 | Skript |
| Pflichtcluster im Scope ohne Server | 11 | Skript, Liste unten |
| Optionalcluster im Scope ohne Server | 35 | Skript, Liste unten |
| matter.js-Behaviours mit eigener Serverlogik über 40 Zeilen | 48 | `wc -l` über `behaviors/*/*Server.ts` |

**Pflichtcluster im Scope ohne Server (11):**

| Cluster | Gerätetyp, der ihn verlangt | matter.js-Logik |
| --- | --- | --- |
| DeviceEnergyManagement | DeviceEnergyManagement (0x050D) | nur generiert |
| EnergyEvse, EnergyEvseMode | EnergyEvse (0x050C) | Mode: ModeBase-Regeln (69 Zeilen) |
| WaterHeaterManagement, WaterHeaterMode | WaterHeater (0x050F) | Mode: ModeBase-Regeln (70 Zeilen) |
| MicrowaveOvenControl, MicrowaveOvenMode | MicrowaveOven (0x0079) | Mode: 48 Zeilen |
| TemperatureControl | TemperatureControlledCabinet (0x0071) | nur generiert |
| SoilMeasurement | SoilSensor (0x0045) | nur generiert |
| MeterIdentification | ElectricalUtilityMeter (0x0511) | nur generiert |
| ClosureDimension | ClosurePanel (0x0231) | nur generiert |

**Optionalcluster im Scope ohne Server (35), nach Gruppe:**

| Gruppe | Cluster |
| --- | --- |
| Sensorik | CarbonMonoxide-, NitrogenDioxide-, Ozone-, Formaldehyde-, Pm1-, TotalVolatileOrganicCompounds-, RadonConcentrationMeasurement; BooleanStateConfiguration |
| Appliances | LaundryWasherControls, LaundryDryerControls, DishwasherAlarm, RefrigeratorAlarm, RefrigeratorAndTemperatureControlledCabinetMode, OvenMode, OvenCavityOperationalState, TemperatureAlarm, ServiceArea |
| Energie | DeviceEnergyManagementMode, EnergyPreference, CommodityPrice, CommodityTariff, CommodityMetering, ElectricalGridConditions, PowerSourceConfiguration |
| Klima | ThermostatUserInterfaceConfiguration |
| Node / Aggregator | FixedLabel, UserLabel, LocalizationConfiguration, TimeFormatLocalization, UnitLocalization, SoftwareDiagnostics, EthernetNetworkDiagnostics, WiFiNetworkDiagnostics, Actions, EcosystemInformation |

Nach Abschluss: 101 von 135 Clustern mit Server. Die restlichen 34 sind die
32 ausgeschlossenen Cluster aus Anhang F (Kamera 7, Media/Content 15, TLS 2,
Joint Fabric und CommissionerControl 3, Thread/Netzwerk 4, OTA-Provider 1)
und zwei Cluster, die kein Gerätetyp des Snapshots verlangt und die vor
Phase 2 einer Scope-Entscheidung bedürfen: AmbientContextSensing und
WaterTankLevelMonitoring.

Von den 72 Gerätetypen im Scope sind 11 **Controller-Gerätetypen** (Anhang
F.3): sie tragen nur Client-Cluster und bleiben ohne Initiator-Rolle
unerreichbar. Die Zahl „60 von 72 vollständig“ oben zählt sie mit, weil
ihnen kein Server-Cluster fehlt; tatsächlich montierbar sind 61.

**Protokoll- und Modell-Lücken** (aus `docs/open-items.md` und
`docs/matterjs-comparison.md`, dort mit Beleg):

| Lücke | Heute | matter.js-Quelle |
| --- | --- | --- |
| TCP-Transport | fehlt | `protocol/src/transport/tcp` (TcpChannel, TcpTransport) |
| BDX | fehlt | `protocol/src/bdx` (BdxSession, BdxMessenger, BdxProtocol; 2 121 Zeilen mit TCP) |
| Timed-Write pro Attribut | nur Request-Gate | `BD-Matter-TimedAndQuotaDeferred` |
| Subscription-Quota pro Fabric | fehlt | dito |
| PASE-Sitzungsparameter (1.3+) | Legacy-Tripel | `BD-Matter-PASE-SessionParametersLegacy` |
| ARL (Managed Aggregator) | nur Konstante | `BD-Matter-ARL-NotMounted` |
| Server auf generierten Definitionen | alle Anwendungs-Server, `cluster/measurement`, GenericSwitch, AdministratorCommissioning und 14 Core-Server migriert (PR #32–#34, 2026-10-09); AccessControl, OperationalCredentials, GroupKeyManagement, Groupcast bewusst übersprungen | ADR 0013, Migrationsliste |
| Gerätetyp-Definitionen als Code | nur Lookups | `docs/matterjs-comparison.md` §6, ◐ |

Drei Lücken, die der Wizard am 2026-10-08 noch vorfand, hat PR #31 am
2026-10-09 geschlossen: das node-eigene Commissioning-Fenster (F-SWEEP-1),
TotalOperationalHours in Millisekunden (F-SWEEP-2) und Teile unter einem
gebridgten Endpoint (F-COMP-1, jetzt `endpoint.Spec.Parts`). Das letzte
war die wichtigste Vorbedingung der Geräteschicht in Teil B: ohne Teile
gäbe es keinen Zweikanal-Schalter, keinen Thermostat mit Feuchtesensor als
eigenem Endpoint und keinen SmokeCoAlarm mit PowerSource-Komponente.

### A.3 Phasen

Die Reihenfolge folgt der Entscheidung „Vollständigkeit zuerst“ und den
Abhängigkeiten. Keine Termine; jede Phase schließt mit einem messbaren
Zustand in `docs/open-items.md` und `docs/certifiability.md`.

**Phase 0: Fabrik vor Breite.** 46 Server von Hand zu schreiben
widerspricht dem Befund, dass 30 davon in matter.js reine generierte
Behaviours ohne eigene Logik sind. Zuerst:

- Ein **generierter Standard-Server** (ADR 0017; `cluster/spec.Instance`
  als vollständiger `contract.ClusterServer`): Attribute mit Constraint-
  und Conformance-Prüfung, Write-Regeln, Privilegien, DataVersion,
  Reporting aus der Definition. Ein Cluster ohne matter.js-Logik braucht
  dann nur einen Host-Port für die Werte und null handgeschriebene Regeln.
  Die ADR-0013-Migration der bestehenden Server ist seit PR #34 erledigt
  (vier Sicherheits-Cluster bewusst ausgenommen); jeder migrierte Server
  schreibt aber Host-Port, Dispatch und Reporting noch von Hand, und genau
  das ersetzt der Standard-Server für die 46 neuen.
- Die Bridge-Codecs auf die generierten Definitionen (ADR 0013, „Bridge
  path still on hand-written wire types“), Cluster für Cluster.
- `schema/writable.go`, `timed.go`, `invoke_privilege.go` bleiben nach der
  Entscheidung vom 2026-10 handgeschrieben (ADR 0013, „What stays as it
  was“): der Abgleich mit generierten Tabellen fand Unterschiede, die pro
  Zeile als Verhaltensänderung zu klassifizieren sind. Ein Server auf dem
  Standard-Server beantwortet Schreibbarkeit, Timed und Privileg selbst aus
  seiner Definition, so dass die Tabellen nur noch die Server decken, die
  nicht darauf laufen.

Abschluss: `docs/open-items.md` §3 enthält nur noch die zwei Cross-Check-
Differenzen und die vier übersprungenen Sicherheits-Cluster.

**Phase 1: Protokoll-Rest.** Timed-Write pro Attribut, Subscription-Quota,
PASE-Sitzungsparameter. Jeder Punkt hat seine matter.js-Quelle bereits in
`by_design.md` zitiert. Abschluss: §1 von `open-items.md` enthält nur noch
die ADR-0014-Hand-off-Zeile.

**Phase 2: Cluster-Breite in vier Tranchen**, jede mit Parity-Test, Eintrag
im Referenz-Daemon und PICS-Regeneration:

| Tranche | Cluster | Erwarteter Aufwand |
| --- | --- | --- |
| 2a Sensorik und Labels | 7 Konzentrations-Cluster, BooleanStateConfiguration, FixedLabel, UserLabel, die drei Localization-Cluster, SoftwareDiagnostics, Ethernet-/WiFiNetworkDiagnostics, PowerSourceConfiguration | gering: alle generiert; nur UserLabel (41 Zeilen) und TimeFormatLocalization (89) haben Regeln |
| 2b Appliances | TemperatureControl, Oven*, Refrigerator*, Laundry*Controls, DishwasherAlarm, TemperatureAlarm, MicrowaveOven*, ServiceArea | mittel: ModeBase-Regeln existieren schon in `cluster/modebase`; ServiceArea (300 Zeilen) und OvenCavityOperationalState (107) haben eigene Logik |
| 2c Energie | DeviceEnergyManagement(+Mode), EnergyEvse(+Mode), WaterHeaterManagement(+Mode), EnergyPreference, MeterIdentification, Commodity*, ElectricalGridConditions | mittel: Modes auf `cluster/modebase`; die Verwaltungs-Cluster sind generiert, aber die Schema-Strukturen (Forecast, Slots) sind groß |
| 2d Rest | ClosureDimension, SoilMeasurement, ThermostatUserInterfaceConfiguration, Actions, EcosystemInformation | gering bis mittel; Actions braucht einen Host-Port für Aktionslisten |

Abschluss: 101 Server; jeder Gerätetyp im Scope montierbar.

**Phase 3: TCP und BDX.** `transport/tcp` nach `TcpTransport.ts`,
Sitzungsparameter-Aushandlung für große Nutzlasten, `bdx/` als Responder
für `DiagnosticLogs.RetrieveLogsRequest` mit `TransferFileDesignator`. Kein
OTA. Abschluss: DLOG-Familie läuft; `BD-chip-DiagLogs-NoBDX` entfällt.

**Phase 4: Geräteschicht `device/` und Node-Fassade** (Teil B). Der
Referenz-Daemon migriert als erster Konsument; `examples/reference-bridge`
schrumpft auf die Gerätebeschreibungen. Abschluss: jede Gerätetyp-Datei
unter `device/` generiert, Referenz-Daemon ohne `contract.EndpointSource`.

**Phase 5: Nachweis und v1.0.** Apple-Home- und Alexa-Läufe mit dem
Referenz-Daemon in der neuen Topologie (Teile, Energie, Appliances), Befunde
in `docs/matter-ecosystem-observations.md`. `v1.0.0` markiert
„1.6.1 vollständig im Scope“ und friert `device/` als öffentliche API ein.

**Danach: der Adapter-Daemon** (Teil C).

Querschnitt in jeder Phase: `make chipdm-check`, Parity-Guards,
`cover-check`-Ratchets, CHANGELOG. Jede Phase, die eine Scope-Grenze berührt,
erhält eine ADR.

---

## Teil B: Die Geräteschicht `device/`

### B.1 Warum in go-fabric, und warum das keine Verletzung der Bridge-Regel ist

`CLAUDE.md` sagt: „Which data point becomes which cluster attribute, which
product maps to which device type — that lives host-side.“ Das bleibt so.
Die Geräteschicht verschiebt die Grenze nicht, sie macht die Matter-Seite der
Grenze benutzbar:

- **Host-Sache bleibt:** CCU-Datenpunkt `LEVEL` ist die Helligkeit; HA-Entität
  `light.kitchen` mit `supported_color_modes: [color_temp]` ist ein
  ColorTemperatureLight. Das ist Produktwissen.
- **Matter-Sache ist:** ein DimmableLight (0x0101, rev 4) trägt Identify,
  Groups, OnOff mit Feature LT, LevelControl mit Features LT und OO,
  CurrentLevel 1..254, MinLevel 1, MaxLevel 254, ScenesManagement. Das steht
  wörtlich so in `parity/schema.json` unter
  `deviceTypes[DimmableLight].effective.requirements` und in matter.js
  `devices/dimmable-light.ts`. Heute muss jeder Host diese Tabelle selbst
  nachbauen; openccu-loom tut es in `internal/north/matteradapter/` und
  `internal/model/custom/*/matter*.go`.

matter.js hat diese Schicht als `packages/node/src/devices` (81 Dateien) und
`packages/node/src/endpoints` (Root, Aggregator, BridgedNode, PowerSource,
ElectricalSensor, …). `docs/matterjs-comparison.md` §6 führt sie als ◐.
Sie zu portieren ist Teil des 100-%-Ziels, nicht ein neues Produkt.

### B.2 Form

Drei Pakete, alle in go-fabric:

| Paket | Mirror von | Inhalt |
| --- | --- | --- |
| `device/` | `packages/node/src/devices`, `endpoints` | Ein Go-Typ pro Gerätetyp im Scope, **generiert** aus dem Snapshot durch ein neues `script/devicegen` (analog `script/clustergen`, ADR 0013). Konstruktor, Pflicht- und Optionalcluster als typisierte Zugriffe, Feature-Auswahl, Teile. |
| `node/` | `ServerNode.create`, `Node.Configuration` | Die Fassade: eine `Config`, ein `New`, ein `Start`. Verdrahtet intern alles, was heute die rund 40 `Attach…`/`Set…`-Aufrufe in `daemon_matter.go` tun. |
| `cluster/<name>` | `behaviors/<name>/*Server.ts` | Bleibt. `device/` montiert diese Server; neu ist nur, dass jeder Server eine typisierte `State`-Struktur und einen `Events`-Satz exportiert, den `device/` durchreicht. |

Die heutige Schicht (`contract.EndpointSource`, `endpoint.Spec`,
`bridge.Snapshotter`) bleibt als Matter-nahe Ebene erhalten. `device/`
erzeugt daraus `endpoint.Spec`-Werte; ein Host, der beide mischt, kann das.

**Beispiel, wie der Host es sieht** (Zielbild, keine API-Zusage):

```go
n, err := node.New(node.Config{
    VendorID: 0xFFF1, ProductID: 0x8001,
    Passcode: 20202021, Discriminator: 3840,
    BasicInformation: node.BasicInformation{VendorName: "…", ProductName: "…"},
    Store: sqlitestore.Open(path),       // Fabrics, ACL, Endpoint-IDs, Subscriptions
    Network: node.Network{Port: 5540},
})

agg := n.Aggregator()                     // endpoints/aggregator.ts

lamp := device.NewDimmableLight(device.Options{
    StableKey: "ccu:0001D3C99:3",         // endpoint.SourceKey
    Name:      "Küche",
    Vendor:    "eQ-3",
    Reachable: func() bool { return dev.Online() },
})
agg.Add(ctx, lamp)                        // aggregator.add(endpoint)

// Controller → Host: Kommandos und Writes kommen als Events an,
// mit den Default-Implementierungen von matter.js (Toggle, MoveToLevel …).
lamp.OnOff().OnChanged(func(on bool) { dev.SetState(on) })       // events.onOff.onOff$Changed
lamp.LevelControl().OnChanged(func(l uint8) { dev.SetLevel(l) })

// Host → Controller: typisierte Setter, Reporting übernimmt das Modul.
lamp.OnOff().Set(ctx, onoff.State{OnOff: true})                   // endpoint.set({onOff:{onOff:true}})
lamp.LevelControl().Set(ctx, levelcontrol.State{CurrentLevel: 128})
```

Zusammengesetzte Geräte (`endpoint.Spec.Parts`, seit PR #31):

```go
alarm := device.NewSmokeCoAlarm(opts)
alarm.AddPart(ctx, device.NewPowerSource(partOpts))   // BridgedNode.parts
```

Feature-Auswahl pro Endpoint, wie `OnOffServer.with("Lighting")` in
matter.js:

```go
light := device.NewExtendedColorLight(opts,
    device.WithFeatures(colorcontrol.FeatureXY|colorcontrol.FeatureCT|colorcontrol.FeatureHS))
```

`Add` führt `endpoint.ValidateDeviceTypes` aus (ADR 0016) und lehnt eine
Zusammenstellung ab, die der Gerätetyp nicht erlaubt; die Fehlermeldung
zitiert die Anforderung aus dem Schema.

### B.3 Was generiert wird, was von Hand kommt

Das Schema trägt bereits alles, was matter.js's Gerätedateien aussprechen:
Pflicht- und Optionalcluster mit Conformance, verlangte Features, Attribut-
Constraints und -Defaults, Komponenten und Bedingungen (gemessen an
`DimmableLight.effective.requirements`: Feature LT auf OnOff, Features LT
und OO sowie CurrentLevel 1..254, MinLevel 1, MaxLevel 254 auf
LevelControl). `script/devicegen` erzeugt daraus pro Gerätetyp:

- den Typ mit Gerätetyp-ID und Revision aus `schema.DeviceTypeRevision`;
- die Cluster-Zugriffe (`OnOff()`, `LevelControl()`, …) für Pflicht- und
  Optionalcluster; ein Optionalcluster ist nil, bis `With…` ihn anfordert;
- die Feature- und Constraint-Vorgaben, die `device/` beim Montieren an den
  Cluster-Server gibt;
- die erlaubten Teile und Bedingungen für `AddPart`.

Von Hand bleiben: die Cluster-Server-Regeln (wie heute), die Fassade
`node/`, der Lebenszyklus (Add, Remove, Reachable, NodeLabel-Persistenz,
ConfigurationVersion) und die Konvertierung zu `endpoint.Spec`. Diese
Handschrift ist einmal zu schreiben, nicht pro Gerätetyp.

Der Generator läuft in `make generate-matter-schema` mit; ein Test prüft wie
bei `clustergen`, dass die committeten Dateien dem Snapshot entsprechen.

### B.4 Laufzeit-Topologie statt Snapshot

Heute liefert der Host eine ganze `endpoint.Snapshot` und die Bridge
assembliert alles neu (`Reassemble`). matter.js arbeitet mit `add` und
`delete` auf dem Aggregator. `device/` bietet beides: `Add`/`Remove` zur
Laufzeit, und intern erzeugt es die Snapshot für den bestehenden Assembler,
so dass Endpoint-ID-Persistenz, Scope-GC und `ModelComplete` unverändert
gelten (`BD-Matter-EndpointID-Persistent`). Der `Snapshotter`-Port bleibt
für Hosts, die ihre Topologie weiter als Ganzes liefern wollen.

### B.5 Stabilität

`device/` ist vom ersten Commit an öffentliche API nach `README.md`
§API stability. Bis `v1.0.0` gilt die Pre-1.0-Regel; mit `v1.0.0` beginnt
das Deprecation-Fenster. Die Generierung macht Umbenennungen billig, aber
nicht kostenlos: eine Gerätetyp-Umbenennung im Schema (matter.js hat das
mehrfach getan) wird zu einem Alias mit Deprecation, nicht zu einem Bruch.

### B.6 Abgrenzung zum heutigen Host-Code

| Heute in openccu-loom | Mit `device/` |
| --- | --- |
| `daemon_matter.go`, 4 333 Zeilen, ~40 Attach/Set-Aufrufe | `node.New(cfg)` plus die Hooks, die loom wirklich braucht (Statusadapter, Ereignis-Publisher) |
| `internal/north/matteradapter/assembler.go`, 778 Zeilen | Zuordnung Kanal → `device.New…`; Gerätetyp-Tabellen entfallen |
| `internal/model/custom/*/matter*.go` | Setter und `OnChanged`-Hooks statt `contract.ClusterServer`-Implementierungen |
| `pkg/interfaces/matter.go` | bleibt, wo loom eigene Ports hat (Status, Fenster, Ephemeral-Provider) |

Die Migration ist inkrementell: `contract.EndpointSource` bleibt gültig,
ein Endpoint kann alt, der nächste neu sein.

---

## Teil C: Der Adapter-Daemon außerhalb von go-fabric

Genau ein neues Repo (Arbeitstitel `go-matter-bridge`), nach `v1.0.0` von
go-fabric. Es hält die Abhängigkeiten, die go-fabric nicht tragen darf
(HA-WebSocket-Client, `go-mqtt`), und ist der zweite Konsument von
`device/` nach dem Referenz-Daemon.

### C.1 Aufbau

```
go-matter-bridge/
  cmd/matter-bridge          der Daemon: Config laden, Quellen starten, node.New
  source/                    das Quellen-Interface
  source/homeassistant/      HA WebSocket (subscribe_entities, call_service)
  source/zigbee2mqtt/        MQTT: bridge/devices (exposes) + <friendly>/state
  source/hadiscovery/        MQTT: homeassistant/<platform>/…/config (über go-hamqtt)
  project/                   Quelle → device/: eine Datei pro Domäne
  mapping/                   optionale YAML-Overrides
```

**Quellen-Interface** (Einschätzung, zu schärfen beim Bau):

```go
type Source interface {
    // Watch liefert den Bestand und danach jede Änderung.
    Watch(ctx context.Context) (<-chan Change, error)
    // Apply setzt einen vom Controller kommenden Wunsch an der Quelle um.
    Apply(ctx context.Context, cmd Command) error
}
```

`Change` beschreibt eine Entität in einem quellenneutralen Vokabular
(Domäne, Fähigkeiten, Zustand, Verfügbarkeit, Identität des physischen
Geräts). Das Vokabular ist das von go-hamqtt `model` (Device, Entity,
Slot, Binding, Availability), das bereits für HA-Discovery definiert ist
und die zweite Richtung nur spiegelt. Damit teilen sich HA-Discovery-Quelle
und zigbee2mqtt-Quelle den Projektor.

**Projektor:** eine Tabelle Domäne und Fähigkeitsmenge → `device`-Typ und
Features, pro Domäne eine Datei mit Golden-Test. Beispiele:

| Quelle | Gerätetyp |
| --- | --- |
| `light` mit `brightness`, ohne Farbe | DimmableLight |
| `light` mit `color_temp` | ColorTemperatureLight |
| `light` mit `hs`/`xy` | ExtendedColorLight |
| `switch`, `outlet` | OnOffPlugInUnit |
| `cover` mit `tilt` | WindowCovering mit Feature TL |
| `climate` | Thermostat, Features nach `hvac_modes` |
| `sensor` mit `device_class: temperature` | TemperatureSensor |
| `binary_sensor` mit `device_class: motion` | OccupancySensor |
| `lock`, `fan`, `vacuum` | DoorLock, Fan, RoboticVacuumCleaner |

Die YAML-Datei überschreibt pro Entität Gerätetyp, Sichtbarkeit und Name.
Ohne Datei läuft der Daemon mit der Tabelle.

### C.2 Was bleibt in openccu-loom

openccu-loom bleibt der Homematic-Daemon und konsumiert `device/` direkt
in-process. Es wird kein Universal-Daemon; HA- und zigbee2mqtt-Quellen
wandern nicht hinein. Sollte loom später HA-Entitäten spiegeln wollen,
importiert es `go-matter-bridge/source` als Bibliothek statt die Quelle
nachzubauen.

### C.3 HA-Direktanbindung, Form noch offen

Zwei Varianten, Entscheidung später:

1. `go-matter-bridge` als HA-Add-on: ein Container, HA-Token aus dem
   Supervisor, Matter-Bridge im Host-Netz. Entspricht dem Betriebsmodell
   von home-assistant-matter-bridge.
2. `go-matter-bridge` als eigenständiger Dienst neben HA, Token per Config.

Die Quelle ist dieselbe; nur Packaging und Netzwerk unterscheiden sich.

---

## Teil D: Risiken und offene Fragen

| Risiko | Einschätzung | Gegenmaßnahme |
| --- | --- | --- |
| TCP/BDX hat ohne OTA nur einen Konsumenten | Aufwand (2 121 matter.js-Zeilen) für die DLOG-Familie mit einem Fall | Phase 3 als letzte Protokollphase; Scope vor Beginn erneut bestätigen |
| Generierter Standard-Server reicht nicht für „generierte“ matter.js-Behaviours | Einige haben Reactor-Logik in `ClusterBehavior`-Basisklassen, nicht in `*Server.ts` | Vor Tranche 2a die Basisklassen `ModeBase`, `ConcentrationMeasurement`, `ResourceMonitoring`, `Label` lesen; sie erklären, warum dort keine `*Server.ts` liegen |
| Apple Home und Teile unter gebridgten Endpoints | seit v0.1.0 nicht gegen Apple geprüft; `docs/matter-ecosystem-observations.md` kennt Apples Topologie-Strenge | Phase-0-Ergebnis früh gegen Apple Home pairen, vor der Cluster-Breite |
| 46 neue PICS-Slices und CSA-Familien | Familienlauf wächst um Stunden | `GOFABRIC_CHIP_FAMILIES` je Tranche; nur der Release-Lauf vollständig |
| `device/` friert mit v1.0 ein, bevor der zweite Konsument existiert | Eine API mit einem Konsumenten ist unbewiesen | Referenz-Daemon vollständig auf `device/`; openccu-loom migriert mindestens Licht und Rollladen vor v1.0 |
| Scope von AmbientContextSensing und WaterTankLevelMonitoring | nicht im Wizard entschieden; kein Gerätetyp verlangt sie | vor Tranche 2a entscheiden; beide klein |

---

## Teil E: Nächste Schritte

1. Dieses Konzept gegenlesen; Entscheidungen aus §0 bestätigen oder
   ändern. Insbesondere die TCP/BDX-ohne-OTA-Spannung.
2. Erledigt am 2026-10-08: [ADR 0017](./adr/0017-generated-default-cluster-server.md)
   (generierter Standard-Server, Phase 0) und
   [ADR 0018](./adr/0018-device-layer-and-node-facade.md) (Geräteschicht
   und Node-Fassade, Phase 4) liegen als „Proposed“ vor;
   `docs/matterjs-comparison.md` §2, §5 und §6, `docs/open-items.md`,
   `docs/feature-scope.md`, `README.md` und `CHANGELOG.md` verweisen auf
   dieses Konzept. Beide ADRs wechseln auf „Accepted“, wenn ihre Phase
   beginnt.
3. Phase 0 beginnen mit dem Standard-Server aus ADR 0017; die
   ADR-0013-Migration, die hier ursprünglich als Einstieg stand, ist seit
   PR #34 erledigt.

---

## Anhang F: Was zu „1.6.1 vollständig“ zunächst nicht gehört

Jeder Eintrag nennt, was fehlt, warum es außerhalb liegt, wo die
Entscheidung steht, und was es kosten würde, ihn später hereinzuholen.
„Entscheidung“ heißt: bewusst ausgeschlossen, keine Backlog-Position.
„Wizard“ heißt: am 2026-10-08 so festgelegt, bei Bedarf neu zu entscheiden.

### F.1 Rollen und Transport

| Punkt | Warum draußen | Quelle | Weg herein |
| --- | --- | --- | --- |
| Controller- und Commissioner-Rolle (`ClientNode`, `DeviceCommissioner`, mDNS-Browsing, DCL-Client) | Eine Bridge antwortet; sie entdeckt, kommissioniert und steuert keine anderen Nodes. | `docs/matterjs-comparison.md` Non-goal 1 | Zweite Zustandsmaschine, Zertifikats-Aussteller-Rolle, Gerätecache. Ein anderes Produkt, kein Ausbau. |
| Minimal-Controller für Binding-Clients | Schon der kleinste Initiator (CASE zum Ziel, Invoke senden) ist die Controller-Rolle im Kleinen. Die einzige Ausnahme bleibt ADR 0008. | Wizard, Runde 1 | ADR-0008-Initiator verallgemeinern; danach F.3 Controller-Gerätetypen. |
| Bluetooth / BTP | Löst ein Problem, das eine LAN-Bridge nicht hat: Netzwerk-Credentials auf ein Gerät ohne Netz bringen. Zieht BlueZ/CoreBluetooth in ein Modul, dessen einzige OS-Abhängigkeit ein UDP-Socket ist. | Non-goal 2 | Plattform-Radio-Stack als eigenes Modul; `commissioning/` um BTP-Framing erweitern. |
| Thread, Thread-Border-Router-Client, Thread-Netzwerk-Diagnose | Der Host hängt an Ethernet oder Wi-Fi; NetworkCommissioning meldet Ethernet. | Non-goal, `docs/matterjs-comparison.md` §2 | Setzt Thread-Hardware oder einen Border-Router voraus. |
| Wi-Fi-Commissioning (NetworkCommissioning Feature WI) | Ohne BLE gibt es keinen Weg, Credentials vor dem Netzbeitritt zu übergeben. | Wizard, Runde 1 | Folgt BLE. |
| Groupcast-Sender-Feature, Gruppen-Senden | Senden ist Initiator-Verhalten. | `BD-Matter-GroupcastNoSender` | Folgt dem Minimal-Controller. |
| TCP für Kamera-Streams | TCP kommt (Phase 3), aber nur als Large-Payload-Pfad für DiagnosticLogs. Streaming-Nutzlasten brauchen die Kamera-Cluster aus F.2. | Wizard, Runde 1 | Mit F.2 Kamera. |

### F.2 Cluster-Familien (32 Cluster)

Gemessen gegen `parity/schema.json`; die Gruppenzuordnung ist die dieses
Dokuments, nicht die der Spezifikation.

| Familie | Cluster | Warum draußen | Weg herein |
| --- | --- | --- | --- |
| Kamera (7) | CameraAvStreamManagement, CameraAvSettingsUserLevelManagement, ZoneManagement, PushAvStreamTransport, WebRtcTransportProvider, WebRtcTransportRequestor, Chime | Streams brauchen WebRTC-Signalisierung, TCP-Nutzlasten und einen Medienpfad im Host; kein Bridge-Gerät des Scopes liefert Video. matter.js's `web-rtc-transport-requestor` hat 209 Zeilen eigene Logik. | Eigene Phase nach v1.0, mit einem konkreten Kamera-Host als Konsument. Chime allein wäre klein, verlangt aber den Doorbell-Kontext. |
| Media / Content (15) | MediaPlayback, KeypadInput, ContentLauncher, ApplicationLauncher, ApplicationBasic, AccountLogin, AudioOutput, Channel, TargetNavigator, MediaInput, LowPower, WakeOnLan, ContentControl, ContentAppObserver, Messages | Fernseher, Casting-Player und Content-Apps sind keine Bridge-Geräte; Casting braucht zudem die Client-Seite (CastingVideoClient). | Tranche nach v1.0, wenn ein Host Media-Geräte bridgen will (z. B. ein AV-Receiver über MQTT). Fast alle sind rein generiert; der Aufwand liegt in den Host-Ports. |
| TLS (2) | TlsCertificateManagement, TlsClientManagement | Dienen den Kamera- und Push-AV-Transporten. | Mit Kamera. |
| Joint Fabric (3) | JointFabricDatastore, JointFabricAdministrator, CommissionerControl | Fabric-übergreifende Administration ist eine Controller-Aufgabe. | Folgt der Controller-Rolle. |
| Thread / Netzwerk-Infrastruktur (4) | ThreadNetworkDiagnostics, ThreadBorderRouterManagement, ThreadNetworkDirectory, WiFiNetworkManagement | Setzen Thread-Hardware oder einen Border-Router voraus. | Mit Thread. |
| OTA-Provider (1) | OtaSoftwareUpdateProvider | Firmware an andere Nodes verteilen ist eine Distributions-, keine Geräterolle. | `BD-Matter-OTAProvider-NotExposed`; folgt dem OTA-Requestor. |

### F.3 Gerätetypen

| Gruppe | Gerätetypen | Warum draußen |
| --- | --- | --- |
| Kamera-Gerätetypen (6) | Camera (0x0142), SnapshotCamera (0x0145), AudioDoorbell (0x0141), Intercom (0x0140), Chime (0x0146), CameraController (0x0147) | verlangen F.2 Kamera als Pflichtcluster |
| Media-Gerätetypen (3) | BasicVideoPlayer (0x0028), CastingVideoPlayer (0x0023), ContentApp (0x0024) | verlangen F.2 Media als Pflichtcluster |
| Infrastruktur (4) | ThreadBorderRouter (0x0091), NetworkInfrastructureManager (0x0090), JointFabricAdministrator (0x0130), OtaProvider (0x0014) | verlangen F.2 Thread, Joint Fabric oder OTA-Provider |
| Controller-Gerätetypen (11) | OnOffLightSwitch (0x0103), DimmerSwitch (0x0104), ColorDimmerSwitch (0x0105), ControlBridge (0x0840), OnOffSensor (0x0850), DoorLockController (0x000B), WindowCoveringController (0x0203), ThermostatController (0x030A), PumpController (0x0304), ClosureController (0x023E), VideoRemoteControl (0x002A) | tragen nur Client-Cluster und Binding; ein Gerät dieses Typs sendet Kommandos an andere Nodes, was die Initiator-Rolle ist. Ein gebridgter Taster wird stattdessen als GenericSwitch (0x000F, Server-Cluster Switch) abgebildet, was go-fabric heute kann. |
| OtaRequestor (0x0012), SecondaryNetworkInterface (0x0019) | | der Requestor ist abgewählt (F.4); eine zweite Netzwerkschnittstelle setzt Thread oder Wi-Fi-Diagnose voraus |

Gemessen: 91 Gerätetypen im Snapshot, davon 13 mit ausgeschlossenen
Pflichtclustern, 11 Controller-Typen und die 2 der letzten Zeile. Im Scope
und montierbar bleiben 65, darunter die Strukturtypen RootNode, Aggregator,
BridgedNode, PowerSource und ElectricalSensor.

### F.4 Montierbare Server, die abgewählt sind (Wizard, Runde 1)

| Punkt | Heute | Warum draußen | Weg herein |
| --- | --- | --- | --- |
| OTA-Requestor vollständig (Query, Download, Apply, DefaultOTAProviders persistent) | Stub, nicht montiert | Ein Bridge-Produkt bringt seinen eigenen Update-Pfad mit; Apple und Google nutzen den Requestor gebridgter Geräte nicht. Nach TCP+BDX (Phase 3) ist er technisch möglich. | matter.js `ota-software-update-requestor` (1 436 Zeilen) portieren, Host-Port für das Anwenden. SU-Familie (13 Fälle) wird lauffähig. |
| Binding-Tabelle und Client-Cluster-Deklaration | Server mit Tabelle vorhanden, kein Endpoint deklariert Clients | Ohne Sender hat die Tabelle keinen Effekt; sie nur zu persistieren täuscht einem Controller Fähigkeit vor. | Mit dem Minimal-Controller. BIND-Familie (3 Fälle). |
| ICDManagement | Attribute 0..2, nicht montiert | Eine netzbetriebene Bridge ist kein Intermittently Connected Device; ein Controller hielte den Node für schläfrig. | Nur für ein Batteriegerät auf go-fabric, das ohne Thread und BLE nicht entsteht. ICDM-Familie (7 Fälle). |
| Access Restriction List (Managed Aggregator) | Konstante und Integrationspunkt | Kein Konsument hat den Managed-Aggregator-Fall. | `BD-Matter-ARL-NotMounted` nennt die Reaktivierungs-Checkliste. |

### F.5 Nachweis

| Punkt | Warum draußen | Quelle |
| --- | --- | --- |
| CSA-Zertifizierung | Nicht verfolgt; Zertifizierbarkeit ist das Ziel. Nichts auf go-fabric darf als zertifiziert bezeichnet werden. | ADR 0011 |
| 89 manuelle Operator-Fälle (`PICS_USER_PROMPT`) | Ein Testlabor-Operator führt einen Schritt aus; es gibt keinen Stand-in. Sie bleiben Klasse (c) mit benanntem Schritt. | `docs/certifiability.md` |
| Google Home als Testcontroller | Steht nicht zur Verfügung; Nachweis läuft gegen Apple Home, Alexa, chip-tool und den matter.js-Controller. | Wizard, Runde 3 |
| Zwei Cross-Check-Differenzen, bei denen CHIP recht hat | Upstream-matter.js-Kandidaten, kein ausgeliefertes Verhalten hängt daran. | `docs/chip-datamodel-crosscheck.md` Klasse (ii) |

### F.6 Noch nicht entschieden

| Punkt | Stand |
| --- | --- |
| AmbientContextSensing, WaterTankLevelMonitoring | Kein Gerätetyp des Snapshots verlangt sie; vor Tranche 2a entscheiden. |
| Form der HA-Direktanbindung (Add-on oder Dienst) | Teil C.3; nach v1.0. |
| Ob TCP+BDX ohne OTA den Aufwand trägt | §0; vor Phase 3 erneut bestätigen. |
