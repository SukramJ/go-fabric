// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge_test

import (
	"context"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/store"
)

// unresolvingLookup is a session lookup that deliberately does NOT implement
// bridge.SessionFabricResolver — the shape a host ends up with when it wires
// a session store without that optional capability.
type unresolvingLookup struct{}

func (unresolvingLookup) Lookup(uint16) (*channel.Session, bool) { return nil, false }

// resolvingLookup is the same thing with the capability present.
type resolvingLookup struct{ unresolvingLookup }

func (resolvingLookup) FabricFor(uint16) (uint8, bool) { return 1, true }

// emptyACL lists nothing. What matters is that it is non-nil: a host that
// attaches an ACL source is asking for access control.
type emptyACL struct{}

func (emptyACL) ListACL(context.Context, uint8) ([]store.ACLEntry, error) { return nil, nil }

// TestStartRefusesAnACLItCannotEnforce is the guard on a silent
// authorisation bypass.
//
// CheckACL answers Success for fabric index 0 because that means "PASE, no
// fabric yet" — correct while commissioning. resolveSessionFabric returns
// that same 0 when the session lookup cannot name a session's fabric. Put
// together, a host that attaches an ACL but a lookup without
// SessionFabricResolver has every CASE session resolve to 0, and every
// operational request passes the access check as though the device were
// still being commissioned. Nothing fails and nothing logs; the ACL is
// simply never applied.
//
// The bridge refuses to start in that configuration, because it is a wiring
// mistake rather than a runtime state, and because the alternative is a
// bridge that looks configured and is not.
func TestStartRefusesAnACLItCannotEnforce(t *testing.T) {
	t.Parallel()

	b := newTestBridge(t)
	b.AttachACLLister(emptyACL{})
	b.AttachSessionLookup(unresolvingLookup{})

	err := b.Start(context.Background())
	if err == nil {
		_ = b.Stop(context.Background())
		t.Fatal("Start accepted an ACL lister with a session lookup that cannot resolve fabrics — " +
			"every CASE session would resolve to fabric 0 and pass CheckACL as if it were commissioning")
	}
	for _, want := range []string{"SessionFabricResolver", "fabric 0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the Start error does not mention %q, so it does not tell the operator what is "+
				"wrong or how to fix it: %v", want, err)
		}
	}
}

// TestStartAcceptsAnACLItCanEnforce is the other direction, and the reason
// the guard above is not simply "reject ACLs": the identical configuration
// with a resolving lookup must start.
func TestStartAcceptsAnACLItCanEnforce(t *testing.T) {
	t.Parallel()

	b := newTestBridge(t)
	b.AttachACLLister(emptyACL{})
	b.AttachSessionLookup(resolvingLookup{})

	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start refused a correctly wired ACL: %v", err)
	}
	t.Cleanup(func() { _ = b.Stop(context.Background()) })
}

// TestStartAcceptsNoACLAtAll covers the third configuration, which must stay
// allowed: a host that attaches no ACL is not asking for access control, and
// CheckACL already denies every operational request for it.
func TestStartAcceptsNoACLAtAll(t *testing.T) {
	t.Parallel()

	b := newTestBridge(t)
	b.AttachSessionLookup(unresolvingLookup{})

	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start refused a bridge with no ACL lister, which needs no fabric resolver: %v", err)
	}
	t.Cleanup(func() { _ = b.Stop(context.Background()) })
}
