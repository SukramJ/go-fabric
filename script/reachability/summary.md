# Exported-API reachability summary

Root set: test-seeded: Test*/Benchmark*/Fuzz*/Example* functions of every test package (this module is a library and has no production main to seed from)
Entry points: 6299 across 228 test packages.

## Overview

| Metric | Count |
|---|---|
| Total exported | 14517 |
| Reached by a test | 10116 |
| Whitelisted | 4397 |
| **Unreached** | **4** |

## Top-20 packages by unreached exported identifiers

| Package | Funcs | Types | Other |
|---|---|---|---|
| endpoint | 1 | 0 | 0 |
| secure/channel | 0 | 0 | 1 |
| secure/mattercert | 0 | 0 | 1 |
| secure/sigma | 0 | 0 | 1 |

## First 50 unreached functions

| Package | Identifier | File | Line |
|---|---|---|---|
| endpoint | ComposeNodeLabel | endpoint/spec.go | 132 |

## Full by-package breakdown

| Package | Funcs | Types | Other |
|---|---|---|---|
| endpoint | 1 | 0 | 0 |
| secure/channel | 0 | 0 | 1 |
| secure/mattercert | 0 | 0 | 1 |
| secure/sigma | 0 | 0 | 1 |
