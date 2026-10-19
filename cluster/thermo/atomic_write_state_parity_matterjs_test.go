// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/thermostat/AtomicWriteStateTest.ts
// at 862ad75f: the atomic-write state emits closed exactly once.
//
// The state is unexported (cluster/thermo/atomic.go, the port of
// AtomicWriteState.ts), so the port runs in the package, on a
// clocktest.Fake; matter.js's `closed` Observable is whenClosed.
package thermo

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
)

// newParityAtomicState is AtomicWriteStateTest.ts:15-26 createState: peer
// (fabric 1, node 1), the Presets attribute, a 1000 ms timeout.
func newParityAtomicState() *atomicWriteState {
	st := newAtomicWriteState(atomicPeer{fabricIndex: 1, nodeID: 1}, []uint32{tdef.AttrPresets}, 1000)
	st.start(clocktest.New(time.Unix(0, 0)))
	return st
}

func TestAtomicWriteStateParity(t *testing.T) {
	t.Parallel()
	t.Run("emits closed once when closed repeatedly", func(t *testing.T) {
		t.Parallel()
		st := newParityAtomicState()
		closedCount := 0
		st.whenClosed(func() { closedCount++ })

		st.close()
		st.close()

		if closedCount != 1 {
			t.Errorf("closed emitted %d times, want 1", closedCount)
		}
	})
}
