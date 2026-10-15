// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/media"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicdef "github.com/SukramJ/go-fabric/cluster/spec/applicationbasic"
	launcherdef "github.com/SukramJ/go-fabric/cluster/spec/applicationlauncher"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/contentcontrol"
	launchdef "github.com/SukramJ/go-fabric/cluster/spec/contentlauncher"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	woldef "github.com/SukramJ/go-fabric/cluster/spec/wakeonlan"
	"github.com/SukramJ/go-fabric/im"
)

// --- ApplicationLauncher and ApplicationBasic ---------------------------

// platform launches one known app and moves its ApplicationBasic.Status,
// as a content app platform does.
type platform struct {
	app *media.AppBasicServer
}

func (p *platform) known(a *launcherdef.ApplicationStruct) bool {
	return a != nil && a.CatalogVendorId == 123 && a.ApplicationId == "exampleid"
}

func (p *platform) status(s basicdef.ApplicationStatusEnum) {
	st := p.app.State()
	st.Status = s
	_ = p.app.SetState(st)
}

func (p *platform) LaunchApp(_ context.Context, st *media.AppLauncherState, req launcherdef.LaunchAppRequest) (launcherdef.LauncherResponse, error) {
	if !p.known(req.Application) {
		return launcherdef.LauncherResponse{Status: launcherdef.StatusAppNotAvailable}, nil
	}
	st.CurrentApp = &launcherdef.ApplicationEPStruct{Application: *req.Application, Endpoint: spectest.Ptr(uint16(3))}
	p.status(basicdef.ApplicationStatusActiveVisibleFocus)
	return launcherdef.LauncherResponse{Status: launcherdef.StatusSuccess, Data: spectest.Ptr([]byte("data"))}, nil
}

func (p *platform) StopApp(_ context.Context, st *media.AppLauncherState, req launcherdef.StopAppRequest) (launcherdef.LauncherResponse, error) {
	if !p.known(req.Application) {
		return launcherdef.LauncherResponse{Status: launcherdef.StatusAppNotAvailable}, nil
	}
	st.CurrentApp = nil
	p.status(basicdef.ApplicationStatusStopped)
	return launcherdef.LauncherResponse{Status: launcherdef.StatusSuccess}, nil
}

func (p *platform) HideApp(_ context.Context, _ *media.AppLauncherState, req launcherdef.HideAppRequest) (launcherdef.LauncherResponse, error) {
	if !p.known(req.Application) {
		return launcherdef.LauncherResponse{}, media.StatusError{Status: im.StatusInvalidCommand}
	}
	p.status(basicdef.ApplicationStatusActiveHidden)
	return launcherdef.LauncherResponse{Status: launcherdef.StatusSuccess}, nil
}

func newApp(t *testing.T) *media.AppBasicServer {
	t.Helper()
	app, err := media.NewAppBasic(media.AppBasicConfig{
		Attributes: []uint32{basicdef.AttrVendorName, basicdef.AttrVendorId, basicdef.AttrProductId},
		Initial: media.AppBasicState{
			VendorName: "Example", VendorID: 0xFFF1, ApplicationName: "Example App", ProductID: 0x8001,
			Application:        &basicdef.ApplicationStruct{CatalogVendorId: 123, ApplicationId: "exampleid"},
			Status:             basicdef.ApplicationStatusActiveVisibleFocus,
			ApplicationVersion: "1.0", AllowedVendorList: []uint16{0xFFF1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

// TestApplicationLauncher: the platform's launcher moves the content
// app's ApplicationBasic.Status — the APPLAUNCHER-3.7 to 3.9 sequence —
// and CurrentApp follows the port.
func TestApplicationLauncher(t *testing.T) {
	t.Parallel()
	app := newApp(t)
	srv, err := media.NewAppLauncher(media.AppLauncherConfig{
		Port:       &platform{app: app},
		Features:   launcherdef.FeatureApplicationPlatform,
		Attributes: []uint32{launcherdef.AttrCurrentApp},
		Initial:    media.AppLauncherState{CatalogList: []uint16{123, 456}},
	})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, launcherdef.Definition, uint32(launcherdef.FeatureApplicationPlatform))
	if v := read(t, srv, launcherdef.AttrCatalogList); !slices.Equal(v.([]uint16), []uint16{123, 456}) {
		t.Errorf("CatalogList %v", v)
	}
	if v := read(t, srv, launcherdef.AttrCurrentApp); v != nil {
		t.Errorf("CurrentApp %v, want null", v)
	}
	example := &launcherdef.ApplicationStruct{CatalogVendorId: 123, ApplicationId: "exampleid"}
	resp, err := invoke(t, srv, launcherdef.Definition, launcherdef.CmdLaunchApp, launcherdef.LaunchAppRequest{Application: example, Data: spectest.Ptr([]byte("Hello World"))})
	if err != nil {
		t.Fatal(err)
	}
	wantResponse(t, launcherdef.Definition, launcherdef.CmdLauncherResponse, resp.(launcherdef.LauncherResponse),
		launcherdef.LauncherResponse{Status: launcherdef.StatusSuccess, Data: spectest.Ptr([]byte("data"))})
	if v := read(t, srv, launcherdef.AttrCurrentApp).(launcherdef.ApplicationEPStruct); v.Application.ApplicationId != "exampleid" {
		t.Errorf("CurrentApp %+v", v)
	}
	resp, _ = invoke(t, srv, launcherdef.Definition, launcherdef.CmdLaunchApp, launcherdef.LaunchAppRequest{
		Application: &launcherdef.ApplicationStruct{CatalogVendorId: 123, ApplicationId: "NonAvailableApp"},
	})
	if resp.(launcherdef.LauncherResponse).Status != launcherdef.StatusAppNotAvailable {
		t.Errorf("unknown app: %+v", resp)
	}
	if _, err := invoke(t, srv, launcherdef.Definition, launcherdef.CmdHideApp, launcherdef.HideAppRequest{Application: example}); err != nil {
		t.Fatal(err)
	}
	if v := read(t, app, basicdef.AttrStatus); v != uint8(basicdef.ApplicationStatusActiveHidden) {
		t.Errorf("Status after HideApp %v", v)
	}
	_, err = invoke(t, srv, launcherdef.Definition, launcherdef.CmdHideApp, launcherdef.HideAppRequest{})
	wantStatus(t, err, im.StatusInvalidCommand)
	if _, err := invoke(t, srv, launcherdef.Definition, launcherdef.CmdStopApp, launcherdef.StopAppRequest{Application: example}); err != nil {
		t.Fatal(err)
	}
	if v := read(t, app, basicdef.AttrStatus); v != uint8(basicdef.ApplicationStatusStopped) {
		t.Errorf("Status after StopApp %v", v)
	}
	if v := read(t, srv, launcherdef.AttrCurrentApp); v != nil {
		t.Errorf("CurrentApp %v after StopApp, want null", v)
	}
	if _, err := media.NewAppLauncher(media.AppLauncherConfig{Port: &platform{}, Features: launcherdef.FeatureApplicationPlatform}); !errors.Is(err, media.ErrMissingState) {
		t.Errorf("AP without CatalogList: %v", err)
	}
	if _, err := media.NewAppLauncher(media.AppLauncherConfig{}); !errors.Is(err, media.ErrNilPort) {
		t.Errorf("no port: %v", err)
	}
}

// TestApplicationBasic: the attribute state is the host's; Application and
// AllowedVendorList are required (no default in application-basic.element.ts:
// 30-44); AllowedVendorList reads at Administer (:42, "R A").
func TestApplicationBasic(t *testing.T) {
	t.Parallel()
	app := newApp(t)
	spectest.CheckServer(t, app, basicdef.Definition, 0)
	if v := read(t, app, basicdef.AttrApplication).(basicdef.ApplicationStruct); v.ApplicationId != "exampleid" {
		t.Errorf("Application %+v", v)
	}
	if v := read(t, app, basicdef.AttrVendorName); v != "Example" {
		t.Errorf("VendorName %v", v)
	}
	if app.MinReadPrivilege(basicdef.AttrAllowedVendorList) != 5 {
		t.Errorf("AllowedVendorList read privilege %d", app.MinReadPrivilege(basicdef.AttrAllowedVendorList))
	}
	if _, err := media.NewAppBasic(media.AppBasicConfig{Initial: media.AppBasicState{AllowedVendorList: []uint16{}}}); !errors.Is(err, media.ErrMissingState) {
		t.Errorf("no Application: %v", err)
	}
	if _, err := media.NewAppBasic(media.AppBasicConfig{Initial: media.AppBasicState{Application: &basicdef.ApplicationStruct{}}}); !errors.Is(err, media.ErrMissingState) {
		t.Errorf("no AllowedVendorList: %v", err)
	}
	if _, err := media.NewAppBasic(media.AppBasicConfig{Initial: media.AppBasicState{
		Application: &basicdef.ApplicationStruct{}, AllowedVendorList: []uint16{}, VendorName: "a name longer than thirty-two bytes",
	}, Attributes: []uint32{basicdef.AttrVendorName}}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("VendorName over max 32: %v", err)
	}
}

// TestWakeOnLan: the two optional attributes are served when listed and
// read the host's values.
func TestWakeOnLan(t *testing.T) {
	t.Parallel()
	lla := []byte{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	srv, err := media.NewWakeOnLan(media.WakeOnLanConfig{
		Attributes: []uint32{woldef.AttrMacAddress, woldef.AttrLinkLocalAddress},
		Initial:    media.WakeOnLanState{MacAddress: "02005E102001", LinkLocalAddress: lla},
	})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, woldef.Definition, 0)
	if v := read(t, srv, woldef.AttrMacAddress); v != "02005E102001" {
		t.Errorf("MacAddress %v", v)
	}
	if v := read(t, srv, woldef.AttrLinkLocalAddress); !slices.Equal(v.([]byte), lla) {
		t.Errorf("LinkLocalAddress %v", v)
	}
	bare, err := media.NewWakeOnLan(media.WakeOnLanConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bare.MatterRead(woldef.AttrMacAddress); ok {
		t.Error("MacAddress served without being listed")
	}
}

// --- ContentLauncher -----------------------------------------------------

type launcher struct{ urls []string }

func (l *launcher) LaunchContent(_ context.Context, _ *media.ContentLauncherState, req launchdef.LaunchContentRequest) (launchdef.LauncherResponse, error) {
	if len(req.Search.ParameterList) == 0 {
		return launchdef.LauncherResponse{}, media.StatusError{Status: im.StatusInvalidCommand}
	}
	return launchdef.LauncherResponse{Status: launchdef.StatusSuccess, Data: spectest.Ptr("exampleData")}, nil
}

func (l *launcher) LaunchURL(_ context.Context, _ *media.ContentLauncherState, req launchdef.LaunchUrlRequest) (launchdef.LauncherResponse, error) {
	l.urls = append(l.urls, req.ContentUrl)
	if req.ContentUrl == "https://badurl" {
		return launchdef.LauncherResponse{Status: launchdef.StatusUrlNotAvailable}, nil
	}
	return launchdef.LauncherResponse{Status: launchdef.StatusSuccess, Data: req.DisplayString}, nil
}

// TestContentLauncher: LaunchContent and LaunchURL reach the port with
// their decoded fields; AcceptHeader defaults to the empty list
// (content-launcher.element.ts:34).
func TestContentLauncher(t *testing.T) {
	t.Parallel()
	port := &launcher{}
	features := launchdef.FeatureContentSearch | launchdef.FeatureUrlPlayback
	srv, err := media.NewContentLauncher(media.ContentLauncherConfig{
		Port: port, Features: features,
		Initial: media.ContentLauncherState{SupportedStreamingProtocols: launchdef.SupportedProtocolsDash},
	})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, launchdef.Definition, uint32(features))
	if v := read(t, srv, launchdef.AttrAcceptHeader); v == nil || len(v.([]string)) != 0 {
		t.Errorf("AcceptHeader %#v, want the empty list", v)
	}
	if v := read(t, srv, launchdef.AttrSupportedStreamingProtocols); v != uint32(launchdef.SupportedProtocolsDash) {
		t.Errorf("SupportedStreamingProtocols %v", v)
	}
	req := launchdef.LaunchContentRequest{
		Search:   launchdef.ContentSearchStruct{ParameterList: []launchdef.ParameterStruct{{Type: launchdef.ParameterActor, Value: "Gaby sHoffman"}}},
		AutoPlay: true, Data: spectest.Ptr("exampleData"),
	}
	resp, err := invoke(t, srv, launchdef.Definition, launchdef.CmdLaunchContent, req)
	if err != nil {
		t.Fatal(err)
	}
	wantResponse(t, launchdef.Definition, launchdef.CmdLauncherResponse, resp.(launchdef.LauncherResponse),
		launchdef.LauncherResponse{Status: launchdef.StatusSuccess, Data: spectest.Ptr("exampleData")})
	_, err = invoke(t, srv, launchdef.Definition, launchdef.CmdLaunchContent, launchdef.LaunchContentRequest{})
	wantStatus(t, err, im.StatusInvalidCommand)
	resp, err = invoke(t, srv, launchdef.Definition, launchdef.CmdLaunchUrl, launchdef.LaunchUrlRequest{ContentUrl: "https://badurl"})
	if err != nil || resp.(launchdef.LauncherResponse).Status != launchdef.StatusUrlNotAvailable {
		t.Fatalf("bad URL: %v %v", resp, err)
	}
	if !slices.Equal(port.urls, []string{"https://badurl"}) {
		t.Errorf("urls %v", port.urls)
	}
	cs, err := media.NewContentLauncher(media.ContentLauncherConfig{Port: port, Features: launchdef.FeatureContentSearch})
	if err != nil {
		t.Fatal(err)
	}
	_, err = invoke(t, cs, launchdef.Definition, launchdef.CmdLaunchUrl, launchdef.LaunchUrlRequest{ContentUrl: "https://csa-iot.org/"})
	wantStatus(t, err, im.StatusUnsupportedCommand)
	if _, err := media.NewContentLauncher(media.ContentLauncherConfig{}); !errors.Is(err, media.ErrNilPort) {
		t.Errorf("no port: %v", err)
	}
}

// --- ContentControl ------------------------------------------------------

// parental implements every ContentControl feature's commands.
type parental struct{ pin string }

func (p *parental) Enable(_ context.Context, st *media.ContentControlState) error {
	st.Enabled = true
	return nil
}

func (p *parental) Disable(_ context.Context, st *media.ContentControlState) error {
	st.Enabled = false
	return nil
}

func (p *parental) UpdatePin(_ context.Context, _ *media.ContentControlState, req ccdef.UpdatePinRequest) error {
	if req.OldPin != p.pin {
		return media.StatusError{Status: im.StatusUnsupportedAccess}
	}
	p.pin = req.NewPin
	return nil
}

func (p *parental) ResetPin(context.Context, *media.ContentControlState) (ccdef.ResetPinResponse, error) {
	p.pin = "000000"
	return ccdef.ResetPinResponse{PinCode: p.pin}, nil
}

func (p *parental) AddBonusTime(_ context.Context, st *media.ContentControlState, req ccdef.AddBonusTimeRequest) error {
	st.RemainingScreenTime += req.BonusTime
	return nil
}

func (p *parental) SetScreenDailyTime(_ context.Context, st *media.ContentControlState, req ccdef.SetScreenDailyTimeRequest) error {
	st.ScreenDailyTime = req.ScreenTime
	return nil
}

func (p *parental) BlockUnratedContent(_ context.Context, st *media.ContentControlState) error {
	st.BlockUnrated = true
	return nil
}

func (p *parental) UnblockUnratedContent(_ context.Context, st *media.ContentControlState) error {
	st.BlockUnrated = false
	return nil
}

func (p *parental) SetOnDemandRatingThreshold(_ context.Context, st *media.ContentControlState, req ccdef.SetOnDemandRatingThresholdRequest) error {
	st.OnDemandRatingThreshold = req.Rating
	return nil
}

func (p *parental) SetScheduledContentRatingThreshold(_ context.Context, st *media.ContentControlState, req ccdef.SetScheduledContentRatingThresholdRequest) error {
	st.ScheduledContentRatingThreshold = req.Rating
	return nil
}

func (p *parental) AddBlockChannels(_ context.Context, st *media.ContentControlState, req ccdef.AddBlockChannelsRequest) error {
	st.BlockChannelList = append(st.BlockChannelList, req.Channels...)
	return nil
}

func (p *parental) RemoveBlockChannels(_ context.Context, st *media.ContentControlState, _ ccdef.RemoveBlockChannelsRequest) error {
	st.BlockChannelList = st.BlockChannelList[:0]
	return nil
}

func (p *parental) AddBlockApplications(_ context.Context, st *media.ContentControlState, req ccdef.AddBlockApplicationsRequest) error {
	st.BlockApplicationList = append(st.BlockApplicationList, req.Applications...)
	return nil
}

func (p *parental) RemoveBlockApplications(_ context.Context, st *media.ContentControlState, _ ccdef.RemoveBlockApplicationsRequest) error {
	st.BlockApplicationList = st.BlockApplicationList[:0]
	return nil
}

func (p *parental) SetBlockContentTimeWindow(_ context.Context, st *media.ContentControlState, req ccdef.SetBlockContentTimeWindowRequest) error {
	st.BlockContentTimeWindow = append(st.BlockContentTimeWindow, req.TimeWindow)
	return nil
}

func (p *parental) RemoveBlockContentTimeWindow(_ context.Context, st *media.ContentControlState, _ ccdef.RemoveBlockContentTimeWindowRequest) error {
	st.BlockContentTimeWindow = st.BlockContentTimeWindow[:0]
	return nil
}

type onOffParental struct{ inner parental }

func (p *onOffParental) Enable(ctx context.Context, st *media.ContentControlState) error {
	return p.inner.Enable(ctx, st)
}

func (p *onOffParental) Disable(ctx context.Context, st *media.ContentControlState) error {
	return p.inner.Disable(ctx, st)
}

const allContentControl = ccdef.FeatureScreenTime | ccdef.FeaturePinManagement | ccdef.FeatureBlockUnrated |
	ccdef.FeatureOnDemandContentRating | ccdef.FeatureScheduledContentRating | ccdef.FeatureBlockChannels |
	ccdef.FeatureBlockApplications | ccdef.FeatureBlockContentTimeWindow

// TestContentControl: every feature's commands reach the port and the
// state it changes is what the attributes read; the PIN commands are
// timed (content-control.element.ts:78, :82).
func TestContentControl(t *testing.T) {
	t.Parallel()
	srv, err := media.NewContentControl(media.ContentControlConfig{
		Port: &parental{pin: "123456"}, Features: allContentControl,
		Initial: media.ContentControlState{
			OnDemandRatings: []ccdef.RatingNameStruct{{RatingName: "G"}}, ScheduledContentRatings: []ccdef.RatingNameStruct{{RatingName: "TV-Y"}},
			BlockChannelList: []ccdef.BlockChannelStruct{}, BlockApplicationList: []ccdef.AppInfoStruct{}, BlockContentTimeWindow: []ccdef.TimeWindowStruct{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, ccdef.Definition, uint32(allContentControl))
	steps := []struct {
		cmd uint32
		req spec.Encodable
	}{
		{ccdef.CmdEnable, ccdef.EnableRequest{}},
		{ccdef.CmdUpdatePin, ccdef.UpdatePinRequest{OldPin: "123456", NewPin: "654321"}},
		{ccdef.CmdAddBonusTime, ccdef.AddBonusTimeRequest{BonusTime: 300}},
		{ccdef.CmdSetScreenDailyTime, ccdef.SetScreenDailyTimeRequest{ScreenTime: 3600}},
		{ccdef.CmdBlockUnratedContent, ccdef.BlockUnratedContentRequest{}},
		{ccdef.CmdSetOnDemandRatingThreshold, ccdef.SetOnDemandRatingThresholdRequest{Rating: "G"}},
		{ccdef.CmdSetScheduledContentRatingThreshold, ccdef.SetScheduledContentRatingThresholdRequest{Rating: "TV-Y"}},
		{ccdef.CmdAddBlockChannels, ccdef.AddBlockChannelsRequest{Channels: []ccdef.BlockChannelStruct{{BlockChannelIndex: spec.Nullable[uint16]{Null: true}, MajorNumber: 6}}}},
		{ccdef.CmdAddBlockApplications, ccdef.AddBlockApplicationsRequest{Applications: []ccdef.AppInfoStruct{{CatalogVendorId: 123, ApplicationId: "exampleid"}}}},
		{ccdef.CmdSetBlockContentTimeWindow, ccdef.SetBlockContentTimeWindowRequest{TimeWindow: ccdef.TimeWindowStruct{
			TimeWindowIndex: spec.Nullable[uint16]{Null: true}, DayOfWeek: 1, TimePeriod: []ccdef.TimePeriodStruct{{StartHour: 20, EndHour: 22}},
		}}},
	}
	for _, s := range steps {
		if _, err := invoke(t, srv, ccdef.Definition, s.cmd, s.req); err != nil {
			t.Fatalf("0x%02X: %v", s.cmd, err)
		}
	}
	st := srv.State()
	if !st.Enabled || st.RemainingScreenTime != 300 || st.ScreenDailyTime != 3600 || !st.BlockUnrated ||
		st.OnDemandRatingThreshold != "G" || len(st.BlockChannelList) != 1 || len(st.BlockApplicationList) != 1 || len(st.BlockContentTimeWindow) != 1 {
		t.Errorf("state %+v", st)
	}
	if v := read(t, srv, ccdef.AttrScreenDailyTime); v != uint32(3600) {
		t.Errorf("ScreenDailyTime %v", v)
	}
	resp, err := invoke(t, srv, ccdef.Definition, ccdef.CmdResetPin, ccdef.ResetPinRequest{})
	if err != nil {
		t.Fatal(err)
	}
	wantResponse(t, ccdef.Definition, ccdef.CmdResetPinResponse, resp.(ccdef.ResetPinResponse), ccdef.ResetPinResponse{PinCode: "000000"})
	_, err = invoke(t, srv, ccdef.Definition, ccdef.CmdUpdatePin, ccdef.UpdatePinRequest{OldPin: "999999", NewPin: "111111"})
	wantStatus(t, err, im.StatusUnsupportedAccess)
	for _, s := range []struct {
		cmd uint32
		req spec.Encodable
	}{
		{ccdef.CmdUnblockUnratedContent, ccdef.UnblockUnratedContentRequest{}},
		{ccdef.CmdRemoveBlockChannels, ccdef.RemoveBlockChannelsRequest{ChannelIndexes: []uint16{0}}},
		{ccdef.CmdRemoveBlockApplications, ccdef.RemoveBlockApplicationsRequest{Applications: []ccdef.AppInfoStruct{{CatalogVendorId: 123, ApplicationId: "exampleid"}}}},
		{ccdef.CmdRemoveBlockContentTimeWindow, ccdef.RemoveBlockContentTimeWindowRequest{TimeWindowIndexes: []uint16{0}}},
		{ccdef.CmdDisable, ccdef.DisableRequest{}},
	} {
		if _, err := invoke(t, srv, ccdef.Definition, s.cmd, s.req); err != nil {
			t.Fatalf("0x%02X: %v", s.cmd, err)
		}
	}
	if st := srv.State(); st.Enabled || st.BlockUnrated || len(st.BlockChannelList) != 0 {
		t.Errorf("state %+v", st)
	}
	if !srv.IsTimedCommand(ccdef.CmdUpdatePin) || !srv.IsTimedCommand(ccdef.CmdResetPin) || !srv.IsTimedCommand(ccdef.CmdEnable) {
		t.Error("the PIN and enable commands are not timed")
	}
	ev := &emitted{}
	srv.SetMatterEventEmitter(ev)
	if err := srv.EmitRemainingScreenTimeExpired(); err != nil {
		t.Fatal(err)
	}
	if err := srv.EmitEnteringBlockContentTimeWindow(); err != nil {
		t.Fatal(err)
	}
	if len(ev.events) != 2 {
		t.Errorf("events %+v", ev.events)
	}
}

// TestContentControlConstruction: a feature whose commands the port lacks
// is ErrNotImplemented; a feature's list without state is ErrMissingState.
func TestContentControlConstruction(t *testing.T) {
	t.Parallel()
	for _, f := range []ccdef.Feature{
		ccdef.FeatureScreenTime, ccdef.FeaturePinManagement, ccdef.FeatureBlockUnrated, ccdef.FeatureOnDemandContentRating,
		ccdef.FeatureScheduledContentRating, ccdef.FeatureBlockChannels, ccdef.FeatureBlockApplications, ccdef.FeatureBlockContentTimeWindow,
	} {
		if _, err := media.NewContentControl(media.ContentControlConfig{Port: &onOffParental{}, Features: f}); !errors.Is(err, media.ErrNotImplemented) {
			t.Errorf("feature 0x%X: %v", f, err)
		}
	}
	if _, err := media.NewContentControl(media.ContentControlConfig{Port: &parental{}, Features: ccdef.FeatureBlockChannels}); !errors.Is(err, media.ErrMissingState) {
		t.Errorf("BC without BlockChannelList: %v", err)
	}
	srv, err := media.NewContentControl(media.ContentControlConfig{Port: &onOffParental{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := srv.MatterAcceptedCommands(); !slices.Equal(got, []uint32{ccdef.CmdEnable, ccdef.CmdDisable}) {
		t.Errorf("AcceptedCommandList %v", got)
	}
	if _, err := media.NewContentControl(media.ContentControlConfig{}); !errors.Is(err, media.ErrNilPort) {
		t.Errorf("no port: %v", err)
	}
}
