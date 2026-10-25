// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	icddef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/icd"
	"github.com/SukramJ/go-fabric/tlv"
)

// The IcdManagement ids the node's image watch reads.
const (
	icdmanagementClusterID         = icddef.ClusterID
	icdmanagementAttrOperatingMode = icddef.AttrOperatingMode
)

// MinLITSpecificationVersion is the lowest Matter specification version
// (BasicInformation SpecificationVersion, 0xMMmmpprr) a peer must report
// for its LIT support to be trusted: 1.4.0. Mirrors matter.js
// IcdClient.ts:961 (MIN_LIT_SPECIFICATION_VERSION).
const MinLITSpecificationVersion uint32 = 0x0104_0000

// The ICD client's registration — matter.js IcdClient.State's nonvolatile
// fields (IcdClient.ts:1039-1087) — is stored with the image under a
// cluster id no cluster has, beside the settings and the record.
const (
	icdCluster uint32 = 0xFFFF_FFFD

	icdFieldKey              uint32 = 0
	icdFieldPendingKey       uint32 = 1
	icdFieldCounterStart     uint32 = 2
	icdFieldLastOffset       uint32 = 3
	icdFieldMonitoredSubject uint32 = 4
	icdFieldClientType       uint32 = 5
)

// ICDRegistration is the controller's Check-In registration on a peer:
// the key it installed, a key a refresh offered and nobody confirmed yet,
// the counter state Check-Ins are validated against, the monitored subject
// and the client type (IcdClient.ts:1039-1087).
type ICDRegistration struct {
	Key              []byte
	PendingKey       []byte
	CounterStart     uint32
	LastOffset       uint32
	MonitoredSubject uint64
	ClientType       icddef.ClientTypeEnum
}

func (r ICDRegistration) clone() ICDRegistration {
	r.Key, r.PendingKey = bytes.Clone(r.Key), bytes.Clone(r.PendingKey)
	return r
}

func (r *ICDRegistration) encode() (map[uint32][]byte, error) {
	out := map[uint32][]byte{icdFieldKey: nil, icdFieldPendingKey: nil, icdFieldCounterStart: nil, icdFieldLastOffset: nil, icdFieldMonitoredSubject: nil, icdFieldClientType: nil}
	if r == nil {
		return out, nil
	}
	put := func(id uint32, f func(e *tlv.Encoder)) error {
		e := tlv.NewEncoder()
		f(e)
		b, err := e.Bytes()
		out[id] = b
		return err
	}
	errs := []error{
		put(icdFieldKey, func(e *tlv.Encoder) { e.PutOctets(tlv.AnonymousTag(), r.Key) }),
		put(icdFieldCounterStart, func(e *tlv.Encoder) { e.PutUint(tlv.AnonymousTag(), uint64(r.CounterStart)) }),
		put(icdFieldLastOffset, func(e *tlv.Encoder) { e.PutUint(tlv.AnonymousTag(), uint64(r.LastOffset)) }),
		put(icdFieldMonitoredSubject, func(e *tlv.Encoder) { e.PutUint(tlv.AnonymousTag(), r.MonitoredSubject) }),
		put(icdFieldClientType, func(e *tlv.Encoder) { e.PutUint(tlv.AnonymousTag(), uint64(r.ClientType)) }),
	}
	if r.PendingKey != nil {
		errs = append(errs, put(icdFieldPendingKey, func(e *tlv.Encoder) { e.PutOctets(tlv.AnonymousTag(), r.PendingKey) }))
	}
	return out, errors.Join(errs...)
}

func decodeICDRegistration(values map[uint32][]byte) (*ICDRegistration, error) {
	if values[icdFieldKey] == nil {
		return nil, nil
	}
	var r ICDRegistration
	for id, b := range values {
		if b == nil {
			continue
		}
		n, err := decodeValue(b)
		if err != nil {
			return nil, fmt.Errorf("icd field %d: %w", id, err)
		}
		switch id {
		case icdFieldKey:
			r.Key, err = spec.DecodeBytes(n)
		case icdFieldPendingKey:
			r.PendingKey, err = spec.DecodeBytes(n)
		case icdFieldCounterStart:
			r.CounterStart, err = spec.DecodeUint[uint32](32)(n)
		case icdFieldLastOffset:
			r.LastOffset, err = spec.DecodeUint[uint32](32)(n)
		case icdFieldMonitoredSubject:
			r.MonitoredSubject, err = spec.DecodeUint[uint64](64)(n)
		case icdFieldClientType:
			r.ClientType, err = spec.DecodeUint[icddef.ClientTypeEnum](8)(n)
		}
		if err != nil {
			return nil, fmt.Errorf("icd field %d: %w", id, err)
		}
	}
	return &r, nil
}

// ICDRegisterOptions configures [ICDClient.Register] (IcdClient.ts:994-999).
type ICDRegisterOptions struct {
	// MonitoredSubject is the subject the peer monitors for us; nil is our
	// own operational node id.
	MonitoredSubject *uint64
	// ClientType is Permanent (the zero value) or Ephemeral.
	ClientType icddef.ClientTypeEnum
	// AllowMultiAdmin registers despite other administrators.
	AllowMultiAdmin bool
	// IgnoredVendors are the administrators the single-admin guard does
	// not count; nil is [TrustedEcosystemVendors].
	IgnoredVendors []uint16
}

// ICDCheckIn is an accepted Check-In (IcdClient.ts:1115-1117, checkedIn).
type ICDCheckIn struct {
	Counter             uint32
	ActiveModeThreshold uint16
}

// icdOpKind is an operation's kind (IcdClient.ts:980-991).
type icdOpKind uint8

const (
	icdRegistering icdOpKind = iota
	icdRefreshing
	icdUnregistering
	icdForgetting
)

func (k icdOpKind) String() string {
	return [...]string{"registering", "refreshing", "unregistering", "forgetting"}[k]
}

// icdTakesOver is which operation may abandon which: Unregister and Forget
// take over a registration or a key refresh, Forget also an Unregister
// that has not cleared yet; a registration or a refresh starts only when
// none runs. Mirrors IcdClient.ts:947-952 (TAKES_OVER).
var icdTakesOver = map[icdOpKind][]icdOpKind{
	icdRegistering:   nil,
	icdRefreshing:    nil,
	icdUnregistering: {icdRegistering, icdRefreshing},
	icdForgetting:    {icdRegistering, icdRefreshing, icdUnregistering},
}

// icdOperation is the one registration, refresh or teardown in flight.
type icdOperation struct {
	kind   icdOpKind
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	err    error
	opts   *ICDRegisterOptions
}

// icdEnv is what the registry gives a node's ICD client.
type icdEnv struct {
	// table is the fabric's ICD peer table.
	table func(fabricIndex uint8) *icd.PeerTable
	// ownNodeID is our node id on the fabric.
	ownNodeID func(fabricIndex uint8) (uint64, bool)
}

// ICDClient is a node's ICD client: the controller's Check-In registration
// on an ICD peer, its receipt of the peer's Check-Ins, the key refresh,
// the automatic registration on a LIT-operating peer with a subscription,
// and the peer's wakefulness, which holds interactions with a sleeping LIT
// peer and parks the node's sustained subscription. A registry's node has
// one; it is safe for concurrent use.
//
// Mirrors matter.js packages/node/src/behavior/system/icd/IcdClient.ts:55-935
// (IcdClient). matter.js runs each operation in a transaction of its own
// and commits under the ICD state lock; Go serialises the commits under one
// lock (commitMu) and an operation commits only while it still owns the
// registration (#commitIfOwned, :356-372).
type ICDClient struct {
	n   *Node
	env icdEnv
	log *slog.Logger

	// commitMu is matter.js's ICD state lock: every write of the stored
	// registration and the receive path holds it. It is never held across
	// the network.
	commitMu sync.Mutex

	// mu guards the fields below. It is never held across the network, the
	// peer table, a wakefulness or a callback.
	mu              sync.Mutex
	reg             *ICDRegistration
	lastCheckInAt   time.Time
	available       bool
	op              *icdOperation
	autoRegistering bool
	autoPending     bool
	fed             *PeerAddress
	handler         icd.CheckInHandler
	unwatch         []func()
	observers       map[uint64]icdObserver
	nextObserver    uint64
	// announce are the registration events of the commit in progress,
	// emitted once commitMu is released (IcdClient.ts:100-101: announced
	// after commit, so a listener sees the stored registration).
	announce []func()
	ctx      context.Context
	cancel   context.CancelFunc
	work     sync.WaitGroup
}

type icdObserver struct {
	checkedIn                 func(ICDCheckIn)
	registered, unregistered  func()
	keyRefreshed, checkInMiss func()
	availableChanged          func(bool)
}

func newICDClient(n *Node, env icdEnv) *ICDClient {
	c := &ICDClient{n: n, env: env, log: n.log, observers: map[uint64]icdObserver{}}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	return c
}

// peerFed is the sustained subscription's feed signal: the start of a
// peer's registration on our fabric of the peer (IcdClient.ts, the
// ClientNodeInteraction.ts:178 icdPeerFed).
func (c *ICDClient) peerFed(fn func(peerNodeID uint64)) func() {
	addr, ok := c.n.PeerAddress()
	if !ok {
		return func() {}
	}
	t := c.env.table(addr.FabricIndex)
	if t == nil {
		return func() {}
	}
	return t.OnPeerFed(fn)
}

// hold holds an interaction ctx carries with a sleeping LIT peer until it
// wakes — matter.js ClientNodeInteraction.#holdUntilAwake
// (ClientNodeInteraction.ts:333-346): the default timeout the time to the
// peer's next Check-In, WithICDAwaitTimeout's override, none for the ICD
// client's own interactions.
func (c *ICDClient) hold(ctx context.Context) error {
	var timeout time.Duration
	if a, ok := ctx.Value(icdAwaitKey{}).(icdAwait); ok {
		if a.skip {
			return nil
		}
		timeout = a.timeout
	}
	w := c.Wakefulness()
	if w == nil || !w.RequiresAwait() {
		return nil
	}
	addr, _ := c.n.PeerAddress()
	return HoldUntilICDPeerAwake(ctx, w, addr, timeout, c.log)
}

// ICD is the node's ICD client; nil for a node no [Registry] holds.
func (n *Node) ICD() *ICDClient { return n.icd }

// load restores the stored registration (IcdClient State, nonvolatile).
func (c *ICDClient) load(values map[uint32][]byte) error {
	reg, err := decodeICDRegistration(values)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.reg = reg
	c.mu.Unlock()
	return nil
}

// Registration returns the stored registration; ok is false while the
// controller is not registered.
func (c *ICDClient) Registration() (ICDRegistration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reg == nil {
		return ICDRegistration{}, false
	}
	return c.reg.clone(), true
}

// Registered reports a stored registration (IcdClient.ts:63-65, isRegistered).
func (c *ICDClient) Registered() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reg != nil
}

// LastCheckInAt is the time the last accepted Check-In arrived; ok is
// false before one, and after the registration was cleared.
func (c *ICDClient) LastCheckInAt() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastCheckInAt, !c.lastCheckInAt.IsZero()
}

// Available reports a reachable registered peer: a subscription to it is
// held, or its next Check-In is not overdue; a non-LIT peer always is
// (IcdClient.ts:1095-1100).
func (c *ICDClient) Available() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.available
}

// PeerSupportsLIT is [Node.LITSupported] (IcdClient.ts:68-70).
func (c *ICDClient) PeerSupportsLIT() bool { return c.n.LITSupported() }

// Awake reports a peer to which a message goes out now: a non-LIT or not
// yet registered peer reads true (IcdClient.ts:75-77).
func (c *ICDClient) Awake() bool {
	if w := c.Wakefulness(); w != nil {
		return w.Awake()
	}
	return true
}

// NextExpectedCheckIn is the deadline of the next Check-In of a registered
// LIT peer; ok is false with no fed peer, unregistered, not LIT, or while a
// subscription is held (IcdClient.ts:83-87).
func (c *ICDClient) NextExpectedCheckIn() (time.Time, bool) {
	if w := c.Wakefulness(); w != nil {
		return w.NextCheckInDue()
	}
	return time.Time{}, false
}

// Wakefulness is the fed peer's wakefulness, nil when no peer is fed
// (IcdClient.ts:300-303).
func (c *ICDClient) Wakefulness() *icd.PeerWakefulness {
	c.mu.Lock()
	fed := c.fed
	c.mu.Unlock()
	if fed == nil {
		return nil
	}
	t := c.env.table(fed.FabricIndex)
	if t == nil {
		return nil
	}
	return t.WakefulnessFor(fed.NodeID)
}

// Observer registrations; each returns its unsubscribe.

// OnCheckIn registers fn for every accepted Check-In.
func (c *ICDClient) OnCheckIn(fn func(ICDCheckIn)) func() {
	return c.observe(icdObserver{checkedIn: fn})
}

// OnRegistered registers fn for every stored registration.
func (c *ICDClient) OnRegistered(fn func()) func() { return c.observe(icdObserver{registered: fn}) }

// OnUnregistered registers fn for every cleared registration (Unregister,
// Forget, decommission).
func (c *ICDClient) OnUnregistered(fn func()) func() { return c.observe(icdObserver{unregistered: fn}) }

// OnKeyRefreshed registers fn for every stored key refresh.
func (c *ICDClient) OnKeyRefreshed(fn func()) func() { return c.observe(icdObserver{keyRefreshed: fn}) }

// OnCheckInMissed registers fn for every missed Check-In of a registered
// LIT peer (IcdClient.ts:812-814).
func (c *ICDClient) OnCheckInMissed(fn func()) func() { return c.observe(icdObserver{checkInMiss: fn}) }

// OnAvailableChanged registers fn for every change of Available.
func (c *ICDClient) OnAvailableChanged(fn func(bool)) func() {
	return c.observe(icdObserver{availableChanged: fn})
}

func (c *ICDClient) observe(o icdObserver) func() {
	c.mu.Lock()
	id := c.nextObserver
	c.nextObserver++
	c.observers[id] = o
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		delete(c.observers, id)
		c.mu.Unlock()
	}
}

func (c *ICDClient) emit(f func(icdObserver)) {
	c.mu.Lock()
	ids := slices.Sorted(maps.Keys(c.observers))
	obs := make([]icdObserver, len(ids))
	for i, id := range ids {
		obs[i] = c.observers[id]
	}
	c.mu.Unlock()
	for _, o := range obs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					c.log.Warn("matter.controller.client.icd_listener_failed", slog.Any("panic", r))
				}
			}()
			f(o)
		}()
	}
}

// LITSupported is matter.js IcdClient.litSupported (IcdClient.ts:967-974):
// the peer's IcdManagement has LongIdleTimeSupport and its BasicInformation
// reports specification 1.4.0 or later.
func (n *Node) LITSupported() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.litSupportedLocked()
}

func (n *Node) litSupportedLocked() bool {
	ic := readyCluster(n.s.root, icddef.ClusterID)
	if ic == nil || ic.injected.featureMap&uint32(icddef.FeatureLongIdleTimeSupport) == 0 {
		return false
	}
	bi := readyCluster(n.s.root, basicinformation.ClusterID)
	if bi == nil {
		return false
	}
	v, ok := uintOf(bi, basicinformation.AttrSpecificationVersion)
	return ok && v >= uint64(MinLITSpecificationVersion)
}

// icdImage is the image's IcdManagement: present, its FeatureMap and the
// attributes the client reads.
type icdImage struct {
	present  bool
	features icddef.Feature
	mode     *uint64
	timings  icd.Timings
}

func (n *Node) icdImage() icdImage {
	n.mu.RLock()
	defer n.mu.RUnlock()
	ic := readyCluster(n.s.root, icddef.ClusterID)
	out := icdImage{timings: icd.DefaultTimings}
	if ic == nil {
		return out
	}
	out.present, out.features = true, icddef.Feature(ic.injected.featureMap)
	if v, ok := uintOf(ic, icddef.AttrOperatingMode); ok {
		out.mode = &v
	}
	// IcdClient.ts:768-783: the peer's timings, the schedule's defaults for
	// what the image lacks.
	if v, ok := uintOf(ic, icddef.AttrActiveModeThreshold); ok {
		out.timings.ActiveModeThreshold = time.Duration(v) * time.Millisecond //nolint:gosec // a uint16 attribute
	}
	if v, ok := uintOf(ic, icddef.AttrActiveModeDuration); ok {
		out.timings.ActiveModeDuration = time.Duration(v) * time.Millisecond //nolint:gosec // a uint32 attribute
	}
	if v, ok := uintOf(ic, icddef.AttrIdleModeDuration); ok {
		out.timings.IdleModeDuration = time.Duration(v) * time.Second //nolint:gosec // a uint32 attribute
	}
	return out
}

// litOperating is #peerIsLongIdleTimeOperating (IcdClient.ts:89-94).
func (c *ICDClient) litOperating() bool {
	img := c.n.icdImage()
	return c.n.LITSupported() && img.mode != nil && *img.mode == uint64(icddef.OperatingModeLit)
}

// fabricContext is our side of the peer's fabric (IcdClient.ts:715-724).
func (c *ICDClient) fabricContext() (addr PeerAddress, own uint64, table *icd.PeerTable, err error) {
	addr, ok := c.n.PeerAddress()
	if !ok {
		return addr, 0, nil, fmt.Errorf("%w: ICD registration requires a commissioned peer", ErrICDOperation)
	}
	own, ok = c.env.ownNodeID(addr.FabricIndex)
	table = c.env.table(addr.FabricIndex)
	if !ok || table == nil {
		return addr, 0, nil, fmt.Errorf("%w: the peer's fabric is not ours", ErrICDOperation)
	}
	return addr, own, table, nil
}

// mayStartLocked: none runs, or kind may take it over (IcdClient.ts:186-189).
func (c *ICDClient) mayStartLocked(kind icdOpKind) bool {
	return c.op == nil || slices.Contains(icdTakesOver[kind], c.op.kind)
}

// startLocked starts an operation of kind, abandoning the one in flight it
// may take over (IcdClient.ts:312-331). The caller holds mu.
func (c *ICDClient) startLocked(kind icdOpKind, opts *ICDRegisterOptions, body func(op *icdOperation) error) (*icdOperation, error) {
	if cur := c.op; cur != nil {
		if !c.mayStartLocked(kind) {
			return nil, fmt.Errorf("%w: ICD client cannot start %v while %v is in progress", ErrICDOperation, kind, cur.kind)
		}
		cur.cancel()
	}
	ctx, cancel := context.WithCancel(c.ctx)
	op := &icdOperation{kind: kind, ctx: ctx, cancel: cancel, done: make(chan struct{}), opts: opts}
	c.op = op
	c.work.Add(1)
	go func() {
		defer c.work.Done()
		err := body(op)
		c.mu.Lock()
		if c.op == op {
			c.op = nil
		}
		op.err = err
		c.mu.Unlock()
		cancel()
		close(op.done)
	}()
	return op, nil
}

// wait waits for op and returns its error.
func (op *icdOperation) wait(ctx context.Context) error {
	select {
	case <-op.done:
		return op.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// owns reports op still the operation in flight, not abandoned
// (IcdClient.ts:334-336). The caller holds mu.
func (c *ICDClient) ownsLocked(op *icdOperation) bool { return c.op == op && op.ctx.Err() == nil }

// commitIfOwned runs write under the state lock while op owns the
// registration and reports whether it ran (IcdClient.ts:356-372).
func (c *ICDClient) commitIfOwned(op *icdOperation, write func()) bool {
	c.commitMu.Lock()
	c.mu.Lock()
	owned := c.ownsLocked(op)
	c.mu.Unlock()
	if owned {
		write()
	}
	c.commitMu.Unlock()
	c.flushAnnouncements()
	return owned
}

// flushAnnouncements emits the registration events the commit stored.
func (c *ICDClient) flushAnnouncements() {
	c.mu.Lock()
	fns := c.announce
	c.announce = nil
	c.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// persist stores reg, nil to clear it — the only writer of the
// stored registration (IcdClient.ts:444-456). The caller holds commitMu.
func (c *ICDClient) persist(reg *ICDRegistration) {
	var before bool
	c.mu.Lock()
	before = c.reg != nil
	var prevKey []byte
	if c.reg != nil {
		prevKey = c.reg.Key
	}
	if reg == nil {
		c.reg = nil
		c.lastCheckInAt = time.Time{}
		c.available = false
	} else {
		r := reg.clone()
		c.reg = &r
	}
	c.mu.Unlock()
	values, err := reg.encode()
	if err == nil {
		err = c.n.saveICD(values)
	}
	if err != nil {
		c.log.Warn("matter.controller.client.store_write_failed", slog.String("what", "icd registration"), slog.Any("err", err))
	}
	// The key's change is announced after the commit (IcdClient.ts:142-150).
	var fn func()
	switch {
	case !before && reg != nil:
		fn = func() { c.emit(func(o icdObserver) { call(o.registered) }) }
	case before && reg == nil:
		fn = func() { c.emit(func(o icdObserver) { call(o.unregistered) }) }
	case before && reg != nil && !bytes.Equal(prevKey, reg.Key):
		fn = func() { c.emit(func(o icdObserver) { call(o.keyRefreshed) }) }
	}
	if fn != nil {
		c.mu.Lock()
		c.announce = append(c.announce, fn)
		c.mu.Unlock()
	}
}

func call(fn func()) {
	if fn != nil {
		fn()
	}
}

// saveICD stores the ICD client's registration.
func (n *Node) saveICD(values map[uint32][]byte) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.erased {
		return nil
	}
	return n.store.Save(n.cfg.ID, 0, icdCluster, values, nil)
}

// Register registers this controller as a Check-In client of the peer: a
// 16-byte random key installed with RegisterClient, the counter baseline
// of the response stored, and the Check-In receive path armed. A call
// while a registration with the same options runs joins it. It refuses
// with [ErrICDOperation] a registration that exists or is torn down, other
// options in flight, no active subscription (the peer's operating mode
// would be stale), a peer that is not online or lacks CheckInProtocolSupport;
// with [*ICDMultiAdminError] a multi-admin peer; with [ErrICDAbandoned] when
// Unregister, Forget, decommission or Close abandoned it. Mirrors matter.js
// IcdClient.ts:499-592 (register, #registerOnPeer; Core §9.16.7.1).
func (c *ICDClient) Register(ctx context.Context, opts ICDRegisterOptions) error {
	c.mu.Lock()
	if cur := c.op; cur != nil && cur.kind == icdRegistering {
		same := c.sameOptions(cur.opts, &opts)
		c.mu.Unlock()
		if !same {
			return fmt.Errorf("%w: an ICD client registration with other options is in progress", ErrICDOperation)
		}
		return cur.wait(ctx)
	}
	if !c.mayStartLocked(icdRegistering) {
		kind := c.op.kind
		c.mu.Unlock()
		return fmt.Errorf("%w: ICD client cannot register while %v is in progress", ErrICDOperation, kind)
	}
	if c.reg != nil {
		c.mu.Unlock()
		return fmt.Errorf("%w: ICD client is already registered; unregister first or let key refresh re-key in place", ErrICDOperation)
	}
	c.mu.Unlock()
	if !c.n.SubscriptionActive() {
		return fmt.Errorf("%w: ICD registration requires an active subscription so the peer operating mode is not stale", ErrICDOperation)
	}
	if !c.n.online() {
		return fmt.Errorf("%w: ICD registration requires the peer node to be online", ErrICDOperation)
	}
	if img := c.n.icdImage(); img.features&icddef.FeatureCheckInProtocolSupport == 0 {
		return fmt.Errorf("%w: ICD registration refused: peer does not support the Check-In Protocol (CIP)", ErrICDOperation)
	}
	c.mu.Lock()
	op, err := c.startLocked(icdRegistering, &opts, func(op *icdOperation) error { return c.registerOnPeer(op, opts) })
	c.mu.Unlock()
	if err != nil {
		return err
	}
	return op.wait(ctx)
}

// resolveOptions applies the defaults (IcdClient.ts:541-549).
func (c *ICDClient) resolveOptions(o *ICDRegisterOptions) ICDRegisterOptions {
	out := ICDRegisterOptions{}
	if o != nil {
		out = *o
	}
	if out.MonitoredSubject == nil {
		if addr, ok := c.n.PeerAddress(); ok {
			if own, ok := c.env.ownNodeID(addr.FabricIndex); ok {
				out.MonitoredSubject = &own
			}
		}
	}
	if out.IgnoredVendors == nil {
		out.IgnoredVendors = TrustedEcosystemVendors
	}
	return out
}

// sameOptions compares with the defaults applied (IcdClient.ts:551-561).
func (c *ICDClient) sameOptions(a, b *ICDRegisterOptions) bool {
	l, r := c.resolveOptions(a), c.resolveOptions(b)
	ls, rs := uint64(0), uint64(0)
	if l.MonitoredSubject != nil {
		ls = *l.MonitoredSubject
	}
	if r.MonitoredSubject != nil {
		rs = *r.MonitoredSubject
	}
	if ls != rs || l.ClientType != r.ClientType || l.AllowMultiAdmin != r.AllowMultiAdmin || len(l.IgnoredVendors) != len(r.IgnoredVendors) {
		return false
	}
	for _, v := range l.IgnoredVendors {
		if !slices.Contains(r.IgnoredVendors, v) {
			return false
		}
	}
	return true
}

func (c *ICDClient) registerOnPeer(op *icdOperation, opts ICDRegisterOptions) error {
	addr, own, table, err := c.fabricContext()
	if err != nil {
		return err
	}
	o := c.resolveOptions(&opts)
	fabrics, err := c.readPeerFabrics(op.ctx)
	if err != nil {
		return c.abandoned(op, err)
	}
	vendors := make([]uint16, 0, len(fabrics))
	for _, f := range fabrics {
		vendors = append(vendors, f.VendorID)
	}
	if err := AssertSingleAdmin(vendors, o.IgnoredVendors, o.AllowMultiAdmin); err != nil {
		return err
	}
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	subject := own
	if o.MonitoredSubject != nil {
		subject = *o.MonitoredSubject
	}
	counter, err := c.registerClient(op.ctx, icddef.RegisterClientRequest{CheckInNodeId: own, MonitoredSubject: subject, Key: key, ClientType: o.ClientType})
	if err != nil {
		return c.abandoned(op, err)
	}
	if !c.commitIfOwned(op, func() {
		reg := &ICDRegistration{Key: key, CounterStart: counter, MonitoredSubject: subject, ClientType: o.ClientType}
		c.persist(reg)
		// The peer just answered: it is reachable; seed its window.
		c.feed(table, addr, reg, true)
	}) {
		return fmt.Errorf("%w: ICD registration was abandoned", ErrICDAbandoned)
	}
	return nil
}

// abandoned maps an error of an abandoned operation to ErrICDAbandoned.
func (c *ICDClient) abandoned(op *icdOperation, err error) error {
	if op.ctx.Err() != nil {
		return fmt.Errorf("%w: %w", ErrICDAbandoned, err)
	}
	return err
}

// registerClient invokes RegisterClient and returns the response's counter.
func (c *ICDClient) registerClient(ctx context.Context, req icddef.RegisterClientRequest) (uint32, error) {
	res, err := c.n.Invoke(withoutICDHold(ctx), CommandPath{Endpoint: 0, Cluster: icddef.ClusterID, Command: icddef.CmdRegisterClient}, req, InvokeOptions{})
	if err != nil {
		return 0, err
	}
	if !res.HasFields {
		return 0, fmt.Errorf("client: RegisterClient answered without a response")
	}
	var resp icddef.RegisterClientResponse
	if err := resp.DecodeTLV(res.Fields); err != nil {
		return 0, fmt.Errorf("client: RegisterClientResponse: %w", err)
	}
	c.noteActive()
	return resp.IcdCounter, nil
}

// readPeerFabrics reads the peer's Fabrics unfiltered (IcdClient.ts:726-732).
func (c *ICDClient) readPeerFabrics(ctx context.Context) ([]fabricDescriptor, error) {
	rep, err := c.n.Read(withoutICDHold(ctx), ReadRequest{ReadRequest: ReadPaths{
		Attributes:     []AttributePath{AttributeAt(0, opCredsCluster, opCredsAttrFabrics)},
		NoFabricFilter: true,
	}, IncludeKnownVersions: true})
	if err != nil {
		return nil, err
	}
	v, ok := rep.Value(0, opCredsCluster, opCredsAttrFabrics)
	if !ok || v.El.Type != tlv.TypeArray {
		return nil, nil
	}
	var out []fabricDescriptor
	for _, e := range v.Children {
		if f, err := decodeFabricDescriptor(e); err == nil {
			out = append(out, f)
		}
	}
	return out, nil
}

// Unregister removes this controller's registration from the peer: it
// abandons a registration or a key refresh in flight, waits for a sleeping
// LIT peer to wake, clears the local registration, then sends
// UnregisterClient with the cleared key. A no-op when not registered; a
// call while one runs joins it. [ErrICDOperation] while Forget runs;
// [ErrICDAbandoned] when Forget, decommission or Close abandoned it before
// the clear; [*ICDPeerAsleepError] when the peer did not wake (the
// registration stays); the peer's error when UnregisterClient failed after
// the clear. Mirrors matter.js IcdClient.ts:611-636 (Core §9.16.7.3).
func (c *ICDClient) Unregister(ctx context.Context) error {
	c.mu.Lock()
	if cur := c.op; cur != nil && cur.kind == icdUnregistering {
		c.mu.Unlock()
		return cur.wait(ctx)
	}
	if c.op == nil && c.reg == nil {
		c.mu.Unlock()
		return nil
	}
	op, err := c.startLocked(icdUnregistering, nil, c.unregisterOnPeer)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	return op.wait(ctx)
}

func (c *ICDClient) unregisterOnPeer(op *icdOperation) error {
	addr, own, _, err := c.fabricContext()
	if err != nil {
		return err
	}
	if err := HoldUntilICDPeerAwake(op.ctx, c.Wakefulness(), addr, 0, c.log); err != nil {
		var asleep *ICDPeerAsleepError
		if errors.As(err, &asleep) {
			return err
		}
		return c.abandoned(op, err)
	}
	key, ok := c.clearLocal(op)
	if !ok {
		return fmt.Errorf("%w: ICD unregistration was abandoned", ErrICDAbandoned)
	}
	_, err = c.n.Invoke(withoutICDHold(op.ctx), CommandPath{Endpoint: 0, Cluster: icddef.ClusterID, Command: icddef.CmdUnregisterClient},
		icddef.UnregisterClientRequest{CheckInNodeId: own, VerificationKey: &key}, InvokeOptions{})
	return err
}

// Forget drops this controller's registration locally, without
// UnregisterClient: it abandons a registration, a key refresh or an
// Unregister that has not cleared yet; the peer keeps a stale registration
// until a later Register replaces it. A no-op when nothing is registered
// or being registered. Mirrors matter.js IcdClient.ts:651-677.
func (c *ICDClient) Forget(ctx context.Context) error {
	c.mu.Lock()
	if cur := c.op; cur != nil && cur.kind == icdForgetting {
		c.mu.Unlock()
		return cur.wait(ctx)
	}
	if c.reg == nil && (c.op == nil || c.op.kind != icdRegistering) {
		c.mu.Unlock()
		return nil
	}
	op, err := c.startLocked(icdForgetting, nil, func(op *icdOperation) error {
		if _, ok := c.clearLocal(op); !ok {
			return fmt.Errorf("%w: ICD forget was abandoned", ErrICDAbandoned)
		}
		c.log.Info("matter.controller.client.icd_forgotten", slog.String("reason", "no peer UnregisterClient; the peer retains a stale registration"))
		return nil
	})
	c.mu.Unlock()
	if err != nil {
		return err
	}
	return op.wait(ctx)
}

// clearLocal clears the stored registration and the receive path; it
// returns the cleared key, ok false when op no longer owns the
// registration (IcdClient.ts:683-691).
func (c *ICDClient) clearLocal(op *icdOperation) (key []byte, ok bool) {
	ok = c.commitIfOwned(op, func() {
		c.mu.Lock()
		if c.reg != nil {
			key = bytes.Clone(c.reg.Key)
		}
		c.mu.Unlock()
		c.persist(nil)
		c.dropReceivePath()
	})
	return key, ok
}

// StayActive asks the peer to stay active for at least d and returns what
// it promised; the wakefulness keeps the peer awake that long. Mirrors
// matter.js IcdClient.ts:706-713 (Core §9.16.7.4).
func (c *ICDClient) StayActive(ctx context.Context, d time.Duration) (time.Duration, error) {
	res, err := c.n.Invoke(ctx, CommandPath{Endpoint: 0, Cluster: icddef.ClusterID, Command: icddef.CmdStayActiveRequest},
		icddef.StayActiveRequestRequest{StayActiveDuration: uint32(d.Milliseconds())}, InvokeOptions{}) //nolint:gosec // G115: a request's milliseconds
	if err != nil {
		return 0, err
	}
	var resp icddef.StayActiveResponse
	if !res.HasFields {
		return 0, fmt.Errorf("client: StayActiveRequest answered without a response")
	}
	if err := resp.DecodeTLV(res.Fields); err != nil {
		return 0, fmt.Errorf("client: StayActiveResponse: %w", err)
	}
	promised := time.Duration(resp.PromisedActiveDuration) * time.Millisecond
	if w := c.Wakefulness(); w != nil {
		w.NoteStayActive(promised)
	}
	return promised, nil
}

// feed arms the receive path of reg: the peer in the fabric's table with
// our handler, the wakefulness's timings from the image, its RequiresAwait
// the peer's LIT operation, a seed of activity, and the availability and
// missed Check-Ins mirrored. The caller holds commitMu. Mirrors matter.js
// IcdClient.ts:747-806 (#feedFabricIcd).
func (c *ICDClient) feed(table *icd.PeerTable, addr PeerAddress, reg *ICDRegistration, seed bool) {
	c.mu.Lock()
	if c.handler == nil {
		c.handler = c.onCheckIn
	}
	h := c.handler
	c.fed = &addr
	c.mu.Unlock()
	table.AddPeer(icd.Peer{PeerNodeID: addr.NodeID, Key: reg.Key, PendingKey: reg.PendingKey, CounterStart: reg.CounterStart, LastOffset: reg.LastOffset}, h)
	w := table.WakefulnessFor(addr.NodeID)
	if w == nil {
		return
	}
	w.SetTimings(c.n.icdImage().timings)
	w.SetRequiresAwait(c.litOperating())
	if seed {
		w.NoteActive()
	}
	c.unwatchAll()
	unAvail := w.OnAvailableChanged(c.onAvailableChanged)
	unMissed := w.OnCheckInMissed(func() { c.emit(func(o icdObserver) { call(o.checkInMiss) }) })
	c.mu.Lock()
	c.unwatch = []func(){unAvail, unMissed}
	c.available = w.Available()
	c.mu.Unlock()
	if p := c.n.peer(); p != nil {
		p.SetICDAwait(func() bool {
			if w := c.Wakefulness(); w != nil {
				return w.RequiresAwait()
			}
			return false
		})
	}
}

func (c *ICDClient) onAvailableChanged(v bool) {
	c.mu.Lock()
	changed := c.available != v
	c.available = v
	c.mu.Unlock()
	if changed {
		c.emit(func(o icdObserver) {
			if o.availableChanged != nil {
				o.availableChanged(v)
			}
		})
	}
}

func (c *ICDClient) unwatchAll() {
	c.mu.Lock()
	fns := c.unwatch
	c.unwatch = nil
	c.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// dropReceivePath stops routing the peer's Check-Ins here; the fed peer is
// kept for decommission (IcdClient.ts:459-464).
func (c *ICDClient) dropReceivePath() {
	c.unwatchAll()
	c.mu.Lock()
	fed := c.fed
	c.mu.Unlock()
	if fed == nil {
		return
	}
	if t := c.env.table(fed.FabricIndex); t != nil {
		t.DeletePeer(fed.NodeID)
	}
}

// restoreReceivePath re-arms the receive path of the stored registration
// after a controller restart, without noting the peer active (its
// reachability is unknown). Mirrors IcdClient.ts:413-420.
func (c *ICDClient) restoreReceivePath() {
	c.commitMu.Lock()
	defer c.commitMu.Unlock()
	c.mu.Lock()
	reg := c.reg
	c.mu.Unlock()
	if reg == nil {
		return
	}
	addr, _, table, err := c.fabricContext()
	if err != nil {
		return
	}
	c.feed(table, addr, reg, false)
}

// onCheckIn is the peer's Check-In handler: the advanced counter state is
// stored, the arrival time noted, a key refresh started when the counter
// asks for one, and the Check-In announced last, so a listener that starts
// Unregister or Forget takes over the refresh. Mirrors IcdClient.ts:816-839.
func (c *ICDClient) onCheckIn(in icd.ReceivedCheckIn) {
	c.commitMu.Lock()
	c.mu.Lock()
	reg := c.reg
	fed := c.fed
	c.mu.Unlock()
	if reg == nil || fed == nil {
		c.commitMu.Unlock()
		return
	}
	table := c.env.table(fed.FabricIndex)
	if table == nil {
		c.commitMu.Unlock()
		return
	}
	if entry, ok := table.PeerFor(fed.NodeID); ok {
		c.persist(&ICDRegistration{
			Key: entry.Key, PendingKey: entry.PendingKey, CounterStart: entry.CounterStart, LastOffset: entry.LastOffset,
			MonitoredSubject: reg.MonitoredSubject, ClientType: reg.ClientType,
		})
	}
	c.mu.Lock()
	c.lastCheckInAt = c.n.clock.Now()
	if c.mayStartLocked(icdRefreshing) && table.RefreshNeeded(fed.NodeID) {
		_, _ = c.startLocked(icdRefreshing, nil, c.refreshKey)
	}
	c.mu.Unlock()
	c.commitMu.Unlock()
	c.flushAnnouncements()
	c.emit(func(o icdObserver) {
		if o.checkedIn != nil {
			o.checkedIn(ICDCheckIn{Counter: in.Counter, ActiveModeThreshold: in.ActiveModeThreshold})
		}
	})
}

// refreshKey never fails: the next Check-In under the old key retries
// (IcdClient.ts:858-871).
func (c *ICDClient) refreshKey(op *icdOperation) error {
	err := c.offerKey(op)
	switch {
	case err == nil:
	case op.ctx.Err() != nil || errors.Is(err, ErrICDAbandoned):
		c.log.Debug("matter.controller.client.icd_refresh_abandoned")
	case errors.Is(err, ErrICDOperation):
		c.log.Error("matter.controller.client.icd_refresh_failed", slog.Any("err", err))
	default:
		c.log.Warn("matter.controller.client.icd_refresh_retry", slog.Any("err", err), slog.String("hint", "will retry on next check-in"))
	}
	return nil
}

// offerKey re-keys the registration before the counter offset reaches
// 2^31: the new key is stored as pending before the peer sees it, since
// the peer may take it without the response reaching us; RegisterClient
// with the current key as the verification key; then the response's
// counter confirms it. Mirrors IcdClient.ts:882-914 (Core §4.22.3.4.1,
// §4.22.4.2, §9.16.7.1).
func (c *ICDClient) offerKey(op *icdOperation) error {
	addr, own, table, err := c.fabricContext()
	if err != nil {
		return err
	}
	var req *icddef.RegisterClientRequest
	var failure error
	if !c.commitIfOwned(op, func() {
		c.mu.Lock()
		reg := c.reg
		c.mu.Unlock()
		if reg == nil {
			failure = fmt.Errorf("%w: ICD key refresh requires an existing registration", ErrICDOperation)
			return
		}
		entry, ok := table.PeerFor(addr.NodeID)
		if !ok {
			failure = fmt.Errorf("%w: ICD key refresh requires the peer's Check-In receive path", ErrICDOperation)
			return
		}
		// A Check-In under the pending key may have confirmed it meanwhile.
		if reg.PendingKey != nil && entry.PendingKey == nil && bytes.Equal(entry.Key, reg.PendingKey) {
			return
		}
		key := reg.PendingKey
		if key == nil {
			key = make([]byte, 16)
			_, _ = rand.Read(key)
		}
		_ = table.SetPendingKey(addr.NodeID, key)
		entry.PendingKey = key
		c.persist(&ICDRegistration{
			Key: entry.Key, PendingKey: key, CounterStart: entry.CounterStart, LastOffset: entry.LastOffset,
			MonitoredSubject: reg.MonitoredSubject, ClientType: reg.ClientType,
		})
		verification := bytes.Clone(reg.Key)
		req = &icddef.RegisterClientRequest{CheckInNodeId: own, MonitoredSubject: reg.MonitoredSubject, Key: key, ClientType: reg.ClientType, VerificationKey: &verification}
	}) {
		return fmt.Errorf("%w: ICD key refresh was abandoned", ErrICDAbandoned)
	}
	if failure != nil || req == nil {
		return failure
	}
	// The refresh waits for a sleeping peer as every interaction does.
	if err := HoldUntilICDPeerAwake(op.ctx, c.Wakefulness(), addr, 0, c.log); err != nil {
		return c.abandoned(op, err)
	}
	counter, err := c.registerClient(op.ctx, *req)
	if err != nil {
		return c.abandoned(op, err)
	}
	c.commitIfOwned(op, func() {
		ok, _ := table.ConfirmKey(addr.NodeID, req.Key, counter)
		if !ok {
			c.log.Warn("matter.controller.client.icd_refresh_unknown_key", slog.String("peer", peerAddressString(addr)))
		}
		c.mu.Lock()
		reg := c.reg
		c.mu.Unlock()
		if entry, ok := table.PeerFor(addr.NodeID); ok && reg != nil {
			c.persist(&ICDRegistration{
				Key: entry.Key, PendingKey: entry.PendingKey, CounterStart: entry.CounterStart, LastOffset: entry.LastOffset,
				MonitoredSubject: reg.MonitoredSubject, ClientType: reg.ClientType,
			})
		}
	})
	return nil
}

// noteActive records the peer answering or reporting (IcdClient.ts:156-158;
// NodeSession.ts:278-283).
func (c *ICDClient) noteActive() {
	if w := c.Wakefulness(); w != nil {
		w.NoteActive()
	}
}

// onSubscriptionActive is #onSubscriptionStatusChanged (IcdClient.ts:160-164).
func (c *ICDClient) onSubscriptionActive(active bool) {
	if active {
		c.ensureLITRegistration()
	}
}

// onOperatingModeChanged follows a peer's SIT⇄LIT flip: the wakefulness's
// RequiresAwait, a live flip into LIT noted as activity, then the
// automatic registration. Mirrors IcdClient.ts:166-178.
func (c *ICDClient) onOperatingModeChanged() {
	if w := c.Wakefulness(); w != nil {
		lit := c.litOperating()
		w.SetRequiresAwait(lit)
		if lit && c.n.online() {
			w.NoteActive()
		}
		// The peer's timings may have arrived with the same report.
		w.SetTimings(c.n.icdImage().timings)
	}
	c.ensureLITRegistration()
}

// ensureLITRegistration registers on a LIT-capable peer with an active
// subscription when nothing is stored or running; a trigger during an
// attempt re-runs it once (IcdClient.ts:195-215).
func (c *ICDClient) ensureLITRegistration() {
	if c.ctx.Err() != nil || !c.n.LITSupported() || !c.n.SubscriptionActive() {
		return
	}
	c.mu.Lock()
	if !c.mayStartLocked(icdRegistering) || c.reg != nil {
		c.mu.Unlock()
		return
	}
	if c.autoRegistering {
		c.autoPending = true
		c.mu.Unlock()
		return
	}
	c.autoRegistering = true
	c.mu.Unlock()
	c.work.Add(1)
	go func() {
		defer c.work.Done()
		c.autoRegister()
	}()
}

// autoRegister registers on a peer confirmed LIT by a fresh read, or on a
// DSLS peer that already serves Check-In clients (IcdClient.ts:217-272).
func (c *ICDClient) autoRegister() {
	defer func() {
		c.mu.Lock()
		c.autoRegistering = false
		again := c.autoPending
		c.autoPending = false
		c.mu.Unlock()
		if again {
			c.ensureLITRegistration()
		}
	}()
	ctx := c.ctx
	idle := func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.op == nil && c.reg == nil
	}
	if !idle() || !c.n.online() || !c.n.LITSupported() {
		return
	}
	mode, err := c.readOperatingMode(ctx)
	if err != nil {
		c.logAutoFailure(err)
		return
	}
	if mode != uint64(icddef.OperatingModeLit) {
		has, err := c.dslsPeerHasRegistrations(ctx)
		if err != nil {
			c.logAutoFailure(err)
			return
		}
		if !has {
			return
		}
	}
	if !idle() {
		return
	}
	if err := c.Register(ctx, ICDRegisterOptions{AllowMultiAdmin: true}); err != nil {
		c.logAutoFailure(err)
	}
}

func (c *ICDClient) logAutoFailure(err error) {
	if c.ctx.Err() != nil || errors.Is(err, ErrICDAbandoned) || errors.Is(err, context.Canceled) {
		c.log.Debug("matter.controller.client.icd_auto_register_abandoned", slog.Any("err", err))
		return
	}
	c.log.Warn("matter.controller.client.icd_auto_register_failed", slog.Any("err", err))
}

// readOperatingMode reads OperatingMode fresh from the peer.
func (c *ICDClient) readOperatingMode(ctx context.Context) (uint64, error) {
	rep, err := c.n.Read(withoutICDHold(ctx), ReadRequest{ReadRequest: ReadPaths{
		Attributes: []AttributePath{AttributeAt(0, icddef.ClusterID, icddef.AttrOperatingMode)},
	}, IncludeKnownVersions: true})
	if err != nil {
		return 0, err
	}
	v, ok := rep.Value(0, icddef.ClusterID, icddef.AttrOperatingMode)
	if !ok {
		return 0, fmt.Errorf("client: the peer reported no OperatingMode")
	}
	return v.El.Uint, nil
}

// dslsPeerHasRegistrations reads RegisteredClients unfiltered on a DSLS
// peer, a co-admin's registration living on its own fabric
// (IcdClient.ts:278-287).
func (c *ICDClient) dslsPeerHasRegistrations(ctx context.Context) (bool, error) {
	if c.n.icdImage().features&icddef.FeatureDynamicSitLitSupport == 0 {
		return false, nil
	}
	rep, err := c.n.Read(withoutICDHold(ctx), ReadRequest{ReadRequest: ReadPaths{
		Attributes:     []AttributePath{AttributeAt(0, icddef.ClusterID, icddef.AttrRegisteredClients)},
		NoFabricFilter: true,
	}, IncludeKnownVersions: true})
	if err != nil {
		return false, err
	}
	v, ok := rep.Value(0, icddef.ClusterID, icddef.AttrRegisteredClients)
	return ok && len(v.Children) > 0, nil
}

// onDecommissioned clears the registration locally — the peer removed our
// fabric, so no UnregisterClient is possible — and removes the peer with
// its wakefulness. Mirrors IcdClient.ts:426-438.
func (c *ICDClient) onDecommissioned() {
	c.mu.Lock()
	if c.op != nil {
		c.op.cancel()
	}
	reg := c.reg
	fed := c.fed
	c.fed = nil
	c.mu.Unlock()
	c.commitMu.Lock()
	if reg != nil {
		c.persist(nil)
	}
	c.unwatchAll()
	c.commitMu.Unlock()
	c.flushAnnouncements()
	if fed != nil {
		if t := c.env.table(fed.FabricIndex); t != nil {
			t.RemovePeer(fed.NodeID)
		}
	}
	if p := c.n.peer(); p != nil {
		p.SetICDAwait(nil)
	}
}

// close abandons the operations and waits for them; the stored
// registration stays (IcdClient.ts:138-140, :930-934).
func (c *ICDClient) close() {
	c.cancel()
	c.work.Wait()
	c.unwatchAll()
}
