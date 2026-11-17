// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import "context"

// HoldReporting marks sub as sending a report, as the engine does while
// a report of its goes out, until the returned function is called.
func HoldReporting(sub *Subscription) func() {
	for !sub.reporting.CompareAndSwap(false, true) {
	}
	return func() { sub.endReport() }
}

// DiscardReports is a [ReportSender] that sends nothing and says so: the
// manager of a test of the cadence math alone, as a nil reporter was
// before the single report hook (review II, R2-P6-api-0508).
func DiscardReports(context.Context, *Subscription, Report) ReportOutcome {
	return NothingForSubscriber
}

// newTestManager builds a manager whose reports are discarded
// ([DiscardReports]).
func newTestManager(cfg Config) *Manager {
	m := NewManager(cfg, nil, nil)
	m.SetReportSender(DiscardReports)
	return m
}
