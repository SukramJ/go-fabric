// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
)

// TestParityMatterJS_OTAProviderDefinition: the server serves the generated
// definition, held against the snapshot (spectest.CheckDefinition) — the
// element file ota-software-update-provider.element.ts: cluster 0x29
// revision 1 (:18-20), no attribute but the globals, QueryImage 0x00 →
// QueryImageResponse 0x01, ApplyUpdateRequest 0x02 → ApplyUpdateResponse
// 0x03, NotifyUpdateApplied 0x04 → status (:22-80), every request at
// Operate access ("O").
func TestParityMatterJS_OTAProviderDefinition(t *testing.T) {
	t.Parallel()
	spectest.CheckDefinition(t, provdef.Definition)
	f := newOTAFixture(t, nil)
	if f.p.MatterClusterID() != 0x0029 || provdef.Revision != 1 {
		t.Fatalf("cluster 0x%04X revision %d", f.p.MatterClusterID(), provdef.Revision)
	}
	if v, ok := f.p.MatterRead(cluster.AttrGlobalClusterRevision); !ok || v != uint16(1) {
		t.Fatalf("ClusterRevision %v %v", v, ok)
	}
	if got := f.p.MatterAcceptedCommands(); !slices.Equal(got, []uint32{0x00, 0x02, 0x04}) {
		t.Errorf("AcceptedCommandList %v", got)
	}
	if got := f.p.MatterGeneratedCommands(); !slices.Equal(got, []uint32{0x01, 0x03}) {
		t.Errorf("GeneratedCommandList %v", got)
	}
	for _, cmd := range []uint32{provdef.CmdQueryImage, provdef.CmdApplyUpdateRequest, provdef.CmdNotifyUpdateApplied} {
		c := provdef.Definition.Command(cmd, spec.Request)
		if c == nil || c.Access.WritePrivilege() != spec.PrivilegeOperate {
			t.Errorf("command 0x%02X access %+v, want Operate", cmd, c)
		}
	}
}

// TestParityMatterJS_OTAProviderConstants: the server's numbers are
// OtaSoftwareUpdateProviderServer.ts's — the 32-byte token (:53), the
// day-long DelayedActionTime cap (:51), Busy for two minutes while an
// update runs (:218, :235) and five when BDX cannot be enabled (:304), the
// 120-second consent retry (:248), the two-minute Discontinue (:435), the
// five-minute transfer idle timeout (:296) and OTA_PROGRESS_TIMEOUT
// (OtaProgress.ts:15, 15 minutes).
func TestParityMatterJS_OTAProviderConstants(t *testing.T) {
	t.Parallel()
	for name, got := range map[string][2]time.Duration{
		"busy":         {otaBusyRetry, 2 * time.Minute},
		"bdx busy":     {otaBdxBusyRetry, 5 * time.Minute},
		"consent":      {otaConsentRetry, 120 * time.Second},
		"stale":        {otaProgressTimeout, 15 * time.Minute},
		"idle timeout": {otaBdxMessageTimeout, 5 * time.Minute},
	} {
		if got[0] != got[1] {
			t.Errorf("%s: %v, want %v", name, got[0], got[1])
		}
	}
	if otaUpdateTokenLength != 32 || otaMaxDelayedActionTime != 86_400 || otaScope != "ota" {
		t.Errorf("token %d, cap %d, scope %q", otaUpdateTokenLength, otaMaxDelayedActionTime, otaScope)
	}
}

// TestParityMatterJS_OTAProviderResponseFields: every field of the three
// answers goes out as OP builds it — NotAvailable alone (:175-177), Busy
// with DelayedActionTime (:233-236), UpdateAvailable with ImageUri,
// SoftwareVersion, SoftwareVersionString, UpdateToken, UserConsentNeeded
// only when true and MetadataForRequestor when set (:393-401),
// ApplyUpdateResponse with both mandatory fields (:456-459) — and decodes
// with the generated codecs the requestor uses.
func TestParityMatterJS_OTAProviderResponseFields(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, otaDelegatesConsent) // UserConsentNeeded goes out only for a delegated consent
	f.src.img.MetadataForRequestor = []byte{1, 2, 3}
	yes := true
	req := bdxQuery()
	req.RequestorCanConsent = &yes
	r := f.query(t, 7, req)
	b := spectest.Encode(t, r)
	back, err := spec.DecodeStruct[provdef.QueryImageResponse](spectest.Decode(t, b))
	if err != nil {
		t.Fatal(err)
	}
	if back.Status != provdef.StatusUpdateAvailable || back.ImageUri == nil || back.SoftwareVersion == nil || back.SoftwareVersionString == nil ||
		back.UpdateToken == nil || back.UserConsentNeeded == nil || !*back.UserConsentNeeded || back.MetadataForRequestor == nil ||
		back.DelayedActionTime != nil {
		t.Fatalf("UpdateAvailable %+v", back)
	}
	// A version other than the token's is refused (the apply table's row 3).
	a := f.apply(t, *r.UpdateToken, 3)
	ab, err := spec.DecodeStruct[provdef.ApplyUpdateResponse](spectest.Decode(t, spectest.Encode(t, a)))
	if err != nil || ab.Action != provdef.ApplyUpdateActionDiscontinue || ab.DelayedActionTime != 120 {
		t.Fatalf("ApplyUpdateResponse %+v %v", ab, err)
	}
}

// The provider's answers of matter.js packages/node/test/behaviors/ota/OtaTest.ts
// at b25ed960. matter.js drives a requestor to the provider on a MockSite
// pair and rewrites its requests (applyRewritten :1767-1851, the admission
// table :5425-5545); the provider's answer is the observable, so these run
// on the provider (otaFixture) and its default manager
// (NewOTAStandaloneManager), the requests as the requestor sends them. A
// removed consent is the manager's removeConsent on the attempts
// (OTAUpdateAttempts.RemoveConsent, SoftwareUpdateManager.ts:1670-1692);
// the queue's half of each case is controller/softwareupdate's.

// removeConsent is SoftwareUpdateManager.removeConsent's half on the
// attempts, as a manager with a consent list runs it.
func (f *otaFixture) removeConsent(version uint32, all bool) {
	m := f.p.mgr.(*otaStandaloneManager)
	m.mu.Lock()
	m.set.RemoveConsent(otaTestPeer, version, all)
	m.mu.Unlock()
}

// TestParityMatterJS_OTAProviderApplyTable ports the apply rows of
// OtaTest.ts :1853-1945 (applyRewritten), :1647 and :4975 at b25ed960:
// the table of SoftwareUpdateManager.applyUpdate (:1897-1956).
func TestParityMatterJS_OTAProviderApplyTable(t *testing.T) {
	t.Parallel()
	unknownToken := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	removed := func(f *otaFixture) { f.removeConsent(2, false) }
	staleAfterRemoval := func(f *otaFixture) {
		f.removeConsent(2, false)
		// A new query of the node finds the attempt stale and ends it.
		f.clock.Advance(16 * time.Minute)
		if r := f.query(t, 8, provdef.QueryImageRequest{VendorId: 0xFFF1, ProductId: 0x8001, SoftwareVersion: 1}); r.Status != provdef.StatusDownloadProtocolNotSupported {
			t.Fatalf("the query that ends the stale attempt: %+v", r)
		}
	}
	for _, tc := range []struct {
		name    string
		before  func(*otaFixture)
		token   []byte
		version uint32
		want    provdef.ApplyUpdateActionEnum
		status  OTAUpdateStatus
	}{
		{"applies the version of a known update token when the requestor asks for version 0", nil, nil, 0, provdef.ApplyUpdateActionProceed, OTAUpdateApplying},
		{"refuses a version other than 0 that differs from its update token", nil, nil, 3, provdef.ApplyUpdateActionDiscontinue, OTAUpdateCancelled},
		{"applies the version of its update token once its consent is gone without removeConsent(), as the offer allowed it", nil, nil, 2, provdef.ApplyUpdateActionProceed, OTAUpdateApplying},
		{"refuses the version of its update token once the consent is removed without a queued update", removed, nil, 2, provdef.ApplyUpdateActionDiscontinue, OTAUpdateCancelled},
		{"applies a consented version under an update token it does not know, as after a restart", nil, unknownToken, 2, provdef.ApplyUpdateActionProceed, OTAUpdateApplying},
		{"refuses version 0 under an update token it does not know", nil, unknownToken, 0, provdef.ApplyUpdateActionDiscontinue, OTAUpdateQuerying},
		{"applies a version above 0 under an update token it does not know, also without a consent", nil, unknownToken, 1, provdef.ApplyUpdateActionProceed, OTAUpdateApplying},
		{"applies a version above 0 under an update token whose attempt a new query ended as stale, also after its consent was removed", staleAfterRemoval, nil, 2, provdef.ApplyUpdateActionProceed, OTAUpdateApplying},
		{"refuses version 0 for a known update token once its consent is gone", removed, nil, 0, provdef.ApplyUpdateActionDiscontinue, OTAUpdateCancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newOTAFixture(t, nil)
			r := f.query(t, 7, bdxQuery())
			if tc.before != nil {
				tc.before(f)
			}
			token := tc.token
			if token == nil {
				token = *r.UpdateToken
			}
			if got := f.apply(t, token, tc.version).Action; got != tc.want {
				t.Fatalf("action %d, want %d", got, tc.want)
			}
			states := f.status.states()
			if last := states[len(states)-1]; last != tc.status {
				t.Errorf("last status %v, want %v (all %v)", last, tc.status, states)
			}
		})
	}

	t.Run("tracks an allowed apply under an update token it did not hand out as an applying update", func(t *testing.T) {
		t.Parallel()
		f := newOTAFixture(t, nil)
		f.query(t, 7, bdxQuery())
		f.apply(t, unknownToken, 2)
		a := f.p.mgr.AttemptFor(otaTestPeer, unknownToken)
		if a == nil || a.Status() != OTAUpdateApplying || !a.Answered() {
			t.Fatalf("attempt %v", a)
		}
		if f.attempts() != 1 {
			t.Errorf("%d attempts: the apply's attempt replaces the query's", f.attempts())
		}
	})

	t.Run("answers a resent apply request of an applying attempt again and reports it applying once", func(t *testing.T) {
		t.Parallel()
		f := newOTAFixture(t, nil)
		r := f.query(t, 7, bdxQuery())
		for range 2 {
			if a := f.apply(t, *r.UpdateToken, 2); a.Action != provdef.ApplyUpdateActionProceed {
				t.Fatalf("apply %+v", a)
			}
		}
	})

	// The C-24 regression of F-b25ed960-57: the removal of the consent of
	// an applying update no longer stops it (:1951).
	t.Run("lets an applying update apply again once its consent is removed, which hasConsent() no longer reports", func(t *testing.T) {
		t.Parallel()
		f := newOTAFixture(t, nil)
		r := f.query(t, 7, bdxQuery())
		f.apply(t, *r.UpdateToken, 2)
		f.removeConsent(0, true)
		if a := f.apply(t, *r.UpdateToken, 2); a.Action != provdef.ApplyUpdateActionProceed {
			t.Fatalf("apply after the removal %+v, want Proceed", a)
		}
	})
}

// TestParityMatterJS_OTAProviderApplyDelay ports the wire half of OtaTest.ts
// :2007-2027 at b25ed960: what a Proceed sends under the provider's apply
// delay (OTAProviderConfig.ApplyDelay, State.applyDelay). The
// RebootResubscribeArmer half is SoftwareUpdateManager's (the register
// names it).
func TestParityMatterJS_OTAProviderApplyDelay(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		delay time.Duration
		want  uint32
	}{
		{"expects the device's return only after the delay it was allowed to apply with", 180 * time.Second, 180},
		{"rounds an apply delay up to whole seconds, since it is a minimum wait", 1500 * time.Millisecond, 2},
		{"sends at most a day of apply delay, which a requestor may treat a longer one as", 48 * time.Hour, 86_400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newOTAFixture(t, func(c *OTAProviderConfig) { c.ApplyDelay = tc.delay })
			r := f.query(t, 7, bdxQuery())
			if a := f.apply(t, *r.UpdateToken, 2); a.Action != provdef.ApplyUpdateActionProceed || a.DelayedActionTime != tc.want {
				t.Errorf("ApplyUpdateResponse %+v, want Proceed %d", a, tc.want)
			}
			if a := f.p.mgr.AttemptFor(otaTestPeer, *r.UpdateToken); a == nil || a.ApplyDelay() != time.Duration(tc.want)*time.Second {
				t.Errorf("the attempt applies with %v, want %d s", a.ApplyDelay(), tc.want)
			}
		})
	}

	// The attempt of an apply with a delay is not stale while the device
	// waits it out (:2028, OtaUpdateAttempts.ts:107-110).
	t.Run("does not count an update as stalled while the device waits out its apply delay", func(t *testing.T) {
		t.Parallel()
		f := newOTAFixture(t, func(c *OTAProviderConfig) { c.ApplyDelay = time.Hour })
		r := f.query(t, 7, bdxQuery())
		f.apply(t, *r.UpdateToken, 2)
		a := f.p.mgr.AttemptFor(otaTestPeer, *r.UpdateToken)
		if a.IsStale(f.clock.Now().Add(time.Hour + 14*time.Minute)) {
			t.Error("stale within the apply delay and the progress timeout")
		}
		if !a.IsStale(f.clock.Now().Add(time.Hour + 16*time.Minute)) {
			t.Error("not stale after the apply delay and the progress timeout")
		}
	})
}

// TestParityMatterJS_OTAProviderAdmission ports the admission table of
// OtaTest.ts :5425-5545 at b25ed960 ("admits a new query of a node with a
// running attempt") on the default manager: the rows that read the
// attempts and the transfer (SoftwareUpdateManager.admitQuery,
// :1802-1881). The linked-update half (stillLinked, the suppressed
// startUp) is controller/softwareupdate's; "answers busy and keeps the
// attempt when the transfer of a node that restarted cannot be cancelled"
// needs a failing cancel, which the BDX responder's forced close never
// is — it is ported on the manager's seam.
func TestParityMatterJS_OTAProviderAdmission(t *testing.T) {
	t.Parallel()
	// activity has the transfer's session (7) last active at transfer and
	// the query's (8) at query, relative to the fixture's start.
	activity := func(start time.Time, transfer, query time.Duration) func(uint16) (time.Time, bool) {
		return func(id uint16) (time.Time, bool) {
			switch id {
			case 7:
				return start.Add(transfer), true
			case 8:
				return start.Add(query), true
			}
			return time.Time{}, false
		}
	}
	for _, tc := range []struct {
		name              string
		transfer, query   time.Duration
		download, stale   bool
		querySession      uint16
		admitted, ended   bool
		rebootQueries     int
		transferCancelled bool
	}{
		{name: "ends a stale attempt and resets its update, also while its BDX transfer looks newer", download: true, stale: true, transfer: time.Hour, querySession: 8, admitted: true, ended: true},
		{name: "ends an attempt without a BDX transfer and resets its update", querySession: 8, admitted: true, ended: true},
		{name: "cancels the transfer of a node that restarted, ends its attempt, resets its update and suppresses the startUp", download: true, query: time.Second, querySession: 8, admitted: true, ended: true, rebootQueries: 1, transferCancelled: true},
		{name: "answers busy while the transfer runs on the session of the query", download: true, querySession: 7},
		{name: "answers busy while the transfer on another session is as recent as the session of the query", download: true, querySession: 8},
		{name: "answers busy while the transfer is newer than the session of the query", download: true, transfer: time.Second, querySession: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var reboots []uint16
			var mu sync.Mutex
			start := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
			f := newOTAFixture(t, func(c *OTAProviderConfig) {
				c.SessionActivity = activity(start, tc.transfer, tc.query)
				c.RebootQuery = func(_ OTAPeer, sessionID uint16) {
					mu.Lock()
					reboots = append(reboots, sessionID)
					mu.Unlock()
				}
			})
			r := f.query(t, 7, bdxQuery())
			var done chan struct{}
			if tc.download {
				_, done = f.startDownload(t, 7, *r.ImageUri)
			}
			if tc.stale {
				f.clock.Advance(16 * time.Minute)
			}
			first := f.p.mgr.AttemptFor(otaTestPeer, *r.UpdateToken)
			second := f.query(t, tc.querySession, bdxQuery())
			if admitted := second.Status == provdef.StatusUpdateAvailable; admitted != tc.admitted {
				t.Fatalf("second query %+v, admitted %v", second, tc.admitted)
			}
			if !tc.admitted && (second.Status != provdef.StatusBusy || *second.DelayedActionTime != 120) {
				t.Errorf("not admitted: %+v, want Busy 120", second)
			}
			if first.Ended() != tc.ended {
				t.Errorf("first attempt ended %v, want %v", first.Ended(), tc.ended)
			}
			mu.Lock()
			if len(reboots) != tc.rebootQueries || (tc.rebootQueries > 0 && reboots[0] != tc.querySession) {
				t.Errorf("reboot queries %v, want %d on session %d", reboots, tc.rebootQueries, tc.querySession)
			}
			mu.Unlock()
			if done != nil {
				if tc.transferCancelled {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Fatal("the transfer of the restarted node was not cancelled")
					}
				} else {
					_ = f.bdx.DisablePeerForScope(bdx.Peer{FabricIndex: otaTestFabric, NodeID: otaTestPeerNode}, otaScope, true)
					<-done
				}
			}
		})
	}
}

// TestParityMatterJS_OTAProviderConsentWhileQuerying ports OtaTest.ts
// :4733, :5547 and :6290 at b25ed960: a query whose attempt ends while the
// consent is asked for answers NotAvailable without a status of its own
// (offerUpdate false, SoftwareUpdateManager.ts:1754-1761), and one whose
// consent is granted reports Querying for its offer.
func TestParityMatterJS_OTAProviderConsentWhileQuerying(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		remove   bool
		status   provdef.StatusEnum
		statuses []OTAUpdateStatus
	}{
		{"answers NotAvailable instead of an offer whose consent was removed while the query ran", true, provdef.StatusNotAvailable, []OTAUpdateStatus{OTAUpdateWaitForConsent}},
		{"reports Querying for an offer once the consent it waited for is granted", false, provdef.StatusUpdateAvailable, []OTAUpdateStatus{OTAUpdateWaitForConsent, OTAUpdateQuerying}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var f *otaFixture
			f = newOTAFixture(t, func(c *OTAProviderConfig) {
				otaNeedsRequestorConsent(c)
				c.Consent = otaConsentFunc(func() (OTAConsentState, time.Duration) {
					if tc.remove {
						f.removeConsent(0, true)
					}
					return OTAConsentGranted, 0
				})
			})
			r := f.query(t, 7, bdxQuery())
			if r.Status != tc.status || (r.UserConsentNeeded != nil && *r.UserConsentNeeded) {
				t.Fatalf("response %+v, want status %d without UserConsentNeeded", r, tc.status)
			}
			if got := f.status.states(); !statusesEqual(got, tc.statuses) {
				t.Errorf("states %v, want %v", got, tc.statuses)
			}
		})
	}

	// :4564: an offer whose consent the requestor obtains is its own
	// consent — the offer allows the apply (OtaUpdateAttempts.ts:81).
	t.Run("lets a requestor that obtains consent itself apply the newest image it was offered without a consent", func(t *testing.T) {
		t.Parallel()
		f := newOTAFixture(t, otaDelegatesConsent) // delegateConsent: true (:4567)
		r := f.query(t, 7, canConsentQuery())
		if r.UserConsentNeeded == nil || !*r.UserConsentNeeded {
			t.Fatalf("response %+v, want UserConsentNeeded", r)
		}
		if a := f.apply(t, *r.UpdateToken, 2); a.Action != provdef.ApplyUpdateActionProceed {
			t.Fatalf("apply %+v, want Proceed", a)
		}
	})

	// :4666: delegation is off by default (SoftwareUpdateManager.ts:2171,
	// mayDelegateConsentTo :638-647), so a requestor that can consent gets
	// no offer without a consent (:698-705) — the provider asks its consent
	// port, and without one declines (OtaSoftwareUpdateProviderServer.ts:201-231).
	t.Run("offers a requestor that can consent the newest image without a consent only while delegation is on", func(t *testing.T) {
		t.Parallel()
		off := newOTAFixture(t, otaNeedsRequestorConsent)
		if r := off.query(t, 7, canConsentQuery()); r.Status != provdef.StatusNotAvailable {
			t.Fatalf("delegation off: response %+v, want NotAvailable", r)
		}
		on := newOTAFixture(t, otaDelegatesConsent)
		if r := on.query(t, 7, canConsentQuery()); r.Status != provdef.StatusUpdateAvailable || r.SoftwareVersion == nil || *r.SoftwareVersion != 2 {
			t.Fatalf("delegation on: response %+v, want version 2", r)
		}
	})
}

// TestParityMatterJS_OTAProviderUpdateApplied ports the attempt's half of
// OtaTest.ts :1682 and :4357 at b25ed960: NotifyUpdateApplied ends the
// attempt of its token Done (#updateApplied :1970-1980), and one under an
// unknown token ends none.
func TestParityMatterJS_OTAProviderUpdateApplied(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	r := f.query(t, 7, bdxQuery())
	a := f.p.mgr.AttemptFor(otaTestPeer, *r.UpdateToken)
	notify := func(token []byte) {
		if _, err := f.p.MatterInvoke(otaCtx(7), provdef.CmdNotifyUpdateApplied, provdef.NotifyUpdateAppliedRequest{UpdateToken: token, SoftwareVersion: 2}); err != nil {
			t.Fatal(err)
		}
	}
	notify([]byte{9, 9})
	if a.Ended() {
		t.Fatal("an unknown token ended the attempt")
	}
	notify(*r.UpdateToken)
	if !a.Ended() || a.Status() != OTAUpdateDone || f.p.mgr.AttemptFor(otaTestPeer, *r.UpdateToken) != nil {
		t.Errorf("attempt ended %v, status %v", a.Ended(), a.Status())
	}
}

// otaNeedsRequestorConsent offers an image that needs a consent.
func otaNeedsRequestorConsent(c *OTAProviderConfig) {
	src := c.ImageSource.(*otaTestSource)
	src.img.ConsentRequired = true
}

// otaDelegatesConsent offers an image that needs a consent, with the
// delegation to a requestor that can consent on.
func otaDelegatesConsent(c *OTAProviderConfig) {
	otaNeedsRequestorConsent(c)
	c.DelegateConsentToRequestor = true
}

func canConsentQuery() provdef.QueryImageRequest {
	q := bdxQuery()
	yes := true
	q.RequestorCanConsent = &yes
	return q
}
