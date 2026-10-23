// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/media"
	"github.com/SukramJ/go-fabric/cluster/spec"
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
	targetdef "github.com/SukramJ/go-fabric/cluster/spec/targetnavigator"
	woldef "github.com/SukramJ/go-fabric/cluster/spec/wakeonlan"
)

// The rest of the media ports on mediaPort (media_test.go): each command
// succeeds and changes nothing.

func (mediaPort) ChangeChannel(context.Context, *media.ChannelState, channeldef.ChangeChannelRequest) (channeldef.ChangeChannelResponse, error) {
	return channeldef.ChangeChannelResponse{}, nil
}

func (mediaPort) ChangeChannelByNumber(context.Context, *media.ChannelState, channeldef.ChangeChannelByNumberRequest) error {
	return nil
}

func (mediaPort) SkipChannel(context.Context, *media.ChannelState, channeldef.SkipChannelRequest) error {
	return nil
}

func (mediaPort) NavigateTarget(context.Context, *media.TargetState, targetdef.NavigateTargetRequest) (targetdef.NavigateTargetResponse, error) {
	return targetdef.NavigateTargetResponse{}, nil
}

func (mediaPort) Sleep(context.Context) error { return nil }

// Enable and Disable are the ContentControl port: Enable sets Enabled.
func (mediaPort) Enable(_ context.Context, st *media.ContentControlState) error {
	st.Enabled = true
	return nil
}

func (mediaPort) Disable(_ context.Context, st *media.ContentControlState) error {
	st.Enabled = false
	return nil
}

func (mediaPort) GetSetupPIN(context.Context, logindef.GetSetupPinRequest) (logindef.GetSetupPinResponse, error) {
	return logindef.GetSetupPinResponse{}, nil
}

func (mediaPort) Login(context.Context, logindef.LoginRequest) error { return nil }

func (mediaPort) Logout(context.Context, logindef.LogoutRequest) error { return nil }

// mediaCases are the self-held media defaults' cases of
// TestSelfHeldDefaults: each declared with its port (ApplicationBasic and
// WakeOnLan with none) on a device type that lists the cluster.
func mediaCases() []struct {
	cluster uint32
	build   func() (*Device, error)
	want    any
} {
	p := mediaPort{}
	player := func(key string, decls ...Declaration) func() (*Device, error) {
		return func() (*Device, error) { return New(&CastingVideoPlayerType, testOptions(key), decls...) }
	}
	inputs := []inputdef.InputInfoStruct{{Index: 1, InputType: inputdef.InputTypeHdmi, Name: "HDMI"}}
	outputs := []outputdef.OutputInfoStruct{{Index: 1, OutputType: outputdef.OutputTypeHdmi, Name: "HDMI"}}
	targets := []targetdef.TargetInfoStruct{{Identifier: 1, Name: "home"}}
	catalogs := []uint16{123}
	vendors := []uint16{0xFFF1}
	return []struct {
		cluster uint32
		build   func() (*Device, error)
		want    any
	}{
		{mpdef.ClusterID, player("mp", mpdef.Declare{Playback: p}), (*media.PlaybackServer)(nil)},
		{keypaddef.ClusterID, player("kp", keypaddef.Declare{Keypad: p}), (*media.KeypadServer)(nil)},
		{launchdef.ClusterID, player("cl", launchdef.Declare{ContentLauncher: p, Features: launchdef.FeatureContentSearch}), (*media.ContentLauncherServer)(nil)},
		{launcherdef.ClusterID, player("al", launcherdef.Declare{
			AppLauncher: p, Features: launcherdef.FeatureApplicationPlatform, Initial: launcherdef.Patch{CatalogList: &catalogs},
		}), (*media.AppLauncherServer)(nil)},
		{inputdef.ClusterID, player("mi", inputdef.Declare{Inputs: p, Initial: inputdef.Patch{InputList: &inputs}}), (*media.InputsServer)(nil)},
		{outputdef.ClusterID, player("ao", outputdef.Declare{Outputs: p, Initial: outputdef.Patch{OutputList: &outputs}}), (*media.OutputsServer)(nil)},
		{channeldef.ClusterID, player("ch", channeldef.Declare{Channels: p, Features: channeldef.FeatureChannelList}), (*media.ChannelsServer)(nil)},
		{targetdef.ClusterID, player("tn", targetdef.Declare{Targets: p, Initial: targetdef.Patch{TargetList: &targets}}), (*media.TargetsServer)(nil)},
		{lowpowerdef.ClusterID, player("lp", lowpowerdef.Declare{LowPower: p}), (*media.LowPowerServer)(nil)},
		{logindef.ClusterID, player("login", logindef.Declare{AccountLogin: p}), (*media.AccountLoginServer)(nil)},
		{woldef.ClusterID, player("wol", woldef.Declare{}), (*media.WakeOnLanServer)(nil)},
		{ccdef.ClusterID, player("cc", ccdef.Declare{ContentControl: p}), (*media.ContentControlServer)(nil)},
		{basicdef.ClusterID, func() (*Device, error) {
			return New(&ContentAppType, testOptions("ab"), basicdef.Declare{Initial: basicdef.Patch{
				Application: &basicdef.ApplicationStruct{CatalogVendorId: 123, ApplicationId: "exampleid"}, AllowedVendorList: &vendors,
			}})
		}, (*media.AppBasicServer)(nil)},
	}
}

// TestMediaDefaultsFromTheDeclaration: a declared port is the kit's media
// server, its state the declaration's Initial — read through the typed
// accessor, changed by the typed Set and handed to the next command; a
// port without the state its server needs is a configuration error, and
// ApplicationBasic without its Application the generated server.
func TestMediaDefaultsFromTheDeclaration(t *testing.T) {
	t.Parallel()
	inputs := []inputdef.InputInfoStruct{{Index: 1, Name: "HDMI"}, {Index: 2, Name: "HDMI"}}
	current := uint8(1)
	d, err := NewCastingVideoPlayer(testOptions("player"), inputdef.Declare{Inputs: selectingInputs{}, Initial: inputdef.Patch{InputList: &inputs, CurrentInput: &current}})
	if err != nil {
		t.Fatal(err)
	}
	if st := d.MediaInput().Get(); st.CurrentInput != 1 || len(st.InputList) != 2 {
		t.Fatalf("MediaInput state %+v, want the declaration's", st)
	}
	more := slices.Concat(inputs, []inputdef.InputInfoStruct{{Index: 3, Name: "USB"}})
	if err := d.MediaInput().Set(inputdef.Patch{InputList: &more}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Server(inputdef.ClusterID).MatterInvoke(context.Background(), inputdef.CmdSelectInput, inputdef.SelectInputRequest{Index: 3}); err != nil {
		t.Fatalf("SelectInput 3 after the typed Set: %v", err)
	}
	if st := d.MediaInput().Get(); st.CurrentInput != 3 || len(st.InputList) != 3 {
		t.Errorf("MediaInput state %+v after the command", st)
	}

	if _, err := NewCastingVideoPlayer(testOptions("bare"), inputdef.Declare{Inputs: selectingInputs{}}); !errors.Is(err, ErrConfiguration) || !errors.Is(err, media.ErrMissingState) {
		t.Errorf("a port without InputList: %v, want ErrConfiguration wrapping media.ErrMissingState", err)
	}
	app, err := NewContentApp(testOptions("app"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := app.Server(basicdef.ClusterID).(*spec.Server); !ok {
		t.Errorf("ApplicationBasic without its Application: %T, want the generated server", app.Server(basicdef.ClusterID))
	}
}

// selectingInputs is a MediaInput port that selects a listed input.
type selectingInputs struct{ mediaPort }

func (selectingInputs) SelectInput(_ context.Context, st *media.InputState, req inputdef.SelectInputRequest) error {
	for _, in := range st.InputList {
		if in.Index == req.Index {
			st.CurrentInput = req.Index
			return nil
		}
	}
	return errors.New("no such input")
}
