// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"fmt"

	tbrmdef "github.com/SukramJ/go-fabric/cluster/spec/threadborderroutermanagement"
	tnddef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	"github.com/SukramJ/go-fabric/cluster/threadbr"
	"github.com/SukramJ/go-fabric/contract"
)

// The Thread Border Router servers (cluster/threadbr, ADR 0027). matter.js
// serves both clusters as the generated stub; their rules follow
// connectedhomeip (cluster/threadbr's package doc). They run on the
// border router the ThreadBorderRouterManagement declaration carries
// (tbrmdef.Declare.BorderRouter); without one both clusters keep the
// generated server.

// newThreadBorderRouterManagementServer is the module's
// ThreadBorderRouterManagement server ([threadbr.NewManagement]) on the
// declared BorderRouter, with the selection's PanChange. The six
// attributes are the border router's: an Initial for them is not applied.
// The node wires its fail-safe as the endpoint joins the topology
// ([threadbr.ManagementServer.AttachFailSafe]).
func newThreadBorderRouterManagementServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(tbrmdef.Declare)
	if d.BorderRouter == nil {
		return nil, notServed(fmt.Errorf("threadborderroutermanagement: %w", errNoPort))
	}
	for _, id := range []uint32{
		tbrmdef.AttrBorderRouterName, tbrmdef.AttrBorderAgentId, tbrmdef.AttrThreadVersion,
		tbrmdef.AttrInterfaceEnabled, tbrmdef.AttrActiveDatasetTimestamp, tbrmdef.AttrPendingDatasetTimestamp,
	} {
		delete(b.initial, id)
	}
	return threadbr.NewManagement(context.Background(), threadbr.ManagementConfig{
		BorderRouter: d.BorderRouter, Features: tbrmdef.Feature(b.opts.Features),
		DataVersion: b.cfg.DataVersion, Logger: Logger,
	})
}

// newThreadNetworkDiagnosticsServer is the module's ThreadNetworkDiagnostics
// server ([threadbr.NewDiagnostics]) on the BorderRouter of the device's
// ThreadBorderRouterManagement server — the kit builds it after the
// device's other servers (coupledLast) — with the selection's MleCounts
// and MacCounts ([threadbr.DiagnosticsFeatures]). A device without a
// border router keeps the generated server; the attributes are the
// border router's, an Initial for them is not applied.
func newThreadNetworkDiagnosticsServer(b defaultBuild) (contract.ClusterServer, error) {
	m, ok := b.sibling(tbrmdef.ClusterID).(*threadbr.ManagementServer)
	if !ok {
		return nil, notServed(fmt.Errorf("threadnetworkdiagnostics: %w", errNoPort))
	}
	features := tnddef.Feature(b.opts.Features)
	if features&^threadbr.DiagnosticsFeatures != 0 {
		return nil, notServed(fmt.Errorf("%w: 0x%X", threadbr.ErrDiagnosticsFeature, uint32(features)))
	}
	clear(b.initial)
	return threadbr.NewDiagnostics(context.Background(), threadbr.DiagnosticsConfig{
		BorderRouter: m.BorderRouter(), Features: features, DataVersion: b.cfg.DataVersion, Logger: Logger,
	})
}
