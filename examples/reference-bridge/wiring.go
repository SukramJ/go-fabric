// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	matterbridge "github.com/SukramJ/go-fabric/bridge"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/secure/attestation"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/mattercert"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// deviceTypeRootNode is the Matter RootNode device type (§10.3).
const deviceTypeRootNode uint32 = 0x0016

// deviceTypeAggregator is the Matter Aggregator device type (§13.2).
const deviceTypeAggregator uint32 = 0x000E

// deviceTypeRevision reads a device type's revision from the generated
// matter.js snapshot instead of restating it, so a schema regeneration
// propagates here without an edit. A device type absent from the snapshot is
// a programming error in this file, not a runtime condition.
func deviceTypeRevision(id uint32) uint16 {
	rev, ok := schema.DeviceTypeRevision(id)
	if !ok {
		panic(fmt.Sprintf("device type %#06x is not in the matter.js schema snapshot", id))
	}
	return rev
}

// rootRefs carries the root-endpoint cluster handles the rest of the wiring
// needs after construction. The cluster servers themselves go onto the
// bridge as an opaque slice; these are the ones with hooks left to wire.
type rootRefs struct {
	generalCom *mattercore.GeneralCommissioning
	opCreds    *mattercore.OperationalCredentials
}

// buildRootClusters constructs endpoint 0's cluster surface.
//
// Without these a commissioner's very first read (ReadCommissioningInfo)
// answers UnsupportedCluster and the pairing aborts before a fabric can be
// installed. The bridge builds none of them: it owns the wire format, the
// host owns its identity.
func buildRootClusters(
	identity bridgeIdentity,
	st *store.Store,
	chain *attestation.Chain,
	onFabricInstalled func(ctx context.Context, fabricIndex uint8, fabricID, nodeID uint64, rootPublicKey []byte),
) ([]contract.ClusterServer, rootRefs, error) {
	var refs rootRefs

	basicInfo, err := mattercore.NewBasicInformation(mattercore.Config{
		VendorName:         "go-fabric example",
		VendorID:           identity.vendorID,
		ProductName:        "reference-bridge",
		ProductID:          identity.productID,
		NodeLabel:          identity.nodeLabel,
		HardwareVersion:    1,
		HardwareVersionStr: "1",
		SoftwareVersion:    1,
		SoftwareVersionStr: "1",
		SerialNumber:       identity.serialNumber,
	})
	if err != nil {
		return nil, refs, fmt.Errorf("basic information: %w", err)
	}

	generalCom, err := mattercore.NewGeneralCommissioning(mattercore.GeneralCommissioningConfig{
		LocationCapability:           mattercore.RegulatoryIndoor,
		SupportsConcurrentConnection: true,
	})
	if err != nil {
		return nil, refs, fmt.Errorf("general commissioning: %w", err)
	}
	refs.generalCom = generalCom

	opCreds, err := mattercore.NewOperationalCredentials(st, mattercore.OpcredsConfig{
		SupportedFabrics: 5,
		// The CSA *test* attestation chain. It is what makes a bare
		// example pairable at all: chip-tool, Apple Home and Google Home
		// ship the matching test PAA in their trust stores, so no
		// vendor-supplied DAC is needed and no attestation bypass flag.
		// A shipped product replaces all four.
		DACPrivateKey: chain.DACKey,
		DAC:           chain.DAC,
		PAI:           chain.PAI,
		// Fires once AddNOC has persisted the fabric. The CASE identity
		// is rebuilt from the freshly written row here — a bridge that
		// skips this answers every post-commissioning Sigma1 with the
		// wrong identity, or none.
		OnFabricInstalled: onFabricInstalled,
		IsFailSafeArmed:   generalCom.FailSafeArmed,
	})
	if err != nil {
		return nil, refs, fmt.Errorf("operational credentials: %w", err)
	}
	refs.opCreds = opCreds

	// The fail-safe hooks close the loop between the two clusters: a fresh
	// commissioning attempt must not inherit pending NOC state from an
	// aborted one, and an expiring window must roll that state back.
	generalCom.SetOnFailSafeArmed(func(context.Context, uint8) { opCreds.ClearPendingState() })
	generalCom.SetOnFailSafeExpired(opCreds.OnFailSafeExpiry)

	accessControl, err := mattercore.NewAccessControl(st)
	if err != nil {
		return nil, refs, fmt.Errorf("access control: %w", err)
	}
	groupKeys, err := mattercore.NewGroupKeyManagement(st, mattercore.GroupKeyMgmtConfig{})
	if err != nil {
		return nil, refs, fmt.Errorf("group key management: %w", err)
	}

	// ServerList is derived from the mounted set rather than written out,
	// so adding a cluster below cannot leave the advertised list behind.
	descriptor, err := mattercore.NewDescriptor(
		[]mattercore.DeviceTypeStruct{
			{DeviceType: deviceTypeRootNode, Revision: deviceTypeRevision(deviceTypeRootNode)},
		},
		nil, // ServerList — provider wired below
		nil, // ClientList — this is not a controller
		nil, // PartsList — provider wired by the caller once the bridge is up
	)
	if err != nil {
		return nil, refs, fmt.Errorf("root descriptor: %w", err)
	}

	servers := []contract.ClusterServer{
		basicInfo,
		accessControl,
		generalCom,
		mattercore.NewNetworkCommissioning(mattercore.NetworkCommissioningConfig{}),
		mattercore.NewGeneralDiagnostics(mattercore.BootReasonPowerOnReboot),
		opCreds,
		groupKeys,
		descriptor,
	}
	descriptor.SetServerListProvider(clusterIDsOf(servers))
	return servers, refs, nil
}

// buildAggregatorClusters constructs endpoint 1, the Aggregator.
//
// The three-tier root → aggregator → bridged shape is not optional: a
// controller walks RootNode.PartsList to the Aggregator and the Aggregator's
// PartsList to the bridged devices. Collapsing the two onto endpoint 0
// leaves a bridge that pairs and then shows no accessories.
func buildAggregatorClusters() ([]contract.ClusterServer, error) {
	descriptor, err := mattercore.NewDescriptor(
		[]mattercore.DeviceTypeStruct{{DeviceType: deviceTypeAggregator, Revision: deviceTypeRevision(deviceTypeAggregator)}},
		nil, nil, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("aggregator descriptor: %w", err)
	}
	servers := []contract.ClusterServer{mattercore.NewIdentify(), descriptor}
	descriptor.SetServerListProvider(clusterIDsOf(servers))
	return servers, nil
}

// clusterIDsOf returns a provider over the cluster ids of the mounted set,
// deduplicated and in mount order.
func clusterIDsOf(servers []contract.ClusterServer) func() []uint32 {
	return func() []uint32 {
		ids := make([]uint32, 0, len(servers))
		seen := make(map[uint32]struct{}, len(servers))
		for _, srv := range servers {
			id := srv.MatterClusterID()
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		return ids
	}
}

// --- CASE operational identity ------------------------------------------

// caseFabric is one installed fabric's operational identity plus the
// verifier rooted at that fabric's trust anchor.
type caseFabric struct {
	identity      *sigma.Identity
	verifier      sigma.PeerVerifier
	rootPublicKey []byte
}

// caseIdentities holds one [caseFabric] per installed fabric and resolves an
// inbound Sigma1 to the right one.
//
// A single-identity bridge works only until a second fabric is installed —
// which Apple Home does within seconds of a pair, and which then signs
// Sigma2 under the wrong fabric's NOC for every reconnect of the first.
type caseIdentities struct {
	mu     sync.RWMutex
	byIdx  map[uint8]*caseFabric
	latest *caseFabric
	logger *slog.Logger
}

func newCaseIdentities(logger *slog.Logger) *caseIdentities {
	return &caseIdentities{byIdx: make(map[uint8]*caseFabric), logger: logger}
}

// ResolveSigma1Destination implements [sigma.IdentityResolver]: it recomputes
// each installed fabric's destinationID and returns the one the initiator
// addressed.
func (c *caseIdentities) ResolveSigma1Destination(destinationID [32]byte, initiatorRandom [sigma.RandomSize]byte) (*sigma.Identity, sigma.PeerVerifier, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, f := range c.byIdx {
		candidate := sigma.ComputeDestinationID(
			f.identity.IPK, initiatorRandom, f.rootPublicKey, f.identity.FabricID, f.identity.NodeID,
		)
		if candidate == destinationID {
			return f.identity, f.verifier, true
		}
	}
	return nil, nil, false
}

// current returns the most recently installed identity, or nil before the
// first AddNOC.
func (c *caseIdentities) current() *caseFabric {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest
}

// announceIdentity returns the DNS-SD identity of one loaded fabric: the
// compressed fabric ID and node ID that name its operational
// `<compressed>-<node>._matter._tcp` instance. Reporting it from the same
// entry the CASE responder answers with is the point — announcing an
// identity the responder does not hold advertises a bridge that cannot
// complete the Sigma1 it just invited.
func (c *caseIdentities) announceIdentity(fabricIndex uint8) (compressedID [8]byte, nodeID uint64, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.byIdx[fabricIndex]
	if !ok || entry.identity == nil {
		return compressedID, 0, false
	}
	return entry.identity.CompressedFabricID, entry.identity.NodeID, true
}

// load rebuilds the identity for one fabric from its persisted rows. Called
// at boot for every already-installed fabric and again from
// OperationalCredentials' OnFabricInstalled hook after each AddNOC.
func (c *caseIdentities) load(ctx context.Context, st *store.Store, fabricIndex uint8) error {
	fabric, err := st.GetFabric(ctx, fabricIndex)
	if err != nil {
		return fmt.Errorf("get fabric %d: %w", fabricIndex, err)
	}
	identity, err := st.GetIdentity(ctx, fabricIndex)
	if err != nil {
		return fmt.Errorf("get identity %d: %w", fabricIndex, err)
	}
	priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), identity.PrivateKey)
	if err != nil {
		return fmt.Errorf("parse operational key %d: %w", fabricIndex, err)
	}
	verifier, err := mattercert.NewVerifier(fabric.RootPublicKey, mattercert.SystemTime{})
	if err != nil {
		return fmt.Errorf("peer verifier %d: %w", fabricIndex, err)
	}
	// The stored IPK is the raw AddNOC.IPKValue; the handshake keys on the
	// derived operational one.
	opIPK, err := sigma.DeriveOperationalIPK(identity.IPK, fabric.CompressedID)
	if err != nil {
		return fmt.Errorf("operational ipk %d: %w", fabricIndex, err)
	}

	entry := &caseFabric{
		identity: &sigma.Identity{
			NOC:                identity.NOC,
			ICAC:               identity.ICAC,
			PrivateKey:         priv,
			NodeID:             fabric.NodeID,
			FabricID:           fabric.FabricID,
			CompressedFabricID: fabric.CompressedID,
			IPK:                opIPK,
			FabricIndex:        fabricIndex,
		},
		verifier:      verifier,
		rootPublicKey: append([]byte(nil), fabric.RootPublicKey...),
	}
	c.mu.Lock()
	c.byIdx[fabricIndex] = entry
	c.latest = entry
	c.mu.Unlock()
	c.logger.Info("case.identity.loaded",
		slog.Int("fabric_index", int(fabricIndex)),
		slog.Uint64("fabric_id", fabric.FabricID),
		slog.Uint64("node_id", fabric.NodeID))
	return nil
}

// --- security wiring ----------------------------------------------------

// security bundles the runtime pieces the bridge's receive path needs. They
// are returned so main can stop the ones that own goroutines.
type security struct {
	sessions *operational.Manager
	subs     *subscription.Manager
	pase     *matterbridge.PerExchangePaseProvider
	casep    *matterbridge.PerExchangeCaseProvider
}

// wireSecurity attaches everything between an inbound datagram and a served
// Interaction Model request: session lookup, MRP acknowledgement,
// subscriptions, PASE (commissioning) and CASE (post-commissioning).
//
// None of it is optional for a bridge meant to be commissioned and then
// read. The bridge accepts a missing piece silently — a nil PASE handler
// drops every PBKDFParamRequest at debug level and the commissioner just
// times out.
func wireSecurity(
	ctx context.Context,
	br *matterbridge.Bridge,
	st *store.Store,
	refs rootRefs,
	ids *caseIdentities,
	pase paseParams,
	logger *slog.Logger,
) *security {
	sessions := operational.NewManager(st)

	// The session lookup is what turns a Header.SessionID on the wire into
	// decryption keys, a fabric index and a subject. The fabric and subject
	// resolvers are what the dispatcher's ACL gate reads; without them every
	// operational request is evaluated against fabric 0.
	lookup := matterbridge.NewOperationalSessionLookup(
		func(id uint16) (*channel.Session, bool) {
			entry, err := sessions.Get(id)
			if err != nil || entry == nil {
				return nil, false
			}
			return entry.Session, true
		},
	).WithFabricResolver(func(id uint16) (uint8, bool) {
		entry, err := sessions.Get(id)
		if err != nil || entry == nil {
			return 0, false
		}
		return entry.FabricIndex(), true
	}).WithSubjectResolver(func(id uint16) (uint64, []uint32, bool) {
		entry, err := sessions.Get(id)
		if err != nil || entry == nil || entry.Session == nil {
			return 0, nil, false
		}
		return entry.Session.PeerNodeID(), entry.Session.PeerCATs(), true
	}).WithActivityMarkers(
		func(id uint16) {
			if e, err := sessions.Get(id); err == nil && e != nil {
				e.MarkActiveRx()
			}
		},
		func(id uint16) {
			if e, err := sessions.Get(id); err == nil && e != nil {
				e.MarkActiveTx()
			}
		},
	)
	br.AttachSessionLookup(lookup)
	br.AttachAckTracker(mrp.NewAckTracker(mrp.DefaultStandaloneAckDelay))

	subs := subscription.NewManager(subscription.Config{}, br.SubscriptionReporter(), logger)
	subs.Start(ctx)
	subs.SetEventReporter(br.SubscriptionEventReporter())
	br.AttachSubscriptionManager(subs)
	sessions.SetOnSessionClose(subs.CloseSession)
	br.AttachSessionRegistry(sessions)
	sessions.StartReaper(ctx, operational.SessionIdleTimeout, time.Minute)

	// PASE, one adapter per exchange. The session id has to be allocated
	// before the adapter is built: it rides out in PBKDFParamResponse and
	// the commissioner echoes it on every later datagram, so a bridge that
	// announces one id and registers the session under another drops
	// everything the commissioner sends after Pake3.
	//nolint:contextcheck // the factory signature is fixed by the provider; the adapters it builds carry no ctx of their own
	paseProvider := matterbridge.NewPerExchangePaseProvider(func() *matterbridge.PaseAdapter {
		adapter, err := buildPaseAdapter(sessions, refs, pase)
		if err != nil {
			logger.Warn("pase.build_failed", slog.String("err", err.Error()))
			return nil
		}
		return adapter
	})
	paseProvider.StartReaper(ctx, 30*time.Second, time.Minute)
	br.AttachPaseHandlerProvider(paseProvider.Resolve)

	// CASE, likewise per exchange. A single responder lands in `Finished`
	// after the first Sigma3 and rejects every later Sigma1.
	caseProvider := matterbridge.NewPerExchangeCaseProvider(func() *matterbridge.CaseAdapter {
		adapter, err := buildCaseAdapter(sessions, ids, logger)
		if err != nil {
			logger.Warn("case.build_failed", slog.String("err", err.Error()))
			return nil
		}
		return adapter
	})
	caseProvider.SetOnEvict(br.ForgetSigma1Replied)
	caseProvider.StartReaper(ctx, 30*time.Second, time.Minute)
	br.AttachCaseHandlerProvider(caseProvider.Resolve)

	return &security{sessions: sessions, subs: subs, pase: paseProvider, casep: caseProvider}
}

// paseParams is the commissioning secret in the shape the SPAKE2+ verifier
// needs it.
type paseParams struct {
	passcode   uint32
	salt       []byte
	iterations int
}

// buildPaseAdapter wires one PASE exchange end to end: PBKDF parameters out,
// SPAKE2+ verifier in, and on success an operational session registered under
// the pre-allocated id with the attestation challenge handed to
// OperationalCredentials.
func buildPaseAdapter(sessions *operational.Manager, refs rootRefs, p paseParams) (*matterbridge.PaseAdapter, error) {
	if !setup.IsValidSetupPIN(p.passcode) {
		return nil, fmt.Errorf("passcode %d is invalid or trivially guessable", p.passcode)
	}
	vc, err := spake2.NewVerifierContext(p.passcode, p.salt, p.iterations)
	if err != nil {
		return nil, fmt.Errorf("spake2 verifier context: %w", err)
	}
	sessionID, err := sessions.AllocateID()
	if err != nil {
		return nil, fmt.Errorf("allocate PASE session id: %w", err)
	}
	adapter := matterbridge.NewPaseAdapterWithFactory(func(transcript []byte) *spake2.Verifier {
		return spake2.NewVerifier(vc, nil, nil, transcript)
	})
	adapter.SetPBKDFParams(uint32(p.iterations), p.salt, sessionID) //nolint:gosec // NewVerifierContext above rejects any iteration count outside [IterationsMin, IterationsMax]
	adapter.SetOnSessionEstablished(func(sharedSecret []byte, peerSessionID uint16) error {
		// PASE predates the fabric, so both node ids are zero.
		entry, err := sessions.OpenFromPaseWithID(sessionID, 0, 0, peerSessionID, sharedSecret)
		if err != nil {
			sessions.ReleaseID(sessionID)
			return err
		}
		// AttestationRequest and CSRRequest signatures bind to the
		// session's challenge; without this the commissioner rejects both.
		refs.opCreds.SetAttestationChallenge(entry.AttestationChallenge)
		// Matter §11.10.5.2 floor: arm a fail-safe if the commissioner has
		// not, so an abandoned attempt still rolls back.
		refs.generalCom.AutoArmOnPaseEstablished(context.Background())
		return nil
	})
	return adapter, nil
}

// buildCaseAdapter wires one CASE exchange: a responder seeded from the
// current operational identity, the multi-fabric destination resolver, and on
// success an operational session registered under whichever id the
// handshake ended up using.
func buildCaseAdapter(sessions *operational.Manager, ids *caseIdentities, logger *slog.Logger) (*matterbridge.CaseAdapter, error) {
	current := ids.current()
	sessionID, err := sessions.AllocateID()
	if err != nil {
		return nil, fmt.Errorf("allocate CASE session id: %w", err)
	}

	identity := (*sigma.Identity)(nil)
	var verifier sigma.PeerVerifier
	if current != nil {
		identity, verifier = current.identity, current.verifier
	} else {
		// Sigma1 before any AddNOC: give the responder an ephemeral key so
		// the handshake fails as a signature mismatch rather than a nil
		// dereference. Nothing can succeed here — there is no fabric yet.
		ephemeral, genErr := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if genErr != nil {
			return nil, fmt.Errorf("ephemeral CASE key: %w", genErr)
		}
		identity = &sigma.Identity{PrivateKey: ephemeral}
		verifier = rejectingVerifier{}
	}

	responder := sigma.NewResponder(identity, verifier, sessionID)
	responder.SetIdentityResolver(ids)
	// A second Sigma1 on the same exchange — Apple grafts its iCloud
	// companion fabric onto one — must not reuse the first session's slot.
	responder.SetSessionIDRenewer(func(previous uint16) (uint16, bool) {
		next, renewErr := sessions.AllocateID()
		if renewErr != nil {
			return 0, false
		}
		sessions.ReleaseID(previous)
		return next, true
	})

	adapter := matterbridge.NewCaseAdapter(responder)
	adapter.SetOnSessionEstablished(func(keys sigma.SessionKeys, peerSessionID uint16) error {
		// Everything is read back off the responder rather than captured at
		// factory time: the destination resolver may have landed on a
		// different fabric than the one seeded above, and the session id may
		// have been renewed.
		fabricIndex := identity.FabricIndex
		localNodeID := identity.NodeID
		peerNodeID := uint64(0)
		effectiveID := sessionID
		var peerCATs []uint32
		if resp := adapter.SnapshotResponder(); resp != nil {
			peerNodeID = resp.PeerNodeID()
			peerCATs = resp.PeerCATs()
			effectiveID = resp.SessionID()
			if fi, nid, ok := resp.SessionIdentity(); ok {
				fabricIndex, localNodeID = fi, nid
			}
		}
		entry, openErr := sessions.OpenFromSigmaWithID(effectiveID, fabricIndex, localNodeID, peerNodeID, peerSessionID, peerCATs, keys)
		if openErr != nil {
			sessions.ReleaseID(effectiveID)
			return openErr
		}
		logger.Info("case.session_established",
			slog.Int("session_id", int(entry.SessionID)),
			slog.Int("fabric_index", int(fabricIndex)),
			slog.Uint64("peer_node_id", peerNodeID))
		return nil
	})
	return adapter, nil
}

// rejectingVerifier stands in before the first fabric exists. It fails every
// peer certificate with a message that names the actual cause, rather than
// letting a random ephemeral key surface as an opaque crypto error.
type rejectingVerifier struct{}

// VerifyAndExtractPubKey implements [sigma.PeerVerifier].
func (rejectingVerifier) VerifyAndExtractPubKey(_, _ []byte) (*ecdsa.PublicKey, error) {
	return nil, errors.New("no fabric installed yet: commission the bridge before opening a CASE session")
}
