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
	mpdef "github.com/SukramJ/go-fabric/cluster/spec/mediaplayback"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/im"
)

// transport is a Playback port with every optional command: it records
// the calls and the decoded requests, and moves the state as a player
// would.
type transport struct {
	calls []string
	reqs  []any
	err   error
}

func ok() (mpdef.PlaybackResponse, error) {
	return mpdef.PlaybackResponse{Status: mpdef.StatusSuccess, Data: spectest.Ptr("data response")}, nil
}

func (p *transport) step(name string, req any, st *media.PlaybackState, state mpdef.PlaybackStateEnum, speed float32) (mpdef.PlaybackResponse, error) {
	p.calls = append(p.calls, name)
	p.reqs = append(p.reqs, req)
	if p.err != nil {
		st.CurrentState = mpdef.PlaybackStateBuffering // a failed command must not commit this
		return mpdef.PlaybackResponse{}, p.err
	}
	st.CurrentState, st.PlaybackSpeed = state, speed
	return ok()
}

func (p *transport) Play(_ context.Context, st *media.PlaybackState) (mpdef.PlaybackResponse, error) {
	return p.step("Play", nil, st, mpdef.PlaybackStatePlaying, 1)
}

func (p *transport) Pause(_ context.Context, st *media.PlaybackState) (mpdef.PlaybackResponse, error) {
	return p.step("Pause", nil, st, mpdef.PlaybackStatePaused, 0)
}

func (p *transport) Stop(_ context.Context, st *media.PlaybackState) (mpdef.PlaybackResponse, error) {
	return p.step("Stop", nil, st, mpdef.PlaybackStateNotPlaying, 0)
}

func (p *transport) StartOver(_ context.Context, st *media.PlaybackState) (mpdef.PlaybackResponse, error) {
	return p.step("StartOver", nil, st, st.CurrentState, st.PlaybackSpeed)
}

func (p *transport) Previous(_ context.Context, st *media.PlaybackState) (mpdef.PlaybackResponse, error) {
	return p.step("Previous", nil, st, mpdef.PlaybackStatePlaying, 1)
}

func (p *transport) Next(_ context.Context, st *media.PlaybackState) (mpdef.PlaybackResponse, error) {
	return p.step("Next", nil, st, mpdef.PlaybackStatePlaying, 1)
}

func (p *transport) Rewind(_ context.Context, st *media.PlaybackState, req mpdef.RewindRequest) (mpdef.PlaybackResponse, error) {
	return p.step("Rewind", req, st, mpdef.PlaybackStatePlaying, -1)
}

func (p *transport) FastForward(_ context.Context, st *media.PlaybackState, req mpdef.FastForwardRequest) (mpdef.PlaybackResponse, error) {
	return p.step("FastForward", req, st, mpdef.PlaybackStatePlaying, 2)
}

func (p *transport) SkipForward(_ context.Context, st *media.PlaybackState, req mpdef.SkipForwardRequest) (mpdef.PlaybackResponse, error) {
	st.SampledPosition = &mpdef.PlaybackPositionStruct{Position: spec.Nullable[uint64]{Value: req.DeltaPositionMilliseconds}}
	return p.step("SkipForward", req, st, st.CurrentState, st.PlaybackSpeed)
}

func (p *transport) SkipBackward(_ context.Context, st *media.PlaybackState, req mpdef.SkipBackwardRequest) (mpdef.PlaybackResponse, error) {
	return p.step("SkipBackward", req, st, st.CurrentState, st.PlaybackSpeed)
}

func (p *transport) Seek(_ context.Context, st *media.PlaybackState, req mpdef.SeekRequest) (mpdef.PlaybackResponse, error) {
	if st.Duration != nil && req.Position > *st.Duration {
		p.calls = append(p.calls, "Seek")
		return mpdef.PlaybackResponse{Status: mpdef.StatusSeekOutOfRange}, nil
	}
	st.SampledPosition = &mpdef.PlaybackPositionStruct{Position: spec.Nullable[uint64]{Value: req.Position}}
	return p.step("Seek", req, st, st.CurrentState, st.PlaybackSpeed)
}

func (p *transport) ActivateAudioTrack(_ context.Context, st *media.PlaybackState, req mpdef.ActivateAudioTrackRequest) error {
	p.calls, p.reqs = append(p.calls, "ActivateAudioTrack"), append(p.reqs, req)
	for _, tr := range st.AvailableAudioTracks {
		if tr.Id == req.TrackId {
			st.ActiveAudioTrack = &tr
			return nil
		}
	}
	return media.StatusError{Status: im.StatusInvalidCommand}
}

func (p *transport) ActivateTextTrack(_ context.Context, st *media.PlaybackState, req mpdef.ActivateTextTrackRequest) error {
	p.calls, p.reqs = append(p.calls, "ActivateTextTrack"), append(p.reqs, req)
	st.ActiveTextTrack = &mpdef.TrackStruct{Id: req.TrackId, TrackAttributes: mpdef.TrackAttributesStruct{LanguageCode: "en"}}
	return nil
}

func (p *transport) DeactivateTextTrack(_ context.Context, st *media.PlaybackState) error {
	p.calls = append(p.calls, "DeactivateTextTrack")
	st.ActiveTextTrack = nil
	return nil
}

// basicTransport implements the mandatory commands only.
type basicTransport struct{ inner transport }

func (b *basicTransport) Play(ctx context.Context, st *media.PlaybackState) (mpdef.PlaybackResponse, error) {
	return b.inner.Play(ctx, st)
}

func (b *basicTransport) Pause(ctx context.Context, st *media.PlaybackState) (mpdef.PlaybackResponse, error) {
	return b.inner.Pause(ctx, st)
}

func (b *basicTransport) Stop(ctx context.Context, st *media.PlaybackState) (mpdef.PlaybackResponse, error) {
	return b.inner.Stop(ctx, st)
}

const allPlaybackFeatures = mpdef.FeatureAdvancedSeek | mpdef.FeatureVariableSpeed | mpdef.FeatureTextTracks |
	mpdef.FeatureAudioTracks | mpdef.FeatureAudioAdvance

func newPlayer(t *testing.T) (*media.PlaybackServer, *transport) {
	t.Helper()
	port := &transport{}
	duration := uint64(80000)
	srv, err := media.NewPlayback(media.PlaybackConfig{
		Port:     port,
		Features: allPlaybackFeatures,
		Events:   []uint32{mpdef.EventStateChanged},
		Initial: media.PlaybackState{
			CurrentState: mpdef.PlaybackStatePaused, Duration: &duration,
			AvailableAudioTracks: []mpdef.TrackStruct{{Id: "a0", TrackAttributes: mpdef.TrackAttributesStruct{LanguageCode: "en"}}},
			AvailableTextTracks:  []mpdef.TrackStruct{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv, port
}

// TestPlaybackTransportCommands: every command reaches the port with its
// decoded fields, the PlaybackResponse goes out as the port built it, and
// the state the port changed is what the attributes then read.
func TestPlaybackTransportCommands(t *testing.T) {
	t.Parallel()
	srv, port := newPlayer(t)
	got := changes(t, srv)
	for _, c := range []struct {
		cmd   uint32
		req   spec.Encodable
		state mpdef.PlaybackStateEnum
		speed float32
	}{
		{mpdef.CmdPlay, mpdef.PlayRequest{}, mpdef.PlaybackStatePlaying, 1},
		{mpdef.CmdPause, mpdef.PauseRequest{}, mpdef.PlaybackStatePaused, 0},
		{mpdef.CmdFastForward, mpdef.FastForwardRequest{AudioAdvanceUnmuted: spectest.Ptr(true)}, mpdef.PlaybackStatePlaying, 2},
		{mpdef.CmdRewind, mpdef.RewindRequest{}, mpdef.PlaybackStatePlaying, -1},
		{mpdef.CmdNext, mpdef.NextRequest{}, mpdef.PlaybackStatePlaying, 1},
		{mpdef.CmdPrevious, mpdef.PreviousRequest{}, mpdef.PlaybackStatePlaying, 1},
		{mpdef.CmdStartOver, mpdef.StartOverRequest{}, mpdef.PlaybackStatePlaying, 1},
		{mpdef.CmdStop, mpdef.StopRequest{}, mpdef.PlaybackStateNotPlaying, 0},
	} {
		resp, err := invoke(t, srv, mpdef.Definition, c.cmd, c.req)
		if err != nil {
			t.Fatalf("0x%02X: %v", c.cmd, err)
		}
		r, _ := ok()
		wantResponse(t, mpdef.Definition, mpdef.CmdPlaybackResponse, resp.(mpdef.PlaybackResponse), r)
		if v := read(t, srv, mpdef.AttrCurrentState); v != uint8(c.state) {
			t.Errorf("0x%02X: CurrentState %v, want %d", c.cmd, v, c.state)
		}
		if v := read(t, srv, mpdef.AttrPlaybackSpeed); v != c.speed {
			t.Errorf("0x%02X: PlaybackSpeed %v, want %v", c.cmd, v, c.speed)
		}
	}
	if want := []string{"Play", "Pause", "FastForward", "Rewind", "Next", "Previous", "StartOver", "Stop"}; !slices.Equal(port.calls, want) {
		t.Errorf("calls %v, want %v", port.calls, want)
	}
	if ff, _ := port.reqs[2].(mpdef.FastForwardRequest); ff.AudioAdvanceUnmuted == nil || !*ff.AudioAdvanceUnmuted {
		t.Errorf("FastForward request %+v, want AudioAdvanceUnmuted true", port.reqs[2])
	}
	if !slices.Contains(*got, mpdef.AttrCurrentState) || !slices.Contains(*got, mpdef.AttrPlaybackSpeed) {
		t.Errorf("reported %v, want CurrentState and PlaybackSpeed", *got)
	}
	if st := srv.State(); st.CurrentState != mpdef.PlaybackStateNotPlaying {
		t.Errorf("State %+v", st)
	}
}

// TestPlaybackSeekAndSkip: the position commands move SampledPosition; a
// Seek past the end answers the port's SeekOutOfRange in the response.
func TestPlaybackSeekAndSkip(t *testing.T) {
	t.Parallel()
	srv, port := newPlayer(t)
	if _, err := invoke(t, srv, mpdef.Definition, mpdef.CmdSeek, mpdef.SeekRequest{Position: 10000}); err != nil {
		t.Fatal(err)
	}
	if v := read(t, srv, mpdef.AttrSampledPosition).(mpdef.PlaybackPositionStruct); v.Position.Value != 10000 {
		t.Errorf("SampledPosition %+v", v)
	}
	resp, err := invoke(t, srv, mpdef.Definition, mpdef.CmdSeek, mpdef.SeekRequest{Position: 100_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if r := resp.(mpdef.PlaybackResponse); r.Status != mpdef.StatusSeekOutOfRange {
		t.Errorf("Seek past the end: %+v", r)
	}
	if _, err := invoke(t, srv, mpdef.Definition, mpdef.CmdSkipForward, mpdef.SkipForwardRequest{DeltaPositionMilliseconds: 500}); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke(t, srv, mpdef.Definition, mpdef.CmdSkipBackward, mpdef.SkipBackwardRequest{DeltaPositionMilliseconds: 500}); err != nil {
		t.Fatal(err)
	}
	if last := port.reqs[len(port.reqs)-1].(mpdef.SkipBackwardRequest); last.DeltaPositionMilliseconds != 500 {
		t.Errorf("SkipBackward request %+v", last)
	}
	// The nullable AS attributes read null until the port sets them.
	if v := read(t, srv, mpdef.AttrStartTime); v != nil {
		t.Errorf("StartTime %v, want null", v)
	}
	if v := read(t, srv, mpdef.AttrDuration); v != uint64(80000) {
		t.Errorf("Duration %v", v)
	}
}

// TestPlaybackTracks: the track commands are status-only; a StatusError
// from the port is the invoke's status and commits nothing.
func TestPlaybackTracks(t *testing.T) {
	t.Parallel()
	srv, _ := newPlayer(t)
	if resp, err := invoke(t, srv, mpdef.Definition, mpdef.CmdActivateAudioTrack, mpdef.ActivateAudioTrackRequest{TrackId: "a0"}); err != nil || resp != nil {
		t.Fatalf("ActivateAudioTrack: %v %v", resp, err)
	}
	if v := read(t, srv, mpdef.AttrActiveAudioTrack).(mpdef.TrackStruct); v.Id != "a0" {
		t.Errorf("ActiveAudioTrack %+v", v)
	}
	_, err := invoke(t, srv, mpdef.Definition, mpdef.CmdActivateAudioTrack, mpdef.ActivateAudioTrackRequest{TrackId: "none"})
	wantStatus(t, err, im.StatusInvalidCommand)
	if v := read(t, srv, mpdef.AttrActiveAudioTrack).(mpdef.TrackStruct); v.Id != "a0" {
		t.Errorf("a refused command changed ActiveAudioTrack: %+v", v)
	}
	if _, err := invoke(t, srv, mpdef.Definition, mpdef.CmdActivateTextTrack, mpdef.ActivateTextTrackRequest{TrackId: "t1"}); err != nil {
		t.Fatal(err)
	}
	if v := read(t, srv, mpdef.AttrActiveTextTrack).(mpdef.TrackStruct); v.Id != "t1" {
		t.Errorf("ActiveTextTrack %+v", v)
	}
	if _, err := invoke(t, srv, mpdef.Definition, mpdef.CmdDeactivateTextTrack, mpdef.DeactivateTextTrackRequest{}); err != nil {
		t.Fatal(err)
	}
	if v := read(t, srv, mpdef.AttrActiveTextTrack); v != nil {
		t.Errorf("ActiveTextTrack %v, want null", v)
	}
	if v := read(t, srv, mpdef.AttrAvailableTextTracks); v == nil {
		t.Error("AvailableTextTracks null, want the empty list")
	}
}

// TestPlaybackPortError: a plain error is FAILURE, and the state the port
// changed before failing is not committed.
func TestPlaybackPortError(t *testing.T) {
	t.Parallel()
	srv, port := newPlayer(t)
	port.err = errPort
	_, err := invoke(t, srv, mpdef.Definition, mpdef.CmdPlay, mpdef.PlayRequest{})
	wantStatus(t, err, im.StatusFailure)
	if !errors.Is(err, errPort) {
		t.Errorf("error %v does not wrap the port's", err)
	}
	if v := read(t, srv, mpdef.AttrCurrentState); v != uint8(mpdef.PlaybackStatePaused) {
		t.Errorf("CurrentState %v after a failed Play", v)
	}
	port.err = media.StatusError{Status: im.StatusBusy}
	_, err = invoke(t, srv, mpdef.Definition, mpdef.CmdPause, mpdef.PauseRequest{})
	wantStatus(t, err, im.StatusBusy)
}

// TestPlaybackOptionalCommands: a port without the optional commands
// leaves them out of AcceptedCommandList and they answer
// UNSUPPORTED_COMMAND; a feature whose commands the port lacks refuses
// construction.
func TestPlaybackOptionalCommands(t *testing.T) {
	t.Parallel()
	srv, err := media.NewPlayback(media.PlaybackConfig{Port: &basicTransport{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := srv.MatterAcceptedCommands(); !slices.Equal(got, []uint32{mpdef.CmdPlay, mpdef.CmdPause, mpdef.CmdStop}) {
		t.Errorf("AcceptedCommandList %v", got)
	}
	_, err = invoke(t, srv, mpdef.Definition, mpdef.CmdNext, mpdef.NextRequest{})
	wantStatus(t, err, im.StatusUnsupportedCommand)
	spectest.CheckServer(t, srv, mpdef.Definition, 0)

	for _, f := range []mpdef.Feature{mpdef.FeatureAdvancedSeek, mpdef.FeatureVariableSpeed, mpdef.FeatureTextTracks, mpdef.FeatureAudioTracks} {
		_, err := media.NewPlayback(media.PlaybackConfig{
			Port: &basicTransport{}, Features: f,
			Initial: media.PlaybackState{AvailableAudioTracks: []mpdef.TrackStruct{}, AvailableTextTracks: []mpdef.TrackStruct{}},
		})
		if !errors.Is(err, media.ErrNotImplemented) {
			t.Errorf("feature 0x%X without its commands: %v, want ErrNotImplemented", f, err)
		}
	}
	if _, err := media.NewPlayback(media.PlaybackConfig{}); !errors.Is(err, media.ErrNilPort) {
		t.Errorf("no port: %v", err)
	}
}

// TestPlaybackSetStateAndEvent: a device-side change is checked and
// reported; StateChanged goes to the stamped endpoint at info priority.
func TestPlaybackSetStateAndEvent(t *testing.T) {
	t.Parallel()
	srv, _ := newPlayer(t)
	got := changes(t, srv)
	st := srv.State()
	st.CurrentState = mpdef.PlaybackStateBuffering
	if err := srv.SetState(st); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*got, []uint32{mpdef.AttrCurrentState}) {
		t.Errorf("reported %v", *got)
	}
	st.CurrentState = 9 // no PlaybackStateEnum value
	if err := srv.SetState(st); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("SetState out of range: %v", err)
	}
	if v := read(t, srv, mpdef.AttrCurrentState); v != uint8(mpdef.PlaybackStateBuffering) {
		t.Errorf("CurrentState %v after a refused SetState", v)
	}
	ev := &emitted{}
	srv.SetMatterEventEmitter(ev)
	srv.SetEndpoint(7)
	if err := srv.EmitStateChanged(mpdef.StateChangedEvent{CurrentState: mpdef.PlaybackStatePlaying}); err != nil {
		t.Fatal(err)
	}
	if len(ev.events) != 1 || ev.events[0].endpoint != 7 || ev.events[0].event != mpdef.EventStateChanged {
		t.Errorf("events %+v", ev.events)
	}
}
