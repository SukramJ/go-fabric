# matter.js citation check

Pin: `db021da06bafe6766cd1a05d505cea03b9ec5081` (parity/schema.json matter.sourceCommit). Regenerate with `make cite-report`; `make cite-check` fails when this file or drift.json moves.

Second tree: a sentence naming matterjs-server is checked against `MATTERJS_SERVER_DIR` (default `../matterjs-server`) at its own `at <sha>`; it has no pin.

| Metric | Count |
|---|---|
| Citations | 9973 |
| Go files citing | 1658 |
| Resolved: exact path from a tree root (packages/, support/, examples/) | 2712 |
| Resolved: unique suffix | 246 |
| Resolved: unique basename | 7015 |
| Checked at their own `at <sha>` | 3129 |
| Checked against matterjs-server (`MATTERJS_SERVER_DIR`) at their `at <sha>` | 672 |
| Drift-checked (the sentence names a name the file contains) | 4536 |
| Hard-checked only | 5437 |
| **Hard errors** | 0 |
| **Drift** | 384 |
