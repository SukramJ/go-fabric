// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/store"
)

// pausingACLPort is the node's store with one ListACL that, once armed,
// reports its entry and then waits for release, or for the controller's
// write to wait on the AccessControl lock, before it returns.
type pausingACLPort struct {
	store.Port
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (p *pausingACLPort) ListACL(ctx context.Context, fabricIndex uint8) ([]store.ACLEntry, error) {
	acl, err := p.Port.ListACL(ctx, fabricIndex)
	if p.armed.CompareAndSwap(true, false) {
		close(p.entered)
		// Until the controller's write went through, or waits for the
		// AccessControl lock this read runs under.
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); runtime.Gosched() {
			select {
			case <-p.release:
				return acl, err
			default:
			}
			if blockedIn("MatterWriteChanged") {
				break
			}
		}
	}
	return acl, err
}

// TestLocalACLEntryTakesTheACLWriteLock (review II, R2-P6-sec-2401): the
// node's own ACL entries — the WebRtcTransportRequestor's and the OTA
// provider's — are read, compared and stored under the AccessControl
// server's write lock, so a controller's ACL write of the same fabric
// runs before or after them as a whole; the controller's entry is in the
// fabric's ACL at the end, beside the node's when the node's ran first.
func TestLocalACLEntryTakesTheACLWriteLock(t *testing.T) {
	t.Parallel()
	for _, site := range []string{"requestor", "ota provider"} {
		t.Run(site, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			n, _, idx := commissionedKeeperNode(t, mdns.NewNoop())
			port := &pausingACLPort{Port: n.st, entered: make(chan struct{}), release: make(chan struct{})}
			n.st = port
			port.armed.Store(true)

			done := make(chan struct{})
			go func() {
				defer close(done)
				if site == "requestor" {
					n.ensureRequestorACLOn(ctx, idx, 7)
				} else {
					n.ensureOTAProviderACLOn(ctx, idx, 7)
				}
			}()
			<-port.entered

			admin := uint64(0x0000_0000_0000_1122)
			written := make(chan error, 1)
			go func() {
				written <- n.root.AccessControl.MatterWrite(ctx, 0x0000, []core.AccessControlEntryStruct{{
					Privilege: 5, AuthMode: 2, Subjects: []uint64{admin}, FabricIndex: idx,
				}})
				close(port.release)
			}()
			<-done
			if err := <-written; err != nil {
				t.Fatalf("the controller's ACL write: %v", err)
			}

			acl, err := n.st.ListACL(ctx, idx)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(acl, func(e store.ACLEntry) bool { return slices.Contains(e.Subjects, admin) }) {
				t.Fatalf("fabric %d's ACL after the node's entry and a controller's write: %+v, want the controller's Administer entry", idx, acl)
			}
		})
	}
}
