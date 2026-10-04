// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package sigma

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"
)

// nocVerifier is a test verifier whose NOCs are bare public keys and whose
// node / fabric ids and CATs come from a table keyed by NOC — enough to
// drive the peer-identity checks [NewPeerInitiator] adds.
type nocVerifier struct {
	nodes   map[string]uint64
	fabrics map[string]uint64
	cats    map[string][]uint32
}

func (nocVerifier) VerifyAndExtractPubKey(noc, _ []byte) (*ecdsa.PublicKey, error) {
	return testVerifier{}.VerifyAndExtractPubKey(noc, nil)
}

func (v nocVerifier) PeerNodeIDFromNOC(noc []byte) (uint64, error) {
	id, ok := v.nodes[string(noc)]
	if !ok {
		return 0, errors.New("unknown noc")
	}
	return id, nil
}

func (v nocVerifier) PeerFabricIDFromNOC(noc []byte) (uint64, error) {
	id, ok := v.fabrics[string(noc)]
	if !ok {
		return 0, errors.New("unknown noc")
	}
	return id, nil
}

func (v nocVerifier) PeerCATsFromNOC(noc []byte) ([]uint32, error) {
	return v.cats[string(noc)], nil
}

// singleFabricResolver resolves a Sigma1 destination the way a real
// multi-fabric host does: by recomputing the destination id.
type singleFabricResolver struct {
	id       *Identity
	verifier PeerVerifier
	root     []byte
}

func (r singleFabricResolver) ResolveSigma1Destination(dest [32]byte, random [RandomSize]byte) (*Identity, PeerVerifier, bool) {
	if ComputeDestinationID(r.id.IPK, random, r.root, r.id.FabricID, r.id.NodeID) == dest {
		return r.id, r.verifier, true
	}
	return nil, nil, false
}

func (r singleFabricResolver) ResolveFabricIndex(uint8) (*Identity, PeerVerifier, bool) {
	return r.id, r.verifier, true
}

type mapResumptionStore map[string]*ResumptionRecord

func (m mapResumptionStore) GetByID(id []byte) (*ResumptionRecord, error) {
	rec, ok := m[string(id)]
	if !ok {
		return nil, errors.New("unknown resumption id")
	}
	return rec, nil
}

type peerFixture struct {
	device, controller *Identity
	verifier           nocVerifier
	root               []byte
}

func newPeerFixture(t *testing.T) peerFixture {
	t.Helper()
	ipk := fabricIPK()
	device := newTestIdentity(t, 0xD0D0, 0xFAB, ipk)
	controller := newTestIdentity(t, 0xC0C0, 0xFAB, ipk)
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root, err := rootKey.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return peerFixture{
		device:     device,
		controller: controller,
		root:       root,
		verifier: nocVerifier{
			nodes:   map[string]uint64{string(device.NOC): device.NodeID, string(controller.NOC): controller.NodeID},
			fabrics: map[string]uint64{string(device.NOC): device.FabricID, string(controller.NOC): controller.FabricID},
			cats:    map[string][]uint32{string(controller.NOC): {0x00010001}},
		},
	}
}

// responder builds the controller side, resolving destinations against
// the controller's identity exactly as a production responder does.
func (f peerFixture) responder(sessionID uint16) *Responder {
	r := NewResponder(f.controller, f.verifier, sessionID)
	r.SetIdentityResolver(singleFabricResolver{id: f.controller, verifier: f.verifier, root: f.root})
	return r
}

func (f peerFixture) initiator(t *testing.T, sessionID uint16, rec *ResumptionRecord) *Initiator {
	t.Helper()
	i, err := NewPeerInitiator(InitiatorConfig{
		Identity:      f.device,
		Verifier:      f.verifier,
		SessionID:     sessionID,
		PeerNodeID:    f.controller.NodeID,
		RootPublicKey: f.root,
		Resumption:    rec,
		SessionParams: &SessionParameters{SessionIdleInterval: 500, SessionActiveInterval: 300},
	})
	if err != nil {
		t.Fatal(err)
	}
	return i
}

// TestPeerInitiator_FullHandshakeAgainstResponder drives the device-side
// initiator against the in-module responder end to end: destination-id
// resolution, Sigma2 verification from wire bytes, Sigma3, and matching
// keys. Mirrors matter.js CaseClient.ts:pair ↔ CaseServer.ts.
func TestPeerInitiator_FullHandshakeAgainstResponder(t *testing.T) {
	t.Parallel()
	f := newPeerFixture(t)
	initiator := f.initiator(t, 0x1111, nil)
	responder := f.responder(0x2222)

	s1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := UnmarshalSigma1(s1)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.InitiatorSessionParams == nil || parsed.InitiatorSessionParams.SessionIdleInterval != 500 {
		t.Fatalf("Sigma1 must carry the initiator session parameters, got %+v", parsed.InitiatorSessionParams)
	}
	if len(parsed.ResumptionID) != 0 {
		t.Fatal("no record held — Sigma1 must not offer resumption")
	}
	sigma2, err := responder.ProcessSigma1(s1)
	if err != nil {
		t.Fatalf("responder rejected Sigma1 (destination id?): %v", err)
	}
	s3, err := initiator.ProcessSigma2Bytes(sigma2.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if err := responder.ProcessSigma3(s3); err != nil {
		t.Fatal(err)
	}
	res, ok := initiator.Result()
	if !ok {
		t.Fatal("no result after a finished handshake")
	}
	respKeys, _ := responder.SessionKeys()
	if !constantTimeKeysEqual(res.Keys, respKeys) {
		t.Fatal("initiator and responder keys differ")
	}
	if res.PeerSessionID != 0x2222 || res.PeerNodeID != f.controller.NodeID || res.Resumed {
		t.Fatalf("result = %+v", res)
	}
	if len(res.PeerCATs) != 1 || res.PeerCATs[0] != 0x00010001 {
		t.Fatalf("peer CATs = %v, want the controller NOC's CATs", res.PeerCATs)
	}
	if !bytes.Equal(res.SharedSecret, responder.ECDHSharedSecret()) || !bytes.Equal(res.ResumptionID, responder.ResumptionID()) {
		t.Fatal("resumption material differs between initiator and responder")
	}
	if responder.PeerNodeID() != f.device.NodeID {
		t.Fatalf("responder saw peer %#x, want the device", responder.PeerNodeID())
	}
}

// TestPeerInitiator_ResumesWithRecord runs a full handshake, then a second
// one offering the resulting record: the responder answers Sigma2_Resume
// and both sides derive the same resumption keys.
func TestPeerInitiator_ResumesWithRecord(t *testing.T) {
	t.Parallel()
	f := newPeerFixture(t)
	first := f.initiator(t, 0x1111, nil)
	resp1 := f.responder(0x2222)
	s1, _ := first.GenerateSigma1()
	sigma2, err := resp1.ProcessSigma1(s1)
	if err != nil {
		t.Fatal(err)
	}
	s3, err := first.ProcessSigma2Bytes(sigma2.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if err := resp1.ProcessSigma3(s3); err != nil {
		t.Fatal(err)
	}
	firstRes, _ := first.Result()

	store := mapResumptionStore{string(resp1.ResumptionID()): {
		SharedSecret: resp1.ECDHSharedSecret(),
		ResumptionID: resp1.ResumptionID(),
		FabricIndex:  1,
		PeerNodeID:   f.device.NodeID,
	}}
	second := f.initiator(t, 0x1112, &ResumptionRecord{
		SharedSecret: firstRes.SharedSecret,
		ResumptionID: firstRes.ResumptionID,
		PeerNodeID:   f.controller.NodeID,
		PeerCATs:     []uint32{0x00020001},
	})
	resp2 := f.responder(0x2223)
	resp2.SetResumptionStore(store)

	s1b, err := second.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	result, err := resp2.ProcessSigma1WithResume(s1b)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsResume() {
		t.Fatal("responder did not take the resumption fast path")
	}
	if err := second.ProcessSigma2Resume(MarshalSigma2Resume(*result.Sigma2Resume)); err != nil {
		t.Fatal(err)
	}
	res, _ := second.Result()
	if !res.Resumed || !constantTimeKeysEqual(res.Keys, result.ResumeKeys) {
		t.Fatal("resumed keys differ from the responder's")
	}
	if res.PeerSessionID != 0x2223 || !bytes.Equal(res.ResumptionID, result.Sigma2Resume.ResumptionID) {
		t.Fatalf("result = %+v", res)
	}
	if len(res.PeerCATs) != 1 || res.PeerCATs[0] != 0x00020001 {
		t.Fatalf("resumed CATs = %v, want the record's", res.PeerCATs)
	}
}

// TestPeerInitiator_StaleRecordFallsBackToFullSigma: a record the
// responder no longer knows still yields a full handshake, as matter.js
// CaseClient handles a Sigma2 answer to a resumption offer.
func TestPeerInitiator_StaleRecordFallsBackToFullSigma(t *testing.T) {
	t.Parallel()
	f := newPeerFixture(t)
	stale := &ResumptionRecord{SharedSecret: bytes.Repeat([]byte{1}, 32), ResumptionID: bytes.Repeat([]byte{2}, 16)}
	initiator := f.initiator(t, 0x1111, stale)
	responder := f.responder(0x2222)
	responder.SetResumptionStore(mapResumptionStore{})
	s1, _ := initiator.GenerateSigma1()
	result, err := responder.ProcessSigma1WithResume(s1)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsResume() {
		t.Fatal("unknown record must not resume")
	}
	s3, err := initiator.ProcessSigma2Bytes(result.Sigma2.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if err := responder.ProcessSigma3(s3); err != nil {
		t.Fatal(err)
	}
	if res, _ := initiator.Result(); res.Resumed {
		t.Fatal("fallback must not report resumption")
	}
}

// TestPeerInitiator_RejectsWrongPeerNode: a responder whose NOC names
// another node is refused before Sigma3 — matter.js CaseClient.ts throws
// on `peerNodeIdNOCert !== peerNodeId`.
func TestPeerInitiator_RejectsWrongPeerNode(t *testing.T) {
	t.Parallel()
	f := newPeerFixture(t)
	f.verifier.nodes[string(f.controller.NOC)] = 0xBAD
	initiator := f.initiator(t, 0x1111, nil)
	responder := NewResponder(f.controller, f.verifier, 0x2222)
	s1, _ := initiator.GenerateSigma1()
	sigma2, err := responder.ProcessSigma1(s1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := initiator.ProcessSigma2Bytes(sigma2.Marshal()); !errors.Is(err, ErrPeerIdentityMismatch) {
		t.Fatalf("err = %v, want ErrPeerIdentityMismatch", err)
	}
	if _, ok := initiator.Result(); ok {
		t.Fatal("a rejected handshake must yield no result")
	}
}

// TestPeerInitiator_RejectsWrongFabric mirrors CaseClient.ts's fabric-id
// check on the responder NOC.
func TestPeerInitiator_RejectsWrongFabric(t *testing.T) {
	t.Parallel()
	f := newPeerFixture(t)
	f.verifier.fabrics[string(f.controller.NOC)] = 0xEEE
	initiator := f.initiator(t, 0x1111, nil)
	responder := NewResponder(f.controller, f.verifier, 0x2222)
	s1, _ := initiator.GenerateSigma1()
	sigma2, _ := responder.ProcessSigma1(s1)
	if _, err := initiator.ProcessSigma2Bytes(sigma2.Marshal()); !errors.Is(err, ErrFabricIDMismatch) {
		t.Fatalf("err = %v, want ErrFabricIDMismatch", err)
	}
}

// TestPeerInitiator_UnexpectedSigma2Resume: no resumption was offered, so
// a Sigma2_Resume is a protocol error (CaseClient.ts "Received an
// unexpected sigma2Resume.").
func TestPeerInitiator_UnexpectedSigma2Resume(t *testing.T) {
	t.Parallel()
	f := newPeerFixture(t)
	initiator := f.initiator(t, 0x1111, nil)
	if _, err := initiator.GenerateSigma1(); err != nil {
		t.Fatal(err)
	}
	raw := MarshalSigma2Resume(Sigma2Resume{ResumptionID: make([]byte, 16), Sigma2ResumeMIC: make([]byte, 16), ResponderSessionID: 9})
	if err := initiator.ProcessSigma2Resume(raw); !errors.Is(err, ErrUnexpectedSigma2Resume) {
		t.Fatalf("err = %v, want ErrUnexpectedSigma2Resume", err)
	}
}

// TestPeerInitiator_TamperedResumeMICRejected: a Sigma2_Resume whose MIC
// does not verify under the record's secret does not establish a session.
func TestPeerInitiator_TamperedResumeMICRejected(t *testing.T) {
	t.Parallel()
	f := newPeerFixture(t)
	rec := &ResumptionRecord{SharedSecret: bytes.Repeat([]byte{3}, 32), ResumptionID: bytes.Repeat([]byte{4}, 16)}
	initiator := f.initiator(t, 0x1111, rec)
	if _, err := initiator.GenerateSigma1(); err != nil {
		t.Fatal(err)
	}
	raw := MarshalSigma2Resume(Sigma2Resume{ResumptionID: bytes.Repeat([]byte{5}, 16), Sigma2ResumeMIC: make([]byte, 16), ResponderSessionID: 9})
	if err := initiator.ProcessSigma2Resume(raw); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

func TestNewPeerInitiator_RequiresIdentityVerifierAndRoot(t *testing.T) {
	t.Parallel()
	f := newPeerFixture(t)
	for name, cfg := range map[string]InitiatorConfig{
		"identity": {Verifier: f.verifier, RootPublicKey: f.root},
		"verifier": {Identity: f.device, RootPublicKey: f.root},
		"root":     {Identity: f.device, Verifier: f.verifier},
	} {
		if _, err := NewPeerInitiator(cfg); err == nil {
			t.Errorf("missing %s: want an error", name)
		}
	}
}

// bareVerifier verifies a chain but cannot bind the NOC to a node or fabric.
type bareVerifier struct{ PeerVerifier }

// A peer initiator must be able to bind the responder's NOC to the node and
// fabric it dials (matter.js CaseClient.ts:#doPair checks both
// unconditionally); a verifier that cannot is refused, not silently trusted.
func TestNewPeerInitiator_RefusesVerifierWithoutIdentityBinding(t *testing.T) {
	t.Parallel()
	f := newPeerFixture(t)
	cfg := InitiatorConfig{Identity: f.device, Verifier: bareVerifier{f.verifier}, RootPublicKey: f.root}
	if _, err := NewPeerInitiator(cfg); err == nil {
		t.Fatal("verifier without node-id / fabric-id extractors: want an error")
	}
}

func TestUnmarshalSigma2_RejectsMissingFields(t *testing.T) {
	t.Parallel()
	enc := sigmaTLVEncoder()
	enc.startStruct()
	enc.putOctets(1, make([]byte, RandomSize))
	enc.endContainer()
	if _, err := UnmarshalSigma2(enc.bytes()); !errors.Is(err, ErrSessionState) {
		t.Fatalf("err = %v, want ErrSessionState", err)
	}
	if _, err := UnmarshalSigma2Resume(enc.bytes()); !errors.Is(err, ErrSessionState) {
		t.Fatalf("resume err = %v, want ErrSessionState", err)
	}
}
