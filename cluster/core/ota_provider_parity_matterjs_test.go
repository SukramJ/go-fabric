// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"slices"
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
	f := newOTAFixture(t, nil)
	f.src.img.MetadataForRequestor = []byte{1, 2, 3}
	f.src.img.ConsentRequired = true
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
	a := f.apply(t, *r.UpdateToken, 2)
	ab, err := spec.DecodeStruct[provdef.ApplyUpdateResponse](spectest.Decode(t, spectest.Encode(t, a)))
	if err != nil || ab.Action != provdef.ApplyUpdateActionDiscontinue || ab.DelayedActionTime != 120 {
		t.Fatalf("ApplyUpdateResponse %+v %v", ab, err)
	}
}

// The provider's answers of matter.js packages/node/test/behaviors/ota/OtaTest.ts
// at 862ad75f. matter.js drives a requestor to the provider on a MockSite
// pair and rewrites its requests (applyRewritten :1488-1539, applyWithDelay
// :1584-1637, the queryImage cases :2288-2591); the provider's answer is
// the observable, so these run on the provider (otaFixture), its requests
// as the requestor sends them. SoftwareUpdateManager's consent
// bookkeeping is the provider's: the consent a QueryImage records for an
// image the source offers without asking (consents), none for an image
// whose consent the requestor obtains itself (UserConsentNeeded).

// otaNeedsRequestorConsent offers an image whose consent the requestor
// obtains: the provider holds none.
func otaNeedsRequestorConsent(c *OTAProviderConfig) {
	src := c.ImageSource.(*otaTestSource)
	src.img.ConsentRequired = true
}

func canConsentQuery() provdef.QueryImageRequest {
	q := bdxQuery()
	yes := true
	q.RequestorCanConsent = &yes
	return q
}

// TestParityMatterJS_OTAProviderApplyConsentAndToken ports the apply
// answers of OtaTest.ts :1437-1582.
func TestParityMatterJS_OTAProviderApplyConsentAndToken(t *testing.T) {
	t.Parallel()

	// :1437-1486. DelayedActionTime is seconds on the wire.
	t.Run("refuses an apply it holds no consent for, naming the wait in seconds", func(t *testing.T) {
		t.Parallel()
		f := newOTAFixture(t, otaNeedsRequestorConsent)
		r := f.query(t, 7, canConsentQuery())
		a := f.apply(t, *r.UpdateToken, 2)
		if a.Action != provdef.ApplyUpdateActionDiscontinue || a.DelayedActionTime != 120 {
			t.Fatalf("ApplyUpdateResponse %+v, want Discontinue 120", a)
		}
	})

	// applyRewritten: the query of the update, then its apply as the
	// rewritten request; the image is version 2.
	applyRewritten := func(t *testing.T, consent func(f *otaFixture), token []byte, version uint32) provdef.ApplyUpdateActionEnum {
		t.Helper()
		f := newOTAFixture(t, nil)
		r := f.query(t, 7, bdxQuery())
		if consent != nil {
			consent(f)
		}
		if token == nil {
			token = *r.UpdateToken
		}
		return f.apply(t, token, version).Action
	}
	unknownToken := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	withdrawn := func(f *otaFixture) {
		f.p.mu.Lock()
		delete(f.p.consents, otaTestPeer)
		f.p.mu.Unlock()
	}
	// addUpdateConsent for the next version replaces the one the query
	// recorded (the provider keeps one consent per peer).
	movedToNext := func(f *otaFixture) {
		f.p.mu.Lock()
		f.p.consents[otaTestPeer] = 3
		f.p.mu.Unlock()
	}
	for _, tc := range []struct {
		name    string
		consent func(*otaFixture)
		token   []byte
		version uint32
		want    provdef.ApplyUpdateActionEnum
	}{
		// :1541.
		{"applies the version of a known update token when the requestor asks for version 0", nil, nil, 0, provdef.ApplyUpdateActionProceed},
		// :1547.
		{"refuses a version other than 0 that differs from its update token", nil, nil, 3, provdef.ApplyUpdateActionDiscontinue},
		// :1553.
		{"refuses a version other than 0 that differs from its update token, even with consent for it", movedToNext, nil, 3, provdef.ApplyUpdateActionDiscontinue},
		// :1561.
		{"applies a consented version under an update token it does not know, as after a restart", nil, unknownToken, 2, provdef.ApplyUpdateActionProceed},
		// :1567.
		{"refuses version 0 for an update token it does not know", nil, unknownToken, 0, provdef.ApplyUpdateActionDiscontinue},
		// :1577.
		{"refuses version 0 for a known update token once its consent is gone", withdrawn, nil, 0, provdef.ApplyUpdateActionDiscontinue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := applyRewritten(t, tc.consent, tc.token, tc.version); got != tc.want {
				t.Errorf("action %d, want %d", got, tc.want)
			}
		})
	}
}

// TestParityMatterJS_OTAProviderApplyDelay ports the wire half of OtaTest.ts
// :1639-1658: what a Proceed sends under the provider's apply delay. The
// delay is matter.js's State.applyDelay, the ApplyPolicy's here; the
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
			f := newOTAFixture(t, func(c *OTAProviderConfig) { c.ApplyPolicy = fixedPolicy{provdef.ApplyUpdateActionProceed, tc.delay} })
			r := f.query(t, 7, bdxQuery())
			if a := f.apply(t, *r.UpdateToken, 2); a.Action != provdef.ApplyUpdateActionProceed || a.DelayedActionTime != tc.want {
				t.Errorf("ApplyUpdateResponse %+v, want Proceed %d", a, tc.want)
			}
		})
	}
}

// TestParityMatterJS_OTAProviderQueryInProgress ports the queryImage cases
// of OtaTest.ts :2288-2591 on the provider's in-progress entries
// (internal.inProgressDetails, the Go inProgress map) and its BDX
// responder's sessions (BdxProtocol.sessionFor).
func TestParityMatterJS_OTAProviderQueryInProgress(t *testing.T) {
	t.Parallel()
	stale := bytes.Repeat([]byte{0xaa}, otaUpdateTokenLength)
	has := func(f *otaFixture, token []byte) bool {
		f.p.mu.Lock()
		defer f.p.mu.Unlock()
		return f.p.inProgress[otaKey(otaTestPeer, token)] != nil
	}
	size := func(f *otaFixture) int {
		f.p.mu.Lock()
		defer f.p.mu.Unlock()
		return len(f.p.inProgress)
	}

	// :2288-2393. A Downloading entry, fresh, without a BDX session.
	t.Run("queryImage Case A: stale in-progress entry with no BDX session → cleared, fresh queryImage proceeds", func(t *testing.T) {
		t.Parallel()
		f := newOTAFixture(t, nil)
		f.p.mu.Lock()
		f.p.inProgress[otaKey(otaTestPeer, stale)] = &otaInProgress{peer: otaTestPeer, lastState: OTAUpdateDownloading, timestamp: f.clock.Now().Add(time.Hour)}
		f.p.mu.Unlock()
		r := f.query(t, 7, bdxQuery())
		if r.Status != provdef.StatusUpdateAvailable {
			t.Fatalf("response %+v", r)
		}
		if has(f, stale) {
			t.Error("the stale aa entry stayed")
		}
		if size(f) == 0 {
			t.Error("no fresh entry")
		}
	})

	// :2395-2522. The transfer of the update runs on another session, its
	// last activity before the query's: the requestor rebooted. The
	// SoftwareUpdateManager's suppressNextStartUp record is the manager's
	// (the report names it); the provider's half is the cancelled
	// transfer, the removed entry and the UpdateAvailable answer.
	t.Run("queryImage Case B: reboot detected (different session, older BDX timestamp) → BDX cancelled, entry cleared, startUp suppressed, UpdateAvailable", func(t *testing.T) {
		t.Parallel()
		f := newOTAFixture(t, nil)
		r := f.query(t, 7, bdxQuery())
		_, done := f.startDownload(t, 7, *r.ImageUri)
		f.clock.Advance(time.Second)
		r2 := f.query(t, 0xDEAD, bdxQuery())
		if r2.Status != provdef.StatusUpdateAvailable {
			t.Fatalf("response %+v, want UpdateAvailable (not Busy)", r2)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the old transfer was not cancelled")
		}
		if has(f, *r.UpdateToken) {
			t.Error("the old entry stayed")
		}
	})

	// :2524-2591. The transfer runs on the query's own session: the update
	// in progress is kept, the query is Busy.
	t.Run("queryImage Case C: in-progress with active BDX session → in-progress state preserved when BDX active", func(t *testing.T) {
		t.Parallel()
		f := newOTAFixture(t, nil)
		r := f.query(t, 7, bdxQuery())
		_, done := f.startDownload(t, 7, *r.ImageUri)
		if busy := f.query(t, 7, bdxQuery()); busy.Status != provdef.StatusBusy {
			t.Fatalf("response %+v, want Busy", busy)
		}
		if !has(f, *r.UpdateToken) || size(f) != 1 {
			t.Errorf("entries %d, the update's kept %v", size(f), has(f, *r.UpdateToken))
		}
		_ = f.bdx.DisablePeerForScope(bdx.Peer{FabricIndex: otaTestFabric, NodeID: otaTestPeerNode}, otaScope, true)
		<-done
	})
}

// TestParityMatterJS_OTAProviderDoneClearsConsent ports OtaTest.ts
// :1403-1435. matter.js adds a consent to the SoftwareUpdateManager and
// reports Done to it (onOtaStatusChange); the Go provider holds the
// consent its query recorded (consents, SoftwareUpdateManager.hasConsent)
// and reaches Done through the NotifyUpdateApplied that reports it.
func TestParityMatterJS_OTAProviderDoneClearsConsent(t *testing.T) {
	t.Parallel()
	t.Run("Done status clears the consent for the completed version", func(t *testing.T) {
		t.Parallel()
		f := newOTAFixture(t, nil)
		r := f.query(t, 7, bdxQuery())
		if !f.p.hasUpdateConsent(otaTestPeer, 2) {
			t.Fatal("no consent for the offered version")
		}
		if _, err := f.p.MatterInvoke(otaCtx(7), provdef.CmdNotifyUpdateApplied, provdef.NotifyUpdateAppliedRequest{UpdateToken: *r.UpdateToken, SoftwareVersion: 2}); err != nil {
			t.Fatal(err)
		}
		if f.p.hasUpdateConsent(otaTestPeer, 2) {
			t.Error("the consent outlived Done")
		}
		if d := f.p.detailsForPeer(otaTestPeer, nil); d != nil {
			t.Errorf("an update stayed in progress: %+v", d)
		}
	})
}
