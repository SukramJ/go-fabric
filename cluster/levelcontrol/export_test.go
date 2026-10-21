// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol

import lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"

// WireOf converts a generated request a LevelSource receives to the
// cluster/wire struct the tests compare against; any other value is
// returned as it is.
func WireOf(req any) any {
	switch r := req.(type) {
	case lvldef.MoveToLevelRequest:
		return wireMoveToLevel(r)
	case lvldef.MoveToLevelWithOnOffRequest:
		return wireMoveToLevel(lvldef.MoveToLevelRequest(r))
	case lvldef.MoveRequest:
		return wireMove(r)
	case lvldef.MoveWithOnOffRequest:
		return wireMove(lvldef.MoveRequest(r))
	case lvldef.StepRequest:
		return wireStep(r)
	case lvldef.StepWithOnOffRequest:
		return wireStep(lvldef.StepRequest(r))
	case lvldef.StopRequest:
		return wireStop(r)
	case lvldef.StopWithOnOffRequest:
		return wireStop(lvldef.StopRequest(r))
	default:
		return req
	}
}
