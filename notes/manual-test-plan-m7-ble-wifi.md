# Manual test plan: BLE and Wi-Fi commissioning of a node (milestone 7)

Written 2026-11-12 for the owner at the device, after `v0.22.0` is tagged.
Milestone 7 (ADR 0031, waves 28a–28c) lets a node of this module without a
network at first boot be commissioned over Bluetooth LE onto Wi-Fi:
`node.Network.BLE` (`ble/bluez` on Linux), `node.Network.WiFi.Driver`
(`wifi/wpasupplicant`), and `examples/ble-wifi-light`, the node every proof
drives.

What is checked automatically and **not** repeated here: the fake-bus tests
of `ble/bluez` and `wifi/wpasupplicant` (`make bluez-check`,
`make wpasupplicant-check`), the in-process node tests
(`examples/ble-wifi-light` `TestNode`, `node/` BLE and Wi-Fi tests), and the
weekly `ble-virtual` job (real bluetoothd over btvirt, both directions;
wpa_supplicant and hostapd on mac80211_hwsim where the runner's kernel has
it). What only a device can show: a real radio, a real access point, real
ecosystems, and time.

Each finding becomes an issue named by its step (e.g. `B3`) with the logs
listed under "Logs to keep". Nothing here is a certification statement
(ADR 0011).

## Part P — preparing the host

Two hosts, the same steps: a Raspberry Pi (Raspberry Pi OS Bookworm,
64-bit, built-in Bluetooth and Wi-Fi) and a generic Linux host (Debian or
Ubuntu with a Bluetooth adapter and a Wi-Fi interface). The Pi has **no
Ethernet cable** for parts B and C.

| # | Step | Expected |
| --- | --- | --- |
| P1 | `sudo apt install bluez wpasupplicant`; `bluetoothctl --version`, `wpa_supplicant -v` | BlueZ 5.66 or later, wpa_supplicant 2.10 or later; note both versions in the issue. |
| P2 | `systemctl status bluetooth`; `bluetoothctl show` | bluetoothd runs; the adapter (`hci0`) is `Powered: yes` (`bluetoothctl power on` otherwise). `rfkill list` shows no soft or hard block. |
| P3 | wpa_supplicant with its D-Bus interface: `systemctl status wpa_supplicant` (the distribution's unit starts it with `-u`); `busctl --system status fi.w1.wpa_supplicant1` | The service answers on the system bus. |
| P4 | NetworkManager off the Wi-Fi interface: `nmcli device set wlan0 managed no` (or an `unmanaged-devices=interface-name:wlan0` entry), or NetworkManager not installed (Raspberry Pi OS Lite with `dhcpcd`: leave it, it does not join networks by itself when `wpa_supplicant.conf` has none) | `nmcli device` shows `wlan0` unmanaged; `wpa_cli -i wlan0 list_networks` is empty. |
| P5 | Permissions: run as root for the first pass; for the second pass run as a user in the `bluetooth` group with a D-Bus policy that allows `fi.w1.wpa_supplicant1` for that user | Both passes start the example without a D-Bus "AccessDenied". |
| P6 | Build the example on the host or cross-build it: `cd examples/ble-wifi-light && GOOS=linux GOARCH=arm64 go build -o ble-wifi-light .` (`GOARCH=arm GOARM=7` for a 32-bit OS) | One static binary, no cgo. |
| P7 | Start it: `sudo ./ble-wifi-light --adapter hci0 --wifi-interface wlan0 --storage light.db --log-level debug 2>light.log` | It prints a QR code and a manual code; `light.log` shows `wpasupplicant: interface ready`, `matter.ble.serving` and `commissioning.own_window.open discriminator=3840`. From another host, `bluetoothctl scan on` lists `MATTER-3840`. |

## Part B — commissioning with chip-tool

chip-tool with BLE (the snap on amd64, or a build with BLE; the harness
image's chip-tool has no BLE). The access point's SSID and passphrase are
`<ssid>` and `<pass>`.

| # | Step | Expected |
| --- | --- | --- |
| B1 | `chip-tool pairing ble-wifi 1 <ssid> <pass> 20202021 3840` | Pairing succeeds within 60 s. `light.log`: PASE over BTP, `AddOrUpdateWiFiNetwork`, `wpasupplicant: added network`, `wpasupplicant: connected`, `mdns.responder.joined iface=wlan0`, CASE, then `matter.ble.stopped`. `bluetoothctl scan on` on another host no longer lists `MATTER-3840`. |
| B2 | `chip-tool networkcommissioning read networks 1 0`, `read last-networking-status 1 0`, `read feature-map 1 0` | `Networks` lists `<ssid>` with `Connected: TRUE`; `LastNetworkingStatus` 0 (Success); FeatureMap 1 (Wi-Fi). |
| B3 | `chip-tool onoff toggle 1 2`, then `onoff read on-off 1 2` | `light.log` shows `msg=light on=true`; the read returns TRUE. |
| B4 | `grep -c '<pass>' light.log` | 0. |
| B5 | Stop the example (Ctrl-C) and start it again with the same `--storage` | It rejoins `<ssid>` at start (`wpasupplicant: connected` before the fabric's records go out), does **not** advertise over BLE, and chip-tool reads it again within 30 s. |

## Part R — commissioning with the reference controller

`go build -o reference-controller ./ble/bluez/cmd/reference-controller` on a
second Linux host with Bluetooth, on the same Wi-Fi network as the access
point (or wired to it).

| # | Step | Expected |
| --- | --- | --- |
| R1 | Factory reset (part F), then `reference-controller --db home.db --attestation accept --trust development pair --ble --ble-adapter hci0 --code '<QR code>' --wifi-ssid <ssid> --wifi-credentials <pass>` | Pairing succeeds; the controller's log names the BLE discovery, PASE over BTP, the network step and the reconnect over IP. Use the **QR code**: the manual code names no discovery capability. |
| R2 | `reference-controller --db home.db read <id> 0/NetworkCommissioning/0xFFFC 0/NetworkCommissioning/Networks 0/NetworkCommissioning/LastNetworkingStatus` | FeatureMap 1, the network `connected: true`, LastNetworkingStatus 0. |
| R3 | `reference-controller --db home.db invoke <id> 2/OnOff/Toggle` | The light switches (`light.log`). |

## Part A — Apple Home

| # | Step | Expected |
| --- | --- | --- |
| A1 | Factory reset; iPhone near the Pi; Home → Add Accessory → scan the QR code | Home finds the accessory over Bluetooth, warns that it is an uncertified accessory (confirm "Add Anyway"), asks for nothing about Wi-Fi (it hands over the iPhone's network) and completes. `light.log` shows the same sequence as B1 with the iPhone's SSID. |
| A2 | Switch the light in Home; restart the example; wait two minutes | Each switch is visible in `light.log` within a second; after the restart Home shows the light without "No Response". |
| A3 | Home → accessory settings → Remove Accessory | `light.log` shows the fabric removed; with the last fabric gone the node opens its own window and is back on the radio (`matter.ble.serving`, `MATTER-3840` visible). |

## Part F — factory reset and a second run

| # | Step | Expected |
| --- | --- | --- |
| F1 | Stop the example, delete `light.db`, `sudo wpa_cli -i wlan0 remove_network all` | A fresh node: it advertises over BLE at the next start. |
| F2 | Run B1 to B4 a second time on the same host | The same result; no leftover network in `wpa_cli -i wlan0 list_networks` beyond the one joined. |

## Part N — negative cases

| # | Step | Expected |
| --- | --- | --- |
| N1 | B1 with a wrong passphrase | chip-tool reports the network step failing (`NetworkCommissioning` status AuthFailure or UnknownError — note which); `wpa_cli -i wlan0 list_networks` lists no network afterwards (the failed join removes the network it added); the example stays on the radio, and a second B1 with the right passphrase succeeds without a restart. |
| N2 | The access point off during B1 | The network step fails within about 20 s (the connect timeout); the node stays commissionable over BLE; with the access point back, B1 succeeds. |
| N3 | The access point off after commissioning, then back on | The node is unreachable while it is off; once it is back, wpa_supplicant reassociates and chip-tool reads the node again within a minute (the node re-announces on the interface). |
| N4 | `rfkill block bluetooth` before starting the example | The example exits with an error naming the adapter (or BlueZ's D-Bus error); nothing advertises. `rfkill unblock bluetooth` and a restart bring it back. |
| N5 | `rfkill block wifi` during B1 | The network step fails (UnknownError, `interface_disabled` in `light.log`); unblock and B1 succeeds. |

## Logs to keep

Per finding: `light.log` (debug), chip-tool's or the reference controller's
output, `journalctl -u bluetooth -u wpa_supplicant --since <start>`,
`btmon -w btmon.snoop` during the BLE part, the BlueZ, wpa_supplicant and
kernel versions, and the host model.

## What a finding triggers

A failure in B, R or A with the radio stack confirmed working (P2, P3) is a
defect of `ble/bluez`, `wifi/wpasupplicant` or the node: an issue with the
step and the logs, fixed before the next release. A failure only in A with B
and R passing is an Apple Home interoperability finding and gets the same
issue plus a comparison with a matter.js node on the same host.
