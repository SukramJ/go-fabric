#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# BLE end to end over virtual HCI controllers (ADR 0024 decision 5, as
# amended 2026-10-18): two btvirt controllers; BlueZ keeps the first for the
# reference controller's BLE build (ble/bluez/cmd/reference-controller),
# the second is taken down in BlueZ and claimed over the HCI user channel by
# a matter.js node with @matter/nodejs-ble (script/ble-virtual/peripheral.mjs,
# bleno). `pair --ble` commissions it with Wi-Fi credentials, then a read
# runs over the operational session (the node advertises on the runner's
# IP interfaces once ConnectNetwork "connected" it). `make ble-virtual-test`
# and .github/workflows/ble-virtual.yml run this script.
#
# The second direction (wave 28c, ADR 0031): examples/ble-wifi-light is the
# peripheral, through ble/bluez on real bluetoothd over the second
# controller, commissioned once by `pair --ble` and once by matter.js's
# controller (controller.mjs). Its Wi-Fi is a real wpa_supplicant on a
# mac80211_hwsim radio with hostapd's access points when the runner's
# kernel has the module (the "hwsim" variant: the example joins the WPA2
# network, and wifi/wpasupplicant's live test runs the driver's joins —
# passphrase, raw PSK, wrong passphrase, open network); otherwise the
# example runs with --no-wifi (the Ethernet NetworkCommissioning, reached
# over the runner's network) and the summary says so. The example's log
# must name no Wi-Fi passphrase and must show it left the radio after
# commissioning.
#
# Linux only. Needs sudo (btvirt, the vhci module, bluetoothd, the HCI user
# channel, BlueZ's D-Bus policy for the controller), BlueZ with its test
# tools (Debian/Ubuntu: bluez bluez-test-tools), Go, and Node.js in the range
# @matter/nodejs-ble declares (>=20.19 <21 or >=22.13) with npm, make and
# g++ (packages/nodejs-ble/README.md: node-gyp builds bleno and noble).
#
# The verdict separates the ENVIRONMENT (btvirt, BlueZ, the vhci module,
# Node.js, the npm install or the bleno build, the HCI claim) from a DEFECT
# (the build, the pairing, the read), as the chip-tool control leg does for
# the harness. The control leg runs after ours: a fresh peripheral, and
# matter.js's own controller (script/ble-virtual/controller.mjs, noble) on
# the central's controller, taken from BlueZ once our central is done. Ours
# failing while matter.js <-> matter.js passes is a defect; both failing is
# the virtual stack, so environment; ours passing is a pass either way (the
# summary names a failed control leg).
#
#   exit 0  pass
#   exit 1  defect      — go-fabric's side; logs in $BLE_LOG_DIR
#   exit 2  environment — nothing about go-fabric was proved
#
# The verdict is also written as Markdown to $BLE_SUMMARY when set (the
# default in a workflow is $GITHUB_STEP_SUMMARY).
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
PKG=$ROOT/script/ble-virtual
LOG_DIR=${BLE_LOG_DIR:-$(mktemp -d)}
SUMMARY=${BLE_SUMMARY:-${GITHUB_STEP_SUMMARY:-}}
NODE_ID=0x1234
mkdir -p "$LOG_DIR"

btvirt_pid=
btmon_pid=
peripheral_pid=
example_pid=
hostapd_pid=
wpa_pid=
hwsim=
notes=()
# alive PID — sudo's pid is root's, so kill -0 from the runner's user
# answers EPERM; /proc answers for every user.
alive() { [ -d "/proc/$1" ]; }
stop_peripheral() {
	[ -n "$peripheral_pid" ] || return 0
	sudo kill "$peripheral_pid" 2>/dev/null || true
	for _ in $(seq 1 50); do
		alive "$peripheral_pid" || break
		sleep 0.2
	done
	sudo pkill -KILL -f "$PKG/peripheral.mjs" 2>/dev/null || true
	peripheral_pid=
}
stop_example() {
	[ -n "$example_pid" ] || return 0
	sudo kill "$example_pid" 2>/dev/null || true
	for _ in $(seq 1 50); do
		alive "$example_pid" || break
		sleep 0.2
	done
	sudo pkill -KILL -f "$EXAMPLE_BIN" 2>/dev/null || true
	example_pid=
}
cleanup() {
	stop_peripheral
	stop_example
	if [ -n "$hostapd_pid" ]; then sudo kill "$hostapd_pid" 2>/dev/null || true; fi
	if [ -n "$wpa_pid" ]; then sudo kill "$wpa_pid" 2>/dev/null || true; fi
	if [ -n "$hwsim" ]; then sudo modprobe -r mac80211_hwsim 2>/dev/null || true; fi
	if [ -n "$btmon_pid" ]; then sudo kill "$btmon_pid" 2>/dev/null || true; fi
	if [ -n "$btvirt_pid" ]; then sudo kill "$btvirt_pid" 2>/dev/null || true; fi
	sudo chown -R "$(id -u):$(id -g)" "$LOG_DIR" 2>/dev/null || true
}
trap cleanup EXIT

version_of() { # package directory under node_modules
	sed -n 's/^  "version": "\(.*\)",$/\1/p' "$PKG/node_modules/$1/package.json" 2>/dev/null | head -1
}

verdict() { # class what
	local class=$1 what=$2 line
	case $class in
	pass) line="**pass** — $what" ;;
	environment) line="**environment** — $what (nothing about go-fabric was proved)" ;;
	defect) line="**defect** — $what (logs: $LOG_DIR)" ;;
	esac
	echo "ble-virtual: $class: $what"
	if [ -n "$SUMMARY" ]; then
		{
			echo "## BLE over btvirt"
			echo
			echo "$line"
			echo
			for n in "${notes[@]}"; do echo "- $n"; done
			echo "- matter.js: \`$(version_of @matter/main || true)\` (@matter/nodejs-ble \`$(version_of @matter/nodejs-ble || true)\`, bleno \`$(version_of @stoprocent/bleno || true)\`, noble \`$(version_of @stoprocent/noble || true)\`)"
			echo "- Node.js: \`$(node --version 2>/dev/null || echo unknown)\`"
			echo "- BlueZ: \`$(bluetoothctl --version 2>/dev/null || echo unknown)\`"
			echo "- btvirt: \`$(btvirt --version 2>/dev/null | head -1 || echo unknown)\`"
			echo "- kernel: \`$(uname -r)\`"
		} >>"$SUMMARY"
	fi
	case $class in
	pass) exit 0 ;;
	environment) exit 2 ;;
	*) exit 1 ;;
	esac
}

# hci_down HCI — the HCI user channel binds only a controller that is down
# (bluetooth-hci-socket README, "User Channel"; EBUSY otherwise); BlueZ then
# drops it. hciconfig names the controller itself: btmgmt 5.72 ignored
# --index and switched hci0 (run 39099786146).
hci_down() {
	local i
	for i in 1 2 3 4 5; do
		sudo hciconfig "$1" down >>"$LOG_DIR/hciconfig.log" 2>&1
		sleep 0.5
		hciconfig "$1" 2>/dev/null | grep -q DOWN && return 0
	done
	hciconfig "$1" >>"$LOG_DIR/hciconfig.log" 2>&1
	return 1
}

# start_peripheral TAG HCI — a fresh matter.js peripheral claiming HCI
# (hciN), waiting for its "ready manual=<code>" line; sets MANUAL. Returns 1
# when it exits or stays silent.
start_peripheral() {
	local tag=$1 hci=$2 out=$LOG_DIR/peripheral-$1.log
	mkdir -p "$LOG_DIR/storage-$tag"
	hci_down "$hci" || return 1
	sudo env HCI_CHANNEL_USER=1 BLE_HCI_ID="${hci#hci}" \
		PERIPHERAL_STORAGE="$LOG_DIR/storage-$tag" PERIPHERAL_LOGFILE="$LOG_DIR/peripheral-$tag-debug.log" \
		"$NODE_BIN" "$PKG/peripheral.mjs" >"$out" 2>&1 &
	peripheral_pid=$!
	MANUAL=
	for _ in $(seq 1 150); do
		MANUAL=$(sed -n 's/^ble-virtual-peripheral: ready manual=\([0-9]*\) .*/\1/p' "$out" | head -1)
		[ -n "$MANUAL" ] && break
		alive "$peripheral_pid" || break
		sleep 0.2
	done
	# bleno claims the controller when matter.js brings BLE up; a refused
	# claim surfaces as an unhandled error after the ready line.
	sleep 2
	if [ -z "$MANUAL" ] || ! alive "$peripheral_pid"; then
		peripheral_pid=
		return 1
	fi
}

# --- environment ------------------------------------------------------------

for tool in sudo btvirt hciconfig bluetoothctl go node npm make g++; do
	command -v "$tool" >/dev/null || verdict environment "$tool is not installed"
done
NODE_BIN=$(command -v node)
# packages/nodejs-ble/package.json at the pin: "node": ">=20.19.0 <21.0.0 || >=22.13.0"
"$NODE_BIN" -e 'const [a,b]=process.versions.node.split(".").map(Number);process.exit((a===20&&b>=19)||(a===22&&b>=13)||a>22?0:1)' ||
	verdict environment "Node.js $("$NODE_BIN" --version) is outside @matter/nodejs-ble's engines (>=20.19 <21 or >=22.13)"
sudo -n true 2>/dev/null || verdict environment "sudo without a password is not available (btvirt needs root)"

sudo modprobe hci_vhci 2>"$LOG_DIR/modprobe.log"
[ -e /dev/vhci ] || verdict environment "no /dev/vhci: the kernel has no hci_vhci module ($(uname -r); Ubuntu: linux-modules-extra-$(uname -r))"
sudo systemctl start bluetooth 2>"$LOG_DIR/bluetoothd.log"
systemctl is-active --quiet bluetooth || verdict environment "bluetoothd does not run"

before=$(ls /sys/class/bluetooth 2>/dev/null | grep -E '^hci[0-9]+$' | sort)
# -L: LE-only controllers with the LE 4.x command set. The default type
# (BREDRLE52) offers LE Extended Create Connection, which the kernel and
# noble then use, and BlueZ 5.72's emulator completes it only towards an
# extended advertising set (emulator/btdev.c cmd_ext_create_conn_complete,
# :5534-5560); bleno advertises with the legacy commands, so the connection
# never came (run 39100460695). BTDEV_TYPE_LE gets the 5.0 commands not at
# all (set_le_commands, :6759-6763).
sudo btvirt -L -l2 >"$LOG_DIR/btvirt.log" 2>&1 &
btvirt_pid=$!
controllers=
for _ in $(seq 1 50); do
	after=$(ls /sys/class/bluetooth 2>/dev/null | grep -E '^hci[0-9]+$' | sort)
	controllers=$(comm -13 <(echo "$before") <(echo "$after") | sort -V)
	[ "$(echo "$controllers" | grep -c hci)" -ge 2 ] && break
	sleep 0.2
done
[ "$(echo "$controllers" | grep -c hci)" -ge 2 ] || verdict environment "btvirt -L -l2 did not create two controllers (see btvirt.log)"
CENTRAL=$(echo "$controllers" | sed -n 1p)
PERIPHERAL=$(echo "$controllers" | sed -n 2p)
sudo hciconfig "$CENTRAL" up >>"$LOG_DIR/hciconfig.log" 2>&1 ||
	verdict environment "$CENTRAL does not power on (see hciconfig.log)"
hci_down "$PERIPHERAL" || verdict environment "$PERIPHERAL does not go down for the HCI user channel (see hciconfig.log)"
hciconfig -a >>"$LOG_DIR/hciconfig.log" 2>&1
# Every controller's HCI traffic, for reading a failure (btmon -r btmon.snoop).
if command -v btmon >/dev/null; then
	sudo btmon -w "$LOG_DIR/btmon.snoop" >/dev/null 2>&1 &
	btmon_pid=$!
fi
echo "ble-virtual: central $CENTRAL (BlueZ), peripheral $PERIPHERAL (bleno)"

(cd "$PKG" && npm ci --no-audit --no-fund) >"$LOG_DIR/npm.log" 2>&1 ||
	verdict environment "npm ci of the matter.js packages failed (see npm.log)"
# bleno and noble are optional dependencies of @matter/nodejs-ble: a failed
# node-gyp build drops them without failing the install.
for mod in @stoprocent/bleno @stoprocent/noble; do
	[ -f "$PKG/node_modules/$mod/package.json" ] ||
		verdict environment "$mod did not build (node-gyp; see npm.log)"
done

# --- the run ----------------------------------------------------------------

BIN="$(mktemp -d)/reference-controller"
(cd "$ROOT/ble/bluez" && go build -o "$BIN" ./cmd/reference-controller) >"$LOG_DIR/build.log" 2>&1 ||
	verdict defect "the BLE build does not compile (see build.log)"

start_peripheral ours "$PERIPHERAL" ||
	verdict environment "the matter.js peripheral did not come up on $PERIPHERAL (bleno build or HCI claim; see peripheral-ours.log)"
notes+=("manual code: \`$MANUAL\`")

# --- our leg: BlueZ on the central, pair --ble, read --------------------------

ctl() { sudo "$BIN" --db "$LOG_DIR/controller.db" --attestation accept --trust development --log-level info --timeout 120s "$@"; }
ours=
our_leg() {
	ctl pair --ble --ble-adapter "$CENTRAL" --code "$MANUAL" --node-id "$NODE_ID" --wifi-ssid lab --wifi-credentials secret12 >"$LOG_DIR/pair.log" 2>&1 ||
		{ ours="pair --ble failed (see pair.log)" && return; }
	grep -q 'AddOrUpdateWiFiNetwork ssid=lab' "$LOG_DIR/peripheral-ours.log" ||
		{ ours="the peripheral never saw AddOrUpdateWiFiNetwork for ssid lab (see peripheral-ours.log)" && return; }
	ctl read "$NODE_ID" 0/BasicInformation/VendorID 0/NetworkCommissioning/0xFFFC >"$LOG_DIR/read.log" 2>&1 ||
		{ ours="the read over the operational session failed after pairing (see read.log)" && return; }
	# One JSON line per attribute, named as matter.js names it (VendorId).
	grep -q '"attribute":"VendorId".*"value":65521' "$LOG_DIR/read.log" ||
		{ ours="the read returned no VendorID 0xFFF1 (see read.log)" && return; }
	# The CLI names a global attribute by its name (FeatureMap) since the
	# typed reads; the weekly run of 2026-11-09 failed on the old 0xFFFC.
	grep -q '"cluster":"NetworkCommissioning","attribute":"FeatureMap".*"value":1}' "$LOG_DIR/read.log" ||
		{ ours="NetworkCommissioning's FeatureMap is not Wi-Fi (see read.log)" && return; }
	# Bonus: an enhanced commissioning window over the same session.
	if ctl window open "$NODE_ID" --timeout 180 >"$LOG_DIR/window.log" 2>&1; then
		notes+=("window open: ok")
	else
		notes+=("window open: failed (see window.log; not part of the verdict)")
	fi
}
our_leg
stop_peripheral
notes+=("our leg: ${ours:-pass}")

# --- the control leg: matter.js's controller on the central ------------------

# noble claims the central's controller over the HCI user channel as bleno
# does the peripheral's, so BlueZ gives it up first; NOBLE_MULTI_ROLE
# (packages/nodejs-ble/README.md) is for bleno and noble on one adapter and
# not needed here.
control=
control_leg() {
	start_peripheral control "$PERIPHERAL" ||
		{ control="the peripheral did not come up a second time (see peripheral-control.log)" && return; }
	hci_down "$CENTRAL" || { control="$CENTRAL does not go down for noble (see hciconfig.log)" && return; }
	mkdir -p "$LOG_DIR/storage-control-controller"
	sudo timeout -k 10 240 env HCI_CHANNEL_USER=1 BLE_HCI_ID="${CENTRAL#hci}" \
		CONTROL_STORAGE="$LOG_DIR/storage-control-controller" CONTROL_CODE="$MANUAL" \
		CONTROL_WIFI_SSID=lab CONTROL_WIFI_CREDENTIALS=secret12 \
		"$NODE_BIN" "$PKG/controller.mjs" >"$LOG_DIR/control.log" 2>&1
	grep -q '^ble-virtual-control: pass' "$LOG_DIR/control.log" ||
		control="matter.js's controller did not commission the peripheral over BLE and read it (see control.log)"
}
control_leg
stop_peripheral
notes+=("control leg (matter.js <-> matter.js): ${control:-pass}")

# --- the second direction: examples/ble-wifi-light as the peripheral ----------

EXAMPLE_BIN="$(mktemp -d)/ble-wifi-light"
(cd "$ROOT/examples/ble-wifi-light" && go build -o "$EXAMPLE_BIN" .) >"$LOG_DIR/build-example.log" 2>&1 ||
	verdict defect "examples/ble-wifi-light does not compile (see build-example.log)"
WIFI_SSID=lab
WIFI_PASS=secret12
WIFI_OPEN_SSID=lab-open

# setup_hwsim — three mac80211_hwsim radios: a station for wpa_supplicant
# and two access points for hostapd (WPA2-PSK "lab", open "lab-open").
# Sets WIFI_IFACE; returns 1 (and the job runs --no-wifi) when the kernel
# has no module or a tool is missing.
WIFI_IFACE=
setup_hwsim() {
	command -v hostapd >/dev/null && command -v wpa_supplicant >/dev/null || {
		notes+=("Wi-Fi: hostapd or wpa_supplicant is not installed")
		return 1
	}
	local before after radios
	before=$(ls -d /sys/class/net/*/wireless 2>/dev/null | sort)
	sudo modprobe mac80211_hwsim radios=3 >>"$LOG_DIR/modprobe.log" 2>&1 || {
		notes+=("Wi-Fi: no mac80211_hwsim module in kernel $(uname -r)")
		return 1
	}
	hwsim=1
	for _ in $(seq 1 50); do
		after=$(ls -d /sys/class/net/*/wireless 2>/dev/null | sort)
		radios=$(comm -13 <(echo "$before") <(echo "$after") | sed 's|/sys/class/net/||; s|/wireless||' | sort -V)
		[ "$(echo "$radios" | grep -c .)" -ge 3 ] && break
		sleep 0.2
	done
	[ "$(echo "$radios" | grep -c .)" -ge 3 ] || {
		notes+=("Wi-Fi: mac80211_hwsim created no three radios")
		return 1
	}
	local sta ap open
	sta=$(echo "$radios" | sed -n 1p)
	ap=$(echo "$radios" | sed -n 2p)
	open=$(echo "$radios" | sed -n 3p)
	if command -v nmcli >/dev/null; then
		for i in $sta $ap $open; do sudo nmcli device set "$i" managed no >/dev/null 2>&1 || true; done
	fi
	sudo systemctl stop wpa_supplicant >/dev/null 2>&1 || true
	sudo pkill -x wpa_supplicant 2>/dev/null || true
	printf 'interface=%s\ndriver=nl80211\nssid=%s\nhw_mode=g\nchannel=1\nwpa=2\nwpa_key_mgmt=WPA-PSK\nrsn_pairwise=CCMP\nwpa_passphrase=%s\n' \
		"$ap" "$WIFI_SSID" "$WIFI_PASS" >"$LOG_DIR/hostapd-wpa2.conf"
	printf 'interface=%s\ndriver=nl80211\nssid=%s\nhw_mode=g\nchannel=6\n' "$open" "$WIFI_OPEN_SSID" >"$LOG_DIR/hostapd-open.conf"
	sudo hostapd "$LOG_DIR/hostapd-wpa2.conf" "$LOG_DIR/hostapd-open.conf" >"$LOG_DIR/hostapd.log" 2>&1 &
	hostapd_pid=$!
	# -u: the D-Bus interface the driver speaks; no interface named, the
	# driver creates it (CreateInterface). -d -t -f: a timestamped debug
	# log in the artifact (a scan's channels and its end; keys stay out of
	# it without -K, wpa_supplicant/config.c dumps them at MSGDUMP).
	sudo wpa_supplicant -u -d -t -f "$LOG_DIR/wpa_supplicant.log" -O /run/wpa_supplicant >"$LOG_DIR/wpa_supplicant.out" 2>&1 &
	wpa_pid=$!
	for _ in $(seq 1 50); do
		grep -q AP-ENABLED "$LOG_DIR/hostapd.log" 2>/dev/null &&
			busctl --system status fi.w1.wpa_supplicant1 >/dev/null 2>&1 && break
		sleep 0.2
	done
	grep -q AP-ENABLED "$LOG_DIR/hostapd.log" || {
		notes+=("Wi-Fi: hostapd did not enable its access points (see hostapd.log)")
		return 1
	}
	busctl --system status fi.w1.wpa_supplicant1 >/dev/null 2>&1 || {
		notes+=("Wi-Fi: wpa_supplicant is not on the system bus (see wpa_supplicant.log)")
		return 1
	}
	WIFI_IFACE=$sta
}
if setup_hwsim; then
	WIFI_VARIANT="hwsim (wpa_supplicant on $WIFI_IFACE, hostapd WPA2 \`$WIFI_SSID\` and open \`$WIFI_OPEN_SSID\`)"
	EXAMPLE_WIFI=(--wifi-interface "$WIFI_IFACE")
else
	WIFI_VARIANT="--no-wifi (no mac80211_hwsim radio: the Ethernet NetworkCommissioning over the runner's network)"
	EXAMPLE_WIFI=(--no-wifi)
fi
notes+=("second direction, Wi-Fi: $WIFI_VARIANT")

# start_example TAG — a fresh example node (new storage) on the second
# controller through BlueZ, waiting for its QR code; sets EXAMPLE_QR and
# EXAMPLE_MANUAL. Returns 1 when it exits or stays silent.
start_example() {
	local tag=$1 out=$LOG_DIR/example-$1.log
	sudo hciconfig "$PERIPHERAL" up >>"$LOG_DIR/hciconfig.log" 2>&1 || return 1
	sudo "$EXAMPLE_BIN" --adapter "$PERIPHERAL" "${EXAMPLE_WIFI[@]}" --storage "$LOG_DIR/example-$tag.db" --log-level debug >"$out" 2>&1 &
	example_pid=$!
	EXAMPLE_QR= EXAMPLE_MANUAL=
	for _ in $(seq 1 150); do
		EXAMPLE_QR=$(sed -n 's/^QR code: \(MT:[^ ]*\)$/\1/p' "$out" | head -1)
		EXAMPLE_MANUAL=$(sed -n 's/^Manual code: \([0-9]*\)$/\1/p' "$out" | head -1)
		[ -n "$EXAMPLE_QR" ] && [ -n "$EXAMPLE_MANUAL" ] && grep -q 'matter.ble.serving' "$out" && break
		alive "$example_pid" || break
		sleep 0.2
	done
	if [ -z "$EXAMPLE_QR" ] || ! alive "$example_pid" || ! grep -q 'matter.ble.serving' "$out"; then
		example_pid=
		return 1
	fi
}

# example_checks TAG — what the example's log must show after a
# commissioning: it left the radio once CommissioningComplete committed
# (matter.ble.stopped after the central's disconnect, 28a), and no line
# names the Wi-Fi passphrase. Prints the failure; returns 1 on one.
example_checks() {
	local out=$LOG_DIR/example-$1.log
	grep -q 'matter.ble.stopped' "$out" || { echo "the example did not leave the radio after commissioning (no matter.ble.stopped in example-$1.log)" && return 1; }
	if grep -q -- "$WIFI_PASS" "$out"; then echo "the example's log names the Wi-Fi passphrase (example-$1.log)" && return 1; fi
	if [ -n "$WIFI_IFACE" ] && ! grep -q 'wpasupplicant: connected' "$out"; then
		echo "the example did not join $WIFI_SSID over wpa_supplicant (example-$1.log)" && return 1
	fi
}

# The example's leg with our central: pair --ble with the QR code (the
# manual code names no discovery capability), then the NetworkCommissioning
# proof over the operational session.
NODE_ID2=0x1235
example_ours=
example_ours_leg() {
	sudo hciconfig "$CENTRAL" up >>"$LOG_DIR/hciconfig.log" 2>&1 ||
		{ example_ours="$CENTRAL does not come back up for BlueZ (see hciconfig.log)" && return; }
	start_example ours ||
		{ example_ours="the example did not come up on $PERIPHERAL (see example-ours.log)" && return; }
	ctl pair --ble --ble-adapter "$CENTRAL" --code "$EXAMPLE_QR" --node-id "$NODE_ID2" --wifi-ssid "$WIFI_SSID" --wifi-credentials "$WIFI_PASS" >"$LOG_DIR/pair-example.log" 2>&1 ||
		{ example_ours="pair --ble did not commission the example (see pair-example.log, example-ours.log)" && return; }
	ctl read "$NODE_ID2" 0/NetworkCommissioning/0xFFFC 0/NetworkCommissioning/Networks 0/NetworkCommissioning/LastNetworkingStatus >"$LOG_DIR/read-example.log" 2>&1 ||
		{ example_ours="the read over the operational session failed (see read-example.log)" && return; }
	# The light is the Aggregator's first bridged endpoint: 2 (endpoint 1
	# is the Aggregator).
	ctl invoke "$NODE_ID2" 2/OnOff/Toggle >"$LOG_DIR/invoke-example.log" 2>&1 ||
		{ example_ours="the OnOff toggle failed (see invoke-example.log)" && return; }
	grep -q 'msg=light on=true' "$LOG_DIR/example-ours.log" ||
		{ example_ours="the toggle did not switch the light on (see example-ours.log)" && return; }
	if [ -n "$WIFI_IFACE" ]; then
		grep -q '"attribute":"FeatureMap".*"value":1}' "$LOG_DIR/read-example.log" ||
			{ example_ours="NetworkCommissioning's FeatureMap is not Wi-Fi (see read-example.log)" && return; }
		grep -q '"attribute":"Networks".*"connected":true' "$LOG_DIR/read-example.log" ||
			{ example_ours="Networks names no connected network (see read-example.log)" && return; }
		grep -q '"attribute":"LastNetworkingStatus".*"value":0}' "$LOG_DIR/read-example.log" ||
			{ example_ours="LastNetworkingStatus is not Success (see read-example.log)" && return; }
	fi
	local why
	why=$(example_checks ours) || { example_ours=$why && return; }
	stop_example
}
example_ours_leg
stop_example
notes+=("second direction, pair --ble -> example: ${example_ours:-pass}")

# The example's leg with matter.js's controller (noble on the central's
# controller, taken from BlueZ).
example_control=
example_control_leg() {
	start_example control ||
		{ example_control="the example did not come up a second time (see example-control.log)" && return; }
	hci_down "$CENTRAL" || { example_control="$CENTRAL does not go down for noble (see hciconfig.log)" && return; }
	mkdir -p "$LOG_DIR/storage-example-controller"
	sudo timeout -k 10 240 env HCI_CHANNEL_USER=1 BLE_HCI_ID="${CENTRAL#hci}" \
		CONTROL_STORAGE="$LOG_DIR/storage-example-controller" CONTROL_CODE="$EXAMPLE_MANUAL" \
		CONTROL_WIFI_SSID="$WIFI_SSID" CONTROL_WIFI_CREDENTIALS="$WIFI_PASS" \
		"$NODE_BIN" "$PKG/controller.mjs" >"$LOG_DIR/control-example.log" 2>&1
	grep -q '^ble-virtual-control: pass' "$LOG_DIR/control-example.log" ||
		{ example_control="matter.js's controller did not commission the example over BLE and read it (see control-example.log, example-control.log)" && return; }
	local why
	why=$(example_checks control) || { example_control=$why && return; }
}
example_control_leg
stop_example
notes+=("second direction, matter.js controller -> example: ${example_control:-pass}")

# The driver against the real wpa_supplicant (hwsim variant only).
wifi_live=
if [ -n "$WIFI_IFACE" ]; then
	LIVE_BIN="$(mktemp -d)/wpasupplicant.test"
	if (cd "$ROOT/wifi/wpasupplicant" && go test -c -o "$LIVE_BIN" .) >"$LOG_DIR/build-wpasupplicant.log" 2>&1; then
		sudo env GOFABRIC_WPA_LIVE_IFACE="$WIFI_IFACE" GOFABRIC_WPA_LIVE_SSID="$WIFI_SSID" \
			GOFABRIC_WPA_LIVE_PASSPHRASE="$WIFI_PASS" GOFABRIC_WPA_LIVE_OPEN_SSID="$WIFI_OPEN_SSID" \
			"$LIVE_BIN" -test.run TestLiveWPASupplicant -test.v >"$LOG_DIR/wpasupplicant-live.log" 2>&1 ||
			wifi_live="the driver's live test failed against wpa_supplicant (see wpasupplicant-live.log)"
		grep -q -- '--- SKIP: TestLiveWPASupplicant ' "$LOG_DIR/wpasupplicant-live.log" &&
			wifi_live="the driver's live test skipped (see wpasupplicant-live.log)"
		if [ -z "$wifi_live" ] && grep -q -- "$WIFI_PASS" "$LOG_DIR/wpasupplicant-live.log"; then
			wifi_live="the driver's live test output names the passphrase"
		fi
	else
		wifi_live="wifi/wpasupplicant's tests do not compile (see build-wpasupplicant.log)"
	fi
	notes+=("wpa_supplicant live test (passphrase, raw PSK, wrong passphrase, open network): ${wifi_live:-pass}")
fi

# --- the verdict -------------------------------------------------------------

# Every leg ran whatever an earlier one did; each result is printed before
# the verdict, so one run reports all of them (the summary lists them too).
for n in "${notes[@]}"; do echo "ble-virtual: $n"; done

# The second direction's verdict comes first: a failure of the example
# under both controllers, while matter.js <-> matter.js passed over the
# same virtual stack, is ours; with that control failing too it is the
# stack's. The Wi-Fi driver's live test is a verdict of its own: its
# failure says nothing about BLE.
second="${example_ours:+pair --ble -> example: $example_ours; }${example_control:+matter.js -> example: $example_control}"
if [ -z "$second" ] && [ -n "$wifi_live" ]; then
	verdict defect "every BLE leg passed; the Wi-Fi driver's live test failed against the real wpa_supplicant: $wifi_live"
fi
if [ -n "$second" ]; then
	if [ -z "$control" ]; then
		verdict defect "the second direction failed — $second — while matter.js's controller commissioned matter.js's peripheral over the same virtual stack"
	elif [ -z "$example_ours" ] || [ -z "$example_control" ]; then
		verdict defect "the second direction failed — $second — while the example was commissioned by the other controller"
	fi
	verdict environment "the second direction failed — $second — and so did the control leg ($control): the virtual stack"
fi

what="pair --ble over $CENTRAL commissioned the matter.js peripheral on $PERIPHERAL with Wi-Fi credentials; read over the operational session; examples/ble-wifi-light on $PERIPHERAL through BlueZ was commissioned by pair --ble and by matter.js's controller (Wi-Fi: $WIFI_VARIANT)"
if [ -z "$ours" ] && [ -z "$control" ]; then
	verdict pass "$what; the control leg passed too"
elif [ -z "$ours" ]; then
	verdict pass "$what (the control leg failed: $control)"
elif [ -z "$control" ]; then
	verdict defect "$ours — matter.js's own controller commissioned the same peripheral over the same virtual stack"
else
	verdict environment "both legs failed — ours: $ours; control: $control — the virtual stack"
fi
