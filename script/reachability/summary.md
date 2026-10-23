# Exported-API reachability summary

Root set: test-seeded: Test*/Benchmark*/Fuzz*/Example* functions of every test package (this module is a library and has no production main to seed from)
Entry points: 6303 across 228 test packages.

## Overview

| Metric | Count |
|---|---|
| Total exported | 14558 |
| Reached by a test | 10127 |
| Whitelisted | 4426 |
| **Unreached** | **5** |

## Top-20 packages by unreached exported identifiers

| Package | Funcs | Types | Other |
|---|---|---|---|
| endpoint | 1 | 0 | 0 |
| node | 1 | 0 | 0 |
| secure/channel | 0 | 0 | 1 |
| secure/mattercert | 0 | 0 | 1 |
| secure/sigma | 0 | 0 | 1 |

## First 50 unreached functions

| Package | Identifier | File | Line |
|---|---|---|---|
| endpoint | ComposeNodeLabel | endpoint/spec.go | 132 |
| node | NewActions | node/routes.go | 111 |

## Full by-package breakdown

| Package | Funcs | Types | Other |
|---|---|---|---|
| endpoint | 1 | 0 | 0 |
| node | 1 | 0 | 0 |
| secure/channel | 0 | 0 | 1 |
| secure/mattercert | 0 | 0 | 1 |
| secure/sigma | 0 | 0 | 1 |
