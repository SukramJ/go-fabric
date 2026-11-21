// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/im"
)

// The update attempts as the manager owns them, driven through the OTA
// provider the controller builds over it — matter.js's OtaTest
// (packages/node/test/behaviors/ota/OtaTest.ts at b25ed960), whose
// provider reports every QueryImage flow to its SoftwareUpdateManager as
// an attempt (SoftwareUpdateManager.ts:1739-2089 at b25ed960). The
// requests arrive as the requestor sends them; the queue is the rig's.

// attemptSource offers version 2 of the test product, without a consent
// to ask for (the manager holds one).
type attemptSource struct{ img []byte }

func (s attemptSource) QueryImage(context.Context, mattercore.OTAPeer, provdef.QueryImageRequest) (mattercore.OTAImage, bool, error) {
	return mattercore.OTAImage{FileDesignator: bdx.FileDesignator("fff1-8001-v2.ota"), SoftwareVersion: 2, SoftwareVersionString: "2.0"}, true, nil
}

func (s attemptSource) Open(context.Context, string) (bdx.Blob, error) {
	return bytes.NewReader(s.img), nil
}

// attemptCtx is an invoke of testPeer on its CASE session sessionID.
func attemptCtx(sessionID uint16) context.Context {
	ctx := im.WithFabricFilter(context.Background(), false, testPeer.FabricIndex)
	ctx = im.WithSubject(ctx, testPeer.NodeID, nil)
	return mattercore.WithInvokeSessionID(ctx, sessionID)
}

type attemptRig struct {
	*managerRig
	p   *mattercore.OTAProvider
	bdx *bdx.Responder
}

func newAttemptRig(t *testing.T, edit ...func(*Config)) *attemptRig {
	t.Helper()
	r := &attemptRig{managerRig: newManagerRig(t, func(c *Config) {
		for _, e := range edit {
			e(c)
		}
	}), bdx: bdx.NewResponder(nil)}
	r.bdx.SetClock(r.clock)
	r.p = providerOf(t, r.managerRig, attemptSource{img: bytes.Repeat([]byte{0x42}, 3000)}, r.bdx)
	return r
}

func (r *attemptRig) query(t *testing.T, sessionID uint16, version uint32) provdef.QueryImageResponse {
	t.Helper()
	resp, err := r.p.MatterInvoke(attemptCtx(sessionID), provdef.CmdQueryImage, provdef.QueryImageRequest{
		VendorId: 0xFFF1, ProductId: 0x8001, SoftwareVersion: version,
		ProtocolsSupported: []provdef.DownloadProtocolEnum{provdef.DownloadProtocolBdxSynchronous},
	})
	if err != nil {
		t.Fatalf("QueryImage: %v", err)
	}
	return resp.(provdef.QueryImageResponse)
}

func (r *attemptRig) apply(t *testing.T, token []byte, version uint32) provdef.ApplyUpdateActionEnum {
	t.Helper()
	resp, err := r.p.MatterInvoke(attemptCtx(7), provdef.CmdApplyUpdateRequest, provdef.ApplyUpdateRequestRequest{UpdateToken: token, NewVersion: version})
	if err != nil {
		t.Fatalf("ApplyUpdateRequest: %v", err)
	}
	return resp.(provdef.ApplyUpdateResponse).Action
}

func (r *attemptRig) notify(t *testing.T, token []byte, version uint32) {
	t.Helper()
	if _, err := r.p.MatterInvoke(attemptCtx(7), provdef.CmdNotifyUpdateApplied, provdef.NotifyUpdateAppliedRequest{UpdateToken: token, SoftwareVersion: version}); err != nil {
		t.Fatalf("NotifyUpdateApplied: %v", err)
	}
}

// startTransfer opens the requestor's receiver-driven download of the
// offer on sessionID and returns once the responder recorded its
// negotiation — it records it before it serves the first block, so the
// first Block is the event that proves it, with no wait on the clock;
// done closes when the responder ends the transfer.
func (r *attemptRig) startTransfer(t *testing.T, sessionID uint16, uri string) (done chan struct{}) {
	t.Helper()
	fd, _, err := bdx.ParseBdxURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	a, b := make(chan bdx.Message, 64), make(chan bdx.Message, 64)
	req, resp := &attemptPipe{in: a, out: b}, &attemptPipe{in: b, out: a}
	init, _ := bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024, FileDesignator: fd}.Encode()
	done = make(chan struct{})
	go func() {
		defer close(done)
		r.bdx.HandleExchange(context.Background(), bdx.Peer{FabricIndex: testPeer.FabricIndex, NodeID: testPeer.NodeID, SessionID: sessionID},
			bdx.Message{ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageReceiveInit), Payload: init}, resp)
	}()
	m := bdx.NewMessenger(req, 5*time.Second)
	if _, _, err := m.Next(context.Background(), bdx.MessageReceiveAccept); err != nil {
		t.Fatalf("ReceiveAccept: %v", err)
	}
	if err := m.SendBlockQuery(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Next(context.Background(), bdx.MessageBlock); err != nil {
		t.Fatalf("first Block: %v", err)
	}
	if s := r.bdx.SessionFor(bdx.Peer{FabricIndex: testPeer.FabricIndex, NodeID: testPeer.NodeID}, "ota"); s == nil {
		t.Fatal("no transfer runs after its first block")
	} else if _, ok := s.Negotiated(); !ok {
		t.Fatal("the transfer serves blocks without a recorded negotiation")
	}
	return done
}

type attemptPipe struct{ in, out chan bdx.Message }

func (p *attemptPipe) Send(_ context.Context, proto uint16, op uint8, payload []byte) error {
	p.out <- bdx.Message{ProtocolID: proto, Opcode: op, Payload: append([]byte(nil), payload...)}
	return nil
}

func (p *attemptPipe) Receive(ctx context.Context) (bdx.Message, error) {
	select {
	case m := <-p.in:
		return m, nil
	case <-ctx.Done():
		return bdx.Message{}, ctx.Err()
	}
}
func (p *attemptPipe) MaxPayloadSize() int { return 1178 }
func (p *attemptPipe) Close() error        { return nil }

func (r *attemptRig) entryStatus() (*Status, bool) {
	q := r.m.QueuedUpdates()
	if len(q) == 0 {
		return nil, false
	}
	return q[0].LastProgressStatus, true
}

// TestManagerConsentRemovalEndsTheApplyRightParity is F-b25ed960-57 (C-24):
// removeConsent takes the apply right of the offers of the peer
// (SoftwareUpdateManager.ts:1670-1692 at b25ed960), so the apply under the
// token is refused (:1951-1955 → :1936-1938) — unless the attempt already
// applies (:1951).
func TestManagerConsentRemovalEndsTheApplyRightParity(t *testing.T) {
	t.Parallel()
	t.Run("refuses the version of its update token once the consent is removed without a queued update", func(t *testing.T) {
		t.Parallel()
		r := newAttemptRig(t)
		q := r.query(t, 7, 1)
		r.m.RemoveConsent(testPeer, 0)
		if got := r.apply(t, *q.UpdateToken, 2); got != provdef.ApplyUpdateActionDiscontinue {
			t.Fatalf("apply after RemoveConsent: action %d, want Discontinue", got)
		}
	})
	t.Run("refuses version 0 for a known update token once its consent is gone", func(t *testing.T) {
		t.Parallel()
		r := newAttemptRig(t)
		q := r.query(t, 7, 1)
		r.m.RemoveConsent(testPeer, 2)
		if got := r.apply(t, *q.UpdateToken, 0); got != provdef.ApplyUpdateActionDiscontinue {
			t.Fatalf("apply 0 after RemoveConsent: action %d, want Discontinue", got)
		}
	})
	t.Run("lets an applying update apply again once its consent is removed, which hasConsent() no longer reports", func(t *testing.T) {
		t.Parallel()
		r := newAttemptRig(t)
		q := r.query(t, 7, 1)
		if got := r.apply(t, *q.UpdateToken, 2); got != provdef.ApplyUpdateActionProceed {
			t.Fatalf("first apply: action %d", got)
		}
		r.m.RemoveConsent(testPeer, 0)
		if got := r.apply(t, *q.UpdateToken, 2); got != provdef.ApplyUpdateActionProceed {
			t.Fatalf("apply of the applying update after RemoveConsent: action %d, want Proceed", got)
		}
	})
}

// TestManagerAdmissionFailsAnAppliedUpdateParity is admission row 2 (C-25):
// a query without a transfer from a requestor whose answered attempt,
// linked to its queued update, is Applying, reporting a version below the
// update's target, fails the update (SoftwareUpdateManager.ts:1836-1850
// at b25ed960); a stale Applying attempt — past its apply delay and the
// progress timeout — only ends and resets it (row 0, :1823-1829).
func TestManagerAdmissionFailsAnAppliedUpdateParity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		after  time.Duration
		failed int
		queued bool
	}{
		{"fails an update whose node queries again below its target after applying it", 20 * time.Minute, 1, false},
		{"ends a stale attempt and resets its update, without a failure", 80 * time.Minute, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newAttemptRig(t, func(c *Config) { c.ApplyDelay = time.Hour })
			r.queue(2, nil)
			q := r.query(t, 7, 1)
			if got := r.apply(t, *q.UpdateToken, 2); got != provdef.ApplyUpdateActionProceed {
				t.Fatalf("apply: action %d", got)
			}
			if s, ok := r.entryStatus(); !ok || s == nil || *s != mattercore.OTAUpdateApplying {
				t.Fatalf("queued update %v, want Applying", s)
			}
			r.clock.Advance(tc.after)
			if got := r.query(t, 8, 1); got.Status != provdef.StatusUpdateAvailable {
				t.Fatalf("second query %+v, want admitted", got)
			}
			if failed, _ := r.counts(); failed != tc.failed {
				t.Errorf("%d updateFailed, want %d", failed, tc.failed)
			}
			if _, queued := r.entryStatus(); queued != tc.queued {
				t.Errorf("queued %v, want %v", queued, tc.queued)
			}
		})
	}
}

// TestManagerRestartQuerySuppressesOnlyAQueuedUpdateParity (C-25): a query
// admitted for the requestor's restart — its transfer on an older session
// cancelled — suppresses the node's next start-up report on the query's
// session only while the peer has a queued update (SoftwareUpdateManager.ts:1873-1879
// at b25ed960).
func TestManagerRestartQuerySuppressesOnlyAQueuedUpdateParity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		queued     bool
		suppressed bool
	}{
		{"suppresses the startUp on the session of a query admitted after a restart during a transfer", true, true},
		{"suppresses no startUp of a node without a queued update", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newAttemptRig(t)
			if tc.queued {
				r.queue(2, nil)
			}
			q := r.query(t, 7, 1)
			done := r.startTransfer(t, 7, *q.ImageUri)
			r.clock.Advance(time.Second)
			if got := r.query(t, 8, 1); got.Status != provdef.StatusUpdateAvailable {
				t.Fatalf("restart query %+v, want admitted", got)
			}
			limit, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			select {
			case <-done:
			case <-limit.Done():
				t.Fatal("the transfer of the restarted node was not cancelled")
			}
			r.m.mu.Lock()
			id, ok := r.m.startUpSuppress[testPeer]
			r.m.mu.Unlock()
			if ok != tc.suppressed || (ok && id != 8) {
				t.Errorf("startUp suppressed %v on session %d, want %v on 8", ok, id, tc.suppressed)
			}
		})
	}
}

// TestManagerUpdateAppliedBranchesParity ports the three branches of
// #updateApplied (SoftwareUpdateManager.ts:1984-2003 at b25ed960; OtaTest.ts
// :1682, :4357, :4542): at or above the target the update is done; below
// it, the update linked to the notifying attempt is retried and any other
// is left as it is.
func TestManagerUpdateAppliedBranchesParity(t *testing.T) {
	t.Parallel()
	t.Run("Done status clears the consent for the completed version", func(t *testing.T) {
		t.Parallel()
		r := newAttemptRig(t)
		r.queue(2, nil)
		q := r.query(t, 7, 1)
		r.apply(t, *q.UpdateToken, 2)
		r.notify(t, *q.UpdateToken, 2)
		if _, done := r.counts(); done != 1 || r.m.HasConsent(testPeer, 2) {
			t.Errorf("done %d, consent kept %v", done, r.m.HasConsent(testPeer, 2))
		}
		if _, queued := r.entryStatus(); queued {
			t.Error("the done update stayed queued")
		}
	})
	t.Run("retries an update that the node completes below its target while it runs", func(t *testing.T) {
		t.Parallel()
		r := newAttemptRig(t)
		r.queue(2, nil)
		q := r.query(t, 7, 1)
		r.apply(t, *q.UpdateToken, 2)
		r.notify(t, *q.UpdateToken, 1)
		s, queued := r.entryStatus()
		if !queued || s == nil || *s != mattercore.OTAUpdateUnknown {
			t.Fatalf("queued %v with status %v, want the update reset for a retry", queued, s)
		}
		if failed, done := r.counts(); failed != 0 || done != 0 {
			t.Errorf("failed %d, done %d, want none", failed, done)
		}
	})
	t.Run("ignores a Done below the target that arrives after its update ended", func(t *testing.T) {
		t.Parallel()
		r := newAttemptRig(t)
		r.queue(2, nil)
		q := r.query(t, 7, 1)
		r.apply(t, *q.UpdateToken, 2)
		r.notify(t, []byte{9, 9, 9}, 1)
		if s, queued := r.entryStatus(); !queued || s == nil || *s != mattercore.OTAUpdateApplying {
			t.Fatalf("queued %v with status %v, want the update still Applying", queued, s)
		}
		if failed, done := r.counts(); failed != 0 || done != 0 {
			t.Errorf("failed %d, done %d, want none", failed, done)
		}
	})
}

// TestManagerAttemptsUnderTheManagerLock (decision 292 (b), W3): the
// attempts have no lock of their own beyond their fields', so every path
// that changes them holds the manager's lock. This runs the provider's
// queries, applies and notifications, RemoveConsent and AddUpdateConsent,
// the fabric removal, the node's removal and the queue's runs against each
// other; the transfer listeners are not in the mix (no transfer runs — the
// BDX listeners report outside the responder's lock, through the same
// ReportAttemptStatus the queries use). Run under -race, a path that does
// not hold the lock is a reported race; the outcome of the interleaving is
// not asserted.
func TestManagerAttemptsUnderTheManagerLock(t *testing.T) {
	t.Parallel()
	r := newAttemptRig(t)
	r.queue(2, nil)
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Go(func() {
			for i := range 25 {
				session := uint16(10 + g)
				q := r.query(t, session, 1)
				if q.UpdateToken == nil {
					continue
				}
				switch (g + i) % 6 {
				case 0:
					r.apply(t, *q.UpdateToken, 2)
				case 1:
					r.m.RemoveConsent(testPeer, 0)
				case 2:
					r.notify(t, *q.UpdateToken, 1)
				case 3:
					_ = providerPorts{r.m}.MatterDropFabric(context.Background(), testPeer.FabricIndex)
				case 4:
					r.m.NodeDeleted(testPeer)
				default:
					_, _ = r.m.AddUpdateConsent(context.Background(), testPeer, Target{VendorID: 0xFFF1, ProductID: 0x8001, TargetSoftwareVersion: 2})
				}
				_ = r.m.QueuedUpdates()
				r.m.triggerQueuedUpdate()
			}
		})
	}
	wg.Wait()
}

// TestManagerQueueRunWithAnUpdateInProgress (F-b25ed960-C1): the queue run
// reads the transfer of an entry in progress with m.mu held, so it reads
// the BDX responder without taking m.mu again. Before, it called
// bdxResponder, which locks m.mu whether or not SetBDX ran (5aaabb82
// manager.go:543-547), and the run deadlocked on the first entry in
// progress whenever the bdxSession test seam was unset — every production
// run.
func TestManagerQueueRunWithAnUpdateInProgress(t *testing.T) {
	t.Parallel()
	for name, responder := range map[string]*bdx.Responder{"without a responder": nil, "with a responder": bdx.NewResponder(nil)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newManagerRig(t, nil)
			if responder != nil {
				r.m.SetBDX(responder)
			}
			r.queue(2, statusPtr(mattercore.OTAUpdateDownloading))
			ran := make(chan struct{})
			go func() {
				defer close(ran)
				r.m.triggerQueuedUpdate()
			}()
			limit, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			select {
			case <-ran:
			case <-limit.Done():
				t.Fatal("the queue run with an entry in progress did not return")
			}
		})
	}
}

// restartedTransfer is a running transfer on session id, last active at.
type restartedTransfer struct {
	id uint16
	at time.Time
}

func (f restartedTransfer) Peer() bdx.Peer {
	return bdx.Peer{FabricIndex: testPeer.FabricIndex, NodeID: testPeer.NodeID, SessionID: f.id}
}
func (f restartedTransfer) LastActivity() time.Time { return f.at }

// TestManagerAdmissionCancelFailsParity ports "answers busy and keeps the
// attempt when the transfer of a node that restarted cannot be cancelled"
// (OtaTest.ts:5425-5545 at b25ed960, the admission table): the cancel of
// the restarted node's transfer fails, so the query is Busy and the
// attempt runs on, linked (SoftwareUpdateManager.ts:1863-1872).
func TestManagerAdmissionCancelFailsParity(t *testing.T) {
	t.Parallel()
	t.Run("answers busy and keeps the attempt when the transfer of a node that restarted cannot be cancelled", func(t *testing.T) {
		t.Parallel()
		r := newAttemptRig(t)
		r.queue(2, nil)
		q := r.query(t, 7, 1)
		cancels := 0
		started := r.clock.Now()
		r.m.admitTransfer = func(Peer) admissionTransfer { return restartedTransfer{id: 7, at: started} }
		r.m.disableTransfer = func(Peer) error { cancels++; return errors.New("simulated BDX cancel failure") }
		r.clock.Advance(time.Second)
		if got := r.query(t, 8, 1); got.Status != provdef.StatusBusy {
			t.Fatalf("query %+v, want Busy", got)
		}
		a := providerPorts{r.m}.AttemptFor(otaPeerOfTest(), *q.UpdateToken)
		if cancels != 1 || a == nil || a.Ended() {
			t.Fatalf("cancels %d, attempt kept %v", cancels, a != nil)
		}
		r.m.mu.Lock()
		linked := r.m.linkedEntryLocked(a) != nil
		_, suppressed := r.m.startUpSuppress[testPeer]
		r.m.mu.Unlock()
		if !linked || suppressed {
			t.Errorf("still linked %v, startUp suppressed %v", linked, suppressed)
		}
	})
}

func otaPeerOfTest() mattercore.OTAPeer {
	return mattercore.OTAPeer{FabricIndex: testPeer.FabricIndex, NodeID: testPeer.NodeID}
}

// TestManagerStartUpBelowTargetFailsTheAppliedAttemptParity ports "Apply
// failure detected when startUp fires after Applying state" (OtaTest.ts:2147
// at b25ed960) on the attempt: the startUp below the target fails the
// update before anything resets it, so #failUpdate reaches the attempt it
// follows — Cancelled, as it applied, and ended (SoftwareUpdateManager.ts:1063-1069,
// :1152-1157 at b25ed960).
func TestManagerStartUpBelowTargetFailsTheAppliedAttemptParity(t *testing.T) {
	t.Parallel()
	t.Run("Apply failure detected when startUp fires after Applying state", func(t *testing.T) {
		t.Parallel()
		r := newAttemptRig(t)
		r.queue(2, nil)
		q := r.query(t, 7, 1)
		if got := r.apply(t, *q.UpdateToken, 2); got != provdef.ApplyUpdateActionProceed {
			t.Fatalf("apply: action %d", got)
		}
		a := providerPorts{r.m}.AttemptFor(otaPeerOfTest(), *q.UpdateToken)
		if a == nil || a.Status() != mattercore.OTAUpdateApplying {
			t.Fatalf("attempt %v, want Applying", a)
		}
		r.m.onSoftwareVersionChanged(testPeer, 1, true)
		if failed, _ := r.counts(); failed != 1 {
			t.Errorf("%d updateFailed, want 1", failed)
		}
		if a.Status() != mattercore.OTAUpdateCancelled || !a.Ended() {
			t.Errorf("attempt %s ended %v, want Cancelled and ended", a.Status(), a.Ended())
		}
	})
	// C-31: only an attempt that applies fails at a startUp below the
	// target (SoftwareUpdateManager.ts:1063 at b25ed960); one that waits for
	// its apply takes the reset path — the entry unlinked and kept queued,
	// the attempt left running, no updateFailed (:1082-1087).
	t.Run("keeps an update whose attempt waits for its apply at a startUp below the target", func(t *testing.T) {
		t.Parallel()
		r := newAttemptRig(t)
		r.queue(2, nil)
		q := r.query(t, 7, 1)
		a := providerPorts{r.m}.AttemptFor(otaPeerOfTest(), *q.UpdateToken)
		if a == nil || !(providerPorts{r.m}).ReportAttemptStatus(a, mattercore.OTAUpdateWaitForApply) {
			t.Fatalf("attempt %v: WaitForApply not recorded", a)
		}
		r.m.onSoftwareVersionChanged(testPeer, 1, true)
		if failed, _ := r.counts(); failed != 0 {
			t.Errorf("%d updateFailed, want 0", failed)
		}
		if len(r.m.QueuedUpdates()) != 1 {
			t.Error("the update left the queue")
		}
		if a.Ended() || a.Status() != mattercore.OTAUpdateWaitForApply {
			t.Errorf("attempt %s ended %v, want WaitForApply and running", a.Status(), a.Ended())
		}
		r.m.mu.Lock()
		linked := r.m.linkedEntryLocked(a) != nil
		r.m.mu.Unlock()
		if linked {
			t.Error("the entry still follows the attempt")
		}
	})
}

// TestManagerConsentRemovalKeepsALegacyApplyingUpdate (C-23, review M2):
// an update the deprecated [Manager.OnStatusChange] moved to Applying, which
// follows no attempt, is as far as an attempt that applies — the removal
// of its consent no longer cancels it (SoftwareUpdateManager.ts:1700 at
// b25ed960 reads the status the entry shows).
func TestManagerConsentRemovalKeepsALegacyApplyingUpdate(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	r.queue(2, nil)
	v := uint32(2)
	r.m.OnStatusChange(testPeer, mattercore.OTAUpdateApplying, &v, 0) //nolint:staticcheck // the deprecated path under test
	r.m.RemoveConsent(testPeer, 2)
	if failed, _ := r.counts(); failed != 0 {
		t.Errorf("%d updateFailed, want the applying update kept", failed)
	}
	if len(r.m.QueuedUpdates()) != 1 {
		t.Error("the applying update left the queue")
	}
}

// TestManagerDelegatesConsentOnlyWhenOnParity ports "offers a requestor
// that can consent the newest image without a consent only while
// delegation is on" (OtaTest.ts:4666 at b25ed960): the delegation is off by
// default (SoftwareUpdateManager.ts:2171), so a node that can consent and
// holds no consent is offered nothing (:698-705); with it on, the newest
// candidate, consent-required (:700, :713). A consented version is offered
// first either way (:699, :701).
func TestManagerDelegatesConsentOnlyWhenOnParity(t *testing.T) {
	t.Parallel()
	yes := true
	req := provdef.QueryImageRequest{RequestorCanConsent: &yes}
	candidates := []StoredImage{{VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 3}, {VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 2}}
	t.Run("offers a requestor that can consent the newest image without a consent only while delegation is on", func(t *testing.T) {
		t.Parallel()
		for _, on := range []bool{false, true} {
			r := newManagerRig(t, func(c *Config) { c.DelegateConsentToRequestor = on })
			r.m.mu.Lock()
			chosen, required := r.m.offerOfLocked(testPeer, candidates, req)
			r.m.mu.Unlock()
			if delegated := (providerPorts{r.m}).MayDelegateConsentTo(otaPeerOfTest(), req); delegated != on {
				t.Errorf("delegation %v: MayDelegateConsentTo %v", on, delegated)
			}
			switch {
			case !on && chosen != nil:
				t.Errorf("delegation off: offered %d", chosen.SoftwareVersion)
			case on && (chosen == nil || chosen.SoftwareVersion != 3 || !required):
				t.Errorf("delegation on: offered %v required %v, want 3 consent-required", chosen, required)
			}
		}
	})
	t.Run("offers the consented version before a delegated one", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, func(c *Config) { c.DelegateConsentToRequestor = true })
		r.queue(2, nil)
		r.m.mu.Lock()
		chosen, required := r.m.offerOfLocked(testPeer, candidates, req)
		r.m.mu.Unlock()
		if chosen == nil || chosen.SoftwareVersion != 2 || required {
			t.Errorf("offered %v required %v, want the consented 2", chosen, required)
		}
	})
}
