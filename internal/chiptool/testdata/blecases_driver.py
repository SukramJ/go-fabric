# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
"""One Python certification case over BLE-Wi-Fi commissioning (milestone 7).

Run by internal/chiptool/blecases_test.go in a privileged container of the
harness image, once per case. It sets up connectedhomeip's own test
environment for a BLE-Wi-Fi commissioning exactly as blewifi_driver.py does
(scripts/tests/chiptest/linux.py:99-116 at 6170af84: the network namespaces
with the app's link down until the Wi-Fi mock associates it, a test system
bus, bluezoo with two adapters, wpa_supplicant's mock with "MatterAP" /
"MatterAPPassword"), starts the app in the app namespace and runs the case
in the tool namespace with the image's BLE-capable Python controller
(/opt/matter-ble), which commissions the app over BLE onto Wi-Fi inside the
case (--commissioning-method ble-wifi --ble-controller 1, the tool's
adapter, commissioning_types.py:88-93).

The case's output is <out>/case.log, the app's <out>/app.log, and
<out>/results.json holds the steps' exit codes; the Go test judges them.
Nothing of chip's is copied here: the case and the mocks are the image's.
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


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True)
    ap.add_argument("--ready", required=True, help="the app's log line that says it serves BLE")
    ap.add_argument("--python", default="/opt/matter-ble/bin/python3")
    ap.add_argument("--script", required=True, help="the case's script")
    ap.add_argument("--script-args", default="[]", help="JSON list: the case's arguments")
    ap.add_argument("--timeout", type=int, default=900)
    ap.add_argument("app", nargs=argparse.REMAINDER, help="after --: the app's argv")
    args = ap.parse_args()
    if args.app and args.app[0] == "--":
        args.app = args.app[1:]
    if not args.app:
        ap.error("the app's argv follows --")
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
        app = subprocess.Popen(ns.app_ns.wrap_cmd(args.app), stdout=app_log, stderr=subprocess.STDOUT)
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

        started = time.monotonic()
        cmd = ns.tool_ns.wrap_cmd([args.python, args.script, *json.loads(args.script_args)])
        with open(os.path.join(args.out, "case.log"), "w") as log:
            try:
                rc = subprocess.run(cmd, stdout=log, stderr=subprocess.STDOUT, cwd=args.out,
                                    timeout=args.timeout).returncode
            except subprocess.TimeoutExpired:
                rc = 124
        record("case", rc, started)
    return 0


if __name__ == "__main__":
    sys.exit(main())
