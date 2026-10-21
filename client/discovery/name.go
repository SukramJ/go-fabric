// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"bytes"
	"encoding/hex"
	"log/slog"
	"math"
	"net/netip"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SukramJ/go-fabric/mdns"
)

// DefaultTTLGraceFactor is the factor applied to a record's TTL when its
// expiry is computed, so timing jitter does not expire it early and cause
// spurious queries. Mirrors matter.js 0ceb202d
// packages/general/src/net/dns-sd/DnssdName.ts:25 DEFAULT_TTL_GRACE_FACTOR.
const DefaultTTLGraceFactor = 1.05

// Record types a [Record] carries a value for.
const (
	TypeA    = mdns.DNSTypeA
	TypePTR  = mdns.DNSTypePTR
	TypeTXT  = mdns.DNSTypeTXT
	TypeAAAA = mdns.DNSTypeAAAA
	TypeSRV  = mdns.DNSTypeSRV
)

// Record is one cached DNS record: matter.js DnssdName.Record, the decoded
// record plus the time it was installed and the time it expires. A Record
// handed out by this package is a copy; its TXT slices must not be
// modified.
type Record struct {
	// Name is the owner name, without the trailing dot.
	Name string
	Type mdns.DNSType
	// TTL is the record's lifetime as installed: the advertised TTL, raised
	// to the cache's minimum, or the eviction delay once superseded.
	TTL        time.Duration
	CacheFlush bool
	// IP is the address of an A or AAAA record.
	IP netip.Addr
	// PTR is the target of a PTR record.
	PTR string
	// SRV is the value of an SRV record.
	SRV mdns.DNSSRV
	// TXT is the entries of a TXT record.
	TXT [][]byte
	// InstalledAt is when the record entered the cache (the packet's
	// receive time).
	InstalledAt time.Time
	// ExpiresAt is InstalledAt + TTL × the grace factor.
	ExpiresAt time.Time
	// SourceIntf is the name of the interface an AAAA record arrived on,
	// "" for every other record (matter.js keeps it for AAAA only, to zone
	// an fe80:: address).
	SourceIntf string
}

func recordOf(r mdns.DNSRecord) *Record {
	return &Record{
		Name:       r.Name,
		Type:       r.Type,
		TTL:        time.Duration(r.TTL) * time.Second,
		CacheFlush: r.CacheFlush,
		IP:         r.IP,
		PTR:        r.PTR,
		SRV:        r.SRV,
		TXT:        r.TXT,
	}
}

// dnsRecord converts back to the wire shape, for a query's known answers.
func (r *Record) dnsRecord() mdns.DNSRecord {
	return mdns.DNSRecord{
		Name:       r.Name,
		Type:       r.Type,
		TTL:        uint32(min(r.TTL/time.Second, math.MaxUint32)), //nolint:gosec // clamped to the uint32 range
		CacheFlush: r.CacheFlush,
		IP:         r.IP,
		PTR:        r.PTR,
		SRV:        r.SRV,
		TXT:        r.TXT,
	}
}

// ipString is the value matter.js keys and compares an A/AAAA record by.
func (r *Record) ipString() string { return r.IP.String() }

// isUnique reports whether records of type t form a unique set that a
// cache-flush record replaces. Mirrors matter.js 0ceb202d
// packages/general/src/codec/DnsCodec.ts:198-207 (SRV, TXT, A, AAAA).
func isUnique(t mdns.DNSType) bool {
	switch t {
	case TypeSRV, TypeTXT, TypeA, TypeAAAA:
		return true
	case TypePTR:
		return false
	}
	return false
}

// recordKey is the identity of a record within its name: A, AAAA and PTR
// by type and value, SRV by type and target:port, TXT by type and its
// entries (hex, sorted). A record of another type has no key and is not
// cached. Mirrors matter.js 0ceb202d
// packages/general/src/net/dns-sd/DnssdName.ts:352-377 (keyOf).
func recordKey(r *Record) string {
	t := strconv.Itoa(int(r.Type))
	switch r.Type {
	case TypeA, TypeAAAA:
		if !r.IP.IsValid() {
			return ""
		}
		return t + " " + r.ipString()
	case TypePTR:
		return t + " " + r.PTR
	case TypeSRV:
		return t + " " + r.SRV.Target + ":" + strconv.Itoa(int(r.SRV.Port))
	case TypeTXT:
		keys := make([]string, 0, len(r.TXT))
		for _, e := range r.TXT {
			keys = append(keys, hex.EncodeToString(e))
		}
		sort.Strings(keys)
		return t + " " + strings.Join(keys, " ")
	}
	return ""
}

// isAvailableService reports whether an SRV record designates a reachable
// service: a target of "" or "." (RFC 2782: not available here) or a port
// of 0 (RFC 6763 §8: a placeholder) does not. Mirrors matter.js 0ceb202d
// packages/general/src/net/dns-sd/DnssdName.ts:344-350; the port is 16-bit
// on the wire, so its "> 65535" arm cannot occur here.
func isAvailableService(srv mdns.DNSSRV) bool {
	if srv.Target == "" || srv.Target == "." {
		return false
	}
	return srv.Port > 0
}

// Parameters are the key/value pairs of a service's TXT records, matter.js
// DnssdParameters (packages/general/src/net/dns-sd/DnssdParameters.ts): the
// raw bytes per key; [Parameters.Get] decodes them as UTF-8.
type Parameters map[string][]byte

// Get returns the value of key decoded as UTF-8 (an invalid byte becomes
// U+FFFD) and whether the key is present. The key matches regardless of
// case, an exact match first. Mirrors matter.js
// packages/general/src/net/dns-sd/DnssdParameters.ts:36-46 (get/raw fold
// the key to lower case).
func (p Parameters) Get(key string) (string, bool) {
	v, ok := p[key]
	if !ok {
		for k, kv := range p {
			if strings.EqualFold(k, key) {
				v, ok = kv, true
				break
			}
		}
		if !ok {
			return "", false
		}
	}
	return decodeUTF8(v), true
}

// signature is the canonical form matter.js compares parameter sets by:
// the sorted keys with their hex values, NUL-separated. Mirrors matter.js
// 0ceb202d packages/general/src/net/dns-sd/IpService.ts:381-386.
func (p Parameters) signature() string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+hex.EncodeToString(p[k]))
	}
	return strings.Join(parts, "\x00")
}

func decodeUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	for len(b) > 0 {
		r, n := utf8.DecodeRune(b)
		if r == utf8.RuneError && n == 1 {
			sb.WriteRune(utf8.RuneError)
		} else {
			sb.Write(b[:n])
		}
		b = b[n:]
	}
	return sb.String()
}

// Changes is one coalesced notification of a [Name]'s record changes,
// matter.js DnssdName.Changes.
type Changes struct {
	Name    *Name
	Updated []*Record
	Deleted []*Record
}

// nameObserver is an observer of a name. Its identity keeps the name
// alive; fn may be nil (matter.js's null observer).
type nameObserver struct {
	fn func(Changes)
}

type pendingChange struct {
	deleted bool
	record  *Record
}

// Name holds the records of one DNS-SD name, matter.js DnssdName
// (packages/general/src/net/dns-sd/DnssdName.ts). A Name exists while it
// has an unexpired record or an observer; [Names.Get] creates one.
type Name struct {
	qname string
	names *Names

	records                    *omap[string, *Record]
	changes                    *omap[string, pendingChange]
	notified                   bool
	maybeDeleting              bool
	params                     Parameters
	dependencies               map[string]*Name
	observers                  []*nameObserver
	nullObserver               *nameObserver
	unavailableServiceReported bool
}

func newName(qname string, names *Names) *Name {
	return &Name{
		qname:   qname,
		names:   names,
		records: newOmap[string, *Record](),
		changes: newOmap[string, pendingChange](),
	}
}

// QName returns the name as first seen, without the trailing dot.
func (n *Name) QName() string { return n.qname }

// Records returns a snapshot of the name's records in installation order.
func (n *Name) Records() []Record {
	var out []Record
	_ = n.names.loop.call(func() {
		for _, r := range n.records.values() {
			out = append(out, *r)
		}
	})
	return out
}

// Parameters returns the TXT parameters of the name.
func (n *Name) Parameters() Parameters {
	var out Parameters
	_ = n.names.loop.call(func() { out = n.parameters() })
	return out
}

// IsDiscovered reports whether the name holds at least one record.
func (n *Name) IsDiscovered() bool {
	var out bool
	_ = n.names.loop.call(func() { out = n.isDiscovered() })
	return out
}

// --- loop-side ---

func (n *Name) isDiscovered() bool { return n.records.len() > 0 }

func (n *Name) isObserved() bool { return len(n.observers) > 0 }

func (n *Name) on(o *nameObserver) {
	if !slices.Contains(n.observers, o) {
		n.observers = append(n.observers, o)
	}
}

// off removes an observer and deletes the name if nothing keeps it.
// Mirrors matter.js 0ceb202d DnssdName.ts:58-61.
func (n *Name) off(o *nameObserver) {
	n.observers = slices.DeleteFunc(n.observers, func(x *nameObserver) bool { return x == o })
	n.deleteIfUnused()
}

// parameters builds the TXT parameters: newest TXT record first, entries in
// wire order, an empty entry or an entry with an empty key skipped, split
// at the first "=", the first occurrence of a key winning, a key without
// "=" having an empty value. Mirrors matter.js 0ceb202d
// packages/general/src/net/dns-sd/DnssdName.ts:76-112.
func (n *Name) parameters() Parameters {
	if n.params != nil {
		return n.params
	}
	var txts []*Record
	for _, r := range n.records.values() {
		if r.Type == TypeTXT {
			txts = append(txts, r)
		}
	}
	slices.SortStableFunc(txts, func(a, b *Record) int { return b.InstalledAt.Compare(a.InstalledAt) })
	raw := make(Parameters)
	// The first occurrence of a key wins regardless of its case. Mirrors
	// matter.js packages/general/src/net/dns-sd/DnssdParameters.ts:27-34.
	seen := make(map[string]struct{})
	for _, r := range txts {
		for _, entry := range r.TXT {
			if len(entry) == 0 {
				continue
			}
			eq := bytes.IndexByte(entry, '=')
			if eq == 0 {
				continue
			}
			var key string
			var value []byte
			if eq < 0 {
				key, value = string(entry), []byte{}
			} else {
				key, value = string(entry[:eq]), entry[eq+1:]
			}
			folded := strings.ToLower(key)
			if _, dup := seen[folded]; dup {
				continue
			}
			seen[folded] = struct{}{}
			raw[key] = value
		}
	}
	n.params = raw
	return raw
}

// installRecord caches rec and reports whether it was kept. Mirrors
// matter.js 0ceb202d packages/general/src/net/dns-sd/DnssdName.ts:118-182.
func (n *Name) installRecord(rec *Record, sourceIntf string, at time.Time) bool {
	key := recordKey(rec)
	if key == "" {
		n.deleteIfUnused()
		return false
	}
	if rec.Type == TypeSRV && !isAvailableService(rec.SRV) {
		if !n.unavailableServiceReported {
			n.unavailableServiceReported = true
			n.names.logger.Info("matter.discovery.srv_unavailable",
				slog.String("name", n.qname),
				slog.String("target", rec.SRV.Target),
				slog.Int("port", int(rec.SRV.Port)))
		}
		n.deleteIfUnused()
		return false
	}

	if old, ok := n.records.get(key); ok {
		n.names.unregisterForExpiration(old)
	}

	// Retire only once the record is kept; the copy it replaces is
	// excluded by key (DnssdName.ts:146-150).
	if rec.CacheFlush && isUnique(rec.Type) {
		n.expireOthersSupersededBy(rec, at, key)
	}

	installed := *rec
	installed.InstalledAt = at
	installed.ExpiresAt = at.Add(time.Duration(math.Round(float64(rec.TTL.Milliseconds())*n.names.ttlGraceFactor)) * time.Millisecond)
	installed.SourceIntf = ""
	if rec.Type == TypeAAAA {
		installed.SourceIntf = sourceIntf
	}
	n.records.set(key, &installed)

	if rec.Type == TypeTXT {
		n.params = nil
	}

	n.names.registerForExpiration(&installed)

	// Keep the target host alive as long as an SRV references it.
	if rec.Type == TypeSRV {
		if _, ok := n.dependencies[key]; !ok {
			dep := n.names.get(rec.SRV.Target)
			if n.nullObserver == nil {
				n.nullObserver = &nameObserver{}
			}
			dep.on(n.nullObserver)
			if n.dependencies == nil {
				n.dependencies = make(map[string]*Name)
			}
			n.dependencies[key] = dep
		}
	}

	n.notify(false, key, &installed)
	return true
}

// installedFor returns the record installed under rec's key.
func (n *Name) installedFor(rec *Record) (string, *Record) {
	if key := recordKey(rec); key != "" {
		if installed, ok := n.records.get(key); ok {
			return key, installed
		}
	}
	n.deleteIfUnused()
	return "", nil
}

// expireRecord shortens a record's remaining life to the eviction delay;
// it never extends it. Mirrors matter.js 0ceb202d DnssdName.ts:202-209.
func (n *Name) expireRecord(rec *Record) {
	key, installed := n.installedFor(rec)
	if installed == nil {
		return
	}
	n.retire(key, installed)
}

// expireOthersSupersededBy retires the records of rec's type that arrived
// before the eviction-delay window rec's set arrived in. Mirrors matter.js
// 0ceb202d DnssdName.ts:216-223.
func (n *Name) expireOthersSupersededBy(rec *Record, at time.Time, exceptKey string) {
	before := at.Add(-n.names.evictionDelay)
	for _, key := range n.records.snapshotKeys() {
		installed, _ := n.records.get(key)
		if key != exceptKey && installed.Type == rec.Type && installed.InstalledAt.Before(before) {
			n.retire(key, installed)
		}
	}
}

// retire. Mirrors matter.js 0ceb202d DnssdName.ts:225-236.
func (n *Name) retire(key string, current *Record) {
	delay := n.names.evictionDelay
	expiresAt := n.names.clock.Now().Add(delay)
	if !current.ExpiresAt.After(expiresAt) {
		return
	}
	expiring := *current
	expiring.TTL = delay
	expiring.ExpiresAt = expiresAt
	n.names.registerForExpiration(&expiring)
	n.names.unregisterForExpiration(current)
	n.records.set(key, &expiring)
}

// deleteRecord. Mirrors matter.js 0ceb202d DnssdName.ts:238-266.
func (n *Name) deleteRecord(rec *Record) {
	key, installed := n.installedFor(rec)
	if installed == nil {
		return
	}
	n.records.del(key)
	if rec.Type == TypeTXT {
		n.params = nil
	}
	if dep, ok := n.dependencies[key]; ok {
		delete(n.dependencies, key)
		dep.off(n.nullObserver)
	}
	n.names.unregisterForExpiration(installed)
	if n.deleteIfUnused() {
		return
	}
	n.notify(true, key, installed)
}

// deleteIfUnused schedules the name's removal once it has neither records
// nor observers, re-checked after the current batch. Mirrors matter.js
// 0ceb202d DnssdName.ts:273-295.
func (n *Name) deleteIfUnused() bool {
	if n.isObserved() || n.isDiscovered() {
		return false
	}
	if n.maybeDeleting {
		return true
	}
	n.maybeDeleting = true
	n.names.loop.microtask(func() {
		n.maybeDeleting = false
		if n.isObserved() || n.isDiscovered() {
			return
		}
		n.names.deleteName(n)
	})
	return true
}

// notify queues a change and emits the coalesced batch after the current
// task. Mirrors matter.js 0ceb202d DnssdName.ts:302-331.
func (n *Name) notify(deleted bool, key string, rec *Record) {
	n.changes.set(key, pendingChange{deleted: deleted, record: rec})
	if n.notified {
		return
	}
	n.notified = true
	n.names.loop.microtask(func() {
		for n.changes.len() > 0 {
			ch := Changes{Name: n}
			for _, c := range n.changes.values() {
				if c.deleted {
					ch.Deleted = append(ch.Deleted, c.record)
				} else {
					ch.Updated = append(ch.Updated, c.record)
				}
			}
			n.changes.clear()
			for _, o := range slices.Clone(n.observers) {
				if o.fn != nil {
					o.fn(ch)
				}
			}
		}
		n.notified = false
	})
}
