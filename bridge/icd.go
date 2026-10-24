// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/icd"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// ICDAdvertisement is the ICD part of the operational DNS-SD record: the
// ICD TXT key (the operating mode, 0 SIT, 1 LIT), the SAT key, and SII
// left out for a LIT ICD (Core §9.15.1.6.2). Mirrors matter.js
// packages/protocol/src/protocol/DeviceAdvertiser.ts:25-36 (IcdAdvertisement).
type ICDAdvertisement struct {
	Mode             uint8
	ActiveThreshold  time.Duration
	OmitIdleInterval bool
}

// icdPorts is the ICD state of a bridge whose node is an ICD; every field
// is zero for one that is not.
type icdPorts struct {
	// fixedBackoff is the MRP pad, nanoseconds.
	fixedBackoff atomic.Int64
	onActivity   atomic.Pointer[func()]
	advert       atomic.Pointer[func() ICDAdvertisement]
}

// SetLocalFixedMRPBackoff pads every retransmission wait of the bridge's
// reliable sends by d after the exponential backoff, unamplified — an
// ICD's fast-poll interval. Mirrors matter.js IcdManagementServer.ts:257
// (SessionManager.localFixedMrpBackoff) and MRP.ts:134-139, :188
// (fixedBackoff). Zero, the default, pads nothing.
func (b *Bridge) SetLocalFixedMRPBackoff(d time.Duration) {
	b.icd.fixedBackoff.Store(int64(d))
	b.mu.RLock()
	t := b.outboundReliable
	b.mu.RUnlock()
	if t != nil {
		t.fixedBackoff.Store(int64(d))
	}
}

// LocalFixedMRPBackoff is the pad SetLocalFixedMRPBackoff set.
func (b *Bridge) LocalFixedMRPBackoff() time.Duration {
	return time.Duration(b.icd.fixedBackoff.Load())
}

// SetOnInboundActivity sets fn, called for every authenticated inbound
// message — the ICD's mode machine extends its active window on it
// (IcdManagementServer.ts:300-306, the NodeActivity inactive observer).
// nil removes it. fn runs on the receive path and must not block.
func (b *Bridge) SetOnInboundActivity(fn func()) {
	if fn == nil {
		b.icd.onActivity.Store(nil)
		return
	}
	b.icd.onActivity.Store(&fn)
}

func (b *Bridge) noteInboundActivity() {
	if fn := b.icd.onActivity.Load(); fn != nil {
		(*fn)()
	}
}

// SetICDAdvertisement sets the source of the ICD part of every operational
// record the bridge publishes from now on; nil publishes none
// (DeviceAdvertiser.ts:195-197, setIcdAdvertisementProvider). A change of
// what it returns takes effect at the next [Bridge.AnnounceFabric].
func (b *Bridge) SetICDAdvertisement(fn func() ICDAdvertisement) {
	if fn == nil {
		b.icd.advert.Store(nil)
		return
	}
	b.icd.advert.Store(&fn)
}

func (b *Bridge) icdAdvertisement() (ICDAdvertisement, bool) {
	fn := b.icd.advert.Load()
	if fn == nil {
		return ICDAdvertisement{}, false
	}
	return (*fn)(), true
}

// ErrNoUnsecuredRoute reports an unsecured send without a listener.
var ErrNoUnsecuredRoute = errors.New("bridge: no listener for an unsecured send")

// SendUnsecured sends one unsecured Secure Channel message of opcode to
// addr on an exchange of its own, from a random ephemeral node id,
// without MRP: no acknowledgement is asked for and nothing is
// retransmitted — the ICD Check-In (opcode 0x50, Core §4.22.3). It is the
// one-shot form of the CASE initiator's unsecured send
// (sendInitiatedUnsecured). Mirrors matter.js IcdManagementServer.ts:366-391
// (sendUnsecured: an unsecured session, suppressAck, disableMrpLogic).
func (b *Bridge) SendUnsecured(addr *net.UDPAddr, opcode uint8, payload []byte) error {
	b.mu.RLock()
	listener := b.listener
	b.mu.RUnlock()
	if listener == nil {
		return ErrNoUnsecuredRoute
	}
	ephemeral, err := randomEphemeralNodeID()
	if err != nil {
		return err
	}
	proto := message.ProtocolHeader{
		Initiator:  true,
		Opcode:     opcode,
		ExchangeID: b.nextOutboundExchangeID(),
		ProtocolID: mrp.SecureChannelProtocolID,
	}
	hdr := message.Header{
		SessionID:       0,
		MessageCounter:  b.nextUnsecuredCounter(),
		HasSourceNodeID: true,
		SourceNodeID:    ephemeral,
	}
	datagram := append(hdr.Marshal(), proto.Marshal()...) //nolint:gocritic // single-allocation join
	datagram = append(datagram, payload...)
	return listener.Send(addr, datagram)
}

// PeerSessionAddress returns the last authenticated UDP address of a CASE
// session of nodeID on fabricIndex, nil when the bridge holds none — the
// cached operational address matter.js's Check-In sender prefers
// (IcdManagementServer.ts:361-364, the peer descriptor's operationalAddress).
func (b *Bridge) PeerSessionAddress(fabricIndex uint8, nodeID uint64) *net.UDPAddr {
	var found *net.UDPAddr
	b.sessionPeerAddrs.Range(func(k, v any) bool {
		sid, ok := k.(uint16)
		if !ok {
			return true
		}
		addr, ok := v.(*net.UDPAddr)
		if !ok || addr == nil || b.resolveSessionFabric(sid) != fabricIndex {
			return true
		}
		if n, _ := b.resolveSessionSubject(sid); n != nodeID {
			return true
		}
		found = addr
		return false
	})
	return found
}

// ResolveOperationalAddress resolves a peer's operational instance through
// the attached operational resolver (AttachOperationalResolver); nil
// without a resolver or an answer.
func (b *Bridge) ResolveOperationalAddress(ctx context.Context, compressedFabricID [8]byte, nodeID uint64) (*net.UDPAddr, error) {
	b.resumption.mu.Lock()
	r := b.resumption.resolver
	b.resumption.mu.Unlock()
	if r == nil {
		return nil, nil
	}
	addrs, err := r.ResolveOperational(ctx, compressedFabricID, nodeID)
	if err != nil || len(addrs) == 0 {
		return nil, err
	}
	return addrs[0], nil
}

// ActiveSubscriptionSubjects is the subject of every CASE session on
// fabricIndex that holds an active subscription, a session the bridge no
// longer resolves left out. Mirrors matter.js IcdCheckInSuppression.ts:39-53
// (activeSubscriptionSubjects), over the bridge's subscription targets.
func (b *Bridge) ActiveSubscriptionSubjects(fabricIndex uint8) []icd.SubscriptionSubject {
	var out []icd.SubscriptionSubject
	seen := map[uint16]bool{}
	b.routing.subTargets.Range(func(_, v any) bool {
		t, ok := v.(subTarget)
		if !ok || t.pase || t.fabricIndex != fabricIndex || seen[t.sessionID] {
			return true
		}
		if b.resolveSessionFabric(t.sessionID) != fabricIndex {
			return true // the session is gone, or closing
		}
		seen[t.sessionID] = true
		out = append(out, icd.SubscriptionSubject{NodeID: t.subjectNodeID, CATs: append([]uint32(nil), t.subjectCATs...)})
		return true
	})
	return out
}
