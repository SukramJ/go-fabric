# Konzept v7 — Phasen 21 und 22: Betrieb und Verpackung (M4-Abschluss)

Stand 2026-10-23, auf `main` `v0.10.2`. Grundlage: die Lesekarte
`notes/briefs/reads/read-m4-phase-21-22.md` (Sonnet, Karte; die
tragenden Zeilen vom Owner-Modell nachgelesen), die Roadmap-Zeilen 21
und 22, ADR 0025/0026, das Gate-2-Urteil (`notes/briefs/reports/G2.md`).
Sprache dieses Dokuments: Deutsch (Phasenkonzept); alles, was daraus in
Code, Docs, ADRs oder Issues geht, ist Englisch.

## 0. Entscheidungen des Owners (2026-10-23)

Werden per Wizard eingeholt und hier nachgetragen:

- **0.1 Persistenz-Port:** Form des `Store`-Ports (A.2).
- **0.2 Metrics-Hook:** eigener Sink-Port ohne Abhängigkeit (A.3) oder
  nur `expvar`-Zähler.
- **0.3 Cookbook-Daemon:** welches `go-*2mqtt` der Beweis für Phase 22
  ist (B.3).
- **0.4 Teil B/C:** die Konzepte go-ha-matter-bridge und `go-hamatter`
  bekommen eigene Repositories (öffentlich, leer bis auf das Konzept)
  oder bleiben bis M5 in diesem Repo (B.4).
- **0.0 Ein Agent:** die Implementierung läuft mit genau einem
  Opus-Agenten zur Zeit (Owner, 2026-10-23); Wellen sind sequenziell.
- **0.5 Versionen:** Phase 21 als `v0.10.x`-Patches je Welle, Phase 22
  schließt mit `v0.11.0`; oder Phase 21 = `v0.11.0`, Phase 22 =
  `v0.12.0`.

## Teil A: Phase 21 — Observability und Persistenz als Ports

### A.1 Ausgangslage (Karte §1–§3)

- Logging: 87 `*slog.Logger`-Felder, jedes Ereignis dotted lower case
  (BX3/BY-2-Guards). **Uneinheitlich sind die Attributschlüssel:** Fabric
  als `fabric_index` (24) und `fabric` (6); Fehler als
  `slog.String("err", err.Error())` (233) und `slog.Any("err", err)` (30);
  Adresse als `src` (50), `address` (23), `addr` (3). Der Nil-Logger
  fällt in `bridge`/`node`/`transport` auf `slog.Default()`, in
  `client/*` auf einen Discard-Handler.
- Diagnose-Ring: `diagevent.Ring` (256 Einträge, nur Speicher), schreibt
  die Bridge (Pairing, Session, Discovery), liest `Bridge.DiagnosticEvents`
  und DiagnosticLogs; **keine Route in `node`** (loom liest ihn über
  den Seam, `advanced-seams.md:203-207`).
- Metriken: keine. Vorhanden sind `node.Health` (Probe), die
  DeviceLoad-Zähler (IM empfangen/gesendet, Subscriptions), die
  Recovered-Panic-Zähler der Listener, `paseFailures`, `reassemblies`.
- Persistenz: `store.Store` ist **ein Struct über `*sql.DB`**
  (`store/store.go:14`), kein Port, obwohl ADR 0026 P7 „Seam“ sagt;
  `node.Config.Store.Credentials *store.Store`. Interfaces davor gibt es
  nur für Teile (`groups.Store`, `ThermostatPresetStore`, `DoorLockAuth`).
  In-Memory-Implementierungen existieren für alle Controller-Stores
  (`client.MemoryStore`, `ca.MemoryStore`, `dcl.MemoryStore`,
  `peer.MemoryResumptionStore`) und für den Endpoint-Store
  (`endpointtest.FakeStore`) — **nicht für die Credentials**;
  `fabrictest` legt je Gerät eine SQLite-Datei an.
- Härtung: Session-/Subscription-Caps existieren mit Konstanten; das
  **PASE-Versuchslimit existiert bereits** (`paseMaxErrors = 20`,
  `bridge/securechannel.go:497-502`, Lockout 15 min → 4 h,
  `docs/security.md:24`) und spiegelt matter.js
  `PaseServer.ts:34` (`PASE_COMMISSIONING_MAX_ERRORS = 20`, Abbruch des
  Fensters `:106-110`; am Pin gelesen). Die Roadmap-Formulierung „after
  §5.5“ meint den Spec-Abschnitt der PASE-Flows; das Item ist erledigt
  und wird nur dokumentiert. Fuzz: 24 Ziele in 10 Paketen; keines in
  `client/interaction`, `client/discovery`, `controller/dcl`.

### A.2 Der Persistenz-Port (Roadmap 21: „Store seam proved by a second implementation“)

**matter.js** hält `StorageService`/`StorageContext` als Port mit
mehreren Backends (`packages/general/src/storage`); der Node bekommt
einen Storage-Service injiziert. **Go-Form** (0.1), drei Varianten:

- **(a) Ein Port je Fachgebiet.** `store` deklariert Interfaces, die
  die Konsumenten heute brauchen — `Credentials` (Fabrics, Identity,
  Resumption, ACL), `Groups` (existiert als `groups.Store`), `Settings`,
  `ServerSubscriptions`, `OTAState`, `ThermostatPresets`,
  `DoorLockAuth`; `*store.Store` implementiert alle; neu `store/memstore`
  implementiert alle im Speicher. `node.Config.Store` nimmt den
  Gesamtport (`store.Port`, die Summe) statt `*store.Store`; die
  SQLite-Form bleibt der Default über `Store.Path`/`Store.DB`.
  Empfohlen: klein genug zum Prüfen, groß genug für eine echte zweite
  Implementierung; `fabrictest` läuft auf `memstore` (schneller, keine
  Dateien).
- **(b) Ein Interface über alle ~55 Methoden.** Mechanisch, aber kein
  Entwurf; ein Host, der nur die Credentials anders ablegen will, muss
  alles implementieren.
- **(c) Nur SQLite `:memory:`.** Kein Port; der Anspruch „zweite
  Implementierung“ wäre leer.

Dazu: `Store.Path`/`DB` bleiben; die Migrationen (v1–v6) bleiben
SQLite-Sache; `docs/guides/persistence.md` beschreibt den Port und was
ein Host mit eigenem Backend garantieren muss (Atomarität je Methode,
Fabric-Scope beim Entfernen). Eine Variante von Store, die ein Host
schreibt, wird durch die **Vertragstests** geprüft: `store/storetest`
fährt dieselbe Tabelle gegen SQLite und `memstore` (das Muster von
`endpointtest.FakeStore`).

### A.3 Metrics-Hook und Diagnose-Ring auf der Facade

- **Metrics (0.2).** Kein OTel/Prometheus (sechste Abhängigkeit, ADR
  0025 P12). Port `node.Metrics` (Interface, stdlib-only, im Paket
  `node` oder `contract`): `Counter(name string, delta int64, attrs ...Attr)`,
  `Gauge(name, value, attrs...)`, mit einem benannten, dokumentierten
  Satz: `sessions.open`, `subscriptions.active`, `im.received`,
  `im.sent`, `pase.failures`, `pase.lockouts`, `aggregator.reassemblies`,
  `mrp.retransmits`, `panics.recovered`. Default nil = nichts; eine
  `node/metrics/expvar`-Implementierung liefert die Namen auf
  `expvar` (stdlib), damit ein Daemon ohne Zusatzcode `/debug/vars`
  hat; die Zähler, die heute verstreut liegen (DeviceLoad,
  RecoveredPanics, paseFailures, reassemblies), speisen den Hook
  **und** behalten ihre Leser. Namen sind spec-nah (ADR 0025 P5), nicht
  matter.js-intern.
- **Diagnose-Ring.** `node.Config.Diagnostics{Capacity int}` (0 = 256),
  `Node.DiagnosticEvents() []DiagnosticEvent` (der Alias existiert),
  `Node.OnDiagnosticEvent(fn)` inline + `…Async` nach der Callback-Regel.
  Der Ring bleibt Speicher; Persistenz ist keine Anforderung.
- **slog-Konvention.** Ein Schlüsselsatz, in `docs/guides/observability.md`
  (neue Seite) als Tabelle: `fabric_index`, `node_id`, `endpoint`,
  `cluster`, `attribute`, `session_id`, `exchange_id`, `peer` (Adresse),
  `err` (immer `slog.Any`), `device` (Host-Schlüssel), `reason`; die
  Ereignisnamen-Präfixe je Paket. Guard: eine Analyzer-Regel neben
  `script/errprefix` (`script/slogkeys`), die fremde Schlüssel und
  `slog.String("err", …)` meldet; der Nil-Logger ist überall
  `slog.Default()` (ADR 0026 P9 sagt das; `client/*` zieht nach).

### A.4 Härtung

- Limits dokumentiert auf einer Seite (`docs/guides/limits.md` oder in
  `operations.md`): Sessions je Fabric und gesamt, Subscriptions, Paths
  je Subscription, PASE-Versuche und Lockout, Fabrics, Gruppen,
  MRP-Backoff — je Wert Konstante, Datei, Spec-Bezug, ob ein Host ihn
  setzen kann (heute meist nicht; wo matter.js einen Wert konfigurierbar
  hält, bekommt `node.Config.Limits{…}` ihn, sonst bleibt er Konstante).
- PASE-Limit: nichts zu tun außer der Tabelle (siehe A.1).
- Fuzz: Ziele für `client/interaction` (Report-/Subscribe-Decoder),
  `client/discovery` (TXT/DNS-SD-Parsing), `controller/dcl` (die
  DCL-Antworten), `bdx/otaimage` (Header) — der Controller liest fremde
  Bytes; `make fuzz` bleibt der Guard. `client/handshake` (Sigma) ist
  Owner-Pfad: ein Fuzz-Ziel dort schreibt der Owner.

### A.5 Wellen Phase 21

| Welle | Modell | Inhalt | Version |
| --- | --- | --- | --- |
| CA | Opus | A.2: Store-Ports, `store/memstore`, `store/storetest`, `node.Config.Store` auf den Port, `fabrictest` auf memstore, `persistence.md` | Patch |
| CB | Opus | A.3: `node.Metrics` + expvar-Implementierung, Ring auf der Facade, slog-Konvention + Guard, `observability.md` | Patch |
| CC | Opus (Fuzz der Client-Decoder), Owner (Sigma) | A.4: Limits-Seite und `Config.Limits`, Fuzz-Ziele | Patch |

CA, CB, CC laufen **nacheinander mit je einem Opus-Agenten** (Owner,
2026-10-23: ein Implementierungs-Agent zur Zeit); die erste startet,
wenn BV-b gemergt ist. Phase 21 schließt mit
dem ◐ „Pluggable storage“ → ✅ in der Vergleichstabelle.

## Teil B: Phase 22 — Verpackung und Cookbook

### B.1 Ausgangslage (Karte §4–§5)

Keine Dockerfile, kein systemd-Unit, kein goreleaser; `release.yml`
veröffentlicht nur Notes, keine Binaries, kein Image; arm64 nur als
BLE-Runner, 32-Bit als Build-Job. Die fünf `go-*2mqtt`-Daemons teilen
das Muster „Dockerfile (golang-alpine Builder, distroless/static,
non-root) + HA-Add-on-Verzeichnis + `addon-image.yml`“ und die
Vokabel-Schicht `go-hamqtt` (`model.Entity`, `model.Device`).

### B.2 Verpackung der Referenz-Apps

- `examples/reference-bridge` und `examples/reference-controller`:
  Dockerfile nach dem Schwester-Muster (distroless, non-root, Healthcheck
  über `node.Health`), `deploy/systemd/*.service` (hardening: `DynamicUser`,
  `ProtectSystem=strict`, `StateDirectory`, `AmbientCapabilities=` keine
  — Port 5540 ist unprivilegiert), eine `docs/guides/operations.md`
  (Betrieb: Dateien, Ports, mDNS/avahi, IPv6, Logs, Health, Limits,
  Backup des Stores).
- Release: `release.yml` baut die zwei Binaries für `linux/{amd64,arm64,arm/v7}`
  (`CGO_ENABLED=0`), hängt sie ans GitHub-Release und schiebt ein
  Multi-Arch-Image nach `ghcr.io/sukramj/go-fabric-reference-bridge`.
  Kein goreleaser (sechste Abhängigkeit vermieden; ein Matrix-Job genügt).
- CI: ein `build (arm64)`-Job neben dem 32-Bit-Job; die chip-tool-Suite
  bleibt amd64.

### B.3 Das Cookbook: „Embed in a daemon“ (0.3)

Der Beweis von Phase 22 („a 2mqtt daemon exposes one device type end to
end on the kit“). Kandidaten nach der Karte:

- **go-zendure2mqtt** (5.8 k Zeilen, `source.Device`/`model.Report`,
  ein Backend-Interface): kleinster Umbau; Gerätetyp: ein
  Energiespeicher als `PowerSource`/`ElectricalPowerMeasurement`-Endpunkt
  (ein Sensor-Gerätetyp). Empfohlen: der Guide bleibt lesbar, der
  Beweis ist echt (Apple Home zeigt Ladezustand und Leistung).
- **go-daikin2mqtt** (Management Point → Endpunkt, Characteristic →
  Attribut): Thermostat-Gerätetyp — mehr Wert für Nutzer, mehr Umbau
  (Setpoints, Modi, Fan).
- **go-homeconnect2mqtt**: Geräte-Profile mit Zuständen → Appliance-
  Cluster; am größten.

Form des Guides (`docs/guides/embed-in-a-daemon.md`): das Rezept in
acht Schritten (Modul einbinden, `node.Config` aus der Daemon-Config,
Store-Pfad, ein Gerät aus der Daemon-Domäne als Deklaration,
Aggregator-Batch beim Gerätelauf, Lifecycle an die Verfügbarkeit,
Pairing-Code in die UI/MQTT, systemd/Docker) — geschrieben gegen den
echten PR im gewählten Daemon, jeder Schritt mit dessen Commit.

### B.4 Teil B/C in ihre Repositories (0.4)

Konzept v6 Teil B (go-ha-matter-bridge) und Teil C (`go-hamatter`,
die HA-Komponente→Cluster-Tabelle) wandern als `CONCEPT.md` in neue
Repositories `SukramJ/go-ha-matter-bridge` und `SukramJ/go-hamatter`
(Apache-2.0, README mit Status „concept, not started“, Verweis auf
go-fabric und die Roadmap M5+). In go-fabric bleibt in v6 je ein Satz
mit dem Link; `docs/roadmap.md` Konsumenten-Tabelle zeigt auf die
Repos. Alternative: beides bleibt bis M5 in `docs/history/`.

### B.5 Wellen Phase 22

| Welle | Modell | Inhalt | Version |
| --- | --- | --- | --- |
| CD | Opus | B.2: Dockerfiles, systemd, `operations.md`, release.yml-Matrix und Image, arm64-Job | Patch |
| CE | Opus (im Daemon-Repo + hier) | B.3: der Umbau im gewählten 2mqtt-Daemon (eigener PR dort, gegen den Tag), `embed-in-a-daemon.md` hier | — |
| CF | Owner | B.4: die zwei Repositories mit Konzept, Verweise, Comparison-Reste (`:139` cli-tool bleibt ○ mit Begründung, `:140` ○ „host concern“), Roadmap, `v0.11.0`, M4 geschlossen | `v0.11.0` |

## Teil C: Was M4 danach erfüllt — und was nicht

- „Done when“: ◐ in Gerätemodell/Runtime nur noch keines (Pluggable
  storage ✅ nach CA; `:129` generierte Server ✅ nach dem Nachtrag — die
  Zeile ist seit Phase 20 inhaltlich erfüllt und wird korrigiert); loom
  baut nach Schritt 2 (openccu-loom#999/#1000) allein auf der Facade;
  ein 2mqtt-Daemon nach CE.
- Nicht in M4: Persistenz des Diagnose-Rings, OTel-Exporter, ein
  Konfig-Dateiformat für den Node (Host-Sache), die HA-Bridge selbst
  (M5+ in ihrem Repo).

## Teil D: Risiken

- A.2 bricht `node.Config.Store.Credentials *store.Store` → Port: unter
  dem ausgesetzten Fenster mit Guide-Zeile; loom nutzt `Store.Path`
  (keine Auswirkung) — prüfen in #1000.
- CE hängt an der Verfügbarkeit des gewählten Daemons und seines
  Maintainers (der Owner); die Welle ist im Daemon-Repo eine eigene PR.
- Das Multi-Arch-Image braucht `docker buildx` in CI und ein
  Paket-Schreibrecht auf ghcr (Repo-Einstellung des Owners).
