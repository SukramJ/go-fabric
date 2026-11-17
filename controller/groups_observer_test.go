// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/store"
)

// TestSetGroupKeyObserverClosesTheController (review II, R2-P6-conc-2301,
// N-rw3ctl-1): a groupcast observer the host registered on Groups() runs
// inside SetGroupKey's Reload. When it closes the controller and then calls
// a gated accessor, Close and the accessor return, and so does SetGroupKey.
// Before, SetGroupKey held the closed gate's read lock across Reload, so
// Close waited for the write lock behind the observer that waited for it.
func TestSetGroupKeyObserverClosesTheController(t *testing.T) {
	t.Parallel()
	_, _, c := siteController(t, fabrictest.Topology{}, nil)
	closed := make(chan error, 1)
	var once sync.Once
	c.Groups().OnGroupcastChanged(func(context.Context, uint8) {
		once.Do(func() {
			go func() { closed <- c.Close(context.Background()) }()
			select {
			case err := <-closed:
				if err != nil {
					t.Errorf("Close from the observer: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Error("Close from the groupcast observer did not return: SetGroupKey holds the closed gate across Reload")
				return
			}
			if nodes := c.Nodes(); nodes != nil {
				t.Errorf("Nodes after Close = %v, want nil", nodes)
			}
		})
	})
	keySet := store.GroupKeySet{GroupKeySetID: 0x01A1, SecurityPolicy: store.SecurityPolicyTrustFirst, EpochKey0: make([]byte, 16), EpochStart0: 1}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if err := c.SetGroupKey(ctx, keySet, 0x0101); err != nil {
		t.Fatalf("SetGroupKey: %v", err)
	}
}
