// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"sync"

	"github.com/SukramJ/go-fabric/bdx"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/store"
)

// The controller's own node. A group message is sealed and sent by a node
// of the fabric, and an OTA image is served by one: the controller role
// has neither, so — as node/groupsend_live_test.go and
// node/ota_live_test.go do — the tool runs a node.Node of its own, in the
// same database, commissioned into its own fabric by its own commissioner
// on first use. It serves no device; it has the OTA provider endpoint
// always, so its endpoint layout never changes between runs.

// selfHooks are the seams a test replaces.
var selfHooks struct {
	advertiser func() mdns.Advertiser
	transport  func() groups.MulticastTransport
	// started learns the own node's id and socket, as DNS-SD would from
	// its operational record.
	started func(nodeID uint64, addr *net.UDPAddr)
}

// selfRecord is what the database keeps of the own node.
type selfRecord struct {
	Passcode      uint32 `json:"passcode"`
	Discriminator uint16 `json:"discriminator"`
	Salt          []byte `json:"salt"`
	NodeID        uint64 `json:"nodeId"`
}

type selfNode struct {
	n      *node.Node
	nodeID uint64
	addr   *net.UDPAddr
	ia     *interaction.Interaction
	images *images
}

func (sn *selfNode) stop() { _ = sn.n.Stop(context.Background()) }

// loadSelfRecord reads the own node's record, creating one on first use.
func loadSelfRecord(ctx context.Context, st *store.Store) (selfRecord, error) {
	var r selfRecord
	v, ok, err := st.GetSetting(ctx, selfKey)
	if err != nil {
		return r, err
	}
	if ok {
		if err := json.Unmarshal([]byte(v), &r); err != nil {
			return r, fmt.Errorf("%s: %w", selfKey, err)
		}
		return r, nil
	}
	for !setup.IsValidPasscode(r.Passcode) {
		r.Passcode = uint32(randomUint64() % 99999998) //nolint:gosec // G115: below 2^27
	}
	r.Discriminator = uint16(randomUint64() & setup.MaxDiscriminator) //nolint:gosec // G115: masked to 12 bits
	r.Salt = make([]byte, 32)
	_, _ = rand.Read(r.Salt)
	r.NodeID = randomUint64()%operationalNodeMax + 1
	b, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	return r, st.SetSetting(ctx, selfKey, string(b))
}

// ensureSelf starts the own node and commissions it into the fabric when
// its fabric table does not hold the fabric yet.
func (s *stack) ensureSelf(ctx context.Context) (*selfNode, error) {
	if s.self != nil {
		return s.self, nil
	}
	rec, err := loadSelfRecord(ctx, s.settings)
	if err != nil {
		return nil, err
	}
	adv := func() mdns.Advertiser { return mdns.NewZeroconf() }
	if selfHooks.advertiser != nil {
		adv = selfHooks.advertiser
	}
	imgs := &images{e: s.e}
	n, err := node.New(node.Config{ //nolint:contextcheck // the node runs on its own context until Stop
		Network:            node.Network{Listen: s.e.opts.nodeListen, PreferIPv4: isIPv4Listen(s.e.opts.nodeListen)},
		Advertiser:         adv(),
		Commissioning:      node.Commissioning{Passcode: rec.Passcode, Discriminator: rec.Discriminator, PBKDFIterations: 1000, Salt: rec.Salt},
		ProductDescription: node.ProductDescription{VendorID: fabricVendorID, ProductID: 0x8000},
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric", ProductName: "reference controller", NodeLabel: "reference-controller", SerialNumber: fmt.Sprintf("%016X", rec.NodeID),
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Store:       node.Store{DB: s.db},
		OTAProvider: &node.OTAProviderConfig{ImageSource: imgs, OnStatusChange: imgs.status},
		Logger:      s.e.log,
	})
	if err != nil {
		return nil, fmt.Errorf("own node: %w", err)
	}
	if err := n.Start(ctx); err != nil {
		return nil, fmt.Errorf("own node: %w", err)
	}
	sn := &selfNode{n: n, nodeID: rec.NodeID, images: imgs}
	s.self = sn
	if sn.addr, err = net.ResolveUDPAddr("udp", n.Bridge().LocalAddr()); err != nil {
		return nil, err
	}
	if sn.addr.IP.IsUnspecified() {
		sn.addr.IP = net.IPv6loopback
		if isIPv4Listen(s.e.opts.listen) || sn.addr.IP.To4() != nil {
			sn.addr.IP = net.IPv4(127, 0, 0, 1)
		}
	}
	if selfHooks.transport != nil {
		n.Groups().SetTransport(selfHooks.transport())
	}
	if selfHooks.started != nil {
		selfHooks.started(rec.NodeID, sn.addr)
	}
	addr := s.fabric.AddressOf(rec.NodeID)
	if _, ok, err := sn.fabricIndex(ctx, s); err != nil {
		return nil, err
	} else if !ok {
		// The own node runs the CSA test chain: its attestation is
		// accepted whatever --attestation says.
		if _, err := s.com.Commission(ctx, commissioner.Options{
			Fabric: s.fabric, Passcode: rec.Passcode, NodeID: rec.NodeID,
			Addresses: []*net.UDPAddr{sn.addr}, OnAttestationFailure: attestation.Accept(),
		}); err != nil {
			return nil, fmt.Errorf("commissioning the own node: %w", err)
		}
		s.e.log.Info("controller.self.commissioned", "node", rec.NodeID)
	}
	// The own node is reached at its socket; DNS-SD is not needed for it.
	p := s.set.Get(addr)
	if p == nil {
		if p, err = s.set.AddKnownPeer(peer.Descriptor{Address: addr, OperationalAddress: sn.addr}); err != nil { //nolint:contextcheck // the peer resolves on the set's context
			return nil, err
		}
	}
	sn.ia = s.com.Interaction(p)
	return sn, nil
}

// fabricIndex is the index of our fabric in the own node's fabric table.
func (sn *selfNode) fabricIndex(ctx context.Context, s *stack) (index uint8, ok bool, err error) {
	fabrics, err := sn.n.Fabrics(ctx)
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

// providerEndpoint is the own node's OTA provider endpoint.
func (sn *selfNode) providerEndpoint() (uint16, error) {
	for _, ep := range sn.n.Bridge().Topology().Endpoints {
		for _, srv := range endpoint.ClusterServers(ep) {
			if srv.MatterClusterID() == provdef.ClusterID {
				return ep.ID, nil
			}
		}
	}
	return 0, errors.New("the own node has no OTA provider endpoint")
}

// images is the own node's OTA image source and consent: the one image
// "ota serve" offers, nothing otherwise.
type images struct {
	e *env

	mu       sync.Mutex
	image    []byte
	vendor   uint16
	product  uint16
	version  uint32
	versionS string
	yes      bool
	onStatus func(peer mattercore.OTAPeer, st mattercore.OTAUpdateStatus, version uint32)
	asking   sync.Mutex
}

const imageFile = "reference-controller.ota"

var _ mattercore.OTAImageSource = (*images)(nil)

// QueryImage implements mattercore.OTAImageSource: the image for its
// vendor and product above the requestor's version, once the console
// granted it (or --yes did). The consent is given here, before the offer,
// rather than through mattercore.OTAConsent: matter.js asks that port only
// for a requestor that cannot consent itself, and discontinues at
// ApplyUpdate an update whose consent it does not hold
// (packages/node/src/behaviors/ota-software-update-provider/
// OtaSoftwareUpdateProviderServer.ts:243-250, :430-437 at 0ceb202d) — so
// the offer states the consent exists.
func (im *images) QueryImage(_ context.Context, p mattercore.OTAPeer, req provdef.QueryImageRequest) (mattercore.OTAImage, bool, error) {
	im.mu.Lock()
	if im.image == nil || req.VendorId != im.vendor || req.ProductId != im.product || req.SoftwareVersion >= im.version {
		im.mu.Unlock()
		return mattercore.OTAImage{}, false, nil
	}
	img := mattercore.OTAImage{FileDesignator: bdx.FileDesignator(imageFile), SoftwareVersion: im.version, SoftwareVersionString: im.versionS}
	yes := im.yes
	im.mu.Unlock()
	if !yes {
		im.asking.Lock()
		ok := confirm(im.e, fmt.Sprintf("node %d asks to update from version %d to %d (%s). Allow?", p.NodeID, req.SoftwareVersion, img.SoftwareVersion, img.SoftwareVersionString))
		im.asking.Unlock()
		if !ok {
			return mattercore.OTAImage{}, false, nil
		}
	}
	return img, true, nil
}

// Open implements mattercore.OTAImageSource.
func (im *images) Open(_ context.Context, file string) (bdx.Blob, error) {
	im.mu.Lock()
	defer im.mu.Unlock()
	if file != imageFile || im.image == nil {
		return nil, fs.ErrNotExist
	}
	return bytes.NewReader(im.image), nil
}

func (im *images) status(p mattercore.OTAPeer, st mattercore.OTAUpdateStatus, version uint32) {
	im.mu.Lock()
	f := im.onStatus
	im.mu.Unlock()
	if f != nil {
		f(p, st, version)
	}
}
