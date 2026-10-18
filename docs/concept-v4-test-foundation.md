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

### A.4 Phase 15: das öffentliche Testpaket `fabrictest`

Umriss, Detail bei Fälligkeit: ein In-Process-Knoten und ein In-Process-
Controller (aus `bridgetest` und `controller/…` zusammengesetzt), das
kommissionierte Paar (`MockSite.addCommissionedPair`,
`packages/node/src/testing/mock-site.ts:156`), ein `Clock`-Port mit einer
Fake-Uhr (`MockTime`: `advance`, `resolve`, `yield`) statt der fünf Ad-hoc-
Fakes im Baum, die Wire-Helfer (`interaction.read/write/invoke/subscribe`,
`node-helpers.ts:317-465`). Öffentliche API, also Deprecation-Fenster nach
README § API stability; die Suiten des Moduls laufen darauf.

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
| 15 | BF | `fabrictest` | `fabrictest/`, Suiten | `v0.7.2` |
| 16 | BG, BH | Ports protocol/test, node/test/node | `*_test.go` | `v0.8.0` |

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
