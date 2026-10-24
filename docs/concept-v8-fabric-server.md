# Konzept v8: `go-fabric-server` und die Familie — Rollen, Aufteilung, Protokoll

Stand 2026-10-24. Geschrieben nach dem Abschluss der Phasen 21 und 22
(`v0.10.9`) auf den Owner-Entscheidungen desselben Tages. Quellen: die
Lesekarte `notes/briefs/reads/read-v8-matterjs-server.md` (matterjs-server
`a7922e85`, 2026-10-24, Version 1.5.5; go-fabric `a0b842b6`), Konzept v7
(Entscheidungen 0.4, 0.6, 0.9), ADR 0020 (Controller-Rolle), ADR 0025/0026
(Facade und Architekturprinzipien), die Cookbook-Welle CE
(`notes/briefs/reports/CE.md`). Englische Fassungen: `CONCEPT.md` der
jeweiligen Repositories entstehen aus diesem Dokument im ersten Commit der
Welle, die sie betrifft.

## 0. Entscheidungen des Owners (2026-10-24)

- **0.1 Kein Stopp nach M4; zwei Stränge.** Die Implementierung läuft weiter.
  Bis zu zwei Opus-Agenten gleichzeitig auf Themen, die sich nicht
  blockieren: Strang 1 in go-fabric (M5 ff.), Strang 2 in der Familie
  (go-fabric-server, go-hamapping, go-ha2fabric). Mehrere Phasen pro Brief
  und PR; ein Release pro gemergtem Bündel.
- **0.2 go-ha2fabric bettet go-fabric in-process ein** (wie go-zendure2mqtt
  im Cookbook CE), kein dünner Client eines Servers. Ersetzt die
  Vormittagsentscheidung desselben Tages.
- **0.3 go-fabric-server trägt beide Rollen von Anfang an:** (a) als
  **Controller-Server** drahtkompatibel zur WebSocket-API von matterjs-server
  (Schema 14, der python-matter-server-Vertrag), damit Home Assistants
  Matter-Integration ihn als Drop-in verwendet; (b) als
  **Geräteseiten-Server** mit einer Fern-API (Deklaration, Zustandsdeltas,
  Kommandos) für Hosts, die nicht in Go laufen. Kamera/WebRTC ausgeschlossen.
- **0.4 go-hamapping ist ein Go-Modul**, Eingabe das go-hamqtt-Vokabular,
  keine JSON-Form; eigenes Repository wegen zweier Konsumenten (die
  go-*2mqtt-Daemons und go-ha2fabric).
- **0.5 Reihenfolge Strang 2:** go-fabric-server-Kern zuerst (Protokoll,
  Daemon, In-Process-Tests), dann go-hamapping, dann go-ha2fabric.
- **0.6 Name der HA-Bridge: `go-ha2fabric`** (Muster `go-<Quelle>2<Ziel>` der Familie, wie go-zendure2mqtt; am 2026-10-24 als go-habridge angelegt und am Abend umbenannt).
- **0.7 Name des Tabellen-Moduls: `go-hamapping`** (die Abbildung des HA-Vokabulars; als go-hafabric angelegt und am 2026-10-24 umbenannt, weil der Name zu nah an go-ha2fabric lag).
- **0.8 Jedes Konzept trägt den Abschnitt „Besser als andere, besser als
  bisher, ist die Aufteilung richtig"** (Owner-Vorgabe für alle Konzepte;
  hier Teil E).

## Teil A: Ausgangslage

### A.1 matterjs-server (die Referenz für die Controller-Hälfte)

Ein Matter-Controller-Server der Open Home Foundation auf matter.js, Drop-in
für den python-matter-server und das Backend von Home Assistants
Matter-Integration. Fakten aus der Karte (Zeilen in der Karte):

- **Transport.** WebSocket `ws://host:5580/ws`; beim Connect sendet der
  Server unaufgefordert ein `server_info`-Objekt ohne Envelope
  (`fabric_id`, `compressed_fabric_id`, `fabric_index`, `schema_version` 14,
  `min_supported_schema_version` 11, `sdk_version`, Credentials-Flags,
  `bluetooth_enabled`, `ble_proxy_enabled`, `controller_node_id`; Karte
  94-143). Keine Authentifizierung; Schutz nur über `--listen-address`
  (Karte 3858-3871).
- **Envelope.** Anfrage `{message_id, command, args?}`; Antwort
  `{message_id, result}` oder `{message_id, error_code, details, status?,
  cluster_status?, reason?}`; Ereignis `{event, data}` nur an Clients nach
  `start_listening` (Karte 38-91, 286-301). Fehlercodes 0–11 wie
  python-matter-server plus OHF-Erweiterungen 100–103 (Karte 196-260).
  Unbekanntes Kommando → `InvalidCommand` (9).
- **Kommandos.** 58 im Dispatch (Karte 730-791): Credentials, Commissioning,
  `get_nodes`/`get_node`, `read_attribute`/`read`/`write_attribute`,
  `device_command`/`batched_device_commands`, `subscribe`/`unsubscribe`
  (Schema 14), ICD (`get_icd_state`, `register_icd`, `resync_icd`,
  `unregister_icd`), Fabrics, ACL/Bindings, OTA, Thread-Diagnose und
  -Topologie, Kamera/WebRTC, Test-Nodes, Log-Level.
- **Datenmodell.** `MatterNodeData` (`node_id`, `date_commissioned`,
  `last_interview`, `interview_version`, `available`, `is_bridge`,
  `attributes`, `attribute_subscriptions: []`, `matter_version`,
  `endpoint_parents`; Karte 2270-2297). Attribute als flache Map
  `"endpoint/cluster/attribute"` mit **dezimalen IDs** (Karte 2308-2314);
  Werte bekannter Cluster namensbasiert, unbekannter tag-basiert (Karte
  2382-2390); BigInt als nackte Zahl (Karte 2398-2407). Ereignisse
  `node_added/updated/removed`, `attribute_updated` als Tripel
  `[node_id, path, value]`, `endpoint_added/removed`, `node_event`,
  `subscription_report/ended`, `server_info_updated`, `server_shutdown`
  (Karte 303-370).
- **Server.** Eine Klasse je Verbindung mit adaptivem Sendepfad: direkt bis
  zur Hochwassermarke, dann geordnete Queue mit Fenster, `node_updated`
  je Node koalesziert, `node_event`/`subscription_report` bei Überlauf
  älteste zuerst verworfen, strukturelle Ereignisse nie, bei Überlauf
  derer Verbindungsabbau (Karte 410-417, 3459-3478). `start_listening`
  liefert den Dump und schaltet danach den Strom frei (Karte 3826-3835).
  Node-Cache als abgeleitete Form aus matter.js-ClientNodes (Karte 3786).
- **BLE-Proxy.** Zweiter Endpunkt `/ble`, der Proxy-Client hält die
  Hardware, der Server kommandiert; Hello/Version-Handshake, Binärrahmen
  für Daten (Karte 4013-4032).
- **Schwächen, die wir nicht übernehmen.** Keine Authentifizierung;
  `server_info` ohne Envelope (Sonderfall im Client); numerische IDs ohne
  Namen (jeder Client braucht ein SDK-Datenmodell); `subscribe_attribute`
  dokumentiert, aber nicht implementiert; Schema 11 ohne Changelog-Eintrag;
  Kleinschreib-Inkonsistenz im `write_attribute`-Ergebnis (Karte 1498);
  Node-Laufzeit und Speicherbedarf; Dashboard und Protokoll in einem Paket.

### A.2 go-fabric (das Fundament beider Hälften)

- **Geräteseite:** `node.New(cfg)`, `Node.Aggregator().Add/Replace/Remove/
  Batch/MarkComplete`, typisierte Deklarationen `<cluster>def.Declare{Initial:
  Patch{…}}`, `Device.Update(HostStatement)`, `SetReachable`,
  `Node.PairingCodes`, `Health`, `DiagnosticEvents`, `contract.Metrics`
  (Karte 4219-4669). Das Schema liegt eingebettet vor (`package parity`,
  `parity/schema.json`) und ist importierbar — der Server braucht **keinen
  Generatorumbau in go-fabric**, er liest dieselben Bytes.
- **Controller-Seite:** `controller.New(ctx, cfg)`, `Controller.Pair`,
  `Registry()`, `Nodes()`, `client.Node.Read/Write/Invoke/Subscribe`,
  `Registry.Commission/Decommission`, Commissioner, CA, Attestation, DCL,
  OTA-Requestor-Gegenstück (Stufen 1–6b, ADR 0020). Offen in M5: ICD-Client,
  TCP-Initiator, OTA-Push-Rest.
- **Test:** `fabrictest` (Site, Device, Controller, `CommissionedPair`,
  `Discover`, `CommissionNode`, Fake-Uhr; Karte 4729-4808) — ein Server
  lässt sich mit In-Process-Controller und -Gerät vollständig ohne Netz testen.

## Teil B: Architektur der Familie

### B.1 Rollen und Grenzen

| Modul | Rolle | Grenze |
| --- | --- | --- |
| go-fabric | Das Matter-Wissen: Wire, Cluster, Device-Kit, `node`-Facade, Controller-Facade. Bibliothek. | Importiert keinen Host; `contract/` stdlib-only. |
| go-fabric-server | Daemon: WebSocket/JSON-API in beiden Rollen, Node-Cache, Store, Betrieb (Image, systemd, Release). | Importiert `node`, `device`, `controller`, `client`, `contract`, `parity`, `schema`, die `<cluster>def`-Pakete, `fabrictest` in Tests — **nichts darunter** (Guard wie `script/archcheck`). Kein Cluster-Wissen eigener Hand. |
| go-hamapping | Tabelle HA-Komponente (go-hamqtt-Vokabular) → Gerätetyp + Deklaration; Kommando → Service/MQTT. Go-Modul. | Importiert `device`, `contract`, `schema`, `eligibility`, `<cluster>def`; kein `node`, kein Netz. |
| go-ha2fabric | HA-Add-on: HA-WebSocket rein, go-hamapping, go-fabric in-process raus. | Importiert go-fabric (Facade) und go-hamapping; kein Server. |
| go-*2mqtt | Bestehende Daemons, go-fabric in-process (CE), später über go-hamapping. | Wie go-ha2fabric. |

### B.2 Innerer Aufbau von go-fabric-server

```
cmd/fabric-server            Flags/Env, Composition Root (ein Prozess, beide Rollen optional)
protocol/                    Envelope, Fehlercodes, Schema-Nummern, JSON-Codec (uint64 nativ), Backpressure-Sender
protocol/schema/             generiert aus go-fabric parity/schema.json: Cluster-/Attribut-/Kommando-Namen ↔ IDs,
                             JSON-Schema der Zustände und Kommandos (Artefakt für Clients)
controllerapi/               die matterjs-server-kompatible Hälfte: Dispatch-Tabelle, Node-Cache, Ereignisse
bridgeapi/                   die Geräteseiten-Hälfte: Deklaration, Deltas, Kommandos, Ereignisse
internal/wire/               Matter-Werte ↔ JSON (namensbasiert bekannt, tag-basiert unbekannt), Pfade
client/                      Go-Client beider Hälften (für Tests, go-ha2fabric nutzt ihn nicht)
deploy/                      Dockerfile (distroless, non-root), systemd, release-Matrix (aus v7 B.2, hierher verlegt)
```

Ein Prozess kann eine, die andere oder beide Rollen fahren (Flags
`--controller`, `--bridge`); beide Rollen sind in go-fabric getrennte
Kompositionswurzeln (`controller.New`, `node.New`) und teilen nur Store-
Datei und Logger — `fabrictest` tut genau das heute schon in einem Prozess.

### B.3 Was go-ha2fabric und go-hamapping aus v6/v7 behalten

Teil B/C von Konzept v6 bleiben inhaltlich (HA-Registry als Quelle,
`state_changed` → Attribute, `call_service` ← Kommando; die Tabelle als
Daten, aus denen die Go-Form generiert wird, mit Guard auf Gleichheit;
Eligibility entscheidet den Gerätetyp). Geändert gegenüber v7-Vormittag:
go-ha2fabric bettet ein (0.2); go-hamapping hat keine JSON-Form (0.4).

## Teil C: Das Protokoll

### C.1 Gemeinsames

- **Ein Envelope für beide Hälften:** Anfrage `{message_id, command, args}`,
  Antwort `{message_id, result}` | `{message_id, error_code, details,
  status?, cluster_status?, reason?}`, Ereignis `{event, data}` — exakt
  matterjs-server (Kompatibilität), auf `/bridge` dieselbe Form.
- **Zahlen:** 64-Bit-IDs als JSON-Zahl (Kompatibilität); Go kodiert `uint64`
  nativ, die Dekodierung nutzt `json.Number`. Das ist ein Punkt, an dem Go
  schlicht besser ist als JavaScript.
- **Schema-Nummer:** auf `/ws` melden wir `schema_version` 14 und
  `min_supported_schema_version` 11 (der HA-Vertrag); Kommandos, die wir
  noch nicht implementieren, antworten `InvalidCommand` (9) mit `details`,
  der den Stand nennt. Eine `capabilities`-Liste im `server_info` (OHF
  kennt das Feld nicht; Clients ignorieren Unbekanntes) nennt die
  implementierten Kommandos. Auf `/bridge` eigene Nummer ab 1.
- **Authentifizierung (unsere Ergänzung):** optionales Bearer-Token
  (`--token`, Header `Authorization` oder Query `?token=`), Unix-Socket als
  Default, wenn `--listen` fehlt; ohne Token nur loopback. Kompatible
  Clients ohne Token laufen über loopback/Unix-Socket weiter.
- **Backpressure:** das Modell von matterjs-server wird übernommen, weil es
  richtig ist (direkt → Queue mit Fenster; `node_updated` koalesziert;
  Reports droppable; strukturelle Ereignisse nie; Überlauf → Close), mit
  Metriken (`contract.Metrics`: `ws.queued`, `ws.dropped`, `ws.closed`).
- **Beobachtbarkeit:** slog-Schlüssel der go-fabric-Konvention, `expvar`
  über `node/metrics/expvar`, Limits aus `node.Config.Limits`, Health-Route
  `GET /health` aus `node.Health`, `GET /metrics` (expvar), `GET /schema`
  (das generierte JSON-Schema mit Nummer).

### C.2 Controller-Hälfte (`/ws`, kompatibel)

- **Kern (Bündel S1):** `server_info`, `start_listening`, `get_nodes`,
  `get_node`, `commission_with_code`, `commission_on_network`,
  `parse_onboarding_payload`, `discover_commissionable_nodes`/`discover`,
  `read_attribute`, `read`, `write_attribute`, `device_command`,
  `batched_device_commands`, `subscribe`, `unsubscribe`,
  `enable/disable_auto_subscription`, `remove_node`, `interview_node`,
  `ping_node`, `get_node_ip_addresses`, `open_commissioning_window`,
  `set_/remove_wifi_credentials`, `set_/remove_thread_dataset`,
  `get_all_credentials`, `set_default_fabric_label`, `get_fabric_label`,
  `get_matter_fabrics`, `remove_matter_fabric`, `diagnostics`,
  `get_/set_loglevel`. Ereignisse: alle Node-, Attribut-, Endpoint-,
  Subscription- und Server-Ereignisse.
- **Mit M5 (Bündel S2):** `set_acl_entry`, `set_node_binding`,
  `check_node_update`, `update_node`, `initiate_ota_upload` +
  `POST /ota-upload`, `get_vendor_names` (DCL vorhanden), die vier
  ICD-Kommandos (brauchen Phase 24), `import_test_node`.
- **Mit M6/ADR 0027 (S3):** `get_thread_border_routers`,
  `get_thread_diagnostics`, `get_network_topology`.
- **Nie:** Kamera/WebRTC (`camera_*`, `send_webrtc_provider_command`),
  BLE-Proxy als Endpunkt `/ble` — der Commissioner spricht BLE lokal
  (ADR 0024); ein Proxy wird erst Thema, wenn ein Container ohne Adapter
  ihn braucht (dann in matterjs-server-Form, Karte 4013-4032).
- **Werte:** `attributes` nach `"endpoint/cluster/attribute"` dezimal,
  Werte bekannter Cluster namensbasiert (camelCase-Feldnamen aus
  `parity/schema.json`), unbekannte tag-basiert, BigInt nackt — Karte
  2308-2407 ist die Spezifikation. **Zusatz** (ignorierbar für alte
  Clients): `attribute_names` im Node-Datensatz, eine Map Pfad → `"OnOff.OnOff"`,
  und `get_schema` — der Schritt zu Namen, ohne den Vertrag zu brechen.

### C.3 Geräteseiten-Hälfte (`/bridge`, neu)

- **Kommandos des Hosts:** `declare` (das Topologie-Dokument: Geräte mit
  stabilem Schlüssel, Gerätetyp-Name, Cluster-Deklarationen mit
  Anfangszuständen — Namen, nicht IDs; ein Dokument = ein
  `Aggregator.Batch`), `set` (Deltas `{device, cluster, patch}` →
  `Server.Set`), `host` (`HostStatement`: Name, Adressen, erreichbar),
  `result` (Antwort auf ein `command`-Ereignis: `{correlation, status |
  payload}`), `pairing_codes`, `node_info`, `start_listening`.
- **Ereignisse an den Host:** `command` (`{correlation, device, cluster,
  command, fields, deadline}` — der Host antwortet mit `result` vor der
  Frist, sonst `Timeout` an den Controller), `write` (ein Controller
  schreibt ein hostgehaltenes Attribut), `fabric_added/removed`,
  `commissioning_window`, `subscription_count`, `server_shutdown`.
- **Fristen und Korrelation sind Pflicht** (matter.js' Invoke-Timeouts);
  ein Host ohne Antwort macht den Endpunkt nicht kaputt, nur das Kommando.
- **Validierung an der Tür:** das generierte JSON-Schema prüft jedes
  `declare`/`set`, der Fehler nennt Pfad und Regel (CONSTRAINT/CONFORMANCE
  wie CT sie in `cluster/spec` eingeführt hat).

## Teil D: Wellen Strang 2 (ein Opus-Agent, Bündel)

| Bündel | Inhalt | Beweis |
| --- | --- | --- |
| S1 | Repo nach Standard (v7 B.4, inkl. Auto-Merge), `CONCEPT.md` (englisch, aus diesem Dokument), `protocol/`, `protocol/schema/`-Generator aus `go-fabric/parity`, `controllerapi` Kern (C.2 Kern), Node-Cache, Ereignisse, Backpressure, Token/Unix-Socket, `GET /health|/metrics|/schema`, Go-Client, `deploy/` (Dockerfile, systemd, release-Matrix), In-Process-Tests mit `fabrictest` (Server mit Controller-Rolle kommissioniert ein Site-Gerät; jeder Kern-Befehl eine Testzeile), Kompatibilitätstest gegen den `python_client` von matterjs-server als Fixture (Nachrichtenformen) | HA-Matter-Integration gegen den Server (owner-manuell), `v0.1.0` |
| S2 | `bridgeapi` (C.3) mit Go-Client, Python-Beispielclient (50 Zeilen), S2-Kommandos der Controller-Hälfte soweit M5 geliefert hat | ein Python-Skript macht ein Gerät sichtbar in Apple Home (owner-manuell); `v0.2.0` |
| S3 | Thread-Kommandos nach ADR 0027, `import_test_node`, Dashboard-Frage (separates Repo oder keins) | `v0.3.0` |
| H1 | go-hamapping: Zeilenformat, Generator Go-Form, Guard, erste Zeilen (light, switch, sensor, binary_sensor, cover, climate, lock, fan) mit `fabrictest`-Rundlauf | jede Zeile ein Rundlauf |
| H2 | go-ha2fabric: HA-WS-Client, Registry-Adapter aufs go-hamqtt-Vokabular, Einbettung (Cookbook), Add-on | HA-Entität in Apple Home |

Strang 1 liefert parallel M5 (23+24, dann 25) und damit S2s ICD-/OTA-
Kommandos.

## Teil E: Besser als andere, besser als bisher, ist die Aufteilung richtig

### E.1 Besser als matterjs-server (die Referenz)

| Punkt | matterjs-server | go-fabric-server | Begründung |
| --- | --- | --- | --- |
| Laufzeit | Node ≥ 22, npm-Abhängigkeitsbaum | statisches Go-Binary, distroless, `CGO_ENABLED=0` | Speicher und Start auf Pi-Klasse; ein Artefakt je Architektur; kein Interpreter im Add-on. |
| 64-Bit-Zahlen | eigener BigInt-JSON-Serializer, Sonderregeln für Clients | `uint64` nativ, `json.Number` | eine Fehlerklasse weniger. |
| Namen | nur numerische IDs, Client braucht SDK-Modell | IDs für Kompatibilität **plus** `attribute_names`, `get_schema`, Namen auf `/bridge` | ein Client ohne Matter-SDK bleibt möglich. |
| Schema | Zahl + Changelog, Lücke bei 11 | Zahl + maschinenlesbares JSON-Schema als Artefakt (`GET /schema`), `capabilities` im `server_info` | ein Client fragt statt zu raten. |
| Sicherheit | keine Authentifizierung, Bind-Adresse | Token, Unix-Socket-Default, loopback ohne Token | der Hinweis „du bist selbst verantwortlich" wird eine Vorgabe. |
| Korrektheit | gute Tests, aber kein Paritätsguard gegen eine Referenz | go-fabric's Guards (Parität, Fuzz, Race, Reachability, Limits, In-Flight-Pacing aus CG) gelten unter dem Server; der Server selbst testet in-process ohne Netz | derselbe Standard wie die Bibliothek. |
| Betrieb | Dashboard im Paket, Log-Level per Dashboard | Protokoll frei von UI; `expvar`, Health, slog-Tabelle, Limits-Seite | ein Dashboard ist ein eigener Client, nicht Teil des Vertrags. |
| Backpressure | gut (koaleszieren, nie-droppen, schließen) | übernommen und mit Metriken sichtbar | nichts erfinden, was richtig ist. |
| Rolle | nur Controller | Controller **und** Geräteseite in einem Daemon | ein Container, der beides kann, für Hosts ohne Go. |

Was matterjs-server besser kann und wir vorerst nicht: Kamera/WebRTC,
Thread-Topologie-Graphen, Migration alter python-matter-server-Daten, Jahre
an Interop-Erfahrung mit HA. Kompatibilität zum Vertrag holt letzteres
weitgehend ein; die ersten beiden sind bewusst ausgeschlossen bzw. M6.

### E.2 Besser als bisher (unsere eigene Linie)

- Konzept v6/v7 hatten go-ha2fabric als Einbettung, der Vormittag des
  2026-10-24 als dünnen Client, der Abend wieder als Einbettung — mit
  Grund: CE hat gezeigt, dass eine Einbettung acht Commits kostet, und ein
  Protokoll zwischen zwei Go-Prozessen Versionierung, doppelte
  Serialisierung und zwei Container ohne Gegenwert bringt. Die
  Fern-Schnittstelle ist für Nicht-Go und für HA als Controller-Client, und
  dort ist sie die beste Form.
- Die Controller-Facade (`controller.New`, ADR 0020) bekommt ihren ersten
  externen Konsumenten — wie loom die Geräte-Facade geprüft hat, prüft
  der Server die Controller-Facade; Lücken werden Facade-Fixes in
  go-fabric (wie die zwei PowerSource-Fixes aus CE), nicht Workarounds im
  Server.
- Verpackung wandert aus der Bibliothek in das Produkt (v7 0.9) und
  bekommt dort einen echten Grund (ein Daemon, den jemand betreibt).

### E.3 Ist die Aufteilung richtig?

Geprüfte Alternativen und warum nicht:

- **Alles in go-fabric (`cmd/`).** Verworfen: eine Bibliothek mit
  Stabilitätsfenster und sechs Abhängigkeiten gegen einen Daemon mit
  Protokoll-Schema, WS-Bibliothek, Image-Release; zwei Takte in einem
  CHANGELOG.
- **go-fabric-server als einzige Tür (auch für Go-Hosts).** Verworfen
  (0.2): Go-Programme bezahlen ein Protokoll für etwas, das ein Import ist.
- **go-hamapping in go-ha2fabric.** Verworfen (0.4): die go-*2mqtt-Daemons
  müssten ein Produkt importieren; zwei Konsumenten mit eigenem Takt
  rechtfertigen das Modul.
- **Nur Geräteseite im Server zuerst.** Verworfen (0.3): nach 0.2 fehlt
  der Konsument; die Controller-Hälfte hat ihn (HA) am ersten Tag.
- **Zwei Daemons (einer je Rolle).** Verworfen: beide Rollen sind in
  go-fabric getrennte Kompositionswurzeln und laufen heute in
  `fabrictest` in einem Prozess; ein Daemon mit Flags ist die kleinere
  Betriebsfläche; ein zweites Image kann jederzeit aus demselben Binary
  entstehen.

Schichtung innerhalb des Servers: `protocol` kennt kein Matter,
`controllerapi`/`bridgeapi` kennen go-fabric's Facaden und das Protokoll,
`internal/wire` kennt das Schema; nichts reicht unter die Facaden. Ein
Guard hält das. Die Familie hat damit genau eine Stelle für Matter-Wissen
(go-fabric), eine für HA-Abbildung (go-hamapping), eine Tür nach außen
(go-fabric-server) und ein Produkt (go-ha2fabric) — jede Grenze folgt einem
Konsumentenwechsel, keine ist Konvention.

## Teil F: Risiken

- **HA-Kompatibilität ist empirisch.** Der python_client als Fixture
  deckt Formen, nicht HA's Nutzung; die manuelle Probe gegen HA ist
  Owner-Schritt je Bündel. Mitigation: `capabilities` und ehrliches
  `InvalidCommand`.
- **Controller-Facade-Lücken** werden Facade-Fixes in go-fabric; der
  Server-Agent darf sie nicht umgehen (Brief-Regel), was den Server-Takt
  an go-fabric koppelt. Mitigation: Facade-Fixes als eigene Commits mit
  Zitat, Release gleich.
- **Schema-Generator im Server** liest `parity/schema.json` über das
  importierte Modul: ein Pin-Wechsel in go-fabric ändert das Artefakt;
  das Schema trägt daher die go-fabric-Version.
- **Zwei Agenten, ein Owner-Review.** Die Reviews bleiben Fable, die
  PR-Größe wächst; Mitigation: Report-Form je Bündel, Guards in den
  Repos, die Konzept-Abschnitte als Checkliste.
