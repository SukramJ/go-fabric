// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/SukramJ/go-fabric/bdx"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/store"
)

// OTAProviderConfig serves OTA software images to the requestors of the
// node's fabrics: the OTA Software Update Provider (0x0029) on an endpoint
// of its own, device type OtaProvider (0x0014), next to the Aggregator
// (ADR 0021). The three ports are the host's part of matter.js's
// SoftwareUpdateManager (mattercore.OTAProvider).
type OTAProviderConfig struct {
	// ImageSource names and serves the images; nil mounts no provider.
	ImageSource OTAImageSource
	// Consent asks the user; nil declines an image that needs a consent
	// the requestor cannot give.
	Consent OTAConsent
	// ApplyPolicy decides an allowed apply; nil proceeds at once.
	ApplyPolicy OTAApplyPolicy
	// BDXMaxBlockSize, when set, caps the block size of the image
	// transfers.
	BDXMaxBlockSize uint16
	// OnStatusChange observes the requestors' update states; nil observes
	// nothing.
	OnStatusChange func(peer OTAPeer, status OTAUpdateStatus, version uint32)
}

// otaProviderKey is the stable key the provider endpoint's number is kept
// under, in the Aggregator's scope.
const otaProviderKey = endpoint.StringKey("go-fabric:ota-provider")

// otaProviderEndpoint is the provider's endpoint as the assembler sees it:
// matter.js's OtaProviderEndpoint (packages/node/src/endpoints/
// ota-provider.ts:57-64 — device type 0x14, revision 1, the
// OtaSoftwareUpdateProvider server).
type otaProviderEndpoint struct{ p *mattercore.OTAProvider }

func (e otaProviderEndpoint) MatterDeviceType() uint16 { return uint16(device.OtaProviderID) }

func (e otaProviderEndpoint) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{e.p}
}

func (e otaProviderEndpoint) spec() endpoint.Spec {
	return endpoint.Spec{StableKey: otaProviderKey, DeviceType: uint16(device.OtaProviderID), Source: e}
}

// buildOTAProvider builds the provider and the BDX responder it serves its
// images through, attached to the bridge (Bridge.AttachBDXResponder).
func (n *Node) buildOTAProvider() error {
	cfg := n.cfg.OTAProvider
	if cfg == nil || cfg.ImageSource == nil {
		return nil
	}
	n.bdx = bdx.NewResponder(n.logger)
	n.bdx.SetClock(n.cfg.Clock) // the transfers' activity on the provider's clock
	p, err := mattercore.NewOTAProvider(mattercore.OTAProviderConfig{
		ImageSource: cfg.ImageSource, Consent: cfg.Consent, ApplyPolicy: cfg.ApplyPolicy,
		BDX: n.bdx, BDXMaxBlockSize: cfg.BDXMaxBlockSize, OnStatusChange: cfg.OnStatusChange,
		// The URI names this node on the requestor's fabric (OP:381).
		NodeID: func(fabricIndex uint8) (uint64, bool) {
			_, nodeID, ok := n.caseIDs.announceIdentity(fabricIndex)
			return nodeID, ok
		},
		Logger: n.logger,
		Clock:  n.cfg.Clock,
	})
	if err != nil {
		return fmt.Errorf("node: ota provider: %w", err)
	}
	n.otaProvider = p
	n.br.AttachBDXResponder(n.bdx)
	return nil
}

// OTAProvider returns the OTA Software Update Provider — nil unless
// [Config].OTAProvider names an image source. A host that assembles its
// own topology ([Config].Topology) mounts it on an endpoint of its own;
// with the facade's Aggregator the node mounts it.
func (n *Node) OTAProvider() *mattercore.OTAProvider { return n.otaProvider }

// placeOTAProvider turns the provider's assembled endpoint from a bridged
// one into a child of the root, the Aggregator's sibling: matter.js adds
// an OtaProviderEndpoint to the server node, not to the aggregator
// (ServerNode parts; the endpoint file src/endpoints/ota-provider.ts:57-64). Assembled as
// a component, it serves its device type alone on its Descriptor and no
// BridgedDeviceBasicInformation.
func placeOTAProvider(topo *endpoint.Topology) {
	for _, ep := range topo.Endpoints {
		if ep != nil && ep.SourceKey != nil && ep.SourceKey.String() == otaProviderKey.String() {
			ep.ParentEndpointID = 0
			ep.HasParentEndpointID = true
			ep.Part = true
			return
		}
	}
}

// isRootChild reports whether ep hangs off the root directly — the
// Aggregator's siblings, which are not in its PartsList.
func isRootChild(ep *endpoint.Endpoint) bool {
	return ep.HasParentEndpointID && ep.ParentEndpointID == 0
}

// otaProviderEndpointID returns the provider endpoint's number, false
// before the first assembly.
func (n *Node) otaProviderEndpointID() (uint16, bool) {
	topo := n.br.Topology()
	if topo == nil {
		return 0, false
	}
	for _, ep := range topo.Endpoints {
		if ep != nil && ep.SourceKey != nil && ep.SourceKey.String() == otaProviderKey.String() {
			return ep.ID, true
		}
	}
	return 0, false
}

// ensureOTAProviderACL lets every node of every fabric invoke the
// provider: an Operate entry for CASE, no subject, the provider's endpoint
// and cluster — added once per fabric whose ACL has none. matter.js adds
// it on the first fabric only (OP:116-158 #nodeOnline,
// fabricAuthority.fabrics[0] at :118), so a requestor on a second fabric of
// a multi-admin bridge could not query; this module grants it on each
// (BD-Matter-OTAProvider-ACLEveryFabric). The facade calls it at Start and
// after each CommissioningComplete.
func (n *Node) ensureOTAProviderACL(ctx context.Context) {
	if n.otaProvider == nil {
		return
	}
	epID, ok := n.otaProviderEndpointID()
	if !ok {
		return
	}
	fabrics, err := n.st.ListFabrics(ctx)
	if err != nil {
		return
	}
	for _, f := range fabrics {
		n.ensureOTAProviderACLOn(ctx, f.FabricIndex, epID)
	}
}

// ensureOTAProviderACLOn adds the provider's Operate/CASE entry to
// fabricIndex's ACL unless it is there.
func (n *Node) ensureOTAProviderACLOn(ctx context.Context, fabricIndex uint8, epID uint16) {
	acl, err := n.st.ListACL(ctx, fabricIndex)
	if err != nil {
		n.logger.Warn("ota.provider.acl_list", slog.Any("err", err))
		return
	}
	cluster := provdef.ClusterID
	for _, e := range acl {
		if e.Privilege == store.PrivilegeOperate && e.AuthMode == store.AuthModeCASE && len(e.Subjects) == 0 &&
			len(e.Targets) == 1 && e.Targets[0].Endpoint != nil && *e.Targets[0].Endpoint == epID &&
			e.Targets[0].Cluster != nil && *e.Targets[0].Cluster == cluster {
			return
		}
	}
	ep := epID
	entry := store.ACLEntry{
		FabricIndex: fabricIndex, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeCASE,
		Targets: []store.ACLTarget{{Endpoint: &ep, Cluster: &cluster}},
	}
	if err := n.st.ReplaceACL(ctx, fabricIndex, append(acl, entry)); err != nil {
		n.logger.Warn("ota.provider.acl_add", slog.Any("err", err))
		return
	}
	// The ACL changed: DataVersion and AccessControlEntryChanged, as
	// matter.js's setStateOf(AccessControlServer) reports it.
	n.root.AccessControl.NotifyAdminEntryInstalled(entry)
	n.logger.Info("ota.provider.acl_added", slog.Int("fabric_index", int(fabricIndex)), slog.Int("endpoint", int(epID)),
		slog.String("hint", "anyone in the fabric may access the OTA Software Update Provider cluster"))
}
