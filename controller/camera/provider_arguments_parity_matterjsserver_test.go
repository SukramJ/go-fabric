// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
)

// The cases of matterjs-server's
// packages/ws-controller/test/WebRtcProviderArgumentsTest.ts:42-463 at
// 0bd06f2, ported by meaning: a payload is the JSON text a client sends, a
// logged warning is a notice of the result, and the fields are the
// generated request the command decodes into, its originating endpoint set
// by request as the session layer sets it.

// requestorEndpoint is the controller's requestor endpoint the tests inject.
const requestorEndpoint = uint16(1)

// documentedIceServers is the ice_servers shape the wire reference
// documents and the offer event emits; clusterIceServers the same servers
// as ICEServerStruct (WebRtcProviderArgumentsTest.ts:15-25, matterjs-server
// at 0bd06f2).
const documentedIceServers = `[{"urls":"stun:stun.example:3478"},{"urls":["turn:turn.example:3478","turns:turn.example:5349"],"username":"u","credential":"p","caid":7}]`

func ptr[T any](v T) *T { return &v }

var clusterIceServers = []wpdef.ICEServerStruct{
	{Urls: []string{"stun:stun.example:3478"}},
	{Urls: []string{"turn:turn.example:3478", "turns:turn.example:5349"}, Username: ptr("u"), Credential: ptr("p"), Caid: ptr(uint16(7))},
}

// refusalText is the text of the argumentError run refuses with.
func refusalText(t *testing.T, err error) string {
	t.Helper()
	var ae *argumentError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v, want an argument refusal", err)
	}
	return ae.msg
}

// providerRequest converts payload and builds the command's request.
func providerRequest(t *testing.T, name providerCommandName, payload string) spec.Encodable {
	t.Helper()
	fields, err := toProviderCommandFields(name, json.RawMessage(payload), "", "")
	if err != nil {
		t.Fatalf("%s %s: %v", name, payload, err)
	}
	req, err := fields.request(requestorEndpoint)
	if err != nil {
		t.Fatalf("%s %s: request: %v", name, payload, err)
	}
	return req
}

// providerRefusal is the text toProviderCommandFields refuses payload with.
func providerRefusal(t *testing.T, name providerCommandName, payload string) string {
	t.Helper()
	_, err := toProviderCommandFields(name, json.RawMessage(payload), "", "")
	return refusalText(t, err)
}

// providerNotices are the notices a conversion gave, as the test's log
// capture collects the boundary's warnings.
func providerNotices(t *testing.T, name providerCommandName, payload, target string) []string {
	t.Helper()
	fields, err := toProviderCommandFields(name, json.RawMessage(payload), target, "")
	if err != nil {
		t.Fatalf("%s %s: %v", name, payload, err)
	}
	return fields.notices
}

// TestProviderArgumentsBothRoutesParityMatterjsServer is describe("one
// boundary for both routes") (WebRtcProviderArgumentsTest.ts:43-96,
// matterjs-server at 0bd06f2). The managed route's ICE servers go through
// toIceServers, which camera_start_stream's argument parser
// (cameraCommands.ts, wave C) calls; the cases hold the two converters to
// one result and one refusal.
func TestProviderArgumentsBothRoutesParityMatterjsServer(t *testing.T) {
	t.Run("puts the documented ice_servers on the provider command from either route", func(t *testing.T) {
		managed, err := toIceServers(json.RawMessage(documentedIceServers), "ice_servers")
		if err != nil {
			t.Fatal(err)
		}
		raw := providerRequest(t, cmdProvideOffer, `{"webRtcSessionId":null,"sdp":"v=0","stream_usage":3,"ice_servers":`+documentedIceServers+`,"ice_transport_policy":"relay"}`).(wpdef.ProvideOfferRequest)
		if !reflect.DeepEqual(managed, clusterIceServers) {
			t.Errorf("managed = %+v, want %+v", managed, clusterIceServers)
		}
		if raw.IceServers == nil || !reflect.DeepEqual(*raw.IceServers, clusterIceServers) {
			t.Errorf("raw iceServers = %+v, want %+v", raw.IceServers, clusterIceServers)
		}
		if raw.IceTransportPolicy == nil || *raw.IceTransportPolicy != "relay" {
			t.Errorf("raw iceTransportPolicy = %v, want relay", raw.IceTransportPolicy)
		}
	})
	t.Run("refuses the same malformed ice_servers entry on either route", func(t *testing.T) {
		entry := `[{"urls":"stun:a.example","turnServer":true}]`
		_, err := toIceServers(json.RawMessage(entry), "ice_servers")
		managed := refusalText(t, err)
		raw := providerRefusal(t, cmdProvideOffer, `{"webRtcSessionId":null,"sdp":"v=0","ice_servers":`+entry+`}`)
		if raw != managed {
			t.Errorf("raw refusal %q, managed %q", raw, managed)
		}
	})
}

// TestProviderArgumentsGatedFieldsParityMatterjsServer is describe("fields
// a re-offer states past their gate") (WebRtcProviderArgumentsTest.ts:98-207,
// matterjs-server at 0bd06f2).
func TestProviderArgumentsGatedFieldsParityMatterjsServer(t *testing.T) {
	t.Run("forwards a re-offer's stream lists as stated and says so", func(t *testing.T) {
		payload := `{"webRtcSessionId":7,"sdp":"v=0","videoStreams":[1],"audioStreams":[2]}`
		got := providerRequest(t, cmdProvideOffer, payload)
		want := wpdef.ProvideOfferRequest{WebRtcSessionId: spec.ValueOf[uint16](7), Sdp: "v=0", OriginatingEndpointId: ptr(requestorEndpoint), VideoStreams: &[]uint16{1}, AudioStreams: &[]uint16{2}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("request = %+v, want %+v", got, want)
		}
		notices := providerNotices(t, cmdProvideOffer, payload, "")
		if len(notices) != 1 {
			t.Fatalf("notices = %q, want one", notices)
		}
		for _, part := range []string{"ProvideOffer", "webRtcSessionId is 7", "videoStreams, audioStreams"} {
			if !strings.Contains(notices[0], part) {
				t.Errorf("notice %q lacks %q", notices[0], part)
			}
		}
	})
	// The gated set comes from each field's conformance, not from a
	// hand-written list.
	t.Run("reports streamUsage on a re-offer, which no list of stream fields would carry", func(t *testing.T) {
		notices := providerNotices(t, cmdProvideOffer, `{"webRtcSessionId":7,"sdp":"v=0","streamUsage":4}`, "")
		if len(notices) != 1 || !strings.Contains(notices[0], "states streamUsage while webRtcSessionId is 7") {
			t.Errorf("notices = %q", notices)
		}
	})
	// The one gated field whose conformance has no fallback clause; it is
	// still forwarded, not refused.
	t.Run("reports metadataEnabled on a re-offer", func(t *testing.T) {
		payload := `{"webRtcSessionId":7,"sdp":"v=0","metadataEnabled":true}`
		got := providerRequest(t, cmdProvideOffer, payload)
		want := wpdef.ProvideOfferRequest{WebRtcSessionId: spec.ValueOf[uint16](7), Sdp: "v=0", OriginatingEndpointId: ptr(requestorEndpoint), MetadataEnabled: ptr(true)}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("request = %+v, want %+v", got, want)
		}
		notices := providerNotices(t, cmdProvideOffer, payload, "")
		if len(notices) != 1 || !strings.Contains(notices[0], "states metadataEnabled while webRtcSessionId is 7") {
			t.Errorf("notices = %q", notices)
		}
	})
	// videoStreamId is "O, D" with no condition, so it shows the set is
	// not just "every optional field".
	t.Run("says nothing for a field whose conformance names no condition", func(t *testing.T) {
		if n := providerNotices(t, cmdProvideOffer, `{"webRtcSessionId":7,"sdp":"v=0","videoStreamId":3}`, ""); len(n) != 0 {
			t.Errorf("notices = %q", n)
		}
	})
	t.Run("names the node and endpoint it was given", func(t *testing.T) {
		n := providerNotices(t, cmdProvideOffer, `{"webRtcSessionId":7,"sdp":"v=0","videoStreams":[1]}`, "node 0x0000000000000042 endpoint 1")
		if len(n) == 0 || !strings.Contains(n[0], "ProvideOffer for node 0x0000000000000042 endpoint 1 states videoStreams") {
			t.Errorf("notices = %q", n)
		}
	})
	t.Run("says nothing for a first offer, whose webRtcSessionId is null", func(t *testing.T) {
		if n := providerNotices(t, cmdProvideOffer, `{"webRtcSessionId":null,"sdp":"v=0","streamUsage":4,"videoStreams":[1]}`, ""); len(n) != 0 {
			t.Errorf("notices = %q", n)
		}
	})
	// SolicitOffer states no WebRTCSessionID at all, so nothing there is
	// gated on one.
	t.Run("says nothing for SolicitOffer", func(t *testing.T) {
		if n := providerNotices(t, cmdSolicitOffer, `{"streamUsage":4,"videoStreams":[1]}`, ""); len(n) != 0 {
			t.Errorf("notices = %q", n)
		}
	})
}

// TestProviderArgumentsFieldsParityMatterjsServer is
// describe("toProviderCommandFields") (WebRtcProviderArgumentsTest.ts:209-462,
// matterjs-server at 0bd06f2).
func TestProviderArgumentsFieldsParityMatterjsServer(t *testing.T) {
	t.Run("accepts every field the command states, and no other", func(t *testing.T) {
		expected := map[providerCommandName][]string{
			cmdProvideOffer:         {"webRtcSessionId", "sdp", "streamUsage", "videoStreamId", "audioStreamId", "iceServers", "iceTransportPolicy", "metadataEnabled", "videoStreams", "audioStreams"},
			cmdSolicitOffer:         {"streamUsage", "videoStreamId", "audioStreamId", "iceServers", "iceTransportPolicy", "metadataEnabled", "videoStreams", "audioStreams"},
			cmdProvideAnswer:        {"webRtcSessionId", "sdp"},
			cmdProvideIceCandidates: {"webRtcSessionId", "iceCandidates"},
		}
		for _, name := range providerCommandNames {
			msg := providerRefusal(t, name, `{"notAField":1}`)
			accepted := strings.Split(msg[strings.Index(msg, "Accepted: ")+len("Accepted: "):], ", ")
			if !slices.Equal(accepted, expected[name]) {
				t.Errorf("%s accepts %q, want %q", name, accepted, expected[name])
			}
			// The spelled list is what the boundary owes a client; the
			// model is what it must match.
			var model []string
			for i := range requestFields(name) {
				if p := propertyName(&requestFields(name)[i]); p != "originatingEndpointId" {
					model = append(model, p)
				}
			}
			if !slices.Equal(accepted, model) {
				t.Errorf("%s accepts %q, the model states %q", name, accepted, model)
			}
		}
	})
	// camera_start_stream answers webrtc_session_id, so a client echoes it
	// back in that spelling.
	t.Run("resolves this API's own snake spelling of a field to it", func(t *testing.T) {
		got := providerRequest(t, cmdProvideIceCandidates, `{"webrtc_session_id":12,"ice_candidates":[{"candidate":"candidate:1","sdpMid":"0","sdpMLineIndex":0}]}`).(wpdef.ProvideIceCandidatesRequest)
		if got.WebRtcSessionId != 12 {
			t.Errorf("webRtcSessionId = %d, want 12", got.WebRtcSessionId)
		}
	})
	// Dropped where the controller overwrites it; refused where the
	// command has no such field.
	t.Run("refuses originatingEndpointId on a command that states no such field", func(t *testing.T) {
		msg := providerRefusal(t, cmdProvideIceCandidates, `{"webRtcSessionId":1,"ice_candidates":[{"candidate":"candidate:1","sdpMid":"0","sdpMLineIndex":0}],"originatingEndpointId":99}`)
		if !regexp.MustCompile(`^unknown ProvideIceCandidates payload key: originatingEndpointId\.`).MatchString(msg) {
			t.Errorf("refusal %q", msg)
		}
	})
	t.Run("holds a string to the floor its field states, where that is all it states", func(t *testing.T) {
		// SdpMid is "min 1" with no ceiling.
		if msg := providerRefusal(t, cmdProvideIceCandidates, `{"webRtcSessionId":1,"ice_candidates":[{"candidate":"candidate:1","sdpMid":"","sdpMLineIndex":0}]}`); msg != "ice_candidates[0].sdpMid must be a string of at least 1 characters" {
			t.Errorf("refusal %q", msg)
		}
		// Candidate states neither end, so its length is not the
		// controller's to judge.
		got := providerRequest(t, cmdProvideIceCandidates, `{"webRtcSessionId":1,"ice_candidates":[{"candidate":"","sdpMid":"0","sdpMLineIndex":0}]}`)
		want := wpdef.ProvideIceCandidatesRequest{WebRtcSessionId: 1, IceCandidates: []wpdef.ICECandidateStruct{{Candidate: "", SdpMid: spec.ValueOf("0"), SdpmLineIndex: spec.ValueOf[uint16](0)}}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("request = %+v, want %+v", got, want)
		}
	})
	refusals := []struct {
		name    string
		command providerCommandName
		payload string
		want    string
		pattern bool
	}{
		{"refuses an unknown key inside an ice_candidates entry", cmdProvideIceCandidates, `{"webRtcSessionId":1,"ice_candidates":[{"candidate":"candidate:1","sdpMid":"0","sdpMLineIndex":0,"priority":5}]}`, `^unknown ice_candidates\[0\] key: priority\.`, true},
		{"requires the members ICECandidateStruct states as mandatory", cmdProvideIceCandidates, `{"webRtcSessionId":1,"ice_candidates":[{"candidate":"candidate:1"}]}`, "ice_candidates[0] requires sdpMid", false},
		{"bounds an ice_candidates entry's members by the struct's own definition", cmdProvideIceCandidates, `{"webRtcSessionId":1,"ice_candidates":[{"candidate":"candidate:1","sdpMid":"0","sdpMLineIndex":65536}]}`, "ice_candidates[0].sdpMLineIndex must be an integer between 0 and 65535", false},
		{"refuses the empty ice_candidates list the field's minimum forbids", cmdProvideIceCandidates, `{"webRtcSessionId":1,"ice_candidates":[]}`, "ice_candidates must be an array of 1 or more entries", false},
		{"refuses a payload that is not an object", cmdProvideOffer, ``, "ProvideOffer payload must be an object", false},
		{"refuses a key that names an Object prototype member", cmdProvideOffer, `{"webRtcSessionId":null,"sdp":"v=0","toString":1}`, `^unknown ProvideOffer payload key: toString\.`, true},
		{"refuses two spellings of the same field", cmdProvideOffer, `{"webRtcSessionId":null,"sdp":"v=0","ice_servers":[{"urls":"stun:a.example"}],"iceServers":[{"urls":"stun:b.example"}]}`, "ProvideOffer payload states iceServers twice, last as iceServers", false},
		{"refuses null for a mandatory field that is not nullable", cmdSolicitOffer, `{"streamUsage":null}`, "streamUsage must not be null", false},
		{"refuses a value whose type the field does not take", cmdProvideOffer, `{"webRtcSessionId":null,"sdp":"v=0","metadataEnabled":"yes"}`, "metadataEnabled must be a boolean", false},
		{"names the key the client sent in the refusal", cmdProvideOffer, `{"webRtcSessionId":null,"sdp":"v=0","ice_servers":[{"urls":5}]}`, "ice_servers[0].urls[0] must be a string of 1 to 2000 characters", false},
	}
	for _, c := range refusals {
		t.Run(c.name, func(t *testing.T) {
			msg := providerRefusal(t, c.command, c.payload)
			if c.pattern && !regexp.MustCompile(c.want).MatchString(msg) || !c.pattern && msg != c.want {
				t.Errorf("refusal %q, want %q", msg, c.want)
			}
		})
	}
	t.Run("resolves the Python Matter Server spelling to the field it names", func(t *testing.T) {
		got := providerRequest(t, cmdProvideOffer, `{"WebRTCSessionID":12,"SDP":"v=0"}`)
		want := wpdef.ProvideOfferRequest{WebRtcSessionId: spec.ValueOf[uint16](12), Sdp: "v=0", OriginatingEndpointId: ptr(requestorEndpoint)}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("request = %+v, want %+v", got, want)
		}
	})
	// The first spelling carries a null the field drops, so nothing records
	// it but the walk.
	t.Run("refuses two spellings of the same field whichever came first", func(t *testing.T) {
		for _, pair := range []string{`"metadataEnabled":null,"metadata_enabled":true`, `"metadata_enabled":true,"metadataEnabled":null`} {
			msg := providerRefusal(t, cmdProvideOffer, `{"webRtcSessionId":null,"sdp":"v=0",`+pair+`}`)
			if !strings.HasPrefix(msg, "ProvideOffer payload states metadataEnabled twice") {
				t.Errorf("%s: refusal %q", pair, msg)
			}
		}
	})
	t.Run("drops a client-supplied originatingEndpointId", func(t *testing.T) {
		got := providerRequest(t, cmdSolicitOffer, `{"streamUsage":3,"originatingEndpointId":99}`)
		want := wpdef.SolicitOfferRequest{StreamUsage: 3, OriginatingEndpointId: requestorEndpoint}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("request = %+v, want %+v", got, want)
		}
	})
	t.Run("requires the fields the command states as mandatory", func(t *testing.T) {
		if msg := providerRefusal(t, cmdProvideOffer, `{"sdp":"v=0"}`); msg != "ProvideOffer payload requires webRtcSessionId" {
			t.Errorf("refusal %q", msg)
		}
		if msg := providerRefusal(t, cmdSolicitOffer, `{}`); msg != "SolicitOffer payload requires streamUsage" {
			t.Errorf("refusal %q", msg)
		}
	})
	t.Run("keeps null for a nullable field and drops it for an optional one", func(t *testing.T) {
		got := providerRequest(t, cmdProvideOffer, `{"webRtcSessionId":null,"sdp":"v=0","metadataEnabled":null}`)
		want := wpdef.ProvideOfferRequest{WebRtcSessionId: spec.NullOf[uint16](), Sdp: "v=0", OriginatingEndpointId: ptr(requestorEndpoint)}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("request = %+v, want %+v", got, want)
		}
	})
	t.Run("bounds every number by the field it becomes", func(t *testing.T) {
		if msg := providerRefusal(t, cmdProvideOffer, `{"webRtcSessionId":65536,"sdp":"v=0"}`); msg != "webRtcSessionId must be an integer between 0 and 65535" {
			t.Errorf("refusal %q", msg)
		}
		if msg := providerRefusal(t, cmdProvideOffer, `{"webRtcSessionId":null,"sdp":"v=0","videoStreams":[65536]}`); msg != "videoStreams[0] must be an integer between 0 and 65535" {
			t.Errorf("refusal %q", msg)
		}
	})
	t.Run("bounds every list by the length the field takes", func(t *testing.T) {
		if msg := providerRefusal(t, cmdProvideOffer, `{"webRtcSessionId":null,"sdp":"v=0","videoStreams":[]}`); msg != "videoStreams must be an array of 1 to 16 entries" {
			t.Errorf("refusal %q", msg)
		}
	})
	t.Run("bounds every string by the length the field takes", func(t *testing.T) {
		if msg := providerRefusal(t, cmdProvideOffer, `{"webRtcSessionId":null,"sdp":"v=0","iceTransportPolicy":"`+strings.Repeat("x", 17)+`"}`); msg != "iceTransportPolicy must be a string of 1 to 16 characters" {
			t.Errorf("refusal %q", msg)
		}
		// sdp states no ceiling, so nothing but its type is checked.
		if msg := providerRefusal(t, cmdProvideOffer, `{"webRtcSessionId":null,"sdp":5}`); msg != "sdp must be a string" {
			t.Errorf("refusal %q", msg)
		}
	})
}
