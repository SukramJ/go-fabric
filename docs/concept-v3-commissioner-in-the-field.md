# Konzept v3: Der Commissioner im Feld

Stand 2026-10-16, go-fabric `v0.5.0`. Fortsetzung von
[Konzept v2](./history/concept-v2-scope-extension.md). Sprache ist Deutsch, weil es
ein Planungsstand ist; die ADRs daraus sind Englisch.

Konzept v2 hat den Commissioner gebaut und gegen eigene Nodes, den
Referenz-Daemon und chip bewiesen. Was fehlt, ist der Weg zu **echten
Geräten im Feld**: ein Seriengerät, das noch kein Netz hat, ein
Seriengerät, dessen PAA der Commissioner offline kennen muss, ein
Betreiber, der seine Schlüssel nicht in einer SQLite-Datei will, und ein
Gerät, das nach dem eigenen Pairing auch an Apple Home oder Alexa gehen
soll. Die vier Punkte unten schließen diese Lücken.

Seit 2026-10-17 ist dieses Konzept das Detaildokument von **Meilenstein 1**
der [Roadmap](./roadmap.md) (`v0.7.0`, Phasen 11–13).

Jede matter.js-Zeile gilt am Pin `0ceb202d`
(`git -C ../matter.js show 0ceb202d:<pfad>`).

---

## 0. Entscheidungen des Owners (2026-10-16)

| # | Frage | Entscheidung |
| --- | --- | --- |
| 0.1 | BLE im Commissioner | **Ja.** Der Commissioner kommissioniert Geräte ohne Netz über BLE (BTP) und gibt ihnen Wi-Fi- oder Thread-Zugangsdaten. Die Geräteseite bleibt ohne Bluetooth. [ADR 0024](./adr/0024-ble-commissioning-in-the-controller.md). |
| 0.2 | Offline-Vertrauensbasis | **Ja.** Ein eingebetteter Schnappschuss der Produktions-PAAs und CD-Signer aus dem DCL, Modi dev/prod/none. |
| 0.3 | Schlüssel über `crypto.Signer` | **Ja, vor dem `v1.0.0`-Freeze**, weil die Naht später ein Break wäre. |
| 0.4 | Fenster öffnen im Referenz-Controller | **Ja.** Multi-Admin aus unserer Richtung. |
| 0.5 | Status-Tabelle im README | **Ja.** Klein. |

Unverändert: `v1.0.0` folgt den Befunden aus dem Testplan
(`notes/manual-test-plan-0.4.0.md`) und friert `device/`, `node/` und
`controller/` ein; Joint Fabric bleibt dahinter (ADR 0020, Stufe 6c).

---

## Teil A: Was die Kandidaten sind

### A.1 BLE-Commissioning (Entscheidung 0.1)

**Was fehlt.** Ein Wi-Fi- oder Thread-Gerät ab Werk hat kein Netz; sein
Commissioning-Kanal ist BLE. `controller/commissioner` kennt nur DNS-SD
und UDP (`commissioner.go:83` nennt BLE, Wi-Fi und Thread ausdrücklich als
nicht portiert). Der Referenz-Controller kann deshalb nur Geräte pairen,
die schon im Netz sind; Teil C des Testplans ist so auf Ethernet- und
bereits onboardete Geräte beschränkt.

**Das Maß in matter.js.**

| Teil | matter.js | Zeilen |
| --- | --- | --- |
| BTP: Handshake, Fragmentierung, Fenster, Acks | `packages/protocol/src/ble/BtpSessionHandler.ts`, `Ble.ts`, `BleConsts.ts` | 875 |
| BLE-Advertisement eines kommissionierbaren Geräts | `packages/protocol/src/advertisement/ble/BleAdvertisement.ts` | (mit gezählt unten) |
| Plattform: Scanner, Central, GATT-Kanal | `packages/nodejs-ble/src/` (über `@stoprocent/noble`) | 2 092 |
| Netz-Schritte des Commissionings | `ControllerCommissioningFlow.ts:1527-1900` (`addOrUpdateWiFiNetwork` :1677, `addOrUpdateThreadNetwork` :1874, `connectNetwork`, Fehlerklassen :235-283) | ~400 |

chip ist für das Verhalten des Werkzeugs die zweite Referenz
(`src/ble/BLEEndPoint.cpp`, `src/platform/Linux/bluez/`).

**Die Go-Seite.** Die Plattformschicht ist die einzige, die eine
Abhängigkeit braucht. Unter Linux spricht man BlueZ über D-Bus; das geht
ohne cgo (`github.com/godbus/dbus/v5`, BSD-2-Clause). macOS (CoreBluetooth)
und Windows (WinRT) brauchen cgo oder COM und sind draußen. Allgemeine
Go-BLE-Bibliotheken bündeln alle Plattformen und ziehen deren
Abhängigkeitsbaum mit; wir sprechen die wenigen D-Bus-Aufrufe (Discovery,
GATT-Client, Notify) selbst.

**Schnitt (ADR 0024).**

1. `transport/btp` — der BTP-Codec und die Session als reiner Go-Code im
   Hauptmodul, Port von `BtpSessionHandler.ts`, gegen matter.js' eigene
   Testvektoren. Keine Abhängigkeit.
2. `controller/ble` — die Naht: `Central` (scannen, verbinden, GATT
   schreiben/abonnieren) als Interface, das Parsen des Advertisements
   (Discriminator, Vendor, Product) aus `BleAdvertisement.ts`, und der
   Adapter, der aus einem `Central` einen Transport für PASE macht.
3. **Ein eigenes Go-Modul** `github.com/SukramJ/go-fabric/ble/bluez`
   (eigenes `go.mod`) implementiert `Central` über BlueZ/D-Bus. So bleibt
   das Hauptmodul bei seinen fünf direkten Abhängigkeiten, und ein Host
   ohne BLE zieht `godbus` nie.
4. `controller/commissioner` — die Netz-Schritte nach
   `ControllerCommissioningFlow.ts`: `ScanNetworks` (optional),
   `AddOrUpdateWiFiNetwork` oder `AddOrUpdateThreadNetwork`,
   `ConnectNetwork`, dann der Wechsel auf die operative Discovery über
   DNS-SD und CASE. `Options.Network` mit Wi-Fi-SSID/Passwort oder
   Thread-Datensatz; die matter.js-Fehlerklassen als Go-Fehler.
5. Referenz-Controller: `pair --ble --wifi-ssid … --wifi-pass …` und
   `pair --ble --thread-dataset <hex>`.

**Thread.** Die Scope-Grenze „kein Thread“ betrifft das Funkmodul eines
Nodes dieses Moduls. Einem Gerät einen Thread-Datensatz zu geben ist eine
Commissioner-Aufgabe ohne Thread-Funk auf unserer Seite; danach erreicht
der Commissioner das Gerät über den Border-Router per IPv6 wie jedes
andere. Deshalb gehört Thread-Provisioning in den Schnitt; ohne Border-Router
ist es nicht testbar und bleibt im Testplan optional.

**Nachweis.** BTP gegen die matter.js-Vektoren (`packages/protocol/test/ble/`);
die Netz-Schritte gegen einen Node mit NetworkCommissioning-Wi-Fi-Feature
(der Referenz-Daemon bekommt dafür einen simulierten Wi-Fi-Treiber,
TESTING ONLY, wie `--wifi` für die Diagnose); BLE Ende-zu-Ende mit
virtuellen HCI-Controllern (`btvirt`, Linux) und chips `chip-all-clusters-app`
mit BLE in einem eigenen, manuell gestarteten CI-Job, weil er
Kernel-Rechte braucht; ein echtes Gerät im Testplan.

**Kosten und Risiken.** Rund 3 000 bis 4 000 Go-Zeilen (Codec, Naht,
BlueZ-Adapter, Netz-Schritte, CLI). BlueZ verlangt Rechte auf dem
System-Bus (Gruppe `bluetooth` oder Polkit) — der Referenz-Controller
nennt das im README. BLE-Stacks sind launisch; der `btvirt`-Job ist die
einzige automatische Gegenstelle.

### A.2 Eingebetteter DCL-Schnappschuss (Entscheidung 0.2)

**Was fehlt.** `--dcl off` heißt heute „nur die CSA-Test-PAAs“; jedes
Seriengerät fällt mit `PaaNotTrusted` durch, und `--dcl prod` braucht bei
jedem Pairing das Netz.

**Schnitt.** `controller/dcl/snapshot`: die Produktions-PAAs und
CD-Signer als DER-Dateien mit einer `SOURCE`-Datei (Ledger-URL, Abrufzeit,
Anzahl, SHA-256 je Datei), eingebettet per `embed`, erzeugt mit dem
eigenen `dcl.Client` durch `make update-dcl-snapshot` (ein Review-Schritt
wie das Schema-Update, nicht automatisch). `attestation.TrustMode`:
`Development` (Schnappschuss plus Test-PAAs, Standard des
Referenz-Controllers), `Production` (nur Schnappschuss, optional online
ergänzt durch `dcl.CertificateStore`), `None` (keine Prüfung, nur mit
ausdrücklichem Flag). CRL-Prüfung bleibt online (ADR 0020, 6a).

**Zu klären vor dem Commit.** Die Nutzungsbedingungen der DCL-Daten: PAA-
und CD-Signer-Zertifikate sind öffentlich, aber die Weitergabe als Paket
braucht eine Quelle und einen Hinweis. Ein Agent liest die Bedingungen,
der Owner entscheidet; ohne Klarheit bleibt der Schnappschuss ein
lokal erzeugter Cache statt eines eingebetteten.

### A.3 Schlüssel über `crypto.Signer` (Entscheidung 0.3)

**Was fehlt.** Der CA-Schlüssel des Controllers und sein operativer
Schlüssel liegen seit Welle 26 im Fabric-Record der SQLite-Datei
(`ca.SettingStore`, die Form von matter.js' `Fabric.Config`,
`Fabric.ts:53-74`). Ein Betreiber mit HSM, TPM oder Cloud-KMS kann sie
dort nicht herausnehmen.

**Schnitt.** `controller/ca` und `commissioner.FabricConfig` nehmen den
CA- und den operativen Schlüssel als `crypto.Signer` (P-256); der
gespeicherte Record hält dann nur die Zertifikate und einen Verweis.
Mitgeliefert: der bisherige SQLite-Weg als Standard (abwärtskompatibel),
eine passphrasen-verschlüsselte Datei (`scrypt` + AES-GCM aus der
Standardbibliothek bzw. `golang.org/x/crypto` — prüfen, ob es schon im
Graph ist) und der Hinweis, wie ein PKCS#11- oder KMS-Signer
angeschlossen wird. Die Geräteseite (`node`, der NOC-Schlüssel eines
Nodes) bekommt dieselbe Naht nur, wenn sie ohne Break geht; sonst ein
eigener Punkt vor dem Freeze.

### A.4 Fenster öffnen im Referenz-Controller (Entscheidung 0.4)

**Was fehlt.** Ein über unseren Controller gepairtes Gerät kann man nur
mit rohem `invoke` und selbst berechnetem PAKE-Verifier für einen zweiten
Admin öffnen — praktisch gar nicht.

**Schnitt.** `reference-controller window open <node> [--timeout s]
[--basic]`: Enhanced (Standard) erzeugt Passcode, Salt, Iterationen und
Verifier und sendet `OpenCommissioningWindow`, Basic sendet
`OpenBasicCommissioningWindow`; ausgegeben werden Manual- und QR-Code.
`window revoke <node>`. Vorbild ist matter.js `PairedNode.ts:1376-1420`
(`openBasicCommissioningWindow`, `openEnhancedCommissioningWindow`, 900 s
Standard) und `CommissioningClient.ts` um :451. Kein neuer Protokollcode:
Verifier und Codes gibt es schon (`spake2`, `secure/setup`).

### A.5 Status-Tabelle im README (Entscheidung 0.5)

Eine kompakte Tabelle „was geht, was nicht, warum“ oben im README,
verlinkt auf `certifiability.md`, `matterjs-comparison.md` und die
ADRs.

---

## Teil B: Phasen

| Phase | Inhalt | ADR | Version | Nachweis |
| --- | --- | --- | --- | --- |
| 11 | A.4 Fenster öffnen, A.5 README-Status, A.2 DCL-Schnappschuss (nach Klärung der Bedingungen), A.3 `crypto.Signer` im Controller | — | `v0.6.0` | In-Repo-Tests; A.4 im Testplan (Gerät an Apple Home weitergeben) |
| 12 | A.1 BLE: `transport/btp`, `controller/ble`, Netz-Schritte im Commissioner, simulierter Wi-Fi-Treiber im Daemon | 0024 | — | BTP-Vektoren, Netz-Schritte gegen den Daemon |
| 13 | A.1 BLE: Modul `ble/bluez`, CLI `pair --ble`, `btvirt`-Job mit chips all-clusters-app | 0024 | `v0.7.0` | BLE Ende-zu-Ende virtuell; echtes Gerät im Testplan |
| — | `v1.0.0`-Freeze nach den Testplan-Befunden | — | `v1.0.0` | — |

Phase 11 und 12 können parallel laufen (disjunkte Pfade: `controller/dcl`,
`controller/ca`, `examples/reference-controller` gegen `transport/btp`,
`controller/ble`, `controller/commissioner`). Der Harness gehört in Phase
13 dem BLE-Job.

---

## Teil C: Was sich an Grenzen ändert

- `CLAUDE.md` §Scope: „no Bluetooth“ wird zu „no Bluetooth on the
  device side; the commissioner speaks BLE (ADR 0024)“.
- `docs/matterjs-comparison.md`: die BLE-Zeile und Non-goal 2 verweisen
  auf ADR 0024.
- Abhängigkeiten: das Hauptmodul bleibt bei fünf; `godbus/dbus` nur im
  Untermodul `ble/bluez` (BSD-2-Clause, in dessen `THIRD-PARTY-NOTICES.md`).
- Plattformen: BLE nur unter Linux. macOS und Windows bleiben draußen,
  solange sie cgo verlangen.

## Teil D: Risiken

| Risiko | Folge | Gegenmittel |
| --- | --- | --- |
| BlueZ-Versionen und Rechte | Pairing scheitert beim Nutzer, nicht im Test | README nennt Rechte und Mindestversion; der Fehler nennt den D-Bus-Grund |
| `btvirt` in CI | Job braucht privilegierte Runner | manuell gestarteter Job, lokaler Lauf als Standard |
| DCL-Bedingungen | Schnappschuss darf nicht eingebettet werden | lokal erzeugter Cache statt Einbettung |
| `crypto.Signer`-Naht bricht API | Break kurz vor 1.0 | Standardweg unverändert, neue Felder optional |
