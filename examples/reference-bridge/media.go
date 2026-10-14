// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/media"
	"github.com/SukramJ/go-fabric/cluster/spec"
	logindef "github.com/SukramJ/go-fabric/cluster/spec/accountlogin"
	basicdef "github.com/SukramJ/go-fabric/cluster/spec/applicationbasic"
	launcherdef "github.com/SukramJ/go-fabric/cluster/spec/applicationlauncher"
	outputdef "github.com/SukramJ/go-fabric/cluster/spec/audiooutput"
	channeldef "github.com/SukramJ/go-fabric/cluster/spec/channel"
	launchdef "github.com/SukramJ/go-fabric/cluster/spec/contentlauncher"
	keypaddef "github.com/SukramJ/go-fabric/cluster/spec/keypadinput"
	inputdef "github.com/SukramJ/go-fabric/cluster/spec/mediainput"
	mpdef "github.com/SukramJ/go-fabric/cluster/spec/mediaplayback"
	targetdef "github.com/SukramJ/go-fabric/cluster/spec/targetnavigator"
	woldef "github.com/SukramJ/go-fabric/cluster/spec/wakeonlan"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/node"
)

// --media (TESTING ONLY): two endpoints for the certification harness's
// media families (ADR 0022) — a video player and a content app — whose
// devices are simulations of connectedhomeip's tv-app at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20 (examples/tv-app/tv-common/,
// the managers the YAML cases were written against). Nothing plays: every
// port answers the way the tv-app's manager answers and moves the state
// the attributes report as it moves its own.
//
//   - "player" is a CastingVideoPlayer (tv-app endpoint 1, device type
//     ma_casting_videoplayer, tv-app.matter:4384-4385) stating
//     ContentAppPlatform and PhysicalInputs: OnOff, MediaPlayback (AS, VS),
//     KeypadInput (NV, LK, NK), ContentLauncher (CS, UP), ApplicationLauncher
//     (AP), MediaInput (NU), AudioOutput (NU), Channel (CL, LI),
//     TargetNavigator, LowPower and WakeOnLan.
//   - "app" is a ContentApp (tv-app endpoint 3, ma_contentapp,
//     tv-app.matter's endpoint 3): ApplicationBasic, ApplicationLauncher
//     (no AP), AccountLogin and KeypadInput — the test suite's app, catalog
//     vendor 123 and application "exampleid" (AppTv.cpp
//     ConvertToPlatformCatalogVendorApp and InstallContentApp: "Vendor1",
//     version "Version1", setup PIN "34567890").
//
// The player's ApplicationLauncher is the content app platform: LaunchApp,
// StopApp and HideApp of the app move its ApplicationBasic.Status, as
// chip's application-launcher-server does through the platform
// (application-launcher-server.cpp:256-275, :350-371).
//
// The app pipe moves the transport: {"Name":"MediaPlaybackState",
// "Operation":"Playing"|"Paused"|"NotPlaying"|"Buffering"} (a command of
// this daemon; the tv-app has no app pipe).

// mediaAppKey is the content app's StableKey (bridged("contentapp", …)).
const mediaAppKey endpoint.StringKey = "demo:contentapp:1"

// The test suite's content app (AppTv.cpp:548-551, :667-669).
const (
	mediaCatalogVendorID uint16 = 123
	mediaAppID                  = "exampleid"
	mediaAppVendorID     uint16 = 1
	mediaAppProductID    uint16 = 11
	mediaAppSetupPIN            = "34567890"
)

// demoMedia is the --media pair: the player's and the app's servers and
// the simulations behind them.
type demoMedia struct {
	logger *slog.Logger

	playback  *media.PlaybackServer
	keypad    *media.KeypadServer
	launcher  *media.ContentLauncherServer
	platform  *media.AppLauncherServer
	inputs    *media.InputsServer
	outputs   *media.OutputsServer
	channels  *media.ChannelsServer
	targets   *media.TargetsServer
	lowPower  *media.LowPowerServer
	wakeOnLan *media.WakeOnLanServer

	app         *media.AppBasicServer
	appKeypad   *media.KeypadServer
	appLauncher *media.AppLauncherServer
	appLogin    *media.AccountLoginServer

	mu sync.Mutex
	// appEndpoint resolves the app's endpoint number; set by
	// attachNodeProviders.
	appEndpoint func() (uint16, bool)
}

func newDemoMedia(logger *slog.Logger) (*demoMedia, error) {
	m := &demoMedia{logger: logger}
	if err := m.buildPlayer(); err != nil {
		return nil, fmt.Errorf("media player: %w", err)
	}
	if err := m.buildApp(); err != nil {
		return nil, fmt.Errorf("content app: %w", err)
	}
	return m, nil
}

//nolint:funlen // one server per cluster of the player
func (m *demoMedia) buildPlayer() error {
	var err error
	// MediaPlaybackManager.h:74-123: Playing at speed 0 (the CI cases'
	// expectation, the header says), position 0, duration 80000.
	duration, start := uint64(80000), uint64(0)
	if m.playback, err = media.NewPlayback(media.PlaybackConfig{
		Port:     playerTransport{},
		Features: mpdef.FeatureAdvancedSeek | mpdef.FeatureVariableSpeed,
		Initial: media.PlaybackState{
			CurrentState:    mpdef.PlaybackStatePlaying,
			StartTime:       &start,
			Duration:        &duration,
			SampledPosition: &mpdef.PlaybackPositionStruct{UpdatedAt: 0, Position: spec.Nullable[uint64]{Value: 0}},
			SeekRangeStart:  &start,
			SeekRangeEnd:    &duration,
		},
	}); err != nil {
		return err
	}
	// tv-app.matter:4515 (KeypadInput featureMap 0x0007).
	if m.keypad, err = media.NewKeypad(media.KeypadConfig{
		Port:     tvKeys{},
		Features: keypaddef.FeatureNavigationKeyCodes | keypaddef.FeatureLocationKeys | keypaddef.FeatureNumberKeys,
	}); err != nil {
		return err
	}
	// ContentLauncherManager.h:47: accept header {"example","example"},
	// no streaming protocol.
	if m.launcher, err = media.NewContentLauncher(media.ContentLauncherConfig{
		Port:     tvLauncher{},
		Features: launchdef.FeatureContentSearch | launchdef.FeatureUrlPlayback,
		Initial:  media.ContentLauncherState{AcceptHeader: []string{"example", "example"}},
	}); err != nil {
		return err
	}
	// ApplicationLauncherManager.cpp:27: catalogs 123 and 456.
	if m.platform, err = media.NewAppLauncher(media.AppLauncherConfig{
		Port:       &tvPlatform{media: m},
		Features:   launcherdef.FeatureApplicationPlatform,
		Attributes: []uint32{launcherdef.AttrCurrentApp},
		Initial:    media.AppLauncherState{CatalogList: []uint16{123, 456}},
	}); err != nil {
		return err
	}
	// MediaInputManager.cpp:25-38: two HDMI inputs, 1 current.
	if m.inputs, err = media.NewInputs(media.InputsConfig{
		Port:     tvInputs{},
		Features: inputdef.FeatureNameUpdates,
		Initial: media.InputState{CurrentInput: 1, InputList: []inputdef.InputInfoStruct{
			{Index: 1, InputType: inputdef.InputTypeHdmi, Name: "HDMI", Description: "High-Definition Multimedia Interface"},
			{Index: 2, InputType: inputdef.InputTypeHdmi, Name: "HDMI", Description: "High-Definition Multimedia Interface"},
		}},
	}); err != nil {
		return err
	}
	// AudioOutputManager.cpp:27-40: three HDMI outputs, 1 current.
	if m.outputs, err = media.NewOutputs(media.OutputsConfig{
		Port:     tvOutputs{},
		Features: outputdef.FeatureNameUpdates,
		Initial: media.OutputState{CurrentOutput: 1, OutputList: []outputdef.OutputInfoStruct{
			{Index: 1, OutputType: outputdef.OutputTypeHdmi, Name: "HDMI"},
			{Index: 2, OutputType: outputdef.OutputTypeHdmi, Name: "HDMI"},
			{Index: 3, OutputType: outputdef.OutputTypeHdmi, Name: "HDMI"},
		}},
	}); err != nil {
		return err
	}
	// ChannelManager.cpp:30-65 and HandleGetLineup :120-129; the first
	// three of its channels, 6.0 current.
	list := tvChannels()
	if m.channels, err = media.NewChannels(media.ChannelsConfig{
		Port:       tvTuner{},
		Features:   channeldef.FeatureChannelList | channeldef.FeatureLineupInfo,
		Attributes: []uint32{channeldef.AttrCurrentChannel},
		Initial: media.ChannelState{
			ChannelList: list,
			Lineup: &channeldef.LineupInfoStruct{
				OperatorName: "Comcast", LineupName: ptr("Comcast King County"), PostalCode: ptr("98052"),
				LineupInfoType: channeldef.LineupInfoTypeMso,
			},
			CurrentChannel: &list[0],
		},
	}); err != nil {
		return err
	}
	// TargetNavigatorManager.h:35, :46: two targets, none current (0).
	if m.targets, err = media.NewTargets(media.TargetsConfig{
		Port:       tvTargets{},
		Attributes: []uint32{targetdef.AttrCurrentTarget},
		Initial: media.TargetState{TargetList: []targetdef.TargetInfoStruct{
			{Identifier: 1, Name: "exampleName"}, {Identifier: 2, Name: "exampleName"},
		}},
	}); err != nil {
		return err
	}
	if m.lowPower, err = media.NewLowPower(media.LowPowerConfig{Port: tvSleep{logger: m.logger}}); err != nil {
		return err
	}
	// WakeOnLanManager.cpp:52-65 serves the primary MAC as 12 hex digits;
	// the daemon has no radio, so a locally administered address.
	m.wakeOnLan, err = media.NewWakeOnLan(media.WakeOnLanConfig{
		Attributes: []uint32{woldef.AttrMacAddress},
		Initial:    media.WakeOnLanState{MacAddress: "02005E102002"},
	})
	return err
}

func (m *demoMedia) buildApp() error {
	var err error
	if m.app, err = media.NewAppBasic(media.AppBasicConfig{
		Attributes: []uint32{basicdef.AttrVendorName, basicdef.AttrVendorId, basicdef.AttrProductId},
		Initial: media.AppBasicState{
			VendorName: "Vendor1", VendorID: mediaAppVendorID, ApplicationName: mediaAppID, ProductID: mediaAppProductID,
			Application:        &basicdef.ApplicationStruct{CatalogVendorId: mediaCatalogVendorID, ApplicationId: mediaAppID},
			Status:             basicdef.ApplicationStatusStopped,
			ApplicationVersion: "Version1",
			AllowedVendorList:  []uint16{mediaAppVendorID},
		},
	}); err != nil {
		return err
	}
	// tv-app.matter's endpoint 3: KeypadInput featureMap 0.
	if m.appKeypad, err = media.NewKeypad(media.KeypadConfig{Port: tvKeys{}}); err != nil {
		return err
	}
	if m.appLauncher, err = media.NewAppLauncher(media.AppLauncherConfig{Port: &tvApp{media: m}}); err != nil {
		return err
	}
	m.appLogin, err = media.NewAccountLogin(media.AccountLoginConfig{Port: tvLogin{}})
	return err
}

// devices builds the two media endpoints.
func (m *demoMedia) devices() ([]node.Endpoint, error) {
	playerOpts := bridged("player", "Living Room TV")
	playerOpts.Conditions = []string{"ContentAppPlatform", "PhysicalInputs"}
	player, err := device.NewCastingVideoPlayer(playerOpts, serve([]contract.ClusterServer{
		m.playback, m.keypad, m.launcher, m.platform, m.inputs, m.outputs, m.channels, m.targets, m.lowPower, m.wakeOnLan,
	})...)
	if err != nil {
		return nil, err
	}
	app, err := device.NewContentApp(bridged("contentapp", "Example App"),
		serve([]contract.ClusterServer{m.app, m.appKeypad, m.appLauncher, m.appLogin})...)
	if err != nil {
		return nil, err
	}
	return []node.Endpoint{player, app}, nil
}

// setAppEndpoint hands the platform the resolver of the app's endpoint
// number (attachNodeProviders).
func (m *demoMedia) setAppEndpoint(resolve func() (uint16, bool)) {
	m.mu.Lock()
	m.appEndpoint = resolve
	m.mu.Unlock()
}

// appStatus moves the content app's ApplicationBasic.Status.
func (m *demoMedia) appStatus(s basicdef.ApplicationStatusEnum) error {
	st := m.app.State()
	st.Status = s
	return m.app.SetState(st)
}

// currentApp is the platform's CurrentApp for the test app: its
// application and endpoint (the endpoint left out until it is known).
func (m *demoMedia) currentApp() *launcherdef.ApplicationEPStruct {
	cur := &launcherdef.ApplicationEPStruct{Application: launcherdef.ApplicationStruct{CatalogVendorId: mediaCatalogVendorID, ApplicationId: mediaAppID}}
	m.mu.Lock()
	resolve := m.appEndpoint
	m.mu.Unlock()
	if resolve != nil {
		if id, ok := resolve(); ok {
			cur.Endpoint = &id
		}
	}
	return cur
}

// setPlaybackState is the app pipe's MediaPlaybackState.
func (m *demoMedia) setPlaybackState(state string) error {
	states := map[string]mpdef.PlaybackStateEnum{
		"Playing": mpdef.PlaybackStatePlaying, "Paused": mpdef.PlaybackStatePaused,
		"NotPlaying": mpdef.PlaybackStateNotPlaying, "Buffering": mpdef.PlaybackStateBuffering,
	}
	s, ok := states[state]
	if !ok {
		return fmt.Errorf("MediaPlaybackState %q: want Playing, Paused, NotPlaying or Buffering", state)
	}
	st := m.playback.State()
	st.CurrentState = s
	return m.playback.SetState(st)
}

// --- the tv-app managers -------------------------------------------------

// tvResponse is the PlaybackResponse every tv-app transport command
// answers on success (MediaPlaybackManager.cpp:130-132).
func tvResponse(status mpdef.StatusEnum) mpdef.PlaybackResponse {
	return mpdef.PlaybackResponse{Status: status, Data: ptr("data response")}
}

// The tv-app's speed limits (MediaPlaybackManager.h:125-126).
const (
	tvMaxForwardSpeed float32 = 10
	tvMaxRewindSpeed  float32 = -10
)

// playerTransport is MediaPlaybackManager.cpp.
type playerTransport struct{}

func position(v uint64) *mpdef.PlaybackPositionStruct {
	return &mpdef.PlaybackPositionStruct{Position: spec.Nullable[uint64]{Value: v}}
}

func positionOf(st *media.PlaybackState) uint64 {
	if st.SampledPosition == nil || st.SampledPosition.Position.Null {
		return 0
	}
	return st.SampledPosition.Position.Value
}

// Play is HandlePlay (:124-134).
func (playerTransport) Play(_ context.Context, st *media.PlaybackState) (mpdef.PlaybackResponse, error) {
	st.CurrentState, st.PlaybackSpeed = mpdef.PlaybackStatePlaying, 1
	return tvResponse(mpdef.StatusSuccess), nil
}

// Pause is HandlePause (:136-146).
func (playerTransport) Pause(_ context.Context, st *media.PlaybackState) (mpdef.PlaybackResponse, error) {
	st.CurrentState, st.PlaybackSpeed = mpdef.PlaybackStatePaused, 0
	return tvResponse(mpdef.StatusSuccess), nil
}

// Stop is HandleStop (:148-159).
func (playerTransport) Stop(_ context.Context, st *media.PlaybackState) (mpdef.PlaybackResponse, error) {
	st.CurrentState, st.PlaybackSpeed, st.SampledPosition = mpdef.PlaybackStateNotPlaying, 0, position(0)
	return tvResponse(mpdef.StatusSuccess), nil
}

// StartOver is HandleStartOver (:294-303).
func (playerTransport) StartOver(_ context.Context, st *media.PlaybackState) (mpdef.PlaybackResponse, error) { //nolint:unparam // the cluster/media port signature
	st.SampledPosition = position(0)
	return tvResponse(mpdef.StatusSuccess), nil
}

// Previous is HandlePrevious (:189-200).
func (playerTransport) Previous(_ context.Context, st *media.PlaybackState) (mpdef.PlaybackResponse, error) { //nolint:unparam // the cluster/media port signature
	st.CurrentState, st.PlaybackSpeed, st.SampledPosition = mpdef.PlaybackStatePlaying, 1, position(0)
	return tvResponse(mpdef.StatusSuccess), nil
}

// Next is HandleNext (:281-292).
func (playerTransport) Next(_ context.Context, st *media.PlaybackState) (mpdef.PlaybackResponse, error) { //nolint:unparam // the cluster/media port signature
	st.CurrentState, st.PlaybackSpeed, st.SampledPosition = mpdef.PlaybackStatePlaying, 1, position(0)
	return tvResponse(mpdef.StatusSuccess), nil
}

// FastForward is HandleFastForward (:161-187).
func (playerTransport) FastForward(_ context.Context, st *media.PlaybackState, _ mpdef.FastForwardRequest) (mpdef.PlaybackResponse, error) { //nolint:unparam // the cluster/media port signature
	if st.PlaybackSpeed == tvMaxForwardSpeed {
		return tvResponse(mpdef.StatusSpeedOutOfRange), nil
	}
	st.CurrentState = mpdef.PlaybackStatePlaying
	if st.PlaybackSpeed <= 0 {
		st.PlaybackSpeed = 1
	} else {
		st.PlaybackSpeed = min(st.PlaybackSpeed*2, tvMaxForwardSpeed)
	}
	return tvResponse(mpdef.StatusSuccess), nil
}

// Rewind is HandleRewind (:202-228).
func (playerTransport) Rewind(_ context.Context, st *media.PlaybackState, _ mpdef.RewindRequest) (mpdef.PlaybackResponse, error) { //nolint:unparam // the cluster/media port signature
	if st.PlaybackSpeed == tvMaxRewindSpeed {
		return tvResponse(mpdef.StatusSpeedOutOfRange), nil
	}
	st.CurrentState = mpdef.PlaybackStatePlaying
	if st.PlaybackSpeed >= 0 {
		st.PlaybackSpeed = -1
	} else {
		st.PlaybackSpeed = max(st.PlaybackSpeed*2, tvMaxRewindSpeed)
	}
	return tvResponse(mpdef.StatusSuccess), nil
}

// SkipForward is HandleSkipForward (:245-257): up to the duration.
func (playerTransport) SkipForward(_ context.Context, st *media.PlaybackState, req mpdef.SkipForwardRequest) (mpdef.PlaybackResponse, error) { //nolint:unparam // the cluster/media port signature
	next := positionOf(st) + req.DeltaPositionMilliseconds
	if st.Duration != nil {
		next = min(next, *st.Duration)
	}
	st.SampledPosition = position(next)
	return tvResponse(mpdef.StatusSuccess), nil
}

// SkipBackward is HandleSkipBackward (:230-243): down to 0.
func (playerTransport) SkipBackward(_ context.Context, st *media.PlaybackState, req mpdef.SkipBackwardRequest) (mpdef.PlaybackResponse, error) { //nolint:unparam // the cluster/media port signature
	cur := positionOf(st)
	next := uint64(0)
	if cur > req.DeltaPositionMilliseconds {
		next = cur - req.DeltaPositionMilliseconds
	}
	st.SampledPosition = position(next)
	return tvResponse(mpdef.StatusSuccess), nil
}

// Seek is HandleSeek (:259-279): SeekOutOfRange past the duration.
func (playerTransport) Seek(_ context.Context, st *media.PlaybackState, req mpdef.SeekRequest) (mpdef.PlaybackResponse, error) { //nolint:unparam // the cluster/media port signature
	if st.Duration != nil && req.Position > *st.Duration {
		return tvResponse(mpdef.StatusSeekOutOfRange), nil
	}
	st.SampledPosition = position(req.Position)
	return tvResponse(mpdef.StatusSuccess), nil
}

// tvKeys is KeypadInputManager.cpp:26-98: the navigation, menu and number
// keys succeed, every other key is UnsupportedKey.
type tvKeys struct{}

func (tvKeys) SendKey(_ context.Context, req keypaddef.SendKeyRequest) (keypaddef.SendKeyResponse, error) {
	if tvSupportedKeys[req.KeyCode] {
		return keypaddef.SendKeyResponse{Status: keypaddef.StatusSuccess}, nil
	}
	return keypaddef.SendKeyResponse{Status: keypaddef.StatusUnsupportedKey}, nil
}

// tvSupportedKeys are the keys KeypadInputManager.cpp:31-95 answers
// Success.
var tvSupportedKeys = map[keypaddef.CecKeyCodeEnum]bool{
	keypaddef.CecKeyCodeUp: true, keypaddef.CecKeyCodeDown: true, keypaddef.CecKeyCodeLeft: true, keypaddef.CecKeyCodeRight: true,
	keypaddef.CecKeyCodeSelect: true, keypaddef.CecKeyCodeBackward: true, keypaddef.CecKeyCodeExit: true, keypaddef.CecKeyCodeRootMenu: true,
	keypaddef.CecKeyCodeSetupMenu: true, keypaddef.CecKeyCodeEnter: true, keypaddef.CecKeyCodeNumber0OrNumber10: true,
	keypaddef.CecKeyCodeNumbers1: true, keypaddef.CecKeyCodeNumbers2: true, keypaddef.CecKeyCodeNumbers3: true, keypaddef.CecKeyCodeNumbers4: true,
	keypaddef.CecKeyCodeNumbers5: true, keypaddef.CecKeyCodeNumbers6: true, keypaddef.CecKeyCodeNumbers7: true, keypaddef.CecKeyCodeNumbers8: true,
	keypaddef.CecKeyCodeNumbers9: true,
}

// tvLauncher is ContentLauncherManager.cpp: LaunchContent answers Success
// with "exampleData" whatever it matched (:111-114), LaunchURL
// URLNotAvailable for https://badurl, AuthFailed for
// https://csa-iot.org/badauth, else Success (:126-138).
type tvLauncher struct{}

func (tvLauncher) LaunchContent(context.Context, *media.ContentLauncherState, launchdef.LaunchContentRequest) (launchdef.LauncherResponse, error) {
	return launchdef.LauncherResponse{Status: launchdef.StatusSuccess, Data: ptr("exampleData")}, nil
}

func (tvLauncher) LaunchURL(_ context.Context, _ *media.ContentLauncherState, req launchdef.LaunchUrlRequest) (launchdef.LauncherResponse, error) {
	resp := launchdef.LauncherResponse{Status: launchdef.StatusSuccess, Data: ptr("exampleData")}
	switch req.ContentUrl {
	case "https://badurl":
		resp.Status = launchdef.StatusUrlNotAvailable
	case "https://csa-iot.org/badauth":
		resp.Status = launchdef.StatusAuthFailed
	}
	return resp, nil
}

// tvPlatform is the player's ApplicationLauncher on the content app
// platform: application-launcher-server.cpp with
// CHIP_DEVICE_CONFIG_APP_PLATFORM_ENABLED — the test app or
// AppNotAvailable (:256-267, :350-361), the app's status moved
// (ActiveVisibleFocus on launch through the platform's current app, Stopped
// on stop :366, ActiveHidden on hide), the launcher manager's response
// with data "data" (ApplicationLauncherManager.cpp:37-74).
type tvPlatform struct{ media *demoMedia }

func (p *tvPlatform) known(a *launcherdef.ApplicationStruct) bool {
	return a != nil && a.CatalogVendorId == mediaCatalogVendorID && a.ApplicationId == mediaAppID
}

func launched() launcherdef.LauncherResponse {
	return launcherdef.LauncherResponse{Status: launcherdef.StatusSuccess, Data: ptr([]byte("data"))}
}

func (p *tvPlatform) LaunchApp(_ context.Context, st *media.AppLauncherState, req launcherdef.LaunchAppRequest) (launcherdef.LauncherResponse, error) {
	if !p.known(req.Application) {
		return launcherdef.LauncherResponse{Status: launcherdef.StatusAppNotAvailable}, nil
	}
	st.CurrentApp = p.media.currentApp()
	return launched(), p.media.appStatus(basicdef.ApplicationStatusActiveVisibleFocus)
}

func (p *tvPlatform) StopApp(_ context.Context, st *media.AppLauncherState, req launcherdef.StopAppRequest) (launcherdef.LauncherResponse, error) {
	if !p.known(req.Application) {
		return launcherdef.LauncherResponse{Status: launcherdef.StatusAppNotAvailable}, nil
	}
	st.CurrentApp = nil
	return launched(), p.media.appStatus(basicdef.ApplicationStatusStopped)
}

func (p *tvPlatform) HideApp(_ context.Context, _ *media.AppLauncherState, req launcherdef.HideAppRequest) (launcherdef.LauncherResponse, error) {
	if !p.known(req.Application) {
		return launcherdef.LauncherResponse{Status: launcherdef.StatusAppNotAvailable}, nil
	}
	return launched(), p.media.appStatus(basicdef.ApplicationStatusActiveHidden)
}

// tvApp is the content app's own ApplicationLauncher: without the platform
// feature the server sets the app's status and calls the launcher manager
// (application-launcher-server.cpp:277-306, :373-395), which answers
// Success with "data".
type tvApp struct{ media *demoMedia }

func (a *tvApp) LaunchApp(context.Context, *media.AppLauncherState, launcherdef.LaunchAppRequest) (launcherdef.LauncherResponse, error) {
	return launched(), a.media.appStatus(basicdef.ApplicationStatusActiveVisibleFocus)
}

func (a *tvApp) StopApp(context.Context, *media.AppLauncherState, launcherdef.StopAppRequest) (launcherdef.LauncherResponse, error) {
	return launched(), a.media.appStatus(basicdef.ApplicationStatusStopped)
}

func (a *tvApp) HideApp(context.Context, *media.AppLauncherState, launcherdef.HideAppRequest) (launcherdef.LauncherResponse, error) {
	return launched(), a.media.appStatus(basicdef.ApplicationStatusActiveHidden)
}

// tvInputs is MediaInputManager.cpp: select and rename a listed input
// (:57-71, :93-114), the status display logged (:73-91).
type tvInputs struct{}

func (tvInputs) SelectInput(_ context.Context, st *media.InputState, req inputdef.SelectInputRequest) error {
	for _, in := range st.InputList {
		if in.Index == req.Index {
			st.CurrentInput = req.Index
			return nil
		}
	}
	return fmt.Errorf("no input %d", req.Index)
}

func (tvInputs) ShowInputStatus(context.Context, *media.InputState) error { return nil }

func (tvInputs) HideInputStatus(context.Context, *media.InputState) error { return nil }

func (tvInputs) RenameInput(_ context.Context, st *media.InputState, req inputdef.RenameInputRequest) error {
	for i := range st.InputList {
		if st.InputList[i].Index == req.Index {
			st.InputList[i].Name = req.Name
			return nil
		}
	}
	return fmt.Errorf("no input %d", req.Index)
}

// tvOutputs is AudioOutputManager.cpp: select and rename a listed output
// (:59-96).
type tvOutputs struct{}

func (tvOutputs) SelectOutput(_ context.Context, st *media.OutputState, req outputdef.SelectOutputRequest) error {
	for _, out := range st.OutputList {
		if out.Index == req.Index {
			st.CurrentOutput = req.Index
			return nil
		}
	}
	return fmt.Errorf("no output %d", req.Index)
}

func (tvOutputs) RenameOutput(_ context.Context, st *media.OutputState, req outputdef.RenameOutputRequest) error {
	for i := range st.OutputList {
		if st.OutputList[i].Index == req.Index {
			st.OutputList[i].Name = req.Name
			return nil
		}
	}
	return fmt.Errorf("no output %d", req.Index)
}

// tvChannels are ChannelManager.cpp:32-54's first three channels.
func tvChannels() []channeldef.ChannelInfoStruct {
	return []channeldef.ChannelInfoStruct{
		{MajorNumber: 6, MinorNumber: 0, Name: ptr("ABC"), CallSign: ptr("KAAL-TV"), AffiliateCallSign: ptr("KAAL")},
		{MajorNumber: 9, MinorNumber: 1, Name: ptr("PBS"), CallSign: ptr("KCTS-TV"), AffiliateCallSign: ptr("KCTS")},
		{MajorNumber: 9, MinorNumber: 2, Name: ptr("PBS Kids"), CallSign: ptr("KCTS-TV"), AffiliateCallSign: ptr("KCTS")},
	}
}

// tvTuner is ChannelManager.cpp: ChangeChannel by name, call sign or
// "major.minor" (:136-192), ChangeChannelByNumber to a listed channel
// (:194-214; FAILURE otherwise, channel-server.cpp:259-262), SkipChannel
// wrapping around the list (:216-232).
type tvTuner struct{}

func (tvTuner) ChangeChannel(_ context.Context, st *media.ChannelState, req channeldef.ChangeChannelRequest) (channeldef.ChangeChannelResponse, error) {
	var hits []int
	for i, c := range st.ChannelList {
		number := fmt.Sprintf("%d.%d", c.MajorNumber, c.MinorNumber)
		if req.Match == number || (c.Name != nil && *c.Name == req.Match) ||
			(c.CallSign != nil && *c.CallSign == req.Match) || (c.AffiliateCallSign != nil && *c.AffiliateCallSign == req.Match) {
			hits = append(hits, i)
		}
	}
	switch len(hits) {
	case 0:
		return channeldef.ChangeChannelResponse{Status: channeldef.StatusNoMatches}, nil
	case 1:
		c := st.ChannelList[hits[0]]
		st.CurrentChannel = &c
		return channeldef.ChangeChannelResponse{Status: channeldef.StatusSuccess, Data: ptr("data response")}, nil
	}
	return channeldef.ChangeChannelResponse{Status: channeldef.StatusMultipleMatches}, nil
}

func (tvTuner) ChangeChannelByNumber(_ context.Context, st *media.ChannelState, req channeldef.ChangeChannelByNumberRequest) error {
	for _, c := range st.ChannelList {
		if c.MajorNumber == req.MajorNumber && c.MinorNumber == req.MinorNumber {
			st.CurrentChannel = &c
			return nil
		}
	}
	return fmt.Errorf("no channel %d.%d", req.MajorNumber, req.MinorNumber)
}

func (tvTuner) SkipChannel(_ context.Context, st *media.ChannelState, req channeldef.SkipChannelRequest) error {
	n := len(st.ChannelList)
	if n == 0 {
		return errors.New("no channels")
	}
	cur := 0
	if st.CurrentChannel != nil {
		for i, c := range st.ChannelList {
			if c.MajorNumber == st.CurrentChannel.MajorNumber && c.MinorNumber == st.CurrentChannel.MinorNumber {
				cur = i
			}
		}
	}
	next := (cur + int(req.Count)) % n
	if next < 0 {
		next += n
	}
	c := st.ChannelList[next]
	st.CurrentChannel = &c
	return nil
}

// tvTargets is TargetNavigatorManager.cpp:58-74: TargetNotFound with data
// "error" for 0 or past the list, else the target current.
type tvTargets struct{}

func (tvTargets) NavigateTarget(_ context.Context, st *media.TargetState, req targetdef.NavigateTargetRequest) (targetdef.NavigateTargetResponse, error) {
	if req.Target == 0 || int(req.Target) > len(st.TargetList) {
		return targetdef.NavigateTargetResponse{Status: targetdef.StatusTargetNotFound, Data: ptr("error")}, nil
	}
	st.CurrentTarget = req.Target
	return targetdef.NavigateTargetResponse{Status: targetdef.StatusSuccess, Data: ptr("data response")}, nil
}

// tvSleep is LowPowerManager.cpp:21-26: logged, SUCCESS.
type tvSleep struct{ logger *slog.Logger }

func (s tvSleep) Sleep(context.Context) error {
	if s.logger != nil {
		s.logger.Info("media.lowpower.sleep")
	}
	return nil
}

// tvLogin is AccountLoginManager.cpp with the test app's PIN: GetSetupPIN
// answers it (:56-66), Login succeeds with it (:31-47), Logout always
// (:49-54).
type tvLogin struct{}

func (tvLogin) GetSetupPIN(context.Context, logindef.GetSetupPinRequest) (logindef.GetSetupPinResponse, error) {
	return logindef.GetSetupPinResponse{SetupPin: mediaAppSetupPIN}, nil
}

func (tvLogin) Login(_ context.Context, req logindef.LoginRequest) error {
	if req.SetupPin != mediaAppSetupPIN {
		return errors.New("wrong setup PIN")
	}
	return nil
}

func (tvLogin) Logout(context.Context, logindef.LogoutRequest) error { return nil }
