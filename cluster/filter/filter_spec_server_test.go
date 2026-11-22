// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package filter_test

import (
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/filter"
	hepa "thingfabric.dev/go-fabric/cluster/spec/hepafiltermonitoring"
	"thingfabric.dev/go-fabric/contract"
)

// TestNotifyReachesListeners holds the Notify the server exported when it
// embedded cluster.AttributeChanges: it still reaches the listeners, now
// the generated server's.
func TestNotifyReachesListeners(t *testing.T) {
	t.Parallel()
	srv, err := filter.NewHepaFilterMonitoring(filter.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var seen []uint32
	unsubscribe := srv.OnAttributeChange(func(change contract.Change) { ids := change.Attributes; seen = append(seen, ids...) })
	srv.Notify(hepa.AttrChangeIndication)
	unsubscribe()
	srv.Notify(hepa.AttrChangeIndication)
	if !slices.Equal(seen, []uint32{hepa.AttrChangeIndication}) {
		t.Errorf("notified %v", seen)
	}
}
