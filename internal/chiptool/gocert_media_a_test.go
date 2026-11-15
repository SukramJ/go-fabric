// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	launcherdef "github.com/SukramJ/go-fabric/cluster/spec/applicationlauncher"
	outputdef "github.com/SukramJ/go-fabric/cluster/spec/audiooutput"
	channeldef "github.com/SukramJ/go-fabric/cluster/spec/channel"
	gocontroller "github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
)

// The media client cases, first half (review II, phase 4, owner decision
// 68): matter.js's TS files whose DUT is a media cluster client sending to
// its `tv` TH (tc-tv-client-support.ts sendsTvCommand), read mirrored onto
// the daemon's --media pair (examples/reference-bridge/media.go), which
// every daemon of the suite carries (bridge_test.go startBridgeWith). The
// TS line citations in this file were read at the snapshot's pin,
// db021da0 (parity/schema.json matter.sourceCommit), not at goCertReadAt.
//
// Each TS step checks two things (tc-support.ts sendCommand :1802-1815):
// the invoke's answer — Success, and a response's Status field 0 where
// the response carries one (invokeWithResponseChecks :1610-1662) — and
// the TH's log printing exactly the payload sent (expectCommandPayload).
// Read mirrored, the TH is the daemon, whose log the Go TH does not read
// for a payload: the answer is checked as the TS checks it, and where the
// command moves an attribute and the TS file names the effect of its
// argument (AudioOutput, Channel), the attribute is read back in place of
// the log check.

func init() {
	for _, c := range []func() goCertCase{
		caseAPPLAUNCHER371, caseAPPLAUNCHER381, caseAPPLAUNCHER391,
		caseAUDIOOUTPUT73, caseAUDIOOUTPUT74,
		caseCHANNEL54, caseCHANNEL55, caseCHANNEL56,
	} {
		registerGoCert(c)
	}
}

// mediaAKnownApplication is TV_KNOWN_APPLICATION
// (tc-tv-client-support.ts:32): catalog vendor 123, application
// "exampleid".
func mediaAKnownApplication() *launcherdef.ApplicationStruct {
	return &launcherdef.ApplicationStruct{CatalogVendorId: 123, ApplicationId: "exampleid"}
}

// mediaAStep is one command step of a media client case: the TS's
// sendsTvCommand, sent by send to ep.
type mediaAStep struct {
	n     string
	line  int
	title string
	// pics is the TS step's client PICS; need the server PICS the mirrored
	// command needs beyond it (the command's feature).
	pics, need string
	send       func(th *goTH, n *client.Node, ep uint16) error
}

// mediaACase is a media client case: step "0" commissions the daemon
// (tc-dd-support.ts commissionThStep :169-182, one line after certTest),
// each further step sends its command to the endpoint advertising
// deviceType — the TS's TV_VIDEO_PLAYER_ENDPOINT or TV_CONTENT_APP_ENDPOINT
// (tc-tv-client-support.ts:18, :24), chip's tv-app endpoints 1 and 3, which
// the daemon serves as the casting video player and the content app
// (families_table_test.go dtCastingVideoPlayer, dtContentApp) — and the
// finalizer removes the fabric (commissioned.decommissionAll).
func mediaACase(tc string, line int, pics, at string, deviceType uint32, steps ...mediaAStep) goCertCase {
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
			return err
		}}},
	}
	for _, s := range steps {
		c.steps = append(c.steps, goStep{n: s.n, line: s.line, title: s.title, pics: s.pics, need: s.need, run: func(th *goTH) error {
			if node == nil {
				return errors.New("step 0 did not commission the TH")
			}
			ep, err := th.mediaAEndpoint(deviceType)
			if err != nil {
				return err
			}
			return s.send(th, node, ep)
		}})
	}
	return c
}

// mediaAEndpoint is the daemon's first endpoint advertising deviceType,
// held to the endpoint the running step's PICS were evaluated on: a gate
// read off another endpoint would gate the step on a device it does not
// address.
func (th *goTH) mediaAEndpoint(deviceType uint32) (uint16, error) {
	ep := th.dut.endpointFor(th.t, deviceType)
	if ep != th.ep {
		return 0, fmt.Errorf("the step's PICS were evaluated on endpoint %d, the device type 0x%04X is endpoint %d", th.ep, deviceType, ep)
	}
	return ep, nil
}

// mediaAInvoke sends one command and notes its answer; statusField holds
// a response's Status (tag 0) to 0 (tc-support.ts :1647-1659).
func (th *goTH) mediaAInvoke(n *client.Node, ep uint16, cluster, cmd uint32, name string, fields spec.Encodable, statusField bool, opts client.InvokeOptions) (client.Result, error) {
	r, err := th.invoke(n, cmdPath(ep, cluster, cmd), fields, statusField, opts)
	if err != nil {
		return r, err
	}
	if r.HasFields {
		th.note("%s on endpoint %d: Success, response %s", name, ep, describeNode(r.Fields))
	} else {
		th.note("%s on endpoint %d: Success", name, ep)
	}
	return r, nil
}

// --- ApplicationLauncher ---------------------------------------------------

// mediaALauncherCase is the three ApplicationLauncher cases: one command
// with TV_KNOWN_APPLICATION to the video player.
func mediaALauncherCase(tc string, cmd uint32, name string, fields spec.Encodable, pics string) goCertCase {
	return mediaACase(tc, 17, "APPLAUNCHER.C", "APPLAUNCHER.S", dtCastingVideoPlayer,
		mediaAStep{n: "1", line: 21, title: "DUT sends " + name + " command to TH", pics: pics, send: func(th *goTH, n *client.Node, ep uint16) error {
			_, err := th.mediaAInvoke(n, ep, launcherdef.ClusterID, cmd, name, fields, true, client.InvokeOptions{})
			return err
		}},
	)
}

// caseAPPLAUNCHER371 mirrors TC-APPLAUNCHER-3.7.1: LaunchApp (:24-25).
func caseAPPLAUNCHER371() goCertCase {
	return mediaALauncherCase("TC-APPLAUNCHER-3.7.1", launcherdef.CmdLaunchApp, "LaunchApp",
		launcherdef.LaunchAppRequest{Application: mediaAKnownApplication()}, "APPLAUNCHER.C.C00.Tx")
}

// caseAPPLAUNCHER381 mirrors TC-APPLAUNCHER-3.8.1: StopApp (:24-25).
func caseAPPLAUNCHER381() goCertCase {
	return mediaALauncherCase("TC-APPLAUNCHER-3.8.1", launcherdef.CmdStopApp, "StopApp",
		launcherdef.StopAppRequest{Application: mediaAKnownApplication()}, "APPLAUNCHER.C.C01.Tx")
}

// caseAPPLAUNCHER391 mirrors TC-APPLAUNCHER-3.9.1: HideApp (:24-25).
func caseAPPLAUNCHER391() goCertCase {
	return mediaALauncherCase("TC-APPLAUNCHER-3.9.1", launcherdef.CmdHideApp, "HideApp",
		launcherdef.HideAppRequest{Application: mediaAKnownApplication()}, "APPLAUNCHER.C.C02.Tx")
}

// --- AudioOutput -----------------------------------------------------------

// caseAUDIOOUTPUT73 mirrors TC-AUDIOOUTPUT-7.3: SelectOutput to output 2,
// "An output both THs seed, and not the one they start on" (:15-16).
func caseAUDIOOUTPUT73() goCertCase {
	const index = 2 // INDEX (:16)
	return mediaACase("TC-AUDIOOUTPUT-7.3", 20, "AUDIOOUTPUT.C", "AUDIOOUTPUT.S", dtCastingVideoPlayer,
		mediaAStep{n: "1", line: 24, title: "DUT sends SelectOutput command to TH", pics: "AUDIOOUTPUT.C.C00.Tx", send: func(th *goTH, n *client.Node, ep uint16) error {
			if _, err := th.mediaAInvoke(n, ep, outputdef.ClusterID, outputdef.CmdSelectOutput, "SelectOutput",
				outputdef.SelectOutputRequest{Index: index}, false, client.InvokeOptions{}); err != nil {
				return err
			}
			v, err := th.readValue(n, attrPath(ep, outputdef.ClusterID, outputdef.AttrCurrentOutput))
			if err != nil {
				return err
			}
			if !isUnsigned(v) || v.El.Uint != index {
				return fmt.Errorf("CurrentOutput reads %s after SelectOutput %d", describeNode(v), index)
			}
			th.note("CurrentOutput %d", index)
			return nil
		}},
	)
}

// caseAUDIOOUTPUT74 mirrors TC-AUDIOOUTPUT-7.4: RenameOutput of output 1
// to "Kitchen Speakers" (:15-17), which needs the NameUpdates feature.
func caseAUDIOOUTPUT74() goCertCase {
	const (
		index = 1                  // INDEX (:16)
		name  = "Kitchen Speakers" // NAME (:17)
	)
	return mediaACase("TC-AUDIOOUTPUT-7.4", 21, "AUDIOOUTPUT.C", "AUDIOOUTPUT.S", dtCastingVideoPlayer,
		mediaAStep{n: "1", line: 25, title: "DUT sends RenameOutput command to TH", pics: "AUDIOOUTPUT.C.C01.Tx", need: "AUDIOOUTPUT.S.F00", send: func(th *goTH, n *client.Node, ep uint16) error {
			if _, err := th.mediaAInvoke(n, ep, outputdef.ClusterID, outputdef.CmdRenameOutput, "RenameOutput",
				outputdef.RenameOutputRequest{Index: index, Name: name}, false, client.InvokeOptions{}); err != nil {
				return err
			}
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
		}},
	)
}

// --- Channel -----------------------------------------------------------------

// mediaACurrentChannel reads Channel.CurrentChannel and holds it to want.
func (th *goTH) mediaACurrentChannel(n *client.Node, ep, wantMajor, wantMinor uint16, after string) error {
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
	return mediaACase("TC-CHANNEL-5.4", 23, "CHANNEL.C", "CHANNEL.S", dtCastingVideoPlayer,
		mediaAStep{n: "1", line: 27, title: "DUT sends ChangeChannel command to TH", pics: "CHANNEL.C.C00.Tx", send: func(th *goTH, n *client.Node, ep uint16) error {
			if _, err := th.mediaAInvoke(n, ep, channeldef.ClusterID, channeldef.CmdChangeChannel, "ChangeChannel",
				channeldef.ChangeChannelRequest{Match: "PBS"}, true, client.InvokeOptions{}); err != nil {
				return err
			}
			return th.mediaACurrentChannel(n, ep, 9, 1, `ChangeChannel "PBS"`)
		}},
	)
}

// caseCHANNEL55 mirrors TC-CHANNEL-5.5: ChangeChannelByNumber to 9.2,
// "seeded on both THs and not the one they start on" (:15-20).
func caseCHANNEL55() goCertCase {
	return mediaACase("TC-CHANNEL-5.5", 24, "CHANNEL.C", "CHANNEL.S", dtCastingVideoPlayer,
		mediaAStep{n: "1", line: 28, title: "DUT sends ChangeChannelByNumber command to TH", pics: "CHANNEL.C.C02.Tx", send: func(th *goTH, n *client.Node, ep uint16) error {
			if _, err := th.mediaAInvoke(n, ep, channeldef.ClusterID, channeldef.CmdChangeChannelByNumber, "ChangeChannelByNumber",
				channeldef.ChangeChannelByNumberRequest{MajorNumber: 9, MinorNumber: 2}, false, client.InvokeOptions{}); err != nil {
				return err
			}
			return th.mediaACurrentChannel(n, ep, 9, 2, "ChangeChannelByNumber 9.2")
		}},
	)
}

// caseCHANNEL56 mirrors TC-CHANNEL-5.6: SkipChannel by -1, "both THs wrap
// to the last channel" (:15-16) — the last entry of ChannelList.
func caseCHANNEL56() goCertCase {
	return mediaACase("TC-CHANNEL-5.6", 20, "CHANNEL.C", "CHANNEL.S", dtCastingVideoPlayer,
		mediaAStep{n: "1", line: 24, title: "DUT sends SkipChannel command to TH", pics: "CHANNEL.C.C03.Tx", send: func(th *goTH, n *client.Node, ep uint16) error {
			if _, err := th.mediaAInvoke(n, ep, channeldef.ClusterID, channeldef.CmdSkipChannel, "SkipChannel",
				channeldef.SkipChannelRequest{Count: -1}, false, client.InvokeOptions{}); err != nil {
				return err
			}
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
			return th.mediaACurrentChannel(n, ep, last.MajorNumber, last.MinorNumber, "SkipChannel -1")
		}},
	)
}
