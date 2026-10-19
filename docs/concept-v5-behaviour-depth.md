# Konzept v5: Verhaltenstiefe (Meilenstein 3)

Stand 2026-10-19, go-fabric `v0.7.3`, matter.js-Pin `862ad75f`. Fortsetzung
von [Konzept v4](./concept-v4-test-foundation.md). Sprache ist Deutsch, weil
es ein Planungsstand ist; ADRs und `by_design`-Einträge daraus sind Englisch.

Meilenstein 2 hat das Fundament gelegt: eine Uhr, ein In-Process-Netz
(`fabrictest`), das Test-Portierungsregister mit Guard und die beiden
großen Protokoll-Ports. Meilenstein 3 nutzt es für das, woran die
Ökosysteme eine Bridge messen: die 25 matter.js-Server mit handgeschriebener
Logik, jeder gegen sein Go-Gegenstück geprüft, mit der portierten
matter.js-Testdatei als Beweis ([Roadmap, Meilenstein 3](./roadmap.md#milestone-3-behaviour-depth)).
Dieses Dokument plant **Phase 17**; Phasen 18 und 19 bekommen ihren
Abschnitt, wenn sie dran sind.

## 0. Entscheidungen des Owners (2026-10-19)

| # | Frage | Entscheidung |
| --- | --- | --- |
| 0.1 | Switch: echter Server mit matter.js' Zustandsautomat? | **Ja, mit beiden Eingängen.** `cluster/switch` nach `SwitchServer.ts`; der Host liefert Positionen; der heutige Gesten-Pfad (`contract.SwitchEventEmitter`) bleibt als zweite Quelle (`by_design` bleibt). Der Daemon-Button liefert Positionen, SWTCH 2.5/2.6 laufen. |
| 0.2 | Thermostat: Umfang | **Der volle matter.js-Umfang.** Presets, PersistedPresets, AtomicRequest, Occupancy-Setpoints als wählbare Features; Presets im Store. Schedules bleiben draußen (in matter.js nicht implementiert). |
| 0.3 | DoorLock: Umfang | **Voller Port, wählbare Features.** USR, PIN, RID, die Schedules, Auto-Relock, Lockout, ablaufende Nutzer; Nutzer und Credentials über `store/` mit einem Schlüssel des Node-Stores. Eine Bridge ohne USR serviert wie heute. |
| 0.4 | Wellen und Versionen | **Vier Wellen, je zwei parallel.** BJ ∥ BK, dann BL ∥ BM; `v0.8.1`–`v0.8.4`; jeder PR trägt `needs-chiptool`. Start nach `v0.8.0` (Abschluss von Phase 16). |

Weiter gelten: Fable plant und prüft, Opus implementiert, Sonnet liest
(`CLAUDE.md`, „Which model does which work"); ein PR pro Welle; Auto-Merge
nach meinem Review; matter.js bleibt der Goldstandard; kleine zitierte Fixes
in der Welle (Konzept v4, 0.11), Unklares oder Großes wird `missing` mit
Grund.

## Teil A: Phase 17 — die Geräteoberfläche

### A.1 Befund

Quelle: `notes/briefs/reads/read-m3-phase17.md` (Sonnet-Read am Pin, von mir
stichprobengeprüft). Zeilen sind matter.js-Zeilen am Pin; Fälle sind `it(`.

| Cluster | matter.js-Server | matter.js-Tests (Fälle, Harness) | Go heute | Register |
| --- | --- | --- | --- | --- |
| Switch | `SwitchServer.ts` 486 (Roh-Position → Entprellen → Mehrfach-/Langdruck-Automat, `#decidePosition` :230-327) | `SwitchServerTest.ts` 3 527 (69, `MockEndpoint`) | **kein Server**: `cluster/wire/genericswitch.go` 328 reicht Host-Gesten durch (`contract.SwitchEventEmitter`, `BD-Matter-ButtonPressCycleFromDiscreteFrames`) | planned |
| Thermostat | `ThermostatServer.ts` 1 790, `AtomicWriteHandler.ts` 493, `AtomicWriteState.ts` 97 (Presets :1276-1603, Atomic :1603) | 6 Dateien 1 776 (78, `MockServerNode`/`MockSite`) | `cluster/thermo` 1 121 + 408: HEAT, COOL, AUTO, LTNE; keine Presets, kein Atomic, keine Occupancy (`thermostat_server.go:62-65`) | 1 partial (19 missing), 5 planned |
| DoorLock | `DoorLockServer.ts` 1 628, `LockAuth.ts` 279, `LockSchedule.ts` 168 | `DoorLockServerTest.ts` 1 946 (68, `MockServerNode`/`MockSite`/`interaction`) | `cluster/lock` 421: nur UBOLT, Lock-State und die drei Events (`doorlock_server.go:32-35`) | planned |
| WindowCovering | `WindowCoveringServer.ts` 654 | `WindowCoveringServerTest.ts` 145 (5, `MockEndpoint`) | `cluster/cover` 898 mit drei Paritätsdateien | planned |
| OnOff | `OnOffServer.ts` 343 | `OnOffServerTest.ts` 352 (13, `MockServerNode`/`interaction`) | `cluster/onoff` 835, Zeilen zitiert | planned |
| LevelControl | `LevelControlServer.ts` 875 | 4 Dateien 944 (32, `MockServerNode`) | `cluster/levelcontrol` 1 232 + 619 + 416, `cluster/transition` 773 | 4 partial (17 missing) |
| ColorControl | `ColorControlServer.ts` 2 046, `ColorConversionUtils.ts` 270, `kelvinToXy.ts` 1 936 | 3 Dateien 610 (13, `MockServerNode`) | `cluster/light` 10 Dateien, 12 Testdateien | 1 partial (3 missing), 2 ported |
| Identify | `IdentifyServer.ts` 156 | `IdentifyServerTest.ts` 187 (8, `MockServerNode`/`interaction`) | `cluster/core/identify.go` 328 | planned |
| ServiceArea | `ServiceAreaServer.ts` 300 | `ServiceAreaServerTest.ts` 234 (5, `MockServerNode`) | `cluster/servicearea` 786 | planned |

291 Fälle; davon 215 in den drei großen Posten Switch, Thermostat, DoorLock,
deren Go-Server den matter.js-Umfang heute nicht haben. Die sechs übrigen
Cluster haben ihren Server; dort ist der Port ein Abgleich mit kleinen Fixes.

### A.2 Was ein Port hier ist

- **Harness.** Ein matter.js-Test auf `MockEndpoint` (Switch,
  WindowCovering) ruft den Server lokal; sein Go-Port ruft den Go-Server
  im Paket (`cluster/<x>`), Zeit auf `clocktest.Fake`. Ein Test auf
  `MockServerNode` ist ein Node-Test: sein Port läuft auf `fabrictest`
  (`Device.Act` für lokale Schreibzugriffe, die Wire-Helfer für
  `interaction`, `MockSite` → `Site`), im Paket `node/` oder im
  Cluster-Paket, wenn es keinen Controller braucht.
- **Bauweise** wie Konzept v4 §A.5: eine Go-Datei je matter.js-Datei mit
  Kopf „A port of matter.js `packages/node/test/behaviors/…` at 862ad75f“,
  Erwartungen aus der matter.js-Datei, Register nach jeder Datei, Ratchet
  im selben Commit; `excluded` nur mit Klasse.
- **Fixes.** Eine Erwartung, die der Go-Server nicht erfüllt, ist ein
  zitierter Fix in der Welle, wenn sie eine klar benannte Stelle hat;
  sonst `missing` mit Grund. Ein Feature, das der Go-Server nicht hat
  (0.1–0.3), ist kein Fix, sondern der Gegenstand der Welle.
- **Device-Lage.** Was ein Server neu kann, muss die Geräteschicht
  (`device/`, `endpoint/materialize.go`) und der Referenz-Daemon zeigen,
  sonst sieht chip-tool es nicht: der Button des Daemons liefert
  Positionen (0.1), der Thermostat trägt Presets (0.2), das Schloss einen
  Nutzer mit PIN (0.3).

### A.3 Die drei großen Posten

**Switch (0.1).** matter.js leitet alle Gesten aus einem
`rawPosition`-Strom ab: Entprellen (`#handleRawPosition` :128,
`#handleDebouncedPosition` :153), die Entscheidung je Position
(`#decidePosition` :230-327), Mehrfachdruck-Zählung (`#countFurtherPress`
:328, `#handleMultiPressComplete` :357, `#endMultiPressSequence` :372),
Langdruck (`#handleLongPress` :343), ein Ereignis-Puffer, der Ereignisse
erst nach Abschluss der Transaktion liefert (`#queueEvent` :219,
`#applyResults` :204); `Internal` :401 hält Timer und Zähler, `State` :454
`rawPosition`, `debounceDelay`, `longPressDelay`, `multiPressDelay`,
`momentaryNeutralPosition`. Go hat davon nichts: der Host erzählt die
Geste (`contract.SwitchEventEmitter`), der Wire-Server reicht sie durch.
Das trägt eine HomeMatic-Taste, aber keinen Doppelklick (SWTCH 2.5/2.6
stehen als „not supported“, `families_table_test.go:787-789`).
Empfehlung: `cluster/switch` als echter Server mit dem Automaten, Eingang
`SetPosition(uint8)` (matter.js `rawPosition`), Features LS, MS, MSR, MSL,
MSM, AS wählbar; der Gesten-Pfad bleibt für Quellen, die nur Gesten
kennen (`by_design` bleibt), beide münden in dieselben Ereignisse.

**Thermostat (0.2).** matter.js serviert Presets mit `PersistedPresets`
(:1276-1603), den `AtomicRequest`-Handler mit Timeout und Fabric-Bindung
(`AtomicWriteHandler.ts`, `AtomicWriteState.ts`), die Occupancy-Kopplung
(:533-581), die Temperaturmessung des Endpunkts (:454-532) und die
Setpoint-Regeln (:830-1185), von denen Go die Reconciliation hat
(`setpoints.go`). `MatterScheduleConfiguration` ist auch in matter.js
nicht implementiert (`ThermostatServer.ts:83`), `ScheduleConfiguration`
abgekündigt — beide bleiben draußen. Apple Home schreibt Presets atomar;
ohne Atomic ist jede Preset-Änderung aus Home ein Fehler. Empfehlung:
Features PRES, OCC und die Atomic-Befehle als wählbare Features, Presets
persistent im Store.

**DoorLock (0.3).** matter.js: Users (`setUser` :261, `getUser` :341,
`clearUser` :381), Credentials (`setCredential` :419 bis `clearCredential`
:604), die drei Schedule-Arten (:653-934), Auto-Relock (:951-1013), die
Autorisierung mit PIN und Lockout (`#authorize` :1049, `#identifyPin`
:1145, `#refuseWrongCode` :1175, `#isLockedOut` :1192), ablaufende Nutzer
(:1208-1288) und die Ereignisse (:1289-1344); `LockAuth.Store` hält Nutzer
und Credentials verschlüsselt (`cipher` :248). Go: Lock-State, die drei
Lock-Befehle, UnboltDoor, die Operation-Events — ein KeyMatic ohne Nutzer.
Empfehlung: voller Port als wählbare Features (USR, PIN, RID, FGP, WDSCH,
YDSCH, HDSCH, COTA), Persistenz über `store/` mit einem Schlüssel des
Node-Stores; eine Bridge ohne USR serviert wie heute.

### A.4 Die sechs kleinen Posten

OnOff (13 Fälle), LevelControl (32, davon 17 heute `missing`: die
Transaktions-Fälle der Kopplungen, Teardown, Anwendungs-Restzeiten),
ColorControl (13, davon 3 `missing`), Identify (8), WindowCovering (5),
ServiceArea (5): Server vorhanden, Zeilen weitgehend zitiert. Die Ports
decken die Lücken der `partial`-Einträge und finden die Abweichungen; die
Fixes sind klein. Hier läuft auch die erste Probe der Harness-Regel aus
A.2, bevor die großen Posten sie brauchen.

### A.5 chip-tool

Die Familien OO, LVL, CC, I, SWTCH (Gruppe `app-lights`) und DRLK, TSTAT,
WNCV, SEAR (`app-appliances`) laufen in CI nur mit dem Label
`needs-chiptool` (`chiptool.yml:146-166`); jeder Phase-17-PR trägt es.
Wächst der Daemon (Button mit Positionen, Thermostat mit Presets, Schloss
mit Nutzer), klassifiziere ich die betroffenen Tabellenzeilen neu
(`internal/chiptool/families_table_test.go`, `docs/certifiability.md`):
SWTCH 2.5/2.6 und DRLK 2.6 sind heute „not supported“/„harness“ wegen
fehlender Features.

### A.6 Versionen

Jede Welle ein Patch (`v0.8.1`–`v0.8.4`); Phase 17 schließt mit der vierten.
`v0.9.0` schließt Meilenstein 3 nach Phase 19.

## Teil B: Wellen

| Welle | Brief | Inhalt | Pfade (disjunkt) | Fälle | Version |
| --- | --- | --- | --- | ---: | --- |
| 17a | BJ (Opus) | OnOff, LevelControl, ColorControl, Identify, WindowCovering, ServiceArea: Ports, kleine Fixes | `cluster/onoff`, `cluster/levelcontrol`, `cluster/light`, `cluster/transition`, `cluster/core/identify.go`, `cluster/cover`, `cluster/servicearea`, `node/*_parity_matterjs_test.go` der sechs | 76 | `v0.8.1` |
| 17b | BK (Opus, parallel) | `cluster/switch` nach `SwitchServer.ts`; Anbindung in `endpoint/materialize.go`, `device/`; Daemon-Button auf Positionen; Port `SwitchServerTest.ts` | `cluster/switch` (neu), `cluster/wire/genericswitch.go`, `contract/switchevents.go`, `endpoint/materialize.go`, `device/`, `examples/reference-bridge/fleet_sensors.go` | 69 | `v0.8.2` |
| 17c | BL (Opus) | Thermostat: Presets, Atomic, Occupancy; die sechs Tests | `cluster/thermo`, `cluster/spec/thermostat` (Features), `store/` (Presets), `examples/reference-bridge/fleet_climate.go` | 78 | `v0.8.3` |
| 17d | BM (Opus, parallel) | DoorLock: Users, Credentials, Schedules, Auto-Relock, Lockout; der Test | `cluster/lock`, `store/` (Lock-Tabellen), `examples/reference-bridge/fleet_safety.go` | 68 | `v0.8.4` |

Konfliktflächen: `CHANGELOG.md`, `script/testparity/register.json` samt
Seite und Ratchet, `script/coverfloor/floors.go`, `script/reachability/`,
`internal/chiptool/families_table_test.go`, `docs/certifiability.md`;
`store/` berühren 17c und 17d (je eigene Tabellen und Dateien). Löse ich
beim Merge der zweiten Welle eines Paars.

## Teil C: Was sich an Grenzen ändert

- `contract/` bekommt für den Switch höchstens einen Positions-Eingang
  (stdlib only, additiv); `SwitchEventEmitter` bleibt.
- `store/` wächst um die Tabellen für Presets und Lock-Nutzer/Credentials;
  ein Host mit eigenem Store implementiert sie, wenn er die Features wählt.
- `device/` und `endpoint/` montieren die neuen Features nur, wenn der
  Host sie wählt; die Default-Komposition bleibt (keine Break).
- Die Familien-Tabelle und `docs/certifiability.md` folgen dem Daemon.

## Teil D: Risiken

- **Switch-Timing.** Entprell-, Lang- und Mehrfachdruck-Fenster auf der
  Fake-Uhr; der Daemon läuft auf der Systemuhr. Die 69 Fälle sind der
  Schutz, chip-tool SWTCH die Probe.
- **Store-Schema.** Zwei Wellen erweitern `store/`; Migrationen müssen
  additiv sein (ein bestehender Store öffnet weiter).
- **Credential-Verschlüsselung.** matter.js hält Credentials mit einem
  AES-Cipher des Nodes; der Schlüsselort in Go ist eine Entscheidung
  (Empfehlung: aus dem Node-Store abgeleitet, nie im Klartext).
- **Umfang der großen Posten.** 215 Fälle in drei Wellen; stoppt ein Agent
  an einer Dateigrenze, führt der nächste auf dem Branch weiter (Konzept
  v4, Tranchenregel).
