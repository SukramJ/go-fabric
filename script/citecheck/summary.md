# matter.js citation check

Pin: `db91886cbe742f28d2d6ae513dfc33988436bf02` (parity/schema.json matter.sourceCommit). Regenerate with `make cite-report`; `make cite-check` fails when this file or drift.json moves.

Second tree: a sentence naming matterjs-server is checked against `MATTERJS_SERVER_DIR` (default `../matterjs-server`) at its own `at <sha>`; it has no pin.

| Metric | Count |
|---|---|
<<<<<<< HEAD
| Citations | 11846 |
| Go files citing | 1884 |
| Resolved: exact path from a tree root (packages/, support/, examples/) | 3416 |
| Resolved: unique suffix | 249 |
| Resolved: unique basename | 8181 |
| Checked at their own `at <sha>`, the pin | 1962 |
| **Checked at another matter.js commit** (their own `at <sha>`, not the pin) | 3364 |
| Checked against matterjs-server (`MATTERJS_SERVER_DIR`) at their `at <sha>` | 678 |
| Drift-checked (the sentence names a name the file contains) | 5334 |
| Hard-checked only | 6512 |
=======
| Citations | 11860 |
| Go files citing | 1887 |
| Resolved: exact path from a tree root (packages/, support/, examples/) | 3422 |
| Resolved: unique suffix | 249 |
| Resolved: unique basename | 8189 |
| Checked at their own `at <sha>`, the pin | 1981 |
| **Checked at another matter.js commit** (their own `at <sha>`, not the pin) | 3355 |
| Checked against matterjs-server (`MATTERJS_SERVER_DIR`) at their `at <sha>` | 678 |
| Drift-checked (the sentence names a name the file contains) | 5342 |
| Hard-checked only | 6518 |
>>>>>>> phase9/p1-d
| **Hard errors** | 0 |
| **Drift** | 416 |

## Off the pin, by commit

Line citations whose sentence names a matter.js commit other than the pin are checked at that commit; their count is held by `citecheckOffPinRatchet`, exactly. The bound bare parts are counted beside them, not ratcheted.

| Commit | Line citations | Bare parts bound to a file |
|---|---|---|
| `0ceb202d` | 114 | 7 |
| `862ad75f` | 3 | 120 |
| `9892fa39` | 2 | 0 |
<<<<<<< HEAD
| `b25ed960` | 726 | 12 |
| `d87d1de4` | 2 | 0 |
| `db021da0` | 2512 | 180 |
=======
| `b25ed960` | 725 | 16 |
| `d87d1de4` | 2 | 0 |
| `db021da0` | 2508 | 180 |
>>>>>>> phase9/p1-d
| `e812bd07` | 1 | 0 |

## Counted, not ratcheted

| Reference | Count |
|---|---|
<<<<<<< HEAD
| A .ts file named without a line part | 2764 |
| … whose sentence names another matter.js commit | 620 |
| A bare line part bound to the .ts file named before it (checked; not a citation, drift or hard error of the run) | 590 |
| … checked at another matter.js commit | 319 |
| … with a hard error at the commit it was checked at | 3 |
| A bare line part after a .ts citation or after no file (not checked) | 3056 |
| A chip citation (a chip source in a sentence naming chip or connectedhomeip) | 1825 |
| … naming a commit that is no matter.js commit | 880 |
=======
| A .ts file named without a line part | 2774 |
| … whose sentence names another matter.js commit | 622 |
| A bare line part bound to the .ts file named before it (checked; not a citation, drift or hard error of the run) | 593 |
| … checked at another matter.js commit | 324 |
| … with a hard error at the commit it was checked at | 3 |
| A bare line part after a .ts citation or after no file (not checked) | 3074 |
| A chip citation (a chip source in a sentence naming chip or connectedhomeip) | 1822 |
| … naming a commit that is no matter.js commit | 878 |
>>>>>>> phase9/p1-d
| … at a commit other than the harness commit `6170af84` (Makefile CHIP_TEST_IMAGE_COMMIT) | 6 |

| chip commit | chip citations |
|---|---|
| `443f6934` | 2 |
| `645b267a` | 3 |
| `6feac778f196483b6355d35fc529f183b293b71f` | 1 |
