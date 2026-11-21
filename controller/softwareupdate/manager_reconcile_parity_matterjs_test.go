// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/client/peer"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/controller/client"
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
		if _, err := r.m.AddUpdateConsent(context.Background(), testPeer, testTarget(2)); !errors.Is(err, ErrNoNode) {
			t.Fatalf("AddUpdateConsent for a gone node: %v, want ErrNoNode", err)
		}
		if err := r.m.ForceUpdate(context.Background(), testPeer, testTarget(2)); !errors.Is(err, ErrNoNode) {
			t.Fatalf("ForceUpdate for a gone node: %v, want ErrNoNode", err)
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
		if err := r.m.ForceUpdate(context.Background(), testPeer, testTarget(2)); !errors.Is(err, ErrNotApplicable) {
			t.Fatalf("ForceUpdate below the running version: %v, want ErrNotApplicable", err)
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
// entry stay for 24 hours, without a second report, and leave without an
// event — FAILED_UPDATE_RETENTION (SoftwareUpdateManager.ts:120-124 at
// b25ed960), the failure and its expiry (SoftwareUpdateManager.ts:1142-1214
// at b25ed960), the contract of the event (SoftwareUpdateManager.ts:2303-2309
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
		r.clock.Advance(24*time.Hour - time.Second)
		if !r.m.HasConsent(testPeer, 2) {
			t.Fatal("the failed consent expired before 24 hours")
		}
		r.clock.Advance(2 * time.Second)
		if r.m.HasConsent(testPeer, 0) {
			t.Fatal("the failed consent outlived 24 hours")
		}
		if failed, _ := r.counts(); failed != 1 {
			t.Fatalf("%d updateFailed, want the expiry silent", failed)
		}
	})
}

// TestManagerQueueEndsParity ports how entries leave the queue: the node
// gone (#dequeueIfGone, #forgetPeer, SoftwareUpdateManager.ts:312-342,
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

// announceLog records the announcements of the queue, as OtaTest's
// interceptAnnouncements (OtaTest.ts:3478-3501 at b25ed960): reply answers
// call n (counted from 1) in place of the node — at once with err, or, with
// hang, never: the call waits for its abort.
type announceLog struct {
	mu       sync.Mutex
	ctxs     []context.Context
	returned []bool
	hangs    []bool
	aborted  []bool // at return
}

// interceptAnnouncements starts the rig's announcements with reply as the
// node's answer; call it before the first consent.
func (r *managerRig) interceptAnnouncements(reply func(n int) (hang bool, err error)) *announceLog {
	l := &announceLog{}
	r.m.announce = func(ctx context.Context, _ Peer, _ uint16) error {
		l.mu.Lock()
		n := len(l.ctxs) + 1
		hang, err := reply(n)
		l.ctxs = append(l.ctxs, ctx)
		l.returned = append(l.returned, false)
		l.hangs = append(l.hangs, hang)
		l.aborted = append(l.aborted, false)
		l.mu.Unlock()
		if hang {
			<-ctx.Done()
			err = ctx.Err()
		}
		l.mu.Lock()
		l.returned[n-1], l.aborted[n-1] = true, ctx.Err() != nil
		l.mu.Unlock()
		return err
	}
	r.m.mu.Lock()
	r.m.announcements = newAnnouncements(r.m)
	r.m.mu.Unlock()
	return l
}

// answered succeeds every announcement; hanging never answers one.
func answered(int) (bool, error) { return false, nil }

func hanging(int) (bool, error) { return true, nil }

// count is the number of announcements sent.
func (l *announceLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.ctxs)
}

// abortedAll is, per announcement, whether its abort fired: at its return,
// or by now for one still running.
func (l *announceLog) abortedAll() []bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]bool, len(l.ctxs))
	for i, ctx := range l.ctxs {
		out[i] = l.aborted[i] || (!l.returned[i] && ctx.Err() != nil)
	}
	return out
}

// settle waits until every announcement that returned moved its entry on,
// so only the announcements still running are left (MockTime.macrotasks).
func (r *managerRig) settle(t *testing.T, l *announceLog) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		l.mu.Lock()
		running, inFlight := 0, false
		for i, done := range l.returned {
			switch {
			case done:
			case l.hangs[i]:
				running++
			default:
				inFlight = true
			}
		}
		l.mu.Unlock()
		r.m.mu.Lock()
		left := len(r.m.running)
		r.m.mu.Unlock()
		if left == running && !inFlight {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d announcements left running, want %d", left, running)
		}
		runtime.Gosched()
	}
}

// consentTo gives testPeer's node the consent for version (OtaTest.ts:4229-4243).
func (r *managerRig) consentTo(t *testing.T, version uint32) bool {
	t.Helper()
	added, err := r.m.AddUpdateConsent(context.Background(), testPeer, testTarget(version))
	if err != nil {
		t.Fatal(err)
	}
	return added
}

// phase is an entry's phase state, failed for a queued entry whose consent
// failed, with its target and the token of the attempt it follows
// (phaseStateOf, linkedUpdates; OtaTest.ts:159-168, :5301-5308).
type phase struct {
	target uint32
	state  string
	token  string
}

func (r *managerRig) phases() []phase {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	out := make([]phase, 0, len(r.m.queue))
	for _, e := range r.m.queue {
		p := phase{target: e.TargetSoftwareVersion, state: e.phase.state.String()}
		if r.m.isFailedLocked(e) {
			p.state = "failed"
		}
		if e.phase.attempt != nil {
			p.token = hex.EncodeToString(e.phase.attempt.Token())
		}
		out = append(out, p)
	}
	return out
}

// consentsAndTargets are the versions testPeer has a consent for and the
// targets of the queued updates (OtaTest.ts:4245-4258).
func (r *managerRig) consentsAndTargets() (consents, targets []uint32) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	consents, targets = []uint32{}, make([]uint32, 0, len(r.m.queue))
	if c := r.m.consents[testPeer]; c != nil {
		consents = append(consents, c.TargetSoftwareVersion)
	}
	for _, e := range r.m.queue {
		targets = append(targets, e.TargetSoftwareVersion)
	}
	return consents, targets
}

func tokenHex(a *mattercore.OTAUpdateAttempt) string { return hex.EncodeToString(a.Token()) }

func (r *managerRig) applyUpdate(token []byte, version uint32) bool {
	return providerPorts{r.m}.ApplyUpdate(context.Background(), testOTAPeer, token, version, 0)
}

func wantPhases(t *testing.T, r *managerRig, want ...phase) {
	t.Helper()
	if got := r.phases(); !slices.Equal(got, want) {
		t.Fatalf("phases %+v, want %+v", got, want)
	}
}

func wantConsentsAndTargets(t *testing.T, r *managerRig, consents, targets []uint32) {
	t.Helper()
	c, tg := r.consentsAndTargets()
	if !slices.Equal(c, consents) || !slices.Equal(tg, targets) {
		t.Fatalf("consents %v targets %v, want %v and %v", c, tg, consents, targets)
	}
}

var unknownToken = []byte{1, 2, 3, 4, 5, 6, 7, 8}

// TestManagerApplyOfTheTokenParity ports the apply rows of OtaTest's
// applyRewritten cases (OtaTest.ts:1766-1949 at b25ed960) on the manager:
// the apply table (applyUpdate, SoftwareUpdateManager.ts:1897-1956) and the
// attempt an apply under an unknown token becomes.
func TestManagerApplyOfTheTokenParity(t *testing.T) {
	t.Parallel()
	// OtaTest.ts:1873
	t.Run("applies the version of its update token once its consent is gone without removeConsent(), as the offer allowed it", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		r.consentTo(t, 1)
		a := deliverOffer(t, r, 1)
		r.m.mu.Lock()
		delete(r.m.consents, testPeer) // as when it was completed meanwhile
		r.m.mu.Unlock()
		if !r.applyUpdate(a.Token(), 1) {
			t.Fatal("apply refused, want Proceed")
		}
	})
	// OtaTest.ts:1865
	t.Run("refuses a version other than 0 that differs from its update token, even with consent for it", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		r.consentTo(t, 1)
		a := deliverOffer(t, r, 1)
		r.consentTo(t, 2)
		if r.applyUpdate(a.Token(), 2) {
			t.Fatal("apply allowed, want Discontinue")
		}
	})
	// OtaTest.ts:1885
	t.Run("applies the version of its update token when a consent for a higher version is added meanwhile", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		r.consentTo(t, 1)
		a := deliverOffer(t, r, 1)
		r.consentTo(t, 2)
		if !r.applyUpdate(a.Token(), 1) {
			t.Fatal("apply refused, want Proceed")
		}
	})
	// OtaTest.ts:1897
	t.Run("tracks an allowed apply under an update token it did not hand out as an applying update", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		r.consentTo(t, 1)
		deliverOffer(t, r, 1)
		if !r.applyUpdate(unknownToken, 1) {
			t.Fatal("apply refused, want Proceed")
		}
		a := providerPorts{r.m}.AttemptFor(testOTAPeer, unknownToken)
		if a == nil || a.Status() != mattercore.OTAUpdateApplying {
			t.Fatalf("attempt %v, want Applying", a)
		}
		r.m.mu.Lock()
		linked := r.m.linkedEntryLocked(a) != nil
		r.m.mu.Unlock()
		if !linked {
			t.Fatal("the queued update does not follow the adopted attempt")
		}
	})
}

// TestManagerQueuePhasesParity ports OtaTest's cases of the queue's phases
// (OtaTest.ts at b25ed960; SoftwareUpdateManager.ts:87-145, :1295-1415).
func TestManagerQueuePhasesParity(t *testing.T) {
	t.Parallel()
	// OtaTest.ts:1610
	t.Run("reports Cancelled and resets the queued update when it refuses the apply of its linked attempt", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		r.interceptAnnouncements(hanging)
		r.consentTo(t, 2)
		a := reportStatus(t, r, 2, mattercore.OTAUpdateDownloading)
		if q := r.m.QueuedUpdates(); len(q) != 1 || q[0].LastProgressStatus == nil || *q[0].LastProgressStatus != mattercore.OTAUpdateDownloading {
			t.Fatalf("before: %+v, want Downloading", q)
		}
		if r.applyUpdate(a.Token(), 3) {
			t.Fatal("apply of another version allowed")
		}
		q := r.m.QueuedUpdates()
		if a.Status() != mattercore.OTAUpdateCancelled || len(q) != 1 || q[0].LastProgressStatus != nil {
			t.Fatalf("attempt %s, queue %+v; want Cancelled and the one entry announced again", a.Status(), q)
		}
	})
	// OtaTest.ts:4307
	t.Run("keeps an update underway when the consent for another version is removed", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		l := r.interceptAnnouncements(answered)
		r.consentTo(t, 1)
		r.consentTo(t, 2)
		r.settle(t, l)
		reportStatus(t, r, 2, mattercore.OTAUpdateDownloading)
		r.m.RemoveConsent(testPeer, 1)
		if got := r.phases(); len(got) != 1 || got[0].state != "progress" {
			t.Fatalf("phases %+v, want progress", got)
		}
		wantConsentsAndTargets(t, r, []uint32{2}, []uint32{2})
	})
	// OtaTest.ts:4326
	t.Run("cancels an update underway when the consent for its version is removed, and queues the version of the consent left", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		l := r.interceptAnnouncements(answered)
		r.consentTo(t, 1)
		r.settle(t, l)
		cancels := 0
		r.m.disableTransfer = func(Peer) error { cancels++; return nil }
		a := reportStatus(t, r, 1, mattercore.OTAUpdateDownloading)
		r.consentTo(t, 2) // the update underway keeps its version
		r.m.RemoveConsent(testPeer, 1)
		providerPorts{r.m}.ReportAttemptStatus(a, mattercore.OTAUpdateCancelled) // as the closed transfer reports
		r.settle(t, l)
		if failed, _ := r.counts(); failed != 1 || cancels != 1 {
			t.Fatalf("failures %d, cancels %d; want 1 and 1", failed, cancels)
		}
		wantConsentsAndTargets(t, r, []uint32{2}, []uint32{2})
	})
	// OtaTest.ts:4948
	t.Run("keeps the apply right of an offer when the consent for another version is removed, and ends the right of an offer of that version", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		r.interceptAnnouncements(hanging)
		r.consentTo(t, 2)
		offered := deliverOffer(t, r, 1) // as offered by an override that obtained the consent for 1
		pp := providerPorts{r.m}
		ofRemoved := pp.BeginQuery(testOTAPeer) // an offer of 2 whose query still runs
		if !pp.OfferUpdate(ofRemoved, []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x02}, 2) {
			t.Fatal("offer of 2 not made")
		}
		r.m.RemoveConsent(testPeer, 2)
		if !r.applyUpdate(offered.Token(), 1) || r.applyUpdate(ofRemoved.Token(), 2) {
			t.Fatal("want the offer of 1 applied and the offer of 2 refused")
		}
	})
	// OtaTest.ts:5276
	t.Run("does not link an attempt that ended before its offer was delivered", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		r.interceptAnnouncements(hanging)
		r.consentTo(t, 1)
		delivered := deliverOffer(t, r, 1)
		pp := providerPorts{r.m}
		ended := pp.BeginQuery(testOTAPeer)
		pp.OfferUpdate(ended, []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x01, 0x07}, 1)
		r.m.mu.Lock()
		r.m.attempts.End(ended) // as the provider ends an attempt while its query still runs
		r.m.mu.Unlock()
		pp.OfferAnswered(ended)
		r.m.mu.Lock()
		linked := r.m.linkedEntryLocked(delivered) != nil
		r.m.mu.Unlock()
		if !linked || delivered.Ended() || ended.Answered() {
			t.Fatalf("linked %v, delivered ended %v, ended answered %v; want true, false, false", linked, delivered.Ended(), ended.Answered())
		}
	})
	// OtaTest.ts:5316
	t.Run("resets an update whose attempt a newer answered offer of another version replaces, so the old status no longer counts", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		l := r.interceptAnnouncements(answered)
		r.consentTo(t, 2)
		r.settle(t, l)
		replaced := reportStatus(t, r, 2, mattercore.OTAUpdateWaitForApply)
		r.consentTo(t, 3)
		wantPhases(t, r, phase{2, "progress", tokenHex(replaced)})
		replacing := deliverOffer(t, r, 3)
		providerPorts{r.m}.ReportAttemptStatus(replacing, mattercore.OTAUpdateDownloading)
		if !replaced.Ended() {
			t.Fatal("the replaced attempt runs on")
		}
		wantPhases(t, r, phase{3, "progress", tokenHex(replacing)})
		r.m.onSoftwareVersionChanged(testPeer, 0, true) // restarts below its target while it downloads
		r.settle(t, l)
		r.m.mu.Lock()
		c := r.m.consents[testPeer]
		r.m.mu.Unlock()
		if c == nil || c.TargetSoftwareVersion != 3 || !c.failedAt.IsZero() {
			t.Fatalf("consent %+v, want 3 not failed", c)
		}
	})
	// OtaTest.ts:5366
	t.Run("ignores a late Cancelled of an attempt whose update was reset as stalled while the update is announced again", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		l := r.interceptAnnouncements(func(n int) (bool, error) { return n != 1, nil })
		r.consentTo(t, 1)
		r.settle(t, l)
		a := reportStatus(t, r, 1, mattercore.OTAUpdateDownloading)
		r.advance(25 * time.Minute)
		r.settle(t, l)
		if n := l.count(); n != 2 {
			t.Fatalf("%d announcements, want 2", n)
		}
		wantPhases(t, r, phase{1, "announcing", ""})
		providerPorts{r.m}.ReportAttemptStatus(a, mattercore.OTAUpdateCancelled)
		r.clock.Advance(time.Second)
		if a.Ended() {
			t.Fatal("the late Cancelled ended the attempt")
		}
		wantPhases(t, r, phase{1, "announcing", ""})
		if got := l.abortedAll(); !slices.Equal(got, []bool{false, false}) {
			t.Fatalf("aborted %v, want neither", got)
		}
	})
	// OtaTest.ts:5402
	t.Run("ends the attempt of a queued update when an apply under an unknown update token replaces it", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		l := r.interceptAnnouncements(answered)
		r.consentTo(t, 1)
		r.settle(t, l)
		replaced := reportStatus(t, r, 1, mattercore.OTAUpdateDownloading)
		if !r.applyUpdate(unknownToken, 1) || !replaced.Ended() {
			t.Fatalf("applied under the unknown token: replaced ended %v", replaced.Ended())
		}
		wantPhases(t, r, phase{1, "progress", hex.EncodeToString(unknownToken)})
	})
	// Go-titled (OtaTest.ts:5122 is not portable: Go's Manager is terminal
	// after Close): the offline half — the attempts end, the entry keeps
	// following its ended attempt, no event (#endAllAttempts,
	// SoftwareUpdateManager.ts:1229-1237 at b25ed960).
	t.Run("keeps an update in progress with its ended attempt when the manager goes offline", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		l := r.interceptAnnouncements(answered)
		r.consentTo(t, 1)
		r.settle(t, l)
		a := reportStatus(t, r, 1, mattercore.OTAUpdateDownloading)
		r.m.Close()
		if !a.Ended() {
			t.Fatal("the attempt runs on after Close")
		}
		wantPhases(t, r, phase{1, "progress", tokenHex(a)})
		if failed, done := r.counts(); failed != 0 || done != 0 {
			t.Fatalf("failed %d, done %d; want no event", failed, done)
		}
	})
	// Go-titled: a version below the target resets an update underway and
	// the queue runs, so it is announced again (#onSoftwareVersionChanged,
	// SoftwareUpdateManager.ts:1035-1089 at b25ed960).
	t.Run("announces an update again when the node restarts below its target before it applies", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		l := r.interceptAnnouncements(answered)
		r.consentTo(t, 2)
		r.settle(t, l)
		wantPhases(t, r, phase{2, "announced", ""})
		r.m.onSoftwareVersionChanged(testPeer, 1, true)
		r.settle(t, l)
		if n := l.count(); n != 2 {
			t.Fatalf("%d announcements, want the reset update announced again", n)
		}
		if failed, _ := r.counts(); failed != 0 {
			t.Fatalf("%d updateFailed, want none", failed)
		}
	})
	// Go-titled: a failed update is not announced again (#failUpdate,
	// #triggerQueuedUpdate; SoftwareUpdateManager.ts:1142-1174, :1332-1334 at
	// b25ed960), and an announcement that fails waits as queued and is
	// announced again by the periodic queue run (:1404-1414, :1296-1300).
	t.Run("does not announce a failed update again", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		l := r.interceptAnnouncements(answered)
		r.consentTo(t, 2)
		r.settle(t, l)
		r.m.dropUndeliverableUpdate(testPeer, 0)
		wantPhases(t, r, phase{2, "failed", ""})
		r.advance(20 * time.Minute)
		r.settle(t, l)
		if n := l.count(); n != 1 {
			t.Fatalf("%d announcements, want the failed update not announced again", n)
		}
		if failed, _ := r.counts(); failed != 1 {
			t.Fatalf("%d updateFailed, want 1", failed)
		}
	})
	t.Run("announces again after a failed announcement with the periodic queue run", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		l := r.interceptAnnouncements(func(n int) (bool, error) {
			if n == 1 {
				return false, errors.New("simulated unreachable node")
			}
			return false, nil
		})
		r.consentTo(t, 2)
		r.settle(t, l)
		if got := r.phases(); len(got) != 1 || got[0].state != "queued" {
			t.Fatalf("phases %+v, want queued after the failed announcement", got)
		}
		r.advance(5 * time.Minute)
		r.settle(t, l)
		wantPhases(t, r, phase{2, "announced", ""})
	})
	// Go-titled: an announcement the node does not answer is aborted after
	// OTA_PROGRESS_TIMEOUT and the entry waits as queued
	// (SoftwareUpdateManager.ts:1389-1391, :1404-1414 at b25ed960).
	t.Run("aborts an announcement the node does not answer within the progress timeout", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		l := r.interceptAnnouncements(func(n int) (bool, error) {
			return n == 1, errors.New("simulated unreachable node") // the retry fails at once
		})
		r.consentTo(t, 2)
		r.clock.Advance(ProgressTimeout - time.Second)
		r.settle(t, l)
		if got := l.abortedAll(); len(got) != 1 || got[0] {
			t.Fatalf("aborted %v before the progress timeout, want one running announcement", got)
		}
		wantPhases(t, r, phase{2, "announcing", ""})
		r.clock.Advance(2 * time.Second)
		r.settle(t, l)
		if got := l.abortedAll(); len(got) < 1 || !got[0] {
			t.Fatalf("aborted %v, want the first announcement aborted", got)
		}
		// The 15-minute queue tick falls on the abort and may retry (the
		// retry fails at once): either way the entry ends queued, reset.
		deadline := time.Now().Add(5 * time.Second)
		for {
			q := r.m.QueuedUpdates()
			if len(q) == 1 && q[0].Status == PendingQueued && q[0].LastProgressStatus != nil &&
				*q[0].LastProgressStatus == mattercore.OTAUpdateUnknown {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("queue %+v, want the entry queued again, reset (Unknown)", q)
			}
			runtime.Gosched()
		}
	})
}

// TestManagerAdmitsANewQueryOfANodeWithARunningAttemptParity ports the
// table of "admits a new query of a node with a running attempt"
// (OtaTest.ts:5425-5545 at b25ed960; admitQuery,
// SoftwareUpdateManager.ts:1802-1881): per row, whether the query is
// admitted, the attempt ended, the queued update still linked to it, the
// startUp suppressed on the query's session, and how often the transfer
// was cancelled.
func TestManagerAdmitsANewQueryOfANodeWithARunningAttemptParity(t *testing.T) {
	t.Parallel()
	const query = 8
	type outcome struct {
		admitted, ended, stillLinked, suppressedStartUp bool
		cancels                                         int
	}
	running := outcome{stillLinked: true}
	for _, tc := range []struct {
		what       string
		stale      bool
		transfer   func(now time.Time) *restartedTransfer
		cancelFail bool
		want       outcome
	}{
		{
			what: "ends a stale attempt and resets its update, also while its BDX transfer looks newer", stale: true,
			transfer: func(now time.Time) *restartedTransfer {
				return &restartedTransfer{id: query + 1, at: now.Add(time.Second)}
			},
			want: outcome{admitted: true, ended: true},
		},
		{what: "ends an attempt without a BDX transfer and resets its update", want: outcome{admitted: true, ended: true}},
		{
			what: "cancels the transfer of a node that restarted, ends its attempt, resets its update and suppresses the startUp",
			transfer: func(now time.Time) *restartedTransfer {
				return &restartedTransfer{id: query + 1, at: now.Add(-time.Second)}
			},
			want: outcome{admitted: true, ended: true, suppressedStartUp: true, cancels: 1},
		},
		{
			what: "answers busy and keeps the attempt when the transfer of a node that restarted cannot be cancelled",
			transfer: func(now time.Time) *restartedTransfer {
				return &restartedTransfer{id: query + 1, at: now.Add(-time.Second)}
			},
			cancelFail: true, want: outcome{stillLinked: true, cancels: 1},
		},
		{
			what: "answers busy while the transfer runs on the session of the query",
			transfer: func(now time.Time) *restartedTransfer {
				return &restartedTransfer{id: query, at: now.Add(-time.Second)}
			},
			want: running,
		},
		{
			what:     "answers busy while the transfer on another session is as recent as the session of the query",
			transfer: func(now time.Time) *restartedTransfer { return &restartedTransfer{id: query + 1, at: now} },
			want:     running,
		},
		{
			what: "answers busy while the transfer is newer than the session of the query",
			transfer: func(now time.Time) *restartedTransfer {
				return &restartedTransfer{id: query + 1, at: now.Add(time.Second)}
			},
			want: running,
		},
	} {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			r := newManagerRig(t, nil)
			r.interceptAnnouncements(hanging)
			r.consentTo(t, 1)
			r.clock.Advance(time.Second)
			a := reportStatus(t, r, 1, mattercore.OTAUpdateDownloading)
			r.m.mu.Lock()
			linked := r.m.linkedEntryLocked(a) != nil
			r.m.mu.Unlock()
			if !linked {
				t.Fatal("the update does not follow the attempt")
			}
			if tc.stale {
				r.clock.Advance(16 * time.Minute)
			}
			var transfer *restartedTransfer
			if tc.transfer != nil {
				transfer = tc.transfer(r.clock.Now())
			}
			r.m.admitTransfer = func(Peer) admissionTransfer {
				if transfer == nil {
					return nil
				}
				return *transfer
			}
			var got outcome
			r.m.disableTransfer = func(Peer) error {
				got.cancels++
				if tc.cancelFail {
					return errors.New("simulated BDX cancel failure")
				}
				return nil
			}
			got.admitted = providerPorts{r.m}.AdmitQuery(context.Background(), testOTAPeer, query, 0)
			got.ended = a.Ended()
			r.m.mu.Lock()
			got.stillLinked = r.m.linkedEntryLocked(a) != nil
			id, ok := r.m.startUpSuppress[testPeer]
			r.m.mu.Unlock()
			got.suppressedStartUp = ok && id == query
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestManagerBDXCancelOfRemovedConsentsParity ports "cancels / keeps the
// BDX transfer of a node when all its consents are removed while its
// update is announcing / failed / applying" (OtaTest.ts:6047-6080 at
// b25ed960; removeConsent, SoftwareUpdateManager.ts:1670-1723 at
// b25ed960).
func TestManagerBDXCancelOfRemovedConsentsParity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		state   string
		cancels bool
	}{{"announcing", true}, {"failed", true}, {"applying", false}} {
		verb := "keeps"
		if tc.cancels {
			verb = "cancels"
		}
		t.Run(verb+" the BDX transfer of a node when all its consents are removed while its update is "+tc.state, func(t *testing.T) {
			t.Parallel()
			r := newManagerRig(t, nil)
			r.interceptAnnouncements(hanging)
			r.consentTo(t, 1)
			switch tc.state {
			case "failed":
				r.m.dropUndeliverableUpdate(testPeer, 0)
			case "applying":
				reportStatus(t, r, 1, mattercore.OTAUpdateApplying)
			}
			var disabled []Peer
			r.m.disableTransfer = func(p Peer) error { disabled = append(disabled, p); return nil }
			r.m.RemoveConsent(testPeer, 0)
			if got := len(disabled) == 1 && disabled[0] == testPeer; got != tc.cancels || (!tc.cancels && len(disabled) != 0) {
				t.Fatalf("disabled %v, want cancelled %v", disabled, tc.cancels)
			}
		})
	}
}

// TestManagerRemovedConsentDisarmsParity ports "stops the fast
// resubscription of a node whose update underway is cancelled by removing
// its consent" (OtaTest.ts:6187 at b25ed960; removeConsent :1707): the
// node's reboot arming is lifted, so its return deadline gives nothing up.
func TestManagerRemovedConsentDisarmsParity(t *testing.T) {
	t.Parallel()
	t.Run("stops the fast resubscription of a node whose update underway is cancelled by removing its consent", func(t *testing.T) {
		t.Parallel()
		calls := &armerCalls{}
		var armer *peer.RebootResubscribeArmer
		r := newManagerRig(t, func(c *Config) {
			armer = peer.NewRebootResubscribeArmer(calls.context(c))
			c.Armer = armer
		})
		t.Cleanup(armer.Close)
		l := r.interceptAnnouncements(answered)
		r.consentTo(t, 1)
		r.settle(t, l)
		reportStatus(t, r, 1, mattercore.OTAUpdateDownloading)
		armer.Arm(testPeer, 0)
		r.m.RemoveConsent(testPeer, 1)
		r.clock.Advance(peer.RebootReturnTimeout(0) + time.Second)
		if lost, _ := calls.counts(); lost != 0 {
			t.Fatalf("the return deadline gave the node up %d times: it was not disarmed", lost)
		}
	})
}

// TestManagerDelegatedOfferParity ports the delegated offers through the
// provider (OtaTest.ts:5216-5274 at b25ed960): the consent for the
// offered version removed before the apply refuses it, although none was
// stored; removed while the query runs, the query is answered NotAvailable.
func TestManagerDelegatedOfferParity(t *testing.T) {
	t.Parallel()
	delegatedQuery := func(t *testing.T, r *managerRig, p *mattercore.OTAProvider) provdef.QueryImageResponse {
		t.Helper()
		yes := true
		resp, err := p.MatterInvoke(attemptCtx(7), provdef.CmdQueryImage, provdef.QueryImageRequest{
			VendorId: 0xFFF1, ProductId: 0x8001, SoftwareVersion: 0, RequestorCanConsent: &yes,
			ProtocolsSupported: []provdef.DownloadProtocolEnum{provdef.DownloadProtocolBdxSynchronous},
		})
		if err != nil {
			t.Fatal(err)
		}
		return resp.(provdef.QueryImageResponse)
	}
	rig := func(t *testing.T) *managerRig {
		t.Helper()
		r := servingRig(t, func(c *Config) { c.DelegateConsentToRequestor = true })
		r.storeImage(t, 1, ModeProd)
		return r
	}
	// OtaTest.ts:5257
	t.Run("refuses the apply of a delegated offer once the consent for its version is removed, although none was stored", func(t *testing.T) {
		t.Parallel()
		r := rig(t)
		responder := bdx.NewResponder(nil)
		responder.SetClock(r.clock)
		p := providerOf(t, r, providerPorts{r.m}, responder)
		q := delegatedQuery(t, r, p)
		if q.Status != provdef.StatusUpdateAvailable || q.UpdateToken == nil {
			t.Fatalf("query %+v, want the delegated offer", q)
		}
		r.m.RemoveConsent(testPeer, 1)
		resp, err := p.MatterInvoke(attemptCtx(7), provdef.CmdApplyUpdateRequest, provdef.ApplyUpdateRequestRequest{UpdateToken: *q.UpdateToken, NewVersion: 1})
		if err != nil {
			t.Fatal(err)
		}
		if got := resp.(provdef.ApplyUpdateResponse).Action; got != provdef.ApplyUpdateActionDiscontinue {
			t.Fatalf("action %v, want Discontinue", got)
		}
	})
	// OtaTest.ts:5266 (×2)
	for _, tc := range []struct {
		title   string
		version uint32
	}{{"for the node", 0}, {"for the version the node would be offered", 1}} {
		t.Run("answers NotAvailable to a delegated query when its consent is removed "+tc.title+" while it runs", func(t *testing.T) {
			t.Parallel()
			r := rig(t)
			responder := bdx.NewResponder(nil)
			responder.SetClock(r.clock)
			p := providerOf(t, r, removingSource{providerPorts{r.m}, r.m, tc.version}, responder)
			if q := delegatedQuery(t, r, p); q.Status != provdef.StatusNotAvailable || q.ImageUri != nil {
				t.Fatalf("query %+v, want NotAvailable", q)
			}
			r.m.mu.Lock()
			attempts := len(r.m.attempts.Of(testOTAPeer))
			r.m.mu.Unlock()
			if attempts != 0 {
				t.Fatalf("%d attempts run, want none", attempts)
			}
		})
	}
}

// removingSource is the manager's image lookup that removes the node's
// consent (for version, all for 0) once it found the image, as OtaTest's
// RemovingOtaProviderServer (OtaTest.ts:3378-3392 at b25ed960).
type removingSource struct {
	providerPorts
	m       *Manager
	version uint32
}

func (s removingSource) QueryImage(ctx context.Context, p mattercore.OTAPeer, req provdef.QueryImageRequest) (mattercore.OTAImage, bool, error) {
	img, ok, err := s.providerPorts.QueryImage(ctx, p, req)
	s.m.RemoveConsent(peerOf(p), s.version)
	return img, ok, err
}

// consentAskingSource offers the manager's image with consent required, as
// OtaTest's ConsentRequiringOtaProviderServer override (OtaTest.ts:3338-3346
// at b25ed960).
type consentAskingSource struct{ providerPorts }

func (s consentAskingSource) QueryImage(ctx context.Context, p mattercore.OTAPeer, req provdef.QueryImageRequest) (mattercore.OTAImage, bool, error) {
	img, ok, err := s.providerPorts.QueryImage(ctx, p, req)
	img.ConsentRequired = true
	return img, ok, err
}

// TestManagerConsentRequiredOverrideParity ports "asks for consent instead
// of delegating it when an override requires consent while delegation is
// off" (OtaTest.ts:4696 at b25ed960): a requestor that can consent is not
// delegated the consent while the delegation is off, so the provider asks
// its consent hook.
func TestManagerConsentRequiredOverrideParity(t *testing.T) {
	t.Parallel()
	t.Run("asks for consent instead of delegating it when an override requires consent while delegation is off", func(t *testing.T) {
		t.Parallel()
		asked := 0
		r := servingRig(t, func(c *Config) {
			c.RequestConsent = func(context.Context, Peer, QueryImageRequest, AvailableDetails) (ConsentState, time.Duration) {
				asked++
				return mattercore.OTAConsentGranted, 0
			}
		})
		r.storeImage(t, 1, ModeProd)
		r.consentTo(t, 1)
		responder := bdx.NewResponder(nil)
		responder.SetClock(r.clock)
		p := providerOf(t, r, consentAskingSource{providerPorts{r.m}}, responder)
		yes := true
		if _, err := p.MatterInvoke(attemptCtx(7), provdef.CmdQueryImage, provdef.QueryImageRequest{
			VendorId: 0xFFF1, ProductId: 0x8001, SoftwareVersion: 0, RequestorCanConsent: &yes,
			ProtocolsSupported: []provdef.DownloadProtocolEnum{provdef.DownloadProtocolBdxSynchronous},
		}); err != nil {
			t.Fatal(err)
		}
		if asked != 1 {
			t.Fatalf("consent hook asked %d times, want 1", asked)
		}
	})
}

// staleOnce has the next presence read answer from a snapshot taken before
// during runs — another change made while the manager's lock is released
// for that read, as the registry read leaves it (review-a M1).
func (r *managerRig) staleOnce(during func()) {
	var used atomic.Bool
	r.m.presenceOf = func() func(Peer) bool {
		r.mu.Lock()
		gone := r.gone
		r.mu.Unlock()
		if used.CompareAndSwap(false, true) {
			during() // a nested read during it answers from its own, live snapshot
		}
		return func(p Peer) bool { return p == testPeer && !gone }
	}
}

// TestManagerPresenceAsOfTheRun (review-a M1; C-23): the queue run and the
// check's cleanup judge a peer gone only for what was there when they read
// the registry — matter.js reads #peerOf inside the change
// (SoftwareUpdateManager.ts:1346-1353 at b25ed960) — so an update queued
// while the manager's lock was released for that read stays.
func TestManagerPresenceAsOfTheRun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		run  func(r *managerRig)
	}{
		{"keeps an update queued while the queue run read the registry", func(r *managerRig) { r.m.triggerQueuedUpdate() }},
		{"keeps a consent and update given while the check read the registry", func(r *managerRig) {
			r.m.query = func(ctx context.Context, only *client.Node, includeStored bool) ([]PeerUpdate, bool, error) {
				r.m.forgetGone()
				return nil, true, nil
			}
			_ = r.m.CheckForUpdates(context.Background())
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newManagerRig(t, nil)
			r.setNode(true, nil)
			r.staleOnce(func() {
				r.setNode(false, nil)
				r.consentTo(t, 2)
			})
			tc.run(r)
			if q := r.m.QueuedUpdates(); len(q) != 1 || !r.m.HasConsent(testPeer, 2) {
				t.Fatalf("queue %+v, consent %v; want the new update and its consent kept", q, r.m.HasConsent(testPeer, 2))
			}
		})
	}
}

// TestManagerChangeEmitsItsOwnEvents (review-a L1; C-23): the events of a
// change run when that change ends, on its caller — #mutate
// (SoftwareUpdateManager.ts:539-561 at b25ed960) — not in another change
// that took the lock while it read the registry.
func TestManagerChangeEmitsItsOwnEvents(t *testing.T) {
	t.Parallel()
	var inOther atomic.Bool
	var failedInOther, failedCalls atomic.Int32
	r := newManagerRig(t, func(c *Config) {
		c.OnUpdateFailed = func(Peer) {
			failedCalls.Add(1)
			if inOther.Load() {
				failedInOther.Add(1)
			}
		}
	})
	r.consentTo(t, 2)
	r.staleOnce(func() {
		inOther.Store(true)
		done := make(chan struct{})
		go func() { defer close(done); r.m.triggerQueuedUpdate() }()
		<-done
		inOther.Store(false)
	})
	r.m.RemoveConsent(testPeer, 2)
	if failedCalls.Load() != 1 || failedInOther.Load() != 0 {
		t.Fatalf("updateFailed %d times, %d of them in another change; want once, in RemoveConsent", failedCalls.Load(), failedInOther.Load())
	}
}

// TestManagerProductCheckQueuesParity ports #checkProductForUpdates
// (SoftwareUpdateManager.ts:902-935 at b25ed960; C-22): a node removed
// during the check is skipped; a consent for the product queues its
// version, and updateAvailable is asked when there is none or it is below
// the version found.
func TestManagerProductCheckQueuesParity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		consent   uint32 // 0: none
		gone      bool
		queued    []uint32
		available int
	}{
		{"queues a lower consent and asks for the version found", 1, false, []uint32{1}, 1},
		{"queues a higher consent without asking", 3, false, []uint32{3}, 0},
		{"asks for a node without a consent", 0, false, []uint32{}, 1},
		{"skips a node removed during the check", 1, true, []uint32{1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var available atomic.Int32
			r := servingRig(t, func(c *Config) {
				c.OnUpdateAvailable = func(Peer, SoftwareUpdateInfo) { available.Add(1) }
			})
			r.storeImage(t, 2, ModeProd)
			if tc.consent != 0 {
				r.consentTo(t, tc.consent)
			}
			r.setNode(tc.gone, nil)
			if _, err := r.m.checkProductForUpdates(context.Background(), 0xFFF1, 0x8001, 0,
				[]prepared{{peer: testPeer, vendorID: 0xFFF1, productID: 0x8001}}, true); err != nil {
				t.Fatal(err)
			}
			_, targets := r.consentsAndTargets()
			if !slices.Equal(targets, tc.queued) || int(available.Load()) != tc.available {
				t.Fatalf("queued %v, updateAvailable %d; want %v and %d", targets, available.Load(), tc.queued, tc.available)
			}
			r.m.mu.Lock()
			_, known := r.m.known[testPeer]
			r.m.mu.Unlock()
			if known == tc.gone {
				t.Fatalf("known update %v for a node gone %v", known, tc.gone)
			}
		})
	}
}

// TestManagerForceRunsTheQueueFirstParity (F2; C-23): forceUpdate's
// #storeConsent runs the queue (#queueUpdate, SoftwareUpdateManager.ts:1291
// at b25ed960) before the forced #startAnnouncement (:1521), so a waiting
// update of another node is announced too.
func TestManagerForceRunsTheQueueFirstParity(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	other := Peer{FabricIndex: 1, NodeID: 0x22}
	r.mu.Lock()
	r.extra = map[Peer]bool{other: true}
	r.mu.Unlock()
	if _, err := r.m.AddUpdateConsent(context.Background(), other, testTarget(2)); err != nil {
		t.Fatal(err) // no announcements yet: it waits as queued
	}
	l := r.interceptAnnouncements(hanging)
	if err := r.m.ForceUpdate(context.Background(), testPeer, testTarget(2)); err != nil {
		t.Fatal(err)
	}
	r.settle(t, l)
	if n := l.count(); n != 2 {
		t.Fatalf("%d announcements, want the waiting node's and the forced one", n)
	}
}

// TestManagerErrNoNodeIsNotApplicable (F3; C-24): matter.js throws one
// OtaUpdateError for an address no node has and for a node not applicable
// (SoftwareUpdateManager.ts:1486-1489, :1574-1578 at b25ed960).
func TestManagerErrNoNodeIsNotApplicable(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	r.setNode(true, nil)
	err := r.m.ForceUpdate(context.Background(), testPeer, testTarget(2))
	if !errors.Is(err, ErrNoNode) || !errors.Is(err, ErrNotApplicable) || !errors.Is(ErrNoNode, ErrNotApplicable) {
		t.Fatalf("ForceUpdate for a gone node: %v, want ErrNoNode that is ErrNotApplicable", err)
	}
}

// TestManagerGoneNodeConsentReported (F4): the check's cleanup reports the
// consent of a gone node removed, as NodeDeleted does (Go-only
// UpdateConsentChanged; #forgetPeer deletes it, SoftwareUpdateManager.ts:312-342
// at b25ed960).
func TestManagerGoneNodeConsentReported(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	r.consentTo(t, 2)
	l := observe(t, r.m)
	r.setNode(true, nil)
	r.m.forgetGone()
	evs := l.await(t, 1)
	if evs[0].Kind != UpdateConsentChanged || evs[0].Consented || evs[0].Peer != testPeer {
		t.Fatalf("event %+v, want the consent reported removed", evs[0])
	}
}
