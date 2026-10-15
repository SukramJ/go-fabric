// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestParityConstants: matter.js packages/protocol/src/peer/ at 0ceb202d —
// ControllerCommissioningFlow.ts:289-301, ControllerCommissioner.ts:123,
// :264, :603-604, :706 and CommissioningConnection.ts:35.
func TestParityConstants(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name      string
		got, want time.Duration
	}{
		{"DEFAULT_FAILSAFE_TIME (CF:289)", DefaultFailSafeTime, time.Minute},
		{"CASE_RECONNECT_TIMEOUT (CF:295)", CASEReconnectTimeout, 5 * time.Minute},
		{"FAILSAFE_PROCESSING_MARGIN (CF:298)", FailSafeProcessingMargin, 10 * time.Second},
		{"FAILSAFE_MAX_SEND_TIME (CF:301)", FailSafeMaxSendTime, 10 * time.Second},
		{"DEFAULT_CASE_CONNECTION_TIMEOUT (CC:123)", DefaultCASEConnectionTimeout, 255 * time.Second},
		{"PASE timeout (CC:264)", DefaultPASETimeout, 30 * time.Second},
		{"NoSharedTrustRoots retry (CC:706)", noSharedTrustRootsRetry, 15 * time.Second},
		{"DELAY_BEFORE_NEXT_ADDRESS (CN:35)", DelayBeforeNextAddress, 10 * time.Second},
	}
	for _, r := range rows {
		if r.got != r.want {
			t.Errorf("%s = %v, want %v", r.name, r.got, r.want)
		}
	}
	if DefaultRegulatoryLocation != gencomm.RegulatoryLocationTypeOutdoor || DefaultRegulatoryCountryCode != "XX" {
		t.Error("regulatory defaults differ from CC:603-604")
	}
	if rootEndpoint != 0 || attestationNonceSize != 32 || csrNonceSize != 32 {
		t.Error("RootEndpointNumber (CF:307) or the nonce sizes (CF:1247, :1394) differ")
	}
}

// TestParityStepTable: the steps of an IP channel and their order
// (CF:536-663): 0.1, 7.1, 8.1, 8.2, 10.1, 11.1, 15.1, 18.1, 20.1, 98.1 —
// no NetworkCommissioning steps (BLE only, :595-625), no OTA provider step
// (its option, :648-655); the breadcrumb starts at 1 (:327).
func TestParityStepTable(t *testing.T) {
	t.Parallel()
	f, _ := newTestFlow(t, func(imRequest) (uint8, []byte, bool) { return 0, nil, false })
	f.sortSteps()
	want := []string{
		"0.1 GetInitialData",
		"7.1 GeneralCommissioning.ArmFailsafe",
		"8.1 GeneralCommissioning.ConfigureRegulatoryInformation",
		"8.2 TimeSynchronization.SynchronizeTime",
		"10.1 OperationalCredentials.DeviceAttestation",
		"11.1 OperationalCredentials.Certificates",
		"15.1 AccessControl",
		"18.1 Reconnect",
		"20.1 GeneralCommissioning.Complete",
		"98.1 OperationalCredentials.UpdateFabricLabel",
	}
	if len(f.steps) != len(want) {
		t.Fatalf("%d steps, want %d", len(f.steps), len(want))
	}
	for i, s := range f.steps {
		if got := fmt.Sprintf("%d.%d %s", s.number, s.sub, s.name); got != want[i] {
			t.Errorf("step %d = %q, want %q", i, got, want[i])
		}
	}
	if f.lastBreadcrumb != 1 || f.defaultFailSafe != time.Minute {
		t.Error("initial breadcrumb (CF:327) or default failsafe (CF:329) differ")
	}
	for _, name := range []string{"8.2", "15.1"} {
		for _, s := range f.steps {
			if fmt.Sprintf("%d.%d", s.number, s.sub) == name {
				if r, err := s.run(context.Background()); r != stepSkipped || err != nil {
					t.Errorf("step %s: %v %v, want Skipped (CF:1199-1211, :1515-1522)", name, r, err)
				}
			}
		}
	}
}

// TestParityErrorClassification: which errors disarm the failsafe and how
// they leave the flow (CF:429-466), and the kinds of CF:223-254.
func TestParityErrorClassification(t *testing.T) {
	t.Parallel()
	status := &interaction.StatusError{Status: 1}
	rows := []struct {
		name string
		err  error
		// commission is causedBy(error, CommissioningError), statusError
		// causedBy(error, StatusResponseError).
		commission  bool
		statusError bool
	}{
		{"CommissioningError", newError(ErrCommissioning, "x", nil), true, false},
		{"DeviceAttestationError", &attestation.Error{Check: attestation.PaaNotTrusted}, true, false},
		{"wrapped CommissioningError", fmt.Errorf("w: %w", newError(ErrOperativeConnectionFailed, "x", nil)), true, false},
		{"StatusResponseError", status, false, true},
		{"CommissioningError caused by a status", newError(ErrCommissioning, "x", status), true, true},
		{"PairRetransmissionLimitReachedError", &PairRetransmissionLimitReachedError{Message: "x"}, false, false},
		{"exchange error", exchange.ErrTransient, false, false},
		{"unexpected data", errUnexpectedResponse, false, false},
	}
	for _, r := range rows {
		if isCommissioningError(r.err) != r.commission || isStatusError(r.err) != r.statusError {
			t.Errorf("%s: commissioning %v status %v", r.name, isCommissioningError(r.err), isStatusError(r.err))
		}
	}
	for _, kind := range []error{
		ErrMaximumCommissionedFabricsReached, ErrCommissioningTimeout, ErrDeviceAlreadyCommissionedToThisFabric,
		ErrFabricLabelConflict, ErrNodeIDConflict, ErrOperativeConnectionFailed, errRecoverable,
	} {
		e := newError(kind, "m", nil)
		if !errors.Is(e, kind) || !errors.Is(e, ErrCommissioning) {
			t.Errorf("%v does not match its kind and ErrCommissioning", kind)
		}
	}
	if (&PeerCommunicationError{Message: "m"}).Error() == "" || (&CommissioningTransitionError{Message: "m"}).Error() == "" {
		t.Error("error text")
	}
}

// TestParityOperationalCredentialsResults: #ensureOperationalCredentialsSuccess
// (CF:674-709) and #ensureGeneralCommissioningSuccess (:712-719).
func TestParityOperationalCredentialsResults(t *testing.T) {
	t.Parallel()
	f, _ := newTestFlow(t, func(imRequest) (uint8, []byte, bool) { return 0, nil, false })
	idx, debug := uint8(3), "dbg"
	rows := []struct {
		context string
		status  uint8
		kind    error
		msg     string
	}{
		{"addNoc", nocStatusOk, nil, ""},
		{"addNoc", nocStatusFabricConflict, ErrDeviceAlreadyCommissionedToThisFabric, "Commission error: This device is already commissioned into this fabric. You cannot commission it again."},
		{"addNoc", nocStatusTableFull, ErrMaximumCommissionedFabricsReached, "Commission error: This device reached the maximum number of fabrics it can be part of. Please remove a fabric before trying to add another one."},
		{"updateFabricLabel", nocStatusLabelConflict, ErrFabricLabelConflict, `Cannot set fabric label to "test" because another fabric on this device already uses this name. Please adjust fabric labels to be unique.`},
		{"updateFabricLabel", nocStatusFabricConflict, ErrCommissioning, `Commission error for "updateFabricLabel": FabricConflict (9), dbg, fabricIndex: 3`},
		{"addNoc", 3, ErrCommissioning, `Commission error for "addNoc": InvalidNoc (3), dbg, fabricIndex: 3`},
	}
	for _, r := range rows {
		err := f.ensureOperationalCredentialsSuccess(r.context, nocResponse{status: r.status, fabricIndex: &idx, debugText: &debug})
		if r.kind == nil {
			if err != nil {
				t.Errorf("%s %d: %v", r.context, r.status, err)
			}
			continue
		}
		var ce *CommissioningError
		if !errors.As(err, &ce) || ce.Kind != r.kind || ce.Message != r.msg { //nolint:errorlint // the identity of the kind
			t.Errorf("%s %d: %v, want %v %q", r.context, r.status, err, r.kind, r.msg)
		}
	}
	err := f.ensureGeneralCommissioningSuccess("setRegulatoryConfig", gencomm.CommissioningErrorValueOutsideRange, "too far")
	if err == nil || err.Error() != `commissioner: Commission error for "setRegulatoryConfig": ValueOutsideRange (1), too far` {
		t.Errorf("general commissioning: %v", err)
	}
	if enumName(gencomm.CommissioningErrorEnumDef, 99) != "undefined" {
		t.Error("an unknown enum value")
	}
}

// TestParityAddNOCFields: AddNoc's fields (CF:1441-1457) — the NOC, the
// ICAC or an empty value, the raw IPK, the root node id as
// CaseAdminSubject, the root vendor id as AdminVendorId.
func TestParityAddNOCFields(t *testing.T) {
	t.Parallel()
	e := tlv.NewEncoder()
	addNOCRequest{noc: []byte{1}, ipk: make([]byte, 16), caseAdminSubject: 112233, adminVendorID: 0xFFF1}.EncodeTLV(e, tlv.AnonymousTag())
	b, _ := e.Bytes()
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		t.Fatal(err)
	}
	var noc, icac, ipk []byte
	var subject uint64
	var vendor uint16
	err = spec.DecodeFields(n, "AddNoc",
		spec.Bind(0, "NocValue", true, &noc, spec.DecodeBytes),
		spec.Bind(1, "IcacValue", true, &icac, spec.DecodeBytes),
		spec.Bind(2, "IpkValue", true, &ipk, spec.DecodeBytesIn(16, 16)),
		spec.Bind(3, "CaseAdminSubject", true, &subject, spec.DecodeUint[uint64](64)),
		spec.Bind(4, "AdminVendorId", true, &vendor, spec.DecodeUint[uint16](16)))
	if err != nil || len(icac) != 0 || subject != 112233 || vendor != 0xFFF1 {
		t.Fatalf("AddNoc %v: icac %x subject %d vendor %x", err, icac, subject, vendor)
	}
	addr := (&Fabric{FabricIndex: 4}).AddressOf(9)
	if addr != (handshake.PeerAddress{FabricIndex: 4, NodeID: 9}) {
		t.Fatalf("AddressOf %+v", addr)
	}
	if !isOperationalNodeID(1) || isOperationalNodeID(0) || !isOperationalNodeID(0xFFFF_FFEF_FFFF_FFFF) || isOperationalNodeID(0xFFFF_FFF0_0000_0000) {
		t.Fatal("the operational node id range differs from NodeId.ts:27-28")
	}
}
