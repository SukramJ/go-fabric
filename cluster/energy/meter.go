// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package energy

import (
	"fmt"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	mtrid "github.com/SukramJ/go-fabric/cluster/spec/meteridentification"
)

// ClusterIDMeterIdentification is the MeterIdentification cluster id.
const ClusterIDMeterIdentification = mtrid.ClusterID

// MeterType is the MeterTypeEnum.
type MeterType = mtrid.MeterTypeEnum

// Meter types.
const (
	MeterTypeUtility = mtrid.MeterTypeUtility
	MeterTypePrivate = mtrid.MeterTypePrivate
	MeterTypeGeneric = mtrid.MeterTypeGeneric
)

// MeterConfig identifies a meter. Every attribute is nullable ("X"); a
// nil field reads as null. ProtocolVersion is optional: served when set.
type MeterConfig struct {
	MeterType         *MeterType
	PointOfDelivery   *string
	MeterSerialNumber *string
	ProtocolVersion   *string
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// NewMeterIdentification builds a MeterIdentification server without the
// PowerThreshold feature. It is the generated server as it stands:
// matter.js's MeterIdentificationServer adds nothing to the generated
// behavior (packages/node/src/behaviors/meter-identification/
// MeterIdentificationServer.ts:15). A later change of the identification
// is the server's [spec.Server.Set].
func NewMeterIdentification(cfg MeterConfig) (*spec.Server, error) {
	opts := spec.Options{}
	initial := map[uint32]any{
		mtrid.AttrMeterType:         nil,
		mtrid.AttrPointOfDelivery:   nil,
		mtrid.AttrMeterSerialNumber: nil,
	}
	if cfg.MeterType != nil {
		initial[mtrid.AttrMeterType] = *cfg.MeterType
	}
	if cfg.PointOfDelivery != nil {
		initial[mtrid.AttrPointOfDelivery] = *cfg.PointOfDelivery
	}
	if cfg.MeterSerialNumber != nil {
		initial[mtrid.AttrMeterSerialNumber] = *cfg.MeterSerialNumber
	}
	if cfg.ProtocolVersion != nil {
		opts.Attributes = []uint32{mtrid.AttrProtocolVersion}
		initial[mtrid.AttrProtocolVersion] = *cfg.ProtocolVersion
	}
	srv, err := spec.NewServer(mtrid.Definition, opts, spec.ServerConfig{DataVersion: cfg.DataVersion, Initial: initial})
	if err != nil {
		return nil, fmt.Errorf("energy: %w", err)
	}
	return srv, nil
}
