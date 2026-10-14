// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package media

import (
	"context"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	mpdef "github.com/SukramJ/go-fabric/cluster/spec/mediaplayback"
)

// PlaybackState is the MediaPlayback attribute state (matter.js
// media-playback.element.ts:32-82): the transport the attributes report.
// A nil pointer or list of a nullable attribute reads as null; an attribute
// the feature selection does not serve is ignored.
type PlaybackState struct {
	// CurrentState (M).
	CurrentState mpdef.PlaybackStateEnum
	// StartTime, Duration, SampledPosition, PlaybackSpeed, SeekRangeEnd and
	// SeekRangeStart: AdvancedSeek (AS).
	StartTime       *uint64
	Duration        *uint64
	SampledPosition *mpdef.PlaybackPositionStruct
	PlaybackSpeed   float32
	SeekRangeEnd    *uint64
	SeekRangeStart  *uint64
	// ActiveAudioTrack and AvailableAudioTracks: AudioTracks (AT).
	ActiveAudioTrack     *mpdef.TrackStruct
	AvailableAudioTracks []mpdef.TrackStruct
	// ActiveTextTrack and AvailableTextTracks: TextTracks (TT).
	ActiveTextTrack     *mpdef.TrackStruct
	AvailableTextTracks []mpdef.TrackStruct
}

func (st PlaybackState) values() map[uint32]any {
	return map[uint32]any{
		mpdef.AttrCurrentState:         st.CurrentState,
		mpdef.AttrStartTime:            nullOr(st.StartTime),
		mpdef.AttrDuration:             nullOr(st.Duration),
		mpdef.AttrSampledPosition:      nullOr(st.SampledPosition),
		mpdef.AttrPlaybackSpeed:        st.PlaybackSpeed,
		mpdef.AttrSeekRangeEnd:         nullOr(st.SeekRangeEnd),
		mpdef.AttrSeekRangeStart:       nullOr(st.SeekRangeStart),
		mpdef.AttrActiveAudioTrack:     nullOr(st.ActiveAudioTrack),
		mpdef.AttrAvailableAudioTracks: nullListOr(st.AvailableAudioTracks),
		mpdef.AttrActiveTextTrack:      nullOr(st.ActiveTextTrack),
		mpdef.AttrAvailableTextTracks:  nullListOr(st.AvailableTextTracks),
	}
}

func (st PlaybackState) clone() PlaybackState {
	st.AvailableAudioTracks = slices.Clone(st.AvailableAudioTracks)
	st.AvailableTextTracks = slices.Clone(st.AvailableTextTracks)
	return st
}

// nullListOr is l as a generated list, or nil (null) when l is nil.
func nullListOr[T spec.Encodable](l []T) any {
	if l == nil {
		return nil
	}
	return spec.List[T](l)
}

// Playback is the MediaPlayback host port: the three mandatory transport
// commands (media-playback.element.ts:96-98), each answering a
// PlaybackResponse (:115-122). st is the state the attributes report; the
// port changes it to what the command did. The optional commands are the
// further interfaces below, accepted when the port implements them.
type Playback interface {
	Play(ctx context.Context, st *PlaybackState) (mpdef.PlaybackResponse, error)
	Pause(ctx context.Context, st *PlaybackState) (mpdef.PlaybackResponse, error)
	Stop(ctx context.Context, st *PlaybackState) (mpdef.PlaybackResponse, error)
}

// PlaybackStartOver is StartOver (optional, :99).
type PlaybackStartOver interface {
	StartOver(ctx context.Context, st *PlaybackState) (mpdef.PlaybackResponse, error)
}

// PlaybackPrevious is Previous (optional, :100).
type PlaybackPrevious interface {
	Previous(ctx context.Context, st *PlaybackState) (mpdef.PlaybackResponse, error)
}

// PlaybackNext is Next (optional, :101).
type PlaybackNext interface {
	Next(ctx context.Context, st *PlaybackState) (mpdef.PlaybackResponse, error)
}

// PlaybackVariableSpeed is Rewind and FastForward, required with
// VariableSpeed (VS, :102-114).
type PlaybackVariableSpeed interface {
	Rewind(ctx context.Context, st *PlaybackState, req mpdef.RewindRequest) (mpdef.PlaybackResponse, error)
	FastForward(ctx context.Context, st *PlaybackState, req mpdef.FastForwardRequest) (mpdef.PlaybackResponse, error)
}

// PlaybackSkip is SkipForward and SkipBackward (optional, :123-135).
type PlaybackSkip interface {
	SkipForward(ctx context.Context, st *PlaybackState, req mpdef.SkipForwardRequest) (mpdef.PlaybackResponse, error)
	SkipBackward(ctx context.Context, st *PlaybackState, req mpdef.SkipBackwardRequest) (mpdef.PlaybackResponse, error)
}

// PlaybackSeek is Seek, required with AdvancedSeek (AS, :136-140).
type PlaybackSeek interface {
	Seek(ctx context.Context, st *PlaybackState, req mpdef.SeekRequest) (mpdef.PlaybackResponse, error)
}

// PlaybackAudioTracks is ActivateAudioTrack, required with AudioTracks
// (AT, :141-149); status only.
type PlaybackAudioTracks interface {
	ActivateAudioTrack(ctx context.Context, st *PlaybackState, req mpdef.ActivateAudioTrackRequest) error
}

// PlaybackTextTracks is ActivateTextTrack and DeactivateTextTrack,
// required with TextTracks (TT, :150-158); status only.
type PlaybackTextTracks interface {
	ActivateTextTrack(ctx context.Context, st *PlaybackState, req mpdef.ActivateTextTrackRequest) error
	DeactivateTextTrack(ctx context.Context, st *PlaybackState) error
}

// PlaybackConfig carries the construction parameters of a MediaPlayback
// server.
type PlaybackConfig struct {
	// Port carries out the commands; required.
	Port Playback
	// Features is the FeatureMap (AS, VS, TT, AT, AA).
	Features mpdef.Feature
	// Events lists the optional events emitted: EventStateChanged.
	Events []uint32
	// Initial is the state the server starts with.
	Initial PlaybackState
	// DataVersion is an optional host-owned tracker.
	DataVersion dataVersion
}

// PlaybackServer is a MediaPlayback (0x0506) server.
type PlaybackServer struct {
	*spec.Server
	stateful[PlaybackState]
	events
}

// NewPlayback builds a MediaPlayback server over cfg.Port.
func NewPlayback(cfg PlaybackConfig) (*PlaybackServer, error) {
	if cfg.Port == nil {
		return nil, fmt.Errorf("%w: MediaPlayback", ErrNilPort)
	}
	p := cfg.Port
	startOver, hasStartOver := p.(PlaybackStartOver)
	previous, hasPrevious := p.(PlaybackPrevious)
	next, hasNext := p.(PlaybackNext)
	speed, hasSpeed := p.(PlaybackVariableSpeed)
	skip, hasSkip := p.(PlaybackSkip)
	seek, hasSeek := p.(PlaybackSeek)
	audio, hasAudio := p.(PlaybackAudioTracks)
	text, hasText := p.(PlaybackTextTracks)
	cmds := []command{
		{mpdef.CmdPlay, true},
		{mpdef.CmdPause, true},
		{mpdef.CmdStop, true},
		{mpdef.CmdStartOver, hasStartOver},
		{mpdef.CmdPrevious, hasPrevious},
		{mpdef.CmdNext, hasNext},
		{mpdef.CmdRewind, hasSpeed},
		{mpdef.CmdFastForward, hasSpeed},
		{mpdef.CmdSkipForward, hasSkip},
		{mpdef.CmdSkipBackward, hasSkip},
		{mpdef.CmdSeek, hasSeek},
		{mpdef.CmdActivateAudioTrack, hasAudio},
		{mpdef.CmdActivateTextTrack, hasText},
		{mpdef.CmdDeactivateTextTrack, hasText},
	}
	srv, err := spec.NewServer(mpdef.Definition, spec.Options{
		Features: uint32(cfg.Features),
		Commands: optionalCommands(cmds),
		Events:   cfg.Events,
	}, spec.ServerConfig{DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("media: MediaPlayback: %w", err)
	}
	if err := checkImplemented(srv, cmds); err != nil {
		return nil, err
	}
	s := &PlaybackServer{Server: srv}
	if err := s.init(srv, cfg.Initial, PlaybackState.values, PlaybackState.clone); err != nil {
		return nil, fmt.Errorf("media: MediaPlayback: %w", err)
	}
	if err := checkState(srv); err != nil {
		return nil, err
	}
	type transport = func(ctx context.Context, st *PlaybackState) (mpdef.PlaybackResponse, error)
	// The payload-less transport commands.
	plain := func(cmdID uint32, fn transport) {
		handleState(srv, &s.stateful, cmdID, func(ctx context.Context, st *PlaybackState, _ struct{}) (any, error) { return fn(ctx, st) })
	}
	plain(mpdef.CmdPlay, p.Play)
	plain(mpdef.CmdPause, p.Pause)
	plain(mpdef.CmdStop, p.Stop)
	if hasStartOver {
		plain(mpdef.CmdStartOver, startOver.StartOver)
	}
	if hasPrevious {
		plain(mpdef.CmdPrevious, previous.Previous)
	}
	if hasNext {
		plain(mpdef.CmdNext, next.Next)
	}
	if hasSpeed {
		handleState(srv, &s.stateful, mpdef.CmdRewind, func(ctx context.Context, st *PlaybackState, req mpdef.RewindRequest) (any, error) {
			return speed.Rewind(ctx, st, req)
		})
		handleState(srv, &s.stateful, mpdef.CmdFastForward, func(ctx context.Context, st *PlaybackState, req mpdef.FastForwardRequest) (any, error) {
			return speed.FastForward(ctx, st, req)
		})
	}
	if hasSkip {
		handleState(srv, &s.stateful, mpdef.CmdSkipForward, func(ctx context.Context, st *PlaybackState, req mpdef.SkipForwardRequest) (any, error) {
			return skip.SkipForward(ctx, st, req)
		})
		handleState(srv, &s.stateful, mpdef.CmdSkipBackward, func(ctx context.Context, st *PlaybackState, req mpdef.SkipBackwardRequest) (any, error) {
			return skip.SkipBackward(ctx, st, req)
		})
	}
	if hasSeek {
		handleState(srv, &s.stateful, mpdef.CmdSeek, func(ctx context.Context, st *PlaybackState, req mpdef.SeekRequest) (any, error) {
			return seek.Seek(ctx, st, req)
		})
	}
	if hasAudio {
		handleState(srv, &s.stateful, mpdef.CmdActivateAudioTrack, statusOnly(audio.ActivateAudioTrack))
	}
	if hasText {
		handleState(srv, &s.stateful, mpdef.CmdActivateTextTrack, statusOnly(text.ActivateTextTrack))
		handleState(srv, &s.stateful, mpdef.CmdDeactivateTextTrack, func(ctx context.Context, st *PlaybackState, _ mpdef.DeactivateTextTrackRequest) (any, error) {
			return nil, text.DeactivateTextTrack(ctx, st)
		})
	}
	return s, nil
}

// EmitStateChanged emits StateChanged (event 0x00, info; :83-95), which the
// server emits when cfg.Events lists it.
func (s *PlaybackServer) EmitStateChanged(ev mpdef.StateChangedEvent) error {
	return s.Emit(s.endpoint(), mpdef.EventStateChanged, ev)
}
