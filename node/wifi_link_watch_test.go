// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/node"
)

// watchingWiFi is acceptingWiFi with a link watch: it says when the
// server started watching.
type watchingWiFi struct {
	acceptingWiFi
	watching chan struct{}
}

func (w watchingWiFi) WatchLink(ctx context.Context, _ func(contract.WiFiLinkChange)) {
	select {
	case w.watching <- struct{}{}:
	default:
	}
	<-ctx.Done()
}

// TestNodeWiFiDriverLinkWatchStarts (review II, R2-P6-api-2450): a host
// driver given as Network.WiFi.Driver that watches its link
// (contract.WiFiLinkWatcher) is watched once the node starts — the
// server finds the port on the driver the node hands it.
func TestNodeWiFiDriverLinkWatchStarts(t *testing.T) {
	t.Parallel()
	drv := watchingWiFi{watching: make(chan struct{}, 1)}
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "wifi.db")))
	cfg.Network.WiFi = &node.WiFi{Driver: drv}
	n := newNode(t, cfg)
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-drv.watching:
	case <-wait.Done():
		t.Fatal("the node's Wi-Fi server never watched the host driver's link")
	}
}
