// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/mdns"
)

// Scanner constants. Mirrors matter.js 0ceb202d
// packages/protocol/src/mdns/CommissionableMdnsScanner.ts:69-75.
const (
	// speculativeTargetTTL is how long a PTR target that never got its
	// SRV/TXT is solicited before it is forgotten.
	speculativeTargetTTL = time.Hour
	// speculativeCleanupInterval is how often forgotten targets are pruned.
	speculativeCleanupInterval = 30 * time.Minute
	// speculativeTargetMax bounds the targets solicited at once.
	speculativeTargetMax = 50
	// CommissioningRetryInterval caps a scanner discovery's retry interval
	// so queries stay dense inside a commissioning window.
	CommissioningRetryInterval = 30 * time.Second
	// maxDiscriminator is the largest long discriminator (12 bits).
	maxDiscriminator = 0xFFF
	maxUint8         = 0xFF
)

// DNS-SD service names. Mirrors matter.js 0ceb202d
// packages/protocol/src/mdns/MdnsConsts.ts:11-12.
const (
	commissionableServiceQName = "_matterc._udp.local"
	commissionerServiceQName   = "_matterd._udp.local"
)

type identifierKind int

const (
	identifyAny identifierKind = iota
	identifyInstance
	identifyLongDiscriminator
	identifyShortDiscriminator
	identifyVendor
	identifyDeviceType
	identifyProduct
)

// Identifier selects the commissionable nodes a [Scanner] looks for,
// matter.js CommissionableDeviceIdentifiers
// (packages/protocol/src/common/Scanner.ts:237-265). The zero value is
// [AnyCommissionable]. Identifiers compare with ==, which is how
// [Scanner.Cancel] matches a running search.
type Identifier struct {
	kind       identifierKind
	instanceID string
	long       uint16
	short      uint8
	vendor     uint16
	product    uint16
	hasProduct bool
	deviceType uint32
}

// ByInstanceID selects the node with this instance name (the first label
// of its `_matterc._udp` name).
func ByInstanceID(id string) Identifier { return Identifier{kind: identifyInstance, instanceID: id} }

// ByLongDiscriminator selects nodes announcing this 12-bit discriminator.
func ByLongDiscriminator(d uint16) Identifier {
	return Identifier{kind: identifyLongDiscriminator, long: d}
}

// ByShortDiscriminator selects nodes whose discriminator's upper four bits
// are d.
func ByShortDiscriminator(d uint8) Identifier {
	return Identifier{kind: identifyShortDiscriminator, short: d}
}

// ByVendor selects nodes of this vendor.
func ByVendor(vendor uint16) Identifier { return Identifier{kind: identifyVendor, vendor: vendor} }

// ByVendorProduct selects nodes of this vendor and product.
func ByVendorProduct(vendor, product uint16) Identifier {
	return Identifier{kind: identifyVendor, vendor: vendor, product: product, hasProduct: true}
}

// ByDeviceType selects nodes announcing this device type.
func ByDeviceType(deviceType uint32) Identifier {
	return Identifier{kind: identifyDeviceType, deviceType: deviceType}
}

// ByProduct selects nodes of this product, whatever the vendor.
func ByProduct(product uint16) Identifier {
	return Identifier{kind: identifyProduct, product: product}
}

// AnyCommissionable selects every node in commissioning mode 1 or 2.
func AnyCommissionable() Identifier { return Identifier{} }

// queryQNames are the names a search queries: always
// `_matterc._udp.local`, plus the instance name or the subtype the
// identifier selects by — `_CM` for a product and for "any". Mirrors
// matter.js 0ceb202d CommissionableMdnsScanner.ts:574-593 with the names of
// MdnsConsts.ts:38-45.
func queryQNames(id Identifier) []string {
	base := []string{commissionableServiceQName}
	sub := func(label string) string { return label + "._sub." + commissionableServiceQName }
	switch id.kind {
	case identifyInstance:
		return append(base, id.instanceID+"."+commissionableServiceQName)
	case identifyLongDiscriminator:
		return append(base, sub("_L"+strconv.Itoa(int(id.long))))
	case identifyShortDiscriminator:
		return append(base, sub("_S"+strconv.Itoa(int(id.short))))
	case identifyVendor:
		return append(base, sub("_V"+strconv.Itoa(int(id.vendor))))
	case identifyDeviceType:
		return append(base, sub("_T"+strconv.FormatUint(uint64(id.deviceType), 10)))
	case identifyAny, identifyProduct:
	}
	return append(base, sub("_CM"))
}

// Device is a commissionable node as the scanner reports it, matter.js
// CommissionableDevice (packages/protocol/src/common/Scanner.ts:203-211).
type Device struct {
	TXTData
	// InstanceID is the first label of the node's `_matterc._udp` name.
	InstanceID string
	// D is the long discriminator.
	D uint16
	// CM is the commissioning mode.
	CM uint8
	// Hostname is the host the node's SRV records agree on, without its
	// domain; "" while there is none or they disagree.
	Hostname string
	// Addresses are the node's addresses, most desirable first.
	Addresses []Address
}

func (d Device) clone() Device {
	d.Addresses = slices.Clone(d.Addresses)
	return d
}

// matches reports whether d is selected by id. Mirrors matter.js 0ceb202d
// CommissionableMdnsScanner.ts:533-561.
func (id Identifier) matches(d *Device) bool {
	vp := func() (vendor, product string, ok bool) {
		if d.VP == nil {
			return "", "", false
		}
		parts := strings.Split(*d.VP, "+")
		if len(parts) > 1 {
			return parts[0], parts[1], true
		}
		return parts[0], "", true
	}
	switch id.kind {
	case identifyInstance:
		return d.InstanceID == id.instanceID
	case identifyLongDiscriminator:
		return d.D == id.long
	case identifyShortDiscriminator:
		return (d.D>>8)&0x0F == uint16(id.short)
	case identifyVendor:
		v, p, ok := vp()
		if !ok {
			return false
		}
		match := v == strconv.Itoa(int(id.vendor))
		if id.hasProduct {
			return match && p == strconv.Itoa(int(id.product))
		}
		return match
	case identifyDeviceType:
		return d.DT != nil && *d.DT == id.deviceType
	case identifyProduct:
		_, p, ok := vp()
		return ok && p == strconv.Itoa(int(id.product))
	case identifyAny:
	}
	return d.CM == 1 || d.CM == 2
}

// buildDevice makes a device of an instance name once its TXT carries a
// valid D (≤ 0xFFF) and CM (≤ 255); T defaults to no TCP and ICD to 0.
// Mirrors matter.js 0ceb202d CommissionableMdnsScanner.ts:470-496.
func buildDevice(nm *Name) (Device, bool) {
	p := nm.parameters()
	dv, _ := p.Get("D")
	cmv, _ := p.Get("CM")
	d, okD := ParseTxtDecimal(dv, maxDiscriminator)
	cm, okCM := ParseTxtDecimal(cmv, maxUint8)
	if !okD || !okCM {
		return Device{}, false
	}
	dd := ParseTXTData(p)
	if dd.T == nil {
		dd.T = &SupportedTransports{}
	}
	if dd.ICD == nil {
		var zero uint8
		dd.ICD = &zero
	}
	instance, _, _ := strings.Cut(nm.qname, ".")
	return Device{
		TXTData:    dd,
		InstanceID: instance,
		D:          narrow16(d),
		CM:         narrow8(cm),
		Hostname:   hostnameOf(nm),
	}, true
}

// hostnameOf is the single host the name's SRV records agree on, without
// its domain, or "" while they disagree. Mirrors matter.js 0ceb202d
// CommissionableMdnsScanner.ts:508-516.
func hostnameOf(nm *Name) string {
	var hosts []string
	for _, r := range nm.records.values() {
		if r.Type != TypeSRV {
			continue
		}
		h, _, _ := strings.Cut(r.SRV.Target, ".")
		if !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	if len(hosts) == 1 {
		return hosts[0]
	}
	return ""
}

type cachedDevice struct {
	device      Device
	service     *Service
	name        *Name
	observer    *nameObserver
	onAddresses *changeObserver
}

type waiter struct {
	identifier Identifier
	seen       map[string]bool
	push       func(Device)
	cancel     func()
}

// notify delivers a device the waiter has not seen and selects. Mirrors
// matter.js 0ceb202d CommissionableMdnsScanner.ts:188-194.
func (w *waiter) notify(d *Device) {
	if w.identifier.matches(d) && !w.seen[d.InstanceID] {
		w.seen[d.InstanceID] = true
		w.push(d.clone())
	}
}

type speculativeTarget struct {
	cancel    func()
	createdAt time.Time
}

// Scanner finds commissionable nodes by DNS-SD, matter.js
// CommissionableMdnsScanner
// (packages/protocol/src/mdns/CommissionableMdnsScanner.ts). It admits the
// `_matterc._udp` and `_matterd._udp` records to the cache, follows PTRs
// to instance names, builds a [Device] once an instance's TXT carries a
// valid D and CM, and reports it once it has an address.
type Scanner struct {
	names *Names

	// Loop-owned state below.
	filter              *ingressFilter
	discoveredObserver  *discoveredObserver
	cache               *omap[string, *cachedDevice]
	pending             map[*nameObserver]pendingDevice
	waiters             []*waiter
	speculative         *omap[string, *speculativeTarget]
	speculativeObserver *nameObserver
	cleanupTimer        stopper
	closed              bool
}

type pendingDevice struct {
	name    *Name
	service *Service
}

// NewScanner returns a scanner on names. Close it when done.
func NewScanner(names *Names) *Scanner {
	s := &Scanner{
		names:               names,
		cache:               newOmap[string, *cachedDevice](),
		pending:             make(map[*nameObserver]pendingDevice),
		speculative:         newOmap[string, *speculativeTarget](),
		speculativeObserver: &nameObserver{},
	}
	if err := names.loop.call(s.start); err != nil {
		s.closed = true
	}
	return s
}

// start. Mirrors matter.js 0ceb202d CommissionableMdnsScanner.ts:92-115.
func (s *Scanner) start() {
	s.filter = s.names.addFilter(isCommissionableRecord)
	s.discoveredObserver = s.names.onDiscovered(s.onDiscovered)
	s.armCleanup()
}

// isCommissionableRecord is the scanner's ingress filter: the
// `_matterc._udp.local` and `_matterd._udp.local` service names and every
// name under them. Mirrors matter.js 0ceb202d
// CommissionableMdnsScanner.ts:95-103.
func isCommissionableRecord(r *Record) bool {
	return isCommissionableName(r.Name)
}

func isCommissionableName(name string) bool {
	lower := strings.ToLower(name)
	return lower == commissionableServiceQName || strings.HasSuffix(lower, "."+commissionableServiceQName) ||
		lower == commissionerServiceQName || strings.HasSuffix(lower, "."+commissionerServiceQName)
}

func (s *Scanner) armCleanup() {
	s.cleanupTimer = s.names.clock.AfterFunc(speculativeCleanupInterval, func() {
		s.names.loop.post(func() {
			if s.closed {
				return
			}
			s.pruneSpeculativeTargets()
			s.armCleanup()
		})
	})
}

// deliveryQueue hands devices from the loop to a search's goroutine.
type deliveryQueue struct {
	mu     sync.Mutex
	items  []Device
	signal chan struct{}
}

func (q *deliveryQueue) push(d Device) {
	q.mu.Lock()
	q.items = append(q.items, d)
	q.mu.Unlock()
	select {
	case q.signal <- struct{}{}:
	default:
	}
}

func (q *deliveryQueue) drain() []Device {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.items
	q.items = nil
	return out
}

// FindCommissionable searches for the nodes id selects until ctx ends or
// [Scanner.Cancel] cancels the search, calling found (when not nil) for
// each node once, and returns every node it found. It first reports the
// cached matches that have an address; if found cancels the search there,
// no query is sent. Then it queries the network — `_matterc._udp.local`
// and the identifier's subtype or instance name, retrying at most every
// [CommissioningRetryInterval]. found runs on the caller's goroutine. The
// error is [ErrClosed] when the cache closes, nil otherwise. Mirrors
// matter.js 0ceb202d CommissionableMdnsScanner.ts:165-256
// (findCommissionableDevicesContinuously).
func (s *Scanner) FindCommissionable(ctx context.Context, id Identifier, found func(Device)) ([]Device, error) {
	q := &deliveryQueue{signal: make(chan struct{}, 1)}
	cancelled := make(chan struct{})
	var once sync.Once
	w := &waiter{identifier: id, seen: make(map[string]bool), push: q.push, cancel: func() { once.Do(func() { close(cancelled) }) }}
	result := []Device{}
	deliver := func() {
		items := q.drain()
		for i := range items {
			result = append(result, items[i])
			if found != nil {
				found(items[i])
			}
		}
	}
	removeWaiter := func() { _ = s.names.loop.call(func() { s.removeWaiter(w) }) }

	invokedCached := false
	//nolint:contextcheck // loop work is ctx-free; a status resolution it may start runs on the cache's own context
	if err := s.names.loop.call(func() { invokedCached = s.register(w) }); err != nil {
		return result, err
	}
	deliver()
	if invokedCached {
		select {
		case <-cancelled:
			removeWaiter()
			return result, nil
		case <-ctx.Done():
			removeWaiter()
			return result, nil
		default:
		}
	}

	var stop func()
	if err := s.names.loop.call(func() { stop = s.startDiscovery(id) }); err != nil {
		return result, err
	}
	var err error
wait:
	for {
		select {
		case <-q.signal:
			deliver()
		case <-ctx.Done():
			break wait
		case <-cancelled:
			break wait
		case <-s.names.loop.done:
			err = ErrClosed
			break wait
		}
	}
	_ = s.names.loop.call(func() {
		stop()
		s.removeWaiter(w)
	})
	deliver()
	return result, err
}

// Discovered returns the cached nodes id selects that have an address,
// without querying. Mirrors matter.js 0ceb202d
// CommissionableMdnsScanner.ts:258-262.
func (s *Scanner) Discovered(id Identifier) []Device {
	var out []Device
	_ = s.names.loop.call(func() {
		for _, c := range s.cache.values() {
			d := s.refresh(c)
			if id.matches(d) && len(d.Addresses) > 0 {
				out = append(out, d.clone())
			}
		}
	})
	return out
}

// Cancel ends every running search for id; each returns what it found.
// Mirrors matter.js 0ceb202d CommissionableMdnsScanner.ts:264-270.
func (s *Scanner) Cancel(id Identifier) {
	_ = s.names.loop.call(func() {
		for _, w := range s.waiters {
			if w.identifier == id {
				w.cancel()
			}
		}
	})
}

// Close ends every search, stops the speculative queries, withdraws the
// scanner's ingress filter and releases its services. Mirrors matter.js
// 0ceb202d CommissionableMdnsScanner.ts:117-138.
func (s *Scanner) Close() {
	_ = s.names.loop.call(s.close)
}

// --- loop-side ---

// register adds the waiter after picking up the names discovered before
// the scanner subscribed, and reports the cached matches; it reports
// whether any was delivered. Mirrors matter.js 0ceb202d
// CommissionableMdnsScanner.ts:171-216.
func (s *Scanner) register(w *waiter) bool {
	for _, nm := range s.names.discoveredNames() {
		s.onDiscovered(nm)
	}
	s.waiters = append(s.waiters, w)
	invoked := false
	for _, c := range s.cache.values() {
		d := s.refresh(c)
		if !w.identifier.matches(d) {
			continue
		}
		if len(d.Addresses) > 0 {
			w.seen[d.InstanceID] = true
			w.push(d.clone())
			invoked = true
		} else {
			// The cached A/AAAA expired since the node's last
			// announcement: ask again and deliver when they come.
			s.solicitAndArmAddresses(c)
		}
	}
	return invoked
}

func (s *Scanner) removeWaiter(w *waiter) {
	s.waiters = slices.DeleteFunc(s.waiters, func(x *waiter) bool { return x == w })
}

// startDiscovery queries the identifier's names for PTRs with the
// commissioning retry cap and returns the function that stops it. Mirrors
// matter.js 0ceb202d CommissionableMdnsScanner.ts:454-467.
func (s *Scanner) startDiscovery(id Identifier) func() {
	qnames := queryQNames(id)
	cancels := make([]func(), 0, len(qnames))
	for _, qname := range qnames {
		cancel, _ := s.names.solicitor.discover(
			&solicitation{name: s.names.get(qname), types: []mdns.DNSType{TypePTR}},
			&Retries{MaximumInterval: CommissioningRetryInterval},
		)
		cancels = append(cancels, cancel)
	}
	return func() {
		for _, c := range cancels {
			c()
		}
	}
}

func (s *Scanner) close() {
	if s.closed {
		return
	}
	s.closed = true
	for _, w := range s.waiters {
		w.cancel()
	}
	if s.cleanupTimer != nil {
		s.cleanupTimer.Stop()
	}
	for _, key := range s.speculative.snapshotKeys() {
		t, _ := s.speculative.get(key)
		t.cancel()
		if nm := s.names.maybeGet(key); nm != nil {
			nm.off(s.speculativeObserver)
		}
	}
	s.speculative.clear()
	s.names.deleteFilter(s.filter)
	s.names.offDiscovered(s.discoveredObserver)
	for obs, p := range s.pending {
		p.name.off(obs)
		p.service.close()
	}
	clear(s.pending)
	for _, c := range s.cache.values() {
		c.name.off(c.observer)
		if c.onAddresses != nil {
			c.service.offChanged(c.onAddresses)
		}
		c.service.close()
	}
	s.cache.clear()
}

// pruneSpeculativeTargets forgets targets solicited for longer than an
// hour. Mirrors matter.js 0ceb202d CommissionableMdnsScanner.ts:140-153.
func (s *Scanner) pruneSpeculativeTargets() {
	cutoff := s.names.clock.Now().Add(-speculativeTargetTTL)
	for _, key := range s.speculative.snapshotKeys() {
		t, _ := s.speculative.get(key)
		if t.createdAt.After(cutoff) {
			continue
		}
		s.speculative.del(key)
		t.cancel()
		if nm := s.names.maybeGet(key); nm != nil {
			nm.off(s.speculativeObserver)
		}
	}
}

func (s *Scanner) clearSpeculativeTarget(key string, nm *Name) {
	t, ok := s.speculative.get(key)
	if !ok {
		return
	}
	s.speculative.del(key)
	t.cancel()
	nm.off(s.speculativeObserver)
}

// onDiscovered follows a service-type name's PTRs to instance names,
// soliciting a target's missing SRV/TXT, and starts tracking an instance
// name. Mirrors matter.js 0ceb202d CommissionableMdnsScanner.ts:272-377.
func (s *Scanner) onDiscovered(nm *Name) {
	if s.closed || !isCommissionableName(nm.qname) {
		return
	}
	lower := strings.ToLower(nm.qname)
	if strings.HasPrefix(lower, "_") {
		s.followPointers(nm)
		return
	}

	s.clearSpeculativeTarget(lower, nm)
	if s.cache.has(lower) {
		return
	}
	// An instance already waiting for a valid TXT keeps its one Service
	// and observer. matter.js (:344-376) opens another pair per search
	// start (:173-175) and never releases the earlier one once the TXT
	// arrives — see notes/parity/by_design.md
	// BD-Matter-ScannerPendingObserverOnce.
	for _, p := range s.pending {
		if p.name == nm {
			return
		}
	}

	svc := newService(s.names, nm.qname)
	if d, ok := buildDevice(nm); ok {
		s.cacheDevice(lower, d, svc, nm)
		return
	}

	// Ask for what is missing now rather than at the next PTR retry.
	types := []mdns.DNSType{TypeSRV, TypeTXT}
	for _, r := range nm.records.values() {
		if r.Type == TypeSRV {
			types = []mdns.DNSType{TypeTXT}
			break
		}
	}
	s.names.solicitor.solicit(&solicitation{name: nm, types: types})

	// Watch the name until the device can be built or the name goes.
	obs := &nameObserver{}
	obs.fn = func(c Changes) {
		if !c.Name.isDiscovered() {
			delete(s.pending, obs)
			nm.off(obs)
			svc.close()
			return
		}
		if d, ok := buildDevice(c.Name); ok {
			delete(s.pending, obs)
			nm.off(obs)
			s.cacheDevice(lower, d, svc, nm)
		}
	}
	s.pending[obs] = pendingDevice{name: nm, service: svc}
	nm.on(obs)
}

// followPointers solicits the SRV/TXT a PTR target lacks, at most
// speculativeTargetMax targets at a time. Mirrors matter.js 0ceb202d
// CommissionableMdnsScanner.ts:281-339.
func (s *Scanner) followPointers(nm *Name) {
	for _, r := range nm.records.values() {
		if r.Type != TypePTR || strings.HasPrefix(r.PTR, "_") {
			continue
		}
		target := s.names.get(r.PTR)
		hasSRV, hasTXT := false, false
		for _, tr := range target.records.values() {
			switch tr.Type {
			case TypeSRV:
				hasSRV = true
			case TypeTXT:
				hasTXT = true
			default:
			}
		}
		var types []mdns.DNSType
		if !hasSRV {
			types = append(types, TypeSRV)
		}
		if !hasTXT {
			types = append(types, TypeTXT)
		}
		if len(types) == 0 {
			continue
		}
		key := strings.ToLower(r.PTR)
		if s.speculative.has(key) || s.speculative.len() >= speculativeTargetMax {
			continue
		}
		target.on(s.speculativeObserver)
		cancel, _ := s.names.solicitor.discover(
			&solicitation{name: target, types: types},
			&Retries{MaximumInterval: CommissioningRetryInterval},
		)
		s.speculative.set(key, &speculativeTarget{cancel: cancel, createdAt: s.names.clock.Now()})
	}
}

// cacheDevice caches a built device and delivers it once it has an
// address. Mirrors matter.js 0ceb202d CommissionableMdnsScanner.ts:379-405.
func (s *Scanner) cacheDevice(lower string, d Device, svc *Service, nm *Name) {
	c := &cachedDevice{device: d, service: svc, name: nm}
	c.observer = &nameObserver{fn: func(ch Changes) {
		if ch.Name.isDiscovered() {
			return
		}
		cached, ok := s.cache.get(lower)
		if !ok {
			return
		}
		s.cache.del(lower)
		nm.off(cached.observer)
		if cached.onAddresses != nil {
			svc.offChanged(cached.onAddresses)
			cached.onAddresses = nil
		}
		cached.service.close()
	}}
	s.cache.set(lower, c)
	nm.on(c.observer)
	if !s.deliverIfResolved(c) {
		s.solicitAndArmAddresses(c)
	}
}

// solicitAndArmAddresses asks for the A/AAAA of every SRV target and
// delivers the device once an address arrives; arming is idempotent.
// Mirrors matter.js 0ceb202d CommissionableMdnsScanner.ts:414-441.
func (s *Scanner) solicitAndArmAddresses(c *cachedDevice) {
	for _, r := range c.name.records.values() {
		if r.Type != TypeSRV {
			continue
		}
		s.names.solicitor.solicit(&solicitation{name: s.names.get(r.SRV.Target), types: []mdns.DNSType{TypeA, TypeAAAA}})
	}
	if c.onAddresses != nil {
		return
	}
	var obs *changeObserver
	obs = c.service.onChanged(func() {
		if s.deliverIfResolved(c) {
			c.service.offChanged(obs)
			c.onAddresses = nil
		}
	})
	c.onAddresses = obs
}

// deliverIfResolved hands the device to every waiter once it has an
// address. Mirrors matter.js 0ceb202d CommissionableMdnsScanner.ts:443-452.
func (s *Scanner) deliverIfResolved(c *cachedDevice) bool {
	d := s.refresh(c)
	if len(d.Addresses) == 0 {
		return false
	}
	for _, w := range slices.Clone(s.waiters) {
		w.notify(d)
	}
	return true
}

// refresh re-reads the addresses and the hostname, which may change after
// the device was cached. Mirrors matter.js 0ceb202d
// CommissionableMdnsScanner.ts:525-531.
func (s *Scanner) refresh(c *cachedDevice) *Device {
	c.device.Addresses = c.service.orderedAddresses()
	c.device.Hostname = hostnameOf(c.name)
	return &c.device
}
