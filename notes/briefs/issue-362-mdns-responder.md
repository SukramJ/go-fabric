# Brief: issue #362 — the module's own mDNS responder (concept v12, P1)

Branch `fix/362-mdns-responder` from `main` at ad70cf1e (concept v12 merged,
#372). One PR, title "mdns: the module's own responder — every record at
120 s as matter.js, grandcat/zeroconf removed, NewResponder, the re-announce
loop replaced by the broadcast schedule (#362)". No release with this PR:
the owner's manual Apple Home / Google Home test is mandatory before the
tag (concept v12 §E.2; the reviewer writes that test plan, not the agent).

The concept is `docs/history/concept-v12-mdns-responder.md` (moved there by the P1 PR once executed) (German; §C.2 is the
shape, §D the decisions, §E the owner's decisions). Read it first. Pins:
matter.js `../matter.js` at 862ad75f, chip via
`git -C ../connectedhomeip show 6170af84:<path>`, grandcat v1.0.0 in
`$(go env GOMODCACHE)/github.com/grandcat/zeroconf@v1.0.0`. Two read maps
with `file:line` for every fact are at
`~/.cache/go-fabric-scratch/read-mdns-ttl.md` (the package, grandcat,
matter.js and chip TTL/announce behaviour) and
`~/.cache/go-fabric-scratch/read-mdns-tests.md` (every matter.js test to
port, every existing go-fabric mdns test, the register entries, the node
side). They are maps, not citations: re-read every line you cite.

## What exists (keep, fold in, remove)

- Keep: `mdns/advertiser.go` (`Advertiser`, `Noop`), `service.go`,
  `commissioner.go`, `rotating_id.go`, `diagnose.go`, the querier side
  (`operational_resolver.go`, `query_conn.go`), `interface_filter.go`.
- Fold in: `host_responder.go` (`hostAnswerer`: A/AAAA answers,
  known-answer, quarter-TTL unicast rule, 900 ms rate limit, 999 ms
  duplicate suppression, `host_responder.go:60-72`, `:163-350`) and
  `subtype_responder.go` (the v4/v6 multicast sockets `:652-720`, the
  subtype PTR answers and announcements `:583-640`, goodbye `:156-169`,
  `subtypePTRTTL` `:35`). The sockets become the responder's; the two answer
  paths become one answer engine over the whole record set.
- Remove: `zeroconf.go` (grandcat `RegisterProxy` `:420`, `Shutdown`
  `:564`, `:739`), `reannounce.go` (`Reannouncer`, `StartReannounceLoop`;
  keep an on-demand `TriggerReannounce` — see Node below),
  `race_skip_test.go`, the grandcat module and its transitive-only
  dependencies (`go.mod:7`, `cenkalti/backoff`, `pkg/errors`; `x/net` and
  `miekg/dns` stay, both direct), `THIRD-PARTY-NOTICES.md:100`, `:147`,
  the `.github/dependabot.yml:4` comment.

## The responder (matter.js is the model)

Files per concept §C.2: `records.go`, `answer.go`, `announce.go`,
`responder.go` (type `Responder`, `NewResponder()`; implements `Advertiser`;
`SetLogger`, `HostName`, `InterfaceFilter` as `Zeroconf` had them —
`node/mdns.go:40-61` and `examples/reference-controller/cli/self.go:53`
are the callers that set them).

1. **Record set** per `Service` — `records.go`, mirroring
   `packages/protocol/src/advertisement/mdns/MdnsAdvertisement.ts:154-184`
   (`#recordsFor`: ptrRecords + SRV priority 0 weight 0 + TXT + A/AAAA) and
   the PTR lists: operational `OperationalMdnsAdvertisement.ts:30-37`
   (`_services._dns-sd._udp` → `_matter._tcp`, `_services` → fabric
   subtype, `_matter._tcp` → instance, fabric subtype → instance),
   commissionable `CommissionableMdnsAdvertisement.ts:37-62` (`_L`, `_S`,
   `_CM`, `_T`, `_V` pairs; `_V` only when not privacy-masked),
   commissioner `CommissionerMdnsAdvertisement.ts:31-52`. TTL 120 s on every
   record (`packages/general/src/codec/DnsCodec.ts:20`; TXT too — matter.js
   before chip's 4500 s, `src/lib/dnssd/wire/records/Txt.h:30` at
   6170af84, concept §D.2). Cache-flush bit on unique types (SRV, TXT, A,
   AAAA; `MdnsAdvertisement.ts:182`, `DnsCodec.ts:198-208` `isUnique`), not
   on PTR. The Service's own `Subtypes`, `TXT`, `Addresses`, `HostName` are
   the inputs (`service.go:129-154`); the address order follows
   `MdnsAdvertisement.ts:167-170`.
2. **Answer engine** — `answer.go`, mirroring `MdnsServer.ts:103-203`
   `#handleMessage`: fast-reject of names not owned (`:120-122`), per-record
   query (`#queryRecords` `:377-388`: case-insensitive name, ANY or exact
   type), additionals (SRV+TXT for a PTR answer, A/AAAA for SRV; none for an
   A/AAAA-only query, `:119-124`), known-answer suppression
   (`#suppressedByKnownAnswer` `:366-375`: same name/type/class, equal value,
   known TTL > record TTL/2), the unicast decision (`:153-168`: unicast only
   if every question carries QU and every answer last went multicast ≤ TTL/4
   ago), the multicast branch's 900 ms drop and the 999 ms duplicate-question
   window (`QUESTION_SUPPRESSION_WINDOW` `:35`, `#shouldSuppressResponse`
   `:394-440`), truncated-query merging (`#processTruncatedQuery`
   `:446-461`, 400–500 ms wait `:496-502`), packet split at 1232 bytes
   (`DnsCodec.ts:26`, `MdnsSocket.ts:155`, `:187`: answers first, additionals
   only in the last packet), the cache-flush bit cleared on unicast answers
   and on incomplete sets (`sendable` `:534-552`), per-interface answering
   (`:107-111`, `:205-214`).
3. **Announce and goodbye** — `announce.go`: Publish announces the whole
   set in the answer section on every multicast interface
   (`#announceRecordsForInterface` `:205-214`, `broadcast` `:216-241`);
   Withdraw/Close send the same set with TTL 0 (`expireAnnouncements`
   `:243-275`; grandcat's goodbye `server.go:593-600` is retired with it).
   No probing (matter.js and chip probe not; concept §D.3). The broadcast
   schedule is `MdnsAdvertiser.DefaultBroadcastSchedule`
   (`MdnsAdvertiser.ts:213-232`: 1 s, factor 2, max 90 s, jitter 0.25,
   until the commissioning timeout; operational stops at the first
   connection, `broadcastAfterConnection: Instant`) and
   `RetransmissionBroadcastSchedule` (`:233-246`: one broadcast per MRP
   retransmission event); the schedule runs as `MdnsAdvertisement.run`
   (`:64-78`) with coalescing (`:85-95`). Drive it by the node's clock
   (`clock` package, as `SubtypeResponder` uses `clock.AfterFunc`), so the
   tests use the fake clock.
4. **Node** — `node/config.go:250-272` `Network.Reannounce` and
   `:295-299` `DefaultReannounceInterval` go (owner decision §E.3; the
   validation at `:594` too); `node/reannounce.go` shrinks to
   `Node.TriggerReannounce` (on-demand broadcast of every published
   service); `node/mdns.go:48` builds `mdns.NewResponder()`. The bridge's
   session-triggered re-announce (`bridge/securechannel.go:1292-1333`,
   anonymous `TriggerReannounce` interface at `:1332`) keeps working — map it
   onto the retransmission/reconnect events the way matter.js's
   `BroadcastEvent` (`Advertiser.ts:93`: startup, reconnect, retransmit)
   does, and say in the report which event each caller is. Other
   `NewZeroconf` callers to rename: `controller/advertise.go:45`,
   `controller/ota.go:206` (+ comments `:71`, `controller/controller.go:179`),
   `examples/reference-controller/cli/self.go:53`,
   `internal/chiptool/gocontroller_test.go:226`, the live tests
   `client/peer/node_live_test.go:28`, `client/discovery/scanner_live_test.go:35`,
   `controller/commissioner/node_live_test.go:28`.
5. **fabrictest**: `fabrictest/mdns.go:24-30` (`ttlService` 4500 → 120 and
   its comment; the controller's own advertisement is 120 s now too).

## Tests

- Port from matter.js (`read-mdns-tests.md` §1.1–1.2 names every case with
  its line; `register.json:4225-4278` and `:4259-4275` list what is
  `missing`): `MdnsServerTest.ts` groups "standard queries" (:81), "Cache-
  flush bit" (:293), "Answer suppression" (:461), "Handle duplicate
  messages" (:601), "unicast (if allowed)" (:934), "splits response" (:1050),
  "Duplicate question suppression" (:1204), "Unknown-name query rejection"
  (:1303), "Close" (:1624); `MdnsTest.ts` "broadcasts the device fabric on
  one port and expires" (:278), "commissionable" (:414), "commissioner"
  (:546, :637), "multiple devices on different ports" (:714), "announcement
  overflow" (:996). Port by meaning into `mdns/*_parity_matterjs_test.go`,
  through a fake socket and the fake clock (no real multicast, no sleep).
  The "Host name" group (:1670) is NOT ported: the hostname rule stays
  go-fabric's (`by_design` L7-D03). Update `script/testparity/register.json`
  (status, `go` lists, `missing`), then `make testparity-check` and the
  ported/missing ratchets in `script/testparity` (W6-test-07) in the same
  commit.
- Existing tests: `read-mdns-tests.md` §3 marks the grandcat-bound ones
  [G] — rewrite them against `Responder` where they pin contract
  (`zeroconf_test.go`, `zeroconf_internal_test.go` publish/withdraw/
  republish/subtype cases, `parity_matterjs_test.go`), retire the ones that
  test grandcat itself (`zeroconf_smoke_test.go`,
  `subtype_responder_manual_test.go`, `reannounce_test.go`'s loop case,
  `TestZeroconf_HandsHostNameToResponder`). Keep the socket-level live tests
  (`subtype_responder_live_test.go`, `host_responder_live_test.go`) on the
  responder's sockets. Extend `TestSubtypePTRTTLParity` to one case per
  record type (PTR, SRV, TXT, A, AAAA, subtype PTR: 120 s; goodbye 0).
- Wire-level smoke: one test that publishes an operational service and
  reads PTR → SRV/TXT → A/AAAA back over the real multicast socket (what
  `zeroconf_smoke_test.go` did through grandcat's browser; use the module's
  own `OpenQueryConn`), `-short`-skipped like the live tests.
- `make race ./mdns/...` runs — the skip file is gone.

## Registers and docs

- `notes/parity/by_design.md`: L7-D02 (`:1066-1082`) retired into the
  "Retired entries" table (the W6 convention); L7-D03 untouched.
- `docs/getting-started/device.md:168`, `:205` (the `Network.Reannounce`
  row), `docs/matterjs-comparison.md:53`, `docs/guides/advanced-seams.md:180`,
  `mdns/doc.go`, the `MDNS.HostName` doc `node/mdns.go:19`;
  `docs/guides/migrating-to-the-facade.md` rows: `NewZeroconf` →
  `NewResponder`; `Network.Reannounce` / `mdns.Reannouncer` /
  `StartReannounceLoop` removed (`TriggerReannounce` stays).
- CHANGELOG `[Unreleased]`: Changed (every record 120 s; the constructor;
  the loop) with the migration lines, Removed (grandcat/zeroconf,
  backoff, pkg/errors), Fixed (the mdns race skip). Reference #362.
- `script/coverfloor/floors.go`: re-measure `mdns` (and any package whose
  tests moved); `script/archcheck/layers.json` if a new file needs a layer.

## Guards (all, with HEAD in the summary log)

`make ci`; `CGO_ENABLED=1 go test -race -count=1 ./mdns/... ./node/...
./bridge/... ./controller/... ./fabrictest/...`; `make cite-check`,
`make reachability-check`, `make apidiff-check` (must name `NewZeroconf`,
`Zeroconf`, `Reannouncer`, `StartReannounceLoop`, `Network.Reannounce`),
`make testparity-check`, `make ahead-check`; `go mod tidy -diff` clean,
`go mod verify`; `make chiptool-test` and
`GOFABRIC_CHIP_FAMILIES=<the commissioning group's families — read
internal/chiptool/families_table_test.go for the group> make chiptool-families`
— the harness is single-user: under the flock, and only when no other
agent holds it. `mkdocs build --strict` for the docs.

## Rules

- `git var GIT_AUTHOR_IDENT` must print `SukramJ <sukramj@icloud.com>`
  before every commit; no Co-authored-by or other trailers.
- `export PATH=$HOME/go/bin:$PATH`; guard bundles under
  `flock ~/.cache/go-fabric-scratch/guards.lock` in the background, logs
  `~/.cache/go-fabric-scratch/i362/` with a summary log; read logs by grep.
- Read slices, not files; cite matter.js paths with line numbers and
  `at 862ad75f` where the comment is a `Mirrors` line; grandcat is never a
  citation target in new code (it is gone).
- Do not touch `secure/`, `commissioning/`, `cluster/core`, ACL.
- Commit in small steps; push; open the PR without auto-merge; report in
  the fixed shape (per deliverable: commit, test; guards with HEAD and
  exit; differences from the brief; decisions; left out; open questions;
  by_design candidates) and list every matter.js/chip citation you wrote.
