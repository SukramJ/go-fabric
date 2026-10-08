// Generates wire fixtures for the ColorControl server (cluster/light) from
// the matter.js checkout's own encoders: one request payload per
// ColorControl command, as matter.js's TlvOfModel(element) encodes it —
// the schema the interaction server decodes a request with
// (packages/node/src/node/integration/ProtocolService.ts) — and the
// colour attributes the server reads, as its value writer must encode them.
//
// Master: bridge/testdata/colorcontrol-wire-fixtures.json
//
// Run from anywhere, against a built matter.js checkout next to go-fabric
// (MATTERJS_DIR names another one):
//
//   node "$GO_FABRIC"/notes/parity/matter/generate-colorcontrol-fixtures.ts > /tmp/cc.json \
//       && mv /tmp/cc.json "$GO_FABRIC"/bridge/testdata/colorcontrol-wire-fixtures.json

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
    const cc = model.ColorControl;
    const record = (label, kind, name, value) => {
        const element = cc[kind].find((e) => e.name === name && (kind !== "commands" || e.direction !== "response"));
        if (element === undefined) {
            throw new Error(`no ${kind} ${name} in ColorControl`);
        }
        out.push({
            label, kind, cluster: cc.id, element: name, id: element.id,
            fixture: value, bytesHex: hex(types.TlvOfModel(element).encode(value)),
        });
    };
    const opts = { optionsMask: { executeIfOff: true }, optionsOverride: { executeIfOff: false } };

    record("move_to_hue", "commands", "MoveToHue", { hue: 200, direction: 2, transitionTime: 150, ...opts });
    record("move_hue", "commands", "MoveHue", { moveMode: 3, rate: 25, ...opts });
    record("step_hue", "commands", "StepHue", { stepMode: 1, stepSize: 60, transitionTime: 20, ...opts });
    record("move_to_saturation", "commands", "MoveToSaturation", { saturation: 254, transitionTime: 100, ...opts });
    record("move_saturation", "commands", "MoveSaturation", { moveMode: 1, rate: 10, ...opts });
    record("step_saturation", "commands", "StepSaturation", { stepMode: 3, stepSize: 15, transitionTime: 10, ...opts });
    record("move_to_hue_and_saturation", "commands", "MoveToHueAndSaturation", { hue: 40, saturation: 120, transitionTime: 0, ...opts });
    record("move_to_color", "commands", "MoveToColor", { colorX: 32768, colorY: 19660, transitionTime: 100, ...opts });
    record("move_color", "commands", "MoveColor", { rateX: -1000, rateY: 2500, ...opts });
    record("step_color", "commands", "StepColor", { stepX: 6000, stepY: -3000, transitionTime: 50, ...opts });
    record("move_to_color_temperature", "commands", "MoveToColorTemperature", { colorTemperatureMireds: 370, transitionTime: 100, ...opts });
    record("enhanced_move_to_hue", "commands", "EnhancedMoveToHue", { enhancedHue: 12000, direction: 0, transitionTime: 30, ...opts });
    record("enhanced_move_hue", "commands", "EnhancedMoveHue", { moveMode: 1, rate: 2048, ...opts });
    record("enhanced_step_hue", "commands", "EnhancedStepHue", { stepMode: 3, stepSize: 4096, transitionTime: 5, ...opts });
    record("enhanced_move_to_hue_and_saturation", "commands", "EnhancedMoveToHueAndSaturation", { enhancedHue: 50000, saturation: 70, transitionTime: 0, ...opts });
    record("color_loop_set", "commands", "ColorLoopSet", {
        updateFlags: { updateAction: true, updateDirection: true, updateTime: true, updateStartHue: true },
        action: 1, direction: 1, time: 30, startHue: 8960, ...opts,
    });
    record("stop_move_step", "commands", "StopMoveStep", { ...opts });
    record("move_color_temperature", "commands", "MoveColorTemperature", {
        moveMode: 1, rate: 20, colorTemperatureMinimumMireds: 160, colorTemperatureMaximumMireds: 360, ...opts,
    });
    record("step_color_temperature", "commands", "StepColorTemperature", {
        stepMode: 3, stepSize: 50, transitionTime: 10, colorTemperatureMinimumMireds: 0, colorTemperatureMaximumMireds: 0, ...opts,
    });

    record("current_x", "attributes", "CurrentX", 24939);
    record("current_hue", "attributes", "CurrentHue", 254);
    record("enhanced_current_hue", "attributes", "EnhancedCurrentHue", 65535);
    record("enhanced_color_mode", "attributes", "EnhancedColorMode", 3);
    record("color_loop_active", "attributes", "ColorLoopActive", 1);
    record("color_capabilities", "attributes", "ColorCapabilities", {
        hueSaturation: true, enhancedHue: true, colorLoop: true, xy: true, colorTemperature: true,
    });
    return out;
}

process.stdout.write(JSON.stringify(fixtures(), null, 2) + "\n");
