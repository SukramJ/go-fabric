# ADR 0024 — BLE commissioning in the controller role

- **Status**: Accepted (2026-10-16; concept v3, decision 0.1)
- **Date**: 2026-10-16
- **Related**: [ADR 0020 — the commissioner and controller role](./0020-commissioner-and-controller-role.md),
  [concept v3](https://github.com/SukramJ/go-fabric/blob/main/docs/history/concept-v3-commissioner-in-the-field.md) §A.1,
  `docs/matterjs-comparison.md` (the BLE row and non-goal 2)

## Context

"No Bluetooth" has been a scope boundary since concept v1: a LAN-attached
bridge has no need to get network credentials onto a device that has none,
and BLE pulls a platform radio stack into a library whose only other OS
dependency is a UDP socket. Since ADR 0020 the module is also a
commissioner, and a commissioner in the field meets exactly that device:
a Wi-Fi or Thread product out of the box, whose only commissioning channel
is BLE. `controller/commissioner` ports `ControllerCommissioningFlow.ts`
without BLE, Wi-Fi or Thread (`commissioner.go:83`), so the reference
controller pairs only devices that are already on the network.

matter.js at `0ceb202d` carries BTP in `packages/protocol/src/ble/`
(`BtpSessionHandler.ts`, 875 lines with `Ble.ts` and `BleConsts.ts`), the
advertisement in `packages/protocol/src/advertisement/ble/`, the platform
in `packages/nodejs-ble/` (2 092 lines over `@stoprocent/noble`), and the
network steps in `ControllerCommissioningFlow.ts:1527-1900`
(`addOrUpdateWiFiNetwork` :1677, `addOrUpdateThreadNetwork` :1874). On
Linux, BlueZ is reached over D-Bus without cgo (`github.com/godbus/dbus/v5`,
BSD-2-Clause); CoreBluetooth and WinRT need cgo or COM.

## Decision

1. **The controller role commissions over BLE; the device role stays
   without Bluetooth.** A node of this module never advertises over BLE.
2. **BTP is pure Go in the main module** (`transport/btp`), a port of
   `BtpSessionHandler.ts` proved against matter.js's test vectors; the
   seam `controller/ble` (`Central`, the advertisement parser, the PASE
   transport over a GATT channel) carries no dependency.
3. **The platform is a separate Go module**,
   `github.com/SukramJ/go-fabric/ble/bluez`, with its own `go.mod`: BlueZ
   over D-Bus, Linux only, no cgo. The main module keeps its five direct
   dependencies; a host without BLE never pulls `godbus`.
   *Amended 2026-10-27 (owner decision, brief BP1): a second platform
   module, `github.com/SukramJ/go-fabric/ble/esphome`, reaches BLE through
   ESPHome Bluetooth proxies (`bluetooth_proxy: active: true`) over the
   ESPHome native API on TCP — plaintext, or
   `Noise_NNpsk0_25519_ChaChaPoly_SHA256` with the device's API key
   (prologue `NoiseAPIInit` + the client hello, aioesphomeapi
   `_frame_helper/noise.py:301`, ESPHome `api_frame_helper_noise.cpp:26-27,
   :274-276`) through `github.com/flynn/noise` (BSD-3-Clause), its one
   dependency beyond the main module. It is pure Go and builds for every
   platform, so decision 6 does not bind it. The API messages are encoded by
   hand after aioesphomeapi's `api.proto` (MIT, read at 3b2a4a2d). A proxy
   serves one API subscriber, the newest, so commissioning through a proxy
   takes it from Home Assistant while the pairing runs; the central does
   not fight for it (`ErrProxyBusy` after a second silence). Proof: an
   in-process fake proxy (`esphometest`) over `bletest`'s peripherals runs
   the commissioner's BLE tests, a Noise known-answer test derived from the
   Noise specification; a real proxy is in the manual test plan.*
4. **The commissioner provisions the network** after
   `ControllerCommissioningFlow.ts`: Wi-Fi credentials or a Thread
   operational dataset, `ConnectNetwork`, then operational discovery over
   DNS-SD and CASE. Provisioning a Thread dataset needs no Thread radio on
   this side, so "no Thread" (the node's radio) is untouched.
5. **Proof**: BTP vectors; the network steps against the reference daemon
   with a simulated Wi-Fi driver (TESTING ONLY); end to end over virtual
   HCI controllers (`btvirt`) with connectedhomeip's all-clusters app in a
   manually started CI job; a real device in the manual test plan.
   *Amended 2026-10-18 (owner decision, phase 13 report BB §3): the
   pinned CHIP image's apps carry no CHIPoBLE — `--ble-controller` exists
   only under `CHIP_DEVICE_CONFIG_ENABLE_CHIPOBLE`
   (`examples/platform/linux/Options.cpp:170-172` at `6170af84`) — so the
   virtual counterpart is a **matter.js node with `@matter/nodejs-ble`**
   (bleno on the second `btvirt` controller), the gold standard itself;
   connectedhomeip's app returns to the job when an image built with BLE
   is pinned. The job stays manually dispatched, on the `ubuntu-24.04-arm`
   runner: the x86-64 `ubuntu-24.04` image's Azure kernel is built without
   Bluetooth (`# CONFIG_BT is not set`, run 39099608818), the arm64 one
   with `CONFIG_BT=m` and `CONFIG_BT_HCIVHCI=m` (probe run 39099728809).
   First green run 39101585016 (report BC).*
6. macOS and Windows stay out while they need cgo.

## Consequences

- `CLAUDE.md`'s scope line and `docs/matterjs-comparison.md` (the BLE row,
  non-goal 2) name the device-side boundary and this ADR.
- The sub-module has its own release tags (`ble/bluez/vX.Y.Z`) and
  `THIRD-PARTY-NOTICES.md`; its version moves with the main module's.
- BlueZ needs system-bus rights (the `bluetooth` group or a polkit rule);
  the reference controller's README says so, and the adapter's errors
  name the D-Bus cause.
