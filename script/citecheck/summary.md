# matter.js citation check

Pin: `b25ed9604f2825857b8de28a3c29cae8124a7e49` (parity/schema.json matter.sourceCommit). Regenerate with `make cite-report`; `make cite-check` fails when this file or drift.json moves.

Second tree: a sentence naming matterjs-server is checked against `MATTERJS_SERVER_DIR` (default `../matterjs-server`) at its own `at <sha>`; it has no pin.

| Metric | Count |
|---|---|
| Citations | 11841 |
| Go files citing | 1885 |
| Resolved: exact path from a tree root (packages/, support/, examples/) | 3413 |
| Resolved: unique suffix | 248 |
| Resolved: unique basename | 8180 |
| Checked at their own `at <sha>`, the pin | 2676 |
| **Checked at another matter.js commit** (their own `at <sha>`, not the pin) | 2641 |
| Checked against matterjs-server (`MATTERJS_SERVER_DIR`) at their `at <sha>` | 678 |
| Drift-checked (the sentence names a name the file contains) | 5333 |
| Hard-checked only | 6508 |
| **Hard errors** | 0 |
| **Drift** | 410 |

## Off the pin, by commit

Line citations whose sentence names a matter.js commit other than the pin are checked at that commit; their count is held by `citecheckOffPinRatchet`, exactly. The bound bare parts are counted beside them, not ratcheted.

| Commit | Line citations | Bare parts bound to a file |
|---|---|---|
| `0ceb202d` | 114 | 7 |
| `862ad75f` | 3 | 121 |
| `9892fa39` | 2 | 0 |
| `d87d1de4` | 2 | 0 |
| `db021da0` | 2519 | 180 |
| `e812bd07` | 1 | 0 |

## Counted, not ratcheted

| Reference | Count |
|---|---|
| A .ts file named without a line part | 2760 |
| … whose sentence names another matter.js commit | 594 |
| A bare line part bound to the .ts file named before it (checked; not a citation, drift or hard error of the run) | 589 |
| … checked at another matter.js commit | 308 |
| … with a hard error at the commit it was checked at | 3 |
| A bare line part after a .ts citation or after no file (not checked) | 3052 |
| A chip citation (a chip source in a sentence naming chip or connectedhomeip) | 1819 |
| … naming a commit that is no matter.js commit | 875 |
| … at a commit other than the harness commit `6170af84` (Makefile CHIP_TEST_IMAGE_COMMIT) | 6 |

| chip commit | chip citations |
|---|---|
| `443f6934` | 2 |
| `645b267a` | 3 |
| `6feac778f196483b6355d35fc529f183b293b71f` | 1 |
