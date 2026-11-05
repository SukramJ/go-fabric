# matter.js citation check

Pin: `862ad75f4d3c7ab528b8a3954168146082cad8a6` (parity/schema.json matter.sourceCommit). Regenerate with `make cite-report`; `make cite-check` fails when this file or drift.json moves.

Second tree: a sentence naming matterjs-server is checked against `MATTERJS_SERVER_DIR` (default `../matterjs-server`) at its own `at <sha>`; it has no pin.

| Metric | Count |
|---|---|
| Citations | 7431 |
| Go files citing | 1324 |
| Resolved: exact path from a tree root (packages/, support/, examples/) | 1499 |
| Resolved: unique suffix | 214 |
| Resolved: unique basename | 5718 |
| Checked at their own `at <sha>` | 1293 |
| Checked against matterjs-server (`MATTERJS_SERVER_DIR`) at their `at <sha>` | 69 |
| Drift-checked (the sentence names a name the file contains) | 3641 |
| Hard-checked only | 3790 |
| **Hard errors** | 0 |
| **Drift** | 253 |
