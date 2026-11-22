// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/internal/deviceseam"
)

func newTestHost(numbers map[string]uint16) *fakeHost {
	return &fakeHost{clk: clock.System{}, numbers: numbers}
}

// TestAddPartMovesThePart (R2-P6-fsm-2501, decision 225): a part bound to
// no running node, added to another owner, moves — out of the former
// owner's parts (the bookkeeping of matter.js's owner setter,
// packages/node/src/endpoint/Endpoint.ts:706-725), unbound from the former owner's node, and bound
// with the new owner to its node. Neither node has started here, the case
// in which matter.js moves too.
func TestAddPartMovesThePart(t *testing.T) {
	t.Parallel()
	h1 := newTestHost(map[string]uint16{"test:a": 2, "test:p": 3})
	h2 := newTestHost(map[string]uint16{"test:b": 5, "test:p": 6})
	a, b, p := mustBridgedNode(t, "a"), mustBridgedNode(t, "b"), mustOnOff(t, "p")
	if err := a.AddPart(p); err != nil {
		t.Fatal(err)
	}
	detachA, _, err := deviceseam.Attach(a, h1)
	if err != nil {
		t.Fatal(err)
	}
	defer detachA()
	if n, ok := p.EndpointNumber(); !ok || n != 3 {
		t.Fatalf("the part on the first node: %d, %v", n, ok)
	}
	// The new owner not added yet: the part leaves the first node and
	// waits, detached, for its new owner's.
	if err := b.AddPart(p); err != nil {
		t.Fatalf("AddPart to another owner: %v", err)
	}
	if slices.Contains(a.Parts(), p) || !slices.Contains(b.Parts(), p) {
		t.Fatalf("after the move a.Parts %v, b.Parts %v", a.Parts(), b.Parts())
	}
	if _, ok := p.EndpointNumber(); ok {
		t.Error("the moved part still answers through the former owner's node")
	}
	detachB, _, err := deviceseam.Attach(b, h2)
	if err != nil {
		t.Fatalf("attach the new owner: %v", err)
	}
	if n, ok := p.EndpointNumber(); !ok || n != 6 {
		t.Errorf("the part on the new owner's node: %d, %v", n, ok)
	}
	// Moved back onto the first owner's node: attached there at once.
	if err := a.AddPart(p); err != nil {
		t.Fatal(err)
	}
	if n, ok := p.EndpointNumber(); !ok || n != 3 {
		t.Errorf("the part moved back: %d, %v", n, ok)
	}
	detachB()
	if n, ok := p.EndpointNumber(); !ok || n != 3 {
		t.Errorf("the former owner's detach touched the moved part: %d, %v", n, ok)
	}
	if err := a.AddPart(p); err != nil || len(a.Parts()) != 1 {
		t.Errorf("a second AddPart of a part: %v, parts %d", err, len(a.Parts()))
	}
}

// TestAddPartRefusesALivePart (R2-P6-fsm-2501, decision 225): a part bound
// to a running node does not move — AddPart answers ErrComposition and the
// part stays its former owner's, bound and numbered as before; matter.js
// refuses the move of a live endpoint too (Parts.ts:64-70,
// Construction.ts:284-287). Unbound from it, the part moves again.
func TestAddPartRefusesALivePart(t *testing.T) {
	t.Parallel()
	live := newTestHost(map[string]uint16{"test:a": 2, "test:p": 3})
	live.running = true
	a, b, p := mustBridgedNode(t, "a"), mustBridgedNode(t, "b"), mustOnOff(t, "p")
	if err := a.AddPart(p); err != nil {
		t.Fatal(err)
	}
	detachA, _, err := deviceseam.Attach(a, live)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AddPart(p); !errors.Is(err, ErrComposition) {
		t.Fatalf("AddPart of a live part: %v, want ErrComposition", err)
	}
	if !slices.Contains(a.Parts(), p) || len(b.Parts()) != 0 || p.owner.Load() != a.Device {
		t.Errorf("the refused move changed the parts: a %v, b %v", a.Parts(), b.Parts())
	}
	if n, ok := p.EndpointNumber(); !ok || n != 3 {
		t.Errorf("the refused part lost its node: %d, %v", n, ok)
	}
	p.life.Lock()
	running := p.running
	p.life.Unlock()
	if !running {
		t.Error("the refused part was stopped")
	}
	detachA()
	if err := b.AddPart(p); err != nil {
		t.Errorf("AddPart once the node let the part go: %v", err)
	}
}

// TestFailedAttachLeavesForeignDevicesBound (R2-P6-fsm-2501): an attach
// that is refused binds nothing and unbinds nothing — the probe of the
// report, a part another node holds, as an assertion.
func TestFailedAttachLeavesForeignDevicesBound(t *testing.T) {
	t.Parallel()
	h1 := newTestHost(map[string]uint16{"test:d": 2, "test:p": 3})
	h2 := newTestHost(map[string]uint16{"test:d": 9})
	d, p := mustBridgedNode(t, "d"), mustOnOff(t, "p")
	if err := d.AddPart(p); err != nil {
		t.Fatal(err)
	}
	detach, _, err := deviceseam.Attach(d, h1)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	if _, _, err := deviceseam.Attach(d, h2); !errors.Is(err, ErrAddedElsewhere) {
		t.Fatalf("a second node: %v, want ErrAddedElsewhere", err)
	}
	if n, ok := p.EndpointNumber(); !ok || n != 3 {
		t.Errorf("the part after the refused attach: %d, %v (want 3 on its node)", n, ok)
	}
	if err := p.SetReachable(context.Background(), false); !errors.Is(err, ErrNotBridged) {
		t.Errorf("SetReachable on the part: %v, want ErrNotBridged", err)
	}
}

// TestAddPartRefusesADeviceANodeHolds (R2-P6-fsm-2501): a bridged device a
// node holds directly is the Aggregator's; AddPart refuses it and changes
// nothing, and a part is not added to a node on its own.
func TestAddPartRefusesADeviceANodeHolds(t *testing.T) {
	t.Parallel()
	h := newTestHost(map[string]uint16{"test:p": 3})
	owner, p := mustBridgedNode(t, "owner"), mustOnOff(t, "p")
	detach, _, err := deviceseam.Attach(p, h)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	if err := owner.AddPart(p); !errors.Is(err, ErrAddedElsewhere) {
		t.Fatalf("AddPart of a device a node holds: %v, want ErrAddedElsewhere", err)
	}
	if len(owner.Parts()) != 0 {
		t.Errorf("the refused part is in the owner's parts")
	}
	if n, ok := p.EndpointNumber(); !ok || n != 3 {
		t.Errorf("the refused part lost its node: %d, %v", n, ok)
	}

	q := mustOnOff(t, "q")
	if err := owner.AddPart(q); err != nil {
		t.Fatal(err)
	}
	if _, _, err := deviceseam.Attach(q, newTestHost(nil)); !errors.Is(err, ErrComposition) {
		t.Errorf("a part attached on its own: %v, want ErrComposition", err)
	}
}

// TestConcurrentAddPartNeverFormsACycle (R2-P6-conc-2521): two devices that
// add each other at once never both succeed — the cycle check and the
// append are one step.
func TestConcurrentAddPartNeverFormsACycle(t *testing.T) {
	t.Parallel()
	for i := range 200 {
		a, b := mustBridgedNode(t, fmt.Sprintf("a%d", i)), mustBridgedNode(t, fmt.Sprintf("b%d", i))
		var wg sync.WaitGroup
		var ea, eb error
		wg.Go(func() { ea = a.AddPart(b.Device) })
		wg.Go(func() { eb = b.AddPart(a.Device) })
		wg.Wait()
		if ea == nil && eb == nil {
			t.Fatalf("run %d: both AddPart calls succeeded, a cycle", i)
		}
	}
}

// TestUpdateFnMayAskTheDevice (R2-P6-conc-2504): Update calls fn without
// the device's lock, so fn may ask the device anything.
func TestUpdateFnMayAskTheDevice(t *testing.T) {
	t.Parallel()
	d := mustOnOff(t, "u")
	// A fn run under the device's lock deadlocks here (go test's timeout).
	err := d.Update(context.Background(), func(st *HostStatement) {
		_, _ = d.EndpointNumber()
		_ = d.Parts()
		_ = d.Spec()
		st.Name = "Renamed"
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Spec().FriendlyName; got != "Renamed" {
		t.Errorf("FriendlyName %q, want Renamed", got)
	}
}

// TestUpdateKeepsAConcurrentRename (R2-P6-conc-2504): fields fn leaves
// unchanged are not written back, so a Rename that lands while fn runs is
// kept.
func TestUpdateKeepsAConcurrentRename(t *testing.T) {
	t.Parallel()
	d := mustOnOff(t, "keep")
	err := d.Update(context.Background(), func(st *HostStatement) {
		if err := d.Rename(context.Background(), "Written"); err != nil {
			t.Error(err)
		}
		st.DeviceAddress = "addr"
	})
	if err != nil {
		t.Fatal(err)
	}
	if s := d.Spec(); s.FriendlyName != "Written" || s.DeviceAddress != "addr" {
		t.Errorf("FriendlyName %q, DeviceAddress %q; want Written, addr", s.FriendlyName, s.DeviceAddress)
	}
}

// TestLifecycleMethodsOnAPartAreRefused (R2-P6-api-2503, step 12): a part
// has no BridgedDeviceBasicInformation of its own; SetReachable, Rename
// and Update answer ErrNotBridged, change nothing and tell the host
// nothing — before and after the node.
func TestLifecycleMethodsOnAPartAreRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	host := newTestHost(map[string]uint16{"test:owner": 2, "test:part": 3})
	owner, part := mustBridgedNode(t, "owner"), mustOnOff(t, "part")
	if err := owner.AddPart(part); err != nil {
		t.Fatal(err)
	}
	check := func(when string) {
		if err := part.SetReachable(ctx, false); !errors.Is(err, ErrNotBridged) {
			t.Errorf("%s: SetReachable: %v, want ErrNotBridged", when, err)
		}
		if err := part.Rename(ctx, "x"); !errors.Is(err, ErrNotBridged) {
			t.Errorf("%s: Rename: %v, want ErrNotBridged", when, err)
		}
		if err := part.Update(ctx, func(st *HostStatement) { st.Name = "y" }); !errors.Is(err, ErrNotBridged) {
			t.Errorf("%s: Update: %v, want ErrNotBridged", when, err)
		}
		if !part.Reachable() || part.Spec().FriendlyName != "part" {
			t.Errorf("%s: the refused calls changed the part: reachable %v, name %q", when, part.Reachable(), part.Spec().FriendlyName)
		}
	}
	check("before the node")
	detach, _, err := deviceseam.Attach(owner, host)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	check("on the node")
	host.mu.Lock()
	defer host.mu.Unlock()
	if len(host.reachable)+len(host.names)+len(host.hostNames) > 0 {
		t.Errorf("the host was told %v %v %v", host.reachable, host.names, host.hostNames)
	}
}

// TestRenameReturnsTheHostsRefusal (R2-P5-apidev-16, step 12): the store's
// refusal of the label reaches the caller, and the device keeps the name
// it had.
func TestRenameReturnsTheHostsRefusal(t *testing.T) {
	t.Parallel()
	refused := errors.New("store refused")
	host := &refusingHost{fakeHost: newTestHost(nil), err: refused}
	d := mustOnOff(t, "lamp")
	detach, _, err := deviceseam.Attach(d, host)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	if err := d.Rename(context.Background(), "Desk"); !errors.Is(err, refused) {
		t.Fatalf("Rename: %v, want the store's refusal", err)
	}
	if got := d.Spec().FriendlyName; got != "lamp" {
		t.Errorf("FriendlyName after the refused rename %q, want lamp", got)
	}
}

// refusingHost refuses every rename.
type refusingHost struct {
	*fakeHost
	err error
}

func (h *refusingHost) Rename(context.Context, endpoint.SourceKey, string) error { return h.err }

func mustOnOff(t *testing.T, name string) *Device {
	t.Helper()
	d, err := NewOnOffLight(testOptions(name))
	if err != nil {
		t.Fatal(err)
	}
	return d.Device
}

func mustBridgedNode(t *testing.T, name string) *BridgedNode {
	t.Helper()
	d, err := NewBridgedNode(testOptions(name))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestAddPartWaitingOnTheFlipSeesIt (decision 225, round 3): the node flips
// Running under deviceseam.Composing, the lock AddPart holds from its
// checks to its last change; an AddPart that has to wait for the flip
// sees the node running and is refused.
func TestAddPartWaitingOnTheFlipSeesIt(t *testing.T) {
	t.Parallel()
	host := newTestHost(map[string]uint16{"test:a": 2, "test:p": 3})
	a, b, p := mustBridgedNode(t, "a"), mustBridgedNode(t, "b"), mustOnOff(t, "p")
	if err := a.AddPart(p); err != nil {
		t.Fatal(err)
	}
	detach, _, err := deviceseam.Attach(a, host)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	done := make(chan error, 1)
	deviceseam.Composing(func() {
		go func() { done <- b.AddPart(p) }() // blocks on the lock held here
		host.mu.Lock()
		host.running = true
		host.mu.Unlock()
	})
	if err := <-done; !errors.Is(err, ErrComposition) {
		t.Fatalf("an AddPart that waited for the flip: %v, want ErrComposition", err)
	}
	if !slices.Contains(a.Parts(), p) || len(b.Parts()) != 0 {
		t.Errorf("the refused move changed the owners: a %v, b %v", a.Parts(), b.Parts())
	}
}
