# matter.js citation check

Pin: `b25ed9604f2825857b8de28a3c29cae8124a7e49` (parity/schema.json matter.sourceCommit). Regenerate with `make cite-report`; `make cite-check` fails when this file or drift.json moves.

Second tree: a sentence naming matterjs-server is checked against `MATTERJS_SERVER_DIR` (default `../matterjs-server`) at its own `at <sha>`; it has no pin.

| Metric | Count |
|---|---|
| Citations | 10549 |
| Go files citing | 1749 |
| Resolved: exact path from a tree root (packages/, support/, examples/) | 2965 |
| Resolved: unique suffix | 251 |
| Resolved: unique basename | 7331 |
| Checked at their own `at <sha>`, the pin | 0 |
| **Checked at another matter.js commit** (their own `at <sha>`, not the pin) | 3952 |
| Checked against matterjs-server (`MATTERJS_SERVER_DIR`) at their `at <sha>` | 678 |
| Drift-checked (the sentence names a name the file contains) | 4624 |
| Hard-checked only | 5908 |
| **Hard errors** | 17 |
| **Drift** | 448 |

## Off the pin, by commit

Line citations whose sentence names a matter.js commit other than the pin are checked at that commit; their count is held by `citecheckOffPinRatchet`, exactly. The bound bare parts are counted beside them, not ratcheted.

| Commit | Line citations | Bare parts bound to a file |
|---|---|---|
| `0ceb202d` | 114 | 7 |
| `862ad75f` | 3 | 127 |
| `9892fa39` | 2 | 0 |
| `c41eb7da` | 2 | 0 |
| `d87d1de4` | 2 | 0 |
| `db021da0` | 3828 | 242 |
| `e812bd07` | 1 | 0 |

## Counted, not ratcheted

| Reference | Count |
|---|---|
| A .ts file named without a line part | 2601 |
| … whose sentence names another matter.js commit | 746 |
| A bare line part bound to the .ts file named before it (checked; not a citation, drift or hard error of the run) | 531 |
| … checked at another matter.js commit | 376 |
| … with a hard error at the commit it was checked at | 2 |
| A bare line part after a .ts citation or after no file (not checked) | 2650 |
| A chip citation (a chip source in a sentence naming chip or connectedhomeip) | 1810 |
| … naming a commit that is no matter.js commit | 881 |
| … at a commit other than the harness commit `6170af84` (Makefile CHIP_TEST_IMAGE_COMMIT) | 26 |

| chip commit | chip citations |
|---|---|
| `645b267a` | 25 |
| `6feac778f196483b6355d35fc529f183b293b71f` | 1 |

## Hard errors

| Kind | Count |
|---|---|
| file not found | 2 |
| line beyond end of file | 15 |

- `client/exchange/checkin_test.go:16: SecureChannelProtocol.ts:149-180: line beyond end of file (packages/protocol/src/securechannel/SecureChannelProtocol.ts has 167 lines)`
- `client/exchange/manager.go:300: packages/protocol/src/securechannel/SecureChannelProtocol.ts:149-180: line beyond end of file (packages/protocol/src/securechannel/SecureChannelProtocol.ts has 167 lines)`
- `client/exchange/manager.go:308: SecureChannelProtocol.ts:164-180: line beyond end of file (packages/protocol/src/securechannel/SecureChannelProtocol.ts has 167 lines)`
- `client/interaction/provider.go:185: packages/protocol/src/peer/PeerExchangeProvider.ts:19-159: line beyond end of file (packages/protocol/src/peer/PeerExchangeProvider.ts has 157 lines)`
- `client/interaction/provider.go:270: PeerExchangeProvider.ts:150-158: line beyond end of file (packages/protocol/src/peer/PeerExchangeProvider.ts has 157 lines)`
- `client/interaction/provider_test.go:116: PeerExchangeProvider.ts:19-159: line beyond end of file (packages/protocol/src/peer/PeerExchangeProvider.ts has 157 lines)`
- `client/interaction/provider_test.go:161: PeerExchangeProvider.ts:150-158: line beyond end of file (packages/protocol/src/peer/PeerExchangeProvider.ts has 157 lines)`
- `controller/client/registry.go:190: SecureChannelProtocol.ts:149-180: line beyond end of file (packages/protocol/src/securechannel/SecureChannelProtocol.ts has 167 lines)`
- `controller/client/registry.go:362: SecureChannelProtocol.ts:164-180: line beyond end of file (packages/protocol/src/securechannel/SecureChannelProtocol.ts has 167 lines)`
- `controller/softwareupdate/manager.go:206: OtaSoftwareUpdateProviderServer.ts:652-660: line beyond end of file (packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts has 469 lines)`
- `controller/softwareupdate/manager.go:210: OtaSoftwareUpdateProviderServer.ts:507-515: line beyond end of file (packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts has 469 lines)`
- `controller/softwareupdate/manager.go:48: OtaSoftwareUpdateProviderServer.ts:467-473: line beyond end of file (packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts has 469 lines)`
- `controller/softwareupdate/provider.go:77: OtaSoftwareUpdateProviderServer.ts:519-528: line beyond end of file (packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts has 469 lines)`
- `controller/softwareupdate/script.go:211: OtaSoftwareUpdateProviderServer.ts:470-473: line beyond end of file (packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts has 469 lines)`
- `internal/chiptool/gocert_idm_test.go:390: test/cert/unit-testing-cluster.ts:16-20: file not found`
- `internal/chiptool/gocert_idm_test.go:619: test/cert/unit-testing-cluster.ts:16-20: file not found`
- `node/parity_matterjs_helpers_test.go:283: CommandInvokeResponseTest.ts:830-865: line beyond end of file (packages/node/test/node/CommandInvokeResponseTest.ts has 863 lines)`
