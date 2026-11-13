// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// noGrantACL is an ACL source without entries: every request is denied.
type noGrantACL struct{}

func (noGrantACL) ListACL(context.Context, uint8) ([]store.ACLEntry, error) { return nil, nil }

// TestAttachACLListerAppliesToTheRunningBridge: an ACL source attached
// after Start is the one the next access check reads (review II,
// W2-api-10). Before, AttachACLLister stored the lister for the next
// Reassemble — which on a node whose devices do not change may never
// come — so a host that replaced its lister on a running bridge kept the
// old one enforced. The readers run while the lister is swapped: under
// -race this is the test of the dispatcher's swap.
func TestAttachACLListerAppliesToTheRunningBridge(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	checker, ok := b.Dispatcher().(im.ACLChecker)
	if !ok {
		t.Fatal("the bridge's dispatcher is no im.ACLChecker")
	}
	check := func() im.StatusCode {
		// fabric 1, a CASE subject, endpoint 0 Descriptor, View.
		return checker.CheckACL(context.Background(), 1, 0x1234, nil, 0, 0x001D, 1)
	}
	if got := check(); got != im.StatusUnsupportedAccess {
		t.Fatalf("CheckACL without a lister = %v, want UnsupportedAccess", got)
	}

	stop := make(chan struct{})
	var readers sync.WaitGroup
	for range 4 {
		readers.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					check()
				}
			}
		})
	}
	b.AttachACLLister(endpoint.UnenforcedACL{})
	if got := check(); got != im.StatusSuccess {
		t.Errorf("CheckACL after AttachACLLister(UnenforcedACL) on a running bridge = %v, want Success without a Reassemble", got)
	}
	b.AttachACLLister(noGrantACL{})
	if got := check(); got != im.StatusUnsupportedAccess {
		t.Errorf("CheckACL after the lister was replaced by one without entries = %v, want UnsupportedAccess", got)
	}
	close(stop)
	readers.Wait()

	// The dispatcher a Reassemble builds carries the current lister.
	b.AttachACLLister(endpoint.UnenforcedACL{})
	if err := b.Reassemble(context.Background()); err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	rebuilt, _ := b.Dispatcher().(im.ACLChecker)
	if got := rebuilt.CheckACL(context.Background(), 1, 0x1234, nil, 0, 0x001D, 1); got != im.StatusSuccess {
		t.Errorf("CheckACL on the reassembled dispatcher = %v, want Success", got)
	}
}
