# Konzept v10 — Meilenstein 6: Vollständigkeit

Stand 2026-10-27. Faktenbasis: Lesekarte `read-m6-completeness.md`
(go-fabric `v0.13.3` = 9fb5dbe5, matter.js-Pin 862ad75f, chip 6170af84);
jede Zeile dort ist Karte, nicht Zitat — jedes Zitat in einem Brief oder in
`notes/parity/` wird am Pin nachgelesen. Sprache: Deutsch (Phasenkonzept);
Briefs, Issues, Doku, Register auf Englisch. Vorgänger: Konzept v8/v9
(Server-Familie, UI), Roadmap M6 (`docs/roadmap.md:274-291`), ADR 0017
(generierter Default-Server), ADR 0027 (Thread Border Router über OTBR REST,
angenommen 2026-10-25), ADR 0020 (Joint Fabric Stufe 6c).

## A. Was die Lesekarte ändert

1. **Phase 26 ist zur Hälfte gelandet.** Die 17 Cluster der Kamera-, Thread-,
   TLS-, Joint-Fabric-, Chime-, AmbientContext- und WaterTank-Gruppe haben
   seit Brief CL (Owner-Entscheidung 2026-10-25) je einen generierten Server
   (`script/clustergen/clusters.go:176-202`); acht der zwölf Gerätetypen sind
   unter `device/` generiert. Die Roadmap-Zeile 278 liest sich noch als
   Zukunft, Zeile 279 nennt ADR 0027 „proposed“, obwohl er angenommen ist.
2. **Verhalten hat nur chip.** matter.js' `*Server.ts` ist bei 16 der 17
   Cluster die 14-/18-Zeilen-Hülle `extends XBehavior {}`; die Ausnahme ist
   `WebRtcTransportRequestorServer.ts` (224 Zeilen, Session-Logik). chip hat
   Server für 15 der 17 (kein Verzeichnis für WaterTankLevelMonitoring; für
   AmbientContextSensing 944 Zeilen + Delegate). Alles jenseits der Hülle ist
   damit „Ahead of matter.js“ (Workflow-Schritt 5): Register-Zeile mit
   matter.js-Marker, chip als Verhaltensquelle, chips Tests als Parity-Fälle.
3. **Der Controller-Rest aus M5** steht im Findings-Register mit M6-Marke:
   Invoke-Batcher (`ClientInteraction.ts:185, :234, :765-829`; 39 Fälle),
   `Discovery`-Form (4 Testdateien, 1 315 Zeilen), gemeldete
   Session-Parameter (`NodeSession.ts:59, :136, :175`; 77 Zeilen), BDX-MRP-
   Verzögerung des Geräts, DCL-Vendor-Info-Service (497 Zeilen, 41 Fälle),
   PQC Phase 1 / NOC-Rotation. Dazu TEVT (Thermostat-Events) als Kandidat.
4. **Fehlende Gerätetypen:** VideoDoorbell und FloodlightCamera sind in
   matter.js leere Komposita (`SupportedBehaviors()`), bei uns in `excluded`,
   weil `devicegen` keine Komposita baut; CastingVideoClient verlangt
   Client-Cluster.

## B. Ziel von M6

Die Vergleichstabelle ohne ◐/○-Zeile, die jünger ist als ihr Meilenstein;
das Findings-Register leer; das Test-Register ohne `planned` (bis auf das,
was ein Owner-Ausschluss trägt). Jeder Cluster, den ein Gerätetyp der
Snapshot-Liste verlangt, hat ein Verhalten — matter.js', wo es eines gibt,
sonst chips, mit Register-Zeile.

## C. Phasen und Wellen

| Welle | Inhalt | Quelle | Release |
| --- | --- | --- | --- |
| **26-A1 Kamera-Signalisierung** | `WebRtcTransportRequestor` mit matter.js' Session-Logik (upsert/remove, NotFound, Events, Operate-ACL auf der ersten Fabric; `WebRtcTransportRequestorServerTest.ts` 605 Zeilen portiert); `Chime` (chip 307); `CameraAvSettingsUserLevelManagement` (chip 220 + Logic 1 288, Tests 1 037); `CameraController`-Gerätetyp vollständig | matter.js / chip | `v0.13.4` |
| **26-A2 Kamera-Streams** | `CameraAvStreamManagement` (chip 2 761 + Tests 1 929; Snapshot-, Video-, Audio-Feature-Sätze hinter einem Port, kein Medien-Stack im Modul), `ZoneManagement` (chip 738, Geometrie 323, Tests 446+142); Gerätetypen Camera, SnapshotCamera, Intercom, AudioDoorbell | chip | `v0.13.5` |
| **26-A3 Kamera-Transport** | `TlsCertificateManagement` + `TlsClientManagement` (chip 575+599+275, Tests 847+560, Fuzz 548) als Voraussetzung, dann `WebRtcTransportProvider` (chip 1 226, Tests 235) und `PushAvStreamTransport` (chip 189 + Logic 2 133, Tests 2 330+1 211+660) — je hinter einem Port, der Host liefert Medien und Zertifikate | chip | `v0.13.6` |
| **26-B Netz-Infrastruktur** | `WiFiNetworkManagement` (chip 170), `ThreadNetworkDirectory` (chip 339), `ThreadNetworkDiagnostics` provisorische Attribute (`ThreadNetworkDiagnosticsProvisionalTest.ts`), Gerätetyp NetworkInfrastructureManager | chip / matter.js | `v0.13.7` |
| **26a Thread Border Router** | ADR 0027: `thread/` (Dataset-Codec, `controller/thread` leitet weiter), `cluster/threadbr` mit `ThreadBorderRouterManagement` nach chip (330, Tests 524), OTBR-REST-Client im Controller, `get_thread_border_routers` / `get_thread_diagnostics` im Server (S-Welle danach) | chip / OTBR | `v0.13.8` |
| **26-C Sensorik-Rest** | `AmbientContextSensing` nach chip (944 + Delegate, Tests 1 153); `WaterTankLevelMonitoring` bleibt generiert (keine Quelle hat Logik → `by_design`-Eintrag, Vergleichszeile ✅) | chip | mit 26-B |
| **26-D Joint Fabric** | `JointFabricAdministrator` (chip 491 + `JCMCommissionee` 510, Test 843) und `JointFabricDatastore` (chip 795) nach chip, hinter Ports; Gerätetyp JointFabricAdministrator; Parität gegen chips Unit-Tests, kein Ökosystem-Test möglich (Owner-Entscheidung 0.3, 2026-10-27; ADR 0020 Stufe 6c damit vorgezogen) | chip | `v0.13.9` |
| **27-A Controller-Rest** | Invoke-Batcher nach matter.js (`ClientInteraction.ts`, 39 Fälle), `Discovery`/`ParallelPaseDiscovery`-Form (4 Dateien), DCL-Vendor-Info-Service (2 Dateien, 952 Zeilen), NOC-Rotation + PQC-Phase-1-Größen (`FabricAuthority.ts:88-99, :181-186`) | matter.js | `v0.13.10` |
| **27-B Geräte-Rest** | gemeldete Session-Parameter (`node.Config`-Option + Accessor, 77 Zeilen), BDX-MRP-Verzögerung (`TransferOptions.AdditionalMrpDelay`), TEVT (die sieben Thermostat-Events; `ActiveScheduleChange` dann tatsächlich emittiert), Komposita in `devicegen` (VideoDoorbell, FloodlightCamera) | matter.js | `v0.13.11` |
| **27-C Sweep** | Vergleichstabelle, Findings-Register, Test-Register, Roadmap-Zeilen 278/279, `docs/certifiability.md`; Gruppen-Multicast gegen einen echten Controller (Owner-Handtest mit Apple Home / chip-tool-Gruppe), ARL bleibt ausgeschlossen | — | **`v0.14.0` schließt M6** |

Jede Welle ist ein Brief mit Port-Form, chip-Zeilen am Harness-Commit
(6170af84; wo chip dort älter ist als 645b267a, wie bei den Thermostat-
Schedules, sagt der Brief den Commit), den zu portierenden chip-Tests als
Parity-Fälle, der Register-Zeile und dem `by_design`-Text als Kandidat. Zwei
Opus-Agenten parallel auf disjunkten Pfaden (eine Kamera-Welle, eine
Netz-/Controller-Welle); ein Release pro gemergter Welle.

## D. Form der chip-Ports (gilt für alle 26-Wellen)

- **Port statt Stack.** Der Server hält Zustand und Regeln (Validierung,
  Statuscodes, Attribut-Semantik, Events); Medien, Zertifikate, Radios und
  Zonen-Geometrie liefert der Host über ein `<cluster>def`-Port-Interface
  (`Declare.<Port>`, generiert wie `SchedulePort`). Kein WebRTC-, TLS- oder
  Video-Stack im Modul.
- **chips Delegate-Schnitt** ist die Vorlage für den Port (die Zeilen stehen
  in der Lesekarte §5), nicht seine Kopie: ein Go-Interface mit so wenig
  Methoden, wie die Regeln brauchen.
- **Parity:** chips `Test<Cluster>Cluster.cpp` wird nach Bedeutung portiert
  (`*_chip_test.go`), matter.js-Fälle, wo sie existieren, zusätzlich.
- **Register:** je Cluster eine Zeile „Ahead of matter.js“ mit dem Marker
  (die 14-Zeilen-Hülle als `class <Name>Server`-Marker), `make ahead-check`
  grün; `by_design`-Eintrag je Welle.
- **Ökosystem:** Kamera-Cluster erscheinen in keinem Host-Gerät, bevor ein
  Livetest mit Apple Home (das einzige Ökosystem mit Matter-Kamera-Support
  2026) bestanden ist — dieselbe Regel wie MSCH.

## E. Besser als andere, besser als vorher, Schnitt richtig?

- **Besser als andere.** matter.js liefert für die 17 Cluster Hüllen; chip
  liefert Verhalten, aber nur in C++ und ohne Host-Schnitt (Delegates mit
  Dutzenden Methoden, Medien-Stack im Beispiel-App). Nach M6 hat go-fabric
  für jeden Snapshot-Cluster ein Verhalten mit Parity-Fällen beider Quellen
  und einem Host-Port, den ein Go-Daemon in einem Nachmittag bedient.
- **Besser als vorher.** Bis M5 war „Stub“ ein generierter Server ohne
  Regeln; ein Host, der Chime oder eine Kamera anbot, musste chips Regeln
  selbst kennen. Nach M6 sind die Regeln im Modul, die Register-Zeile sagt,
  woher, und der Pin-Lift prüft wöchentlich, ob matter.js nachgezogen hat.
- **Schnitt.** Drei Kamera-Wellen statt einer, weil TLS die Voraussetzung
  von PushAV ist und WebRTC-Requestor die einzige matter.js-Quelle: erst
  matter.js-Parity, dann chip. Thread BR als eigene Welle wegen des
  OTBR-Clients (ADR 0027). Der Controller-Rest ist eine Welle, weil alle
  vier Teile matter.js-Ports mit vorhandenen Tests sind. Joint Fabric war die
  eine offene Scope-Frage (ADR 0020 Stufe 6c vs. Owner-Satz 2026-10-25 „in
  scope as implementations“) — entschieden: jetzt, als Welle 26-D.

## F. Entscheidungen (Owner, 2026-10-27, per Wizard)

- **0.1 Invoke-Batcher:** implementieren, Welle 27-A (matter.js-Verhalten,
  `ClientInteraction.ts` Lanes und Timer; die 39 `ClientInvokeTest`-Fälle).
- **0.2 Kamera-Tiefe:** alle drei Kamera-Wellen A1–A3 in M6, hinter
  Host-Ports, kein Medien-Stack im Modul, Livetest-Regel wie MSCH.
- **0.3 Joint Fabric:** jetzt, als Welle 26-D nach chip (ADR 0020 Stufe 6c
  wird vorgezogen; ADR-Zeile amendiert). Parität nur gegen chips Unit-Tests.
- **0.4 WaterTankLevelMonitoring:** generierter Server bleibt; `by_design`-
  Eintrag „keine Quelle hat Logik“ (Empfehlung, nicht abgefragt).
- **0.5 Komposita:** `devicegen` lernt PartsList-Komposita (VideoDoorbell,
  FloodlightCamera) in 27-B; CastingVideoClient bleibt ausgeschlossen.
- **0.6 DCL-Vendor-Info-Service:** in 27-A portieren (Empfehlung, nicht
  abgefragt).
- **0.7 Parallelität:** zwei Opus-Agenten (Kamera-Strang 26-A1→A2→A3 ∥
  Netz-/Controller-Strang 26-B→26a→26-C→26-D→27-A), dann 27-B und 27-C;
  ein Release pro Welle `v0.13.4`–`v0.13.11`, **`v0.14.0` schließt M6**.
