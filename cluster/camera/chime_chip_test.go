// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Chime: a port of connectedhomeip
// src/app/clusters/chime-server/tests/TestChimeCluster.cpp at 6170af84, by
// meaning. chip's ClusterTester reads, writes and invokes the cluster
// directly; here the server's MatterRead, MatterWrite and MatterInvoke do,
// the MockChimeDelegate (:40-81) is mockChime, and chip's dirty list is the
// server's change notification.
package camera_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	chimedef "github.com/SukramJ/go-fabric/cluster/spec/chime"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// mockChime is MockChimeDelegate (:40-81): two sounds, a recorded play and
// a configurable answer.
type mockChime struct {
	mu        sync.Mutex
	sounds    []camera.ChimeSound
	played    []uint8
	playErr   error
	soundsErr error
}

func newMockChime() *mockChime {
	return &mockChime{sounds: []camera.ChimeSound{{ChimeId: 1, Name: "Ding Dong"}, {ChimeId: 2, Name: "Ring Ring"}}}
}

func (m *mockChime) InstalledChimeSounds() ([]camera.ChimeSound, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.soundsErr != nil {
		return nil, m.soundsErr
	}
	return slices.Clone(m.sounds), nil
}

func (m *mockChime) PlayChimeSound(_ context.Context, id uint8) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.played = append(m.played, id)
	return m.playErr
}

func newChime(t *testing.T, port camera.ChimePort, edit ...func(*camera.ChimeConfig)) *camera.Chime {
	t.Helper()
	cfg := camera.ChimeConfig{Port: port}
	for _, e := range edit {
		e(&cfg)
	}
	c, err := camera.NewChime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// statusOf is the IM status an error answers, SUCCESS for none.
func statusOf(err error) im.StatusCode {
	if err == nil {
		return im.StatusSuccess
	}
	var sc im.StatusCodeError
	if errors.As(err, &sc) {
		return sc.MatterStatusCode()
	}
	return im.StatusFailure
}

func readChime[T any](t *testing.T, c *camera.Chime, attrID uint32) T {
	t.Helper()
	v, ok := c.MatterRead(attrID)
	if !ok {
		t.Fatalf("attribute 0x%04X not readable", attrID)
	}
	got, ok := v.(T)
	if !ok {
		t.Fatalf("attribute 0x%04X is %T", attrID, v)
	}
	return got
}

func playChime(c *camera.Chime, id *uint8) error {
	_, err := c.MatterInvoke(context.Background(), chimedef.CmdPlayChimeSound, chimedef.PlayChimeSoundRequest{ChimeId: id})
	return err
}

// countChanges counts the attribute changes c reports.
func countChanges(c *camera.Chime) *int {
	n := new(int)
	c.OnAttributeChange(func(contract.Change) { *n++ })
	return n
}

func TestChipChime(t *testing.T) { //nolint:gocognit,funlen // one subtest per chip TEST_F
	t.Run("TestAttributesList", func(t *testing.T) { // :99-104
		c := newChime(t, newMockChime())
		for _, id := range []uint32{chimedef.AttrInstalledChimeSounds, chimedef.AttrSelectedChime, chimedef.AttrEnabled} {
			if !slices.Contains(c.MatterAttributes(), id) {
				t.Errorf("attribute 0x%04X not listed", id)
			}
		}
	})
	t.Run("TestAcceptedCommands", func(t *testing.T) { // :106-112
		c := newChime(t, newMockChime())
		if got := c.MatterAcceptedCommands(); !slices.Equal(got, []uint32{chimedef.CmdPlayChimeSound}) {
			t.Fatalf("accepted %v", got)
		}
	})
	t.Run("TestDelegateErrors", func(t *testing.T) { // :114-130
		m := newMockChime()
		m.playErr = spec.Errorf(im.StatusBusy, "busy")
		c := newChime(t, m)
		if got := statusOf(playChime(c, nil)); got != im.StatusBusy || len(m.played) != 1 {
			t.Fatalf("PlayChimeSound %s, played %v", got, m.played)
		}
		m.soundsErr = errors.New("internal")
		if _, ok := c.MatterRead(chimedef.AttrInstalledChimeSounds); ok {
			t.Fatal("InstalledChimeSounds read with a failing port")
		}
	})
	t.Run("TestNoOpWrites", func(t *testing.T) { // :132-153
		c := newChime(t, newMockChime())
		n := countChanges(c)
		ctx := context.Background()
		steps := []struct {
			attr  uint32
			value any
			want  int
		}{
			{chimedef.AttrSelectedChime, uint64(1), 1},
			{chimedef.AttrSelectedChime, uint64(1), 1},
			{chimedef.AttrEnabled, false, 2},
			{chimedef.AttrEnabled, false, 2},
		}
		for i, s := range steps {
			if err := c.MatterWrite(ctx, s.attr, s.value); err != nil {
				t.Fatalf("step %d: %v", i, err)
			}
			if *n != s.want {
				t.Fatalf("step %d: %d changes, want %d", i, *n, s.want)
			}
		}
	})
	t.Run("TestReadClusterRevision", func(t *testing.T) { // :155-160; chip kRevision 2
		if got := readChime[uint16](t, newChime(t, newMockChime()), 0xFFFD); got != 2 { // chimedef.Revision, chip kRevision (Chime/Metadata.h:20)
			t.Fatalf("ClusterRevision %d", got)
		}
	})
	t.Run("TestReadFeatureMap", func(t *testing.T) { // :162-167
		if got := readChime[uint32](t, newChime(t, newMockChime()), 0xFFFC); got != 0 {
			t.Fatalf("FeatureMap %d", got)
		}
	})
	t.Run("TestReadSelectedChime", func(t *testing.T) { // :169-174
		if got := readChime[uint8](t, newChime(t, newMockChime()), chimedef.AttrSelectedChime); got != 0 {
			t.Fatalf("SelectedChime %d", got)
		}
	})
	t.Run("TestReadEnabled", func(t *testing.T) { // :176-181
		if got := readChime[bool](t, newChime(t, newMockChime()), chimedef.AttrEnabled); !got {
			t.Fatal("Enabled false")
		}
	})
	t.Run("TestInstalledChimeSoundsLifecycle", func(t *testing.T) { // :183-213
		m := newMockChime()
		c := newChime(t, m)
		got := readChime[spec.List[camera.ChimeSound]](t, c, chimedef.AttrInstalledChimeSounds)
		if want := m.sounds; !slices.Equal([]camera.ChimeSound(got), want) {
			t.Fatalf("InstalledChimeSounds %+v", got)
		}
		m.mu.Lock()
		m.sounds = append(m.sounds, camera.ChimeSound{ChimeId: 3, Name: "New Sound"})
		m.mu.Unlock()
		got = readChime[spec.List[camera.ChimeSound]](t, c, chimedef.AttrInstalledChimeSounds)
		if len(got) != 3 || got[2] != (camera.ChimeSound{ChimeId: 3, Name: "New Sound"}) {
			t.Fatalf("InstalledChimeSounds %+v", got)
		}
	})
	t.Run("TestWriteAttributes", func(t *testing.T) { // :215-244
		c := newChime(t, newMockChime())
		ctx := context.Background()
		for _, id := range []uint8{1, 2} {
			if err := c.MatterWrite(ctx, chimedef.AttrSelectedChime, uint64(id)); err != nil {
				t.Fatal(err)
			}
			if got := readChime[uint8](t, c, chimedef.AttrSelectedChime); got != id {
				t.Fatalf("SelectedChime %d, want %d", got, id)
			}
		}
		if got := statusOf(c.MatterWrite(ctx, chimedef.AttrSelectedChime, uint64(3))); got != im.StatusNotFound {
			t.Fatalf("write of an unknown chime: %s", got)
		}
		if got := readChime[uint8](t, c, chimedef.AttrSelectedChime); got != 2 {
			t.Fatalf("SelectedChime %d after the refused write", got)
		}
		for _, on := range []bool{false, true} {
			if err := c.MatterWrite(ctx, chimedef.AttrEnabled, on); err != nil {
				t.Fatal(err)
			}
			if got := readChime[bool](t, c, chimedef.AttrEnabled); got != on {
				t.Fatalf("Enabled %v, want %v", got, on)
			}
		}
	})
	t.Run("TestPersistence", func(t *testing.T) { // :246-284
		m := newMockChime()
		c := newChime(t, m)
		if got := readChime[uint8](t, c, chimedef.AttrSelectedChime); got != 0 {
			t.Fatalf("SelectedChime %d", got)
		}
		if !readChime[bool](t, c, chimedef.AttrEnabled) {
			t.Fatal("Enabled false")
		}
		ctx := context.Background()
		if err := c.MatterWrite(ctx, chimedef.AttrSelectedChime, uint64(2)); err != nil {
			t.Fatal(err)
		}
		if err := c.MatterWrite(ctx, chimedef.AttrEnabled, false); err != nil {
			t.Fatal(err)
		}
		// The restart: the host persisted what the server reported and
		// hands it back (cluster/camera doc, Persistence).
		selected, enabled := c.SelectedChime(), c.Enabled()
		again := newChime(t, m, func(cfg *camera.ChimeConfig) { cfg.SelectedChime, cfg.Enabled = selected, &enabled })
		if got := readChime[uint8](t, again, chimedef.AttrSelectedChime); got != 2 {
			t.Fatalf("SelectedChime %d after the restart", got)
		}
		if readChime[bool](t, again, chimedef.AttrEnabled) {
			t.Fatal("Enabled after the restart")
		}
	})
	t.Run("TestPlayChimeSound", func(t *testing.T) { // :286-300
		m := newMockChime()
		c := newChime(t, m)
		if err := playChime(c, nil); err != nil || len(m.played) != 1 {
			t.Fatalf("PlayChimeSound %v, played %v", err, m.played)
		}
		if err := c.MatterWrite(context.Background(), chimedef.AttrEnabled, false); err != nil {
			t.Fatal(err)
		}
		if err := playChime(c, nil); err != nil || len(m.played) != 1 {
			t.Fatalf("PlayChimeSound disabled %v, played %v", err, m.played)
		}
	})
}

// TestChimePlayRules holds the PlayChimeSound rules no chip test checks:
// an unknown ChimeID is NOT_FOUND (ChimeCluster.cpp:271-277), a given one
// plays instead of SelectedChime (:279), and a sound that started emits
// ChimeStartedPlaying with its id (:283-303).
func TestChimePlayRules(t *testing.T) {
	m := newMockChime()
	c := newChime(t, m, func(cfg *camera.ChimeConfig) { cfg.SelectedChime = 1 })
	em := &chimeEvents{}
	c.SetMatterEventEmitter(em)
	c.SetEndpoint(3)
	unknown, two := uint8(9), uint8(2)
	if got := statusOf(playChime(c, &unknown)); got != im.StatusNotFound || len(m.played) != 0 {
		t.Fatalf("unknown ChimeID: %s, played %v", got, m.played)
	}
	if err := playChime(c, &two); err != nil {
		t.Fatal(err)
	}
	if err := playChime(c, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.played, []uint8{2, 1}) {
		t.Fatalf("played %v", m.played)
	}
	want := []chimeEvent{{3, chimedef.ChimeStartedPlayingEvent{ChimeId: 2}}, {3, chimedef.ChimeStartedPlayingEvent{ChimeId: 1}}}
	if !slices.Equal(em.events, want) {
		t.Fatalf("events %+v", em.events)
	}
	m.playErr = errors.New("speaker gone")
	if got := statusOf(playChime(c, nil)); got != im.StatusFailure || len(em.events) != 2 {
		t.Fatalf("a refused play: %s, %d events", got, len(em.events))
	}
}

type chimeEvent struct {
	endpoint uint16
	data     any
}

type chimeEvents struct{ events []chimeEvent }

func (e *chimeEvents) MatterEmitEvent(endpoint uint16, _, id uint32, data any, _ contract.EventPriority) {
	if id == chimedef.EventChimeStartedPlaying {
		e.events = append(e.events, chimeEvent{endpoint, data})
	}
}
