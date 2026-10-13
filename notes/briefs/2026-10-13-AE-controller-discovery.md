# Brief AE — `controller/discovery`: DNS-SD browsing and service resolution for the controller (ADR 0020, stage 1, part 1)

Owner: Fable. Implementer: Opus. Branch `wave12/discovery` from
`planning/briefs-wave-12`, pushed, **no PR**. Report in
`notes/briefs/reports/AE.md`. Read first: `notes/briefs/README.md`,
[ADR 0020](../../docs/adr/0020-commissioner-and-controller-role.md) (the
boundary: `controller/` is a sibling of `bridge/`; `bridge/`, `node/`,
`device/`, `endpoint/` are not changed; `contract/` stays stdlib-only),
concept v2 §B.1 (the stage table). matter.js at **`0ceb202d`** (the
checkout `../matter.js`); paths below are relative to
`packages/protocol/src` (P/) and `packages/general/src/net` (G/). Read the
lines a fact cites before you mirror them; where a line range is wrong,
say so in the report and cite what you read. One commit per item,
reachability in each, CHANGELOG under `[Unreleased]` → Added. Guards:
`make ci`, `make race`, `make reachability-check`; no harness (brief AF has
it). Brief AF (`wave12/handshake`) builds `controller/exchange` and
`controller/handshake` and does not touch `mdns/` or
`controller/discovery`; you do not touch `controller/exchange`,
`controller/handshake`, `secure/`, `transport/`, `bridge/`.

## What exists

- `mdns/` is an advertiser plus one resolver: `mdns.OperationalResolver`
  (`mdns/operational_resolver.go:61-140`) resolves one operational instance
  by SRV/TXT/A/AAAA queries on a multicast query connection
  (`openMulticastQueryConn` `:288`, `resolution.query/absorb/addresses`
  `:169-260`, backoff `resolveInitialInterval`/`resolveBackoffFactor`/
  `resolveMaxInterval`, link-local zones kept per address). There is no
  browser for `_matterc._udp` and no record cache with TTLs.
- The device side emits the TXT keys the controller parses
  (`mdns/service.go`); its parity tests hold the key format.
- `bridge/bridgetest.Commissioner` resolves the device it dials through
  `Options.Advertiser` (`commissioner.go:241`) — a test shortcut, not a
  browser.

## The port (matter.js shape → Go)

matter.js splits discovery into a shared record cache (`DnssdNames` /
`DnssdName`, G/dns-sd), a live per-name view (`IpService`, G/dns-sd), a
query loop with a retry schedule (`DnssdSolicitor`, G/dns-sd), the TXT
parser and device types (`Scanner.ts`, P/common) and the commissionable
scanner (`CommissionableMdnsScanner.ts`, P/mdns). Operational instances
have no scanner class: each peer owns an `IpService` for its
`<CFID>-<NODEID>._matter._tcp.local` name (`P/peer/Peer.ts:108-117`). Port
that shape as package `controller/discovery` with these types (Go names
are yours; keep the matter.js name in the doc comment):

1. **Record cache** (`Names`, `Name`): records keyed as matter.js keys them
   (`G/dns-sd/DnssdName.ts:352-377`: A/AAAA/PTR by type+value, SRV by
   type+target:port, TXT by type+sorted entries); TTL with grace factor
   1.05 (`DnssdName.ts:25`, `expiresAt = installedAt + ttl·1.05` `:156`);
   a record with ttl < 15 s is raised to 15 s and ttl 0 is a goodbye that
   takes effect after the 1 s eviction delay
   (`G/dns-sd/DnssdNames.ts:192-211`, defaults `:549-552`); a cache-flush
   record retires older unique records of its type after the eviction
   delay (`DnssdName.ts:148-150`, `:216-236`); an SRV whose target is `""`
   or `"."` or whose port is 0 or > 65535 is ignored (`DnssdName.ts:344-350`);
   TXT parameters: newest TXT first, entries split at the first `=`, empty
   entry or empty key skipped, first occurrence of a key wins, a key
   without `=` has an empty value (`DnssdName.ts:76-112`). Only records
   whose name ends with `._matter._tcp.local`, `._matterc._udp.local` or
   `._matterd._udp.local` enter the cache (`P/mdns/MdnsService.ts:25-27`,
   `:90-95`); A/AAAA are accepted only from a packet that carried a
   relevant record (`DnssdNames.ts:187-211`, staged IPs pruned every
   minute `:113-116`).
2. **Service view** (`Service`, matter.js `IpService`, G/dns-sd/IpService.ts):
   one DNS-SD name; SRV entries keyed target:port (`:249-294`); an A gives
   its value, an AAAA that is link-local (`fe80::/10`) without a `%` gets
   `%<receive interface>` appended (`:392-408`); the address set is
   `{ip, port}` (`:316`); a new address marks the service reachable
   (`:315-328`); change notification is coalesced (`:342-355`); a TXT
   change recomputes the parameter signature and notifies only when it
   differs (`:202-247`); `addressChanges` yields deletes first, then adds
   in desirability order (`:137-200`). Desirability
   (`G/net/ServerAddressSet.ts`, `compareDesirability` — grep it): selection
   preference first (`G/net/ServerAddress.ts:170-176`: IPv6 link-local,
   then ULA `fc00::/7` (`/^f[cd]/i`), then global IPv6, then IPv4;
   classification `:195-209`), then health (unhealthy later, newest
   healthy first), then SRV priority.
3. **Solicitor** (`Solicitor`, G/dns-sd/DnssdSolicitor.ts): `solicit`
   sends one query (several solicitations in one tick coalesce into one
   packet, known answers = the name's current records with cache-flush
   cleared, `:212-276`); `discover` repeats on a retry schedule until
   aborted, the first call per name owns the loop and later callers only
   join (`:23-42`); the first send is immediate (`:189-193`); the default
   schedule is initial 1 s, jitter 0.2, backoff ×2, maximum 1 h
   (`:94-99`; `G/RetrySchedule.ts:290-337`: jitter added, then clamped to
   the maximum). Build it on the multicast query connection of
   `mdns/operational_resolver.go` — export what you need from `mdns/` as a
   small public type (`mdns.QueryConn`: send a query, receive responses
   with the receiving interface index) with a doc comment; do not change
   `OperationalResolver`'s behaviour.
4. **TXT parsing** (`DiscoveryData`, P/common/Scanner.ts:75-191):
   `parseTxtDecimal` accepts `^(0|[1-9]\d*)$` up to a maximum, else
   absent (`:199-205`); `DN`, `RI`, `PI` verbatim (`:124-128`); `VP` split
   at `+`, vendor ≤ 65535 else VP dropped, product parsed independently
   and dropped alone when invalid (`:132-138`); `DT` ≤ 2^32−1 (`:143`);
   `PH` ≤ 2^32−1 and 0 rejected (`:150-157`); `ICD` ∈ {0,1} (`:160`); `T`
   ≤ 65535 decoded as the bitmap tcpClient = bit 1, tcpServer = bit 2,
   bit 0 reserved (`:167-172`, `P/common/SupportedTransportsBitmap.ts:9-23`);
   `SII`/`SAI` ≤ 3 600 000 ms, `SAT` 1..65535, an invalid value is
   absent so the MRP defaults apply (`:176-184`). `CM` is parsed only by
   the scanner (`CommissionableMdnsScanner.ts:472-473`: D ≤ 0xFFF and
   CM ≤ 255 both required, else the device is not built). `JF` does not
   exist in matter.js. Serve the same type for operational TXT (Peer.ts
   parses it with `DiscoveryData` too, `:134`).
5. **Commissionable scanner** (`Scanner`, P/mdns/CommissionableMdnsScanner.ts):
   ingress filter on `._matterc._udp.local` / `._matterd._udp.local`
   (`:95-105`); query names per identifier (`:574-593`): always
   `_matterc._udp.local` plus `<instance>._matterc._udp.local`,
   `_L<n>._sub…`, `_S<n>._sub…`, `_V<vendor>._sub…`, `_T<n>._sub…`, or
   `_CM._sub…` for product-only and "any"; discover with the retry cap
   30 s (`:73`, `:454-467`); PTR targets missing SRV/TXT are solicited
   speculatively, at most 50 at a time, forgotten after 1 h, pruned every
   30 min (`:69-71`, `:314-335`); an instance name is cached once it has a
   valid D and CM (`:470-496`; T defaults to none, ICD to 0 `:485-486`)
   and delivered to waiters only once it has an address (`:443-452`),
   addresses and hostname re-read on every delivery (`:525-531`; hostname
   = the single distinct SRV target's first label, else absent
   `:508-516`); identifier match (`:533-561`): instance id; long
   discriminator = D; short = `(D >> 8) & 0x0F`; vendor (+product) from
   VP; device type = DT; product-only from VP; `{}` = CM is 1 or 2.
   API: `FindCommissionable(ctx, Identifier, func(Device)) ([]Device, error)`
   (continuous until ctx ends — `findCommissionableDevicesContinuously`
   `:165-256`: cached matches first, then the network), `Discovered(Identifier)`
   (cache only `:258`), `Cancel(Identifier)` (`:264`), `Close`.
6. **Operational service**: `Names.Service(compressedFabricID, nodeID)`
   with the name `getOperationalDeviceQname` (`P/mdns/MdnsConsts.ts:36`,
   both ids as upper-case hex), and a `Resolve(ctx)` that runs matter.js's
   `IpServiceResolution` (`G/dns-sd/IpServiceResolution.ts:434-538`): SRV+TXT
   query for the instance with the SRV targets as associated names, A/AAAA
   per target that has none, on the default schedule, until an address
   not known before appears or ctx ends. `mdns.OperationalResolver` stays
   for the bridge (ADR 0008); the controller's peers (brief AG, wave 13)
   use this service.

Not in this brief: `PeerAddressMonitor` (probing needs sessions — wave
13), `NetworkProfile`, BLE, the `_matterd` commissioner-side records
beyond letting them into the cache.

## Guards and tests

- Unit tests per type with record fixtures; a parity test file
  `controller/discovery/parity_matterjs_test.go` that pins: the record
  keys, the TTL rules (15 s floor, grace, goodbye after 1 s), the TXT
  rules of §4 as a table (one row per key and edge: `D=0abc` rejected,
  `SAT=0` absent, `SII=3600001` absent, `VP=65536+1` dropped, `VP=1+x` →
  `"1"`, `T=6` → tcpClient+tcpServer), the identifier matches, the query
  names, and the address order (link-local before ULA before global before
  IPv4). Cite the matter.js lines in each case.
- A live test (`*_live_test.go`, the pattern of `mdns/operational_resolver_live_test.go`)
  that advertises a commissionable record with `mdns.Advertiser` on the
  loopback multicast and finds it by long discriminator, by short
  discriminator and by `{}`; skip when multicast is unavailable, as the
  existing live tests do.
- Coverage floor for the new package in `script/coverfloor/floors.go`;
  reachability regenerated.

## Report

`notes/briefs/reports/AE.md`: the type map (matter.js class → Go type),
what was left out, the lines that differed from the brief, open
questions. The owner's decisions follow in the report file.
