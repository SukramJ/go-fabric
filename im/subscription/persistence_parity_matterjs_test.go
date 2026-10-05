// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

// TestParityMatterJS_ReestablishTimeout pins REESTABLISH_SUBSCRIPTIONS_TIMEOUT
// = Seconds(2) from matter.js
// packages/node/src/behavior/system/subscriptions/SubscriptionsServer.ts.
func TestParityMatterJS_ReestablishTimeout(t *testing.T) {
	t.Parallel()
	if ReestablishTimeout != 2*time.Second {
		t.Fatalf("ReestablishTimeout = %s, matter.js REESTABLISH_SUBSCRIPTIONS_TIMEOUT = 2s", ReestablishTimeout)
	}
}

// TestParityMatterJS_PersistedSubscriptionFields: the persisted record
// carries exactly the fields of matter.js SubscriptionsServer's state
// schema entry (`SubscriptionState.subscriptions[]`) — no fewer, so a
// re-established subscription is the same subscription, and no filters,
// which matter.js does not persist either. "v" is this module's encoding
// version.
func TestParityMatterJS_PersistedSubscriptionFields(t *testing.T) {
	t.Parallel()
	b, err := MarshalPeerSubscription(samplePeerSubscription())
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatal(err)
	}
	var got []string
	for k := range top {
		if k != "v" {
			got = append(got, k)
		}
	}
	slices.Sort(got)
	// FieldElement names from the DatatypeModel in SubscriptionsServer.ts.
	want := []string{
		"attributeRequests", "eventRequests", "isFabricFiltered", "maxInterval",
		"maxIntervalCeiling", "minIntervalFloor", "peerAddress", "sendInterval", "subscriptionId",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("persisted fields = %v, matter.js schema = %v", got, want)
	}
	var addr map[string]json.RawMessage
	if err := json.Unmarshal(top["peerAddress"], &addr); err != nil {
		t.Fatal(err)
	}
	if _, ok := addr["fabricIndex"]; !ok {
		t.Error("peerAddress.fabricIndex missing")
	}
	if _, ok := addr["nodeId"]; !ok {
		t.Error("peerAddress.nodeId missing")
	}
	// Attribute path entries use the AttributePath field names matter.js
	// persists (endpointId / clusterId / attributeId, wildcards omitted).
	var attrs []map[string]json.RawMessage
	if err := json.Unmarshal(top["attributeRequests"], &attrs); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"endpointId", "clusterId", "attributeId"} {
		if _, ok := attrs[0][k]; !ok {
			t.Errorf("attribute path field %q missing", k)
		}
	}
	if len(attrs[1]) != 0 {
		t.Errorf("a wildcard path must persist with no fields, got %v", attrs[1])
	}
	// Durations are milliseconds, as matter.js stores `duration` fields.
	var send int64
	if err := json.Unmarshal(top["sendInterval"], &send); err != nil || send != 300_000 {
		t.Errorf("sendInterval = %d (err %v), want 300000 ms", send, err)
	}
}
