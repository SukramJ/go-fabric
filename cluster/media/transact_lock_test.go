// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/media"
	mpdef "github.com/SukramJ/go-fabric/cluster/spec/mediaplayback"
)

// reportingTransport reports the device's state through SetState from
// inside Play, as a player that answers at once would.
type reportingTransport struct {
	basicTransport
	srv *media.PlaybackServer
}

func (r *reportingTransport) Play(ctx context.Context, st *media.PlaybackState) (mpdef.PlaybackResponse, error) {
	dev := r.srv.State()
	dev.CurrentState = mpdef.PlaybackStateBuffering
	if err := r.srv.SetState(dev); err != nil {
		return mpdef.PlaybackResponse{}, err
	}
	st.CurrentState = mpdef.PlaybackStatePlaying
	return mpdef.PlaybackResponse{Status: mpdef.StatusSuccess}, nil
}

// TestPortMayCallSetState pins G1-16 (ADR 0026 P6): a command's port runs
// with the state lock released, so a port that calls SetState does not
// deadlock, and the command's own state is the one committed.
func TestPortMayCallSetState(t *testing.T) {
	t.Parallel()
	port := &reportingTransport{}
	srv, err := media.NewPlayback(media.PlaybackConfig{Port: port})
	if err != nil {
		t.Fatal(err)
	}
	port.srv = srv
	done := make(chan error, 1)
	go func() {
		_, err := invoke(t, srv, mpdef.Definition, mpdef.CmdPlay, mpdef.PlayRequest{})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second): // a held lock never releases: this bound only names the failure
		t.Fatal("the server holds its state lock across the port call")
	}
	if got := srv.State().CurrentState; got != mpdef.PlaybackStatePlaying {
		t.Fatalf("CurrentState %d after Play, want Playing (the command wins)", got)
	}
}
