// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/controller/camera"
)

// TestParseICECandidatesParityMatterjsServer ports the ice_candidates
// cases of matterjs-server packages/ws-controller/test/WebRtcProviderArgumentsTest.ts
// at 0bd06f2 (:262-323) onto camera.ParseICECandidates, the converter
// camera_provide_ice_candidates runs on its list (cameraCommands.ts:342-366):
// a list of candidate objects converts, keys matched with case and word
// separators ignored; a wrong key, a missing mandatory member, a member
// out of its struct's bounds, an empty list and a list that is not one are
// refused in the converter's wording.
func TestParseICECandidatesParityMatterjsServer(t *testing.T) {
	t.Parallel()
	accepted := []struct {
		name, json string
		want       []wpdef.ICECandidateStruct
	}{
		{
			"a candidate object in the list", `[{"candidate":"candidate:1","sdpMid":"0","sdpMLineIndex":0}]`,
			[]wpdef.ICECandidateStruct{{Candidate: "candidate:1", SdpMid: spec.ValueOf("0"), SdpmLineIndex: spec.ValueOf[uint16](0)}},
		},
		{
			"the list, keys spelled as the client spells them", `[{"candidate":"candidate:1","sdp_mid":"0","sdp_m_line_index":null},{"Candidate":"candidate:2","SDPMid":null,"sdpMLineIndex":1}]`,
			[]wpdef.ICECandidateStruct{
				{Candidate: "candidate:1", SdpMid: spec.ValueOf("0"), SdpmLineIndex: spec.Nullable[uint16]{Null: true}},
				{Candidate: "candidate:2", SdpMid: spec.Nullable[string]{Null: true}, SdpmLineIndex: spec.ValueOf[uint16](1)},
			},
		},
		// :273-282: candidate states neither end, so its length is not the
		// server's to judge.
		{
			"an empty candidate string", `[{"candidate":"","sdpMid":"0","sdpMLineIndex":0}]`,
			[]wpdef.ICECandidateStruct{{Candidate: "", SdpMid: spec.ValueOf("0"), SdpmLineIndex: spec.ValueOf[uint16](0)}},
		},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := camera.ParseICECandidates(json.RawMessage(tc.json))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseICECandidates(%s) = %+v, want %+v", tc.json, got, tc.want)
			}
		})
	}

	refused := []struct {
		name, json, want string
	}{
		{"a wrong key", `[{"candidate":"candidate:1","sdpMid":"0","sdpMLineIndex":0,"priority":5}]`, `^unknown ice_candidates\[0\] key: priority\.`},                                    // :285-294
		{"a missing mandatory member", `[{"candidate":"candidate:1"}]`, `^ice_candidates\[0\] requires sdpMid$`},                                                                        // :296-305
		{"a member past its bounds", `[{"candidate":"candidate:1","sdpMid":"0","sdpMLineIndex":65536}]`, `^ice_candidates\[0\]\.sdpMLineIndex must be an integer between 0 and 65535$`}, // :307-316
		{"a string below its floor", `[{"candidate":"candidate:1","sdpMid":"","sdpMLineIndex":0}]`, `^ice_candidates\[0\]\.sdpMid must be a string of at least 1 characters$`},          // :262-272
		{"the empty list", `[]`, `^ice_candidates must be an array of 1 or more entries$`},                                                                                              // :318-323
		{"a candidate object, not a list", `{"candidate":"candidate:1","sdpMid":"0"}`, `^ice_candidates must be an array of 1 or more entries$`},
		{"null", `null`, `^ice_candidates must be an array of 1 or more entries$`},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := camera.ParseICECandidates(json.RawMessage(tc.json))
			if err == nil {
				t.Fatalf("ParseICECandidates(%s) = %+v, want refused", tc.json, got)
			}
			if !errors.Is(err, camera.ErrInvalidArgument) {
				t.Errorf("error %v does not match ErrInvalidArgument", err)
			}
			if !regexp.MustCompile(tc.want).MatchString(err.Error()) {
				t.Errorf("error %q, want %s", err.Error(), tc.want)
			}
		})
	}
}
