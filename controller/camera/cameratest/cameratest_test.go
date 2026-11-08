// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cameratest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/controller/camera/cameratest"
)

func TestNewNamesTheCamera(t *testing.T) {
	t.Parallel()
	cam, err := cameratest.New(cameratest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cam.Device() == nil || cam.Port() == nil {
		t.Fatal("no device or port")
	}
	port := cameratest.NewPort()
	named, err := cameratest.New(cameratest.Options{Key: "front-door", Name: "Front door", Port: port})
	if err != nil {
		t.Fatal(err)
	}
	if named.Port() != port {
		t.Fatal("the camera does not serve the port it was given")
	}
	// Without a node behind it the camera's signals reach no requestor.
	ctx := t.Context()
	cand := []wpdef.ICECandidateStruct{{Candidate: "candidate:1", SdpMid: spec.ValueOf("0")}}
	for name, err := range map[string]error{
		"Offer":         named.Offer(ctx, 1, "v=0"),
		"Answer":        named.Answer(ctx, 1, "v=0"),
		"IceCandidates": named.IceCandidates(ctx, 1, cand),
		"End":           named.End(ctx, 1, 2),
	} {
		if err == nil {
			t.Errorf("%s for an unknown session succeeded", name)
		}
	}
}

func TestPortRecordsAndRefuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := cameratest.NewPort()
	if got := p.LastOffer(); got.SessionID != 0 || len(p.Offers()) != 0 {
		t.Fatalf("offers before one %+v", got)
	}
	s, err := p.ProvideOffer(ctx, wpdef.OfferArgs{SessionID: 4, PeerNodeID: 9, StreamUsage: wpdef.StreamUsageLiveView})
	if err != nil || s.Id != 4 || s.PeerNodeId != 9 {
		t.Fatalf("ProvideOffer %+v (%v)", s, err)
	}
	if _, deferred, err := p.SolicitOffer(ctx, wpdef.OfferArgs{SessionID: 5}); err != nil || deferred {
		t.Fatalf("SolicitOffer deferred %v (%v)", deferred, err)
	}
	if got := p.LastOffer(); got.SessionID != 5 || len(p.Offers()) != 2 {
		t.Fatalf("last offer %+v of %d", got, len(p.Offers()))
	}
	if err := p.ProvideAnswer(ctx, 5, "v=0 answer"); err != nil {
		t.Fatal(err)
	}
	if sdp, ok := p.Answered(5); !ok || sdp != "v=0 answer" {
		t.Fatalf("answered %q %v", sdp, ok)
	}
	if err := p.ProvideIceCandidates(ctx, 4, []wpdef.ICECandidateStruct{{Candidate: "a"}, {Candidate: "b"}}); err != nil {
		t.Fatal(err)
	}
	if got := p.Candidates(4); len(got) != 2 || got[1].Candidate != "b" {
		t.Fatalf("candidates %+v", got)
	}
	if _, ended := p.Ended(4); ended {
		t.Fatal("ended before EndSession")
	}
	if err := p.EndSession(ctx, 4, 3); err != nil {
		t.Fatal(err)
	}
	if r, ended := p.Ended(4); !ended || r != 3 {
		t.Fatalf("ended %d %v", r, ended)
	}
	if id, err := p.VideoStreamAllocate(ctx, csdef.VideoStreamStruct{VideoStreamId: cameratest.AllocatedVideoStream}); err != nil || id != cameratest.AllocatedVideoStream {
		t.Fatalf("reuse %d (%v)", id, err)
	}
	v, _ := p.VideoStreamAllocate(ctx, csdef.VideoStreamStruct{})
	a, _ := p.AudioStreamAllocate(ctx, csdef.AudioStreamStruct{})
	sn, _ := p.SnapshotStreamAllocate(ctx, csdef.SnapshotStreamStruct{})
	if v != 11 || a != 12 || sn != 13 {
		t.Fatalf("allocations %d %d %d, want 11 12 13", v, a, sn)
	}
	p.VideoStreamAllocated(ctx, csdef.VideoStreamStruct{}, 0)
	res := csdef.VideoResolutionStruct{Width: 640, Height: 480}
	if shot, err := p.CaptureSnapshot(ctx, nil, res); err != nil || shot.ImageCodec != csdef.ImageCodecJpeg || shot.Resolution != res {
		t.Fatalf("snapshot %+v (%v)", shot, err)
	}

	refusal := errors.New("refused")
	calls := map[cameratest.Op]func() error{
		cameratest.OpSolicitOffer:         func() error { _, _, err := p.SolicitOffer(ctx, wpdef.OfferArgs{}); return err },
		cameratest.OpProvideOffer:         func() error { _, err := p.ProvideOffer(ctx, wpdef.OfferArgs{}); return err },
		cameratest.OpProvideAnswer:        func() error { return p.ProvideAnswer(ctx, 1, "") },
		cameratest.OpProvideICECandidates: func() error { return p.ProvideIceCandidates(ctx, 1, nil) },
		cameratest.OpEndSession:           func() error { return p.EndSession(ctx, 1, 0) },
		cameratest.OpVideoStreamAllocate: func() error {
			_, err := p.VideoStreamAllocate(ctx, csdef.VideoStreamStruct{})
			return err
		},
		cameratest.OpAudioStreamAllocate: func() error {
			_, err := p.AudioStreamAllocate(ctx, csdef.AudioStreamStruct{})
			return err
		},
		cameratest.OpSnapshotStreamAllocate: func() error {
			_, err := p.SnapshotStreamAllocate(ctx, csdef.SnapshotStreamStruct{})
			return err
		},
		cameratest.OpCaptureSnapshot:          func() error { _, err := p.CaptureSnapshot(ctx, nil, res); return err },
		cameratest.OpVideoStreamModify:        func() error { return p.VideoStreamModify(ctx, 1, nil, nil) },
		cameratest.OpVideoStreamDeallocate:    func() error { return p.VideoStreamDeallocate(ctx, 1) },
		cameratest.OpAudioStreamDeallocate:    func() error { return p.AudioStreamDeallocate(ctx, 1) },
		cameratest.OpSnapshotStreamModify:     func() error { return p.SnapshotStreamModify(ctx, 1, nil, nil) },
		cameratest.OpSnapshotStreamDeallocate: func() error { return p.SnapshotStreamDeallocate(ctx, 1) },
	}
	for op, call := range calls {
		if err := call(); err != nil {
			t.Errorf("%s before a refusal: %v", op, err)
		}
		p.Refuse(op, refusal)
		if err := call(); !errors.Is(err, refusal) {
			t.Errorf("%s refused: %v", op, err)
		}
		p.Refuse(op, nil)
		if err := call(); err != nil {
			t.Errorf("%s accepted again: %v", op, err)
		}
	}
}
