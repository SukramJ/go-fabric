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
	"fmt"
	"net"

	"github.com/SukramJ/go-fabric/bdx"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// Config is the node configuration of spec: its socket (IPv4 only when
// spec.Listen names an IPv4 host, as a lab network without IPv6 needs),
// commissioning
// window, product, database and OTA provider, the controller's logger
// and clock. The provider's status observer is a node callback, not
// configuration: [Observe] registers it on the node New built.
func Config(spec controller.OwnNodeSpec) node.Config {
	p := spec.Provider
	return node.Config{
		Network:    node.Network{Listen: spec.Listen, PreferIPv4: isIPv4Listen(spec.Listen)},
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

// isIPv4Listen reports whether a listen address names an IPv4 host.
func isIPv4Listen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() != nil
}

// Start is node.New and Start over [Config]; the address is the node's
// socket, the loopback of its family for an unspecified one.
func Start(ctx context.Context, spec controller.OwnNodeSpec) (controller.OwnNode, *net.UDPAddr, error) {
	n, err := node.New(ctx, Config(spec))
	if err != nil {
		return nil, nil, err
	}
	Observe(n, spec)
	if err := n.Start(ctx); err != nil {
		_ = n.Stop(context.WithoutCancel(ctx)) // a failed Start leaves the node startable and its store open
		return nil, nil, err
	}
	addr, err := net.ResolveUDPAddr("udp", n.Bridge().LocalAddr())
	if err != nil {
		_ = n.Stop(context.WithoutCancel(ctx))
		return nil, nil, err
	}
	if addr.IP.IsUnspecified() {
		addr.IP = net.IPv6loopback
		if isIPv4Listen(spec.Listen) {
			addr.IP = net.IPv4(127, 0, 0, 1)
		}
	}
	return Wrap(n), addr, nil
}

// Observe registers spec's callbacks on n, the node built from [Config]:
// the provider's status observer ([node.Node.OnOTAProviderStatus]).
// [Start] calls it; a host that builds the node from Config itself calls
// it once after node.New. It returns the unsubscribe function.
func Observe(n *node.Node, spec controller.OwnNodeSpec) (unsubscribe func()) {
	off := n.OnOTAProviderStatus(spec.Provider.OnStatusChange)
	restore := wrapCommands(n.OTAProvider(), spec.Provider.Commands)
	return func() { off(); restore() }
}

// wrapCommands installs the update manager's command wrappers
// (softwareupdate.ProviderCommands, the scripted provider of a test
// harness) over the provider's own handlers, and returns the function that
// restores them.
func wrapCommands(p *node.OTAProviderServer, c *softwareupdate.ProviderCommands) (restore func()) {
	if p == nil || c == nil {
		return func() {}
	}
	if c.QueryImage != nil {
		_ = p.Handle(provdef.CmdQueryImage, func(ctx context.Context, fields any) (any, error) {
			req, err := fieldsAs[provdef.QueryImageRequest](fields)
			if err != nil {
				return nil, err
			}
			own := func(ctx context.Context) (provdef.QueryImageResponse, error) { return p.QueryImage(ctx, req) }
			peer, ok := requestor(ctx)
			if !ok {
				return own(ctx)
			}
			return c.QueryImage(ctx, peer, req, own)
		})
	}
	if c.ApplyUpdate != nil {
		_ = p.Handle(provdef.CmdApplyUpdateRequest, func(ctx context.Context, fields any) (any, error) {
			req, err := fieldsAs[provdef.ApplyUpdateRequestRequest](fields)
			if err != nil {
				return nil, err
			}
			own := func(ctx context.Context) (provdef.ApplyUpdateResponse, error) { return p.ApplyUpdateRequest(ctx, req) }
			peer, ok := requestor(ctx)
			if !ok {
				return own(ctx)
			}
			return c.ApplyUpdate(ctx, peer, req, own)
		})
	}
	if c.NotifyUpdateApplied != nil {
		_ = p.Handle(provdef.CmdNotifyUpdateApplied, func(ctx context.Context, fields any) (any, error) {
			req, err := fieldsAs[provdef.NotifyUpdateAppliedRequest](fields)
			if err != nil {
				return nil, err
			}
			own := func(ctx context.Context) error { return p.NotifyUpdateApplied(ctx, req) }
			peer, ok := requestor(ctx)
			if !ok {
				return nil, own(ctx)
			}
			return nil, c.NotifyUpdateApplied(ctx, peer, req, own)
		})
	}
	return func() {
		for _, cmd := range []uint32{provdef.CmdQueryImage, provdef.CmdApplyUpdateRequest, provdef.CmdNotifyUpdateApplied} {
			_ = p.Handle(cmd, nil)
		}
	}
}

// requestor is the CASE peer a command came from; a command without one
// goes to the provider's own handler, which refuses it.
func requestor(ctx context.Context) (mattercore.OTAPeer, bool) {
	_, fabric := im.FabricFilterFromContext(ctx)
	nodeID, _ := im.SubjectFromContext(ctx)
	if fabric == 0 || nodeID == 0 || im.IsPASEFromContext(ctx) {
		return mattercore.OTAPeer{}, false
	}
	return mattercore.OTAPeer{FabricIndex: fabric, NodeID: nodeID}, true
}

// fieldsAs is a command's decoded request, as a value or a pointer.
func fieldsAs[T any](fields any) (T, error) {
	switch f := fields.(type) {
	case *T:
		if f != nil {
			return *f, nil
		}
	case T:
		return f, nil
	}
	var zero T
	return zero, invalidCommand(fmt.Sprintf("controllernode: command fields %T", fields))
}

// invalidCommand is answered INVALID_COMMAND, as the provider answers
// fields it cannot take.
type invalidCommand string

func (e invalidCommand) Error() string { return string(e) }

// MatterStatusCode reports the IM status the error is answered with.
func (invalidCommand) MatterStatusCode() im.StatusCode { return im.StatusInvalidCommand }

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
