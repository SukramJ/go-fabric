// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"log/slog"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// Peer is a node on one of our fabrics: its descriptor, its sessions, its
// operational DNS-SD service and the work that keeps a session to it. It
// is matter.js Peer (packages/protocol/src/peer/Peer.ts:64-674 at
// 0ceb202d) without the transport preference, the network profile, the
// BasicInformation and physical-properties inputs, and the client
// interaction (stages 3 and 4). It is safe for concurrent use.
type Peer struct {
	set     *Set
	address handshake.PeerAddress
	service *discovery.Service
	status  *discovery.Status
	log     *slog.Logger

	// ctx is matter.js Peer.#abort (Peer.ts:74): Close cancels it, and with
	// it the observers, the connection process and the address monitor.
	ctx     context.Context
	cancel  context.CancelFunc
	workers sync.WaitGroup

	// mu guards closed, desc, saving, sessions and workers.
	mu                    sync.Mutex
	desc                  Descriptor
	sessions              []*exchange.Session
	saving                bool
	closed                bool
	connecting            *connection
	connectionInitiatedAt time.Time
	monitor               *monitor
	observers             []*observerEntry

	// icdAwait reports a sleeping LIT ICD peer the controller awaits
	// (SetICDAwait); nil for a peer that is no registered ICD.
	icdAwait atomic.Pointer[func() bool]
	// litOperating reports the peer's physical property
	// isLongIdleTimeOperating (SetLongIdleTimeOperating).
	litOperating atomic.Pointer[func() bool]
}

// LITMinIdleInterval is the idle interval a LIT ICD peer is assumed to
// have when no source gives its SII: the 500 ms default is too aggressive
// for a sleeping LIT ICD. Mirrors matter.js Peer.ts:59 (LIT_MIN_IDLE_INTERVAL).
const LITMinIdleInterval = 5 * time.Second

// SetLongIdleTimeOperating sets the report of the peer's physical property
// isLongIdleTimeOperating — a LIT ICD operating as LIT — which
// [Peer.SessionParameters] reads (Peer.ts:305-315). nil removes it.
func (p *Peer) SetLongIdleTimeOperating(fn func() bool) {
	if fn == nil {
		p.litOperating.Store(nil)
		return
	}
	p.litOperating.Store(&fn)
}

// SetICDAwait sets the report of whether the peer is a LIT ICD the
// controller awaits — its ICD wakefulness's RequiresAwait. While it
// reports true the address monitor never probes the peer: a sleeping LIT
// ICD does not answer, and a failed probe would close a healthy session
// (PeerAddressMonitor.ts:143-148). nil removes it.
func (p *Peer) SetICDAwait(fn func() bool) {
	if fn == nil {
		p.icdAwait.Store(nil)
		return
	}
	p.icdAwait.Store(&fn)
}

func (p *Peer) icdAwaited() bool {
	fn := p.icdAwait.Load()
	return fn != nil && (*fn)()
}

// newPeer builds the peer d describes — Peer.ts:95-188. The peer's
// service is its node's operational instance on fabric's compressed
// fabric id (:108-117), reachable at once when an operational address is
// known (:119-122).
func newPeer(s *Set, d Descriptor, fabric handshake.Fabric) *Peer {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Peer{
		set:     s,
		address: d.Address,
		service: s.ctx.Names.Service(fabric.Identity.CompressedFabricID, d.Address.NodeID),
		log:     s.log.With(slog.String("peer", addressString(d.Address))),
		ctx:     ctx,
		cancel:  cancel,
		desc:    d.clone(),
	}
	p.status = p.service.Status()
	if d.OperationalAddress != nil {
		p.status.SetReachable(true)
	}
	return p
}

// start begins observing the service: on a change the discovery data
// follows the TXT record and the address check is scheduled
// (Peer.ts:126-139).
func (p *Peer) start() {
	changes := p.service.Changes(p.ctx)
	p.goWork(func() {
		for range changes {
			p.onServiceChanged()
		}
	})
}

// goWork runs fn as one of the peer's workers (matter.js Peer.#workers),
// unless the peer is closed.
func (p *Peer) goWork(fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.workers.Add(1)
	go func() {
		defer p.workers.Done()
		fn()
	}()
}

// onServiceChanged: an operational TXT record states SII, SAI, SAT, T and
// ICD as a group, so one it omits no longer applies; the record's keys
// overlay the rest (Peer.ts:126-135). Then the address check is scheduled
// (:137-138).
func (p *Peer) onServiceChanged() {
	hasTXT := p.service.HasTXTRecord()
	parsed := p.service.TXTData()
	p.mu.Lock()
	merged := overlayTXT(cleared(p.desc.DiscoveryData, hasTXT), parsed)
	if p.desc.DiscoveryData == nil || !sameTXT(*p.desc.DiscoveryData, merged) {
		p.desc.DiscoveryData = &merged
		p.changedLocked()
	}
	p.mu.Unlock()
	p.scheduleAddressCheck()
}

// cleared copies dd with the operational keys dropped when a TXT record
// is present.
func cleared(dd *discovery.TXTData, hasTXT bool) discovery.TXTData {
	var out discovery.TXTData
	if dd != nil {
		out = *dd
	}
	if hasTXT {
		out.SII, out.SAI, out.SAT, out.T, out.ICD = nil, nil, nil, nil, nil
	}
	return out
}

// overlayTXT is the object spread {...base, ...over} over the keys over
// states.
func overlayTXT(base, over discovery.TXTData) discovery.TXTData {
	pick := func(dst **string, v *string) {
		if v != nil {
			*dst = v
		}
	}
	pick(&base.VP, over.VP)
	pick(&base.DN, over.DN)
	pick(&base.RI, over.RI)
	pick(&base.PI, over.PI)
	if over.DT != nil {
		base.DT = over.DT
	}
	if over.PH != nil {
		base.PH = over.PH
	}
	if over.SII != nil {
		base.SII = over.SII
	}
	if over.SAI != nil {
		base.SAI = over.SAI
	}
	if over.SAT != nil {
		base.SAT = over.SAT
	}
	if over.T != nil {
		base.T = over.T
	}
	if over.ICD != nil {
		base.ICD = over.ICD
	}
	return base
}

// sameTXT compares two discovery data sets by value (isDeepEqual,
// PeerDescriptor.ts:133-140).
func sameTXT(a, b discovery.TXTData) bool {
	return eqPtr(a.VP, b.VP) && eqPtr(a.DN, b.DN) && eqPtr(a.RI, b.RI) && eqPtr(a.PI, b.PI) &&
		eqPtr(a.DT, b.DT) && eqPtr(a.PH, b.PH) && eqPtr(a.SII, b.SII) && eqPtr(a.SAI, b.SAI) &&
		eqPtr(a.SAT, b.SAT) && eqPtr(a.T, b.T) && eqPtr(a.ICD, b.ICD)
}

func eqPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// mergeKnown applies AddKnownPeer's rules to a known peer
// (PeerSet.ts:315-337).
func (p *Peer) mergeKnown(d Descriptor) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if d.OperationalAddress != nil && !sameUDPAddr(p.desc.OperationalAddress, d.OperationalAddress) {
		p.desc.OperationalAddress = cloneUDPAddr(d.OperationalAddress)
		p.changedLocked()
	}
	if d.CaseAuthenticatedTags != nil && (p.desc.CaseAuthenticatedTags == nil || !slices.Equal(p.desc.CaseAuthenticatedTags, d.CaseAuthenticatedTags)) {
		p.desc.CaseAuthenticatedTags = slices.Clone(d.CaseAuthenticatedTags)
		p.changedLocked()
	}
	if d.DiscoveryData != nil {
		merged := *d.DiscoveryData
		if p.desc.DiscoveryData != nil {
			merged = overlayTXT(merged, *p.desc.DiscoveryData)
		}
		if p.desc.DiscoveryData == nil || !sameTXT(*p.desc.DiscoveryData, merged) {
			p.desc.DiscoveryData = &merged
			p.changedLocked()
		}
	}
	if p.desc.ReportedSessionParameters == nil && d.ReportedSessionParameters != nil {
		r := *d.ReportedSessionParameters
		p.desc.ReportedSessionParameters = &r
		p.changedLocked()
	}
}

// changedLocked records a descriptor change: one save runs for every
// burst of changes made before it starts — matter.js's #isSaving guard
// around #save (Peer.ts:99-106, :575-579).
func (p *Peer) changedLocked() {
	if p.saving || p.closed {
		return
	}
	p.saving = true
	p.workers.Add(1)
	go func() {
		defer p.workers.Done()
		p.save()
	}()
}

// save stores the descriptor and reports the update; a store error is
// logged at error level, as matter.js logs a failed worker
// (general/src/util/Multiplex.ts:41-47).
func (p *Peer) save() {
	p.mu.Lock()
	p.saving = false
	d := p.desc.clone()
	p.mu.Unlock()
	if st := p.set.ctx.Store; st != nil {
		if err := st.SavePeer(d); err != nil {
			p.log.Error("matter.controller.peer.save_failed", slog.Any("err", err))
		}
	}
	if h := p.set.ctx.Updated; h != nil {
		h(p)
	}
	for _, o := range p.observing() {
		if o.Updated != nil {
			o.Updated()
		}
	}
}

// addSession takes a session of the peer: the operational address follows
// the session's (UDP branch of Peer.ts:141-166), the session's reported
// parameters replace the descriptor's when it reported any (:176-178),
// and the session leaves the peer when it begins to close (:168-174).
func (p *Peer) addSession(s *exchange.Session) {
	p.mu.Lock()
	if p.closed || slices.Contains(p.sessions, s) {
		p.mu.Unlock()
		return
	}
	p.sessions = append(p.sessions, s)
	p.followAddressLocked(s)
	if r := s.ReportedParameters(); r != nil && (p.desc.ReportedSessionParameters == nil || *p.desc.ReportedSessionParameters != *r) {
		p.desc.ReportedSessionParameters = r
		p.changedLocked()
	}
	p.mu.Unlock()
	p.sessionAdded()
	for _, o := range p.observing() {
		if o.SessionAdded != nil {
			o.SessionAdded(s)
		}
	}
	p.goWork(func() {
		select {
		case <-s.Closing():
			p.removeSession(s)
		case <-p.ctx.Done():
		}
	})
}

// followAddressLocked makes the session's address the operational address
// (Peer.ts:142-144, :161-163).
func (p *Peer) followAddressLocked(s *exchange.Session) {
	if a := s.Addr(); a != nil && !sameUDPAddr(p.desc.OperationalAddress, a) {
		p.desc.OperationalAddress = cloneUDPAddr(a)
		p.changedLocked()
	}
}

// followAddress makes the session's address, after a migration, the
// operational address (Peer.ts:161-163, MessageChannel.networkAddressChanged).
func (p *Peer) followAddress(s *exchange.Session) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.followAddressLocked(s)
}

// removeSession drops a closing session; the set's Disconnected hook runs
// when it was the last (PeerSet.ts:97-101), then the observers'
// SessionDeleted (the set observes the peer's sessions first).
func (p *Peer) removeSession(s *exchange.Session) {
	p.mu.Lock()
	n := len(p.sessions)
	p.sessions = slices.DeleteFunc(p.sessions, func(x *exchange.Session) bool { return x == s })
	removed := n > len(p.sessions)
	last := removed && len(p.sessions) == 0
	p.mu.Unlock()
	if last && p.set.ctx.Disconnected != nil {
		p.set.ctx.Disconnected(p)
	}
	if !removed {
		return
	}
	for _, o := range p.observing() {
		if o.SessionDeleted != nil {
			o.SessionDeleted(s)
		}
	}
}

// Exchanges is the exchange manager the peer's sessions live on — the
// one of its set (matter.js PeerConnection.Context.exchanges, read by
// PeerExchangeProvider.ts:127-135).
func (p *Peer) Exchanges() *exchange.Manager { return p.set.ctx.Exchanges }

// Address is the peer's address.
func (p *Peer) Address() handshake.PeerAddress { return p.address }

// Descriptor returns a copy of the peer's descriptor.
func (p *Peer) Descriptor() Descriptor {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.desc.clone()
}

// Sessions returns the peer's sessions.
func (p *Peer) Sessions() []*exchange.Session {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.sessions)
}

// Service is the peer's operational DNS-SD service.
func (p *Peer) Service() *discovery.Service { return p.service }

// HasSession reports whether the peer has a session that is neither
// closing nor lost (Peer.ts:571-573).
func (p *Peer) HasSession() bool { return p.NewestSession() != nil }

// NewestSession is the live session the peer was last heard from on — the
// highest [exchange.Session.ActiveTimestamp], the first of equals — nil
// when none (Peer.ts:581-601).
func (p *Peer) NewestSession() *exchange.Session {
	var found *exchange.Session
	var at time.Time
	for _, s := range p.Sessions() {
		if s.IsClosed() || s.IsPeerLost() {
			continue
		}
		if t := s.ActiveTimestamp(); found == nil || at.Before(t) {
			found, at = s, t
		}
	}
	return found
}

// SessionParameters is the best guess of the peer's session parameters:
// what its last session reported, the intervals it does not report taken
// from its TXT record, the fallbacks for the rest. Mirrors matter.js
// Peer.sessionParameters (Peer.ts:281-317) without the BasicInformation
// and TCP inputs. With no SII from either source, a LIT-operating peer's
// idle interval is [LITMinIdleInterval] (:305-315).
func (p *Peer) SessionParameters() sessionparams.Parameters {
	p.mu.Lock()
	var r sessionparams.Parameters
	if p.desc.ReportedSessionParameters != nil {
		r = *p.desc.ReportedSessionParameters
	}
	var dd discovery.TXTData
	if p.desc.DiscoveryData != nil {
		dd = *p.desc.DiscoveryData
	}
	p.mu.Unlock()
	if r.SessionIdleInterval == 0 && dd.SII != nil {
		r.SessionIdleInterval = uint32(dd.SII.Milliseconds()) //nolint:gosec // TXTData bounds SII to one hour
	}
	if fn := p.litOperating.Load(); fn != nil && r.SessionIdleInterval == 0 && (*fn)() {
		r.SessionIdleInterval = uint32(LITMinIdleInterval.Milliseconds())
	}
	if r.SessionActiveInterval == 0 && dd.SAI != nil {
		r.SessionActiveInterval = uint32(dd.SAI.Milliseconds()) //nolint:gosec // TXTData bounds SAI to one hour
	}
	if r.SessionActiveThreshold == 0 && dd.SAT != nil {
		r.SessionActiveThreshold = uint16(dd.SAT.Milliseconds()) //nolint:gosec // TXTData bounds SAT to 65535 ms
	}
	return exchange.PeerParameters(&r)
}

// Disconnect aborts the connection process and waits for it to end; with
// a cause it also gives up every session with the peer, as one known
// unreachable (we removed our fabric from it, say), without the
// CloseSession of a graceful close. Without a cause the sessions stay.
// Mirrors matter.js Peer.disconnect (Peer.ts:504-527), whose
// initiateForceClose marks each session lost (NodeSession.ts:349-352).
func (p *Peer) Disconnect(ctx context.Context, cause error) {
	p.stopConnecting(ctx)
	if cause != nil {
		p.set.ctx.Exchanges.HandlePeerLoss(p.address, cause, time.Time{})
	}
}

// Close closes the peer without removing what is stored of it: it stops
// observing, aborts its work, waits for the address monitor's run, closes each of its sessions gracefully
// (CloseSession on the wire), waits for its workers, closes its service
// and leaves the set. Mirrors matter.js Peer.close (Peer.ts:543-565).
func (p *Peer) Close(ctx context.Context) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	mon := p.monitor
	p.mu.Unlock()
	p.cancel()
	if mon != nil {
		mon.close()
	}
	mgr := p.set.ctx.Exchanges
	for _, s := range mgr.SessionsFor(p.address) {
		mgr.CloseSession(ctx, s)
	}
	p.workers.Wait()
	p.service.Close()
	p.set.remove(p)
}

// Delete closes the peer and forgets it: its resumption record and its
// stored descriptor go. Mirrors matter.js Peer.delete (Peer.ts:529-541);
// the descriptor's removal is the node layer's there.
func (p *Peer) Delete(ctx context.Context) {
	p.log.Info("matter.controller.peer.removing")
	p.Close(ctx)
	if r := p.set.ctx.Resumption; r != nil {
		if _, err := r.DeleteResumptionRecord(p.address); err != nil {
			p.log.Error("matter.controller.peer.resumption_delete_failed", slog.Any("err", err))
		}
	}
	if st := p.set.ctx.Store; st != nil {
		if err := st.DeletePeer(p.address); err != nil {
			p.log.Error("matter.controller.peer.delete_failed", slog.Any("err", err))
		}
	}
}

// sameUDPAddr reports whether a and b are the same address, nil equal to
// nil — matter.js compares an operational address deeply before it
// replaces it (PeerDescriptor.ts:120-127).
func sameUDPAddr(a, b *net.UDPAddr) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.IP.Equal(b.IP) && a.Port == b.Port && a.Zone == b.Zone
}

func cloneUDPAddr(a *net.UDPAddr) *net.UDPAddr {
	c := *a
	c.IP = slices.Clone(a.IP)
	return &c
}
