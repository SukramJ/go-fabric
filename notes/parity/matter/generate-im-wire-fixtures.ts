// Generates IM-message-level wire-byte fixtures for go-fabric's Go encoders.
//
// Each record pairs a canonical matter.js .encode() output (hex) with the
// JSON-form of the input, so the Go test can reconstruct the Go struct,
// re-encode via MarshalTLV, and assert byte equality.
//
// Regen command (run from inside the matter.js checkout):
//   node "$GO_FABRIC"/notes/parity/matter/generate-im-wire-fixtures.ts \
//       > "$GO_FABRIC"/im/testdata/im-wire-fixtures.json
//
// Or from the go-fabric repo root:
//   cd /Users/markus/Documents/GitHub/matter.js && \
//     node "$GO_FABRIC"/notes/parity/matter/generate-im-wire-fixtures.ts \
//     > "$GO_FABRIC"/im/testdata/im-wire-fixtures.json
//
// The generated im-wire-fixtures.json is committed to the repo. The Go test
// at internal/north/matter/im/parity_wire_fixtures_test.go loads it via
// go:embed and asserts that each Go MarshalTLV call produces the same bytes.
//
// Notes on by-design divergences documented in notes/parity/by_design.md:
//   - SubscribeResponse: Go uses PutUint32/PutUint16 for subscriptionId and
//     maxInterval because chip-tool and Apple Home require explicit-width
//     encoding per the spec (not magnitude-encoded like matter.js TlvUInt32).
//   - ReportData keepalive: matter.js omits the attributeReports field
//     when undefined; Go always emits an empty attributeReports array.
//     These by-design cases are marked with "byDesignDivergence: true"
//     and skipped in the strict byte-equality assertion.

// Resolve @matter/types from the matter.js checkout. The script may be
// invoked from any working directory; we use Module._resolveFilename to
// find the package relative to the matter.js checkout root before
// requiring it.
const Module = require("module");
const path = require("path");

// Locate matter.js checkout: two directories up from this script's
// notes/parity/matter/ location.
const scriptDir = __dirname;
const matterJsRoot = path.resolve(scriptDir, "../../../../matter.js");
const typesMain = path.join(matterJsRoot, "node_modules/@matter/types/dist/cjs/index.js");

const {
    TlvDataReportForSend,
    TlvSubscribeRequest,
    TlvSubscribeResponse,
    TlvStatusResponse,
    TlvReadRequest,
    TlvWriteRequest,
    TlvWriteResponse,
    TlvInvokeRequest,
    TlvInvokeResponseForSend,
    TlvTimedRequest,
    TlvBoolean,
    TlvString,
    TlvArray,
    TlvUInt8,
} = require(typesMain);

// The IM revision matter.js stamps on every message it sends
// (packages/model/src/common/Specification.ts INTERACTION_MODEL_REVISION,
// 12 at the Matter 1.6.1 pin). Read from the checkout rather than written
// as a literal, so a regeneration follows matter.js instead of a stale copy.
const modelMain = path.join(matterJsRoot, "node_modules/@matter/model/dist/cjs/index.js");
const { Specification } = require(modelMain);
const IM_REVISION: number = Specification.INTERACTION_MODEL_REVISION;

function toHex(bytes: Uint8Array): string {
    return Buffer.from(bytes).toString("hex");
}

interface Fixture {
    label: string;
    description: string;
    type: string;
    fixture: any;
    bytesHex: string;
    // When true the Go encoder intentionally diverges from matter.js on this
    // case; the test skips byte-equality but still exercises the encoder
    // without panicking and verifies a decode round-trip.
    byDesignDivergence?: boolean;
    byDesignNote?: string;
}

const out: Fixture[] = [];

function add(label: string, description: string, msgType: string, input: any, schema: any, opts?: { byDesign?: boolean; byDesignNote?: string }) {
    const bytes = schema.encode(input);
    out.push({
        label,
        description,
        type: msgType,
        fixture: input,
        bytesHex: toHex(bytes),
        ...(opts?.byDesign ? { byDesignDivergence: true, byDesignNote: opts.byDesignNote } : {}),
    });
}

// =============================================================
// ReportData (TlvDataReportForSend)
// =============================================================

// F3 regression case: suppressResponse=false on a non-empty ReportData.
// Before fix F3, go-fabric omitted suppressResponse when value was false.
// matter.js always emits it as TlvOptionalField with a concrete value.
add(
    "report_data_supress_false_empty_attrs",
    "ReportData with empty attributeReports and suppressResponse=false — the F3 regression fixture",
    "ReportData",
    {
        suppressResponse: false,
        attributeReports: [],
        interactionModelRevision: IM_REVISION,
    },
    TlvDataReportForSend,
);

// suppressResponse=true: keepalive path
// Note: matter.js omits attributeReports when it is undefined.
// Go always emits an empty attributeReports array even on keepalives.
// This is a by-design divergence documented in notes/parity/by_design.md.
add(
    "report_data_keepalive_no_subid",
    "ReportData keepalive (suppressResponse=true, no subscriptionId, no attributeReports field)",
    "ReportData",
    {
        suppressResponse: true,
        interactionModelRevision: IM_REVISION,
    },
    TlvDataReportForSend,
    {
        byDesign: true,
        byDesignNote: "matter.js omits attributeReports when undefined; Go always emits an empty array",
    },
);

// suppressResponse=true with subscriptionId: the typical keepalive from
// ServerSubscription.ts:777 — subscriptionId present, no attributeReports.
add(
    "report_data_keepalive_with_subid",
    "ReportData keepalive with subscriptionId=5, suppressResponse=true",
    "ReportData",
    {
        subscriptionId: 5,
        suppressResponse: true,
        interactionModelRevision: IM_REVISION,
    },
    TlvDataReportForSend,
    {
        byDesign: true,
        byDesignNote: "matter.js omits attributeReports when undefined; Go always emits an empty array",
    },
);

// suppressResponse=false with subscriptionId + empty attributeReports.
// By-design: Go uses PutUint32 for subscriptionId (always 4-byte); matter.js
// TlvUInt32 magnitude-encodes (subscriptionId=42 fits in 1 byte → 1-byte wire).
// The Go code comment in read.go explains the Apple Home requirement.
add(
    "report_data_with_subid_supress_false",
    "ReportData: subscriptionId=42, suppressResponse=false, empty attributeReports",
    "ReportData",
    {
        subscriptionId: 42,
        suppressResponse: false,
        attributeReports: [],
        interactionModelRevision: IM_REVISION,
    },
    TlvDataReportForSend,
    {
        byDesign: true,
        byDesignNote: "Go uses PutUint32 for SubscriptionId (always 4-byte); matter.js TlvUInt32 magnitude-encodes small values. Apple Home requires fixed-width.",
    },
);

// moreChunkedMessages=true — by-design: subscriptionId fixed-width divergence.
add(
    "report_data_more_chunked_supress_false",
    "ReportData: moreChunkedMessages=true (mid-chunk), suppressResponse=false, subscriptionId=42",
    "ReportData",
    {
        subscriptionId: 42,
        suppressResponse: false,
        attributeReports: [],
        moreChunkedMessages: true,
        interactionModelRevision: IM_REVISION,
    },
    TlvDataReportForSend,
    {
        byDesign: true,
        byDesignNote: "Go uses PutUint32 for SubscriptionId (always 4-byte); matter.js TlvUInt32 magnitude-encodes small values.",
    },
);

// moreChunkedMessages=true with suppressResponse=false and no subscription
add(
    "report_data_chunked_no_subid",
    "ReportData: moreChunkedMessages=true, no subscriptionId (read-request chunk)",
    "ReportData",
    {
        suppressResponse: false,
        attributeReports: [],
        moreChunkedMessages: true,
        interactionModelRevision: IM_REVISION,
    },
    TlvDataReportForSend,
);

// eventReports present (no attributeReports) — by-design: subscriptionId fixed-width.
add(
    "report_data_event_only",
    "ReportData: eventReports array present (non-empty), suppressResponse=false",
    "ReportData",
    {
        subscriptionId: 99,
        suppressResponse: false,
        eventReports: [],
        interactionModelRevision: IM_REVISION,
    },
    TlvDataReportForSend,
    {
        byDesign: true,
        byDesignNote: "Go uses PutUint32 for SubscriptionId (always 4-byte); matter.js TlvUInt32 magnitude-encodes small values.",
    },
);

// Both moreChunkedMessages and suppressResponse=true (final chunk of subscription keepalive)
add(
    "report_data_final_chunk_supress_true",
    "ReportData: final chunk, suppressResponse=true (no more chunks)",
    "ReportData",
    {
        subscriptionId: 7,
        suppressResponse: true,
        interactionModelRevision: IM_REVISION,
    },
    TlvDataReportForSend,
    {
        byDesign: true,
        byDesignNote: "matter.js omits attributeReports when undefined; Go always emits an empty array",
    },
);

// =============================================================
// SubscribeRequest (TlvSubscribeRequest)
// =============================================================

// SubscribeRequest fixtures: the client encoder (im.SubscribeRequest.MarshalTLV,
// ADR 0020 stage 3) omits empty optional lists and stamps the IM revision, as
// matter.js's TlvSubscribeRequest does; every fixture is byte-compared.

add(
    "subscribe_req_minimal_keep_true",
    "SubscribeRequest: keepSubscriptions=true, intervals=0/60, isFabricFiltered=false",
    "SubscribeRequest",
    {
        keepSubscriptions: true,
        minIntervalFloorSeconds: 0,
        maxIntervalCeilingSeconds: 60,
        isFabricFiltered: false,
        interactionModelRevision: IM_REVISION,
    },
    TlvSubscribeRequest,
);

add(
    "subscribe_req_minimal_keep_false",
    "SubscribeRequest: keepSubscriptions=false, intervals=0/30, isFabricFiltered=false",
    "SubscribeRequest",
    {
        keepSubscriptions: false,
        minIntervalFloorSeconds: 0,
        maxIntervalCeilingSeconds: 30,
        isFabricFiltered: false,
        interactionModelRevision: IM_REVISION,
    },
    TlvSubscribeRequest,
);

// With attribute requests
add(
    "subscribe_req_with_attrs_fabric_filtered",
    "SubscribeRequest: one attribute path, isFabricFiltered=true",
    "SubscribeRequest",
    {
        keepSubscriptions: false,
        minIntervalFloorSeconds: 1,
        maxIntervalCeilingSeconds: 30,
        attributeRequests: [{ endpointId: 1, clusterId: 6, attributeId: 0 }],
        isFabricFiltered: true,
        interactionModelRevision: IM_REVISION,
    },
    TlvSubscribeRequest,
);

// With event requests
add(
    "subscribe_req_with_events",
    "SubscribeRequest: one event path, no attribute requests",
    "SubscribeRequest",
    {
        keepSubscriptions: true,
        minIntervalFloorSeconds: 0,
        maxIntervalCeilingSeconds: 60,
        eventRequests: [{ endpointId: 1, clusterId: 6, eventId: 0 }],
        isFabricFiltered: false,
        interactionModelRevision: IM_REVISION,
    },
    TlvSubscribeRequest,
);

// With dataVersionFilters
add(
    "subscribe_req_with_dvf",
    "SubscribeRequest: with one dataVersionFilter (endpoint=1, cluster=6, dataVersion=100)",
    "SubscribeRequest",
    {
        keepSubscriptions: false,
        minIntervalFloorSeconds: 0,
        maxIntervalCeilingSeconds: 30,
        isFabricFiltered: false,
        dataVersionFilters: [{ path: { endpointId: 1, clusterId: 6 }, dataVersion: 100 }],
        interactionModelRevision: IM_REVISION,
    },
    TlvSubscribeRequest,
);

// =============================================================
// SubscribeResponse (TlvSubscribeResponse)
// =============================================================

// Canonical subscribe response.
// By-design divergence: matter.js uses magnitude-encoded TlvUInt32 for
// subscriptionId; Go uses PutUint32 (always 4-byte) because chip-tool
// and Apple Home require explicit-width encoding.
add(
    "subscribe_resp_canonical",
    "SubscribeResponse: subscriptionId=1, maxInterval=30",
    "SubscribeResponse",
    {
        subscriptionId: 1,
        maxInterval: 30,
        interactionModelRevision: IM_REVISION,
    },
    TlvSubscribeResponse,
    {
        byDesign: true,
        byDesignNote: "Go uses PutUint32/PutUint16 (fixed-width); matter.js TlvUInt32/UInt16 magnitude-encodes. Spec requires fixed-width; chip-tool and Apple reject magnitude-encoded subId/maxInterval.",
    },
);

add(
    "subscribe_resp_large_subid",
    "SubscribeResponse: subscriptionId=0x12345678 (forces 4-byte encoding), maxInterval=120",
    "SubscribeResponse",
    {
        subscriptionId: 0x12345678,
        maxInterval: 120,
        interactionModelRevision: IM_REVISION,
    },
    TlvSubscribeResponse,
    {
        byDesign: true,
        byDesignNote: "Go uses PutUint32/PutUint16 (fixed-width); matter.js TlvUInt32/UInt16 magnitude-encodes.",
    },
);

// =============================================================
// StatusResponse (TlvStatusResponse)
// =============================================================

add(
    "status_resp_success",
    "StatusResponse: status=SUCCESS(0)",
    "StatusResponse",
    { status: 0, interactionModelRevision: IM_REVISION },
    TlvStatusResponse,
);

add(
    "status_resp_unsupported_attribute",
    "StatusResponse: status=UNSUPPORTED_ATTRIBUTE(0x86)",
    "StatusResponse",
    { status: 0x86, interactionModelRevision: IM_REVISION },
    TlvStatusResponse,
);

add(
    "status_resp_unsupported_cluster",
    "StatusResponse: status=UNSUPPORTED_CLUSTER(0xC3)",
    "StatusResponse",
    { status: 0xc3, interactionModelRevision: IM_REVISION },
    TlvStatusResponse,
);

add(
    "status_resp_failure",
    "StatusResponse: status=FAILURE(0x01)",
    "StatusResponse",
    { status: 0x01, interactionModelRevision: IM_REVISION },
    TlvStatusResponse,
);

add(
    "status_resp_invalid_action",
    "StatusResponse: status=INVALID_ACTION(0x80)",
    "StatusResponse",
    { status: 0x80, interactionModelRevision: IM_REVISION },
    TlvStatusResponse,
);

add(
    "status_resp_busy",
    "StatusResponse: status=BUSY(0x9C)",
    "StatusResponse",
    { status: 0x9c, interactionModelRevision: IM_REVISION },
    TlvStatusResponse,
);

// =============================================================
// ReadRequest (TlvReadRequest)
// =============================================================

// ReadRequest fixtures: the client encoder (im.ReadRequest.MarshalTLV, ADR 0020
// stage 3) omits empty optional lists and stamps the IM revision, as matter.js's
// TlvReadRequest does; every fixture is byte-compared.

add(
    "read_req_minimal",
    "ReadRequest: no attributeRequests, isFabricFiltered=false",
    "ReadRequest",
    { isFabricFiltered: false, interactionModelRevision: IM_REVISION },
    TlvReadRequest,
);

add(
    "read_req_with_attrs",
    "ReadRequest: one attribute path (ep=1, cluster=6, attr=0), isFabricFiltered=false",
    "ReadRequest",
    {
        attributeRequests: [{ endpointId: 1, clusterId: 6, attributeId: 0 }],
        isFabricFiltered: false,
        interactionModelRevision: IM_REVISION,
    },
    TlvReadRequest,
);

add(
    "read_req_wildcard",
    "ReadRequest: wildcard attribute path (no endpointId/clusterId/attributeId), isFabricFiltered=false",
    "ReadRequest",
    {
        attributeRequests: [{}],
        isFabricFiltered: false,
        interactionModelRevision: IM_REVISION,
    },
    TlvReadRequest,
);

add(
    "read_req_fabric_filtered",
    "ReadRequest: one attribute path, isFabricFiltered=true",
    "ReadRequest",
    {
        attributeRequests: [{ endpointId: 0, clusterId: 0x001f, attributeId: 0 }],
        isFabricFiltered: true,
        interactionModelRevision: IM_REVISION,
    },
    TlvReadRequest,
);

add(
    "read_req_with_events",
    "ReadRequest: event requests for cluster 6 event 0 on ep=1",
    "ReadRequest",
    {
        eventRequests: [{ endpointId: 1, clusterId: 6, eventId: 0 }],
        isFabricFiltered: false,
        interactionModelRevision: IM_REVISION,
    },
    TlvReadRequest,
);

// =============================================================
// WriteRequest (TlvWriteRequest)
// =============================================================

// WriteRequest fixtures: the client encoder (im.WriteRequest.MarshalTLV, ADR 0020
// stage 3). Write() always sets moreChunkedMessages false
// (packages/protocol/src/action/request/Write.ts:77), so every fixture carries it.
const boolTrueTlv = TlvBoolean.encodeTlv(true);

add(
    "write_req_not_timed",
    "WriteRequest: timedRequest=false, one AttributeDataIB (OnOff attr, bool true)",
    "WriteRequest",
    {
        timedRequest: false,
        writeRequests: [
            {
                path: { endpointId: 1, clusterId: 6, attributeId: 0 },
                data: boolTrueTlv,
            },
        ],
        moreChunkedMessages: false,
        interactionModelRevision: IM_REVISION,
    },
    TlvWriteRequest,
);

add(
    "write_req_timed",
    "WriteRequest: timedRequest=true (timed write transaction)",
    "WriteRequest",
    {
        timedRequest: true,
        writeRequests: [
            {
                path: { endpointId: 1, clusterId: 6, attributeId: 0 },
                data: boolTrueTlv,
            },
        ],
        moreChunkedMessages: false,
        interactionModelRevision: IM_REVISION,
    },
    TlvWriteRequest,
);

add(
    "write_req_suppress_response",
    "WriteRequest: suppressResponse=true, timedRequest=false",
    "WriteRequest",
    {
        suppressResponse: true,
        timedRequest: false,
        writeRequests: [
            {
                path: { endpointId: 1, clusterId: 6, attributeId: 0 },
                data: boolTrueTlv,
            },
        ],
        moreChunkedMessages: false,
        interactionModelRevision: IM_REVISION,
    },
    TlvWriteRequest,
);

// =============================================================
// WriteResponse (TlvWriteResponse)
// =============================================================

add(
    "write_resp_success_single",
    "WriteResponse: single attribute status SUCCESS",
    "WriteResponse",
    {
        writeResponses: [
            {
                path: { endpointId: 1, clusterId: 6, attributeId: 0 },
                status: { status: 0 },
            },
        ],
        interactionModelRevision: IM_REVISION,
    },
    TlvWriteResponse,
);

add(
    "write_resp_empty",
    "WriteResponse: empty writeResponses array",
    "WriteResponse",
    {
        writeResponses: [],
        interactionModelRevision: IM_REVISION,
    },
    TlvWriteResponse,
);

add(
    "write_resp_error_single",
    "WriteResponse: single attribute status UnsupportedAttribute(0x86)",
    "WriteResponse",
    {
        writeResponses: [
            {
                path: { endpointId: 1, clusterId: 6, attributeId: 99 },
                status: { status: 0x86 },
            },
        ],
        interactionModelRevision: IM_REVISION,
    },
    TlvWriteResponse,
);

// =============================================================
// InvokeRequest (TlvInvokeRequest)
// =============================================================

// InvokeRequest fixtures: the bridge decodes InvokeRequests (sent by controllers),
// it does not encode them. These fixtures are used for Stage-3 round-trip decode
// tests only. The encode path (MarshalTLV) does not exist for InvokeRequest.
add(
    "invoke_req_not_timed_not_suppressed",
    "InvokeRequest: suppressResponse=false, timedRequest=false, one command — decode-only fixture",
    "InvokeRequest",
    {
        suppressResponse: false,
        timedRequest: false,
        invokeRequests: [
            {
                commandPath: { endpointId: 1, clusterId: 6, commandId: 0 },
                commandFields: [],
            },
        ],
        interactionModelRevision: IM_REVISION,
    },
    TlvInvokeRequest,
    {
        byDesign: true,
        byDesignNote: "InvokeRequest is decode-only in go-fabric (bridge receives, never sends); no MarshalTLV to test",
    },
);

add(
    "invoke_req_timed",
    "InvokeRequest: timedRequest=true (door-lock timed invoke) — decode-only fixture",
    "InvokeRequest",
    {
        suppressResponse: false,
        timedRequest: true,
        invokeRequests: [
            {
                commandPath: { endpointId: 1, clusterId: 0x101, commandId: 0 },
                commandFields: [],
            },
        ],
        interactionModelRevision: IM_REVISION,
    },
    TlvInvokeRequest,
    {
        byDesign: true,
        byDesignNote: "InvokeRequest is decode-only in go-fabric (bridge receives, never sends); no MarshalTLV to test",
    },
);

add(
    "invoke_req_suppress_response",
    "InvokeRequest: suppressResponse=true, timedRequest=false — decode-only fixture",
    "InvokeRequest",
    {
        suppressResponse: true,
        timedRequest: false,
        invokeRequests: [
            {
                commandPath: { endpointId: 0, clusterId: 0x0003, commandId: 0 },
                commandFields: [],
            },
        ],
        interactionModelRevision: IM_REVISION,
    },
    TlvInvokeRequest,
    {
        byDesign: true,
        byDesignNote: "InvokeRequest is decode-only in go-fabric (bridge receives, never sends); no MarshalTLV to test",
    },
);

add(
    "invoke_req_empty_requests",
    "InvokeRequest: no invoke requests — decode-only fixture",
    "InvokeRequest",
    {
        suppressResponse: false,
        timedRequest: false,
        invokeRequests: [],
        interactionModelRevision: IM_REVISION,
    },
    TlvInvokeRequest,
    {
        byDesign: true,
        byDesignNote: "InvokeRequest is decode-only in go-fabric (bridge receives, never sends); no MarshalTLV to test",
    },
);

// =============================================================
// InvokeResponseForSend (TlvInvokeResponseForSend)
// =============================================================

add(
    "invoke_resp_empty_success",
    "InvokeResponseForSend: suppressResponse=false, empty invokeResponses",
    "InvokeResponse",
    {
        suppressResponse: false,
        invokeResponses: [],
        interactionModelRevision: IM_REVISION,
    },
    TlvInvokeResponseForSend,
);

add(
    "invoke_resp_suppress_true",
    "InvokeResponseForSend: suppressResponse=true, empty invokeResponses",
    "InvokeResponse",
    {
        suppressResponse: true,
        invokeResponses: [],
        interactionModelRevision: IM_REVISION,
    },
    TlvInvokeResponseForSend,
);

// =============================================================
// TimedRequest (TlvTimedRequest)
// =============================================================
// TimedRequest fixtures: the client encoder (im.TimedRequest.MarshalTLV, ADR 0020
// stage 3), as InteractionClientMessenger.sendTimedRequest fills it.

add(
    "timed_req_short",
    "TimedRequest: timeout=6000ms (typical door-lock timed-request window)",
    "TimedRequest",
    { timeout: 6000, interactionModelRevision: IM_REVISION },
    TlvTimedRequest,
);

add(
    "timed_req_max",
    "TimedRequest: timeout=65535ms (maximum uint16 value)",
    "TimedRequest",
    { timeout: 65535, interactionModelRevision: IM_REVISION },
    TlvTimedRequest,
);

add(
    "timed_req_minimal",
    "TimedRequest: timeout=1ms",
    "TimedRequest",
    { timeout: 1, interactionModelRevision: IM_REVISION },
    TlvTimedRequest,
);

// =============================================================
// Client requests with the optional lists (ADR 0020 stage 3)
// =============================================================

add(
    "read_req_event_filters_dvf",
    "ReadRequest: attribute and event paths, one eventFilter (eventMin only, Read.ts:171-176), one dataVersionFilter, fabric-filtered",
    "ReadRequest",
    {
        attributeRequests: [{ endpointId: 1, clusterId: 6 }],
        eventRequests: [{ endpointId: 1, clusterId: 6 }],
        eventFilters: [{ eventMin: 0x1_0000_0001 }],
        isFabricFiltered: true,
        dataVersionFilters: [{ path: { endpointId: 1, clusterId: 6 }, dataVersion: 0x8000_0001 }],
        interactionModelRevision: IM_REVISION,
    },
    TlvReadRequest,
);

add(
    "subscribe_req_event_filters",
    "SubscribeRequest: attribute and event paths, an eventFilter with nodeId, a dataVersionFilter, intervals 1/3600",
    "SubscribeRequest",
    {
        keepSubscriptions: true,
        minIntervalFloorSeconds: 1,
        maxIntervalCeilingSeconds: 3600,
        attributeRequests: [{ endpointId: 1, clusterId: 6, attributeId: 0 }],
        eventRequests: [{ clusterId: 0x28 }],
        eventFilters: [{ nodeId: 0x1234, eventMin: 7 }],
        isFabricFiltered: false,
        dataVersionFilters: [{ path: { endpointId: 0, clusterId: 0x28 }, dataVersion: 5 }],
        interactionModelRevision: IM_REVISION,
    },
    TlvSubscribeRequest,
);

add(
    "write_req_data_version",
    "WriteRequest: one AttributeDataIB with dataVersion=0x01020304 (NodeLabel, a string)",
    "WriteRequest",
    {
        timedRequest: false,
        writeRequests: [
            {
                dataVersion: 0x01020304,
                path: { endpointId: 0, clusterId: 0x28, attributeId: 5 },
                data: TlvString.encodeTlv("label"),
            },
        ],
        moreChunkedMessages: false,
        interactionModelRevision: IM_REVISION,
    },
    TlvWriteRequest,
);

// The list chunking of Write.ts:149-171: REPLACE-ALL as the empty list
// (listIndex undefined, ArraySchema.encodeAsChunkedArray, TlvArray.ts:156-164),
// then one ADD per element with listIndex null.
const chunked = TlvArray(TlvUInt8).encodeAsChunkedArray([1, 2]);
add(
    "write_req_chunked_list",
    "WriteRequest: a list of two uint8 chunked into REPLACE-ALL [] and two appends (listIndex null), timed",
    "WriteRequest",
    {
        timedRequest: true,
        writeRequests: chunked.map(({ element, listIndex }: { element: any; listIndex: number | null | undefined }) => ({
            path: { endpointId: 1, clusterId: 0x1e, attributeId: 0, listIndex },
            data: element,
        })),
        moreChunkedMessages: false,
        interactionModelRevision: IM_REVISION,
    },
    TlvWriteRequest,
);

console.log(JSON.stringify(out, null, 2));
