// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media

import (
	"context"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	mpdef "github.com/SukramJ/go-fabric/cluster/spec/mediaplayback"
)

// PlaybackState is the MediaPlayback attribute state (matter.js
// media-playback.element.ts:32-82), the cluster's generated State: the
// transport the attributes report. CurrentState is mandatory; StartTime,
// Duration, SampledPosition, PlaybackSpeed, SeekRangeEnd and SeekRangeStart
// are AdvancedSeek's (AS), the audio tracks AudioTracks' (AT), the text
// tracks TextTracks' (TT). A nil pointer of a nullable attribute — a list
// too — reads as null; an attribute the feature selection does not serve is
// ignored.
type PlaybackState = mpdef.State

func playbackValues(st PlaybackState) map[uint32]any {
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

func clonePlayback(st PlaybackState) PlaybackState {
	st.AvailableAudioTracks = cloneList(st.AvailableAudioTracks)
	st.AvailableTextTracks = cloneList(st.AvailableTextTracks)
	return st
}

// Playback is the MediaPlayback host port (the generated
// [mpdef.Playback], the declaration's Playback): the three mandatory
// transport commands (media-playback.element.ts:96-98), each answering a
// PlaybackResponse (:115-122). st is the state the attributes report; the
// port changes it to what the command did. The optional commands are the
// further interfaces below, accepted when the port implements them.
type Playback = mpdef.Playback

// PlaybackStartOver is StartOver (optional, :99).
type PlaybackStartOver = mpdef.PlaybackStartOver

// PlaybackPrevious is Previous (optional, :100).
type PlaybackPrevious = mpdef.PlaybackPrevious

// PlaybackNext is Next (optional, :101).
type PlaybackNext = mpdef.PlaybackNext

// PlaybackVariableSpeed is Rewind and FastForward, required with
// VariableSpeed (VS, :102-114).
type PlaybackVariableSpeed = mpdef.PlaybackVariableSpeed

// PlaybackSkip is SkipForward and SkipBackward (optional, :123-135).
type PlaybackSkip = mpdef.PlaybackSkip

// PlaybackSeek is Seek, required with AdvancedSeek (AS, :136-140).
type PlaybackSeek = mpdef.PlaybackSeek

// PlaybackAudioTracks is ActivateAudioTrack, required with AudioTracks
// (AT, :141-149); status only.
type PlaybackAudioTracks = mpdef.PlaybackAudioTracks

// PlaybackTextTracks is ActivateTextTrack and DeactivateTextTrack,
// required with TextTracks (TT, :150-158); status only.
type PlaybackTextTracks = mpdef.PlaybackTextTracks

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
	s := &PlaybackServer{}
	h := handlers{}
	type transport = func(ctx context.Context, st *PlaybackState) (mpdef.PlaybackResponse, error)
	// The payload-less transport commands.
	plain := func(cmdID uint32, fn transport) {
		handleState(h, &s.stateful, cmdID, func(ctx context.Context, st *PlaybackState, _ struct{}) (any, error) { return fn(ctx, st) })
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
		handleState(h, &s.stateful, mpdef.CmdRewind, func(ctx context.Context, st *PlaybackState, req mpdef.RewindRequest) (any, error) {
			return speed.Rewind(ctx, st, req)
		})
		handleState(h, &s.stateful, mpdef.CmdFastForward, func(ctx context.Context, st *PlaybackState, req mpdef.FastForwardRequest) (any, error) {
			return speed.FastForward(ctx, st, req)
		})
	}
	if hasSkip {
		handleState(h, &s.stateful, mpdef.CmdSkipForward, func(ctx context.Context, st *PlaybackState, req mpdef.SkipForwardRequest) (any, error) {
			return skip.SkipForward(ctx, st, req)
		})
		handleState(h, &s.stateful, mpdef.CmdSkipBackward, func(ctx context.Context, st *PlaybackState, req mpdef.SkipBackwardRequest) (any, error) {
			return skip.SkipBackward(ctx, st, req)
		})
	}
	if hasSeek {
		handleState(h, &s.stateful, mpdef.CmdSeek, func(ctx context.Context, st *PlaybackState, req mpdef.SeekRequest) (any, error) {
			return seek.Seek(ctx, st, req)
		})
	}
	if hasAudio {
		handleState(h, &s.stateful, mpdef.CmdActivateAudioTrack, statusOnly(audio.ActivateAudioTrack))
	}
	if hasText {
		handleState(h, &s.stateful, mpdef.CmdActivateTextTrack, statusOnly(text.ActivateTextTrack))
		handleState(h, &s.stateful, mpdef.CmdDeactivateTextTrack, func(ctx context.Context, st *PlaybackState, _ mpdef.DeactivateTextTrackRequest) (any, error) {
			return nil, text.DeactivateTextTrack(ctx, st)
		})
	}
	srv, err := newServer(mpdef.Definition, spec.Options{
		Features: uint32(cfg.Features),
		Commands: optionalCommands(cmds),
		Events:   cfg.Events,
	}, spec.ServerConfig{Handlers: h, DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("media: MediaPlayback: %w", err)
	}
	if err := checkImplemented(srv, cmds); err != nil {
		return nil, err
	}
	s.Server = srv
	if err := s.init(srv, cfg.Initial, playbackValues, clonePlayback, storeState[PlaybackState]); err != nil {
		return nil, fmt.Errorf("media: MediaPlayback: %w", err)
	}
	if err := checkState(srv); err != nil {
		return nil, err
	}
	return s, nil
}

// SetAttributes implements [spec.AttributeSetter] ([stateful.setAttributes]).
func (s *PlaybackServer) SetAttributes(values map[uint32]any) error { return s.setAttributes(values) }

// EmitStateChanged emits StateChanged (event 0x00, info; :83-95), which the
// server emits when cfg.Events lists it.
func (s *PlaybackServer) EmitStateChanged(ev mpdef.StateChangedEvent) error {
	return s.Emit(s.endpoint(), mpdef.EventStateChanged, ev)
}
