// Generates wire fixtures for the application cluster servers
// (cluster/alarm, cluster/fan, cluster/pump) and the Switch press events
// from the matter.js checkout's own encoders: the FanControl Step request
// payloads the bridge decodes, and the SmokeCoAlarm /
// PumpConfigurationAndControl / Switch event payloads its value writer
// encodes — each as matter.js's
// TlvOfModel(element) encodes it, the schema the interaction server
// decodes requests and encodes events with
// (packages/node/src/node/integration/ProtocolService.ts).
//
// Master: bridge/testdata/application-wire-fixtures.json
//
// Run from anywhere, against a built matter.js checkout next to go-fabric:
//
//   node "$GO_FABRIC"/notes/parity/matter/generate-application-fixtures.ts > /tmp/a.json \
//       && mv /tmp/a.json "$GO_FABRIC"/bridge/testdata/application-wire-fixtures.json

const path = require("path");

const matterJsRoot = path.resolve(__dirname, "../../../../matter.js");
const mod = (name) => require(path.join(matterJsRoot, "node_modules/@matter", name, "dist/cjs/index.js"));

const general = mod("general");
const model = mod("model");
const types = mod("types");

// matter.js logs to stdout; keep the JSON clean.
general.Logger.level = "error";

const hex = (b) => Buffer.from(b).toString("hex");

function elementTlv(cluster, kind, name) {
    const element = cluster[kind].find((e) => e.name === name);
    if (element === undefined) {
        throw new Error(`no ${kind} ${name} in ${cluster.name}`);
    }
    return types.TlvOfModel(element);
}

function fixtures() {
    const out = [];
    const record = (label, cluster, kind, name, value) => {
        out.push({
            label, kind, cluster: cluster.id, element: name,
            fixture: value, bytesHex: hex(elementTlv(cluster, kind, name).encode(value)),
        });
    };

    // FanControl Step: Direction mandatory, Wrap / LowestOff optional.
    const fan = model.FanControl;
    record("step_increase_minimal", fan, "commands", "Step", { direction: 0 });
    record("step_decrease_wrap", fan, "commands", "Step", { direction: 1, wrap: true });
    record("step_full", fan, "commands", "Step", { direction: 0, wrap: false, lowestOff: false });
    record("step_lowest_off_only", fan, "commands", "Step", { direction: 1, lowestOff: true });

    // SmokeCoAlarm events: the five with an AlarmSeverityLevel field and
    // the six without fields.
    const smoke = model.SmokeCoAlarm;
    for (const [name, level] of [["SmokeAlarm", 2], ["CoAlarm", 1], ["LowBattery", 1], ["InterconnectSmokeAlarm", 2], ["InterconnectCoAlarm", 1]]) {
        record(`smoke_${name}`, smoke, "events", name, { alarmSeverityLevel: level });
    }
    for (const name of ["HardwareFault", "EndOfService", "SelfTestComplete", "AlarmMuted", "MuteEnded", "AllClear"]) {
        record(`smoke_${name}`, smoke, "events", name, undefined);
    }

    // PumpConfigurationAndControl: every event is fieldless.
    const pump = model.PumpConfigurationAndControl;
    for (const name of ["SupplyVoltageLow", "DryRunning", "PumpBlocked", "TurbineOperation"]) {
        record(`pump_${name}`, pump, "events", name, undefined);
    }

    // Switch: the four press events the GenericSwitch server emits.
    const sw = model.Switch;
    record("switch_InitialPress", sw, "events", "InitialPress", { newPosition: 1 });
    record("switch_LongPress", sw, "events", "LongPress", { newPosition: 1 });
    record("switch_ShortRelease", sw, "events", "ShortRelease", { previousPosition: 1 });
    record("switch_LongRelease", sw, "events", "LongRelease", { previousPosition: 0 });
    return out;
}

process.stdout.write(JSON.stringify(fixtures(), null, 2) + "\n");
