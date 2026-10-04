// Generates group-messaging fixtures for go-fabric's parity tests from the
// matter.js checkout's own code.
//
// Two outputs, selected by the first argument:
//
//   wire    GroupKeyManagement / Groups command payloads as matter.js's
//           TlvOfModel(command) encodes them — the schema the interaction
//           server decodes requests with and encodes responses with
//           (packages/node/src/node/integration/ProtocolService.ts).
//           Master: bridge/testdata/group-wire-fixtures.json
//
//   crypto  Operational group key, group session id, privacy key and
//           multicast address for fixed inputs, plus complete encrypted
//           group messages, all computed by matter.js's protocol package
//           (FabricGroups.setFromGroupKeySet, KeySets.sessionIdFromKey,
//           MessagePrivacy.deriveKey, Groups.multicastAddress,
//           GroupSession.encode).
//           Master: groups/testdata/group-crypto-fixtures.json
//
// Run from anywhere, against a built matter.js checkout next to go-fabric:
//
//   node "$GO_FABRIC"/notes/parity/matter/generate-group-fixtures.ts wire > /tmp/w.json \
//       && mv /tmp/w.json "$GO_FABRIC"/bridge/testdata/group-wire-fixtures.json
//   node "$GO_FABRIC"/notes/parity/matter/generate-group-fixtures.ts crypto > /tmp/c.json \
//       && mv /tmp/c.json "$GO_FABRIC"/groups/testdata/group-crypto-fixtures.json

const path = require("path");

const matterJsRoot = path.resolve(__dirname, "../../../../matter.js");
const mod = (name) => require(path.join(matterJsRoot, "node_modules/@matter", name, "dist/cjs/index.js"));

const general = mod("general");
const model = mod("model");
const types = mod("types");

const hex = (b) => Buffer.from(b).toString("hex");

// Epoch-us values are unix-based inside matter.js; TlvEpochUs subtracts the
// Matter epoch offset on the wire. Fixtures record WIRE values.
const OFFSET = types.MATTER_EPOCH_OFFSET_US ;
const epoch = (wire) => (wire === null ? null : OFFSET + BigInt(wire));

function commandTlv(cluster, name) {
    const command = cluster.commands.find((c) => c.name === name);
    if (command === undefined) {
        throw new Error(`no command ${name} in ${cluster.name}`);
    }
    return types.TlvOfModel(command);
}

function wireFixtures() {
    const gkm = model.GroupKeyManagement;
    const groups = model.Groups;
    const out = [];
    const record = (label, cluster, command, fixture, value) => {
        out.push({ label, cluster: cluster.id, command, fixture, bytesHex: hex(commandTlv(cluster, command).encode(value)) });
    };

    const keySet = (f) => ({
        groupKeySetId: f.groupKeySetId,
        groupKeySecurityPolicy: f.groupKeySecurityPolicy,
        epochKey0: f.epochKey0 === null ? null : Buffer.from(f.epochKey0, "hex"),
        epochStartTime0: epoch(f.epochStartTime0),
        epochKey1: f.epochKey1 === null ? null : Buffer.from(f.epochKey1, "hex"),
        epochStartTime1: epoch(f.epochStartTime1),
        epochKey2: f.epochKey2 === null ? null : Buffer.from(f.epochKey2, "hex"),
        epochStartTime2: epoch(f.epochStartTime2),
        ...(f.groupKeyMulticastPolicy === undefined ? {} : { groupKeyMulticastPolicy: f.groupKeyMulticastPolicy }),
    });

    for (const [label, f] of Object.entries({
        keyset_write_one_epoch: {
            groupKeySetId: 0x01a1, groupKeySecurityPolicy: 0,
            epochKey0: "d0d1d2d3d4d5d6d7d8d9dadbdcdddedf", epochStartTime0: 2220000,
            epochKey1: null, epochStartTime1: null, epochKey2: null, epochStartTime2: null,
        },
        keyset_write_three_epochs_all_nodes_policy: {
            groupKeySetId: 0x0102, groupKeySecurityPolicy: 0,
            epochKey0: "a0a1a2a3a4a5a6a7a8a9aaabacadaeaf", epochStartTime0: 1,
            epochKey1: "b0b1b2b3b4b5b6b7b8b9babbbcbdbebf", epochStartTime1: 18446744073709,
            epochKey2: "c0c1c2c3c4c5c6c7c8c9cacbcccdcecf", epochStartTime2: 1110000000000000,
            groupKeyMulticastPolicy: 1,
        },
    })) {
        record(label, gkm, "KeySetWrite", f, { groupKeySet: keySet(f) });
    }

    // KeySetReadResponse as GroupKeyManagementServer.keySetRead builds it:
    // keys null, PerGroupId reported while the model defines the field.
    for (const [label, f] of Object.entries({
        keyset_read_response_one_epoch: {
            groupKeySetId: 0x01a1, groupKeySecurityPolicy: 0,
            epochKey0: null, epochStartTime0: 2220000,
            epochKey1: null, epochStartTime1: null, epochKey2: null, epochStartTime2: null,
            groupKeyMulticastPolicy: 0,
        },
        keyset_read_response_ipk: {
            groupKeySetId: 0, groupKeySecurityPolicy: 0,
            epochKey0: null, epochStartTime0: 0,
            epochKey1: null, epochStartTime1: null, epochKey2: null, epochStartTime2: null,
            groupKeyMulticastPolicy: 0,
        },
        keyset_read_response_three_epochs: {
            groupKeySetId: 0x0102, groupKeySecurityPolicy: 0,
            epochKey0: null, epochStartTime0: 1,
            epochKey1: null, epochStartTime1: 18446744073709,
            epochKey2: null, epochStartTime2: 1110000000000000,
            groupKeyMulticastPolicy: 0,
        },
    })) {
        record(label, gkm, "KeySetReadResponse", f, { groupKeySet: keySet(f) });
    }

    record("keyset_read_all_indices_response", gkm, "KeySetReadAllIndicesResponse",
        { groupKeySetIds: [0, 0x01a1, 0x0102] }, { groupKeySetIds: [0, 0x01a1, 0x0102] });

    // Groups (0x0004) responses, as GroupsServer returns them.
    record("groups_add_group_response", groups, "AddGroupResponse", { status: 0, groupId: 0x0101 }, { status: 0, groupId: 0x0101 });
    record("groups_view_group_response", groups, "ViewGroupResponse",
        { status: 0, groupId: 0x0101, groupName: "Kitchen" }, { status: 0, groupId: 0x0101, groupName: "Kitchen" });
    record("groups_view_group_response_not_found", groups, "ViewGroupResponse",
        { status: 0x8b, groupId: 0x0202, groupName: "" }, { status: 0x8b, groupId: 0x0202, groupName: "" });
    record("groups_get_group_membership_response", groups, "GetGroupMembershipResponse",
        { capacity: 0xfc, groupList: [0x0101, 0x0102] }, { capacity: 0xfc, groupList: [0x0101, 0x0102] });
    record("groups_remove_group_response", groups, "RemoveGroupResponse", { status: 0x8b, groupId: 0x0303 }, { status: 0x8b, groupId: 0x0303 });

    return out;
}

async function cryptoFixtures() {
    throw new Error("crypto fixtures: see the crypto section below");
}

async function main() {
    const which = process.argv[2];
    let out;
    if (which === "wire") {
        out = wireFixtures();
    } else if (which === "crypto") {
        out = await cryptoFixtures();
    } else {
        throw new Error("usage: generate-group-fixtures.ts wire|crypto");
    }
    process.stdout.write(JSON.stringify(out, (_k, v) => (typeof v === "bigint" ? v.toString() : v), 2) + "\n");
}

main().catch(e => {
    console.error(e);
    process.exit(1);
});
