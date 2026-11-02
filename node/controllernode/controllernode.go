// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package controllernode starts the controller's own node: the server node
// the controller's OTA push serves its updates from
// (controller.OTAConfig.StartNode).
//
// matter.js's controller is a ServerNode and mounts the
// OtaSoftwareUpdateProviderServer on itself (docs/OTA.md "Enable OTA
// Provider on Controller Creation" at 862ad75f). This module's controller
// role sits below the node facade (ADR 0026 principle 1), so the node it
// needs is composed here, in the facade layer, and handed to the
// controller through its port (controller.OwnNode): a node.Node with
// node/'s OTA provider, the one provider implementation.
package controllernode

import (
	"bytes"
	"context"
	"net"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/node"
)

// Config is the node configuration of spec: its socket, commissioning
// window, product, database and OTA provider, the controller's logger
// and clock. The provider's status observer is a node callback, not
// configuration: [Observe] registers it on the node New built.
func Config(spec controller.OwnNodeSpec) node.Config {
	p := spec.Provider
	return node.Config{
		Network:    node.Network{Listen: spec.Listen},
		Advertiser: spec.Advertiser,
		Commissioning: node.Commissioning{
			Passcode: spec.Passcode, Discriminator: spec.Discriminator, PBKDFIterations: pbkdfIterations(spec.Iterations), Salt: spec.Salt,
		},
		ProductDescription: node.ProductDescription{VendorID: spec.VendorID, ProductID: spec.ProductID},
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric", ProductName: "controller", NodeLabel: "controller", SerialNumber: spec.SerialNumber,
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Store: node.Store{DB: spec.DB},
		OTAProvider: &node.OTAProviderConfig{
			ImageSource: p.ImageSource, Consent: p.Consent, ApplyPolicy: p.ApplyPolicy, BDXMaxBlockSize: p.BDXMaxBlockSize,
			RebootQuery: p.RebootQuery, TransferBlockSize: p.TransferBlockSize,
		},
		Logger: spec.Logger,
		Clock:  spec.Clock,
	}
}

// pbkdfIterations is the controller's iteration count as the node's
// field; a count outside [node.PBKDFIterationsMin, node.PBKDFIterationsMax]
// is 0, which node.Config.Validate refuses by name.
func pbkdfIterations(v int) uint32 {
	if v < node.PBKDFIterationsMin || v > node.PBKDFIterationsMax {
		return 0
	}
	return uint32(v)
}

// Start is node.New and Start over [Config]; the address is the node's
// socket, loopback for an unspecified one.
func Start(ctx context.Context, spec controller.OwnNodeSpec) (controller.OwnNode, *net.UDPAddr, error) {
	n, err := node.New(ctx, Config(spec))
	if err != nil {
		return nil, nil, err
	}
	Observe(n, spec)
	if err := n.Start(ctx); err != nil {
		return nil, nil, err
	}
	addr, err := net.ResolveUDPAddr("udp", n.Bridge().LocalAddr())
	if err != nil {
		_ = n.Stop(context.WithoutCancel(ctx))
		return nil, nil, err
	}
	if addr.IP.IsUnspecified() {
		addr.IP = net.IPv6loopback
	}
	return Wrap(n), addr, nil
}

// Observe registers spec's callbacks on n, the node built from [Config]:
// the provider's status observer ([node.Node.OnOTAProviderStatus]).
// [Start] calls it; a host that builds the node from Config itself calls
// it once after node.New. It returns the unsubscribe function.
func Observe(n *node.Node, spec controller.OwnNodeSpec) (unsubscribe func()) {
	return n.OnOTAProviderStatus(spec.Provider.OnStatusChange)
}

// Wrap is n as the controller's own node.
func Wrap(n *node.Node) controller.OwnNode { return ownNode{n} }

type ownNode struct{ n *node.Node }

func (o ownNode) OTAProviderEndpoint() (uint16, bool) { return o.n.OTAProviderEndpoint() }

func (o ownNode) BDX() *bdx.Responder { return o.n.BDX() }

func (o ownNode) Stop(ctx context.Context) error { return o.n.Stop(ctx) }

// HoldsFabric implements controller.OwnNode over the node's fabric table.
func (o ownNode) HoldsFabric(ctx context.Context, fabricID, nodeID uint64, rootPublicKey []byte) (bool, error) {
	fabrics, err := o.n.FabricInfos(ctx)
	if err != nil {
		return false, err
	}
	for _, f := range fabrics {
		if f.FabricID == fabricID && f.NodeID == nodeID && bytes.Equal(f.RootPublicKey, rootPublicKey) {
			return true, nil
		}
	}
	return false, nil
}

// Node is the node.Node of an own node Wrap or Start made; nil for
// another controller.OwnNode.
func Node(o controller.OwnNode) *node.Node {
	if w, ok := o.(ownNode); ok {
		return w.n
	}
	return nil
}
