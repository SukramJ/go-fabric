// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media

import (
	"context"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/contentcontrol"
)

// ContentControlState is the ContentControl attribute state
// (content-control.element.ts:35-74); each attribute is served with the
// feature its conformance names. The lists of a selected feature have no
// default: nil refuses construction ([ErrMissingState]).
type ContentControlState struct {
	// Enabled (M).
	Enabled bool
	// OnDemandRatings and OnDemandRatingThreshold: OnDemandContentRating
	// (OCR).
	OnDemandRatings         []ccdef.RatingNameStruct
	OnDemandRatingThreshold string
	// ScheduledContentRatings and ScheduledContentRatingThreshold:
	// ScheduledContentRating (SCR).
	ScheduledContentRatings         []ccdef.RatingNameStruct
	ScheduledContentRatingThreshold string
	// ScreenDailyTime and RemainingScreenTime: ScreenTime (ST).
	ScreenDailyTime     uint32
	RemainingScreenTime uint32
	// BlockUnrated: BlockUnrated (BU).
	BlockUnrated bool
	// BlockChannelList: BlockChannels (BC).
	BlockChannelList []ccdef.BlockChannelStruct
	// BlockApplicationList: BlockApplications (BA).
	BlockApplicationList []ccdef.AppInfoStruct
	// BlockContentTimeWindow: BlockContentTimeWindow (BTW).
	BlockContentTimeWindow []ccdef.TimeWindowStruct
}

func (st ContentControlState) values() map[uint32]any {
	return map[uint32]any{
		ccdef.AttrEnabled:                         st.Enabled,
		ccdef.AttrOnDemandRatings:                 listOr(st.OnDemandRatings),
		ccdef.AttrOnDemandRatingThreshold:         st.OnDemandRatingThreshold,
		ccdef.AttrScheduledContentRatings:         listOr(st.ScheduledContentRatings),
		ccdef.AttrScheduledContentRatingThreshold: st.ScheduledContentRatingThreshold,
		ccdef.AttrScreenDailyTime:                 st.ScreenDailyTime,
		ccdef.AttrRemainingScreenTime:             st.RemainingScreenTime,
		ccdef.AttrBlockUnrated:                    st.BlockUnrated,
		ccdef.AttrBlockChannelList:                listOr(st.BlockChannelList),
		ccdef.AttrBlockApplicationList:            listOr(st.BlockApplicationList),
		ccdef.AttrBlockContentTimeWindow:          listOr(st.BlockContentTimeWindow),
	}
}

func (st ContentControlState) clone() ContentControlState {
	st.OnDemandRatings = slices.Clone(st.OnDemandRatings)
	st.ScheduledContentRatings = slices.Clone(st.ScheduledContentRatings)
	st.BlockChannelList = slices.Clone(st.BlockChannelList)
	st.BlockApplicationList = slices.Clone(st.BlockApplicationList)
	st.BlockContentTimeWindow = slices.Clone(st.BlockContentTimeWindow)
	return st
}

// ContentControl is the ContentControl host port: Enable and Disable
// (:87-88), status only. Each feature's commands are a further interface,
// required with the feature.
type ContentControl interface {
	Enable(ctx context.Context, st *ContentControlState) error
	Disable(ctx context.Context, st *ContentControlState) error
}

// ContentControlPIN is UpdatePin and ResetPin (PinManagement, PM, :77-86).
type ContentControlPIN interface {
	UpdatePin(ctx context.Context, st *ContentControlState, req ccdef.UpdatePinRequest) error
	ResetPin(ctx context.Context, st *ContentControlState) (ccdef.ResetPinResponse, error)
}

// ContentControlScreenTime is AddBonusTime and SetScreenDailyTime
// (ScreenTime, ST, :89-101).
type ContentControlScreenTime interface {
	AddBonusTime(ctx context.Context, st *ContentControlState, req ccdef.AddBonusTimeRequest) error
	SetScreenDailyTime(ctx context.Context, st *ContentControlState, req ccdef.SetScreenDailyTimeRequest) error
}

// ContentControlUnrated is BlockUnratedContent and UnblockUnratedContent
// (BlockUnrated, BU, :103-107).
type ContentControlUnrated interface {
	BlockUnratedContent(ctx context.Context, st *ContentControlState) error
	UnblockUnratedContent(ctx context.Context, st *ContentControlState) error
}

// ContentControlOnDemandRating is SetOnDemandRatingThreshold
// (OnDemandContentRating, OCR, :109-115).
type ContentControlOnDemandRating interface {
	SetOnDemandRatingThreshold(ctx context.Context, st *ContentControlState, req ccdef.SetOnDemandRatingThresholdRequest) error
}

// ContentControlScheduledRating is SetScheduledContentRatingThreshold
// (ScheduledContentRating, SCR, :117-123).
type ContentControlScheduledRating interface {
	SetScheduledContentRatingThreshold(ctx context.Context, st *ContentControlState, req ccdef.SetScheduledContentRatingThresholdRequest) error
}

// ContentControlChannels is AddBlockChannels and RemoveBlockChannels
// (BlockChannels, BC, :125-142).
type ContentControlChannels interface {
	AddBlockChannels(ctx context.Context, st *ContentControlState, req ccdef.AddBlockChannelsRequest) error
	RemoveBlockChannels(ctx context.Context, st *ContentControlState, req ccdef.RemoveBlockChannelsRequest) error
}

// ContentControlApps is AddBlockApplications and RemoveBlockApplications
// (BlockApplications, BA, :144-164).
type ContentControlApps interface {
	AddBlockApplications(ctx context.Context, st *ContentControlState, req ccdef.AddBlockApplicationsRequest) error
	RemoveBlockApplications(ctx context.Context, st *ContentControlState, req ccdef.RemoveBlockApplicationsRequest) error
}

// ContentControlTimeWindows is SetBlockContentTimeWindow and
// RemoveBlockContentTimeWindow (BlockContentTimeWindow, BTW, :166-184).
type ContentControlTimeWindows interface {
	SetBlockContentTimeWindow(ctx context.Context, st *ContentControlState, req ccdef.SetBlockContentTimeWindowRequest) error
	RemoveBlockContentTimeWindow(ctx context.Context, st *ContentControlState, req ccdef.RemoveBlockContentTimeWindowRequest) error
}

// ContentControlConfig carries the construction parameters of a
// ContentControl server.
type ContentControlConfig struct {
	// Port carries out the commands; required.
	Port ContentControl
	// Features is the FeatureMap (ST, PM, BU, OCR, SCR, BC, BA, BTW).
	Features ccdef.Feature
	// Initial is the state the server starts with.
	Initial ContentControlState
	// DataVersion is an optional host-owned tracker.
	DataVersion dataVersion
}

// ContentControlServer is a ContentControl (0x050F) server. Its events
// (RemainingScreenTimeExpired with ST, EnteringBlockContentTimeWindow with
// BTW; :75-76) are the host's to raise.
type ContentControlServer struct {
	*spec.Server
	stateful[ContentControlState]
	events
}

// NewContentControl builds a ContentControl server over cfg.Port.
func NewContentControl(cfg ContentControlConfig) (*ContentControlServer, error) { //nolint:funlen // one registration per command of eight features
	if cfg.Port == nil {
		return nil, fmt.Errorf("%w: ContentControl", ErrNilPort)
	}
	port := cfg.Port
	pin, hasPIN := port.(ContentControlPIN)
	screen, hasScreen := port.(ContentControlScreenTime)
	unrated, hasUnrated := port.(ContentControlUnrated)
	onDemand, hasOnDemand := port.(ContentControlOnDemandRating)
	scheduled, hasScheduled := port.(ContentControlScheduledRating)
	channels, hasChannels := port.(ContentControlChannels)
	apps, hasApps := port.(ContentControlApps)
	windows, hasWindows := port.(ContentControlTimeWindows)
	cmds := []command{
		{ccdef.CmdEnable, true},
		{ccdef.CmdDisable, true},
		{ccdef.CmdUpdatePin, hasPIN},
		{ccdef.CmdResetPin, hasPIN},
		{ccdef.CmdAddBonusTime, hasScreen},
		{ccdef.CmdSetScreenDailyTime, hasScreen},
		{ccdef.CmdBlockUnratedContent, hasUnrated},
		{ccdef.CmdUnblockUnratedContent, hasUnrated},
		{ccdef.CmdSetOnDemandRatingThreshold, hasOnDemand},
		{ccdef.CmdSetScheduledContentRatingThreshold, hasScheduled},
		{ccdef.CmdAddBlockChannels, hasChannels},
		{ccdef.CmdRemoveBlockChannels, hasChannels},
		{ccdef.CmdAddBlockApplications, hasApps},
		{ccdef.CmdRemoveBlockApplications, hasApps},
		{ccdef.CmdSetBlockContentTimeWindow, hasWindows},
		{ccdef.CmdRemoveBlockContentTimeWindow, hasWindows},
	}
	s := &ContentControlServer{}
	h := handlers{}
	type plain = func(ctx context.Context, st *ContentControlState) error
	empty := func(cmdID uint32, fn plain) {
		handleState(h, &s.stateful, cmdID, func(ctx context.Context, st *ContentControlState, _ struct{}) (any, error) {
			return nil, fn(ctx, st)
		})
	}
	empty(ccdef.CmdEnable, port.Enable)
	empty(ccdef.CmdDisable, port.Disable)
	if hasPIN {
		handleState(h, &s.stateful, ccdef.CmdUpdatePin, statusOnly(pin.UpdatePin))
		handleState(h, &s.stateful, ccdef.CmdResetPin, func(ctx context.Context, st *ContentControlState, _ struct{}) (any, error) {
			return pin.ResetPin(ctx, st)
		})
	}
	if hasScreen {
		handleState(h, &s.stateful, ccdef.CmdAddBonusTime, statusOnly(screen.AddBonusTime))
		handleState(h, &s.stateful, ccdef.CmdSetScreenDailyTime, statusOnly(screen.SetScreenDailyTime))
	}
	if hasUnrated {
		empty(ccdef.CmdBlockUnratedContent, unrated.BlockUnratedContent)
		empty(ccdef.CmdUnblockUnratedContent, unrated.UnblockUnratedContent)
	}
	if hasOnDemand {
		handleState(h, &s.stateful, ccdef.CmdSetOnDemandRatingThreshold, statusOnly(onDemand.SetOnDemandRatingThreshold))
	}
	if hasScheduled {
		handleState(h, &s.stateful, ccdef.CmdSetScheduledContentRatingThreshold, statusOnly(scheduled.SetScheduledContentRatingThreshold))
	}
	if hasChannels {
		handleState(h, &s.stateful, ccdef.CmdAddBlockChannels, statusOnly(channels.AddBlockChannels))
		handleState(h, &s.stateful, ccdef.CmdRemoveBlockChannels, statusOnly(channels.RemoveBlockChannels))
	}
	if hasApps {
		handleState(h, &s.stateful, ccdef.CmdAddBlockApplications, statusOnly(apps.AddBlockApplications))
		handleState(h, &s.stateful, ccdef.CmdRemoveBlockApplications, statusOnly(apps.RemoveBlockApplications))
	}
	if hasWindows {
		handleState(h, &s.stateful, ccdef.CmdSetBlockContentTimeWindow, statusOnly(windows.SetBlockContentTimeWindow))
		handleState(h, &s.stateful, ccdef.CmdRemoveBlockContentTimeWindow, statusOnly(windows.RemoveBlockContentTimeWindow))
	}
	srv, err := newServer(ccdef.Definition, spec.Options{Features: uint32(cfg.Features)},
		spec.ServerConfig{Handlers: h, DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("media: ContentControl: %w", err)
	}
	if err := checkImplemented(srv, cmds); err != nil {
		return nil, err
	}
	s.Server = srv
	if err := s.init(srv, cfg.Initial, ContentControlState.values, ContentControlState.clone); err != nil {
		return nil, fmt.Errorf("media: ContentControl: %w", err)
	}
	if err := checkState(srv); err != nil {
		return nil, err
	}
	return s, nil
}

// EmitRemainingScreenTimeExpired emits RemainingScreenTimeExpired (event
// 0x00, info, ScreenTime; :75).
func (s *ContentControlServer) EmitRemainingScreenTimeExpired() error {
	return s.Emit(s.endpoint(), ccdef.EventRemainingScreenTimeExpired, ccdef.RemainingScreenTimeExpiredEvent{})
}

// EmitEnteringBlockContentTimeWindow emits EnteringBlockContentTimeWindow
// (event 0x01, info, BlockContentTimeWindow; :76).
func (s *ContentControlServer) EmitEnteringBlockContentTimeWindow() error {
	return s.Emit(s.endpoint(), ccdef.EventEnteringBlockContentTimeWindow, ccdef.EnteringBlockContentTimeWindowEvent{})
}
