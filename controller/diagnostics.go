// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/clock"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/diag"
	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/thread/brdiscovery"
)

// SessionDiagnostics returns the controller's sessions and peers: every
// session of every peer, flat in Sessions, and one [diag.PeerInfo] per
// peer — its client node's connection state, its connection process, its
// DNS-SD service, its ICD Check-Ins — referencing its sessions by id. It
// is the shape node.Node.SessionDiagnostics returns, so one page model
// serves both roles; Occupancy stays zero (Capacity 0: the controller's
// exchange manager keeps no id-occupancy figure) and each session's
// Subscriptions 0 (the controller's subscriptions are kept by peer).
//
// It reads the peer set's clone of its peers and each peer's clone of its
// sessions, so it holds no lock across the peers, and asks the DNS-SD
// cache for each peer's names: it must not be called from a callback the
// cache runs (an [Controller.OnDiagnosticEvent] function among them).
//
// matter.js exposes the same state as live sets — PeerSet.peers
// (packages/protocol/src/peer/PeerSet.ts:218 at db021da0) and
// SessionManager.sessions (session/SessionManager.ts:376) — and serves a
// host no snapshot of it (BD-Matter-ControllerDiagnostics). Once Close has
// begun it is the zero value.
func (c *Controller) SessionDiagnostics() diag.SessionDiagnostics {
	if _, ok := c.open(); !ok {
		return diag.SessionDiagnostics{}
	}
	now := c.clock.Now()
	out := diag.SessionDiagnostics{AsOf: now}
	set := c.peerSet.Load()
	if set == nil {
		return out
	}
	reg := c.registry.Load()
	for _, p := range set.Peers() {
		info := peerInfoOf(p, reg)
		for _, s := range p.Sessions() {
			out.Sessions = append(out.Sessions, sessionInfoOf(s, now))
			info.SessionIDs = append(info.SessionIDs, s.ID())
		}
		out.Peers = append(out.Peers, info)
	}
	return out
}

// peerInfoOf is p as the snapshot reports it, with the client node of
// reg at its address when there is one.
func peerInfoOf(p *peer.Peer, reg *client.Registry) diag.PeerInfo {
	addr := p.Address()
	d := p.Descriptor()
	info := diag.PeerInfo{
		FabricIndex: addr.FabricIndex, NodeID: addr.NodeID,
		TimeOffline: p.TimeOffline(), OperationalHost: d.OperationalHost, TCPUnsupported: d.TCPUnsupported,
	}
	if d.OperationalAddress != nil {
		info.OperationalAddress = d.OperationalAddress.String()
	}
	if svc := p.Service(); svc != nil {
		info.Reachable = svc.Reachable()
		if name := svc.Name(); name != nil && name.IsDiscovered() {
			info.DiscoveredNames = []string{name.QName()}
		}
	}
	var n *client.Node
	if reg != nil {
		n = reg.GetByAddress(addr)
	}
	if n == nil {
		return info
	}
	info.Registered = true
	info.State = n.ConnectionState().String()
	if err := n.LoadError(); err != nil {
		info.LoadError = err.Error()
	}
	info.SubscriptionActive = n.SubscriptionActive()
	if icd := n.ICD(); icd != nil {
		if at, ok := icd.LastCheckInAt(); ok {
			info.LastCheckInAt = at
		}
		if at, ok := icd.NextExpectedCheckIn(); ok {
			info.NextExpectedCheckIn = at
		}
	}
	return info
}

// sessionInfoOf is s as the snapshot reports it, its ages at now. A peer's
// session is CASE (the set adopts no PASE session); the exchange session
// exports no any-direction timestamp, so LastActivity and Idle stay zero.
func sessionInfoOf(s *exchange.Session, now time.Time) diag.SessionInfo {
	info := diag.SessionInfo{
		ID: s.ID(), FabricIndex: s.FabricIndex(), PeerNodeID: s.PeerNodeID(), LocalNodeID: s.NodeID(),
		LastPeerActivity: s.ActiveTimestamp(),
		CreatedAt:        s.CreatedAt(), Resumed: s.Resumed(), Transport: s.Transport().String(), PeerLost: s.IsPeerLost(),
	}
	info.PeerIdle = ageAt(now, info.LastPeerActivity)
	if r := s.ReportedParameters(); r != nil {
		info.PeerIdleInterval = time.Duration(r.SessionIdleInterval) * time.Millisecond
		info.PeerActiveInterval = time.Duration(r.SessionActiveInterval) * time.Millisecond
		info.PeerActiveThreshold = time.Duration(r.SessionActiveThreshold) * time.Millisecond
	}
	return info
}

// ageAt is now minus t, 0 for a zero t or a t after now.
func ageAt(now, t time.Time) time.Duration {
	if t.IsZero() || t.After(now) {
		return 0
	}
	return now.Sub(t)
}

// DiagnosticEvents returns the controller's diagnostic trace, newest
// first: a copy the caller may keep. The trace holds what the controller
// observes — pairing steps that failed, sessions established, lost and
// closed, connection-state transitions, operational records and border
// routers appearing, reachability probes, ICD Check-Ins, software-update
// statuses and refused camera signals — each with an operator sentence
// (diagevent.Kind…). Its size is [Config].Diagnostics. None once Close has
// begun.
func (c *Controller) DiagnosticEvents() []diag.Event {
	if _, ok := c.open(); !ok {
		return nil
	}
	return c.diagRing.Snapshot()
}

// OnDiagnosticEvent registers fn for every event the controller records
// from now on. fn runs inline, on the goroutine that recorded the event —
// the receive path, a peer's worker, the DNS-SD cache's goroutine — after
// the trace stored it and outside its lock: it must not block, may read
// [Controller.DiagnosticEvents], and must not call
// [Controller.SessionDiagnostics] or [Controller.DiagnoseMDNS], which
// wait for the DNS-SD cache that records the discovery events. There is
// no queued variant: the controller keeps no ordered callback queue (the
// node's OnDiagnosticEventAsync runs on one). It returns the unsubscribe
// function; once Close has begun it registers nothing.
func (c *Controller) OnDiagnosticEvent(fn func(diag.Event)) (unsubscribe func()) {
	if _, ok := c.open(); !ok || fn == nil {
		return func() {}
	}
	return c.diagRing.Observe(fn)
}

// newTrace is the controller's diagnostic ring, sized by d, on clk.
func newTrace(d diag.Diagnostics, clk clock.Clock) *diagevent.Ring {
	r := diagevent.NewRing(d.Effective())
	r.SetClock(clk)
	return r
}

// startTrace begins what the trace observes beyond the hooks New wires:
// the peer set the session gauge counts over, the DNS-SD cache's
// operational records and border routers, and the ICD Check-Ins of the
// nodes the registry loaded. New calls it once the registry exists.
func (c *Controller) startTrace() {
	c.peerSet.Store(c.peers)
	c.stopDiscover = c.watchDiscovery()
	for _, n := range c.reg.Commissioned() {
		c.watchICD(n)
	}
}

// record adds one event to the trace.
func (c *Controller) record(kind diagevent.Kind, severity diagevent.Severity, message string, detail map[string]string) {
	c.diagRing.Record(diagevent.Event{Kind: kind, Severity: severity, Message: message, Detail: detail})
}

// nodeHex is a node id as the trace writes it: 16 upper-case hex digits,
// the operational instance name's form.
func nodeHex(id uint64) string { return fmt.Sprintf("%016X", id) }

// pairTrace is one Pair's state in the trace: the step that failed
// last, which names the pairing's failure.
type pairTrace struct {
	mu         sync.Mutex
	failedStep string
}

func (t *pairTrace) failed(step string) {
	t.mu.Lock()
	t.failedStep = step
	t.mu.Unlock()
}

func (t *pairTrace) lastFailed() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.failedStep
}

// sentence is prefix as one sentence, with err's text after a colon when
// there is one: "Pairing failed: no device." — a full stop of err's own is
// not doubled.
func sentence(prefix string, err error) string {
	if err == nil {
		return prefix + "."
	}
	text := strings.TrimRight(strings.TrimSpace(err.Error()), ".")
	if text == "" {
		return prefix + "."
	}
	return prefix + ": " + text + "."
}

// errText is err's text, "" for nil.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// tracePairing wraps a Pair's progress hook: the first started step
// records "Pairing started.", a failed step a warning — one discovery
// transport of a race may fail while another wins, so a failed step is
// not yet a failed pairing ([Controller.Pair] records that) — and then
// host runs.
func (c *Controller) tracePairing(pc PairingCode, tr *pairTrace, host func(PairingStep)) func(PairingStep) {
	var started sync.Once
	kind := "qr"
	if pc.Manual {
		kind = "manual"
	}
	return func(st PairingStep) {
		switch st.Phase {
		case PairingStarted:
			started.Do(func() {
				c.record(diagevent.KindPairing, diagevent.SeverityInfo, "Pairing started.",
					map[string]string{"code_kind": kind, "discriminator": strconv.Itoa(int(pc.Discriminator))})
			})
		case PairingFailed:
			tr.failed(st.Name)
			c.record(diagevent.KindPairing, diagevent.SeverityWarning, sentence("Pairing step "+st.Name+" failed", st.Err),
				map[string]string{"step": st.Name, "err": errText(st.Err)})
		case PairingSucceeded, PairingSkipped:
		}
		if host != nil {
			host(st)
		}
	}
}

// recordPairFailed records a Pair that returned err: one pair.failures and
// an error naming the step that failed last, when one did.
func (c *Controller) recordPairFailed(tr *pairTrace, err error) {
	c.count(MetricPairFailures)
	detail := map[string]string{"err": errText(err)}
	prefix := "Pairing failed"
	if step := tr.lastFailed(); step != "" {
		detail["step"] = step
		prefix += " at step " + step
	}
	c.record(diagevent.KindPairing, diagevent.SeverityError, sentence(prefix, err), detail)
}

// recordPaired records a node Pair commissioned.
func (c *Controller) recordPaired(n *client.Node) {
	who, detail := nodeDetail(n, n.ID())
	c.record(diagevent.KindPairing, diagevent.SeverityInfo, who+" was paired.", detail)
}

// peerAdded is the peer set's PeerAdded hook: the peer's sessions,
// establishment and reachability verdicts enter the trace.
func (c *Controller) peerAdded(p *peer.Peer) {
	addr := p.Address()
	id := nodeHex(addr.NodeID)
	fabric := strconv.Itoa(int(addr.FabricIndex))
	sessionDetail := func(s *exchange.Session) map[string]string {
		return map[string]string{"node_id": id, "fabric_index": fabric, "session_id": strconv.Itoa(int(s.ID()))}
	}
	p.Observe(peer.Observer{
		SessionAdded: func(s *exchange.Session) {
			d := sessionDetail(s)
			d["transport"], d["resumed"] = s.Transport().String(), strconv.FormatBool(s.Resumed())
			c.record(diagevent.KindSession, diagevent.SeverityInfo, "A secure session with node "+id+" was established.", d)
			c.gaugeSessions()
		},
		SessionDeleted: func(s *exchange.Session) {
			if s.IsPeerLost() {
				c.record(diagevent.KindSession, diagevent.SeverityWarning, "The secure session with node "+id+" was lost; the controller reconnects.", sessionDetail(s))
			} else {
				c.record(diagevent.KindSession, diagevent.SeverityInfo, "The secure session with node "+id+" was closed.", sessionDetail(s))
			}
			c.gaugeSessions()
		},
		EstablishmentUnresponsive: func() {
			c.count(MetricCASEUnresponsive)
			c.record(diagevent.KindSession, diagevent.SeverityWarning, "CASE with node "+id+" got no answer within the MRP budget.",
				map[string]string{"node_id": id, "fabric_index": fabric})
		},
		ReachabilityVerified: func(reason peer.ReachabilityReason, ok bool, address string) {
			c.count(MetricReachabilityProbes)
			d := map[string]string{"node_id": id, "fabric_index": fabric, "reason": reason.String(), "address": address}
			if ok {
				c.record(diagevent.KindReachability, diagevent.SeverityInfo, fmt.Sprintf("Node %s answered a %s probe at %s.", id, reason, address), d)
				return
			}
			c.record(diagevent.KindReachability, diagevent.SeverityWarning, fmt.Sprintf("Node %s answered no %s probe; the session is given up.", id, reason), d)
		},
	})
}

// peerRemoved is the peer set's PeerRemoved hook: a closed peer's
// sessions left without SessionDeleted, so the gauge is taken again.
func (c *Controller) peerRemoved(*peer.Peer) { c.gaugeSessions() }

// diagHooks chains the trace in front of inner's hooks: a connection-state
// transition is recorded, a commissioned node's ICD Check-Ins are watched
// and a decommissioned node's no longer; inner's hooks run after.
func (c *Controller) diagHooks(inner func(id string) client.Hooks) func(id string) client.Hooks {
	return func(id string) client.Hooks {
		h := inner(id)
		state := h.ConnectionStateChanged
		h.ConnectionStateChanged = func(s client.ConnectionState) {
			c.connectionStateChanged(id, s)
			if state != nil {
				state(s)
			}
		}
		commissioned := h.Commissioned
		h.Commissioned = func() {
			if n := c.nodeByID(id); n != nil {
				c.watchICD(n)
			}
			if commissioned != nil {
				commissioned()
			}
		}
		decommissioned := h.Decommissioned
		h.Decommissioned = func() {
			if n := c.nodeByID(id); n != nil {
				c.unwatchICD(n)
			}
			if decommissioned != nil {
				decommissioned()
			}
		}
		deleted := h.Deleted
		h.Deleted = func() {
			c.unwatchDeletedICD(id)
			if deleted != nil {
				deleted()
			}
		}
		return h
	}
}

// nodeByID is the registry's node with local id, nil before the registry
// exists or for an unknown id.
func (c *Controller) nodeByID(id string) *client.Node {
	if r := c.registry.Load(); r != nil {
		return r.Get(id)
	}
	return nil
}

// nodeDetail names the node with local id for an event: its operational
// node id when it has a peer address, its local id otherwise.
func nodeDetail(n *client.Node, id string) (who string, detail map[string]string) {
	detail = map[string]string{"node": id}
	who = fmt.Sprintf("Node %q", id)
	if n == nil {
		return who, detail
	}
	if a, ok := n.PeerAddress(); ok {
		detail["node_id"], detail["fabric_index"] = nodeHex(a.NodeID), strconv.Itoa(int(a.FabricIndex))
		who = "Node " + nodeHex(a.NodeID)
	}
	return who, detail
}

// connectionStateChanged records a client node's transition: a lost or
// waiting connection as a warning, connected and disconnected as info.
func (c *Controller) connectionStateChanged(id string, s client.ConnectionState) {
	who, detail := nodeDetail(c.nodeByID(id), id)
	detail["state"] = s.String()
	severity, message := diagevent.SeverityInfo, who+" is "+s.String()+"."
	switch s {
	case client.Connected:
		message = who + " is connected."
	case client.Disconnected:
		message = who + " is disconnected."
	case client.Reconnecting:
		severity, message = diagevent.SeverityWarning, who+" lost its connection; the controller reconnects."
	case client.WaitingForDeviceDiscovery:
		severity, message = diagevent.SeverityWarning, who+" looks offline; the controller waits for its operational record in DNS-SD."
	}
	c.record(diagevent.KindSession, severity, message, detail)
}

// watchICD records n's ICD Check-Ins and missed Check-Ins, once per node:
// for the nodes the registry loaded and for each node commissioned since.
// The observers are registered before the watch is published, so an
// unwatchICD that follows stops them.
func (c *Controller) watchICD(n *client.Node) {
	icd := n.ICD()
	if icd == nil {
		return
	}
	c.icdMu.Lock()
	defer c.icdMu.Unlock()
	if _, ok := c.icdWatched[n]; ok || c.icdClosed {
		return
	}
	id := n.ID()
	stops := []func(){
		icd.OnCheckIn(func(in client.ICDCheckIn) {
			who, detail := nodeDetail(n, id)
			detail["counter"] = strconv.FormatUint(uint64(in.Counter), 10)
			c.record(diagevent.KindICD, diagevent.SeverityInfo, fmt.Sprintf("%s checked in (counter %d).", who, in.Counter), detail)
		}),
		icd.OnCheckInMissed(func() {
			c.count(MetricICDCheckInsMissed)
			who, detail := nodeDetail(n, id)
			c.record(diagevent.KindICD, diagevent.SeverityWarning, who+" missed its expected check-in.", detail)
		}),
	}
	if c.icdWatched == nil {
		c.icdWatched = map[*client.Node][]func(){}
	}
	c.icdWatched[n] = stops
}

// unwatchICD stops watching n's Check-Ins (its node was decommissioned).
func (c *Controller) unwatchICD(n *client.Node) {
	c.icdMu.Lock()
	stops := c.icdWatched[n]
	delete(c.icdWatched, n)
	c.icdMu.Unlock()
	for _, stop := range stops {
		stop()
	}
}

// unwatchDeletedICD stops watching the node with local id that left the
// registry: a node Forget or Erase deleted fires no Decommissioned (review
// II, R2-P1-ctl-05). The registry no longer holds it when its Deleted hook
// runs, and a node created since may hold the id, so the watch is found by
// its id among the watched nodes the registry does not hold under it.
func (c *Controller) unwatchDeletedICD(id string) {
	current := c.nodeByID(id)
	c.icdMu.Lock()
	var gone []*client.Node
	for n := range c.icdWatched {
		if n.ID() == id && n != current {
			gone = append(gone, n)
		}
	}
	c.icdMu.Unlock()
	for _, n := range gone {
		c.unwatchICD(n)
	}
}

// unwatchAllICD stops every ICD watch and refuses new ones (Close).
func (c *Controller) unwatchAllICD() {
	c.icdMu.Lock()
	watched := c.icdWatched
	c.icdWatched, c.icdClosed = nil, true
	c.icdMu.Unlock()
	for _, stops := range watched {
		for _, stop := range stops {
			stop()
		}
	}
}

// watchingICD reports whether n's Check-Ins are watched (a test seam).
func (c *Controller) watchingICD(n *client.Node) bool {
	c.icdMu.Lock()
	defer c.icdMu.Unlock()
	_, ok := c.icdWatched[n]
	return ok
}

// operationalSuffix is the DNS-SD suffix of the operational instances the
// discovery trace watches, compared case-insensitively.
const operationalSuffix = "._matter._tcp.local"

// watchDiscovery records the operational records of our fabric's nodes
// appearing and disappearing in the DNS-SD cache, and the border routers
// the registry adds and removes. It returns the function that stops both.
// The watch runs on the cache's goroutine and only records.
func (c *Controller) watchDiscovery() func() {
	cfid := c.fabric.Identity.CompressedFabricID
	prefix := strings.ToUpper(hex.EncodeToString(cfid[:])) + "-"
	seen := map[string]bool{} // the cache goroutine's alone
	stopNames := c.names.Watch([]string{operationalSuffix}, func(st discovery.NameState) {
		instance, _, _ := strings.Cut(st.QName, ".")
		upper := strings.ToUpper(instance)
		if !strings.HasPrefix(upper, prefix) {
			return
		}
		node := strings.TrimPrefix(upper, prefix)
		key := strings.ToLower(st.QName)
		detail := map[string]string{"node_id": node, "qname": st.QName, "srv_target": st.SRVTarget()}
		switch {
		case st.Discovered && !seen[key]:
			seen[key] = true
			c.record(diagevent.KindDiscovery, diagevent.SeverityInfo, "Node "+node+"'s operational record appeared.", detail)
		case !st.Discovered && seen[key]:
			delete(seen, key)
			c.record(diagevent.KindDiscovery, diagevent.SeverityInfo, "Node "+node+"'s operational record disappeared.", detail)
		}
	})
	stopBRs := c.brs.OnChange(func(e brdiscovery.Event) {
		detail := map[string]string{"ext_address": e.Entry.ExtAddress, "network_name": e.Entry.NetworkName, "host_name": e.Entry.HostName}
		name := e.Entry.ExtAddress
		if e.Entry.NetworkName != "" {
			name += " (" + e.Entry.NetworkName + ")"
		}
		switch e.Kind {
		case brdiscovery.EventAdded:
			c.record(diagevent.KindDiscovery, diagevent.SeverityInfo, "Thread border router "+name+" appeared.", detail)
		case brdiscovery.EventRemoved:
			c.record(diagevent.KindDiscovery, diagevent.SeverityInfo, "Thread border router "+name+" disappeared.", detail)
		case brdiscovery.EventUpdated:
		}
	})
	return func() {
		stopBRs()
		stopNames()
	}
}

// traceOTAStatus wraps the update manager's OnStatus: the status enters
// the trace — Cancelled, an interrupted transfer, as a warning — and then
// host runs.
func (c *Controller) traceOTAStatus(host func(softwareupdate.Peer, softwareupdate.Status, uint32)) func(softwareupdate.Peer, softwareupdate.Status, uint32) {
	return func(p softwareupdate.Peer, status softwareupdate.Status, version uint32) {
		severity := diagevent.SeverityInfo
		if status == mattercore.OTAUpdateCancelled {
			severity = diagevent.SeverityWarning
		}
		id := nodeHex(p.NodeID)
		c.record(diagevent.KindOTA, severity, fmt.Sprintf("Node %s's software update is %s (version %d).", id, status, version),
			map[string]string{"node_id": id, "fabric_index": strconv.Itoa(int(p.FabricIndex)), "status": status.String(), "version": strconv.FormatUint(uint64(version), 10)})
		if host != nil {
			host(p, status, version)
		}
	}
}

// DiagnoseMDNS reports what keeps the controller's DNS-SD from working:
// mdns.Diagnose of the records it advertises (the commissioner record of
// [Config].Advertise, the operational record of a served tree) — without
// its no_commissionable_service finding, since a controller is not
// commissioned through its advertisement — and the browse-side
// [mdns.CodeNoOperationalNames] when a commissioned node waits for device
// discovery and the DNS-SD cache holds none of the commissioned nodes'
// operational records. The controller has no interface allow-list (the
// node's MDNS.Interfaces), so mdns.DiagnoseInterfaces is not called.
// Errors come first, the order otherwise stable — the order of
// node.Node.DiagnoseMDNS. It asks the DNS-SD cache: it must not be called
// from a callback the cache runs.
func (c *Controller) DiagnoseMDNS() []mdns.Finding {
	c.closeMu.RLock()
	defer c.closeMu.RUnlock()
	if c.closed {
		return nil
	}
	var out []mdns.Finding
	if c.adv != nil {
		for _, f := range mdns.Diagnose(c.adv.Active()) {
			if f.Code == "no_commissionable_service" { // the node's: a controller advertises no _matterc record
				continue
			}
			out = append(out, f)
		}
	}
	if reg := c.registry.Load(); reg != nil && c.names != nil {
		out = append(out, c.diagnoseBrowse(reg.Commissioned())...)
	}
	slices.SortStableFunc(out, func(a, b mdns.Finding) int {
		switch {
		case a.Severity == b.Severity:
			return 0
		case a.Severity == mdns.SeverityError:
			return -1
		case b.Severity == mdns.SeverityError:
			return 1
		}
		return 0
	})
	return out
}

// diagnoseBrowse counts the waiting nodes and those whose operational
// record is in the cache, for mdns.DiagnoseOperationalBrowse.
func (c *Controller) diagnoseBrowse(nodes []*client.Node) []mdns.Finding {
	cached := map[string]bool{}
	for _, name := range c.names.DiscoveredNames() {
		cached[strings.ToLower(name.QName())] = true
	}
	cfid := c.fabric.Identity.CompressedFabricID
	var waiting, discovered int
	for _, n := range nodes {
		if n.ConnectionState() == client.WaitingForDeviceDiscovery {
			waiting++
		}
		if a, ok := n.PeerAddress(); ok && cached[strings.ToLower(discovery.OperationalQName(cfid, a.NodeID))] {
			discovered++
		}
	}
	return mdns.DiagnoseOperationalBrowse(len(nodes), waiting, discovered)
}
