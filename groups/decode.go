// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"

	"thingfabric.dev/go-fabric/secure/aesccm"
	"thingfabric.dev/go-fabric/secure/channel"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// Errors returned by [Manager.Decode]. Every one of them means the
// datagram is dropped without an answer.
var (
	// ErrNotGroupMessage is returned for a datagram whose Security Flags
	// do not name a group session.
	ErrNotGroupMessage = errors.New("groups: not a group message")
	// ErrNoKey is what a [DecodeError] without a candidate key is: no
	// key set mapped to a group in GroupKeyMap has an epoch key with the
	// message's group session id. Mirrors the first message of matter.js
	// GroupSessionDecodeError (packages/protocol/src/session/
	// GroupSession.ts:94-98 at db021da0).
	ErrNoKey = errors.New("groups: no key candidate for group message")
	// ErrDecrypt is what a [DecodeError] with candidate keys is: no
	// candidate authenticated the message for its destination group.
	// Mirrors the second message of matter.js GroupSessionDecodeError
	// (GroupSession.ts:94-98 at db021da0).
	ErrDecrypt = errors.New("groups: group message failed to authenticate")
	// ErrMalformed is returned for a group message that authenticated but
	// carries no source node id or no group id.
	ErrMalformed = errors.New("groups: malformed group message")
)

// NoKeyError was the [ErrNoKey] outcome with the fabric and group an
// unmapped key set recovered.
//
// Deprecated: [Manager.Decode] no longer returns it — a key set that
// GroupKeyMap maps no group to is never tried, as in matter.js
// (GroupSession.ts:366-376 at db021da0). A message no candidate passes is
// a [DecodeError]. Removable in v0.25.0.
type NoKeyError struct {
	// Authenticated reports that an unmapped key set opened the message;
	// FabricIndex and GroupID are set only then.
	Authenticated bool
	FabricIndex   uint8
	GroupID       uint16
}

// DecodeError is the outcome of a group message for which no candidate
// key passed: none authenticated it for its destination group, or there
// was no candidate at all. Any sender can cause it. It holds what is known
// about the message for the Groupcast testing report and nothing that
// lets it be processed. errors.Is matches [ErrNoKey] without candidates
// and [ErrDecrypt] with them. Mirrors matter.js GroupSessionDecodeError
// and GroupMessageRejection (packages/protocol/src/session/
// GroupSession.ts:37-102 at db021da0).
type DecodeError struct {
	// CandidateFabrics are the fabrics with at least one candidate key —
	// a key with the message's session id whose key set GroupKeyMap maps
	// to a group — ascending and each once; empty when there was none.
	CandidateFabrics []uint8
	// GroupID is the group id the message names (HasGroupID), to derive
	// the address it arrived on — never to be reported as the group:
	// the destination of the first candidate that authenticated the
	// message, else the id of a plain header; unset behind privacy when
	// no candidate authenticated it.
	GroupID    uint16
	HasGroupID bool
	// GroupFabricIndex is the fabric whose candidate key authenticated
	// GroupID, so it owns the id; 0 for an id from a plain header. It is
	// not the fabric to report on.
	GroupFabricIndex uint8
}

func (e *DecodeError) Error() string {
	if len(e.CandidateFabrics) == 0 {
		return ErrNoKey.Error()
	}
	return ErrDecrypt.Error()
}

// Is makes errors.Is(err, ErrNoKey) hold without a candidate and
// errors.Is(err, ErrDecrypt) with one.
func (e *DecodeError) Is(target error) bool {
	if len(e.CandidateFabrics) == 0 {
		return target == ErrNoKey
	}
	return target == ErrDecrypt
}

// Event is the report of the rejected message from sourceIP: it names no
// fabric, since no key passed, and carries the candidate fabrics and the
// group id with its owner for the listener — which reports it on its own
// fabric with [GroupMessageEvent.ResultFor]. Mirrors matter.js
// SessionManager.groupSessionFromPacket emitting `{ kind:
// "unauthenticated", candidateFabrics, group, sourceIp }`
// (packages/protocol/src/session/SessionManager.ts:809-820 at db021da0).
func (e *DecodeError) Event(sourceIP net.IP) GroupMessageEvent {
	ev := GroupMessageEvent{
		Result:                 TestResultNoAvailableKey,
		CandidateFabrics:       slices.Clone(e.CandidateFabrics),
		HeaderGroupID:          e.GroupID,
		HasHeaderGroupID:       e.HasGroupID,
		HeaderGroupFabricIndex: e.GroupFabricIndex,
		SourceIP:               slices.Clone(sourceIP),
	}
	if len(e.CandidateFabrics) > 0 {
		ev.Result = TestResultFailedAuth
	}
	return ev
}

func (e *NoKeyError) Error() string {
	if e.Authenticated {
		return fmt.Sprintf("%s (an unmapped key set of fabric %d authenticated group %d)", ErrNoKey, e.FabricIndex, e.GroupID)
	}
	return ErrNoKey.Error()
}

// Is makes errors.Is(err, ErrNoKey) hold.
func (e *NoKeyError) Is(target error) bool { return target == ErrNoKey }

// Message is a group message that authenticated under one of this node's
// operational group keys.
type Message struct {
	// Header is the message header with the privacy obfuscation removed.
	Header message.Header
	// Payload is the decrypted message: protocol header and application
	// payload.
	Payload []byte
	// FabricIndex is the fabric whose key set authenticated the message.
	FabricIndex uint8
	// GroupID is the destination group — authoritative only now, since
	// privacy obfuscates it on the wire.
	GroupID uint16
	// SourceNodeID is the sender.
	SourceNodeID uint64
	// LocalNodeID is this node's operational node id on FabricIndex — the
	// id a path of the message that names a node is compared with
	// (matter.js DataResponse.nodeId, the node's id on the session's
	// fabric, packages/protocol/src/action/server/DataResponse.ts:57-62 at
	// b25ed960).
	LocalNodeID uint64
	// KeySetID is the key set that authenticated the message.
	KeySetID uint16
	// HasValidMapping reports whether the key that authenticated the
	// message belongs to the key set GroupKeyMap maps GroupID to. It is
	// always true for a message Decode returns: a candidate key the group
	// is not mapped to fails there, and the next one is tried (matter.js
	// GroupSession.ts:431-433 at db021da0). Two key sets with identical
	// key material share a session id, so the one that decrypted may
	// differ from the one the group is mapped to; the check compares key
	// material. Mirrors matter.js Groups.subjectForGroup.
	HasValidMapping bool
	// Endpoints are the local endpoints that are members of GroupID on
	// the fabric, in the order they joined.
	Endpoints []uint16
	// Duplicate reports a message counter the reception state has seen
	// for this sender and key already. Mirrors matter.js
	// ExchangeManager's DuplicateMessageError branch for group sessions.
	Duplicate bool
}

// candidate is one operational key a group message may be encrypted
// under: matter.js's `{ key, privacyKey, keySetId, fabric }`.
type candidate struct {
	fabric   *fabricGroups
	keySetID uint16
	epoch    epochKey
}

// Decode authenticates a group message and returns it with the subject it
// is evaluated under.
//
// Mirrors matter.js GroupSession.decodeWithCandidateKeys and
// SessionManager.groupSessionFromPacket at db021da0
// (packages/protocol/src/session/GroupSession.ts:330-462,
// SessionManager.ts:806-820): the candidates are the epoch keys, across
// every fabric, whose group session id equals the message's session id;
// only key sets that GroupKeyMap maps a group to participate ("As in the
// CHIP SDK, a key set that no group maps to is never tried",
// GroupSession.ts:371-374); each candidate is tried in turn —
// privacy-deobfuscating the header with its privacy key when the P flag
// is set, then opening the AEAD with the nonce built from the Security
// Flags, the message counter and the source node id. A candidate that
// opens the message still fails when GroupKeyMap does not map the
// message's group to a key set holding its key (:425-433), or when its
// payload's protocol header does not parse (:435-442), and the next one
// is tried; the first that passes wins. Only then does the message
// counter pass the per-key, per-sender reception window (matter.js
// MessagingState.receptionStateFor, an encrypted-with-rollover window).
// When none passes, the error is a [*DecodeError] with the candidate
// fabrics and the group the message names (:454-461).
//
// The input is not modified.
func (m *Manager) Decode(ctx context.Context, datagram []byte) (*Message, error) {
	if len(datagram) < 4 {
		return nil, fmt.Errorf("%w: %d bytes", ErrMalformed, len(datagram))
	}
	secFlags := datagram[3]
	if message.SessionType(secFlags&0x03) != message.SessionGroup {
		return nil, ErrNotGroupMessage
	}
	sessionID := binary.LittleEndian.Uint16(datagram[1:3])
	if err := m.ensureAllLoaded(ctx, loadForDatagram); err != nil {
		return nil, err
	}

	// The source node id builds the nonce. Its presence (the S flag in
	// the unobfuscated Message Flags byte) is the same for every
	// candidate; matter.js ends the decode on its absence
	// (GroupSession.decode, UnexpectedDataError).
	if datagram[0]&0x04 == 0 {
		return nil, fmt.Errorf("%w: no source node id", ErrMalformed)
	}
	privacy := secFlags&0x80 != 0
	cands := m.candidates(sessionID)
	rejected := &DecodeError{}
	for _, c := range cands {
		if !slices.Contains(rejected.CandidateFabrics, c.fabric.index) {
			rejected.CandidateFabrics = append(rejected.CandidateFabrics, c.fabric.index)
		}
	}
	for _, c := range cands {
		msg, ok := tryDecrypt(datagram, sessionID, privacy, c)
		if !ok {
			continue
		}
		if msg.Header.DestSize != message.DestGroup || msg.Header.DestGroupID == 0 {
			// The group id is authoritative only after decryption;
			// matter.js requires one and rejects group id 0
			// (SessionManager.groupSessionFromPacket, GroupId.assertGroupId).
			return nil, fmt.Errorf("%w: group id missing", ErrMalformed)
		}
		// The first candidate that opens the message owns the id it
		// names (GroupSession.ts:429, `group ??=`).
		if !rejected.HasGroupID {
			rejected.GroupID, rejected.HasGroupID, rejected.GroupFabricIndex = msg.Header.DestGroupID, true, c.fabric.index
		}
		if !m.mappingAllows(c, msg.Header.DestGroupID) {
			continue // GroupSession.ts:431-433
		}
		// An authenticated payload whose protocol header does not parse
		// fails this candidate and the next is tried, before the
		// reception window takes the counter: matter.js decodes the
		// payload inside the candidate loop and continues on its
		// UnexpectedDataError, "As in the CHIP SDK" (GroupSession.ts:435-442
		// at db021da0); connectedhomeip's group key attempt fails when
		// SecureMessageCodec::Decrypt, which decodes the payload header
		// after the decryption, fails, and goes on to the next key
		// (src/transport/SessionManager.cpp:1148-1151, :1213-1230;
		// src/transport/SecureMessageCodec.cpp:91-93 at 6170af84). Review
		// II, F-db021da0-35.
		if _, _, err := message.UnmarshalProtocolHeader(msg.Payload); err != nil {
			continue
		}
		if err := m.completeMessage(msg, c); err != nil {
			return nil, err
		}
		return msg, nil
	}
	// With no candidate that opened it, the group id is the plain
	// header's, without a fabric; behind privacy there is none
	// (GroupSession.ts:454-456).
	if !rejected.HasGroupID && !privacy {
		if hdr, _, err := message.UnmarshalHeader(datagram); err == nil && hdr.DestSize == message.DestGroup {
			rejected.GroupID, rejected.HasGroupID = hdr.DestGroupID, true
		}
	}
	return nil, rejected
}

// mappingAllows reports whether GroupKeyMap maps groupID to a key set
// that holds c's operational key. It compares key material, so a key of
// another key set with identical material is allowed too. Mirrors
// matter.js Groups.mappingAllows and KeySets.containsOperationalKey
// (packages/protocol/src/groups/Groups.ts:78-81,
// packages/protocol/src/groups/KeySets.ts:44-49 at b25ed960).
func (m *Manager) mappingAllows(c candidate, groupID uint16) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mappedID, ok := c.fabric.idMap[groupID]
	if !ok {
		return false
	}
	ks, ok := c.fabric.keySets[mappedID]
	if !ok {
		return false
	}
	return slices.ContainsFunc(ks.epochs, func(e epochKey) bool { return slices.Equal(e.key, c.epoch.key) })
}

// candidates collects the epoch keys with sessionID of the key sets that
// GroupKeyMap maps a group to, ordered by fabric index and key set id so
// the outcome does not depend on map iteration. Each fabric's keys are
// looked up in its session index (fabricGroups.bySession), so the cost
// is a walk of the fabrics whatever the datagram's session id (review II,
// R2-C2-bound-0301). Mirrors matter.js GroupSession.ts:366-376 at
// db021da0 (fabric.groups.isKeySetMapped).
func (m *Manager) candidates(sessionID uint16) []candidate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	idxs := make([]uint8, 0, len(m.fabrics))
	for idx := range m.fabrics {
		idxs = append(idxs, idx)
	}
	slices.Sort(idxs)
	var out []candidate
	for _, idx := range idxs {
		out = append(out, m.fabrics[idx].bySession[sessionID]...)
	}
	return out
}

// tryDecrypt attempts one candidate on a private copy of the datagram.
func tryDecrypt(datagram []byte, sessionID uint16, privacy bool, c candidate) (*Message, bool) {
	buf := slices.Clone(datagram)
	if len(buf) < 4+aesccm.TagSize {
		return nil, false
	}
	if privacy {
		mic := buf[len(buf)-aesccm.TagSize:]
		end := protectedHeaderEnd(buf)
		if end > len(buf)-aesccm.TagSize {
			return nil, false
		}
		region := buf[4:end]
		mask, err := channel.PrivacyKeystream(c.epoch.privacy, sessionID, mic, len(region))
		if err != nil || channel.ApplyPrivacyMask(mask, region) != nil {
			return nil, false
		}
	}
	hdr, hdrLen, err := message.UnmarshalHeader(buf)
	if err != nil || hdrLen > len(buf) {
		return nil, false // a wrong privacy key yields a garbage header
	}
	nonce := make([]byte, aesccm.NonceSize)
	nonce[0] = hdr.NonceSecurityFlags()
	binary.LittleEndian.PutUint32(nonce[1:5], hdr.MessageCounter)
	binary.LittleEndian.PutUint64(nonce[5:13], hdr.SourceNodeID)
	plain, err := c.epoch.ccm.Open(nil, nonce, buf[hdrLen:], hdr.AAD())
	if err != nil {
		return nil, false
	}
	return &Message{Header: hdr, Payload: plain}, true
}

// protectedHeaderEnd returns where the privacy-protected header region —
// message counter and the optional source and destination node ids —
// ends. The Message Flags byte that sizes it is not obfuscated.
func protectedHeaderEnd(buf []byte) int {
	flags := buf[0]
	end := 8 // flags + session id + security flags + counter
	if flags&0x04 != 0 {
		end += 8
	}
	switch flags & 0x03 {
	case 1:
		end += 8
	case 2:
		end += 2
	}
	return end
}

// completeMessage fills in the subject of a message whose candidate
// passed [Manager.mappingAllows] and runs the reception window; an error
// when the key's reception state holds no window for the sender and has
// no room for one ([Manager.receptionWindow]), and [errFabricChanged]
// when the candidate's fabric is no longer the one installed.
//
// The installed-fabric check, the window and its Accept share one m.mu
// read lock: ForgetFabric deletes the fabric under the write lock, so a
// decode either takes its counter before the delete — into the window
// ForgetFabric then parks — or sees the fabric gone and is dropped
// (review II, R2-P7-sec-02). Lock order m.mu → receptionMu; no path takes
// m.mu while holding receptionMu.
func (m *Manager) completeMessage(msg *Message, c candidate) error {
	msg.FabricIndex = c.fabric.index
	msg.GroupID = msg.Header.DestGroupID
	msg.SourceNodeID = msg.Header.SourceNodeID
	msg.KeySetID = c.keySetID
	msg.HasValidMapping = true

	m.mu.RLock()
	if m.fabrics[c.fabric.index] != c.fabric {
		m.mu.RUnlock()
		return fmt.Errorf("%w: fabric %d, group %#04x", errFabricChanged, msg.FabricIndex, msg.GroupID)
	}
	msg.LocalNodeID = c.fabric.nodeID
	if e, ok := c.fabric.table[msg.GroupID]; ok {
		msg.Endpoints = slices.Clone(e.Endpoints)
	}
	w, ok := m.receptionWindow(c.epoch.key, msg.SourceNodeID)
	if ok {
		msg.Duplicate = !w.Accept(msg.Header.MessageCounter)
	}
	m.mu.RUnlock()
	if !ok {
		if m.firstRefusal(c.epoch.key) {
			m.logger.Warn("groups.reception.full",
				slog.Int("fabric_index", int(msg.FabricIndex)),
				slog.Int("group_id", int(msg.GroupID)),
				slog.Uint64("source_node_id", msg.SourceNodeID),
				slog.Int("max", maxGroupSendersPerKey))
		}
		return fmt.Errorf("%w: source node %#016x, group %#04x", errReceptionFull, msg.SourceNodeID, msg.GroupID)
	}
	return nil
}

// errFabricChanged drops a message whose candidate's fabric was forgotten
// or reloaded while the message was being decrypted: the reception window
// is taken only for the fabric state the candidate came from, so a forget
// in between neither hands the message a fresh, unprimed window nor takes
// a counter the parked window misses (review II, R2-P7-sec-02). The
// sender's next message decodes against the state now installed.
var errFabricChanged = errors.New("groups: the candidate's fabric changed during the decode")

// maxGroupSendersPerKey bounds the source node ids an operational key
// holds reception state for. Because a sender beyond it is refused for as
// long as the key is held ([Manager.receptionWindow]), it is a hard cap on
// the group senders one key set serves — senders that left (removed or
// re-commissioned nodes) keep their places until the key goes — so the
// number is chosen from memory, not from a reference: a window is a
// counter, a 32-bit bitmap and a map entry, so 256 senders cost tens of
// kilobytes per key at most. connectedhomeip's CHIP_CONFIG_MAX_GROUP_DATA_PEERS
// (15, src/lib/core/CHIPConfig.h:832-834 at 6170af84) is the size of an
// LRU cache that evicts, not a cap, and is not the source of this one.
const maxGroupSendersPerKey = 256

// errReceptionFull refuses the message of a sender the key's reception
// state does not hold, at [maxGroupSendersPerKey].
var errReceptionFull = errors.New("groups: the operational key's reception state holds its bound of senders")

// firstRefusal reports whether key has refused no sender at its bound
// before, and marks it: the refusal is logged once per key, until the
// key's reception state is dropped (forgetUnreferenced).
func (m *Manager) firstRefusal(key []byte) bool {
	k := hex.EncodeToString(key)
	m.receptionMu.Lock()
	defer m.receptionMu.Unlock()
	if m.receptionFull[k] {
		return false
	}
	m.receptionFull[k] = true
	return true
}

// receptionWindow returns the reception state for (key, source node),
// creating it on first use; ok is false when the key holds no window for
// source and already holds [maxGroupSendersPerKey].
//
// matter.js keeps one reception state per operational key and source node
// without a bound (packages/protocol/src/groups/MessagingState.ts:19,
// :80-90 at db021da0); connectedhomeip bounds the senders per fabric — 15
// data, 2 control by default (src/lib/core/CHIPConfig.h:832-844) — and
// replaces the least recently used sender when the table is full
// (src/transport/GroupPeerMessageCounter.cpp:59-98). A table that evicts
// must trust the first counter of a sender it no longer holds, and a
// party without the key can then replay what it recorded; this package
// did while it was bounded that way (review II, R2-P0-rv-B01; pinned by
// TestRecordedGroupMessageStaysADuplicate). So the bound here evicts
// nobody: a sender beyond it that the table does not hold yet is refused
// (decision 133, ahead of matter.js). Every held sender keeps its replay
// protection whole, and what a holder of the key can deny by stating
// source node ids is its own key's group traffic — a key belongs to one
// fabric's key set, so the fault stays inside the fabric that caused it.
// Only a message that authenticated under a mapped key reaches here
// ([Manager.Decode]; pinned by
// TestOnlyAnAuthenticatedGroupMessageCreatesReceptionState), so a party
// without the key takes no place.
//
// The state is keyed on the operational key, not on fabric and key set:
// key sets of one fabric that derive the same key share one window, as in
// matter.js (MessagingState.ts:80-90, receptionStateFor). matter.js keeps
// that state per fabric (packages/protocol/src/groups/FabricGroups.ts:33,
// :67 at db021da0) and this manager one map for the node: the operational
// key is derived with the fabric's compressed id as salt
// ([OperationalKey]), so two fabrics derive the same key only under the
// same compressed fabric id (review II, TM R3.6). It is dropped with the
// last key set deriving the key — [Manager.Reload] after KeySetWrite /
// KeySetRemove and [Manager.ForgetFabric] run forgetUnreferenced, matter.js
// packages/protocol/src/groups/FabricGroups.ts:207-225
// (#forgetUnreferencedReceptionState). Pinned by
// TestReceptionStateTeardownParity (review 2026-11 W2-sec-05).
func (m *Manager) receptionWindow(key []byte, source uint64) (*mrp.Window, bool) {
	k := hex.EncodeToString(key)
	m.receptionMu.Lock()
	defer m.receptionMu.Unlock()
	perKey, ok := m.reception[k]
	if !ok {
		perKey = make(map[uint64]*mrp.Window)
		m.reception[k] = perKey
	}
	w, ok := perKey[source]
	if !ok {
		if len(perKey) >= maxGroupSendersPerKey {
			return nil, false
		}
		w = mrp.NewWindowEncryptedRollover()
		perKey[source] = w
	}
	return w, true
}
