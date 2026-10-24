// SPDX-License-Identifier: Apache-2.0
// Extracts the AllTests tree of matter.js conformanceTest.ts (pin 862ad75f) into the
// flat case list of cluster/spec/testdata/conformance_matterjs.json:
//   git -C ../matter.js show 862ad75f:packages/node/test/behavior/state/validation/conformanceTest.ts > /tmp/c.ts
//   node notes/parity/matter/extract-conformance-cases.js /tmp/c.ts cluster/spec/testdata/conformance_matterjs.json
"use strict";
const fs = require("fs");
const [src, out] = process.argv.slice(2);
const text = fs.readFileSync(src, "utf8");
const start = text.indexOf("const AllTests = ");
const end = text.indexOf('describe("conformance"');
let body = text.slice(start + "const AllTests = ".length, end).trim();
if (body.endsWith(";")) body = body.slice(0, -1);
const lines = text.slice(0, start).split("\n").length;

const NESTING = Symbol("NESTING");
function Fields(...defs) {
    return { kind: "fields", fields: defs.map(f => ({ name: "Test", type: "int16", ...f })) };
}
function Features(def) {
    return { kind: "features", features: Object.entries(def).map(([name, title]) => ({ name, title })) };
}
function FieldElement(f) {
    return f;
}
function Tests(...defs) {
    const r = { [NESTING]: true };
    for (const d of defs) {
        if (d && d.kind === "fields") r.fields = d.fields;
        else if (d && d.kind === "features") r.features = d.features;
        else r.entries = d;
    }
    return r;
}
const ConformanceError = "ConformanceError";
const EnumValueConformanceError = "EnumValueConformanceError";
const UnknownEnumValueError = "UnknownEnumValueError";
function missing(conformance, fieldName = "test") {
    return { type: ConformanceError, message: `Validating Test.${fieldName}: Conformance "${conformance}": Matter requires you to set this attribute` };
}
function disallowed(conformance, fieldName = "test") {
    return { type: ConformanceError, message: `Validating Test.${fieldName}: Conformance "${conformance}": Matter does not allow you to set this attribute` };
}
function disallowedEnum(conformance, name, value) {
    return { type: EnumValueConformanceError, message: `Validating Test.test: Conformance "${conformance}": Matter does not allow enum value ${name} (ID ${value}) here` };
}
function undefinedEnum(name, value) {
    return { type: UnknownEnumValueError, message: `Validating Test.test: Matter does not define the enum value ${value} for ${name}` };
}

const AllTests = eval("(" + body + ")");
const cases = [];
function walk(path, what, structure) {
    structure = { ...structure };
    if (what.fields) structure.fields = what.fields;
    if (what.features) structure.features = what.features;
    if (what[NESTING]) {
        for (const k in what.entries) walk([...path, k], what.entries[k], structure);
        return;
    }
    cases.push({
        path,
        fields: structure.fields,
        features: structure.features || [],
        supports: what.supports || [],
        record: what.record === undefined ? {} : what.record,
        error: what.error,
    });
}
walk(["conformance"], AllTests, {});
fs.writeFileSync(
    out,
    JSON.stringify(
        {
            source: "matter.js packages/node/test/behavior/state/validation/conformanceTest.ts at 862ad75f, AllTests (from line " + lines + "), flattened by testValidation (validation-test-utils.ts:115-140)",
            cases,
        },
        null,
        1,
    ) + "\n",
);
console.log(cases.length, "cases");
