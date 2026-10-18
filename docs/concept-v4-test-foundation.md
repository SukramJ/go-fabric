# Konzept v4: Das Testfundament (Meilenstein 2)

Detaildokument zu [Meilenstein 2 der Roadmap](./roadmap.md#milestone-2-the-test-foundation),
Phasen 14–16. Deutsch, wie Konzept v3; nicht veröffentlicht. Stand 2026-10-18,
nach dem Release `v0.7.0`.

## 0. Entscheidungen des Owners (2026-10-18)

| # | Frage | Entscheidung |
| --- | --- | --- |
| 0.1 | Form des Test-Portierungsregisters | **JSON-Quelle, generierte Markdown-Seite.** `script/testparity/register.json` ist die Wahrheit, `notes/parity/matterjs_tests.md` wird daraus erzeugt; der Guard prüft Register gegen einen eingebetteten Schnappschuss der Testdateien am Pin, mit exaktem Ratchet wie `reachability-check`. |
| 0.2 | Verhaltens-Fixes der 57 Upstream-Commits | **Jeder Fix wird klassifiziert** (haben wir / Lücke → Findings-Register / nicht zutreffend). Lücken werden Register-Einträge, nicht in Phase 14 behoben. |
| 0.3 | Nachprüfung der 3 696 Zitate mit Zeilennummer | **Dauerhafter Guard `make cite-check`**, lokal gegen `../matter.js`, in CI gegen einen Klon am Pin. |
| 0.4 | CHIP-Testimage (matter.js baut jetzt aus `3584c8c8`) | **Nicht in Phase 14**; eigene Welle danach, die auch prüft, ob das neue Image CHIPoBLE trägt. |

Weiter gelten: Fable plant und prüft, Opus implementiert, Sonnet liest
(`CLAUDE.md`, „Which model does which work"); ein PR pro Welle; Auto-Merge nach
meinem Review; matter.js bleibt der Goldstandard.

## Teil A: Was die Bausteine sind

### A.1 Der Pin-Lift auf `862ad75f`

Alter Pin `0ceb202d`, neuer Pin `862ad75f` (matter.js `main`, 2026-10-17; der
Commit nach `v0.18.0-alpha.0-20261017-d02c422c9`). 57 Commits: 7 Versions-
und CHIP-BUILD-Commits, 2 `ci`/`docs`, der Rest Fixes und Tests.

**Schnappschoss-Diff (gelesen, Commit `046218a` auf `m2/phase-14-pin`):** die
einzige Modelländerung ist `BridgedNode` → `composition: "full-family"`
(matter.js #4735, `packages/model/src/standard/elements/BridgedNodeDT.ts`).
Alle anderen 199 Dateien bewegen nur die Provenienz-Zeile. Die Golden-Dateien
der Generatoren sind aktualisiert; `go test ./schema/... ./script/clustergen/...
./script/devicegen/...` ist grün.

**Was daraus folgt:** matter.js' `DescriptorServer.ts:227-270` listet für
einen Endpunkt, dessen Gerätetyp full-family komponiert, **alle Nachfahren**
in der PartsList; unser Assembler listet nur die direkten Teile
(`endpoint/assembler.go:230-238`, `PartIDs`). Ein Bridged Node mit
verschachtelten Teilen (Kühlschrank-Beispiel, Device Library 1.6.1 § 2.5.6)
meldet bei uns zu wenig. Das ist Item 1 des Briefs BD, mit Paritätstest.

**Zweite Hälfte des Lifts, meine Arbeit (0.2):** die Fixes der 57 Commits
gegen go-fabric klassifizieren. Grundlage ist der Sonnet-Read
`notes/reference/read-matterjs-delta-862ad75f.md`; das Ergebnis ist eine
Tabelle in `notes/parity/pin-lifts.md` (neu; ein Abschnitt pro Lift, bleibt
als Protokoll für die nächsten) und je ein Eintrag im Findings-Register pro
Lücke. Kandidaten, die ich jetzt schon sehe: INVALID_SUBSCRIPTION auf den
Report eines gekündigten Subscribe (#4777), Gruppen-/ACL-/ICD-Zustand beim
Ersatz einer Fabric (#4779), nur die vom Fail-Safe gehaltene Fabric-Änderung
aufschieben (#4778), DNS-SD-TXT-Schlüssel ohne Groß/Klein (#4741),
Known-Answers mit Rest-TTL (#4756), AtomicRequest vor den Einzelstatus (#4759),
Vendor-ID außerhalb des Bereichs im manuellen Code (#4765), Utility-Komponenten
auf dem komponierenden Endpunkt zählen (#4744), „never refuse what only a Root
assertion requires" (#4746).

### A.2 Der Zitate-Guard `make cite-check`

**Befund.** 3 696 Zitate der Form `…Foo.ts:NN` in 636 Go-Dateien; davon 629 mit
`packages/…`-Pfad, 154 mit Teilpfad (`session/SessionManager.ts`), 2 913 nur
mit Dateinamen (`ExchangeManager.ts:108`). Kein Guard prüft sie; bei jedem Lift
veralten Zeilennummern unbemerkt.

**Bauweise.** `script/citecheck` (Go, neben `script/reachability`):

1. **Sammeln.** Alle `*.go` (Tests eingeschlossen) werden nach `<pfad>.ts:<zeile>[-<zeile>]`
   durchsucht, mit optionalem Zusatz `at <sha>` im selben Satz. Ein Zitat mit
   eigenem `sha` wird an diesem Commit geprüft (historische Zitate in
   Kommentaren und `by_design`-Begründungen), alle anderen am Pin aus
   `parity/schema.json` (`matter.sourceCommit`).
2. **Auflösen.** `packages/…` exakt; Teilpfad als eindeutiges Suffix; nackter
   Dateiname als eindeutiger Basename im Baum am Pin (`git ls-tree -r`).
   Mehrdeutig ist ein harter Fehler mit den Kandidaten; die Korrektur
   verlängert den Pfad im Kommentar.
3. **Prüfen.** Datei existiert am Commit (`git show <sha>:<pfad>`, nie der
   Arbeitsbaum); Zeile ≤ Dateilänge (hart). **Drift (weich):** der Kommentar
   nennt neben dem Zitat einen Bezeichner (CamelCase-Wort oder `#name`); steht
   der nicht innerhalb von ±15 Zeilen um die zitierte Zeile, ist das Zitat
   gedriftet. Ohne Bezeichner im Satz wird nur hart geprüft.
4. **Ratchet.** `script/citecheck/drift.json` (committed) listet die
   gedrifteten Zitate; `make cite-check` regeneriert, scheitert bei
   Veränderung der Datei und bei harten Fehlern; `citecheckDriftRatchet` ist
   exakt in beide Richtungen, wie `reachabilityUnreachedRatchet`. Startwert =
   Messung nach den Korrekturen der Welle; Ziel 0 bis zum Ende von M2.
5. **CI.** Job `cite-check` in `ci.yml`: Klon `--filter=blob:none --no-checkout`,
   `sparse-checkout set packages`, `checkout <pin>` (Blobs nur für `packages/`),
   `MATTERJS_DIR` darauf. Netzabhängig wie der `chipdm`-Job; ein Fehlschlag
   des Klons ist Exit 2 und damit rot (eine Prüfung, die nicht lief, war nicht
   grün). Lokal: `make cite-check` gegen `../matter.js`, das dafür am Pin
   stehen muss (`git -C ../matter.js show <pin>:…` liest unabhängig vom
   ausgecheckten Stand).

Nicht im Guard: Zitate in `docs/` und `notes/` (zweite Stufe, `--md`), Zitate
ohne Zeilennummer, chip-Zitate (`.cpp:`/`.h:`/`.py:` — Folgewelle, gleicher
Mechanismus gegen den Harness-Commit).

### A.3 Das Test-Portierungsregister

**Befund.** 324 `*Test.ts`: `packages/node/test` 210, `packages/protocol/test`
114 (am neuen Pin). Grundlage der Erstbefüllung ist der Sonnet-Read
`notes/reference/read-matterjs-tests-862ad75f.md` (Datei, Zeilen,
`describe`-Namen, Kandidaten-Go-Tests per grep).

**Bauweise.** `script/testparity`:

- `matterjs_tests.json` — **Schnappschuss** am Pin, erzeugt von
  `make testparity-snapshot` aus dem matter.js-Baum (`git ls-tree`/`git show`
  am `sourceCommit`): Pfad, Zeilen, SHA-1 des Inhalts, `describe`-Namen der
  obersten Ebene. Wird beim Pin-Lift neu erzeugt; sein Diff zeigt neue,
  entfernte und geänderte Testdateien.
- `register.json` — **Register**, von Hand gepflegt, ein Eintrag pro Pfad:
  `status` ∈ `ported` | `partial` | `planned` | `excluded`; `go`: Liste
  `pkg/path:TestName` (bei `ported`/`partial`); `missing`: `it`-Namen, die
  bei `partial` fehlen; `class` (bei `excluded`) aus einer festen Liste:
  `controller-stage` (mit `until: <Stufe/Meilenstein>`), `js-runtime-only`,
  `joint-fabric`, `device-ble-thread` (bis M7), `test-infrastructure`
  (Helper-Tests ohne Matter-Verhalten); `reason`: ein Satz; `snapshotSha`:
  der SHA-1, gegen den die Zuordnung zuletzt geprüft wurde.
- `notes/parity/matterjs_tests.md` — **generiert** (`make testparity`):
  Tabellen pro Verzeichnis, Summen je Status, Liste der `planned`.
- **Guard** `make testparity-check` (Go-Test in `script/testparity`): jede
  Schnappschuss-Datei hat einen Eintrag; kein Eintrag ohne Datei; jede
  `go`-Referenz löst sich per `go/ast` auf eine existierende `Test…`-Funktion
  auf; ein `ported`/`partial`-Eintrag, dessen `snapshotSha` nicht mehr dem
  Schnappschuss entspricht, wird gemeldet (die matter.js-Datei hat sich
  geändert, die Zuordnung ist neu zu prüfen); die Markdown-Seite ist
  unverändert nach Regenerierung; **Ratchet** `testparityPlannedRatchet` =
  Anzahl `planned`, exakt in beide Richtungen.
- **CI.** Job `testparity` in `ci.yml`, ohne matter.js-Klon (der Schnappschuss
  ist eingebettet).

**Erstbefüllung.** Opus schlägt pro Datei Status und `go`-Referenzen aus dem
Sonnet-Read vor; `ported` nur, wenn die Go-Tests die `it`-Fälle semantisch
abdecken, sonst `partial` mit `missing`. Ich prüfe jeden `excluded`-Eintrag
und eine Stichprobe von zehn Prozent der `ported`. Der Ratchet-Startwert ist
die Zahl der `planned` nach der Befüllung; Phase 16 bringt ihn für
`protocol/test` und `node/test/node` auf 0, Meilenstein 3 für
`node/test/behaviors`.

### A.4 Phase 15: der `Clock`-Port und das öffentliche Testpaket `fabrictest`

**Entscheidungen des Owners (2026-10-18, Phase 15):** (0.5) der eine Zeit-Port
lebt im neuen Top-level-Paket `clock`; (0.6) **alle** rohen Zeitstellen der
Knotenseite werden in Phase 15 umgestellt, Cluster-Timer eingeschlossen; (0.7)
das kommissionierte Paar spricht über einen In-Memory-Transport, UDP-Loopback
bleibt Option; (0.8) ein neues Paket `fabrictest` über `bridgetest` und dem
Controller, `bridge/bridgetest` und `endpoint/endpointtest` bleiben stabil.

**Befund** (`notes/reference/read-test-helpers-for-fabrictest.md`): sechs
Clock-Formen (`exchange.Clock` Now+AfterFunc, `controller/exchange/clock.go:11`;
`btp.Clock`, `onoff.Clock`, `core.OtaClock` nur AfterFunc; `mattercert.TimeSource`;
nackte `Now func() time.Time`-Felder in sieben Cluster-Configs, `mdns`, `groups`,
`controller/ca`), 20 benannte Test-Fakes, 22 `clock.AfterFunc`-Stellen im
Controller hinter dem Seam, aber **kein Seam auf der Knotenseite**: 66 rohe
`time.Now`/Timer/Ticker-Stellen in `bridge/` (Ack-Pump `ackpump.go:304`,
PASE/CASE-Provider, Fenster, Health), `im/subscription/engine.go:39`, `node/`,
`secure/operational/manager.go:1159`, `mdns/`, dazu 14 in `cluster/`.
`bridgetest.Commissioner` läuft auf der Systemuhr über Loopback-UDP
(`bridge/bridgetest/commissioner.go:219`). loom importiert neun Symbole aus
`bridgetest`/`endpointtest`; die 2mqtt-Projekte nutzen go-fabric noch nicht.

**Bauweise, Welle 15a (Brief BF): das Paket `clock`.**

- `clock.Clock` = `Now() time.Time`, `AfterFunc(d, f) Timer`, `NewTimer(d) Timer`
  (mit `C()`), `NewTicker(d) Ticker` (`C()`, `Stop()`, `Reset(d)`); `clock.System`.
  Stdlib-only. `exchange.Clock` wird Alias; `btp.Clock`, `onoff.Clock`,
  `core.OtaClock`, `mattercert.TimeSource` und jedes `Config.Now`-Feld bleiben
  (öffentliche API, Deprecation-Fenster nach README § API stability), bekommen
  daneben `Config.Clock clock.Clock`; gesetzt gewinnt `Clock`, sonst der alte
  Weg, sonst `clock.System`.
- `clock/clocktest.Fake` nach matter.js `MockTime`
  (`packages/testing/src/mocks/time.ts`: `enable` :342, `reset` :350, `now`
  :456, `resolve` :537, `advance` :638, `yield` :669): `Advance(d)` feuert
  fällige Timer und Ticker in Zeitreihenfolge, jeden Callback in eigener
  Goroutine, und kehrt zurück, wenn alle zurückgekehrt sind; `Yield()` lässt
  angestoßene Goroutinen laufen; `Pending()`; `Set(t)`. Die 20 Fakes im Baum
  werden durch `clocktest.Fake` ersetzt (eine Welle, per Skript wo gleichförmig).
- Jede der 66 + 14 Stellen liest die Uhr des Besitzers: `bridge.Config.Clock`,
  `node.Config.Clock` (reicht an Bridge, Store-Persist, Reopen-Poll, OTA
  durch), `subscription.Config.Clock`, `operational`-Manager, `mdns`-Configs,
  die Cluster-Configs. Ticker-Schleifen (`ackpump.go:304`, `engine.go:39`,
  `persist.go:310`, `node.go:409`, `manager.go:1159`, `zeroconf.go:634`) laufen
  auf `Clock.NewTicker`; `time.Sleep`/`time.After` auf `NewTimer`.
- Guard: ein Test in `clock` prüft per `go/ast`, dass außerhalb von `clock/`,
  `examples/` und `_test.go` kein `time.Now`, `time.AfterFunc`, `time.NewTimer`,
  `time.NewTicker`, `time.Sleep`, `time.After`, `time.Tick` mehr steht (eine
  Ausnahmeliste mit Begründung, Ziel leer); `make race` ist Pflicht.

**Bauweise, Welle 15b (Brief BG): das Paket `fabrictest`.**

- `fabrictest.Site` nach `MockSite` (`packages/node/src/testing/mock-site.ts:31-212`:
  `addController` :112, `addDevice` :134, `addUncommissionedPair` :144,
  `addCommissionedPair` :156, `close` :182): `NewSite(t, Options)` mit einer
  `clocktest.Fake`, einem In-Memory-Netz (Datagramme zwischen Knoten über
  Kanäle, bedient durch `bridgetest.ServeConn` auf der Knotenseite und einen
  Conn-Channel des Controllers; Verlust/Verzögerung einstellbar für MRP-Tests)
  und `Options.Loopback` für echtes UDP.
- `Site.AddDevice(topology)` = In-Process-Knoten (aus `node`, Fake-Store),
  `Site.AddController()` = In-Process-Controller (aus `controller/…`),
  `Site.CommissionedPair()` = beide, kommissioniert, mit CASE-Session.
- Wire-Helfer nach `node-helpers.ts` `interaction` (`:317-465`): `Read`,
  `Write`, `Invoke`, `Subscribe`, `Connect`; dazu `Node.Act` für den lokalen
  Serverzugriff wie `MockServerNode` (`mock-server-node.ts:102-348`).
- Die vier `startNode`-Kopien unter `controller/` und die zwei handgebauten
  Controller-Verdrahtungen (`controller/client/live_test.go`,
  `controller/interaction/live_test.go`) wandern auf `fabrictest`; `bridgetest`
  und `endpointtest` bleiben unverändert, Duplikate werden Deprecated.
- Öffentliche API: `docs/guides/` bekommt eine Seite „Testing against an
  in-process controller"; die Concurrency-Doku nennt das Paket.

### A.5 Phase 16: die Protokoll-Ports

`packages/protocol/test` (114) und `packages/node/test/node` (49) als Go-Tests
gegen diesen Stack, auf `fabrictest`. Reihenfolge nach Register: zuerst die
Dateien, deren Gegenstand wir haben (MRP, MessageExchange, SessionManager,
PASE/CASE, MessageCodec, Certificates, DeviceAttestationValidator, Bdx, Btp,
Mdns, FailsafeContext), dann `node/test/node`. Was `excluded` ist, bleibt es
mit Klasse und Frist.

## Teil B: Phasen und Wellen

| Welle | Brief | Inhalt | Pfade (disjunkt) | Version |
| --- | --- | --- | --- | --- |
| 14a | BD (Opus) | Pin-Lift abschließen auf `m2/phase-14-pin`: BridgedNode full-family im Assembler + Paritätstest; `chipdm`-Acknowledgements nachgeführt; `cite-check` gebaut, Drift korrigiert, Ratchet gesetzt; CI-Job | `endpoint/`, `script/citecheck/`, Go-Kommentare überall, `Makefile` (Ziel `cite-check`), `ci.yml` (Job `cite-check`) | — |
| 14b | BE (Opus, parallel) | Schnappschuss, Register, Generator, Guard, Erstbefüllung, CI-Job | `script/testparity/`, `notes/parity/matterjs_tests.md`, `Makefile` (Ziele `testparity*`), `ci.yml` (Job `testparity`) | — |
| 14c | ich | Fix-Delta klassifiziert: `notes/parity/pin-lifts.md`, Register-Einträge | `notes/parity/` | `v0.7.1` nach 14a–c |
| 15a | BF | `clock` + `clocktest.Fake`, alle Zeitstellen umgestellt, die 20 Fakes ersetzt, der Guard | `clock/`, `bridge/`, `im/`, `node/`, `secure/`, `mdns/`, `cluster/`, `controller/exchange` (Alias) | — |
| 15b | BG | `fabrictest` (Site, In-Memory-Netz, Paar, Wire-Helfer), Migration der Controller-Testhelfer, Guide | `fabrictest/`, `controller/*_test.go`, `docs/guides/` | `v0.7.2` |
| 16 | BH, BI | Ports protocol/test, node/test/node | `*_test.go` | `v0.8.0` |

`Makefile` und `ci.yml` berühren 14a und 14b beide; den Merge-Konflikt löse
ich (Fable-Zeile). Beide Briefs lesen die Sonnet-Reads, nicht den Baum.

## Teil C: Was sich an Grenzen ändert

- `contract/` unverändert. `schema/` und `device/` nur durch die Regenerierung.
- CI bekommt zwei Jobs; `cite-check` ist der zweite netzabhängige Job auf dem
  Merge-Pfad (nach `chipdm`). Beide ziehen einen fremden Baum an einem festen
  Commit, nie HEAD.
- `fabrictest` (Phase 15) wird öffentliche API.
- Das CHIP-Image bleibt bei `6170af84` (0.4); die Image-Welle folgt M2-14.

## Teil D: Risiken

- **Mehrdeutige Dateinamen** unter den 2 913 nackten Zitaten: der Guard macht
  sie zu harten Fehlern; die Korrektur ist mechanisch (Pfad verlängern), aber
  zahlreich. Opus korrigiert per Skript, nicht einzeln.
- **Drift-Volumen unbekannt.** Der Startwert des Ratchets ist die Messung;
  ein hoher Wert ist eine Zahl, kein Urteil. Korrektur in Wellen bis 0.
- **Qualität der Erstbefüllung.** `ported` ist eine Behauptung über zwei
  Testbäume; Stichprobe und Register-Review fangen den Rest.
- **matter.js bewegt sich täglich.** Der Pin ist ein Commit; der Schnappschuss
  der Tests steht fest; `matterjs-pin.yml` misst den Abstand weiter.
