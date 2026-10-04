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

    // OperationalState / RvcOperationalState: the fieldless requests, the
    // response, both events and the structured attributes.
    const ops = model.OperationalState;
    const rvc = model.RvcOperationalState;
    record("opstate_pause_request", ops, "commands", "Pause", undefined);
    record("rvc_go_home_request", rvc, "commands", "GoHome", undefined);
    const responses = {
        no_error: { errorStateId: 0 },
        invalid_in_state: { errorStateId: 3 },
        manufacturer: { errorStateId: 0x80, errorStateLabel: "Door open", errorStateDetails: "close the door" },
        details_only: { errorStateId: 1, errorStateDetails: "water supply" },
    };
    for (const [label, state] of Object.entries(responses)) {
        record(`opstate_response_${label}`, ops, "commands", "OperationalCommandResponse", { commandResponseState: state });
    }
    record("rvc_response_stuck", rvc, "commands", "OperationalCommandResponse",
        { commandResponseState: { errorStateId: 0x41, errorStateDetails: "left wheel blocked" } });
    record("opstate_event_operational_error", ops, "events", "OperationalError",
        { errorState: { errorStateId: 2, errorStateDetails: "drain blocked" } });
    record("rvc_event_operational_error", rvc, "events", "OperationalError",
        { errorState: { errorStateId: 0x4c } });
    for (const [label, ev] of Object.entries({
        minimal: { completionErrorCode: 0 },
        full: { completionErrorCode: 2, totalOperationalTime: 3600, pausedTime: 120 },
        nulls: { completionErrorCode: 0, totalOperationalTime: null, pausedTime: null },
        total_only: { completionErrorCode: 0, totalOperationalTime: 259200 },
    })) {
        record(`opstate_event_completion_${label}`, ops, "events", "OperationCompletion", ev);
    }
    record("opstate_state_list", ops, "attributes", "OperationalStateList", [
        { operationalStateId: 0 }, { operationalStateId: 1 }, { operationalStateId: 2 }, { operationalStateId: 3 },
        { operationalStateId: 0x80, operationalStateLabel: "Pre-soak" },
    ]);
    record("rvc_state_list", rvc, "attributes", "OperationalStateList", [
        { operationalStateId: 3 }, { operationalStateId: 0x40 }, { operationalStateId: 0x41 }, { operationalStateId: 0x42 },
    ]);
    record("opstate_operational_error", ops, "attributes", "OperationalError",
        { errorStateId: 0x81, errorStateLabel: "Lid", errorStateDetails: "open" });
    record("opstate_phase_list", ops, "attributes", "PhaseList", ["pre-soak", "rinse", "spin"]);
    return out;
}

process.stdout.write(JSON.stringify(fixtures(), null, 2) + "\n");
