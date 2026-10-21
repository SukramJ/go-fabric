// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"slices"
	"testing"

	mpdef "github.com/SukramJ/go-fabric/cluster/spec/mediaplayback"
	"github.com/SukramJ/go-fabric/im"
)

// transportOnly is a MediaPlayback port with the three mandatory commands
// and none of the optional ones.
type transportOnly struct{}

func (transportOnly) Play(context.Context, *mpdef.State) (mpdef.PlaybackResponse, error) {
	return mpdef.PlaybackResponse{}, nil
}

func (transportOnly) Pause(context.Context, *mpdef.State) (mpdef.PlaybackResponse, error) {
	return mpdef.PlaybackResponse{}, nil
}

func (transportOnly) Stop(context.Context, *mpdef.State) (mpdef.PlaybackResponse, error) {
	return mpdef.PlaybackResponse{}, nil
}

// TestAnOptionalCommandThePortLacksIsUnsupported: a declared Playback
// without the optional commands leaves them out of AcceptedCommandList,
// and a controller's Next answers UNSUPPORTED_COMMAND — matter.js's answer
// for a command the behavior does not implement, not a silent fallback.
func TestAnOptionalCommandThePortLacksIsUnsupported(t *testing.T) {
	t.Parallel()
	playing := mpdef.PlaybackStatePlaying
	d, err := NewCastingVideoPlayer(testOptions("player"), mpdef.Declare{Playback: transportOnly{}, Initial: mpdef.Patch{CurrentState: &playing}})
	if err != nil {
		t.Fatal(err)
	}
	srv := d.Server(mpdef.ClusterID)
	l, ok := srv.(interface{ MatterAcceptedCommands() []uint32 })
	if !ok {
		t.Fatalf("MediaPlayback is %T, which lists no commands", srv)
	}
	if got := l.MatterAcceptedCommands(); !slices.Equal(got, []uint32{mpdef.CmdPlay, mpdef.CmdPause, mpdef.CmdStop}) {
		t.Errorf("AcceptedCommandList %v, want the three mandatory commands", got)
	}
	_, err = srv.MatterInvoke(context.Background(), mpdef.CmdNext, mpdef.NextRequest{})
	var status interface{ MatterStatusCode() im.StatusCode }
	if !errors.As(err, &status) || status.MatterStatusCode() != im.StatusUnsupportedCommand {
		t.Errorf("Next: %v, want UNSUPPORTED_COMMAND", err)
	}
}
