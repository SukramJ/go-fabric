# matter.js citation check

Pin: `db021da06bafe6766cd1a05d505cea03b9ec5081` (parity/schema.json matter.sourceCommit). Regenerate with `make cite-report`; `make cite-check` fails when this file or drift.json moves.

Second tree: a sentence naming matterjs-server is checked against `MATTERJS_SERVER_DIR` (default `../matterjs-server`) at its own `at <sha>`; it has no pin.

| Metric | Count |
|---|---|
| Citations | 8434 |
| Go files citing | 1435 |
| Resolved: exact path from a tree root (packages/, support/, examples/) | 2000 |
| Resolved: unique suffix | 234 |
| Resolved: unique basename | 6200 |
| Checked at their own `at <sha>` | 1680 |
| Checked against matterjs-server (`MATTERJS_SERVER_DIR`) at their `at <sha>` | 672 |
| Drift-checked (the sentence names a name the file contains) | 3894 |
| Hard-checked only | 4540 |
| **Hard errors** | 0 |
| **Drift** | 416 |
