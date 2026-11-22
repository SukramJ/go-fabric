// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/controller/client"
)

// TestSetDefaultSubscriptionFailedInitialReadReplacesNothing
// (R2-P5-apinode-15): on a started, newly commissioned node the change runs
// the initial read first; when that read fails the call returns its error
// and the request is not replaced — the settings, the stored ones and the
// cache's filter keep the previous request. Before, the new request stayed
// set and stored with the cache still on the old filter.
func TestSetDefaultSubscriptionFailedInitialReadReplacesNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		st := &client.MemoryStore{}
		sn := newScriptedNode(t, client.Config{ID: "peer1", Store: st})
		if err := sn.node.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		client.SetNewlyCommissioned(sn.node)
		unfiltered := &client.SubscribeOptions{NoFabricFilter: true}
		// The read goes out and is never answered: the call's context ends.
		if err := sn.node.SetDefaultSubscription(shortContext(t), unfiltered); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("SetDefaultSubscription = %v, want the failed read's context.DeadlineExceeded", err)
		}
		if got := sn.node.DefaultSubscription(); got != nil {
			t.Errorf("the default subscription is %+v after the failed call, want the previous one (nil)", got)
		}
		if got, err := client.StoredDefaultSubscription(st, sn.node.ID()); err != nil || got != nil {
			t.Errorf("the stored default subscription is %+v (%v) after the failed call, want none", got, err)
		}
		if !client.CacheFabricFiltered(sn.node) {
			t.Error("the cache left its fabric filter although the request was not replaced")
		}
	})
}
