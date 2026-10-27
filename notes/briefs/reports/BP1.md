# Report BP1 — `ble/esphome`: Matter BLE commissioning through ESPHome Bluetooth proxies

Brief: `notes/briefs/2026-10-27-BP1-ble-esphome.md`. Branch `bp1/ble-esphome`
from `origin/main` (`ec026d57`). Sources read in the clones, never run:
aioesphomeapi at `3b2a4a2d` (MIT; `api.proto`, `_frame_helper/noise.py`,
`noise_encryption.py`, `plain_text.py`, `packets.py`, `connection.py`,
`client.py`, `model.py`), ESPHome at `1cc6c9d1` (read only, nothing copied;
`bluetooth_proxy/`, `bluetooth_connection/`, `api/api_frame_helper_noise.cpp`,
`api/api_frame_helper_plaintext.cpp`). Every line the code builds on was
re-read in the clones; the cited lines are the clones', not the fact map's
(several of the map's line numbers were off by a few and are corrected in the
code). matter.js is cited only where the BlueZ platform already cites it
(`NobleBleChannel.ts`, `NobleBleClient.ts`, `BleScanner.ts` at the pin).

## Per §

### §1 Shape — `8ddc145d`

- `ble/esphome` (own `go.mod`, `replace … => ../..`, requires go-fabric
  `v0.13.3`, `github.com/flynn/noise v1.1.0`, `golang.org/x/crypto v0.58.0`).
  Implements `ble.Central`, `ble.Connection`, `ble.Reachability`.
  `NewCentral([]Proxy, Options) (*Central, error)`, `Proxy{Addr, Key, Name}`
  (port 6053 by default), `Options{Logger, Clock, Dial, ClientInfo,
  ConnectTimeout, SilenceTimeout}`, `ParseProxy("host[:port][,key]")`,
  `Close`.
- `internal/wire`: `proto.go` (the messages by hand, every one citing its
  `api.proto` lines; proto3 rules; repeated uint64 read packed or unpacked,
  written unpacked as the firmware writes it), `frame.go` (plaintext:
  `0x00 | varuint len | varuint type | payload`, varuints capped at 4 bytes,
  frames at 65535), `noise.go` — **the one file for the launcher's review**:
  both roles, `Noise_NNpsk0_25519_ChaChaPoly_SHA256`, initiator = client,
  PSK = decoded key, prologue `NoiseAPIInit\x00\x00`, the client hello
  `01 00 00` and `01 len16 00 msg1` in one write, the server hello
  (`0x01 name\0 mac\0`), the reject frame (`"Handshake MAC failure"` →
  `ErrInvalidKey`), transport frames `0x01 len16 AEAD(type16 len16 payload)`
  with flynn's CipherState (nonce `0^4 || LE64 counter`, empty AD, one
  counter per direction, encryption and write under one mutex so the
  counter order is the wire order). Cited: `noise.py:50, :62-84, :151-195,
  :197-205, :207-275, :299-301, :305-323, :325-362, :375-414`,
  `noise_encryption.py:18, :44, :51-65, :68-103`.
- Session: HelloRequest (API 1.19) → HelloResponse (major > 2 refused),
  DeviceInfoRequest, and from API 1.15 DeviceCapabilitiesRequest
  (`client.py:520-545`); Ping every 20 s, 90 s pong timeout; the device's
  DisconnectRequest answered. A proxy without ACTIVE_CONNECTIONS
  (`ErrNoActiveConnections`) or REMOTE_CACHING (`ErrNoRemoteCaching`) is
  refused at connect.
- Scan: `SubscribeBluetoothLEAdvertisementsRequest{flags=1}` (raw, 93) where
  RAW_ADVERTISEMENTS is set, `flags=0` and the parsed 67 otherwise; AD
  structures parsed, 0x16 / UUID 0xFFF6, exactly 8 bytes accepted by
  `btp.DecodeAdvertisementServiceData`; everything else dropped. Reports
  forwarded only while scanning; sightings kept per (address, proxy) with
  RSSI and address type; a proxy's sightings forgotten when its connection
  ends.
- Connect: the usable proxy with the strongest RSSI among the reports within
  30 s of the latest; `BluetoothDeviceRequest{CONNECT_V3_WITHOUT_CACHE,
  has_address_type, address_type}` → 69 (MTU, or GATTError "connect");
  services 70 → 71… → 72; Matter service by short `0xFFF6` or its 128-bit
  form; C1/C2/C3 by `transport/btp` UUIDs; C2's CCCD by `0x2902`. Missing C1,
  C2 or CCCD → `ErrNoMatterService`.
- GATT: one operation in flight per connection, replies matched by type and
  handle (`client.py:1215-1244`); `WriteC1` = write request with
  `response=true`; `SubscribeC2` = NotifyRequest(enable) then the CCCD write
  `02 00` (`01 00` only for a notify-only C2); `UnsubscribeC2` = CCCD
  `00 00` (bounded 5 s) then NotifyRequest(disable) fire and forget;
  `ReadC3`; `Disconnect` = DISCONNECT, bounded 5 s. Indications delivered in
  order on a goroutine of the connection's (the BlueZ pattern). A
  disconnect (69 `connected=false`), the API connection's loss, or an
  operation that overruns its bound ends the connection; every later call
  wraps `btp.ErrDisconnected`.
- Reconnect: backoff 1 s doubling to 1 min; reset after a session.
- One subscriber: `ble.esphome.proxy_taken` logged at every subscribe. The
  firmware never tells the old subscriber it lost the subscription
  (`bluetooth_proxy.cpp:740-765`), so the central watches for silence: a
  subscribed proxy that forwards no advertisement batch for
  `SilenceTimeout` (30 s; batches come every 100–200 ms,
  `bluetooth_proxy.cpp:118-121, :686-709`) is presumed taken → reconnect
  once after the backoff (which subscribes, so takes it back once); silent
  again → busy: `ErrProxyBusy` from `Connect` and from a re-asked
  `StartScanning` when every proxy is busy, not reconnected, until the next
  `StartScanning` after a `StopScanning`. The subscription is held while a
  connection runs, after `StopScanning` too: the firmware disconnects every
  slot of a subscriber that leaves (`bluetooth_proxy.cpp:637-648`).
- Reachability: a connected, non-busy proxy reported the peripheral and has
  a slot (`free > 0`, or the address in `allocated`, or no
  `BluetoothConnectionsFreeResponse` yet).
- `THIRD-PARTY-NOTICES.md`, `NOTICE`, `LICENSE`, `licenses/` (flynn/noise and
  x/crypto texts) in the module.

### §2 Tests — `8ddc145d`

- `esphometest`: a fake proxy on a loopback listener, plaintext or Noise
  (`wire.ServerHandshake`), in front of a `bletest.Central` of the given
  peripherals: newest subscriber gets advertisements (a batch every 50 ms,
  with a non-Matter device in each) and every GATT reply; notify
  registration local, indications forwarded only after the CCCD write;
  `Take`, `DropClients`, `DisconnectPeripheral`, `SetRSSI`, `Events`; a
  Noise client at the plaintext fake gets `"\x00Bad indicator byte"`
  (`api_frame_helper_plaintext.cpp:211-235`).
- `internal/wire`: encoding known answers (hand-derived bytes), the firmware
  shapes (unpacked and packed uuid words, short UUIDs, allocated), round
  trips, plaintext framing limits, **the Noise known-answer test**: the whole
  NNpsk0 chain derived from the Noise specification by hand
  (SHA-256, HKDF, X25519, ChaCha20-Poly1305 from the standard library and
  x/crypto, fixed ephemeral keys) holds the client's first write
  (hello + msg1), the server hello frame, msg2 and the first two transport
  frames (nonces 0 and 1) byte for byte; the refusals (wrong key, plaintext
  at Noise, Noise at plaintext, malformed server frames, keys).
- `esphome`: `TestScanReportsMatterAdvertisements` (raw and parsed),
  `TestConnectAndGATT` (short and 128-bit UUIDs; MTU 100 → segment 97; the
  order connect → services → read C3 → write C1 → notify on → CCCD 0200 →
  first indication; CCCD 0000; peripheral disconnect propagation; write
  after it = `btp.ErrDisconnected`), `TestConnectErrors` (unseen, no slot →
  `GATTError`, reachability with a full slot table), `TestBestRSSIWins` (and
  the swap), `TestProxyLossDuringSession` (channel closes with
  `btp.ErrDisconnected`, slots freed, resubscribe, reachable again),
  `TestProxyTakenThenBusy`, `TestRefusals` (no active connections, no remote
  caching, capabilities path, wrong key, plaintext/Noise mismatches, all
  unreachable vs one of two), `TestNoiseProxy`, `TestParseProxy`.
- The commissioner's tests through the fake (`commission_test.go`):
  `TestPairOverBLE` (controller/ble_test.go's body; plaintext and Noise:
  ErrConfiguration without a network, then PASE over BTP, Wi-Fi steps, CASE),
  `TestDialRunsExchangesWithoutMRP` and `TestChannelClosesWhenPeripheralDrops`
  (controller/ble/channel_test.go's bodies); only the central constructor
  differs.
- `-race -count=3` on the module green before the guards.

### §3 Docs and release — `5b2973fc`, `52bbf6d6`, `3615a861`

- `make esphome-check` (build linux/darwin/windows, vet, `-race` tests,
  golangci-lint, `go mod tidy -diff`, `go mod verify`) in `make ci`; CI job
  `esphome` (name `ble/esphome`) beside `bluez`; workflow YAML parsed, every
  job has steps.
- CLI: the backend is chosen in the program, so the flag sits in the shared
  command line: `cli.Options.Proxies func(specs []string, log *slog.Logger)`
  and `pair --bluetooth-proxy host[:port][,key]` (repeatable, implies
  `--ble`; `errNoProxies` in a build without it), set by the new
  `ble/esphome/cmd/reference-controller`. Test `TestPairBluetoothProxyFlag`.
- `docs/guides/ble-proxies.md` (+ mkdocs nav), the comparison row ("ahead",
  none in matter.js or chip), README row, getting-started line, reference
  controller README §"Through ESPHome Bluetooth proxies", CHANGELOG
  `[Unreleased]` Added (two entries).
- Snapshots: reachability (unreached 3, unchanged; one new whitelisted test
  name), citations (5787 → 5793, drift 258 unchanged, hard errors 0).

## Guards

Run at HEAD `3615a861` (the wire error-prefix fix-up follows it):

| Guard | Result |
| --- | --- |
| `ble/esphome` `go test -race -count=3 ./...` (before) and `-count=2` (in the guard run) | pass |
| `ble/esphome` build linux/darwin/windows, vet, golangci-lint (0 issues), `go mod tidy -diff` | pass |
| `make reachability`, `make cite-report` → committed `3615a861`; `make cite-check` | pass (unreached 3, drift 258, hard errors 0) |
| `mkdocs build --strict` | pass |
| `examples/reference-controller/cli` BLE tests | pass |
| `make ci` | **failed in `test`**: `script/errprefix` (ADR 0026 P11) — `internal/wire`'s error texts began `esphome:`, the package is `wire`. Fixed (texts now `wire: …`); `go test ./script/errprefix/` passes after the fix. The rest of `make test` had 254 packages ok and no other FAIL before the stop. |
| `make race` ×2, the rest of `make ci` (lint, cover-check, bluez-check, esphome-check), `make reachability-check` | **not run**: the guard run was stopped by Claude Code for low system memory (not a failure of the run); not restarted on my own. To run: `make ci && make race && make race`. |


Skipped: chip-tool (no device-side change, as the brief says).

## Differences from the brief

1. **No ConnectRequest.** It is `AuthenticationRequest` (ids 3/4), removed
   in ESPHome 2026.1.0 (`api.proto:105-106`); aioesphomeapi sends only
   HelloRequest without a password (`connection.py:660-686`).
2. **Feature flags** come from `DeviceCapabilitiesResponse` (149/150) from
   API 1.15, from `DeviceInfoResponse` field 15 below, as
   `device_capabilities_compat` does (`client.py:520-545`); field 15 is
   marked superseded (`api.proto:311-312`).
3. **REMOTE_CACHING is required as well** (`ErrNoRemoteCaching`):
   aioesphomeapi refuses to connect without it (`client.py:1330-1343`), the
   firmware sets it with `active` (`bluetooth_proxy.h:207-235`).
4. **CONNECT_V3_WITHOUT_CACHE**, not CONNECT: V1 is removed
   (`api.proto:1917`).
5. **"The proxy drops our subscription"** has no message: detected by
   advertisement silence (`SilenceTimeout`, 30 s). The busy mark clears at
   the next fresh scan; an overrun GATT operation ends the connection.
6. **The commissioner tests are ported, not run in place**: they are
   package tests of the main module, which cannot import the nested module.
   The bodies are theirs; `TestDialHandshakeTimeoutAndDisconnect` is not
   ported (its silent central is no proxy).
7. **No recorded aioesphomeapi exchange** (nothing may run from the
   clones); the known-answer test is derived from the Noise specification
   and covers both handshake messages and the transport frames.
8. **x/crypto bumped** to v0.58.0 (flynn/noise pins a 2021 pseudo-version).
9. **`controller.Config` has no BLE field**; the guide names
   `PairOptions.BLE` only.
10. `ErrInvalidKey`, `ErrMalformedKey`, `ErrRequiresEncryption`,
    `ErrPlaintextDevice` are re-exported from `esphome` (wire is internal).

## Decisions

- `internal/wire` holds both ends, so the fake and the client cannot drift;
  the known answers pin the bytes against the sources, not against the fake.
- The central refuses at `NewCentral` on a permanent refusal of any proxy
  and on every proxy unreachable; one unreachable of several is retried.
- `ListeningClock` not implemented (optional; the proxies' scan time is not
  observable beyond the advertisement stream).

## Left out

- Dependabot does not cover `ble/esphome` (nor `ble/bluez` today).
- No release tag; `ble/esphome/vX.Y.Z` moves with the main module's.
- No live run against a real proxy (none on the test bench); the owner's
  manual test plan line would be: `pair --bluetooth-proxy <proxy>,<key>`
  with a Wi-Fi Matter device, Home Assistant's use of the proxy observed.

## Questions

1. Busy reset: the busy mark clears at the next fresh `StartScanning` (a new
   pairing may take the proxy from Home Assistant again). Alternative:
   sticky until `Close`. Recommendation: keep the reset.
2. `SilenceTimeout` default 30 s — keep, or shorter (a pairing waits that
   long before the take-back)? Recommendation: keep 30 s.
3. Dependabot entries for `ble/bluez` and `ble/esphome`? Recommendation:
   add both in the next CI-only change.

## Proposed texts

**ADR 0024, amendment** (decision 3):

> *Amended 2026-10-27 (owner decision, brief BP1): a second platform module,
> `github.com/SukramJ/go-fabric/ble/esphome`, reaches BLE through ESPHome
> Bluetooth proxies (`bluetooth_proxy: active: true`) over the ESPHome native
> API on TCP — plaintext, or Noise_NNpsk0_25519_ChaChaPoly_SHA256 with the
> device's API key through `github.com/flynn/noise` (BSD-3-Clause), its one
> dependency beyond the main module. It is pure Go and builds for every
> platform, so decision 6 does not bind it. The API messages are encoded by
> hand after aioesphomeapi's `api.proto` (MIT). A proxy serves one API
> subscriber, the newest, so commissioning through a proxy takes it from Home
> Assistant while the pairing runs; the central does not fight for it
> (`ErrProxyBusy`). Proof: an in-process fake proxy (`esphometest`) over
> `bletest`'s peripherals runs the commissioner's BLE tests, and a Noise
> known-answer test derived from the Noise specification; a real proxy is in
> the manual test plan.*

**by_design, Ahead of matter.js** (one paragraph):

> **BLE through ESPHome Bluetooth proxies** (`ble/esphome`). matter.js
> marker: none — `packages/nodejs-ble` reaches BLE only through noble on a
> local adapter, and `BleScanner.ts:46-52` names proxies only as a reason for
> `isPeripheralReachable`; connectedhomeip has no proxy transport either. The
> central runs the BlueZ platform's sequence over the ESPHome native API
> after aioesphomeapi (`api.proto`, `_frame_helper/noise.py` at 3b2a4a2d) and
> ESPHome's `bluetooth_proxy` behaviour (at 1cc6c9d1); it implements
> `isPeripheralReachable` from the proxies' connection slots. Reason: a host
> without a Bluetooth adapter commissions through the proxies a Home
> Assistant site already has.
