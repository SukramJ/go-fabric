// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/media"
	logindef "github.com/SukramJ/go-fabric/cluster/spec/accountlogin"
	basicdef "github.com/SukramJ/go-fabric/cluster/spec/applicationbasic"
	launcherdef "github.com/SukramJ/go-fabric/cluster/spec/applicationlauncher"
	outputdef "github.com/SukramJ/go-fabric/cluster/spec/audiooutput"
	channeldef "github.com/SukramJ/go-fabric/cluster/spec/channel"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/contentcontrol"
	launchdef "github.com/SukramJ/go-fabric/cluster/spec/contentlauncher"
	keypaddef "github.com/SukramJ/go-fabric/cluster/spec/keypadinput"
	lowpowerdef "github.com/SukramJ/go-fabric/cluster/spec/lowpower"
	inputdef "github.com/SukramJ/go-fabric/cluster/spec/mediainput"
	mpdef "github.com/SukramJ/go-fabric/cluster/spec/mediaplayback"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	targetdef "github.com/SukramJ/go-fabric/cluster/spec/targetnavigator"
	woldef "github.com/SukramJ/go-fabric/cluster/spec/wakeonlan"
	"github.com/SukramJ/go-fabric/contract"
)

// server is an origin audit's constructor: a fresh server per case from
// newServer, or tb fails.
func server(newServer func() (contract.ClusterServer, error)) func(tb testing.TB) contract.ClusterServer {
	return func(tb testing.TB) contract.ClusterServer {
		tb.Helper()
		srv, err := newServer()
		if err != nil {
			tb.Fatal(err)
		}
		return srv
	}
}

// act is an origin case's action that fails tb on err.
func act(fn func(srv contract.ClusterServer) error) func(tb testing.TB, srv contract.ClusterServer) {
	return func(tb testing.TB, srv contract.ClusterServer) {
		tb.Helper()
		if err := fn(srv); err != nil {
			tb.Fatal(err)
		}
	}
}

// invokeOK invokes cmdID with fields on srv.
func invokeOK(cmdID uint32, fields any) func(tb testing.TB, srv contract.ClusterServer) {
	return act(func(srv contract.ClusterServer) error {
		_, err := srv.MatterInvoke(context.Background(), cmdID, fields)
		return err
	})
}

// refusedWrite is the controller-write row of a cluster without a writable
// attribute: the write of attrID is refused and changes nothing.
func refusedWrite(attrID uint32, value any) spectest.OriginCase {
	return spectest.OriginCase{Name: "controller write", Act: func(tb testing.TB, srv contract.ClusterServer) {
		tb.Helper()
		if err := srv.MatterWrite(context.Background(), attrID, value); err == nil {
			tb.Fatalf("a write of the read-only attribute 0x%04X succeeded", attrID)
		}
	}}
}

// protocols is a ContentLauncher whose LaunchURL moves
// SupportedStreamingProtocols, so the command's change can be audited.
type protocols struct{ launcher }

func (p *protocols) LaunchURL(ctx context.Context, st *media.ContentLauncherState, req launchdef.LaunchUrlRequest) (launchdef.LauncherResponse, error) {
	st.SupportedStreamingProtocols = launchdef.SupportedProtocolsHls
	return p.launcher.LaunchURL(ctx, st, req)
}

// TestChangesOrigin is the origin audit of the media servers: a host's
// typed Set reports OriginHostSet, a command's change OriginCommand, and a
// controller's write — no media attribute is writable — nothing; a server
// without attributes reports nothing at all.
func TestChangesOrigin(t *testing.T) {
	t.Parallel()
	paused := mpdef.PlaybackStatePaused
	t.Run("MediaPlayback", func(t *testing.T) {
		t.Parallel()
		spectest.CheckOrigins(t, server(func() (contract.ClusterServer, error) {
			return media.NewPlayback(media.PlaybackConfig{Port: &basicTransport{}})
		}),
			spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: act(func(srv contract.ClusterServer) error {
				return mpdef.Wrap(srv).Set(mpdef.Patch{CurrentState: &paused})
			})},
			spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: invokeOK(mpdef.CmdPause, mpdef.PauseRequest{})},
			refusedWrite(mpdef.AttrCurrentState, uint8(1)),
		)
	})
	hls := launchdef.SupportedProtocolsHls
	t.Run("ContentLauncher", func(t *testing.T) {
		t.Parallel()
		spectest.CheckOrigins(t, server(func() (contract.ClusterServer, error) {
			return media.NewContentLauncher(media.ContentLauncherConfig{Port: &protocols{}, Features: launchdef.FeatureUrlPlayback})
		}),
			spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: act(func(srv contract.ClusterServer) error {
				return launchdef.Wrap(srv).Set(launchdef.Patch{SupportedStreamingProtocols: &hls})
			})},
			spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: invokeOK(launchdef.CmdLaunchUrl, launchdef.LaunchUrlRequest{ContentUrl: "https://example"})},
			refusedWrite(launchdef.AttrSupportedStreamingProtocols, uint32(1)),
		)
	})
	catalogs := []uint16{123, 456}
	exampleApp := &launcherdef.ApplicationStruct{CatalogVendorId: 123, ApplicationId: "exampleid"}
	t.Run("ApplicationLauncher", func(t *testing.T) {
		t.Parallel()
		spectest.CheckOrigins(t, server(func() (contract.ClusterServer, error) {
			return media.NewAppLauncher(media.AppLauncherConfig{
				Port: &platform{app: newApp(t)}, Features: launcherdef.FeatureApplicationPlatform,
				Attributes: []uint32{launcherdef.AttrCurrentApp}, Initial: media.AppLauncherState{CatalogList: []uint16{123}},
			})
		}),
			spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: act(func(srv contract.ClusterServer) error {
				return launcherdef.Wrap(srv).Set(launcherdef.Patch{CatalogList: &catalogs})
			})},
			spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: invokeOK(launcherdef.CmdLaunchApp, launcherdef.LaunchAppRequest{Application: exampleApp})},
			refusedWrite(launcherdef.AttrCatalogList, []uint16{1}),
		)
	})
	hidden := basicdef.ApplicationStatusActiveHidden
	t.Run("ApplicationBasic", func(t *testing.T) {
		t.Parallel()
		spectest.CheckOrigins(t, server(func() (contract.ClusterServer, error) { return newApp(t), nil }),
			spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: act(func(srv contract.ClusterServer) error {
				return basicdef.Wrap(srv).Set(basicdef.Patch{Status: &hidden})
			})},
			refusedWrite(basicdef.AttrStatus, uint8(1)),
		)
	})
	two := uint8(2)
	t.Run("MediaInput", func(t *testing.T) {
		t.Parallel()
		spectest.CheckOrigins(t, server(func() (contract.ClusterServer, error) {
			return media.NewInputs(media.InputsConfig{Port: &inputs{}, Initial: media.InputState{
				InputList: []inputdef.InputInfoStruct{{Index: 1, Name: "HDMI"}, {Index: 2, Name: "HDMI"}}, CurrentInput: 1,
			}})
		}),
			spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: act(func(srv contract.ClusterServer) error {
				return inputdef.Wrap(srv).Set(inputdef.Patch{CurrentInput: &two})
			})},
			spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: invokeOK(inputdef.CmdSelectInput, inputdef.SelectInputRequest{Index: 2})},
			refusedWrite(inputdef.AttrCurrentInput, uint8(2)),
		)
	})
	t.Run("AudioOutput", func(t *testing.T) {
		t.Parallel()
		spectest.CheckOrigins(t, server(func() (contract.ClusterServer, error) {
			return media.NewOutputs(media.OutputsConfig{Port: &outputs{}, Initial: media.OutputState{OutputList: hdmiOutputs(), CurrentOutput: 1}})
		}),
			spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: act(func(srv contract.ClusterServer) error {
				return outputdef.Wrap(srv).Set(outputdef.Patch{CurrentOutput: &two})
			})},
			spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: invokeOK(outputdef.CmdSelectOutput, outputdef.SelectOutputRequest{Index: 2})},
			refusedWrite(outputdef.AttrCurrentOutput, uint8(2)),
		)
	})
	t.Run("Channel", func(t *testing.T) {
		t.Parallel()
		list := channels()
		spectest.CheckOrigins(t, server(func() (contract.ClusterServer, error) {
			return media.NewChannels(media.ChannelsConfig{
				Port: &tuner{}, Features: channeldef.FeatureChannelList, Attributes: []uint32{channeldef.AttrCurrentChannel},
				Initial: media.ChannelState{ChannelList: list, CurrentChannel: &list[0]},
			})
		}),
			spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: act(func(srv contract.ClusterServer) error {
				return channeldef.Wrap(srv).Set(channeldef.Patch{CurrentChannel: &list[1]})
			})},
			spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: invokeOK(channeldef.CmdSkipChannel, channeldef.SkipChannelRequest{Count: 1})},
			refusedWrite(channeldef.AttrChannelList, []channeldef.ChannelInfoStruct{}),
		)
	})
	t.Run("TargetNavigator", func(t *testing.T) {
		t.Parallel()
		spectest.CheckOrigins(t, server(func() (contract.ClusterServer, error) { return newTargets(t), nil }),
			spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: act(func(srv contract.ClusterServer) error {
				return targetdef.Wrap(srv).Set(targetdef.Patch{CurrentTarget: &two})
			})},
			spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: invokeOK(targetdef.CmdNavigateTarget, targetdef.NavigateTargetRequest{Target: 1})},
			refusedWrite(targetdef.AttrCurrentTarget, uint8(1)),
		)
	})
	mac := "02005E102002"
	t.Run("WakeOnLan", func(t *testing.T) {
		t.Parallel()
		spectest.CheckOrigins(t, server(func() (contract.ClusterServer, error) {
			return media.NewWakeOnLan(media.WakeOnLanConfig{Attributes: []uint32{woldef.AttrMacAddress}})
		}),
			spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: act(func(srv contract.ClusterServer) error {
				return woldef.Wrap(srv).Set(woldef.Patch{MacAddress: &mac})
			})},
			refusedWrite(woldef.AttrMacAddress, mac),
		)
	})
	on := true
	t.Run("ContentControl", func(t *testing.T) {
		t.Parallel()
		spectest.CheckOrigins(t, server(func() (contract.ClusterServer, error) {
			return media.NewContentControl(media.ContentControlConfig{Port: &onOffParental{}})
		}),
			spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: act(func(srv contract.ClusterServer) error {
				return ccdef.Wrap(srv).Set(ccdef.Patch{Enabled: &on})
			})},
			spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: invokeOK(ccdef.CmdEnable, ccdef.EnableRequest{})},
			refusedWrite(ccdef.AttrEnabled, true),
		)
	})
	// The servers without attributes: neither a command nor anything else
	// reports a change.
	for name, tc := range map[string]struct {
		srv   func(tb testing.TB) contract.ClusterServer
		cmdID uint32
		req   any
	}{
		"KeypadInput": {server(func() (contract.ClusterServer, error) {
			return media.NewKeypad(media.KeypadConfig{Port: &keypad{}})
		}), keypaddef.CmdSendKey, keypaddef.SendKeyRequest{KeyCode: keypaddef.CecKeyCodeUp}},
		"LowPower": {server(func() (contract.ClusterServer, error) {
			return media.NewLowPower(media.LowPowerConfig{Port: &sleeper{}})
		}), lowpowerdef.CmdSleep, lowpowerdef.SleepRequest{}},
		"AccountLogin": {server(func() (contract.ClusterServer, error) {
			return media.NewAccountLogin(media.AccountLoginConfig{Port: &login{}})
		}), logindef.CmdLogout, logindef.LogoutRequest{}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spectest.CheckOrigins(t, tc.srv, spectest.OriginCase{Name: "command", Act: func(tb testing.TB, srv contract.ClusterServer) {
				tb.Helper()
				_, _ = srv.MatterInvoke(context.Background(), tc.cmdID, tc.req)
			}})
		})
	}
}

// newTargets is a TargetNavigator over targets with two targets, none
// current.
func newTargets(tb testing.TB) *media.TargetsServer {
	tb.Helper()
	srv, err := media.NewTargets(media.TargetsConfig{
		Port: targets{}, Attributes: []uint32{targetdef.AttrCurrentTarget},
		Initial: media.TargetState{
			TargetList:    []targetdef.TargetInfoStruct{{Identifier: 1, Name: "home"}, {Identifier: 2, Name: "settings"}},
			CurrentTarget: 0xFF,
		},
	})
	if err != nil {
		tb.Fatal(err)
	}
	return srv
}

// TestTypedSetReachesCommands: a host's typed Set is the state the next
// command is handed — a target the Set adds is navigable, and the
// command's commit keeps the Set's list rather than the state before it.
func TestTypedSetReachesCommands(t *testing.T) {
	t.Parallel()
	srv := newTargets(t)
	list := []targetdef.TargetInfoStruct{{Identifier: 1, Name: "home"}, {Identifier: 2, Name: "settings"}, {Identifier: 3, Name: "guide"}}
	if err := targetdef.Wrap(srv).Set(targetdef.Patch{TargetList: &list}); err != nil {
		t.Fatal(err)
	}
	resp, err := srv.MatterInvoke(context.Background(), targetdef.CmdNavigateTarget, targetdef.NavigateTargetRequest{Target: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := resp.(targetdef.NavigateTargetResponse); r.Status != targetdef.StatusSuccess {
		t.Errorf("NavigateTarget 3 after the Set: %+v, want Success", resp)
	}
	st := targetdef.Wrap(srv).Get()
	if st.CurrentTarget != 3 || len(st.TargetList) != 3 {
		t.Errorf("state after the command: %+v", st)
	}
	if got := srv.State(); got.CurrentTarget != 3 || len(got.TargetList) != 3 {
		t.Errorf("State() after the command: %+v", got)
	}
}
