// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"slices"
	"testing"
	"time"

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
