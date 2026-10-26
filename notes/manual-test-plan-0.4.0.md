# Manueller Testplan für `v0.4.0`

Stand 2026-10-14, geschrieben für den Owner am Gerät. `v0.4.0` ist das
Release mit vollem Commissioner, OTA-Provider, Media-Clustern, DCL-Client
und dem Referenz-Controller (Wellen 13–26). Es friert noch keine API ein;
die Befunde dieses Plans entscheiden, was vor `v1.0.0` noch geändert
wird (Konzept v2 Phase 5, ADR 0020, ADR 0021, ADR 0022).

Was automatisch geprüft ist und hier **nicht** wiederholt wird: die
chip-tool-Suite und die Zertifizierungsfamilien (`make chiptool-test`,
`make chiptool-families`, CI), die Live-Tests in `node/` und
`controller/` (eigener Commissioner gegen eigene Nodes, Gruppen,
Bindings, OTA, DiagnosticLogs, CommissionerControl), die Parity-Guards.
Was nur manuell geht: echte Ökosysteme, echte Geräte, echtes Netz, Zeit.

Jeder Befund wird als Issue mit dem Abschnittskürzel (z. B. `L3`,
`C2`) und dem Log angelegt; `--log-level debug` beim Daemon und beim
Controller. Nichts hier ist eine Zertifizierungsaussage (ADR 0011).

## Teil L — mit OpenCCU-Loom (Geräteseite)

Voraussetzung: loom auf `go-fabric v0.4.0` gehoben, Build grün, die
neuen Ports (`node.Config.OTAProvider`, `Network.WiFi`, `Client`,
`CommissionerControl`) bleiben nil, wenn loom sie nicht braucht.

| # | Schritt | Erwartung |
| --- | --- | --- |
| L1 | Datenbank von 0.3.1 übernehmen, loom starten, 2 Minuten warten | Startup ohne Fehler; Fabrics und Subscriptions der Controller bestehen weiter (kein Neukommissionieren, Apple Home zeigt die Geräte ohne „Keine Antwort“). |
| L2 | **Apple Home**: neues Zuhause, Kommissionierung per QR-Code | Pairing in unter 60 s; alle loom-Gerätetypen erscheinen mit Namen und Raum; keine „Zubehör nicht unterstützt“-Meldung für Typen aus `device/`. |
| L3 | Schalten, Dimmen, Farbe, Rollo auf/zu/Position, Thermostat-Sollwert, Fenster-/Bewegungssensor auslösen | Jede Aktion unter 1 s sichtbar; ein Zustandswechsel am CCU-Gerät erscheint in Home ohne Pull (Subscription). |
| L4 | Gerät in loom entfernen und neu anlegen (Reassemble) | Home zeigt das Gerät nach dem Entfernen als nicht erreichbar und nach dem Neuanlegen wieder unter demselben Endpoint (Endpoint-Stabilität, `examples/reference-bridge/README.md` §Endpoint numbers). |
| L5 | loom neu starten, Router neu starten | Home verbindet sich ohne Neukommissionierung; Subscriptions nach dem Neustart wiederhergestellt (`ReestablishFormerSubscriptions`), Latenz nach dem Router-Neustart unter 30 s. |
| L6 | Zweiten Admin freigeben (Home → Gerät → Mit anderem Ökosystem koppeln) und mit **Alexa** oder **Google Home** koppeln | Zweite Fabric; beide Apps zeigen denselben Zustand nach einem Schaltvorgang aus der jeweils anderen App. |
| L7 | Fabric aus einer App entfernen | Die andere Fabric bleibt; `nodes`/Subscriptions der entfernten Fabric sind weg (`GeneralDiagnostics`/`OperationalCredentials` über den Referenz-Controller prüfen, Teil C). |
| L8 | 48 h Dauerlauf mit beiden Apps | Kein Speicherwachstum (RSS nach 48 h unter dem Doppelten des Startwerts), Session-Zahl stabil (`C6`), keine Reconnect-Schleifen im Log. |
| L9 | Media (nur wenn loom einen Player bridgt; sonst entfällt) | Apple Home zeigt Media-Geräte nicht an — das ist kein Fehler; die Prüfung läuft über `C4`. |

## Teil C — außerhalb von loom mit dem Referenz-Controller (Controller-Seite)

`go build ./examples/reference-controller`; jede Zeile unten ist ein
Aufruf, `--db home.db` vorangestellt. Mit echten Geräten
`--dcl prod` (sonst gelten nur die eingebetteten Test-PAAs, und jedes
Seriengerät fällt mit `PaaNotTrusted` durch — dann ist `--attestation
accept` der bewusste Bypass, nicht der Normalfall). Die
Befehlsreferenz steht in `examples/reference-controller/README.md`.

| # | Schritt | Erwartung |
| --- | --- | --- |
| C1 | `pair --code <QR der Lampe> --dcl prod` mit einer Matter-Lampe oder -Steckdose (IP, kein Thread/BLE) | Pairing; die erste echte PAA-Kette und CRL kommen aus dem Ledger (`--log-level debug` zeigt die GETs); `nodes` zeigt den Node mit Verdict `clean` oder nennt die Abweichung. |
| C2 | `node <id>`, `read <id> '*/OnOff/OnOff'`, `invoke <id> 1/OnOff/Toggle`, `write <id> 0/BasicInformation/NodeLabel '"flur"'` | Vollständiges Bild; Toggle schaltet; der Label-Write wird beim nächsten `read` gezeigt. |
| C3 | `subscribe <id> 1/OnOff/OnOff --max 30`, Lampe am Gerät oder in deren App schalten, Gerät stromlos machen und wieder einschalten | Jeder Wechsel als JSON-Zeile; nach dem Stromausfall meldet die Subscription den Verlust und kommt mit dem Gerät zurück (Address-Monitor, Resumption). |
| C4 | `pair` gegen den Referenz-Daemon mit `--media` (`examples/reference-bridge`), dann `invoke <id> <player-ep>/MediaPlayback/Play` und `read` von `MediaPlayback/CurrentState` | Der einzige Media-Nachweis außerhalb der Familien; Zustand wechselt auf Playing. |
| C5 | Zweites echtes Gerät pairen; `group create 257 --key random --nodes <a>,<b> --endpoints 1`; `group send 257 OnOff/Toggle` | Beide schalten gleichzeitig. Ein Gerät vor Matter 1.5 hat kein Groupcast; der Controller fällt auf KeySetWrite/AddGroup/ACL zurück (README §group). |
| C6 | `bind <b> 1 --to <a>/1/OnOff` — falls Gerät b ein Binding-Client ist (Schalter, Fernbedienung) | b schaltet a ohne Controller; Binding-Eintrag und ACL per `read` sichtbar. |
| C7 | `ota serve --image <datei> --announce <id> --once` mit einem Requestor, der ein Image annimmt (ESP32-Beispiel oder der Referenz-Daemon, dessen Root einen OTA-Requestor trägt) | Zustimmung auf der Konsole, Transfer, Gerät meldet die neue Version; ohne Requestor entfällt der Schritt (In-Repo-Nachweis bleibt). |
| C8 | Controller-Prozess beenden, neu starten, `nodes` | Dieselbe Fabric, dieselbe Root-Node-Id (persistierte Fabric); alle Nodes antworten ohne Neukommissionierung. |
| C9 | loom aus dem Referenz-Controller pairen (`pair --code <loom-QR>`), `nodes`, `node <id>` | Das Verdict von loom aus Sicht des eigenen Device-Type-Validators; jede `violated`-Zeile ist ein Befund für loom oder für `device/`. |
| C10 | Gerät mit CommissionerControl (nur falls vorhanden, z. B. ein Fabric-Sync-Bridge-Beispiel von chip) | Entfällt sonst; der Nachweis ist `TestLiveCommissionerControl`. |
| C11 | `window open <id>` an einem über den Referenz-Controller gepairten Gerät, dann das Gerät mit dem ausgegebenen QR- oder Manual-Code in Apple Home hinzufügen; danach `window revoke <id>` | Apple Home kommissioniert das Gerät als zweiten Admin; `read <id> 0/OperationalCredentials/Fabrics` zeigt zwei Fabrics; `revoke` ohne offenes Fenster meldet „no commissioning window open“. In-Repo-Nachweis: `TestReferenceControllerWindow`. |
| C7a (v0.13.0) | `ota serve --image <file> --announce <id> --once` against the reference daemon started with `--ota-harness` (or a real requestor); the image's header names the device's vendor, product and a higher version | The controller's own node serves the image through the update manager (consent on the console unless `--yes`): the console prints Querying, Downloading, WaitForApply, Applying, Done; `read <id> 0/BasicInformation/SoftwareVersion` shows the new version. `--version`, `--vendor` or `--product` other than the header's is refused. In-repo proof: `TestOTAPushToTheReferenceDaemon`, `TestOTAPushFromTheController`. |
| C13 (v0.13.0) | A peer that serves TCP (the reference daemon, `--tcp true` by default): a small program over `controller.New` with `TCP: controller.TCPConfig{Outgoing: true}` and `TransportPreference: exchange.TransportTCP` pairs it and reads `*/*/*` (the reference controller has no TCP flag) | The read goes over a TCP session (`--log-level debug` shows the connection), the result is the same as over UDP; stopping the daemon closes the TCP session and the next interaction reconnects. In-repo proof: `TestClientNodeTcpParity`, `TestLargeMessageReadOverTCP`. |
| C12 | BLE-Build (`ble/bluez/cmd/reference-controller`, Nutzer in der Gruppe `bluetooth`): ein echtes Wi-Fi-Gerät ab Werk (oder nach Factory-Reset) mit `pair --ble --code <QR> --wifi-ssid <SSID> --wifi-credentials <Passwort> [--ble-adapter hci0]` pairen; `read <id> 0/NetworkCommissioning/Networks`; dann `window open <id>` und das Gerät mit dem ausgegebenen Code in Apple Home hinzufügen | Gefunden über die BLE-Ankündigung, PASE über BTP, Netz-Schritte, danach CASE über IP (DNS-SD); `Networks` zeigt das SSID verbunden; Apple Home kommissioniert als zweiter Admin über IP, `read <id> 0/OperationalCredentials/Fabrics` zeigt zwei Fabrics. Ohne Rechte nennt der Fehler `org.freedesktop.DBus.Error.AccessDenied`. In-Repo-Nachweis: `TestPairOverBLE`, der `ble-virtual`-Workflow. |

## Teil N — Netz und Zeit

| # | Schritt | Erwartung |
| --- | --- | --- |
| N1 | loom-Host mit zwei Interfaces (LAN + WLAN) | Kommissionierung und Erreichbarkeit über beide; mDNS auf beiden (`avahi-browse -r _matter._tcp`). |
| N2 | IPv6-only-Controller (Apple TV als Home-Hub, WLAN ohne IPv4) | Pairing und Steuerung funktionieren (nur IPv6 ist der Normalfall des Moduls). |
| N3 | Uhrzeit am loom-Host um Stunden verstellen, dann zurück | Keine Session-Abbrüche; TimeSynchronization meldet den Sprung, Subscriptions bleiben. |

## Nicht testbar und nicht nötig

DCL-Testnet (nur für Entwicklungszertifikate), Joint Fabric (hinter
`v1.0.0` verschoben), Kamera, Thread, BLE, CastingVideoClient.

## Was ein Befund auslöst

Ein Fehlschlag in L2–L6 oder C1–C3 ist ein Blocker für `v1.0.0`; alles
andere wird als Issue bewertet. Die Ergebnisse kommen als Tabelle mit
Datum, Gerät, App-Version in `notes/handover.md` und als
Phase-5-Zeile in Konzept v2.
