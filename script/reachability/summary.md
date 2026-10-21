# Exported-API reachability summary

Root set: test-seeded: Test*/Benchmark*/Fuzz*/Example* functions of every test package (this module is a library and has no production main to seed from)
Entry points: 6153 across 224 test packages.

## Overview

| Metric | Count |
|---|---|
| Total exported | 14432 |
| Reached by a test | 9995 |
| Whitelisted | 4432 |
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
| cluster/measurement | CelsiusToInt16 | cluster/measurement/measurement.go | 1662 |
| endpoint | ComposeNodeLabel | endpoint/spec.go | 133 |

## Full by-package breakdown

| Package | Funcs | Types | Other |
|---|---|---|---|
| cluster/measurement | 1 | 0 | 0 |
| endpoint | 1 | 0 | 0 |
| secure/channel | 0 | 0 | 1 |
| secure/mattercert | 0 | 0 | 1 |
| secure/sigma | 0 | 0 | 1 |
