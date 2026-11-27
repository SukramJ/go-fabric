# matter.js citation check

Pin: `db91886cbe742f28d2d6ae513dfc33988436bf02` (parity/schema.json matter.sourceCommit). Regenerate with `make cite-report`; `make cite-check` fails when this file or drift.json moves.

Second tree: a sentence naming matterjs-server is checked against `MATTERJS_SERVER_DIR` (default `../matterjs-server`) at its own `at <sha>`; it has no pin.

| Metric | Count |
|---|---|
| Citations | 11857 |
| Go files citing | 1886 |
| Resolved: exact path from a tree root (packages/, support/, examples/) | 3419 |
| Resolved: unique suffix | 249 |
| Resolved: unique basename | 8189 |
| Checked at their own `at <sha>`, the pin | 1977 |
| **Checked at another matter.js commit** (their own `at <sha>`, not the pin) | 3358 |
| Checked against matterjs-server (`MATTERJS_SERVER_DIR`) at their `at <sha>` | 678 |
| Drift-checked (the sentence names a name the file contains) | 5341 |
| Hard-checked only | 6516 |
| **Hard errors** | 0 |
| **Drift** | 416 |

## Off the pin, by commit

Line citations whose sentence names a matter.js commit other than the pin are checked at that commit; their count is held by `citecheckOffPinRatchet`, exactly. The bound bare parts are counted beside them, not ratcheted.

| Commit | Line citations | Bare parts bound to a file |
|---|---|---|
| `0ceb202d` | 114 | 7 |
| `862ad75f` | 3 | 121 |
| `9892fa39` | 2 | 0 |
| `b25ed960` | 725 | 16 |
| `d87d1de4` | 2 | 0 |
| `db021da0` | 2511 | 180 |
| `e812bd07` | 1 | 0 |

## Counted, not ratcheted

| Reference | Count |
|---|---|
| A .ts file named without a line part | 2766 |
| … whose sentence names another matter.js commit | 623 |
| A bare line part bound to the .ts file named before it (checked; not a citation, drift or hard error of the run) | 590 |
| … checked at another matter.js commit | 324 |
| … with a hard error at the commit it was checked at | 3 |
| A bare line part after a .ts citation or after no file (not checked) | 3068 |
| A chip citation (a chip source in a sentence naming chip or connectedhomeip) | 1830 |
| … naming a commit that is no matter.js commit | 885 |
| … at a commit other than the harness commit `6170af84` (Makefile CHIP_TEST_IMAGE_COMMIT) | 6 |

| chip commit | chip citations |
|---|---|
| `443f6934` | 2 |
| `645b267a` | 3 |
| `6feac778f196483b6355d35fc529f183b293b71f` | 1 |
