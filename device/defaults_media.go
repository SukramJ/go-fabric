// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"fmt"
	"slices"

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
	targetdef "github.com/SukramJ/go-fabric/cluster/spec/targetnavigator"
	woldef "github.com/SukramJ/go-fabric/cluster/spec/wakeonlan"
	"github.com/SukramJ/go-fabric/contract"
)

// The kit's media servers (cluster/media, ADR 0022) on the ports their
// declarations carry (brief BU-b): a cluster whose server needs a port
// keeps the generated server without one; ApplicationBasic and WakeOnLan
// need none, and are the kit's once the declaration's Initial holds what
// they need. Each takes its state from the declaration's Initial
// ([configFrom]) over the device type's defaults. A port whose server
// refuses the declaration — a list the state needs and the Initial does
// not give, a feature whose commands the port does not implement — is a
// configuration error rather than the generated server: the host declared
// the port to have it served. A command the conformance makes optional is
// accepted when the port implements it, as cluster/media accepts it.

func init() {
	for id, f := range map[uint32]serverFactory{
		mpdef.ClusterID:       newMediaPlaybackServer,
		keypaddef.ClusterID:   newKeypadInputServer,
		launchdef.ClusterID:   newContentLauncherServer,
		launcherdef.ClusterID: newApplicationLauncherServer,
		inputdef.ClusterID:    newMediaInputServer,
		outputdef.ClusterID:   newAudioOutputServer,
		channeldef.ClusterID:  newChannelServer,
		targetdef.ClusterID:   newTargetNavigatorServer,
		lowpowerdef.ClusterID: newLowPowerServer,
		logindef.ClusterID:    newAccountLoginServer,
		woldef.ClusterID:      newWakeOnLanServer,
		basicdef.ClusterID:    newApplicationBasicServer,
		ccdef.ClusterID:       newContentControlServer,
	} {
		selfHeldDefaults[id] = f
	}
}

// newMediaPlaybackServer is the module's MediaPlayback server
// ([media.NewPlayback]) on the declared Playback, its state from the
// declaration's Initial.
func newMediaPlaybackServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(mpdef.Declare)
	if d.Playback == nil {
		return nil, notServed(fmt.Errorf("mediaplayback: %w", errNoPort))
	}
	cfg := media.PlaybackConfig{
		Port: d.Playback, Features: mpdef.Feature(b.opts.Features), Events: slices.Clone(b.opts.Events), DataVersion: b.cfg.DataVersion,
	}
	configFrom(&cfg.Initial, b.def, b.initial)
	return media.NewPlayback(cfg)
}

// newKeypadInputServer is the module's KeypadInput server
// ([media.NewKeypad]) on the declared Keypad.
func newKeypadInputServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(keypaddef.Declare)
	if d.Keypad == nil {
		return nil, notServed(fmt.Errorf("keypadinput: %w", errNoPort))
	}
	return media.NewKeypad(media.KeypadConfig{Port: d.Keypad, Features: keypaddef.Feature(b.opts.Features), DataVersion: b.cfg.DataVersion})
}

// newContentLauncherServer is the module's ContentLauncher server
// ([media.NewContentLauncher]) on the declared ContentLauncher.
func newContentLauncherServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(launchdef.Declare)
	if d.ContentLauncher == nil {
		return nil, notServed(fmt.Errorf("contentlauncher: %w", errNoPort))
	}
	cfg := media.ContentLauncherConfig{Port: d.ContentLauncher, Features: launchdef.Feature(b.opts.Features), DataVersion: b.cfg.DataVersion}
	configFrom(&cfg.Initial, b.def, b.initial)
	return media.NewContentLauncher(cfg)
}

// newApplicationLauncherServer is the module's ApplicationLauncher server
// ([media.NewAppLauncher]) on the declared AppLauncher.
func newApplicationLauncherServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(launcherdef.Declare)
	if d.AppLauncher == nil {
		return nil, notServed(fmt.Errorf("applicationlauncher: %w", errNoPort))
	}
	cfg := media.AppLauncherConfig{
		Port: d.AppLauncher, Features: launcherdef.Feature(b.opts.Features), Attributes: slices.Clone(b.opts.Attributes),
		DataVersion: b.cfg.DataVersion,
	}
	configFrom(&cfg.Initial, b.def, b.initial)
	return media.NewAppLauncher(cfg)
}

// newMediaInputServer is the module's MediaInput server
// ([media.NewInputs]) on the declared Inputs.
func newMediaInputServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(inputdef.Declare)
	if d.Inputs == nil {
		return nil, notServed(fmt.Errorf("mediainput: %w", errNoPort))
	}
	cfg := media.InputsConfig{Port: d.Inputs, Features: inputdef.Feature(b.opts.Features), DataVersion: b.cfg.DataVersion}
	configFrom(&cfg.Initial, b.def, b.initial)
	return media.NewInputs(cfg)
}

// newAudioOutputServer is the module's AudioOutput server
// ([media.NewOutputs]) on the declared Outputs.
func newAudioOutputServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(outputdef.Declare)
	if d.Outputs == nil {
		return nil, notServed(fmt.Errorf("audiooutput: %w", errNoPort))
	}
	cfg := media.OutputsConfig{Port: d.Outputs, Features: outputdef.Feature(b.opts.Features), DataVersion: b.cfg.DataVersion}
	configFrom(&cfg.Initial, b.def, b.initial)
	return media.NewOutputs(cfg)
}

// newChannelServer is the module's Channel server ([media.NewChannels])
// on the declared Channels.
func newChannelServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(channeldef.Declare)
	if d.Channels == nil {
		return nil, notServed(fmt.Errorf("channel: %w", errNoPort))
	}
	cfg := media.ChannelsConfig{
		Port: d.Channels, Features: channeldef.Feature(b.opts.Features), Attributes: slices.Clone(b.opts.Attributes),
		DataVersion: b.cfg.DataVersion,
	}
	configFrom(&cfg.Initial, b.def, b.initial)
	return media.NewChannels(cfg)
}

// newTargetNavigatorServer is the module's TargetNavigator server
// ([media.NewTargets]) on the declared Targets.
func newTargetNavigatorServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(targetdef.Declare)
	if d.Targets == nil {
		return nil, notServed(fmt.Errorf("targetnavigator: %w", errNoPort))
	}
	cfg := media.TargetsConfig{
		Port: d.Targets, Attributes: slices.Clone(b.opts.Attributes), Events: slices.Clone(b.opts.Events),
		DataVersion: b.cfg.DataVersion,
	}
	configFrom(&cfg.Initial, b.def, b.initial)
	return media.NewTargets(cfg)
}

// newLowPowerServer is the module's LowPower server ([media.NewLowPower])
// on the declared LowPower.
func newLowPowerServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(lowpowerdef.Declare)
	if d.LowPower == nil {
		return nil, notServed(fmt.Errorf("lowpower: %w", errNoPort))
	}
	return media.NewLowPower(media.LowPowerConfig{Port: d.LowPower, DataVersion: b.cfg.DataVersion})
}

// newAccountLoginServer is the module's AccountLogin server
// ([media.NewAccountLogin]) on the declared AccountLogin.
func newAccountLoginServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(logindef.Declare)
	if d.AccountLogin == nil {
		return nil, notServed(fmt.Errorf("accountlogin: %w", errNoPort))
	}
	return media.NewAccountLogin(media.AccountLoginConfig{Port: d.AccountLogin, Events: slices.Clone(b.opts.Events), DataVersion: b.cfg.DataVersion})
}

// newWakeOnLanServer is the module's WakeOnLan server
// ([media.NewWakeOnLan]), its state from the declaration's Initial.
func newWakeOnLanServer(b defaultBuild) (contract.ClusterServer, error) {
	cfg := media.WakeOnLanConfig{Attributes: slices.Clone(b.opts.Attributes), DataVersion: b.cfg.DataVersion}
	configFrom(&cfg.Initial, b.def, b.initial)
	return media.NewWakeOnLan(cfg)
}

// newApplicationBasicServer is the module's ApplicationBasic server
// ([media.NewAppBasic]) once the declaration's Initial gives the
// Application and AllowedVendorList it has no default for; the generated
// server until then.
func newApplicationBasicServer(b defaultBuild) (contract.ClusterServer, error) {
	cfg := media.AppBasicConfig{Attributes: slices.Clone(b.opts.Attributes), DataVersion: b.cfg.DataVersion}
	configFrom(&cfg.Initial, b.def, b.initial)
	srv, err := media.NewAppBasic(cfg)
	if errors.Is(err, media.ErrMissingState) {
		return nil, notServed(err)
	}
	return srv, err
}

// newContentControlServer is the module's ContentControl server
// ([media.NewContentControl]) on the declared ContentControl, its state
// from the declaration's Initial.
func newContentControlServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(ccdef.Declare)
	if d.ContentControl == nil {
		return nil, notServed(fmt.Errorf("contentcontrol: %w", errNoPort))
	}
	cfg := media.ContentControlConfig{Port: d.ContentControl, Features: ccdef.Feature(b.opts.Features), DataVersion: b.cfg.DataVersion}
	configFrom(&cfg.Initial, b.def, b.initial)
	return media.NewContentControl(cfg)
}
