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
//   groupcast
//           Groupcast (0x0065) command payloads, the Membership and
//           AccessControl.AuxiliaryAcl attribute values, and the
//           GroupcastTesting / AuxiliaryAccessUpdated event payloads, as
//           TlvOfModel(element) encodes them.
//           Master: bridge/testdata/groupcast-wire-fixtures.json
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
//   node "$GO_FABRIC"/notes/parity/matter/generate-group-fixtures.ts groupcast > /tmp/g.json \
//       && mv /tmp/g.json "$GO_FABRIC"/bridge/testdata/groupcast-wire-fixtures.json
//   node "$GO_FABRIC"/notes/parity/matter/generate-group-fixtures.ts crypto > /tmp/c.json \
//       && mv /tmp/c.json "$GO_FABRIC"/groups/testdata/group-crypto-fixtures.json

const path = require("path");

const matterJsRoot = path.resolve(__dirname, "../../../../matter.js");
const mod = (name) => require(path.join(matterJsRoot, "node_modules/@matter", name, "dist/cjs/index.js"));

const general = mod("general");
const model = mod("model");
const types = mod("types");

// matter.js logs to stdout; keep the JSON clean.
general.Logger.level = "error";

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

    // Groups (0x0004) requests.
    record("groups_add_group", groups, "AddGroup", { groupId: 0x0101, groupName: "Kitchen" }, { groupId: 0x0101, groupName: "Kitchen" });
    record("groups_add_group_if_identifying", groups, "AddGroupIfIdentifying", { groupId: 0xfeff, groupName: "" }, { groupId: 0xfeff, groupName: "" });
    record("groups_view_group", groups, "ViewGroup", { groupId: 0x0202 }, { groupId: 0x0202 });
    record("groups_get_group_membership", groups, "GetGroupMembership", { groupList: [0x0101, 7] }, { groupList: [0x0101, 7] });
    record("groups_get_group_membership_all", groups, "GetGroupMembership", { groupList: [] }, { groupList: [] });
    record("groups_remove_group", groups, "RemoveGroup", { groupId: 0x0303 }, { groupId: 0x0303 });

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

function elementTlv(cluster, kind, name) {
    const element = cluster[kind].find((e) => e.name === name);
    if (element === undefined) {
        throw new Error(`no ${kind} ${name} in ${cluster.name}`);
    }
    return types.TlvOfModel(element);
}

// Groupcast payloads with the shapes GroupcastServer and
// AccessControlServer produce. Byte strings are hex in the fixture.
function groupcastFixtures() {
    const gc = model.Groupcast;
    const acl = model.AccessControl;
    const out = [];
    const bytesOf = (v) => (typeof v === "string" ? Buffer.from(v, "hex") : v);
    // Attribute values go out the way InteractionMessenger encodes a
    // non-fabric-filtered read: a fabric-sensitive field may be missing.
    const record = (label, cluster, kind, name, fixture, value) => {
        const tlv = elementTlv(cluster, kind, name);
        const bytes =
            kind === "attributes"
                ? types.TlvAny.encode(tlv.encodeTlv(value, { allowMissingFieldsForNonFabricFilteredRead: true }))
                : tlv.encode(value);
        out.push({ label, kind, cluster: cluster.id, element: name, fixture, bytesHex: hex(bytes) });
    };

    // Requests.
    for (const [label, f] of Object.entries({
        join_group_minimal: { groupId: 0x0101, endpoints: [2, 3], keySetId: 0x0042 },
        join_group_full: {
            groupId: 0xfff7, endpoints: [0xfffe], keySetId: 0xffff, key: "000102030405060708090a0b0c0d0e0f",
            useAuxiliaryAcl: true, replaceEndpoints: false, mcastAddrPolicy: 1,
        },
    })) {
        record(label, gc, "commands", "JoinGroup", f, { ...f, ...(f.key ? { key: bytesOf(f.key) } : {}) });
    }
    record("leave_group_all", gc, "commands", "LeaveGroup", { groupId: 0x0101 }, { groupId: 0x0101 });
    record("leave_group_endpoints", gc, "commands", "LeaveGroup", { groupId: 0, endpoints: [2, 0x0102] }, { groupId: 0, endpoints: [2, 0x0102] });
    record("update_group_key", gc, "commands", "UpdateGroupKey",
        { groupId: 0x0101, keySetId: 7, key: "f0f1f2f3f4f5f6f7f8f9fafbfcfdfeff" },
        { groupId: 0x0101, keySetId: 7, key: bytesOf("f0f1f2f3f4f5f6f7f8f9fafbfcfdfeff") });
    record("configure_auxiliary_acl", gc, "commands", "ConfigureAuxiliaryAcl", { groupId: 0x0101, useAuxiliaryAcl: true }, { groupId: 0x0101, useAuxiliaryAcl: true });
    record("groupcast_testing_default", gc, "commands", "GroupcastTesting", { testOperation: 1 }, { testOperation: 1 });
    record("groupcast_testing_duration", gc, "commands", "GroupcastTesting", { testOperation: 0, durationSeconds: 1200 }, { testOperation: 0, durationSeconds: 1200 });

    // Responses.
    record("leave_group_response", gc, "commands", "LeaveGroupResponse", { groupId: 0x0101, endpoints: [2, 0x0300] }, { groupId: 0x0101, endpoints: [2, 0x0300] });
    record("leave_group_response_wildcard", gc, "commands", "LeaveGroupResponse", { groupId: 0, endpoints: [] }, { groupId: 0, endpoints: [] });

    // Membership as #deriveMembership derives it; a foreign entry of an
    // unfiltered read carries no KeySetId (access S).
    const membership = [
        { groupId: 0x0101, endpoints: [2, 0x0300], keySetId: 0x0042, hasAuxiliaryAcl: true, mcastAddrPolicy: 0, fabricIndex: 1 },
        { groupId: 0x0202, endpoints: [], keySetId: 0xffff, hasAuxiliaryAcl: false, mcastAddrPolicy: 1, fabricIndex: 1 },
        { groupId: 0x0303, endpoints: [4], hasAuxiliaryAcl: false, mcastAddrPolicy: 1, fabricIndex: 2 },
    ];
    record("membership", gc, "attributes", "Membership", membership, membership);

    // AuxiliaryAcl entries as #emitAuxAcl / #auxiliaryAclFor build them.
    const auxAcl = [
        {
            privilege: 3, authMode: 3, subjects: [0x0101],
            targets: [{ cluster: null, endpoint: 2, deviceType: null }, { cluster: null, endpoint: 0x0300, deviceType: null }],
            auxiliaryType: 1, fabricIndex: 1,
        },
        { fabricIndex: 2 },
    ];
    record("auxiliary_acl", acl, "attributes", "AuxiliaryAcl", auxAcl,
        auxAcl.map((e) => (e.subjects ? { ...e, subjects: e.subjects.map(BigInt) } : e)));

    // Events.
    const testing = {
        sourceIpAddress: "fe800000000000000000000000000001", destinationIpAddress: "ff0500000000000000000000000000fa",
        groupId: 0x0101, endpointId: 2, clusterId: 6, elementId: 2, accessAllowed: true, groupcastTestResult: 0, fabricIndex: 1,
    };
    record("groupcast_testing_event_success", gc, "events", "GroupcastTesting", testing,
        { ...testing, sourceIpAddress: bytesOf(testing.sourceIpAddress), destinationIpAddress: bytesOf(testing.destinationIpAddress) });
    const failed = { destinationIpAddress: "ff0500000000000000000000000000fa", groupcastTestResult: 3, fabricIndex: 1 };
    record("groupcast_testing_event_failed_auth", gc, "events", "GroupcastTesting", failed,
        { ...failed, destinationIpAddress: bytesOf(failed.destinationIpAddress) });
    record("auxiliary_access_updated", acl, "events", "AuxiliaryAccessUpdated", { adminNodeId: "119", fabricIndex: 1 }, { adminNodeId: 119n, fabricIndex: 1 });
    record("auxiliary_access_updated_no_admin", acl, "events", "AuxiliaryAccessUpdated", { adminNodeId: null, fabricIndex: 2 }, { adminNodeId: null, fabricIndex: 2 });
    return out;
}

// A fabric object with exactly the members FabricGroups and GroupSession
// read: identity and crypto. The IPK only seeds key set 0, which no group
// message can use.
function fakeFabric(crypto, f) {
    return {
        fabricIndex: 1,
        fabricId: BigInt(f.fabricId),
        nodeId: BigInt(f.sourceNodeId),
        globalId: BigInt("0x" + f.compressedFabricId),
        crypto,
        identityProtectionKey: new Uint8Array(16),
        operationalIdentityProtectionKey: new Uint8Array(16),
        addSession() {},
    };
}

async function cryptoFixtures() {
    const protocol = mod("protocol");
    const nodejs = mod("nodejs");
    const crypto = new nodejs.NodeJsCrypto();

    const inputs = [
        {
            label: "kitchen_privacy",
            fabricId: "0x0000000000000fab", compressedFabricId: "87e1b004e235a130", sourceNodeId: "0x000000000001b669",
            keySetId: 0x01a1, epochKey: "d0d1d2d3d4d5d6d7d8d9dadbdcdddedf", groupId: 0x0101,
            counter: 0x01020304, exchangeId: 0x0055, privacy: true,
            // InvokeRequest: OnOff.Toggle on a wildcard endpoint.
            opcode: 0x08, payload: "1528002801360215370024010625020624030218181824ff0c18",
        },
        {
            label: "kitchen_no_privacy",
            fabricId: "0x0000000000000fab", compressedFabricId: "87e1b004e235a130", sourceNodeId: "0x000000000001b669",
            keySetId: 0x01a1, epochKey: "d0d1d2d3d4d5d6d7d8d9dadbdcdddedf", groupId: 0x0101,
            counter: 0xfffffffe, exchangeId: 0x0056, privacy: false,
            opcode: 0x08, payload: "1528002801360215370024010625020624030218181824ff0c18",
        },
        {
            label: "other_fabric_large_ids",
            fabricId: "0xfedcba9876543210", compressedFabricId: "0123456789abcdef", sourceNodeId: "0x1122334455667788",
            keySetId: 0x0102, epochKey: "a0a1a2a3a4a5a6a7a8a9aaabacadaeaf", groupId: 0xfeff,
            counter: 7, exchangeId: 0x1234, privacy: true,
            opcode: 0x06, payload: "15283d003e00",
        },
    ];

    const out = [];
    for (const f of inputs) {
        const fabric = fakeFabric(crypto, f);
        const groups = new protocol.FabricGroups(fabric);
        fabric.groups = groups;
        await groups.setFromGroupKeySet({
            groupKeySetId: f.keySetId, groupKeySecurityPolicy: 0,
            epochKey0: Buffer.from(f.epochKey, "hex"), epochStartTime0: epoch(1),
            epochKey1: null, epochStartTime1: null, epochKey2: null, epochStartTime2: null,
        });
        const ks = groups.keySets.forId(f.keySetId);
        groups.groupKeyIdMap = new Map([[f.groupId, f.keySetId]]);

        const session = new protocol.GroupSession({
            id: ks.groupSessionId0, fabric, keySetId: f.keySetId,
            peerNodeId: types.NodeId(0xffffffffffff0000n | BigInt(f.groupId)),
            operationalGroupKey: ks.operationalEpochKey0, operationalPrivacyKey: ks.operationalPrivacyKey0,
            multicastAddress: groups.multicastAddressFor(f.groupId),
        });
        const message = {
            packetHeader: {
                sessionId: ks.groupSessionId0, sessionType: 1, messageId: f.counter,
                sourceNodeId: BigInt(f.sourceNodeId), destGroupId: f.groupId,
                hasPrivacyEnhancements: f.privacy, isControlMessage: false, hasMessageExtensions: false,
            },
            payloadHeader: {
                exchangeId: f.exchangeId, protocolId: 1, messageType: f.opcode, isInitiatorMessage: true,
                requiresAck: false, ackedMessageId: undefined, hasSecuredExtension: false,
            },
            payload: Buffer.from(f.payload, "hex"),
        };
        const plaintext = protocol.MessageCodec.encodePayload(message).applicationPayload;
        const datagram = protocol.MessageCodec.encodePacket(session.encode(message));
        out.push({
            ...f,
            operationalKey: hex(ks.operationalEpochKey0),
            groupSessionId: ks.groupSessionId0,
            privacyKey: hex(ks.operationalPrivacyKey0),
            multicastAddress: groups.multicastAddressFor(f.groupId),
            plaintext: hex(plaintext),
            datagram: hex(datagram),
        });
    }
    return out;
}

async function main() {
    const which = process.argv[2];
    let out;
    if (which === "wire") {
        out = wireFixtures();
    } else if (which === "groupcast") {
        out = groupcastFixtures();
    } else if (which === "crypto") {
        out = await cryptoFixtures();
    } else {
        throw new Error("usage: generate-group-fixtures.ts wire|groupcast|crypto");
    }
    process.stdout.write(JSON.stringify(out, (_k, v) => (typeof v === "bigint" ? v.toString() : v), 2) + "\n");
}

main().catch(e => {
    console.error(e);
    process.exit(1);
});
