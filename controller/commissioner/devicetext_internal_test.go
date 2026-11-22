// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"strings"
	"testing"

	gencomm "thingfabric.dev/go-fabric/cluster/spec/generalcommissioning"
)

// TestDeviceTextIsNeutralised: a device's DebugText reaches the error a
// commissioning returns without its control characters and cut at 128
// bytes, at every site that puts it into a message — the
// GeneralCommissioning and NOC responses and the NetworkCommissioning
// suffix. Plain printable text keeps the message shape the parity tests
// pin (review II, R2-P6-sec-1861).
func TestDeviceTextIsNeutralised(t *testing.T) {
	t.Parallel()
	f, _ := newTestFlow(t, func(imRequest) (uint8, []byte, bool) { return 0, nil, false })
	hostile := "line\nbreak\x1b[31mred" + strings.Repeat("x", 300)
	idx := uint8(1)
	for name, msg := range map[string]string{
		"GeneralCommissioning":   f.ensureGeneralCommissioningSuccess("armFailSafe", gencomm.CommissioningErrorBusyWithOtherAdmin, hostile).Error(),
		"OperationalCredentials": f.ensureOperationalCredentialsSuccess("addNoc", nocResponse{status: 3, fabricIndex: &idx, debugText: &hostile}).Error(),
		"NetworkCommissioning":   "x" + debugSuffix(", ", &hostile),
	} {
		if strings.ContainsAny(msg, "\n\x1b") {
			t.Errorf("%s: raw control characters in %q", name, msg)
		}
		if strings.Contains(msg, strings.Repeat("x", 129)) {
			t.Errorf("%s: the device text is not cut: %d bytes", name, len(msg))
		}
	}
	if got := deviceText("too far"); got != "too far" {
		t.Errorf("plain text: %q", got)
	}
}
