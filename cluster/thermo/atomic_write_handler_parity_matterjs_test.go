// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts
// at 862ad75f: BeginWrite's command-level and per-attribute answers, the
// timeout, and a write held by another peer.
//
// matter.js invokes on a MockServerNode as peers 1, 2 and 0 of one fabric
// (invokeAs); the port invokes over the wire from fabrictest pairs: the
// first peer is the commissioned pair's controller, a second peer a second
// controller commissioned onto its own fabric (another fabric index is
// another Atomic Writer, as another node id is), the PASE peer a PASE
// session in an open window. The ACL's privilege is fabrictest
// Pair.Grant. A refusal of the whole command is the *interaction.StatusError
// of the invoke (matter.js's cmd-status); a refusal per attribute is the
// AtomicResponse the device answers.
//
// The five cases of "CommitWrite of more than one attribute", which stage
// Schedules beside Presets, run in the package
// (atomic_commit_parity_matterjs_test.go). Not ported: the
// inherited-atomic-quality case (a TypeScript schema extension of a
// behavior class, which Go has no counterpart for: the quality comes from
// the generated definition).
package thermo_test

import (
	"testing"
	"time"

	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// The attributes of AtomicWriteHandlerTest.ts:39-45.
const (
	nonAtomicAttribute       = tdef.AttrOccupiedHeatingSetpoint
	unsupportedAttribute     = 0xFFF0
	attributeListAttribute   = 0xFFFB
	nonAtomicManageAttribute = tdef.AttrSystemMode
)

// createNode is AtomicWriteHandlerTest.ts:176-181, :216-233: the presets
// thermostat (:158-174, thermostatConfig with five presets) with the
// controller at privilege.
func createNode(t *testing.T, privilege store.Privilege) *thermostatPair {
	t.Helper()
	p := commissionedThermostat(t, func(*fabrictest.Site) thermo.ThermostatConfig { return thermostatConfig(5, []thermo.Preset{}) })
	if err := p.pair.Grant(parityCtx(t), privilege); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParityMatterJS_AtomicWriteHandlerTest(t *testing.T) {
	presets := []uint32{presetsAttribute}
	timeout := timeoutMs(5000)

	t.Run("rejects a second BeginWrite on the same cluster/endpoint with INVALID_IN_STATE (§7.15.6.4.1)", func(t *testing.T) {
		p := createNode(t, store.PrivilegeAdminister)
		ctx := parityCtx(t)
		first, err := p.atomicRequest(ctx, p.pair, beginWrite, presets, timeout)
		if err != nil {
			t.Fatal(err)
		}
		decodeAtomicResponse(t, first)
		_, err = p.atomicRequest(ctx, p.pair, beginWrite, presets, timeout)
		wantStatus(t, err, im.StatusInvalidInState)
	})

	t.Run("discards an abandoned atomic write when the timeout expires (§7.15.6.4.1 step 3.5.3)", func(t *testing.T) {
		p := createNode(t, store.PrivilegeAdminister)
		ctx := parityCtx(t)
		first, err := p.atomicRequest(ctx, p.pair, beginWrite, presets, timeout)
		if err != nil {
			t.Fatal(err)
		}
		decodeAtomicResponse(t, first)

		p.site.Fake().Advance(5001 * time.Millisecond)

		// The state was discarded on timeout, so a fresh BeginWrite succeeds
		// instead of failing with INVALID_IN_STATE.
		second, err := p.atomicRequest(ctx, p.pair, beginWrite, presets, timeout)
		if err != nil {
			t.Fatal(err)
		}
		decodeAtomicResponse(t, second)
	})

	t.Run("returns INVALID_COMMAND for a non-atomic attribute in BeginWrite (§7.15.6.4.1 step 3.1.2)", func(t *testing.T) {
		p := createNode(t, store.PrivilegeAdminister)
		r, err := p.atomicRequest(parityCtx(t), p.pair, beginWrite, []uint32{nonAtomicAttribute}, timeout)
		if err != nil {
			t.Fatal(err)
		}
		wantAtomic(t, decodeAtomicResponse(t, r), tdef.StatusFailure,
			[]tdef.AtomicResponseAttributeStatusEntry{{AttributeId: nonAtomicAttribute, StatusCode: tdef.StatusInvalidCommand}}, nil)
	})

	t.Run("returns UNSUPPORTED_ACCESS ahead of INVALID_COMMAND for an inaccessible non-atomic attribute (§7.15.6.4.1 step 3.1.1)", func(t *testing.T) {
		p := createNode(t, store.PrivilegeOperate)
		r, err := p.atomicRequest(parityCtx(t), p.pair, beginWrite, []uint32{nonAtomicManageAttribute}, timeout)
		if err != nil {
			t.Fatal(err)
		}
		wantAtomic(t, decodeAtomicResponse(t, r), tdef.StatusFailure,
			[]tdef.AtomicResponseAttributeStatusEntry{{AttributeId: nonAtomicManageAttribute, StatusCode: tdef.StatusUnsupportedAccess}}, nil)
	})

	t.Run("checks the whole request before the per-attribute statuses of a BeginWrite (§7.15.6.4)", func(t *testing.T) {
		t.Run("returns INVALID_COMMAND for an attribute the server does not support", func(t *testing.T) {
			p := createNode(t, store.PrivilegeAdminister)
			_, err := p.atomicRequest(parityCtx(t), p.pair, beginWrite, []uint32{presetsAttribute, unsupportedAttribute}, timeout)
			wantStatus(t, err, im.StatusInvalidCommand)
		})

		t.Run("returns INVALID_COMMAND for a missing timeout while another peer holds the attribute", func(t *testing.T) {
			p := createNode(t, store.PrivilegeAdminister)
			ctx := parityCtx(t)
			if _, err := p.atomicRequest(ctx, p.pair, beginWrite, presets, timeout); err != nil {
				t.Fatal(err)
			}
			second := p.secondPeer(t)
			_, err := p.atomicRequest(ctx, second, beginWrite, presets, nil)
			wantStatus(t, err, im.StatusInvalidCommand)
		})

		t.Run("returns INVALID_COMMAND for a PASE session, whose Atomic Writer ID is invalid, while another peer holds the attribute", func(t *testing.T) {
			p := createNode(t, store.PrivilegeAdminister)
			ctx := parityCtx(t)
			if _, err := p.atomicRequest(ctx, p.pair, beginWrite, presets, timeout); err != nil {
				t.Fatal(err)
			}
			if err := p.pair.Device.OpenWindow(ctx, 0); err != nil {
				t.Fatal(err)
			}
			pase, err := p.site.AddController(fabrictest.ControllerOptions{FabricID: 2, NodeID: 0x2222}).PASE(ctx, p.pair.Device)
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.atomicRequest(ctx, pase, beginWrite, presets, timeout)
			wantStatus(t, err, im.StatusInvalidCommand)
		})

		t.Run("returns INVALID_COMMAND ahead of INVALID_IN_STATE for an unsupported attribute from a peer holding a write", func(t *testing.T) {
			p := createNode(t, store.PrivilegeAdminister)
			ctx := parityCtx(t)
			if _, err := p.atomicRequest(ctx, p.pair, beginWrite, presets, timeout); err != nil {
				t.Fatal(err)
			}
			_, err := p.atomicRequest(ctx, p.pair, beginWrite, []uint32{unsupportedAttribute}, timeout)
			wantStatus(t, err, im.StatusInvalidCommand)
		})

		t.Run("reports a global attribute as supported but not atomic", func(t *testing.T) {
			p := createNode(t, store.PrivilegeAdminister)
			r, err := p.atomicRequest(parityCtx(t), p.pair, beginWrite, []uint32{attributeListAttribute}, timeout)
			if err != nil {
				t.Fatal(err)
			}
			wantAtomic(t, decodeAtomicResponse(t, r), tdef.StatusFailure,
				[]tdef.AtomicResponseAttributeStatusEntry{{AttributeId: attributeListAttribute, StatusCode: tdef.StatusInvalidCommand}}, nil)
		})
	})

	// §7.15.3 mandates INVALID_IN_STATE, but CHIP and TC_TSTAT_4_2 step 15
	// expect BUSY when another peer holds the atomic write
	// (AtomicWriteHandlerTest.ts:371-372).
	t.Run("rejects a write to an attribute held by a different peer with BUSY", func(t *testing.T) {
		p := createNode(t, store.PrivilegeAdminister)
		ctx := parityCtx(t)
		first, err := p.atomicRequest(ctx, p.pair, beginWrite, presets, timeout)
		if err != nil {
			t.Fatal(err)
		}
		decodeAtomicResponse(t, first)

		second := p.secondPeer(t)
		statuses, err := p.writePresets(ctx, second, []thermo.Preset{})
		if err != nil {
			t.Fatal(err)
		}
		if len(statuses) != 1 || statuses[0].Status != im.StatusBusy || statuses[0].Path.Attribute != presetsAttribute ||
			statuses[0].Path.HasListIndex || statuses[0].Path.ListAppend {
			t.Errorf("write statuses %+v, want one BUSY for Presets without a list index", statuses)
		}
	})
}
