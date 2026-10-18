// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.
//
// The control leg of the ble-virtual job (script/ble-virtual-test.sh):
// matter.js's own controller commissions the same matter.js peripheral
// (peripheral.mjs) over BLE, with noble (@matter/nodejs-ble) on the other
// btvirt controller, then reads BasicInformation.VendorID over the
// operational session. When this fails as well as our central, the virtual
// stack is at fault, not go-fabric (the chiptool-control pattern).
//
// Modelled on matter.js's controller example at the pin 0ceb202d,
// examples/controller/src/ControllerNode.ts (CommissioningController,
// discoveryCapabilities.ble, commissioning.wifiNetwork from
// ble.wifi.ssid / ble.wifi.credentials). matter.js is Apache-2.0, as this
// module is; this script only uses the published packages.
//
// Environment:
//   BLE_HCI_ID          the HCI index noble claims (ble.hci.id; default 0)
//   CONTROL_STORAGE     the storage directory (fresh per run)
//   CONTROL_CODE        the manual pairing code the peripheral printed
//   CONTROL_WIFI_SSID, CONTROL_WIFI_CREDENTIALS
//   HCI_CHANNEL_USER=1  read by noble: bind the HCI user channel
//
// Exit 0 after the read returned the VendorID, 1 otherwise; the last line
// is "ble-virtual-control: pass …" or "ble-virtual-control: fail …".

const { Environment, Logger, LogLevel } = await import("@matter/main");
const { BasicInformationCluster, GeneralCommissioning } = await import("@matter/main/clusters");
const { ManualPairingCodeCodec } = await import("@matter/main/types");
const { CommissioningController } = await import("@project-chip/matter.js");

Logger.level = LogLevel[process.env.CONTROL_LOG ?? "INFO"];
const environment = Environment.default;
if (process.env.CONTROL_STORAGE) environment.vars.set("storage.path", process.env.CONTROL_STORAGE);
environment.vars.set("ble.hci.id", Number(process.env.BLE_HCI_ID ?? 0));
environment.vars.set("ble.enable", true);
await import("@matter/nodejs-ble");

function fail(what, err) {
    console.log(`ble-virtual-control: fail ${what}${err ? `: ${err?.stack ?? err}` : ""}`);
    process.exit(1);
}

const code = process.env.CONTROL_CODE;
if (!code) fail("CONTROL_CODE is not set");
const { shortDiscriminator, passcode } = ManualPairingCodeCodec.decode(code);

try {
    const controller = new CommissioningController({
        environment: { environment, id: "ble-virtual-control" },
        autoConnect: false,
        adminFabricLabel: "ble-virtual control",
    });
    await controller.start();

    const nodeId = await controller.commissionNode({
        commissioning: {
            regulatoryLocation: GeneralCommissioning.RegulatoryLocationType.IndoorOutdoor,
            regulatoryCountryCode: "XX",
            wifiNetwork: {
                wifiSsid: process.env.CONTROL_WIFI_SSID ?? "lab",
                wifiCredentials: process.env.CONTROL_WIFI_CREDENTIALS ?? "secret12",
            },
        },
        discovery: {
            identifierData: { shortDiscriminator },
            discoveryCapabilities: { ble: true },
        },
        passcode,
    });
    console.log(`ble-virtual-control: commissioned node ${nodeId}`);

    const node = await controller.getNode(nodeId);
    if (!node.isConnected) node.connect();
    if (!node.initialized) await node.events.initialized;
    const vendorId = await node.getRootClusterClient(BasicInformationCluster).attributes.vendorId.get(true);
    console.log(`ble-virtual-control: pass VendorID ${vendorId} over the operational session`);
    await controller.close();
    process.exit(0);
} catch (err) {
    fail("commissioning or read", err);
}
