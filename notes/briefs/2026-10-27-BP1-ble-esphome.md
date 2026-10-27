# Brief BP1 — `ble/esphome`: Matter BLE commissioning through ESPHome Bluetooth proxies (go-fabric, second `ble.Central` backend)

Date 2026-10-27. Owner decision 2026-10-27: the commissioner shall pair
over the owner's ESPHome Bluetooth proxies (`active: true`), so a host
needs neither a BLE adapter nor D-Bus. Fact map: the launcher's
`read-esphome-bluetooth-proxy.md` (a map; verify every line you build on
in the clones at `/home/markus/Dokumente/GitHub/aioesphomeapi` (commit
3b2a4a2d, MIT) and `/home/markus/Dokumente/GitHub/esphome` (sparse, commit
1cc6c9d1, GPL/MIT mixed — read only, copy nothing from it; the wire format
is the fact, the API proto is MIT via aioesphomeapi)). **Model:** Opus.
Branch `bp1/ble-esphome` from go-fabric main. One pull request, one report
`notes/briefs/reports/BP1.md`. go-fabric `CLAUDE.md` rules (citations,
token rules, no attribution lines, English). Copy this brief to
`notes/briefs/` in the first commit. matter.js has no proxy support of any
kind; connectedhomeip neither: this is an addition behind the port of
ADR 0024, not a parity item — the register entry is a `by_design`
paragraph the owner writes from your report.

## 1 Shape

- New nested module `ble/esphome` (own `go.mod`, like `ble/bluez`; ADR
  0024 decision 3: a platform backend is a separate module). It
  implements `controller/ble.Central` (`controller/ble/central.go:23-36`),
  `Connection` (`:55-77`), and the optional `Reachability` (`:41-43`) from
  `BluetoothConnectionsFreeResponse`. The commissioner, `transport/btp` and
  `controller/ble/channel.go` stay untouched: BTP's needs are in the map §5
  (segment = ATT MTU − 3 clamped to [20, 244]; handshake = `WriteC1`, then
  `SubscribeC2`, 15 s for the response; every packet through `WriteC1`
  write-with-response; C2 indications delivered in order on a goroutine
  that is not the writer's).
- `esphome.NewCentral(proxies []esphome.Proxy, opts Options) (*Central, error)`;
  `Proxy{Addr string (host:port, default port 6053), Key string (base64
  32 bytes, empty = plaintext), Name string}`; `Options{Logger, Clock,
  Dial func}`. Several proxies: scanning runs on all, advertisements are
  merged by address with the best RSSI per address, `Connect` uses the
  proxy that last saw the peripheral strongest; a proxy that drops is
  reconnected with backoff and does not fail the central.
- Wire: the ESPHome native API (verified in `aioesphomeapi/api.proto`):
  plaintext framing `0x00 | varuint len | varuint type | payload`; Noise
  framing `0x01 | u16 len | encrypted(u16 type | u16 len | payload)` with
  `Noise_NNpsk0_25519_ChaChaPoly_SHA256`, prologue `NoiseAPIInit\x00\x00`,
  PSK = the decoded key (`_frame_helper/noise.py:216, :297-301`); the
  client hello and the handshake bytes as `noise.py` sends them;
  `HelloRequest` (API 1.19) → `HelloResponse`, `ConnectRequest` →
  `ConnectResponse`, `DeviceInfoRequest` → `DeviceInfoResponse`
  (`bluetooth_proxy_feature_flags`: PASSIVE_SCAN 1<<0,
  ACTIVE_CONNECTIONS 1<<1, REMOTE_CACHING 1<<2, PAIRING 1<<3,
  CACHE_CLEARING 1<<4, RAW_ADVERTISEMENTS 1<<5 — `model.py:138-147`;
  refuse a proxy without ACTIVE_CONNECTIONS with a clear error, and
  without RAW_ADVERTISEMENTS fall back to the parsed
  `BluetoothLEAdvertisementResponse`), `PingRequest`/`PingResponse` every
  20 s. Messages (ids verified): `SubscribeBluetoothLEAdvertisementsRequest`
  (66) → `BluetoothLERawAdvertisementsResponse` (93; raw AD bytes, parse
  the AD structures: type 0x16 service data with UUID `FFF6`, first byte
  0x00, 8 bytes — `transport/btp/codec.go:337-353` has the decoder, reuse
  it); `UnsubscribeBluetoothLEAdvertisementsRequest` (87);
  `BluetoothDeviceRequest` (68; CONNECT with `address_type` from the
  advertisement, DISCONNECT) → `BluetoothDeviceConnectionResponse` (69;
  `connected`, `mtu`, `error`; `connected=false` later = disconnect);
  `BluetoothGATTGetServicesRequest` → `…ServicesResponse` /
  `…ServicesDoneResponse` (find the Matter service `FFF6`, C1/C2/C3 by
  the UUIDs in `transport/btp/consts.go:16-24` — C3 is
  `64630238-8772-45F2-B87D-748A83218F04`; keep the handles and C2's CCCD
  descriptor handle 0x2902); `BluetoothGATTWriteRequest` (75;
  `response=true`) → `BluetoothGATTWriteResponse` / `BluetoothGATTErrorResponse`;
  `BluetoothGATTNotifyRequest` (78) → `BluetoothGATTNotifyResponse`, **plus
  the CCCD write** `BluetoothGATTWriteDescriptorRequest` with `02 00`
  (C2 is indicate-only; the firmware only registers locally,
  `bluetooth_connection_bluedroid.cpp:325` "the CCCD write is the API
  client's responsibility"); `BluetoothGATTNotifyDataResponse` (79) =
  the C2 indications; `BluetoothGATTReadRequest` → `…ReadResponse` for C3;
  `SubscribeBluetoothConnectionsFreeRequest` → `BluetoothConnectionsFreeResponse`.
  Hand-written varint/protobuf encoders for exactly these messages (as
  the module hand-writes TLV), no protobuf dependency; every encoder
  cites the proto line.
- Dependencies of the nested module: go-fabric (replace `../..` as
  `ble/bluez` does) and **one** Noise library — `github.com/flynn/noise`
  (BSD-3-Clause) unless `golang.org/x/crypto` already covers it in
  `ble/bluez`'s tree (it does not: x/crypto has no Noise). Record it in
  `THIRD-PARTY-NOTICES.md` and `licenses/`. The Noise usage (handshake,
  nonce handling, the 0x01 framing) is reviewed by the launcher line by
  line: keep it in one file `noise.go` with the aioesphomeapi lines cited.
- One API subscriber per proxy: subscribing replaces the previous
  subscriber (`bluetooth_proxy.cpp`, map §3) — Home Assistant's own
  Bluetooth integration is usually that subscriber. Document it (the
  proxy should be dedicated or HA's use of it accepted to flap), expose
  it in the log at connect (`ble.esphome.proxy_taken`), and do not fight
  back: when the proxy drops our subscription, reconnect once after the
  backoff and report `ErrProxyBusy` if it is taken again.

## 2 Tests

- `ble/esphome/esphometest`: an in-process fake proxy speaking the
  plaintext framing (and Noise when a key is set) over a `net.Listener`,
  backed by `controller/ble/bletest`'s peripheral model so the existing
  commissioner tests run unchanged against `esphome.NewCentral` with a
  fake proxy — the proof that the backend is a drop-in `Central`.
- Unit tests: framing (both), the Noise handshake against a recorded
  aioesphomeapi exchange if one can be generated with the Python client
  offline (optional; otherwise against the fake's symmetric implementation
  plus a known-answer test of the hash chain), AD parsing (Matter service
  data, non-Matter advertisements dropped), service discovery and handle
  mapping, CCCD write before the first indication, MTU → segment size,
  disconnect propagation, proxy loss during a session (`btp.ErrDisconnected`),
  two proxies seeing one device (best RSSI wins), a proxy without
  ACTIVE_CONNECTIONS refused.
- `-race`; the module's `make ci` covers nested modules as it covers
  `ble/bluez` (check the Makefile and CI; add the module where `ble/bluez`
  is listed).

## 3 Docs and release

- `docs/guides/ble-proxies.md` (short: what a proxy needs — `active: true`,
  an API key —, the one-subscriber rule, how to construct the central and
  hand it to `controller.PairOptions.BLE` / `controller.Config`), a row in
  `docs/matterjs-comparison.md` (ahead: neither matter.js nor chip does
  this), ADR 0024 gets an amendment proposed in the report (the launcher
  writes it), CHANGELOG `[Unreleased]` Added for `ble/esphome`; the
  reference-controller CLI gets `--bluetooth-proxy host[:port][,key]`
  (repeatable) next to its adapter flag if the CLI is where the BLE
  backend is chosen (say where).
- Guards: `make ci`, `make race` twice, the nested module's own
  `go test -race ./...`, `make reachability`/`cite-check` snapshots
  committed, `mkdocs build --strict`. No chip-tool run (no device-side
  change).
- Hand back once: PR number, HEAD, guards with results, differences from
  the brief, Questions verbatim, the `by_design`/ADR text proposed.
  Then go-fabric-server wave S5 (`--bluetooth-proxy`, add-on option)
  follows in its own brief.
