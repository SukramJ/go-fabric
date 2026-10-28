// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/client/jcm"
	"github.com/SukramJ/go-fabric/cluster/spec"
	jfadef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricadministrator"
	opcredsdef "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// rawBytesList encodes a list of octet strings.
type rawBytesList [][]byte

func (l rawBytesList) EncodeTLV(e *tlv.Encoder, tag tlv.Tag) {
	e.StartArray(tag)
	for _, b := range l {
		e.PutOctets(tlv.AnonymousTag(), b)
	}
	_ = e.EndContainer()
}

// jfbDevice is a scripted device that administers JF-B: it reports the
// JCM read paths and signs SignVIDVerificationRequest over the session's
// attestation challenge (the flow's).
type jfbDevice struct {
	f         *jfbFabric
	challenge func() []byte
	noAdmin   bool
}

func (d *jfbDevice) handle(req imRequest) (uint8, []byte, bool) {
	switch req.opcode {
	case im.OpcodeReadRequest:
		noc := opcredsdef.NOCStruct{Noc: d.f.noc, FabricIndex: d.f.index}
		noc.Icac.Value = d.f.icac
		type attr struct {
			ep            uint16
			cluster, attr uint32
			v             spec.Encodable
		}
		attrs := []attr{
			{0, opCredsCluster, opcredsdef.AttrFabrics, spec.List[opcredsdef.FabricDescriptorStruct]{{RootPublicKey: d.f.rootPub, VendorId: d.f.vendorID, FabricId: d.f.fabricID, NodeId: d.f.nodeID, FabricIndex: d.f.index}}},
			{0, opCredsCluster, opcredsdef.AttrNocs, spec.List[opcredsdef.NOCStruct]{noc}},
			{0, opCredsCluster, opcredsdef.AttrTrustedRootCertificates, rawBytesList{d.f.rcac}},
		}
		if !d.noAdmin {
			attrs = append(attrs, attr{1, jfadef.ClusterID, jfadef.AttrAdministratorFabricIndex, uintValue(d.f.index)})
		}
		return im.OpcodeReportData, encodeTLV(func(e *tlv.Encoder) {
			e.StartStruct(tlv.AnonymousTag())
			e.StartArray(tlv.ContextTag(1))
			for _, a := range attrs {
				e.StartStruct(tlv.AnonymousTag())
				e.StartStruct(tlv.ContextTag(1))
				e.PutUint(tlv.ContextTag(0), 1)
				im.ConcreteAttributePath{Endpoint: a.ep, Cluster: a.cluster, Attribute: a.attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}.MarshalTLV(e, tlv.ContextTag(1))
				a.v.EncodeTLV(e, tlv.ContextTag(2))
				_ = e.EndContainer()
				_ = e.EndContainer()
			}
			_ = e.EndContainer()
			_ = e.EndContainer()
		}), true
	case im.OpcodeInvokeRequest:
		path, fields, err := decodeInvoke(req.payload)
		if err != nil {
			return 0, nil, false
		}
		r, err := spec.DecodeStruct[opcredsdef.SignVidVerificationRequestRequest](fields)
		if err != nil {
			return 0, nil, false
		}
		resp, _, err := jfbSigner{f: d.f, challenge: d.challenge()}.SignVIDVerification(context.Background(), r.FabricIndex, r.ClientChallenge)
		if err != nil {
			return 0, nil, false
		}
		path.Command = opcredsdef.CmdSignVidVerificationResponse
		return im.OpcodeInvokeResponse, invokeResponseOf(path, resp), true
	}
	return 0, nil, false
}

// TestJointFabricStep: the JCM step over a scripted device — the read,
// the parse, the vendor id verified over the session, the consent; a
// refused consent and a device without an administrator fail it with
// ErrJointFabric.
func TestJointFabricStep(t *testing.T) {
	t.Parallel()
	f := newJFBFabric(t)
	for _, c := range []struct {
		name    string
		consent bool
		noAdmin bool
		want    jcm.Result
	}{
		{"consent", true, false, jcm.Success},
		{"refused", false, false, jcm.UserDeniedConsent},
		{"no administrator", true, true, 0xFFFF},
	} {
		dev := &jfbDevice{f: f, noAdmin: c.noAdmin}
		fl, _ := newTestFlow(t, dev.handle)
		dev.challenge = func() []byte { return fl.challenge }
		var asked TrustInfo
		fl.opts.JointFabric = &JointFabricOptions{Consent: func(info TrustInfo) bool { asked = info; return c.consent }}
		_, err := fl.jointFabricTrustVerification(t.Context())
		switch {
		case c.want == jcm.Success:
			if err != nil || asked.AdminVendorID != f.vendorID {
				t.Errorf("%s: %v, asked %+v", c.name, err, asked)
			}
		case c.want == 0xFFFF:
			if !errors.Is(err, ErrJointFabric) || !errors.Is(err, errJCMParse) {
				t.Errorf("%s: %v", c.name, err)
			}
		default:
			var fail jcm.Failure
			if !errors.Is(err, ErrJointFabric) || !errors.As(err, &fail) || fail.Result != c.want || fail.Stage != jcm.StageAskingUserForConsent {
				t.Errorf("%s: %v", c.name, err)
			}
		}
	}
	fl, _ := newTestFlow(t, (&jfbDevice{f: f}).handle)
	fl.opts.JointFabric = &JointFabricOptions{}
	fl.initializeSteps()
	fl.sortSteps()
	var names []string
	for _, s := range fl.steps {
		names = append(names, s.name)
	}
	for i, n := range names {
		if n == StepJointFabric && (names[i-1] != "OperationalCredentials.DeviceAttestation" || names[i+1] != "OperationalCredentials.Certificates") {
			t.Fatalf("steps %v", names)
		}
	}
}
