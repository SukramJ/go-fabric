// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/secure/aesccm"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// Errors returned by [Manager.Decode]. Every one of them means the
// datagram is dropped without an answer.
var (
	// ErrNotGroupMessage is returned for a datagram whose Security Flags
	// do not name a group session.
	ErrNotGroupMessage = errors.New("groups: not a group message")
	// ErrNoKey is returned when no key set mapped to a group in
	// GroupKeyMap has an epoch key with the message's group session id.
	// Mirrors matter.js GroupSessionNoKeyError.
	ErrNoKey = errors.New("groups: no key candidate for group message")
	// ErrDecrypt is returned when the message authenticated under none of
	// the candidate keys. Mirrors matter.js GroupSessionDecodeError.
	ErrDecrypt = errors.New("groups: group message failed to authenticate")
	// ErrMalformed is returned for a group message that authenticated but
	// carries no source node id or no group id.
	ErrMalformed = errors.New("groups: malformed group message")
)

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
	// KeySetID is the key set that authenticated the message.
	KeySetID uint16
	// HasValidMapping reports whether the key that authenticated the
	// message belongs to the key set GroupKeyMap maps GroupID to. Two key
	// sets with identical key material share a session id, so the one
	// that decrypted may differ from the one the group is mapped to.
	// Without a valid mapping the message carries no Group subject and
	// no access control entry can grant it anything. Mirrors matter.js
	// Groups.subjectForGroup.
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
// Mirrors matter.js GroupSession.decode and SessionManager
// .groupSessionFromPacket (packages/protocol/src/session/GroupSession.ts,
// SessionManager.ts): the candidates are the epoch keys, across every
// fabric, whose group session id equals the message's session id; only
// key sets that GroupKeyMap maps a group to participate ("matching the
// CHIP SDK, only mapped key sets count as available"); each candidate is
// tried in turn — privacy-deobfuscating the header with its privacy key
// when the P flag is set, then opening the AEAD with the nonce built from
// the Security Flags, the message counter and the source node id — and
// the first that authenticates wins. The message counter then passes the
// per-key, per-sender reception window (matter.js
// MessagingState.receptionStateFor, an encrypted-with-rollover window).
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
	if err := m.ensureAllLoaded(ctx); err != nil {
		return nil, err
	}

	// The source node id builds the nonce. Its presence (the S flag in
	// the unobfuscated Message Flags byte) is the same for every
	// candidate; matter.js ends the decode on its absence
	// (GroupSession.decode, UnexpectedDataError).
	if datagram[0]&0x04 == 0 {
		return nil, fmt.Errorf("%w: no source node id", ErrMalformed)
	}
	cands := m.candidates(sessionID)
	if len(cands) == 0 {
		return nil, ErrNoKey
	}
	privacy := secFlags&0x80 != 0
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
		m.completeMessage(msg, c)
		return msg, nil
	}
	return nil, ErrDecrypt
}

// candidates collects the mapped key sets' epoch keys with sessionID,
// ordered by fabric index and key set id so the outcome does not depend on
// map iteration.
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
		f := m.fabrics[idx]
		mapped := make(map[uint16]bool, len(f.idMap))
		for _, ks := range f.idMap {
			mapped[ks] = true
		}
		ids := make([]uint16, 0, len(f.keySets))
		for id := range f.keySets {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			if !mapped[id] {
				continue
			}
			for _, e := range f.keySets[id].epochs {
				if e.sessionID == sessionID {
					out = append(out, candidate{fabric: f, keySetID: id, epoch: e})
				}
			}
		}
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

// completeMessage fills in the subject and runs the reception window.
func (m *Manager) completeMessage(msg *Message, c candidate) {
	msg.FabricIndex = c.fabric.index
	msg.GroupID = msg.Header.DestGroupID
	msg.SourceNodeID = msg.Header.SourceNodeID
	msg.KeySetID = c.keySetID

	m.mu.RLock()
	if mappedID, ok := c.fabric.idMap[msg.GroupID]; ok {
		if ks, ok := c.fabric.keySets[mappedID]; ok {
			msg.HasValidMapping = slices.ContainsFunc(ks.epochs, func(e epochKey) bool { return slices.Equal(e.key, c.epoch.key) })
		}
	}
	if e, ok := c.fabric.table[msg.GroupID]; ok {
		msg.Endpoints = slices.Clone(e.Endpoints)
	}
	m.mu.RUnlock()

	msg.Duplicate = !m.receptionWindow(c.epoch.key, msg.SourceNodeID).Accept(msg.Header.MessageCounter)
}

// receptionWindow returns the reception state for (key, source node),
// creating it on first use. Only authenticated messages reach here, so
// the map grows with genuine senders only.
func (m *Manager) receptionWindow(key []byte, source uint64) *mrp.Window {
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
		w = mrp.NewWindowEncryptedRollover()
		perKey[source] = w
	}
	return w
}
