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
// package that lives in another module (ble/bluez) or that is yet to come.
//
// # Changing the rules
//
// An exception needs a reason and is added only in the commit that explains
// it; otherwise the list may only shrink. Moving a package between layers is
// an edit of layers.json with the reason in the commit message; the move is a
// claim about the architecture, so it is reviewed as one.
package archcheck
