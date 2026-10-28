// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/client/jcm"
	"github.com/SukramJ/go-fabric/cluster/spec"
	jfadef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricadministrator"
	opcredsdef "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestJCMParseRefusals: ParseExtraCommissioningInfo's refusals
// (connectedhomeip jcm/DeviceCommissioner.cpp:87-328 at 6170af84) — a null
// AdministratorFabricIndex, a VID Verification Statement, a fabric
// without an ICAC, no matching root.
func TestJCMParseRefusals(t *testing.T) {
	anon := tlv.AnonymousTag()
	edit := func(f *jfbFabric, path uint32, n func(t *testing.T) spec.Node) func(t *testing.T) error {
		return func(t *testing.T) error {
			t.Helper()
			tr, _ := newTrust(t, &delegate{})
			r := f.report(t)
			for i := range r.Attributes {
				if r.Attributes[i].Path.Attribute == path && (path != jfadef.AttrAdministratorFabricIndex || r.Attributes[i].Path.Cluster == jfadef.ClusterID) {
					r.Attributes[i].Value = n(t)
				}
			}
			return tr.parse(r)
		}
	}
	f := newJFBFabric(t)
	cases := map[string]func(t *testing.T) error{
		"null AdministratorFabricIndex": edit(f, jfadef.AttrAdministratorFabricIndex, func(t *testing.T) spec.Node {
			t.Helper()
			return nodeOf(t, func(e *tlv.Encoder) { e.PutNull(anon) })
		}),
		"VID Verification Statement": edit(f, opcredsdef.AttrFabrics, func(t *testing.T) spec.Node {
			t.Helper()
			st := make([]byte, 85)
			return nodeOf(t, func(e *tlv.Encoder) {
				spec.List[opcredsdef.FabricDescriptorStruct]{{RootPublicKey: f.rootPub, VendorId: f.vendorID, FabricId: f.fabricID, FabricIndex: f.index, VidVerificationStatement: &st}}.EncodeTLV(e, anon)
			})
		}),
		"no ICAC": edit(f, opcredsdef.AttrNocs, func(t *testing.T) spec.Node {
			t.Helper()
			n := opcredsdef.NOCStruct{Noc: f.noc, FabricIndex: f.index}
			n.Icac.Null = true
			return nodeOf(t, func(e *tlv.Encoder) { spec.List[opcredsdef.NOCStruct]{n}.EncodeTLV(e, anon) })
		}),
		"another root": edit(f, opcredsdef.AttrTrustedRootCertificates, func(t *testing.T) spec.Node {
			t.Helper()
			other := newJFBFabric(t)
			return nodeOf(t, func(e *tlv.Encoder) { e.StartArray(anon); e.PutOctets(anon, other.rcac); _ = e.EndContainer() })
		}),
	}
	for name, run := range cases {
		if err := run(t); !errors.Is(err, errJCMParse) {
			t.Errorf("%s: %v, want errJCMParse", name, err)
		}
	}
}

// TestJCMVerifyAdministratorInformation: the administrator CAT is
// required (DeviceCommissioner.cpp:383-390).
func TestJCMVerifyAdministratorInformation(t *testing.T) {
	tr, f := newTrust(t, &delegate{})
	if err := tr.parse(f.report(t)); err != nil {
		t.Fatal(err)
	}
	if r := tr.verifyAdministratorInformation(); r != jcm.Success {
		t.Fatalf("with the CAT: %s", r)
	}
	tr.info.AdminNOC = slices.Clone(tr.info.AdminICAC) // an ICAC carries no CAT
	if r := tr.verifyAdministratorInformation(); r != jcm.InvalidAdministratorCAT {
		t.Fatalf("without the CAT: %s", r)
	}
	tr.info.AdminEndpointID = jcm.InvalidEndpointID
	if r := tr.verifyAdministratorInformation(); r != jcm.InvalidAdministratorEndpointID {
		t.Fatalf("invalid endpoint: %s", r)
	}
	tr.opts.Consent = nil
	if r := tr.askUserForConsent(); r != jcm.TrustVerificationDelegateNotSet {
		t.Fatalf("no consent: %s", r)
	}
}
