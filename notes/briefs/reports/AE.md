# Report AE — `controller/discovery`: DNS-SD browsing and service resolution (ADR 0020, stage 1, part 1)

Branch `wave12/discovery` from `planning/briefs-wave-12` (1a109ed), pushed,
no PR. matter.js read at `0ceb202d` (the `../matter.js` checkout); every
`// Mirrors matter.js …` comment added cites `0ceb202d` lines that were read.
Each item commit carries its regenerated reachability inventory (unreached
stays 5) and a CHANGELOG entry under `[Unreleased]` → Added.
`notes/parity/by_design.md`, the findings register, `bridge/`, `node/`,
`secure/`, `transport/`, `controller/exchange` and `controller/handshake`
are untouched. The package imports `mdns` and the standard library only
(its tests also import `github.com/miekg/dns` to build response packets).

| Commit | Item |
| --- | --- |
| e0d9eb8 `feat(discovery): record cache Names/Name for the controller's DNS-SD browser` | §1 |
| 3170118 `feat(discovery): Service, the live view of one DNS-SD service (matter.js IpService)` | §2 |
| 1816b04 `feat(discovery): Solicitor with the DNS-SD retry schedule (matter.js DnssdSolicitor)` | §3 |
| ff69c45 `feat(discovery): TXTData, the TXT parser of matter.js DiscoveryData` | §4 |
| 4ce7b30 `feat(discovery): Scanner for commissionable nodes (matter.js CommissionableMdnsScanner)` | §5 |
| f4e8e8b `feat(discovery): operational service and Resolve (matter.js IpServiceResolution)` | §6 |
| c2c50de `test(discovery): live scanner test on the host's multicast, coverage floor 95` | guards |
| (this commit) `notes: report AE` | — |

## Type map

| matter.js `0ceb202d` | Go |
| --- | --- |
| `MdnsSocket` (socket half: send to the groups, receive with source interface) — G/dns-sd/MdnsSocket.ts | `mdns.QueryConn`, `mdns.OpenQueryConn`, `mdns.QueryResponse` (the former unexported `queryConn` / `received` of `operational_resolver.go`, renamed and exported; `OperationalResolver` unchanged) |
| `DnsCodec` records (decoded shape) — G/../codec/DnsCodec.ts | `mdns.DNSRecord`, `mdns.DNSSRV`, `mdns.DNSQuestion`, `mdns.DNSMessage`, `mdns.DNSType`, `mdns.ParseDNSMessage`, `mdns.PackDNSQuery` (so `controller/discovery` needs no `miekg/dns`) |
| `DnssdNames` — G/dns-sd/DnssdNames.ts | `discovery.Names` (`NewNames`, `Open`, `Get`, `Has`, `DiscoveredNames`, `Close`, `Solicitor`, `Service`) |
| `DnssdName`, `DnssdName.Record`, `DnssdName.Changes` — G/dns-sd/DnssdName.ts | `discovery.Name`, `discovery.Record`, `discovery.Changes` |
| `DnssdParameters` — G/dns-sd/DnssdParameters.ts | `discovery.Parameters` |
| `DEFAULT_TTL_GRACE_FACTOR`, `DnssdNames.defaults` | `DefaultTTLGraceFactor`, `DefaultEvictionDelay`, `DefaultMinTTL` |
| `IpService` — G/dns-sd/IpService.ts | `discovery.Service` (`NewService`, `Addresses`, `Parameters`, `HasTXTRecord`, `Reachable`, `AddressChanges`, `TXTData`, `Close`) |
| `IpServiceStatus.isReachable` (only the flag) | `Service.Reachable` |
| `IpServiceResolution` — G/dns-sd/IpServiceResolution.ts | `Service.Resolve` |
| `ServerAddressIp`, `ServerAddress.selectionPreferenceOfIp`, `ServerAddressSet.compareDesirability` / `compareHealth` | `discovery.Address`, `SelectionPreference` (+ `Preference*` constants), `CompareDesirability` |
| `IpService.AddressChange` | `discovery.AddressChange`, `AddressChangeKind` |
| `DnssdSolicitor` / `QueryMulticaster`, `Solicitation`, `Discovery`, `DefaultRetries` — G/dns-sd/DnssdSolicitor.ts | `discovery.Solicitor`, `Solicitation`, `Discovery`, `DefaultRetries` |
| `RetrySchedule`, `RetrySchedule.Configuration` — G/RetrySchedule.ts | `discovery.Retries` (+ unexported `retrySchedule`) |
| `DiscoveryData`, `parseTxtDecimal` — P/common/Scanner.ts | `discovery.TXTData`, `ParseTXTData`, `ParseTxtDecimal` (renamed: `discovery.DiscoveryData` trips revive's stutter rule) |
| `SupportedTransportsBitmap` — P/common/SupportedTransportsBitmap.ts | `discovery.SupportedTransports` |
| `CommissionableDevice` | `discovery.Device` |
| `CommissionableDeviceIdentifiers` | `discovery.Identifier` (`ByInstanceID`, `ByLongDiscriminator`, `ByShortDiscriminator`, `ByVendor`, `ByVendorProduct`, `ByDeviceType`, `ByProduct`, `AnyCommissionable`) |
| `CommissionableMdnsScanner` — P/mdns/CommissionableMdnsScanner.ts | `discovery.Scanner` (`NewScanner`, `FindCommissionable`, `Discovered`, `Cancel`, `Close`), `CommissioningRetryInterval` |
| `getOperationalDeviceQname` — P/mdns/MdnsConsts.ts:36-37 | `discovery.OperationalQName`, `Names.Service(cfid, nodeID)` |

The event loop: matter.js runs all of this on one JavaScript loop. Each
`Names` owns one goroutine holding all cache, service, solicitor and scanner
state (`loop.go`); exported methods hand work to it and wait until the task
and everything it deferred has run. matter.js microtasks (record-change
notification, deletion of an unused name) run right after the task;
macrotasks (`Time.sleep(0)`: query coalescing, service-change coalescing)
run after that. User callbacks (`FindCommissionable`'s `found`) run on the
caller's goroutine, never on the loop. Iteration order of JavaScript `Map`s
is kept with a small insertion-ordered map (`omap.go`) where matter.js
depends on it.

## Guards

- `make ci`: green — build, vet, `go test -count=1 ./...` (172 packages
  `ok`), gofumpt + golangci-lint (0 issues), `go mod tidy -diff`,
  `go mod verify`, cover-check (174 packages at or above their floor;
  `controller/discovery` 95.5 % against the new floor 95, `mdns` 92.8 %
  against 91).
- `make race`: green — 172 packages `ok`, no race reported (the
  discovery tests run under the detector; the live test is excluded, see
  below).
- `make reachability-check`: green — 10 566 exported, 7 430 reached,
  3 131 whitelisted, unreached stays **5** (the ratchet), none in
  `controller/discovery` or `mdns`.
- Parity: `controller/discovery/parity_matterjs_test.go` — record keys,
  TTL rules (15 s floor, grace 1.05, goodbye after 1 s, a goodbye never
  extends), cache-flush supersession, unavailable SRV, TXT parameters, the
  ingress filters, address order and the address shape, the retry
  schedule, the TXT parser table (one row per key and edge, including the
  brief's `D=0abc`, `SAT=0`, `SII=3600001`, `VP=65536+1`, `VP=1+x`, `T=6`),
  query names, identifier matches, device building and hostname, the
  operational qname and the resolution's queries. Each case cites its
  matter.js lines.
- Live: `scanner_live_test.go` advertises a commissionable record with
  `mdns.Zeroconf` + `SubtypeResponder` and finds it by long discriminator,
  by short discriminator and with "any" — each on a fresh cache so every
  search goes to the network. It passed on this host (not skipped). It is
  built with `!race`, like the mdns live tests skip under `-race`
  (grandcat/zeroconf's Shutdown/recv race, `mdns/race_skip_test.go`).

## Left out

- `PeerAddressMonitor`, the rest of `IpServiceStatus` (connecting,
  resolving on an unreachable peer), `NetworkProfile`, BLE — as the brief
  says (wave 13).
- The `_matterd` records enter the cache (the scanner's filter admits them)
  but nothing beyond that.
- `MdnsSocket`'s relevance pre-filter (`MdnsRelevanceFilter.ts`, an
  optimisation that drops irrelevant packets before decoding) and its
  query/answer splitting at `MAX_MDNS_MESSAGE_SIZE` = 1232 bytes
  (`MdnsSocket.ts:94-…`, `DnsCodec.ts:26`). A query is sent as one packet
  whatever its known-answer count. See open question 4.
- The `ipv4` parameter of `IpServiceResolution` (always true here).

## Lines that differed from the brief

- `IpServiceResolution.ts:434-538` — the file has 134 lines; the function
  is `:26-134`.
- `G/RetrySchedule.ts:290-337` — the file has 120 lines; the iterator is
  `:28-60` (configuration merge `:114-119`).
- `Scanner.ts` (P/common): the brief's numbers are off by about 45 lines.
  At `0ceb202d`: `DiscoveryData` `:71-145`, `parseTxtDecimal` `:153-159`,
  DN/RI/PI `:78-82`, VP `:84-94`, DT `:96-102`, PH `:104-111`, ICD
  `:113-119`, T `:121-127`, SII/SAI/SAT `:129-140`. The rules themselves are
  as the brief states.
- `ServerAddressSet.compareDesirability` is `:165-184`, `compareHealth`
  `:191-229`. The brief's "then SRV priority" holds, but matter.js prefers
  the **higher** priority value (`b.priority - a.priority`, `:179-181`) —
  the inverse of RFC 2782's "lower is preferred" — and `IpService`'s
  addresses carry no priority (`IpService.ts:316` builds `{ip, port}`), so
  the arm never applies to a discovered address. Ported as written.
- `DnssdNames.ts:195-198`: the 15 s floor applies to every record type,
  although the option's doc comment (`:517-520`) says "for PTR records".
  Ported as the code does it.
- `ServerAddress.ts` link-local test is `isIpv6LinkLocal` `:178-182`
  (`/^fe[89ab]/i`), used by `selectionPreferenceOfIp` `:195-209`. Because
  it is a text test, an IPv4-mapped IPv6 address ranks as IPv6, not IPv4
  (the existing `mdns.SelectionPreference` unmaps it). The discovery
  package has its own `SelectionPreference` that follows matter.js.
- Everything else the brief cites matched what I read.

## Behaviour mirrored that a reviewer may trip over

- `AddressChanges` restart (`IpService.ts:151-199`): a round counts all its
  addresses as known before delivering them, so when the set changes
  mid-delivery the not-yet-delivered additions of the interrupted round are
  not delivered on the restart. Pinned by
  `TestDiffAddresses_RestartKeepsInterruptedRoundKnown`.
- A resolution re-reads the SRV targets only when the service notifies
  (`IpServiceResolution.ts:88-117`), i.e. on an address or TXT change; a
  new SRV whose target has no address yet is picked up when the old
  target's addresses go or the responder includes addresses.
- A cached device's TXT data is built once; only addresses and hostname are
  re-read per delivery (`CommissionableMdnsScanner.ts:525-531`).

## Go-only choices

- `discovery.Retries` uses zero for "unset" (matter.js `undefined`).
- The retry base saturates at the largest `time.Duration` instead of
  wrapping after ~33 doublings; matter.js computes in floating point and
  the 1 h cap clamps it, so the schedule is the same. Found because a
  30-minute fake-clock test looped on a negative interval.
- `FindCommissionable` returns `nil` when ctx ends, `Cancel` cancels it or
  the scanner closes (matter.js resolves the promise in all three), and
  `ErrClosed` when the `Names` closes. `Resolve` returns the addresses, or
  ctx's error, or `ErrClosed`. `Solicitor.Discover` returns nil when ctx
  ends or the schedule runs out.
- `Parameters.Get` decodes invalid UTF-8 one U+FFFD per invalid byte;
  `TextDecoder`'s maximal-subpart rule can differ on multi-byte fragments.

## Open questions

1. **Scanner re-entry (upstream leak, mirrored).**
   `findCommissionableDevicesContinuously` re-runs `#onDiscovered` over
   every discovered name (`CommissionableMdnsScanner.ts:173-175`), and an
   instance not yet cached gets a further `IpService` and pending observer
   each time (`:344-376`). Mirrored and pinned
   (`TestScanner_InvalidTXTNeverBuildsAndNameExpiryDropsTheCache` expects
   two pending observers after one announcement and one search). When a
   valid TXT then arrives, every pending observer calls `#cacheDevice`; the
   later one overwrites the cache entry and the earlier `IpService` and
   name observer are never released, so the instance name stays observed
   (never deleted) for the scanner's lifetime. Keep the mirror, or guard
   it (a `by_design` entry plus a one-line `pending` check)?
2. **Naming.** `DiscoveryData` became `TXTData` / `ParseTXTData` /
   `Service.TXTData` (revive's stutter rule on `discovery.DiscoveryData`).
   Acceptable, or prefer a lint exception and the matter.js name?
3. **Coalescing scope.** Solicitations made in one loop task share a packet
   (as matter.js's same-tick rule). Two separate exported `Solicit` calls
   are two loop tasks and go out as two packets. Internal callers (scanner,
   resolution) coalesce as matter.js does. Fine as is?
4. **Packet size.** `MdnsSocket`'s splitting of oversize queries (1232
   bytes) is not ported; a query with many known answers goes out as one
   (possibly fragmented) packet. A `by_design` entry, or port the splitting
   into `mdns` in a later brief?
5. **`mdns` API surface.** The brief asked for `mdns.QueryConn`; to keep
   `miekg/dns` out of `controller/discovery` I also exported a small typed
   record codec (`DNSRecord`, `ParseDNSMessage`, `PackDNSQuery`, …). If the
   owner prefers `controller/discovery` to import `miekg/dns` directly, the
   codec can move there.
6. **Live test under `-race`.** The live test is excluded from race builds
   by a build tag (zeroconf's upstream race). AF's harness or a later
   brief may want a race-clean advertiser for it.
7. **`by_design` candidates** (not written, per the rules): the omitted
   relevance pre-filter and query splitting (Q4); per-byte U+FFFD decoding;
   `FindCommissionable`'s error contract.
