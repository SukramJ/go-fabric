// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ethdiagdef "github.com/SukramJ/go-fabric/cluster/spec/ethernetnetworkdiagnostics"
)

// EthernetReporter is the host port for EthernetNetworkDiagnostics, read
// on every attribute read. Each method answers ok=false when the host
// cannot tell: PHYRate, FullDuplex and CarrierDetect then read as null,
// TimeSinceReset as 0 — what connectedhomeip encodes when its provider
// fails (src/app/clusters/ethernet-network-diagnostics-server/
// EthernetDiagnosticsCluster.cpp ReadAttribute :69-118 for the nullable
// three, EncodeU64Value :38-49 and :119-121 for TimeSinceReset, at the
// harness pin 6170af8461b10b1766044122ac83332c6d00ab20).
type EthernetReporter interface {
	// PHYRate is the interface's current nominal speed.
	PHYRate() (rate ethdiagdef.PHYRateEnum, ok bool)
	// FullDuplex is the interface's full-duplex operating status.
	FullDuplex() (fullDuplex, ok bool)
	// CarrierDetect is the Carrier Detect control signal.
	CarrierDetect() (carrier, ok bool)
	// TimeSinceReset is the time since the interface was reset.
	TimeSinceReset() (value uint64, ok bool)
}

// EthernetNetworkDiagnosticsConfig carries the construction parameters of
// an EthernetNetworkDiagnostics server.
type EthernetNetworkDiagnosticsConfig struct {
	// Reporter answers the reads; required.
	Reporter EthernetReporter
	// Attributes lists the optional attributes served: any of
	// AttrPhyRate, AttrFullDuplex, AttrCarrierDetect and
	// AttrTimeSinceReset. The packet and error counters (PKTCNT, ERRCNT)
	// and ResetCounts are not offered.
	Attributes []uint32
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// NewEthernetNetworkDiagnostics builds an EthernetNetworkDiagnostics
// (0x0037) server on the generated definition (cluster/spec/
// ethernetnetworkdiagnostics, ADR 0013), with no feature.
//
// Mirrors matter.js packages/node/src/behaviors/ethernet-network-diagnostics/
// EthernetNetworkDiagnosticsServer.ts, `export class
// EthernetNetworkDiagnosticsServer extends EthernetNetworkDiagnosticsBehavior {}`
// (:14): the generated behavior with no logic of its own. The values are
// the host's, read through its reporter on every read as connectedhomeip
// reads its provider (EthernetDiagnosticsCluster.cpp ReadAttribute
// :61-146), so a change is not reported to a subscriber.
func NewEthernetNetworkDiagnostics(cfg EthernetNetworkDiagnosticsConfig) (*spec.Server, error) {
	if cfg.Reporter == nil {
		return nil, fmt.Errorf("core: EthernetNetworkDiagnostics: %w", ErrNilReporter)
	}
	srv, err := spec.NewServer(ethdiagdef.Definition, spec.Options{Attributes: cfg.Attributes}, spec.ServerConfig{
		DataVersion: cfg.DataVersion,
		Source:      ethernetSource{cfg.Reporter},
	})
	if err != nil {
		return nil, fmt.Errorf("core: EthernetNetworkDiagnostics: %w", err)
	}
	return srv, nil
}

// ErrNilReporter rejects a diagnostics server built without its host port.
var ErrNilReporter = errors.New("a reporter is required")

// ethernetSource answers the served attributes from the host, in the
// stored form the reply encoder writes: a uint8 for the enum, nil for
// null.
type ethernetSource struct{ r EthernetReporter }

// MatterAttribute implements [spec.Source].
func (s ethernetSource) MatterAttribute(attrID uint32) (any, bool) {
	switch attrID {
	case ethdiagdef.AttrPhyRate:
		if v, ok := s.r.PHYRate(); ok {
			return uint8(v), true
		}
		return nil, true
	case ethdiagdef.AttrFullDuplex:
		if v, ok := s.r.FullDuplex(); ok {
			return v, true
		}
		return nil, true
	case ethdiagdef.AttrCarrierDetect:
		if v, ok := s.r.CarrierDetect(); ok {
			return v, true
		}
		return nil, true
	case ethdiagdef.AttrTimeSinceReset:
		v, ok := s.r.TimeSinceReset()
		if !ok {
			v = 0
		}
		return v, true
	}
	return nil, false
}
