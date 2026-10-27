# Report BP2 — ESPHome proxies over mDNS, keys per device, the nested module's version, a BLE side for `fabrictest`

Brief: `notes/briefs/2026-10-27-BP2-proxy-discovery.md`. Branch
`bp2/proxy-discovery`, rebased onto the release commit of v0.13.7
(`c401a2b4`). Sources read in the clones, never run: aioesphomeapi at
`3b2a4a2d` (`host_resolver.py:29`, `discover.py:55-94`,
`tests/test_discover.py:144`), ESPHome at `3b1bf1d` (read only, nothing
copied; `components/mdns/mdns_component.cpp:128-262`, `mdns_esp32.cpp:44-46`).
matter.js only through `git show 862ad75f:` (`MdnsService.ts:27, :90-94`,
`CommissionableMdnsScanner.ts:100-105`, `DnssdNames.ts`).

## Per §

### §1 Discovery

- **The browse** (`client/discovery`, main module): the cache admits only
  Matter's service types (`isMatterServiceRecord`), so `Names.Browse(ctx,
  serviceType) ([]Instance, error)` is new: an ingress filter for the type
  while it runs, the type's PTR on the solicitor's schedule, SRV and TXT
  asked for when an instance lacks them, A and AAAA once per SRV target, a
  `Service` view per instance; it returns at ctx's end the instances still
  advertised (PTR present) with an address, ordered by QName. Instances
  cached by an earlier browse count at once. `Instance{Name, QName,
  Addresses, Parameters}`. Tests on the fake connection (`browse_test.go`):
  a foreign type found and an unrelated service kept out of the cache; the
  piecewise case (PTR only → SRV/TXT asked → A/AAAA asked → link-local AAAA
  zoned); cached instances in a second browse, an unresolved instance left
  out, a PTR goodbye withdrawing after the eviction delay; refusals (empty
  type, closed cache).
- **TXT** (ESPHome `mdns_component.cpp:133-242`): `version`, `mac` (twelve
  lower-case hex digits, `test_discover.py:144`), `config_hash`, `board`,
  `platform`, `network`, `friendly_name` when set, `api_encryption` with a
  PSK / `api_encryption_supported` without, `api_provisioning`,
  `ota_signed`, `project_*`, `package_import_url`. The instance name is the
  node name (`mdns_esp32.cpp:46`). Proxy-ness is not in the record, as the
  brief says.
- **`ble/esphome`**: `ServiceType`, `Browser` (`*discovery.Names` is one),
  `Keys` (by name, case-insensitive, then by MAC in any spelling),
  `ParseKey("name=key")` (checks base64 of 32 bytes), `Discover(ctx,
  browser, keys, timeout) ([]Proxy, error)` (connects once to each
  candidate, keeps ACTIVE_CONNECTIONS + REMOTE_CACHING, logs
  `ble.esphome.proxy_skipped` with the reason), `DiscoverOptions{Browser,
  Keys, Rescan, Timeout}`, `Options.Discover`, `ErrNoBrowser`. A device that
  advertises `api_encryption` without a key is skipped without a dial.
- **The central**: the proxy set is dynamic (`setMu`, each proxy with its
  own context and a list of addresses tried in order). `NewCentral` keeps
  BP1's rules for the given proxies, then browses once; it fails when no
  proxy, given or discovered, answers. A loop browses every `Rescan`
  (5 min) and on every proxy loss: new candidates are dialled at once and
  join (`proxy_discovered`); a known name with a changed address set takes
  it and cuts its backoff short (`proxy_moved`); a discovered proxy not
  advertised and idle (no API connection, no GATT connection) leaves
  (`proxy_removed`); a device advertised at a given proxy's address is that
  proxy; a device refused for good is dialled again only when its
  `config_hash` changes. `StartScanning` on an empty discovered set
  triggers a browse instead of `ErrProxyBusy`.
- **Tests** (`discover_test.go`, real clock): `TestDiscover` (plaintext,
  key by name, key by MAC with dashes, passive proxy skipped with
  `ErrNoActiveConnections`, keyed device without key skipped with
  `ErrRequiresEncryption`, wrong key skipped with `ErrInvalidKey`, probes
  closed), `TestDiscoverRefusals`, `TestParseKey`, `TestCentralDiscovery`
  (static and discovered merge — one client per proxy; scan through both;
  `Move` to a new port followed after the loss; a withdrawn, closed proxy
  dropped; a given proxy stays while not advertised). The three BP1
  commissioner tests run in both modes (`static`, `discovered`).
- `esphometest.Network` (in-memory DNS-SD answering PTR/SRV/TXT/A with the
  firmware's TXT keys, `Withdraw` with a goodbye), `Proxy.Move`, `Port`,
  `Clients`.

### §2 The nested module's version

`ble/esphome/go.mod` requires go-fabric `v0.13.7` (was `v0.13.3`), tidied;
`github.com/miekg/dns` is now a direct require of the nested module (the
fake network; it was indirect already). **At the next release the nested
module is tagged `ble/esphome/v0.13.8` at the release commit — and its
require must move to `v0.13.8` there**: this wave adds
`discovery.Names.Browse`, which v0.13.7 does not have (see Questions).
Dependabot: `.github/dependabot.yml` lists only `/` and Actions, not
`ble/bluez`, so by the brief's condition nothing was added.

### §3 `fabrictest.Device.BLE()`

`fabrictest/ble.go`: a `*bletest.Peripheral` with the device's
discriminator, vendor and product id, address `C0:FF:EE:00:xx:yy` by the
device index, serving BTP through `bridgetest.ServeConn(d.Bridge(), conn)`
(the bridge read per session, so a restart is followed). `fabrictest` keeps
no `ble/esphome` import (the shape the brief asked for). Moved onto it:
`controller/ble_test.go`'s `TestPairOverBLE` and `ble/esphome`'s
`TestPairOverBLE`; `fabrictest/ble_test.go` pairs a site's device over
`bletest.NewCentral(dev.BLE())`. The reference controller's CLI test keeps
its hand-built peripheral (its harness is no `fabrictest` site). Guide:
`docs/guides/testing-in-process.md` §BLE.

### §4 Docs, CLI, release

- `pair --bluetooth-proxy-discover` and `--bluetooth-proxy-key name=key`
  (repeatable; a key without discovery is refused); both imply `--ble`. The
  hook is `cli.Options.Proxies func(cli.ProxyFlags, *slog.Logger)`;
  `ble/esphome/cmd/reference-controller` opens a `discovery.Open` cache for
  discovery and closes it with the central. `TestPairBluetoothProxyFlag`
  extended.
- `docs/guides/ble-proxies.md` §Discovery and keys per device (the
  one-subscriber rule unchanged), `docs/matterjs-comparison.md` row,
  reference controller README, `ble/esphome` package doc §Discovery,
  CHANGELOG `[Unreleased]` Added (four entries).

## Guards

GUARDS

## Differences from the brief

1. **`Discover` logs on `slog.Default`**: the brief's signature has no
   logger; the central's discovery logs on `Options.Logger`.
2. **`DiscoverOptions.Timeout`** (the browse window, 3 s) beside `Keys,
   Browser, Rescan`: a browse needs an end.
3. **`cli.Options.Proxies` takes `cli.ProxyFlags`** instead of the spec
   list: one hook for both flags. `examples/` is outside apidiff; the
   CHANGELOG line names it.
4. **Removal is conservative**: a discovered proxy leaves only when it is
   not advertised *and* idle; one still connected stays until a browse
   misses it while it is down.
5. **A permanently refused device is remembered by `config_hash`** so a
   site's other ESPHome devices are not dialled every five minutes.
6. **Dependabot unchanged** (the config does not list `ble/bluez`).

## Decisions

- The browse sits in `client/discovery` on the shared cache (no second
  mDNS stack), widening it for one type the way the commissionable scanner
  does; the Matter filter is untouched.
- Addresses per proxy are a list tried in order, so a stale cached SRV
  beside the new one costs one refused dial, not a stuck proxy.
- `fabrictest.Device.BLE()` returns a `bletest.Peripheral`, not a central:
  the consumer chooses the central (bletest, esphometest).

## Left out

- No by_design text written (Fable's); proposed below.
- No live run against a real proxy (none on the bench); the manual test
  plan line: `pair --bluetooth-proxy-discover --bluetooth-proxy-key
  <name>=<key>` on a LAN with one active proxy and one plain ESPHome
  device — the plain device logged as skipped.
- chip-tool: skipped (no device-side change).

## Questions

1. The nested module now needs go-fabric's `discovery.Names.Browse`, which
   v0.13.7 lacks: the require `v0.13.7` builds in-repo (replace) but not
   for a consumer resolving the tag. Bump the require to `v0.13.8` in the
   release commit before tagging `ble/esphome/v0.13.8`? Recommendation:
   yes, the launcher bumps it with the tag.
2. `NewCentral` with discovery fails with `ErrNoProxies` when nothing
   answers at start. A daemon (go-fabric-server S6) may prefer a central
   that starts empty and fills on the rescans. Recommendation: keep the
   failure; S6 opens the central per pairing, as the CLI does.
3. Dependabot for `ble/bluez` and `ble/esphome` (BP1 Q3 again)?
   Recommendation: add both in a CI-only change.

## by_design candidate

Append to **BD-Controller-BleOverEsphomeProxies**:

> Discovery (brief BP2): the proxies are found over DNS-SD as
> `_esphomelib._tcp.local` (ESPHome `mdns_component.cpp:140, :146-150` at
> 3b1bf1d; aioesphomeapi `host_resolver.py:29` at 3b2a4a2d), browsed on
> the commissioner's cache through `discovery.Names.Browse`, which widens
> matter.js's Matter-only ingress filter (`MdnsService.ts:27, :90-94` at
> 862ad75f) for one foreign type as `CommissionableMdnsScanner.ts:100-105`
> does for its own; a device counts as a proxy only after connecting, by
> its feature flags. Reasoning: a site's proxies change with DHCP and
> deployments; neither reference has a proxy to discover.
