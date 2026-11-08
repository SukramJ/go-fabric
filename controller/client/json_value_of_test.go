// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/controller/client"
)

// TestValueOfRendersAGeneratedResponse: camera.Provider.Send's
// ProvideOfferResponse renders through ValueOf and DecodeJSON as a
// response read off the wire does — keyed by matter.js's property names
// (webRtcSessionId, the spelling of matterjs-server
// docs/websockets_api.md:1881 at 0bd06f2) in the name form, by field id in
// the tag form, an explicit null kept and an absent field left out.
func TestValueOfRendersAGeneratedResponse(t *testing.T) {
	t.Parallel()
	resp := wpdef.ProvideOfferResponse{
		WebRtcSessionId: 7,
		VideoStreamId:   &spec.Nullable[uint16]{Value: 1},
		AudioStreamId:   &spec.Nullable[uint16]{Null: true},
	}
	v, err := client.ValueOf(resp)
	if err != nil {
		t.Fatal(err)
	}
	f := client.ResponseField(wpdef.ClusterID, wpdef.CmdProvideOfferResponse)
	for form, want := range map[client.Form]string{
		client.NameBased: `{"webRtcSessionId":7,"videoStreamId":1,"audioStreamId":null}`,
		client.TagBased:  `{"0":7,"1":1,"2":null}`,
	} {
		got, err := client.DecodeJSON(f, v, form)
		if err != nil {
			t.Fatal(err)
		}
		if !sameJSON(t, got, []byte(want)) {
			t.Errorf("form %d: %s, want %s", form, got, want)
		}
	}

	absent, err := client.ValueOf(wpdef.ProvideOfferResponse{WebRtcSessionId: 8})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := client.DecodeJSON(f, absent, client.NameBased); err != nil || !sameJSON(t, got, []byte(`{"webRtcSessionId":8}`)) {
		t.Errorf("absent streams: %s (%v)", got, err)
	}

	if _, err := client.ValueOf(nil); !errors.Is(err, client.ErrNoValue) {
		t.Errorf("ValueOf(nil): %v, want ErrNoValue", err)
	}
}
