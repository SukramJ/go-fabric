// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package energy

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ep "github.com/SukramJ/go-fabric/cluster/spec/energypreference"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// ClusterIDEnergyPreference is the EnergyPreference cluster id.
const ClusterIDEnergyPreference = ep.ClusterID

// The EnergyPreference datatypes, the generated ones.
type (
	// PreferenceFeature is an EnergyPreference FeatureMap bit.
	PreferenceFeature = ep.Feature
	// Balance is one BalanceStruct entry of EnergyBalances or
	// LowPowerModeSensitivities.
	Balance = ep.BalanceStruct
	// Priority is the EnergyPriorityEnum.
	Priority = ep.EnergyPriorityEnum
)

// EnergyPreference features (BALA, LPMS; at least one).
const (
	PreferenceFeatureEnergyBalance           = ep.FeatureEnergyBalance
	PreferenceFeatureLowPowerModeSensitivity = ep.FeatureLowPowerModeSensitivity
)

// Energy priorities.
const (
	PriorityComfort          = ep.EnergyPriorityComfort
	PrioritySpeed            = ep.EnergyPrioritySpeed
	PriorityEfficiency       = ep.EnergyPriorityEfficiency
	PriorityWaterConsumption = ep.EnergyPriorityWaterConsumption
)

// PreferenceChanger is told when a controller selects another entry —
// attrID is CurrentEnergyBalance or CurrentLowPowerModeSensitivity, index
// the entry of the matching list — so the device can act on it and
// persist it (quality "N"); an error refuses the write. chip tells its
// delegate the same way, after the index check
// (EnergyPreferenceCluster.cpp:118-125).
type PreferenceChanger interface {
	ChangePreference(ctx context.Context, attrID uint32, index uint8) error
}

// PreferenceConfig carries the construction parameters.
type PreferenceConfig struct {
	Features PreferenceFeature
	// EnergyBalances (BALA) is fixed: 2 to 10 entries.
	EnergyBalances []Balance
	// EnergyPriorities (BALA) is fixed: exactly 2 entries.
	EnergyPriorities []Priority
	// LowPowerModeSensitivities (LPMS) is fixed: 2 to 10 entries.
	LowPowerModeSensitivities []Balance
	// CurrentEnergyBalance (BALA) is an index into EnergyBalances.
	CurrentEnergyBalance uint8
	// CurrentLowPowerModeSensitivity (LPMS) is an index into
	// LowPowerModeSensitivities.
	CurrentLowPowerModeSensitivity uint8
	// Changer, optional, is told about a controller's write.
	Changer PreferenceChanger
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// ErrIndex is a Current… index at or beyond its list's length.
var ErrIndex = errors.New("energy: index outside its list")

// PreferenceServer implements [contract.ClusterServer] for
// EnergyPreference. Everything is the generated server's but one rule:
// a Current… value is an index into its list, so a write at or beyond the
// list's length is CONSTRAINT_ERROR (EnergyPreferenceCluster.cpp:108-116,
// SetCurrentUint8Attribute; the lengths from
// EnergyPreferenceCluster.h:57-58, :69-70).
type PreferenceServer struct {
	*spec.Instance

	srv     *spec.Server
	lengths map[uint32]int
	changer PreferenceChanger
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*PreferenceServer)(nil)
	_ contract.ClusterDataVersion             = (*PreferenceServer)(nil)
	_ contract.ClusterAttributeLister         = (*PreferenceServer)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*PreferenceServer)(nil)
	_ contract.AttributeChangeNotifier        = (*PreferenceServer)(nil)
)

// NewEnergyPreference builds an EnergyPreference server.
func NewEnergyPreference(cfg PreferenceConfig) (*PreferenceServer, error) {
	s := &PreferenceServer{
		lengths: map[uint32]int{
			ep.AttrCurrentEnergyBalance:           len(cfg.EnergyBalances),
			ep.AttrCurrentLowPowerModeSensitivity: len(cfg.LowPowerModeSensitivities),
		},
		changer: cfg.Changer,
	}
	inst, err := spec.New(ep.Definition, spec.Options{Features: uint32(cfg.Features)})
	if err != nil {
		return nil, fmt.Errorf("energy: %w", err)
	}
	initial := map[uint32]any{}
	if inst.Serves(ep.AttrEnergyBalances) {
		initial[ep.AttrEnergyBalances] = spec.List[Balance](slices.Clone(cfg.EnergyBalances))
		initial[ep.AttrEnergyPriorities] = priorityList(slices.Clone(cfg.EnergyPriorities))
		initial[ep.AttrCurrentEnergyBalance] = cfg.CurrentEnergyBalance
	}
	if inst.Serves(ep.AttrLowPowerModeSensitivities) {
		initial[ep.AttrLowPowerModeSensitivities] = spec.List[Balance](slices.Clone(cfg.LowPowerModeSensitivities))
		initial[ep.AttrCurrentLowPowerModeSensitivity] = cfg.CurrentLowPowerModeSensitivity
	}
	for _, id := range []uint32{ep.AttrCurrentEnergyBalance, ep.AttrCurrentLowPowerModeSensitivity} {
		if v, ok := initial[id].(uint8); ok && int(v) >= s.lengths[id] {
			return nil, fmt.Errorf("%w: attribute 0x%04X index %d, list of %d", ErrIndex, id, v, s.lengths[id])
		}
	}
	srv, err := spec.NewServer(ep.Definition, spec.Options{Features: uint32(cfg.Features)}, spec.ServerConfig{
		DataVersion: cfg.DataVersion,
		Sink:        preferenceSink{s},
		Initial:     initial,
	})
	if err != nil {
		return nil, fmt.Errorf("energy: %w", err)
	}
	s.Instance, s.srv = srv.Instance, srv
	return s, nil
}

// preferenceSink applies the index rule to a controller's write and tells
// the host.
type preferenceSink struct{ s *PreferenceServer }

// MatterWriteAttribute implements [spec.Sink]; value is a uint8, the only
// writable attributes being the two indexes.
func (p preferenceSink) MatterWriteAttribute(ctx context.Context, attrID uint32, value any) error {
	index, _ := value.(uint8)
	if n := p.s.lengths[attrID]; int(index) >= n {
		return spec.Errorf(im.StatusConstraintError, "EnergyPreference: index %d outside a list of %d", index, n)
	}
	if p.s.changer == nil {
		return nil
	}
	if err := p.s.changer.ChangePreference(ctx, attrID, index); err != nil {
		return fmt.Errorf("energy: EnergyPreference write: %w", err)
	}
	return nil
}

// CurrentEnergyBalance returns the selected EnergyBalances index (BALA).
func (s *PreferenceServer) CurrentEnergyBalance() (uint8, bool) {
	v, ok := s.srv.Value(ep.AttrCurrentEnergyBalance)
	n, _ := v.(uint8)
	return n, ok
}

// CurrentLowPowerModeSensitivity returns the selected
// LowPowerModeSensitivities index (LPMS).
func (s *PreferenceServer) CurrentLowPowerModeSensitivity() (uint8, bool) {
	v, ok := s.srv.Value(ep.AttrCurrentLowPowerModeSensitivity)
	n, _ := v.(uint8)
	return n, ok
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *PreferenceServer) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
func (s *PreferenceServer) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.srv.OnMatterAttributesChanged(cb)
}

// MatterRead resolves an attribute.
func (s *PreferenceServer) MatterRead(attrID uint32) (any, bool) {
	return s.srv.MatterRead(attrID)
}

// MatterWrite applies a write to one of the two indexes; the definition
// answers every other write and every non-uint8 value.
func (s *PreferenceServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return s.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke answers UNSUPPORTED_COMMAND: the cluster has no commands.
func (s *PreferenceServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return s.srv.MatterInvoke(ctx, cmdID, fields)
}

// priorityList is EnergyPriorities' value: a list of enum8, which the
// generator gives no list type of its own.
type priorityList []Priority

// EncodeTLV implements [spec.Encodable].
func (l priorityList) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartArray(tag)
	for _, p := range l {
		enc.PutUint(tlv.AnonymousTag(), uint64(p))
	}
	_ = enc.EndContainer()
}
