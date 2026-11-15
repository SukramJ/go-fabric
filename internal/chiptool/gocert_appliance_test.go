// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/modeselect"
	"github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/schema"
)

// The appliance cases: matter.js's TS files whose DUT is a ModeSelect,
// Thermostat, WindowCovering or DoorLock client, read mirrored onto the
// reference daemon's servers. Their line citations were read at the
// snapshot's pin, db021da0 (not at goCertReadAt).
//
// Each TS step sends one command through tc-support.ts sendCommand
// (:1802-1815): the invoke must succeed, a response that carries a Status
// field must carry 0 (invokeWithResponseChecks :1610-1661), and the TH's
// log must show the request's payload (expectCommandPayload). The port
// holds the first two; the third is the TH device's own log of what it
// received, which the mirrored direction has no equivalent of — the Go TH
// encodes the payload from the generated request types itself.
//
// The TS files put the cluster on endpoint 1, where both of their THs
// serve it; the port runs on the first daemon endpoint that serves it
// (goCertCase.at).

func init() {
	for _, c := range []func() goCertCase{caseMOD22, caseTSTAT32, caseWNCV61, caseWNCV71} {
		registerGoCert(c)
	}
}

// commandSucceeds invokes a command and, where the response carries a
// Status field, holds it to 0 — invokeWithResponseChecks
// (tc-support.ts:1640-1659), which finds the field by name; statusTag is
// its context tag in the generated response type, -1 for a response
// without one. A command whose model is timed goes as a timed invoke with
// a 2 s timeout (TC-DRLK-3.2.test.ts:59, :69).
func commandSucceeds(th *goTH, n *client.Node, cluster, cmd uint32, fields spec.Encodable, statusTag int) (client.Result, error) {
	var opts client.InvokeOptions
	if schema.IsTimedInvoke(cluster, cmd) {
		opts = client.InvokeOptions{Timed: true, TimedTimeout: 2 * time.Second}
	}
	r, err := th.invoke(n, cmdPath(th.ep, cluster, cmd), fields, false, opts)
	if err != nil {
		return r, err
	}
	if statusTag >= 0 {
		st, ok := structField(r.Fields, uint8(statusTag)) //nolint:gosec // a context tag
		if !ok || st.El.IsNull || st.El.Uint != 0 {
			return r, fmt.Errorf("command 0x%04X/0x%02X answered %s: response status %s", cluster, cmd, describeNode(r.Fields), describeNode(st))
		}
	}
	return r, nil
}

// caseMOD22 mirrors TC-MOD-2.2: the TH reads SupportedModes and changes
// to one of them.
func caseMOD22() goCertCase {
	var (
		n     *client.Node
		modes []uint64
	)
	return goCertCase{
		tc: "TC-MOD-2.2", line: 47, pics: []string{"MOD.C.C00.Tx"}, at: "MOD.S",
		steps: []goStep{
			{n: "0", line: 48, title: "the DUT commissions the TH", run: func(th *goTH) error {
				var err error
				_, n, err = th.commission("dut")
				return err
			}},
			{n: "1", line: 51, title: "DUT reads the SupportedModes attribute from the TH", pics: "MOD.C.A0002", run: func(th *goTH) error {
				v, err := th.readValue(n, attrPath(th.ep, modeselect.ClusterID, modeselect.AttrSupportedModes))
				if err != nil {
					return err
				}
				// modeNumbers (:32-45): a non-empty list whose every entry has
				// a numeric Mode (ModeOptionStruct field 1).
				if v.El.IsNull || !v.El.IsContainer || len(v.Children) == 0 {
					return fmt.Errorf("SupportedModes is %s, not a non-empty list", describeNode(v))
				}
				modes = nil
				for _, e := range v.Children {
					m, ok := structField(e, 1)
					if !ok || m.El.IsNull || !isUnsigned(m) {
						return fmt.Errorf("SupportedModes entry %s has no numeric Mode", describeNode(e))
					}
					modes = append(modes, m.El.Uint)
				}
				th.note("TH supports modes %v", modes)
				return nil
			}},
			{n: "2", line: 91, title: "DUT sends a ChangeToMode command with a mode from step 1", pics: "MOD.C.C00.Tx", run: func(th *goTH) error {
				if len(modes) == 0 {
					return errors.New("step 1 recorded no supported modes")
				}
				// Any listed mode, one that is not 0 where there is one (:99-100).
				newMode := modes[0]
				for _, m := range modes {
					if m != 0 {
						newMode = m
						break
					}
				}
				if _, err := commandSucceeds(th, n, modeselect.ClusterID, modeselect.CmdChangeToMode,
					modeselect.ChangeToModeRequest{NewMode: uint8(newMode)}, -1); err != nil { //nolint:gosec // a uint8 Mode
					return err
				}
				th.note("ChangeToMode NewMode=%d: Success", newMode)
				return nil
			}},
		},
	}
}

// caseTSTAT32 mirrors TC-TSTAT-3.2: SetpointRaiseLower up and down by
// 1 °C in Heat mode.
func caseTSTAT32() goCertCase {
	var n *client.Node
	// MODE_HEAT and AMOUNT (:17-21).
	const (
		modeHeat = thermostat.SetpointRaiseLowerModeHeat
		amount   = 10
	)
	raiseLower := func(a int8) func(th *goTH) error {
		return func(th *goTH) error {
			if _, err := commandSucceeds(th, n, thermostat.ClusterID, thermostat.CmdSetpointRaiseLower,
				thermostat.SetpointRaiseLowerRequest{Mode: modeHeat, Amount: a}, -1); err != nil {
				return err
			}
			th.note("SetpointRaiseLower Mode=%d Amount=%d: Success", modeHeat, a)
			return nil
		}
	}
	return goCertCase{
		tc: "TC-TSTAT-3.2", line: 49, pics: []string{"TSTAT.C"}, at: "TSTAT.S",
		steps: []goStep{
			{n: "0", line: 50, title: "commission DUT to TH", run: func(th *goTH) error {
				var err error
				_, n, err = th.commission("dut")
				return err
			}},
			{n: "1", line: 53, title: "DUT raises the setpoint with SetpointRaiseLower", pics: "TSTAT.C.C00.Tx", run: raiseLower(amount)},
			{n: "2", line: 66, title: "DUT lowers the setpoint with SetpointRaiseLower", pics: "TSTAT.C.C00.Tx", run: raiseLower(-amount)},
		},
	}
}

// wncvPreconditions mirrors tc-wncv-support.ts windowCoveringPreconditions
// (:24-63): the DUT commissions the TH and reads its WindowCovering
// FeatureMap, and unmet names a precondition its features do not meet.
func wncvPreconditions(n **client.Node, unmet func(th *goTH, fm uint64) string) func(th *goTH) error {
	return func(th *goTH) error {
		var err error
		if _, *n, err = th.commission("dut"); err != nil {
			return err
		}
		v, err := th.readValue(*n, attrPath(th.ep, windowcovering.ClusterID, 0xFFFC))
		if err != nil {
			return err
		}
		if v.El.IsNull || !isUnsigned(v) {
			return fmt.Errorf("FeatureMap is %s, which names no features", describeNode(v))
		}
		if why := unmet(th, v.El.Uint); why != "" {
			return fmt.Errorf("FeatureMap=0x%X: %s", v.El.Uint, why)
		}
		th.note("FeatureMap=0x%X", v.El.Uint)
		return nil
	}
}

// wncvSends is a step that sends cmd with fields and expects Success.
func wncvSends(n **client.Node, name string, cmd uint32, fields spec.Encodable) func(th *goTH) error {
	return func(th *goTH) error {
		if _, err := commandSucceeds(th, *n, windowcovering.ClusterID, cmd, fields, -1); err != nil {
			return err
		}
		th.note("%s: Success", name)
		return nil
	}
}

func hasFeatures(fm uint64, f ...windowcovering.Feature) bool {
	for _, b := range f {
		if fm&uint64(b) == 0 {
			return false
		}
	}
	return true
}

// caseWNCV61 mirrors TC-WNCV-6.1: UpOrOpen, DownOrClose and StopMotion.
func caseWNCV61() goCertCase {
	var n *client.Node
	return goCertCase{
		tc: "TC-WNCV-6.1", line: 25, pics: []string{"WNCV.C"}, at: "WNCV.S",
		steps: []goStep{
			{
				n: "0", line: 26, title: "the DUT commissions the TH, which supports Lift and/or Tilt",
				run: wncvPreconditions(&n, func(_ *goTH, fm uint64) string {
					// :29-31.
					if hasFeatures(fm, windowcovering.FeatureLift) || hasFeatures(fm, windowcovering.FeatureTilt) {
						return ""
					}
					return "the TH supports neither Lift nor Tilt"
				}),
			},
			// sendsCommand (:14-23): no parameters.
			{n: "1a", line: 38, title: "DUT sends UpOrOpen command to TH", pics: "WNCV.C.C00.Tx", run: wncvSends(&n, "UpOrOpen", windowcovering.CmdUpOrOpen, windowcovering.UpOrOpenRequest{})},
			{n: "2a", line: 42, title: "DUT sends DownOrClose command to TH", pics: "WNCV.C.C01.Tx", run: wncvSends(&n, "DownOrClose", windowcovering.CmdDownOrClose, windowcovering.DownOrCloseRequest{})},
			{n: "3a", line: 46, title: "DUT sends a StopMotion command to TH", pics: "WNCV.C.C02.Tx", run: wncvSends(&n, "StopMotion", windowcovering.CmdStopMotion, windowcovering.StopMotionRequest{})},
		},
	}
}

// caseWNCV71 mirrors TC-WNCV-7.1: GoToLiftPercentage and
// GoToTiltPercentage to 50 %.
//
// Step 0's TS requires both position-aware axes (:43-47), stricter than the
// plan's "and/or" because the TS's THs are fixed and the plan defines no
// outcome for the other axis's command (:41-42). Read mirrored, a step
// whose axis the daemon does not serve is gated by its mirrored PICS
// (WNCV.S.C05.Rsp, WNCV.S.C08.Rsp) and recorded skipped, so the outcome is
// defined for every step that runs: the port holds each axis whose
// GoTo…Percentage command the daemon declares to its position-aware
// features, and at least one axis to be there.
func caseWNCV71() goCertCase {
	var n *client.Node
	const halfOpen = 5000 // HALF_OPEN (:11-12)
	return goCertCase{
		tc: "TC-WNCV-7.1", line: 37, pics: []string{"WNCV.C"}, at: "WNCV.S",
		steps: []goStep{
			{
				n: "0", line: 38, title: "the DUT commissions the TH, which supports position-aware Lift and position-aware Tilt",
				run: wncvPreconditions(&n, func(th *goTH, fm uint64) string {
					var unmet []string
					axes := 0
					for _, a := range []struct {
						name, pics string
						f          []windowcovering.Feature
					}{
						{"position-aware Lift", "WNCV.S.C05.Rsp", []windowcovering.Feature{windowcovering.FeatureLift, windowcovering.FeaturePositionAwareLift}},
						{"position-aware Tilt", "WNCV.S.C08.Rsp", []windowcovering.Feature{windowcovering.FeatureTilt, windowcovering.FeaturePositionAwareTilt}},
					} {
						declared, err := th.picsHolds(a.pics, th.ep)
						if err != nil {
							return err.Error()
						}
						if !declared {
							continue
						}
						axes++
						if !hasFeatures(fm, a.f...) {
							unmet = append(unmet, fmt.Sprintf("%s is declared but the TH lacks %s", a.pics, a.name))
						}
					}
					if axes == 0 {
						unmet = append(unmet, "the TH lacks position-aware Lift or position-aware Tilt")
					}
					return strings.Join(unmet, "; ")
				}),
			},
			{
				n: "1a", line: 54, title: "DUT sends GoToLiftPercentage command with 50% to TH", pics: "WNCV.C.C05.Tx",
				run: wncvSends(&n, "GoToLiftPercentage 5000", windowcovering.CmdGoToLiftPercentage, windowcovering.GoToLiftPercentageRequest{LiftPercent100thsValue: halfOpen}),
			},
			{
				n: "2a", line: 65, title: "DUT sends GoToTiltPercentage command with 50% to TH", pics: "WNCV.C.C08.Tx",
				run: wncvSends(&n, "GoToTiltPercentage 5000", windowcovering.CmdGoToTiltPercentage, windowcovering.GoToTiltPercentageRequest{TiltPercent100thsValue: halfOpen}),
			},
		},
	}
}
