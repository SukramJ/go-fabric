// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import "github.com/SukramJ/go-fabric/im"

// sendReportChunks is [Bridge.sendReportChunksNotify] without the final
// wait, for the tests that drive a report's chunks themselves. It has no
// caller outside the tests (review II, R2-P6-doc-0515).
func (b *Bridge) sendReportChunks(target subTarget, report im.ReportData) ([]uint32, error) {
	counters, _, err := b.sendReportChunksNotify(target, report, false)
	return counters, err
}

// sendInitiatedReport is [Bridge.sendInitiatedReportArmed] without the
// final wait, for the tests that send a report on a fresh exchange
// (review II, R2-P6-doc-0515).
func (b *Bridge) sendInitiatedReport(target subTarget, report im.ReportData) (counters []uint32, exchangeID uint16, err error) {
	counters, exchangeID, _, err = b.sendInitiatedReportArmed(target, report, false)
	return counters, exchangeID, err
}
