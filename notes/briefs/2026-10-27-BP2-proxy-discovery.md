# Brief BP2 — ESPHome proxies found over mDNS, keys per device, the nested module's tag, a BLE side for `fabrictest` (go-fabric `ble/esphome`)

Date 2026-10-27. Owner decision 2026-10-27: proxies are discovered over
DNS-SD and keyed per device, not per address. Builds on BP1 (`ble/esphome`,
report `notes/briefs/reports/BP1.md`, ADR 0024 decision 3 as amended) and
answers go-fabric-server report S5's two questions. **Model:** Opus.
Branch `bp2/proxy-discovery` from main. One pull request, one report
`notes/briefs/reports/BP2.md`. go-fabric `CLAUDE.md` rules (sources only
via `git show` at the pins; citations; no attribution lines; English; token
rules). Copy this brief to `notes/briefs/` in the first commit. Disjoint
from the running waves: touch nothing under `cluster/camera`, `thread/`,
`cluster/threadbr`, `cluster/netinfra`, or `controller/softwareupdate`.

## 1 Discovery (`ble/esphome`)

- ESPHome devices advertise `_esphomelib._tcp.local.` with TXT keys
  `version`, `mac`, `config_hash`, `board` and conditional ones (verify in
  `/home/markus/Dokumente/GitHub/aioesphomeapi` — `aioesphomeapi/zeroconf.py`
  or wherever the service type and the TXT keys are read — and in the
  ESPHome `mdns` component of `/home/markus/Dokumente/GitHub/esphome`,
  read-only). Whether a device is a Bluetooth proxy is not in the TXT
  record; it is in `DeviceInfoResponse` /
  `DeviceCapabilitiesResponse`'s `bluetooth_proxy_feature_flags` after
  connecting (BP1's `wire`).
- `esphome.Discover(ctx, browser, keys Keys, timeout) ([]Proxy, error)`:
  browses the service type with go-fabric's own DNS-SD browser
  (`mdns` / `client/discovery` — the one the commissioner uses for
  commissionable nodes; add a generic service-type browse if only Matter
  types are supported, with its own tests on the in-memory network), takes
  the address and port of every instance, resolves the key from `Keys`
  (by instance name or by the TXT `mac`; a device without a key is used
  only when it answers plaintext), connects once, keeps a device with
  ACTIVE_CONNECTIONS (and REMOTE_CACHING as BP1 requires), drops the rest
  with a log line each (`ble.esphome.proxy_skipped`, reason). `Keys` is
  `map[string]string` keyed by name or MAC (`"kitchen-proxy"`,
  `"AA:BB:CC:DD:EE:FF"`); `ParseKey("name=key")`.
- `NewCentral` accepts `Options.Discover *DiscoverOptions{Keys, Browser,
  Rescan time.Duration}`: with it the central starts with the discovered
  set, re-browses on `Rescan` (default 5 min) and on every proxy loss, and
  adds or drops proxies without disturbing a running connection; a proxy
  given explicitly in `proxies` stays regardless. Address changes (DHCP)
  are followed by re-resolution on loss.
- Tests on the in-memory network: an advertised fake proxy is found and
  used for a pairing (the BP1 commissioner tests run once more with
  discovery instead of a static list); a device without
  ACTIVE_CONNECTIONS is skipped; a keyed device needs the key; a renamed
  address is followed after a loss; the static list and discovery merge.
- Log keys: add every new key to `script/slogkeys/slogkeys_test.go`'s
  table and regenerate the guide section.

## 2 The nested module's version (S5 Q1)

`ble/esphome/go.mod` requires go-fabric `v0.13.3`; bump it to the current
release (`v0.13.6` or newer at the time), tidy, and note in the report
that the nested module is tagged `ble/esphome/v0.13.7` (or the release
this wave ships in) at the release commit — the launcher tags it with the
release, as `ble/bluez/v0.12.1` was tagged. Add the nested modules to
Dependabot if the repository's config lists `ble/bluez` (BP1 Q3).

## 3 A BLE side for `fabrictest` (S5 Q2)

go-fabric-server's in-process pairing needed test-only arch exceptions
to build a BTP peripheral from `controller/ble/bletest`,
`bridge/bridgetest.ServeConn` and `transport/btp`. Give `fabrictest` the
missing piece so a consumer needs none of them: `fabrictest.Device.BLE()
*bletest.Peripheral` (or `esphometest.ForDevice(dev)` — pick the shape
that keeps `fabrictest` free of `ble/esphome`; `bletest` is already below
it) that advertises the device's commissionable payload over the
in-memory BLE model and serves BTP through the device's own bridge, so
`Site` pairings over BLE work for any consumer; the BP1 commissioner
tests move onto it where they built the peripheral by hand. Document in
the testing-in-process guide.

## 4 Docs, CLI, release

`docs/guides/ble-proxies.md` (discovery, keys per device, the
one-subscriber rule unchanged), the reference controller's
`--bluetooth-proxy-discover` and `--bluetooth-proxy-key name=key`
(repeatable) next to `--bluetooth-proxy`; CHANGELOG `[Unreleased]` Added
(no break). Guards: `make ci`, `make race` twice, the nested module's own
`go test -race ./...`, `go test ./script/...`, `make reachability` +
`cite-report` (`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`)
with snapshots committed, `make archcheck`, `mkdocs build --strict`; one
bundle at the end (if the memory reaper kills `make`, report it and do not
restart it on your own). Hand back once: PR number, HEAD, guards with
results, differences, Questions verbatim. go-fabric-server wave S6 (the
`--bluetooth-proxy-discover` flag and the add-on option
`bluetooth_proxy_keys`) follows in its own brief.

## Guard serialisation (owner, 2026-10-27)

The host has five cores and other agents run bundles too: run every guard bundle under `flock /home/markus/.cache/go-fabric-scratch/guards.lock` (one bundle at a time on the machine; wait for the lock, do not skip it), e.g. `flock /home/markus/.cache/go-fabric-scratch/guards.lock bash -c "make ci && make race && make race"`.
