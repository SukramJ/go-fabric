// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sigma

import (
	"crypto/ecdh"
	"crypto/rand"
	"testing"
)

// sigma1ResumingWith builds a Sigma1 whose initiatorResumeMic is right for
// secret and the returned 16-byte resumption id.
func sigma1ResumingWith(t *testing.T, secret []byte) (sigma1Bytes, rid []byte) {
	t.Helper()
	rid = make([]byte, ResumptionIDSize)
	var random [RandomSize]byte
	if _, err := rand.Read(rid); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	key, err := hkdfDerive(secret, append(append([]byte(nil), random[:]...), rid...), HKDFInfoSigma1Resume, SessionKeySize)
	if err != nil {
		t.Fatal(err)
	}
	mic, err := sealResumeMIC(key, nonceResume1MIC)
	if err != nil {
		t.Fatal(err)
	}
	eph, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	enc := sigmaTLVEncoder()
	enc.startStruct()
	enc.putOctets(1, random[:])
	enc.putUint16(2, 0x1234)
	enc.putOctets(3, make([]byte, 32))
	enc.putOctets(4, eph.PublicKey().Bytes())
	enc.putOctets(6, rid)
	enc.putOctets(7, mic)
	enc.endContainer()
	return enc.bytes(), rid
}

// TestResumeRecordOfTheWrongShapeIsAMiss holds R2-P6-sec-0113 on the
// responder: a record whose shared secret is not 32 bytes or whose id is
// not 16 is treated as no record, and the Sigma1 gets a full Sigma2. With
// an empty secret both resume MICs are computable from the public random
// and resumption id, so resuming on it would hand anyone a session. The
// control is the same Sigma1 against a 32-byte secret: it resumes.
func TestResumeRecordOfTheWrongShapeIsAMiss(t *testing.T) {
	t.Parallel()
	resume := func(t *testing.T, secret []byte, recID func(rid []byte) []byte) bool {
		t.Helper()
		s1, rid := sigma1ResumingWith(t, secret)
		store := newStubStore()
		store.records[string(rid)] = &ResumptionRecord{SharedSecret: secret, ResumptionID: recID(rid), FabricIndex: 1, PeerNodeID: 0xAAAA}
		// The baseline carries the record's fabric index: a stamped record
		// under an unstamped baseline is a miss of its own
		// (R2-C2-sec-0101), which the control must not hit.
		id := newTestIdentity(t, 0xBBBB, 1, fabricIPK())
		id.FabricIndex = 1
		r := NewResponder(id, testVerifier{}, 0x2001)
		r.SetResumptionStore(store)
		res, err := r.ProcessSigma1WithResume(s1)
		if err != nil {
			t.Fatalf("ProcessSigma1WithResume: %v", err)
		}
		if !res.IsResume() && res.Sigma2 == nil {
			t.Fatal("neither a resume nor a full Sigma2")
		}
		return res.IsResume()
	}
	same := func(rid []byte) []byte { return rid }
	if !resume(t, make([]byte, 32), same) {
		t.Fatal("control: a 32-byte secret did not resume")
	}
	for name, c := range map[string]struct {
		secret []byte
		recID  func([]byte) []byte
	}{
		"empty secret":          {nil, same},
		"secret of 16 bytes":    {make([]byte, 16), same},
		"secret of 33 bytes":    {make([]byte, 33), same},
		"record id of 5 bytes":  {make([]byte, 32), func(rid []byte) []byte { return rid[:5] }},
		"record id of no bytes": {make([]byte, 32), func([]byte) []byte { return nil }},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if resume(t, c.secret, c.recID) {
				t.Error("resumed on a record of the wrong shape, want a full Sigma2")
			}
		})
	}
}

// TestPeerInitiatorIgnoresAResumptionRecordOfTheWrongShape holds
// R2-P6-sec-0113 on the initiator: a record whose shared secret is not 32
// bytes is not offered — the Sigma1 carries no resumption id — as one
// whose id is not 16 bytes already was not.
func TestPeerInitiatorIgnoresAResumptionRecordOfTheWrongShape(t *testing.T) {
	t.Parallel()
	f := newPeerFixture(t)
	offers := func(secret []byte) bool {
		i := f.initiator(t, 0x1111, &ResumptionRecord{SharedSecret: secret, ResumptionID: make([]byte, ResumptionIDSize), FabricIndex: 1, PeerNodeID: f.controller.NodeID})
		b, err := i.GenerateSigma1()
		if err != nil {
			t.Fatal(err)
		}
		s1, err := UnmarshalSigma1(b)
		if err != nil {
			t.Fatal(err)
		}
		return s1.ResumptionID != nil
	}
	if !offers(make([]byte, 32)) {
		t.Fatal("control: a 32-byte secret was not offered")
	}
	for _, n := range []int{1, 16, 31, 33} {
		if offers(make([]byte, n)) {
			t.Errorf("a %d-byte secret was offered for resumption", n)
		}
	}
}
