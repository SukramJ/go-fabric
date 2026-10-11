// Generates BDX message and OTA image wire-byte fixtures for go-fabric's bdx/
// package: every BDX message matter.js encodes (packages/protocol/src/bdx/
// schema/*), the BDX StatusReport (BdxStatusMessageSchema.ts), a set of
// *Init / *Accept payloads matter.js refuses to decode with the BdxStatusCode
// it refuses them with, and OTA image files written by matter.js's
// OtaImageWriter (packages/protocol/src/ota/OtaImageWriter.ts).
//
// Regen command (run from inside the built matter.js checkout):
//   node "$GO_FABRIC"/notes/parity/matter/generate-bdx-wire-fixtures.ts > /tmp/bdx.json \
//       && mv /tmp/bdx.json "$GO_FABRIC"/bdx/testdata/bdx-wire-fixtures.json
//
// The checkout needs its protocol package transpiled (`npm run build` in
// packages/protocol; the type-check step may fail on missing dev
// dependencies, the transpile step is what this script needs).

const path = require("path");

const scriptDir = __dirname;
const matterJsRoot = process.env.MATTERJS_DIR ?? path.resolve(scriptDir, "../../../../matter.js");
const protocol = require(path.join(matterJsRoot, "node_modules/@matter/protocol/dist/cjs/index.js"));
const types = require(path.join(matterJsRoot, "node_modules/@matter/types/dist/cjs/index.js"));
const general = require(path.join(matterJsRoot, "node_modules/@matter/general/dist/cjs/index.js"));

const { BdxMessage, BdxStatusMessage, OtaImageWriter, TlvOtaImageHeader } = protocol;
const { BdxMessageType, GeneralStatusCode, BdxStatusCode } = types;
const { Bytes, StandardCrypto } = general;

const hex = (b: Uint8Array) => Buffer.from(b).toString("hex");
const bytes = (s: string) => new Uint8Array(Buffer.from(s, "utf8"));
const raw = (h: string) => new Uint8Array(Buffer.from(h, "hex"));

// JSON form of a message: bigint as string with an "n" suffix, bytes as hex.
function jsonOf(value: unknown): unknown {
    if (typeof value === "bigint") return `${value}n`;
    if (value instanceof Uint8Array) return { hex: hex(value) };
    if (Array.isArray(value)) return value.map(jsonOf);
    if (value !== null && typeof value === "object") {
        const out: Record<string, unknown> = {};
        for (const [k, v] of Object.entries(value)) {
            if (v !== undefined) out[k] = jsonOf(v);
        }
        return out;
    }
    return value;
}

const messages: Array<{ label: string; kind: number; message: any }> = [
    // *Init: the requestor's ReceiveInit (OtaSoftwareUpdateRequestorServer.ts
    // #handleBdxDownload: receiver drive only, 1024-byte blocks on a non-TCP
    // channel), and the range-control variants of BdxInitMessagesSchema.ts.
    {
        label: "receive_init_requestor",
        kind: BdxMessageType.ReceiveInit,
        message: {
            transferProtocol: { version: 0, senderDrive: false, receiverDrive: true, asynchronousTransfer: false },
            maxBlockSize: 1024,
            fileDesignator: bytes("fff1-8001-v2.ota"),
        },
    },
    {
        label: "receive_init_both_modes_offset_length",
        kind: BdxMessageType.ReceiveInit,
        message: {
            transferProtocol: { version: 0, senderDrive: true, receiverDrive: true, asynchronousTransfer: false },
            maxBlockSize: 8192,
            startOffset: 16,
            maxLength: 4096,
            fileDesignator: bytes("scope/file.bin"),
        },
    },
    {
        label: "send_init_wide_range_metadata",
        kind: BdxMessageType.SendInit,
        message: {
            transferProtocol: { version: 0, senderDrive: true, receiverDrive: false, asynchronousTransfer: false },
            maxBlockSize: 1174,
            startOffset: 0x1_0000_0000n,
            maxLength: 0x2_0000_0000n,
            fileDesignator: raw("00ff10"),
            metaData: raw("1518"),
        },
    },
    {
        label: "send_init_zero_length_is_indefinite",
        kind: BdxMessageType.SendInit,
        message: {
            transferProtocol: { version: 0, senderDrive: true, receiverDrive: false, asynchronousTransfer: false },
            maxBlockSize: 512,
            maxLength: 0,
            fileDesignator: bytes("a"),
        },
    },
    // *Accept
    {
        label: "send_accept",
        kind: BdxMessageType.SendAccept,
        message: {
            transferControl: { version: 0, senderDrive: false, receiverDrive: true, asynchronousTransfer: false },
            maxBlockSize: 1024,
        },
    },
    {
        label: "send_accept_metadata",
        kind: BdxMessageType.SendAccept,
        message: {
            transferControl: { version: 0, senderDrive: true, receiverDrive: false, asynchronousTransfer: false },
            maxBlockSize: 256,
            metaData: raw("1518"),
        },
    },
    {
        label: "receive_accept_receiver_drive_length",
        kind: BdxMessageType.ReceiveAccept,
        message: {
            transferControl: { version: 0, senderDrive: false, receiverDrive: true, asynchronousTransfer: false },
            maxBlockSize: 1024,
            length: 70000,
        },
    },
    {
        label: "receive_accept_sender_drive_no_length",
        kind: BdxMessageType.ReceiveAccept,
        message: {
            transferControl: { version: 0, senderDrive: true, receiverDrive: false, asynchronousTransfer: false },
            maxBlockSize: 1024,
        },
    },
    {
        label: "receive_accept_wide_length_metadata",
        kind: BdxMessageType.ReceiveAccept,
        message: {
            transferControl: { version: 0, senderDrive: false, receiverDrive: true, asynchronousTransfer: false },
            maxBlockSize: 1024,
            length: 0x1_0000_0001n,
            metaData: raw("1518"),
        },
    },
    // Block messages (BdxBlockMessagesSchema.ts)
    { label: "block_query", kind: BdxMessageType.BlockQuery, message: { blockCounter: 0 } },
    { label: "block_query_wrapped", kind: BdxMessageType.BlockQuery, message: { blockCounter: 0xffffffff } },
    {
        label: "block_query_with_skip",
        kind: BdxMessageType.BlockQueryWithSkip,
        message: { blockCounter: 7, bytesToSkip: 4096n },
    },
    { label: "block", kind: BdxMessageType.Block, message: { blockCounter: 3, data: raw("0102030405") } },
    { label: "block_eof", kind: BdxMessageType.BlockEof, message: { blockCounter: 4, data: raw("ff") } },
    { label: "block_eof_empty", kind: BdxMessageType.BlockEof, message: { blockCounter: 5, data: raw("") } },
    { label: "block_ack", kind: BdxMessageType.BlockAck, message: { blockCounter: 3 } },
    { label: "block_ack_eof", kind: BdxMessageType.BlockAckEof, message: { blockCounter: 4 } },
];

const out: Record<string, unknown> = {};

out.messages = messages.map(({ label, kind, message }) => {
    const encoded = BdxMessage.encode({ kind, message });
    const decoded = BdxMessage.decode(kind, encoded).message;
    return { label, kind, message: jsonOf(message), decoded: jsonOf(decoded), bytesHex: hex(encoded) };
});

out.statusReports = [
    { general: GeneralStatusCode.Failure, protocol: BdxStatusCode.BadBlockCounter },
    { general: GeneralStatusCode.Failure, protocol: BdxStatusCode.TransferMethodNotSupported },
    { general: GeneralStatusCode.Success, protocol: BdxStatusCode.Success },
].map(({ general: generalStatus, protocol: protocolStatus }) => ({
    generalStatus,
    protocolStatus,
    bytesHex: hex(BdxStatusMessage.encode({ generalStatus, protocolStatus })),
}));

// Payloads matter.js's decoders refuse, and the BdxStatusCode they carry.
const refused = [
    { label: "init_version_1", kind: BdxMessageType.ReceiveInit, hex: "21000004" + "0100" + "61" },
    { label: "init_no_drive", kind: BdxMessageType.ReceiveInit, hex: "00000004" + "0100" + "61" },
    { label: "init_empty_designator", kind: BdxMessageType.SendInit, hex: "10000004" + "0000" },
    { label: "init_block_size_zero", kind: BdxMessageType.SendInit, hex: "10000000" + "0100" + "61" },
    { label: "accept_no_mode", kind: BdxMessageType.SendAccept, hex: "000004" },
    { label: "receive_accept_version_2", kind: BdxMessageType.ReceiveAccept, hex: "12000004" },
];
out.refused = refused.map(({ label, kind, hex: h }) => {
    try {
        BdxMessage.decode(kind, raw(h));
        throw new Error(`${label}: matter.js accepted the payload`);
    } catch (error: any) {
        if (error.code === undefined) throw error;
        return { label, kind, bytesHex: h, code: error.code };
    }
});

async function images() {
    const crypto = new StandardCrypto();
    const payload = new Uint8Array(3000);
    for (let i = 0; i < payload.length; i++) payload[i] = (i ^ 0x42) & 0xff;
    const variants = [
        { label: "image_sha256", vendorId: 0xfff1, productId: 0x8001, softwareVersion: 2, softwareVersionString: "2.0" },
        {
            label: "image_sha512_bounds_notes",
            vendorId: 0xfff1,
            productId: 0x8001,
            softwareVersion: 0x10002,
            softwareVersionString: "1.0.2-rc",
            minApplicableSoftwareVersion: 1,
            maxApplicableSoftwareVersion: 0x10001,
            releaseNotesUrl: "https://example.com/notes",
            imageDigestType: "SHA-512",
        },
    ];
    const result = [];
    for (const { label, ...options } of variants) {
        const { image } = await OtaImageWriter.create(crypto, { ...options, payload });
        const headerSize = Buffer.from(image).readUInt32LE(12);
        const header = TlvOtaImageHeader.decode(image.subarray(16, 16 + headerSize));
        result.push({ label, header: jsonOf(header), imageHex: hex(image) });
    }
    return result;
}

images().then(list => {
    out.images = list;
    process.stdout.write(JSON.stringify(out, null, 2) + "\n");
});
