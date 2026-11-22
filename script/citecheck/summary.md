# matter.js citation check

Pin: `b25ed9604f2825857b8de28a3c29cae8124a7e49` (parity/schema.json matter.sourceCommit). Regenerate with `make cite-report`; `make cite-check` fails when this file or drift.json moves.

Second tree: a sentence naming matterjs-server is checked against `MATTERJS_SERVER_DIR` (default `../matterjs-server`) at its own `at <sha>`; it has no pin.

| Metric | Count |
|---|---|
| Citations | 11763 |
| Go files citing | 1872 |
| Resolved: exact path from a tree root (packages/, support/, examples/) | 3378 |
| Resolved: unique suffix | 248 |
| Resolved: unique basename | 8137 |
| Checked at their own `at <sha>`, the pin | 2542 |
| **Checked at another matter.js commit** (their own `at <sha>`, not the pin) | 2658 |
| Checked against matterjs-server (`MATTERJS_SERVER_DIR`) at their `at <sha>` | 678 |
| Drift-checked (the sentence names a name the file contains) | 5306 |
| Hard-checked only | 6457 |
| **Hard errors** | 0 |
| **Drift** | 412 |

## Off the pin, by commit

Line citations whose sentence names a matter.js commit other than the pin are checked at that commit; their count is held by `citecheckOffPinRatchet`, exactly. The bound bare parts are counted beside them, not ratcheted.

| Commit | Line citations | Bare parts bound to a file |
|---|---|---|
| `0ceb202d` | 114 | 7 |
| `862ad75f` | 3 | 121 |
| `9892fa39` | 2 | 0 |
| `d87d1de4` | 2 | 0 |
| `db021da0` | 2536 | 181 |
| `e812bd07` | 1 | 0 |

## Counted, not ratcheted

| Reference | Count |
|---|---|
| A .ts file named without a line part | 2751 |
| … whose sentence names another matter.js commit | 595 |
| A bare line part bound to the .ts file named before it (checked; not a citation, drift or hard error of the run) | 577 |
| … checked at another matter.js commit | 309 |
| … with a hard error at the commit it was checked at | 2 |
| A bare line part after a .ts citation or after no file (not checked) | 2994 |
| A chip citation (a chip source in a sentence naming chip or connectedhomeip) | 1757 |
| … naming a commit that is no matter.js commit | 840 |
| … at a commit other than the harness commit `6170af84` (Makefile CHIP_TEST_IMAGE_COMMIT) | 4 |

| chip commit | chip citations |
|---|---|
| `645b267a` | 3 |
| `6feac778f196483b6355d35fc529f183b293b71f` | 1 |
