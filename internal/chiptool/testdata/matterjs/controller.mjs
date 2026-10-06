// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.
//
// A matter.js controller (../matter.js, CommissioningController) driven over
// stdin/stdout, one JSON object per line each way: the second, independent
// controller the chip-tool suite holds the reference daemon against. It is
// run from a scratch directory whose node_modules links ../matter.js's
// node_modules, so nothing is built or written inside the matter.js tree.
//
// Commands: {"cmd":"commission","ip":…,"port":…,"passcode":…,"discriminator":…}
//           {"cmd":"read","endpoint":…,"cluster":"OnOff","attribute":"onOff"}
//           {"cmd":"invoke","endpoint":…,"cluster":"OnOff","command":"toggle"}
//           {"cmd":"write","endpoint":…,"cluster":"BasicInformation","attribute":"nodeLabel","value":…}
//           {"cmd":"waitChange","endpoint":…,"cluster":"OnOff","attribute":"onOff","timeoutMs":…}
//           {"cmd":"openWindow","timeout":180}
//           {"cmd":"state"}  {"cmd":"exit"}
// Every reply is {"ok":true,…} or {"ok":false,"error":…}.
import readline from "node:readline";

const { Environment, StorageService, Logger, LogLevel } = await import("@matter/main");
const Clusters = await import("@matter/main/clusters");
const { NodeId, EndpointNumber } = await import("@matter/main/types");
const { CommissioningController } = await import("@project-chip/matter.js");

Logger.level = LogLevel[process.env.MATTERJS_LOG ?? "WARN"];
const environment = Environment.default;
environment.vars.set("storage.path", process.env.MATTERJS_STORAGE);
if (process.env.MATTERJS_MDNS_INTERFACE) environment.vars.set("mdns.networkInterface", process.env.MATTERJS_MDNS_INTERFACE);

let controller;
let node;
const changes = []; // attribute changes the subscription delivered
const states = [];

function out(obj) {
    process.stdout.write(JSON.stringify(obj, (_k, v) => (typeof v === "bigint" ? v.toString() : v)) + "\n");
}

function cluster(name) {
    const c = Clusters[name] ?? Clusters[`${name}Cluster`];
    if (c === undefined) throw new Error(`unknown cluster ${name}`);
    return c.Complete ?? c;
}

async function start() {
    if (controller !== undefined) return;
    controller = new CommissioningController({
        environment: { environment, id: process.env.MATTERJS_ID ?? "gofabric-matterjs" },
        autoConnect: false,
        adminFabricLabel: "matter.js",
    });
    await controller.start();
}

async function attach(nodeId) {
    node = await controller.getNode(NodeId(nodeId));
    node.events.attributeChanged.on(({ path, value }) =>
        changes.push({ endpoint: path.endpointId, cluster: path.clusterId, attribute: path.attributeName, value, at: Date.now() }),
    );
    node.events.stateChanged.on(s => states.push({ state: s, at: Date.now() }));
    if (!node.isConnected) node.connect();
    if (!node.initialized) await node.events.initialized;
}

async function handle(msg) {
    switch (msg.cmd) {
        case "commission": {
            await start();
            if (!controller.isCommissioned()) {
                const id = await controller.commissionNode({
                    commissioning: { regulatoryLocation: 0, regulatoryCountryCode: "XX" },
                    discovery: {
                        knownAddress: { ip: msg.ip, port: msg.port, type: "udp" },
                        identifierData:
                            msg.shortDiscriminator !== undefined
                                ? { shortDiscriminator: msg.shortDiscriminator }
                                : { longDiscriminator: msg.discriminator },
                    },
                    passcode: msg.passcode,
                });
                await attach(id);
                return { nodeId: id };
            }
            const id = controller.getCommissionedNodes()[0];
            await attach(id);
            return { nodeId: id, resumed: true };
        }
        case "read": {
            const client = node.getClusterClientForDevice(EndpointNumber(msg.endpoint), cluster(msg.cluster));
            if (msg.endpoint === 0) {
                const root = node.getRootClusterClient(cluster(msg.cluster));
                return { value: await root.attributes[msg.attribute].get(true) };
            }
            return { value: await client.attributes[msg.attribute].get(true) };
        }
        case "write": {
            const client =
                msg.endpoint === 0
                    ? node.getRootClusterClient(cluster(msg.cluster))
                    : node.getClusterClientForDevice(EndpointNumber(msg.endpoint), cluster(msg.cluster));
            await client.attributes[msg.attribute].set(msg.value);
            return {};
        }
        case "invoke": {
            const client = node.getClusterClientForDevice(EndpointNumber(msg.endpoint), cluster(msg.cluster));
            const res = await (msg.fields === undefined ? client.commands[msg.command]() : client.commands[msg.command](msg.fields));
            return { response: res ?? null };
        }
        case "waitChange": {
            const since = msg.since ?? 0;
            const deadline = Date.now() + (msg.timeoutMs ?? 10000);
            const clusterId = cluster(msg.cluster).id;
            while (Date.now() < deadline) {
                const hit = changes.find(
                    c => c.at >= since && c.endpoint === msg.endpoint && c.cluster === clusterId && c.attribute === msg.attribute,
                );
                if (hit) return { change: hit };
                await new Promise(r => setTimeout(r, 50));
            }
            throw new Error(`no change of ${msg.cluster}.${msg.attribute} on endpoint ${msg.endpoint} within ${msg.timeoutMs} ms`);
        }
        case "openWindow": {
            const { manualPairingCode, qrPairingCode } = await node.openEnhancedCommissioningWindow(msg.timeout ?? 180);
            return { manualPairingCode, qrPairingCode };
        }
        case "state":
            return { connected: node?.isConnected ?? false, states, now: Date.now() };
        case "exit":
            await controller?.close();
            out({ ok: true });
            process.exit(0);
    }
    throw new Error(`unknown command ${msg.cmd}`);
}

const rl = readline.createInterface({ input: process.stdin });
out({ ok: true, ready: true });
for await (const line of rl) {
    if (!line.trim()) continue;
    try {
        out({ ok: true, ...(await handle(JSON.parse(line))) });
    } catch (e) {
        let chain = String(e?.stack ?? e);
        for (let c = e?.cause; c; c = c.cause) chain += "\ncaused by: " + String(c?.stack ?? c);
        out({ ok: false, error: chain });
    }
}
