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
Checkout `../matter.js` am 2026-10-12. Seit Welle 11 ist `0ceb202d` der
Pin, den `parity/schema.json` und jede Zeilenangabe im Repo tragen.

---

## 0. Entscheidungen aus dem Wizard (2026-10-12)

| # | Frage | Empfehlung war | **Entscheidung** |
| --- | --- | --- | --- |
| 0.1 | Initiator-Rolle | Minimal-Controller | **Voller Commissioner**: Discovery, PASE/CASE-Client, Attestation-Prüfung, NOC-Ausstellung, Client-Interaktion, Binding-Client, Groupcast-Sender, Joint Fabric; DCL-Client als letzte Stufe. Konsument: **beides**, ein Controller-Daemon als Produkt und das Harness-Werkzeug (ersetzt chip-tool und den matter.js-Controller für die BIND- und Controller-Fälle). |
| 0.2 | Media/Content | nur mit Konsument | **Ja, jetzt generiert** (15 Cluster, 3 Gerätetypen). |
| 0.3 | OTA-Provider, DLOG über BDX | Provider ja, DLOG nein | **Beides.** DLOG über BDX geht über matter.js hinaus; Referenz ist dann chip (`src/app/clusters/diagnostic-logs-server`), wie CLAUDE.md es für Verhalten vorsieht, das matter.js nicht modelliert. `BD-chip-DiagLogs-NoBDX` entfällt. |
| 0.4 | Netzwerk | Diagnose + Interface | **Diagnose + Interface.** WiFiNetworkDiagnostics mit Host-Port, Gerätetyp SecondaryNetworkInterface; NetworkCommissioning bleibt Ethernet-only. |
| 0.5 | ICD vollständig | nein | **Nein.** |
| 0.6 | BLE, Thread | Nicht-Ziele | **Bleiben Nicht-Ziele.** |
| 0.7 | Kamera + TLS | nein, bis Kamera-Host | **Nein, bis ein Kamera-Host existiert.** |
| 0.8 | ARL | endgültig nein | **Endgültig nein.** `BD-Matter-ARL-NotMounted` wird als „nicht in matter.js“ umklassifiziert. |
| 0.9 | matter.js-Pin | heben, als erste Phase | **Ja, als erste Phase** auf `0ceb202d`. |
| 0.10 | Reihenfolge | Pin, Controller, OTA/DLOG, Media, Wi-Fi | **Pin, Commissioner, OTA/DLOG, Media, Wi-Fi.** |
| 0.11 | Version | nach `v1.0.0` | **Vor `v1.0.0`.** `v1.0.0` erst mit Commissioner, OTA-Provider und Media; Phase 5 (Apple Home, Alexa) läuft parallel, sobald der Owner am Gerät ist. |

Zwei Spannungen aus diesen Entscheidungen müssen benannt werden.

**Erstens: der Commissioner ist die größte Scope-Bewegung seit dem Start.**
CLAUDE.md, `docs/matterjs-comparison.md` §Non-goals, ADR 0008 und Konzept
v1 F.1 nennen „keine Controller-/Commissioner-Rolle“ als Entscheidung,
nicht als Backlog. Die Entscheidung 0.1 hebt sie auf. Das braucht eine ADR
(0020), die die alte Grenze als überholt markiert und die neue zieht: der
Commissioner ist ein eigenes Paket mit eigener Zustandsmaschine, die
Responder-Seite (`bridge/`, `node/`, `device/`) bleibt davon unberührt,
und `contract/` bleibt stdlib-only. Was der Commissioner mit dem Responder
teilt (`secure/`, `tlv/`, `im/`-Codecs, `mdns/`, `transport/`), wird
dabei zum ersten Mal von zwei Rollen benutzt; jede Änderung dort läuft
weiter über die Responder-Guards.

**Zweitens: `v1.0.0` rückt um die Dauer der Phasen 6 bis 9.** Das
Einfrieren von `device/` und `node/` verschiebt sich; Konsumenten (openccu-
loom) leben solange auf `v0.x`-Garantien. Der Vorteil: `v1.0.0` enthält
dann beide Rollen und die Media-Breite, und ein späteres `v2.0.0` für den
Commissioner entfällt.

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

### B.1 Commissioner und Controller (Paket `controller/`, Arbeitstitel)

**Was fehlt.** Alles, was in `docs/matterjs-comparison.md` §1–§3 mit
„Controller“ markiert ist: Discovery kommissionierbarer und operativer
Nodes, PASE- und CASE-Client, Attestation-Prüfung gegen einen PAA-Store,
Zertifikatsausstellung (RCAC, NOC), der Commissioning-Flow (ArmFailSafe →
CSR → AddTrustedRoot → AddNOC → NetworkCommissioning → CommissioningComplete),
Client-Interaktion (Read, Write, Invoke, Subscribe mit Wiederaufnahme),
Peer-Verwaltung mit Adress-Überwachung, Binding-Client, Groupcast-Sender,
Joint Fabric (3 Cluster) und der DCL-Client.

**Was da ist.** `secure/sigma.NewPeerInitiator`, `bridge/case_initiator.go`
(ADR 0008), `bridge/bridgetest.Commissioner` (#53: PASE → ArmFailSafe →
Attestation/CSR → AddTrustedRoot → AddNOC → CASE → CommissioningComplete,
mit Test-CA; `Dial/Commission/PASE/CASE/CASEAs/RemoveFabric`,
`Session.Read/Write/Invoke/Subscribe`), `secure/mattercert` (Codec),
`secure/attestation` (Geräteseite), `mdns/` (Advertiser, kein Browser),
`commissioning/` (Responder-Seite von PASE), der matter.js-Controller im
Harness (`internal/chiptool/controller_test.go`).

**Form.** Ein Paket `controller/` außerhalb von `bridge/`, nach matter.js
in Stufen:

| Stufe | Port von | matter.js-Zeilen | Ergebnis |
| --- | --- | --- | --- |
| 1 Transportclient | `CommissionableMdnsScanner.ts` (593), `common/Scanner.ts` (330), `PaseClient.ts` (229), `CaseClient.ts` (402), `peer/PeerConnection.ts` (866), `PeerSet.ts` (350), `Peer.ts` (759), `PeerAddressMonitor.ts` (293) | 3 822 | DNS-SD-Browsing, Sessions zu beliebigen Nodes, Adress-Verfolgung |
| 2 Commissioning | `ControllerCommissioningFlow.ts` (2 141), `ControllerCommissioner.ts` (746), `CommissioningConnection.ts` (313), `DeviceCommissioner.ts` (376), Zertifikatsausstellung (RCAC/NOC, heute als Test-CA in `bridgetest`), Attestation-Prüfung mit PAA-Store | 3 576 + CA | ein Node wird kommissioniert; der Harness nutzt es statt chip-tool |
| 3 Client-Interaktion | `action/client/` (3 201: `ClientInteraction.ts` 1 294, Subscriptions 1 222, `InputChunk.ts` 432) | 3 201 | Read/Write/Invoke/Subscribe mit Chunking und Wiederaufnahme |
| 4 Client-Node-Modell | `ClientNode.ts` (409), `node/client/` (4 273: `ClientStructure.ts` 1 692, `Peers.ts` 966, `PeerBehavior.ts` 507) | 4 682 | typisiertes Abbild entfernter Nodes aus dem Schema; `device/`-Typen als Client-Sicht |
| 5 Binding, Groupcast, Controller-Typen | `behaviors/binding/` (998), Sender-Anteil von `GroupcastServer.ts` (940), 11 Controller-Gerätetypen im Generator | ~1 500 | Binding wirkt, BIND-Familie, Groupcast-Sender-Feature |
| 6 Joint Fabric, DCL | `joint-fabric-*`, `commissioner-control` (3 Cluster), `protocol/src/dcl/` (4 691) | ~5 500 | fabric-übergreifende Administration; DCL-Prüfung von Attestation und OTA-URLs |

Stand 2026-10-13: Stufe 1 ist zweigeteilt — `controller/discovery`,
`controller/exchange` und `controller/handshake` (Scanner, Exchange-Schicht,
PASE- und CASE-Client) in Welle 12 (#73), die Peer-Verwaltung
(`PeerConnection.ts`, `PeerSet.ts`, `Peer.ts`, `PeerAddressMonitor.ts`) als
Brief AG in Welle 13 (#81). Stufe 2 (`controller/ca`, `controller/attestation`, `controller/interaction` als Invoke/Read-Minimum, `controller/commissioner`) als Brief AH in Welle 14 (#83): der eigene Commissioner kommissioniert den Referenz-Daemon über Multicast-DNS-SD und liest über CASE. Stufe 3 (`controller/interaction` mit Write, Subscribe, Subscription-Registry, Sustained Subscriptions und eingehenden Reports; der CASE-Responder `controller/handshake.CASEServer`; die Client-Encoder in `im/`) als Brief AI in Welle 15 (#90): der eigene Controller abonniert den Referenz-Daemon und empfängt nach dessen Neustart die wiederhergestellte Subscription über die vom Daemon geöffnete CASE-Session. Brief AJ (Welle 16, #93) hat `bridgetest.Commissioner` auf `controller/` verschoben (Oberfläche unverändert, Stufe 2 damit abgeschlossen). Stufe 4 ist zweigeteilt: die erste Hälfte (`controller/client` — das Descriptor-getriebene Abbild eines entfernten Nodes mit Cache und `Store`-Seam, die Cluster-Shape-Analyse aus `PeerBehavior.ts` statt generierter Typen, die Default-Sustained-Subscription mit DataVersion-Filtern und Event-Cursor, Verbindungszustand, physische Eigenschaften und Intervall-Grenzen; dazu `peer.Peer.Observe`, `interaction.SubscribeRequest.RefreshRequest`, `spec.DecodeValue`) als Brief AK in Welle 17 (#95); die zweite Hälfte (`Peers.ts`: Registry mit `peerN`-Ids, Wiederherstellung beim Start, Adressbuch mit Ablauf, Leave/Shutdown/StartUp-Reaktionen, Fabric-Label-Abgleich, SQLite-Store) als Brief AL in Welle 18 (#97): Stufe 4 damit abgeschlossen. Die `device/`-Typen als Client-Sicht bleiben ein späterer Brief. Phase 10 (B.4) läuft als Brief AM parallel in Welle 19.

Gesamt rund 22 000 matter.js-Zeilen. Go-Einschätzung 25 000–35 000 Zeilen
mit Tests, sechs bis acht Wellen. Stufe 6 ist abtrennbar; der DCL-Client
ist ein HTTP-Client gegen die CSA-Ledger-API und zieht keine Abhängigkeit
(`net/http`).

**Konsumenten.** (1) Ein Controller-Daemon als Produkt (außerhalb von
go-fabric, wie der Adapter-Daemon aus Konzept v1 Teil C; Arbeitstitel
`go-matter-controller`): kommissioniert, hält Peers, abonniert, bietet die
Geräte einem Host an. (2) Das Harness: `internal/chiptool` kommissioniert
den Referenz-Daemon heute mit chip-tool und dem matter.js-Controller; der
eigene Commissioner wird dritte Gegenstelle und trägt die BIND- und
Controller-Fälle. `bridgetest.Commissioner` ist seit Welle 16 ein
Wrapper über `controller/`; seine Test-CA ist in `controller/ca`
aufgegangen (der feste Ausstellungszeitpunkt bleibt by_design).

**Öffnet.** BIND (3), 11 Gerätetypen, Groupcast-Sender, Joint Fabric;
Parität in `docs/matterjs-comparison.md` §1–§3 auf ✅.

**Grenze (ADR 0020).** Kein BLE, kein Thread, kein Wi-Fi-Commissioning
über BLE: Nodes werden über IP kommissioniert (on-network, wie chip-tool
`pairing onnetwork`). Kein eigenes Gerätemodell im Controller-Daemon; die
Client-Sicht kommt aus `device/` und dem Schema.

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

**DiagnosticLogs über BDX** (0.3): matter.js hat keinen Transfer, chip
schon (`src/app/clusters/diagnostic-logs-server/`, BDX-Sender bei
`RetrieveLogsRequest` mit `TransferFileDesignator`). Port nach chip, Zitat
auf chip, `BD-chip-DiagLogs-NoBDX` entfällt; DLOG/2.1 bleibt manuell,
läuft aber im Harness mit dem eigenen Commissioner (B.1 Stufe 3) als
Empfänger.

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
**Risiko.** Ohne Konsument kein Ecosystem-Nachweis; ob Apple Home
Media-Geräte hinter Bridges zeigt, ist in Phase 5 zu prüfen. Entscheidung
0.2: jetzt generiert, Nachweis über chip-Familien und den eigenen
Controller.

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
| ARL | nicht in matter.js | Parität verlangt es nicht; endgültig (0.8) |

---

## Teil C: Phasen

| Phase | Inhalt | ADR | Abschluss |
| --- | --- | --- | --- |
| 5 | Apple Home, Alexa mit dem Referenz-Daemon; parallel zu 6–10, sobald der Owner am Gerät ist | — | Befunde in `docs/matter-ecosystem-observations.md` |
| 6 | matter.js-Pin auf `0ceb202d` (Snapshot-Diff klassifiziert, Generatoren, Zitate geprüft); `certifiability.md` §„Families“ korrigiert; Register Tier 1/2 abgearbeitet | — | `chipdm-check` grün; Register ohne Tier 1/2  **Erledigt in Welle 11 (2026-10-13; Briefs AC, AD; Register Tier 1/2 bis auf CountryCode geschlossen).** |
| 7 | Commissioner Stufen 1–5 (B.1), sechs bis acht Wellen; Stufe 6 (Joint Fabric, DCL) als eigene Welle am Ende | 0020 | Harness kommissioniert mit dem eigenen Commissioner; BIND grün; 74 Gerätetypen |
| 8 | OTA-Provider und DLOG über BDX (B.2) | 0021 (ergänzt 0019) | SU-Provider-Fälle und DLOG-Transfer gegen den eigenen Requestor/Commissioner |
| 9 | Media/Content (B.3) | 0022 | 116 Cluster, 77 Gerätetypen; Media-Familien |
| 10 | Wi-Fi-Diagnose, SecondaryNetworkInterface (B.4) | — | DGWIFI grün  **Erledigt in Welle 19 (2026-10-14; Brief AM, #99): DGWIFI 2.1 und 2.3 grün mit dem simulierten Reporter des Referenz-Daemons, `BD-Matter-WiFiHost-NetworkCommissioningEthernet`.** |
| `v1.0.0` | nach 7–9; friert `device/`, `node/` und `controller/` | — | README §API stability |

Querschnitt wie in v1: `make chipdm-check`, Parity-Guards, Ratchets,
CHANGELOG. Jede Phase mit ADR ergänzt `docs/matterjs-comparison.md`
§Non-goals und CLAUDE.md §„Scope boundaries“.

---

## Teil D: Risiken

| Risiko | Folge | Gegenmaßnahme |
| --- | --- | --- |
| Zwei Rollen in einem Modul | `secure/`, `im/`, `mdns/` werden von Responder und Controller benutzt; eine Controller-Änderung bricht die Responder-Parität | `controller/` importiert, `bridge/` importiert nicht zurück; jede gemeinsame Änderung läuft über die Responder-Guards und die chip-tool-Suite |
| Aufwand des Commissioners (25 000–35 000 Go-Zeilen) | `v1.0.0` rückt um Monate | Stufen 1–3 zuerst, jede mit Harness-Nachweis; Stufe 6 abtrennbar |
| Media ohne Konsument | tote API, PICS-Lücken | Nachweis über chip-Familien und den eigenen Controller; Host-Ports klein halten |
| Pin-Hebung verschiebt 1.6.1-Elemente | Zitate und Snapshot drift | Snapshot-Diff vor der Generierung lesen; `chipdm-check` |
| Apple Home zeigt Controller-Typen oder Media nicht | Nachweis fehlt | `docs/matter-ecosystem-observations.md` vor der Entscheidung 0.2 |

---

## Teil E: Nächste Schritte

1. ADR 0020 „Commissioner and controller role“ (Proposed) mit der Grenze
   aus B.1 und den Konsumenten; CLAUDE.md, `docs/matterjs-comparison.md`
   §Non-goals, ADR 0008 und Konzept v1 F.1 erhalten je eine Notiz
   „überholt durch 0020“.
2. Phase 6 als Welle 9: Pin heben, `certifiability.md` korrigieren,
   Register Tier 1/2. Briefs nach dem Wellen-Schema in `notes/briefs/`.
3. Phase 7 Stufe 1 als Welle 10, mit einem Brief je Stufe; die
   Stufen-Tabelle in B.1 ist die Brief-Liste.
