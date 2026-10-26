// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"bytes"
	"context"
	"errors"
	"net"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/node/controllernode"
	"github.com/SukramJ/go-fabric/store"
)

// The controller's own node. A group message is sealed and sent by a node
// of the fabric, and an OTA image is served by one: controller.New runs
// such a node for its OTA push (controller.Config.OTA) — in the same
// database, commissioned into its own fabric by its own commissioner on
// first use, with the OTA provider endpoint always, so its endpoint layout
// never changes between runs. The group and ota commands open the stack
// with it.

// selfHooks are the seams a test replaces.
var selfHooks struct {
	advertiser func() mdns.Advertiser
	transport  func() groups.MulticastTransport
	// started learns the own node's id and socket, as DNS-SD would from
	// its operational record.
	started func(nodeID uint64, addr *net.UDPAddr)
}

type selfNode struct {
	n      *node.Node
	nodeID uint64
	addr   *net.UDPAddr
	ia     *interaction.Interaction
}

// ownNodeConfig is the OTA configuration the own node runs with: the
// image store the ota command fills, local images allowed (matter.js
// offers a stored image that is not "prod" only with allowTestOtaImages,
// SoftwareUpdateManager.ts:398-406 at 862ad75f), --node-listen, and the
// statuses to the serving command.
func (s *stack) ownNodeConfig() *controller.OTAConfig {
	s.images = softwareupdate.NewImages(softwareupdate.ImagesConfig{Logger: s.e.log})
	adv := func() mdns.Advertiser { return mdns.NewZeroconf() }
	if selfHooks.advertiser != nil {
		adv = selfHooks.advertiser
	}
	return &controller.OTAConfig{
		Images: s.images, AllowTestImages: true, Listen: s.e.opts.nodeListen, Advertiser: adv(),
		OnStatus: func(p softwareupdate.Peer, st softwareupdate.Status, v uint32) {
			s.statusMu.Lock()
			f := s.onStatus
			s.statusMu.Unlock()
			if f != nil {
				f(p, st, v)
			}
		},
		StartNode: startSelf,
	}
}

// startSelf starts the own node — node/controllernode's configuration,
// node.New and Start, its socket on loopback when it is unspecified — and
// runs the test seams.
func startSelf(ctx context.Context, spec controller.OwnNodeSpec) (controller.OwnNode, *net.UDPAddr, error) {
	cfg := controllernode.Config(spec)
	cfg.Network.PreferIPv4 = isIPv4Listen(cfg.Network.Listen)
	n, err := node.New(cfg) //nolint:contextcheck // node.New takes no context
	if err != nil {
		return nil, nil, err
	}
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
		if cfg.Network.PreferIPv4 {
			addr.IP = net.IPv4(127, 0, 0, 1)
		}
	}
	if selfHooks.transport != nil {
		n.Groups().SetTransport(selfHooks.transport())
	}
	if selfHooks.started != nil {
		selfHooks.started(spec.NodeID, addr)
	}
	return controllernode.Wrap(n), addr, nil
}

// migrateSelfRecord moves the own node's record of an earlier version
// ("controller.self") to the controller's key ("controller.ownnode"); the
// JSON is the same.
func migrateSelfRecord(ctx context.Context, st *store.Store) error {
	old, ok, err := st.GetSetting(ctx, selfKey)
	if err != nil || !ok {
		return err
	}
	if _, ok, err := st.GetSetting(ctx, ownNodeKey); err != nil || ok {
		return err
	}
	return st.SetSetting(ctx, ownNodeKey, old)
}

// ownNodeKey is controller.New's settings key of the own node.
const ownNodeKey = "controller.ownnode"

// ensureSelf is the own node of the stack's controller.
func (s *stack) ensureSelf(context.Context) (*selfNode, error) {
	if s.self != nil {
		return s.self, nil
	}
	own, id, addr := s.ctl.OwnNode()
	n := controllernode.Node(own)
	if n == nil {
		return nil, errors.New("the stack runs no own node")
	}
	p := s.set.Get(s.fabric.AddressOf(id))
	if p == nil {
		return nil, errors.New("the own node is not a peer")
	}
	s.self = &selfNode{n: n, nodeID: id, addr: addr, ia: s.com.Interaction(p)}
	return s.self, nil
}

// fabricIndex is the index of our fabric in the own node's fabric table.
func (sn *selfNode) fabricIndex(ctx context.Context, s *stack) (index uint8, ok bool, err error) {
	fabrics, err := sn.n.FabricInfos(ctx)
	if err != nil {
		return 0, false, err
	}
	for _, f := range fabrics {
		if f.FabricID == s.fabric.FabricID && f.NodeID == sn.nodeID && bytes.Equal(f.RootPublicKey, s.fabric.RootPublicKey) {
			return f.FabricIndex, true, nil
		}
	}
	return 0, false, nil
}
