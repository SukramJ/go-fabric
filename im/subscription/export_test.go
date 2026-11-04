// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

// HoldReporting marks sub as sending a report, as the engine does while
// a report of its goes out, until the returned function is called.
func HoldReporting(sub *Subscription) func() {
	for !sub.reporting.CompareAndSwap(false, true) {
	}
	return func() { sub.reporting.Store(false) }
}
