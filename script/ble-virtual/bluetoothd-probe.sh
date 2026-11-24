#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# bluetoothd probe (issue #610 row 1, decision 574; phase 8 track L0). It
# measures what bluetoothd sends on the system bus, and whether a GATT
# application and an LE advertisement registered by a bus client stay
# registered, around five events:
#
#   1  Adapter1.Powered false -> true (set over D-Bus), and the same
#      through the kernel (hciconfig down / up, as script/ble-virtual-test.sh
#      takes a controller from BlueZ)
#   2  bluetoothd stop / start (systemctl, as ble-virtual-test.sh starts it)
#   3  controller remove / add (the btvirt process killed and started again,
#      the invocation of ble-virtual-test.sh)
#   4  the registering client's bus name leaving the bus (SIGKILL, so no
#      Unregister call is made)
#   5  a connected central's bus name leaving the bus (Device1.Connect by a
#      client, then SIGKILL of that client; issue #610 row 6)
#
# It measures; it does not judge. The record is:
#   record.txt         every scenario in its own section: the commands with
#                      their epoch timestamps and output, busctl snapshots
#                      before and after, the client's log lines of the
#                      window, and the dbus-monitor messages between the
#                      scenario's BEGIN and END markers
#   dbus-monitor.log   the whole unfiltered `dbus-monitor --system` stream;
#                      markers are signals member=Section on
#                      /org/gofabric/probe/marker carrying "BEGIN|END <n>"
#   bluetoothd.journal journalctl -u bluetooth for the run
#   client-*.log       the probe clients (script/ble-virtual/probe_client.py)
#   tools.txt          versions and the --help text of every tool used, the
#                      source of each flag
#
# "Survives" is measured by the client calling RegisterApplication and
# RegisterAdvertisement again with the same object paths from the same bus
# name after the event and recording bluetoothd's reply verbatim, beside
# LEAdvertisingManager1.ActiveInstances. The reading of the reply is left
# to whoever judges the record.
#
# Linux only, needs sudo, hci_vhci, bluez + bluez-test-tools (btvirt), and
# python3 with the dbus and gi modules (python3-dbus, python3-gi).
# .github/workflows/ble-virtual.yml runs it on workflow_dispatch with
# probe=true. Exit 0 when every scenario ran; exit 2 when the environment
# (vhci, bluetoothd, btvirt, python modules) is missing.
# The redirects are the runner user's on purpose (the record stays readable
# without sudo), and /sys/class/bluetooth holds only hciN names.
# shellcheck disable=SC2024,SC2010,SC2001
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
CLIENT=$ROOT/script/ble-virtual/probe_client.py
LOG_DIR=${BLE_LOG_DIR:-$(mktemp -d)}
mkdir -p "$LOG_DIR"
REC=$LOG_DIR/record.txt
STEPS=$LOG_DIR/steps.log
MON=$LOG_DIR/dbus-monitor.log
ADAPTER_FILE=$LOG_DIR/adapter
PY=/usr/bin/python3
: >"$REC"
: >"$STEPS"

btvirt_pid=
monitor_pid=
periph_pid=
central_pid=

cleanup() {
	sudo pkill -KILL -f "^$PY -u $CLIENT" 2>/dev/null || true
	for p in $central_pid $periph_pid $monitor_pid $btvirt_pid; do
		sudo kill "$p" 2>/dev/null || true
	done
	sudo chown -R "$(id -u):$(id -g)" "$LOG_DIR" 2>/dev/null || true
}
trap cleanup EXIT

now() { date +%s.%N | cut -c1-17; }
note() { echo "$(now) # $*" | tee -a "$STEPS"; }
# run CMD... — runs a command, records the timestamp, the command, its
# output and its exit status in steps.log.
run() {
	local out rc
	echo "$(now) \$ $*" >>"$STEPS"
	out=$("$@" 2>&1)
	rc=$?
	[ -n "$out" ] && sed 's/^/    /' <<<"$out" >>"$STEPS"
	echo "    [exit $rc]" >>"$STEPS"
	return $rc
}
env_fail() {
	note "ENVIRONMENT: $*"
	echo "bluetoothd-probe: environment: $*"
	exit 2
}

# marker LABEL — a signal through the bus daemon, so the monitor stream
# carries the section boundary in order with bluetoothd's own messages.
marker() {
	note "marker $1"
	sudo dbus-send --system --type=signal /org/gofabric/probe/marker org.gofabric.Probe.Section "string:$1" 2>>"$STEPS"
	sleep 0.3
}

# snapshot LABEL — busctl's view of org.bluez: the object tree, the adapter
# objects' interfaces and the properties the scenarios move.
snapshot() {
	local a
	note "snapshot $1"
	run sudo busctl --no-pager tree org.bluez
	for a in $(sudo busctl --no-pager tree --list org.bluez 2>/dev/null | grep -E '^/org/bluez/hci[0-9]+$'); do
		run sudo busctl --no-pager introspect org.bluez "$a"
		run sudo busctl --no-pager get-property org.bluez "$a" org.bluez.Adapter1 Powered
		run sudo busctl --no-pager get-property org.bluez "$a" org.bluez.LEAdvertisingManager1 ActiveInstances
	done
}

# controllers — the hciN in /sys/class/bluetooth, as ble-virtual-test.sh
# lists them.
controllers() { ls /sys/class/bluetooth 2>/dev/null | grep -E '^hci[0-9]+$' | sort -V; }

start_btvirt() {
	local before after i
	before=$(controllers)
	# The invocation of script/ble-virtual-test.sh:212.
	echo "$(now) \$ sudo btvirt -L -l2 &" >>"$STEPS"
	sudo btvirt -L -l2 >>"$LOG_DIR/btvirt.log" 2>&1 &
	btvirt_pid=$!
	NEW=
	for i in $(seq 1 50); do
		after=$(controllers)
		NEW=$(comm -13 <(echo "$before") <(echo "$after") | sort -V)
		[ "$(echo "$NEW" | grep -c hci)" -ge 2 ] && break
		sleep 0.2
	done
	note "btvirt pid $btvirt_pid, new controllers: $(echo "$NEW" | tr '\n' ' ') (after ${i} polls)"
	[ "$(echo "$NEW" | grep -c hci)" -ge 2 ]
}

# wait_adapter PATH — until bluetoothd exports PATH (up to 10 s).
wait_adapter() {
	local i
	for i in $(seq 1 50); do
		sudo busctl --no-pager get-property org.bluez "$1" org.bluez.Adapter1 Powered >/dev/null 2>&1 && {
			note "$1 exported after $i polls"
			return 0
		}
		sleep 0.2
	done
	note "$1 not exported after 10 s"
	return 1
}

set_powered() { # PATH true|false
	run sudo busctl --no-pager set-property org.bluez "$1" org.bluez.Adapter1 Powered b "$2"
}

# reregister — SIGUSR1 to the peripheral client: it calls both Register
# methods again from the same bus name and logs the replies.
# The signal goes to the python process itself, found by its command line
# (as ble-virtual-test.sh:85 finds peripheral.mjs): sudo's own process has
# the same arguments behind "sudo", and SIGKILL is not relayed by sudo.
client_pid() { pgrep -f "^$PY -u $CLIENT $1" | sed -n 1p; }
reregister() {
	note "SIGUSR1 to the peripheral client (re-register, same paths, same bus name)"
	run sudo kill -USR1 "$(client_pid peripheral)"
	sleep 3
}

# section N TITLE — a section of record.txt: the step lines, the client
# lines and the monitor lines between this section's markers, filled at the
# end (the monitor stream is only complete once dbus-monitor stopped).
SECTIONS=()
begin() {
	SECTIONS+=("$1|$2|$(wc -l <"$STEPS")|$(now)")
	marker "BEGIN $1"
}
end() {
	marker "END $1"
	SECTIONS[${#SECTIONS[@]} - 1]+="|$(wc -l <"$STEPS")|$(now)"
}

# --- environment ------------------------------------------------------------

{
	echo "# tools (the source of every flag this probe passes)"
	echo "## uname"; uname -a
	echo "## bluetoothd"; /usr/libexec/bluetooth/bluetoothd --version 2>&1 || /usr/lib/bluetooth/bluetoothd --version 2>&1
	echo "## bluetoothctl --version"; bluetoothctl --version 2>&1
	echo "## btvirt --help"; btvirt --help 2>&1
	echo "## dbus-monitor --help"; dbus-monitor --help 2>&1
	echo "## dbus-send --help"; dbus-send --help 2>&1
	echo "## busctl --version"; busctl --version 2>&1
	echo "## busctl --help"; busctl --help 2>&1
	echo "## hciconfig --help"; hciconfig --help 2>&1
	echo "## journalctl --version"; journalctl --version 2>&1
	echo "## python3"; "$PY" --version 2>&1
	"$PY" -c 'import dbus, gi; print("dbus", dbus.__version__, "gi", gi.__version__)' 2>&1
	echo "## systemctl cat bluetooth"; systemctl cat bluetooth 2>&1
} >"$LOG_DIR/tools.txt"

for tool in sudo btvirt hciconfig busctl dbus-monitor dbus-send journalctl; do
	command -v "$tool" >/dev/null || env_fail "$tool is not installed"
done
"$PY" -c 'import dbus, dbus.mainloop.glib; from gi.repository import GLib' 2>>"$STEPS" ||
	env_fail "python3 has no dbus / gi module (python3-dbus, python3-gi)"
sudo -n true 2>/dev/null || env_fail "sudo without a password is not available"

# script/ble-virtual-test.sh:199-202.
run sudo modprobe hci_vhci
[ -e /dev/vhci ] || env_fail "no /dev/vhci ($(uname -r))"
run sudo systemctl start bluetooth
systemctl is-active --quiet bluetooth || env_fail "bluetoothd does not run"
JOURNAL_SINCE=$(date '+%Y-%m-%d %H:%M:%S')

# The monitor: unfiltered, so no match rule depends on which unique name
# owns org.bluez at a given moment (scenario 2 changes it).
echo "$(now) \$ sudo dbus-monitor --system > dbus-monitor.log &" >>"$STEPS"
sudo dbus-monitor --system >"$MON" 2>&1 &
monitor_pid=$!
sleep 1
grep -q . "$MON" || env_fail "dbus-monitor --system wrote nothing (see dbus-monitor.log)"

start_btvirt || env_fail "btvirt -L -l2 did not create two controllers (see btvirt.log)"
CENTRAL=$(echo "$NEW" | sed -n 1p)
PERIPHERAL=$(echo "$NEW" | sed -n 2p)
note "central $CENTRAL, peripheral $PERIPHERAL"
wait_adapter "/org/bluez/$CENTRAL" || env_fail "bluetoothd does not export $CENTRAL"
wait_adapter "/org/bluez/$PERIPHERAL" || env_fail "bluetoothd does not export $PERIPHERAL"
set_powered "/org/bluez/$CENTRAL" true
set_powered "/org/bluez/$PERIPHERAL" true
echo "/org/bluez/$PERIPHERAL" >"$ADAPTER_FILE"

begin 0 "setup: the peripheral client registers application and advertisement on /org/bluez/$PERIPHERAL"
snapshot "0 before"
echo "$(now) \$ sudo $PY -u $CLIENT peripheral $ADAPTER_FILE > client-peripheral.log &" >>"$STEPS"
sudo "$PY" -u "$CLIENT" peripheral "$ADAPTER_FILE" >"$LOG_DIR/client-peripheral.log" 2>&1 &
periph_pid=$!
for _ in $(seq 1 50); do
	grep -q 'probe-client: ready' "$LOG_DIR/client-peripheral.log" && break
	sleep 0.2
done
sleep 2
snapshot "0 after"
end 0
grep -q 'probe-client: ready' "$LOG_DIR/client-peripheral.log" || env_fail "the peripheral client did not register (see client-peripheral.log)"

# --- 1: Powered ---------------------------------------------------------------

begin 1a "Adapter1.Powered false -> true over D-Bus on /org/bluez/$PERIPHERAL"
snapshot "1a before"
set_powered "/org/bluez/$PERIPHERAL" false
sleep 3
snapshot "1a powered off"
set_powered "/org/bluez/$PERIPHERAL" true
sleep 3
snapshot "1a powered on"
reregister
snapshot "1a after re-register"
end 1a

begin 1b "kernel: hciconfig $PERIPHERAL down -> up (ble-virtual-test.sh:153, :224)"
snapshot "1b before"
run sudo hciconfig "$PERIPHERAL" down
sleep 3
snapshot "1b down"
run sudo hciconfig "$PERIPHERAL" up
sleep 3
snapshot "1b up"
reregister
snapshot "1b after re-register"
end 1b

# --- 2: bluetoothd restart ------------------------------------------------------

begin 2 "bluetoothd stop -> start (systemctl)"
snapshot "2 before"
run sudo systemctl stop bluetooth
sleep 3
run systemctl is-active bluetooth
run sudo systemctl start bluetooth
run systemctl is-active bluetooth
wait_adapter "/org/bluez/$PERIPHERAL"
sleep 3
snapshot "2 restarted"
reregister
snapshot "2 after re-register"
end 2

# --- 3: controller remove / add -------------------------------------------------

begin 3 "controller remove -> add (btvirt killed, started again)"
snapshot "3 before"
run sudo kill "$btvirt_pid"
for _ in $(seq 1 50); do
	controllers | grep -qx "$PERIPHERAL" || break
	sleep 0.2
done
run ls /sys/class/bluetooth
sleep 3
snapshot "3 removed"
OLD_PERIPHERAL=$PERIPHERAL
if start_btvirt; then
	CENTRAL=$(echo "$NEW" | sed -n 1p)
	PERIPHERAL=$(echo "$NEW" | sed -n 2p)
	note "after re-add: central $CENTRAL, peripheral $PERIPHERAL (was $OLD_PERIPHERAL)"
	wait_adapter "/org/bluez/$PERIPHERAL"
	wait_adapter "/org/bluez/$CENTRAL"
	sleep 3
	snapshot "3 added"
	set_powered "/org/bluez/$PERIPHERAL" true
	set_powered "/org/bluez/$CENTRAL" true
	sleep 2
	echo "/org/bluez/$PERIPHERAL" >"$ADAPTER_FILE"
	reregister
	snapshot "3 after re-register"
else
	note "btvirt did not come back with two controllers"
fi
end 3

# --- 4: the registering client's bus name leaves ---------------------------------

begin 4 "the peripheral client's bus name leaves (SIGKILL, no Unregister)"
snapshot "4 before"
run grep unique-name "$LOG_DIR/client-peripheral.log"
run sudo kill -KILL "$(client_pid peripheral)"
sudo kill "$periph_pid" 2>/dev/null
periph_pid=
sleep 3
snapshot "4 after"
end 4

# --- 5: a connected central's bus name leaves -----------------------------------

begin 5 "a central client connects $CENTRAL -> $PERIPHERAL, then its bus name leaves (SIGKILL)"
# A fresh peripheral client so the peripheral advertises again.
echo "$(now) \$ sudo $PY -u $CLIENT peripheral $ADAPTER_FILE > client-peripheral2.log &" >>"$STEPS"
sudo "$PY" -u "$CLIENT" peripheral "$ADAPTER_FILE" >"$LOG_DIR/client-peripheral2.log" 2>&1 &
periph_pid=$!
sleep 3
PEER=$(sudo busctl --no-pager get-property org.bluez "/org/bluez/$PERIPHERAL" org.bluez.Adapter1 Address 2>/dev/null | sed -n 's/^s "\(.*\)"$/\1/p')
note "peripheral address $PEER"
snapshot "5 before"
echo "$(now) \$ sudo $PY -u $CLIENT central /org/bluez/$CENTRAL $PEER > client-central.log &" >>"$STEPS"
sudo "$PY" -u "$CLIENT" central "/org/bluez/$CENTRAL" "$PEER" >"$LOG_DIR/client-central.log" 2>&1 &
central_pid=$!
connected=
for _ in $(seq 1 300); do
	grep -q 'probe-client: connected' "$LOG_DIR/client-central.log" && connected=1 && break
	[ -n "$(client_pid central)" ] || break
	sleep 0.2
done
if [ -n "$connected" ]; then
	DEV="/org/bluez/$CENTRAL/dev_${PEER//:/_}"
	sleep 2
	run sudo busctl --no-pager get-property org.bluez "$DEV" org.bluez.Device1 Connected
	snapshot "5 connected"
	run sudo kill -KILL "$(client_pid central)"
	sudo kill "$central_pid" 2>/dev/null
	central_pid=
	for t in 1 2 4 8; do
		sleep "$t"
		note "${t} s more after the central's bus name left"
		run sudo busctl --no-pager get-property org.bluez "$DEV" org.bluez.Device1 Connected
	done
	snapshot "5 after"
else
	note "the central did not connect (see client-central.log); scenario 5 not measured"
fi
end 5

# --- record ----------------------------------------------------------------------

sleep 1
sudo kill "$monitor_pid" 2>/dev/null
monitor_pid=
sleep 1
sudo journalctl -u bluetooth --since "$JOURNAL_SINCE" -o short-precise --no-pager >"$LOG_DIR/bluetoothd.journal" 2>&1

{
	echo "bluetoothd probe record (issue #610 row 1) — script/ble-virtual/bluetoothd-probe.sh"
	echo "kernel $(uname -r); bluetoothd $(grep -A1 '^## bluetoothd' "$LOG_DIR/tools.txt" | sed -n 2p)"
	echo "timestamps: epoch seconds (dbus-monitor time= uses the same clock)"
	echo
	for s in "${SECTIONS[@]}"; do
		IFS='|' read -r id title from t0 to t1 <<<"$s"
		echo "=================================================================="
		echo "SCENARIO $id: $title"
		echo "=================================================================="
		echo "--- commands (steps.log lines $((from + 1))-$to) ---"
		sed -n "$((from + 1)),${to}p" "$STEPS"
		echo "--- probe client lines in the window ---"
		cat "$LOG_DIR"/client-*.log 2>/dev/null | awk -v a="$t0" -v b="$t1" '$1+0 >= a+0 && $1+0 <= b+0' | sort -n
		echo "--- dbus-monitor between the markers BEGIN $id and END $id ---"
		awk -v b="BEGIN $id" -v e="END $id" '
			/member=Section$/ { sec=1; next }
			sec==1 { sec=0; if (index($0, "\"" b "\"")) { on=1; next } if (index($0, "\"" e "\"")) { on=0 } ; next }
			on { print }
		' "$MON"
		echo
	done
} >"$REC"

note "done"
echo "bluetoothd-probe: record in $REC"
