// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package closurecontrol is the generated definition of the Matter
// ClosureControl cluster (definition_gen.go, server_gen.go) and the
// hand-written capability ports of its Mover (ports.go).
package closurecontrol

import "context"

// The optional interfaces of a [Mover] that unlock the features whose
// commands and fields need more than a position: MotionLatching (LT),
// Speed (SP) and Calibration (CL). Instantaneous (IS) needs none — it
// withdraws Stop, MovementCompleted and CountdownTime, a fact of the
// declaration's Features. The kit's server (cluster/closure) refuses a
// declared feature whose interface the Mover lacks, naming it.
//
// Hand-written beside the generated server_gen.go: script/clustergen owns
// that file, these are the kit server's capability ports
// (connectedhomeip ClosureControlClusterDelegate.h HandleMoveToCommand's
// latch and speed, HandleCalibrateCommand, at 6170af84).

// Latcher is an optional interface of a [Mover] for MotionLatching (LT):
// LatchControlModes reports which of latching and unlatching a
// controller may request (the fixed attribute, read once when the server
// is built), Latch drives the latch after the server's checks — true
// latches, false unlatches. In a MoveTo that also moves, the server
// unlatches before the motion and latches after it is accepted, as
// connectedhomeip's closure-app orders a panel's actions. An error
// answers FAILURE and leaves the target state.
type Latcher interface {
	LatchControlModes() LatchControlModesBitmap
	Latch(ctx context.Context, latch bool) error
}

// SpeedMover is an optional interface of a [Mover] for Speed (SP):
// SetSpeed applies the speed of a MoveTo after the server's checks,
// before the motion it accompanies. An error answers FAILURE and leaves
// the target state.
type SpeedMover interface {
	SetSpeed(ctx context.Context, speed ThreeLevelAutoEnum) error
}

// Calibrator is an optional interface of a [Mover] for Calibration (CL):
// Calibrate starts the calibration after the server's checks; on success
// the server sets MainState Calibrating, and the drive reports Stopped
// when it is done. An error answers FAILURE and leaves MainState.
type Calibrator interface {
	Calibrate(ctx context.Context) error
}
