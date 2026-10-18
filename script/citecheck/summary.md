# matter.js citation check

Pin: `862ad75f4d3c7ab528b8a3954168146082cad8a6` (parity/schema.json matter.sourceCommit). Regenerate with `make cite-report`; `make cite-check` fails when this file or drift.json moves.

| Metric | Count |
|---|---|
| Citations | 3716 |
| Go files citing | 643 |
| Resolved: exact path from a tree root (packages/, support/, examples/) | 781 |
| Resolved: unique suffix | 132 |
| Resolved: unique basename | 2790 |
| Checked at their own `at <sha>` | 249 |
| Drift-checked (the sentence names a name the file contains) | 1762 |
| Hard-checked only | 1936 |
| **Hard errors** | 18 |
| **Drift** | 266 |

## Hard errors

| Kind | Count |
|---|---|
| ambiguous | 9 |
| file not found | 4 |
| line beyond end of file | 5 |

- `binding/reason.go:37: BindingManager.ts:721-737: line beyond end of file (packages/node/src/behaviors/binding/BindingManager.ts has 720 lines)`
- `bridge/commissioning_window.go:545: AdministratorCommissioningServer.ts:283-290: line beyond end of file (packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts has 267 lines)`
- `cluster/energy/meter.go:42: packages/node/src/behaviors/meter-identification/MeterIdentificationServer.ts:15: line beyond end of file (packages/node/src/behaviors/meter-identification/MeterIdentificationServer.ts has 14 lines)`
- `cluster/matter_negative_write_parity_test.go:341: packages/node/src/standard/elements/window-covering-cluster.element.ts:72: file not found`
- `cluster/matter_negative_write_parity_test.go:358: packages/node/src/standard/elements/window-covering-cluster.element.ts:79: file not found`
- `cluster/matter_negative_write_parity_test.go:673: packages/node/src/standard/elements/window-covering-cluster.element.ts:72: file not found`
- `controller/ble/scanner.go:99: BleScanner.ts:124-674: ambiguous (packages/nodejs-ble/src/BleScanner.ts, packages/protocol/src/common/BleScanner.ts, packages/react-native/src/ble/BleScanner.ts)`
- `controller/ble/scanner_parity_matterjs_test.go:614: BleScanner.ts:476-496: ambiguous (packages/nodejs-ble/src/BleScanner.ts, packages/protocol/src/common/BleScanner.ts, packages/react-native/src/ble/BleScanner.ts)`
- `controller/client/endpoint.go:22: endpoints/root.ts:314-319: ambiguous (packages/main/src/forwards/endpoints/root.ts, packages/node/src/endpoints/root.ts)`
- `controller/client/network.go:266: ClientEventEmitter.ts:156-175: line beyond end of file (packages/node/src/node/client/ClientEventEmitter.ts has 160 lines)`
- `controller/client/node.go:148: node/client/ClientNodePhysicalProperties.ts:19-134: line beyond end of file (packages/node/src/node/client/ClientNodePhysicalProperties.ts has 133 lines)`
- `controller/exchange/errors.go:32: errors.ts:17: ambiguous (packages/cli-tool/src/errors.ts, packages/general/src/transaction/errors.ts, packages/model/src/common/errors.ts, packages/model/src/decoration/errors.ts, packages/node/src/endpoint/errors.ts, packages/protocol/src/action/errors.ts, packages/protocol/src/groups/errors.ts, packages/protocol/src/protocol/errors.ts, packages/protocol/src/thread/errors.ts, packages/testing/src/docker/errors.ts, packages/thread-br-client/src/diagnostic/errors.ts, support/codegen/src/chipdm/errors.ts)`
- `device/controller_types_test.go:17: on-off-light-switch.ts:75-95: ambiguous (packages/main/src/forwards/devices/on-off-light-switch.ts, packages/node/src/devices/on-off-light-switch.ts)`
- `internal/chipdm/acknowledged.go:68: ValueValidator.ts:172-178: ambiguous (packages/model/src/logic/definition-validation/ValueValidator.ts, packages/node/src/behavior/state/validation/ValueValidator.ts)`
- `node/node_test.go:286: Endpoint.ts:985: ambiguous (packages/matter.js/src/device/Endpoint.ts, packages/node/src/endpoint/Endpoint.ts)`
- `secure/mattercert/validate_rcac_test.go:424: packages/.../Rcac.ts:140: file not found`
- `secure/setup/decode.go:132: Schema.ts:18-25: ambiguous (packages/model/src/models/Schema.ts, packages/types/src/schema/Schema.ts)`
- `secure/setup/decode.go:378: Schema.ts:18-25: ambiguous (packages/model/src/models/Schema.ts, packages/types/src/schema/Schema.ts)`
