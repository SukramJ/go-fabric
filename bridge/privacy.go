// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
)

// privacyMICSuffix is the trailing-MIC byte count that participates
// in the privacy IV. Matches [channel.PrivacyMICSuffixSize] but kept
// local so this file's intent stays self-contained.
const privacyMICSuffix = channel.PrivacyMICSuffixSize

// secFlagPrivacyBit and secFlagSessionTypeBits mirror the transport
// package's unexported Security Flags layout (P bit 7, session type in
// bits 1-0). Re-declared here so the bridge layer can inspect a still-
// masked frame without decoding it.
const (
	secFlagPrivacyBit      = 0x80
	secFlagSessionTypeBits = 0x03
)

// maybeUnmaskPrivacy applies the Matter §4.7.3.1 inbound privacy
// unmask to buf in place when the inbound datagram carries the P
// bit. No-op on non-Privacy frames; returns an error when SessionID
// resolution fails or the frame is too short to carry both header
// + 16-byte MIC.
//
// Layout assumed (Matter §4.4.1):
//
//	byte 0     Message Flags (unprotected)
//	bytes 1-2  SessionID (unprotected)
//	byte 3     Security Flags (unprotected — P bit lives here)
//	byte 4..   MessageCounter (4) + optional Source/Dest NodeIDs (0/2/8/10/16)
//	last 16    AES-CCM MIC (after ciphertext body)
//
// The privacy mask covers bytes 4..end-of-header — the whole
// counter + node-id region, up to 20 bytes — with the AES-CTR
// keystream [channel.PrivacyKeystream] derives from the session's
// privacy key, the SessionID and the frame's MIC.
//
// Privacy enhancements are only defined for group messages (Matter
// §4.9 applies them to group sessions). A unicast frame carrying the P
// bit is invalid and is dropped — matter.js ExchangeManager.ts:220-224
// and the chip SDK do the same — rather than unmasked and handed to a
// unicast session. SessionID==0 (PASE pre-fabric) likewise MUST NOT
// carry the P bit; both are reported as errors so the caller drops the
// datagram.
func (b *Bridge) maybeUnmaskPrivacy(buf []byte) error {
	if len(buf) < 4 {
		// Too short — caller's UnmarshalHeader will fail with a
		// clearer error. Don't shadow it.
		return nil
	}
	secFlags := buf[3]
	if secFlags&secFlagPrivacyBit == 0 {
		return nil // not privacy-protected
	}
	sessionID := binary.LittleEndian.Uint16(buf[1:3])
	if sessionID == 0 {
		return errors.New("privacy: P bit set on SessionID=0 (PASE) — spec violation")
	}
	if message.SessionType(secFlags&secFlagSessionTypeBits) != message.SessionGroup {
		return errors.New("privacy: P bit set on a unicast message — dropped (privacy is defined for group messages only)")
	}

	b.mu.RLock()
	lookup := b.sessions
	b.mu.RUnlock()
	if lookup == nil {
		return errors.New("privacy: no session lookup wired")
	}
	sess, ok := lookup.Lookup(sessionID)
	if !ok {
		return fmt.Errorf("privacy: session %d not found", sessionID)
	}
	privacyKey, err := sess.PeerPrivacyKey()
	if err != nil {
		return fmt.Errorf("privacy: derive peer key: %w", err)
	}

	// MIC is the last 16 bytes of the datagram (AES-CCM-128 default
	// tag size). Need at least 4 + 16 bytes total to compute the
	// mask + have something to mask.
	if len(buf) < 4+privacyMICSuffix {
		return fmt.Errorf("privacy: datagram too short (%d bytes) for MIC tail", len(buf))
	}
	mic := buf[len(buf)-16:] // 16-byte MIC; the nonce uses its last 11 bytes

	// Unmask the whole protected suffix of the message header (the
	// MIC is body-tail and stays outside it).
	hdrEnd := privacyHeaderEnd(buf)
	if hdrEnd <= 4 {
		return nil // header has no protected portion (only flags + sessionID)
	}
	protected := buf[4:hdrEnd]
	mask, err := channel.PrivacyKeystream(privacyKey, sessionID, mic, len(protected))
	if err != nil {
		return fmt.Errorf("privacy: mask derive: %w", err)
	}
	return channel.ApplyPrivacyMask(mask, protected)
}

// privacyHeaderEnd returns the byte offset where the header ends and
// the encrypted body begins. Replicates the layout decision in
// `message.UnmarshalHeader` without re-parsing the (still-masked)
// counter/NodeID fields. We only need to know how far the protected
// suffix extends, not the actual values.
func privacyHeaderEnd(buf []byte) int {
	if len(buf) < 4 {
		return len(buf)
	}
	flags := buf[0]
	end := 4 + 4 // flags + sessionID + secflags + counter
	if flags&0x04 != 0 {
		end += 8 // SourceNodeID
	}
	switch flags & 0x03 {
	case 1:
		end += 8 // DestNodeID 64-bit
	case 2:
		end += 2 // DestNodeID 16-bit (group)
	}
	if end > len(buf) {
		return len(buf)
	}
	return end
}

// applyOutboundPrivacy applies the Matter §4.7.3.1 outbound mask to
// the freshly-encrypted datagram bytes when the message header's
// Security Flags carry the P bit. Caller has already produced the
// final wire bytes (header || ciphertext || MIC); this helper masks
// the privacy-protected header suffix in place.
//
// Called from the secure reply path in reply.go: the response header
// echoes the request's Privacy flag, so a peer that sends
// privacy-protected unicast gets a reply whose header suffix is masked
// as the P bit announces. Ordinary traffic clears the bit and the
// helper returns immediately.
func applyOutboundPrivacy(sess *channel.Session, sessionID uint16, datagram []byte) error {
	if sess == nil || len(datagram) < 4 {
		return nil
	}
	if datagram[3]&secFlagPrivacyBit == 0 {
		return nil
	}
	if len(datagram) < 4+privacyMICSuffix {
		return fmt.Errorf("privacy outbound: datagram too short (%d bytes) for MIC tail", len(datagram))
	}
	privacyKey, err := sess.PrivacyKey()
	if err != nil {
		return fmt.Errorf("privacy outbound: derive key: %w", err)
	}
	mic := datagram[len(datagram)-16:]
	hdrEnd := privacyHeaderEnd(datagram)
	if hdrEnd <= 4 {
		return nil
	}
	protected := datagram[4:hdrEnd]
	mask, err := channel.PrivacyKeystream(privacyKey, sessionID, mic, len(protected))
	if err != nil {
		return fmt.Errorf("privacy outbound: mask derive: %w", err)
	}
	return channel.ApplyPrivacyMask(mask, protected)
}
