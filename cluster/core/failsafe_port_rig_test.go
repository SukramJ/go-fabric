// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

// armedFailSafe is a test rig's fail-safe port
// ([core.OpcredsConfig.FailSafe]): a window that is armed, for fabric — 0
// for one armed over PASE, the accessing fabric of a request without a
// fabric. A server without a port answers FailsafeRequired (review II,
// W2-sec-06), so a rig that drives CSRRequest, AddTrustedRootCertificate,
// AddNOC or UpdateNOC names the window it runs in.
func armedFailSafe(fabric uint8) func() (uint8, bool) {
	return func() (uint8, bool) { return fabric, true }
}
