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
// Command ids are written without their direction: a request and a
// response with one id are one id of the level. That merge loses nothing
// only while every direction the pin (parity/schema.json) carries for an
// id is present at the tag wherever the tag has the id at all — else the
// level would admit, at that level, a direction it lacked. The extractor
// checks exactly that against the pin GOFABRIC_PIN names (generate-level.sh
// passes it) and fails naming the cluster and id (review II R2-P6-par-2713,
// wizard-6b §2 gen Q2).
//
// Run inside a matter.js checkout at the tag (make generate-level TAG=…
// copies it there, as generate-matter-schema does for the main extractor).
import { ClusterModel, DeviceTypeModel, Matter, Specification } from "@matter/model";
import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";

const sorted = (ids: Iterable<number>) => [...new Set(ids)].sort((a, b) => a - b);
function isMandatory(m: any, revision: number): boolean {
    if (m.conformance === undefined) return false;
    const text = `${m.conformance}`.trim();
    if (text === "M") return true;
    const rev = /^Rev >= v(\d+)$/.exec(text);
    return rev !== null && Number(rev[1]) <= revision;
}

// The provenance is required: a level without its tag and commit names a
// release it cannot prove it was extracted from, so a failed git call ends
// the run (review II S-11) instead of writing tag and sourceCommit
// undefined.
let sourceCommit: string;
let tag: string;
try {
    sourceCommit = execSync("git rev-parse HEAD", { encoding: "utf8" }).trim();
    tag = execSync("git describe --tags --exact-match HEAD", { encoding: "utf8" }).trim();
} catch (e) {
    console.error("provenance:", (e as Error).message);
    process.exit(1);
}
if (!sourceCommit || !tag) {
    console.error(`provenance: commit ${JSON.stringify(sourceCommit)}, tag ${JSON.stringify(tag)}: both are required`);
    process.exit(1);
}

// The pin's command directions per cluster id and command id, from the
// effective direction (the raw one when the effective layer states none),
// as script/generate_matter_schema.go's commandDirection reads them.
const pinPath = process.env.GOFABRIC_PIN;
if (!pinPath) {
    console.error("GOFABRIC_PIN is not set: run the extractor through script/generate-level.sh");
    process.exit(1);
}
const pinDirections = new Map<number, Map<number, Set<string>>>();
for (const c of JSON.parse(readFileSync(pinPath, "utf8")).clusters as any[]) {
    const byId = new Map<number, Set<string>>();
    for (const m of c.commands ?? []) {
        const dir = m.effective?.direction || m.direction;
        if (dir !== "request" && dir !== "response") {
            throw new Error(`pin cluster ${c.id} command ${m.id}: direction ${JSON.stringify(dir)}`);
        }
        if (!byId.has(m.id)) byId.set(m.id, new Set());
        byId.get(m.id)!.add(dir);
    }
    pinDirections.set(c.id, byId);
}

// checkDirections fails when the tag has a command id without a direction
// the pin carries for it: the merged id would admit that direction at the
// level although the tag lacks it.
function checkDirections(clusterId: number, commands: any[]) {
    const pin = pinDirections.get(clusterId);
    if (pin === undefined) return;
    const atTag = new Map<number, Set<string>>();
    for (const m of commands) {
        if (typeof m.id !== "number") continue;
        if (!atTag.has(m.id)) atTag.set(m.id, new Set());
        atTag.get(m.id)!.add(m.effectiveDirection === "response" ? "response" : m.effectiveDirection === "request" ? "request" : "unknown");
    }
    for (const [id, dirs] of atTag) {
        const want = pin.get(id);
        if (want === undefined) continue;
        for (const d of want) {
            if (!dirs.has(d)) {
                throw new Error(
                    `cluster ${clusterId} command ${id}: the pin has it as ${[...want].join(" and ")}, the tag as ${[...dirs].join(" and ")}; ` +
                        "a level file merges the directions, so it would admit the missing one (R2-P6-par-2713)",
                );
            }
        }
    }
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
    checkDirections(c.id, commands);
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
