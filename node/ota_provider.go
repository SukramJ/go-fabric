// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"thingfabric.dev/go-fabric/bdx"
	mattercore "thingfabric.dev/go-fabric/cluster/core"
	provdef "thingfabric.dev/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
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
	// Manager owns the requestors' update attempts and decides the
	// queries and applies the provider takes
	// (mattercore.OTAProviderConfig.Manager); nil is the provider's
	// default manager, which queues no update. [Node.OnOTAProviderStatus]
	// learns the statuses with either; with a Manager, RebootQuery,
	// TransferBlockSize and DelegateConsentToRequestor fail [New].
	Manager OTAUpdateManager
	// DelegateConsentToRequestor lets the default manager offer a
	// requestor that says it can consent an image that needs a consent
	// without asking Consent, with UserConsentNeeded
	// (mattercore.OTAProviderConfig.DelegateConsentToRequestor); off, as
	// matter.js's default, Consent is asked. With a Manager it fails
	// [New]: the manager decides the delegation.
	DelegateConsentToRequestor bool
	// ApplyDelay is how long a requestor allowed to apply an update waits
	// before it applies, in whole seconds rounded up and at most a day
	// (mattercore.OTAProviderConfig.ApplyDelay, matter.js
	// State.applyDelay); zero applies at once.
	ApplyDelay time.Duration
	// ApplyPolicy decides an apply before the manager's apply table; nil
	// leaves it to the table.
	//
	// Deprecated: the delay is ApplyDelay and the apply is the Manager's
	// to decide; removable in v0.26.0.
	ApplyPolicy OTAApplyPolicy
	// BDXMaxBlockSize, when set, caps the block size of the image
	// transfers.
	BDXMaxBlockSize uint16
	// RebootQuery learns a QueryImage taken for the requestor's restart
	// (mattercore.OTAProviderConfig.RebootQuery); nil ignores it.
	//
	// Deprecated: the restart is the Manager's to judge; removable in
	// v0.26.0.
	RebootQuery func(peer OTAPeer, sessionID uint16)
	// TransferBlockSize caps one peer's transfer block size in place of
	// BDXMaxBlockSize (mattercore.OTAProviderConfig.TransferBlockSize);
	// nil uses BDXMaxBlockSize.
	//
	// Deprecated: the Manager's BDXTransferOptionsFor names a peer's
	// transfer options; removable in v0.26.0.
	TransferBlockSize func(peer OTAPeer) uint16
	// GrantFabric decides, per fabric of the node, whether the provider
	// writes its ACL entry into that fabric's ACL: Operate for CASE, no
	// subject, the provider's endpoint and cluster — every node of the
	// fabric may then invoke the provider. nil writes it into no fabric.
	// matter.js writes it only into the first fabric its node's own
	// controller administers, by root certificate
	// (OtaSoftwareUpdateProviderServer.ts:116-158 at db021da0;
	// FabricAuthority.ts:112-114, :126-128), and a [Node] has no controller
	// of its own, so no fabric gets it unasked: the fabric's administrator
	// grants the access, or the host returns true here for the fabrics it
	// opens the provider to. The node asks at Start and after each
	// CommissioningComplete, on the calling goroutine.
	//
	// A grant only ever adds the entry. Withdrawing it — false for a fabric
	// granted before, or GrantFabric left nil — removes nothing: the entry
	// a granting start wrote, like the one a version before GrantFabric
	// wrote into every fabric's ACL, stays in that fabric's ACL, because the
	// node cannot tell an entry it wrote from one the fabric's administrator
	// wrote. The fabric's administrator removes it by writing the fabric's
	// ACL without it.
	GrantFabric func(fabric FabricInfo) bool
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
	if d := n.cfg.Limits.BDXAdditionalMrpDelay; d > 0 {
		n.bdx.SetAdditionalMrpDelay(d)
	}
	pcfg := mattercore.OTAProviderConfig{
		ImageSource: cfg.ImageSource, Consent: cfg.Consent, Manager: cfg.Manager, ApplyDelay: cfg.ApplyDelay,
		ApplyPolicy: cfg.ApplyPolicy, //nolint:staticcheck // the deprecated policy is honoured for its window
		BDX:         n.bdx, BDXMaxBlockSize: cfg.BDXMaxBlockSize,
		DelegateConsentToRequestor: cfg.DelegateConsentToRequestor,
		// The host's deprecated hooks go to the provider as given: beside a
		// Manager it refuses them rather than drop them.
		RebootQuery:       cfg.RebootQuery,       //nolint:staticcheck // honoured for the deprecation window
		TransferBlockSize: cfg.TransferBlockSize, //nolint:staticcheck // honoured for the deprecation window
		// The URI names this node on the requestor's fabric
		// (OtaSoftwareUpdateProviderServer.ts:300 at b25ed960).
		NodeID: func(fabricIndex uint8) (uint64, bool) {
			_, nodeID, ok := n.caseIDs.announceIdentity(fabricIndex)
			return nodeID, ok
		},
		Logger: n.logger,
		Clock:  n.cfg.Clock,
		// The provider reports the statuses to the node's listeners,
		// whichever manager runs.
		OnAttemptStatus: func(peer OTAPeer, status OTAUpdateStatus, version uint32) {
			n.otaStatusListeners.emit(n.logger, "ota_provider_status", otaProviderStatus{peer, status, version})
		},
	}
	if cfg.Manager == nil {
		// The default manager compares a transfer's session with a
		// query's by the peer's last activity on each (review II,
		// F-db021da0-27).
		pcfg.SessionActivity = n.peerActivity //nolint:staticcheck // the default manager's admission input, until the node has its own port
	}
	p, err := mattercore.NewOTAProvider(pcfg)
	if err != nil {
		return fmt.Errorf("node: ota provider: %w", err)
	}
	n.otaProvider = p
	n.br.AttachBDXResponder(n.bdx)
	return nil
}

// otaProviderStatus is one requestor's update state, as the provider
// reports it.
type otaProviderStatus struct {
	peer    OTAPeer
	status  OTAUpdateStatus
	version uint32
}

// OnOTAProviderStatus registers fn for every update state a requestor of
// the node's OTA provider ([Config].OTAProvider) reports: the peer, its
// status and the version it concerns — the statuses the provider reports
// to its manager, whichever manager runs ([OTAProviderConfig].Manager or
// the default; mattercore.OTAProviderConfig.OnAttemptStatus). fn runs inline on the provider's
// goroutine and must not block — a download's WaitForApply is reported on
// the receive path, when the requestor's final BlockAckEof arrives, so a
// blocking fn stalls that session's datagrams; [Node.OnOTAProviderStatusAsync]
// queues it instead. It returns the unsubscribe function (ADR 0025 principle 8).
func (n *Node) OnOTAProviderStatus(fn func(peer OTAPeer, status OTAUpdateStatus, version uint32)) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	return n.otaStatusListeners.add(func(s otaProviderStatus) { fn(s.peer, s.status, s.version) })
}

// OnOTAProviderStatusAsync is [Node.OnOTAProviderStatus] with fn queued on
// the node's callback goroutine, one call after the other, while the node
// runs; Stop returns once every queued call ran.
func (n *Node) OnOTAProviderStatusAsync(fn func(peer OTAPeer, status OTAUpdateStatus, version uint32)) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	return n.otaStatusListeners.add(func(s otaProviderStatus) {
		n.queued("ota_provider_status", func() { fn(s.peer, s.status, s.version) })
	})
}

// OTAProvider returns the OTA Software Update Provider — nil unless
// [Config].OTAProvider names an image source. A host that assembles its
// own topology ([Config].Snapshotter) mounts it on an endpoint of its own;
// with the facade's Aggregator the node mounts it.
func (n *Node) OTAProvider() *OTAProviderServer { return n.otaProvider }

// OTAProviderEndpoint is the provider endpoint's number, false without a
// provider or before Start's assembly.
func (n *Node) OTAProviderEndpoint() (uint16, bool) {
	if n.otaProvider == nil {
		return 0, false
	}
	return n.otaProviderEndpointID()
}

// BDX is the BDX responder the provider serves its images through — the
// transfers a controller's update manager reads the liveness of and
// closes (matter.js SoftwareUpdateManager.#bdxSessionFor,
// SoftwareUpdateManager.ts:322-328 at db021da0); nil without a provider.
func (n *Node) BDX() *BDXResponder { return n.bdx }

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

// ensureOTAProviderACL lets every node of a granted fabric invoke the
// provider: an Operate entry for CASE, no subject, the provider's endpoint
// and cluster — added once to each fabric [OTAProviderConfig].GrantFabric
// grants whose ACL has none, and to no fabric without GrantFabric.
// matter.js adds it to the first fabric its own controller administers
// (OtaSoftwareUpdateProviderServer.ts:116-158 at db021da0,
// fabricAuthority.fabrics[0]; FabricAuthority.ts:112-114, :126-128: the
// fabrics whose root certificate is its CA's), which a [Node] does not
// have. The facade calls it at Start and after each CommissioningComplete.
// It never removes an entry, from a fabric it no longer grants either
// (see GrantFabric).
func (n *Node) ensureOTAProviderACL(ctx context.Context) {
	if n.otaProvider == nil || n.cfg.OTAProvider == nil || n.cfg.OTAProvider.GrantFabric == nil {
		return
	}
	grant := n.cfg.OTAProvider.GrantFabric
	epID, ok := n.otaProviderEndpointID()
	if !ok {
		return
	}
	fabrics, err := n.st.ListFabrics(ctx)
	if err != nil {
		// No entry is written until the next Start or
		// CommissioningComplete: a granted requestor refused meanwhile
		// is refused for this (review II, R2-C2-err-2456).
		n.logger.Warn("ota.provider.acl_list_failed", slog.Any("err", err))
		return
	}
	for i := range fabrics {
		if grant(fabricInfoOf(fabrics[i])) {
			n.ensureOTAProviderACLOn(ctx, fabrics[i].FabricIndex, epID)
		}
	}
}

// ensureOTAProviderACLOn adds the provider's Operate/CASE entry to
// fabricIndex's ACL unless it is there, under the AccessControl server's
// write lock (addLocalOperateEntry).
func (n *Node) ensureOTAProviderACLOn(ctx context.Context, fabricIndex uint8, epID uint16) {
	if !n.addLocalOperateEntry(ctx, fabricIndex, epID, provdef.ClusterID, "ota.provider.acl") {
		return
	}
	// The ACL changed: the DataVersion moves, and no
	// AccessControlEntryChanged is emitted, because matter.js's
	// setStateOf(AccessControlServer) runs without a remote actor and
	// #handleAccessControlListChange returns before its events
	// (AccessControlServer.ts:284-302 at b25ed960).
	n.root.AccessControl.NotifyLocalEntryInstalled()
	n.logger.Info("ota.provider.acl_added", slog.Int("fabric_index", int(fabricIndex)), slog.Int("endpoint", int(epID)),
		slog.String("hint", "anyone in the fabric may access the OTA Software Update Provider cluster"))
}
