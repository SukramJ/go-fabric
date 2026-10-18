# Exported-API reachability summary

Root set: test-seeded: Test*/Benchmark*/Fuzz*/Example* functions of every test package (this module is a library and has no production main to seed from)
Entry points: 5316 across 218 test packages.

## Overview

| Metric | Count |
|---|---|
| Total exported | 12784 |
| Reached by a test | 9014 |
| Whitelisted | 3763 |
| **Unreached** | **7** |

## Top-20 packages by unreached exported identifiers

| Package | Funcs | Types | Other |
|---|---|---|---|
| cluster/measurement | 1 | 0 | 0 |
| controller/dcl/snapshot | 1 | 0 | 0 |
| endpoint | 1 | 0 | 0 |
| secure/channel | 0 | 0 | 1 |
| secure/mattercert | 0 | 0 | 1 |
| secure/sigma | 0 | 0 | 1 |
| transport/btp | 0 | 1 | 0 |

## First 50 unreached functions

| Package | Identifier | File | Line |
|---|---|---|---|
| cluster/measurement | CelsiusToInt16 | cluster/measurement/measurement.go | 1662 |
| controller/dcl/snapshot | LoadAt | controller/dcl/snapshot/snapshot.go | 131 |
| endpoint | ComposeNodeLabel | endpoint/spec.go | 133 |

## Full by-package breakdown

| Package | Funcs | Types | Other |
|---|---|---|---|
| cluster/measurement | 1 | 0 | 0 |
| controller/dcl/snapshot | 1 | 0 | 0 |
| endpoint | 1 | 0 | 0 |
| secure/channel | 0 | 0 | 1 |
| secure/mattercert | 0 | 0 | 1 |
| secure/sigma | 0 | 0 | 1 |
| transport/btp | 0 | 1 | 0 |
