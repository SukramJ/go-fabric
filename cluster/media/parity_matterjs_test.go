// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package media_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/media"
	"github.com/SukramJ/go-fabric/cluster/spec"
	logindef "github.com/SukramJ/go-fabric/cluster/spec/accountlogin"
	basicdef "github.com/SukramJ/go-fabric/cluster/spec/applicationbasic"
	launcherdef "github.com/SukramJ/go-fabric/cluster/spec/applicationlauncher"
	outputdef "github.com/SukramJ/go-fabric/cluster/spec/audiooutput"
	channeldef "github.com/SukramJ/go-fabric/cluster/spec/channel"
	observerdef "github.com/SukramJ/go-fabric/cluster/spec/contentappobserver"
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

// TestParityMatterJS_MediaServers: each of the fourteen servers serves its
// generated definition, held against the matter.js snapshot
// (spectest.CheckDefinition), with the cluster id and revision of its
// element file at 0ceb202d (packages/model/src/standard/elements/
// <name>.element.ts: the Cluster(...) id and the ClusterRevision default),
// and a full feature selection checked against the conformance
// (spectest.CheckServer: mandatory elements present, disallowed ones
// absent, GeneratedCommandList, privileges, read-only writes refused).
func TestParityMatterJS_MediaServers(t *testing.T) {
	t.Parallel()
	must := func(srv contract.ClusterServer, err error) contract.ClusterServer {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return srv
	}
	player, _ := newPlayer(t)
	app := newApp(t)
	for _, c := range []struct {
		element  string
		id       uint32
		revision uint16
		def      *spec.Cluster
		srv      contract.ClusterServer
		features uint32
	}{
		// media-playback.element.ts:19-21
		{"media-playback", 0x0506, 2, mpdef.Definition, player, uint32(allPlaybackFeatures)},
		// keypad-input.element.ts:18-20
		{"keypad-input", 0x0509, 1, keypaddef.Definition, must(media.NewKeypad(media.KeypadConfig{Port: &keypad{}, Features: 7})), 7},
		// content-launcher.element.ts:18-20
		{"content-launcher", 0x050A, 2, launchdef.Definition, must(media.NewContentLauncher(media.ContentLauncherConfig{Port: &launcher{}, Features: 0x1F})), 0x1F},
		// application-launcher.element.ts:18-20
		{"application-launcher", 0x050C, 2, launcherdef.Definition, must(media.NewAppLauncher(media.AppLauncherConfig{
			Port: &platform{app: app}, Features: 1, Attributes: []uint32{launcherdef.AttrCurrentApp},
			Initial: media.AppLauncherState{CatalogList: []uint16{}},
		})), 1},
		// application-basic.element.ts:17-19
		{"application-basic", 0x050D, 1, basicdef.Definition, app, 0},
		// account-login.element.ts:18-20
		{"account-login", 0x050E, 2, logindef.Definition, must(media.NewAccountLogin(media.AccountLoginConfig{Port: &login{}})), 0},
		// audio-output.element.ts:18-20
		{"audio-output", 0x050B, 1, outputdef.Definition, must(media.NewOutputs(media.OutputsConfig{
			Port: &outputs{}, Features: 1, Initial: media.OutputState{OutputList: hdmiOutputs()},
		})), 1},
		// channel.element.ts:18-20
		{"channel", 0x0504, 2, channeldef.Definition, must(media.NewChannels(media.ChannelsConfig{
			Port: &tuner{}, Features: 0xF, Attributes: []uint32{channeldef.AttrCurrentChannel},
		})), 0xF},
		// target-navigator.element.ts:19-21
		{"target-navigator", 0x0505, 2, targetdef.Definition, must(media.NewTargets(media.TargetsConfig{
			Port: targets{}, Initial: media.TargetState{TargetList: []targetdef.TargetInfoStruct{}},
		})), 0},
		// media-input.element.ts:18-20
		{"media-input", 0x0507, 1, inputdef.Definition, must(media.NewInputs(media.InputsConfig{
			Port: &inputs{}, Features: 1, Initial: media.InputState{InputList: []inputdef.InputInfoStruct{}},
		})), 1},
		// low-power.element.ts:16-19
		{"low-power", 0x0508, 1, lowpowerdef.Definition, must(media.NewLowPower(media.LowPowerConfig{Port: &sleeper{}})), 0},
		// wake-on-lan.element.ts:12-14
		{"wake-on-lan", 0x0503, 1, woldef.Definition, must(media.NewWakeOnLan(media.WakeOnLanConfig{})), 0},
		// content-control.element.ts:19-21
		{"content-control", 0x050F, 1, ccdef.Definition, must(media.NewContentControl(media.ContentControlConfig{
			Port: &parental{}, Features: allContentControl,
			Initial: media.ContentControlState{
				OnDemandRatings: []ccdef.RatingNameStruct{}, ScheduledContentRatings: []ccdef.RatingNameStruct{},
				BlockChannelList: []ccdef.BlockChannelStruct{}, BlockApplicationList: []ccdef.AppInfoStruct{},
				BlockContentTimeWindow: []ccdef.TimeWindowStruct{},
			},
		})), uint32(allContentControl)},
		// content-app-observer.element.ts:18-20
		{"content-app-observer", 0x0510, 1, observerdef.Definition, must(media.NewContentAppObserver(media.ContentAppObserverConfig{Port: observer{}})), 0},
	} {
		t.Run(c.element, func(t *testing.T) {
			t.Parallel()
			spectest.CheckDefinition(t, c.def)
			if c.def.ID != c.id || c.def.Revision != c.revision {
				t.Errorf("definition 0x%04X revision %d, want 0x%04X revision %d", c.def.ID, c.def.Revision, c.id, c.revision)
			}
			if c.srv.MatterClusterID() != c.id {
				t.Errorf("server cluster 0x%04X", c.srv.MatterClusterID())
			}
			if v, ok := c.srv.MatterRead(cluster.AttrGlobalClusterRevision); !ok || v != c.revision {
				t.Errorf("ClusterRevision %v", v)
			}
			spectest.CheckServer(t, c.srv, c.def, c.features)
		})
	}
}
