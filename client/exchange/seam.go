// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import "github.com/SukramJ/go-fabric/internal/exchangeseam"

// The one-way seam for a test outside this package that stands in for
// traffic it did not send; see the exchangeseam package doc for why it is
// not a method on Session.
func init() {
	exchangeseam.NotifyActivity = func(sess any, received bool) {
		if s, ok := sess.(*Session); ok {
			s.notifyActivity(received)
		}
	}
}
