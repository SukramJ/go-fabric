// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"log/slog"
	"strconv"
	"testing"
)

// TestDeliveryQueue_BoundDropsTheOldest: a search's delivery queue holds
// at most maxQueuedDeliveries devices while found has not taken them; a
// further one drops the oldest queued (review II, R3-bound-3552, owner
// decision 573).
func TestDeliveryQueue_BoundDropsTheOldest(t *testing.T) {
	t.Parallel()
	q := &deliveryQueue{signal: make(chan struct{}, 1)}
	for i := range maxQueuedDeliveries + 3 {
		q.push(Device{InstanceID: strconv.Itoa(i)})
	}
	got := q.drain()
	if len(got) != maxQueuedDeliveries {
		t.Fatalf("queued %d, want %d", len(got), maxQueuedDeliveries)
	}
	if got[0].InstanceID != "3" || got[len(got)-1].InstanceID != strconv.Itoa(maxQueuedDeliveries+2) {
		t.Fatalf("kept %s..%s, want the newest", got[0].InstanceID, got[len(got)-1].InstanceID)
	}
	if again := q.drain(); len(again) != 0 {
		t.Fatalf("second drain %d", len(again))
	}
}

// TestDeliveryQueue_DropIsLogged: the drops are logged with their count
// when the search takes the queue, once per drain (review II,
// R3-bound-3552, owner decision 573).
func TestDeliveryQueue_DropIsLogged(t *testing.T) {
	t.Parallel()
	var log recordedLog
	q := &deliveryQueue{signal: make(chan struct{}, 1), logger: slog.New(&log)}
	for i := range maxQueuedDeliveries + 5 {
		q.push(Device{InstanceID: strconv.Itoa(i)})
	}
	q.drain()
	q.push(Device{InstanceID: "x"})
	q.drain()
	msgs := log.messages("matter.discovery.deliveries_dropped")
	if len(msgs) != 1 || msgs[0]["dropped"] != "5" {
		t.Fatalf("logged %v, want one record of 5 dropped", msgs)
	}
}

// TestDeliveryQueue_DroppedDeviceIsDeliveredAgain: the bound equals the
// waiter's maxSeenInstances, so the device an overflow drops is the one
// the waiter forgets at the same push, and its next announcement is
// delivered again (review II, R3-bound-3552, owner decision 573).
func TestDeliveryQueue_DroppedDeviceIsDeliveredAgain(t *testing.T) {
	t.Parallel()
	q := &deliveryQueue{signal: make(chan struct{}, 1)}
	w := &waiter{identifier: ByLongDiscriminator(3840), seen: newInstanceSet(), push: q.push}
	for i := range maxQueuedDeliveries + 1 {
		w.notify(&Device{InstanceID: strconv.Itoa(i), D: 3840})
	}
	if got := q.drain(); len(got) != maxQueuedDeliveries || got[0].InstanceID != "1" {
		t.Fatalf("queued %d from %s", len(got), got[0].InstanceID)
	}
	w.notify(&Device{InstanceID: "0", D: 3840})
	if got := q.drain(); len(got) != 1 || got[0].InstanceID != "0" {
		t.Fatalf("re-announced device: %v", got)
	}
}
