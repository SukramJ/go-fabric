// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"sync/atomic"

	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
)

// The send half of group communication: outbound group sessions, framed,
// sealed and privacy-obfuscated group messages to the group's multicast
// address. Mirrors matter.js at 0ceb202d:
// packages/protocol/src/session/SessionManager.ts:712-744
// (groupSessionForAddress), packages/protocol/src/session/GroupSession.ts
// (create, encode), packages/protocol/src/protocol/MessageExchange.ts:
// 640-776 (#send on a group session) and packages/protocol/src/groups/
// (Groups.currentKeyForId, KeySets.currentKeyForId, errors.ts).

// Errors of [Manager.Sender] and [Sender.Send].
var (
	// ErrKeyNotStarted is returned when every epoch key of the group's
	// key set starts in the future, so there is no current key to send
	// with. Mirrors matter.js GroupKeyNotStartedError
	// (packages/protocol/src/groups/errors.ts:21-26), thrown by
	// KeySets.currentKeyForId (KeySets.ts:126-132). Like [ErrNoKey] — its
	// sibling GroupKeySetMissingError (errors.ts:18-19) for a group
	// GroupKeyMap does not map, or maps to a key set the fabric does not
	// hold — it is a NoUsableGroupKeyError: provisioning that is
	// incomplete or not yet in effect.
	ErrKeyNotStarted = errors.New("groups: no epoch key of the group's key set has started")
	// ErrNoInterface is returned when no transport can reach the group's
	// multicast address. Mirrors GroupSession.create's "IPv6 interface not
	// initialized" (GroupSession.ts:144-148).
	ErrNoInterface = errors.New("groups: no interface for the group's multicast address")
	// ErrInvalidGroupID is returned for group id 0. Mirrors
	// GroupId.assertGroupId (packages/types/src/datatype/GroupId.ts:54-58)
	// as SessionManager.groupSessionForAddress applies it (:714-715).
	ErrInvalidGroupID = errors.New("groups: a group id must be specified and can not be 0")
	// ErrInvalidGroupOperation is returned for a request a group cannot
	// carry: a timed request, a path naming an endpoint, an incomplete
	// write path. Mirrors matter.js InvalidGroupOperationError as
	// ClientGroupInteraction raises it
	// (packages/node/src/node/client/ClientGroupInteraction.ts:34-81).
	ErrInvalidGroupOperation = errors.New("groups: operation not supported on a group")
)

// groupPort is where group messages are sent: the standard Matter port.
// Mirrors GroupSession.create opening its channel to STANDARD_MATTER_PORT
// (GroupSession.ts:150-153).
const groupPort = 5540

// MulticastTransport sends one datagram to an IPv6 multicast group —
// *udp.Listener (SendMulticast). It is the "UDP interface for the
// multicast address" of matter.js GroupSession.create
// (GroupSession.ts:144-153).
type MulticastTransport interface {
	SendMulticast(group net.IP, port int, payload []byte) error
}

// SetTransport sets the socket group messages leave through; nil
// detaches it, after which [Manager.Sender] answers [ErrNoInterface].
func (m *Manager) SetTransport(t MulticastTransport) {
	m.senderMu.Lock()
	m.transport = t
	m.senderMu.Unlock()
}

// senderKey identifies a cached outbound group session: the group it was
// asked for — the bucket SessionManager.ts:725-728 looks in — and, as
// GroupSession.matches (GroupSession.ts:226-237) compares them, the
// fabric, the group session id and the operational key. Session ids are a
// 16-bit hash of the key, so two key sets can share one; the key decides.
type senderKey struct {
	fabricIndex uint8
	groupID     uint16
	sessionID   uint16
	key         string
}

// Sender is an outbound group session: it sends group messages to one
// group of one fabric under the group's current operational key. Obtain
// one from [Manager.Sender]. Group sessions are long-lived and do not use
// MRP: nothing is acknowledged, retransmitted or answered
// (GroupSession.ts:74-75, MessageExchange.ts:660). Safe for concurrent
// use.
type Sender struct {
	m      *Manager
	closed atomic.Bool

	fabricIndex uint8
	groupID     uint16
	sessionID   uint16
	keySetID    uint16
	key         []byte
	privacyKey  []byte
	address     net.IP
	nodeID      uint64
}

// FabricIndex is the fabric the sender sends on.
func (s *Sender) FabricIndex() uint8 { return s.fabricIndex }

// GroupID is the destination group.
func (s *Sender) GroupID() uint16 { return s.groupID }

// SessionID is the group session id of the operational key — the
// Session ID of every message the sender sends.
func (s *Sender) SessionID() uint16 { return s.sessionID }

// KeySetID is the key set whose current epoch key the sender uses.
func (s *Sender) KeySetID() uint16 { return s.keySetID }

// Address is the IPv6 multicast address the sender sends to (port 5540).
func (s *Sender) Address() net.IP { return slices.Clone(s.address) }

// NodeID is this node's node id on the fabric: the source node id of the
// messages.
func (s *Sender) NodeID() uint64 { return s.nodeID }

// currentKey selects the key a sender uses for groupID: the key set
// GroupKeyMap maps the group to, and of it the epoch key with the latest
// start time that is not in the future. Mirrors Groups.currentKeyForId
// (Groups.ts:122-133) and KeySets.currentKeyForId (KeySets.ts:108-135):
//
//   - Groups.ts:123-126 no mapping, Groups.ts:127-131 a mapping to a key
//     set the fabric does not hold: GroupKeySetMissingError — [ErrNoKey];
//   - KeySets.ts:126-133 the epochs in slot order, those whose start time
//     (Timestamp.fromMicroseconds, microseconds to milliseconds) is not
//     after Time.nowMs, the last of them; none: GroupKeyNotStartedError —
//     [ErrKeyNotStarted].
//
// Key set 0, the IPK, never reaches here: the manager does not hold it
// (loadFabric), so a group mapped to it has no key set the fabric holds —
// in matter.js it has no session id and SessionManager.ts:719-723 fails.
// Called with m.mu held.
func (f *fabricGroups) currentKey(groupID uint16, nowMs int64) (uint16, epochKey, error) {
	ksID, ok := f.idMap[groupID]
	if !ok {
		return 0, epochKey{}, fmt.Errorf("%w: no group key set found for group %d", ErrNoKey, groupID)
	}
	ks, ok := f.keySets[ksID]
	if !ok {
		return 0, epochKey{}, fmt.Errorf("%w: group %d is mapped to group key set %d, which the fabric does not hold", ErrNoKey, groupID, ksID)
	}
	var (
		current epochKey
		found   bool
	)
	for _, e := range ks.epochs {
		// startTime <= now with startTime = start / 1000 in milliseconds,
		// compared exactly in microseconds.
		if nowMs >= 0 && e.start <= uint64(nowMs)*1000 {
			current, found = e, true
		}
	}
	if !found {
		return 0, epochKey{}, fmt.Errorf("%w: group key set %d has no current key to send with", ErrKeyNotStarted, ksID)
	}
	return ksID, current, nil
}

// Sender returns the outbound group session for groupID on the fabric.
// Mirrors matter.js SessionManager.groupSessionForAddress
// (SessionManager.ts:712-744) with GroupSession.create
// (GroupSession.ts:129-167):
//
//   - :714-715 group id 0 is refused ([ErrInvalidGroupID]);
//   - :717 the fabric must exist ([ErrNoFabric]);
//   - :718-723 the group's current key ([ErrNoKey], [ErrKeyNotStarted]);
//   - :725-732 a cached session for the same group, fabric, session id
//     and operational key is reused — never one matched by session id
//     alone;
//   - GroupSession.ts:141-142 the multicast address follows the group's
//     policy ([Manager.MulticastAddressFor]), fixed at creation as the
//     session's is;
//   - :144-148 without a transport there is no interface for it
//     ([ErrNoInterface]);
//   - :163 the privacy key is derived from the operational key;
//   - :739 the node-global group data counter numbers every message — a
//     store that cannot persist it refuses ([ErrNoCounterStore]).
func (m *Manager) Sender(ctx context.Context, fabricIndex uint8, groupID uint16) (*Sender, error) {
	if groupID == 0 {
		return nil, ErrInvalidGroupID
	}
	m.mu.Lock()
	f, err := m.fabricLocked(ctx, fabricIndex)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	keySetID, ek, err := f.currentKey(groupID, m.now().UnixMilli())
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	address, nodeID := f.multicastAddress(groupID), f.nodeID
	m.mu.Unlock()

	key := senderKey{fabricIndex: fabricIndex, groupID: groupID, sessionID: ek.sessionID, key: hex.EncodeToString(ek.key)}
	m.senderMu.Lock()
	defer m.senderMu.Unlock()
	if s, ok := m.senders[key]; ok {
		return s, nil
	}
	if m.transport == nil {
		return nil, ErrNoInterface
	}
	if _, err := m.groupDataCounter(ctx); err != nil {
		return nil, err
	}
	s := &Sender{
		m: m, fabricIndex: fabricIndex, groupID: groupID, sessionID: ek.sessionID, keySetID: keySetID,
		key: slices.Clone(ek.key), privacyKey: slices.Clone(ek.privacy), address: address, nodeID: nodeID,
	}
	m.senders[key] = s
	m.logger.Debug("matter.groups.sender_created",
		slog.Int("fabric_index", int(fabricIndex)), slog.Int("group_id", int(groupID)),
		slog.Int("session_id", int(ek.sessionID)), slog.Int("key_set", int(keySetID)), slog.String("address", address.String()))
	return s, nil
}

// closeSenders closes and drops the cached sessions of a removed fabric:
// a closed [Sender] answers [ErrNoFabric]. Mirrors the sessions a fabric
// holds (GroupSession registers with fabric.addSession,
// GroupSession.ts:104-105) closing with it, and GroupSession.close
// leaving the manager (:432-435).
func (m *Manager) closeSenders(fabricIndex uint8) {
	m.senderMu.Lock()
	defer m.senderMu.Unlock()
	for k, s := range m.senders {
		if k.fabricIndex == fabricIndex {
			s.closed.Store(true)
			delete(m.senders, k)
		}
	}
}

// nextExchangeID hands out the exchange id of a new initiated exchange: a
// random start, then incremented, wrapping past 0xFFFF to 0. Mirrors
// matter.js ExchangeCounter (packages/protocol/src/protocol/
// ExchangeManager.ts:729-743) as initiateExchangeForSession uses it
// (:145-146).
func (m *Manager) nextExchangeID() (uint16, error) {
	m.senderMu.Lock()
	defer m.senderMu.Unlock()
	if !m.exchangeSeeded {
		var b [2]byte
		if _, err := rand.Read(b[:]); err != nil {
			return 0, fmt.Errorf("groups: exchange id seed: %w", err)
		}
		m.exchangeID, m.exchangeSeeded = binary.LittleEndian.Uint16(b[:]), true
	}
	m.exchangeID++ // wraps 0xFFFF to 0 as :738-740 does
	return m.exchangeID, nil
}

// Send sends one message — protocol and opcode with their payload — to the
// group as the initiator of a new exchange. Mirrors matter.js
// MessageExchange.#send on a group session (MessageExchange.ts:640-776)
// with GroupSession.encode (GroupSession.ts:251-287) and
// MessageCodec.encodePacket (MessageCodec.ts:440-445):
//
//   - MessageExchange.ts:719 the message counter is the next value of the
//     node-global group data counter;
//   - :723-732 the packet header: Session ID the group session id,
//     Session Type Group, the counter, the destination group id, the
//     source node id our node id on the fabric, the P flag set
//     (GroupSession.usePrivacy, GroupSession.ts:173-179), no destination
//     node id, no message extensions;
//   - :660, :737-749 the payload header: a new exchange id, the initiator
//     flag, no acknowledgement requested and none carried — a group
//     session uses no MRP, so nothing is retransmitted (:752);
//   - GroupSession.ts:251-287 sealed under the operational group key and
//     privacy-obfuscated ([channel.EncryptGroup],
//     [channel.ObfuscateGroupHeader]);
//   - GroupSession.ts:150-153 sent to the multicast address on port 5540.
func (s *Sender) Send(ctx context.Context, payload []byte, protocol uint16, opcode uint8) error {
	if s.closed.Load() {
		return fmt.Errorf("%w: %d (its group session is closed)", ErrNoFabric, s.fabricIndex)
	}
	datagram, err := s.seal(ctx, payload, protocol, opcode)
	if err != nil {
		return err
	}
	s.m.senderMu.Lock()
	t := s.m.transport
	s.m.senderMu.Unlock()
	if t == nil {
		return ErrNoInterface
	}
	if err := t.SendMulticast(s.address, groupPort, datagram); err != nil {
		return fmt.Errorf("groups: send to [%s]:%d: %w", s.address, groupPort, err)
	}
	return nil
}

// seal builds the datagram of one group message.
func (s *Sender) seal(ctx context.Context, payload []byte, protocol uint16, opcode uint8) ([]byte, error) {
	counter, err := s.m.groupDataCounter(ctx)
	if err != nil {
		return nil, err
	}
	messageID, err := counter.next(ctx)
	if err != nil {
		return nil, err
	}
	exchangeID, err := s.m.nextExchangeID()
	if err != nil {
		return nil, err
	}
	return s.encode(messageID, exchangeID, payload, protocol, opcode)
}

// encode frames, seals and obfuscates one group message with the given
// message counter and exchange id.
func (s *Sender) encode(messageID uint32, exchangeID uint16, payload []byte, protocol uint16, opcode uint8) ([]byte, error) {
	hdr := message.Header{
		SessionID: s.sessionID, MessageCounter: messageID,
		HasSourceNodeID: true, SourceNodeID: s.nodeID,
		DestSize: message.DestGroup, DestGroupID: s.groupID,
		SessionType: message.SessionGroup, Privacy: true,
	}
	ph := message.ProtocolHeader{Initiator: true, Opcode: opcode, ExchangeID: exchangeID, ProtocolID: protocol}
	plain := append(ph.Marshal(), payload...)
	headerBytes, ciphertext, err := channel.EncryptGroup(s.key, s.nodeID, hdr, plain)
	if err != nil {
		return nil, err
	}
	wireHeader, err := channel.ObfuscateGroupHeader(s.privacyKey, s.sessionID, headerBytes, ciphertext)
	if err != nil {
		return nil, err
	}
	return append(wireHeader, ciphertext...), nil
}
