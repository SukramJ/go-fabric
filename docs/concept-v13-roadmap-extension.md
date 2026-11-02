# Konzept v13 — Roadmap-Erweiterung: Beweiskraft, Sicht, Rest der Norm

Stand 2026-11-02, nach v0.16.0 (Review 2026-11 abgeschlossen, Milestones
1–6 geschlossen, Milestone 7 BLE-Geräteseite offen). Owner-Auftrag: „die
Roadmap erweitern“ in drei Themen — Qualität der Tests, UI, Rest des
Standards. Pins: matter.js 862ad75f, connectedhomeip 6170af84. Faktenbasis:
drei Sonnet-Lesekarten (`read-roadmap-quality.md`, `read-roadmap-ui.md`,
`read-roadmap-standard.md`, 2026-11-02); jede tragende Zeile vom Reviewer
am Pin nachgelesen.

## A. Ausgangslage, eine Stufe höher

Die Roadmap-Prämisse (`docs/roadmap.md:11-23`) ist erfüllt, soweit sie
zählbar ist: Alle 135 Cluster und 91 Gerätetypen des Snapshots haben einen
Server; jedes `*Server.ts` von matter.js über 100 Zeilen wird von Go-Code
zitiert; jedes `packages/protocol/src`-Verzeichnis ebenso. Was fehlt, ist
nicht Breite, sondern drei Dinge:

1. **Beweiskraft.** 707 chip-Fälle: 400 bestehen, 131 nicht anwendbar, 167
   „Harness“ — davon 150 als „manuell, nichts zu automatisieren“ eingestuft
   (`docs/certifiability.md:246-559`). matter.js hat 68 dieser Fälle in
   `support/chip-testing/test/cert/` selbst automatisiert (TC-ACT-3.2,
   TC-BDX, TC-BIND, TC-CADMIN-1.17, TC-DD-3.11–3.21, TC-G-3.2, TC-IDM-1.1 bis
   6.4, TC-LVL-8.1, TC-S-3.1, TC-SC-4.2/4.8/5.3/6.1/8.1–8.7, TC-SU-1.1 bis
   4.1, TC-SWTCH-3.2 …); 45 davon stehen bei uns als „manuell“. Die
   Einstufung ist widerlegt. Dazu 1140 nicht portierte matter.js-Fälle in
   140 teilportierten Dateien (`script/testparity/register.json`), 31
   chip-Präfixe mit ≥ 3 Fällen ohne Familie, vier echte Defekte
   (TC-CNET-4.1/4.4/4.9, TC-EEVSE-2.3), 242 `time.Sleep` in Tests, 18
   Stress-Flakes, und drei Protokollbehauptungen, die nie ein echter
   Controller geprüft hat (`docs/matterjs-comparison.md:211-223`:
   Gruppen-Messaging, Subscription-Wiederaufnahme, Timed-Write je Attribut).
2. **Sicht.** go-fabric-ui liefert U1/U2 (7 Node-Seiten, 4 Controller-Seiten
   mit 9 Tabs), aber keine Topologie, keine Live-Attribute, kein e2e; die UI
   nutzt vom Server-WS nichts (`get_network_topology` mit Thread/Wi-Fi-
   LQI/RSSI, `attribute_updated` liegen brach); das Modul liefert mehr als
   der Vertrag trägt (`node.Observers`, `SessionDiagnostics`,
   `Subscriptions()`, ICD, OTA-Provider, Health, Diagnostik-Server).
3. **Rest der Norm.** Features, die der Snapshot definiert und ein Server
   hier nicht bedient: bei vieren hat matter.js sie (DoorLock FACE,
   AdministratorCommissioning BC, OccupancySensing jenseits PIR, PowerSource
   WIRED/RECHG/REPLC) — nach der Prämisse fällig; bei den übrigen hat
   matter.js sie auch nicht (GeneralCommissioning TC/NR, Descriptor TAGLIST,
   Thermostat SB/TSUGGEST, ClosureControl LT/IS/SP/CL, Valve TS/LVL,
   LevelControl FQ, AirQuality-Stufen, Konzentrations-LEV/MED/CRI/PEA/AVG,
   ColorControl-Primaries) — Konsumentenfrage mit chip als Autorität. Dazu
   ARL (kein Konsument), NFC (matter.js hat es nicht), BLE-Geräteseite
   (M7) und chips Datenmodell 1.7 (12 Cluster, 13 Gerätetypen über dem
   Snapshot), das erst mit matter.js' Pin kommt.

## B. Ziel

Drei Milestones, je ein Thema, je eine Minor-Version — in der Reihenfolge,
die der Owner wählt (§E.1). Jeder Milestone zeigt die drei Säulen der
Roadmap (Familien grün oder klassifiziert, matter.js-Fälle portiert und
im Register, Vergleichszeilen ✅ oder `by_design`).

## C. Die drei Themen

### C.1 Milestone 8 — Beweiskraft (Qualität)

**Befund** (§A.1). Die Lücke ist nicht Testmenge, sondern Beweisart: Was
automatisierbar ist, läuft noch nicht; was in-process bewiesen ist, hat
keinen echten Controller gesehen; was Scheduling-abhängig ist, flakt.

**Phasen**

| Phase | Inhalt | Beweis |
| --- | --- | --- |
| 29 | **Die 68 matter.js-Zertifizierungsfälle** aus `support/chip-testing/test/cert/` dem Sinn nach als Go-Fälle im Harness: der Go-Controller (`internal/chiptool/gocontroller_test.go`) gegen den Referenz-Daemon, chip-tool-Schritte dort, wo matter.js sie nutzt. Reklassifizierung der 45 „manuell“-Zeilen; die 23 übrigen (SC-8.x, WEBRTCR, SU-2.2/2.4/4.1, TBRM-3.1, ICDB-1.3, ICDM-6.1) nach Familie. | `certifiability.md`: Harness-Fälle 167 → ≤ 120; jeder portierte Fall zitiert die TS-Quelle |
| 30 | **Die vier Defekte** (CNET: `WiFiNetworkCommissioning` persistiert die committeten Netze nicht; EEVSE-2.3: der generierte `SetTargets`-Decoder antwortet CONSTRAINT_ERROR vor RESOURCE_EXHAUSTED — Decoder-Reihenfolge je Liste, matter.js `ValueValidator` als Modell) und die **drei Protokollbeweise** (chip-tool-Leg: `groupcast`/`groups` mit Multicast-`onoff toggle`; Subscription-Wiederaufnahme nach chip-tool-Neustart; Timed-Write je Attribut, `schema/timed.go` als Quelle). | 0 Klasse-(a); Vergleichszeilen :62 ✅ oder `by_design` |
| 31 | **matter.js-Portierung, Tranche Server:** `ServerNodeTest` (66), `InteractionProtocolTest` (39), `ServerSubscriptionTest` (27), `SuppressResponseTest` (13), die Validierungsfamilie (`DeviceTypeConformance*` 47+24, `RuntimeValidation` 27, `StandardDeviceTypeCorpus` 17, `ValueValidator`/`constraint`/`ValidatedElements` 38+11+23) — 332 Fälle. | missing-Ratchet 1140 → ≤ 810 |
| 32 | **matter.js-Portierung, Tranche Controller und Protokoll:** `ClientNodeTest` (106), `ClientSubscribeTest` (25), `ClientEventNotificationTest` (23), `IcdClientTest` (52), `DclOtaUpdateServiceTest` (36), `CertificatesTest`/`PqcAttestationCertificatesTest` (21+19, Fable), `SessionManagerTest` (19, Fable), `OtaTest` (11) — 312 Fälle. | missing ≤ 500 |
| 33 | **Neue Register-Wurzeln:** `packages/types/test/tlv` (218 — unser Codec-Kern), `packages/general/test/net` (228 — DNS-SD, nach dem Responder v12), `packages/general/test/codec` (34), `packages/model/test/aspects` (125 — unser Conformance-Compiler), `packages/types/test/schema` (78 — QR, Manual Code, Base38), `packages/general/test/crypto` (99, Fable). Der Rest von `general`/`model`/`nodejs` bleibt ausgeschlossen mit Grund (Plattform, Util). | Wurzeln in `matterjs_tests.json`, Ratchets gesetzt |
| 34 | **Familien ohne Lauf:** ein zweites Kompositionsprofil des Referenz-Daemons (`--profile camera|jointfabric|tls|thread`), damit WEBRTCP (32), AVSM (21), PAVST (15), AVANALY (14), TLSCERT (13), ZONEMGMT, CHIME, JFADMIN/JFDS, TBRM (gegen den virtuellen OTBR), DGTHREAD, THNETDIR laufen; ESALM/EPALM/TSTATM prüfen, ob die Cluster am Pin im Snapshot sind. | 31 Präfixe → ≤ 8, der Rest klassifiziert (b)/(d) |
| 35 | **Determinismus:** `time.Sleep` 242 → Clock-Seams (die 165 außerhalb `synctest`), die 18 Stress-Flakes auf 0, W6-test-17 (48 `err == nil`-Stellen bekommen Status-Assertions), `t.Skip` in mdns (66, fällt mit v12) und endpoint (34) begründet oder weg. | Nightly `stress-flakes` leer; Sleep-Ratchet |

**Besser als vorher, besser als andere — stimmt der Schnitt?** Vorher
drei Testquellen mit drei Lücken; nachher dieselben drei Quellen, aber
jede mit Ratchet (missing, Harness-Fälle, Sleeps) und mit dem echten
Controller als vierter Säule, wo in-process-Beweise bisher genügten.
Gegen matter.js: Es hat 1839 Tests für sein eigenes Harness
(`cert-framework`) und 74 Familien; wir laufen 120 Familien, aber
unsere „manuell“-Grenze war falsch gezogen. Der Schnitt bleibt: drei
Quellen, ein Register je Quelle, keine vierte Testform.

### C.2 Milestone 9 — Sicht (UI)

**Befund** (§A.2). Die Daten sind da, die Seiten zeigen sie nicht; der
Vertrag (`contract/openapi.yaml` 0.2.0) ist enger als Modul und Server.

**Phasen** (Repo go-fabric-ui, eigenes Konzept U3 dort, wie U1/U2; hier
nur die Modulseite und die Vertragsfolge)

| Phase | Inhalt | Beweis |
| --- | --- | --- |
| U3 | **Topologie.** Node-Familie: Graph Aggregator → Bridged Devices → Parts, mit Fabrics und ihren Sessions/Subscriptions als zweiter Ebene (Daten: `Topology()`, `FabricInfos`, `SessionDiagnostics`, `Subscriptions().CountBySession` — alle vorhanden). Controller-Familie: Netzgraph aus `get_network_topology` (Thread-Rolle/RLOC16/LQI/RSSI, Wi-Fi SSID/BSSID) — heute WS-only, kommt in den Vertrag. Graph-Bibliothek: §E.4. | Vertrag 0.3.0 (additiv), Golden-Fixtures, vitest |
| U4 | **Gerätedetails.** Je Endpoint: Gerätetyp mit Revision, Cluster mit Revision/FeatureMap, Attribut-Inventar mit Live-Werten (SSE `attribute_updated`), Events, Parts, Tags, Erreichbarkeit, die Surface-Entscheidung des Hosts; Controller-Seite: Inventar-Tab statt Einzelpfad-Reader, Event-Log, dekodierte FeatureMaps. Modulseite: `node.Observers.OnAttributeChanged` existiert? — prüfen; sonst `EventFiring`-Seam (`im/subscription/manager.go:746`) als Quelle. | Vertrag 0.3.0; Node- und Controller-Detailseite |
| U5 | **UX und Betrieb.** Live-Status statt Reload, Diagnose-Seite mit Log-Level, Log-Stream und `ping_node`; Kommissionierungsfluss mit Kamera-QR-Scan (heute Kopfkommentar „camera not used“); Ökosystem-Hinweise (Apple/Google) im Pairing; der Server zeigt die Node-Seiten für jeden Node, nicht nur die Bridge; die Selection-Seite bekommt im Server einen Grund oder einen Inhalt. | Playwright-e2e je Seite (heute keins), README/Vertragsversion aktuell |

**Besser als vorher, besser als andere — stimmt der Schnitt?** Vorher
Seiten je Ressource; nachher Seiten je Frage („Wer ist mit wem
verbunden?“, „Was tut dieses Gerät jetzt?“). Gegen home-assistant-matter-hub
(15 Routen, ReactFlow-Netzkarte Hub → Bridge → Gerät → Fabric, 906-Zeilen-
Endpoint-Karte, Health je Fabric): Dessen Karte ist Besitzstruktur, nicht
Funk; unsere Controller-Topologie hat die Funkdaten, die matter-hub nicht
hat. Der Schnitt bleibt: Svelte-Bibliothek + Go-Modul mit Bundle; der
Vertrag ist die einzige Grenze; der Server bringt WS-Daten in den
HTTP/SSE-Vertrag statt die UI an den WS zu binden.

### C.3 Milestone 10 — Rest der Norm (Standard)

**Befund** (§A.3). Keine Cluster-Lücke; Feature-Lücken in zwei Klassen.

**Phasen**

| Phase | Inhalt | Beweis |
| --- | --- | --- |
| 36 | **Paritäts-Features** (matter.js hat sie): DoorLock FACE (`DoorLockServer.ts:84-96`), AdministratorCommissioning BC (`AdministratorCommissioningServer.ts:106-116`; Finding :551-555), OccupancySensing OTHER/US/PHY/AIR/RAD/RFS/VIS/OCCEVENT mit `occupancyChanged` (`OccupancySensingServer.ts:89`), PowerSource WIRED/RECHG/REPLC (`PowerSourceServer.ts:13-15`). | Vergleichszeilen ✅; Familien DRLK/CADMIN/OCC/PS |
| 37 | **Konsumenten-Features** (chip als Autorität, Ahead-Zeilen): Auswahl §E.3 aus Descriptor TAGLIST (Apple Home: semantische Tags an zusammengesetzten Geräten; Finding :609, `BD-Matter-ClosureWithoutTagList`), Konzentrations-LEV/MED/CRI/PEA/AVG und AirQuality-Stufen (CO₂-/Luftsensoren), ClosureControl LT/IS/SP/CL (Tore, Kalibrierung), Valve LVL/TS (Ventile), Thermostat TSUGGEST, GeneralCommissioning TC, LevelControl FQ, ColorControl-Primaries. | Ahead-Zeilen, chip-Familien grün |
| 38 | **Protokollreste**, konsumentengetrieben: OTA-Requestor über HTTPS, BTP-Neuverhandlung nach Stall (`NobleBleChannel`), Trusted-Time-Source-Client, Fabric-Sync-Rest; ARL und NFC bleiben Nicht-Ziele (kein Konsument; matter.js hat NFC nicht). | Vergleichszeilen |
| 39 | **Pin 1.7.** Wenn matter.js' Snapshot auf 1.7 geht (chips `data_model/1.7`, winter2027: Humidistat, Electrical Distribution/Alarm/Protection Alarm, Proximity Ranging, AV Analysis, Network Identity Management, Commissioning Proxy, Media File Management, Audio Control, Ambient Sensing Union, Thermostat Mode): Pin-Lift, Generatoren, `chipdm-check` 1.7. Vorher nichts. | wöchentlicher `matterjs-pin`-Job meldet den Sprung |

**Besser als vorher, besser als andere — stimmt der Schnitt?** Die
Prämisse „alles, was matter.js hat“ bleibt der Maßstab für Phase 36; Phase
37 geht darüber hinaus nur mit Konsument und chip-Quelle (Ahead-Register,
Workflow-Schritt 5). Milestone 7 (BLE-Geräteseite) bleibt ein eigener
Milestone; §E.1 ordnet ihn ein.

## D. Entscheidungen, die das Konzept trifft

1. Drei Milestones 8/9/10 mit den Phasen 29–39 (U3–U5 im UI-Repo); eine
   Minor-Version je Milestone; Milestone 9 trägt die Version von
   go-fabric-ui (0.3.0) und die Vertragsversion.
2. Die „manuell, nichts zu automatisieren“-Einstufung wird in
   `certifiability.md` zu „manuell, von matter.js automatisiert (TS-Datei)“
   für die 45 Fälle, bis Phase 29 sie läuft.
3. Register-Wurzeln werden um die sechs Verzeichnisse aus Phase 33
   erweitert; der Rest wird mit Grund ausgeschlossen.
4. Sicherheitspfade in den Portierungen (Certificates, PQC,
   SessionManager, crypto) sind Fable-Arbeit; die übrigen Tranchen Opus;
   Lesekarten Sonnet (CLAUDE.md-Modellregel).
5. Keine vierte Testform: keine Property-/Mutation-Tests als eigenes
   Register; Fuzz-Ziele kommen dort hinzu, wo eine Tranche einen Decoder
   berührt.

## E. Owner-Entscheidungen (2026-11-02)

1. **Reihenfolge: M8 → M10 → M9 → M7.** Erst die Beweise, dann die
   Feature-Reste auf der gesicherten Basis, dann die UI mit Vertrag 0.3.0,
   die BLE-Geräteseite zuletzt (kein Konsument).
2. **Phase 29 treibt die matter.js-Fälle mit dem Go-Controller** im Harness
   (die TH-Rolle wie matter.js' `certTest`); chip-tool nur dort, wo
   matter.js es selbst nutzt.
3. **Phase 37 nimmt alle vier Gruppen:** Descriptor TAGLIST; Konzentrations-
   LEV/MED/CRI/PEA/AVG und AirQuality-Stufen; ClosureControl LT/IS/SP/CL und
   Valve LVL/TS; Thermostat TSUGGEST, GeneralCommissioning TC, LevelControl
   FQ, ColorControl-Primaries — jede als Ahead-Zeile mit chip-Quelle.
4. **U3 mit eigenem SVG**, ohne Graph-Bibliothek (passt zum Kit; eine
   Welle mehr, keine Abhängigkeit).

## F. Was das Konzept nicht tut

- Keine Änderung der Prämisse, der Säulen oder der Modellregel.
- Keine Implementierungsdetails: jede Phase bekommt ihr eigenes Konzept
  oder ihren Brief, wenn sie dran ist (`docs/roadmap.md` §„How a phase is
  planned“).
- Kein v1.0.0-Termin.
