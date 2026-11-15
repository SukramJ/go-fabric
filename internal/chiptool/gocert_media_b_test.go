// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/keypadinput"
	"github.com/SukramJ/go-fabric/cluster/spec/lowpower"
	"github.com/SukramJ/go-fabric/cluster/spec/mediainput"
	"github.com/SukramJ/go-fabric/cluster/spec/mediaplayback"
	gocontroller "github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
)

// The media client cases of KEYPADINPUT, LOWPOWER, MEDIAINPUT,
// MEDIAPLAYBACK and TGTNAV: matter.js's TS files whose DUT sends one
// command per step to the `tv` TH's video player, read mirrored onto the
// reference daemon's --media video player (examples/reference-bridge/
// media.go), which every daemon of the leg carries (TestGoCertificationCases
// requires the flag, bridge_test.go startBridgeWith adds it). The TS line
// citations here were read at the snapshot's pin, db021da0, not at
// [goCertReadAt].
//
// Every step is sendsTvCommand (tc-tv-client-support.ts:47-57) or, for
// LOWPOWER, sendCommand (tc-support.ts:1802-1815): the DUT invokes the
// command with the TS's arguments, the invoke must succeed and a response
// that carries a Status must carry 0 (invokeWithResponseChecks
// tc-support.ts:1610-1662), and the TH's log must show the request with
// exactly that payload (expectCommandPayload :1698-1738). Read mirrored,
// the Go TH sends the same command and holds the same response checks;
// the TH-log payload check becomes the daemon's: for a command whose
// payload lands in an attribute, the attribute read back carries the
// payload's value as chip's tv-app — the TS's TH — stores it
// (connectedhomeip 6170af84 examples/tv-app/tv-common/clusters/…, cited
// per step); for Sleep, the daemon's log line; for a command without
// fields there is no payload beyond the command itself, which the Go
// controller sends alone in its request.

func init() {
	for _, c := range []func() goCertCase{
		caseKEYPADINPUT31,
		caseLOWPOWER22,
		caseMEDIAINPUT314, caseMEDIAINPUT315, caseMEDIAINPUT316, caseMEDIAINPUT317,
		caseMEDIAPLAYBACK65, caseMEDIAPLAYBACK67, caseMEDIAPLAYBACK610,
	} {
		registerGoCert(c)
	}
}

// mediaBRun is a media client case's commissioned DUT: commissionThStep
// (tc-dd-support.ts:169-181) and the CommissionedRefs it fills.
type mediaBRun struct {
	ctrl *gocontroller.Controller
	n    *client.Node
}

// commission is step 0, "Test setup: commission TH to DUT."
func (r *mediaBRun) commission(th *goTH) error {
	var err error
	r.ctrl, r.n, err = th.commission("dut")
	if err == nil {
		th.note("commissioned the daemon")
	}
	return err
}

// finalize is commissioned.decommissionAll, every case's .finalize.
func (r *mediaBRun) finalize(th *goTH) {
	if r.n != nil {
		_ = r.ctrl.Registry().Decommission(th.ctx, r.n)
	}
}

// mediaBPlayer is the daemon's video player — the `tv` TH's
// TV_VIDEO_PLAYER_ENDPOINT (tc-tv-client-support.ts:13-18), the endpoint
// advertising dtCastingVideoPlayer in the daemon's topology block — and
// holds the step's PICS-selected endpoint to it, so a step gated on one
// endpoint never sends to another.
func mediaBPlayer(th *goTH) (uint16, error) {
	for _, m := range reTopologyLine.FindAllStringSubmatch(th.dut.snapshotStdout(), -1) {
		for _, dt := range strings.Split(m[2], ",") {
			v, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(dt), "0x"), 16, 32)
			if err != nil || v != dtCastingVideoPlayer {
				continue
			}
			ep, _ := strconv.Atoi(m[1])
			if uint16(ep) != th.ep { //nolint:gosec // an endpoint number
				return 0, fmt.Errorf("the step's PICS select endpoint %d, the daemon's video player is endpoint %d", th.ep, ep)
			}
			return th.ep, nil
		}
	}
	return 0, fmt.Errorf("no endpoint of the daemon advertises the casting video player (0x%04X)", dtCastingVideoPlayer)
}

// mediaBSend is a sendsTvCommand step's run: the command with fields to the
// video player, the response's status field held to 0 where the response
// carries one (statusField), then check — the payload's mirror — if any.
func (r *mediaBRun) mediaBSend(cluster, cmd uint32, name string, fields spec.Encodable, statusField bool,
	check func(th *goTH, ep uint16) error,
) func(th *goTH) error {
	return func(th *goTH) error {
		if r.n == nil {
			return errors.New("step 0 did not commission the daemon")
		}
		ep, err := mediaBPlayer(th)
		if err != nil {
			return err
		}
		if _, err := th.invoke(r.n, cmdPath(ep, cluster, cmd), fields, statusField, client.InvokeOptions{}); err != nil {
			return err
		}
		if statusField {
			th.note("%s on endpoint %d: Success, response status 0", name, ep)
		} else {
			th.note("%s on endpoint %d: Success", name, ep)
		}
		if check != nil {
			return check(th, ep)
		}
		return nil
	}
}

// mediaBReadU8 reads a uint8 attribute of the video player.
func (r *mediaBRun) mediaBReadU8(th *goTH, ep uint16, cluster, attr uint32) (uint64, error) {
	v, err := th.readValue(r.n, attrPath(ep, cluster, attr))
	if err != nil {
		return 0, err
	}
	if v.El.IsNull || v.El.IsContainer {
		return 0, fmt.Errorf("attribute 0x%04X/0x%04X reads %s, not a number", cluster, attr, describeNode(v))
	}
	return v.El.Uint, nil
}

// mediaBTrackID reads a MediaPlayback track attribute's Id.
func (r *mediaBRun) mediaBTrackID(th *goTH, ep uint16, attr uint32) (string, error) {
	v, err := th.readValue(r.n, attrPath(ep, mediaplayback.ClusterID, attr))
	if err != nil {
		return "", err
	}
	if v.El.IsNull {
		return "", fmt.Errorf("MediaPlayback 0x%04X reads null", attr)
	}
	var tr mediaplayback.TrackStruct
	if err := tr.DecodeTLV(v); err != nil {
		return "", err
	}
	return tr.Id, nil
}

// mediaBPosition reads SampledPosition's position (null reads as an error)
// and Duration.
func (r *mediaBRun) mediaBPosition(th *goTH, ep uint16) (pos, duration uint64, err error) {
	v, err := th.readValue(r.n, attrPath(ep, mediaplayback.ClusterID, mediaplayback.AttrSampledPosition))
	if err != nil {
		return 0, 0, err
	}
	var p mediaplayback.PlaybackPositionStruct
	if v.El.IsNull {
		return 0, 0, errors.New("SampledPosition reads null")
	}
	if err := p.DecodeTLV(v); err != nil {
		return 0, 0, err
	}
	if p.Position.Null {
		return 0, 0, errors.New("SampledPosition's position reads null")
	}
	d, err := th.readValue(r.n, attrPath(ep, mediaplayback.ClusterID, mediaplayback.AttrDuration))
	if err != nil {
		return 0, 0, err
	}
	if d.El.IsNull {
		return 0, 0, errors.New("Duration reads null")
	}
	return p.Position.Value, d.El.Uint, nil
}

// caseKEYPADINPUT31 mirrors TC-KEYPADINPUT-3.1: SendKey Up.
func caseKEYPADINPUT31() goCertCase {
	r := &mediaBRun{}
	const keyCodeUp = 1 // KEY_CODE_UP (:16)
	return goCertCase{
		tc: "TC-KEYPADINPUT-3.1", line: 20, pics: []string{"KEYPADINPUT.C"}, at: "KEYPADINPUT.S", finalize: r.finalize,
		steps: []goStep{
			{n: "0", line: 21, title: "Test setup: commission TH to DUT.", run: r.commission},
			// The key has no attribute to land in; chip's tv-app answers
			// UnsupportedKey for a key it does not handle (KeypadInputManager.cpp
			// :26-98, the daemon's tvKeys), so the response status 0 is the
			// daemon's evidence that a navigation key arrived.
			{
				n: "1", line: 24, pics: "KEYPADINPUT.C.C00.Tx", title: "DUT sends SendKey command to TH",
				run: r.mediaBSend(keypadinput.ClusterID, keypadinput.CmdSendKey, "SendKey Up",
					keypadinput.SendKeyRequest{KeyCode: keyCodeUp}, true, nil),
			},
		},
	}
}

// caseLOWPOWER22 mirrors TC-LOWPOWER-2.2: Sleep. chip's all-clusters app
// puts LowPower on endpoint 1 (:14-15); the daemon serves it on the video
// player, as chip's tv-app does (tv-app.matter:4506 at 6170af84, inside
// endpoint 1), so the case runs there.
func caseLOWPOWER22() goCertCase {
	r := &mediaBRun{}
	return goCertCase{
		tc: "TC-LOWPOWER-2.2", line: 19, pics: []string{"LOWPOWER.C"}, at: "LOWPOWER.S", finalize: r.finalize,
		steps: []goStep{
			{n: "0", line: 27, title: "Preconditions: the DUT commissions the TH.", run: r.commission},
			{n: "1", line: 30, pics: "LOWPOWER.C.C00.Tx", title: "DUT sends Sleep command to TH", run: func(th *goTH) error {
				if r.n == nil {
					return errors.New("step 0 did not commission the daemon")
				}
				// The TS's TH-log check (:38, "Sleep carries no parameters")
				// mirrored onto the daemon's log: its LowPower delegate logs
				// the Sleep it handled (examples/reference-bridge/media.go
				// tvSleep, after chip's LowPowerManager.cpp:21-26).
				offset := len(th.dut.snapshotStderr())
				if _, err := th.invoke(r.n, cmdPath(th.ep, lowpower.ClusterID, lowpower.CmdSleep), lowpower.SleepRequest{}, false, client.InvokeOptions{}); err != nil {
					return err
				}
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if strings.Contains(th.dut.snapshotStderr()[offset:], "msg=media.lowpower.sleep") {
						th.note("Sleep on endpoint %d: Success; the daemon logged media.lowpower.sleep", th.ep)
						return nil
					}
					time.Sleep(20 * time.Millisecond)
				}
				return fmt.Errorf("Sleep on endpoint %d succeeded, but the daemon logged no media.lowpower.sleep", th.ep)
			}},
		},
	}
}

// caseMEDIAINPUT314 mirrors TC-MEDIAINPUT-3.14: SelectInput 2.
func caseMEDIAINPUT314() goCertCase {
	r := &mediaBRun{}
	const index = 2 // INDEX (:16)
	return goCertCase{
		tc: "TC-MEDIAINPUT-3.14", line: 20, pics: []string{"MEDIAINPUT.C"}, at: "MEDIAINPUT.S", finalize: r.finalize,
		steps: []goStep{
			{n: "0", line: 21, title: "Test setup: commission TH to DUT.", run: r.commission},
			{
				n: "1", line: 24, pics: "MEDIAINPUT.C.C00.Tx", title: "DUT sends SelectInput command to TH",
				run: r.mediaBSend(mediainput.ClusterID, mediainput.CmdSelectInput, "SelectInput 2",
					mediainput.SelectInputRequest{Index: index}, false, func(th *goTH, ep uint16) error {
						// The payload: chip's HandleSelectInput makes the index
						// current (MediaInputManager.cpp:57-71).
						got, err := r.mediaBReadU8(th, ep, mediainput.ClusterID, mediainput.AttrCurrentInput)
						if err != nil {
							return err
						}
						if got != index {
							return fmt.Errorf("CurrentInput reads %d after SelectInput %d", got, index)
						}
						th.note("CurrentInput %d", got)
						return nil
					}),
			},
		},
	}
}

// caseMEDIAINPUT315 mirrors TC-MEDIAINPUT-3.15: ShowInputStatus.
func caseMEDIAINPUT315() goCertCase {
	r := &mediaBRun{}
	return goCertCase{
		tc: "TC-MEDIAINPUT-3.15", line: 17, pics: []string{"MEDIAINPUT.C"}, at: "MEDIAINPUT.S", finalize: r.finalize,
		steps: []goStep{
			{n: "0", line: 18, title: "Test setup: commission TH to DUT.", run: r.commission},
			{
				n: "1", line: 21, pics: "MEDIAINPUT.C.C01.Tx", title: "DUT sends ShowInputStatus command to TH",
				run: r.mediaBSend(mediainput.ClusterID, mediainput.CmdShowInputStatus, "ShowInputStatus",
					mediainput.ShowInputStatusRequest{}, false, nil),
			},
		},
	}
}

// caseMEDIAINPUT316 mirrors TC-MEDIAINPUT-3.16: HideInputStatus.
func caseMEDIAINPUT316() goCertCase {
	r := &mediaBRun{}
	return goCertCase{
		tc: "TC-MEDIAINPUT-3.16", line: 17, pics: []string{"MEDIAINPUT.C"}, at: "MEDIAINPUT.S", finalize: r.finalize,
		steps: []goStep{
			{n: "0", line: 18, title: "Test setup: commission TH to DUT.", run: r.commission},
			{
				n: "1", line: 21, pics: "MEDIAINPUT.C.C02.Tx", title: "DUT sends HideInputStatus command to TH",
				run: r.mediaBSend(mediainput.ClusterID, mediainput.CmdHideInputStatus, "HideInputStatus",
					mediainput.HideInputStatusRequest{}, false, nil),
			},
		},
	}
}

// caseMEDIAINPUT317 mirrors TC-MEDIAINPUT-3.17: RenameInput 1 "HDMI 1".
func caseMEDIAINPUT317() goCertCase {
	r := &mediaBRun{}
	const (
		index = 1        // INDEX (:16)
		name  = "HDMI 1" // NAME (:17)
	)
	return goCertCase{
		tc: "TC-MEDIAINPUT-3.17", line: 21, pics: []string{"MEDIAINPUT.C"}, at: "MEDIAINPUT.S", finalize: r.finalize,
		steps: []goStep{
			{n: "0", line: 22, title: "Test setup: commission TH to DUT.", run: r.commission},
			{
				n: "1", line: 25, pics: "MEDIAINPUT.C.C03.Tx", title: "DUT sends RenameInput command to TH",
				run: r.mediaBSend(mediainput.ClusterID, mediainput.CmdRenameInput, "RenameInput 1 \"HDMI 1\"",
					mediainput.RenameInputRequest{Index: index, Name: name}, false, func(th *goTH, ep uint16) error {
						// The payload: chip's HandleRenameInput stores the name
						// on the input of that index (MediaInputManager.cpp:93-113).
						v, err := th.readValue(r.n, attrPath(ep, mediainput.ClusterID, mediainput.AttrInputList))
						if err != nil {
							return err
						}
						for _, c := range v.Children {
							var in mediainput.InputInfoStruct
							if err := in.DecodeTLV(c); err != nil {
								return err
							}
							if in.Index != index {
								continue
							}
							if in.Name != name {
								return fmt.Errorf("InputList's input %d is named %q after RenameInput %q", index, in.Name, name)
							}
							th.note("InputList's input %d is named %q", index, in.Name)
							return nil
						}
						return fmt.Errorf("InputList lists no input %d: %s", index, describeNode(v))
					}),
			},
		},
	}
}

// mediaBPlayback is a MediaPlayback command step whose request has no
// fields and whose PlaybackResponse carries a Status.
func (r *mediaBRun) mediaBPlayback(cmd uint32, name string) func(th *goTH) error {
	return r.mediaBSend(mediaplayback.ClusterID, cmd, name, emptyFields, true, nil)
}

// caseMEDIAPLAYBACK65 mirrors TC-MEDIAPLAYBACK-6.5: Play, Pause, Stop.
func caseMEDIAPLAYBACK65() goCertCase {
	r := &mediaBRun{}
	return goCertCase{
		tc: "TC-MEDIAPLAYBACK-6.5", line: 17, pics: []string{"MEDIAPLAYBACK.C"}, at: "MEDIAPLAYBACK.S", finalize: r.finalize,
		steps: []goStep{
			{n: "0", line: 18, title: "Test setup: commission TH to DUT.", run: r.commission},
			{n: "1", line: 21, pics: "MEDIAPLAYBACK.C.C00.Tx", title: "DUT sends Play command to TH", run: r.mediaBPlayback(mediaplayback.CmdPlay, "Play")},
			{n: "2", line: 25, pics: "MEDIAPLAYBACK.C.C01.Tx", title: "DUT sends Pause command to TH", run: r.mediaBPlayback(mediaplayback.CmdPause, "Pause")},
			{n: "3", line: 29, pics: "MEDIAPLAYBACK.C.C02.Tx", title: "DUT sends Stop command to TH", run: r.mediaBPlayback(mediaplayback.CmdStop, "Stop")},
		},
	}
}

// caseMEDIAPLAYBACK67 mirrors TC-MEDIAPLAYBACK-6.7: Play, StartOver, Next,
// Previous, SkipForward and SkipBackward 10 s.
func caseMEDIAPLAYBACK67() goCertCase {
	r := &mediaBRun{}
	const delta = 10_000 // DELTA_POSITION_MS (:16)
	var before uint64
	// skip reads the position before the command (pre) and holds the one
	// after it to want(before, duration) — the payload's delta as chip's
	// tv-app applies it.
	skip := func(cmd uint32, name string, fields spec.Encodable, want func(pos, duration uint64) uint64, cite string) func(th *goTH) error {
		send := r.mediaBSend(mediaplayback.ClusterID, cmd, name, fields, true, func(th *goTH, ep uint16) error {
			pos, duration, err := r.mediaBPosition(th, ep)
			if err != nil {
				return err
			}
			if w := want(before, duration); pos != w {
				return fmt.Errorf("SampledPosition %d -> %d after %s, want %d (%s, Duration %d)", before, pos, name, w, cite, duration)
			}
			th.note("SampledPosition %d -> %d", before, pos)
			return nil
		})
		return func(th *goTH) error {
			if r.n == nil {
				return errors.New("step 0 did not commission the daemon")
			}
			ep, err := mediaBPlayer(th)
			if err != nil {
				return err
			}
			if before, _, err = r.mediaBPosition(th, ep); err != nil {
				return err
			}
			return send(th)
		}
	}
	return goCertCase{
		tc: "TC-MEDIAPLAYBACK-6.7", line: 20, pics: []string{"MEDIAPLAYBACK.C"}, at: "MEDIAPLAYBACK.S", finalize: r.finalize,
		steps: []goStep{
			{n: "0", line: 21, title: "Test setup: commission TH to DUT.", run: r.commission},
			{n: "1", line: 24, pics: "MEDIAPLAYBACK.C.C00.Tx", title: "DUT sends Play command to TH", run: r.mediaBPlayback(mediaplayback.CmdPlay, "Play")},
			{n: "2", line: 28, pics: "MEDIAPLAYBACK.C.C03.Tx", title: "DUT sends StartOver command to TH", run: r.mediaBPlayback(mediaplayback.CmdStartOver, "StartOver")},
			{n: "3", line: 32, pics: "MEDIAPLAYBACK.C.C05.Tx", title: "DUT sends Next command to TH", run: r.mediaBPlayback(mediaplayback.CmdNext, "Next")},
			{n: "4", line: 36, pics: "MEDIAPLAYBACK.C.C04.Tx", title: "DUT sends Previous command to TH", run: r.mediaBPlayback(mediaplayback.CmdPrevious, "Previous")},
			{
				n: "5", line: 40, pics: "MEDIAPLAYBACK.C.C08.Tx", title: "DUT sends SkipForward command to TH",
				run: skip(mediaplayback.CmdSkipForward, "SkipForward 10000", mediaplayback.SkipForwardRequest{DeltaPositionMilliseconds: delta},
					func(pos, duration uint64) uint64 { return min(pos+delta, duration) },
					"HandleSkipForward, MediaPlaybackManager.cpp:245-257"),
			},
			{
				n: "6", line: 46, pics: "MEDIAPLAYBACK.C.C09.Tx", title: "DUT sends SkipBackward command to TH",
				run: skip(mediaplayback.CmdSkipBackward, "SkipBackward 10000", mediaplayback.SkipBackwardRequest{DeltaPositionMilliseconds: delta},
					func(pos, _ uint64) uint64 {
						if pos > delta {
							return pos - delta
						}
						return 0
					},
					"HandleSkipBackward, MediaPlaybackManager.cpp:230-243"),
			},
		},
	}
}

// caseMEDIAPLAYBACK610 mirrors TC-MEDIAPLAYBACK-6.10: ActivateAudioTrack,
// ActivateTextTrack, DeactivateTextTrack.
func caseMEDIAPLAYBACK610() goCertCase {
	r := &mediaBRun{}
	const (
		audioTrackID     = "activeAudioTrackId_1" // AUDIO_TRACK_ID (:19)
		audioOutputIndex = 1                      // AUDIO_OUTPUT_INDEX (:20)
		textTrackID      = "activeTextTrackId_1"  // TEXT_TRACK_ID (:21)
	)
	// active holds a track attribute's Id to the one the command named:
	// chip's HandleActivateAudioTrack / HandleActivateTextTrack make the
	// listed track of that id active (MediaPlaybackManager.cpp:305-333).
	active := func(attr uint32, what, want string) func(th *goTH, ep uint16) error {
		return func(th *goTH, ep uint16) error {
			got, err := r.mediaBTrackID(th, ep, attr)
			if err != nil {
				return err
			}
			if got != want {
				return fmt.Errorf("%s reads track %q after activating %q", what, got, want)
			}
			th.note("%s %q", what, got)
			return nil
		}
	}
	return goCertCase{
		tc: "TC-MEDIAPLAYBACK-6.10", line: 25, pics: []string{"MEDIAPLAYBACK.C"}, at: "MEDIAPLAYBACK.S", finalize: r.finalize,
		steps: []goStep{
			{n: "0", line: 26, title: "Test setup: commission TH to DUT.", run: r.commission},
			{
				n: "1", line: 29, pics: "MEDIAPLAYBACK.C.C0c.Tx", title: "DUT sends ActivateAudioTrack command to TH",
				run: r.mediaBSend(mediaplayback.ClusterID, mediaplayback.CmdActivateAudioTrack, "ActivateAudioTrack "+audioTrackID,
					mediaplayback.ActivateAudioTrackRequest{TrackId: audioTrackID, AudioOutputIndex: &spec.Nullable[uint8]{Value: audioOutputIndex}},
					false, active(mediaplayback.AttrActiveAudioTrack, "ActiveAudioTrack", audioTrackID)),
			},
			{
				n: "2", line: 38, pics: "MEDIAPLAYBACK.C.C0d.Tx", title: "DUT sends ActivateTextTrack command to TH",
				run: r.mediaBSend(mediaplayback.ClusterID, mediaplayback.CmdActivateTextTrack, "ActivateTextTrack "+textTrackID,
					mediaplayback.ActivateTextTrackRequest{TrackId: textTrackID},
					false, active(mediaplayback.AttrActiveTextTrack, "ActiveTextTrack", textTrackID)),
			},
			{
				n: "3", line: 44, pics: "MEDIAPLAYBACK.C.C0e.Tx", title: "DUT sends DeactivateTextTrack command to TH",
				run: r.mediaBSend(mediaplayback.ClusterID, mediaplayback.CmdDeactivateTextTrack, "DeactivateTextTrack",
					mediaplayback.DeactivateTextTrackRequest{}, false, nil),
			},
		},
	}
}
