// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	netcomm "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// uintValue encodes an unsigned attribute value.
type uintValue uint64

func (v uintValue) EncodeTLV(e *tlv.Encoder, tag tlv.Tag) { e.PutUint(tag, uint64(v)) }

// regulatoryDevice answers the regulatory step: LocationCapability and
// SetRegulatoryConfig, the first of which is refused for a country other
// than "XX".
type regulatoryDevice struct {
	mu   sync.Mutex
	sent []gencomm.SetRegulatoryConfigRequest
}

func (d *regulatoryDevice) handle(req imRequest) (opcode uint8, payload []byte, ok bool) {
	switch req.opcode {
	case im.OpcodeReadRequest:
		return im.OpcodeReportData, reportOf(gencomm.ClusterID, gencomm.AttrLocationCapability, uintValue(gencomm.RegulatoryLocationTypeIndoorOutdoor)), true
	case im.OpcodeInvokeRequest:
		path, fields, err := decodeInvoke(req.payload)
		if err != nil {
			return 0, nil, false
		}
		var r gencomm.SetRegulatoryConfigRequest
		_ = r.DecodeTLV(fields)
		d.mu.Lock()
		d.sent = append(d.sent, r)
		d.mu.Unlock()
		code := gencomm.CommissioningErrorOk
		if r.CountryCode != "XX" {
			code = gencomm.CommissioningErrorValueOutsideRange
		}
		path.Command = gencomm.CmdSetRegulatoryConfigResponse
		return im.OpcodeInvokeResponse, invokeResponseOf(path, gencomm.SetRegulatoryConfigResponse{ErrorCode: code}), true
	}
	return 0, nil, false
}

// TestConfigureRegulatoryInformation: only with a Wi-Fi or Thread
// interface (CF:1109-1112); IndoorOutdoor takes the option (:1125-1126);
// the breadcrumb increments after the first SetRegulatoryConfig (:1141);
// ValueOutsideRange retries once with "XX" (:1150-1175).
func TestConfigureRegulatoryInformation(t *testing.T) {
	t.Parallel()
	dev := &regulatoryDevice{}
	f, _ := newTestFlow(t, dev.handle)
	ctx := context.Background()
	if _, err := f.configureRegulatoryInformation(ctx); !errors.Is(err, ErrCommissioning) {
		t.Fatalf("without network features: %v", err)
	}
	f.data.networkFeatures = []networkFeature{{endpoint: 0, featureMap: uint32(netcomm.FeatureEthernetNetworkInterface)}}
	if r, err := f.configureRegulatoryInformation(ctx); r != stepSkipped || err != nil {
		t.Fatalf("Ethernet only: %v %v", r, err)
	}
	f.data.networkFeatures = append(f.data.networkFeatures, networkFeature{endpoint: 1, featureMap: uint32(netcomm.FeatureWiFiNetworkInterface)})
	indoor := gencomm.RegulatoryLocationTypeIndoor
	f.opts.RegulatoryLocation, f.opts.RegulatoryCountryCode = &indoor, "DE"
	if r, err := f.configureRegulatoryInformation(ctx); r != stepSuccess || err != nil {
		t.Fatalf("Wi-Fi: %v %v", r, err)
	}
	dev.mu.Lock()
	defer dev.mu.Unlock()
	if len(dev.sent) != 2 || dev.sent[0].CountryCode != "DE" || dev.sent[1].CountryCode != "XX" ||
		dev.sent[0].Breadcrumb != 1 || dev.sent[1].Breadcrumb != 2 || dev.sent[0].NewRegulatoryConfig != indoor || f.lastBreadcrumb != 2 {
		t.Fatalf("SetRegulatoryConfig %+v, breadcrumb %d", dev.sent, f.lastBreadcrumb)
	}
}

// nocDevice answers every invoke with a NocResponse of status.
type nocDevice struct{ status uint8 }

type nocResponseValue struct{ status uint8 }

func (v nocResponseValue) EncodeTLV(e *tlv.Encoder, tag tlv.Tag) {
	e.StartStruct(tag)
	e.PutUint(tlv.ContextTag(0), uint64(v.status))
	_ = e.EndContainer()
}

func (d *nocDevice) handle(req imRequest) (opcode uint8, payload []byte, ok bool) {
	if req.opcode != im.OpcodeInvokeRequest {
		return 0, nil, false
	}
	path, _, err := decodeInvoke(req.payload)
	if err != nil {
		return 0, nil, false
	}
	path.Command = 0x08
	return im.OpcodeInvokeResponse, invokeResponseOf(path, nocResponseValue{d.status}), true
}

// TestUpdateFabricLabel: without a fabric index the step fails (CF:1479-1485);
// a LabelConflict is a recoverable error around FabricLabelConflictError
// (:1498-1500, :696-702).
func TestUpdateFabricLabel(t *testing.T) {
	t.Parallel()
	f, _ := newTestFlow(t, (&nocDevice{status: nocStatusLabelConflict}).handle)
	ctx := context.Background()
	if r, err := f.updateFabricLabel(ctx); r != stepFailure || err != nil {
		t.Fatalf("no fabric index: %v %v", r, err)
	}
	f.data.fabricIndex = 2
	_, err := f.updateFabricLabel(ctx)
	var ce *CommissioningError
	if !errors.As(err, &ce) || ce.Kind != errRecoverable || !errors.Is(err, ErrFabricLabelConflict) { //nolint:errorlint // the identity of the kind
		t.Fatalf("label conflict: %v", err)
	}
	f2, _ := newTestFlow(t, (&nocDevice{status: nocStatusOk}).handle)
	f2.data.fabricIndex = 2
	if r, err := f2.updateFabricLabel(ctx); r != stepSuccess || err != nil {
		t.Fatalf("label set: %v %v", r, err)
	}
}

// fabricsDevice reports a full fabric table.
func fabricsDevice(req imRequest) (opcode uint8, payload []byte, ok bool) {
	if req.opcode != im.OpcodeReadRequest {
		return 0, nil, false
	}
	return im.OpcodeReportData, encodeTLV(func(e *tlv.Encoder) {
		e.StartStruct(tlv.AnonymousTag())
		e.StartArray(tlv.ContextTag(1))
		for _, a := range []uint32{opCredsAttrSupportedFabrics, opCredsAttrCommissionedFabrics} {
			e.StartStruct(tlv.AnonymousTag())
			e.StartStruct(tlv.ContextTag(1))
			e.PutUint(tlv.ContextTag(0), 1)
			im.ConcreteAttributePath{Cluster: opCredsCluster, Attribute: a, HasEndpoint: true, HasCluster: true, HasAttribute: true}.MarshalTLV(e, tlv.ContextTag(1))
			e.PutUint(tlv.ContextTag(2), 5)
			_ = e.EndContainer()
			_ = e.EndContainer()
		}
		_ = e.EndContainer()
		_ = e.EndContainer()
	}), true
}

// TestInitialDataFullFabricTable: commissionedFabrics >= supportedFabrics
// is MaximumCommissionedFabricsReachedError (CF:759-763).
func TestInitialDataFullFabricTable(t *testing.T) {
	t.Parallel()
	f, _ := newTestFlow(t, fabricsDevice)
	_, err := f.getInitialData(context.Background())
	if !errors.Is(err, ErrMaximumCommissionedFabricsReached) {
		t.Fatalf("full table: %v", err)
	}
	var ce *CommissioningError
	if !errors.As(err, &ce) || ce.Message != "Commissioned fabrics (5) exceed supported fabrics (5). Please remove some fabrics before commissioning." {
		t.Fatalf("message %q", ce.Message)
	}
	if err := response(interactionResultWithout(), "X", func(spec.Node) error { return nil }); !errors.Is(err, errUnexpectedResponse) {
		t.Fatalf("a response without fields: %v", err)
	}
}

func interactionResultWithout() interaction.Result { return interaction.Result{} }
