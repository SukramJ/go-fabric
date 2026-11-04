# matter.js citation check

Pin: `862ad75f4d3c7ab528b8a3954168146082cad8a6` (parity/schema.json matter.sourceCommit). Regenerate with `make cite-report`; `make cite-check` fails when this file or drift.json moves.

Second tree: a sentence naming matterjs-server is checked against `MATTERJS_SERVER_DIR` (default `../matterjs-server`) at its own `at <sha>`; it has no pin.

| Metric | Count |
|---|---|
| Citations | 7237 |
| Go files citing | 1276 |
| Resolved: exact path from a tree root (packages/, support/, examples/) | 1416 |
| Resolved: unique suffix | 214 |
| Resolved: unique basename | 5606 |
| Checked at their own `at <sha>` | 1155 |
| Checked against matterjs-server (`MATTERJS_SERVER_DIR`) at their `at <sha>` | 70 |
| Drift-checked (the sentence names a name the file contains) | 3544 |
| Hard-checked only | 3692 |
| **Hard errors** | 1 |
| **Drift** | 254 |

## Hard errors

| Kind | Count |
|---|---|
| file not found | 1 |

- `controller/client/event_history.go:29: packages/node/src/node/client/ClientEventEmitter.ts:45-74: file not found`
