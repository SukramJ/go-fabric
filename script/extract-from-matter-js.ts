// Walks @matter/model's MatterDefinition tree and emits a parity-friendly
// snapshot for this module's matter-side code: device types with revisions
// and their cluster requirements (server/client + conformance),
// clusters with revisions + featureMap + per-attribute IDs/types/conformance/
// constraints + commands + events. Output is JSON on stdout — pipe to
// parity/schema.json, the embed every parity test and the schema generator
// read.
//
// Two layers, kept apart on purpose. The raw layer (every key the extract
// carried before the "effective" additions) is the element files' own text,
// merged base-first for derived clusters exactly as before, so the parity
// tests that read it keep reading the same bytes. The resolved layer is
// matter.js's operational model (Matter, @matter/model) read through its own
// inheritance: each attribute, command, event and feature gains an
// "effective" object, each cluster its "datatypes" (and "base" when it
// derives from one), and the snapshot a "globalDatatypes" list. That is what
// script/clustergen generates cluster definitions from — see
// docs/adr/0013-generated-cluster-definitions.md.
import { ClusterModel, DatatypeModel, Matter, MatterDefinition, Specification } from "@matter/model";
import { execSync } from "node:child_process";

// ReqOut is one cluster requirement of a device type: which cluster the
// Device Library specifies for the type, on which side (server/client), and
// under which conformance. These become schema.DeviceTypeServerClusters
// (schema/devicetypes.go, generated), the oracle that decides whether an
// endpoint of a given device type may mount a cluster as a server.
interface ReqOut {
    id: number;
    name: string;
    element: string;
    conformance?: string;
}

interface DeviceTypeOut {
    id: number;
    name: string;
    classification?: string;
    revision: number;
    requirements: ReqOut[];
}

interface AttrOut {
    id: number;
    name: string;
    type?: string;
    conformance?: string;
    access?: string;
    constraint?: string;
    quality?: string;
    default?: any;
}

interface CmdOut {
    id: number;
    name: string;
    direction?: string;
    conformance?: string;
    response?: string;
}

interface EvtOut {
    id: number;
    name: string;
    priority?: string;
    conformance?: string;
}

interface ClusterOut {
    id: number;
    name: string;
    revision: number;
    featureMap: number;
    attributes: AttrOut[];
    commands: CmdOut[];
    events: EvtOut[];
    features?: { name: string; conformance?: string; description?: string; bit?: number }[];
}

const out = {
    matter: {
        revision: undefined as string | undefined,
        specificationVersion: undefined as number | undefined,
        interactionModelRevision: undefined as number | undefined,
        dataModelRevision: undefined as number | undefined,
        sourceCommit: undefined as string | undefined,
    },
    deviceTypes: [] as DeviceTypeOut[],
    clusters: [] as ClusterOut[],
};

// Matter spec metadata, straight off the @matter/model package export so the
// snapshot records which spec revision the schema was extracted at.
out.matter.revision = Specification.REVISION;
out.matter.specificationVersion = Specification.SPECIFICATION_VERSION;
out.matter.interactionModelRevision = Specification.INTERACTION_MODEL_REVISION;
out.matter.dataModelRevision = Specification.DATA_MODEL_REVISION;

try {
    // Record the matter.js HEAD commit the snapshot was extracted from, so the
    // pinned reference is traceable. This script runs inside the matter.js
    // checkout, so HEAD is matter.js's. Deterministic: changes only when the
    // matter.js source does.
    out.matter.sourceCommit = execSync("git rev-parse HEAD", {
        encoding: "utf8",
    }).trim();
} catch (e) {
    console.error("source commit:", (e as Error).message);
}

function jsonable(v: any): any {
    if (typeof v === "bigint") return v.toString();
    if (Array.isArray(v)) return v.map(jsonable);
    if (typeof v === "object" && v !== null) {
        const o: any = {};
        for (const k of Object.keys(v)) o[k] = jsonable(v[k]);
        return o;
    }
    return v;
}

const root: any = MatterDefinition;
const children: any[] = root.children ?? [];

// Index every named top-level element so a cluster declared as
// `{ name, id, type: "Base" }` (e.g. the ConcentrationMeasurement /
// ResourceMonitoring families) resolves its inherited ClusterRevision +
// members from the base. Without this, type-inheriting clusters collapse to
// revision 1 with empty attribute lists when matter.js HEAD declares them by
// reference instead of inline.
const byName = new Map<string, any>();
for (const c of children) if (c.name && !byName.has(c.name)) byName.set(c.name, c);

// Merge inherited children: base first, then own overrides, keyed by
// (tag, id|name, discriminator) — see keyOf below.
function resolvedChildren(node: any, seen: Set<string> = new Set()): any[] {
    let base: any[] = [];
    if (node.type && byName.has(node.type) && !seen.has(node.type)) {
        seen.add(node.type);
        base = resolvedChildren(byName.get(node.type), seen);
    }
    // Two elements are the same member only when matter.js's own model treats
    // them as one. Model.key is the effective id plus a per-tag discriminator
    // (../matter.js/packages/model/src/models/Model.ts:195), and exactly two
    // tags define one: a command's direction — request and response commands
    // share an id, and a command that omits `direction` is discriminated by
    // the "...Response" naming convention (models/CommandModel.ts:64) — and a
    // requirement's element type, so a cluster required on both the server and
    // the client side stays two entries (models/RequirementModel.ts:24).
    // Keying on (tag, id) alone collapses each such pair onto one survivor,
    // silently dropping members: Groups (0x0004) keeps six of its ten commands.
    const discriminatorOf = (ch: any): string => {
        if (ch.tag === "command") {
            if (ch.direction !== undefined) return String(ch.direction);
            return typeof ch.name === "string" && ch.name.endsWith("Response") ? "response" : "request";
        }
        if (ch.tag === "requirement" && ch.element !== undefined) return String(ch.element);
        return "";
    };
    const keyOf = (ch: any) => `${ch.tag}:${typeof ch.id === "number" ? ch.id : ch.name}:${discriminatorOf(ch)}`;
    const merged = new Map<string, any>();
    for (const ch of base) merged.set(keyOf(ch), ch);
    for (const ch of (node.children ?? [])) merged.set(keyOf(ch), ch); // own wins
    return [...merged.values()];
}

for (const c of children) {
    if (c.tag === "deviceType" && typeof c.id === "number") {
        let rev = 1;
        const kids = resolvedChildren(c);
        const desc = kids.find((ch: any) => ch.tag === "requirement" && ch.name === "Descriptor");
        if (desc) {
            const dtList = desc.children?.find((ch: any) => ch.name === "DeviceTypeList");
            if (dtList?.default?.[0]?.revision) rev = dtList.default[0].revision;
        }
        // Cluster requirements carry a numeric id; the nested per-attribute /
        // per-command / per-feature requirements do not and are skipped.
        const requirements: ReqOut[] = [];
        for (const ch of kids) {
            if (ch.tag !== "requirement" || typeof ch.id !== "number" || !ch.element) continue;
            requirements.push({ id: ch.id, name: ch.name, element: ch.element, conformance: ch.conformance });
        }
        requirements.sort((a, b) => a.id - b.id || a.element.localeCompare(b.element));
        out.deviceTypes.push({
            id: c.id,
            name: c.name,
            classification: c.classification,
            revision: rev,
            requirements,
        });
    } else if (c.tag === "cluster" && typeof c.id === "number") {
        let rev = 1;
        let featureMap = 0;
        const attributes: AttrOut[] = [];
        const commands: CmdOut[] = [];
        const events: EvtOut[] = [];
        const features: { name: string; conformance?: string; description?: string; bit?: number }[] = [];
        for (const ch of resolvedChildren(c)) {
            if (ch.tag === "attribute") {
                if (ch.id === 0xFFFD || ch.name === "ClusterRevision") {
                    if (ch.default !== undefined) rev = ch.default;
                }
                if (ch.id === 0xFFFC || ch.name === "FeatureMap") {
                    if (typeof ch.default === "number") featureMap = ch.default;
                    for (const f of (ch.children ?? [])) {
                        if (f.tag === "field") {
                            // `constraint` is the bit POSITION, which is NOT the
                            // field's index in this list: feature bits are sparse
                            // (DoorLock has no bit 3 and no bit 9), so deriving a
                            // bit from array order mislabels every feature after
                            // the first gap. Recording it is what lets a
                            // conformance check read the position instead of
                            // assuming one.
                            const bit = f.constraint === undefined ? undefined : Number(f.constraint);
                            features.push({
                                name: f.name,
                                conformance: f.conformance,
                                description: f.description,
                                bit: Number.isInteger(bit) ? bit : undefined,
                            });
                        }
                    }
                }
                if (typeof ch.id === "number") {
                    attributes.push({
                        id: ch.id,
                        name: ch.name,
                        type: ch.type,
                        conformance: ch.conformance,
                        access: ch.access,
                        constraint: ch.constraint,
                        quality: ch.quality,
                        default: ch.default !== undefined ? jsonable(ch.default) : undefined,
                    });
                }
            } else if (ch.tag === "command" && typeof ch.id === "number") {
                commands.push({ id: ch.id, name: ch.name, direction: ch.direction, conformance: ch.conformance, response: ch.response });
            } else if (ch.tag === "event" && typeof ch.id === "number") {
                events.push({ id: ch.id, name: ch.name, priority: ch.priority, conformance: ch.conformance });
            }
        }
        attributes.sort((a, b) => a.id - b.id);
        commands.sort((a, b) => a.id - b.id);
        events.sort((a, b) => a.id - b.id);
        out.clusters.push({ id: c.id, name: c.name, revision: rev, featureMap, attributes, commands, events, features });
    }
}

out.deviceTypes.sort((a, b) => a.id - b.id);
out.clusters.sort((a, b) => a.id - b.id);

// ---------------------------------------------------------------------------
// Resolved layer.
//
// Every value below is read through matter.js's operational model rather than
// the element text: ClusterModel.attributes / commands / events / datatypes
// include what a cluster inherits from its base (ModeBase, OperationalState,
// ConcentrationMeasurement, ResourceMonitoring, AlarmBase, …), and the
// effective* accessors (packages/model/src/models/ValueModel.ts) walk the
// shadow chain an override leaves partial. Aspects are emitted as matter.js's
// own ASTs (Conformance.ast, the Access / Quality / Constraint fields), so the
// Go side evaluates the same structure matter.js evaluates instead of
// re-parsing the text.

// declaredType is the type the element text states, found by walking the
// shadow chain: a derived cluster's override (RvcRunMode.SupportedModes)
// states no type and takes its base's (ModeBase.SupportedModes, "list").
function declaredType(m: any): string | undefined {
    const seen = new Set<any>();
    let x = m;
    while (x !== undefined && x.type === undefined && !seen.has(x)) {
        seen.add(x);
        x = x.shadow;
    }
    return x?.type;
}

// effectiveDefault is the default the shadow chain states, as jsonable.
function effectiveDefault(m: any): any {
    const seen = new Set<any>();
    let x = m;
    while (x !== undefined && !seen.has(x)) {
        if (x.default !== undefined) return jsonable(x.default);
        seen.add(x);
        x = x.shadow;
    }
    return undefined;
}

function conformanceOut(c: any): any {
    if (c === undefined || c.isEmpty) return undefined;
    return { text: String(c), ast: jsonable(c.ast) };
}

function accessOut(a: any): any {
    const o: any = {};
    for (const k of ["rw", "readPriv", "writePriv", "fabric", "timed"]) {
        if (a?.[k] !== undefined) o[k] = a[k];
    }
    return Object.keys(o).length ? o : undefined;
}

const qualityFlags = [
    "nullable", "nonvolatile", "fixed", "scene", "reportable", "changesOmitted",
    "singleton", "quieter", "largeMessage", "diagnostics", "atomic",
];

function qualityOut(q: any): any {
    const o: any = {};
    for (const k of qualityFlags) {
        if (q?.[k] === true) o[k] = true;
    }
    return Object.keys(o).length ? o : undefined;
}

// constraintOut keeps the structure matter.js parses a constraint into
// (packages/model/src/aspects/Constraint.ts): numeric bounds stay numbers, a
// bound naming a sibling ("minMeasuredValue to maxMeasuredValue") stays the
// FieldValue reference matter.js resolves at validation time.
function constraintOut(c: any): any {
    if (c === undefined || c.isEmpty) return undefined;
    const o: any = { text: String(c) };
    for (const k of ["none", "desc", "value", "min", "max", "in", "cpMax"]) {
        if (c[k] !== undefined) o[k] = jsonable(c[k]);
    }
    if (c.entry !== undefined) o.entry = constraintOut(c.entry);
    if (c.parts !== undefined) o.parts = c.parts.map(constraintOut);
    return o;
}

// typeOut describes the value type: the stated type name, matter.js's
// metatype, the primitive it encodes as, and — for a named datatype — where
// that datatype lives, so a generator can tell a cluster's own
// ModeOptionStruct from the global "percent".
function typeOut(m: any): any {
    const o: any = {};
    const type = declaredType(m);
    if (type !== undefined) o.type = type;
    const meta = m.effectiveMetatype;
    if (meta !== undefined) o.metatype = meta;
    const prim = m.primitiveBase?.name;
    if (prim !== undefined) o.primitive = prim;
    const base = m.base;
    if (base instanceof DatatypeModel) {
        const owner: any = base.parent;
        if (owner instanceof ClusterModel) o.scope = "cluster";
        else if (owner !== undefined && owner.tag === "matter" && base.children.length) o.scope = "global";
    }
    const entry = m.listEntry;
    if (entry !== undefined) o.entry = valueOut(entry, "member");
    // An anonymous struct ("struct" stated inline, as the AttributeStatus
    // entries of Thermostat's AtomicResponse) defines its fields in place;
    // a named one is described once, under the datatypes.
    if (type === "struct" && meta === "object") o.fields = fieldsOut(m);
    return o;
}

// valueOut is the effective description of an attribute ("element") or of a
// struct, command or event field or list entry ("member"). A member's access
// is its record's — only a fabric-sensitive field ("S") states one of its
// own that matters on the wire — so a member carries just that.
function valueOut(m: any, role: "element" | "member" = "element"): any {
    const o: any = typeOut(m);
    const conformance = conformanceOut(m.effectiveConformance);
    if (conformance !== undefined) o.conformance = conformance;
    const access = accessOut(m.effectiveAccess);
    if (role === "element" && access !== undefined) o.access = access;
    if (role === "member" && access?.fabric !== undefined) o.access = { fabric: access.fabric };
    const quality = qualityOut(m.effectiveQuality);
    if (quality !== undefined) o.quality = quality;
    const constraint = constraintOut(m.effectiveConstraint);
    if (constraint !== undefined) o.constraint = constraint;
    const def = effectiveDefault(m);
    if (def !== undefined) o.default = def;
    return o;
}

// enumeratedOut describes one value of an enum or one bit (range) of a
// bitmap: its id or bits, its name, and the conformance that gates it on a
// feature ("Warning" of ChangeIndicationEnum needs WRN).
function enumeratedOut(f: any): any {
    const o: any = {};
    const id = f.effectiveId;
    if (id !== undefined) o.id = id;
    o.name = f.name;
    if (f.title !== undefined) o.title = f.title;
    const conformance = conformanceOut(f.effectiveConformance);
    if (conformance !== undefined) o.conformance = conformance;
    const constraint = constraintOut(f.effectiveConstraint);
    if (constraint !== undefined) o.constraint = constraint;
    return o;
}

// fieldsOut lists a struct's, command's, event's, enum's or bitmap's members
// as matter.js's Scope resolves them (base members included), in a stable
// order: by id, a bitmap's by its first bit.
function fieldsOut(m: any): any[] {
    const meta = m.effectiveMetatype;
    const enumerated = meta === "enum" || meta === "bitmap";
    const out: any[] = [];
    for (const f of m.members) {
        if (f.tag !== "field") continue;
        if (enumerated) {
            out.push(enumeratedOut(f));
            continue;
        }
        const o: any = {};
        const id = f.effectiveId;
        if (id !== undefined) o.id = id;
        o.name = f.name;
        Object.assign(o, valueOut(f, "member"));
        out.push(o);
    }
    const order = (o: any) => {
        if (typeof o.id === "number") return o.id;
        const c = o.constraint;
        if (typeof c?.value === "number") return c.value;
        if (typeof c?.min === "number") return c.min;
        return Number.MAX_SAFE_INTEGER;
    };
    out.sort((a, b) => order(a) - order(b) || a.name.localeCompare(b.name));
    return out;
}

function datatypeOut(d: any): any {
    const o: any = { name: d.name, ...typeOut(d) };
    const conformance = conformanceOut(d.effectiveConformance);
    if (conformance !== undefined) o.conformance = conformance;
    const constraint = constraintOut(d.effectiveConstraint);
    if (constraint !== undefined) o.constraint = constraint;
    o.fields = fieldsOut(d);
    return o;
}

function eventPriority(e: any): string | undefined {
    const seen = new Set<any>();
    let x = e;
    while (x !== undefined && !seen.has(x)) {
        if (x.priority !== undefined) return x.priority;
        seen.add(x);
        x = x.shadow;
    }
    return undefined;
}

const unmatched: string[] = [];

for (const raw of out.clusters as any[]) {
    const model: any = Matter.get(ClusterModel, raw.id);
    if (model === undefined) {
        unmatched.push(`cluster ${raw.name}`);
        continue;
    }
    if (model.type !== undefined && model.base instanceof ClusterModel) raw.base = model.type;

    const attrs = new Map<number, any>();
    for (const a of model.attributes) if (typeof a.id === "number") attrs.set(a.id, a);
    for (const a of raw.attributes) {
        const m = attrs.get(a.id);
        if (m === undefined) {
            unmatched.push(`${raw.name} attribute ${a.name}`);
            continue;
        }
        a.effective = valueOut(m);
    }

    const cmds = new Map<string, any>();
    for (const c of model.commands) cmds.set(`${c.id}:${c.effectiveDirection}`, c);
    for (const c of raw.commands) {
        const direction = c.direction ?? (c.name.endsWith("Response") ? "response" : "request");
        const m = cmds.get(`${c.id}:${direction}`);
        if (m === undefined) {
            unmatched.push(`${raw.name} command ${c.name}`);
            continue;
        }
        const e: any = {};
        if (m.effectiveDirection !== undefined) e.direction = m.effectiveDirection;
        if (m.effectiveResponse !== undefined) e.response = m.effectiveResponse;
        const conformance = conformanceOut(m.effectiveConformance);
        if (conformance !== undefined) e.conformance = conformance;
        const access = accessOut(m.effectiveAccess);
        if (access !== undefined) e.access = access;
        e.fields = fieldsOut(m);
        c.effective = e;
    }

    const evts = new Map<number, any>();
    for (const e of model.events) if (typeof e.id === "number") evts.set(e.id, e);
    for (const ev of raw.events) {
        const m = evts.get(ev.id);
        if (m === undefined) {
            unmatched.push(`${raw.name} event ${ev.name}`);
            continue;
        }
        const e: any = {};
        const priority = eventPriority(m);
        if (priority !== undefined) e.priority = priority;
        const conformance = conformanceOut(m.effectiveConformance);
        if (conformance !== undefined) e.conformance = conformance;
        const access = accessOut(m.effectiveAccess);
        if (access !== undefined) e.access = access;
        e.fields = fieldsOut(m);
        ev.effective = e;
    }

    const feats = new Map<string, any>();
    for (const f of model.features) feats.set(f.name, f);
    for (const f of raw.features ?? []) {
        const m = feats.get(f.name);
        if (m === undefined) {
            unmatched.push(`${raw.name} feature ${f.name}`);
            continue;
        }
        const e: any = {};
        if (m.title !== undefined) e.title = m.title;
        const conformance = conformanceOut(m.effectiveConformance);
        if (conformance !== undefined) e.conformance = conformance;
        f.effective = e;
    }

    // A derived cluster's own FeatureMap replaces the base's in the raw
    // layer, so a feature it inherits without restating (TemperatureAlarm's
    // RESET from Alarm Base) is missing from the loop above. The operational
    // model has it — ClusterModel.features visits the FeatureMap's
    // inheritance — so emit it from there, in bit order.
    const rawFeatures = new Set((raw.features ?? []).map((f: any) => f.name));
    let inherited = false;
    for (const m of model.features) {
        if (rawFeatures.has(m.name)) continue;
        const bit = m.constraint === undefined ? undefined : Number(`${m.constraint}`);
        const f: any = {
            name: m.name,
            conformance: m.conformance === undefined ? undefined : `${m.conformance}`,
            description: m.description,
            bit: Number.isInteger(bit) ? bit : undefined,
        };
        const e: any = {};
        if (m.title !== undefined) e.title = m.title;
        const conformance = conformanceOut(m.effectiveConformance);
        if (conformance !== undefined) e.conformance = conformance;
        f.effective = e;
        (raw.features ??= []).push(f);
        inherited = true;
    }
    if (inherited) raw.features.sort((a: any, b: any) => (a.bit ?? 0) - (b.bit ?? 0));

    const datatypes: any[] = [];
    for (const d of model.datatypes) datatypes.push(datatypeOut(d));
    datatypes.sort((a, b) => a.name.localeCompare(b.name));
    raw.datatypes = datatypes;
}

// The global datatypes that carry members (the structs, enums and bitmaps
// clusters reference by name, such as the semantic tag struct); the global
// scalars ("percent", "epoch-s", …) are described where they are used.
const globalDatatypes: any[] = [];
for (const d of (Matter as any).children) {
    if (d.tag !== "datatype" || !d.children.length) continue;
    globalDatatypes.push(datatypeOut(d));
}
globalDatatypes.sort((a, b) => a.name.localeCompare(b.name));
(out as any).globalDatatypes = globalDatatypes;

if (unmatched.length) {
    // A raw element the operational model does not know means the two layers
    // disagree about the cluster's shape; refuse to emit a snapshot that
    // would describe it twice, differently.
    console.error("elements without an operational model:\n  " + unmatched.join("\n  "));
    process.exit(1);
}

console.log(JSON.stringify(out, null, 2));

// The import above is a bare `@matter/model` specifier, so node has to resolve
// it from inside the matter.js checkout — copy this file in rather than running
// it in place, and build packages/model first (`npm run build`). Writing stdout
// straight over parity/schema.json would truncate the embed if the extractor
// throws, so land it in a temporary file and move it into place only on a
// zero exit status:
//
//   cd ../matter.js
//   cp <go-fabric>/script/extract-from-matter-js.ts .occu-extract.mts
//   node .occu-extract.mts > /tmp/schema.json && \
//       mv /tmp/schema.json <go-fabric>/parity/schema.json
//   rm .occu-extract.mts
//
// Then regenerate the typed Go maps that read it:
//
//   cd <go-fabric> && go generate ./schema/...
