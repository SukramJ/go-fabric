// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"fmt"
	"testing"

	"thingfabric.dev/go-fabric/client/exchange"
)

// TestInteractionClientMessengerReportInterruption ports the
// "reportInterruption" group of matter.js
// packages/protocol/test/interaction/InteractionClientMessengerTest.ts:837-876
// at db91886c (#5046) on the rule readDataReports applies to a failed wait
// for a chunk: a chunk-missing interruption is recorded when the wait for
// the next chunk of a report ends without a message, none when no report
// arrived at all. matter.js stalls a fake exchange after N chunks; the Go
// half of that over a scripted peer is
// TestClientSubscriptionHandlerParity's chunk-missing cases. Beyond the
// port: another failure of the wait — a PeerUnresponsiveError, which
// PeerMessageMissingError.Is matches, or a cancelled context — records
// nothing (ClientSubscriptionHandlerTest.ts:177-184).
func TestInteractionClientMessengerReportInterruption(t *testing.T) {
	t.Parallel()
	missing := fmt.Errorf("wait: %w", &exchange.PeerMessageMissingError{})
	for _, tc := range []struct {
		name         string
		continuation bool
		err          error
		want         reportInterruption
	}{
		{"records a missing chunk when the next chunk of a report does not arrive", true, missing, reportChunkMissing},
		{"records no interruption when no report arrives", false, missing, ""},
		{"another failure of the next chunk's wait", true, &exchange.PeerUnresponsiveError{}, ""},
		{"an aborted wait", true, context.Canceled, ""},
	} {
		if got := waitInterruption(tc.continuation, tc.err); got != tc.want {
			t.Errorf("%s: interruption %q, want %q", tc.name, got, tc.want)
		}
	}
}
