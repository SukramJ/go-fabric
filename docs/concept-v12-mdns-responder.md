# Konzept v12 — Der eigene mDNS-Responder: alle Records aus einer Hand, 120 s wie matter.js

Stand 2026-11-02, Issue [#362](https://github.com/SukramJ/go-fabric/issues/362)
(Review 2026-11, W2-wire-06, `by_design` L7-D02). Pins: matter.js 862ad75f,
connectedhomeip 6170af84. Faktenbasis: Lesekarte `read-mdns-ttl.md`
(Sonnet, 2026-11-02), jede tragende Zeile vom Reviewer am Pin nachgelesen.

## A. Das Problem, eine Stufe höher

Das Modul besitzt die Matter-Wire; DNS-SD ist Wire. Heute ist der
Advertiser aber zweigeteilt: Die primären Records (PTR auf den Instanznamen,
die Service-Enumeration `_services._dns-sd._udp`, SRV, TXT) baut und
beantwortet `github.com/grandcat/zeroconf` v1.0.0 (`mdns/zeroconf.go:420`
`RegisterProxy`, `:564`/`:739` `Shutdown` — mehr ruft das Modul nicht auf);
die Subtype-PTRs (`_L`, `_S`, `_CM`, `_V`, `_T`, Fabric-Subtype) und die
Host-A/AAAA-Antworten baut das Modul selbst auf miekg/dns
(`mdns/subtype_responder.go`, 815 Zeilen; `mdns/host_responder.go`, 481
Zeilen) auf eigenen Multicast-Sockets neben grandcats Socket
(`subtype_responder.go:654`, `:700`).

Die Folgen dieser Teilung:

1. **TTL.** grandcat gibt PTR/SRV/TXT 3200 s (`server.go:178`); A/AAAA
   erzwingt es auf 120 s (`server.go:613-618`). matter.js baut jeden Record
   mit `DEFAULT_MDNS_TTL` = 120 s (`packages/general/src/codec/DnsCodec.ts:20`),
   chip ebenfalls (`src/lib/dnssd/wire/records/ResourceRecord.h:35`; nur TXT
   4500 s, `wire/records/Txt.h:30`). Unsere eigenen Records tragen 120 s
   (`subtype_responder.go:35`, `host_responder.go:40`). Ein Knoten, der ohne
   Goodbye verschwindet, bleibt 53 Minuten statt zwei im Cache eines
   Controllers. grandcats Setter `Server.TTL` (`server.go:206-209`) schreibt
   `s.ttl` unsynchronisiert, nachdem `RegisterProxy` die Goroutinen schon
   gestartet hat (`server.go:135-136`) — der Race, den L7-D02 nennt.
2. **Race-Gate.** grandcats `Shutdown` hat einen eigenen Data Race; das
   gesamte mdns-Testbinary überspringt sich unter `-race`
   (`mdns/race_skip_test.go:14-42`). `make race` ist Release-Gate, und
   ausgerechnet das Discovery-Paket ist davon ausgenommen.
3. **Parität.** Das Verhalten der primären Records ist nicht das von
   matter.js: grandcat probt zweimal und kündigt zweimal an (+1 s, +2 s),
   dann nie wieder (`server.go:512-573`); es beantwortet nur Service-Typ-,
   Service-Name- und Instanz-Fragen, Known-Answer-Suppression nur für den
   ersten PTR, kein Rate-Limit, keine Unicast-Response-Behandlung. Die
   Paritätsfälle von matter.js' Responder sind deshalb als "missing"
   registriert, weil "grandcat die Pakete baut, die ein Unit-Test nicht
   treiben kann" (`script/testparity/register.json:4256`, `:4273`).
4. **Abhängigkeiten.** grandcat bringt `cenkalti/backoff`, `pkg/errors` und
   ein altes miekg/dns mit (`go.mod` der Abhängigkeit); das Modul hat
   miekg/dns v1.1.73 ohnehin direkt.

Der Fehler liegt nicht in einem TTL-Wert, sondern im Schnitt: Die Hälfte des
Responders lebt in einer Abhängigkeit mit anderem Verhalten als der
Goldstandard, und die andere Hälfte umgeht sie auf einem zweiten Socket.

## B. Ziel

Ein Responder im Modul, modelliert an matter.js' `MdnsServer`
(`packages/protocol/src/mdns/MdnsServer.ts`, 570 Zeilen) und
`MdnsAdvertisement`/`MdnsAdvertiser`
(`packages/protocol/src/advertisement/mdns/`): Er baut den vollständigen
Record-Satz jedes publizierten Service, beantwortet jede Frage dazu, kündigt
an, verabschiedet sich mit TTL 0 — alle Records 120 s, Unique-Records mit
Cache-Flush, Known-Answer-Suppression, Unicast-Response (QU), Truncated
Queries, Rate-Limit — über die Sockets, die das Modul heute schon für die
Subtypes hält. grandcat fällt weg; das mdns-Paket läuft unter `-race`;
die als "missing" registrierten Responder-Paritätsfälle werden portierbar.

## C. Architektur

### C.1 Die drei Optionen

| | (a) Eigener Responder | (b) Fork von grandcat (`internal/zeroconf`) | (c) `Server.TTL(120)` nach `RegisterProxy` |
| --- | --- | --- | --- |
| TTL 120 s auf allen Records | ja | ja (Konstante im Fork) | ja, aber der Schreibzugriff raced mit den laufenden Goroutinen — `make race` rot; verworfen |
| Verhalten = matter.js | ja: `MdnsServer` ist das Modell, Fall für Fall portiert | nein: grandcats Probing/Antwortlogik bleibt, ein Fork pflegt fremdes Verhalten | nein |
| Zwei Sockets, zwei Antwortmaschinen | **ein** Responder, ein Socket-Paar (v4/v6) | bleibt zweigeteilt | bleibt |
| `-race` auf mdns | ja (eigener Code, Lifecycle wie `SubtypeResponder`) | nur nach Reparatur des Fork-Shutdowns | nein |
| Abhängigkeiten | grandcat, backoff, pkg/errors weg | grandcat weg, Fork-Code (1541 Zeilen MIT) im Baum, `THIRD-PARTY-NOTICES` bleibt | unverändert |
| Aufwand | ~900–1200 Zeilen Go inkl. Tests; das Modul hat Host- und Subtype-Antwort bereits | ~300 Zeilen Änderung + 1541 übernommene | 3 Zeilen |
| Risiko | Interop mit Apple/Google-Caches — durch die chip-tool-Suite (Commissioning über DNS-SD) und einen manuellen Ökosystem-Test abgedeckt | gering, aber die Paritätslücke bleibt | — |

**Entscheidung des Konzepts: (a).** Begründung in einem Satz: Das Modul
hat zwei Drittel des Responders schon (Host-Antworten mit Known-Answer,
Viertel-TTL-Regel, Rate-Limit, Duplikat-Unterdrückung; Subtype-Antworten
mit Announce und Goodbye), und das fehlende Drittel — die primären Records —
ist genau der Teil, dessen Verhalten vom Goldstandard abweicht.

### C.2 Die Form

```
mdns/
  responder.go        Responder: Publish/Withdraw/Active/Close (Advertiser),
                      StartReannounceLoop/TriggerReannounce (Reannouncer)
  records.go          recordSet(svc Service) — der Record-Satz eines Service
  answer.go           die Antwortmaschine: Frage → Answers + Additionals
  announce.go         Announce-Plan, Goodbye
  host_responder.go   bleibt: hostAnswerer wird Teil der Antwortmaschine
  subtype_responder.go geht in records.go/answer.go auf (die Sockets bleiben)
  zeroconf.go         entfällt
```

**Record-Satz** je Service, wie matter.js `OperationalMdnsAdvertisement.ts:37-40`
und das Commissionable-Pendant: PTR `_services._dns-sd._udp.local` →
Service-Typ; PTR `_services._dns-sd._udp.local` → jeder Subtype; PTR
Service-Typ → Instanz; PTR Subtype → Instanz; SRV Instanz → Host:Port; TXT
Instanz; A/AAAA Host. TTL 120 s überall (matter.js vor chip: TXT 120, nicht
4500 — Rangfolge 1). Unique-Records (SRV, TXT, A, AAAA) mit Cache-Flush-Bit
(`MdnsAdvertisement.ts:182` `DnsRecordType.isUnique`), Shared-Records (PTR)
ohne.

**Antwortmaschine** wie `MdnsServer.#handleMessage` (`MdnsServer.ts:103-203`):
Frage gegen den Record-Satz (`#queryRecords`, `:377`), Additionals (SRV/TXT
zum PTR, A/AAAA zum SRV), Known-Answer-Suppression (`:366`), QU-Bit:
Unicast-Antwort nur, wenn jede Frage es setzt und der Record nicht kürzlich
multicast ging (`:153-167`), Truncated Queries sammeln (`:446`),
Interface-bezogene Antworten (`:205`, `:353`). Die Regeln, die
`hostAnswerer` heute schon hat (`host_responder.go:60-72`, `:163-350`:
Viertel-TTL-Unicast, 900 ms Rate-Limit, 999 ms Duplikat-Unterdrückung),
gelten für alle Records.

**Announce und Goodbye.** Publish kündigt den Satz an; Goodbye ist der
Satz mit TTL 0 (`MdnsServer.ts:243-275` `expireAnnouncements`). matter.js
probt nicht (kein `probe` in `MdnsServer.ts`); chip auch nicht. Der
Announce-Plan folgt `MdnsAdvertiser.DefaultBroadcastSchedule`
(`MdnsAdvertiser.ts:213-232`): 1 s Start, Faktor 2, max 90 s, Jitter 0,25,
bis zum Commissioning-Timeout; operational endet mit der ersten Verbindung
(`broadcastAfterConnection: Instant`). Die heutige periodische
Re-Announce-Schleife (`node/config.go:299`, 30 min, begründet mit Apples
75-Minuten-Cache) verliert mit 120 s ihre Begründung: Ein Cache fragt vor
Ablauf nach (RFC 6762 §5.2, matter.js' eigener Cache hält TTL × 1,05,
`DnssdName.ts:25`, `:152`). Der Kommentar `mdns/reannounce.go:14-20`
("matter.js sendet keine unaufgeforderte Ankündigung", gegen 0ceb202d) ist
am Pin falsch — die Schleife wird durch den Announce-Plan ersetzt, siehe
Owner-Entscheidung 3.

**Sockets.** Die v4/v6-Multicast-Sockets des `SubtypeResponder`
(`subtype_responder.go:652-720`, Wildcard-Bind + JoinGroup je Interface,
Interface-Filter `interface_filter.go`) werden die Sockets des Responders.
Nichts bindet mehr daneben.

**Öffentliche API.** `Advertiser` (`advertiser.go:26-38`), `Reannouncer`,
`Enabled`, `Noop`, `Service` und die Builder bleiben. `NewZeroconf` wird
`NewResponder` (Regel: entfernen, nicht deprecaten, Migrationszeile im
Guide; der Knoten baut den Advertiser selbst, `node/mdns.go:48-59`, Hosts
rufen ihn in der Regel nicht direkt). Kein grandcat-Typ steckt heute in einer
exportierten Signatur (`zeroconf.go:71` ist unexportiert).

### C.3 Besser als vorher, besser als andere — stimmt der Schnitt?

- **Vorher:** zwei Antwortmaschinen auf zwei Sockets, Verhalten der primären
  Records aus einer Abhängigkeit, die nicht der Goldstandard ist; ein
  Race-Gate mit Ausnahme. **Nachher:** eine Antwortmaschine, ein Record-Satz,
  ein Verhalten, das matter.js Fall für Fall folgt und dessen Paritätsfälle
  portierbar sind; `make race` ohne Ausnahme.
- **Gegen matter.js:** gleicher Schnitt — `MdnsServer` (Antwort) und
  `MdnsAdvertisement` (Record-Satz, Announce-Plan) sind dort zwei Klassen;
  hier `answer.go` und `records.go`/`announce.go`. Nichts, was matter.js
  nicht hat, kommt hinzu; die Viertel-TTL- und Rate-Limit-Regeln sind
  RFC 6762 §6, die matter.js in `#handleMessage` ebenso anwendet.
- **Gegen chip:** chip hat denselben eigenen Responder (`minimal_mdns`),
  wählt aber TXT 4500 s. Wir folgen matter.js (Rangfolge 1); die Abweichung
  zu chip wird nicht notiert, weil chip hier nicht die Autorität ist.
- **Der Schnitt zwischen Modul und Host** ändert sich nicht: `Advertiser`
  bleibt die Schnittstelle, `Noop` und die fabrictest-Harness bleiben; die
  Serverfamilie und loom sehen nur den neuen Konstruktornamen.

## D. Entscheidungen, die das Konzept trifft

1. Option (a); grandcat und seine transitiven Abhängigkeiten gehen in
   derselben PR (`go.mod`, `THIRD-PARTY-NOTICES.md:100`, `:147`;
   `mdns/race_skip_test.go` wird gelöscht).
2. TTL 120 s auf allen Records, auch TXT (matter.js vor chip).
3. Kein Probing (matter.js und chip proben nicht).
4. L7-D02 wird nach dem Merge zurückgezogen (Retired-Tabelle); die
   `register.json`-Einträge 4256/4273 werden zu portierten Fällen.
5. Die Paritätstests kommen aus matter.js' Responder-Tests
   (`packages/protocol/test/mdns/` — Phase 0 kartiert sie) und ergänzen
   `TestSubtypePTRTTLParity` um einen Fall je Record-Typ.
6. Akzeptanz auf der Wire: `make chiptool-test` (Commissioning über DNS-SD)
   und die Familien der Commissioning-Gruppe; dazu der manuelle
   Ökosystem-Test (Owner-Entscheidung 2).

## E. Owner-Entscheidungen (2026-11-02)

1. **Option (a)**, der eigene Responder.
2. **Manueller Ökosystem-Test ist Pflicht** vor dem Tag der Release, die
   den Responder enthält: Apple Home und Google Home — Commissioning,
   Neustart des Knotens ohne Goodbye, Wiederfinden nach 2 min. Der Reviewer
   liefert den Testplan mit der P1-PR; der Owner testet auf seiner Hardware.
3. **Die Re-Announce-Schleife wird entfernt** zugunsten des
   matter.js-Announce-Plans: `node.Config.Network.Reannounce`, das
   `Reannouncer`-Interface und `StartReannounceLoop` gehen mit
   Migrationszeile; `TriggerReannounce` bleibt als On-Demand-Ankündigung
   (`Node.TriggerReannounce`).
4. **`NewZeroconf` wird `NewResponder`** in derselben PR, Migrationszeile im
   Guide (loom: nur der Paritätstest `internal/netutil/gofabric_parity_test.go:69`).

## F. Phasen

- **P0 (Sonnet, Lesekarte):** matter.js `packages/protocol/test/mdns/` und
  `test/advertisement/`: jeder Testfall mit Zeile, was er pinnt; chip
  `src/lib/dnssd/minimal_mdns/tests/` als Zweitquelle. Reviewer liest die
  tragenden Zeilen.
- **P1 (Opus, eine PR):** `records.go`, `answer.go`, `announce.go`,
  `responder.go`; Host- und Subtype-Logik hineingezogen; grandcat entfernt;
  Paritätstests portiert; `race_skip_test.go` gelöscht; Floors für `mdns`
  neu gemessen; L7-D02 zurückgezogen; `docs/guides/advanced-seams.md:180`
  und die Paketdoku; CHANGELOG Changed (TTL, Konstruktorname, Abhängigkeit
  weg) mit Migrationszeile. Guards: `make ci`, `make race ./mdns/...` ohne
  Skip, `make chiptool-test`, `GOFABRIC_CHIP_FAMILIES=<Commissioning-Gruppe>
  make chiptool-families`, `make apidiff-check` (nennt `NewZeroconf`).
- **P2 (Reviewer, Fable):** jede matter.js-Zeile der Kommentare am Pin;
  die Antwortmaschine gegen `MdnsServer.ts` gelesen; Owner-Entscheidung 2
  ausgeführt; dann Tag.

## G. Was das Konzept nicht tut

- Kein allgemeiner mDNS-Stack (`mdns/doc.go:30-36` bleibt wahr): nur die
  Records und Fragen, die Matter-DNS-SD braucht.
- Kein Probing, keine Konfliktauflösung über RFC 6762 §8 hinaus dessen,
  was matter.js tut (nichts).
- Die Querier-Seite (`operational_resolver.go`, `query_conn.go`,
  `client/discovery`) bleibt unberührt.
- Kein Thread/BLE-Bezug; kein Wechsel der Hostname-Regel (L7-D03).
