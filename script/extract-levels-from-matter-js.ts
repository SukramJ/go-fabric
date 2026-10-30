// Emits one specification level — the ids matter.js's model carries at one
// tagged release — as JSON on stdout, for parity/levels/<level>.json
// (concept v11 §C.5, ADR 0030 decision 5, brief S3 part 1).
//
// Deliberately slim: it imports only what @matter/model exports at every
// tag from v0.13.0 on (Matter, ClusterModel, DeviceTypeModel,
// Specification), never the "effective" layer script/extract-from-matter-js.ts
// reads (RequirementResolver does not exist at v0.13.0). Ids only — no
// names, no text, no datatypes. `mandatory` lists what the element's own
// conformance makes unconditionally mandatory at the tag's cluster
// revision: conformance text "M", or "Rev >= vN" with N at most the
// cluster's ClusterRevision (BasicInformation.ConfigurationVersion at
// revision 6, concept v11 §C.9).
//
// Run inside a matter.js checkout at the tag (make generate-level TAG=…
// copies it there, as generate-matter-schema does for the main extractor).
import { ClusterModel, DeviceTypeModel, Matter, Specification } from "@matter/model";
import { execSync } from "node:child_process";

const sorted = (ids: Iterable<number>) => [...new Set(ids)].sort((a, b) => a - b);
function isMandatory(m: any, revision: number): boolean {
    if (m.conformance === undefined) return false;
    const text = `${m.conformance}`.trim();
    if (text === "M") return true;
    const rev = /^Rev >= v(\d+)$/.exec(text);
    return rev !== null && Number(rev[1]) <= revision;
}

let sourceCommit: string | undefined;
let tag: string | undefined;
try {
    sourceCommit = execSync("git rev-parse HEAD", { encoding: "utf8" }).trim();
    tag = execSync("git describe --tags --exact-match HEAD", { encoding: "utf8" }).trim();
} catch (e) {
    console.error("provenance:", (e as Error).message);
}

const deviceTypes: any[] = [];
for (const dt of Matter.deviceTypes as DeviceTypeModel[]) {
    if (typeof dt.id !== "number") continue;
    deviceTypes.push({ id: dt.id, revision: dt.revision });
}
deviceTypes.sort((a, b) => a.id - b.id);

const clusters: any[] = [];
for (const c of Matter.clusters as ClusterModel[]) {
    if (typeof c.id !== "number") continue;
    const features: any[] = [];
    for (const f of c.features as any[]) {
        // The bit is the FeatureMap field's constraint, never its position
        // in the list: feature bits are sparse.
        const bit = f.constraint === undefined ? NaN : Number(`${f.constraint}`);
        if (!Number.isInteger(bit)) throw new Error(`cluster ${c.id} feature ${f.name}: no bit`);
        features.push({ bit, code: f.name });
    }
    features.sort((a, b) => a.bit - b.bit);
    const ids = (ms: any[]) => sorted(ms.filter(m => typeof m.id === "number").map(m => m.id));
    const mand = (ms: any[]) => ids(ms.filter(m => isMandatory(m, c.revision)));
    const attributes = c.attributes as any[];
    const commands = c.commands as any[];
    const events = c.events as any[];
    clusters.push({
        id: c.id,
        revision: c.revision,
        features,
        attributes: ids(attributes),
        commands: ids(commands),
        events: ids(events),
        mandatory: { attributes: mand(attributes), commands: mand(commands), events: mand(events) },
    });
}
clusters.sort((a, b) => a.id - b.id);

const out = {
    matter: { revision: Specification.REVISION, tag, sourceCommit, method: "model" },
    deviceTypes,
    clusters,
};
process.stdout.write(JSON.stringify(out, null, 2) + "\n");
