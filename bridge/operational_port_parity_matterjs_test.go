// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
)

// TestOperationalPortParityMatterJS ports the commissionable-node cases of
// matter.js packages/node/test/node/ServerNodeTest.ts:326-376 at 862ad75f
// ("operational port"): a node without an explicit port is on the standard
// Matter port 5540 (STANDARD_MATTER_PORT), an explicit one is honoured.
// The port is the configured one before the socket binds
// (effectiveUDPPort), which node.Config.Network.Listen feeds; the test binds
// nothing, so it does not race a daemon on 5540.
func TestOperationalPortParityMatterJS(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		listen string
		want   int
	}{
		{"binds the standard Matter port for a commissionable node without an explicit port", "", 5540},
		{"honors an explicit port on a commissionable node", ":5555", 5555},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: c.listen}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := b.effectiveUDPPort(); got != c.want {
				t.Fatalf("port %d, want %d", got, c.want)
			}
		})
	}
}
