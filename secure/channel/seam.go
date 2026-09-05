// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package channel

// This file installs the module-internal seam that the bridge reads on
// behalf of bridge/bridgetest. It declares no exported identifier on
// purpose: anchoring the inbound duplicate-detection window is something
// only the wire may do in production — the first message that decrypts
// under the session anchors it — so an exported Session method for it would
// be production API that exists solely to serve tests. See the channelseam
// package doc.

import "github.com/SukramJ/go-fabric/internal/channelseam"

func init() {
	channelseam.AnchorPeerCounter = func(sess any, counter uint32) bool {
		s, ok := sess.(*Session)
		if !ok || s == nil {
			return false
		}
		// Accept on an as-yet-unprimed window is precisely the anchor:
		// it stores counter as the maximum and seeds the bitmap from
		// initialBitmap (all ones for the no-rollover window a secure
		// session uses). On a window that has already seen traffic it
		// merely records counter, which is not an anchor — the false it
		// returns for an already-recorded counter is the only signal
		// available for that, and the caller surfaces it as an error.
		return s.in.Accept(counter)
	}
}
