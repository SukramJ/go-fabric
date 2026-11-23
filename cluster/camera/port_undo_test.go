// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/camera"
	csdef "thingfabric.dev/go-fabric/cluster/spec/cameraavstreammanagement"
	zmdef "thingfabric.dev/go-fabric/cluster/spec/zonemanagement"
	"thingfabric.dev/go-fabric/im"
)

// undoStreams is the chip mock that records every deallocation and, with
// reuse set, answers a video allocation with that stream's id.
type undoStreams struct {
	*mockStreams
	undoMu sync.Mutex
	undone []string
	reuse  uint16
}

func (u *undoStreams) record(what string, id uint16) {
	u.undoMu.Lock()
	u.undone = append(u.undone, fmt.Sprintf("%s %d", what, id))
	u.undoMu.Unlock()
}

func (u *undoStreams) undoneList() []string {
	u.undoMu.Lock()
	defer u.undoMu.Unlock()
	return slices.Clone(u.undone)
}

func (u *undoStreams) VideoStreamAllocate(ctx context.Context, args camera.VideoStream) (uint16, error) {
	if u.reuse != 0 {
		return u.reuse, nil
	}
	return u.mockStreams.VideoStreamAllocate(ctx, args)
}

func (u *undoStreams) VideoStreamDeallocate(ctx context.Context, id uint16) error {
	u.record("video", id)
	return u.mockStreams.VideoStreamDeallocate(ctx, id)
}

func (u *undoStreams) AudioStreamDeallocate(ctx context.Context, id uint16) error {
	u.record("audio", id)
	return u.mockStreams.AudioStreamDeallocate(ctx, id)
}

func (u *undoStreams) SnapshotStreamDeallocate(ctx context.Context, id uint16) error {
	u.record("snapshot", id)
	return u.mockStreams.SnapshotStreamDeallocate(ctx, id)
}

func newUndoAvStream(t *testing.T) (*camera.AvStream, *undoStreams) {
	t.Helper()
	u := &undoStreams{mockStreams: &mockStreams{}}
	cfg := chipAvStreamConfig(u.mockStreams)
	cfg.Video, cfg.Audio, cfg.Snapshot = u, u, u
	a, err := camera.NewAvStream(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a, u
}

// undoZones is the chip mock that records the zones it removes.
type undoZones struct {
	*mockZones
	undoMu  sync.Mutex
	removed []uint16
}

func (u *undoZones) RemoveZone(ctx context.Context, id uint16) error {
	u.undoMu.Lock()
	u.removed = append(u.removed, id)
	u.undoMu.Unlock()
	return u.mockZones.RemoveZone(ctx, id)
}

// refuseOnce refuses the first commit and admits every later one.
func refuseOnce() func() error {
	var once sync.Once
	return func() error {
		err := error(nil)
		once.Do(func() { err = errors.New("table refused") })
		return err
	}
}

// TestRefusedCommitUndoesThePortsAllocation (R2-C2-err-1602, decision 534):
// at every site where the port creates something before the table stores
// it — the three stream allocations and the zone create — a refused
// commit undoes it at the port before the command answers FAILURE, so no
// stream or zone runs on that the table does not hold and no controller
// could end. The standing guard of the class for AvStream and Zones; the
// Provider and PushTransport sites are TestProviderEndsTheSessionItCouldNotStore
// and TestPushDeallocatesTheConnectionItCouldNotStore.
func TestRefusedCommitUndoesThePortsAllocation(t *testing.T) {
	t.Parallel()
	streams := []struct {
		name   string
		cmd    uint32
		req    any
		undone string
		held   func(*camera.AvStream) int
	}{
		{"VideoStreamAllocate", csdef.CmdVideoStreamAllocate, videoAllocate(), "video 1", func(a *camera.AvStream) int { return len(a.VideoStreams()) }},
		{"AudioStreamAllocate", csdef.CmdAudioStreamAllocate, audioAllocate(), "audio 1", func(a *camera.AvStream) int { return len(a.AudioStreams()) }},
		{"SnapshotStreamAllocate", csdef.CmdSnapshotStreamAllocate, snapshotAllocate(), "snapshot 1", func(a *camera.AvStream) int { return len(a.SnapshotStreams()) }},
	}
	for _, c := range streams {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			a, u := newUndoAvStream(t)
			camera.SetAvStreamCommitRefusal(a, refuseOnce())
			_, st := invokeStream(a, c.cmd, c.req)
			wantCode(t, c.name, st, im.StatusFailure)
			if got := u.undoneList(); !slices.Equal(got, []string{c.undone}) {
				t.Errorf("deallocated at the port %v, want [%s]", got, c.undone)
			}
			if n := c.held(a); n != 0 {
				t.Errorf("%d streams stored", n)
			}
			// The port's slot is free again: the next allocation succeeds.
			_, st = invokeStream(a, c.cmd, c.req)
			wantCode(t, c.name+" again", st, im.StatusSuccess)
		})
	}
	t.Run("CreateTwoDCartesianZone", func(t *testing.T) {
		t.Parallel()
		u := &undoZones{mockZones: &mockZones{}}
		z, _ := newChipZones(t, twoDUser, func(cfg *camera.ZonesConfig) { cfg.Port = u })
		camera.SetZoneCommitRefusal(z, refuseOnce())
		_, st := invokeZones(z, zmdef.CmdCreateTwoDCartesianZone, createZoneRequest("Zone One", triangle(0)))
		wantCode(t, "create", st, im.StatusFailure)
		u.undoMu.Lock()
		removed := slices.Clone(u.removed)
		u.undoMu.Unlock()
		if !slices.Equal(removed, []uint16{0}) {
			t.Errorf("removed at the port %v, want [0]", removed)
		}
		if n := len(z.ZoneList()); n != 0 {
			t.Errorf("%d zones stored", n)
		}
		// The ID the port gave up is handed out again.
		resp, st := invokeZones(z, zmdef.CmdCreateTwoDCartesianZone, createZoneRequest("Zone One", triangle(0)))
		wantCode(t, "create again", st, im.StatusSuccess)
		if id := resp.(zmdef.CreateTwoDCartesianZoneResponse).ZoneId; id != 0 {
			t.Errorf("zone id %d, want 0", id)
		}
	})
}

// TestRefusedNarrowingKeepsTheReusedStream (R2-C2-err-1602, decision 534):
// a stream the port reuses is one the table already holds, and a
// controller can still deallocate it; a narrowing whose commit is refused
// answers FAILURE and does not deallocate it.
func TestRefusedNarrowingKeepsTheReusedStream(t *testing.T) {
	t.Parallel()
	a, u := newUndoAvStream(t)
	resp, st := invokeStream(a, csdef.CmdVideoStreamAllocate, videoAllocate()) // 30 to 120 fps
	wantCode(t, "allocate", st, im.StatusSuccess)
	u.reuse = resp.(csdef.VideoStreamAllocateResponse).VideoStreamId
	camera.SetAvStreamCommitRefusal(a, refuseOnce())
	req := videoAllocate()
	req.MinFrameRate = 60 // narrows the stream to 60 to 120
	_, st = invokeStream(a, csdef.CmdVideoStreamAllocate, req)
	wantCode(t, "reuse", st, im.StatusFailure)
	if got := u.undoneList(); len(got) != 0 {
		t.Errorf("deallocated at the port %v, want none", got)
	}
	if l := a.VideoStreams(); len(l) != 1 || l[0].VideoStreamId != u.reuse || l[0].MinFrameRate != 30 {
		t.Errorf("streams %+v, want stream %d as allocated", l, u.reuse)
	}
}
