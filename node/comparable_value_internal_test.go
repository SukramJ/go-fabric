// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/endpoint"
)

// ifaceEP is an Endpoint whose type is comparable and whose value is not
// when meta holds a slice.
type ifaceEP struct {
	s    *endpoint.Spec
	meta any
}

func (e ifaceEP) Spec() endpoint.Spec { return *e.s }

// ifaceSrv is a cluster server of the same shape.
type ifaceSrv struct {
	*spec.Server
	meta any
}

// TestComparableValueJudgesTheValue (review II, R4-api-4703, R4-api-4753,
// the Fable diff read's B1): the predicates judge the value, so a value
// of a comparable type whose interface field holds a slice is never
// compared with == — no panic in sameEndpoint, sameInstance or a
// persister's map key — while one holding a comparable value still is.
func TestComparableValueJudgesTheValue(t *testing.T) {
	s := &endpoint.Spec{}
	bad1, bad2 := ifaceEP{s: s, meta: []string{"a"}}, ifaceEP{s: s, meta: []string{"a"}}
	ok1, ok2 := ifaceEP{s: s, meta: "a"}, ifaceEP{s: s, meta: "a"}
	if comparableValue(bad1) || comparableValue(nil) || !comparableValue(ok1) {
		t.Fatalf("comparableValue: slice-holding %v, nil %v, string-holding %v; want false, false, true",
			comparableValue(bad1), comparableValue(nil), comparableValue(ok1))
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("predicate panicked: %v", r)
			}
		}()
		if !sameEndpoint(bad1, bad2) {
			t.Error("sameEndpoint of two key-matched values that cannot be compared: false; want true (the key matched them)")
		}
		if sameEndpoint(bad1, ok1) != true {
			t.Error("sameEndpoint with one value that cannot be compared: false; want true")
		}
		if sameInstance(bad1, bad1) || sameInstance(bad1, ok1) || sameInstance(ok1, bad1) {
			t.Error("sameInstance with a value that cannot be compared: true; want false")
		}
		if comparableServer(ifaceSrv{meta: []string{"a"}}) {
			t.Error("comparableServer of a server holding a slice: true; want false")
		}
	}()
	if !sameEndpoint(ok1, ok2) || !sameInstance(ok1, ok2) || sameInstance(ok1, ifaceEP{s: s, meta: "b"}) {
		t.Error("comparable values: sameEndpoint/sameInstance do not compare with ==")
	}
	if !comparableServer(ifaceSrv{meta: "a"}) || comparableServer(nil) {
		t.Error("comparableServer: a server holding a string is comparable, nil is not")
	}
}
