// Generates wire fixtures for the ResourceMonitoring server (cluster/filter:
// HepaFilterMonitoring, ActivatedCarbonFilterMonitoring) from the matter.js
// checkout's own encoders: the ResetCondition request payload the bridge
// decodes and the ReplacementProductList values its value writer encodes —
// each as matter.js's TlvOfModel(element) encodes it.
//
// Master: bridge/testdata/filter-wire-fixtures.json
//
// Run from anywhere, against a built matter.js checkout next to go-fabric
// (MATTERJS_DIR names another one):
//
//   node "$GO_FABRIC"/notes/parity/matter/generate-filter-fixtures.ts > /tmp/f.json \
//       && mv /tmp/f.json "$GO_FABRIC"/bridge/testdata/filter-wire-fixtures.json

const path = require("path");

const matterJsRoot = process.env.MATTERJS_DIR ?? path.resolve(__dirname, "../../../../matter.js");
const mod = (name) => require(path.join(matterJsRoot, "node_modules/@matter", name, "dist/cjs/index.js"));

const general = mod("general");
const model = mod("model");
const types = mod("types");

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

    const hepa = model.HepaFilterMonitoring;
    const carbon = model.ActivatedCarbonFilterMonitoring;
    record("hepa_reset_condition", hepa, "commands", "ResetCondition", undefined);
    record("carbon_reset_condition", carbon, "commands", "ResetCondition", undefined);
    record("hepa_products", hepa, "attributes", "ReplacementProductList", [
        { productIdentifierType: 0, productIdentifierValue: "012345678905" },
        { productIdentifierType: 4, productIdentifierValue: "HEPA-H13" },
    ]);
    record("carbon_products", carbon, "attributes", "ReplacementProductList", [
        { productIdentifierType: 2, productIdentifierValue: "4006381333931" },
    ]);
    record("hepa_products_empty", hepa, "attributes", "ReplacementProductList", []);
    return out;
}

process.stdout.write(JSON.stringify(fixtures(), null, 2) + "\n");
