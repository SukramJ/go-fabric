// Generates the wire bytes of matter.js's session parameters
// (TlvSessionParameters, packages/protocol/src/session/pase/PaseMessages.ts)
// for go-fabric's secure/sessionparams parity test: what a node states in its
// PBKDFParamResponse and Sigma2 with each TCP role. The objects are the shape
// SessionManager's getter hands the PASE and CASE servers at db021da0
// (packages/protocol/src/session/SessionManager.ts:391-400): the
// supportedTransports bitmap always, maxTcpMessageSize only with a TCP role;
// ServerNetworkRuntime assigns supportedTransports from the TCP config
// whether or not a TCP role is set
// (packages/node/src/behavior/system/network/ServerNetworkRuntime.ts:204-210).
//
// Regen command (run from inside the built matter.js checkout):
//   node "$GO_FABRIC"/notes/parity/matter/generate-session-params-fixtures.ts > /tmp/sp.json \
//       && mv /tmp/sp.json "$GO_FABRIC"/secure/sessionparams/testdata/session-params-fixtures.json

const path = require("path");

const scriptDir = __dirname;
const matterJsRoot = process.env.MATTERJS_DIR ?? path.resolve(scriptDir, "../../../../matter.js");
const protocol = require(path.join(matterJsRoot, "node_modules/@matter/protocol/dist/cjs/index.js"));

const { TlvSessionParameters } = protocol;

const hex = (b: Uint8Array) => Buffer.from(b).toString("hex");

const cases: Array<{ label: string; params: any }> = [
    { label: "no_tcp", params: { supportedTransports: { tcpClient: false, tcpServer: false } } },
    { label: "tcp_client", params: { supportedTransports: { tcpClient: true, tcpServer: false }, maxTcpMessageSize: 64000 } },
    { label: "tcp_server", params: { supportedTransports: { tcpClient: false, tcpServer: true }, maxTcpMessageSize: 64000 } },
    { label: "tcp_both", params: { supportedTransports: { tcpClient: true, tcpServer: true }, maxTcpMessageSize: 64000 } },
];

const out = cases.map(c => ({ label: c.label, hex: hex(TlvSessionParameters.encode(c.params)) }));
process.stdout.write(JSON.stringify({ generator: "generate-session-params-fixtures.ts", cases: out }, null, 2) + "\n");
