# Report BB — milestone 1, phase 13: `ble/bluez`, `pair --ble`, btvirt, CNET Wi-Fi

Branch `m1/phase-13`, started on `origin/m1/phase-12` + the brief, rebased
onto `origin/main` 5ae6583 after #144 merged (no conflicts; the cherry-picked
brief dropped, main carries it as #146). matter.js read at `0ceb202d`
(`git show`), chip at `6170af84`. Nothing under `notes/parity/`, `docs/adr/`
or `CLAUDE.md` changed.

| Commit | Item |
| --- | --- |
| 31bc774 `feat(ble/bluez)` | 1 |
| 646677f `feat(reference-controller): pair --ble …` | 2 |
| b19735b `ci(ble-virtual)` | 3 |
| 0e193da `test(chiptool): a second CNET run …` | 4 |
| 413c814 `chore(reachability)` | guard (two line numbers moved) |
| 4a5ee90 `docs(reference-controller)` | 2, README: BlueZ not yet verified live |

## 1. `ble/bluez`

**Built.** Module `github.com/SukramJ/go-fabric/ble/bluez` (own `go.mod`:
`require github.com/SukramJ/go-fabric v0.6.0` + `replace => ../..`,
`github.com/godbus/dbus/v5 v5.2.4`; README says the release bumps the
require). `NewCentral(adapter) (ble.Central, error)`, `""` = hci0; the value
also has `Close`. `bus` seam (Call, Signals, Close) over godbus;
`SetDiscoveryFilter{UUIDs:[btp.ServiceUUID], Transport:"le",
DuplicateData:true}` + `StartDiscovery` (InProgress = running), `StopDiscovery`;
`InterfacesAdded`/`PropertiesChanged` of `Device1` → `ServiceData[ServiceUUID]`
(8 bytes only); `Device1.Connect`, wait `ServicesResolved`, `GattService1`
with the Matter UUID, `GattCharacteristic1` C1 (`WriteValue` type
`request`), C2 (`StartNotify`, `Value` via `PropertiesChanged`, delivered
in order on a per-connection goroutine), C3 (`ReadValue`); ATT_MTU from C1's
`MTU`, else 0 (BTP minimum); `StopNotify`, `Disconnect`; `Disconnected()`
closes on `Connected=false`, `InterfacesRemoved` or a closed bus. `*Error`
carries the D-Bus error name; `ErrNoAdapter`, `ErrNoMatterService`,
`ErrUnsupported` (the `!linux` stub). UUIDs only from `transport/btp`.
Own `LICENSE`, `NOTICE`, `THIRD-PARTY-NOTICES.md`, `licenses/LICENSE-godbus-dbus.txt`.
Main `go.mod` unchanged. `make bluez-check` (build Linux + darwin stub, vet,
test, golangci-lint, `go mod tidy -diff`, `go mod verify`) is in `make ci`
and a new `ble/bluez` job in `ci.yml`.
**Guard.** `central_linux_test.go` over an in-process fake bus
(`fakebus_linux_test.go`): adapter missing, D-Bus names in errors
(AccessDenied, NotReady), the filter dict and call order, InProgress,
advertisement filtering (length, UUID, adapter), replay to a new callback,
one StopDiscovery; `ble.NewScanner` + `ble.Dial` end to end over a bletest
peripheral behind the fake bus (MTU 185, and no MTU with a late
`ServicesResolved`), C1→C2 order and `type=request`; C1/C2 missing →
`ErrNoMatterService` + Disconnect; `Connected=false`, `InterfacesRemoved`
and bus close end the connection; NotConnected write wraps
`btp.ErrDisconnected`. Coverage 86.4 %. Race ×3 clean.
**Differences from the sequence.** Bounds as matter.js puts them on noble:
connect 2 min (`NobleBleChannel.ts:203`), interview 1 min (:220), StopNotify
/ Disconnect 5 s (`BLE_DISCONNECT_TIMEOUT` :38). Not ported: the retry on a
disconnect during connect (`reTryHandler` :273-297), the 2 s ATT_MTU settle
wait (`attMtuOf` :526-560; BlueZ exposes the MTU as a property after the
exchange, read once), the `connectable` check (`NobleBleClient.ts:184-187`;
BlueZ's `Device1` has no such property), the scan-transition state machine
and the adapter-state deferral (`ScanControl`, `stateChange`; BlueZ answers
`NotReady` instead). Advertisements are reported whenever BlueZ sends them,
not only while our scan runs. No `ListeningClock`/`Reachability`.
**Decisions.** A second `Connect` to a device with an open connection is
refused. `StopDiscovery` NotReady is no error.
**Left out.** Live BlueZ (item 3). Logging in the central (no logger in the
fixed signature).
**Questions.** None.

## 2. `pair --ble`

**Built.** The command line moved to `examples/reference-controller/cli`
(`cli.Run(args, stdin, stdout, Options)`, `Options.Central`);
`examples/reference-controller/main.go` is the plain build (`--ble` →
"this build has no BLE; build ble/bluez/cmd/reference-controller");
`ble/bluez/cmd/reference-controller/main.go` is the BLE build.
`pair --ble [--ble-adapter hci0]`: `ble.NewScanner(...).Find` by the
code's discriminator (long from QR, short from manual), `ble.Dial`,
`commissioner.Options.Channel`; refuses without `--wifi-ssid`/
`--wifi-credentials` or `--thread-dataset` ("a device on BLE has no network
yet"). `controller/client.Registry.Commission` accepts a node without
addresses when `Options.Channel` is set (matter.js locates it by a `ble`
`NetworkAddress`, `CommissioningClient.ts:883-895`, which passes :220-223).
README (BLE build, rights, adapter flag), `docs/getting-started/controller.md`
(one paragraph, cli/ source paths), `docs/guides/{persistence,concurrency}.md`
paths, manual test plan row C12, coverage floors (`…/reference-controller` 0
with why, `…/cli` 70; measured 83.1).
**Guard.** `cli/pair_ble_test.go`: `TestPairOverBLE` pairs the harness node
(Wi-Fi commissioning over a test driver) over bletest + `bridgetest.ServeConn`
with `--ble --code … --wifi-ssid lab --wifi-credentials secret12`, the driver
connects "lab/secret12", then `read … Networks` over CASE shows
`connected:true`; `TestPairBLEWithoutBLEOrNetwork` (plain build, no network,
adapter error with `--ble-adapter hci9`).
**Differences.** —
**Decisions.** The BlueZ central is closed when the command ends (via
`io.Closer`). Minimum BlueZ version: **not verified** (see below); the
README says so rather than naming one.
**Left out.** —
**Questions.** The registry change in `controller/client` was not named by
the brief; it is the smallest change that lets a BLE-found node commission
(alternative: an IP-less `ble` address in `Record`).

## 3. btvirt job

**Built.** `script/ble-virtual-test.sh` (verdict: exit 0 pass, 1 defect,
2 environment; Markdown to `$GITHUB_STEP_SUMMARY`), `make ble-virtual-test`,
`.github/workflows/ble-virtual.yml` (`workflow_dispatch` only, ubuntu-24.04:
`bluez bluez-tests avahi-daemon`, vhci, bluetoothd, `btvirt -l2`, power on,
image check, BLE build, app with `--ble-controller <n>` on host network +
`/run/dbus` + privileged, `pair --ble`, `read`).
**The image's app has no BLE.** `chip-all-clusters-app --help` (and every
other app in the pinned image) has no `--ble-controller` — the option exists
only with `CHIP_DEVICE_CONFIG_ENABLE_CHIPOBLE` (`examples/platform/linux/
Options.cpp:170-172` at the pin; the brief's `--ble-device` is not the pin's
name) — and no `--wifi`; `org.bluez` does not occur in the app or chip-tool
binaries. The script checks this and ends **environment** — the item is an
environment limit, no workaround.
**Dispatch.** `gh workflow run ble-virtual.yml --ref m1/phase-13` → HTTP 404
"workflow … not found on the default branch": a new workflow cannot be
dispatched from a branch. The first run is the owner's after the merge; with
the pinned image it will end "environment" (btvirt/BlueZ on the runner are
exercised up to that check).
**Guard.** Locally: the script classifies "btvirt is not installed" as
environment (exit 2); `bash -n`.
**Left out.** A live run; BlueZ version and rights unverified (host: BlueZ
5.85, `org.bluez` did not answer — no adapter; no sudo for btvirt).
**Questions.** An image with CHIPoBLE (or a separately built
`chip-all-clusters-app` with BLE) is the owner's decision.

## 4. CNET Wi-Fi cases

**Built.** Family row `CNETWIFI` (`cases: "CNET"`, `variant: "wifi-sim"`):
every daemon of the run gets `--wifi-sim`; PICS generated from that daemon
(`testdata/pics-wifi-sim/ep0.txt`, only the slice that differs; drift-checked
against it); commissioned snapshots keyed by variant, each provisioned with
the Wi-Fi network a BLE-Wi-Fi TH would have configured (py:
`testdata/provision_wifi.py`; yaml: chip-tool ArmFailSafe /
AddOrUpdateWiFiNetwork / ConnectNetwork / CommissioningComplete) because chip
configures no network over IP (`AutoCommissioner.cpp:780-797`); Python cases
get `--in-test-commissioning-method ble-wifi --discriminator --passcode
--wifi-ssid zigbeehome --wifi-passphrase password123 --endpoint 0` (chip's
runner keeps the Wi-Fi flags only with a ble-wifi/nfc-wifi method; the
SSID/passphrase are the cases' PIXIT defaults, `Test_TC_CNET_4_5.yaml`).
Case ids `CNETWIFI/<n>`; golden, `docs/certifiability.md` (generated block +
prose), `chiptool.yml` group `commissioning` gains CNETWIFI. The default CNET
run and the certified composition are unchanged.
**Guard.** `GOFABRIC_CHIP_FAMILIES=CNETWIFI make chiptool-families-update`
(exit 0) and `GOFABRIC_CHIP_FAMILIES=CNET,CNETWIFI make chiptool-families`
(exit 0, 333 s). `make chiptool-test` skipped: nothing under `node/` or
`cluster/core/` changed.

| Case | Gate | Result | Class |
| --- | --- | --- | --- |
| 1.4 | — | pass | — |
| 4.1 | F00 | fails: Networks has no connected entry | (a) defect: Wi-Fi server does not persist committed networks across a restart (chip: `NetworkCommissioningWiFiDriver.cpp:77-118, :135-138`) |
| 4.4 | F00 | fails: no connected network | (a) same |
| 4.5 (yaml) | F00 | pass | — |
| 4.9 | F00 | fails: no connected network | (a) same |
| 4.11 | none | needs a 2nd AP and switches the TH host's Wi-Fi | (c) harness |
| 4.15 | F00 | pass | — |
| 4.23 | F00 | wrong SSID connects | (c) harness: the simulated driver connects any SSID |
| 4.25, 4.26, 4.27, 4.29 | none | skip themselves | (b) not supported: no PDC |
| 4.13, 4.14 | — | manual | (c) |
| 4.2, 4.6, 4.10, 4.12, 4.16, 4.22 | F01 | not applicable | (b) |
| 4.3 | F02 | not applicable | (b) |
| 4.20, 4.21 | — | manual | (c) |

**Decisions.** A separate family name (`CNETWIFI`) rather than a variant
inside the CNET row, so golden ids, the doc table and the workflow groups
stay one row per run.
**Left out.** Fixing the persistence gap (cluster/core, not in the brief).
**Questions.** The persistence gap is a finding for the register.

## by_design candidates

- `ble/bluez` without the noble-specific retry, MTU settle wait and
  `connectable` check (BlueZ's API has no counterpart).
- `Registry.Commission` located by `Options.Channel` instead of a `ble`
  network address in the record.
- CNETWIFI's snapshots provisioned with the Wi-Fi network after IP
  commissioning, standing in for a BLE-Wi-Fi TH.

## Guards

After the rebase onto `origin/main` 5ae6583:

- `make ci` (build, vet, test, lint, cover-check, bluez-check) — exit 0 at 0e193da.
- `make race` (whole module, CGO_ENABLED=1) — exit 0 at 0e193da.
- `ble/bluez`: `go test -race -count=3 ./...` — exit 0 at 0e193da; build,
  darwin build, vet, test, golangci-lint, `go mod tidy -diff`, `go mod verify`
  inside `make bluez-check` (part of `make ci`).
- `make reachability-check` — exit 2 at 0e193da (two inventory line numbers
  moved), regenerated in 413c814, exit 0 there; unreached unchanged.
- Harness (before the rebase, the same harness code): `GOFABRIC_CHIP_FAMILIES=CNETWIFI
  make chiptool-families-update` exit 0, `GOFABRIC_CHIP_FAMILIES=CNET,CNETWIFI
  make chiptool-families` exit 0 (worktree at 4cc8021 + item 4 uncommitted).
- Skipped: `make chiptool-test` (no file under `node/` or `cluster/core/`);
  the `ble-virtual` workflow (not dispatchable before it is on main, and the
  pinned image has no BLE); no guard rerun for 4a5ee90 and this report
  (documentation only).
