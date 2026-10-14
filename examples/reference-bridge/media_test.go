// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	matterbridge "github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/spec"
	logindef "github.com/SukramJ/go-fabric/cluster/spec/accountlogin"
	basicdef "github.com/SukramJ/go-fabric/cluster/spec/applicationbasic"
	launcherdef "github.com/SukramJ/go-fabric/cluster/spec/applicationlauncher"
	outputdef "github.com/SukramJ/go-fabric/cluster/spec/audiooutput"
	channeldef "github.com/SukramJ/go-fabric/cluster/spec/channel"
	launchdef "github.com/SukramJ/go-fabric/cluster/spec/contentlauncher"
	keypaddef "github.com/SukramJ/go-fabric/cluster/spec/keypadinput"
	lowpowerdef "github.com/SukramJ/go-fabric/cluster/spec/lowpower"
	inputdef "github.com/SukramJ/go-fabric/cluster/spec/mediainput"
	mpdef "github.com/SukramJ/go-fabric/cluster/spec/mediaplayback"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	targetdef "github.com/SukramJ/go-fabric/cluster/spec/targetnavigator"
	woldef "github.com/SukramJ/go-fabric/cluster/spec/wakeonlan"
	"github.com/SukramJ/go-fabric/endpoint"
)

// startMediaFleet is a fleet with --media on a started node.
func startMediaFleet(t *testing.T) (f *fleet, player, app *endpoint.Endpoint, br *matterbridge.Bridge) {
	t.Helper()
	f = newFleet()
	m, err := newDemoMedia(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	f.media = m
	n := testNode(t, f, filepath.Join(t.TempDir(), "media.db"))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	br = n.Bridge()
	return f, mountedEndpoint(t, br, "demo-player-1"), mountedEndpoint(t, br, "demo-contentapp-1"), br
}

// invokeMounted invokes a command on the server ep mounts for clusterID.
func invokeMounted(t *testing.T, ep *endpoint.Endpoint, clusterID, cmdID uint32, req spec.Encodable) any {
	t.Helper()
	for _, srv := range endpoint.ClusterServers(ep) {
		if srv == nil || srv.MatterClusterID() != clusterID {
			continue
		}
		fields, err := spec.Lookup(clusterID).Command(cmdID, spec.Request).Decode(spectest.Decode(t, spectest.Encode(t, req)))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := srv.MatterInvoke(context.Background(), cmdID, fields)
		if err != nil {
			t.Fatalf("endpoint %d cluster 0x%04X command 0x%02X: %v", ep.ID, clusterID, cmdID, err)
		}
		return resp
	}
	t.Fatalf("endpoint %d mounts no cluster 0x%04X", ep.ID, clusterID)
	return nil
}

// TestMediaFlag: with --media the node bridges a CastingVideoPlayer and a
// ContentApp after every other device, both free of device-type
// violations, serving the clusters chip's tv-app serves on its endpoints 1
// and 3; without the flag there is neither, and the app pipe's
// MediaPlaybackState is refused.
func TestMediaFlag(t *testing.T) {
	plain := newFleet()
	if err := plain.applyPipeCommand(pipeCommand{Name: "MediaPlaybackState", Operation: "Paused"}); err == nil {
		t.Error("MediaPlaybackState accepted without --media")
	}
	f, player, app, br := startMediaFleet(t)
	if player.DeviceType != 0x0023 || app.DeviceType != 0x0024 {
		t.Fatalf("device types 0x%04X, 0x%04X", player.DeviceType, app.DeviceType)
	}
	esa := mountedEndpoint(t, br, "demo-esa-1")
	if player.ID <= esa.ID || app.ID <= player.ID {
		t.Errorf("endpoints player %d, app %d after the ESA %d: the media pair must come last", player.ID, app.ID, esa.ID)
	}
	for _, v := range endpoint.ValidateDeviceTypes(br.Topology()) {
		if v.Endpoint == player.ID || v.Endpoint == app.ID {
			t.Errorf("device type violation: %s", v)
		}
	}
	for _, id := range []uint32{0x0006, 0x0506, 0x0509, 0x050A, 0x050C, 0x0507, 0x050B, 0x0504, 0x0505, 0x0508, 0x0503} {
		readMounted(t, player, id, 0xFFFD)
	}
	for _, id := range []uint32{0x050D, 0x050C, 0x050E, 0x0509} {
		readMounted(t, app, id, 0xFFFD)
	}
	if v := readMounted(t, player, woldef.ClusterID, woldef.AttrMacAddress).(string); len(v) != 12 {
		t.Errorf("MACAddress %q, want 12 hex digits (WAKEONLAN-4.1 minLength 12)", v)
	}
	if err := f.applyPipeCommand(pipeCommand{Name: "MediaPlaybackState", Operation: "Buffering"}); err != nil {
		t.Fatal(err)
	}
	if v := readMounted(t, player, mpdef.ClusterID, mpdef.AttrCurrentState); v != uint8(mpdef.PlaybackStateBuffering) {
		t.Errorf("CurrentState %v after the pipe command", v)
	}
	if err := f.applyPipeCommand(pipeCommand{Name: "MediaPlaybackState", Operation: "Rewinding"}); err == nil {
		t.Error("an unknown playback state accepted")
	}
}

// TestMediaPlaybackLikeTheTvApp: the MEDIAPLAYBACK-6.1 to 6.4 sequences
// against the simulated transport.
func TestMediaPlaybackLikeTheTvApp(t *testing.T) {
	_, player, _, _ := startMediaFleet(t)
	state := func() any { return readMounted(t, player, mpdef.ClusterID, mpdef.AttrCurrentState) }
	speed := func() any { return readMounted(t, player, mpdef.ClusterID, mpdef.AttrPlaybackSpeed) }
	invokeMounted(t, player, mpdef.ClusterID, mpdef.CmdPause, mpdef.PauseRequest{})
	if state() != uint8(1) || speed() != float32(0) {
		t.Errorf("after Pause: %v %v", state(), speed())
	}
	for _, want := range []float32{1, 2} {
		invokeMounted(t, player, mpdef.ClusterID, mpdef.CmdFastForward, mpdef.FastForwardRequest{})
		if speed() != want || state() != uint8(0) {
			t.Errorf("FastForward: speed %v state %v, want %v", speed(), state(), want)
		}
	}
	for _, want := range []float32{-1, -2} {
		invokeMounted(t, player, mpdef.ClusterID, mpdef.CmdRewind, mpdef.RewindRequest{})
		if speed() != want {
			t.Errorf("Rewind: speed %v, want %v", speed(), want)
		}
	}
	invokeMounted(t, player, mpdef.ClusterID, mpdef.CmdPlay, mpdef.PlayRequest{})
	if speed() != float32(1) {
		t.Errorf("Play: speed %v", speed())
	}
	invokeMounted(t, player, mpdef.ClusterID, mpdef.CmdSkipForward, mpdef.SkipForwardRequest{DeltaPositionMilliseconds: 10000})
	if p := readMounted(t, player, mpdef.ClusterID, mpdef.AttrSampledPosition).(mpdef.PlaybackPositionStruct); p.Position.Value != 10000 {
		t.Errorf("SampledPosition %+v", p)
	}
	invokeMounted(t, player, mpdef.ClusterID, mpdef.CmdSkipBackward, mpdef.SkipBackwardRequest{DeltaPositionMilliseconds: 10000})
	if p := readMounted(t, player, mpdef.ClusterID, mpdef.AttrSampledPosition).(mpdef.PlaybackPositionStruct); p.Position.Value != 0 {
		t.Errorf("SampledPosition %+v", p)
	}
	if r := invokeMounted(t, player, mpdef.ClusterID, mpdef.CmdSeek, mpdef.SeekRequest{Position: 100_000_000}).(mpdef.PlaybackResponse); r.Status != mpdef.StatusSeekOutOfRange {
		t.Errorf("Seek past the end: %+v", r)
	}
	invokeMounted(t, player, mpdef.ClusterID, mpdef.CmdStop, mpdef.StopRequest{})
	if state() != uint8(2) {
		t.Errorf("after Stop: %v", state())
	}
	for _, cmd := range []struct {
		id  uint32
		req spec.Encodable
	}{{mpdef.CmdStartOver, mpdef.StartOverRequest{}}, {mpdef.CmdNext, mpdef.NextRequest{}}, {mpdef.CmdPrevious, mpdef.PreviousRequest{}}} {
		if r := invokeMounted(t, player, mpdef.ClusterID, cmd.id, cmd.req).(mpdef.PlaybackResponse); r.Status != mpdef.StatusSuccess {
			t.Errorf("0x%02X: %+v", cmd.id, r)
		}
	}
}

// TestMediaPlayerClustersLikeTheTvApp: the channel, input, output, target,
// keypad and launcher sequences of the CHANNEL, MEDIAINPUT, AUDIOOUTPUT,
// TGTNAV, KEYPADINPUT and CONTENTLAUNCHER cases.
func TestMediaPlayerClustersLikeTheTvApp(t *testing.T) {
	_, player, app, _ := startMediaFleet(t)
	ch := func() channeldef.ChannelInfoStruct {
		return readMounted(t, player, channeldef.ClusterID, channeldef.AttrCurrentChannel).(channeldef.ChannelInfoStruct)
	}
	if c := ch(); c.MajorNumber != 6 || c.MinorNumber != 0 {
		t.Errorf("CurrentChannel %+v, want 6.0 (CHANNEL-5.3)", c)
	}
	invokeMounted(t, player, channeldef.ClusterID, channeldef.CmdSkipChannel, channeldef.SkipChannelRequest{Count: 1})
	if c := ch(); c.MajorNumber != 9 || c.MinorNumber != 1 {
		t.Errorf("CurrentChannel after SkipChannel(1) %+v, want 9.1", c)
	}
	invokeMounted(t, player, channeldef.ClusterID, channeldef.CmdChangeChannelByNumber, channeldef.ChangeChannelByNumberRequest{MajorNumber: 9, MinorNumber: 2})
	if c := ch(); c.MinorNumber != 2 {
		t.Errorf("CurrentChannel %+v, want 9.2", c)
	}
	if r := invokeMounted(t, player, channeldef.ClusterID, channeldef.CmdChangeChannel, channeldef.ChangeChannelRequest{Match: "PBS"}).(channeldef.ChangeChannelResponse); r.Status != channeldef.StatusSuccess {
		t.Errorf("ChangeChannel PBS %+v", r)
	}
	if r := invokeMounted(t, player, channeldef.ClusterID, channeldef.CmdChangeChannel, channeldef.ChangeChannelRequest{Match: "KCTS"}).(channeldef.ChangeChannelResponse); r.Status != channeldef.StatusMultipleMatches {
		t.Errorf("ChangeChannel KCTS %+v", r)
	}
	if r := invokeMounted(t, player, channeldef.ClusterID, channeldef.CmdChangeChannel, channeldef.ChangeChannelRequest{Match: "CNN"}).(channeldef.ChangeChannelResponse); r.Status != channeldef.StatusNoMatches {
		t.Errorf("ChangeChannel CNN %+v", r)
	}
	invokeMounted(t, player, inputdef.ClusterID, inputdef.CmdSelectInput, inputdef.SelectInputRequest{Index: 2})
	invokeMounted(t, player, inputdef.ClusterID, inputdef.CmdShowInputStatus, inputdef.ShowInputStatusRequest{})
	invokeMounted(t, player, inputdef.ClusterID, inputdef.CmdHideInputStatus, inputdef.HideInputStatusRequest{})
	invokeMounted(t, player, inputdef.ClusterID, inputdef.CmdRenameInput, inputdef.RenameInputRequest{Index: 1, Name: "A1"})
	if v := readMounted(t, player, inputdef.ClusterID, inputdef.AttrCurrentInput); v != uint8(2) {
		t.Errorf("CurrentInput %v", v)
	}
	invokeMounted(t, player, outputdef.ClusterID, outputdef.CmdSelectOutput, outputdef.SelectOutputRequest{Index: 3})
	invokeMounted(t, player, outputdef.ClusterID, outputdef.CmdRenameOutput, outputdef.RenameOutputRequest{Index: 1, Name: "CertTest"})
	if l := readMounted(t, player, outputdef.ClusterID, outputdef.AttrOutputList).(spec.List[outputdef.OutputInfoStruct]); l[0].Name != "CertTest" {
		t.Errorf("OutputList %+v", l)
	}
	if r := invokeMounted(t, player, targetdef.ClusterID, targetdef.CmdNavigateTarget, targetdef.NavigateTargetRequest{Target: 1}).(targetdef.NavigateTargetResponse); r.Status != targetdef.StatusSuccess {
		t.Errorf("NavigateTarget 1 %+v", r)
	}
	if r := invokeMounted(t, player, targetdef.ClusterID, targetdef.CmdNavigateTarget, targetdef.NavigateTargetRequest{Target: 0}).(targetdef.NavigateTargetResponse); r.Status != targetdef.StatusTargetNotFound {
		t.Errorf("NavigateTarget 0 %+v", r)
	}
	for _, key := range []keypaddef.CecKeyCodeEnum{9, 10, 33, 41} {
		if r := invokeMounted(t, player, keypaddef.ClusterID, keypaddef.CmdSendKey, keypaddef.SendKeyRequest{KeyCode: key}).(keypaddef.SendKeyResponse); r.Status != keypaddef.StatusSuccess {
			t.Errorf("SendKey %d %+v", key, r)
		}
	}
	if r := invokeMounted(t, player, keypaddef.ClusterID, keypaddef.CmdSendKey, keypaddef.SendKeyRequest{KeyCode: keypaddef.CecKeyCodePlay}).(keypaddef.SendKeyResponse); r.Status != keypaddef.StatusUnsupportedKey {
		t.Errorf("SendKey Play %+v", r)
	}
	if r := invokeMounted(t, player, launchdef.ClusterID, launchdef.CmdLaunchUrl, launchdef.LaunchUrlRequest{ContentUrl: "https://csa-iot.org/badauth"}).(launchdef.LauncherResponse); r.Status != launchdef.StatusAuthFailed {
		t.Errorf("LaunchURL badauth %+v", r)
	}
	if r := invokeMounted(t, player, launchdef.ClusterID, launchdef.CmdLaunchUrl, launchdef.LaunchUrlRequest{ContentUrl: "https://badurl"}).(launchdef.LauncherResponse); r.Status != launchdef.StatusUrlNotAvailable {
		t.Errorf("LaunchURL badurl %+v", r)
	}
	if r := invokeMounted(t, player, launchdef.ClusterID, launchdef.CmdLaunchContent, launchdef.LaunchContentRequest{
		Search: launchdef.ContentSearchStruct{ParameterList: []launchdef.ParameterStruct{{Type: 0, Value: "Gaby sHoffman"}}}, AutoPlay: true,
	}).(launchdef.LauncherResponse); r.Status != launchdef.StatusSuccess || r.Data == nil || *r.Data != "exampleData" {
		t.Errorf("LaunchContent %+v", r)
	}
	invokeMounted(t, player, lowpowerdef.ClusterID, lowpowerdef.CmdSleep, lowpowerdef.SleepRequest{})
	if r := invokeMounted(t, app, logindef.ClusterID, logindef.CmdGetSetupPin, logindef.GetSetupPinRequest{TempAccountIdentifier: "1111111111111111"}).(logindef.GetSetupPinResponse); r.SetupPin != mediaAppSetupPIN {
		t.Errorf("GetSetupPIN %+v", r)
	}
	invokeMounted(t, app, logindef.ClusterID, logindef.CmdLogin, logindef.LoginRequest{TempAccountIdentifier: "1111111111111111", SetupPin: mediaAppSetupPIN})
	invokeMounted(t, app, logindef.ClusterID, logindef.CmdLogout, logindef.LogoutRequest{})
}

// TestMediaPlatformMovesTheAppStatus: the player's ApplicationLauncher is
// the content app platform — LaunchApp, HideApp and StopApp of the test app
// move the app's ApplicationBasic.Status (APPLAUNCHER-3.7 to 3.9), an
// unknown app is AppNotAvailable, CurrentApp names the app's endpoint.
func TestMediaPlatformMovesTheAppStatus(t *testing.T) {
	_, player, app, _ := startMediaFleet(t)
	status := func() any { return readMounted(t, app, basicdef.ClusterID, basicdef.AttrStatus) }
	example := &launcherdef.ApplicationStruct{CatalogVendorId: mediaCatalogVendorID, ApplicationId: mediaAppID}
	if r := invokeMounted(t, player, launcherdef.ClusterID, launcherdef.CmdLaunchApp, launcherdef.LaunchAppRequest{Application: example}).(launcherdef.LauncherResponse); r.Status != launcherdef.StatusSuccess {
		t.Fatalf("LaunchApp %+v", r)
	}
	if status() != uint8(basicdef.ApplicationStatusActiveVisibleFocus) {
		t.Errorf("Status after LaunchApp %v", status())
	}
	cur := readMounted(t, player, launcherdef.ClusterID, launcherdef.AttrCurrentApp).(launcherdef.ApplicationEPStruct)
	if cur.Endpoint == nil || *cur.Endpoint != app.ID {
		t.Errorf("CurrentApp %+v, want the app's endpoint %d", cur, app.ID)
	}
	invokeMounted(t, player, launcherdef.ClusterID, launcherdef.CmdHideApp, launcherdef.HideAppRequest{Application: example})
	if status() != uint8(basicdef.ApplicationStatusActiveHidden) {
		t.Errorf("Status after HideApp %v", status())
	}
	invokeMounted(t, player, launcherdef.ClusterID, launcherdef.CmdStopApp, launcherdef.StopAppRequest{Application: example})
	if status() != uint8(basicdef.ApplicationStatusStopped) {
		t.Errorf("Status after StopApp %v", status())
	}
	other := &launcherdef.ApplicationStruct{CatalogVendorId: mediaCatalogVendorID, ApplicationId: "NonAvailableApp"}
	for _, cmd := range []struct {
		id  uint32
		req spec.Encodable
	}{
		{launcherdef.CmdLaunchApp, launcherdef.LaunchAppRequest{Application: other}},
		{launcherdef.CmdStopApp, launcherdef.StopAppRequest{Application: other}},
		{launcherdef.CmdHideApp, launcherdef.HideAppRequest{Application: other}},
	} {
		if r := invokeMounted(t, player, launcherdef.ClusterID, cmd.id, cmd.req).(launcherdef.LauncherResponse); r.Status != launcherdef.StatusAppNotAvailable {
			t.Errorf("0x%02X of an unknown app: %+v", cmd.id, r)
		}
	}
	// The content app's own launcher (ALOGIN-12.1's first step).
	if r := invokeMounted(t, app, launcherdef.ClusterID, launcherdef.CmdLaunchApp, launcherdef.LaunchAppRequest{Application: example}).(launcherdef.LauncherResponse); r.Status != launcherdef.StatusSuccess {
		t.Errorf("the app's LaunchApp %+v", r)
	}
	invokeMounted(t, app, launcherdef.ClusterID, launcherdef.CmdHideApp, launcherdef.HideAppRequest{})
	invokeMounted(t, app, launcherdef.ClusterID, launcherdef.CmdStopApp, launcherdef.StopAppRequest{})
	if status() != uint8(basicdef.ApplicationStatusStopped) {
		t.Errorf("Status after the app's StopApp %v", status())
	}
}
