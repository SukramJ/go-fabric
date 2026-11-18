# matter.js citation check

Pin: `db021da06bafe6766cd1a05d505cea03b9ec5081` (parity/schema.json matter.sourceCommit). Regenerate with `make cite-report`; `make cite-check` fails when this file or drift.json moves.

Second tree: a sentence naming matterjs-server is checked against `MATTERJS_SERVER_DIR` (default `../matterjs-server`) at its own `at <sha>`; it has no pin.

| Metric | Count |
|---|---|
| Citations | 10472 |
| Go files citing | 1743 |
| Resolved: exact path from a tree root (packages/, support/, examples/) | 2901 |
| Resolved: unique suffix | 251 |
| Resolved: unique basename | 7320 |
| Checked at their own `at <sha>`, the pin | 3792 |
| **Checked at another matter.js commit** (their own `at <sha>`, not the pin) | 122 |
| Checked against matterjs-server (`MATTERJS_SERVER_DIR`) at their `at <sha>` | 678 |
| Drift-checked (the sentence names a name the file contains) | 4608 |
| Hard-checked only | 5864 |
| **Hard errors** | 0 |
| **Drift** | 305 |

## Off the pin, by commit

Line citations whose sentence names a matter.js commit other than the pin are checked at that commit; their count is held by `citecheckOffPinRatchet`, exactly. The bound bare parts are counted beside them, not ratcheted.

| Commit | Line citations | Bare parts bound to a file |
|---|---|---|
| `0ceb202d` | 114 | 8 |
| `862ad75f` | 3 | 127 |
| `9892fa39` | 2 | 0 |
| `d87d1de4` | 2 | 0 |
| `e812bd07` | 1 | 0 |

## Counted, not ratcheted

| Reference | Count |
|---|---|
| A .ts file named without a line part | 2596 |
| … whose sentence names another matter.js commit | 466 |
| A bare line part bound to the .ts file named before it (checked; not a citation, drift or hard error of the run) | 531 |
| … checked at another matter.js commit | 135 |
| … with a hard error at the commit it was checked at | 3 |
| A bare line part after a .ts citation or after no file (not checked) | 2629 |
| A chip citation (a chip source in a sentence naming chip or connectedhomeip) | 1806 |
| … naming a commit that is no matter.js commit | 877 |
| … at a commit other than the harness commit `6170af84` (Makefile CHIP_TEST_IMAGE_COMMIT) | 26 |

| chip commit | chip citations |
|---|---|
| `645b267a` | 25 |
| `6feac778f196483b6355d35fc529f183b293b71f` | 1 |
