// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

// The security wiring between an inbound datagram and a served Interaction
// Model request — session lookup, MRP acknowledgement, subscriptions, PASE,
// CASE (responder and the re-establishment initiator) — ported from the
// reference daemon (examples/reference-bridge/wiring.go:398-940 at b4ea1f96^), which
// brief R migrates onto this package.

import (
	"bytes"
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
	"github.com/SukramJ/go-fabric/internal/sessionseam"
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
	// rootCert is the fabric's root certificate (nil on a legacy row).
	rootCert []byte
	// addNOCIPK is the operational IPK derived from AddNOC's IPKValue,
	// the identity's key until key set 0 is rotated.
	addNOCIPK [16]byte
	// ipks are the destination identifier's candidate keys: every
	// operational key of key set 0 (matter.js Fabric.destinationIdsFor,
	// Fabric.ts:399-403, `keySets.allKeysForId(0)`; chip
	// CASESession.cpp:962-990 tries every IPK epoch key) and addNOCIPK.
	// identity.IPK is the current one among them (ipkCandidates).
	ipks [][16]byte
	// gen is the load that installed the identity, one more for every
	// load ([caseIdentities.load]: AddNOC, UpdateNOC, a start); a key set
	// rotation keeps it. A handshake admits its session only under the
	// generation it began with (review II, R2-P6-sec-2412).
	gen uint64
}

// peerVerifier is the verifier rooted at a fabric's trust anchor: built
// from the root certificate when the fabric row holds it, so a root-signed
// NOC's authorityKeyIdentifier is checked against the root's
// subjectKeyIdentifier (matter.js Noc.ts:174-188 at db021da0); from the
// root's public key alone for a legacy row without it.
func peerVerifier(rcac, rootPublicKey []byte, ts mattercert.TimeSource) (*mattercert.Verifier, error) {
	if len(rcac) > 0 {
		return mattercert.NewVerifierFromRCAC(rcac, ts)
	}
	return mattercert.NewVerifier(rootPublicKey, ts)
}

// caseIdentities holds one [caseFabric] per installed fabric and resolves an
// inbound Sigma1 to the right one.
//
// A single-identity bridge works only until a second fabric is installed —
// which Apple Home does within seconds of a pair, and which then signs
// Sigma2 under the wrong fabric's NOC for every reconnect of the first.
type caseIdentities struct {
	// claims hands each resumption id to one resume at a time
	// ([resumeClaims]; owner decision 231).
	claims resumeClaims
	// testAfterStake, a test seam, runs in a responder's install right
	// after its stake was taken; nil outside tests.
	testAfterStake func()

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
	// loads counts the loads: the last one's [caseFabric.gen].
	loads uint64
	// resumptions is the store the handshakes' resumption records go to;
	// [caseIdentities.persistAdmitted] removes one again that an update
	// overtook. Nil keeps every record (tests that build no node).
	resumptions store.Resumption
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
	id, ver, _, ok := c.resolveSigma1Destination(destinationID, initiatorRandom)
	return id, ver, ok
}

// resolveSigma1Destination is ResolveSigma1Destination, with the load the
// identity comes from ([caseFabric.gen]).
func (c *caseIdentities) resolveSigma1Destination(destinationID [32]byte, initiatorRandom [sigma.RandomSize]byte) (*sigma.Identity, sigma.PeerVerifier, uint64, bool) {
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
				return f.identity, f.verifier, f.gen, true
			}
			matched := *f.identity
			matched.IPK = ipk
			return &matched, f.verifier, f.gen, true
		}
	}
	return nil, nil, 0, false
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
		c.logger.Warn("case.identity.ipk_keys_failed", slog.Int("fabric_index", int(fabricIndex)), slog.Any("err", err))
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
	id, ver, _, ok := c.resolveFabricIndex(fabricIndex)
	return id, ver, ok
}

// resolveFabricIndex is ResolveFabricIndex, with the load the identity
// comes from ([caseFabric.gen]).
func (c *caseIdentities) resolveFabricIndex(fabricIndex uint8) (*sigma.Identity, sigma.PeerVerifier, uint64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	f, ok := c.byIdx[fabricIndex]
	if !ok || f.identity == nil {
		return nil, nil, 0, false
	}
	return f.identity, f.verifier, f.gen, true
}

// handshakeResolver is one responder handshake's identity resolver: the
// node's ([caseIdentities]), recording on the handshake's stake which
// load of which fabric's identity the handshake answers under, so an
// UpdateNOC that replaces it before the handshake installs its session
// refuses the install ([handshakeStake.of]; review II, R2-P6-sec-2412).
type handshakeResolver struct {
	ids  *caseIdentities
	held *handshakeStake
}

var (
	_ sigma.IdentityResolver    = handshakeResolver{}
	_ sigma.FabricIndexResolver = handshakeResolver{}
)

// CommitResolution implements the sigma responder's optional commit: the
// Sigma1 the last resolution served was accepted, so the handshake answers
// under that resolution now. A resolution for a Sigma1 the responder
// refused after resolving it is never committed (review II, decision 244).
func (r handshakeResolver) CommitResolution() { r.held.commit() }

func (r handshakeResolver) ResolveSigma1Destination(destinationID [32]byte, initiatorRandom [sigma.RandomSize]byte) (*sigma.Identity, sigma.PeerVerifier, bool) {
	id, ver, gen, ok := r.ids.resolveSigma1Destination(destinationID, initiatorRandom)
	if ok {
		r.held.resolved(id.FabricIndex, gen)
	}
	return id, ver, ok
}

func (r handshakeResolver) ResolveFabricIndex(fabricIndex uint8) (*sigma.Identity, sigma.PeerVerifier, bool) {
	id, ver, gen, ok := r.ids.resolveFabricIndex(fabricIndex)
	if ok {
		r.held.resolved(fabricIndex, gen)
	}
	return id, ver, ok
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

// errCASEFabricRemoved refuses the session of a CASE handshake whose
// fabric was removed while it ran.
var errCASEFabricRemoved = errors.New("node: the CASE session's fabric was removed during its handshake")

// errCASEFabricUpdated refuses a CASE session whose fabric's identity an
// UpdateNOC replaced during its handshake ([caseIdentities.admitSessionAt]).
var errCASEFabricUpdated = errors.New("node: the CASE session's fabric was updated during its handshake")

// admitSession opens the session of a completed CASE handshake on
// fabricIndex with open — unless the fabric was removed while the
// handshake ran. The responder keeps the identity it resolved at Sigma1,
// and the initiator the one it started with; a removal that ran between
// Sigma1 and the handshake's end swept the fabric's sessions before this
// one existed, which would then live on the removed index with nothing to
// end it (review II, R2-P2-sess-05, R2-P2-fabric-09). Ahead of matter.js
// (BD-Matter-CASE-RemovedFabricRefusedAtCompletion): its CaseServer
// resolves the fabric while it answers Sigma1
// (packages/protocol/src/session/case/CaseServer.ts:239 at db021da0),
// where a fabric that is being deleted is refused
// (packages/protocol/src/fabric/FabricManager.ts:331-333), and creates
// the session with that fabric once Sigma3 is verified (:316-328);
// CaseServer.ts itself does not look at the fabric's state again in
// between.
//
// The identity is checked before and after the open. The removal forgets
// the identity first and closes the fabric's sessions afterwards
// (node.fabricRemoved: forget, then the bridge's EmitFabricRemovedContext),
// so a removal that lands between the two checks either finds this session
// open and closes it, or leaves the second check without the identity, and
// the session is closed here — the session open returned, only while it
// still holds its id.
func (c *caseIdentities) admitSession(sessions *operational.Manager, fabricIndex uint8, open func() (*operational.Entry, error)) (*operational.Entry, error) {
	return c.admitSessionAt(sessions, fabricIndex, 0, open)
}

// admitSessionAt is [caseIdentities.admitSession] for a handshake that
// began under the identity of load gen (0: any): an UpdateNOC that
// replaced the identity in between — a load since — refuses the session
// as a removal does, at both checks, so no session or resumption record
// made under the old NOC outlives the update's sweep. connectedhomeip
// aborts a handshake pending on a fabric its update touches
// (src/protocols/secure_channel/CASESession.h:149-153 OnFabricUpdated →
// InvalidateIfPendingEstablishmentOnFabric, CASESession.cpp:423-434 at
// 6170af84; review II, R2-P6-sec-2412).
func (c *caseIdentities) admitSessionAt(sessions *operational.Manager, fabricIndex uint8, gen uint64, open func() (*operational.Entry, error)) (*operational.Entry, error) {
	if err := c.admissible(fabricIndex, gen); err != nil {
		return nil, err
	}
	entry, err := open()
	if err != nil {
		return nil, err
	}
	if err := c.admissible(fabricIndex, gen); err != nil {
		// The entry itself, not its id: by now the id may name another
		// session or another handshake's stake (decision 159).
		sessionseam.CloseEntry(sessions, entry)
		return nil, err
	}
	return entry, nil
}

// persistAdmitted stores rec, the resumption record of a session admitted
// under load gen (0: any) of its fabric's identity, and checks the
// admission once more after the store: an UpdateNOC whose identity load
// and RemoveResumptionsByFabric both ran between the admit and the store
// would leave a record made under the replaced NOC (facade residual 3b of
// R2-P6-sec-2412), which [caseIdentities.dropIfReplaced] removes again.
// matter.js deletes a fabric's records when it replaces the fabric
// (packages/protocol/src/common/FailsafeContext.ts:167-170 at db021da0);
// the update's own sweep (node.fabricUpdated) closes the session. The
// error is the store's.
func (c *caseIdentities) persistAdmitted(ctx context.Context, sessions *operational.Manager, gen uint64, rec store.ResumptionRecord) error {
	if err := sessions.PersistResumptionRecord(ctx, rec); err != nil {
		return err
	}
	c.dropIfReplaced(ctx, gen, rec)
	return nil
}

// dropIfReplaced removes rec from the store when its handshake's identity
// (load gen of rec's fabric) is no longer admissible — only while the
// peer's record is still rec: a later handshake's record stays.
func (c *caseIdentities) dropIfReplaced(ctx context.Context, gen uint64, rec store.ResumptionRecord) {
	if c.resumptions == nil || c.admissible(rec.FabricIndex, gen) == nil {
		return
	}
	cur, err := c.resumptions.GetResumptionByPeer(ctx, rec.FabricIndex, rec.PeerNodeID)
	if err != nil || !bytes.Equal(cur.ResumptionID, rec.ResumptionID) {
		return
	}
	if err := c.resumptions.RemoveResumption(ctx, rec.FabricIndex, rec.PeerNodeID); err != nil {
		c.logger.Warn("case.resumption.remove_failed", slog.Int("fabric_index", int(rec.FabricIndex)), slog.Any("err", err))
	}
}

// admissible reports why a handshake that began under load gen (0: any)
// of fabricIndex's identity may not admit its session: the fabric's
// identity is gone, or another load replaced it.
func (c *caseIdentities) admissible(fabricIndex uint8, gen uint64) error {
	f, ok := c.forFabric(fabricIndex)
	switch {
	case !ok:
		return fmt.Errorf("%w: fabric %d", errCASEFabricRemoved, fabricIndex)
	case gen != 0 && f.gen != gen:
		return fmt.Errorf("%w: fabric %d", errCASEFabricUpdated, fabricIndex)
	}
	return nil
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
	verifier, err := peerVerifier(fabric.RootCert, fabric.RootPublicKey, clock.Or(c.clock))
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
		rootCert:      append([]byte(nil), fabric.RootCert...),
		addNOCIPK:     addNOCIPK,
		ipks:          ipks,
	}
	c.mu.Lock()
	c.loads++
	entry.gen = c.loads
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
	// lookup is the session lookup the bridge reads every session through.
	lookup *bridge.OperationalSessionLookup
	subs   *subscription.Manager
	pase   *bridge.PerExchangePaseProvider
	salts  *windowSalt
	casep  *bridge.PerExchangeCaseProvider
	// responderParams is what every PASE and CASE responder advertises.
	responderParams *sessionparams.Parameters
}

// responderSessionParams is the SessionParameters the node's PASE and
// CASE responders advertise (PBKDFParamResponse tag 5, Sigma2's
// responderSessionParams): bridge.DefaultSessionParameters with the TCP
// roles the node serves — tcpServer for incoming, tcpClient for outgoing
// — so a peer learns them during the handshake and may open TCP to us,
// and a bitmap of 0 when TCP is off. Mirrors matter.js
// packages/node/src/behavior/system/network/ServerNetworkRuntime.ts:204-210
// at db021da0 (`sessions.sessionParameters = {supportedTransports:
// {tcpClient: tcpConfig.outgoing, tcpServer: tcpConfig.incoming}, …}`
// whatever the roles; review II, R2-P6-par-2905); the size bound is
// sessionparams.DefaultMaxTCPMessageSize (bridge.SessionParametersFor).
//
// maxPathsPerInvoke, when non-zero, replaces the default MaxPathsPerInvoke:
// the session parameters state the node's own (SessionManager
// sessionParameters carry basicInformation.maxPathsPerInvoke).
//
// intervals are the node's MRP intervals ([Config.Sessions]); nil is the
// defaults (ServerNetworkRuntime.ts:359 spreads sessions.intervals).
func responderSessionParams(tcp bridge.TCPConfig, maxPathsPerInvoke uint16, intervals *SessionIntervals) *sessionparams.Parameters {
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
	intervals.apply(&p)
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
	br *bridge.Bridge,
	st store.Port,
	root *RootClusters,
	ids *caseIdentities,
	pase paseParams,
	responderParams *sessionparams.Parameters,
	limits Limits,
	logger *slog.Logger,
	events *sessionEvents,
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
	}).WithPeerScheduleResolver(peerScheduleOf(sessions)).
		WithRetransmitIntervalResolver(retransmitIntervalOf(sessions)).WithFabricResolver(func(id uint16) (uint8, bool) {
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

	// The attach installs the bridge's report sender: every report of
	// the manager goes out through it (review II, R2-P6-api-0508).
	subs := subscription.NewManager(limits.subscriptionConfig(br.Clock()), nil, logger)
	br.AttachSubscriptionManager(subs)
	// One close hook, two observers: the session's subscriptions fall
	// with it, and the bridge forgets its route — closing a TCP
	// connection whose last session it was (bridge.SessionClosed).
	sessions.SetOnSessionClose(func(id uint16) {
		subs.CloseSession(id)
		br.SessionClosed(id)
		events.closed(id)
	})
	subs.SetOnSessionSubscriptionsChanged(events.subscriptionsChanged)
	br.AttachSessionRegistry(sessions)

	// PASE, one adapter per exchange. The session id has to be allocated
	// before the adapter is built: it rides out in PBKDFParamResponse and
	// the commissioner echoes it on every later datagram, so a bridge that
	// announces one id and registers the session under another drops
	// everything the commissioner sends after Pake3.
	//nolint:contextcheck // the factory signature is fixed by the provider; the adapters it builds carry no ctx of their own
	paseProvider := bridge.NewPerExchangePaseProvider(func() *bridge.PaseAdapter {
		adapter, err := buildPaseAdapter(sessions, root, pase, responderParams)
		if err != nil {
			logger.Warn("pase.build_failed", slog.Any("err", err))
			return nil
		}
		return adapter
	})
	paseProvider.SetClock(br.Clock())
	// Not attached here: it accepts PASE only while the node's own
	// commissioning window is open (commissioning.go), as matter.js sets
	// its PASE commissioner per window (DeviceCommissioner).

	// CASE, likewise per exchange. A single responder lands in `Finished`
	// after the first Sigma3 and rejects every later Sigma1.
	caseProvider := bridge.NewPerExchangeCaseProvider(func() *bridge.CaseAdapter { //nolint:contextcheck // the adapter outlives this call; its callbacks run per handshake
		adapter, err := buildCaseAdapter(sessions, ids, responderParams, logger, events.opened)
		if err != nil {
			logger.Warn("case.build_failed", slog.Any("err", err))
			return nil
		}
		return adapter
	})
	caseProvider.SetOnEvict(br.ForgetSigma1Replied)
	caseProvider.SetClock(br.Clock())
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
	br.AttachCaseInitiatorProvider(caseInitiatorProvider(st, sessions, ids, logger, events.opened))

	return &security{sessions: sessions, lookup: lookup, subs: subs, pase: paseProvider, salts: pase.salts, casep: caseProvider, responderParams: responderParams}
}

// start runs the subscription engine and the session, PASE and CASE
// reapers until ctx — the node's lifetime — ends or [Node.Stop] stops
// them. [Node.Start] calls it, so [New] starts none of them (ADR 0026
// principle 5).
func (s *security) start(ctx context.Context) {
	s.subs.Start(ctx)
	s.sessions.StartReaper(ctx, operational.SessionIdleTimeout, time.Minute)
	s.pase.StartReaper(ctx, handshakeReapInterval, handshakeAdapterTTL)
	s.casep.StartReaper(ctx, handshakeReapInterval, handshakeAdapterTTL)
}

// caseInitiatorProvider prepares the CASE handshakes the bridge opens to
// re-establish former subscriptions: this node's identity on the peer's
// fabric, a reserved session id, and — when one is stored — the peer's
// resumption record, which matter.js CaseClient offers whenever it holds
// one. A completed handshake registers the session and stores the new
// resumption record, as matter.js does after every pair.
//
// onOpened, when set, learns the session id of every session a handshake
// opened (the node's OnSessionOpened).
func caseInitiatorProvider(st store.Resumption, sessions *operational.Manager, ids *caseIdentities, logger *slog.Logger, onOpened func(sessionID uint16)) bridge.CaseInitiatorProvider {
	return func(ctx context.Context, fabricIndex uint8, peerNodeID uint64) (*bridge.CaseInitiation, error) {
		fabric, ok := ids.forFabric(fabricIndex)
		if !ok {
			return nil, fmt.Errorf("node: no CASE identity loaded for fabric %d", fabricIndex)
		}
		// The handshake runs under this identity; an UpdateNOC that
		// replaces it before the handshake ends refuses the session
		// (admitSessionAt).
		gen := fabric.gen
		stake, err := sessions.AllocateStake()
		if err != nil {
			return nil, fmt.Errorf("allocate CASE session id: %w", err)
		}
		sessionID := stake.ID
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
			sessions.ReleaseStake(stake)
			return nil, err
		}
		localNodeID := fabric.identity.NodeID
		return &bridge.CaseInitiation{
			Initiator:          initiator,
			CompressedFabricID: fabric.identity.CompressedFabricID,
			OnEstablished: func(ctx context.Context, res sigma.InitiatorResult) error {
				entry, err := ids.admitSessionAt(sessions, fabricIndex, gen, func() (*operational.Entry, error) {
					// The install takes the stake, not the bare id: a stake an
					// allocation at a full table took over installs nothing
					// (operational.ErrStakeLost, N3-findev-1).
					return sessions.OpenFromSigmaAsInitiatorWithStake(stake, fabricIndex, localNodeID, res.PeerNodeID, res.PeerSessionID, res.PeerCATs, res.Keys)
				})
				if err != nil {
					return err
				}
				if p := res.PeerSessionParams; p != nil {
					entry.SetPeerMRPIntervals(p.SessionIdleInterval, p.SessionActiveInterval, uint32(p.SessionActiveThreshold))
					entry.SetPeerTransportParameters(p.EffectiveTransports(), p.EffectiveMaxTCPMessageSize())
				}
				if onOpened != nil {
					onOpened(entry.SessionID)
				}
				// This handshake is an initiator's, yet the record is
				// saved without the role (Initiator false) on purpose: the
				// node reaches it only to re-establish former subscriptions
				// (bridge.reestablishPeer), whose targets are peers that
				// connected to the node, so the record counts against the
				// responder's cap of 16 per fabric, as theirs would. A
				// host's call of the deprecated Bridge.ConnectPeer or
				// Bridge.ConnectOtaProvider reaches it too and is counted
				// the same way. Setting Initiator
				// would move it under the client's cap of 1024, which any
				// subscriber could then fill (review II, Fable read of
				// R2-P7-sec-01; BD-Matter-ResumptionRecordsPerFabric).
				rec := store.ResumptionRecord{
					FabricIndex: fabricIndex, PeerNodeID: res.PeerNodeID, ResumptionID: res.ResumptionID,
					SharedSecret: res.SharedSecret, CASEAuthTags: res.PeerCATs,
				}
				if p := res.PeerSessionParams; p != nil {
					rec.SessionParameters = *p
				}
				if err := ids.persistAdmitted(ctx, sessions, gen, rec); err != nil {
					logger.Debug("case.initiator.resumption_persist", slog.Any("err", err))
				}
				return nil
			},
			OnAbandoned: func() { sessions.ReleaseStake(stake) },
		}, nil
	}
}

// retransmitIntervalOf answers the base interval of a (re)transmission on
// a session from its entry: the interval the peer advertised for its
// state now, active or idle, the spec default where it advertised none
// ([operational.Entry.RetransmitBaseInterval]). Without it the bridge's
// tracker sent every secure-session retransmission on the 500 ms idle
// default whatever the peer advertised. Mirrors matter.js
// packages/protocol/src/protocol/MessageChannel.ts:268-275 at db021da0
// (every transmission takes the peer's session parameters and
// isPeerActive) into packages/protocol/src/protocol/MRP.ts:162-190 (the
// base `isPeerActive ? activeInterval : idleInterval`, :181).
func retransmitIntervalOf(sessions *operational.Manager) func(uint16, time.Time) (time.Duration, bool) {
	return func(id uint16, now time.Time) (time.Duration, bool) {
		e, err := sessions.Get(id)
		if err != nil || e == nil {
			return 0, false
		}
		return e.RetransmitBaseInterval(now), true
	}
}

// peerScheduleOf answers a session's peer MRP schedule from its entry
// ([operational.Entry.PeerMRPSchedule]; review II, F-db021da0-13).
func peerScheduleOf(sessions *operational.Manager) func(uint16) (time.Duration, time.Duration, time.Duration, bool, bool) {
	return func(id uint16) (time.Duration, time.Duration, time.Duration, bool, bool) {
		e, err := sessions.Get(id)
		if err != nil || e == nil {
			return 0, 0, 0, false, false
		}
		idle, active, threshold, peerActive := e.PeerMRPSchedule()
		return idle, active, threshold, peerActive, true
	}
}

// paseParams is the commissioning secret in the shape the SPAKE2+ verifier
// needs it.
type paseParams struct {
	passcode   uint32
	iterations int
	// salts is the salt of the basic window open now ([windowSalt]).
	salts *windowSalt
	// life is the node's lifetime: the fail-safe a PASE session arms
	// lives under it, so Stop ends its watcher.
	life context.Context
}

// buildPaseAdapter wires one PASE exchange end to end: PBKDF parameters out,
// SPAKE2+ verifier in, and on success an operational session registered under
// the pre-allocated id with the attestation challenge handed to
// OperationalCredentials.
func buildPaseAdapter(sessions *operational.Manager, root *RootClusters, p paseParams, responderParams *sessionparams.Parameters) (*bridge.PaseAdapter, error) {
	if !setup.IsValidSetupPIN(p.passcode) {
		return nil, errSetupPIN
	}
	salt, vc, err := p.salts.current()
	if err != nil {
		return nil, err
	}
	return buildPaseAdapterFromContext(p.life, sessions, root, vc, salt, p.iterations, responderParams)
}

// windowSalt is the PBKDF salt of the node's basic commissioning window
// — its own window and an administrator's basic window, both answered
// with the configured passcode — with the verifier context built from it.
// A fresh 32-byte salt is drawn per window open, as matter.js draws one
// for the PASE server of every window it opens
// (packages/protocol/src/protocol/DeviceCommissioner.ts:129-136, :214 at
// db021da0); nothing stores it, a commissioner learns it from the
// PBKDFParamResponse of the window it uses (review II, decision 251).
// connectedhomeip answers every basic window with the provisioned salt
// (src/app/server/CommissioningWindowManager.cpp:300-303 at 6170af84);
// the node follows matter.js.
type windowSalt struct {
	passcode   uint32
	iterations int

	mu       sync.Mutex
	openedAt time.Time
	salt     []byte
	vc       *spake2.VerifierContext
	// ended: the window the salt was drawn for is no longer open.
	ended bool
}

// renew draws the salt of the window opened at openedAt, unless it was
// drawn for that open already: the same open instant and no close since.
func (w *windowSalt) renew(openedAt time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.salt != nil && !w.ended && w.openedAt.Equal(openedAt) {
		return nil
	}
	return w.drawLocked(openedAt)
}

// end marks the window the salt was drawn for as no longer open, so the
// next open draws a fresh one even at the same instant.
func (w *windowSalt) end() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.ended = true
	w.mu.Unlock()
}

// current is the salt of the window open now and its verifier context;
// the first call before any window drew one draws it.
func (w *windowSalt) current() ([]byte, *spake2.VerifierContext, error) {
	if w == nil {
		return nil, nil, errors.New("node: no PBKDF salt source")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.salt == nil {
		if err := w.drawLocked(time.Time{}); err != nil {
			return nil, nil, err
		}
	}
	if w.vc == nil {
		// Derived on the first exchange of the window, not in the window's
		// transition, which keeps the transition hook short. The window
		// reads as open before the hook has run (the hooks fire after the
		// window's lock is released), and the hook renews the salt before
		// it attaches the PASE acceptor (renewSalt before attachPase): a
		// PBKDFParamRequest that arrives in between finds no acceptor and
		// is consumed by the fail-closed noop. A derivation in the hook
		// widened that gap; it is older than decision 251 and open in the
		// findings register.
		vc, err := spake2.NewVerifierContext(w.passcode, w.salt, w.iterations)
		if err != nil {
			return nil, nil, fmt.Errorf("spake2 verifier context: %w", err)
		}
		w.vc = vc
	}
	return w.salt, w.vc, nil
}

func (w *windowSalt) drawLocked(openedAt time.Time) error {
	salt, err := bridge.RandomSalt()
	if err != nil {
		return err
	}
	w.openedAt, w.salt, w.vc, w.ended = openedAt, salt, nil, false
	return nil
}

// buildPaseAdapterFromContext is the passcode-independent half of
// buildPaseAdapter, shared with the enhanced commissioning window, whose
// verifier arrives precomputed from the commissioner (commissioning.go).
//
// life is the node's lifetime, under which the PASE auto-armed fail-safe's
// expiry watcher runs (cluster/core GeneralCommissioning
// watchFailSafeExpiry): [Node.Stop] cancels it, so a stopped node leaves
// no watcher behind (W5-ex-06).
func buildPaseAdapterFromContext(life context.Context, sessions *operational.Manager, root *RootClusters, vc *spake2.VerifierContext, salt []byte, iterations int, responderParams *sessionparams.Parameters) (*bridge.PaseAdapter, error) {
	stake, err := sessions.AllocateStake()
	if err != nil {
		return nil, fmt.Errorf("allocate PASE session id: %w", err)
	}
	sessionID := stake.ID
	adapter := bridge.NewPaseAdapterWithFactory(func(transcript []byte) *spake2.Verifier {
		return spake2.NewVerifier(vc, nil, nil, transcript)
	})
	adapter.SetPBKDFParams(uint32(iterations), salt, sessionID) //nolint:gosec // NewVerifierContext / the window's command validation bound the iteration count
	// The staked id goes back when the handshake can no longer finish
	// (review II, R2-P3-lan-dev-02) — this handshake's stake only, which
	// leaves a live session and another handshake's stake alone
	// (R2-P3-dr-dev-07).
	adapter.SetOnUnfinished(func(uint16) { sessions.ReleaseStake(stake) })
	// Our session parameters, the TCP roles included (PaseServer.ts:151
	// responderSessionParams = sessions.sessionParameters); nil keeps the
	// adapter's defaults.
	if responderParams != nil {
		p := *responderParams
		adapter.SetResponderSessionParams(&p)
	}
	adapter.SetOnSessionEstablished(func(sharedSecret []byte, peerSessionID uint16) error {
		// Ke has served once the session's keys are derived from it, and
		// this is the only copy outside the handshake's verifier
		// (bridge.PaseSessionEstablished): overwritten on every way out.
		// Best effort, within Go's limits (secure/sigma/zero.go);
		// connectedhomeip clears the same
		// (src/crypto/CHIPCryptoPALOpenSSL.cpp:1150-1151 at 6170af84,
		// `ClearSecretData(Kae)`; review II, W1-sec-05, owner decision 6).
		defer clear(sharedSecret)
		// PASE predates the fabric, so both node ids are zero.
		// The install takes the stake, not the bare id: a stake an
		// allocation at a full table took over installs nothing
		// (operational.ErrStakeLost, decision 159).
		entry, err := sessions.OpenFromPaseWithStake(stake, 0, 0, peerSessionID, sharedSecret)
		if err != nil {
			sessions.ReleaseStake(stake)
			return err
		}
		// The commissioner's SessionParameters size the session's
		// retransmissions and name its transports — matter.js
		// PaseServer.ts:155-157 `session.timingParameters = initiatorSessionParams`.
		if p := adapter.PeerSessionParams(); p != nil {
			entry.SetPeerMRPIntervals(p.SessionIdleInterval, p.SessionActiveInterval, uint32(p.SessionActiveThreshold))
			entry.SetPeerTransportParameters(p.EffectiveTransports(), p.EffectiveMaxTCPMessageSize())
		}
		// Matter §11.10.5.2 floor: arm a fail-safe if the commissioner has
		// not, so an abandoned attempt still rolls back. A session whose
		// arm is refused — a fail-safe rollback ended it, its PASE close
		// possibly naming the sessions before this one existed — is
		// closed, and the handshake fails: no PASE session stays without
		// a fail-safe (connectedhomeip
		// src/app/server/CommissioningWindowManager.cpp:240-242 at
		// 6170af84, MarkForEviction and HandleFailedAttempt; review II,
		// R2-P3-pase-03).
		if err := root.GeneralCommissioning.ArmOnPaseSession(life, sessionID); err != nil {
			sessionseam.CloseEntry(sessions, entry)
			return fmt.Errorf("node: the PASE session's fail-safe: %w", err)
		}
		return nil
	})
	return adapter, nil
}

// handshakeStake is the session id stake one CASE responder handshake
// holds: the id it announces, staked anew when the handshake starts over.
// Only this stake is ever released, never whatever holds its id now
// (review II, R2-P3-dr-dev-07).
type handshakeStake struct {
	sessions *operational.Manager
	// ids, when set, judges the identity the handshake answers under:
	// fabric and gen, which the handshake's resolver records
	// (handshakeResolver).
	ids    *caseIdentities
	mu     sync.Mutex
	stake  operational.Stake
	fabric uint8
	gen    uint64
	// pending is the resolution of the Sigma1 being judged, committed to
	// fabric and gen only once the responder accepts it
	// ([handshakeResolver.CommitResolution]).
	pending       bool
	pendingFabric uint8
	pendingGen    uint64
}

// answers records that the handshake answers under load gen of fabric's
// identity.
func (h *handshakeStake) answers(fabric uint8, gen uint64) {
	h.mu.Lock()
	h.fabric, h.gen, h.pending = fabric, gen, false
	h.mu.Unlock()
}

// resolved records the resolution of the Sigma1 being judged: load gen of
// fabric's identity. It stands for the handshake only once committed.
func (h *handshakeStake) resolved(fabric uint8, gen uint64) {
	h.mu.Lock()
	h.pending, h.pendingFabric, h.pendingGen = true, fabric, gen
	h.mu.Unlock()
}

// commit makes the pending resolution the handshake's: its Sigma1 was
// accepted.
func (h *handshakeStake) commit() {
	h.mu.Lock()
	if h.pending {
		h.fabric, h.gen, h.pending = h.pendingFabric, h.pendingGen, false
	}
	h.mu.Unlock()
}

// renew stakes an id for a handshake whose Sigma1 the responder accepted —
// its first, or one that starts over — and releases the stake it held
// before, if any: the responder's [sigma.Responder.SetSessionIDRenewer]
// source.
func (h *handshakeStake) renew(uint16) (uint16, bool) {
	next, err := h.sessions.AllocateStake()
	if err != nil {
		return 0, false
	}
	h.mu.Lock()
	previous := h.stake
	h.stake = next
	h.mu.Unlock()
	h.sessions.ReleaseStake(previous)
	return next.ID, true
}

// of returns the stake when it is the one of id, the id the handshake
// announced, and the identity the handshake answers under is still its
// fabric's — no UpdateNOC replaced it since (review II, R2-P6-sec-2412);
// otherwise the zero Stake, which every install refuses
// ([operational.ErrStakeLost]).
func (h *handshakeStake) of(id uint16) operational.Stake {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stake.ID != id {
		return operational.Stake{}
	}
	if h.ids != nil && h.gen != 0 && h.ids.admissible(h.fabric, h.gen) != nil {
		return operational.Stake{}
	}
	return h.stake
}

// genOf is the load of fabric's identity the handshake answers under, 0
// when the handshake resolved another fabric or none: generations are one
// counter over every fabric, so another fabric's would refuse a good
// session (review II, R2-P6-sec-2412).
func (h *handshakeStake) genOf(fabric uint8) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.fabric != fabric {
		return 0
	}
	return h.gen
}

// release releases the stake when it is the one of id, the id the
// handshake announced.
func (h *handshakeStake) release(id uint16) {
	h.mu.Lock()
	s := h.stake
	h.mu.Unlock()
	if s.ID == id {
		h.sessions.ReleaseStake(s)
	}
}

// buildCaseAdapter wires one CASE exchange: a responder seeded from the
// current operational identity, the multi-fabric destination resolver, and on
// success an operational session registered under whichever id the
// handshake ended up using.
//
// onOpened, when set, learns the id of the session the handshake opened
// (the node's OnSessionOpened).
func buildCaseAdapter(sessions *operational.Manager, ids *caseIdentities, responderParams *sessionparams.Parameters, logger *slog.Logger, onOpened func(sessionID uint16)) (*bridge.CaseAdapter, error) {
	current := ids.current()
	// The handshake stakes its session id once its Sigma1 is accepted, not
	// here: a Sigma1 that starts no handshake stakes nothing, so it cannot
	// close the least recently active session at a full table (review II,
	// R2-P3-dr-dev-08). The responder is built without an id and takes it
	// from the renewer ([sigma.Responder.SetSessionIDRenewer]).
	held := &handshakeStake{sessions: sessions, ids: ids}

	identity := (*sigma.Identity)(nil)
	var verifier sigma.PeerVerifier
	if current != nil {
		identity, verifier = current.identity, current.verifier
		held.answers(current.identity.FabricIndex, current.gen)
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

	responder := sigma.NewResponder(identity, verifier, 0)
	responder.SetIdentityResolver(handshakeResolver{ids: ids, held: held})
	// Our session parameters ride in Sigma2 (CaseServer.ts:272
	// responderSessionParams: sessions.sessionParameters); nil leaves
	// NewCaseAdapter's defaults.
	if responderParams != nil {
		p := *responderParams
		responder.SetSessionParameters(&p)
	}
	// The handshake's id, staked when its Sigma1 is accepted; a second
	// Sigma1 on the same exchange — Apple grafts its iCloud companion
	// fabric onto one — must not reuse the first session's slot.
	responder.SetSessionIDRenewer(held.renew)

	// Session resumption as the responder (Matter §4.13.2.4): a Sigma1
	// presenting a resumption id this node issued resumes with
	// Sigma2_Resume instead of a full handshake. matter.js CaseServer looks
	// the record up in SessionManager and saves a fresh one after every
	// session: after the initiator's success report on a resume, after
	// Sigma3 on a full handshake
	// (packages/protocol/src/session/case/CaseServer.ts:219, :344 at
	// db021da0). Here the record is replaced when the session is installed,
	// before Sigma2_Resume is sent (review II, R2-P6-sec-3320); TC-SC-3.2
	// checks the resume.
	responder.SetResumptionStore(resumptionLookup{sessions: sessions, claims: &ids.claims})

	adapter := bridge.NewCaseAdapter(responder)
	// The staked id goes back when the handshake fails or its adapter is
	// dropped unfinished (review II, R2-P3-lan-dev-02) — this handshake's
	// stake only, which leaves a live session and another handshake's
	// stake alone (R2-P3-dr-dev-07).
	adapter.SetOnUnfinished(held.release)
	adapter.SetOnSessionEstablished(func(keys sigma.SessionKeys, peerSessionID uint16) error {
		// Everything is read back off the responder rather than captured at
		// factory time: the destination resolver may have landed on a
		// different fabric than the one seeded above, and the session id may
		// have been renewed.
		fabricIndex := identity.FabricIndex
		localNodeID := identity.NodeID
		peerNodeID := uint64(0)
		var effectiveID uint16
		var peerCATs []uint32
		if resp := adapter.SnapshotResponder(); resp != nil {
			peerNodeID = resp.PeerNodeID()
			peerCATs = resp.PeerCATs()
			effectiveID = resp.SessionID()
			if fi, nid, ok := resp.SessionIdentity(); ok {
				fabricIndex, localNodeID = fi, nid
			}
		}
		// A resume holds its record's id from acceptance
		// ([resumptionLookup.ClaimResumptionID]) until the record is
		// replaced, or gives it back when it installs nothing.
		var presented []byte
		if resp := adapter.SnapshotResponder(); resp != nil {
			if info := resp.ResumeInfo(); info.Resumed {
				presented = info.PresentedResumptionID
			}
		}
		// Admitted under the load the handshake answered under: an update
		// that lands after the stake was taken refuses the install
		// (review II, R2-P6-sec-2412).
		gen := held.genOf(fabricIndex)
		entry, openErr := ids.admitSessionAt(sessions, fabricIndex, gen, func() (*operational.Entry, error) {
			// The install takes this handshake's stake, not the bare id:
			// a stake an allocation at a full table took over installs
			// nothing (operational.ErrStakeLost, N3-findev-1).
			stake := held.of(effectiveID)
			if ids.testAfterStake != nil {
				ids.testAfterStake()
			}
			return sessions.OpenFromSigmaWithStake(stake, fabricIndex, localNodeID, peerNodeID, peerSessionID, peerCATs, keys)
		})
		if openErr != nil {
			held.release(effectiveID)
			ids.claims.release(presented)
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
			slog.Uint64("node_id", peerNodeID))
		if onOpened != nil {
			onOpened(entry.SessionID)
		}
		// The record the peer resumes from next time: the id this
		// handshake issued (Sigma2's, or the fresh one Sigma2_Resume
		// carried) with the session's shared secret.
		if resp := adapter.SnapshotResponder(); resp != nil {
			if id, secret := resp.ResumptionID(), resp.ECDHSharedSecret(); len(id) == 16 && len(secret) > 0 {
				// With the initiator's session parameters, as a CASE client
				// that resumes from the same record keeps them (review II,
				// Fable P-5). A resumed session keeps the parameters of the
				// record it resumed from, the id alone renewed — matter.js
				// re-saves the old record with the new id (CaseServer.ts:213,
				// :219 at db021da0; Fable P-5a).
				params, _ := resp.PeerSessionParameters()
				if info := resp.ResumeInfo(); info.Resumed {
					if prev, err := sessions.LookupResumption(context.Background(), info.PresentedResumptionID); err == nil {
						params = prev.SessionParameters
					}
				}
				if err := ids.persistAdmitted(context.Background(), sessions, gen, store.ResumptionRecord{
					FabricIndex: fabricIndex, PeerNodeID: peerNodeID, ResumptionID: id, SharedSecret: secret,
					CASEAuthTags: peerCATs, SessionParameters: params,
				}); err != nil {
					// The presented record stays in the store; its id stays
					// claimed, so it resumes nothing more.
					logger.Warn("case.resumption.persist_failed", slog.Any("err", err))
					return nil
				}
			}
		}
		// The presented record was replaced: its id is gone from the store.
		ids.claims.release(presented)
		return nil
	})
	return adapter, nil
}

// resumptionLookup serves the CASE responder's resumption records from the
// session manager's store, each id to one resume at a time.
type resumptionLookup struct {
	sessions *operational.Manager
	claims   *resumeClaims
}

// ClaimResumptionID implements the sigma responder's optional claim: the
// id is held from the resume's acceptance until its record was replaced
// or the resume installed nothing, so concurrent copies of one Sigma1 —
// whatever their exchange or source node id — resume once (review II,
// R2-P6-sec-3320, owner decision 231).
func (l resumptionLookup) ClaimResumptionID(id []byte) (release func(), ok bool) {
	if l.claims == nil {
		return func() {}, true
	}
	if !l.claims.claim(id) {
		return nil, false
	}
	return func() { l.claims.release(id) }, true
}

// resumeClaims are the resumption ids a resume holds
// ([resumptionLookup.ClaimResumptionID]). The zero value is ready. An id
// stays held while its resume runs. An id whose record could not be
// replaced (the persist failed) stays held for the life of the process,
// also after a later handshake replaced that peer's record: the set is
// bounded by those resumes, each one authenticated (a valid resume MIC
// over a record the store held), not by the store's records.
type resumeClaims struct {
	mu   sync.Mutex
	held map[string]struct{}
}

func (c *resumeClaims) claim(id []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, taken := c.held[string(id)]; taken {
		return false
	}
	if c.held == nil {
		c.held = make(map[string]struct{})
	}
	c.held[string(id)] = struct{}{}
	return true
}

// release gives id back; nil or an id not held is a no-op.
func (c *resumeClaims) release(id []byte) {
	if len(id) == 0 {
		return
	}
	c.mu.Lock()
	delete(c.held, string(id))
	c.mu.Unlock()
}

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

// handshakeAdapterTTL is how long an idle PASE or CASE handshake adapter
// lives before the reaper drops it; handshakeReapInterval is how often the
// reaper looks, a quarter of the TTL as the providers' StartReaper
// recommends (bridge.PerExchangePaseProvider.StartReaper). Named so the
// three reaper calls cannot swap the two durations (review II,
// R2-P5-apinode-16).
const (
	handshakeAdapterTTL   = time.Minute
	handshakeReapInterval = handshakeAdapterTTL / 4
)
