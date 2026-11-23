// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"reflect"

	"thingfabric.dev/go-fabric/contract"
)

// keptSlot names which of the endpoint's materialisation sources a kept
// server was built from.
type keptSlot uint8

const (
	keptMeasurement keptSlot = iota // Endpoint.Measurement
	keptPowerSource                 // Endpoint.PowerSource
)

// keptKey identifies a kept server on its endpoint.
type keptKey struct {
	slot    keptSlot
	cluster uint32
}

// keptServer is a kept instance and the source it was built from.
type keptServer struct {
	src any
	srv contract.ClusterServer
}

// keepStateful replaces every [contract.StatefulServer] in built — what the
// materialiser of slot just built from src — by the instance the endpoint
// keeps for that slot and cluster when it was built from the same source,
// and keeps the new one otherwise. A kept server of the slot whose cluster
// the materialiser no longer builds is dropped. Safe for concurrent use:
// two dispatches that materialise at once hand out the same instance.
func (e *Endpoint) keepStateful(slot keptSlot, src any, built []contract.ClusterServer) []contract.ClusterServer {
	st := e.endpointState()
	st.mu.Lock()
	defer st.mu.Unlock()
	seen := map[uint32]struct{}{}
	for i, s := range built {
		if _, ok := s.(contract.StatefulServer); !ok {
			continue
		}
		key := keptKey{slot: slot, cluster: s.MatterClusterID()}
		seen[key.cluster] = struct{}{}
		if k, ok := st.kept[key]; ok && sameSource(k.src, src) {
			built[i] = k.srv
			continue
		}
		if st.kept == nil {
			st.kept = map[keptKey]keptServer{}
		}
		st.kept[key] = keptServer{src: src, srv: s}
	}
	for key := range st.kept {
		if _, ok := seen[key.cluster]; key.slot == slot && !ok {
			delete(st.kept, key)
		}
	}
	return built
}

// sameSource reports whether a and b are the same materialisation source.
// It never panics: a source whose dynamic value is not comparable is never
// the same as another (R2-C2-api-0651 is the same trap).
func sameSource(a, b any) bool {
	if a == nil || b == nil {
		return false
	}
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	if va.Type() != vb.Type() || !va.Comparable() || !vb.Comparable() {
		return false
	}
	return va.Equal(vb)
}
