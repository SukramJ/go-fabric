// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

// A port of matter.js packages/protocol/test/action/request/SubscribeTest.ts at
// 862ad75f: the SubscribeRequest a client formulates — paths with
// wildcards, keepSubscriptions, the interval bounds and their range,
// fabric filtering, data version and event filters.
//
// matter.js tests the Subscribe() request object; the Go controller
// formulates the request inside Interaction.Subscribe, so each case reads
// the SubscribeRequest off the wire of a scripted peer. Unset interval
// bounds are not left out on the wire: the request then carries the
// defaults ClientInteraction.subscribe fills in (ClientInteraction.ts:1043-1046).

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// subscribeInteraction is an Interaction that may subscribe.
func subscribeInteraction(s *scripted) *interaction.Interaction {
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
	return interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg})
}

// subscribeCaptured runs Interaction.Subscribe against a scripted peer,
// refuses the request with a StatusResponse and returns the
// SubscribeRequest as sent.
func subscribeCaptured(t *testing.T, r interaction.SubscribeRequest) im.SubscribeRequest {
	t.Helper()
	s := newScripted(t)
	done := subscribeAsync(subscribeInteraction(s), r)
	req := s.next(t)
	if req.proto.Opcode != im.OpcodeSubscribeRequest {
		t.Fatalf("opcode 0x%02X", req.proto.Opcode)
	}
	parsed, err := im.UnmarshalSubscribeRequestTLV(tlv.NewDecoder(req.payload))
	if err != nil {
		t.Fatal(err)
	}
	s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusInvalidAction))
	var se *interaction.StatusError
	if res := await(t, done); !errors.As(res.err, &se) {
		t.Fatalf("refused subscription: %v", res.err)
	}
	return parsed
}

var subscribeOnOff = []im.ConcreteAttributePath{readCluster(1, 6)}

func TestSubscribeRequestBuildBasic(t *testing.T) {
	t.Parallel()
	t.Run("creates subscription request for attributes", func(t *testing.T) {
		t.Parallel()
		req := subscribeCaptured(t, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{readAttr(1, 6, 0)}})
		if len(req.AttributeRequests) != 1 || req.AttributeRequests[0] != readAttr(1, 6, 0) {
			t.Fatalf("attributes %+v", req.AttributeRequests) // SubscribeTest.ts:25-28
		}
	})
	t.Run("creates subscription request for events", func(t *testing.T) {
		t.Parallel()
		ev := im.ConcreteEventPath{Endpoint: 0, Cluster: 0x28, Event: 0, HasEndpoint: true, HasCluster: true, HasEvent: true}
		req := subscribeCaptured(t, interaction.SubscribeRequest{Events: []im.ConcreteEventPath{ev}})
		if len(req.EventRequests) != 1 || req.EventRequests[0].Endpoint != 0 || req.EventRequests[0].Cluster != 0x28 {
			t.Fatalf("events %+v", req.EventRequests) // SubscribeTest.ts:42-44
		}
	})
	t.Run("creates subscription for both attributes and events", func(t *testing.T) {
		t.Parallel()
		req := subscribeCaptured(t, interaction.SubscribeRequest{Attributes: subscribeOnOff, Events: []im.ConcreteEventPath{readEvents(0, 0x28)}})
		if len(req.AttributeRequests) != 1 || len(req.EventRequests) != 1 {
			t.Fatalf("request %+v", req) // SubscribeTest.ts:63-64
		}
	})
}

func TestSubscribeRequestBuildKeepSubscriptions(t *testing.T) {
	t.Parallel()
	// keepSubscriptions false is ReplaceSubscriptions true.
	t.Run("defaults keepSubscriptions to true", func(t *testing.T) {
		t.Parallel()
		if req := subscribeCaptured(t, interaction.SubscribeRequest{Attributes: subscribeOnOff}); !req.KeepSubscriptions {
			t.Fatal("keepSubscriptions false") // SubscribeTest.ts:79
		}
	})
	t.Run("sets keepSubscriptions to false when specified", func(t *testing.T) {
		t.Parallel()
		if req := subscribeCaptured(t, interaction.SubscribeRequest{Attributes: subscribeOnOff, ReplaceSubscriptions: true}); req.KeepSubscriptions {
			t.Fatal("keepSubscriptions true") // SubscribeTest.ts:93
		}
	})
	t.Run("sets keepSubscriptions to true explicitly", func(t *testing.T) {
		t.Parallel()
		if req := subscribeCaptured(t, interaction.SubscribeRequest{Attributes: subscribeOnOff, ReplaceSubscriptions: false}); !req.KeepSubscriptions {
			t.Fatal("keepSubscriptions false") // SubscribeTest.ts:107
		}
	})
}

func TestSubscribeRequestBuildIntervals(t *testing.T) {
	t.Parallel()
	t.Run("sets minimum interval floor", func(t *testing.T) {
		t.Parallel()
		req := subscribeCaptured(t, interaction.SubscribeRequest{Attributes: subscribeOnOff, MinIntervalFloor: interaction.Interval(time.Second)})
		if req.MinIntervalFloor != 1 {
			t.Fatalf("minIntervalFloor %d, want 1", req.MinIntervalFloor) // SubscribeTest.ts:124
		}
	})
	t.Run("sets maximum interval ceiling", func(t *testing.T) {
		t.Parallel()
		req := subscribeCaptured(t, interaction.SubscribeRequest{Attributes: subscribeOnOff, MaxIntervalCeiling: interaction.Interval(60 * time.Second)})
		if req.MaxIntervalCeiling != 60 {
			t.Fatalf("maxIntervalCeiling %d, want 60", req.MaxIntervalCeiling) // SubscribeTest.ts:138
		}
	})
	t.Run("sets both min and max intervals", func(t *testing.T) {
		t.Parallel()
		req := subscribeCaptured(t, interaction.SubscribeRequest{
			Attributes:       subscribeOnOff,
			MinIntervalFloor: interaction.Interval(time.Second), MaxIntervalCeiling: interaction.Interval(60 * time.Second),
		})
		if req.MinIntervalFloor != 1 || req.MaxIntervalCeiling != 60 {
			t.Fatalf("intervals %d/%d, want 1/60", req.MinIntervalFloor, req.MaxIntervalCeiling) // SubscribeTest.ts:153-154
		}
	})
	t.Run("allows undefined intervals for default behavior", func(t *testing.T) {
		t.Parallel()
		// DEFAULT_MIN_INTERVAL_FLOOR 1 s and SUBSCRIPTION_MAX_INTERVAL_PUBLISHER_LIMIT
		// 60 min (ClientInteraction.ts:165, :173).
		req := subscribeCaptured(t, interaction.SubscribeRequest{Attributes: subscribeOnOff})
		if req.MinIntervalFloor != 1 || req.MaxIntervalCeiling != 3600 {
			t.Fatalf("intervals %d/%d, want the defaults 1/3600", req.MinIntervalFloor, req.MaxIntervalCeiling)
		}
	})
}

func TestSubscribeRequestBuildIntervalValidation(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name string
		req  interaction.SubscribeRequest
	}{
		{"throws on negative minimum interval floor", interaction.SubscribeRequest{MinIntervalFloor: interaction.Interval(-time.Second)}},
		{"throws on negative maximum interval ceiling", interaction.SubscribeRequest{MaxIntervalCeiling: interaction.Interval(-time.Second)}},
		{"throws on minimum interval floor exceeding UINT16_MAX seconds", interaction.SubscribeRequest{MinIntervalFloor: interaction.Interval(65536 * time.Second)}},
		{"throws on maximum interval ceiling exceeding UINT16_MAX seconds", interaction.SubscribeRequest{MaxIntervalCeiling: interaction.Interval(65536 * time.Second)}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			s := newScripted(t)
			r := row.req
			r.Attributes = subscribeOnOff
			_, err := subscribeInteraction(s).Subscribe(context.Background(), r)
			if !errors.Is(err, interaction.ErrMalformedRequest) || !strings.Contains(err.Error(), "out of range") {
				t.Fatalf("%v, want a malformed request out of range", err) // SubscribeTest.ts:172-228
			}
		})
	}
}

func TestSubscribeRequestBuildWildcards(t *testing.T) {
	t.Parallel()
	t.Run("subscribes to all attributes in a cluster", func(t *testing.T) {
		t.Parallel()
		req := subscribeCaptured(t, interaction.SubscribeRequest{Attributes: subscribeOnOff})
		if len(req.AttributeRequests) != 1 || req.AttributeRequests[0].HasAttribute {
			t.Fatalf("attributes %+v", req.AttributeRequests) // SubscribeTest.ts:242
		}
	})
	t.Run("subscribes to all events in a cluster", func(t *testing.T) {
		t.Parallel()
		req := subscribeCaptured(t, interaction.SubscribeRequest{Events: []im.ConcreteEventPath{readEvents(0, 0x28)}})
		if len(req.EventRequests) != 1 || req.EventRequests[0].HasEvent {
			t.Fatalf("events %+v", req.EventRequests) // SubscribeTest.ts:255
		}
	})
	t.Run("subscribes to all clusters on an endpoint", func(t *testing.T) {
		t.Parallel()
		req := subscribeCaptured(t, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{{Endpoint: 1, HasEndpoint: true}}})
		if len(req.AttributeRequests) != 1 || req.AttributeRequests[0].HasCluster || req.AttributeRequests[0].HasAttribute {
			t.Fatalf("attributes %+v", req.AttributeRequests) // SubscribeTest.ts:267-268
		}
	})
}

func TestSubscribeRequestBuildFabricFiltering(t *testing.T) {
	t.Parallel()
	t.Run("enables fabric filtering by default", func(t *testing.T) {
		t.Parallel()
		if req := subscribeCaptured(t, interaction.SubscribeRequest{Attributes: subscribeOnOff}); !req.FabricFiltered {
			t.Fatal("not fabric-filtered") // SubscribeTest.ts:283
		}
	})
	t.Run("disables fabric filtering when specified", func(t *testing.T) {
		t.Parallel()
		if req := subscribeCaptured(t, interaction.SubscribeRequest{Attributes: subscribeOnOff, NoFabricFilter: true}); req.FabricFiltered {
			t.Fatal("fabric-filtered") // SubscribeTest.ts:297
		}
	})
}

func TestSubscribeRequestBuildDataVersionFilters(t *testing.T) {
	t.Parallel()
	t.Run("includes data version filters", func(t *testing.T) {
		t.Parallel()
		req := subscribeCaptured(t, interaction.SubscribeRequest{
			Attributes:         subscribeOnOff,
			DataVersionFilters: []im.DataVersionFilter{{Endpoint: 1, Cluster: 6, DataVersion: 12345}},
		})
		if len(req.DataVersionFilters) != 1 || req.DataVersionFilters[0].DataVersion != 12345 {
			t.Fatalf("filters %+v", req.DataVersionFilters) // SubscribeTest.ts:318-319
		}
	})
}

func TestSubscribeRequestBuildEventFilters(t *testing.T) {
	t.Parallel()
	t.Run("includes event filters", func(t *testing.T) {
		t.Parallel()
		req := subscribeCaptured(t, interaction.SubscribeRequest{
			Events:       []im.ConcreteEventPath{readEvents(0, 0x28)},
			EventFilters: []im.EventMinimumNumber{{EventMin: 1000}},
		})
		if len(req.EventFilters) != 1 || req.EventFilters[0].EventMin != 1000 {
			t.Fatalf("filters %+v", req.EventFilters) // SubscribeTest.ts:338-339
		}
	})
}
