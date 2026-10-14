// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package media

import (
	"context"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	channeldef "github.com/SukramJ/go-fabric/cluster/spec/channel"
)

// ChannelState is the Channel attribute state (channel.element.ts:30-41).
// ChannelList (ChannelList feature, CL) defaults to the empty list (:31);
// Lineup (LineupInfo, LI) and CurrentChannel (optional) are nullable and
// read as null while nil.
type ChannelState struct {
	ChannelList    []channeldef.ChannelInfoStruct
	Lineup         *channeldef.LineupInfoStruct
	CurrentChannel *channeldef.ChannelInfoStruct
}

func (st ChannelState) values() map[uint32]any {
	list := st.ChannelList
	if list == nil {
		list = []channeldef.ChannelInfoStruct{} // the element's default, :31
	}
	return map[uint32]any{
		channeldef.AttrChannelList:    spec.List[channeldef.ChannelInfoStruct](list),
		channeldef.AttrLineup:         nullOr(st.Lineup),
		channeldef.AttrCurrentChannel: nullOr(st.CurrentChannel),
	}
}

func (st ChannelState) clone() ChannelState {
	st.ChannelList = slices.Clone(st.ChannelList)
	return st
}

// Channels is the Channel host port. ChangeChannelByNumber and SkipChannel
// are mandatory (:57-69), status only; ChangeChannel (CL | LI, :43-55)
// answers ChangeChannelResponse. The port tunes and makes the channel
// current in st. A port that also implements [ChannelGuide] takes the
// ElectronicGuide commands, and [ChannelRecorder] RecordProgram's.
type Channels interface {
	ChangeChannel(ctx context.Context, st *ChannelState, req channeldef.ChangeChannelRequest) (channeldef.ChangeChannelResponse, error)
	ChangeChannelByNumber(ctx context.Context, st *ChannelState, req channeldef.ChangeChannelByNumberRequest) error
	SkipChannel(ctx context.Context, st *ChannelState, req channeldef.SkipChannelRequest) error
}

// ChannelGuide is GetProgramGuide, required with ElectronicGuide (EG,
// :71-98).
type ChannelGuide interface {
	GetProgramGuide(ctx context.Context, st *ChannelState, req channeldef.GetProgramGuideRequest) (channeldef.ProgramGuideResponse, error)
}

// ChannelRecorder is RecordProgram and CancelRecordProgram, required with
// RecordProgram and ElectronicGuide (RP & EG, :100-126); status only.
type ChannelRecorder interface {
	RecordProgram(ctx context.Context, st *ChannelState, req channeldef.RecordProgramRequest) error
	CancelRecordProgram(ctx context.Context, st *ChannelState, req channeldef.CancelRecordProgramRequest) error
}

// ChannelsConfig carries the construction parameters of a Channel server.
type ChannelsConfig struct {
	// Port carries out the commands; required.
	Port Channels
	// Features is the FeatureMap (CL, LI, EG, RP).
	Features channeldef.Feature
	// Attributes lists the optional attributes served: AttrCurrentChannel.
	Attributes []uint32
	// Initial is the state the server starts with.
	Initial ChannelState
	// DataVersion is an optional host-owned tracker.
	DataVersion dataVersion
}

// ChannelsServer is a Channel (0x0504) server.
type ChannelsServer struct {
	*spec.Server
	stateful[ChannelState]
}

// NewChannels builds a Channel server over cfg.Port.
func NewChannels(cfg ChannelsConfig) (*ChannelsServer, error) {
	if cfg.Port == nil {
		return nil, fmt.Errorf("%w: Channel", ErrNilPort)
	}
	port := cfg.Port
	guide, hasGuide := port.(ChannelGuide)
	recorder, hasRecorder := port.(ChannelRecorder)
	cmds := []command{
		{channeldef.CmdChangeChannel, true},
		{channeldef.CmdChangeChannelByNumber, true},
		{channeldef.CmdSkipChannel, true},
		{channeldef.CmdGetProgramGuide, hasGuide},
		{channeldef.CmdRecordProgram, hasRecorder},
		{channeldef.CmdCancelRecordProgram, hasRecorder},
	}
	srv, err := spec.NewServer(channeldef.Definition, spec.Options{Features: uint32(cfg.Features), Attributes: cfg.Attributes},
		spec.ServerConfig{DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("media: Channel: %w", err)
	}
	if err := checkImplemented(srv, cmds); err != nil {
		return nil, err
	}
	s := &ChannelsServer{Server: srv}
	if err := s.init(srv, cfg.Initial, ChannelState.values, ChannelState.clone); err != nil {
		return nil, fmt.Errorf("media: Channel: %w", err)
	}
	handleState(srv, &s.stateful, channeldef.CmdChangeChannel, func(ctx context.Context, st *ChannelState, req channeldef.ChangeChannelRequest) (any, error) {
		return port.ChangeChannel(ctx, st, req)
	})
	handleState(srv, &s.stateful, channeldef.CmdChangeChannelByNumber, statusOnly(port.ChangeChannelByNumber))
	handleState(srv, &s.stateful, channeldef.CmdSkipChannel, statusOnly(port.SkipChannel))
	if hasGuide {
		handleState(srv, &s.stateful, channeldef.CmdGetProgramGuide, func(ctx context.Context, st *ChannelState, req channeldef.GetProgramGuideRequest) (any, error) {
			return guide.GetProgramGuide(ctx, st, req)
		})
	}
	if hasRecorder {
		handleState(srv, &s.stateful, channeldef.CmdRecordProgram, statusOnly(recorder.RecordProgram))
		handleState(srv, &s.stateful, channeldef.CmdCancelRecordProgram, statusOnly(recorder.CancelRecordProgram))
	}
	return s, nil
}
