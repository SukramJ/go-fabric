# Report BC — milestone 1, phase 13b: the virtual BLE job with a matter.js peripheral

Branch `m1/phase-13b`, started on `notes/brief-bc` d52a0ac, rebased onto
`origin/main` f211c4a after #148 merged (the brief commit dropped as
already applied). matter.js read at `0ceb202d` (`git show`), chip at
`6170af84`, BlueZ 5.72 sources (`emulator/btdev.c`, `emulator/main.c`)
from the upstream tag. Nothing under `notes/parity/`, `docs/adr/` or
`CLAUDE.md` changed.

| Commit | Item |
| --- | --- |
| `script/ble-virtual: the matter.js BLE peripheral` | 1 |
| `ci(ble-virtual): pair --ble against the matter.js peripheral` + six run fixes (`bluez-test-tools`, extra modules, kernel log, arm64 runner, `hciconfig`, read path / JSON match) | 2 |
| `ci(ble-virtual): the control leg …`, `… LE-only btvirt controllers` | 3 |
| `fix(ble/bluez): no UUIDs in the discovery filter` | defect the job found |
| `docs: the virtual BLE counterpart …` | 4 |

The run fixes stay separate commits because each cites the run that showed
it; a squash merge folds them.

## 1. The peripheral

**Built.** `script/ble-virtual/peripheral.mjs`: on/off light, root endpoint
`ServerNode.RootEndpoint.with(WifiNetworkCommissioningServer)`, the Wi-Fi
server command for command after `DummyWifiNetworkCommissioningServer.ts`
(ConnectNetwork marks connected, `restartAdvertisement()`),
`discoveryCapabilities {onIpNetwork:false, ble:true}` as `DeviceNodeFull.ts`,
`ble.hci.id` then `ble.enable` then `import("@matter/nodejs-ble")`
(`install.ts`, `NodeJsBle.ts:29-39`), passcode 20202021 / discriminator 3840,
prints `ready manual=34970112332 qr=…`, file logger at debug.
`package.json` pins `@matter/main`, `@matter/nodejs`, `@matter/nodejs-ble`,
`@project-chip/matter.js` to **`0.18.0-alpha.0-20261008-6066b7499`** (the
npm alpha of the tag 16 commits below the pin; the next alpha, `…20261013-2e5a97ca3`,
is after it); `package-lock.json` committed (bleno 0.13.1, noble 2.8.0,
bluetooth-hci-socket 2.3.0); `node_modules/` ignored.
**Guard.** Local run without an adapter: the node comes up, FeatureMap
`{wiFiNetworkInterface:true}`, then `EPERM` at `Hci.init` (expected).
**Decisions.** `.with()` instead of `behaviors.require` after `create`
(same composition, no transient "NetworkCommissioning missing" warning).
**Left out.** Thread variant.

## 2. The job and the script

**Built.** `script/ble-virtual-test.sh` and the workflow: `btvirt -L -l2`,
BlueZ keeps hci0 (`hciconfig up`), hci1 goes down (`hciconfig down`,
verified) and bleno binds it over the HCI user channel as root
(`HCI_CHANNEL_USER=1`); Node 22 via `setup-node` (npm cache on the lock),
`npm ci`, a check that bleno/noble were not dropped (optional deps), the BLE
build, `pair --ble --code <manual> --wifi-ssid lab --wifi-credentials
secret12`, the peripheral's `AddOrUpdateWiFiNetwork ssid=lab`, `read`
(VendorId 65521, NetworkCommissioning 0xFFFC = 1), `window open` (bonus,
not in the verdict). btmon records all controllers. Docker, avahi and the
CHIP image left the job. Three-way exit and step summary kept; environment
now names Node.js, `npm ci`, a dropped bleno/noble build, a controller that
does not go down, a peripheral that does not come up.
**Run fixes, each from a run.** 39099305585: `bluez-tests` does not exist
on 24.04 → `bluez-test-tools`. 39099404692: `hci_vhci` not in the base
modules → `linux-modules-extra-$(uname -r)`. 39099608818: x86-64 kernel
`6.17.0-1022-azure` has `# CONFIG_BT is not set`; a throwaway probe
(39099728809) found `CONFIG_BT=m`, `CONFIG_BT_HCIVHCI=m` on
`ubuntu-24.04-arm` (and on `ubuntu-22.04`) → job on `ubuntu-24.04-arm`.
39099786146: `btmgmt --index 1 power off` answered for hci0 (5.72 ignored
`--index`), hci1 stayed up, bleno `EBUSY` → `hciconfig`. 39101046417: the
CLI does not name global attributes (`has no attribute "FeatureMap"`) →
`0xFFFC`. 39101207635: the CLI prints `VendorId` → grep the JSON line.
**Guard.** `bash -n`; the dispatched run (below).
**Left out.** shellcheck/actionlint (the repo runs neither; not installed).

## 3. The control leg

**Built.** `script/ble-virtual/controller.mjs` (CommissioningController as
`examples/controller/src/ControllerNode.ts`: `discoveryCapabilities.ble`,
`commissioning.wifiNetwork`, manual code decoded), run after our leg against
a fresh peripheral, noble on hci0 over the user channel once BlueZ gave it
up. Ours fails + control passes = defect; both fail = environment; ours
passes = pass (a failed control is named in the summary).
**Found with it.** Run 39100460695 (both legs failed): noble sent `LE
Extended Create Connection`, nothing completed — BlueZ 5.72's emulator
completes it only towards an extended advertising set
(`emulator/btdev.c` `cmd_ext_create_conn_complete`, 5534-5560) and bleno
advertises legacy. `btvirt -L` (BTDEV_TYPE_LE gets no 5.0 commands,
`set_le_commands` 6759-6763) → legacy LE Create Connection on both sides.
**Decisions.** No `NOBLE_MULTI_ROLE` (two adapters, the README's case is
one); ours first, so BlueZ never has to retake hci0.

## The defect the job found — `ble/bluez` discovery filter

Run 39100460695's btmon trace: hci0 received the peripheral's ADV_IND
(Flags + Service Data 0xfff6, no UUID field) while BlueZ ran `MGMT Start
Service Discovery` with the Matter UUID, and no `Device Found` came — the
kernel's service discovery passes only advertisements with the UUID in a
UUID16/32/128 field. chip's Linux scanner omits UUIDs for this reason
(`src/platform/Linux/bluez/ChipDeviceScanner.cpp:227-236` at `6170af84`).
Fix: filter `Transport "le"` + `DuplicateData` only; `serviceData` already
selects the Matter service data. Test, module README, doc.go and CHANGELOG
updated. **For Fable to verify:** the comment in `central_linux.go`
(`StartScanning`) cites chip and the run.

## 4. Docs

`docs/matterjs-comparison.md` BLE row, the reference controller README
(BlueZ verified on virtual controllers only, the job's legs), CHANGELOG
entries of `ble-virtual` and `ble/bluez`. C12 unchanged.

## Workflow run

**https://github.com/SukramJ/go-fabric/actions/runs/39101585016 — success**
at 7b209e8 (rebased HEAD): "pass: pair --ble over hci0 commissioned the
matter.js peripheral on hci1 with Wi-Fi credentials; read over the
operational session; the control leg passed too"; window open ok.
Ours: pass. Control: pass (VendorID 65521). Environment: ubuntu-24.04-arm,
kernel 6.17.0-1022-azure (arm64), BlueZ 5.72, btvirt "Bluetooth emulator
ver 5.72", Node v22.23.3, matter.js `0.18.0-alpha.0-20261008-6066b7499`,
bleno 0.13.1, noble 2.8.0. First green run: 39101367424 (before the rebase).

## Guards

- `make ci` (build, vet, test, lint, cover-check, bluez-check) — exit 0 at
  7b209e8 (rebased HEAD).
- `ble/bluez`: `go test -race -count=3 ./...` — exit 0 at 7b209e8.
- No guard rerun for this report (notes only).
- Skipped: chip-tool (nothing under `node/`, `cluster/`, the daemon);
  `make reachability-check` (the inventory has no `ble/bluez` entry, no
  main-module Go change).

## Questions

- The reference controller's `read` does not resolve global attribute
  names (`FeatureMap`, `ClusterRevision`): a finding or by design?
- The job needs the arm64 runner while the x86-64 image's kernel lacks
  Bluetooth; ADR 0024 §5 says "GitHub-hosted runner" without an
  architecture — note it there?
- matter.js alpha's controller logs `synchronous-transaction-conflict` in
  `OperationalCredentialsServer.#handleUpdatedFabric` at start (harmless
  here; upstream's).

## by_design candidates

- `ble/bluez` scans without a UUID filter where matter.js calls
  `startScanningAsync([SERVICE_UUID_SHORT])`; the Matter service data
  selects in our code instead (chip's Linux scanner does the same).
