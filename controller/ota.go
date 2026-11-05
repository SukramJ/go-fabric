// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"errors"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/peer"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
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
	// ScriptQueryImage, ScriptApplyUpdate and OnProviderExchange are the
	// manager's test-harness seam: they script the provider's answers and
	// report what it answered ([softwareupdate.Config]).
	ScriptQueryImage   func(p softwareupdate.Peer, req softwareupdate.QueryImageRequest) (softwareupdate.QueryImageScript, bool)
	ScriptApplyUpdate  func(p softwareupdate.Peer, req provdef.ApplyUpdateRequestRequest) (softwareupdate.ApplyUpdateScript, bool)
	OnProviderExchange func(x softwareupdate.ProviderExchange)
}

// openOTA builds the update manager and the provider server, which
// serveEndpoints mounts; startOTA runs the manager once the tree serves.
func (c *Controller) openOTA(ctx context.Context, cfg Config) error {
	o := cfg.OTA
	images := o.Images
	if images == nil {
		images = softwareupdate.NewImages(softwareupdate.ImagesConfig{Logger: c.log})
	}
	mgr, err := softwareupdate.New(softwareupdate.Config{
		Registry: c.reg, Images: images, FabricIndex: c.fabricIndex(), ProviderNodeID: c.fabric.RootNodeID, ProviderEndpoint: OTAProviderEndpoint,
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
		ScriptQueryImage: o.ScriptQueryImage, ScriptApplyUpdate: o.ScriptApplyUpdate, OnProviderExchange: o.OnProviderExchange,
		Clock: c.clock, Logger: c.log,
	})
	if err != nil {
		return err
	}
	c.ota.Store(mgr)
	provider, err := c.newOTAProvider(ctx, mgr, o.BDXAdditionalMrpDelay)
	if err != nil {
		return err
	}
	c.otaProvider = provider
	mgr.SetBDX(provider.bdx)
	mgr.SetProvider(c.fabric.RootNodeID, OTAProviderEndpoint)
	return nil
}

// startOTA retires a former own node and starts the manager.
func (c *Controller) startOTA(ctx context.Context, cfg Config) error {
	if err := c.retireOwnNode(ctx, store.New(cfg.DB), c.log); err != nil {
		return err
	}
	c.ota.Load().Start()
	return nil
}

func (c *Controller) fabricIndex() uint8 { return c.fabric.AddressOf(0).FabricIndex }

// OwnNode is the controller's own node — the node the OTA provider runs on
// and the group sender a program may use — with its node id and address;
// nil without [Config].OTA.

// SoftwareUpdates is the update manager — matter.js's SoftwareUpdateManager
// (docs/OTA.md "Triggering an Update", "Monitoring Update Progress" at
// 862ad75f): AddUpdateConsent, ForceUpdate, RemoveConsent, QueryUpdates,
// QueuedUpdates; nil without [Config].OTA.
func (c *Controller) SoftwareUpdates() *softwareupdate.Manager { return c.ota.Load() }

// BDX is the provider's BDX responder, the transfers a node downloads an
// image through; nil without [Config.OTA].
func (c *Controller) BDX() *bdx.Responder {
	if c.otaProvider == nil {
		return nil
	}
	return c.otaProvider.bdx
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

// armerObserver sees the report sessions the reboot armer is fed — a test
// seam (export_ota_test.go), as matter.js's OtaTest listens on
// ClientSubscriptions.reportStarted (OtaTest.ts:919-921 at 862ad75f). It
// observes only; nil in production.
type armerObserver struct {
	ReportStarted func(s peer.ArmerSession)
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
		as := armerSession{s: s, initiator: c.peers.Initiated(s)}
		if o := c.armerObs.Load(); o != nil && o.ReportStarted != nil {
			o.ReportStarted(as)
		}
		c.armer.ReportStarted(as)
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
