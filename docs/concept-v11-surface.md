# Konzept v11 — Die Oberfläche als Wert: Referenz, Spezifikationsstufe, Controller-Profil

Stand 2026-10-29. Faktenbasis: go-fabric `8f2926a9` (v0.14.0), matter.js-Pin
`862ad75f`, chip `6170af84`. Jede Zeile mit `file:line` wurde in dieser
Sitzung gelesen; alles andere ist als **unverifiziert** markiert. Sprache:
Deutsch (Phasenkonzept); Briefs, Issues, Doku, Register auf Englisch.
Anlass: Issues #331 (matter.js-Kompatibilitätsmodus) und #332
(Spec-Level-Obergrenze). Das Konzept nimmt beide auf, geht aber vom
Problem aus, nicht vom Issue-Text.

## A. Das Problem, eine Stufe höher

Was ein Controller von einem Knoten sieht — Gerätetypen und ihre
Revisionen, Cluster und `ClusterRevision`, `FeatureMap`, `AttributeList`,
`AcceptedCommandList`, `GeneratedCommandList`, `EventList` — nenne ich im
Folgenden die **Oberfläche**. Sie entscheidet, ob Apple Home, Google Home
oder Alexa pairen, laden und weiterarbeiten.

Heute ist die Oberfläche ein **Nebenprodukt**. Sie entsteht aus drei
Quellen, die niemand zusammen sieht:

1. dem Gerätetyp (`device/type.go:22-71`: mandatierte Cluster, Features,
   Elemente),
2. der Host-Deklaration (`cluster/spec/declaration.go:33-49`: `Features`,
   `Initial`, `Elements`),
3. den Defaults des Kit-Servers (`device/device.go:321` `defaultServer`,
   `device/defaults.go:412` `cfg.Features = b.opts.Features`).

Der Server prüft nur, ob er ein Bit überhaupt bedient
(`cluster/thermo/thermostat_server.go:460`). Ob matter.js es implementiert,
ob die gewählte Spezifikationsstufe es kennt, ob ein Controller es
verträgt — das weiß zur Laufzeit niemand, und nichts protokolliert, **warum**
ein Element da ist oder fehlt. Ändert sich die Oberfläche eines gepairten
Endpoints (loom 0.94.0: MSCH nachträglich an), erfährt es weder der Host
noch der Controller auf dem vom Standard vorgesehenen Weg.

Die beiden Issues beschreiben je eine **Regel über diese Oberfläche**.
Die saubere Lösung ist nicht zwei Schalter, sondern: die Oberfläche wird
ein **expliziter, inspizierbarer Wert**, und Regeln sind **Policies über
diesen Wert** — austauschbar, kombinierbar, mit Protokoll jeder
Entscheidung.

**Leitsatz (Owner, 2026-10-29):** Commissioning und Weiterbetrieb gehen vor
Funktionsumfang. **Ergänzung (Owner, 2026-10-29):** Das Konzept darf loom
nie daran hindern, ein Feature anzubieten, das loom unbedingt braucht. Jede
Regel hier ist deshalb ein Standard mit Freischaltung, nie eine Wand
(Abschnitt D.6).

## B. Ziel

1. Ein Host kann zu jedem Zeitpunkt beantworten: *Was sieht ein Controller,
   und warum genau das?* — als Wert, maschinenlesbar, mit Herkunft je
   Element.
2. **Standard ist matter.js' Oberfläche.** Was go-fabric über matter.js
   hinaus bedient (das Register *Ahead of matter.js*,
   `notes/parity/by_design.md:79-111`) erreicht einen Controller nur, wenn
   der Host es im Code freischaltet. Ein vergessener Schalter ist ein
   Build-Fehler, kein stilles Weglassen.
3. Ein Operator kann die Oberfläche mit **einer** Einstellung auf eine
   Spezifikationsstufe begrenzen, ohne dass go-fabric seine
   Zertifizierungsfähigkeit an der neuesten Stufe verliert.
4. Eine Änderung der Oberfläche eines gepairten Endpoints ist ein
   **sichtbares Ereignis** mit Diff, und sie erreicht den Controller auf dem
   Weg, den der Standard dafür hat.
5. Jede Änderung an der Oberfläche in einem Pull Request ist ein
   **Review-Diff**, nicht ein Testausfall bei einem Owner-Handtest.
6. Die Architektur trägt eine dritte Achse, die heute niemand messen kann,
   aber jeder braucht: **Controller-Profile** mit gemessener Evidenz. Sie
   wird gebaut, aber zunächst leer ausgeliefert.

## C. Architektur

### C.1 Das Paket `surface/`

Ein neues öffentliches Paket unterhalb von `device/`, abhängig nur von
`cluster/spec` und `schema` (ADR 0026, Prinzip 1: Schichten von unten nach
oben; `surface` ist Datenmodell und Regelwerk, kein Protokoll).

```go
// Die Oberfläche eines Knotens, kanonisch geordnet, hashbar, JSON-kodierbar.
type Surface struct {
    Root      Endpoint
    Endpoints []Endpoint          // nach StableKey geordnet
}
type Endpoint struct {
    Key         string            // gerenderter endpoint.SourceKey
    DeviceTypes []DeviceTypeRef   // {ID, Revision}
    Clusters    []Cluster
}
type Cluster struct {
    ID, Revision                 uint32
    FeatureMap                   uint32
    Attributes, Accepted,
    Generated, Events            []uint32
}
func (s Surface) Digest() [32]byte
func Diff(a, b Surface) []Change  // Element, vorher, nachher
```

Das ist genau das, was ein Wildcard-Read von Descriptor und den globalen
Attributen zurückgibt — die „fünf Listen“ aus #331, auf den Knoten
verallgemeinert. Die Quelle im Code ist `spec.Instance`
(`cluster/spec/instance.go:61-69`), die die Listen bereits je Cluster hält.

### C.2 Elemente mit Herkunft

Policies urteilen nicht über die fertige Oberfläche, sondern über
**Kandidaten-Elemente mit Herkunft**. Sonst kann keine Regel zwischen
„Pflicht“ und „Wunsch“, zwischen „Host-Bug“ und „Operator-Entscheid“
unterscheiden.

```go
type Element struct {
    Endpoint  string
    Cluster   uint32
    Kind      Kind        // DeviceType, Cluster, Feature, Attribute, Command, Event
    ID        uint32
    Origin    Origin      // warum der Kandidat existiert
    Reference Reference   // Baseline | Ahead(registerID)
    Levels    LevelSet    // Stufen, in deren Modell das Element existiert
    Mandatory bool        // am Pin für die gewählte Feature-Auswahl mandatiert
}
type Origin uint8 // OriginClusterMandatory, OriginDeviceType, OriginHostDeclared, OriginKitDefault
```

- **Origin** stammt aus dem Builder: `device/type.go:98-120`
  (`ElementRequirement.Presence`) und `device/device.go:507`
  (`clusterMandatedFeatures`) liefern Pflicht und Gerätetyp, die
  Deklaration (`spec.ClusterDeclaration`) liefert Host, der Rest ist
  Kit-Default.
- **Reference** kommt aus dem Code des Servers (C.4).
- **Levels** kommt aus der generierten Stufentabelle (C.5).
- **Mandatory** ist die Konformitätsauswertung am Pin, die
  `spec.CheckFeatures` und die Instanz heute schon machen
  (`cluster/spec/instance.go:105-140`).

### C.3 Policies und ihr Protokoll

```go
type Verdict uint8 // Keep, Remove, Refuse
type Decision struct { Element; Policy string; Verdict; Reason string }

type Policy interface {
    Name() string
    Judge(Element) (Verdict, string)
}

type Policies struct {
    Extensions Extensions   // Register-IDs, die der Host freischaltet (#331)
    Level      Level        // Spezifikationsstufe; Nullwert = Pin (#332)
    Profiles   []Profile    // Controller-Profile mit Evidenz (C.6)
    Custom     []Policy     // Host-eigene Regeln, gleicher Mechanismus
}
func (p Policies) Digest() [32]byte
```

Der Nullwert von `Policies` ist das heutige Verhalten **minus** die
Ahead-Oberfläche: matter.js-Baseline an der Pin-Stufe, keine Profile.

Die Kette wertet **jede** Policy für **jedes** Element aus; es gibt keinen
Kurzschluss. Ein Element bekommt so viele `Decision`-Zeilen wie Policies,
die zuständig waren. Präzedenz beim Zusammenführen: `Refuse` > `Remove` >
`Keep`. Das Protokoll (`Report`) ist vollständig: jedes Kandidaten-Element
erscheint mindestens einmal — ein Guard hält das.

**Zwei Arten von Konflikt, zwei Arten von Urteil.** Das ist die
wichtigste Entwurfsentscheidung des Konzepts:

| Konflikt zwischen … | Urteil | Begründung |
| --- | --- | --- |
| Code und Code (Host deklariert ein Ahead-Element, Host hat es nicht freigeschaltet) | **Refuse**, Build-Fehler mit Register-ID | Beide Seiten sind Entscheidungen desselben Entwicklers; der Widerspruch ist ein Bug und muss laut sein (#331, Entscheidung 3). |
| Code und Operator-Einstellung (Host deklariert ein Element, der Operator hat eine Stufe oder ein Profil gewählt, die es nicht kennen) | **Remove**, Protokollzeile | Code kann die Einstellung nicht vorhersehen; der Operator hat sie bewusst gewählt. Ein Daemon darf wegen einer Operator-Wahl nicht sterben. |
| Pflicht am Pin und Operator-Einstellung | **Keep**, Protokollzeile „kept: mandatory at pin“ | Entfernen bräche die Konformität an der neuesten Stufe (#332, Scope 3). |

Ein Gerätetyp oder Cluster, den die Stufe nicht kennt, wird **nicht
komponiert** — das Gerät erscheint im Protokoll als zurückgehalten, der
Knoten startet. Der Host zeigt das an (loom: Matter-Seite). Das weicht von
#332 Scope 3 („fails with an error“) ab; Begründung in der Tabelle.

### C.4 Die Referenz-Policy (#331)

Der Server, nicht ein Register-Text, weiß, welche Bits und Elemente eine
Register-Zeile implementieren. Er deklariert das in einer Zeile:

```go
// cluster/thermo
var Ahead = surface.AheadElements{
    "BD-chip-Thermostat-Schedules": {Features: ThermostatFeatureMSCH},
    "BD-chip-Thermostat-Events":    {Features: ThermostatFeatureTEVT},
}
```

Die Kit-Server registrieren ihre `AheadElements` beim Builder; der Builder
setzt daraus `Element.Reference`. Die Policy ist dann trivial: `Ahead(id)`
und `id ∉ Extensions` → `Refuse`, Fehler `ErrExtensionNotEnabled` mit der
Register-ID im Text.

**Deklaration und Messung sind getrennt.** Die Deklaration sagt, welche
Elemente zur Zeile gehören. Die Messung (`make ahead-check`, erweitert)
baut jeden betroffenen Cluster mit und ohne diese Elemente und vergleicht
die fünf Listen; das Ergebnis ist die neue Register-Spalte `surface`
(yes/no). Vollständigkeit in beide Richtungen: jede `AheadElements`-ID ist
eine Register-Zeile, jede Zeile mit `surface: yes` hat eine Deklaration.
Beißprobe: eine Deklaration entfernen → Guard rot. Die Kandidatenliste aus
#331 wird dabei **gemessen, nicht übernommen**.

Was nicht zur Oberfläche gehört — die `…RulesInServer`-Zeilen
(`by_design.md:81-91, 93-107`) — hat keine Deklaration und keine Policy.
Es bleibt an (#331, Entscheidung 2).

### C.5 Die Stufen-Policy (#332) — und ihre lizenzsaubere Quelle

#332 nennt als Datenquelle connectedhomeips `data_model/<ver>`. Das
widerspricht ADR 0015: die XML-Dateien tragen einen CSA-Hinweis, der
Veröffentlichung und abgeleitete Werke verbietet
(`docs/adr/0015-chip-data-model-read-at-run-time.md:26-36`); eine
committete Tabelle aus ihnen ist beides. Das Konzept löst das mit einer
Quelle, die ohnehin unser Goldstandard ist:

**matter.js' eigene Releases tragen das Modell je Spezifikationsrevision.**
Gemessen mit `git show <tag>:packages/model/src/common/Specification.ts`:

| matter.js-Tag | `Specification.REVISION` |
| --- | --- |
| v0.11.0, v0.12.0 | 1.3.0.1 |
| v0.13.0 | 1.4 |
| v0.14.0, v0.15.0 | 1.4.1 |
| v0.16.0 | 1.4.2 |
| v0.17.0 | 1.5.1 |
| v0.17.9 | 1.6.0 |
| v0.18.0 (7e1f5d789, 2026-10-20) | 1.6.1 |

Apache-2.0, derselbe Extraktor (`script/extract-from-matter-js.ts`),
dasselbe Snapshot-Format. Die Stufen sind damit **die Revisionen, die
matter.js je ausgeliefert hat** — und das ist die inhaltlich richtige
Definition: ein Controller auf matter.js-Basis einer Generation hat genau
dieses Modell gesehen. Es gibt keine Stufe „1.5“ (matter.js sprang von
1.4.2 auf 1.5.1); die Frage nach wählbaren Patch-Stufen aus #332 beantwortet
die Datenlage: wählbar ist, was es gibt.

**Unverifiziert, Phase S3 prüft es zuerst:** ob der heutige Extraktor an
den älteren Tags läuft (die „effective“-Schicht nutzt APIs, die v0.13.0
vielleicht nicht hat). Rückfallebene: ein schlanker Stufen-Extraktor, der
nur IDs, Revisionen, Feature-Codes und die Konformität liest — die
`*.element.ts`-Dateien sind reine Daten.

Erzeugt wird **eine** Tabelle `schema/levels_gen.go`: je Element eine
`LevelSet` (Bitmenge der Stufen, in deren Modell es existiert) — eine
Bitmenge, kein „seit Revision“, weil die Geschichte nicht monoton ist
(#332: `SCH` fehlt in 1.5.1 bis 1.6.1 und ist in 1.7 wieder da). Elemente
werden über IDs gematcht, nie über Namen (#332, zweite Konsequenz).

**Gegenprobe ohne Lizenzbruch:** `internal/chipdm` liest zur Testzeit
bereits `data_model/<ver>` und hält es nur im Speicher
(`internal/chipdm/model.go:28-31`). Ein neuer Test vergleicht die
generierte Stufentabelle mit den Versionen, die beide Seiten haben (1.4,
1.4.1, 1.4.2, 1.5.1, 1.6, 1.6.1), und listet Differenzen nach dem Muster
von `acknowledged.go`. So bekommt #332 seine „gemessene“ Qualität, und
committet wird weiterhin nichts aus dem XML.

Die Policy selbst: Element optional **und** Stufe ∉ `Levels` → `Remove`;
Element mandatiert am Pin **und** Stufe ∉ `Levels` → `Keep` mit Grund;
Gerätetyp oder Cluster ∉ `Levels` → Gerät zurückgehalten.
`ClusterRevision` bleibt immer die des Pins (Verhalten bleibt neueste
Revision, #332 Out of scope). Die Ausnahmeliste („mandatiert am Pin, an
Stufe L unbekannt“) wird aus der Tabelle **berechnet**, nie geschrieben,
und von `make docs` in `docs/reference/spec-levels.md` veröffentlicht.

Ein Wert oberhalb des Pins wird mit `ErrLevelAbovePin` abgelehnt.

### C.6 Controller-Profile — die Achse, die eigentlich fehlt

#332 stellt es selbst fest: an Stufe 1.5 wären MSCH (1.4) und
Hold/RunningState (1.0) alle noch bedient; die Stufe hätte den
Apple-Ausfall nicht verhindert. Und #331 sagt: die Baseline verspricht
matter.js-Konformität, nicht Controller-Verträglichkeit. Die Achse, die den
Vorfall adressiert, ist eine dritte: **was ein konkreter Controller
verträgt**.

go-fabric hat den Keim dazu schon, auf der Client-Seite:
`eligibility.Compat` und `EcosystemForVendor`
(`eligibility/eligibility.go:21-24`, `eligibility/compat.go`) wissen,
welche Gerätetypen ein Controller stillschweigend fallen lässt. Die
Device-Seite bekommt das Gegenstück:

```go
type Profile struct {
    Name     string            // "apple-home", "google-home", "alexa"
    Removals []ProfileRule     // Element-Muster → Grund, Konfidenz, Quelle
}
```

Die Regeln stammen aus **einem Register mit Evidenz**, geführt nach den
Konfidenzstufen der bestehenden Seite `docs/matter-ecosystem-observations.md`
(Reproduced / Inherited / Reported, Zeilen 29-38). Es wird **leer**
ausgeliefert: heute ist kein Element gemessen, und das Konzept behauptet
keines. Die erste Zeile kommt aus dem Owner-Handtest, den #331 motiviert:
Apple Home, Thermostat, MSCH allein, dann Hold allein, dann RunningState
allein — je ein Pairing, je ein Befund. Ein Profil mit einer Zeile der
Konfidenz *Reported* darf im Register stehen, aber nur eine *Reproduced*-
Zeile darf im ausgelieferten Profil ein Element entfernen (Owner-
Entscheidung erbeten, E.5).

Ein Host wählt Profile, wie er die Stufe wählt: als Operator-Einstellung.
Konflikte sind deshalb `Remove`, nie `Refuse`.

### C.7 Wo die Policies greifen

Heute baut `device.New` (`device/device.go:117`) seine Server sofort; die
Feature-Auswahl entscheidet die `spec.Instance`. Die Policies müssen also
**vor** dem Server-Bau wirken. Entscheidung: `device.Options` bekommt
`Surface *surface.Policies`; `node.Config` bekommt `Surface
surface.Policies`. Ein Gerät, das mit einem anderen `Policies.Digest()`
gebaut wurde als der Knoten, den es betritt, wird beim `Add` mit
`ErrPolicyMismatch` abgewiesen. Vergisst ein Host das Feld, gilt der
Nullwert: Baseline, Pin, keine Profile — die sichere Seite. Ein Host
verliert durch das Vergessen nur seine Operator-Einstellungen, nie den
Schutz.

Die Alternative — der Knoten komponiert das Gerät erst beim `Add`, wie
matter.js' Endpoint seine Behaviors erst bei `node.add` initialisiert —
ist das langfristig sauberere Modell („der Knoten besitzt die
Komposition“). Es ist ein Bruch der `device`-API und kommt, wenn
überhaupt, mit v1.0; dieses Konzept braucht es nicht und hält sich den Weg
offen, indem die Komposition schon jetzt eine Funktion
`compose(declarations, policies) → (servers, Report)` wird.

### C.8 Introspektion

```go
func (n *Node) Surface() surface.Report      // effektive Oberfläche + jede Decision
func (d *Device) Surface() surface.Report
type Report struct {
    Policies  [32]byte
    Effective Surface
    Decisions []Decision
    Withheld  []WithheldDevice             // Geräte, die eine Stufe nicht komponiert hat
}
```

Der Referenz-Daemon bekommt `--dump-surface` (JSON auf stdout, Exit 0).
Das ist zugleich das Messwerkzeug für die Guards (C.10) und das, was loom
auf seiner Matter-Seite zeigt: je Endpoint die Register-IDs, die er
bedient, die Stufe, die Profile, die entfernten Elemente mit Grund.

### C.9 Stabilität der Oberfläche über die Zeit

Der Vorfall in loom war ein **nachträgliches** Einschalten. `FeatureMap`
trägt die Qualität F (fixed; `cluster/spec/model.go:67`); ein Controller
darf sie cachen. Der Standard hat seit 1.4 ein Signal dafür, dass sich die
Konfiguration eines Geräts geändert hat: `ConfigurationVersion` auf
BasicInformation und BridgedDeviceBasicInformation — beide im Snapshot
(`parity/schema.json:27490, :44979`), die Endpoint-Schicht trägt den Wert
schon (`endpoint/spec.go:87-90`, `Endpoint.IncreaseConfigurationVersion`).

Regel: der `Surface.Digest()` jedes Endpoints wird mit dem Endpoint-
Datensatz persistiert (`endpoint.Record`, neben `DeviceType`, das
`assignOrReuseID` heute schon bei Drift aktualisiert,
`endpoint/assembler.go:325-340`). Weicht der Digest bei Reassembly oder
Neustart für denselben `StableKey` ab, gilt der konfigurierte Modus:

| Modus | Wirkung | Wann |
| --- | --- | --- |
| `Announce` (Default) | `ConfigurationVersion` des Endpoints steigt, Descriptor-DataVersion steigt, ein `slog`-Ereignis `surface changed` mit dem Diff | Der standardkonforme Weg. |
| `Reendpoint` | Die Endpoint-Nummer wird zurückgezogen und neu vergeben; der Controller sieht ein Gerät verschwinden und ein neues erscheinen | Für Controller, die `ConfigurationVersion` nachweislich ignorieren (Profil-Regel) oder auf Owner-Wunsch. |

Für den Root-Endpoint gibt es nur `Announce`.

**Unverifiziert, Phase S4 liest es zuerst:** ob und wie matter.js
`ConfigurationVersion` bei einer Behavior-Änderung erhöht
(`packages/node/src/behaviors/basic-information/`,
`bridged-device-basic-information/`). Tut es das, wird es gespiegelt und
zitiert; tut es das nicht, ist die Regel eine Register-Zeile *Ahead of
matter.js* mit chip als Verhaltensquelle — oder, falls auch chip nichts
tut, ein `by_design`-Eintrag.

### C.10 Guards

1. **Oberflächen-Snapshot** (`script/surfacecheck`, Ratsche wie
   `script/reachability`): komponiert die Topologie des Referenz-Daemons
   und einen festen Satz Referenzgeräte mit Nullwert-Policies und schreibt
   `surface.json`. CI vergleicht. Jede Änderung dessen, was ein Controller
   sieht, ist damit ein Review-Diff im Pull Request. Das ist der stärkste
   einzelne Guard dieses Konzepts.
2. **ahead-check** (erweitert): Spalte `surface` gemessen;
   Vollständigkeit beidseitig; Beißprobe.
3. **Stufen-Guards**: Tabelle regeneriert und verglichen; je Stufe ein
   komponiertes Referenzgerät ohne optionales Element, das die Stufe nicht
   kennt (Negativkontrolle: am Pin wird es bedient); Ausnahmeliste
   berechnet und verglichen; chipdm-Gegenprobe zur Testzeit.
4. **Policy-Kette**: Präzedenz; Protokoll-Vollständigkeit; `Add` mit
   fremdem Digest abgewiesen; Nullwert = Baseline (die fünf Listen jedes
   gegateten Clusters gleichen matter.js).
5. **Stabilität**: Digest-Drift → `ConfigurationVersion` steigt;
   `Reendpoint` vergibt neu; kein Drift → nichts passiert.
6. **chip-tool**: Default-Build unverändert, Commissioning-Guard grün. Die
   Erweiterung `BD-chip-Thermostat-Schedules` auf einem Thermostat-
   Endpoint des Referenz-Daemons (Flag `--extensions`), PICS
   `TSTAT.S.F07=1` dort, `TC_TSTAT_4_5` in der TSTAT-Familie — oder jede
   ausgeschlossene Stufe mit Klasse und Grund in `docs/certifiability.md`
   (#331 Addendum). Ob das Harness-Image den Fall trägt, ist
   **ungeprüft**; fehlt er, gehört das Heben von `CHIP_TEST_IMAGE` in S2.

## D. Entscheidungen, die das Konzept trifft

1. **Die Oberfläche ist ein Wert mit Herkunft je Element**, und jede Regel
   über sie ist eine `Policy` mit Protokoll. Kein Sonderpfad je Issue.
2. **Code-gegen-Code ist `Refuse`, Code-gegen-Operator ist `Remove`.** Das
   ersetzt #332s Startfehler durch ein zurückgehaltenes Gerät im Protokoll.
3. **Die Stufenquelle ist matter.js' Release-Historie**, nicht CSA-XML.
   ADR 0015 bleibt unberührt; chipdm ist Gegenprobe zur Testzeit.
4. **Stufen sind die Revisionen, die matter.js ausgeliefert hat.** Keine
   synthetische 1.5.
5. **Controller-Profile werden gebaut und leer ausgeliefert.** Eine Zeile
   entsteht nur durch Messung mit Quelle.
6. **Nichts ist eine Wand.** Eine Register-ID in `Extensions` schaltet
   jedes Ahead-Element frei; `Custom` nimmt Host-Regeln; Stufe und Profile
   sind Operator-Wahl. Was loom unbedingt anbieten muss, schaltet loom in
   einer Zeile frei, und C.9 macht das Einschalten für gepairte Controller
   verträglich.
7. **Ganze Cluster werden nicht gegatet** (#331, Owner 2026-10-29). Welche
   Cluster ein Endpoint erreicht, entscheiden die Gerätetypen des Hosts.
8. **Verhalten bleibt immer das der Pin-Revision.** Stufe und Profil
   entfernen Oberfläche, nie Logik.

## E. Offene Fragen an den Owner

1. **`Reendpoint` als Modus** — gewünscht, oder reicht `Announce` plus
   Protokoll? `Reendpoint` kostet dem Nutzer Raumzuordnungen im
   Controller.
2. **Policies je Gerät überschreibbar** (#331, offene Frage)? Vorschlag:
   nein. Ein Knoten, eine Oberfläche; wer je Gerät differenzieren will,
   schreibt eine `Custom`-Policy, die auf `Element.Endpoint` prüft.
3. **Root-Oberfläche unter der Stufe** (#332, offene Frage)? Vorschlag:
   ja, dieselbe Regel; die Ausnahmeliste zeigt, was bleibt.
4. **TEVT**: Register-Zeile oder nach dem nächsten Pin-Lift aufgeholt?
   `make ahead-check` entscheidet; das Konzept nimmt nichts an.
5. **Profil-Regel-Schwelle**: entfernt nur *Reproduced*, oder auch
   *Reported*? Vorschlag: *Reproduced* entfernt, *Reported* steht im
   Register und im Protokoll als Warnung.
6. **Paketname** `surface` — oder `exposure`? `surface` ist kurz und
   deckt sich mit der Sprache in #331.

## F. Phasen

Eine Welle je Phase (Wellen-PR-Regel); S1 ist Voraussetzung für alle
weiteren, S2 bis S4 sind untereinander unabhängig, S5 setzt S4 voraus.

| Phase | Inhalt | Modell | Abnahme |
| --- | --- | --- | --- |
| **S1 Wert und Protokoll** | `surface/` (Surface, Element, Policy, Policies, Report), Komposition als `compose(...)`, `Node.Surface()`, `Device.Surface()`, `--dump-surface`, `script/surfacecheck` mit erstem Snapshot. Keine Verhaltensänderung. | Fable (Entwurf, Paket-Doku), Opus (Implementierung nach Brief) | Snapshot committet; Protokoll vollständig; alle Guards grün; `apidiff` meldet nur Zusätze. |
| **S2 Referenz** | `AheadElements` in `thermo` und jedem Server, den die Messung nennt; Referenz-Policy; `ErrExtensionNotEnabled`; `ahead-check` mit Spalte `surface`, Vollständigkeit, Beißprobe; `--extensions` im Daemon; PICS; `TC_TSTAT_4_5`; CHANGELOG **Breaking** für Hosts, die MSCH/TEVT setzen. | Fable (Register, Messung, `by_design`-Zeilen), Opus (Gates) | Default-Build: fünf Listen = matter.js für jede gegatete Zeile; Commissioning-Guard grün; 4.5 läuft oder ist klassifiziert. |
| **S3 Stufe** | Extraktor an den matter.js-Tags verifizieren (oder Stufen-Extraktor); `schema/levels_gen.go`; Stufen-Policy; Ausnahmeliste berechnet; `docs/reference/spec-levels.md`; chipdm-Gegenprobe; `--spec-level` im Daemon; ADR-Nachtrag zu 0015 (Quelle der Stufen). | Sonnet (Extraktion, Generator), Fable (Policy-Regeln, ADR), Opus (Guards) | `--spec-level 1.4.2`: kein Closure, keine Kamera, kein Chime komponiert; Thermostat ohne TEVT/TSUGGEST; am Pin unverändert. |
| **S4 Stabilität** | matter.js zu `ConfigurationVersion` lesen; Digest im `endpoint.Record`; `Announce`; optional `Reendpoint`; Guards. | Fable | Drift-Test: Version steigt; Neustart ohne Drift: nichts. |
| **S5 Profile** | `Profile`, Register `notes/parity/controller_profiles.md` mit Konfidenzspalte; Owner-Handtest Apple Home (MSCH / Hold / RunningState je einzeln); erste Zeilen; `eligibility.Compat` als Client-Zwilling dokumentiert. | Fable; Handtest Owner | Profil `apple-home` existiert; entfernt genau die *Reproduced*-Zeilen; Protokoll nennt jede. |

Begleitend: **ADR 0030** „Die bediente Oberfläche ist ein Wert unter
Policies“ (Entscheidungen D.1 bis D.8), `docs/architecture.md` erhält den
Abschnitt *Surface*, `docs/matter-parity-contract.md` den
Oberflächen-Snapshot als Guard, Roadmap-Zeile im Block nach M6.

## G. Was das Konzept nicht tut

- Es behauptet nicht, welche Stufe oder welches Profil ein Controller
  verträgt. Es macht Einschränkungen reproduzierbar und Messungen
  speicherbar.
- Es verhält sich nicht wie eine ältere Cluster-Revision.
- Es gatet keine ganzen Cluster und ersetzt nicht die Gerätetyp-Wahl des
  Hosts.
- Es verlegt keine Operator-Oberfläche in dieses Modul: welche Erweiterung,
  Stufe oder Profil ein Host seinen Operatoren zeigt, bleibt Host-Sache
  (loom: ein Expertenschalter für die Thermostat-Wochenprogramme, eine
  Stufenwahl, später eine Profilwahl).

## H. Loom-Folge (eigenes Issue dort)

loom setzt `Extensions("BD-chip-Thermostat-Schedules")`, wenn
`north.matter.thermostat_schedules` an ist, und streicht die eigene
Feature-Bit-Entscheidung (`internal/model/custom/climate/matter_schedule.go:157`,
laut #331). Stufe und Profil werden Operator-Einstellungen. Die
Matter-Seite zeigt `Node.Surface()`. Hold und RunningState bleiben in loom
aus, bis der Handtest aus S5 sie gemessen hat — nicht, weil go-fabric sie
hindert, sondern weil ihr Apple-Verhalten unbekannt ist.
