// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/bdx"
	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store"
)

// listFailingIn is a credential store whose ListFabrics fails when the
// node calls it from the function named in.
type listFailingIn struct {
	store.Port
	in string
}

func (s *listFailingIn) ListFabrics(ctx context.Context) ([]store.FabricRecord, error) {
	buf := make([]byte, 16<<10)
	if strings.Contains(string(buf[:runtime.Stack(buf, false)]), s.in) {
		return nil, errors.New("listing refused")
	}
	return s.Port.ListFabrics(ctx)
}

// syncLog is a log buffer the node's goroutines write concurrently.
type syncLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *syncLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *syncLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// TestGrantedACLListFailureIsLogged (review II, R2-C2-err-2456): when the
// fabric listing behind the OTA provider's or the WebRTC requestor's
// granted ACL entry fails, the node logs it — the entry is not written,
// and a granted peer refused for it is otherwise refused without a word.
func TestGrantedACLListFailureIsLogged(t *testing.T) {
	t.Parallel()
	grantAll := func(node.FabricInfo) bool { return true }
	for _, tc := range []struct {
		name, in, event string
		adjust          func(t *testing.T, cfg *node.Config) []node.Endpoint
	}{
		{"ota provider", "node.(*Node).ensureOTAProviderACL(", "ota.provider.acl_list_failed", func(_ *testing.T, cfg *node.Config) []node.Endpoint {
			cfg.OTAProvider = &node.OTAProviderConfig{
				ImageSource: &oneImage{img: mattercore.OTAImage{FileDesignator: bdx.FileDesignator("img.ota"), SoftwareVersion: 2, SoftwareVersionString: "2"}},
				GrantFabric: grantAll,
			}
			return nil
		}},
		{"webrtc requestor", "node.(*Node).ensureRequestorACL(", "webrtc.requestor.acl_list_failed", func(t *testing.T, cfg *node.Config) []node.Endpoint {
			t.Helper()
			cfg.WebRTCRequestor = &node.WebRTCRequestorConfig{GrantFabric: grantAll}
			ctl, err := device.NewCameraController(device.Options{StableKey: endpoint.StringKey("ctl:1"), Name: "ctl"})
			if err != nil {
				t.Fatal(err)
			}
			return []node.Endpoint{ctl}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
			if err := store.Apply(ctx, db); err != nil {
				t.Fatal(err)
			}
			st := store.New(db)
			if _, err := st.AddFabric(ctx, store.FabricRecord{FabricID: 0x0A00, NodeID: 0x0B00, VendorID: 0xFFF1, RootPublicKey: make([]byte, 65), CompressedID: [8]byte{1}}); err != nil {
				t.Fatal(err)
			}
			var logged syncLog
			cfg := minimalConfig(db)
			cfg.Store.Credentials = &listFailingIn{Port: st, in: tc.in}
			cfg.Logger = slog.New(slog.NewTextHandler(&logged, nil))
			eps := tc.adjust(t, &cfg)
			n := newNode(t, cfg)
			for _, ep := range eps {
				if err := n.Aggregator().Add(ctx, ep); err != nil {
					t.Fatal(err)
				}
			}
			if err := n.Start(ctx); err != nil {
				t.Fatal(err)
			}
			if out := logged.String(); !strings.Contains(out, tc.event) || !strings.Contains(out, "listing refused") {
				t.Errorf("no %s warning with the store's error after a refused fabric listing:\n%s", tc.event, out)
			}
		})
	}
}
