// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"context"
	"net"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/store"
)

// The controller's own identity serves what a node of the fabric serves:
// the group and ota commands send group messages and serve OTA images as
// RootNodeID through controller.New's own tree (controller.Config.OTA,
// Controller.Groups) — matter.js's one controller node. Until phase 37 the
// tool ran a second node for both (the "own node"); its record migrates
// below.

// selfHooks are the seams a test replaces.
var selfHooks struct {
	advertiser func() mdns.Advertiser
	transport  func() groups.MulticastTransport
	// started learns the controller's node id and socket, as DNS-SD would
	// from its operational record.
	started func(nodeID uint64, addr *net.UDPAddr)
}

// otaConfig is the OTA push the stack runs with: the image store and the
// serving command's status observer.
func (s *stack) otaConfig() *controller.OTAConfig {
	s.images = softwareupdate.NewImages(softwareupdate.ImagesConfig{Logger: s.e.log})
	return &controller.OTAConfig{
		Images: s.images, AllowTestImages: true,
		OnStatus: func(p softwareupdate.Peer, st softwareupdate.Status, v uint32) {
			s.statusMu.Lock()
			f := s.onStatus
			s.statusMu.Unlock()
			if f != nil {
				f(p, st, v)
			}
		},
	}
}

// migrateSelfRecord moves the tool's record of its former own node under
// controller.New's key, which retires it (controller/ota_provider.go).
func migrateSelfRecord(ctx context.Context, st *store.Store) error {
	old, ok, err := st.GetSetting(ctx, selfKey)
	if err != nil || !ok {
		return err
	}
	if _, ok, err := st.GetSetting(ctx, ownNodeKey); err != nil || ok {
		return err
	}
	if err := st.SetSetting(ctx, ownNodeKey, old); err != nil {
		return err
	}
	return st.DeleteSetting(ctx, selfKey)
}

// ownNodeKey is controller.New's settings key of the former own node.
const ownNodeKey = "controller.ownnode"
