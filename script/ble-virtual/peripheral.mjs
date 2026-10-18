// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.
//
// The BLE peripheral of the ble-virtual job (script/ble-virtual-test.sh,
// ADR 0024 decision 5 as amended 2026-10-18): a matter.js on/off light that
// is commissionable over BLE only, with bleno (@matter/nodejs-ble) on one
// btvirt controller, and a Wi-Fi NetworkCommissioning server that accepts
// the credentials the commissioner sends and "connects".
//
// Modelled on matter.js's own BLE example at the pin 0ceb202d,
// examples/device-onoff-advanced/src/DeviceNodeFull.ts (the ServerNode with
// discoveryCapabilities.ble, ble.enable, the Wi-Fi variant behind
// --ble-fake-wifi) and its cluster/DummyWifiNetworkCommissioningServer.ts
// (the Wi-Fi server below follows it command for command). matter.js is
// Apache-2.0, as this module is; this script only uses the published
// packages (package.json pins them, package-lock.json locks them) and
// changes nothing in matter.js.
//
// Environment:
//   BLE_HCI_ID            the HCI index bleno claims (ble.hci.id; default 1)
//   PERIPHERAL_STORAGE    the storage directory (fresh per run)
//   PERIPHERAL_LOGFILE    a debug log file (matter.js's file logger)
//   HCI_CHANNEL_USER=1    read by bleno: bind the HCI user channel, which
//                         needs the controller down in BlueZ
//
// Prints one line "ble-virtual-peripheral: ready manual=<code> qr=<code>"
// once the node is online and advertising over BLE, and
// "ble-virtual-peripheral: commissioned" when the first fabric lands.

const PASSCODE = 20202021;
const DISCRIMINATOR = 3840;

const { Bytes, DeviceTypeId, Endpoint, Environment, LogDestination, LogLevel, Logger, ServerNode, VendorId } =
    await import("@matter/main");
const { GeneralCommissioningBehavior } = await import("@matter/main/behaviors/general-commissioning");
const { NetworkCommissioningBehavior } = await import("@matter/main/behaviors/network-commissioning");
const { NetworkCommissioning } = await import("@matter/main/clusters/network-commissioning");
const { OnOffLightDevice } = await import("@matter/main/devices");
const { Ble, DeviceAdvertiser, DeviceCommissioner } = await import("@matter/main/protocol");
const { createFileLogger } = await import("@matter/nodejs");

const environment = Environment.default;
if (process.env.PERIPHERAL_STORAGE) environment.vars.set("storage.path", process.env.PERIPHERAL_STORAGE);
if (process.env.PERIPHERAL_LOGFILE) {
    Logger.destinations.filelogger = LogDestination({
        write: await createFileLogger(process.env.PERIPHERAL_LOGFILE),
        level: LogLevel("debug"),
    });
}

// packages/nodejs-ble/README.md: the HCI id through "ble.hci.id", BLE through
// "ble.enable"; NodeJsBle reads the id when ble.enable installs it
// (NodeJsBle.ts:29-39, install.ts), so the id goes first.
environment.vars.set("ble.hci.id", Number(process.env.BLE_HCI_ID ?? 1));
environment.vars.set("ble.enable", true);
await import("@matter/nodejs-ble");
if (!environment.has(Ble)) {
    console.error("ble-virtual-peripheral: @matter/nodejs-ble did not install a Ble instance");
    process.exit(3);
}

const firstNetworkId = new Uint8Array(32);

function breadcrumb(behavior, value) {
    if (value !== undefined) behavior.agent.get(GeneralCommissioningBehavior).state.breadcrumb = value;
}

// DummyWifiNetworkCommissioningServer.ts at the pin, without the console
// noise: every command succeeds, ConnectNetwork marks the network connected
// and restarts the advertisement so the node announces itself on IP.
class WifiNetworkCommissioningServer extends NetworkCommissioningBehavior.with(
    NetworkCommissioning.Feature.WiFiNetworkInterface,
) {
    scanNetworks({ ssid, breadcrumb: crumb }) {
        breadcrumb(this, crumb);
        const networkingStatus = NetworkCommissioning.NetworkCommissioningStatus.Success;
        this.state.lastNetworkingStatus = networkingStatus;
        return {
            networkingStatus,
            wiFiScanResults: [
                {
                    security: { unencrypted: false, wep: false, wpaPersonal: false, wpa2Personal: true, wpa3Personal: true },
                    ssid: ssid || Bytes.fromString("lab"),
                    bssid: Bytes.fromString("00:00:00:00:00:00"),
                    channel: 1,
                },
            ],
        };
    }

    addOrUpdateWiFiNetwork({ ssid, breadcrumb: crumb }) {
        console.log(`ble-virtual-peripheral: AddOrUpdateWiFiNetwork ssid=${Bytes.toString(ssid)}`);
        this.env.get(DeviceCommissioner).assertFailsafeArmed("Failsafe timer needs to be armed to add or update networks.");
        breadcrumb(this, crumb);
        const networkingStatus = NetworkCommissioning.NetworkCommissioningStatus.Success;
        this.state.lastNetworkingStatus = networkingStatus;
        this.state.lastNetworkId = firstNetworkId;
        return { networkingStatus, networkIndex: 0 };
    }

    removeNetwork({ breadcrumb: crumb }) {
        this.env.get(DeviceCommissioner).assertFailsafeArmed("Failsafe timer needs to be armed to add or update networks.");
        breadcrumb(this, crumb);
        const networkingStatus = NetworkCommissioning.NetworkCommissioningStatus.Success;
        this.state.lastNetworkingStatus = networkingStatus;
        this.state.lastNetworkId = firstNetworkId;
        return { networkingStatus, networkIndex: 0 };
    }

    async connectNetwork({ breadcrumb: crumb }) {
        console.log("ble-virtual-peripheral: ConnectNetwork");
        this.env.get(DeviceCommissioner).assertFailsafeArmed("Failsafe timer needs to be armed to add or update networks.");
        breadcrumb(this, crumb);
        this.state.networks[0].connected = true;
        const networkingStatus = NetworkCommissioning.NetworkCommissioningStatus.Success;
        this.state.lastNetworkingStatus = networkingStatus;
        this.state.lastNetworkId = firstNetworkId;
        this.state.lastConnectErrorValue = null;
        this.env.get(DeviceAdvertiser).restartAdvertisement();
        return { networkingStatus, errorValue: null };
    }

    reorderNetwork({ breadcrumb: crumb }) {
        breadcrumb(this, crumb);
        const networkingStatus = NetworkCommissioning.NetworkCommissioningStatus.Success;
        this.state.lastNetworkingStatus = networkingStatus;
        return { networkingStatus, networkIndex: 0 };
    }
}

const uniqueId = `ble-virtual-${Date.now()}`;
// The root endpoint carries the Wi-Fi server from the start (DeviceNodeFull.ts
// requires it after ServerNode.create; .with() is the same composition
// without the transient "NetworkCommissioning missing" conformance warning).
const server = await ServerNode.create(ServerNode.RootEndpoint.with(WifiNetworkCommissioningServer), {
    id: uniqueId,
    network: {
        port: Number(process.env.PERIPHERAL_PORT ?? 5540),
        // DeviceNodeFull.ts: with BLE the node announces itself only over BLE
        // until it is commissioned.
        discoveryCapabilities: { onIpNetwork: false, ble: true },
        ble: true,
    },
    commissioning: { passcode: PASSCODE, discriminator: DISCRIMINATOR },
    productDescription: { name: "ble-virtual peripheral", deviceType: DeviceTypeId(OnOffLightDevice.deviceType) },
    basicInformation: {
        vendorName: "matter-node.js",
        vendorId: VendorId(0xfff1),
        nodeLabel: "ble-virtual",
        productName: "ble-virtual peripheral",
        productLabel: "ble-virtual peripheral",
        productId: 0x8000,
        serialNumber: uniqueId,
        uniqueId,
    },
    networkCommissioning: {
        maxNetworks: 1,
        interfaceEnabled: true,
        lastConnectErrorValue: 0,
        lastNetworkId: firstNetworkId,
        lastNetworkingStatus: null,
        networks: [{ networkId: firstNetworkId, connected: false }],
        scanMaxTimeSeconds: 3,
        connectMaxTimeSeconds: 3,
        supportedWiFiBands: [NetworkCommissioning.WiFiBand["2G4"]],
    },
});

await server.add(new Endpoint(OnOffLightDevice, { id: "onoff" }));
console.log(`ble-virtual-peripheral: NetworkCommissioning featureMap ${JSON.stringify(server.state.networkCommissioning.featureMap)}`);

server.lifecycle.commissioned.on(() => console.log("ble-virtual-peripheral: commissioned"));

for (const signal of ["SIGINT", "SIGTERM"]) {
    process.on(signal, () => {
        server
            .close()
            .catch(() => {})
            .finally(() => process.exit(0));
    });
}

await server.start();
const { manualPairingCode, qrPairingCode } = server.state.commissioning.pairingCodes;
console.log(`ble-virtual-peripheral: ready manual=${manualPairingCode} qr=${qrPairingCode}`);
