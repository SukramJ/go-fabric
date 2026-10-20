# Konzept v5: Verhaltenstiefe (Meilenstein 3)

Stand 2026-10-19, go-fabric `v0.7.3`, matter.js-Pin `862ad75f`. Fortsetzung
von [Konzept v4](./history/concept-v4-test-foundation.md). Sprache ist Deutsch, weil
es ein Planungsstand ist; ADRs und `by_design`-Einträge daraus sind Englisch.

Meilenstein 2 hat das Fundament gelegt: eine Uhr, ein In-Process-Netz
(`fabrictest`), das Test-Portierungsregister mit Guard und die beiden
großen Protokoll-Ports. Meilenstein 3 nutzt es für das, woran die
Ökosysteme eine Bridge messen: die 25 matter.js-Server mit handgeschriebener
Logik, jeder gegen sein Go-Gegenstück geprüft, mit der portierten
matter.js-Testdatei als Beweis ([Roadmap, Meilenstein 3](./roadmap.md#milestone-3-behaviour-depth)).
Dieses Dokument plant **Phase 17** (Teil A–D, abgeschlossen mit `v0.8.4`)
**Phase 18** (Teil E, abgeschlossen mit `v0.8.8`) und **Phase 19** (Teil F).

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

## Teil E: Phase 18 — das System-Set

Stand 2026-10-19, go-fabric `v0.8.4`. Quelle:
`notes/briefs/reads/read-m3-phase-18-system-set.md` (Sonnet-Read am Pin,
von mir stichprobengeprüft: Harness-Importe jeder Testdatei, die
Register-Einträge, die ADR-0013-Liste). Zeilen sind matter.js-Zeilen am
Pin; Fälle sind `it(`.

### E.0 Entscheidungen des Owners (Phase 18, 2026-10-19)

| # | Frage | Empfehlung | Entscheidung |
| --- | --- | --- | --- |
| 18.1 | Wer implementiert die vier Server, die ADR 0013 auslässt (AccessControl, OperationalCredentials, GroupKeyManagement, Groupcast; `CLAUDE.md` „Which model does which work", erste Zeile) und die Commissioning-Tests? | **Fable selbst**, in zwei eigenen Wellen (18b, 18c); Opus die übrigen zwei (18a, 18d). Die Regel ist bewusst: jede Erwartung in diesen Ports ist ein Satz über Sicherheit und wird später als Fakt zitiert. | **Opus implementiert, Fable prüft.** Alle vier Wellen an Opus-Agenten; ich reviewe jeden PR und verifiziere jede Zitatzeile der Sicherheitsserver gegen den Pin, bevor sie gemergt wird. `CLAUDE.md` trägt die Ausnahme. |
| 18.2 | `AccessControlCustomExtensionTest.ts` (7 Fälle): eine herstellerspezifische Erweiterung eines Standard-Clusters (eigenes Attribut, eigener Befehl auf AccessControl, über den Draht und vom Controller gelesen). | **Verschieben auf Meilenstein 4** (`planned`, `until: M4`): das ist kein Verhalten des Servers, sondern eine Fähigkeit der `cluster/spec`-Laufzeit und des Controllers, die go-fabric nicht hat; sie gehört zur Einbettungsschicht. | **Verschoben auf Meilenstein 4.** Register `planned`, `until: M4`. |
| 18.3 | Wellen und Versionen | **Vier Wellen, je zwei parallel**: 18a ∥ 18b, dann 18c ∥ 18d; `v0.8.5`–`v0.8.8`; jeder PR trägt `needs-chiptool` (seit der DD-Korrektur wieder per Auto-Merge). | **Vier Wellen nacheinander.** 18a, 18b, 18c, 18d; `v0.8.5`–`v0.8.8`; keine Paare, also keine Merge-Konflikte zwischen Wellen; ein Brief je Welle, wenn sie dran ist. |

### E.1 Befund

Register heute (`script/testparity/register.json`): 62 Fälle portiert, 150 in
`partial`-Einträgen offen, dazu vier `planned`-Dateien ohne Go-Zeile
(27 Fälle). Zwei Server haben am Pin keine Testdatei.

| Cluster | matter.js-Server | Tests (Fälle, Harness) | Go heute | Register (portiert / offen) |
| --- | --- | --- | --- | --- |
| Descriptor | `DescriptorServer.ts` 303 | `DescriptorServerTest.ts` 685 (31, `MockEndpoint` + `MockServerNode`), `…ClientListTest.ts` (6, `MockEndpoint`) | `cluster/core/descriptor.go` 307, 13 Parity-Funktionen | 13 / 19 (Tag-Edits, Voll-Familien-Komposition, Hierarchie-Reihenfolgen, PartsList-Ordnung; ClientList explizit/beidseitig) |
| GeneralDiagnostics | `GeneralDiagnosticsServer.ts` 631 | `…OperationalHoursTest.ts` (10, `MockServerNode`), `…FeaturesTest.ts` (5, `MockServerNode`) | `cluster/core/general_diagnostics.go` 964, kein Parity-Test | 3 / 6 + 5 `planned` (DataModelTest-Feature gegen das Pfad-pro-Invoke-Limit) |
| AccessControl | `AccessControlServer.ts` 775 | `…AuxiliaryTest.ts` (13, `MockServerNode`/`MockSite`), `…CustomExtensionTest.ts` (7) | `cluster/core/access_control.go` 1 229, `_auxiliary.go` 246; 16 Parity-Funktionen | 4 / 9 + 7 `planned` (18.2) |
| OperationalCredentials | `…Server.ts` 632 + `VendorIdVerification` 325 | `…ServerTest.ts` 360 (12, `MockServerNode`/`MockSite`/`TestFabric`) | `cluster/core/operational_credentials.go` 2 425, `vid_verification.go`; 16 + 11 Parity-Funktionen (Fabric-Manager, Store) | 0 / 12 `planned` (AddNOC-Vendor-Prüfungen, Zertifikatsprüfungen, SetVidVerificationStatement, UpdateNOC behält Gruppenzustand, finale Fabric-Änderung unter bloßem Fail-Safe) |
| GroupKeyManagement | `…Server.ts` 618 | `…ServerTest.ts` 209 (7, `MockServerNode`/`MockSite`) | `cluster/core/group_key_management.go` 1 018; 1 Parity-Funktion | 2 / 6 (Groupcast-Feature-Ablehnung, Schlüssel-Limit, Multicast-Policy-Fälle) |
| Groups | `GroupsServer.ts` 232 | `GroupsServerSchemaTest.ts` 100 (3, `MockServerNode`); `protocol/test/groups/` (6 Dateien) | `cluster/core/groups.go` 390; 4 Parity-Funktionen; `groups/` | 7 / 7 + 3 `planned` (gelockerte AddGroup-Constraints; KeySets 2, MessagingState 5) |
| Groupcast | `GroupcastServer.ts` 938 | `GroupcastServerTest.ts` 2 243 (65 in 20 `describe`, `MockServerNode`/`MockNetwork`/`MockSite`) | `cluster/core/groupcast.go` 1 025, `groups/groupcast.go`; 8 Parity-Funktionen | 6 / 49 (GKM- und Endpoint-Spiegel, Legacy-Prune, Quota, Policy-Reihenfolge, Multicast-Mitgliedschaft und -Fehler, Testing-Ereignisse, abgeleitete Membership, Persistenz) |
| Binding | `BindingServer.ts` 214 + `BindingManager.ts` 720 | `BindingManagerTest.ts` (66 gesamt in drei Dateien: Manager, Integration, Server; `MockServerNode`/`MockNetwork`) | `cluster/core/binding.go` 495, `binding/` 1 111, `node/client.go`; Ports in `binding/manager_test.go`, `node/binding_live_test.go` | 12 / 47 (Client-Auswahl, Warteschlange vor online, Fabric-Ersatz, Gruppen-Sendungen und ihre Ablehnungen, Dormant-Fälle) |
| BasicInformation | `…Server.ts` 213 + Validatoren 57 | drei Dateien, 214 (13, `MockServerNode`) | `cluster/core/basic_information.go` 1 068; 18 + 13 Parity-Funktionen | 9 / 6 (Bridged-Gerät ohne VID/PID, Caller-Werte über CapabilityMinima, ConfigurationVersion: Callback-Reihenfolge, abgelehnte Senkung, gebündelter Bump) |
| GeneralCommissioning | `…Server.ts` 285 + `ServerNodeFailsafeContext.ts` 135 | **keine Testdatei am Pin**; Beweis sind `packages/node/test/behavior/system/commissioning/` (`CommissioningWindowLifetimeTest.ts` 16, `CommissioningRestartTest.ts` 2, `FabricBeforeStartTest.ts` 4) und die Familien CGEN, CADMIN | `cluster/core/general_commissioning.go` 976, 55 Tests | Lifetime 7 / 9, Restart und FabricBeforeStart `planned` |
| AdministratorCommissioning | `…Server.ts` 267 | **keine Testdatei am Pin**; wie oben | `bridge/commissioning_window.go` 1 111, `cluster/wire/admincommissioning.go` 644; 55 Tests | s. o. |
| ScenesManagement | `…Server.ts` 1 109 | `…ServerTest.ts` 99 (1, `MockSite`) | `cluster/core/scenes_management.go` 886, `scenes_attributes.go` 376; 10 Tests | 1 / 1 (Boolean-Szenenwert über OnOff) |

Was das Register nicht zeigt: Groupcast und Binding sind in matter.js
Node-Tests mit `MockNetwork` (Multicast-Beitritt, Fehlerserien, Peer-Verlust
über Subscription-Timeout). Ihr Go-Port läuft auf `fabrictest`; wo
`fabrictest` kein Multicast-Netz stellt, ist das eine Erweiterung des
In-Process-Netzes (Teil C), kein `excluded`.

### E.2 Was ein Port hier ist

A.2 gilt. Dazu:

- **Sicherheitsserver.** AccessControl, OperationalCredentials,
  GroupKeyManagement und Groupcast (ADR 0013 „skipped") und alles unter
  `commissioning/`, `secure/` und der Commissioning-Fenster-Logik der Bridge
  implementiert Opus nach Brief, und ich prüfe (18.1): jeder Fix dort ist
  ein eigener Commit mit Zitat, den ich gegen den Pin lese, bevor der PR
  gemergt wird; eine Erwartung, die der Agent nicht belegen kann, bleibt
  `missing` mit Grund, nie ein stiller Fix.
- **Runtime-Interna** von matter.js (Env-Lazy-Install, Transaktionskonflikte,
  Reaktor-Reihenfolge wie „sets policy before `groupKeyIdMap.added` fires",
  Storage-Setter) sind `excluded js-runtime-only`, je Fall mit Grund.
  Verhalten, das ein Controller sieht (Membership, GKM-Tabellen, Adressen,
  Ereignisse, Persistenz über Neustart), ist nie `excluded`.
- **Zwei Server ohne Test.** GeneralCommissioning und
  AdministratorCommissioning bekommen keinen erfundenen Test: ihr Beweis
  sind die drei System-Commissioning-Dateien (22 Fälle) und CGEN/CADMIN.
  Ein Audit der beiden Server gegen `…Server.ts` am Pin gehört in den
  Bericht von 18b (Unterschiede nach Zeile), nicht ins Register.

### E.3 Die vier Posten

| Welle | Inhalt | Fälle offen | Pfade (disjunkt) | Modell |
| --- | --- | ---: | --- | --- |
| 18a | Descriptor (19 + `ClientList`), GeneralDiagnostics (6 + Features 5), BasicInformation (6), Groups-Schema (3), ScenesManagement (1), `protocol/test/groups` KeySets + MessagingState (7) | 47 | `cluster/core/{descriptor,general_diagnostics,basic_information,groups,scenes_*}.go`, `groups/` (KeySets), `bridge/` (ConfigurationVersion), `device/`, `endpoint/` | Opus |
| 18b | AccessControl Auxiliary (9), OperationalCredentials (12), GroupKeyManagement (6), System-Commissioning (Lifetime 9, Restart 2, FabricBeforeStart 4); Audit GeneralCommissioning und AdministratorCommissioning | 42 | `cluster/core/{access_control*,operational_credentials*,group_key_management*,general_commissioning*}.go`, `bridge/commissioning_window*.go`, `node/`, `secure/` | Opus, Fable-Review Zeile für Zeile |
| 18c | Groupcast (49) | 49 | `cluster/core/groupcast.go`, `groups/groupcast.go`, `groups/manager.go`, `internal/fabrictest` (Multicast-Netz) | Opus, Fable-Review Zeile für Zeile |
| 18d | Binding (34 + 12 + 1) | 47 | `binding/`, `cluster/core/binding.go`, `node/client.go`, `endpoint/binding_mount.go` | Opus |

Die Wellen laufen nacheinander (18.3); jede beginnt auf dem main der
vorigen, Konfliktflächen entfallen. Briefe: BN (18a), BO (18b), BP (18c),
BQ (18d), jeder geschrieben, wenn seine Welle dran ist, mit dem Befund der
vorigen.

### E.4 chip-tool

Die Familien der Phase (Roadmap: ACL, ACE, DESC, DGGEN, OPCREDS) laufen
heute grün in den `core-*`-Gruppen jedes PRs. Ein Fix, der die PICS eines
Endpunkts bewegt, regeneriert Slices und Goldens in der Welle
(Harness-Regel); sonst bleibt der Daemon unverändert, und der
`needs-chiptool`-Lauf ist die Probe, kein Gegenstand.

### E.5 Versionen

Jede Welle ein Patch (`v0.8.5`–`v0.8.8`), nacheinander; Phase 18 schließt
mit der vierten. `v0.9.0` schließt Meilenstein 3 nach Phase 19 (unverändert A.6).

### E.6 Risiken

- **Groupcast-Netz.** 49 Fälle, davon rund ein Drittel auf
  `MockNetwork` (Multicast-Beitritt, IANA-Adresse über Fabrics, Join-Fehler
  mit Wiederholung). `fabrictest` braucht dafür ein Multicast-Modell mit
  Fehlerinjektion; ohne es bleiben diese Fälle `missing`, nicht
  `excluded`.
- **Binding-Clients.** matter.js installiert deklarierte Client-Behaviors
  auf dem materialisierten Endpunkt; go-fabric kennt Clients als
  `node/client.go`-Pfad. Die Client-Auswahl-Fälle (`BindingManagerTest.ts`
  „client selection") sind der Teil, der am ehesten eine Grenze von
  `contract/` berührt; 18d meldet das als Frage, bevor es baut.
- **OperationalCredentials.** Zertifikats- und Vendor-ID-Prüfungen sind
  Pflichtverhalten (TC-OPCREDS); ein Fix dort läuft mit `make race` und der
  OPCREDS-Familie vor dem Push.
- **Umfang.** 185 offene Fälle in vier Wellen; Tranchenregel wie Teil D.


## Teil F: Phase 19 — OTA und der Rest der Behaviour-Tests

Geschrieben am 2026-10-20, nach `v0.8.7` (18c) und mit Welle 18d (Brief
BQ) in Arbeit. Stand des Registers: 52 `planned`, davon 14 unter
`packages/node/test/behaviors`, 23 Laufzeit-Testdateien unter
`node/test/behavior/*`, `endpoint/*`, `endpoints/*` ohne Zuordnung, und 84
`missing`-Fälle in 22 `partial`-Dateien (47 davon Binding, Welle 18d).

### F.0 Entscheidungen des Owners (Phase 19, 2026-10-20)

| Nr. | Frage | Empfehlung | Entscheidung |
| --- | --- | --- | --- |
| 19.1 | Wellenschnitt | Zwei Wellen (19a OTA, 19b der Rest) | **Eine Welle.** Brief BR, ein PR, `v0.8.9`; `v0.9.0` schließt Meilenstein 3 danach. |
| 19.2 | ICD, WebRTC, Thread-Diagnose im Register | ICD → M5, WebRTC + Thread `excluded` | **Wie empfohlen.** `IcdManagementServerTest.ts` (60) und `IcdModeTest.ts` (14) `until: M5 phase 23` (Roadmap Phase 23); `WebRtcTransportRequestorServerTest.ts` (28) und `ThreadNetworkDiagnosticsProvisionalTest.ts` (2) `until: M6 phase 26` — korrigiert beim Schnitt `v0.8.8` (Owner, 2026-10-20): Roadmap-Phase 26 baut beide, und das Register hat keine passende `excluded`-Klasse; die Empfehlung `excluded` beruhte auf der Kamera- und Thread-Entscheidung in `docs/matterjs-comparison.md`, die nur die Stubs betrifft. |
| 19.3 | 23 Laufzeit-Testdateien (277 Fälle) ohne `until` | nach Inhalt klassifizieren | **Wie empfohlen.** Gerätebeobachtbar → Phase 19: `CommissioningWindowTest.ts` 8, `RemoteDescriptorTest.ts` 14, `FabricLabelReconciliationTest.ts` 2, `ConnectionStateTest.ts` 11, `SessionsBehaviorTest.ts` 3, `EventsBehaviorTest.ts` 2 (40). Validierung, Zustands- und Endpoint-Modell → `until: M4`: `ClusterBehaviorFeaturesTest.ts` 15, `MarkChangedTest.ts` 10, `ValuePatcherTest.ts` 8, `conformanceTest.ts` 16, `EndpointNumberConflictTest.ts` 20, `ProtocolServiceTest.ts` 13, `SemanticNamespaceTest.ts` 3, `ConstructionValidationTest.ts` 28, `RuntimeValidationTest.ts` 65, `StandardDeviceTypeCorpusTest.ts` 4, `LargeBridgeTest.ts` 2 (184). Discovery → `until: M5 phase 25`: `CommissioningIdentityTest.ts` 7, `ContinuousDiscoveryTest.ts` 1, `DiscoveryCancelTest.ts` 23, `DiscoveryErrorTest.ts` 13, `DiscoveryTransportsTest.ts` 9 (53). `DclBehaviorTest.ts` 21 → `until: M6 phase 27`. | Ergänzung beim Schnitt `v0.8.8` (Owner, 2026-10-20): die Lesekarte (`notes/briefs/reads/read-m3-phase-19.md`) zeigt, dass `CommissioningWindowTest.ts`, `FabricLabelReconciliationTest.ts`, `ConnectionStateTest.ts` und `RemoteDescriptorTest.ts` (35 Fälle) den Controller-Peer prüfen, nicht das Gerät; Entscheidung: **in Phase 19 lassen**, Port gegen `controller/` auf dem fabrictest-Controller. |
| 19.4 | Was „Register bei 100 % für `packages/node/test/behaviors`" heißt | kein `planned` mehr, jedes `missing` begründet | **Wie empfohlen.** Jede Datei dort ist `ported` oder `partial`; jedes `missing` trägt seinen Grund: js-runtime-only (Transaktionen, Observer-Handles), ein `by_design`-Eintrag, oder `until: M4/M5` mit Findings-Eintrag. Die Roadmap-Zeile ist so präzisiert. |

Die Register-Tags aus 19.2 und 19.3 setzt der Release-Schnitt `v0.8.8`
(nach Welle 18d, damit das Register nicht zwei Zweige gleichzeitig bewegt).

### F.1 Befund

**OTA.** `packages/node/test/behaviors/ota/OtaTest.ts` (2 819 Zeilen, 50
Fälle in zwei `describe`, dazu `ota-utils.ts`) prüft beide Seiten auf einer
`MockSite`: einen Knoten mit `OtaSoftwareUpdateProviderServer` (30,7 kB)
und dem `SoftwareUpdateManager` (64,9 kB: Consent-Warteschlange, Token- und
Versionsregeln, Apply-Delay, Stillstands- und Reboot-Erkennung, BDX-Fortschritt,
`suppressNextStartUp`, Aufräumen von Testimages) und einen Knoten mit
`OtaSoftwareUpdateRequestorServer` (62,8 kB: Anfrage, Download, Apply,
Neustart, Busy-Wiederholungen, Disable). Go hat beide Server
(`cluster/core/ota_software_update_requestor.go`, `ota_provider.go`,
`node/ota_provider.go`, `bdx/`) mit 26 + 11 eigenen Tests; das Register
führt die Datei `planned` ohne `go`. Die OTA-Reboot-Fälle (`:706-1049`)
beobachten den Requestor-Knoten über eine Subscription des
Provider-Knotens (`subscribedPeer`): sie brauchen den Client-Stack eines
fabrictest-Geräts, den Welle 18d für Binding zuerst benutzt.
Korrektur beim Schnitt `v0.8.8` (Lesekarte §1): in matter.js hostet der
*Controller*-Knoten den Provider-Endpunkt mit dem `SoftwareUpdateManager`
(`ota-utils.ts:202-239`), das Gerät den Requestor; in Go sitzt der Provider
auf einem Knoten (`node.Config.OTAProvider`) mit Consent-, Apply-Policy- und
Status-Hooks. Brief BR ordnet die Fälle des Managers (Warteschlange, DCL,
Reboot-Armer, BDX-Fortschritt) den Go-Hooks zu, wo sie existieren, und
lässt den Rest `missing` mit Grund und Findings-Eintrag.

**Die kleinen Server.** Acht `planned`-Dateien mit 19 Fällen:
commodity-tariff 3, energy-evse-mode 3, fan-control 2, measurement 3,
occupancy-sensing 3, power-source 1, rvc-operational-state 2,
time-format-localization 2 — alle Server in Go vorhanden (generiert oder
`cluster/<gruppe>/`). Dazu die offenen `missing`-Fälle der Phasen 17 und
18 außer Binding (37): boolean-state 1, concentration-measurement 2,
hepa-filter-monitoring 2, ColorControl 2, Identify 4, RemainingTime 2,
die beiden Kopplungen 2, GeneralDiagnostics-Betriebsstunden 1,
GroupKeyManagement 1, OperationalCredentials 2 (Fabric-Ereignis für
Label/VID, Findings BO), AccessControl 3 (verzögerte ACL-Übernahme; der
Extension-Hook und der untypisierte Eintrag sind M4 bzw. `by_design`),
AtomicWriteHandler 6, Presets 6, DoorLock 1 (monotone Uhr, M4),
ConfigurationVersion 1 und Groupcast 1 (`by_design`).

**Laufzeit-Dateien (19.3).** 40 Fälle in sechs Dateien: Sessions (3) und
Events (2) auf dem Server-Knoten; das Kommissionierfenster (8), der
Fabric-Label-Abgleich (2) und der Verbindungszustand (11) prüfen den
Controller-Peer einer kommissionierten Paarung, der Remote-Descriptor (14)
ist ein Controller-Datentyp — Korrektur beim Schnitt `v0.8.8` nach der
Lesekarte; der Owner lässt sie in Phase 19 (Port gegen `controller/`).

Phase 19 umfasst damit rund 146 Fälle: 50 OTA, 19 kleine Server, 37
offene Teilfälle, 40 Laufzeit-Fälle.

### F.2 Was ein Port hier ist

A.2 und E.2 gelten. Dazu:

- **OTA auf fabrictest mit zwei Geräten:** das Provider-Gerät trägt den
  OtaProvider-Endpunkt (`node.Config.OTAProvider`), das Requestor-Gerät den
  Requestor; BDX läuft über das Site-Netz; die `subscribedPeer`-Fälle
  über den Client-Stack des Provider-Geräts (18d zeigt, ob er auf der Site
  läuft; sonst `missing` mit Grund und Register-Kandidat).
- **Offene Teilfälle:** gerätebeobachtbar → portieren oder Fix nach Regel 4;
  Transaktion oder Observer-Handle → `missing` „js-runtime-only" mit Satz;
  fehlende Node-Option → `until: M4` mit Findings-Eintrag. Kein Fall wird
  stillschweigend `excluded`.
- **Die Laufzeit-Dateien** laufen auf fabrictest wie BO's Commissioning-Tests.

### F.3 Der Posten

| Welle | Inhalt | Fälle offen | Pfade | Modell |
| --- | --- | ---: | --- | --- |
| 19 | OTA (50), die kleinen Server (19), die offenen Teilfälle (37), die Laufzeit-Dateien (40) | ≈ 146 | `cluster/core/ota_*.go`, `node/ota_provider.go`, `bdx/`, `cluster/{fan,measurement,modebase,opstate,…}/`, `cluster/core/`, `cluster/thermostat/`, `node/`, `fabrictest/` | Opus; Fable liest die zitierten Fix-Zeilen des OTA-Requestors und -Providers (Image-Prüfung, Consent) gegen den Pin |

Brief BR, geschrieben nach dem Merge von 18d mit dessen Befund.

### F.4 chip-tool

SU (OTA) läuft in `core-su` jedes PRs; die übrigen Familien bleiben die
Probe (E.4).

### F.5 Versionen

Welle 19 ein Patch (`v0.8.9`); `v0.9.0` schließt Meilenstein 3 mit dem
Register nach 19.4 und dem Roadmap-Status (A.6).

### F.6 Risiken

- **OTA-Reboot-Fälle** hängen am Client-Stack auf der Site (F.2); ohne
  ihn bleiben vier Fälle `missing`.
- **SoftwareUpdateManager-Umfang.** 65 kB matter.js gegen den Go-Provider:
  jede Lücke wird Fix oder Findings-Eintrag nach Regel, kein Nachbau auf
  Verdacht; ein Fix an Consent oder Image-Prüfung ist eine Fix-Zeile, die
  der Owner liest.
- **Umfang.** 146 Fälle in einer Welle (19.1): Tranchenregel wie Teil D,
  Report pro Datei.
