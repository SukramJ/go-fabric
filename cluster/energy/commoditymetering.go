// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package energy

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	commtr "github.com/SukramJ/go-fabric/cluster/spec/commoditymetering"
	"github.com/SukramJ/go-fabric/im"
)

// CommodityMetering (0x0B07). Its chip rules are in
// src/app/clusters/commodity-metering-server/commodity-metering-server.cpp,
// cited below by line.

// ClusterIDCommodityMetering is the CommodityMetering cluster id.
const ClusterIDCommodityMetering = commtr.ClusterID

// The CommodityMetering datatypes, the generated ones.
type (
	// MeteredQuantity is the MeteredQuantityStruct.
	MeteredQuantity = commtr.MeteredQuantityStruct
	// MeteringTariffUnit is the Globals TariffUnitEnum as
	// CommodityMetering carries it.
	MeteringTariffUnit = commtr.TariffUnitEnum
)

// The metering limits chip applies (commodity-metering-server.cpp:47-48).
const (
	meteringMaxTariffComponentIDs   = 128
	meteringMaxMeteredQuantitiesMin = 1
)

var (
	// ErrMeteringNoMaximum is a non-null MeteredQuantity while
	// MaximumMeteredQuantities is null (CHIP_ERROR_INCORRECT_STATE,
	// commodity-metering-server.cpp:192-195).
	ErrMeteringNoMaximum = errors.New("energy: CommodityMetering: MeteredQuantity needs MaximumMeteredQuantities")
	// ErrMeteringListLength is a MeteredQuantity longer than
	// MaximumMeteredQuantities (CHIP_ERROR_INVALID_LIST_LENGTH, :200-203).
	ErrMeteringListLength = errors.New("energy: CommodityMetering: MeteredQuantity longer than MaximumMeteredQuantities")
	// ErrMeteringMaximum is a MaximumMeteredQuantities below 1
	// (CHIP_ERROR_INVALID_ARGUMENT, :300-307).
	ErrMeteringMaximum = errors.New("energy: CommodityMetering: MaximumMeteredQuantities below 1")
)

// CommodityMeteringConfig carries the construction parameters.
type CommodityMeteringConfig struct {
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// CommodityMeteringServer is the CommodityMetering server: the generated
// server, which serves the four read-only attributes, with chip's setter
// rules in front of it. It has no command and no event.
type CommodityMeteringServer struct {
	*spec.Server

	mu sync.Mutex
}

// NewCommodityMetering builds a CommodityMetering server. Every attribute
// starts null, as chip's Instance::Init sets them
// (commodity-metering-server.cpp:150-158).
func NewCommodityMetering(cfg CommodityMeteringConfig) (*CommodityMeteringServer, error) {
	srv, err := spec.NewServer(commtr.Definition, spec.Options{}, spec.ServerConfig{
		DataVersion: cfg.DataVersion,
		Initial: map[uint32]any{
			commtr.AttrMeteredQuantity:          nil,
			commtr.AttrMeteredQuantityTimestamp: nil,
			commtr.AttrTariffUnit:               nil,
			commtr.AttrMaximumMeteredQuantities: nil,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("energy: %w", err)
	}
	return &CommodityMeteringServer{Server: srv}, nil
}

// SetMaximumMeteredQuantities records MaximumMeteredQuantities; nil is
// null, a value below 1 [ErrMeteringMaximum]
// (commodity-metering-server.cpp:290-315).
func (s *CommodityMeteringServer) SetMaximumMeteredQuantities(maximum *uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if maximum == nil {
		return s.Set(commtr.AttrMaximumMeteredQuantities, nil)
	}
	if *maximum < meteringMaxMeteredQuantitiesMin {
		return ErrMeteringMaximum
	}
	return s.Set(commtr.AttrMaximumMeteredQuantities, *maximum)
}

// SetMeteredQuantity records MeteredQuantity
// (commodity-metering-server.cpp:174-239): nil is null; a list needs a
// non-null MaximumMeteredQuantities and may not be longer; an entry's
// TariffComponentIDs are copied up to 128 (SpanCopier with
// kMaxTariffComponentIDsPerMeteredQuantityEntry, :165-172). An equal
// list changes nothing (:178-181).
//
// chip frees the stored list before it checks the new one (:183-186), so
// a refused list leaves the attribute's storage empty; this server leaves
// the attribute as it was.
func (s *CommodityMeteringServer) SetMeteredQuantity(quantities []MeteredQuantity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if quantities == nil {
		return s.Set(commtr.AttrMeteredQuantity, nil)
	}
	v, _ := s.Value(commtr.AttrMaximumMeteredQuantities)
	maximum, ok := v.(uint16)
	if !ok {
		return ErrMeteringNoMaximum
	}
	if len(quantities) > int(maximum) {
		return ErrMeteringListLength
	}
	out := make([]MeteredQuantity, len(quantities))
	for i, q := range quantities {
		ids := q.TariffComponentIds
		if len(ids) > meteringMaxTariffComponentIDs {
			ids = ids[:meteringMaxTariffComponentIDs]
		}
		out[i] = MeteredQuantity{TariffComponentIds: append([]uint32{}, ids...), Quantity: q.Quantity}
	}
	if old, _ := s.Value(commtr.AttrMeteredQuantity); reflect.DeepEqual(old, out) {
		return nil
	}
	return s.Set(commtr.AttrMeteredQuantity, out)
}

// SetMeteredQuantityTimestamp records MeteredQuantityTimestamp, in Matter
// epoch seconds; nil is null (commodity-metering-server.cpp:241-262).
func (s *CommodityMeteringServer) SetMeteredQuantityTimestamp(epochS *uint32) error {
	if epochS == nil {
		return s.Set(commtr.AttrMeteredQuantityTimestamp, nil)
	}
	return s.Set(commtr.AttrMeteredQuantityTimestamp, *epochS)
}

// SetTariffUnit records TariffUnit; nil is null, a value outside the
// enum CONSTRAINT_ERROR (commodity-metering-server.cpp:264-288).
func (s *CommodityMeteringServer) SetTariffUnit(unit *MeteringTariffUnit) error {
	if unit == nil {
		return s.Set(commtr.AttrTariffUnit, nil)
	}
	if *unit > commtr.TariffUnitKVAh {
		return statusError{im.StatusConstraintError, "energy: CommodityMetering: TariffUnit outside the enum"}
	}
	return s.Set(commtr.AttrTariffUnit, *unit)
}

// MaximumMeteredQuantities returns MaximumMeteredQuantities; nil while
// null.
func (s *CommodityMeteringServer) MaximumMeteredQuantities() *uint16 {
	v, _ := s.Value(commtr.AttrMaximumMeteredQuantities)
	if n, ok := v.(uint16); ok {
		return &n
	}
	return nil
}

// MeteredQuantity returns MeteredQuantity; nil while null.
func (s *CommodityMeteringServer) MeteredQuantity() []MeteredQuantity {
	v, _ := s.Value(commtr.AttrMeteredQuantity)
	q, _ := v.([]MeteredQuantity)
	return q
}

// MeteredQuantityTimestamp returns MeteredQuantityTimestamp; nil while
// null.
func (s *CommodityMeteringServer) MeteredQuantityTimestamp() *uint32 {
	v, _ := s.Value(commtr.AttrMeteredQuantityTimestamp)
	if n, ok := v.(uint32); ok {
		return &n
	}
	return nil
}

// TariffUnit returns TariffUnit; nil while null.
func (s *CommodityMeteringServer) TariffUnit() *MeteringTariffUnit {
	v, _ := s.Value(commtr.AttrTariffUnit)
	if n, ok := v.(uint8); ok {
		u := MeteringTariffUnit(n)
		return &u
	}
	return nil
}
