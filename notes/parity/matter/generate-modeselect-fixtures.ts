// Generates wire fixtures for ModeSelect SupportedModes from the matter.js
// checkout's own encoder: the attribute value as matter.js's
// TlvOfModel(element) encodes it, the schema the interaction server
// encodes attribute reports with
// (packages/node/src/node/integration/ProtocolService.ts). The values are
// in bounds (SupportedModes max 255, Label max 64, SemanticTags max 64) and
// mix small and large integers, so the fixture pins matter.js's
// smallest-width integer encoding.
//
// Master: cluster/modeselect/testdata/modeselect-wire-fixtures.json
//
// Run from anywhere, against a built matter.js checkout next to go-fabric
// (or MATTERJS_DIR=<checkout>):
//
//   node "$GO_FABRIC"/notes/parity/matter/generate-modeselect-fixtures.ts > /tmp/ms.json \
//       && mv /tmp/ms.json "$GO_FABRIC"/cluster/modeselect/testdata/modeselect-wire-fixtures.json

const path = require("path");

const matterJsRoot = process.env.MATTERJS_DIR ?? path.resolve(__dirname, "../../../../matter.js");
const mod = (name) => require(path.join(matterJsRoot, "node_modules/@matter", name, "dist/cjs/index.js"));

const general = mod("general");
const model = mod("model");
const types = mod("types");

// matter.js logs to stdout; keep the JSON clean.
general.Logger.level = "error";

const hex = (b) => Buffer.from(b).toString("hex");

function fixtures() {
    const out = [];
    const record = (label, cluster, kind, name, value) => {
        const element = cluster[kind].find((e) => e.name === name);
        if (element === undefined) {
            throw new Error(`no ${kind} ${name} in ${cluster.name}`);
        }
        out.push({
            label, kind, cluster: cluster.id, element: name,
            fixture: value, bytesHex: hex(types.TlvOfModel(element).encode(value)),
        });
    };

    const ms = model.ModeSelect;
    record("supported_modes_small_ints", ms, "attributes", "SupportedModes", [
        { label: "Normal", mode: 0, semanticTags: [{ mfgCode: 0, value: 0 }] },
        { label: "Eco", mode: 7, semanticTags: [] },
    ]);
    record("supported_modes_wide_ints", ms, "attributes", "SupportedModes", [
        { label: "Vendor", mode: 200, semanticTags: [{ mfgCode: 0xfff1, value: 0x4000 }, { mfgCode: 300, value: 2 }] },
    ]);
    record("supported_modes_utf8_label", ms, "attributes", "SupportedModes", [
        { label: "Spülen", mode: 1, semanticTags: [] },
    ]);
    record("supported_modes_empty", ms, "attributes", "SupportedModes", []);
    return out;
}

process.stdout.write(JSON.stringify(fixtures(), null, 2) + "\n");
