// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"strings"
	"testing"

	padef "thingfabric.dev/go-fabric/cluster/spec/pushavstreamtransport"
	"thingfabric.dev/go-fabric/im"
)

// TestPushDeallocatesTheConnectionItCouldNotStore (R2-C2-err-1602): the
// port allocated the transport, CurrentConnections refused the entry, so
// the server deallocates it at the port before it answers FAILURE — no
// controller can deallocate a connection the table does not hold.
func TestPushDeallocatesTheConnectionItCouldNotStore(t *testing.T) {
	t.Parallel()
	p, m := newPush(t)
	o := motionOptions()
	track := strings.Repeat("v", 17) // above TrackName's 16, which no check before the commit holds
	o.ContainerOptions.CmafContainerOptions.TrackName = &track
	_, err := p.MatterInvoke(peer(1, 0x55), padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: o})
	wantCode(t, "AllocatePushTransport", statusOf(err), im.StatusFailure)
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.allocated) != 0 || len(m.deallocated) != 1 {
		t.Errorf("port holds %d transports, deallocated %v; want the refused one deallocated", len(m.allocated), m.deallocated)
	}
	if n := len(p.Connections()); n != 0 {
		t.Errorf("%d connections stored", n)
	}
}
