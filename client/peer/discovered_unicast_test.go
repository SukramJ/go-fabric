// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// TestDiscoveredAddressesAreUnicastOnly: a discovered address meets the
// filter a hinted one meets (unicastDestination): an AAAA record of the
// node's host that names the link's all-nodes group is not attempted.
func TestDiscoveredAddressesAreUnicastOnly(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig507(t)
		r.nodeIPs = []string{"ff02::1"}
		p := r.peer("")
		r.nodeOnly()
		r.connect(p)
		r.advance(time.Minute)
		for _, s := range r.sent {
			if strings.HasPrefix(s.to, "[ff02::1") {
				t.Fatalf("Sigma1 sent to the all-nodes group %s; order: %s", s.to, r.order())
			}
		}
	})
}
