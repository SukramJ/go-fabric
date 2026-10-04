// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// Wire parity of the appliance clusters — OperationalState,
// RvcOperationalState and the ModeBase derivations — against the bytes
// matter.js's TlvOfModel(element) produces for them, from the same master
// as the other application clusters (testdata/application-wire-fixtures.json,
// notes/parity/matter/generate-application-fixtures.ts). Requests run the
// matter.js bytes through commandFieldsReader; responses, events and
// attribute values run through the production writers and must come out
// byte for byte.

import (
	"encoding/hex"
	"encoding/json"
	"slices"
	"testing"

	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/tlv"
)

// applianceClusters are the clusters this file checks; the generic
// application test skips them.
var applianceClusters = []uint32{
	clusterwire.OperationalStateClusterID, clusterwire.RvcOperationalStateClusterID,
	clusterwire.LaundryWasherModeClusterID, clusterwire.RvcRunModeClusterID,
	clusterwire.RvcCleanModeClusterID, clusterwire.DishwasherModeClusterID,
}

type applianceFixture struct {
	Label    string          `json:"label"`
	Kind     string          `json:"kind"`
	Cluster  uint32          `json:"cluster"`
	Element  string          `json:"element"`
	Fixture  json.RawMessage `json:"fixture"`
	BytesHex string          `json:"bytesHex"`
}

func loadApplianceFixtures(t *testing.T, clusters ...uint32) []applianceFixture {
	t.Helper()
	all := loadApplianceFixturesForFuzz(t)
	out := slices.DeleteFunc(all, func(f applianceFixture) bool { return !slices.Contains(clusters, f.Cluster) })
	if len(out) == 0 {
		t.Fatalf("no fixtures for clusters %v", clusters)
	}
	return out
}

// loadApplianceFixturesForFuzz reads every application fixture with its
// fixture value raw.
func loadApplianceFixturesForFuzz(tb testing.TB) []applianceFixture {
	tb.Helper()
	var all []applianceFixture
	if err := json.Unmarshal(applicationWireFixturesJSON, &all); err != nil {
		tb.Fatal(err)
	}
	return all
}

// encodeCommandResponse runs v through the production command-fields
// writer.
func encodeCommandResponse(t *testing.T, v any) string {
	t.Helper()
	enc := tlv.NewEncoder()
	defaultCommandFieldsWriter(enc, tlv.AnonymousTag(), v)
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// errorStateFixture is matter.js's ErrorStateStruct.
type errorStateFixture struct {
	ErrorStateID      uint8  `json:"errorStateId"`
	ErrorStateLabel   string `json:"errorStateLabel"`
	ErrorStateDetails string `json:"errorStateDetails"`
}

func (f errorStateFixture) wire() clusterwire.ErrorStateStruct {
	return clusterwire.ErrorStateStruct{ErrorStateID: f.ErrorStateID, ErrorStateLabel: f.ErrorStateLabel, ErrorStateDetails: f.ErrorStateDetails}
}

// elapsedFixture reads an optional, nullable elapsed-s field.
func elapsedFixture(t *testing.T, fields map[string]json.RawMessage, name string) *clusterwire.ElapsedS {
	t.Helper()
	raw, ok := fields[name]
	if !ok {
		return nil
	}
	if string(raw) == "null" {
		return &clusterwire.ElapsedS{Null: true}
	}
	var v uint32
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return &clusterwire.ElapsedS{Seconds: v}
}

func TestOperationalStatePayloadsMatchMatterJS(t *testing.T) {
	t.Parallel()
	for _, f := range loadApplianceFixtures(t, clusterwire.OperationalStateClusterID, clusterwire.RvcOperationalStateClusterID) {
		t.Run(f.Label, func(t *testing.T) {
			t.Parallel()
			var got any
			switch f.Kind + "/" + f.Element {
			case "commands/Pause", "commands/GoHome":
				// Fieldless requests decode without complaint.
				decodeCommandFields(t, f.Cluster, map[string]uint32{"Pause": 0x00, "GoHome": 0x80}[f.Element], f.BytesHex)
				return
			case "commands/OperationalCommandResponse":
				var fx struct {
					CommandResponseState errorStateFixture `json:"commandResponseState"`
				}
				mustUnmarshal(t, f.Fixture, &fx)
				if enc := encodeCommandResponse(t, clusterwire.OperationalCommandResponse{CommandResponseState: fx.CommandResponseState.wire()}); enc != f.BytesHex {
					t.Fatalf("encoded %s\n  matter.js %s", enc, f.BytesHex)
				}
				return
			case "events/OperationalError":
				var fx struct {
					ErrorState errorStateFixture `json:"errorState"`
				}
				mustUnmarshal(t, f.Fixture, &fx)
				got = clusterwire.OperationalErrorEvent{ErrorState: fx.ErrorState.wire()}
			case "events/OperationCompletion":
				var fields map[string]json.RawMessage
				mustUnmarshal(t, f.Fixture, &fields)
				var code uint8
				mustUnmarshal(t, fields["completionErrorCode"], &code)
				got = clusterwire.OperationCompletionEvent{
					CompletionErrorCode:  code,
					TotalOperationalTime: elapsedFixture(t, fields, "totalOperationalTime"),
					PausedTime:           elapsedFixture(t, fields, "pausedTime"),
				}
			case "attributes/OperationalStateList":
				var fx []struct {
					OperationalStateID    uint8  `json:"operationalStateId"`
					OperationalStateLabel string `json:"operationalStateLabel"`
				}
				mustUnmarshal(t, f.Fixture, &fx)
				list := []clusterwire.OperationalStateStruct{}
				for _, e := range fx {
					list = append(list, clusterwire.OperationalStateStruct{OperationalStateID: e.OperationalStateID, OperationalStateLabel: e.OperationalStateLabel})
				}
				got = list
			case "attributes/OperationalError":
				var fx errorStateFixture
				mustUnmarshal(t, f.Fixture, &fx)
				got = fx.wire()
			case "attributes/PhaseList":
				var fx []string
				mustUnmarshal(t, f.Fixture, &fx)
				got = fx
			default:
				t.Fatalf("unhandled fixture %s %s", f.Kind, f.Element)
			}
			if enc := encodeValue(t, got); enc != f.BytesHex {
				t.Fatalf("encoded %s\n  matter.js %s", enc, f.BytesHex)
			}
		})
	}
}

func mustUnmarshal(t *testing.T, raw json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("fixture: %v", err)
	}
}

func TestModeBasePayloadsMatchMatterJS(t *testing.T) {
	t.Parallel()
	for _, f := range loadApplianceFixtures(t, clusterwire.LaundryWasherModeClusterID, clusterwire.RvcRunModeClusterID,
		clusterwire.RvcCleanModeClusterID, clusterwire.DishwasherModeClusterID) {
		t.Run(f.Label, func(t *testing.T) {
			t.Parallel()
			switch f.Kind + "/" + f.Element {
			case "commands/ChangeToMode":
				var fx struct {
					NewMode uint8 `json:"newMode"`
				}
				mustUnmarshal(t, f.Fixture, &fx)
				got, ok := decodeCommandFields(t, f.Cluster, clusterwire.ModeBaseCmdChangeToMode, f.BytesHex).(clusterwire.ChangeToModeRequest)
				if !ok || got.NewMode != fx.NewMode {
					t.Fatalf("decoded %+v, want NewMode %d", got, fx.NewMode)
				}
			case "commands/ChangeToModeResponse":
				var fx struct {
					Status     uint8  `json:"status"`
					StatusText string `json:"statusText"`
				}
				mustUnmarshal(t, f.Fixture, &fx)
				if enc := encodeCommandResponse(t, clusterwire.ChangeToModeResponse{Status: fx.Status, StatusText: fx.StatusText}); enc != f.BytesHex {
					t.Fatalf("encoded %s\n  matter.js %s", enc, f.BytesHex)
				}
			case "attributes/SupportedModes":
				var fx []struct {
					Label    string `json:"label"`
					Mode     uint8  `json:"mode"`
					ModeTags []struct {
						MfgCode *uint16 `json:"mfgCode"`
						Value   uint16  `json:"value"`
					} `json:"modeTags"`
				}
				mustUnmarshal(t, f.Fixture, &fx)
				var modes []clusterwire.ModeOptionStruct
				for _, m := range fx {
					opt := clusterwire.ModeOptionStruct{Label: m.Label, Mode: m.Mode}
					for _, tg := range m.ModeTags {
						opt.ModeTags = append(opt.ModeTags, clusterwire.ModeTagStruct{MfgCode: tg.MfgCode, Value: tg.Value})
					}
					modes = append(modes, opt)
				}
				if enc := encodeValue(t, modes); enc != f.BytesHex {
					t.Fatalf("encoded %s\n  matter.js %s", enc, f.BytesHex)
				}
			default:
				t.Fatalf("unhandled fixture %s %s", f.Kind, f.Element)
			}
		})
	}
}
