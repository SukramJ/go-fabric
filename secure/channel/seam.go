// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package channel

// This file installs the module-internal seam that the bridge reads on
// behalf of bridge/bridgetest. It declares no exported identifier on
// purpose: moving the inbound duplicate-detection window is something only
// the wire may do in production — every message that decrypts under the
// session records its counter there — so an exported Session method for it would
// be production API that exists solely to serve tests. See the channelseam
// package doc.

import "github.com/SukramJ/go-fabric/internal/channelseam"

func init() {
	channelseam.AnchorPeerCounter = func(sess any, counter uint32) bool {
		s, ok := sess.(*Session)
		if !ok || s == nil {
			return false
		}
		// Accept records counter exactly as the arrival of a message
		// with that counter would: on a fresh session's window, which
		// starts at 0 (see New), it becomes the maximum, and the
		// counters below it stay acceptable within the 32-window as
		// they would in production. On a window that has already seen
		// the counter the false it returns is the only signal available
		// for that, and the caller surfaces it as an error.
		return s.in.Accept(counter)
	}
}
