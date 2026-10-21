// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

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
	targetdef "github.com/SukramJ/go-fabric/cluster/spec/targetnavigator"
	woldef "github.com/SukramJ/go-fabric/cluster/spec/wakeonlan"
	"github.com/SukramJ/go-fabric/device"
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
//     ContentAppPlatform and PhysicalInputs: OnOff, MediaPlayback (AS, VS, TT, AT, AA),
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
// Each cluster is declared (<cluster>def.Declare) with the manager as its
// port and the manager's state as its Initial: the device kit mounts the
// module's media server on it, and the state the daemon moves on its own
// goes through the device's typed accessors.
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
const mediaAppKey device.StringKey = "demo:contentapp:1"

// The test suite's content app (AppTv.cpp:548-551, :667-669).
const (
	mediaCatalogVendorID uint16 = 123
	mediaAppID                  = "exampleid"
	mediaAppVendorID     uint16 = 1
	mediaAppProductID    uint16 = 11
	mediaAppSetupPIN            = "34567890"
)

// demoMedia is the --media pair: the player and the app, and the
// simulations behind their ports.
type demoMedia struct {
	logger *slog.Logger

	player *device.CastingVideoPlayer
	app    *device.ContentApp

	mu sync.Mutex
	// appEndpoint resolves the app's endpoint number; set by
	// attachNodeProviders.
	appEndpoint func() (uint16, bool)
}

func newDemoMedia(logger *slog.Logger) (*demoMedia, error) {
	m := &demoMedia{logger: logger}
	var err error
	if m.player, err = m.newPlayer(); err != nil {
		return nil, fmt.Errorf("media player: %w", err)
	}
	if m.app, err = m.newApp(); err != nil {
		return nil, fmt.Errorf("content app: %w", err)
	}
	return m, nil
}

//nolint:funlen // one declaration per cluster of the player
func (m *demoMedia) newPlayer() (*device.CastingVideoPlayer, error) {
	// MediaPlaybackManager.h:74-123: Playing at speed 0 (the CI cases'
	// expectation, the header says), position 0, duration 80000.
	duration, start, speed := uint64(80000), uint64(0), float32(0)
	playing := mpdef.PlaybackStatePlaying
	audio, text := tvAudioTracks(), tvTextTracks()
	// ContentLauncherManager.h:47: accept header {"example","example"},
	// no streaming protocol.
	header, protocols := []string{"example", "example"}, launchdef.SupportedProtocolsBitmap(0)
	// ApplicationLauncherManager.cpp:27: catalogs 123 and 456.
	catalogs := []uint16{123, 456}
	// MediaInputManager.cpp:25-38: two HDMI inputs, 1 current.
	inputs, currentInput := []inputdef.InputInfoStruct{
		{Index: 1, InputType: inputdef.InputTypeHdmi, Name: "HDMI", Description: "High-Definition Multimedia Interface"},
		{Index: 2, InputType: inputdef.InputTypeHdmi, Name: "HDMI", Description: "High-Definition Multimedia Interface"},
	}, uint8(1)
	// AudioOutputManager.cpp:27-40: three HDMI outputs, 1 current.
	outputs, currentOutput := []outputdef.OutputInfoStruct{
		{Index: 1, OutputType: outputdef.OutputTypeHdmi, Name: "HDMI"},
		{Index: 2, OutputType: outputdef.OutputTypeHdmi, Name: "HDMI"},
		{Index: 3, OutputType: outputdef.OutputTypeHdmi, Name: "HDMI"},
	}, uint8(1)
	// ChannelManager.cpp:30-65 and HandleGetLineup :120-129; the first
	// three of its channels, 6.0 current.
	list := tvChannels()
	lineup := &channeldef.LineupInfoStruct{
		OperatorName: "Comcast", LineupName: ptr("Comcast King County"), PostalCode: ptr("98052"),
		LineupInfoType: channeldef.LineupInfoTypeMso,
	}
	// TargetNavigatorManager.h:35, :46: two targets, none current (0).
	targets, currentTarget := []targetdef.TargetInfoStruct{
		{Identifier: 1, Name: "exampleName"}, {Identifier: 2, Name: "exampleName"},
	}, uint8(0)
	// WakeOnLanManager.cpp:52-65 serves the primary MAC as 12 hex digits;
	// the daemon has no radio, so a locally administered address.
	mac := "02005E102002"

	opts := bridged("player", "Living Room TV")
	opts.Conditions = []string{"ContentAppPlatform", "PhysicalInputs"}
	return device.NewCastingVideoPlayer(opts,
		mpdef.Declare{
			Playback: playerTransport{},
			// tv-app.matter:4475 (featureMap 0x001F: AS, VS, TT, AT, AA).
			Features: mpdef.FeatureAdvancedSeek | mpdef.FeatureVariableSpeed | mpdef.FeatureTextTracks |
				mpdef.FeatureAudioTracks | mpdef.FeatureAudioAdvance,
			Initial: mpdef.Patch{
				CurrentState:         &playing,
				StartTime:            &start,
				Duration:             &duration,
				SampledPosition:      &mpdef.PlaybackPositionStruct{UpdatedAt: 0, Position: spec.Nullable[uint64]{Value: 0}},
				PlaybackSpeed:        &speed,
				SeekRangeStart:       &start,
				SeekRangeEnd:         &duration,
				ActiveAudioTrack:     &audio[0],
				AvailableAudioTracks: &audio,
				AvailableTextTracks:  &text,
				Null:                 []uint32{mpdef.AttrActiveTextTrack},
			},
		},
		// tv-app.matter:4515 (KeypadInput featureMap 0x0007).
		keypaddef.Declare{
			Keypad:   tvKeys{},
			Features: keypaddef.FeatureNavigationKeyCodes | keypaddef.FeatureLocationKeys | keypaddef.FeatureNumberKeys,
		},
		launchdef.Declare{
			ContentLauncher: tvLauncher{},
			Features:        launchdef.FeatureContentSearch | launchdef.FeatureUrlPlayback,
			Initial:         launchdef.Patch{AcceptHeader: &header, SupportedStreamingProtocols: &protocols},
		},
		launcherdef.Declare{
			AppLauncher: &tvPlatform{media: m},
			Features:    launcherdef.FeatureApplicationPlatform,
			Initial:     launcherdef.Patch{CatalogList: &catalogs, Null: []uint32{launcherdef.AttrCurrentApp}},
		},
		inputdef.Declare{
			Inputs: tvInputs{}, Features: inputdef.FeatureNameUpdates,
			Initial: inputdef.Patch{InputList: &inputs, CurrentInput: &currentInput},
		},
		outputdef.Declare{
			Outputs: tvOutputs{}, Features: outputdef.FeatureNameUpdates,
			Initial: outputdef.Patch{OutputList: &outputs, CurrentOutput: &currentOutput},
		},
		channeldef.Declare{
			Channels: tvTuner{}, Features: channeldef.FeatureChannelList | channeldef.FeatureLineupInfo,
			Initial: channeldef.Patch{ChannelList: &list, Lineup: lineup, CurrentChannel: &list[0]},
		},
		targetdef.Declare{Targets: tvTargets{}, Initial: targetdef.Patch{TargetList: &targets, CurrentTarget: &currentTarget}},
		lowpowerdef.Declare{LowPower: tvSleep{logger: m.logger}},
		woldef.Declare{Initial: woldef.Patch{MacAddress: &mac}},
	)
}

func (m *demoMedia) newApp() (*device.ContentApp, error) {
	vendor, name, version := "Vendor1", mediaAppID, "Version1"
	vendorID, productID := mediaAppVendorID, mediaAppProductID
	stopped := basicdef.ApplicationStatusStopped
	allowed := []uint16{mediaAppVendorID}
	return device.NewContentApp(bridged("contentapp", "Example App"),
		basicdef.Declare{Initial: basicdef.Patch{
			VendorName: &vendor, VendorId: &vendorID, ApplicationName: &name, ProductId: &productID,
			Application:        &basicdef.ApplicationStruct{CatalogVendorId: mediaCatalogVendorID, ApplicationId: mediaAppID},
			Status:             &stopped,
			ApplicationVersion: &version,
			AllowedVendorList:  &allowed,
		}},
		// tv-app.matter's endpoint 3: KeypadInput featureMap 0.
		keypaddef.Declare{Keypad: tvKeys{}},
		launcherdef.Declare{AppLauncher: &tvApp{media: m}},
		logindef.Declare{AccountLogin: tvLogin{}},
	)
}

// devices is the two media endpoints, built with the pair.
func (m *demoMedia) devices() ([]node.Endpoint, error) { //nolint:unparam // the fleet's builder (fleet.go endpoints) takes the error
	return []node.Endpoint{m.player, m.app}, nil
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
	return m.app.ApplicationBasic().Set(basicdef.Patch{Status: &s})
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
	return m.player.MediaPlayback().Set(mpdef.Patch{CurrentState: &s})
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

func positionOf(st *mpdef.State) uint64 {
	if st.SampledPosition == nil || st.SampledPosition.Position.Null {
		return 0
	}
	return st.SampledPosition.Position.Value
}

// Play is HandlePlay (:124-134).
func (playerTransport) Play(_ context.Context, st *mpdef.State) (mpdef.PlaybackResponse, error) {
	st.CurrentState, st.PlaybackSpeed = mpdef.PlaybackStatePlaying, 1
	return tvResponse(mpdef.StatusSuccess), nil
}

// Pause is HandlePause (:136-146).
func (playerTransport) Pause(_ context.Context, st *mpdef.State) (mpdef.PlaybackResponse, error) {
	st.CurrentState, st.PlaybackSpeed = mpdef.PlaybackStatePaused, 0
	return tvResponse(mpdef.StatusSuccess), nil
}

// Stop is HandleStop (:148-159).
func (playerTransport) Stop(_ context.Context, st *mpdef.State) (mpdef.PlaybackResponse, error) {
	st.CurrentState, st.PlaybackSpeed, st.SampledPosition = mpdef.PlaybackStateNotPlaying, 0, position(0)
	return tvResponse(mpdef.StatusSuccess), nil
}

// StartOver is HandleStartOver (:294-303).
func (playerTransport) StartOver(_ context.Context, st *mpdef.State) (mpdef.PlaybackResponse, error) { //nolint:unparam // the mediaplayback port signature
	st.SampledPosition = position(0)
	return tvResponse(mpdef.StatusSuccess), nil
}

// Previous is HandlePrevious (:189-200).
func (playerTransport) Previous(_ context.Context, st *mpdef.State) (mpdef.PlaybackResponse, error) { //nolint:unparam // the mediaplayback port signature
	st.CurrentState, st.PlaybackSpeed, st.SampledPosition = mpdef.PlaybackStatePlaying, 1, position(0)
	return tvResponse(mpdef.StatusSuccess), nil
}

// Next is HandleNext (:281-292).
func (playerTransport) Next(_ context.Context, st *mpdef.State) (mpdef.PlaybackResponse, error) { //nolint:unparam // the mediaplayback port signature
	st.CurrentState, st.PlaybackSpeed, st.SampledPosition = mpdef.PlaybackStatePlaying, 1, position(0)
	return tvResponse(mpdef.StatusSuccess), nil
}

// FastForward is HandleFastForward (:161-187).
func (playerTransport) FastForward(_ context.Context, st *mpdef.State, _ mpdef.FastForwardRequest) (mpdef.PlaybackResponse, error) { //nolint:unparam // the mediaplayback port signature
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
func (playerTransport) Rewind(_ context.Context, st *mpdef.State, _ mpdef.RewindRequest) (mpdef.PlaybackResponse, error) { //nolint:unparam // the mediaplayback port signature
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
func (playerTransport) SkipForward(_ context.Context, st *mpdef.State, req mpdef.SkipForwardRequest) (mpdef.PlaybackResponse, error) { //nolint:unparam // the mediaplayback port signature
	next := positionOf(st) + req.DeltaPositionMilliseconds
	if st.Duration != nil {
		next = min(next, *st.Duration)
	}
	st.SampledPosition = position(next)
	return tvResponse(mpdef.StatusSuccess), nil
}

// SkipBackward is HandleSkipBackward (:230-243): down to 0.
func (playerTransport) SkipBackward(_ context.Context, st *mpdef.State, req mpdef.SkipBackwardRequest) (mpdef.PlaybackResponse, error) { //nolint:unparam // the mediaplayback port signature
	cur := positionOf(st)
	next := uint64(0)
	if cur > req.DeltaPositionMilliseconds {
		next = cur - req.DeltaPositionMilliseconds
	}
	st.SampledPosition = position(next)
	return tvResponse(mpdef.StatusSuccess), nil
}

// Seek is HandleSeek (:259-279): SeekOutOfRange past the duration.
func (playerTransport) Seek(_ context.Context, st *mpdef.State, req mpdef.SeekRequest) (mpdef.PlaybackResponse, error) { //nolint:unparam // the mediaplayback port signature
	if st.Duration != nil && req.Position > *st.Duration {
		return tvResponse(mpdef.StatusSeekOutOfRange), nil
	}
	st.SampledPosition = position(req.Position)
	return tvResponse(mpdef.StatusSuccess), nil
}

// tvTrack is one track of MediaPlaybackManager.h:77-118: its id, a
// language code and a display name.
func tvTrack(id, language, name string) mpdef.TrackStruct {
	return mpdef.TrackStruct{Id: id, TrackAttributes: mpdef.TrackAttributesStruct{
		LanguageCode: language, DisplayName: &spec.Nullable[string]{Value: name},
	}}
}

// tvAudioTracks are the tv-app's two audio tracks (MediaPlaybackManager.h:
// 86-101); the first is active (:77-85).
func tvAudioTracks() []mpdef.TrackStruct {
	return []mpdef.TrackStruct{
		tvTrack("activeAudioTrackId_0", "languageCode1", "displayName1"),
		tvTrack("activeAudioTrackId_1", "languageCode2", "displayName2"),
	}
}

// tvTextTracks are the tv-app's two text tracks (:103-118); none is active
// (:102).
func tvTextTracks() []mpdef.TrackStruct {
	return []mpdef.TrackStruct{
		tvTrack("activeTextTrackId_0", "languageCode1", "displayName1"),
		tvTrack("activeTextTrackId_1", "languageCode2", "displayName2"),
	}
}

// tracks is the track list p points to; none for null.
func tracks(p *[]mpdef.TrackStruct) []mpdef.TrackStruct {
	if p == nil {
		return nil
	}
	return *p
}

// errNoTrack refuses a track id the player does not offer:
// HandleActivateAudioTrack and HandleActivateTextTrack answer false
// (MediaPlaybackManager.cpp:305-333), which the server sends as FAILURE.
var errNoTrack = errors.New("no such track")

// ActivateAudioTrack is HandleActivateAudioTrack (:305-318); the audio
// output index is not used.
func (playerTransport) ActivateAudioTrack(_ context.Context, st *mpdef.State, req mpdef.ActivateAudioTrackRequest) error {
	for _, tr := range tracks(st.AvailableAudioTracks) {
		if tr.Id == req.TrackId {
			st.ActiveAudioTrack = &tr
			return nil
		}
	}
	return errNoTrack
}

// ActivateTextTrack is HandleActivateTextTrack (:320-333).
func (playerTransport) ActivateTextTrack(_ context.Context, st *mpdef.State, req mpdef.ActivateTextTrackRequest) error {
	for _, tr := range tracks(st.AvailableTextTracks) {
		if tr.Id == req.TrackId {
			st.ActiveTextTrack = &tr
			return nil
		}
	}
	return errNoTrack
}

// DeactivateTextTrack is HandleDeactivateTextTrack (:335-343).
func (playerTransport) DeactivateTextTrack(_ context.Context, st *mpdef.State) error { //nolint:unparam // the mediaplayback port signature
	st.ActiveTextTrack = nil
	return nil
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

func (tvLauncher) LaunchContent(context.Context, *launchdef.State, launchdef.LaunchContentRequest) (launchdef.LauncherResponse, error) {
	return launchdef.LauncherResponse{Status: launchdef.StatusSuccess, Data: ptr("exampleData")}, nil
}

func (tvLauncher) LaunchURL(_ context.Context, _ *launchdef.State, req launchdef.LaunchUrlRequest) (launchdef.LauncherResponse, error) {
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

func (p *tvPlatform) LaunchApp(_ context.Context, st *launcherdef.State, req launcherdef.LaunchAppRequest) (launcherdef.LauncherResponse, error) {
	if !p.known(req.Application) {
		return launcherdef.LauncherResponse{Status: launcherdef.StatusAppNotAvailable}, nil
	}
	st.CurrentApp = p.media.currentApp()
	return launched(), p.media.appStatus(basicdef.ApplicationStatusActiveVisibleFocus)
}

func (p *tvPlatform) StopApp(_ context.Context, st *launcherdef.State, req launcherdef.StopAppRequest) (launcherdef.LauncherResponse, error) {
	if !p.known(req.Application) {
		return launcherdef.LauncherResponse{Status: launcherdef.StatusAppNotAvailable}, nil
	}
	st.CurrentApp = nil
	return launched(), p.media.appStatus(basicdef.ApplicationStatusStopped)
}

func (p *tvPlatform) HideApp(_ context.Context, _ *launcherdef.State, req launcherdef.HideAppRequest) (launcherdef.LauncherResponse, error) {
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

func (a *tvApp) LaunchApp(context.Context, *launcherdef.State, launcherdef.LaunchAppRequest) (launcherdef.LauncherResponse, error) {
	return launched(), a.media.appStatus(basicdef.ApplicationStatusActiveVisibleFocus)
}

func (a *tvApp) StopApp(context.Context, *launcherdef.State, launcherdef.StopAppRequest) (launcherdef.LauncherResponse, error) {
	return launched(), a.media.appStatus(basicdef.ApplicationStatusStopped)
}

func (a *tvApp) HideApp(context.Context, *launcherdef.State, launcherdef.HideAppRequest) (launcherdef.LauncherResponse, error) {
	return launched(), a.media.appStatus(basicdef.ApplicationStatusActiveHidden)
}

// tvInputs is MediaInputManager.cpp: select and rename a listed input
// (:57-71, :93-114), the status display logged (:73-91).
type tvInputs struct{}

func (tvInputs) SelectInput(_ context.Context, st *inputdef.State, req inputdef.SelectInputRequest) error {
	for _, in := range st.InputList {
		if in.Index == req.Index {
			st.CurrentInput = req.Index
			return nil
		}
	}
	return fmt.Errorf("no input %d", req.Index)
}

func (tvInputs) ShowInputStatus(context.Context, *inputdef.State) error { return nil }

func (tvInputs) HideInputStatus(context.Context, *inputdef.State) error { return nil }

func (tvInputs) RenameInput(_ context.Context, st *inputdef.State, req inputdef.RenameInputRequest) error {
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

func (tvOutputs) SelectOutput(_ context.Context, st *outputdef.State, req outputdef.SelectOutputRequest) error {
	for _, out := range st.OutputList {
		if out.Index == req.Index {
			st.CurrentOutput = req.Index
			return nil
		}
	}
	return fmt.Errorf("no output %d", req.Index)
}

func (tvOutputs) RenameOutput(_ context.Context, st *outputdef.State, req outputdef.RenameOutputRequest) error {
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

func (tvTuner) ChangeChannel(_ context.Context, st *channeldef.State, req channeldef.ChangeChannelRequest) (channeldef.ChangeChannelResponse, error) {
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

func (tvTuner) ChangeChannelByNumber(_ context.Context, st *channeldef.State, req channeldef.ChangeChannelByNumberRequest) error {
	for _, c := range st.ChannelList {
		if c.MajorNumber == req.MajorNumber && c.MinorNumber == req.MinorNumber {
			st.CurrentChannel = &c
			return nil
		}
	}
	return fmt.Errorf("no channel %d.%d", req.MajorNumber, req.MinorNumber)
}

func (tvTuner) SkipChannel(_ context.Context, st *channeldef.State, req channeldef.SkipChannelRequest) error {
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

func (tvTargets) NavigateTarget(_ context.Context, st *targetdef.State, req targetdef.NavigateTargetRequest) (targetdef.NavigateTargetResponse, error) {
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
