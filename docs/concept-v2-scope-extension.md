# Konzept v2: Scope-Erweiterung nach „Matter 1.6.1 vollständig“

Stand 2026-10-12, go-fabric `v0.3.0` plus #55/#56. Fortsetzung von
[`concept-matter-1.6.1-and-device-layer.md`](./concept-matter-1.6.1-and-device-layer.md)
(Konzept v1). Sprache ist Deutsch, weil es ein Planungsstand ist; die ADRs
daraus sind Englisch.

Dieses Dokument hat vier Teile:

- **Teil 0** sind die Wizard-Fragen, die der Owner beantwortet. Jede trägt
  die Optionen, was jede Option kostet und öffnet, und eine Empfehlung.
- **Teil A** misst, was Konzept v1 erreicht hat und was heute noch fehlt,
  getrennt nach matter.js-Parität und chip-Zertifizierbarkeit.
- **Teil B** beschreibt jeden Erweiterungskandidaten: was fehlt, das Maß in
  matter.js, was er öffnet, wovon er abhängt.
- **Teil C** ordnet die gewählten Kandidaten in Phasen und nennt die ADRs.

Jede Zahl trägt ihre Quelle. matter.js-Zeilen sind `wc -l` auf dem lokalen
Checkout `../matter.js` am 2026-10-12. **Achtung:** der Checkout steht seit
dem Pull von 11:57 auf `0ceb202d`, 65 Commits vor dem Pin `4633bfd7`, den
`parity/schema.json` und jede Zeilenangabe im Repo tragen. Die Maße hier
sind Größenordnungen; Zeilenzitate im Code bleiben auf dem Pin, bis Frage
0.9 entschieden ist.

---

## 0. Wizard-Fragen

Antwortformat wie in Konzept v1: pro Frage eine Entscheidung, die Tabelle
wird nach dem Wizard mit der Spalte „Entscheidung“ ergänzt.

| # | Frage | Optionen | Empfehlung |
| --- | --- | --- | --- |
| 0.1 | **Initiator-Rolle: wie weit?** | (a) keine (Stand heute, nur ADR 0008); (b) **Minimal-Controller**: CASE zum gebundenen Peer, Invoke/Read/Subscribe, Binding wirkt, Groupcast-Senden, die 11 Controller-Gerätetypen; (c) voller Commissioner: Discovery, Attestation-Prüfung, NOC-Ausstellung, Joint Fabric, DCL. | **(b).** Größter Hebel für Parität und Zertifizierbarkeit (BIND, Controller-Typen) auf vorhandenem Unterbau (`secure/sigma.NewPeerInitiator`, `bridge/case_initiator.go`). (c) ist nach Konzept v1 F.1 „ein anderes Produkt“ und bleibt es. |
| 0.2 | **Media/Content (15 Cluster, 3 Gerätetypen): aufnehmen?** | (a) nein; (b) ja, als generierte Tranche ohne Konsumenten; (c) ja, sobald ein Host ein Media-Gerät bridgen will. | **(c), vorbereitet als (b)-fähig.** In matter.js sind alle 15 Server reine Stubs; der Aufwand liegt in `device/`-Typen und Host-Ports. Ohne Konsument gibt es keinen Ecosystem-Nachweis. |
| 0.3 | **OTA-Provider und DiagnosticLogs über BDX?** | (a) nein; (b) OTA-Provider ja, DLOG nein (matter.js-Parität); (c) beides. | **(b).** Der Provider ist in matter.js ein 668-Zeilen-Server auf dem vorhandenen `bdx/`; DLOG über BDX hat matter.js nicht (`BD-chip-DiagLogs-NoBDX` bleibt), und DLOG/2.1 ist manuell. |
| 0.4 | **Netzwerk: Wi-Fi-Diagnose, Wi-Fi-Commissioning, SecondaryNetworkInterface?** | (a) nichts; (b) WiFiNetworkDiagnostics und SecondaryNetworkInterface als Host-Port (die Bridge läuft auch auf Wi-Fi-Hosts); (c) zusätzlich NetworkCommissioning Feature WI mit BLE. | **(b).** Diagnose ist ein generierter Server mit Host-Port. (c) setzt BLE voraus (0.6). |
| 0.5 | **ICDManagement vollständig (inkl. Check-in-Sender)?** | (a) nein; (b) ja. | **(a).** Eine netzbetriebene Bridge ist kein ICD; ein Controller hielte den Node für schläfrig. Nur mit einem Batteriegerät auf go-fabric, das ohne Thread/BLE nicht entsteht. ICDM (7) und ICDB bleiben (b)/(d). |
| 0.6 | **BLE/BTP und Thread?** | (a) bleiben Nicht-Ziele; (b) BLE als eigenes Modul (BlueZ) für Wi-Fi-Commissioning; (c) auch Thread-BR-Client. | **(a).** matter.js trägt dafür `nodejs-ble` (2 092 Zeilen, Noble/Bleno) und `thread-br-client` (10 754); beides zieht Plattform-Stacks in ein Modul mit einem UDP-Socket als einziger OS-Abhängigkeit. |
| 0.7 | **Kamera (7 Cluster) + TLS (2) + 6 Gerätetypen?** | (a) nein; (b) ja, mit konkretem Kamera-Host. | **(a) bis ein Host existiert.** matter.js hat keinen WebRTC-Stack; außer `WebRtcTransportRequestorServer` (209 Zeilen) sind die Server Stubs. Der Medienpfad ist vollständig Host-Sache. |
| 0.8 | **ARL (Managed Aggregator)?** | (a) nein; (b) ja. | **(a), endgültig.** matter.js implementiert ARL nicht (`AccessControlServer.ts` kennt keine Access Restriction); Parität verlangt es nicht. `BD-Matter-ARL-NotMounted` wird in „nicht in matter.js“ umklassifiziert. |
| 0.9 | **matter.js-Pin heben?** | (a) bleibt `4633bfd7`; (b) auf `0ceb202d` (65 Commits), Snapshot regenerieren, Zeilenzitate prüfen. | **(b) als erste Phase**, bevor neue Zitate entstehen. Eine Review-Entscheidung nach CLAUDE.md; der Diff des Snapshots zeigt, ob 1.6.1-Elemente wanderten. |
| 0.10 | **Reihenfolge** | (a) Parität zuerst (0.1, 0.3, 0.2); (b) Zertifizierbarkeit zuerst (0.1, 0.5, 0.4); (c) Konsumenten zuerst (0.2 nur mit Host). | **(a)** mit 0.9 davor: Minimal-Controller, dann OTA-Provider, dann Wi-Fi-Diagnose, Media bei Bedarf. |
| 0.11 | **Version** | Erweiterungen vor oder nach `v1.0.0`? | **Nach.** `v1.0.0` friert `device/` und `node/` nach Phase 5 (Apple Home, Alexa). Jede Erweiterung ist additiv (`v1.x`), die Initiator-Rolle bekommt ein eigenes Paket statt neuer Felder in `node.Config`. |

---

## Teil A: Stand

### A.1 Konzept v1

| Phase | Stand |
| --- | --- |
| 0 Fabrik (ADR 0017) | erledigt, #40 |
| 1 Protokoll-Rest | erledigt, #41 |
| 2 Cluster-Breite 2a–2d | erledigt, #42–#47 (101 Server) |
| 3 TCP, BDX, OTA-Requestor (ADR 0019) | erledigt, #49 |
| 4 `device/` und `node/` (ADR 0018) | erledigt, #51 (63 Typen) |
| 5 Nachweis und `v1.0.0` | **offen**: Apple-Home- und Alexa-Läufe brauchen den Owner am Gerät |

Danach kamen Welle 7 (#53: Test-Commissioner in `bridge/bridgetest`,
Tier-3-Transportbefunde, `v0.3.0`) und Welle 8 (#55: Session-Schluss-
Befunde). Der Test-Commissioner ist bewusst **kein** Controller: er hat
keinen Resolver, keine Retransmission, eine feste Test-CA
(`BD-Matter-Commissioner-TestCA`).

### A.2 Was zur matter.js-Parität fehlt (gemessen)

`docs/matterjs-comparison.md` führt jede Zeile mit ○ oder ◐; Konzept v1
Anhang F nennt die Begründung. Zusammengefasst nach Bereichen, mit dem Maß
in matter.js (Zeilen, `../matter.js` am 2026-10-12):

| Bereich | matter.js | Maß |
| --- | --- | --- |
| Controller: Peer-Verwaltung, Commissioning-Flow | `protocol/src/peer/` | 7 105 (davon `ControllerCommissioningFlow.ts` 2 141, `PeerConnection.ts` 866, `Peer.ts` 759) |
| Controller: Client-Interaktion | `protocol/src/action/client/` | 3 201 (`ClientInteraction.ts` 1 294, Subscriptions 1 222) |
| Controller: Node-Fassade | `node/src/node/ClientNode*.ts`, `node/client/` | 4 790 (`ClientStructure.ts` 1 692, `Peers.ts` 966) |
| Controller: Handshake-Initiatoren | `CaseClient.ts`, `PaseClient.ts`, `DeviceCommissioner.ts` | 402 + 229 + 376 |
| Controller: Discovery | `CommissionableMdnsScanner.ts`, `common/Scanner.ts` | 593 + 330 |
| DCL-Client | `protocol/src/dcl/` | 4 691 |
| Binding-Client | `behaviors/binding/` | 998 (`BindingManager.ts` 737) |
| Groupcast-Sender | `behaviors/groupcast/GroupcastServer.ts` (Sender-Anteil) | 940 gesamt |
| OTA-Provider | `behaviors/ota-software-update-provider/` | 668 Server + BDX-Sendeflüsse (208) |
| Media/Content | 15 `*Server.ts` | je 14 Zeilen (Stubs); Gerätetypen 155/189/151 |
| Kamera + TLS | 9 `*Server.ts` | Stubs außer `WebRtcTransportRequestorServer.ts` 209; kein WebRTC-Stack in matter.js |
| ICD | `protocol/src/icd/`, `behaviors/icd-management/` | 1 051 + 1 043 |
| Netzwerk-Diagnose, Thread-Cluster | 6 `*Server.ts` | je 14 Zeilen (Stubs) |
| BLE, Thread-BR | `nodejs-ble/`, `thread-br-client/` | 2 092 + 10 754 |
| ARL | — | nicht in matter.js |

Außerhalb des Scopes, aber offen, bleiben die Register-Einträge:
11 Tier-1/2-Befunde, 11 Application-Server-Befunde und die Harness-Befunde
(TC-EEVSE-2.3, WNCV Maintenance, ColorControl XY,
`notes/parity/matter_behaviour_findings.md`). Sie sind keine
Scope-Erweiterung und laufen als Wellen weiter.

### A.3 Was zur chip-Zertifizierbarkeit fehlt

`docs/open-items.md`: 608 Fälle, 351 bestanden, 111 per PICS nicht
unterstützt (b), 140 Harness/manuell (c, 126 manuell), 5 außerhalb (d),
1 Defekt (a). Die 126 manuellen Fälle bleiben in jedem Scope. Was eine
Erweiterung öffnet, nach Familie (Zeilen im Statusblock von
`docs/certifiability.md`):

| Familie | Fälle | öffnet durch |
| --- | --- | --- |
| BIND | 3 | Minimal-Controller (0.1 b): `BIND.C` ist die Client-Rolle |
| SU Provider-Fälle | Teil von 10 | OTA-Provider (0.3) |
| ICDM, ICDB | 7 + Familie | ICD (0.5) |
| CNET Wi-Fi/Thread | Teil von 21 | Wi-Fi-Commissioning (0.4 c) bzw. Thread (0.6) |
| DD, SC BLE-Fälle, SC_TC | Teil von 28 + 18 | BLE (0.6) |
| DLOG/2.1 | 1 | manuell, bleibt (c) |
| Media-, Kamera-Familien | im Image, heute nicht gelistet | 0.2, 0.7 |

Produktseitig (VID/PID, DAC, CD, DCL-Eintrag, Laborlauf) bleibt außerhalb
der Bibliothek (`docs/certifiability.md` §„What a product owner must
supply“). Nebenbefund: §„Families this module does not run yet“ dort
nennt noch Energie-, Appliance- und Measurement-Cluster als „kein Server“
und BDX als nicht implementiert; der Abschnitt ist seit #47/#49 veraltet und
wird mit der ersten Phase dieses Konzepts korrigiert.

---

## Teil B: Die Kandidaten

### B.1 Minimal-Controller (Initiator-Rolle im Kleinen)

**Was fehlt.** Ein Endpoint kann keinen Client-Cluster deklarieren, die
Binding-Tabelle löst keinen Peer auf (`BD-Matter-Binding-StoreOnly`,
Finding „The default Binding server resolves no peer“), Groupcast sendet
nicht (`BD-Matter-GroupcastNoSender`), die 11 Controller-Gerätetypen
(OnOffLightSwitch, DimmerSwitch, ColorDimmerSwitch, ControlBridge,
OnOffSensor, DoorLockController, WindowCoveringController,
ThermostatController, PumpController, ClosureController,
VideoRemoteControl) sind aus `script/devicegen` ausgeschlossen.

**Was da ist.** `secure/sigma.NewPeerInitiator` und
`bridge/case_initiator.go` (ADR 0008: CASE zu einem früheren Subscriber,
`connectPeer`, `existingPeerSession`, `runCASEInitiator`), ein
`OperationalResolver`-Port, der matter.js-Resolver im Test-Commissioner.
`bridgetest.Commissioner` ist ein Testwerkzeug und bleibt es.

**Form.** Ein Paket `client/` (Arbeitstitel) mit: Peer-Adressauflösung
über DNS-SD (`_matter._tcp`, Port von `OperationalResolver`), CASE über den
verallgemeinerten ADR-0008-Initiator, ein `Invoke`/`Read`/`Subscribe`-Client
nach `ClientInteraction.ts` (nur der Teil, den Binding braucht: Invoke und
Read, keine Client-Struktur, kein `ClientNode`), ein `BindingManager` nach
`BindingManager.ts` (737 Zeilen), der bei einem Host-Ereignis die gebundenen
Ziele anspricht, und der Groupcast-Sender aus `GroupcastServer.ts`.
`device/` bekommt die 11 Controller-Typen aus dem Generator; ein Endpoint
deklariert Client-Cluster in der Descriptor-ClientList.

**Maß.** matter.js: rund 2 500 Zeilen (CaseClient 402, Binding 998,
Groupcast-Sender, der Invoke/Read-Teil von `ClientInteraction.ts`). Go:
Einschätzung 4 000–6 000 Zeilen mit Tests; zwei bis drei Wellen.

**Öffnet.** BIND (3), 11 Gerätetypen, Groupcast-Sender-Feature; schließt
zwei Findings und zwei by_design-Einträge.

**Abhängigkeiten.** Keine. Nicht enthalten: Commissioning anderer Nodes,
Attestation-Prüfung, NOC-Ausstellung, DCL, Joint Fabric. Das bleibt die
Grenze „kein Commissioner“; die ADR nennt sie neu.

### B.2 OTA-Provider

**Was fehlt.** `OtaSoftwareUpdateProvider` (0x0029) und der Gerätetyp
OtaProvider (0x0014); `BD-Matter-OTAProvider-NotExposed`.

**Was da ist.** `bdx/` (Empfänger und Initiator, 3 960 Zeilen mit Tests),
der Requestor als Gegenstück, `chip-ota-provider-app` im Harness-Image als
Referenz-Gegenstelle für den Requestor.

**Form.** Port von `OtaSoftwareUpdateProviderServer.ts` (668 Zeilen):
QueryImage mit Host-Port für das Image-Verzeichnis, ApplyUpdateRequest,
NotifyUpdateApplied; BDX-Sender (`OutboundFlow`, `DrivenSendingFlow`,
`FollowingSendingFlow`, 208 Zeilen) als Ergänzung zu `bdx/`. Der
Referenz-Daemon bekommt einen Provider-Endpoint mit einem Test-Image.

**Maß.** Go-Einschätzung 1 500–2 000 Zeilen; eine Welle.

**Öffnet.** SU-Provider-Fälle gegen den eigenen Requestor im Harness
(Device-zu-Device), OtaProvider-Gerätetyp. Nachweis: unser Requestor lädt
vom eigenen Provider.

### B.3 Media/Content

**Was fehlt.** 15 Cluster, drei Gerätetypen (BasicVideoPlayer,
CastingVideoPlayer, ContentApp); CastingVideoClient bleibt draußen
(Client-Rolle).

**Form.** Alle 15 matter.js-Server sind generierte Stubs; der
Standard-Server (ADR 0017) trägt sie direkt. Aufwand: `clustergen`-Liste,
`devicegen`-Liste, Host-Ports für MediaPlayback (Playback-Zustand), Channel,
KeypadInput, ApplicationLauncher; Referenz-Daemon mit einem Player-Endpoint.

**Maß.** Go-Einschätzung 2 000–3 000 Zeilen, überwiegend generiert; eine
Welle.

**Öffnet.** Media-Familien des Images; Parität um 15 Cluster.
**Risiko.** Ohne Konsument kein Ecosystem-Nachweis; Apple Home zeigt
Media-Geräte hinter Bridges nicht an (zu prüfen in Phase 5).

### B.4 Wi-Fi-Diagnose und SecondaryNetworkInterface

**Form.** `WiFiNetworkDiagnostics` als generierter Server mit Host-Port
(Signalstärke, BSSID, Zähler), `SecondaryNetworkInterface` (0x0019) als
Gerätetyp; `NetworkCommissioning` bleibt Ethernet-only. Eine halbe Welle.
**Öffnet.** DGWIFI-Fälle; die Bridge auf Wi-Fi-Hosts sagt die Wahrheit.

### B.5 Nicht empfohlen, mit Maß

| Kandidat | matter.js | Warum nicht |
| --- | --- | --- |
| ICD vollständig | 2 094 Zeilen, Check-in-Sender, Modi | kein Batteriegerät auf go-fabric |
| Kamera + TLS | 9 Stubs + 209, kein WebRTC-Stack in matter.js | Medienpfad vollständig Host-Sache; erst mit Kamera-Host |
| BLE/BTP | 2 092 + 674 | Plattform-Radio; einziger Nutzen ist Wi-Fi-Commissioning |
| Thread | 10 754 + 4 Stubs | Hardware oder Border-Router |
| Voller Commissioner, Joint Fabric, DCL | 7 105 + 4 790 + 4 691 | anderes Produkt |
| ARL | nicht in matter.js | Parität verlangt es nicht |

---

## Teil C: Phasen (Vorschlag, nach Wizard anzupassen)

| Phase | Inhalt | Abschluss |
| --- | --- | --- |
| 5 (v1) | Apple Home, Alexa; `v1.0.0` | `device/`, `node/` eingefroren |
| 6 | matter.js-Pin auf `0ceb202d` (0.9), `certifiability.md` §„Families“ korrigiert, Register-Befunde Tier 1/2 abgearbeitet | Snapshot-Diff klassifiziert; Register ohne Tier 1/2 |
| 7 | Minimal-Controller (B.1), ADR 0020 | BIND grün; 74 Gerätetypen; Binding-Findings geschlossen |
| 8 | OTA-Provider (B.2), ADR 0021 (ergänzt 0019) | SU Provider-Fälle gegen den eigenen Requestor |
| 9 | Wi-Fi-Diagnose (B.4) | DGWIFI grün |
| 10 | Media/Content (B.3), nur mit Konsument | Player-Endpoint im Referenz-Daemon, Media-Familien |

Querschnitt wie in v1: `make chipdm-check`, Parity-Guards, Ratchets,
CHANGELOG; jede Phase, die eine Scope-Grenze bewegt, bekommt eine ADR und
eine Zeile in `docs/matterjs-comparison.md` §Non-goals.

---

## Teil D: Risiken

| Risiko | Folge | Gegenmaßnahme |
| --- | --- | --- |
| Der Minimal-Controller wächst zum Commissioner | zweites Produkt im Modul | ADR 0020 zieht die Grenze: kein PASE-Client, keine NOC-Ausstellung, keine Discovery kommissionierbarer Nodes |
| Media ohne Konsument | tote API, PICS-Lücken | Phase 10 nur mit Host; sonst nicht |
| Pin-Hebung verschiebt 1.6.1-Elemente | Zitate und Snapshot drift | Snapshot-Diff vor der Generierung lesen; `chipdm-check` |
| Apple Home zeigt Controller-Typen oder Media nicht | Nachweis fehlt | `docs/matter-ecosystem-observations.md` vor der Entscheidung 0.2 |
