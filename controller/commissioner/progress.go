// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

// StepPhase is where a commissioning step stands in a [Step] report.
type StepPhase uint8

// The phases of a step. A step is reported Started, then exactly one of
// Succeeded, Failed or Skipped — matter.js's CommissioningStepResultCode
// Success / Failure / Skipped (packages/protocol/src/peer/
// ControllerCommissioningFlow.ts:164-169 at 862ad75f); its Stop, which
// ends the flow early (:427-429), is reported Succeeded.
const (
	StepStarted StepPhase = iota + 1
	StepSucceeded
	StepFailed
	StepSkipped
)

// String is the phase's name: started, succeeded, failed, skipped.
func (p StepPhase) String() string {
	switch p {
	case StepStarted:
		return "started"
	case StepSucceeded:
		return "succeeded"
	case StepFailed:
		return "failed"
	case StepSkipped:
		return "skipped"
	}
	return "unknown"
}

// Step is one report of commissioning progress ([Options].Progress).
//
// Number, Sub and Name are matter.js's CommissioningStep stepNumber,
// subStepNumber and name verbatim (ControllerCommissioningFlow.ts:184-199,
// the list :536-656 at 862ad75f) — "GetInitialData" 0.1,
// "GeneralCommissioning.ArmFailsafe" 7.1, …, "Reconnect" 18.1 (which
// includes the operational CASE connection, step 19, :629),
// "GeneralCommissioning.Complete" 20.1,
// "OperationalCredentials.UpdateFabricLabel" 98.1 — the steps
// executeCommissioning runs and logs one by one (:384). matter.js names no
// step for what runs before the flow; the two reports of it are this
// module's own: [StepDiscovery] 2.1 (the controller's search for the
// device over DNS-SD or BLE) and [StepPASE] 2.2 (the PASE session over
// the commissioning channel), numbered after the "step 2 (device
// discovery and commissioning channel establishment)" executeCommissioning
// names (:444-446). Reports arrive in execution order, so 2.x precedes 0.1.
// matter.js names no step for what runs after the flow either; the
// module reports the node's start as [StepSubscribe] 21.1, its own
// number after the flow's 20.1 (the initial read and the default
// subscription, packages/node/src/behavior/system/commissioning/
// CommissioningClient.ts:343-361 at 862ad75f), reported after the
// flow's last step — 98.1 here, as this port does not run matter.js's
// 99.1 AdditionalLogic.AddDefaultOtaProvider. It has no Skipped case:
// every successful commissioning starts the node, and a failed step is
// the last report (:438-462).
//
// Err is the error of a Failed step: the step's own, or that of the
// failsafe bookkeeping after it (:397-424). A recoverable failure
// (RecoverableCommissioningError, :432-437) is reported Failed and the
// flow goes on; a step whose result is Failure without an error — 98.1
// without a fabric index (:1480-1485) — is Failed with a nil Err.
type Step struct {
	Number uint8
	Sub    uint8
	Name   string
	Phase  StepPhase
	Err    error
}

// The names of the two steps before the flow and the one after it, this
// module's (see [Step]).
const (
	StepDiscovery = "Discovery"
	StepPASE      = "PASE"
	StepSubscribe = "Subscribe"
)

// report calls the progress hook, if any.
func (o *Options) report(number, sub uint8, name string, phase StepPhase, err error) {
	if o.Progress != nil {
		o.Progress(Step{Number: number, Sub: sub, Name: name, Phase: phase, Err: err})
	}
}

// ReportSubscribe reports the [StepSubscribe] step through progress, a nil
// progress reporting nothing. The node starts after
// [Commissioner.Commission] returned, so its caller reports it.
func ReportSubscribe(progress func(Step), phase StepPhase, err error) {
	if progress != nil {
		progress(Step{Number: 21, Sub: 1, Name: StepSubscribe, Phase: phase, Err: err})
	}
}

// ReportDiscovery reports the [StepDiscovery] step through progress, a nil
// progress reporting nothing. The search for the device runs before
// [Commissioner.Commission], so its caller reports it.
func ReportDiscovery(progress func(Step), phase StepPhase, err error) {
	if progress != nil {
		progress(Step{Number: 2, Sub: 1, Name: StepDiscovery, Phase: phase, Err: err})
	}
}
