// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/cluster/spec"
	gencomm "thingfabric.dev/go-fabric/cluster/spec/generalcommissioning"
	jfadef "thingfabric.dev/go-fabric/cluster/spec/jointfabricadministrator"
	opcredsdef "thingfabric.dev/go-fabric/cluster/spec/operationalcredentials"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// TestReportedValueDecodedByShapeParityMatterjs: a reported value is
// decoded by its shape — an octet string or a string past its constraint
// is read, a struct without a mandatory field is decoded with the field
// unset — and a value whose shape does not decode is absent, the read
// going on. matter.js decodes a data report's attribute with
// decodeAttributeValueWithSchema, whose decodeTlv does not validate, and
// skips an entry that does not decode (InputChunk.ts:299-303, :312-320;
// AttributeDataDecoder.ts:311-335; TlvSchema.ts:64-66 at db021da0).
// Review II, F-db021da0-15.
func TestReportedValueDecodedByShapeParityMatterjs(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.DiscardHandler)
	anon := tlv.AnonymousTag()

	t.Run("constraints are not checked", func(t *testing.T) {
		t.Parallel()
		// FabricDescriptorStruct: Label is 0..32, RootPublicKey 65..65,
		// FabricIndex 1..254 in the generated decoder.
		n := nodeOf(t, func(e *tlv.Encoder) {
			e.StartArray(anon)
			e.StartStruct(anon)
			e.PutOctets(tlv.ContextTag(1), make([]byte, 70))
			e.PutUint(tlv.ContextTag(2), 0xFFF1)
			e.PutUint(tlv.ContextTag(3), 1)
			e.PutUint(tlv.ContextTag(4), 2)
			e.PutUTF8(tlv.ContextTag(5), strings.Repeat("x", 40))
			e.PutUint(tlv.ContextTag(254), 255)
			_ = e.EndContainer()
			_ = e.EndContainer()
		})
		def := opcredsdef.FabricDescriptorStructDef
		got, ok := reportedStructList(log, "Fabrics", def, n)
		if !ok || len(got) != 1 {
			t.Fatalf("decoded %v, %v", got, ok)
		}
		if key, _ := fieldBytes(def, got[0], "RootPublicKey"); len(key) != 70 {
			t.Fatalf("RootPublicKey of %d bytes", len(key))
		}
		if idx, _ := fieldUint[uint8](def, got[0], "FabricIndex"); idx != 255 {
			t.Fatalf("FabricIndex %d", idx)
		}
	})

	t.Run("a missing mandatory field is left unset", func(t *testing.T) {
		t.Parallel()
		def := gencomm.BasicCommissioningInfoDef
		n := nodeOf(t, func(e *tlv.Encoder) {
			e.StartStruct(anon)
			e.PutUint(tlv.ContextTag(0), 60)
			_ = e.EndContainer()
		})
		got, ok := reportedStruct(log, "BasicCommissioningInfo", def, n)
		if !ok {
			t.Fatal("a struct without a mandatory field did not decode")
		}
		if v, has := fieldUint[uint16](def, got, "FailSafeExpiryLengthSeconds"); !has || v != 60 {
			t.Fatalf("FailSafeExpiryLengthSeconds %d, %v", v, has)
		}
		if _, has := fieldUint[uint16](def, got, "MaxCumulativeFailsafeSeconds"); has {
			t.Fatal("MaxCumulativeFailsafeSeconds set although not reported")
		}
	})

	t.Run("a value of the wrong shape is absent", func(t *testing.T) {
		t.Parallel()
		n := nodeOf(t, func(e *tlv.Encoder) { e.PutUint(anon, 7) })
		if _, ok := reportedStruct(log, "BasicCommissioningInfo", gencomm.BasicCommissioningInfoDef, n); ok {
			t.Fatal("an integer decoded as a struct")
		}
	})

	t.Run("an integer of the other signedness is taken and bounds-checked", func(t *testing.T) {
		t.Parallel()
		// matter.js under relaxNumberTypeChecks (TlvNumber.ts:124-141 at
		// b25ed960): a signed element for an unsigned field.
		if v, isNull, ok := reportedUint[uint8](log, "LocationCapability", 8, nodeOf(t, func(e *tlv.Encoder) { e.PutInt(anon, 2) })); !ok || isNull || v != 2 {
			t.Fatalf("signed 2 for a uint8: %d, null %v, ok %v", v, isNull, ok)
		}
		if _, _, ok := reportedUint[uint8](log, "LocationCapability", 8, nodeOf(t, func(e *tlv.Encoder) { e.PutInt(anon, -1) })); ok {
			t.Fatal("signed -1 for a uint8 taken")
		}
		if _, isNull, ok := reportedUint[uint8](log, "AdministratorFabricIndex", 8, nodeOf(t, func(e *tlv.Encoder) { e.PutNull(anon) })); !ok || !isNull {
			t.Fatalf("null: null %v, ok %v", isNull, ok)
		}
		// A difference, recorded: a same-signedness value above the width
		// is absent here; matter.js reads it unchecked (TlvNumber.ts:142).
		if _, _, ok := reportedUint[uint8](log, "LocationCapability", 8, nodeOf(t, func(e *tlv.Encoder) { e.PutUint(anon, 300) })); ok {
			t.Fatal("300 for a uint8 taken")
		}
		// The site: a signed AdministratorFabricIndex is the administrator's.
		tr, f := newTrust(t, &delegate{})
		r := f.report(t)
		for i := range r.Attributes {
			if a := &r.Attributes[i]; a.Path.Attribute == jfadef.AttrAdministratorFabricIndex && a.Path.Cluster == jfadef.ClusterID {
				a.Value = nodeOf(t, func(e *tlv.Encoder) { e.PutInt(anon, int64(f.index)) })
			}
		}
		if err := tr.parseAdminFabricIndexAndEndpointID(r); err != nil || tr.info.AdminFabricIndex != f.index {
			t.Fatalf("parse: %v, index %d", err, tr.info.AdminFabricIndex)
		}
	})

	t.Run("the JCM reads take a value past its constraint", func(t *testing.T) {
		t.Parallel()
		tr, f := newTrust(t, &delegate{})
		r := f.report(t)
		// The administrator fabric's Label past 32 and its NOC past 400
		// bytes: the generated decoders refused both.
		noc := append(append([]byte(nil), f.noc...), make([]byte, 401)...)
		for i := range r.Attributes {
			a := &r.Attributes[i]
			if a.Path.Cluster != opCredsCluster {
				continue
			}
			switch a.Path.Attribute {
			case opcredsdef.AttrFabrics:
				a.Value = nodeOf(t, func(e *tlv.Encoder) {
					spec.List[opcredsdef.FabricDescriptorStruct]{{RootPublicKey: f.rootPub, VendorId: f.vendorID, FabricId: f.fabricID, NodeId: f.nodeID, Label: strings.Repeat("l", 40), FabricIndex: f.index}}.EncodeTLV(e, anon)
				})
			case opcredsdef.AttrNocs:
				n := opcredsdef.NOCStruct{Noc: noc, FabricIndex: f.index}
				n.Icac.Value = f.icac
				a.Value = nodeOf(t, func(e *tlv.Encoder) { spec.List[opcredsdef.NOCStruct]{n}.EncodeTLV(e, anon) })
			}
		}
		if err := tr.parse(r); err != nil {
			t.Fatal(err)
		}
		if len(tr.info.AdminNOC) != len(noc) || tr.info.AdminVendorID != f.vendorID || tr.info.AdminFabricID != f.fabricID {
			t.Fatalf("info %+v", tr.info)
		}
	})

	t.Run("a JCM value of the wrong shape is absent, the parse refuses as for no value", func(t *testing.T) {
		t.Parallel()
		tr, f := newTrust(t, &delegate{})
		r := f.report(t)
		kept := r.Attributes[:0]
		for _, a := range r.Attributes {
			if a.Path.Cluster == opCredsCluster && a.Path.Attribute == opcredsdef.AttrFabrics {
				a.Value = nodeOf(t, func(e *tlv.Encoder) { e.PutUint(anon, 7) })
			}
			kept = append(kept, a)
		}
		r = &interaction.Report{Attributes: kept}
		err := tr.parse(r)
		if !errors.Is(err, errJCMParse) || !strings.Contains(err.Error(), "no root public key") {
			t.Fatalf("parse: %v", err)
		}
	})
}

// TestArmFailsafeTakesBasicCommissioningInfoByShape: the flow arms with a
// BasicCommissioningInfo that lacks MaxCumulativeFailsafeSeconds (the
// commissioning expiry left undefined), and refuses one whose shape does
// not decode as no BasicCommissioningInfo — matter.js dereferences the
// value it could not decode and fails there too
// (ControllerCommissioningFlow.ts:861-872 at db021da0). Review II,
// F-db021da0-15.
func TestArmFailsafeTakesBasicCommissioningInfoByShape(t *testing.T) {
	t.Parallel()
	device := func(value spec.Encodable) func(imRequest) (uint8, []byte, bool) {
		dev := &failsafeDevice{}
		return func(req imRequest) (uint8, []byte, bool) {
			if req.opcode == im.OpcodeReadRequest {
				return im.OpcodeReportData, reportOf(gencomm.ClusterID, gencomm.AttrBasicCommissioningInfo, value), true
			}
			return dev.handle(req)
		}
	}
	t.Run("without MaxCumulativeFailsafeSeconds", func(t *testing.T) {
		t.Parallel()
		f, _ := newTestFlow(t, device(encodableFunc(func(e *tlv.Encoder, tag tlv.Tag) {
			e.StartStruct(tag)
			e.PutUint(tlv.ContextTag(0), 60)
			_ = e.EndContainer()
		})))
		if _, err := f.armFailsafe(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
		if _, expiry := f.failsafeTimes(); !expiry.IsZero() {
			t.Fatalf("commissioning expiry %v, want undefined", expiry)
		}
	})
	t.Run("of the wrong shape", func(t *testing.T) {
		t.Parallel()
		f, _ := newTestFlow(t, device(encodableFunc(func(e *tlv.Encoder, tag tlv.Tag) { e.PutUint(tag, 7) })))
		_, err := f.armFailsafe(context.Background(), 0)
		if !errors.Is(err, errUnexpectedResponse) || !strings.Contains(err.Error(), "no BasicCommissioningInfo") {
			t.Fatalf("arm: %v", err)
		}
	})
}
