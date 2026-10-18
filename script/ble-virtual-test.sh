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
cleanup() {
	stop_peripheral
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
sudo btvirt -l2 >"$LOG_DIR/btvirt.log" 2>&1 &
btvirt_pid=$!
controllers=
for _ in $(seq 1 50); do
	after=$(ls /sys/class/bluetooth 2>/dev/null | grep -E '^hci[0-9]+$' | sort)
	controllers=$(comm -13 <(echo "$before") <(echo "$after") | sort -V)
	[ "$(echo "$controllers" | grep -c hci)" -ge 2 ] && break
	sleep 0.2
done
[ "$(echo "$controllers" | grep -c hci)" -ge 2 ] || verdict environment "btvirt -l2 did not create two controllers (see btvirt.log)"
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
	ctl read "$NODE_ID" 0/BasicInformation/VendorID 0/NetworkCommissioning/FeatureMap >"$LOG_DIR/read.log" 2>&1 ||
		{ ours="the read over the operational session failed after pairing (see read.log)" && return; }
	grep -q 'VendorID' "$LOG_DIR/read.log" || { ours="the read returned no VendorID (see read.log)" && return; }
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

# --- the verdict -------------------------------------------------------------

what="pair --ble over $CENTRAL commissioned the matter.js peripheral on $PERIPHERAL with Wi-Fi credentials; read over the operational session"
if [ -z "$ours" ] && [ -z "$control" ]; then
	verdict pass "$what; the control leg passed too"
elif [ -z "$ours" ]; then
	verdict pass "$what (the control leg failed: $control)"
elif [ -z "$control" ]; then
	verdict defect "$ours — matter.js's own controller commissioned the same peripheral over the same virtual stack"
else
	verdict environment "both legs failed — ours: $ours; control: $control — the virtual stack"
fi
