// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package archcheck holds the module's layering as a test.
//
// ADR 0026 principle 1 orders the module's packages into layers, bottom up
// (foundation, transport, security and storage, model and protocol, servers,
// endpoints, roles, facade, support), and allows a package to import only its
// own layer or a lower one. Principle 11 makes that a guard rather than a
// review habit: [TestLayering] loads every package of the module, reduces each
// non-test import to a module-relative path, assigns both ends to a layer from
// layers.json and fails on every edge that points up and is not a listed
// exception. It also fails on a package no layer names, on an exception that
// no longer occurs, and on a rule file that is malformed. It runs under
// "go test ./..." and as "make archcheck".
//
// # Reading a failure
//
// A violation is one line, "from -> to (layer A -> layer B)": package from
// imports package to, and to sits in a higher layer. Either the import is
// wrong (move the code it needs down, or inject it, ADR 0026 principle 12) or
// the package sits in the wrong layer (see below). A "stale exception" line
// means the edge it excuses is gone; delete the exception. An "unassigned
// package" line means a new package needs a place in layers.json.
//
// # The rule file
//
// layers.json lists the layers lowest first. A pattern "x" names the package x;
// "x/..." names x and everything below it. A package belongs to the layer of
// the most specific pattern that matches it (the longest base path, an exact
// name before a "/..." pattern of the same base; the module root is "."), whatever the order of the
// file, so "cluster/spec/..." in model-and-protocol beats "cluster/..." in
// servers, and the exact "cluster" (shared machinery, model-and-protocol)
// beats the "cluster/..." pattern of the servers layer. Patterns that match nothing are not an error: a layer may name a
// package that is yet to come. The nested modules (ble/bluez, ble/esphome,
// wifi/wpasupplicant, examples/ble-wifi-light) are loaded from their own
// directories and held to the same layers: they are released on their own,
// and the root's ./... does not reach them.
//
// # Forbidden imports
//
// The layer order cannot state that two packages of the same layer, or a
// higher layer and a lower one, must not meet. The "forbidden" list of
// layers.json can: each rule names from-patterns, to-patterns and a reason,
// and TestLayering fails on every package a to-pattern matches that a package
// a from-pattern matches depends on through non-test imports, whatever their
// layers — directly, "forbidden import: from -> to (reason)", or through a
// chain of other packages, "forbidden import: from -> to via a -> b (reason)"
// with the shortest chain (review II, R4-test-4801, decision 601: a seam
// between the two does not hide the dependency). A rule's "except" list names
// the packages a from-pattern matches that it still does not hold, each exact
// and with its own reason; an exception whose package no longer depends on
// what the rule forbids is reported as "stale forbidden exception". The first
// rule holds ADR 0020 decision 1: bridge, node, device and endpoint and every
// package below them do not depend on controller/... (review II, R3-api-3780,
// decisions 556, 583), with one exception, the test support bridge/bridgetest;
// TestADR0020SiblingRuleIsInTheRules fails when the rule is taken out of the
// file, narrowed, or given another exception. A forbidden import is fixed by
// moving the code both sides need to a role-neutral package, as the ledger
// client moved from controller/dcl to dcl.
//
// # Changing the rules
//
// An exception needs a reason and is added only in the commit that explains
// it; otherwise the list may only shrink. Moving a package between layers is
// an edit of layers.json with the reason in the commit message; the move is a
// claim about the architecture, so it is reviewed as one.
package archcheck
