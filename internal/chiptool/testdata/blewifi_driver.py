# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
"""The BLE-Wi-Fi leg's orchestration inside the harness image (wave 28c).

Run by internal/chiptool/blewifi_test.go in a privileged container of the
harness image. It sets up connectedhomeip's own test environment for a
BLE-Wi-Fi commissioning, unchanged and imported from the image, as chip's
runner does for --commissioning-method ble-wifi
(scripts/tests/chiptest/linux.py:99-116 at 6170af84): the network
namespaces with the app's link down until the Wi-Fi mock associates it
(IsolatedNetworkNamespace, app_link_up=False, app link "wlx-app"), a test
system bus (DBusTestSystemBus), bluezoo with two adapters (BluetoothMock;
the app takes adapter 0, the tool adapter 1,
src/python_testing/matter_testing_infrastructure/matter/testing/commissioning_types.py:88-93)
and wpa_supplicant's mock with the network "MatterAP"/"MatterAPPassword"
(WpaSupplicantMock). The app runs in the app namespace, chip-tool-ble in
the tool namespace: `pairing ble-wifi` with --ble-controller 1
(scripts/tests/chiptest/test_definition.py:523-525, :571), then the reads
and the toggle over the operational network.

Every step's output is written to <out>/<step>.log and its exit code to
<out>/results.json; the Go test judges them. Nothing of chip's is copied
here.
"""

import argparse
import contextlib
import json
import os
import subprocess
import sys
import time

from matter.testing.linux import BluetoothMock, DBusTestSystemBus, IsolatedNetworkNamespace, WpaSupplicantMock

SSID = "MatterAP"
PASSPHRASE = "MatterAPPassword"
NODE_ID = "1"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True)
    ap.add_argument("--app", required=True, help="the app's argv, one string, split on spaces")
    ap.add_argument("--ready", required=True, help="the app's log line that says it serves BLE")
    ap.add_argument("--endpoint", required=True, help="the OnOff endpoint")
    ap.add_argument("--chip-tool", default="/bin/chip-tool-ble")
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)
    results = []

    def record(name, rc, started):
        results.append({"step": name, "exit": rc, "seconds": round(time.monotonic() - started, 3)})
        with open(os.path.join(args.out, "results.json"), "w") as f:
            json.dump(results, f, indent=1)

    with contextlib.ExitStack() as stack:
        ns = stack.enter_context(IsolatedNetworkNamespace(index=0, app_link_up=False, app_link_name="wlx-app",
                                                          tool_link_name="eth-tool"))
        stack.enter_context(DBusTestSystemBus())
        stack.enter_context(BluetoothMock())
        stack.enter_context(WpaSupplicantMock(["wlx-app"], SSID, PASSPHRASE, ns))

        app_log = open(os.path.join(args.out, "app.log"), "w")
        app = subprocess.Popen(ns.app_ns.wrap_cmd(args.app.split()), stdout=app_log, stderr=subprocess.STDOUT)
        stack.callback(app_log.close)

        def stop_app():
            app.terminate()
            try:
                app.wait(15)
            except subprocess.TimeoutExpired:
                app.kill()
        stack.callback(stop_app)

        started = time.monotonic()
        ready = False
        while time.monotonic() - started < 60 and app.poll() is None:
            with open(os.path.join(args.out, "app.log")) as f:
                if args.ready in f.read():
                    ready = True
                    break
            time.sleep(0.2)
        record("app-ready", 0 if ready else 1, started)
        if not ready:
            return 0

        kvs = os.path.join(args.out, "tool-kvs")
        os.makedirs(kvs, exist_ok=True)

        def tool(name, *argv, timeout=180):
            started = time.monotonic()
            cmd = ns.tool_ns.wrap_cmd([args.chip_tool, *argv, "--storage-directory", kvs])
            try:
                p = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=timeout)
                out, rc = p.stdout, p.returncode
            except subprocess.TimeoutExpired as e:
                out, rc = (e.stdout or b"").decode(errors="replace") if isinstance(e.stdout, bytes) else (e.stdout or ""), 124
            with open(os.path.join(args.out, name + ".log"), "w") as f:
                f.write(out)
            record(name, rc, started)
            return rc

        if tool("pairing", "pairing", "ble-wifi", NODE_ID, SSID, PASSPHRASE, "20202021", "3840",
                "--ble-controller", "1", timeout=300) != 0:
            return 0
        tool("feature-map", "networkcommissioning", "read", "feature-map", NODE_ID, "0")
        tool("networks", "networkcommissioning", "read", "networks", NODE_ID, "0")
        tool("last-networking-status", "networkcommissioning", "read", "last-networking-status", NODE_ID, "0")
        tool("toggle", "onoff", "toggle", NODE_ID, args.endpoint)
        tool("on-off", "onoff", "read", "on-off", NODE_ID, args.endpoint)
    return 0


if __name__ == "__main__":
    sys.exit(main())
