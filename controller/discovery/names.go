// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/mdns"
)

// Cache defaults. Mirrors matter.js 0ceb202d
// packages/general/src/net/dns-sd/DnssdNames.ts:549-552
// (DnssdNames.defaults).
const (
	// DefaultEvictionDelay is how long a record survives once a goodbye
	// or a cache-flush record supersedes it (RFC 6762 §10.1).
	DefaultEvictionDelay = time.Second
	// DefaultMinTTL is the lowest TTL a record is cached with; a shorter
	// one is raised to it ("the value that Apple uses").
	DefaultMinTTL = 15 * time.Second
)

// Staged-address bounds and pruning. Mirrors matter.js 0ceb202d
// DnssdNames.ts:21-22 (STAGED_IP_RECORDS_MAX_HOSTS / _MIN_HOSTS) and
// :113-116 (the one-minute prune timer).
const (
	stagedIPMaxHosts      = 250
	stagedIPMinHosts      = 200
	stagedIPPruneInterval = time.Minute
)

// matterServiceSuffixes are the DNS-SD service types whose records enter
// the cache. Mirrors matter.js 0ceb202d
// packages/protocol/src/mdns/MdnsService.ts:25-27 (MATTER_SERVICE_SUFFIXES)
// and its ingress filter :90-95.
var matterServiceSuffixes = []string{"._matter._tcp.local", "._matterc._udp.local", "._matterd._udp.local"}

func isMatterServiceRecord(r *Record) bool {
	lower := strings.ToLower(r.Name)
	for _, s := range matterServiceSuffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	return false
}

type ingressFilter struct {
	accept func(*Record) bool
}

type stagedIP struct {
	record     *Record
	receivedAt time.Time
	sourceIntf string
}

type discoveredObserver struct {
	fn func(*Name)
}

// Names is the record cache of DNS-SD names collected by multicast DNS,
// matter.js DnssdNames (packages/general/src/net/dns-sd/DnssdNames.ts).
// It receives every response on its [mdns.QueryConn], keeps the records of
// the names it cares about with their TTLs, and serves the [Service] views,
// the [Solicitor] and the [Scanner] built on it.
//
// Records enter the cache when their name ends with `._matter._tcp.local`,
// `._matterc._udp.local` or `._matterd._udp.local` (the ingress filter of
// matter.js MdnsService), when a scanner's filter accepts them, or when
// their name is already in the cache (an SRV target, a name someone
// observes). An A/AAAA record for a host not yet in the cache is staged
// and replayed once an SRV names that host — but only from a packet that
// carried a relevant record, so unrelated LAN announcements cannot attach
// addresses to a later Matter SRV target.
type Names struct {
	conn   mdns.QueryConn
	logger *slog.Logger
	loop   *loop

	clock          clock
	random         func() uint32
	interfaceName  func(index int) string
	evictionDelay  time.Duration
	minTTL         time.Duration
	ttlGraceFactor float64

	closeOnce sync.Once

	// Loop-owned state below.
	names        *omap[string, *Name]
	stagedIPs    *omap[string, []stagedIP]
	filters      []*ingressFilter
	discovered   []*discoveredObserver
	expiring     map[*Record]struct{}
	expiryTimer  stopper
	expiryAt     time.Time
	pruneTimer   stopper
	currentBatch *[]*Name
	closed       bool
}

// namesConfig carries the knobs tests turn; production uses the defaults.
type namesConfig struct {
	clock         clock
	random        func() uint32
	interfaceName func(int) string
}

// Open joins the mDNS groups ([mdns.OpenQueryConn]) and returns a cache
// receiving on them. Close it to leave the groups.
func Open(logger *slog.Logger) (*Names, error) {
	conn, err := mdns.OpenQueryConn()
	if err != nil {
		return nil, fmt.Errorf("discovery: open: %w", err)
	}
	return NewNames(conn, logger), nil
}

// NewNames returns a cache receiving on conn. The cache owns conn from
// here on: [Names.Close] closes it.
func NewNames(conn mdns.QueryConn, logger *slog.Logger) *Names {
	return newNames(conn, logger, namesConfig{})
}

func newNames(conn mdns.QueryConn, logger *slog.Logger, cfg namesConfig) *Names {
	if logger == nil {
		logger = slog.Default()
	}
	n := &Names{
		conn:           conn,
		logger:         logger,
		loop:           newLoop(),
		clock:          cfg.clock,
		random:         cfg.random,
		interfaceName:  cfg.interfaceName,
		evictionDelay:  DefaultEvictionDelay,
		minTTL:         DefaultMinTTL,
		ttlGraceFactor: DefaultTTLGraceFactor,
		names:          newOmap[string, *Name](),
		stagedIPs:      newOmap[string, []stagedIP](),
		expiring:       make(map[*Record]struct{}),
	}
	if n.clock == nil {
		n.clock = wallClock{}
	}
	if n.random == nil {
		n.random = rand.Uint32 //nolint:gosec // retry jitter, not a secret
	}
	if n.interfaceName == nil {
		n.interfaceName = interfaceNameOf
	}
	n.filters = append(n.filters, &ingressFilter{accept: isMatterServiceRecord})
	n.armPrune()
	go n.run()
	return n
}

func interfaceNameOf(index int) string {
	if index <= 0 {
		return ""
	}
	ifi, err := net.InterfaceByIndex(index)
	if err != nil {
		return ""
	}
	return ifi.Name
}

func (n *Names) run() {
	defer close(n.loop.done)
	var responses <-chan mdns.QueryResponse
	if n.conn != nil {
		responses = n.conn.Responses()
	}
	for {
		select {
		case fn := <-n.loop.ops:
			n.loop.exec(fn)
		case r, ok := <-responses:
			if !ok {
				responses = nil
				continue
			}
			n.loop.exec(func() { n.handleMessage(r) })
		case <-n.loop.closing:
			n.loop.exec(n.shutdown)
			return
		}
	}
}

// Close stops the cache: every discovery ends, every waiter returns, the
// connection closes. Idempotent.
func (n *Names) Close() error {
	n.closeOnce.Do(func() {
		close(n.loop.closing)
		<-n.loop.done
		if n.conn != nil {
			_ = n.conn.Close()
		}
	})
	return nil
}

// Get returns the [Name] for qname (case-insensitive, without the trailing
// dot), creating it when it does not exist. A name created here and never
// observed stays in the cache until a record of it is deleted.
func (n *Names) Get(qname string) *Name {
	var out *Name
	if err := n.loop.call(func() { out = n.get(qname) }); err != nil {
		return newName(strings.TrimSuffix(qname, "."), n)
	}
	return out
}

// Has reports whether qname is in the cache.
func (n *Names) Has(qname string) bool {
	var out bool
	_ = n.loop.call(func() { out = n.has(qname) })
	return out
}

// DiscoveredNames returns the names that currently hold records, in the
// order they entered the cache.
func (n *Names) DiscoveredNames() []*Name {
	var out []*Name
	_ = n.loop.call(func() { out = n.discoveredNames() })
	return out
}

// --- loop-side ---

func (n *Names) has(qname string) bool {
	return n.names.has(strings.ToLower(strings.TrimSuffix(qname, ".")))
}

func (n *Names) maybeGet(qname string) *Name {
	nm, _ := n.names.get(strings.ToLower(strings.TrimSuffix(qname, ".")))
	return nm
}

// get returns the name, creating it and replaying any staged A/AAAA for
// it. Mirrors matter.js 0ceb202d DnssdNames.ts:329-358.
func (n *Names) get(qname string) *Name {
	qname = strings.TrimSuffix(qname, ".")
	if nm := n.maybeGet(qname); nm != nil {
		return nm
	}
	nm := newName(qname, n)
	key := strings.ToLower(qname)
	n.names.set(key, nm)

	if staged, ok := n.stagedIPs.get(key); ok {
		n.stagedIPs.del(key)
		now := n.clock.Now()
		for _, s := range staged {
			// Keep the original receive time so expiry counts from the
			// real discovery, not the replay.
			if now.Sub(s.receivedAt) < n.graced(s.record.TTL) {
				nm.installRecord(s.record, s.sourceIntf, s.receivedAt)
			}
		}
		if nm.isDiscovered() {
			if n.currentBatch != nil {
				*n.currentBatch = append(*n.currentBatch, nm)
			} else {
				n.emitDiscovered(nm)
			}
		}
	}
	return nm
}

func (n *Names) graced(ttl time.Duration) time.Duration {
	return time.Duration(float64(ttl) * n.ttlGraceFactor)
}

func (n *Names) discoveredNames() []*Name {
	var out []*Name
	for _, nm := range n.names.values() {
		if nm.isDiscovered() {
			out = append(out, nm)
		}
	}
	return out
}

func (n *Names) deleteName(nm *Name) {
	key := strings.ToLower(nm.qname)
	if known, ok := n.names.get(key); ok && known == nm {
		n.names.del(key)
	}
}

func (n *Names) emitDiscovered(nm *Name) {
	for _, o := range slices.Clone(n.discovered) {
		o.fn(nm)
	}
}

func (n *Names) accepted(r *Record) bool {
	if len(n.filters) == 0 {
		return true
	}
	for _, f := range n.filters {
		if f.accept(r) {
			return true
		}
	}
	return false
}

// handleMessage folds one received packet into the cache and announces
// the names it discovered once the whole packet is in. Mirrors matter.js
// 0ceb202d DnssdNames.ts:151-165.
func (n *Names) handleMessage(in mdns.QueryResponse) {
	msg, err := mdns.ParseDNSMessage(in.Msg)
	if err != nil {
		return
	}
	var newly []*Name
	func() {
		defer func() { n.currentBatch = nil }()
		newly = n.processMessage(msg, n.interfaceName(in.IfIndex))
	}()
	for _, nm := range newly {
		if nm.isDiscovered() {
			n.emitDiscovered(nm)
		}
	}
}

// processMessage. Mirrors matter.js 0ceb202d DnssdNames.ts:167-314.
func (n *Names) processMessage(msg mdns.DNSMessage, sourceIntf string) []*Name {
	// A query's known answers are the querier's cache, maybe from another
	// link; they are never cached here (:168-172).
	if !msg.Response {
		return nil
	}
	wire := slices.Concat(msg.Answers, msg.Additional)
	records := make([]*Record, len(wire))
	for i, r := range wire {
		records[i] = recordOf(r)
	}
	consumed := make([]bool, len(records))
	packetAt := n.clock.Now()
	newly := []*Name{}
	n.currentBatch = &newly
	packetRelevant := false

	handle := func(i int) {
		consumed[i] = true
		rec := records[i]
		nm := n.get(rec.Name)
		if rec.TTL != 0 {
			if rec.TTL < n.minTTL {
				raised := *rec
				raised.TTL = n.minTTL
				rec = &raised
			}
			was := nm.isDiscovered()
			if nm.installRecord(rec, sourceIntf, packetAt) {
				packetRelevant = true
			}
			if !was && nm.isDiscovered() {
				newly = append(newly, nm)
			}
		} else {
			packetRelevant = true
			// A goodbye takes effect after the eviction delay so a host
			// that reboots and re-announces keeps its records (:208-210).
			nm.expireRecord(rec)
		}
	}

	// Records the filters accept (:214-230).
	for i, r := range records {
		if n.accepted(r) {
			handle(i)
		}
	}

	// Records of names already in the cache, e.g. an SRV target; repeat
	// until a pass handles nothing new (:232-244).
	for {
		before := countFalse(consumed)
		for i, r := range records {
			if consumed[i] || !n.has(r.Name) {
				continue
			}
			handle(i)
		}
		if countFalse(consumed) >= before {
			break
		}
	}

	n.unstageGoodbyes(records, consumed)
	if packetRelevant {
		n.stageAddresses(records, consumed, packetAt, sourceIntf)
	}
	return newly
}

// unstageGoodbyes honours a goodbye for a staged host regardless of the
// packet's relevance: it only touches entries admitted under the gate.
// Mirrors matter.js 0ceb202d DnssdNames.ts:246-271.
func (n *Names) unstageGoodbyes(records []*Record, consumed []bool) {
	for i, r := range records {
		if consumed[i] || (r.Type != TypeA && r.Type != TypeAAAA) || r.TTL != 0 {
			continue
		}
		key := strings.ToLower(r.Name)
		if n.names.has(key) {
			continue
		}
		staged, ok := n.stagedIPs.get(key)
		if !ok {
			continue
		}
		remaining := slices.DeleteFunc(slices.Clone(staged), func(s stagedIP) bool {
			return s.record.Type == r.Type && s.record.ipString() == r.ipString()
		})
		if len(remaining) == 0 {
			n.stagedIPs.del(key)
		} else {
			n.stagedIPs.set(key, remaining)
		}
	}
}

// stageAddresses stages A/AAAA for hosts not yet in the cache, replayed
// when a later SRV names the host; only called for a relevant packet.
// Mirrors matter.js 0ceb202d DnssdNames.ts:273-311.
func (n *Names) stageAddresses(records []*Record, consumed []bool, packetAt time.Time, sourceIntf string) {
	for i, r := range records {
		if consumed[i] || (r.Type != TypeA && r.Type != TypeAAAA) || r.TTL == 0 {
			continue
		}
		key := strings.ToLower(r.Name)
		if n.names.has(key) {
			continue
		}
		rec := r
		if rec.TTL < n.minTTL {
			raised := *rec
			raised.TTL = n.minTTL
			rec = &raised
		}
		staged, _ := n.stagedIPs.get(key)
		if rec.CacheFlush {
			supersededBefore := packetAt.Add(-n.evictionDelay)
			staged = slices.DeleteFunc(slices.Clone(staged), func(s stagedIP) bool {
				return s.record.Type == rec.Type && s.receivedAt.Before(supersededBefore)
			})
		}
		entry := stagedIP{record: rec, receivedAt: packetAt, sourceIntf: sourceIntf}
		if idx := slices.IndexFunc(staged, func(s stagedIP) bool {
			return s.record.Type == rec.Type && s.record.ipString() == rec.ipString()
		}); idx >= 0 {
			staged[idx] = entry
		} else {
			staged = append(staged, entry)
		}
		// Delete and set moves the host to the tail, so pruning evicts
		// the least recently touched first.
		n.stagedIPs.del(key)
		n.stagedIPs.set(key, staged)
	}
}

func countFalse(b []bool) int {
	c := 0
	for _, v := range b {
		if !v {
			c++
		}
	}
	return c
}

// pruneStagedIPs drops staged addresses past their graced TTL and, past
// the high-water mark, the least recently touched hosts down to the low
// one. Mirrors matter.js 0ceb202d DnssdNames.ts:125-149.
func (n *Names) pruneStagedIPs() {
	if n.stagedIPs.len() == 0 {
		return
	}
	now := n.clock.Now()
	for _, key := range n.stagedIPs.snapshotKeys() {
		staged, _ := n.stagedIPs.get(key)
		live := slices.DeleteFunc(slices.Clone(staged), func(s stagedIP) bool {
			return now.Sub(s.receivedAt) >= n.graced(s.record.TTL)
		})
		switch {
		case len(live) == 0:
			n.stagedIPs.del(key)
		case len(live) != len(staged):
			n.stagedIPs.set(key, live)
		}
	}
	if n.stagedIPs.len() > stagedIPMaxHosts {
		for _, key := range n.stagedIPs.snapshotKeys() {
			if n.stagedIPs.len() <= stagedIPMinHosts {
				break
			}
			n.stagedIPs.del(key)
		}
	}
}

func (n *Names) armPrune() {
	n.pruneTimer = n.clock.AfterFunc(stagedIPPruneInterval, func() {
		n.loop.post(func() {
			if n.closed {
				return
			}
			n.pruneStagedIPs()
			n.armPrune()
		})
	})
}

// registerForExpiration schedules rec's deletion at its ExpiresAt
// (matter.js's expiration Scheduler, DnssdNames.ts:85-95).
func (n *Names) registerForExpiration(rec *Record) {
	n.expiring[rec] = struct{}{}
	if n.expiryTimer == nil || rec.ExpiresAt.Before(n.expiryAt) {
		n.armExpiry(rec.ExpiresAt)
	}
}

func (n *Names) unregisterForExpiration(rec *Record) {
	delete(n.expiring, rec)
}

func (n *Names) armExpiry(at time.Time) {
	if n.closed {
		return
	}
	if n.expiryTimer != nil {
		n.expiryTimer.Stop()
	}
	n.expiryAt = at
	n.expiryTimer = n.clock.AfterFunc(max(at.Sub(n.clock.Now()), 0), func() {
		n.loop.post(n.expireDue)
	})
}

// expireDue deletes every record whose time has come and re-arms the timer
// for the next.
func (n *Names) expireDue() {
	if n.closed {
		return
	}
	n.expiryTimer = nil
	now := n.clock.Now()
	var due []*Record
	for rec := range n.expiring {
		if !rec.ExpiresAt.After(now) {
			due = append(due, rec)
		}
	}
	slices.SortFunc(due, func(a, b *Record) int { return a.ExpiresAt.Compare(b.ExpiresAt) })
	for _, rec := range due {
		if _, still := n.expiring[rec]; !still {
			continue
		}
		delete(n.expiring, rec)
		if nm := n.maybeGet(rec.Name); nm != nil {
			nm.deleteRecord(rec)
		}
	}
	var next time.Time
	for rec := range n.expiring {
		if next.IsZero() || rec.ExpiresAt.Before(next) {
			next = rec.ExpiresAt
		}
	}
	if !next.IsZero() {
		n.armExpiry(next)
	}
}

// shutdown runs on the loop when Close is called.
func (n *Names) shutdown() {
	n.closed = true
	if n.expiryTimer != nil {
		n.expiryTimer.Stop()
	}
	if n.pruneTimer != nil {
		n.pruneTimer.Stop()
	}
	n.stagedIPs.clear()
	n.discovered = nil
	n.expiring = make(map[*Record]struct{})
	n.names.clear()
}
