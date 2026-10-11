// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package operational

import "testing"

// TestEntryKeepsPeerTransportParameters: the peer's effective transports
// and TCP message bound from its SessionParameters are kept on the entry
// for the TCP sender, as matter.js keeps maxTcpMessageSize on the Session
// (Session.ts:86, :118); unset they read as zero.
func TestEntryKeepsPeerTransportParameters(t *testing.T) {
	t.Parallel()
	e := &Entry{}
	if tr, mx := e.PeerTransportParameters(); tr != 0 || mx != 0 {
		t.Fatalf("fresh entry states %d/%d", tr, mx)
	}
	e.SetPeerTransportParameters(1<<2, 64000)
	if tr, mx := e.PeerTransportParameters(); tr != 1<<2 || mx != 64000 {
		t.Fatalf("got %d/%d", tr, mx)
	}
}
