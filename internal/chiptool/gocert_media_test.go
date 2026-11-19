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
	logindef "github.com/SukramJ/go-fabric/cluster/spec/accountlogin"
	launcherdef "github.com/SukramJ/go-fabric/cluster/spec/applicationlauncher"
	outputdef "github.com/SukramJ/go-fabric/cluster/spec/audiooutput"
	channeldef "github.com/SukramJ/go-fabric/cluster/spec/channel"
	launchdef "github.com/SukramJ/go-fabric/cluster/spec/contentlauncher"
	"github.com/SukramJ/go-fabric/cluster/spec/keypadinput"
	"github.com/SukramJ/go-fabric/cluster/spec/lowpower"
	"github.com/SukramJ/go-fabric/cluster/spec/mediainput"
	"github.com/SukramJ/go-fabric/cluster/spec/mediaplayback"
	"github.com/SukramJ/go-fabric/cluster/spec/targetnavigator"
	gocontroller "github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
)

// The media client cases (review II, phase 4, owner decision 68):
// matter.js's TS files whose DUT is a media cluster client sending to its
// `tv` TH, read mirrored onto the daemon's --media pair
// (examples/reference-bridge/media.go), which every daemon of the leg
// carries (TestGoCertificationCases requires the flag, bridge_test.go
// startBridgeWith adds it). The TS line citations in this file were read
// at [goCertReadAt], the snapshot's pin b25ed960 (parity/schema.json
// matter.sourceCommit).
//
// Every step is sendsTvCommand (tc-tv-client-support.ts:47-57) or, for
// LOWPOWER, sendCommand (tc-support.ts:1825-1838): the DUT invokes the
// command with the TS's arguments, the invoke must succeed and a response
// that carries a Status must carry 0 (invokeWithResponseChecks
// tc-support.ts:1633-1685), and the TH's log must show the request with
// exactly that payload (expectCommandPayload :1721-1761). Read mirrored,
// the TH is the daemon, whose log the Go TH does not read for a payload:
// the Go TH sends the same command and holds the same response checks,
// and the TH-log payload check becomes the daemon's — for a command whose
// payload lands in an attribute, the attribute read back carries the
// payload's value as chip's tv-app — the TS's TH — stores it
// (connectedhomeip 6170af84 examples/tv-app/tv-common/clusters/…, cited
// per step) or as the TS file names its effect (AudioOutput, Channel); for
// Sleep, the daemon's log line; for a command without fields there is no
// payload beyond the command itself, which the Go controller sends alone
// in its request. The read-back goes beyond the TS, which reads no
// attribute: it holds the daemon's media model to chip's tv-app, and the
// SkipForward/SkipBackward equality holds only for a model whose position
// does not advance on its own.

func init() {
	for _, c := range []func() goCertCase{
		caseALOGIN122, caseAPPLAUNCHER371, caseAPPLAUNCHER381, caseAPPLAUNCHER391,
		caseAUDIOOUTPUT73, caseAUDIOOUTPUT74, caseCHANNEL54, caseCHANNEL55, caseCHANNEL56,
		caseCONTENTLAUNCHER104, caseCONTENTLAUNCHER106,
		caseKEYPADINPUT31,
		caseLOWPOWER22,
		caseMEDIAINPUT314, caseMEDIAINPUT315, caseMEDIAINPUT316, caseMEDIAINPUT317,
		caseMEDIAPLAYBACK65, caseMEDIAPLAYBACK67, caseMEDIAPLAYBACK610,
		caseTGTNAV82,
	} {
		registerGoCert(c)
	}
}

// --- The media case -----------------------------------------------------------

// mediaCommand is one command a step sends: statusField holds a
// response's Status (tag 0) to 0 (tc-support.ts :1670-1682).
type mediaCommand struct {
	cluster, cmd uint32
	name         string
	fields       spec.Encodable
	statusField  bool
	opts         client.InvokeOptions
}

// mediaStep is one step of a media client case. Its run is send followed
// by check (the payload's mirror; nil where the TS names none), or run
// where the step does more than that.
type mediaStep struct {
	n     string
	line  int
	title string
	// pics is the TS step's client PICS; need the server PICS the mirrored
	// command needs beyond it (the command's feature).
	pics, need string
	send       mediaCommand
	check      func(th *goTH, n *client.Node, ep uint16) error
	run        func(th *goTH, n *client.Node, ep uint16) error
}

// mediaCase is a media client case: step "0" commissions the daemon
// (tc-dd-support.ts commissionThStep :169-182, one line after certTest),
// each further step sends its command to the endpoint advertising
// deviceType — the TS's TV_VIDEO_PLAYER_ENDPOINT or TV_CONTENT_APP_ENDPOINT
// (tc-tv-client-support.ts:18, :24), chip's tv-app endpoints 1 and 3, which
// the daemon serves as the casting video player and the content app
// (families_table_test.go dtCastingVideoPlayer, dtContentApp) — and the
// finalizer removes the fabric (commissioned.decommissionAll).
func mediaCase(tc string, line int, pics, at string, deviceType uint32, steps ...mediaStep) goCertCase {
	var (
		ctrl *gocontroller.Controller
		node *client.Node
	)
	c := goCertCase{
		tc: tc, line: line, pics: []string{pics}, at: at,
		finalize: func(th *goTH) {
			if node != nil && ctrl != nil {
				_ = ctrl.Registry().Decommission(th.ctx, node)
			}
		},
		steps: []goStep{{n: "0", line: line + 1, title: "Test setup: commission TH to DUT.", run: func(th *goTH) error {
			var err error
			ctrl, node, err = th.commission("dut")
			if err == nil {
				th.note("commissioned the daemon")
			}
			return err
		}}},
	}
	for i := range steps {
		s := &steps[i]
		c.steps = append(c.steps, goStep{n: s.n, line: s.line, title: s.title, pics: s.pics, need: s.need, run: func(th *goTH) error {
			if node == nil {
				return errors.New("step 0 did not commission the daemon")
			}
			ep, err := th.mediaEndpoint(deviceType)
			if err != nil {
				return err
			}
			if s.run != nil {
				return s.run(th, node, ep)
			}
			if err := th.mediaInvoke(node, ep, s.send); err != nil {
				return err
			}
			if s.check != nil {
				return s.check(th, node, ep)
			}
			return nil
		}})
	}
	return c
}

// mediaEndpoint is the daemon's first endpoint advertising deviceType,
// held to the endpoint the running step's PICS were evaluated on: a gate
// read off another endpoint would gate the step on a device it does not
// address.
func (th *goTH) mediaEndpoint(deviceType uint32) (uint16, error) {
	ep := th.dut.endpointFor(th.t, deviceType)
	if ep != th.ep {
		return 0, fmt.Errorf("the step's PICS were evaluated on endpoint %d, the device type 0x%04X is endpoint %d", th.ep, deviceType, ep)
	}
	return ep, nil
}

// mediaInvoke sends one command and notes its answer.
func (th *goTH) mediaInvoke(n *client.Node, ep uint16, c mediaCommand) error {
	r, err := th.invoke(n, cmdPath(ep, c.cluster, c.cmd), c.fields, c.statusField, c.opts)
	if err != nil {
		return err
	}
	if r.HasFields {
		th.note("%s on endpoint %d: Success, response %s", c.name, ep, describeNode(r.Fields))
	} else {
		th.note("%s on endpoint %d: Success", c.name, ep)
	}
	return nil
}

// mediaReadU8 reads a uint8 attribute.
func (th *goTH) mediaReadU8(n *client.Node, ep uint16, cluster, attr uint32) (uint64, error) {
	v, err := th.readValue(n, attrPath(ep, cluster, attr))
	if err != nil {
		return 0, err
	}
	if v.El.IsNull || v.El.IsContainer {
		return 0, fmt.Errorf("attribute 0x%04X/0x%04X reads %s, not a number", cluster, attr, describeNode(v))
	}
	return v.El.Uint, nil
}

// --- AccountLogin ----------------------------------------------------------

// mediaLoginTimed is the content app target's options: every
// AccountLogin command is timed, with a 2 s timeout
// (TC-ALOGIN-12.2.test.ts:17-21).
var mediaLoginTimed = client.InvokeOptions{Timed: true, TimedTimeout: 2 * time.Second}

// mediaTempAccountID is TEMP_ACCOUNT_IDENTIFIER (TC-ALOGIN-12.2.test.ts:24).
const mediaTempAccountID = "TempAccountId123"

// caseALOGIN122 mirrors TC-ALOGIN-12.2: GetSetupPIN, Login and Logout,
// timed, to the content app.
func caseALOGIN122() goCertCase {
	return mediaCase("TC-ALOGIN-12.2", 28, "ALOGIN.C", "ALOGIN.S", dtContentApp,
		mediaStep{n: "1", line: 32, title: "DUT sends GetSetupPIN command to TH", send: mediaCommand{
			cluster: logindef.ClusterID, cmd: logindef.CmdGetSetupPin, name: "GetSetupPIN",
			fields: logindef.GetSetupPinRequest{TempAccountIdentifier: mediaTempAccountID}, opts: mediaLoginTimed,
		}},
		// The Setup PIN is CHIP_TV_SETUP_PIN (TC-ALOGIN-12.2.test.ts:51,
		// TestAccountLoginServer.ts:12), the PIN chip's tv-app answers and
		// accepts on its content app endpoint (AccountLoginManager.h:35).
		mediaStep{n: "2", line: 44, title: "DUT sends Login command to TH", send: mediaCommand{
			cluster: logindef.ClusterID, cmd: logindef.CmdLogin, name: "Login",
			fields: logindef.LoginRequest{TempAccountIdentifier: mediaTempAccountID, SetupPin: "tempPin123"}, opts: mediaLoginTimed,
		}},
		mediaStep{n: "3", line: 56, title: "DUT sends Logout command to TH", send: mediaCommand{
			cluster: logindef.ClusterID, cmd: logindef.CmdLogout, name: "Logout",
			fields: logindef.LogoutRequest{}, opts: mediaLoginTimed,
		}},
	)
}

// --- ApplicationLauncher ---------------------------------------------------

// mediaKnownApplication is TV_KNOWN_APPLICATION
// (tc-tv-client-support.ts:32): catalog vendor 123, application
// "exampleid".
func mediaKnownApplication() *launcherdef.ApplicationStruct {
	return &launcherdef.ApplicationStruct{CatalogVendorId: 123, ApplicationId: "exampleid"}
}

// mediaLauncherCase is the three ApplicationLauncher cases: one command
// with TV_KNOWN_APPLICATION to the video player.
func mediaLauncherCase(tc string, cmd uint32, name string, fields spec.Encodable, pics string) goCertCase {
	return mediaCase(tc, 17, "APPLAUNCHER.C", "APPLAUNCHER.S", dtCastingVideoPlayer,
		mediaStep{n: "1", line: 21, title: "DUT sends " + name + " command to TH", pics: pics, send: mediaCommand{
			cluster: launcherdef.ClusterID, cmd: cmd, name: name, fields: fields, statusField: true,
		}},
	)
}

// caseAPPLAUNCHER371 mirrors TC-APPLAUNCHER-3.7.1: LaunchApp (:24-25).
func caseAPPLAUNCHER371() goCertCase {
	return mediaLauncherCase("TC-APPLAUNCHER-3.7.1", launcherdef.CmdLaunchApp, "LaunchApp",
		launcherdef.LaunchAppRequest{Application: mediaKnownApplication()}, "APPLAUNCHER.C.C00.Tx")
}

// caseAPPLAUNCHER381 mirrors TC-APPLAUNCHER-3.8.1: StopApp (:24-25).
func caseAPPLAUNCHER381() goCertCase {
	return mediaLauncherCase("TC-APPLAUNCHER-3.8.1", launcherdef.CmdStopApp, "StopApp",
		launcherdef.StopAppRequest{Application: mediaKnownApplication()}, "APPLAUNCHER.C.C01.Tx")
}

// caseAPPLAUNCHER391 mirrors TC-APPLAUNCHER-3.9.1: HideApp (:24-25).
func caseAPPLAUNCHER391() goCertCase {
	return mediaLauncherCase("TC-APPLAUNCHER-3.9.1", launcherdef.CmdHideApp, "HideApp",
		launcherdef.HideAppRequest{Application: mediaKnownApplication()}, "APPLAUNCHER.C.C02.Tx")
}

// --- AudioOutput -----------------------------------------------------------

// caseAUDIOOUTPUT73 mirrors TC-AUDIOOUTPUT-7.3: SelectOutput to output 2,
// "An output both THs seed, and not the one they start on" (:15-16).
func caseAUDIOOUTPUT73() goCertCase {
	const index = 2 // INDEX (:16)
	return mediaCase("TC-AUDIOOUTPUT-7.3", 20, "AUDIOOUTPUT.C", "AUDIOOUTPUT.S", dtCastingVideoPlayer,
		mediaStep{
			n: "1", line: 24, title: "DUT sends SelectOutput command to TH", pics: "AUDIOOUTPUT.C.C00.Tx",
			send: mediaCommand{
				cluster: outputdef.ClusterID, cmd: outputdef.CmdSelectOutput, name: "SelectOutput",
				fields: outputdef.SelectOutputRequest{Index: index},
			},
			check: func(th *goTH, n *client.Node, ep uint16) error {
				v, err := th.readValue(n, attrPath(ep, outputdef.ClusterID, outputdef.AttrCurrentOutput))
				if err != nil {
					return err
				}
				if !isUnsigned(v) || v.El.Uint != index {
					return fmt.Errorf("CurrentOutput reads %s after SelectOutput %d", describeNode(v), index)
				}
				th.note("CurrentOutput %d", index)
				return nil
			},
		},
	)
}

// caseAUDIOOUTPUT74 mirrors TC-AUDIOOUTPUT-7.4: RenameOutput of output 1
// to "Kitchen Speakers" (:15-17), which needs the NameUpdates feature.
func caseAUDIOOUTPUT74() goCertCase {
	const (
		index = 1                  // INDEX (:16)
		name  = "Kitchen Speakers" // NAME (:17)
	)
	return mediaCase("TC-AUDIOOUTPUT-7.4", 21, "AUDIOOUTPUT.C", "AUDIOOUTPUT.S", dtCastingVideoPlayer,
		mediaStep{
			n: "1", line: 25, title: "DUT sends RenameOutput command to TH", pics: "AUDIOOUTPUT.C.C01.Tx", need: "AUDIOOUTPUT.S.F00",
			send: mediaCommand{
				cluster: outputdef.ClusterID, cmd: outputdef.CmdRenameOutput, name: "RenameOutput",
				fields: outputdef.RenameOutputRequest{Index: index, Name: name},
			},
			check: func(th *goTH, n *client.Node, ep uint16) error {
				v, err := th.readValue(n, attrPath(ep, outputdef.ClusterID, outputdef.AttrOutputList))
				if err != nil {
					return err
				}
				for _, c := range v.Children {
					var o outputdef.OutputInfoStruct
					if err := o.DecodeTLV(c); err != nil {
						return err
					}
					if o.Index == index {
						if o.Name != name {
							return fmt.Errorf("OutputList entry %d is named %q after RenameOutput %q", index, o.Name, name)
						}
						th.note("OutputList entry %d named %q", index, name)
						return nil
					}
				}
				return fmt.Errorf("OutputList %s has no entry %d", describeNode(v), index)
			},
		},
	)
}

// --- Channel -----------------------------------------------------------------

// mediaCurrentChannel reads Channel.CurrentChannel and holds it to want.
func (th *goTH) mediaCurrentChannel(n *client.Node, ep, wantMajor, wantMinor uint16, after string) error {
	v, err := th.readValue(n, attrPath(ep, channeldef.ClusterID, channeldef.AttrCurrentChannel))
	if err != nil {
		return err
	}
	if v.El.IsNull {
		return fmt.Errorf("CurrentChannel reads null after %s", after)
	}
	var c channeldef.ChannelInfoStruct
	if err := c.DecodeTLV(v); err != nil {
		return err
	}
	if c.MajorNumber != wantMajor || c.MinorNumber != wantMinor {
		return fmt.Errorf("CurrentChannel is %d.%d after %s, want %d.%d", c.MajorNumber, c.MinorNumber, after, wantMajor, wantMinor)
	}
	th.note("CurrentChannel %d.%d", wantMajor, wantMinor)
	return nil
}

// caseCHANNEL54 mirrors TC-CHANNEL-5.4: ChangeChannel matching "PBS", "The
// name of channel 9.1 on both THs" and no other key of a seeded channel
// (:15-19).
func caseCHANNEL54() goCertCase {
	return mediaCase("TC-CHANNEL-5.4", 23, "CHANNEL.C", "CHANNEL.S", dtCastingVideoPlayer,
		mediaStep{
			n: "1", line: 27, title: "DUT sends ChangeChannel command to TH", pics: "CHANNEL.C.C00.Tx",
			send: mediaCommand{
				cluster: channeldef.ClusterID, cmd: channeldef.CmdChangeChannel, name: "ChangeChannel",
				fields: channeldef.ChangeChannelRequest{Match: "PBS"}, statusField: true,
			},
			check: func(th *goTH, n *client.Node, ep uint16) error {
				return th.mediaCurrentChannel(n, ep, 9, 1, `ChangeChannel "PBS"`)
			},
		},
	)
}

// caseCHANNEL55 mirrors TC-CHANNEL-5.5: ChangeChannelByNumber to 9.2,
// "seeded on both THs and not the one they start on" (:15-20).
func caseCHANNEL55() goCertCase {
	return mediaCase("TC-CHANNEL-5.5", 24, "CHANNEL.C", "CHANNEL.S", dtCastingVideoPlayer,
		mediaStep{
			n: "1", line: 28, title: "DUT sends ChangeChannelByNumber command to TH", pics: "CHANNEL.C.C02.Tx",
			send: mediaCommand{
				cluster: channeldef.ClusterID, cmd: channeldef.CmdChangeChannelByNumber, name: "ChangeChannelByNumber",
				fields: channeldef.ChangeChannelByNumberRequest{MajorNumber: 9, MinorNumber: 2},
			},
			check: func(th *goTH, n *client.Node, ep uint16) error {
				return th.mediaCurrentChannel(n, ep, 9, 2, "ChangeChannelByNumber 9.2")
			},
		},
	)
}

// caseCHANNEL56 mirrors TC-CHANNEL-5.6: SkipChannel by -1, "both THs wrap
// to the last channel" (:15-16) — the last entry of ChannelList.
func caseCHANNEL56() goCertCase {
	return mediaCase("TC-CHANNEL-5.6", 20, "CHANNEL.C", "CHANNEL.S", dtCastingVideoPlayer,
		mediaStep{
			n: "1", line: 24, title: "DUT sends SkipChannel command to TH", pics: "CHANNEL.C.C03.Tx",
			send: mediaCommand{
				cluster: channeldef.ClusterID, cmd: channeldef.CmdSkipChannel, name: "SkipChannel",
				fields: channeldef.SkipChannelRequest{Count: -1},
			},
			check: func(th *goTH, n *client.Node, ep uint16) error {
				v, err := th.readValue(n, attrPath(ep, channeldef.ClusterID, channeldef.AttrChannelList))
				if err != nil {
					return err
				}
				if len(v.Children) == 0 {
					return errors.New("ChannelList reads empty")
				}
				var last channeldef.ChannelInfoStruct
				if err := last.DecodeTLV(v.Children[len(v.Children)-1]); err != nil {
					return err
				}
				return th.mediaCurrentChannel(n, ep, last.MajorNumber, last.MinorNumber, "SkipChannel -1")
			},
		},
	)
}

// --- ContentLauncher -------------------------------------------------------

// caseCONTENTLAUNCHER104 mirrors TC-CONTENTLAUNCHER-10.4: LaunchContent
// searching ParameterEnum Channel "PBS" with AutoPlay TRUE (:15-20, :31),
// which needs the ContentSearch feature.
func caseCONTENTLAUNCHER104() goCertCase {
	return mediaCase("TC-CONTENTLAUNCHER-10.4", 24, "CONTENTLAUNCHER.C", "CONTENTLAUNCHER.S", dtCastingVideoPlayer,
		mediaStep{
			n: "1", line: 28, title: "DUT sends LaunchContent command to TH", pics: "CONTENTLAUNCHER.C.C00.Tx", need: "CONTENTLAUNCHER.S.F00",
			send: mediaCommand{
				cluster: launchdef.ClusterID, cmd: launchdef.CmdLaunchContent, name: "LaunchContent", statusField: true,
				fields: launchdef.LaunchContentRequest{
					Search:   launchdef.ContentSearchStruct{ParameterList: []launchdef.ParameterStruct{{Type: launchdef.ParameterChannel, Value: "PBS"}}},
					AutoPlay: true,
				},
			},
		},
	)
}

// caseCONTENTLAUNCHER106 mirrors TC-CONTENTLAUNCHER-10.6: LaunchURL with
// an https URL, a DisplayString and a BrandingInformation (:15-23), which
// needs the URLPlayback feature.
func caseCONTENTLAUNCHER106() goCertCase {
	return mediaCase("TC-CONTENTLAUNCHER-10.6", 27, "CONTENTLAUNCHER.C", "CONTENTLAUNCHER.S", dtCastingVideoPlayer,
		mediaStep{
			n: "1", line: 31, title: "DUT sends LaunchURL command to TH", pics: "CONTENTLAUNCHER.C.C01.Tx", need: "CONTENTLAUNCHER.S.F01",
			send: mediaCommand{
				cluster: launchdef.ClusterID, cmd: launchdef.CmdLaunchUrl, name: "LaunchURL", statusField: true,
				fields: launchdef.LaunchUrlRequest{
					ContentUrl:          "https://csa-iot.org/content",
					DisplayString:       ptr("Example content"),
					BrandingInformation: &launchdef.BrandingInformationStruct{ProviderName: "provider"},
				},
			},
		},
	)
}

// --- KeypadInput ---------------------------------------------------------------

// caseKEYPADINPUT31 mirrors TC-KEYPADINPUT-3.1: SendKey Up.
func caseKEYPADINPUT31() goCertCase {
	const keyCodeUp = 1 // KEY_CODE_UP (:16)
	return mediaCase("TC-KEYPADINPUT-3.1", 20, "KEYPADINPUT.C", "KEYPADINPUT.S", dtCastingVideoPlayer,
		// The key has no attribute to land in; chip's tv-app answers
		// UnsupportedKey for a key it does not handle (KeypadInputManager.cpp
		// :26-98, the daemon's tvKeys), so the response status 0 is the
		// daemon's evidence that a navigation key arrived.
		mediaStep{n: "1", line: 24, pics: "KEYPADINPUT.C.C00.Tx", title: "DUT sends SendKey command to TH", send: mediaCommand{
			cluster: keypadinput.ClusterID, cmd: keypadinput.CmdSendKey, name: "SendKey Up",
			fields: keypadinput.SendKeyRequest{KeyCode: keyCodeUp}, statusField: true,
		}},
	)
}

// --- LowPower --------------------------------------------------------------------

// caseLOWPOWER22 mirrors TC-LOWPOWER-2.2: Sleep. chip's all-clusters app
// puts LowPower on endpoint 1 (:14-15); the daemon serves it on the video
// player, as chip's tv-app does (tv-app.matter:4506 at 6170af84, inside
// endpoint 1), so the case runs there.
func caseLOWPOWER22() goCertCase {
	c := mediaCase("TC-LOWPOWER-2.2", 19, "LOWPOWER.C", "LOWPOWER.S", dtCastingVideoPlayer,
		mediaStep{n: "1", line: 30, pics: "LOWPOWER.C.C00.Tx", title: "DUT sends Sleep command to TH", run: func(th *goTH, n *client.Node, ep uint16) error {
			// The TS's TH-log check (:38, "Sleep carries no parameters")
			// mirrored onto the daemon's log: its LowPower delegate logs
			// the Sleep it handled (examples/reference-bridge/media.go
			// tvSleep, after chip's LowPowerManager.cpp:21-26).
			offset := len(th.dut.snapshotStderr())
			if err := th.mediaInvoke(n, ep, mediaCommand{cluster: lowpower.ClusterID, cmd: lowpower.CmdSleep, name: "Sleep", fields: lowpower.SleepRequest{}}); err != nil {
				return err
			}
			if waitFor(th.ctx, 5*time.Second, func() bool {
				return strings.Contains(th.dut.snapshotStderr()[offset:], "msg=media.lowpower.sleep")
			}) {
				th.note("the daemon logged media.lowpower.sleep")
				return nil
			}
			return fmt.Errorf("Sleep on endpoint %d succeeded, but the daemon logged no media.lowpower.sleep", ep)
		}},
	)
	// The TS's setup is its precondition step (:27), not the line after
	// certTest.
	c.steps[0].line, c.steps[0].title = 27, "Preconditions: the DUT commissions the TH."
	return c
}

// --- MediaInput ------------------------------------------------------------------

// caseMEDIAINPUT314 mirrors TC-MEDIAINPUT-3.14: SelectInput 2.
func caseMEDIAINPUT314() goCertCase {
	const index = 2 // INDEX (:16)
	return mediaCase("TC-MEDIAINPUT-3.14", 20, "MEDIAINPUT.C", "MEDIAINPUT.S", dtCastingVideoPlayer,
		mediaStep{
			n: "1", line: 24, pics: "MEDIAINPUT.C.C00.Tx", title: "DUT sends SelectInput command to TH",
			send: mediaCommand{
				cluster: mediainput.ClusterID, cmd: mediainput.CmdSelectInput, name: "SelectInput 2",
				fields: mediainput.SelectInputRequest{Index: index},
			},
			check: func(th *goTH, n *client.Node, ep uint16) error {
				// The payload: chip's HandleSelectInput makes the index
				// current (MediaInputManager.cpp:57-71).
				got, err := th.mediaReadU8(n, ep, mediainput.ClusterID, mediainput.AttrCurrentInput)
				if err != nil {
					return err
				}
				if got != index {
					return fmt.Errorf("CurrentInput reads %d after SelectInput %d", got, index)
				}
				th.note("CurrentInput %d", got)
				return nil
			},
		},
	)
}

// caseMEDIAINPUT315 mirrors TC-MEDIAINPUT-3.15: ShowInputStatus.
func caseMEDIAINPUT315() goCertCase {
	return mediaCase("TC-MEDIAINPUT-3.15", 17, "MEDIAINPUT.C", "MEDIAINPUT.S", dtCastingVideoPlayer,
		mediaStep{n: "1", line: 21, pics: "MEDIAINPUT.C.C01.Tx", title: "DUT sends ShowInputStatus command to TH", send: mediaCommand{
			cluster: mediainput.ClusterID, cmd: mediainput.CmdShowInputStatus, name: "ShowInputStatus",
			fields: mediainput.ShowInputStatusRequest{},
		}},
	)
}

// caseMEDIAINPUT316 mirrors TC-MEDIAINPUT-3.16: HideInputStatus.
func caseMEDIAINPUT316() goCertCase {
	return mediaCase("TC-MEDIAINPUT-3.16", 17, "MEDIAINPUT.C", "MEDIAINPUT.S", dtCastingVideoPlayer,
		mediaStep{n: "1", line: 21, pics: "MEDIAINPUT.C.C02.Tx", title: "DUT sends HideInputStatus command to TH", send: mediaCommand{
			cluster: mediainput.ClusterID, cmd: mediainput.CmdHideInputStatus, name: "HideInputStatus",
			fields: mediainput.HideInputStatusRequest{},
		}},
	)
}

// caseMEDIAINPUT317 mirrors TC-MEDIAINPUT-3.17: RenameInput 1 "HDMI 1".
func caseMEDIAINPUT317() goCertCase {
	const (
		index = 1        // INDEX (:16)
		name  = "HDMI 1" // NAME (:17)
	)
	return mediaCase("TC-MEDIAINPUT-3.17", 21, "MEDIAINPUT.C", "MEDIAINPUT.S", dtCastingVideoPlayer,
		mediaStep{
			n: "1", line: 25, pics: "MEDIAINPUT.C.C03.Tx", title: "DUT sends RenameInput command to TH",
			send: mediaCommand{
				cluster: mediainput.ClusterID, cmd: mediainput.CmdRenameInput, name: "RenameInput 1 \"HDMI 1\"",
				fields: mediainput.RenameInputRequest{Index: index, Name: name},
			},
			check: func(th *goTH, n *client.Node, ep uint16) error {
				// The payload: chip's HandleRenameInput stores the name
				// on the input of that index (MediaInputManager.cpp:93-114).
				v, err := th.readValue(n, attrPath(ep, mediainput.ClusterID, mediainput.AttrInputList))
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
			},
		},
	)
}

// --- MediaPlayback ---------------------------------------------------------------

// mediaPlayback is a MediaPlayback command whose request has no fields and
// whose PlaybackResponse carries a Status.
func mediaPlayback(cmd uint32, name string) mediaCommand {
	return mediaCommand{cluster: mediaplayback.ClusterID, cmd: cmd, name: name, fields: emptyFields, statusField: true}
}

// mediaTrackID reads a MediaPlayback track attribute's Id.
func (th *goTH) mediaTrackID(n *client.Node, ep uint16, attr uint32) (string, error) {
	v, err := th.readValue(n, attrPath(ep, mediaplayback.ClusterID, attr))
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

// mediaPosition reads SampledPosition's position (null reads as an error)
// and Duration.
func (th *goTH) mediaPosition(n *client.Node, ep uint16) (pos, duration uint64, err error) {
	v, err := th.readValue(n, attrPath(ep, mediaplayback.ClusterID, mediaplayback.AttrSampledPosition))
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
	d, err := th.readValue(n, attrPath(ep, mediaplayback.ClusterID, mediaplayback.AttrDuration))
	if err != nil {
		return 0, 0, err
	}
	if d.El.IsNull {
		return 0, 0, errors.New("Duration reads null")
	}
	return p.Position.Value, d.El.Uint, nil
}

// caseMEDIAPLAYBACK65 mirrors TC-MEDIAPLAYBACK-6.5: Play, Pause, Stop.
func caseMEDIAPLAYBACK65() goCertCase {
	return mediaCase("TC-MEDIAPLAYBACK-6.5", 17, "MEDIAPLAYBACK.C", "MEDIAPLAYBACK.S", dtCastingVideoPlayer,
		mediaStep{n: "1", line: 21, pics: "MEDIAPLAYBACK.C.C00.Tx", title: "DUT sends Play command to TH", send: mediaPlayback(mediaplayback.CmdPlay, "Play")},
		mediaStep{n: "2", line: 25, pics: "MEDIAPLAYBACK.C.C01.Tx", title: "DUT sends Pause command to TH", send: mediaPlayback(mediaplayback.CmdPause, "Pause")},
		mediaStep{n: "3", line: 29, pics: "MEDIAPLAYBACK.C.C02.Tx", title: "DUT sends Stop command to TH", send: mediaPlayback(mediaplayback.CmdStop, "Stop")},
	)
}

// caseMEDIAPLAYBACK67 mirrors TC-MEDIAPLAYBACK-6.7: Play, StartOver, Next,
// Previous, SkipForward and SkipBackward 10 s.
func caseMEDIAPLAYBACK67() goCertCase {
	const delta = 10_000 // DELTA_POSITION_MS (:16)
	// skip reads the position before the command and holds the one after
	// it to want(before, duration) — the payload's delta as chip's tv-app
	// applies it.
	skip := func(cmd uint32, name string, fields spec.Encodable, want func(pos, duration uint64) uint64, cite string) func(th *goTH, n *client.Node, ep uint16) error {
		return func(th *goTH, n *client.Node, ep uint16) error {
			before, _, err := th.mediaPosition(n, ep)
			if err != nil {
				return err
			}
			if err := th.mediaInvoke(n, ep, mediaCommand{cluster: mediaplayback.ClusterID, cmd: cmd, name: name, fields: fields, statusField: true}); err != nil {
				return err
			}
			pos, duration, err := th.mediaPosition(n, ep)
			if err != nil {
				return err
			}
			if w := want(before, duration); pos != w {
				return fmt.Errorf("SampledPosition %d -> %d after %s, want %d (%s, Duration %d)", before, pos, name, w, cite, duration)
			}
			th.note("SampledPosition %d -> %d", before, pos)
			return nil
		}
	}
	return mediaCase("TC-MEDIAPLAYBACK-6.7", 20, "MEDIAPLAYBACK.C", "MEDIAPLAYBACK.S", dtCastingVideoPlayer,
		mediaStep{n: "1", line: 24, pics: "MEDIAPLAYBACK.C.C00.Tx", title: "DUT sends Play command to TH", send: mediaPlayback(mediaplayback.CmdPlay, "Play")},
		mediaStep{n: "2", line: 28, pics: "MEDIAPLAYBACK.C.C03.Tx", title: "DUT sends StartOver command to TH", send: mediaPlayback(mediaplayback.CmdStartOver, "StartOver")},
		mediaStep{n: "3", line: 32, pics: "MEDIAPLAYBACK.C.C05.Tx", title: "DUT sends Next command to TH", send: mediaPlayback(mediaplayback.CmdNext, "Next")},
		mediaStep{n: "4", line: 36, pics: "MEDIAPLAYBACK.C.C04.Tx", title: "DUT sends Previous command to TH", send: mediaPlayback(mediaplayback.CmdPrevious, "Previous")},
		mediaStep{
			n: "5", line: 40, pics: "MEDIAPLAYBACK.C.C08.Tx", title: "DUT sends SkipForward command to TH",
			run: skip(mediaplayback.CmdSkipForward, "SkipForward 10000", mediaplayback.SkipForwardRequest{DeltaPositionMilliseconds: delta},
				func(pos, duration uint64) uint64 { return min(pos+delta, duration) },
				"HandleSkipForward, MediaPlaybackManager.cpp:245-257"),
		},
		mediaStep{
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
	)
}

// caseMEDIAPLAYBACK610 mirrors TC-MEDIAPLAYBACK-6.10: ActivateAudioTrack,
// ActivateTextTrack, DeactivateTextTrack.
func caseMEDIAPLAYBACK610() goCertCase {
	const (
		audioTrackID     = "activeAudioTrackId_1" // AUDIO_TRACK_ID (:19)
		audioOutputIndex = 1                      // AUDIO_OUTPUT_INDEX (:20)
		textTrackID      = "activeTextTrackId_1"  // TEXT_TRACK_ID (:21)
	)
	// active holds a track attribute's Id to the one the command named:
	// chip's HandleActivateAudioTrack / HandleActivateTextTrack make the
	// listed track of that id active (MediaPlaybackManager.cpp:305-333).
	active := func(attr uint32, what, want string) func(th *goTH, n *client.Node, ep uint16) error {
		return func(th *goTH, n *client.Node, ep uint16) error {
			got, err := th.mediaTrackID(n, ep, attr)
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
	return mediaCase("TC-MEDIAPLAYBACK-6.10", 25, "MEDIAPLAYBACK.C", "MEDIAPLAYBACK.S", dtCastingVideoPlayer,
		mediaStep{
			n: "1", line: 29, pics: "MEDIAPLAYBACK.C.C0c.Tx", title: "DUT sends ActivateAudioTrack command to TH",
			send: mediaCommand{
				cluster: mediaplayback.ClusterID, cmd: mediaplayback.CmdActivateAudioTrack, name: "ActivateAudioTrack " + audioTrackID,
				fields: mediaplayback.ActivateAudioTrackRequest{TrackId: audioTrackID, AudioOutputIndex: &spec.Nullable[uint8]{Value: audioOutputIndex}},
			},
			check: active(mediaplayback.AttrActiveAudioTrack, "ActiveAudioTrack", audioTrackID),
		},
		mediaStep{
			n: "2", line: 38, pics: "MEDIAPLAYBACK.C.C0d.Tx", title: "DUT sends ActivateTextTrack command to TH",
			send: mediaCommand{
				cluster: mediaplayback.ClusterID, cmd: mediaplayback.CmdActivateTextTrack, name: "ActivateTextTrack " + textTrackID,
				fields: mediaplayback.ActivateTextTrackRequest{TrackId: textTrackID},
			},
			check: active(mediaplayback.AttrActiveTextTrack, "ActiveTextTrack", textTrackID),
		},
		mediaStep{n: "3", line: 44, pics: "MEDIAPLAYBACK.C.C0e.Tx", title: "DUT sends DeactivateTextTrack command to TH", send: mediaCommand{
			cluster: mediaplayback.ClusterID, cmd: mediaplayback.CmdDeactivateTextTrack, name: "DeactivateTextTrack",
			fields: mediaplayback.DeactivateTextTrackRequest{},
		}},
	)
}

// --- TargetNavigator -------------------------------------------------------------

// caseTGTNAV82 mirrors TC-TGTNAV-8.2: NavigateTarget 2.
func caseTGTNAV82() goCertCase {
	const target = 2 // TARGET (:16)
	return mediaCase("TC-TGTNAV-8.2", 20, "TGTNAV.C", "TGTNAV.S", dtCastingVideoPlayer,
		mediaStep{
			n: "1", line: 24, pics: "TGTNAV.C.C00.Tx", title: "DUT sends NavigateTarget command to TH",
			send: mediaCommand{
				cluster: targetnavigator.ClusterID, cmd: targetnavigator.CmdNavigateTarget, name: "NavigateTarget 2",
				fields: targetnavigator.NavigateTargetRequest{Target: target}, statusField: true,
			},
			check: func(th *goTH, n *client.Node, ep uint16) error {
				// The payload: chip's HandleNavigateTarget makes the
				// target current (TargetNavigatorManager.cpp:58-74).
				got, err := th.mediaReadU8(n, ep, targetnavigator.ClusterID, targetnavigator.AttrCurrentTarget)
				if err != nil {
					return err
				}
				if got != target {
					return fmt.Errorf("CurrentTarget reads %d after NavigateTarget %d", got, target)
				}
				th.note("CurrentTarget %d", got)
				return nil
			},
		},
	)
}
