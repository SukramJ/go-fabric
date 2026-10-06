# Exported-API reachability summary

Root set: test-seeded: Test*/Benchmark*/Fuzz*/Example* functions of every test package (this module is a library and has no production main to seed from)
Entry points: 3717 across 67 test packages.

## Overview

| Metric | Count |
|---|---|
| Total exported | 4266 |
| Reached by a test | 2490 |
| Whitelisted | 1771 |
| **Unreached** | **5** |

## Top-20 packages by unreached exported identifiers

| Package | Funcs | Types | Other |
|---|---|---|---|
| cluster/measurement | 1 | 0 | 0 |
| endpoint | 1 | 0 | 0 |
| secure/channel | 0 | 0 | 1 |
| secure/mattercert | 0 | 0 | 1 |
| secure/sigma | 0 | 0 | 1 |

## First 50 unreached functions

| Package | Identifier | File | Line |
|---|---|---|---|
| cluster/measurement | CelsiusToInt16 | cluster/measurement/measurement.go | 1742 |
| endpoint | ComposeNodeLabel | endpoint/spec.go | 104 |

## Full by-package breakdown

| Package | Funcs | Types | Other |
|---|---|---|---|
| cluster/measurement | 1 | 0 | 0 |
| endpoint | 1 | 0 | 0 |
| secure/channel | 0 | 0 | 1 |
| secure/mattercert | 0 | 0 | 1 |
| secure/sigma | 0 | 0 | 1 |
