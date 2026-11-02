# Manueller Testplan — der eigene mDNS-Responder (Konzept v12, #362)

Stand 2026-11-02, geschrieben für den Owner am Gerät. Owner-Entscheidung
§E.2 des Konzepts: Dieser Test ist **Pflicht vor dem Tag** der Release, die
den Responder enthält. Was automatisch geprüft ist und hier nicht
wiederholt wird: die portierten matter.js-Responder-Fälle (`mdns/*parity*`),
`make race ./mdns/...`, `make chiptool-test` und die Commissioning-Familien.
Was nur manuell geht: die Caches von Apples mDNSResponder und Googles
Resolver, echtes Netz, echte Zeit.

Voraussetzung: loom (oder der Referenz-Daemon `examples/reference-bridge`)
auf dem PR-Stand, `--log-level debug`; ein Mac oder iPhone im selben
Netz für `dns-sd`. Jeder Befund wird als Issue mit dem Kürzel (z. B. `M3`)
und dem Log angelegt.

## Teil M — Wire (Mac/Linux, ohne Ökosystem)

| # | Schritt | Erwartung |
| --- | --- | --- |
| M1 | Daemon starten; `dns-sd -B _matter._tcp` und `dns-sd -B _matterc._udp` (macOS) bzw. `avahi-browse -r _matter._tcp` (Linux) | Instanz erscheint innerhalb von 2 s; bei offenem Fenster die commissionable Instanz mit Subtypes `_L`, `_S`, `_CM`, `_V` |
| M2 | `dns-sd -L <Instanz> _matter._tcp` | SRV mit Host `<MAC-Hostname>.local` und Port, TXT wie `mdns.Service` baut (SII/SAI/SAT/T/ICD) |
| M3 | Wireshark/`tcpdump -i <if> udp port 5353 -v`: ein Announce des Daemons lesen | Alle Records TTL 120 (PTR, SRV, TXT, A/AAAA, Subtype-PTRs); Cache-Flush-Bit nur auf SRV/TXT/A/AAAA; keine Probes; Announces bei 1 s, 2 s, 4 s … bis 90 s |
| M4 | Daemon sauber stoppen (SIGTERM) | Goodbye-Paket: derselbe Record-Satz mit TTL 0; `dns-sd -B` zeigt `Rmv` innerhalb von 1 s |
| M5 | Daemon mit `kill -9` beenden, Stoppuhr | `dns-sd -B` zeigt `Rmv` nach 120 s ± 10 s (vorher: 53 min) |
| M6 | Frage mit QU-Bit (`dns-sd -Q <Instanz>._matter._tcp.local SRV`) zweimal binnen 1 s | Erste Antwort multicast oder unicast je Regel, zweite Frage innerhalb 999 ms unterdrückt (Debug-Log `mdns.suppressed`) |

## Teil A — Apple Home

| # | Schritt | Erwartung |
| --- | --- | --- |
| A1 | Neues Zuhause, Kommissionierung per QR-Code | Pairing unter 60 s; Geräte erscheinen |
| A2 | Daemon neu starten (sauber) | Geräte in Home unter 10 s wieder erreichbar |
| A3 | Daemon `kill -9`, 3 Minuten warten, dann starten | Home zeigt „Keine Antwort“ innerhalb von ~2–3 min; nach dem Start unter 30 s wieder erreichbar (Home findet die neue Ankündigung) |
| A4 | Daemon 12 Stunden laufen lassen, zwischendurch Aktionen | Keine „Keine Antwort“-Phasen; im Debug-Log keine Anfragen, die unbeantwortet bleiben (`mdns.query` ohne `mdns.answer`) |
| A5 | Fabric in Home entfernen | Operational Record der Fabric per Goodbye zurückgezogen (`dns-sd -B _matter._tcp`: `Rmv` des Fabric-Subtypes) |

## Teil G — Google Home

| # | Schritt | Erwartung |
| --- | --- | --- |
| G1 | Kommissionierung per QR-Code (Google Home App, Matter) | Pairing unter 90 s |
| G2 | Daemon sauber neu starten | Geräte unter 10 s erreichbar |
| G3 | Daemon `kill -9`, 3 Minuten, starten | Wie A3 |
| G4 | 12 Stunden laufen lassen | Wie A4 |

## Teil N — Netz

| # | Schritt | Erwartung |
| --- | --- | --- |
| N1 | Zweites Interface (WLAN + LAN) am Daemon-Host | Jede Antwort auf dem Interface der Frage; keine Records fremder Interfaces (Debug-Log `interface=`) |
| N2 | IPv6-only-Netz (IPv4 am Host abschalten) | Discovery und Pairing über AAAA; keine A-Records angekündigt |
| N3 | Zwei Daemons im selben Netz (loom + Referenz-Daemon) | Beide sichtbar, keine Hostnamen-Kollision (L7-D03: MAC-Hostname) |

## Was ein Befund auslöst

Ein roter M-Punkt ist ein Modul-Defekt (Issue mit `tcpdump`-Mitschnitt).
Ein roter A/G-Punkt bei grünen M-Punkten ist ein Ökosystem-Befund: matter.js
am Pin lesen (`MdnsServer.ts`, `MdnsAdvertiser.ts`), ob matter.js hier etwas
anders tut, bevor ein eigener Weg entsteht. Kein Tag, solange A1–A3 oder
G1–G3 rot sind.
