// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

// The security wiring between an inbound datagram and a served Interaction
// Model request — session lookup, MRP acknowledgement, subscriptions, PASE,
// CASE (responder and the re-establishment initiator) — ported from the
// reference daemon (examples/reference-bridge/wiring.go:398-940 at b4ea1f96^), which
// brief R migrates onto this package.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/mattercert"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// --- CASE operational identity ------------------------------------------

// caseFabric is one installed fabric's operational identity plus the
// verifier rooted at that fabric's trust anchor.
type caseFabric struct {
	identity      *sigma.Identity
	verifier      sigma.PeerVerifier
	rootPublicKey []byte
	// addNOCIPK is the operational IPK derived from AddNOC's IPKValue,
	// the identity's key until key set 0 is rotated.
	addNOCIPK [16]byte
	// ipks are the destination identifier's candidate keys: every
	// operational key of key set 0 (matter.js Fabric.destinationIdsFor,
	// Fabric.ts:399-403, `keySets.allKeysForId(0)`; chip
	// CASESession.cpp:962-990 tries every IPK epoch key) and addNOCIPK.
	// identity.IPK is the current one among them (ipkCandidates).
	ipks [][16]byte
}

// caseIdentities holds one [caseFabric] per installed fabric and resolves an
// inbound Sigma1 to the right one.
//
// A single-identity bridge works only until a second fabric is installed —
// which Apple Home does within seconds of a pair, and which then signs
// Sigma2 under the wrong fabric's NOC for every reconnect of the first.
type caseIdentities struct {
	// mu guards byIdx and latest.
	mu     sync.RWMutex
	byIdx  map[uint8]*caseFabric
	latest *caseFabric
	logger *slog.Logger
	// clock is the certificate verifiers' time; nil is clock.System.
	clock clock.Clock
	// groups holds key set 0, the IPK key set, per fabric; nil keeps the
	// AddNOC key alone.
	groups *groups.Manager
}

func newCaseIdentities(logger *slog.Logger, g *groups.Manager) *caseIdentities {
	return &caseIdentities{byIdx: make(map[uint8]*caseFabric), logger: logger, groups: g}
}

// ResolveSigma1Destination implements [sigma.IdentityResolver]: it recomputes
// each installed fabric's destinationID under every key of its key set 0
// and returns the one the initiator addressed, keyed on the key that
// matched. Mirrors matter.js FabricManager.findFabricFromDestinationId
// (FabricManager.ts:325-331) over Fabric.destinationIdsFor (Fabric.ts:399-403,
// every key of key set 0); the matched key then salts Sigma2 and Sigma3 as
// chip CASESession.cpp:973-990 caches it (matter.js keys the salts on the
// AddNOC IPK alone, CaseServer.ts:240-284 — `BD-Matter-CASE-SaltOnMatchedIPK`).
func (c *caseIdentities) ResolveSigma1Destination(destinationID [32]byte, initiatorRandom [sigma.RandomSize]byte) (*sigma.Identity, sigma.PeerVerifier, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, f := range c.byIdx {
		for _, ipk := range f.ipks {
			candidate := sigma.ComputeDestinationID(
				ipk, initiatorRandom, f.rootPublicKey, f.identity.FabricID, f.identity.NodeID,
			)
			if candidate != destinationID {
				continue
			}
			if ipk == f.identity.IPK {
				return f.identity, f.verifier, true
			}
			matched := *f.identity
			matched.IPK = ipk
			return &matched, f.verifier, true
		}
	}
	return nil, nil, false
}

// ipkCandidates returns the fabric's destination identifier keys and the
// current one: the keys of key set 0 from the group manager (empty when it
// holds none, as a store that predates AddNOC writing key set 0) and
// addNOCIPK, which matter.js seeds key set 0 with (FabricGroups.ts:35-50)
// and which stays a candidate so an initiator on the AddNOC key is
// answered whatever the group state. The current key is matter.js
// Fabric.currentDestinationIdFor's (Fabric.ts:391-393: the second-newest
// of three, else the only one), the AddNOC key without key set 0.
func (c *caseIdentities) ipkCandidates(ctx context.Context, fabricIndex uint8, addNOCIPK [16]byte) (current [16]byte, ipks [][16]byte) {
	current = addNOCIPK
	if c.groups == nil {
		return current, [][16]byte{addNOCIPK}
	}
	keys, err := c.groups.IPKKeys(ctx, fabricIndex)
	if err != nil {
		c.logger.Warn("case.identity.ipk_keys_failed", slog.Int("fabric_index", int(fabricIndex)), slog.String("err", err.Error()))
	}
	for _, k := range keys {
		if len(k) != sigma.OperationalIPKSize {
			continue
		}
		var ipk [16]byte
		copy(ipk[:], k)
		if !slices.Contains(ipks, ipk) {
			ipks = append(ipks, ipk)
		}
	}
	if cur, err := c.groups.CurrentIPK(ctx, fabricIndex); err == nil && len(cur) == sigma.OperationalIPKSize {
		copy(current[:], cur)
	}
	if !slices.Contains(ipks, addNOCIPK) {
		ipks = append(ipks, addNOCIPK)
	}
	return current, ipks
}

// refreshIPK re-reads an installed fabric's key set 0 after a KeySetWrite
// or KeySetRemove reloaded the fabric's group state (the group manager's
// OnGroupcastChanged): the identity a later Sigma1 resolves to keys on the
// rotated set. A responder in flight keeps the identity it resolved.
func (c *caseIdentities) refreshIPK(ctx context.Context, fabricIndex uint8) {
	c.mu.RLock()
	f, ok := c.byIdx[fabricIndex]
	c.mu.RUnlock()
	if !ok || f.identity == nil {
		return
	}
	current, ipks := c.ipkCandidates(ctx, fabricIndex, f.addNOCIPK)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byIdx[fabricIndex] != f {
		return
	}
	if current == f.identity.IPK && slices.Equal(ipks, f.ipks) {
		return
	}
	identity := *f.identity
	identity.IPK = current
	entry := *f
	entry.identity = &identity
	entry.ipks = ipks
	c.byIdx[fabricIndex] = &entry
	if c.latest == f {
		c.latest = &entry
	}
	c.logger.Info("case.identity.ipk_rotated", slog.Int("fabric_index", int(fabricIndex)), slog.Int("keys", len(ipks)))
}

// ResolveFabricIndex implements [sigma.FabricIndexResolver]: a resumed
// session answers as the fabric its resumption record names, not as the
// most recently installed one. Without it a controller resuming on fabric
// 1 after fabric 2 was installed got a session on fabric 2, and every
// access check failed (TC-ACL-2.10 after the reboot).
func (c *caseIdentities) ResolveFabricIndex(fabricIndex uint8) (*sigma.Identity, sigma.PeerVerifier, bool) { //nolint:unparam // sigma.FabricIndexResolver's signature
	c.mu.RLock()
	defer c.mu.RUnlock()
	f, ok := c.byIdx[fabricIndex]
	if !ok || f.identity == nil {
		return nil, nil, false
	}
	return f.identity, f.verifier, true
}

// forget drops a removed fabric's identity, so a Sigma1 addressed to it is
// no longer answered: a fabric whose commissioning was rolled back (or
// that RemoveFabric deleted) must not keep a CASE identity — the next
// commissioning of the same fabric ID would otherwise land its sessions
// on the stale index (TC-CGEN-2.4). Returns the identity's DNS-SD names
// so the caller can withdraw the record.
func (c *caseIdentities) forget(fabricIndex uint8) (compressedID [8]byte, nodeID uint64, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.byIdx[fabricIndex]
	if !ok || entry.identity == nil {
		return compressedID, 0, false
	}
	delete(c.byIdx, fabricIndex)
	if c.latest == entry {
		c.latest = nil
		for _, e := range c.byIdx {
			c.latest = e
		}
	}
	return entry.identity.CompressedFabricID, entry.identity.NodeID, true
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

// forFabric returns the loaded identity of one fabric.
func (c *caseIdentities) forFabric(fabricIndex uint8) (*caseFabric, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.byIdx[fabricIndex]
	return entry, ok && entry.identity != nil
}

// fabricIdentities is what loading a fabric's CASE identity reads.
type fabricIdentities interface {
	GetFabric(ctx context.Context, fabricIndex uint8) (store.FabricRecord, error)
	GetIdentity(ctx context.Context, fabricIndex uint8) (store.IdentityRecord, error)
}

// load rebuilds the identity for one fabric from its persisted rows. Called
// at boot for every already-installed fabric and again from
// OperationalCredentials' OnFabricInstalled hook after each AddNOC.
func (c *caseIdentities) load(ctx context.Context, st fabricIdentities, fabricIndex uint8) error {
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
	verifier, err := mattercert.NewVerifier(fabric.RootPublicKey, clock.Or(c.clock))
	if err != nil {
		return fmt.Errorf("peer verifier %d: %w", fabricIndex, err)
	}
	// The stored IPK is the raw AddNOC.IPKValue; the handshake keys on the
	// derived operational one — the current key of key set 0 once it is
	// rotated (ipkCandidates).
	addNOCIPK, err := sigma.DeriveOperationalIPK(identity.IPK, fabric.CompressedID)
	if err != nil {
		return fmt.Errorf("operational ipk %d: %w", fabricIndex, err)
	}
	opIPK, ipks := c.ipkCandidates(ctx, fabricIndex, addNOCIPK)

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
		addNOCIPK:     addNOCIPK,
		ipks:          ipks,
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
// are returned so Stop can stop the ones that own goroutines.
type security struct {
	sessions *operational.Manager
	subs     *subscription.Manager
	pase     *bridge.PerExchangePaseProvider
	casep    *bridge.PerExchangeCaseProvider
	// responderParams is what every PASE and CASE responder advertises.
	responderParams *sessionparams.Parameters
}

// responderSessionParams is the SessionParameters the node's PASE and
// CASE responders advertise (PBKDFParamResponse tag 5, Sigma2's
// responderSessionParams): bridge.DefaultSessionParameters with the TCP
// roles the node serves — tcpServer for incoming, tcpClient for outgoing
// — so a peer learns them during the handshake and may open TCP to us,
// and nothing when TCP is off. Mirrors matter.js ServerNetworkRuntime.ts:
// 355-363 (`SessionManager.sessionParameters = {…, supportedTransports:
// {tcpClient: outgoing, tcpServer: incoming}}` only with a TCP role) at
// 0ceb202d; the size bound is sessionparams.DefaultMaxTCPMessageSize
// (bridge.SessionParametersFor).
//
// maxPathsPerInvoke, when non-zero, replaces the default MaxPathsPerInvoke:
// the session parameters state the node's own (SessionManager
// sessionParameters carry basicInformation.maxPathsPerInvoke).
func responderSessionParams(tcp bridge.TCPConfig, maxPathsPerInvoke uint16) *sessionparams.Parameters {
	var transports uint16
	if tcp.Incoming {
		transports |= sessionparams.TransportTCPServer
	}
	if tcp.Outgoing {
		transports |= sessionparams.TransportTCPClient
	}
	p := bridge.SessionParametersFor(transports, 0)
	if maxPathsPerInvoke != 0 {
		p.MaxPathsPerInvoke = maxPathsPerInvoke
	}
	return &p
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
	br *bridge.Bridge,
	st store.Port,
	root *RootClusters,
	ids *caseIdentities,
	pase paseParams,
	responderParams *sessionparams.Parameters,
	logger *slog.Logger,
) *security {
	sessions := operational.NewManager(st)
	sessions.SetClock(br.Clock())

	// The session lookup is what turns a Header.SessionID on the wire into
	// decryption keys, a fabric index and a subject. The fabric and subject
	// resolvers are what the dispatcher's ACL gate reads; without them every
	// operational request is evaluated against fabric 0.
	lookup := bridge.NewOperationalSessionLookup(
		func(id uint16) (*channel.Session, bool) {
			entry, err := sessions.Get(id)
			if err != nil || entry == nil {
				return nil, false
			}
			return entry.Session, true
		},
	).WithTransportResolver(func(id uint16) (uint16, uint32, bool) {
		e, err := sessions.Get(id)
		if err != nil || e == nil {
			return 0, 0, false
		}
		tr, mx := e.PeerTransportParameters()
		return tr, mx, tr != 0 || mx != 0
	}).WithFabricResolver(func(id uint16) (uint8, bool) {
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
	}).WithPASEResolver(func(id uint16) (bool, bool) {
		entry, err := sessions.Get(id)
		if err != nil || entry == nil {
			return false, false
		}
		return entry.IsPASE(), true
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

	subs := subscription.NewManager(subscription.Config{Clock: br.Clock()}, br.SubscriptionReporter(), logger)
	subs.Start(ctx)
	subs.SetEventReporter(br.SubscriptionEventReporter())
	br.AttachSubscriptionManager(subs)
	// One close hook, two observers: the session's subscriptions fall
	// with it, and the bridge forgets its route — closing a TCP
	// connection whose last session it was (bridge.SessionClosed).
	sessions.SetOnSessionClose(func(id uint16) {
		subs.CloseSession(id)
		br.SessionClosed(id)
	})
	br.AttachSessionRegistry(sessions)
	sessions.StartReaper(ctx, operational.SessionIdleTimeout, time.Minute)

	// PASE, one adapter per exchange. The session id has to be allocated
	// before the adapter is built: it rides out in PBKDFParamResponse and
	// the commissioner echoes it on every later datagram, so a bridge that
	// announces one id and registers the session under another drops
	// everything the commissioner sends after Pake3.
	//nolint:contextcheck // the factory signature is fixed by the provider; the adapters it builds carry no ctx of their own
	paseProvider := bridge.NewPerExchangePaseProvider(func() *bridge.PaseAdapter {
		adapter, err := buildPaseAdapter(sessions, root, pase, responderParams)
		if err != nil {
			logger.Warn("pase.build_failed", slog.String("err", err.Error()))
			return nil
		}
		return adapter
	})
	paseProvider.SetClock(br.Clock())
	paseProvider.StartReaper(ctx, 30*time.Second, time.Minute)
	// Not attached here: it accepts PASE only while the node's own
	// commissioning window is open (commissioning.go), as matter.js sets
	// its PASE commissioner per window (DeviceCommissioner).

	// CASE, likewise per exchange. A single responder lands in `Finished`
	// after the first Sigma3 and rejects every later Sigma1.
	caseProvider := bridge.NewPerExchangeCaseProvider(func() *bridge.CaseAdapter { //nolint:contextcheck // the adapter outlives this call; its callbacks run per handshake
		adapter, err := buildCaseAdapter(sessions, ids, responderParams, logger)
		if err != nil {
			logger.Warn("case.build_failed", slog.String("err", err.Error()))
			return nil
		}
		return adapter
	})
	caseProvider.SetOnEvict(br.ForgetSigma1Replied)
	caseProvider.SetClock(br.Clock())
	caseProvider.StartReaper(ctx, 30*time.Second, time.Minute)
	br.AttachCaseHandlerProvider(caseProvider.Resolve)

	// Subscription persistence and re-establishment (docs/adr/0008). The
	// store keeps each CASE subscription across a restart; after one, the
	// bridge resolves the controller over mDNS, opens CASE to it as the
	// initiator, and resumes the subscription under its old id — Start
	// calls ReestablishFormerSubscriptions once the identities are loaded.
	// Skip these and every restart costs each controller its subscription
	// liveness timeout before it re-subscribes.
	br.AttachSubscriptionStore(st)
	br.AttachOperationalResolver(operationalResolver(logger, br.Clock()))
	br.AttachCaseInitiatorProvider(caseInitiatorProvider(st, sessions, ids, logger))

	return &security{sessions: sessions, subs: subs, pase: paseProvider, casep: caseProvider, responderParams: responderParams}
}

// caseInitiatorProvider prepares the CASE handshakes the bridge opens to
// re-establish former subscriptions: this node's identity on the peer's
// fabric, a reserved session id, and — when one is stored — the peer's
// resumption record, which matter.js CaseClient offers whenever it holds
// one. A completed handshake registers the session and stores the new
// resumption record, as matter.js does after every pair.
func caseInitiatorProvider(st store.Resumption, sessions *operational.Manager, ids *caseIdentities, logger *slog.Logger) bridge.CaseInitiatorProvider {
	return func(ctx context.Context, fabricIndex uint8, peerNodeID uint64) (*bridge.CaseInitiation, error) {
		fabric, ok := ids.forFabric(fabricIndex)
		if !ok {
			return nil, fmt.Errorf("node: no CASE identity loaded for fabric %d", fabricIndex)
		}
		sessionID, err := sessions.AllocateID()
		if err != nil {
			return nil, fmt.Errorf("allocate CASE session id: %w", err)
		}
		var resumption *sigma.ResumptionRecord
		if rec, rerr := st.GetResumptionByPeer(ctx, fabricIndex, peerNodeID); rerr == nil {
			resumption = &sigma.ResumptionRecord{
				SharedSecret: rec.SharedSecret,
				ResumptionID: rec.ResumptionID,
				FabricIndex:  fabricIndex,
				PeerNodeID:   peerNodeID,
				PeerCATs:     rec.CASEAuthTags,
			}
		}
		initiator, err := sigma.NewPeerInitiator(sigma.InitiatorConfig{
			Identity:      fabric.identity,
			Verifier:      fabric.verifier,
			SessionID:     sessionID,
			PeerNodeID:    peerNodeID,
			RootPublicKey: fabric.rootPublicKey,
			Resumption:    resumption,
		})
		if err != nil {
			sessions.ReleaseID(sessionID)
			return nil, err
		}
		localNodeID := fabric.identity.NodeID
		return &bridge.CaseInitiation{
			Initiator:          initiator,
			CompressedFabricID: fabric.identity.CompressedFabricID,
			OnEstablished: func(ctx context.Context, res sigma.InitiatorResult) error {
				entry, err := sessions.OpenFromSigmaAsInitiatorWithID(sessionID, fabricIndex, localNodeID, res.PeerNodeID, res.PeerSessionID, res.PeerCATs, res.Keys)
				if err != nil {
					return err
				}
				if p := res.PeerSessionParams; p != nil {
					entry.SetPeerMRPIntervals(p.SessionIdleInterval, p.SessionActiveInterval, uint32(p.SessionActiveThreshold))
					entry.SetPeerTransportParameters(p.EffectiveTransports(), p.EffectiveMaxTCPMessageSize())
				}
				if err := sessions.PersistResumption(ctx, fabricIndex, res.PeerNodeID, res.ResumptionID, res.SharedSecret, res.PeerCATs); err != nil {
					logger.Debug("case.initiator.resumption_persist", slog.String("err", err.Error()))
				}
				return nil
			},
			OnAbandoned: func() { sessions.ReleaseID(sessionID) },
		}, nil
	}
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
func buildPaseAdapter(sessions *operational.Manager, root *RootClusters, p paseParams, responderParams *sessionparams.Parameters) (*bridge.PaseAdapter, error) {
	if !setup.IsValidSetupPIN(p.passcode) {
		return nil, fmt.Errorf("node: passcode %d is invalid or trivially guessable", p.passcode)
	}
	vc, err := spake2.NewVerifierContext(p.passcode, p.salt, p.iterations)
	if err != nil {
		return nil, fmt.Errorf("spake2 verifier context: %w", err)
	}
	return buildPaseAdapterFromContext(sessions, root, vc, p.salt, p.iterations, responderParams)
}

// buildPaseAdapterFromContext is the passcode-independent half of
// buildPaseAdapter, shared with the enhanced commissioning window, whose
// verifier arrives precomputed from the commissioner (commissioning.go).
func buildPaseAdapterFromContext(sessions *operational.Manager, root *RootClusters, vc *spake2.VerifierContext, salt []byte, iterations int, responderParams *sessionparams.Parameters) (*bridge.PaseAdapter, error) {
	sessionID, err := sessions.AllocateID()
	if err != nil {
		return nil, fmt.Errorf("allocate PASE session id: %w", err)
	}
	adapter := bridge.NewPaseAdapterWithFactory(func(transcript []byte) *spake2.Verifier {
		return spake2.NewVerifier(vc, nil, nil, transcript)
	})
	adapter.SetPBKDFParams(uint32(iterations), salt, sessionID) //nolint:gosec // NewVerifierContext / the window's command validation bound the iteration count
	// Our session parameters, the TCP roles included (PaseServer.ts:151
	// responderSessionParams = sessions.sessionParameters); nil keeps the
	// adapter's defaults.
	if responderParams != nil {
		p := *responderParams
		adapter.SetResponderSessionParams(&p)
	}
	adapter.SetOnSessionEstablished(func(sharedSecret []byte, peerSessionID uint16) error {
		// PASE predates the fabric, so both node ids are zero.
		entry, err := sessions.OpenFromPaseWithID(sessionID, 0, 0, peerSessionID, sharedSecret)
		if err != nil {
			sessions.ReleaseID(sessionID)
			return err
		}
		// The commissioner's SessionParameters size the session's
		// retransmissions and name its transports — matter.js
		// PaseServer.ts:155-157 `session.timingParameters = initiatorSessionParams`.
		if p := adapter.PeerSessionParams(); p != nil {
			entry.SetPeerMRPIntervals(p.SessionIdleInterval, p.SessionActiveInterval, uint32(p.SessionActiveThreshold))
			entry.SetPeerTransportParameters(p.EffectiveTransports(), p.EffectiveMaxTCPMessageSize())
		}
		// AttestationRequest and CSRRequest signatures bind to the
		// session's challenge; without this the commissioner rejects both.
		root.OperationalCredentials.SetAttestationChallenge(entry.AttestationChallenge)
		// Matter §11.10.5.2 floor: arm a fail-safe if the commissioner has
		// not, so an abandoned attempt still rolls back.
		root.GeneralCommissioning.AutoArmOnPaseEstablished(context.Background())
		return nil
	})
	return adapter, nil
}

// buildCaseAdapter wires one CASE exchange: a responder seeded from the
// current operational identity, the multi-fabric destination resolver, and on
// success an operational session registered under whichever id the
// handshake ended up using.
func buildCaseAdapter(sessions *operational.Manager, ids *caseIdentities, responderParams *sessionparams.Parameters, logger *slog.Logger) (*bridge.CaseAdapter, error) {
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
	// Our session parameters ride in Sigma2 (CaseServer.ts:272
	// responderSessionParams: sessions.sessionParameters); nil leaves
	// NewCaseAdapter's defaults.
	if responderParams != nil {
		p := *responderParams
		responder.SetSessionParameters(&p)
	}
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

	// Session resumption as the responder (Matter §4.13.2.4): a Sigma1
	// presenting a resumption id this node issued resumes with
	// Sigma2_Resume instead of a full handshake. matter.js CaseServer looks
	// the record up in SessionManager and saves a fresh one after every
	// session (CaseServer.ts:210); TC-SC-3.2 checks the resume.
	responder.SetResumptionStore(resumptionLookup{sessions})

	adapter := bridge.NewCaseAdapter(responder)
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
		// The initiator's Sigma1 SessionParameters size the session's
		// retransmissions and name its transports (matter.js
		// CaseServer.ts:339 `sessionParameters: secureSession.parameters`).
		if resp := adapter.SnapshotResponder(); resp != nil {
			if p, ok := resp.PeerSessionParameters(); ok {
				entry.SetPeerMRPIntervals(p.SessionIdleInterval, p.SessionActiveInterval, uint32(p.SessionActiveThreshold))
				entry.SetPeerTransportParameters(p.EffectiveTransports(), p.EffectiveMaxTCPMessageSize())
			}
		}
		logger.Info("case.session_established",
			slog.Int("session_id", int(entry.SessionID)),
			slog.Int("fabric_index", int(fabricIndex)),
			slog.Uint64("peer_node_id", peerNodeID))
		// The record the peer resumes from next time: the id this
		// handshake issued (Sigma2's, or the fresh one Sigma2_Resume
		// carried) with the session's shared secret.
		if resp := adapter.SnapshotResponder(); resp != nil {
			if id, secret := resp.ResumptionID(), resp.ECDHSharedSecret(); len(id) == 16 && len(secret) > 0 {
				if err := sessions.PersistResumption(context.Background(), fabricIndex, peerNodeID, id, secret, peerCATs); err != nil {
					logger.Warn("case.resumption.persist_failed", slog.String("err", err.Error()))
				}
			}
		}
		return nil
	})
	return adapter, nil
}

// resumptionLookup serves the CASE responder's resumption records from the
// session manager's store.
type resumptionLookup struct{ sessions *operational.Manager }

// GetByID implements [sigma.ResumptionStore].
func (l resumptionLookup) GetByID(resumptionID []byte) (*sigma.ResumptionRecord, error) {
	rec, err := l.sessions.LookupResumption(context.Background(), resumptionID)
	if err != nil {
		return nil, err
	}
	return &sigma.ResumptionRecord{
		SharedSecret: rec.SharedSecret,
		ResumptionID: rec.ResumptionID,
		FabricIndex:  rec.FabricIndex,
		PeerNodeID:   rec.PeerNodeID,
		PeerCATs:     rec.CASEAuthTags,
	}, nil
}

// rejectingVerifier stands in before the first fabric exists. It fails every
// peer certificate with a message that names the actual cause, rather than
// letting a random ephemeral key surface as an opaque crypto error.
type rejectingVerifier struct{}

// VerifyAndExtractPubKey implements [sigma.PeerVerifier].
func (rejectingVerifier) VerifyAndExtractPubKey(_, _ []byte) (*ecdsa.PublicKey, error) {
	return nil, errors.New("node: no fabric installed yet: commission the bridge before opening a CASE session")
}

// operationalResolver is the mDNS resolver of the node's CASE initiator,
// its query retries on c.
func operationalResolver(logger *slog.Logger, c clock.Clock) *mdns.OperationalResolver {
	r := mdns.NewOperationalResolver(logger)
	r.SetClock(c)
	return r
}
