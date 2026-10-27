// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	chimedef "github.com/SukramJ/go-fabric/cluster/spec/chime"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// ChimeClusterID is the Chime cluster id.
const ChimeClusterID = chimedef.ClusterID

// Generated types the chime speaks.
type (
	// ChimeSound is one installed sound: its id and its name.
	ChimeSound = chimedef.ChimeSoundStruct
	// ChimePort is the device's chime: the installed sounds and the
	// player.
	ChimePort = chimedef.ChimePort
)

// ErrNoChimePort reports a Chime built without its port.
var ErrNoChimePort = errors.New("camera: Chime needs a ChimePort")

// ChimeConfig carries the construction parameters of a [Chime].
type ChimeConfig struct {
	// Port is the device's chime; required.
	Port ChimePort
	// SelectedChime is the value the host persisted; chip starts at 0
	// (ChimeCluster.h:95), which need not name an installed sound.
	SelectedChime uint8
	// Enabled is the value the host persisted; nil starts enabled, as chip
	// does (ChimeCluster.h:96).
	Enabled *bool
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// Chime implements [contract.ClusterServer] for Chime: the installed sounds
// are the port's, read on every read of InstalledChimeSounds; SelectedChime
// and Enabled are held here and accept a controller's write; PlayChimeSound
// plays through the port and emits ChimeStartedPlaying. The rules are
// connectedhomeip's ChimeCluster.cpp at 6170af84 (matter.js's ChimeServer
// is the generated behaviour).
type Chime struct {
	*spec.Instance
	spec.SelfHeld

	srv  *spec.Server
	port ChimePort

	// mu guards endpoint, selected and enabled.
	mu       sync.Mutex
	endpoint uint16
	selected uint8
	enabled  bool
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*Chime)(nil)
	_ contract.ClusterDataVersion             = (*Chime)(nil)
	_ contract.ClusterAttributeLister         = (*Chime)(nil)
	_ contract.ClusterCommandLister           = (*Chime)(nil)
	_ contract.ClusterEventLister             = (*Chime)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*Chime)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*Chime)(nil)
	_ contract.AttributeChangeNotifier        = (*Chime)(nil)
	_ contract.EventReceiver                  = (*Chime)(nil)
	_ spec.AttributeSetter                    = (*Chime)(nil)
	_ spec.CommandRegistrar                   = (*Chime)(nil)
	_ spec.EventEmitting                      = (*Chime)(nil)
)

// NewChime builds a Chime server on cfg.Port. FeatureMap is 0 and the
// attributes are the mandatory three (ChimeCluster.cpp:75-81, :113-114).
func NewChime(cfg ChimeConfig) (*Chime, error) {
	if cfg.Port == nil {
		return nil, ErrNoChimePort
	}
	c := &Chime{port: cfg.Port, selected: cfg.SelectedChime, enabled: true}
	if cfg.Enabled != nil {
		c.enabled = *cfg.Enabled
	}
	handlers := map[uint32]spec.CommandHandler{chimedef.CmdPlayChimeSound: c.playChimeSound}
	srv, err := spec.NewServer(chimedef.Definition, spec.Options{}, spec.ServerConfig{
		Handlers: handlers, DataVersion: cfg.DataVersion, Source: chimeSounds{c}, Sink: chimeSink{c},
	})
	if err != nil {
		return nil, fmt.Errorf("camera: %w", err)
	}
	c.srv, c.Instance, c.SelfHeld = srv, srv.Instance, spec.NewSelfHeld(srv)
	if err := srv.SetAttributes(map[uint32]any{chimedef.AttrSelectedChime: c.selected, chimedef.AttrEnabled: c.enabled}); err != nil {
		return nil, fmt.Errorf("camera: %w", err)
	}
	return c, nil
}

// SelectedChime returns the selected chime.
func (c *Chime) SelectedChime() uint8 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.selected
}

// Enabled reports whether the chime plays.
func (c *Chime) Enabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled
}

// SoundsChanged reports a change of the port's installed sounds:
// InstalledChimeSounds advances its data version and is reported.
func (c *Chime) SoundsChanged() error { return c.srv.MarkChanged(chimedef.AttrInstalledChimeSounds) }

// isSupportedChimeID reports whether the port has an installed sound of
// id. Mirrors chip ChimeCluster.cpp:175-193 (IsSupportedChimeID).
func (c *Chime) isSupportedChimeID(id uint8) bool {
	sounds, err := c.port.InstalledChimeSounds()
	if err != nil {
		return false
	}
	return slices.ContainsFunc(sounds, func(s ChimeSound) bool { return s.ChimeId == id })
}

// chimeSounds serves InstalledChimeSounds from the port, read on every
// read; an error, or a list the attribute's schema refuses, fails the
// read. Mirrors chip ChimeCluster.cpp:119-121, :137-173
// (EncodeSupportedChimeSounds).
type chimeSounds struct{ c *Chime }

// MatterAttribute implements [spec.Source].
func (s chimeSounds) MatterAttribute(attrID uint32) (any, bool) {
	if attrID != chimedef.AttrInstalledChimeSounds {
		return nil, false
	}
	sounds, err := s.c.port.InstalledChimeSounds()
	if err != nil {
		return nil, false
	}
	if sounds == nil {
		sounds = []ChimeSound{}
	}
	list := spec.List[ChimeSound](sounds)
	if _, err := s.c.CheckSet(attrID, list, nil); err != nil {
		return nil, false
	}
	return list, true
}

// chimeSink carries out a write of SelectedChime or Enabled after the
// definition's checks. SelectedChime must name an installed sound
// (NOT_FOUND), checked before an unchanged value is a no-op; the
// unchanged value of either is stored without a report. Mirrors chip
// ChimeCluster.cpp:195-238 (WriteAttribute, SetSelectedChime,
// SetEnabled).
type chimeSink struct{ c *Chime }

// MatterWriteAttribute implements [spec.Sink].
func (k chimeSink) MatterWriteAttribute(_ context.Context, attrID uint32, value any) error {
	c := k.c
	switch attrID {
	case chimedef.AttrSelectedChime:
		id, _ := value.(uint8)
		if !c.isSupportedChimeID(id) { // :218
			return spec.Errorf(im.StatusNotFound, "Chime: SelectedChime %d is not installed", id)
		}
		c.mu.Lock()
		c.selected = id
		c.mu.Unlock()
	case chimedef.AttrEnabled:
		on, _ := value.(bool)
		c.mu.Lock()
		c.enabled = on
		c.mu.Unlock()
	}
	return nil
}

// SetAttributes implements [spec.AttributeSetter]: the host's own change
// of SelectedChime (an installed sound, as a write requires) or Enabled;
// InstalledChimeSounds is the port's ([spec.ErrNotHeld]). All or nothing.
func (c *Chime) SetAttributes(values map[uint32]any) error {
	ids := slices.Sorted(maps.Keys(values))
	checked := make(map[uint32]any, len(ids))
	for _, id := range ids {
		if id == chimedef.AttrInstalledChimeSounds {
			return fmt.Errorf("%w: camera: InstalledChimeSounds is the ChimePort's", spec.ErrNotHeld)
		}
		v, err := c.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		if id == chimedef.AttrSelectedChime {
			if sel, _ := v.(uint8); !c.isSupportedChimeID(sel) {
				return fmt.Errorf("%w: camera: SelectedChime %d is not installed", spec.ErrInvalidValue, sel)
			}
		}
		checked[id] = v
	}
	c.mu.Lock()
	if v, ok := checked[chimedef.AttrSelectedChime]; ok {
		c.selected, _ = v.(uint8)
	}
	if v, ok := checked[chimedef.AttrEnabled]; ok {
		c.enabled, _ = v.(bool)
	}
	c.mu.Unlock()
	return c.srv.SetAttributes(checked)
}

// playChimeSound carries out PlayChimeSound: nothing plays while the chime
// is disabled, and the command succeeds; a ChimeID must name an installed
// sound (NOT_FOUND), otherwise SelectedChime plays; the port's refusal is
// the answer, and a sound that started emits ChimeStartedPlaying. Mirrors
// chip ChimeCluster.cpp:258-303 (HandlePlayChimeSound,
// GenerateChimeStartedPlayingEvent).
func (c *Chime) playChimeSound(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[chimedef.PlayChimeSoundRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "Chime: PlayChimeSound payload %T", fields)
	}
	c.mu.Lock()
	enabled, id, endpoint := c.enabled, c.selected, c.endpoint
	c.mu.Unlock()
	if !enabled { // :263-266
		return nil, nil
	}
	if req.ChimeId != nil { // :271-280
		if !c.isSupportedChimeID(*req.ChimeId) {
			return nil, spec.Errorf(im.StatusNotFound, "Chime: ChimeID %d is not installed", *req.ChimeId)
		}
		id = *req.ChimeId
	}
	if err := c.port.PlayChimeSound(ctx, id); err != nil { // :282
		return nil, portError("PlayChimeSound", err)
	}
	if c.Emits(chimedef.EventChimeStartedPlaying) { // :283-287
		_ = c.srv.Emit(endpoint, chimedef.EventChimeStartedPlaying, chimedef.ChimeStartedPlayingEvent{ChimeId: id})
	}
	return nil, nil
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (c *Chime) MatterDataVersion() uint32 { return c.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (c *Chime) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return c.srv.OnAttributeChange(cb)
}

// MatterRead resolves an attribute.
func (c *Chime) MatterRead(attrID uint32) (any, bool) { return c.srv.MatterRead(attrID) }

// MatterWrite applies a write of SelectedChime or Enabled.
func (c *Chime) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return c.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke dispatches PlayChimeSound; a host's handler runs in its
// place ([spec.SelfHeld.Handle]).
func (c *Chime) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &c.SelfHeld, cmdID, fields)
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (c *Chime) SetMatterEventEmitter(emitter contract.EventEmitter) {
	c.srv.SetMatterEventEmitter(emitter)
}

// SetEndpoint stamps the endpoint the events are addressed to; the bridge
// calls it at reassembly.
func (c *Chime) SetEndpoint(endpoint uint16) {
	c.mu.Lock()
	c.endpoint = endpoint
	c.mu.Unlock()
}

// portError maps a port's refusal onto the answer: an error carrying an
// IM status passes through as is, any other answers FAILURE.
func portError(what string, err error) error {
	var sc im.StatusCodeError
	if errors.As(err, &sc) {
		return err
	}
	return spec.Errorf(im.StatusFailure, "camera: %s refused: %v", what, err)
}
