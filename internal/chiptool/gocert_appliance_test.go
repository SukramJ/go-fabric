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
	"github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/cluster/spec/modeselect"
	"github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/schema"
)

// The appliance cases: matter.js's TS files whose DUT is a ModeSelect,
// Thermostat, WindowCovering or DoorLock client, read mirrored onto the
// reference daemon's servers. Their line citations were read at
// [goCertReadAt], the snapshot's pin b25ed960 — TC-DRLK-3.2's at its own
// readAt, db021da0.
//
// Each TS step sends one command through tc-support.ts sendCommand
// (:1825-1838): the invoke must succeed, a response that carries a Status
// field must carry 0 (invokeWithResponseChecks :1633-1684), and the TH's
// log must show the request's payload (expectCommandPayload). The port
// holds the first two; the third is the TH device's own log of what it
// received, which the mirrored direction has no equivalent of — the Go TH
// encodes the payload from the generated request types itself.
//
// The TS files put the cluster on endpoint 1, where both of their THs
// serve it; the port runs on the first daemon endpoint that serves it
// (goCertCase.at).

func init() {
	for _, c := range []func() goCertCase{caseMOD22, caseTSTAT32, caseWNCV61, caseWNCV71, caseDRLK32} {
		registerGoCert(c)
	}
}

// commandSucceeds invokes a command and, where the response carries a
// Status field, holds it to 0 — invokeWithResponseChecks
// (tc-support.ts:1663-1682), which finds the field by name; statusTag is
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

// caseDRLK32 mirrors TC-DRLK-3.2: the door, schedule, user and credential
// commands, each answered with success.
func caseDRLK32() goCertCase {
	var n *client.Node
	// USER_INDEX, SECOND_USER_INDEX, SCHEDULE_INDEX, LOCAL_START_TIME,
	// LOCAL_END_TIME, PIN, PIN_CREDENTIAL (:21-37).
	const (
		userIndex       = 1
		secondUserIndex = 2
		scheduleIndex   = 1
		localStartTime  = 1_893_456_000
		localEndTime    = 1_893_542_400
	)
	pin := []byte("123456")
	pinCredential := doorlock.CredentialStruct{CredentialType: doorlock.CredentialTypePin, CredentialIndex: 1}
	// addUser (:40-50).
	addUser := func(idx uint16) doorlock.SetUserRequest {
		return doorlock.SetUserRequest{
			OperationType:  doorlock.DataOperationTypeAdd,
			UserIndex:      idx,
			UserName:       spec.ValueOf("xxx"),
			UserUniqueId:   spec.ValueOf(uint32(6452) + uint32(idx)),
			UserStatus:     spec.ValueOf(doorlock.UserStatusOccupiedEnabled),
			UserType:       spec.ValueOf(doorlock.UserTypeUnrestrictedUser),
			CredentialRule: spec.ValueOf(doorlock.CredentialRuleSingle),
		}
	}
	// sendsLockCommand (:58-74); statusTag is the Status field's tag in
	// the command's response (definition_gen.go's response types), -1
	// for a status-only answer or a response without one.
	sends := func(name string, cmd uint32, fields spec.Encodable, statusTag int) func(th *goTH) error {
		return func(th *goTH) error {
			r, err := commandSucceeds(th, n, doorlock.ClusterID, cmd, fields, statusTag)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if r.HasFields {
				th.note("%s: Success, response %s", name, describeNode(r.Fields))
			} else {
				th.note("%s: Success", name)
			}
			return nil
		}
	}
	return goCertCase{
		tc: "TC-DRLK-3.2", line: 80, pics: []string{"DRLK.C"}, at: "DRLK.S",
		// At b25ed960 steps 7-9 gate on DRLK.C.F0a and steps 10-12 on DRLK.C.F0b
		// instead of F04 (TC-DRLK-3.2.test.ts:126, :136-171), which the port does
		// not mirror yet; the daemon declares F04, F0a and F0b 0, so they skip either way.
		readAt: "db021da0",
		steps: []goStep{
			{n: "0", line: 81, title: "commission TH to DUT", run: func(th *goTH) error {
				var err error
				_, n, err = th.commission("dut")
				return err
			}},
			{
				n: "0b", line: 84, title: "Precondition: DUT adds a user to the TH", pics: "DRLK.C.F08 & DRLK.C.C1a.Tx",
				run: sends("SetUser", doorlock.CmdSetUser, addUser(userIndex), -1),
			},
			{n: "1", line: 88, title: "DUT sends LockDoor command to TH", pics: "DRLK.C.C00.Tx", run: sends("LockDoor", doorlock.CmdLockDoor, doorlock.LockDoorRequest{}, -1)},
			{n: "2", line: 92, title: "DUT sends UnlockDoor command to TH", pics: "DRLK.C.C01.Tx", run: sends("UnlockDoor", doorlock.CmdUnlockDoor, doorlock.UnlockDoorRequest{}, -1)},
			{
				n: "3", line: 96, title: "DUT sends UnlockWithTimeout command to TH", pics: "DRLK.C.C03.Tx",
				run: sends("UnlockWithTimeout", doorlock.CmdUnlockWithTimeout, doorlock.UnlockWithTimeoutRequest{Timeout: 60}, -1),
			},
			{
				n: "4", line: 100, title: "DUT sends SetWeekDaySchedule command to TH", pics: "DRLK.C.F04 & DRLK.C.C0b.Tx",
				run: sends("SetWeekDaySchedule", doorlock.CmdSetWeekDaySchedule, doorlock.SetWeekDayScheduleRequest{
					WeekDayIndex: scheduleIndex, UserIndex: userIndex, DaysMask: doorlock.DaysMaskMonday,
					StartHour: 15, StartMinute: 45, EndHour: 16, EndMinute: 55,
				}, -1),
			},
			{
				n: "5", line: 114, title: "DUT sends GetWeekDaySchedule command to TH", pics: "DRLK.C.F04 & DRLK.C.C0c.Tx",
				run: sends("GetWeekDaySchedule", doorlock.CmdGetWeekDaySchedule,
					doorlock.GetWeekDayScheduleRequest{WeekDayIndex: scheduleIndex, UserIndex: userIndex}, 2),
			},
			{
				n: "6", line: 120, title: "DUT sends ClearWeekDaySchedule command to TH", pics: "DRLK.C.F04 & DRLK.C.C0d.Tx",
				run: sends("ClearWeekDaySchedule", doorlock.CmdClearWeekDaySchedule,
					doorlock.ClearWeekDayScheduleRequest{WeekDayIndex: scheduleIndex, UserIndex: userIndex}, -1),
			},
			{
				n: "7", line: 126, title: "DUT sends SetYearDaySchedule command to TH", pics: "DRLK.C.F04 & DRLK.C.C0e.Tx",
				run: sends("SetYearDaySchedule", doorlock.CmdSetYearDaySchedule, doorlock.SetYearDayScheduleRequest{
					YearDayIndex: scheduleIndex, UserIndex: userIndex, LocalStartTime: localStartTime, LocalEndTime: localEndTime,
				}, -1),
			},
			{
				n: "8", line: 137, title: "DUT sends GetYearDaySchedule command to TH", pics: "DRLK.C.F04 & DRLK.C.C0f.Tx",
				run: sends("GetYearDaySchedule", doorlock.CmdGetYearDaySchedule,
					doorlock.GetYearDayScheduleRequest{YearDayIndex: scheduleIndex, UserIndex: userIndex}, 2),
			},
			{
				n: "9", line: 143, title: "DUT sends ClearYearDaySchedule command to TH", pics: "DRLK.C.F04 & DRLK.C.C10.Tx",
				run: sends("ClearYearDaySchedule", doorlock.CmdClearYearDaySchedule,
					doorlock.ClearYearDayScheduleRequest{YearDayIndex: scheduleIndex, UserIndex: userIndex}, -1),
			},
			{
				n: "10", line: 149, title: "DUT sends Set Holiday Day Schedule command to TH", pics: "DRLK.C.F04 & DRLK.C.C11.Tx",
				run: sends("SetHolidaySchedule", doorlock.CmdSetHolidaySchedule, doorlock.SetHolidayScheduleRequest{
					HolidayIndex: scheduleIndex, LocalStartTime: localStartTime, LocalEndTime: localEndTime,
					OperatingMode: doorlock.OperatingModeNormal,
				}, -1),
			},
			{
				n: "11", line: 160, title: "DUT sends Get Holiday Day Schedule command to TH", pics: "DRLK.C.F04 & DRLK.C.C12.Tx",
				run: sends("GetHolidaySchedule", doorlock.CmdGetHolidaySchedule, doorlock.GetHolidayScheduleRequest{HolidayIndex: scheduleIndex}, 1),
			},
			{
				n: "12", line: 166, title: "DUT sends Clear Holiday Day Schedule command to TH", pics: "DRLK.C.F04 & DRLK.C.C13.Tx",
				run: sends("ClearHolidaySchedule", doorlock.CmdClearHolidaySchedule, doorlock.ClearHolidayScheduleRequest{HolidayIndex: scheduleIndex}, -1),
			},
			{
				n: "13", line: 172, title: "DUT sends SetUser command to TH", pics: "DRLK.C.F08 & DRLK.C.C1a.Tx",
				run: sends("SetUser", doorlock.CmdSetUser, addUser(secondUserIndex), -1),
			},
			{
				n: "14", line: 176, title: "DUT sends GetUser to TH", pics: "DRLK.C.F08 & DRLK.C.C1b.Tx",
				run: sends("GetUser", doorlock.CmdGetUser, doorlock.GetUserRequest{UserIndex: secondUserIndex}, -1),
			},
			{
				n: "15", line: 180, title: "DUT sends ClearUser command to TH", pics: "DRLK.C.F08 & DRLK.C.C1d.Tx",
				run: sends("ClearUser", doorlock.CmdClearUser, doorlock.ClearUserRequest{UserIndex: secondUserIndex}, -1),
			},
			{
				// A credential added to an existing user carries a null
				// UserStatus and UserType (:187).
				n: "16", line: 184, title: "DUT sends SetCredential command to TH", pics: "DRLK.C.F08 & DRLK.C.C22.Tx",
				run: sends("SetCredential", doorlock.CmdSetCredential, doorlock.SetCredentialRequest{
					OperationType: doorlock.DataOperationTypeAdd, Credential: pinCredential, CredentialData: pin,
					UserIndex: spec.ValueOf(uint16(userIndex)), UserStatus: spec.NullOf[doorlock.UserStatusEnum](), UserType: spec.NullOf[doorlock.UserTypeEnum](),
				}, 0),
			},
			{
				n: "17", line: 198, title: "DUT sends GetCredentialStatus to TH", pics: "DRLK.C.F08 & DRLK.C.C24.Tx",
				run: sends("GetCredentialStatus", doorlock.CmdGetCredentialStatus, doorlock.GetCredentialStatusRequest{Credential: pinCredential}, -1),
			},
			{
				n: "18", line: 204, title: "DUT sends ClearCredential command to TH", pics: "DRLK.C.F08 & DRLK.C.C26.Tx",
				run: sends("ClearCredential", doorlock.CmdClearCredential, doorlock.ClearCredentialRequest{Credential: spec.ValueOf(pinCredential)}, -1),
			},
		},
	}
}
