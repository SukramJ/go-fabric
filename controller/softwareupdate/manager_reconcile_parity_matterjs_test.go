// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
)

// The queue, consent and offer rules of matter.js's SoftwareUpdateManager
// at b25ed960 (decision 292, wave P5-A) — OtaTest
// (packages/node/test/behaviors/ota/OtaTest.ts at b25ed960), ported by
// meaning on the manager rig, whose node seam is OtaTest's device
// (initSiteWithoutTestImages: the test product at version 0).

// storeImage stores an image of the test product at version in mode, as
// OtaTest's storeImage (OtaTest.ts:3430-3444 at b25ed960).
func (r *managerRig) storeImage(t *testing.T, version uint32, mode Mode) {
	t.Helper()
	image, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: version, SoftwareVersionString: fmt.Sprintf("v%d.0.0", version),
		Payload: bytes.Repeat([]byte{byte(version)}, 1024),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.cfg.Images.Store(context.Background(), image, mode, nil); err != nil {
		t.Fatal(err)
	}
}

// servingRig is a rig whose store serves an image without a ledger
// binding, as OtaTest's stored prod images are served.
func servingRig(t *testing.T, edit func(*Config)) *managerRig {
	t.Helper()
	return newManagerRig(t, func(c *Config) {
		c.Images = NewImages(ImagesConfig{AllowUnverifiedLedgerImages: true})
		if edit != nil {
			edit(c)
		}
	})
}

// offerToRequestor is what the manager offers the node for a QueryImage
// from its version (OtaTest.ts:3540-3550 at b25ed960).
func (r *managerRig) offerToRequestor(canConsent bool) (AvailableDetails, bool) {
	return r.m.UpdateExistsFor(context.Background(), testPeer, provdef.QueryImageRequest{
		VendorId: 0xFFF1, ProductId: 0x8001, SoftwareVersion: r.basic.SoftwareVersion,
		ProtocolsSupported:  []provdef.DownloadProtocolEnum{provdef.DownloadProtocolBdxSynchronous},
		RequestorCanConsent: &canConsent,
	})
}

var lastUpdateToken atomic.Uint64

// deliverOffer makes the calls the OTA provider makes for a QueryImage
// whose offer of version reached the node, and returns the attempt
// (OtaTest.ts:91-104 at b25ed960).
func deliverOffer(t *testing.T, r *managerRig, version uint32) *mattercore.OTAUpdateAttempt {
	t.Helper()
	pp := providerPorts{r.m}
	a := pp.BeginQuery(testOTAPeer)
	token := binary.BigEndian.AppendUint64(nil, lastUpdateToken.Add(1))
	if !pp.OfferUpdate(a, token, version) {
		t.Fatalf("the offer of version %d was not made", version)
	}
	pp.ReportAttemptStatus(a, mattercore.OTAUpdateQuerying)
	pp.OfferAnswered(a)
	pp.EndQuery(a)
	return a
}

// reportStatus delivers an offer of version as deliverOffer does and
// reports status for it (OtaTest.ts:111-123 at b25ed960).
func reportStatus(t *testing.T, r *managerRig, version uint32, status Status) *mattercore.OTAUpdateAttempt {
	t.Helper()
	a := deliverOffer(t, r, version)
	providerPorts{r.m}.ReportAttemptStatus(a, status)
	return a
}

func testTarget(version uint32) Target {
	return Target{VendorID: 0xFFF1, ProductID: 0x8001, TargetSoftwareVersion: version}
}

// TestManagerOfferParity ports the offer rows of updateExistsFor
// (SoftwareUpdateManager.ts:657-715 at b25ed960): the consented version
// exactly (:698-699), else the newest candidate when the consent may be
// delegated (:700, mayDelegateConsentTo :638-647), else nothing.
func TestManagerOfferParity(t *testing.T) {
	t.Parallel()
	// OtaTest.ts:4607
	t.Run("offers a requestor that can consent the newest image when its consented version is not stored", func(t *testing.T) {
		t.Parallel()
		r := servingRig(t, func(c *Config) { c.DelegateConsentToRequestor = true })
		r.storeImage(t, 1, ModeProd)
		if _, err := r.m.AddUpdateConsent(context.Background(), testPeer, testTarget(2)); err != nil {
			t.Fatal(err)
		}
		d, ok := r.offerToRequestor(true)
		if !ok || d.NewSoftwareVersion != 1 || !d.ConsentRequired {
			t.Fatalf("offer %+v %v, want version 1 with consent required", d, ok)
		}
	})
	// OtaTest.ts:4793
	t.Run("does not delegate consent to a node with LocalConfigDisabled", func(t *testing.T) {
		t.Parallel()
		r := servingRig(t, func(c *Config) { c.DelegateConsentToRequestor = true })
		r.setNode(false, func(s *basicinfo.State) { s.LocalConfigDisabled = true })
		r.storeImage(t, 1, ModeProd)
		if d, ok := r.offerToRequestor(true); ok {
			t.Fatalf("offer %+v, want none", d)
		}
	})
	// OtaTest.ts:5661
	t.Run("does not delegate consent to a requestor that cannot obtain it, also while delegation is on", func(t *testing.T) {
		t.Parallel()
		r := servingRig(t, func(c *Config) { c.DelegateConsentToRequestor = true })
		r.storeImage(t, 1, ModeProd)
		if d, ok := r.offerToRequestor(false); ok {
			t.Fatalf("offer %+v to a requestor that cannot consent, want none", d)
		}
		if d, ok := r.offerToRequestor(true); !ok || d.NewSoftwareVersion != 1 {
			t.Fatalf("offer %+v %v, want version 1", d, ok)
		}
	})
	// DclOtaUpdateService.ts:59, :394-407 at b25ed960: on equal versions
	// local, then test, then prod.
	t.Run("offers a stored local image before a prod image of the same version", func(t *testing.T) {
		t.Parallel()
		r := servingRig(t, func(c *Config) { c.AllowTestImages = true })
		r.storeImage(t, 1, ModeProd)
		r.storeImage(t, 1, ModeLocal)
		got := r.m.storedCandidates(context.Background(), 0xFFF1, 0x8001, 0)
		if len(got) != 2 || got[0].Mode != ModeLocal || got[1].Mode != ModeProd {
			t.Fatalf("candidates %+v, want local then prod", got)
		}
	})
}

// TestManagerConsentParity ports the consent rows of addUpdateConsent and
// forceUpdate (SoftwareUpdateManager.ts:1455-1625 at b25ed960).
func TestManagerConsentParity(t *testing.T) {
	t.Parallel()
	t.Run("refuses a consent for a peer address no node has", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		r.setNode(true, nil)
		if _, err := r.m.AddUpdateConsent(context.Background(), testPeer, testTarget(2)); err == nil {
			t.Fatal("AddUpdateConsent for a gone node: nil error")
		}
		if r.m.HasConsent(testPeer, 0) {
			t.Fatal("a consent for a gone node was stored")
		}
	})
	t.Run("stores no consent for a version at or below the one the node runs", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		r.setNode(false, func(s *basicinfo.State) { s.SoftwareVersion = 2 })
		added, err := r.m.AddUpdateConsent(context.Background(), testPeer, testTarget(2))
		if err != nil || added || r.m.HasConsent(testPeer, 0) {
			t.Fatalf("added %v, err %v, consent %v; want false, nil, none", added, err, r.m.HasConsent(testPeer, 0))
		}
	})
	t.Run("refuses to force a version at or below the one the node runs", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		r.setNode(false, func(s *basicinfo.State) { s.SoftwareVersion = 2 })
		if err := r.m.ForceUpdate(context.Background(), testPeer, testTarget(2)); err == nil {
			t.Fatal("ForceUpdate below the running version: nil error")
		}
		if r.m.HasConsent(testPeer, 0) {
			t.Fatal("ForceUpdate stored a consent")
		}
	})
	t.Run("names the transfer options of the consent, not of the update underway", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		e := r.queue(2, statusPtr(mattercore.OTAUpdateDownloading))
		small, large := uint16(256), uint16(1024)
		r.m.mu.Lock()
		e.MaxBDXBlockSize = &small
		r.m.mu.Unlock()
		target := testTarget(3)
		target.MaxBDXBlockSize = &large
		if _, err := r.m.AddUpdateConsent(context.Background(), testPeer, target); err != nil {
			t.Fatal(err)
		}
		if o := r.m.TransferOptionsFor(testPeer); o.MaxBlockSize == nil || *o.MaxBlockSize != large {
			t.Fatalf("options %+v, want the consent's block size %d", o, large)
		}
	})
}

// TestManagerFailedUpdateParity ports the failed update: its consent and
// entry stay for 24 hours, without a second updateFailed, and leave
// without an event (SoftwareUpdateManager.ts:120-124, :1142-1214, :2303-2308
// at b25ed960).
func TestManagerFailedUpdateParity(t *testing.T) {
	t.Parallel()
	t.Run("keeps the consent of a failed update for 24 hours and then drops it without an event", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		r.queue(2, statusPtr(mattercore.OTAUpdateApplying))
		r.m.onSoftwareVersionChanged(testPeer, 1, true)
		if failed, _ := r.counts(); failed != 1 || !r.m.HasConsent(testPeer, 2) {
			t.Fatalf("failed %d, consent %v; want 1 and the consent kept", failed, r.m.HasConsent(testPeer, 2))
		}
		if q := r.m.QueuedUpdates(); len(q) != 0 {
			t.Fatalf("queue %+v, want the failed update unlisted", q)
		}
		r.clock.Advance(24*time.Hour + time.Second)
		if r.m.HasConsent(testPeer, 0) {
			t.Fatal("the failed consent outlived 24 hours")
		}
		if failed, _ := r.counts(); failed != 1 {
			t.Fatalf("%d updateFailed, want the expiry silent", failed)
		}
	})
}

// TestManagerQueueEndsParity ports how entries leave the queue: the node
// gone (#dequeueIfGone, #forgetPeer, SoftwareUpdateManager.ts:312-341,
// :1342-1353 at b25ed960) and an update done while a consent for a higher
// version waits (#completeUpdate :1120-1140).
func TestManagerQueueEndsParity(t *testing.T) {
	t.Parallel()
	t.Run("drops the queued update of a node that is gone without an event", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		r.queue(2, nil)
		r.setNode(true, nil)
		r.m.triggerQueuedUpdate()
		if q := r.m.QueuedUpdates(); len(q) != 0 {
			t.Fatalf("queue %+v, want the gone node's update dropped", q)
		}
		if failed, _ := r.counts(); failed != 0 {
			t.Fatalf("%d updateFailed, want none", failed)
		}
	})
	t.Run("ends the queued update of a deleted node without an event", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		r.queue(2, statusPtr(mattercore.OTAUpdateDownloading))
		r.setNode(true, nil)
		r.m.NodeDeleted(testPeer)
		if q := r.m.QueuedUpdates(); len(q) != 0 || r.m.HasConsent(testPeer, 0) {
			t.Fatalf("queue %+v, consent %v; want both gone", q, r.m.HasConsent(testPeer, 0))
		}
		if failed, _ := r.counts(); failed != 0 {
			t.Fatalf("%d updateFailed, want none", failed)
		}
	})
	t.Run("queues the version of a consent left above the version an update completed", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		r.queue(2, statusPtr(mattercore.OTAUpdateDownloading))
		if _, err := r.m.AddUpdateConsent(context.Background(), testPeer, testTarget(3)); err != nil {
			t.Fatal(err)
		}
		r.m.onSoftwareVersionChanged(testPeer, 2, false)
		q := r.m.QueuedUpdates()
		if _, done := r.counts(); done != 1 || len(q) != 1 || q[0].TargetSoftwareVersion != 3 {
			t.Fatalf("done %d, queue %+v; want the update done and version 3 queued", done, q)
		}
	})
}

// TestManagerScriptedDiscontinueParity is F-b25ed960-C3: a scripted
// Discontinue removes the consent of the version the token applies
// (appliedVersionOf) only when there is one, and the provider answers by
// its apply table (InProcessControllerAdapter.ts:759-772 at b25ed960).
func TestManagerScriptedDiscontinueParity(t *testing.T) {
	t.Parallel()
	t.Run("proceeds with a scripted Discontinue under an unknown token for a version above 0", func(t *testing.T) {
		t.Parallel()
		s := newScriptRig(t)
		s.applies = []ApplyUpdateScript{{Action: provdef.ApplyUpdateActionDiscontinue}}
		req := provdef.ApplyUpdateRequestRequest{UpdateToken: []byte{1, 2, 3, 4, 5, 6, 7, 8}, NewVersion: 2}
		resp, err := s.commands(t).ApplyUpdate(context.Background(), testOTAPeer, req, s.ownApply(req))
		if err != nil {
			t.Fatal(err)
		}
		if resp.Action != provdef.ApplyUpdateActionProceed {
			t.Fatalf("action %v, want Proceed (row 2 of the apply table)", resp.Action)
		}
	})
	t.Run("discontinues with a scripted Discontinue for version 0 under an unknown token", func(t *testing.T) {
		t.Parallel()
		s := newScriptRig(t)
		if _, err := s.r.m.AddUpdateConsent(context.Background(), testPeer, testTarget(2)); err != nil {
			t.Fatal(err)
		}
		s.applies = []ApplyUpdateScript{{Action: provdef.ApplyUpdateActionDiscontinue}}
		req := provdef.ApplyUpdateRequestRequest{UpdateToken: []byte{1, 2, 3, 4, 5, 6, 7, 8}, NewVersion: 0}
		resp, err := s.commands(t).ApplyUpdate(context.Background(), testOTAPeer, req, s.ownApply(req))
		if err != nil {
			t.Fatal(err)
		}
		if resp.Action != provdef.ApplyUpdateActionDiscontinue || !s.r.m.HasConsent(testPeer, 2) {
			t.Fatalf("action %v, consent kept %v; want Discontinue and the consent kept", resp.Action, s.r.m.HasConsent(testPeer, 2))
		}
	})
}

// TestManagerCloseAbortsTheAnnouncement ports #stopAnnouncements
// (SoftwareUpdateManager.ts:1446-1453 at b25ed960, called by
// #nodeGoingOffline :296-310): an announcement the node never answers is
// aborted when the manager goes offline, so Close does not wait for it.
func TestManagerCloseAbortsTheAnnouncement(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	r.m.announce = func(ctx context.Context, _ Peer, _ uint16) error {
		entered <- struct{}{}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}
	r.m.mu.Lock()
	r.m.announcements = newAnnouncements(r.m)
	r.m.mu.Unlock()
	if _, err := r.m.AddUpdateConsent(context.Background(), testPeer, testTarget(2)); err != nil {
		t.Fatal(err)
	}
	<-entered
	closed := make(chan struct{})
	go func() { r.m.Close(); close(closed) }()
	watchdog, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	select {
	case <-closed:
	case <-watchdog.Done():
		close(release)
		<-closed
		t.Fatal("Close waited for an announcement the node never answers")
	}
}
