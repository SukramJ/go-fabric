# Konzept v9 — go-fabric-ui: eine Oberfläche für alle Daemons der Familie

Stand 2026-10-25. Nachfolger von [Konzept v8](./concept-v8-fabric-server.md)
(die Familie und ihre Rollen). Gegenstand: eine zentral gepflegte
Web-Oberfläche für Auswahl, Kopplung, Health und Diagnose, die jeder Daemon
der Familie nur einbindet. Quellen: openccu-loom `b40b289a` (die fünf
Matter-Seiten unter `assets/ui/src/routes/matter/`, die REST-Routen
`internal/north/rest/router.go:1328-1359`, die Handler
`internal/north/rest/handlers/matter*.go`, die Tokens `assets/ui/src/app.css:60-128`,
die Theme-Brücke `assets/ui/src/lib/theme/ha-bridge.ts`), go-ha2fabric
`v0.1.0` (`internal/web`, eine Read-only-Seite), go-fabric `v0.12.0`
(`node.Node`-Facade), Lesekarte `read-u1-loom-matter-ui.md`.

## 0. Entscheidungen des Owners (2026-10-25)

- **0.1** Eigenes Repository **go-fabric-ui**. Eine Version liefert zwei
  Artefakte: ein Go-Modul (`github.com/SukramJ/go-fabric-ui`, Paket
  `fabricui`: HTTP-Handler auf der `node.Node`-Facade, der Vertrag, das
  fertig gebaute Bundle per `go:embed`) und ein npm-Paket
  `@sukramj/fabric-ui` (Svelte-Seiten, UI-Kit, Tokens, TS-Client) für
  Projekte mit eigener SPA.
- **0.2** Technik **Svelte 5 + Vite + Tailwind 4**, wie openccu-loom; Build
  im CI. Optik wie Home Assistant über looms Token-Konvention und die
  Theme-Brücke (A.3).
- **0.3** Zwei Seitenfamilien: **Node** (Geräteseite: Health, Kopplung,
  Fabrics, Auswahl, Endpoints, Diagnose) in Welle **U1**, aus loom
  extrahiert; **Controller** (Kommissionieren, Discovery, Nodes,
  Subscriptions, Bindings/ACL, Multi-Admin, OTA, Thread) in Welle **U2**
  nach CJ/S3.
- **0.4** Der Auswahlzustand eines Daemons (Entity-/Label-Overrides) liegt
  beim Daemon unter `/data`, geschichtet über die Add-on-Optionen; nie in
  den Supervisor-Optionen.
- **0.5** Reihenfolge: **U1 vor H3**. H3 (go-ha2fabric: Labels,
  go-hamapping-Folgen, Bugfix, matter-hub-Vergleich) konsumiert
  go-fabric-ui und liefert den HA-Provider.
- **0.6** npm-Ziel **npmjs.org `@sukramj/fabric-ui`**; Konto und
  Publish-Token sind Owner-Schritte (G).
- **0.7** go-fabric-server als HA-Add-on: Entscheidung nach S3.
- **0.8** Kein „matter" im Repository- und Paketnamen; Routen heißen
  `/api/v1/node/…` und `/api/v1/controller/…`.

## Teil A: Ausgangslage

### A.1 Was openccu-loom heute hat

Fünf Svelte-Seiten (`Matter.svelte` 123 Zeilen als Shell mit Tabs,
`MatterPair.svelte` 279, `MatterFabrics.svelte` 209,
`MatterExposureList.svelte` 584, `MatterDiagnostics.svelte` 352), ein Store
`matter.svelte.ts` (381; Dirty-Map für Auswahländerungen, eine
Bulk-Schreiboperation, sechs WebSocket-Ereignistypen) und hand-geschriebene
Typen `api/matter-types.ts` (218). Dahinter 17 REST-Routen unter
`/api/v1/matter/*` (`router.go:1328-1359`): status, fabrics (+ DELETE),
sessions, mdns, endpoints, compatibility, events, force-sync,
factory-reset, setup-payload, exposable (GET, PUT, bulk),
commissioning/window (open, close), share. Alles Schreibende ist
Admin-gated. Die Handler rufen die go-fabric-Facade: `node.Node.RemoveFabric`,
`OpenBasicWindow`, `PairingCodes`, `CommissioningWindow().OpenOwnWindow/RevokeWindow`,
`bridge.Topology()`/`Reassemble`, `Sessions()`, `Subscriptions()`, den
mDNS-Advertiser, den Fabric-Store (Lesekarte §2).

Loom-spezifisch ist nur die Auswahl: ein „exposable item" ist ein
`matteradapter.Candidate{Key SourceKey, DisplayName, ChannelType, Verdict eligibility.Verdict}`
(`candidates.go:28-33`), der Schlüssel fünfteilig
(`CentralName|DeviceAddress|ChannelNo|DPKind|DPKey`), der Speicher ein
`matterendpoint.Store` mit `Get/List/Upsert/CountEnabled`
(`matter_exposures.go:25-29`). Die Seite zeigt CCU-Felder
(`central_name`, `device_address`, `channel_no`, `dp_kind`, `dp_key`,
`parameter_label`). Alles andere auf den fünf Seiten ist hostneutral.

### A.2 Was go-ha2fabric heute hat

Eine serverseitig gerenderte Read-only-Seite (`internal/web`): QR,
manueller Code, Fabrics, exponierte Entitäten mit Verdikt, HA-Status,
Health; `GET /api/health`, `GET /api/plan`; Ingress nur von `172.30.32.2`.
Kein Schreiben, keine Auswahl in der UI.

### A.3 Wie die HA-Optik entsteht

Loom definiert Verbrauchs-Tokens mit HA-Namen (`--ha-primary-color`,
`--ha-card-background-color`, `--ha-divider-color`, …; `app.css:60-82`,
Dark-Block `:84-104`) und einen HA-Skin, der sie aus HAs eigenen Variablen
auflöst (`html[data-skin="ha"]`, `app.css:106-128`). Die Theme-Brücke
`ha-bridge.ts` erkennt den Ingress-Iframe (`isEmbedded`, `:64`) und kopiert
HAs echte CSS-Variablen (`HA_THEME_VARS`, `:13ff`) vom Elternfenster auf
`:root`, inklusive Hell/Dunkel aus der Hintergrundluminanz. Ingress läuft
unter HAs Origin, deshalb ist der Zugriff auf das Elternfenster erlaubt.
Ergebnis: im Add-on-Panel trägt die Seite das aktive HA-Theme, außerhalb
die HA-Defaults. Genau das will der Owner; es wird übernommen, nicht neu
erfunden.

### A.4 Was go-fabric liefert

`node.Node`: `Health`, `FabricInfos`, `RemoveFabric`, `OpenBasicWindow`,
`CommissioningWindow`, `PairingCodes`, `PairingCodesFor`,
`DiagnosticEvents`, `Topology`, Diagnose-Zähler (Sessions, Subscriptions)
über die Observability-Facade (v0.10.6). Für die Node-Familie fehlt der
Facade nichts Großes; was loom aus `bridge`-Seams liest (mDNS-Aktivität,
Session-Belegung), prüft U1 gegen die Facade und meldet Lücken für ein
go-fabric-Bündel (CK), statt Seams zu nutzen.

## Teil B: Architektur

### B.1 Repository go-fabric-ui

```
go-fabric-ui/
  go.mod                     github.com/SukramJ/go-fabric-ui; Abhängigkeit: go-fabric (node, eligibility)
  fabricui/                  Paket fabricui: Mount, Options, Handler, Provider-Schnittstellen
  fabricui/internal/api/     Handler je Route, portiert aus loom (mit den 72 Handler-Tests)
  fabricui/dist/             das gebaute Standalone-Bundle (go:embed all:dist), siehe B.5
  contract/openapi.yaml      der Vertrag v1 (Abschnitte node, controller)
  contract/fixtures/*.json   goldene Antworten; Go- und TS-Tests lesen dieselben Dateien
  ui/                        npm-Paket @sukramj/fabric-ui (Svelte-Lib) + Standalone-Shell
  ui/src/lib/pages/node/     Health, Pair, Fabrics, Selection, Endpoints, Diagnostics
  ui/src/lib/pages/controller/  U2
  ui/src/lib/kit/            Badge, Button, Card, DataTable, Empty/Error/LoadingState, Icon, Input, PageHeader, PageShell, Select, Tabs
  ui/src/lib/theme/          tokens.css (HA-Namen), ha-bridge.ts
  ui/src/lib/i18n/           en.json, de.json; t() injizierbar
  ui/src/lib/api/            TS-Client, aus openapi.yaml generiert (openapi-typescript)
  ui/src/shell/              Standalone-App: Hash-Routing, Navigation nach vorhandenen Familien
  docs/                      Einbindung (Go-Modul, npm), Vertrag, Provider schreiben
```

### B.2 Die Go-Hälfte

```go
type Options struct {
    Node       NodeSource         // *node.Node erfüllt es; nil = keine Node-Familie
    Exposure   ExposureProvider   // nil = keine Auswahl-Seite
    Controller ControllerSource   // U2; nil = keine Controller-Familie
    Admin      func(*http.Request) bool // Schreibrechte; Ingress-Hosts: immer true hinter dem Proxy
    Events     EventSource        // optional: Änderungen als Server-Sent Events
    Prefix     string             // "/api/v1" Standard
}
func Mount(mux *http.ServeMux, o Options) error   // API-Routen
func Handler(o Options) http.Handler               // API + eingebettetes Bundle (Standalone)
```

`NodeSource` ist das Methoden-Subset von `*node.Node`, das die Node-Familie
liest (A.4); kein Seam unterhalb der Facade. `ExposureProvider` ist
generisch, damit die Auswahl-Seite ohne Host-Begriffe auskommt:

```go
type ExposureProvider interface {
    Schema(ctx) (Schema, error)                 // Spalten (Key, Label, Filterbar), Gruppierung, Filter
    List(ctx) ([]Item, error)                   // Key, Fields map[string]string, Group, Name, Verdict, Enabled, Source
    Preview(ctx, []Update) ([]Item, error)      // Trockenlauf: Verdikte nach den Änderungen
    Apply(ctx, []Update) (applied int, err error)
}
```

`Verdict` ist go-fabric `eligibility.Verdict` (State, DeviceType, Clusters,
Reason). `Source` sagt, woher ein Verdikt kommt (Regel, Override, Label,
Assist), damit die Seite die Schichtung aus 0.4 anzeigen kann. Loom
implementiert den Provider über `matteradapter.Candidate` und seinen
Store; go-ha2fabric über die H2-Regel plus Overrides unter `/data`.

### B.3 Die Svelte-Hälfte

Die Seiten importieren nur aus `$lib` des Pakets: Kit, Tokens, `t` und
Toast als Kontext-Schnittstellen (der Host reicht seine Implementierung
ein; die Standalone-Shell bringt eigene), den generierten Client. Keine
Host-Begriffe: die Auswahl-Seite rendert `Schema.Columns`, Filter aus
`Schema.Filters`, Gruppen aus `Item.Group`. 64-Bit-Ids kommen als Hex
(`*_hex`, loom `matter-types.ts`-Hinweis) und werden nie als JS-Number
gerechnet. Der QR wird clientseitig aus dem Payload gezeichnet (loom
`qr.ts` mit `qrcode` MIT; go-ha2fabric H2 hat einen eigenen Encoder — U1
nimmt einen, begründet).

### B.4 Der Vertrag

`contract/openapi.yaml` ist die eine Quelle. Go-Handler und TS-Client
werden gegen dieselben goldenen Fixtures getestet (`contract/fixtures`),
ohne zusätzliche Go-Abhängigkeit (kein OpenAPI-Validator im Go-Modul; die
TS-Seite prüft Schema-Konformität mit `openapi-typescript`-Typen, die
Go-Seite die Fixtures byteweise nach Normalisierung). Routen Node-Familie
v1 (aus looms Tabelle, umbenannt, hostneutral):

| Route | Methode | Quelle in go-fabric |
| --- | --- | --- |
| `/node/status` | GET | `Health`, `FabricInfos` (Anzahl), `Topology` (Endpoints), `CommissioningWindow` (offen, Dauer), mDNS-Status |
| `/node/fabrics`, `/node/fabrics/{index}` | GET, DELETE (admin) | `FabricInfos`, `RemoveFabric` |
| `/node/pairing` | GET (admin) | `PairingCodes` (QR-Payload, manueller Code, Discriminator, Passcode) |
| `/node/pairing/window`, `/node/pairing/window/close` | POST (admin) | `OpenBasicWindow` mit Zufalls-Credentials oder `CommissioningWindow().OpenOwnWindow`, `RevokeWindow`; 409 wenn offen |
| `/node/endpoints` | GET | `Topology` (Nummer, Eltern, Gerätetyp, Revision, Reachable, Name, Cluster) |
| `/node/sessions` | GET | Diagnose-Facade (Sessions, Belegung, Subscriptions je Session) |
| `/node/mdns` | GET | Advertiser-Status, Dienste, Befunde |
| `/node/compatibility` | GET | Ökosysteme aus Fabric-Vendor-Ids (DCL-Vendor-Tabelle), Befunde |
| `/node/events` | GET | `DiagnosticEvents` |
| `/node/exposure/schema`, `/node/exposure` | GET | Provider `Schema`, `List` |
| `/node/exposure/preview`, `/node/exposure` | POST, PUT (admin) | Provider `Preview`, `Apply` (Bulk; eine Transaktion ist Sache des Providers) |
| `/node/maintenance/reassemble`, `/node/maintenance/factory-reset` | POST (admin) | Host-Hook (`Options.Reassemble`), `RemoveFabric` je Fabric mit Bestätigungs-Body |
| `/node/stream` | GET | Server-Sent Events: fabric_added/removed, window_opened/closed, endpoints_changed, exposure_changed |

SSE statt looms WebSocket-Hub: ein Host ohne eigenen Hub braucht nichts
weiter; loom speist seinen Hub aus derselben `EventSource`. Die
Controller-Familie (U2) wird im Vertrag angelegt, aber erst mit CJ/S3
gefüllt: `/controller/commission` (Code/QR, Netz, Fortschritt),
`/controller/discovery`, `/controller/nodes`, `/controller/nodes/{id}/…`
(Endpoints, Attribute, Subscriptions, Bindings, ACL, Kopplungsfenster
öffnen, OTA, Thread-Datensatz), `/controller/fabric/label`.

### B.5 Build, Veröffentlichung, Einbettung

- `ui/` baut mit Vite nach `fabricui/dist/` (`base: './'`, Hash-Routing,
  Ingress-sicher). Das Dist **ist committed**: ein Go-Modul wird aus dem
  Git-Tag geholt, ein Konsument ohne Node muss das Bundle mitbekommen.
  Ein CI-Job baut das Bundle neu und schlägt fehl, wenn das committete
  Dist nicht byteidentisch ist (reproduzierbarer Build mit festgepinnten
  Versionen und `npm ci`). Alternative, verworfen: Dist nur im
  Release-Branch bauen — zwei Zweige, die auseinanderlaufen.
- Release: ein Tag `vX.Y.Z` veröffentlicht das npm-Paket (gleiche
  Version) und erzeugt das GitHub-Release; `make version-check` hält
  `ui/package.json`, einen Go-Versionsstring und den Changelog zusammen
  (die Lehre aus go-fabric-server und go-ha2fabric).
- Ein Daemon ohne eigene SPA: `mux.Handle("/", fabricui.Handler(opts))`,
  kein Node im eigenen Repo, kein zweites Image. Loom: npm-Paket in seiner
  Shell, Go-Modul für die Handler; seine fünf Seiten und 17 Routen
  entfallen in einer eigenen loom-Welle.
- Abhängigkeiten Go: go-fabric, stdlib. JS: svelte, vite,
  tailwindcss, @tailwindcss/vite, qrcode, lucide (Icons), bits-ui nur
  wenn das Kit es braucht (prüfen; weniger ist besser); alles MIT.
  `THIRD-PARTY-NOTICES.md` führt die npm-Pakete, die im Bundle landen
  (loom tut das heute nicht; wir schon).
- Sicherheit: Schreibrouten nur mit `Options.Admin`; im Ingress-Fall
  prüft der Host die Proxy-Quelle (go-ha2fabric: `172.30.32.2`), die
  Standalone-Shell sendet bei Schreibaktionen ein Header-Token aus
  `/node/status` (CSRF-Schutz gegen fremde Seiten im selben Browser).
  Secrets (Passcode) erscheinen nur in der Admin-Route `/node/pairing`.

## Teil C: Wellen

| Welle | Repo | Inhalt | Ergebnis |
| --- | --- | --- | --- |
| **U1** | go-fabric-ui (neu, B.4-Checkliste aus v7) | Vertrag v1 Node-Familie; Go-Modul mit Handlern (loom-Port mit Tests), `ExposureProvider`, SSE, eingebettetes Bundle; Svelte-Lib mit den sechs Node-Seiten (Auswahl generisch), Kit, Tokens, Theme-Brücke, i18n EN/DE, generierter Client, vitest-Tests; Standalone-Shell; Dist-Guard; Release-Pipeline mit npm-Publish; Docs | `v0.1.0`; `go get` liefert die UI |
| **H3** | go-hamapping, go-ha2fabric | wie Brief H3 (Labels, Q3–Q5, Bugfix, matter-hub-Vergleich) **plus** `fabricui.Handler` statt `internal/web`, HA-`ExposureProvider` (Regel + Overrides unter `/data/selection.json`, Quelle je Verdikt), Ingress-Admin-Check | go-ha2fabric `v0.2.0` |
| **L1** | openccu-loom | eigene Matter-Seiten und `/api/v1/matter/*` durch `@sukramj/fabric-ui` und `fabricui.Mount` ersetzen; CCU-Provider; Hub aus SSE speisen | eine Quelle für alle |
| **U2** | go-fabric-ui | Controller-Familie gegen `controller.Controller`/`client.Node` nach CJ/S3 | `v0.2.0`; go-fabric-server bindet beide Familien |
| **CK** | go-fabric | Facade-Lücken, die U1 meldet (mDNS-Status, Session-Belegung als Facade statt Seam), eine Zeile je Lücke | Patch-Release |

## Teil D: Besser als andere, besser als bisher, ist die Aufteilung richtig

### D.1 Besser als home-assistant-matter-hub und loom heute

- **Ein Ort, drei Konsumenten.** matter-hub hat eine React-App für genau
  ein Produkt; loom hat Seiten, die nur loom nutzen kann. go-fabric-ui
  trennt hostneutral (Node-Betrieb) von hostspezifisch (was exponierbar
  ist) an einer Schnittstelle, die aus loom abgeleitet ist und dort
  bewiesen hat, dass sie trägt.
- **Kein Node-Toolchain beim Konsumenten.** Das Go-Modul bringt das
  Bundle mit; go-ha2fabric und go-fabric-server bleiben ein statisches
  Binary pro Architektur (v8 E.1).
- **Echte HA-Optik, nicht nachgemalt.** Die Theme-Brücke übernimmt HAs
  lebende Theme-Variablen im Ingress; matter-hub bringt ein eigenes
  Material-Theme mit.
- **Trockenlauf vor dem Schreiben.** `Preview` zeigt die go-hamapping-
  Verdikte zu einer Auswahl, bevor sie gilt; loom und matter-hub ändern
  erst und zeigen dann.

### D.2 Besser als bisher

- go-ha2fabric H2 hat eine eigene Seite mit eigenem QR-Encoder und
  eigenem Health-JSON; H3 ersetzt das durch den Import. Was H2 richtig
  gemacht hat (Ingress-Quellprüfung, keine Secrets im Log), wird Regel im
  Vertrag.
- Loom schrieb Typen von Hand neben einer `openapi.yaml`; hier ist der
  Vertrag die Quelle und die Fixtures sind auf beiden Seiten gleich.
- Loom pflegt 268 i18n-Strings in einer 8.700-Zeilen-Datei; die Lib hat
  eigene Kataloge je Sprache, die der Host erweitert.

### D.3 Ist die Aufteilung richtig?

- **Warum nicht in go-fabric?** Die Bibliothek bliebe nicht mehr „sechs
  Abhängigkeiten, kein cgo, ein `go build`": sie bekäme eine
  JS-Toolchain im CI und ein 1-MB-Bundle im Modul, das jeder Konsument
  lädt, der nur den Stack will.
- **Warum nicht nur npm?** Dann baut jeder Daemon selbst (Node im
  Release-Workflow, zweites Image) und schreibt die Go-Handler doppelt.
- **Warum nicht bei loom lassen?** Weil Kopieren driftet; der Owner hat
  die Drift zwischen loom und go-ha2fabric schon gesehen.
- **Was die Aufteilung kostet:** eine Version mehr zu halten (Go-Modul
  und npm im Gleichschritt), ein committetes Build-Artefakt mit Guard,
  und loom muss einmal migrieren. Der Vertrag ist die Stelle, an der
  Fehler teuer sind; deshalb Fixtures auf beiden Seiten und ein
  `apidiff`-artiger Guard auf `openapi.yaml` (kein Entfernen in einem
  Minor).

## Teil E: Risiken

- **npm-Lieferkette.** Festgepinnte Versionen, `npm ci`, Dependabot,
  gitleaks, Lizenz-Check wie in den Schwester-Repos; nur Pakete, die im
  Bundle landen, zählen für die Notices.
- **Dist im Git.** Große Diffs; der Guard macht sie prüfbar
  (byteidentisch oder rot). Sourcemaps werden nicht committed.
- **64-Bit-Ids** im JSON: Hex-Felder Pflicht, Zahlfelder nur informativ.
- **Version-Kopplung** Go ↔ npm: ein Tag, ein Workflow, `version-check`.
- **Ingress-Grenzfälle:** Basispfad (Hash-Routing), Theme-Brücke nur
  same-origin (sonst still inaktiv), Admin-Check beim Host.
- **Controller-Familie zu früh:** U2 wartet auf CJ/S3; der Vertrag legt
  nur die Abschnitte an.

## Teil G: Owner-Schritte

1. Konzept freigeben (dieser Text).
2. npm: Organisation oder Benutzer `sukramj` auf npmjs.org, Publish-Token
   als Secret `NPM_TOKEN` im neuen Repository.
3. Nach U1: go-ha2fabric-Add-on mit der neuen UI im eigenen HA testen
   (Theme, Kopplung mit Apple Home über die Seite).
