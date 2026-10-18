#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# BLE end to end over virtual HCI controllers (ADR 0024 decision 5): two
# btvirt controllers on one BlueZ, connectedhomeip's chip-all-clusters-app
# from the pinned CHIP image advertising on one, the reference controller's
# BLE build (ble/bluez/cmd/reference-controller) pairing it over the other,
# then a read over CASE. `make ble-virtual-test` and
# .github/workflows/ble-virtual.yml run this script.
#
# Linux only. Needs sudo (btvirt, the vhci module, bluetoothd, BlueZ's
# D-Bus policy for the controller), Docker, avahi-daemon, and BlueZ with
# its test tools (Debian/Ubuntu: bluez bluez-tests).
#
# The verdict separates the ENVIRONMENT (btvirt, BlueZ, the vhci module,
# the image, its app built without BLE) from a DEFECT (the build, the
# pairing, the read), as the chip-tool control leg does for the harness:
#
#   exit 0  pass
#   exit 1  defect      — go-fabric's side or the app's; logs in $BLE_LOG_DIR
#   exit 2  environment — nothing about go-fabric was proved
#
# The verdict is also written as Markdown to $BLE_SUMMARY when set (the
# default in a workflow is $GITHUB_STEP_SUMMARY).
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
IMAGE=${CHIP_TEST_IMAGE:-$(sed -n 's/^CHIP_TEST_IMAGE[[:space:]]*?=[[:space:]]*//p' "$ROOT/Makefile")}
LOG_DIR=${BLE_LOG_DIR:-$(mktemp -d)}
SUMMARY=${BLE_SUMMARY:-${GITHUB_STEP_SUMMARY:-}}
# The all-clusters app's default onboarding: passcode 20202021,
# discriminator 3840.
QR=MT:-24J0AFN00KA0648G00
NODE=0x1234
APP=ble-virtual-app
mkdir -p "$LOG_DIR"

btvirt_pid=
cleanup() {
	docker logs "$APP" >"$LOG_DIR/app.log" 2>&1 || true
	docker rm -f "$APP" >/dev/null 2>&1 || true
	if [ -n "$btvirt_pid" ]; then sudo kill "$btvirt_pid" 2>/dev/null || true; fi
}
trap cleanup EXIT

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
			echo "- image: \`$IMAGE\`"
			echo "- BlueZ: \`$(bluetoothctl --version 2>/dev/null || echo unknown)\`"
			echo "- kernel: \`$(uname -r)\`"
		} >>"$SUMMARY"
	fi
	case $class in
	pass) exit 0 ;;
	environment) exit 2 ;;
	*) exit 1 ;;
	esac
}

# --- environment ------------------------------------------------------------

for tool in sudo docker btvirt bluetoothctl go; do
	command -v "$tool" >/dev/null || verdict environment "$tool is not installed"
done
sudo -n true 2>/dev/null || verdict environment "sudo without a password is not available (btvirt needs root)"
systemctl is-active --quiet avahi-daemon || verdict environment "avahi-daemon is not running (the CHIP apps advertise over it)"

sudo modprobe hci_vhci 2>"$LOG_DIR/modprobe.log"
[ -e /dev/vhci ] || verdict environment "no /dev/vhci: the kernel has no hci_vhci module ($(uname -r))"
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
for hci in "$CENTRAL" "$PERIPHERAL"; do
	sudo btmgmt --index "${hci#hci}" power on >>"$LOG_DIR/btmgmt.log" 2>&1 ||
		verdict environment "$hci does not power on (see btmgmt.log)"
done
echo "ble-virtual: central $CENTRAL, peripheral $PERIPHERAL"

docker pull -q "$IMAGE" >"$LOG_DIR/pull.log" 2>&1 || verdict environment "the CHIP image does not pull"
# chip's Linux apps take --ble-controller only when built with CHIPoBLE
# (examples/platform/linux/Options.cpp:170-172 at the harness pin,
# CHIP_DEVICE_CONFIG_ENABLE_CHIPOBLE). Without it there is no BLE peer.
docker run --rm --entrypoint /usr/bin/chip-all-clusters-app "$IMAGE" --help >"$LOG_DIR/app-help.log" 2>&1
grep -q -- '--ble-controller' "$LOG_DIR/app-help.log" ||
	verdict environment "the pinned image's chip-all-clusters-app is built without BLE (no --ble-controller in its --help)"

# --- the run ----------------------------------------------------------------

BIN="$(mktemp -d)/reference-controller"
(cd "$ROOT/ble/bluez" && go build -o "$BIN" ./cmd/reference-controller) >"$LOG_DIR/build.log" 2>&1 ||
	verdict defect "the BLE build does not compile (see build.log)"

# The container reaches the host's bluetoothd and avahi over /run/dbus,
# as the harness reaches avahi; privileged for the HCI socket.
docker run -d --name "$APP" --network host --privileged --security-opt apparmor=unconfined \
	-v /run/dbus:/run/dbus --entrypoint /usr/bin/chip-all-clusters-app "$IMAGE" \
	--ble-controller "${PERIPHERAL#hci}" --KVS /tmp/kvs-app >/dev/null 2>&1 ||
	verdict environment "the all-clusters app does not start"
sleep 5
docker ps -q --filter "name=^$APP$" | grep -q . || verdict defect "the all-clusters app exited at start (see app.log)"

# The app serves NetworkCommissioning over Ethernet on the runner; the
# Wi-Fi flags are what pair --ble requires, and the flow skips the network
# steps for a device that has neither Wi-Fi nor Thread.
ctl() { sudo "$BIN" --db "$LOG_DIR/controller.db" --attestation accept --trust development --log-level info --timeout 120s "$@"; }
ctl pair --ble --ble-adapter "$CENTRAL" --code "$QR" --node-id "$NODE" --wifi-ssid lab --wifi-credentials secret12 >"$LOG_DIR/pair.log" 2>&1 ||
	verdict defect "pair --ble failed (see pair.log)"
ctl read "$NODE" 0/BasicInformation/VendorID 0/NetworkCommissioning/FeatureMap >"$LOG_DIR/read.log" 2>&1 ||
	verdict defect "the read over CASE failed after pairing (see read.log)"
grep -q 'VendorID' "$LOG_DIR/read.log" || verdict defect "the read returned no VendorID (see read.log)"
verdict pass "pair --ble over $CENTRAL commissioned the all-clusters app on $PERIPHERAL; read over CASE"
