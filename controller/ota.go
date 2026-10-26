// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/store"
)

// OTAConfig turns on the controller's OTA push — matter.js's
// SoftwareUpdateManager with the OtaSoftwareUpdateProviderServer on the
// controller's own node (packages/node/src/behavior/system/software-update/
// SoftwareUpdateManager.ts, packages/node/src/behaviors/
// ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts at
// 862ad75f; docs/OTA.md "Enable OTA Provider on Controller Creation").
//
// matter.js serves the provider from the controller's ServerNode. This
// module's controller role runs no server node: a server node is the node
// facade, a layer above the roles (ADR 0026 principle 1). So the controller
// has [OTAConfig].StartNode start one — the controller's own node, in the
// controller's database, commissioned into our fabric by our commissioner
// on its first start — with node/'s OTA provider mounted and the update
// manager as its image source, consent and apply policy
// (controller/softwareupdate); node/controllernode.Start is that starter.
// The provider is one implementation, the device-side one.
type OTAConfig struct {
	// Images is the image store and the ledger lookups; nil is an
	// in-memory store without ledgers, which offers local images only.
	Images *softwareupdate.Images
	// AllowTestImages, UpdateCheckInterval, AnnounceAsDefaultProvider,
	// AnnouncementInterval, MaxBDXBlockSize, BDXAdditionalMrpDelay and
	// ApplyDelay are the manager's ([softwareupdate.Config]).
	AllowTestImages           bool
	UpdateCheckInterval       time.Duration
	AnnounceAsDefaultProvider bool
	AnnouncementInterval      time.Duration
	MaxBDXBlockSize           uint16
	BDXAdditionalMrpDelay     *time.Duration
	ApplyDelay                time.Duration
	// RequestConsent, OnUpdateAvailable, OnUpdateApplying, OnUpdateDone,
	// OnUpdateFailed and OnStatus are the manager's hooks.
	RequestConsent                                 func(ctx context.Context, p softwareupdate.Peer, req softwareupdate.QueryImageRequest, d softwareupdate.AvailableDetails) (softwareupdate.ConsentState, time.Duration)
	OnUpdateAvailable                              func(p softwareupdate.Peer, info softwareupdate.SoftwareUpdateInfo)
	OnUpdateApplying, OnUpdateDone, OnUpdateFailed func(p softwareupdate.Peer)
	OnStatus                                       func(p softwareupdate.Peer, status softwareupdate.Status, version uint32)

	// Listen is the own node's UDP address; "" is "[::]:0".
	Listen string
	// Advertiser publishes the own node's operational record, which a
	// requestor finds the provider by; nil is mdns.NewZeroconf().
	Advertiser mdns.Advertiser
	// ProductID is the own node's product id; 0 is 0x8000.
	ProductID uint16
	// StartNode starts the own node from spec and returns it with its
	// address (required): node/controllernode.Start, or a test's starter
	// that runs the node on its site (fabrictest.Site.ControllerConfig).
	StartNode func(ctx context.Context, spec OwnNodeSpec) (OwnNode, *net.UDPAddr, error)
}

// OwnNodeSpec is what [OTAConfig].StartNode starts the own node with:
// node/controllernode.Config makes the node.Config of it.
type OwnNodeSpec struct {
	// NodeID is the node id the node takes on our fabric; the
	// commissioning that follows looks it up by it.
	NodeID uint64
	// Listen and Advertiser are [OTAConfig]'s, defaulted.
	Listen     string
	Advertiser mdns.Advertiser
	// Passcode, Discriminator, Salt and Iterations are its commissioning
	// window's — the record the controller keeps of the node.
	Passcode      uint32
	Discriminator uint16
	Salt          []byte
	Iterations    int
	// VendorID is our fabric's vendor; ProductID [OTAConfig]'s, defaulted;
	// SerialNumber names the node.
	VendorID, ProductID uint16
	SerialNumber        string
	// DB is the controller's database, which the node keeps its state in.
	DB *sql.DB
	// Provider is the OTA provider the node mounts: the update manager's
	// ports (softwareupdate.Manager.ProviderConfig).
	Provider softwareupdate.ProviderConfig
	// Logger and Clock are the controller's.
	Logger *slog.Logger
	Clock  clock.Clock
}

// OwnNode is the own node as the controller's OTA push uses it; a
// node.Node wrapped by node/controllernode.Wrap is one.
type OwnNode interface {
	// OTAProviderEndpoint is the provider's endpoint number.
	OTAProviderEndpoint() (uint16, bool)
	// BDX is the provider's BDX responder.
	BDX() *bdx.Responder
	// HoldsFabric reports whether the node's fabric table holds the
	// fabric of fabricID with that root public key, the node as nodeID.
	HoldsFabric(ctx context.Context, fabricID, nodeID uint64, rootPublicKey []byte) (bool, error)
	// Stop stops the node.
	Stop(ctx context.Context) error
}

// ownNodeKey is the settings key of the own node's record.
const ownNodeKey = "controller.ownnode"

// ownNodeRecord is what the database keeps of the own node.
type ownNodeRecord struct {
	Passcode      uint32 `json:"passcode"`
	Discriminator uint16 `json:"discriminator"`
	Salt          []byte `json:"salt"`
	NodeID        uint64 `json:"nodeId"`
}

// loadOwnNode reads the own node's record, creating one on the first run.
func loadOwnNode(ctx context.Context, st *store.Store) (ownNodeRecord, error) {
	var r ownNodeRecord
	v, ok, err := st.GetSetting(ctx, ownNodeKey)
	if err != nil {
		return r, err
	}
	if ok {
		if err := json.Unmarshal([]byte(v), &r); err != nil {
			return r, fmt.Errorf("%s: %w", ownNodeKey, err)
		}
		return r, nil
	}
	var b [8]byte
	for !setup.IsValidPasscode(r.Passcode) {
		_, _ = rand.Read(b[:4])
		r.Passcode = binary.BigEndian.Uint32(b[:4]) % 99999998
	}
	_, _ = rand.Read(b[:2])
	r.Discriminator = binary.BigEndian.Uint16(b[:2]) & setup.MaxDiscriminator
	r.Salt = make([]byte, 32)
	_, _ = rand.Read(r.Salt)
	r.NodeID = random()%0xFFFF_FFEF_FFFF_FFFF + 1
	enc, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	return r, st.SetSetting(ctx, ownNodeKey, string(enc))
}

// startOTA builds the update manager, starts the own node with the
// manager's provider, commissions it into our fabric when its fabric
// table lacks it, and starts the manager.
func (c *Controller) startOTA(ctx context.Context, cfg Config) error {
	o := cfg.OTA
	images := o.Images
	if images == nil {
		images = softwareupdate.NewImages(softwareupdate.ImagesConfig{Logger: c.log})
	}
	rec, err := loadOwnNode(ctx, store.New(cfg.DB))
	if err != nil {
		return err
	}
	mgr, err := softwareupdate.New(softwareupdate.Config{
		Registry: c.reg, Images: images, FabricIndex: c.fabricIndex(), ProviderNodeID: rec.NodeID,
		VendorID: c.fabric.RootVendorID, Armer: c.armer,
		SessionID: func(p softwareupdate.Peer) (uint16, bool) {
			if pp := c.peers.Get(p); pp != nil {
				if s := pp.NewestSession(); s != nil {
					return s.ID(), true
				}
			}
			return 0, false
		},
		AllowTestImages: o.AllowTestImages, UpdateCheckInterval: o.UpdateCheckInterval,
		AnnounceAsDefaultProvider: o.AnnounceAsDefaultProvider, AnnouncementInterval: o.AnnouncementInterval,
		MaxBDXBlockSize: o.MaxBDXBlockSize, BDXAdditionalMrpDelay: o.BDXAdditionalMrpDelay, ApplyDelay: o.ApplyDelay,
		RequestConsent: o.RequestConsent, OnUpdateAvailable: o.OnUpdateAvailable, OnUpdateApplying: o.OnUpdateApplying,
		OnUpdateDone: o.OnUpdateDone, OnUpdateFailed: o.OnUpdateFailed, OnStatus: o.OnStatus,
		Clock: c.clock, Logger: c.log,
	})
	if err != nil {
		return err
	}
	c.ota.Store(mgr)
	listen := o.Listen
	if listen == "" {
		listen = "[::]:0"
	}
	adv := o.Advertiser
	if adv == nil {
		adv = mdns.NewZeroconf()
	}
	product := o.ProductID
	if product == 0 {
		product = 0x8000
	}
	spec := OwnNodeSpec{
		NodeID: rec.NodeID, Listen: listen, Advertiser: adv,
		Passcode: rec.Passcode, Discriminator: rec.Discriminator, Salt: rec.Salt, Iterations: 1000,
		VendorID: c.fabric.RootVendorID, ProductID: product, SerialNumber: fmt.Sprintf("%016X", rec.NodeID),
		DB: cfg.DB, Provider: mgr.ProviderConfig(), Logger: c.log, Clock: c.clock,
	}
	n, addr, err := o.StartNode(ctx, spec)
	if err != nil {
		return fmt.Errorf("controller: own node: %w", err)
	}
	c.ownNode, c.ownNodeID, c.ownAddr = n, rec.NodeID, addr
	mgr.SetBDX(n.BDX())
	if err := c.commissionOwnNode(ctx, rec, addr); err != nil {
		return err
	}
	ep, ok := n.OTAProviderEndpoint()
	if !ok {
		return errors.New("controller: the own node has no OTA provider endpoint")
	}
	mgr.SetProvider(rec.NodeID, ep)
	mgr.Start()
	return nil
}

// commissionOwnNode commissions the own node into our fabric unless its
// fabric table holds it, and adds it to the peers at its address — DNS-SD
// is not needed for it.
func (c *Controller) commissionOwnNode(ctx context.Context, rec ownNodeRecord, addr *net.UDPAddr) error {
	have, err := c.ownNode.HoldsFabric(ctx, c.fabric.FabricID, rec.NodeID, c.fabric.RootPublicKey)
	if err != nil {
		return err
	}
	if !have {
		// The own node runs the CSA test attestation: it is accepted.
		if _, err := c.com.Commission(ctx, commissioner.Options{
			Fabric: c.fabric, Passcode: rec.Passcode, NodeID: rec.NodeID,
			Addresses: []*net.UDPAddr{addr}, OnAttestationFailure: attestation.Accept(),
		}); err != nil {
			return fmt.Errorf("controller: commissioning the own node: %w", err)
		}
		c.log.Info("controller.own_node.commissioned", "node", rec.NodeID)
	}
	paddr := c.fabric.AddressOf(rec.NodeID)
	if c.peers.Get(paddr) == nil {
		//nolint:contextcheck // the peer resolves on the set's context
		if _, err := c.peers.AddKnownPeer(peer.Descriptor{Address: paddr, OperationalAddress: addr}); err != nil {
			return err
		}
	}
	return nil
}

// fabricIndex is our fabric's local index — the index the peer addresses
// of our nodes carry.
func (c *Controller) fabricIndex() uint8 { return c.fabric.AddressOf(0).FabricIndex }

// OwnNode is the controller's own node — the node the OTA provider runs on
// and the group sender a program may use — with its node id and address;
// nil without [Config].OTA.
func (c *Controller) OwnNode() (n OwnNode, nodeID uint64, addr *net.UDPAddr) {
	return c.ownNode, c.ownNodeID, c.ownAddr
}

// SoftwareUpdates is the update manager — matter.js's SoftwareUpdateManager
// (docs/OTA.md "Triggering an Update", "Monitoring Update Progress" at
// 862ad75f): AddUpdateConsent, ForceUpdate, RemoveConsent, QueryUpdates,
// QueuedUpdates; nil without [Config].OTA.
func (c *Controller) SoftwareUpdates() *softwareupdate.Manager {
	if c.ownNode == nil {
		return nil
	}
	return c.ota.Load()
}

// armerContext is the reboot armer's view of the stack — matter.js's
// SessionManager and ClientSubscriptions (RebootResubscribeArmer.ts:58-75
// at 862ad75f).
func (c *Controller) armerContext() peer.RebootArmerContext {
	return peer.RebootArmerContext{
		Clock: c.clock, Logger: c.log,
		ClosePeerSessionsBefore: func(p exchange.PeerAddress, t time.Time) {
			if pp := c.peers.Get(p); pp != nil {
				for _, s := range pp.Sessions() {
					if s.CreatedAt().Before(t) {
						s.Close()
					}
				}
			}
		},
		PeerLost:           func(p exchange.PeerAddress) { c.mgr.HandlePeerLoss(p, errRebootReturnTimeout, time.Time{}) },
		CloseSubscriptions: func(p exchange.PeerAddress) { c.subs.CloseForPeer(p) },
	}
}

// sessionAdded feeds a peer's new session to the armer
// (RebootResubscribeArmer.ts:117-150 at 862ad75f).
func (c *Controller) sessionAdded(p *peer.Peer, s *exchange.Session) {
	if c.armer != nil && s.IsSecure() {
		c.armer.SessionAdded(armerSession{s: s, initiator: c.peers.Initiated(s)})
	}
}

// reportStarted feeds a report's session to the armer
// (RebootResubscribeArmer.ts:68, :165-172).
func (c *Controller) reportStarted(s *exchange.Session) {
	if c.armer != nil {
		c.armer.ReportStarted(armerSession{s: s, initiator: c.peers.Initiated(s)})
	}
}

// errRebootReturnTimeout is the cause of a peer the armer gave up.
var errRebootReturnTimeout = errors.New("controller: no session after the expected reboot")

// armerSession is an exchange session as the armer sees it.
type armerSession struct {
	s         *exchange.Session
	initiator bool
}

func (a armerSession) Peer() exchange.PeerAddress {
	return exchange.PeerAddress{FabricIndex: a.s.FabricIndex(), NodeID: a.s.PeerNodeID()}
}
func (a armerSession) Initiator() bool      { return a.initiator }
func (a armerSession) CreatedAt() time.Time { return a.s.CreatedAt() }

// otaHooks feeds every node's hooks to the update manager before the
// program's — the manager is built after the registry, so each hook reads
// it when it runs.
func (c *Controller) otaHooks(user func(id string) client.Hooks) func(id string) client.Hooks {
	return func(id string) client.Hooks {
		var h client.Hooks
		if user != nil {
			h = user(id)
		}
		inner := h
		nodeOf := func() *client.Node {
			if r := c.registry.Load(); r != nil {
				return r.Get(id)
			}
			return nil
		}
		h.Changed = func() {
			if m, n := c.ota.Load(), nodeOf(); m != nil && n != nil {
				m.NodeChanged(n)
			}
			if inner.Changed != nil {
				inner.Changed()
			}
		}
		h.Event = func(endpoint uint16, cluster, event uint32, e client.EventData, value any) {
			if m, n := c.ota.Load(), nodeOf(); m != nil && n != nil {
				m.NodeEvent(n, endpoint, cluster, event, value)
			}
			if inner.Event != nil {
				inner.Event(endpoint, cluster, event, e, value)
			}
		}
		h.SubscriptionStatusChanged = func(active bool) {
			if m, n := c.ota.Load(), nodeOf(); m != nil && n != nil {
				m.NodeSubscription(n, active)
			}
			if inner.SubscriptionStatusChanged != nil {
				inner.SubscriptionStatusChanged(active)
			}
		}
		return h
	}
}
