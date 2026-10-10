// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package wire_test

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	dwmdef "github.com/SukramJ/go-fabric/cluster/spec/dishwashermode"
	lwmdef "github.com/SukramJ/go-fabric/cluster/spec/laundrywashermode"
	pumpdef "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol"
	rcmdef "github.com/SukramJ/go-fabric/cluster/spec/rvccleanmode"
	rvcdef "github.com/SukramJ/go-fabric/cluster/spec/rvcoperationalstate"
	rrmdef "github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	scadef "github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/tlv"
)

func encodeBytes(t *testing.T, v spec.Encodable) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	v.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		t.Fatalf("encode %T: %v", v, err)
	}
	return b
}

func agree(t *testing.T, name string, want spec.Encodable, others ...spec.Encodable) {
	t.Helper()
	w := encodeBytes(t, want)
	for _, o := range others {
		if got := encodeBytes(t, o); !bytes.Equal(got, w) {
			t.Errorf("%s: %T encodes %x, %T encodes %x", name, want, w, o, got)
		}
	}
}

func ptr[T any](v T) *T { return &v }

// TestOperationalStateCodecsAgreeWithRvc holds the claim the cluster/wire
// OperationalState types rest on: they convert to the generated
// OperationalState codecs, and RvcOperationalState's generated codecs
// write the same bytes, so one conversion serves both derivations.
func TestOperationalStateCodecsAgreeWithRvc(t *testing.T) {
	t.Parallel()
	for _, e := range []wire.ErrorStateStruct{
		{ErrorStateID: 0},
		{ErrorStateID: 0x41, ErrorStateDetails: "stuck"},
		{ErrorStateID: 0x80, ErrorStateLabel: "mfg", ErrorStateDetails: "details"},
	} {
		rvc := rvcdef.ErrorStateStruct{ErrorStateId: rvcdef.ErrorStateEnum(e.ErrorStateID)}
		if wire.HasOperationalStateLabel(e.ErrorStateID) {
			rvc.ErrorStateLabel = ptr(e.ErrorStateLabel)
		}
		if e.ErrorStateDetails != "" {
			rvc.ErrorStateDetails = ptr(e.ErrorStateDetails)
		}
		agree(t, "ErrorStateStruct", e, rvc)
		agree(t, "OperationalCommandResponse", wire.OperationalCommandResponse{CommandResponseState: e},
			rvcdef.OperationalCommandResponse{CommandResponseState: rvc})
		agree(t, "OperationalErrorEvent", wire.OperationalErrorEvent{ErrorState: e},
			rvcdef.OperationalErrorEvent{ErrorState: rvc})
	}
	agree(t, "OperationalStateList",
		wire.OperationalStateList{{OperationalStateID: 0x40}, {OperationalStateID: 0x81, OperationalStateLabel: "mfg"}},
		spec.List[rvcdef.OperationalStateStruct]{{OperationalStateId: 0x40}, {OperationalStateId: 0x81, OperationalStateLabel: ptr("mfg")}})
	agree(t, "OperationCompletionEvent",
		wire.OperationCompletionEvent{CompletionErrorCode: 1, TotalOperationalTime: &wire.ElapsedS{Seconds: 300}, PausedTime: &wire.ElapsedS{Null: true}},
		rvcdef.OperationCompletionEvent{CompletionErrorCode: 1, TotalOperationalTime: &spec.Nullable[uint32]{Value: 300}, PausedTime: &spec.Nullable[uint32]{Null: true}})
}

// TestModeBaseCodecsAgreeAcrossDerivations holds the claim the
// cluster/wire ModeBase types rest on: they convert to LaundryWasherMode's
// generated codecs, and the other three derivations' generated codecs
// write the same bytes.
func TestModeBaseCodecsAgreeAcrossDerivations(t *testing.T) {
	t.Parallel()
	modes := wire.ModeOptionList{
		{Label: "Normal", Mode: 0, ModeTags: []wire.ModeTagStruct{{Value: 0x4000}}},
		{Label: "Vendor", Mode: 7, ModeTags: []wire.ModeTagStruct{{MfgCode: ptr(uint16(0xFFF1)), Value: 0x8000}, {Value: 1}}},
		{Label: "Bare", Mode: 9},
	}
	agree(t, "SupportedModes", modes,
		spec.List[lwmdef.ModeOptionStruct]{
			{Label: "Normal", Mode: 0, ModeTags: []lwmdef.ModeTagStruct{{Value: 0x4000}}},
			{Label: "Vendor", Mode: 7, ModeTags: []lwmdef.ModeTagStruct{{MfgCode: ptr(uint16(0xFFF1)), Value: 0x8000}, {Value: 1}}},
			{Label: "Bare", Mode: 9, ModeTags: []lwmdef.ModeTagStruct{}},
		},
		spec.List[rrmdef.ModeOptionStruct]{
			{Label: "Normal", Mode: 0, ModeTags: []rrmdef.ModeTagStruct{{Value: 0x4000}}},
			{Label: "Vendor", Mode: 7, ModeTags: []rrmdef.ModeTagStruct{{MfgCode: ptr(uint16(0xFFF1)), Value: 0x8000}, {Value: 1}}},
			{Label: "Bare", Mode: 9},
		},
		spec.List[rcmdef.ModeOptionStruct]{
			{Label: "Normal", Mode: 0, ModeTags: []rcmdef.ModeTagStruct{{Value: 0x4000}}},
			{Label: "Vendor", Mode: 7, ModeTags: []rcmdef.ModeTagStruct{{MfgCode: ptr(uint16(0xFFF1)), Value: 0x8000}, {Value: 1}}},
			{Label: "Bare", Mode: 9},
		},
		spec.List[dwmdef.ModeOptionStruct]{
			{Label: "Normal", Mode: 0, ModeTags: []dwmdef.ModeTagStruct{{Value: 0x4000}}},
			{Label: "Vendor", Mode: 7, ModeTags: []dwmdef.ModeTagStruct{{MfgCode: ptr(uint16(0xFFF1)), Value: 0x8000}, {Value: 1}}},
			{Label: "Bare", Mode: 9},
		})
	for _, r := range []wire.ChangeToModeResponse{{Status: 0}, {Status: 3, StatusText: "busy"}} {
		agree(t, "ChangeToModeResponse", r,
			lwmdef.ChangeToModeResponse{Status: lwmdef.ModeChangeStatus(r.Status), StatusText: r.StatusText},
			rrmdef.ChangeToModeResponse{Status: rrmdef.ModeChangeStatus(r.Status), StatusText: r.StatusText},
			rcmdef.ChangeToModeResponse{Status: rcmdef.ModeChangeStatus(r.Status), StatusText: r.StatusText},
			dwmdef.ChangeToModeResponse{Status: dwmdef.ModeChangeStatus(r.Status), StatusText: r.StatusText})
	}
}

// TestFieldlessEventMatchesGeneratedFieldlessEvents: FieldlessEvent
// encodes as the generated payloads of the fieldless events it stands for
// do.
func TestFieldlessEventMatchesGeneratedFieldlessEvents(t *testing.T) {
	t.Parallel()
	agree(t, "FieldlessEvent", wire.FieldlessEvent{},
		scadef.HardwareFaultEvent{}, scadef.AllClearEvent{}, pumpdef.DryRunningEvent{}, pumpdef.PumpBlockedEvent{})
}

// TestClosureValuesKeepTheirBytes pins the ClosureControl attribute values
// to the bytes the bridge's hand-written encoder wrote before they moved
// onto the generated codecs (measured on that encoder, anonymous tag).
func TestClosureValuesKeepTheirBytes(t *testing.T) {
	t.Parallel()
	cur := func(v wire.ClosureCurrentPosition) *wire.ClosureCurrentPosition { return &v }
	tgt := func(v wire.ClosureTargetPosition) *wire.ClosureTargetPosition { return &v }
	for _, c := range []struct {
		v    spec.Encodable
		want string
	}{
		{(*wire.ClosureOverallCurrentState)(nil), "14"},
		{&wire.ClosureOverallCurrentState{}, "153400340318"},
		{&wire.ClosureOverallCurrentState{Position: cur(0)}, "15240000340318"},
		{&wire.ClosureOverallCurrentState{Position: cur(5), SecureState: ptr(false)}, "15240005280318"},
		{&wire.ClosureOverallCurrentState{SecureState: ptr(true)}, "153400290318"},
		{&wire.ClosureOverallCurrentState{Position: cur(255), SecureState: ptr(true)}, "152400ff290318"},
		{(*wire.ClosureOverallTargetState)(nil), "14"},
		{&wire.ClosureOverallTargetState{}, "15340018"},
		{&wire.ClosureOverallTargetState{Position: tgt(0)}, "1524000018"},
		{&wire.ClosureOverallTargetState{Position: tgt(4)}, "1524000418"},
		{&wire.ClosureOverallTargetState{Position: tgt(255)}, "152400ff18"},
		{wire.ClosureErrorList{}, "1618"},
		{wire.ClosureErrorList{0}, "16040018"},
		{wire.ClosureErrorList{0, 1, 0xff}, "160400040104ff18"},
	} {
		if got := hex.EncodeToString(encodeBytes(t, c.v)); got != c.want {
			t.Errorf("%#v = %s, want %s", c.v, got, c.want)
		}
	}
}
