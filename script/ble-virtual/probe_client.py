# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# The registering client of script/ble-virtual/bluetoothd-probe.sh (issue
# #610 row 1). It is a bus client of bluetoothd, nothing else; every line it
# prints carries the wall clock (epoch seconds, the clock dbus-monitor's
# time= uses) so the probe can lay it beside the monitor record.
#
#   probe_client.py peripheral ADAPTER_FILE
#       Registers one GATT application (GattManager1.RegisterApplication:
#       a root with ObjectManager, one primary service, one readable
#       characteristic) and one LE advertisement
#       (LEAdvertisingManager1.RegisterAdvertisement) on the adapter path
#       read from ADAPTER_FILE, and logs every method bluetoothd calls on
#       them (GetManagedObjects, Release, ReadValue). On SIGUSR1 it re-reads
#       ADAPTER_FILE and calls both Register methods again with the same
#       object paths, logging bluetoothd's reply verbatim (success or the
#       error name), and never unregisters. It does not follow any signal:
#       what survives is what bluetoothd keeps on its own.
#
#   probe_client.py central ADAPTER PEER_ADDRESS
#       Discovers PEER_ADDRESS on ADAPTER, calls Device1.Connect on it,
#       prints "connected" and then waits to be killed, so the probe can
#       remove this caller's bus name while the link is up.
import os
import signal
import sys
import time

import dbus
import dbus.mainloop.glib
import dbus.service
from gi.repository import GLib

BLUEZ = "org.bluez"
APP = "/org/gofabric/probe/app"
SVC = APP + "/service0"
CHR = SVC + "/char0"
ADV = "/org/gofabric/probe/adv0"
SVC_UUID = "0000fff6-0000-1000-8000-00805f9b34fb"
CHR_UUID = "0000fff7-0000-1000-8000-00805f9b34fb"


def log(*parts):
    print("%.6f probe-client: %s" % (time.time(), " ".join(str(p) for p in parts)), flush=True)


class Advertisement(dbus.service.Object):
    def props(self):
        return {
            "Type": dbus.String("peripheral"),
            "ServiceUUIDs": dbus.Array([SVC_UUID], signature="s"),
            "LocalName": dbus.String("gofabric-probe"),
            "Discoverable": dbus.Boolean(True),
        }

    @dbus.service.method(dbus.PROPERTIES_IFACE, in_signature="s", out_signature="a{sv}")
    def GetAll(self, iface):
        log("call", "Properties.GetAll", ADV, iface)
        return self.props()

    @dbus.service.method("org.bluez.LEAdvertisement1", in_signature="", out_signature="")
    def Release(self):
        log("call", "LEAdvertisement1.Release", ADV)


class Characteristic(dbus.service.Object):
    def props(self):
        return {
            "org.bluez.GattCharacteristic1": {
                "Service": dbus.ObjectPath(SVC),
                "UUID": dbus.String(CHR_UUID),
                "Flags": dbus.Array(["read"], signature="s"),
            }
        }

    @dbus.service.method("org.bluez.GattCharacteristic1", in_signature="a{sv}", out_signature="ay")
    def ReadValue(self, options):
        log("call", "GattCharacteristic1.ReadValue", CHR)
        return dbus.Array([dbus.Byte(1)], signature="y")


class Application(dbus.service.Object):
    @dbus.service.method("org.freedesktop.DBus.ObjectManager", in_signature="", out_signature="a{oa{sa{sv}}}")
    def GetManagedObjects(self):
        log("call", "ObjectManager.GetManagedObjects", APP)
        return {
            dbus.ObjectPath(SVC): {
                "org.bluez.GattService1": {
                    "UUID": dbus.String(SVC_UUID),
                    "Primary": dbus.Boolean(True),
                }
            },
            dbus.ObjectPath(CHR): Characteristic.props(None),
        }


def peripheral(adapter_file):
    bus = dbus.SystemBus()
    log("unique-name", bus.get_unique_name())
    Application(bus, APP)
    Characteristic(bus, CHR)
    Advertisement(bus, ADV)

    def register(why):
        with open(adapter_file) as f:
            adapter = f.read().strip()
        log("register", why, "adapter", adapter)
        obj = bus.get_object(BLUEZ, adapter)
        for iface, method, path in (
            ("org.bluez.GattManager1", "RegisterApplication", APP),
            ("org.bluez.LEAdvertisingManager1", "RegisterAdvertisement", ADV),
        ):
            try:
                obj.get_dbus_method(method, iface)(dbus.ObjectPath(path), dbus.Dictionary({}, signature="sv"), timeout=10)
                log("reply", method, adapter, "ok")
            except dbus.exceptions.DBusException as e:
                log("reply", method, adapter, "error", e.get_dbus_name(), repr(e.get_dbus_message()))
        return True

    loop = GLib.MainLoop()
    GLib.unix_signal_add(GLib.PRIORITY_DEFAULT, signal.SIGUSR1, lambda: register("SIGUSR1"))
    GLib.unix_signal_add(GLib.PRIORITY_DEFAULT, signal.SIGTERM, lambda: loop.quit() or False)
    register("start")
    log("ready")
    loop.run()
    log("exit")


def central(adapter, peer):
    bus = dbus.SystemBus()
    log("unique-name", bus.get_unique_name())
    dev = adapter + "/dev_" + peer.upper().replace(":", "_")
    ad = bus.get_object(BLUEZ, adapter)
    try:
        ad.SetDiscoveryFilter(dbus.Dictionary({"Transport": "le"}, signature="sv"), dbus_interface="org.bluez.Adapter1")
    except dbus.exceptions.DBusException as e:
        log("reply", "SetDiscoveryFilter", "error", e.get_dbus_name())
    ad.StartDiscovery(dbus_interface="org.bluez.Adapter1")
    log("discovery", "started", adapter)
    om = dbus.Interface(bus.get_object(BLUEZ, "/"), "org.freedesktop.DBus.ObjectManager")
    deadline = time.time() + 30
    while time.time() < deadline:
        if dbus.ObjectPath(dev) in om.GetManagedObjects():
            break
        time.sleep(0.5)
    else:
        log("device-not-found", dev)
        sys.exit(3)
    log("device-found", dev)
    try:
        ad.StopDiscovery(dbus_interface="org.bluez.Adapter1")
    except dbus.exceptions.DBusException as e:
        log("reply", "StopDiscovery", "error", e.get_dbus_name())
    try:
        bus.get_object(BLUEZ, dev).Connect(dbus_interface="org.bluez.Device1", timeout=30)
        log("reply", "Connect", dev, "ok")
    except dbus.exceptions.DBusException as e:
        log("reply", "Connect", dev, "error", e.get_dbus_name(), repr(e.get_dbus_message()))
        sys.exit(4)
    log("connected", dev, "pid", os.getpid())
    GLib.MainLoop().run()


if __name__ == "__main__":
    dbus.mainloop.glib.DBusGMainLoop(set_as_default=True)
    if len(sys.argv) == 3 and sys.argv[1] == "peripheral":
        peripheral(sys.argv[2])
    elif len(sys.argv) == 4 and sys.argv[1] == "central":
        central(sys.argv[2], sys.argv[3])
    else:
        sys.exit("usage: probe_client.py peripheral ADAPTER_FILE | central ADAPTER PEER_ADDRESS")
