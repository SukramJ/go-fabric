// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package light_test

import (
	"testing"
	"testing/synctest"
	"time"

	ccdef "thingfabric.dev/go-fabric/cluster/spec/colorcontrol"
)

// TestImmediateMoveReportsTheCommandsValue (R2-C2-fsm-1401): an immediate
// MoveToColorTemperature that replaces a running transition puts its own
// value on the wire at the command's instant, not the stopped
// transition's last step followed by the command's value a second later.
func TestImmediateMoveReportsTheCommandsValue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, r, _ := managedCT(true)
		if err := ctInvoke(t, srv, ccdef.CmdMoveToColorTemperature, ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 370, TransitionTime: 600}); err != nil {
			t.Fatal(err)
		}
		ctSleep(1550 * time.Millisecond)
		r.reset()
		if err := ctInvoke(t, srv, ccdef.CmdMoveToColorTemperature, ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 200}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if mireds, _ := r.snapshot(); len(mireds) == 0 || mireds[0] != 200 {
			t.Errorf("ColorTemperatureMireds reports at the command %v, want the first to be 200", mireds)
		}
	})
}
